package handlers

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/meowrain/localsend-go/internal/config"
	"github.com/meowrain/localsend-go/internal/discovery"
	"github.com/meowrain/localsend-go/internal/discovery/shared"
	"github.com/meowrain/localsend-go/internal/models"
	"github.com/meowrain/localsend-go/internal/utils/logger"
	"github.com/meowrain/localsend-go/internal/utils/sha256"
	"github.com/schollz/progressbar/v3"
)

// baseRequest 构造通用的 PrepareReceiveRequest 信封，只填本次发送的 files。
func baseRequest(files map[string]models.FileInfo) models.PrepareReceiveRequest {
	return models.PrepareReceiveRequest{
		Info: models.Info{
			Alias:       shared.Message.Alias,
			Version:     shared.Message.Version,
			DeviceModel: shared.Message.DeviceModel,
			DeviceType:  shared.Message.DeviceType,
			Fingerprint: shared.Message.Fingerprint,
			Port:        shared.Message.Port,
			Protocol:    shared.Message.Protocol,
			Download:    shared.Message.Download,
		},
		Files: files,
	}
}

// fileMetadata 根据本地文件路径生成协议所需的 FileInfo，ID 使用文件名。
func fileMetadata(filePath string, info os.FileInfo) (models.FileInfo, error) {
	sha256Hash, err := sha256.CalculateSHA256(filePath)
	if err != nil {
		return models.FileInfo{}, fmt.Errorf("error calculating SHA256 hash: %w", err)
	}
	return models.FileInfo{
		ID:       info.Name(), // 使用文件名作为 ID
		FileName: info.Name(),
		Size:     info.Size(),
		// fileType 按协议是 MIME（image/png 等），不是扩展名：
		// 官方 App 依赖它决定预览与保存策略。
		FileType: mimeTypeForPath(filePath),
		SHA256:   sha256Hash,
		// metadata 是 2.1 新增的可空字段。只填 os.FileInfo 能拿到的 modified；
		// accessed（atime）在 Windows 上取不到、Linux/macOS 上又要平台特定 syscall，
		// 收益极低，因此一律留空（协议标注 nullable，接收端必须容忍缺失）。
		Metadata: &models.FileMetadata{Modified: info.ModTime().UTC().Format(time.RFC3339)},
	}, nil
}

// SendFileToOtherDevicePrepare 向对端发起 prepare-upload 请求。
// device 中的 Protocol/Port 决定目标地址；缺失时按 resolveSendTarget 的规则回退。
func SendFileToOtherDevicePrepare(device models.SendModel, path string) (*models.PrepareReceiveResponse, error) {
	// 准备所有文件的元数据
	files := make(map[string]models.FileInfo)
	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			fileInfo, err := fileMetadata(filePath, info)
			if err != nil {
				return err
			}
			files[fileInfo.ID] = fileInfo
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error walking the path: %w", err)
	}
	return sendPrepareRequest(device, baseRequest(files))
}

// sendPrepareRequest 把已构造好的 prepare 请求发出去并解析响应。
// 目标地址由 device 的 Protocol/Port 决定；本机配置了 PIN 时自动带上 ?pin=。
func sendPrepareRequest(device models.SendModel, request models.PrepareReceiveRequest) (*models.PrepareReceiveResponse, error) {
	// 将请求结构体编码为JSON
	requestJson, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("error encoding request to JSON: %w", err)
	}

	// 发送POST请求：协议与端口取自对端广播，不再写死 https/53317
	prepareURL := buildSendURL(device, "/api/localsend/v2/prepare-upload")
	// PIN 走查询参数（协议 4.1）。这里取的是「对端的 PIN」，不是本机接收端的 PIN：
	// 两者含义完全不同，混用会让 `--pin` 互相污染（本机要求 123456，就一定会拿
	// 123456 去开对端）。
	// 用 url.Values 编码，PIN 里出现 & # % 空格之类的字符时才不会把查询串拼坏。
	if pin := config.ConfigData.SendPIN(); pin != "" {
		query := url.Values{}
		query.Set("pin", pin)
		prepareURL += "?" + query.Encode()
	}
	client := &http.Client{
		Timeout: 60 * time.Second, // 传输超时
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // 忽略TLS
			},
		},
	}
	resp, err := client.Post(prepareURL, "application/json", bytes.NewBuffer(requestJson))
	if err != nil {
		return nil, fmt.Errorf("error sending POST request: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应
	if resp.StatusCode != http.StatusOK {
		err := describePrepareError(resp)
		// 204 不是错误：接收端判断这些文件已经在它那里且内容一致，本次传输结束
		if resp.StatusCode == statusFinished {
			return nil, fmt.Errorf("%w: %v", ErrNothingToTransfer, err)
		}
		// 401/403/409/429 补一句可操作的中文提示，直接透出给用户
		if hint := prepareErrorHint(err); hint != "" {
			return nil, fmt.Errorf("%w（%s）", err, hint)
		}
		return nil, err
	}

	// 解码响应JSON为PrepareReceiveResponse结构体
	var prepareReceiveResponse models.PrepareReceiveResponse
	if err := json.NewDecoder(resp.Body).Decode(&prepareReceiveResponse); err != nil {
		return nil, fmt.Errorf("error decoding response JSON: %w", err)
	}

	return &prepareReceiveResponse, nil
}

// uploadRetryMax 是单个文件的上传尝试次数上限（含首次）。
//
// 只有 422（校验和不匹配）会重试：官方发送端对「内容传完但校验失败」最多重试
// 3 次，之后放弃。其余状态码（403 token 失效、409 会话被占、5xx 落盘失败）
// 重试也不会有不同结果，直接失败让用户看到真实原因。
const uploadRetryMax = 3

// retryableUpload 判断这个错误值不值得再传一次，目前只认 422。
func retryableUpload(err error) bool {
	var sce *statusCodeError
	return errors.As(err, &sce) && sce.Status == statusChecksumMismatch
}

// uploadWithRetry 给 uploadContent 加上限次重试。
// open 用来在每次重试时取一份新的内容源：ReadCloser 已经被上一次读到底，
// 复用同一个只会第二次发出 0 字节。
func uploadWithRetry(ctx context.Context, device models.SendModel, sessionId, fileId, token string, open func() (io.ReadCloser, int64, error), progress progressFunc) error {
	var lastErr error
	for attempt := 1; attempt <= uploadRetryMax; attempt++ {
		src, size, err := open()
		if err != nil {
			return err
		}
		err = uploadContent(ctx, device, sessionId, fileId, token, size, src, progress)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryableUpload(err) || ctx.Err() != nil || attempt == uploadRetryMax {
			break
		}
		logger.Warnf("Upload of %s rejected with 422, retrying (%d/%d)", fileId, attempt, uploadRetryMax)
	}
	return lastErr
}

// progressFunc 是发送进度的回调：fileID 为当前文件，done/total 为字节数。
// 为 nil 时使用终端进度条；Web 模式传入自己的实现，避免往 stderr 写 ANSI 控制字符。
type progressFunc func(fileID string, done, total int64)

// progressSink 把写入字节数换算成累计进度并回调，供 Web 端读取。
type progressSink struct {
	fn     progressFunc
	fileID string
	total  int64
	done   int64
}

func (p *progressSink) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	p.fn(p.fileID, p.done, p.total)
	return len(b), nil
}

// uploadContent 通过 /upload 接口发送一段内容，size 为总字节数，src 由调用方负责关闭。
// 目标协议与端口取自对端广播的设备信息（device.Protocol / device.Port）。
func uploadContent(ctx context.Context, device models.SendModel, sessionId, fileId, token string, size int64, src io.ReadCloser, progress progressFunc) error {
	defer src.Close()

	// 创建进度条（仅在终端模式下；Web 模式由 progress 回调接管）
	var bar *progressbar.ProgressBar
	if progress == nil {
		bar = progressbar.NewOptions64(
			size,
			progressbar.OptionSetDescription(fmt.Sprintf("上传 %s", fileId)),
			progressbar.OptionSetWidth(15),
			progressbar.OptionShowBytes(true),
			progressbar.OptionThrottle(time.Second), // 降低刷新频率，减少闪烁
			progressbar.OptionShowCount(),
			progressbar.OptionClearOnFinish(), // 完成时清除进度条
			progressbar.OptionSetRenderBlankState(true),
			progressbar.OptionSetPredictTime(true), // 预测剩余时间
			progressbar.OptionFullWidth(),          // 使用全宽显示
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        "█", // 使用实心方块
				SaucerHead:    "█",
				SaucerPadding: "░", // 使用灰色方块作为背景
				BarStart:      "|",
				BarEnd:        "|",
			}),
			progressbar.OptionOnCompletion(func() {
				fmt.Fprint(os.Stderr, "\n")
			}),
		)
	}

	// Web 模式用回调汇报进度；终端模式直接写进度条
	var sink io.Writer = io.Discard
	if progress != nil {
		sink = &progressSink{fn: progress, fileID: fileId, total: size}
	} else {
		sink = bar
	}

	// 构建文件上传的 URL：协议与端口取自对端广播的设备信息；
	// 查询参数统一用 url.Values 编码，中文、空格、&、#、% 等字符会被正确转义，
	// 避免接收端解析出错误的 fileId/token。
	query := url.Values{}
	query.Set("sessionId", sessionId)
	query.Set("fileId", fileId)
	query.Set("token", token)
	uploadURL := buildSendURL(device, "/api/localsend/v2/upload") + "?" + query.Encode()

	// 使用 pipe 来避免将整个文件加载到内存中
	pr, pw := io.Pipe()

	// 创建一个错误通道来传递上传过程中的错误
	uploadErr := make(chan error, 1)

	go func() {
		defer pw.Close()
		// 在新的 goroutine 中写出文件数据
		_, err := io.Copy(io.MultiWriter(pw, sink), src)
		if err != nil {
			uploadErr <- err
			return
		}
	}()

	// 创建带有 TLS 配置的 HTTP 客户端
	client := &http.Client{
		Timeout: 30 * time.Minute,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // 跳过证书验证
			},
			MaxIdleConns:       100,
			IdleConnTimeout:    90 * time.Second,
			DisableCompression: true,
		},
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "POST", uploadURL, pr)
	if err != nil {
		return fmt.Errorf("error creating POST request: %w", err)
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = size

	// 使用自定义客户端发送请求，而不是 http.DefaultClient
	resp, err := client.Do(req)

	// 检查是否被取消
	select {
	case <-ctx.Done():
		return fmt.Errorf("传输已取消")
	case werr := <-uploadErr:
		if werr != nil {
			return fmt.Errorf("上传出错: %w", werr)
		}
	default:
	}
	// 先确认请求本身成功，再解引用 resp：client.Do 失败时 resp 为 nil。
	if err != nil {
		// 关闭 pipe 读端，避免写 goroutine 因为没人消费而永久阻塞。
		_ = pr.CloseWithError(err)
		return fmt.Errorf("error sending file upload request: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应
	if resp.StatusCode != http.StatusOK {
		return describeUploadError(resp)
	}

	if progress == nil {
		fmt.Println() // 添加换行，让进度条显示更清晰
		logger.Success("File uploaded successfully")
	}
	return nil
}

// uploadEntry 描述一个待上传条目：如何匹配接收端返回的 token，以及数据从哪里来。
type uploadEntry struct {
	file localFile
	// fileSize 是条目大小，供 Web 进度统计使用；0 表示未知，上传时以 open 返回的为准。
	fileSize int64
	// open 返回该条目的内容、总字节数与错误；返回的 ReadCloser 由上传流程负责关闭。
	open func() (io.ReadCloser, int64, error)
}

// sendSpec 描述一次发送：prepare 阶段要声明的文件，以及 upload 阶段要逐个上传的条目。
type sendSpec struct {
	prepare func(device models.SendModel) (*models.PrepareReceiveResponse, error)
	// entries 列出本次实际要上传的条目（顺序即上传顺序）。
	entries []uploadEntry
	// progress 非 nil 时用它汇报进度（Web 模式）；nil 表示用终端进度条。
	progress progressFunc
	// ctx 非 nil 时作为上传链路的上游取消源（Web 任务用）；nil 表示由本流程自建。
	ctx context.Context
}

// DeviceSelector 从设备快照流中选择一个发送目标。
// 终端 UI、测试和 Web 调用方各自提供实现，handlers 不感知 UI。
type DeviceSelector func(updates <-chan []models.SendModel) (models.SendModel, error)

// contextOrBackground 返回 spec 的上游 context，nil 时退化为 Background。
func (s sendSpec) contextOrBackground() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

// prepareAndUpload 复用同一套 prepare/upload 流程：先声明文件，再逐个上传内容。
// target 非 nil 时直接向该设备发送，不走 TUI 选择（Web 模式用）。
func prepareAndUpload(spec sendSpec, target *models.SendModel, selectDevice DeviceSelector) error {
	var device models.SendModel
	if target != nil {
		device = *target
	} else {
		if selectDevice == nil {
			return fmt.Errorf("device selector is required")
		}
		updates := make(chan []models.SendModel)
		discovery.ListenAndStartBroadcasts(updates)
		fmt.Println("Please select a device you want to send file to:")
		selected, err := selectDevice(updates)
		if err != nil {
			return err
		}
		device = selected
	}
	response, err := spec.prepare(device)
	if err != nil {
		// 204：接收端判断无需传输，这是正常结束，不是失败
		if errors.Is(err, ErrNothingToTransfer) {
			logger.Success("Receiver already has all files, nothing to transfer")
			return nil
		}
		return err
	}
	if response == nil {
		return nil
	}

	// 创建一个用于取消的 context。
	// spec.ctx 非 nil 时（Web 任务）它就是上游：任务取消会传导到每一次上传请求；
	// 同时仍然注册 cancel handler，接收端 /cancel 也能中断（cancelUpstream 负责两者都触发）。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopUpstream := context.AfterFunc(spec.contextOrBackground(), cancel)
	defer stopUpstream()

	// 使用共享的 HTTP 服务器来处理取消请求
	logger.Info("Registering cancel handler for session: ", response.SessionID)
	RegisterCancelHandler(response.SessionID, cancel)
	defer UnregisterCancelHandler(response.SessionID)

	for _, entry := range spec.entries {
		fileID, token, ok := matchUploadTarget(response.Files, entry.file, len(spec.entries))
		if !ok {
			return fmt.Errorf("error walking the path: %w", fmt.Errorf("token not found for file: %s", entry.file.name))
		}
		// 进度回调按「本地条目的稳定 key」上报，而不是接收端返回的 fileID：
		// 官方 App 会重写 fileId，去回调一个前端不认识的 id 只会让进度原地不动。
		prog := spec.progress
		if prog != nil {
			key := entry.file.relID
			if key == "" {
				key = entry.file.name
			}
			inner := prog
			prog = func(_ string, done, total int64) { inner(key, done, total) }
		}
		if err := uploadWithRetry(ctx, device, response.SessionID, fileID, token, entry.open, prog); err != nil {
			return fmt.Errorf("error walking the path: %w", fmt.Errorf("error uploading file: %w", err))
		}
	}
	return nil
}

// SendFile 函数
func SendFile(path string, selectDevice DeviceSelector) error {
	// 先罗列出所有待发送文件，prepare 与 upload 两个阶段共用同一份清单。
	var entries []uploadEntry
	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relID, err := relativeFileID(path, filePath)
		if err != nil {
			return err
		}
		localPath := filePath
		entries = append(entries, uploadEntry{
			file: localFile{
				path:  localPath,
				name:  info.Name(),
				relID: relID,
			},
			open: func() (io.ReadCloser, int64, error) {
				file, err := os.Open(localPath)
				if err != nil {
					return nil, 0, fmt.Errorf("error opening file: %w", err)
				}
				fileInfo, err := file.Stat()
				if err != nil {
					file.Close()
					return nil, 0, fmt.Errorf("error getting file info: %w", err)
				}
				return file, fileInfo.Size(), nil
			},
		})
		return nil
	})
	if err != nil {
		return fmt.Errorf("error walking the path: %w", err)
	}

	return prepareAndUpload(sendSpec{
		prepare: func(device models.SendModel) (*models.PrepareReceiveResponse, error) {
			return SendFileToOtherDevicePrepare(device, path)
		},
		entries: entries,
	}, nil, selectDevice)
}

// SendText 把一段文本封装成虚拟 .txt 文件，走与 SendFile 相同的 prepare/upload 流程。
func SendText(text string, selectDevice DeviceSelector) error {
	if strings.TrimSpace(text) == "" {
		return ErrEmptyText
	}
	content := []byte(text)
	// fileId 与 fileName 都取自同一个名字，保证 prepare 与 upload 阶段一致。
	fileName := textFileName(time.Now())
	fileInfo := models.FileInfo{
		ID:       fileName,
		FileName: fileName,
		Size:     int64(len(content)),
		FileType: defaultTextMimeType, // 协议要求 fileType 是 MIME，不是扩展名
		SHA256:   sha256OfBytes(content),
		Preview:  TruncatePreview(text),
		// 虚拟文件没有真实 mtime，用当前时间，保证接收端能拿到一个可解析的 metadata
		Metadata: &models.FileMetadata{Modified: time.Now().UTC().Format(time.RFC3339)},
	}

	return prepareAndUpload(sendSpec{
		prepare: func(device models.SendModel) (*models.PrepareReceiveResponse, error) {
			return sendPrepareRequest(device, baseRequest(map[string]models.FileInfo{
				fileInfo.ID: fileInfo,
			}))
		},
		entries: []uploadEntry{{
			file: localFile{name: fileInfo.FileName, relID: fileInfo.FileName},
			open: func() (io.ReadCloser, int64, error) {
				return io.NopCloser(bytes.NewReader(content)), int64(len(content)), nil
			},
		}},
	}, nil, selectDevice)
}

// localFile 是待上传文件在本地的描述。
type localFile struct {
	path  string // 本地绝对/相对路径
	name  string // 文件名（basename），也是 prepare 阶段使用的 fileId
	relID string // 相对发送根目录的路径，用于匹配接收端重写过的 fileId
}

// relativeFileID 返回 filePath 相对 root 的路径（统一用 /），用作备选的 fileId。
func relativeFileID(root, filePath string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fileAbs, err := filepath.Abs(filePath)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(rootAbs); err == nil && !info.IsDir() {
		return filepath.Base(fileAbs), nil
	}
	rel, err := filepath.Rel(rootAbs, fileAbs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// matchUploadTarget 在接收端返回的 fileId->token 映射里找到当前文件对应的条目。
// 官方 App 会用自己生成的 id 覆盖我们提交的 fileId，因此按「相对路径 -> 文件名」依次回退匹配。
//
// localCount 是本次待上传的文件总数：只有「本地只有一个文件、接收端也只回了一个 token」
// 时才允许兜底，否则多文件场景下会让所有文件共用同一个 token，互相覆盖。
func matchUploadTarget(tokens map[string]string, f localFile, localCount int) (string, string, bool) {
	for _, candidate := range []string{f.name, f.relID} {
		if candidate == "" {
			continue
		}
		if token, ok := tokens[candidate]; ok {
			return candidate, token, true
		}
	}
	// 只有一个文件且接收端只回了一个 token 时，直接用它
	if localCount == 1 && len(tokens) == 1 {
		for id, token := range tokens {
			return id, token, true
		}
	}
	return "", "", false
}

// NormalSendHandler 是 Web 页面旧版上传入口（POST /send，multipart）。
//
// 新版页面已经改用 /api/web/upload（逐文件进度、保留多级目录、冲突策略），
// 这里保留是为了兼容旧书签/脚本：
//   - directoryName 与旧版一样只允许「单级目录名」，不再直接 filepath.Join；
//   - 上传大小受 config.functions.web_upload_limit_mb 限制；
//   - 写盘走临时文件 + rename，失败不会留下半截文件。
func NormalSendHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	logger.Info("Handling upload request...")

	if limit := config.ConfigData.WebUploadLimitBytes(); limit > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, limit)
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, fmt.Sprintf("上传内容超过上限 %d MiB", maxErr.Limit>>20), http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, fmt.Sprintf("解析表单失败: %v", err), http.StatusBadRequest)
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	// directoryName 只接受单级目录名："../x"、"a/b" 都会被拒绝，
	// 避免旧接口成为目录穿越入口。
	finalUploadDir, err := webAbsPath(r.FormValue("directoryName"))
	if err != nil {
		http.Error(w, "非法目录名", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		http.Error(w, "未上传任何文件", http.StatusBadRequest)
		return
	}

	if err := os.MkdirAll(finalUploadDir, 0o755); err != nil {
		http.Error(w, fmt.Sprintf("无法创建上传目录: %v", err), http.StatusInternalServerError)
		return
	}

	for _, fileHeader := range files {
		name, err := sanitizeFileName(fileHeader.Filename)
		if err != nil {
			http.Error(w, "非法文件名", http.StatusBadRequest)
			return
		}
		if _, err := saveMultipartFile(fileHeader, filepath.Join(finalUploadDir, name)); err != nil {
			logger.Errorf("Save uploaded file %s failed: %v", name, err)
			http.Error(w, fmt.Sprintf("保存文件失败: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "文件上传成功，共计 %d 个文件\n", len(files))
}
