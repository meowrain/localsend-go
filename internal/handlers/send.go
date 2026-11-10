package handlers

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/meowrain/localsend-go/internal/discovery"
	"github.com/meowrain/localsend-go/internal/discovery/shared"
	"github.com/meowrain/localsend-go/internal/models"
	"github.com/meowrain/localsend-go/internal/tui"
	"github.com/meowrain/localsend-go/internal/utils/logger"
	"github.com/meowrain/localsend-go/internal/utils/sha256"
	"github.com/schollz/progressbar/v3"
)

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
			sha256Hash, err := sha256.CalculateSHA256(filePath)
			if err != nil {
				return fmt.Errorf("error calculating SHA256 hash: %w", err)
			}
			fileMetadata := models.FileInfo{
				ID:       info.Name(), // 使用文件名作为 ID
				FileName: info.Name(),
				Size:     info.Size(),
				FileType: filepath.Ext(filePath),
				SHA256:   sha256Hash,
			}
			files[fileMetadata.ID] = fileMetadata
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error walking the path: %w", err)
	}

	// 创建并填充 PrepareReceiveRequest 结构体
	request := models.PrepareReceiveRequest{
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

	// 将请求结构体编码为JSON
	requestJson, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("error encoding request to JSON: %w", err)
	}

	// 发送POST请求：协议与端口取自对端广播，不再写死 https/53317
	prepareURL := buildSendURL(device, "/api/localsend/v2/prepare-upload")
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
		switch resp.StatusCode {
		case 204:
			return nil, fmt.Errorf("finished (No file transfer needed)")
		case 400:
			return nil, fmt.Errorf("invalid body")
		case 403:
			return nil, fmt.Errorf("rejected")
		case 500:
			return nil, fmt.Errorf("unknown error by receiver")
		}
		return nil, fmt.Errorf("failed to send metadata: received status code %d", resp.StatusCode)
	}

	// 解码响应JSON为PrepareReceiveResponse结构体
	var prepareReceiveResponse models.PrepareReceiveResponse
	if err := json.NewDecoder(resp.Body).Decode(&prepareReceiveResponse); err != nil {
		return nil, fmt.Errorf("error decoding response JSON: %w", err)
	}

	return &prepareReceiveResponse, nil
}

// uploadFile 上传单个文件，目标协议与端口同样取自对端广播的设备信息。
func uploadFile(ctx context.Context, device models.SendModel, sessionId, fileId, token, filePath string) error {
	// 打开要发送的文件
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()

	// 获取文件大小用于进度条
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("error getting file info: %w", err)
	}
	fileSize := fileInfo.Size()

	// 创建进度条
	bar := progressbar.NewOptions64(
		fileSize,
		progressbar.OptionSetDescription(fmt.Sprintf("上传 %s", filepath.Base(filePath))),
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
		// 在新的 goroutine 中写入文件数据
		_, err := io.Copy(io.MultiWriter(pw, bar), file)
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
	req.ContentLength = fileSize

	// 使用自定义客户端发送请求，而不是 http.DefaultClient
	resp, err := client.Do(req)

	// 检查是否被取消
	select {
	case <-ctx.Done():
		return fmt.Errorf("传输已取消")
	case err := <-uploadErr:
		if err != nil {
			return fmt.Errorf("上传出错: %w", err)
		}
	default:
		if err != nil {
			return fmt.Errorf("error sending file upload request: %w", err)
		}
	}

	// 检查响应
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case 400:
			return fmt.Errorf("missing parameters")
		case 403:
			return fmt.Errorf("invalid token or IP address")
		case 409:
			return fmt.Errorf("blocked by another session")
		case 500:
			return fmt.Errorf("unknown error by receiver")
		}
		return fmt.Errorf("file upload failed: received status code %d", resp.StatusCode)
	}

	fmt.Println() // 添加换行，让进度条显示更清晰
	logger.Success("File uploaded successfully")
	return nil
}

// SendFile 函数
func SendFile(path string) error {
	updates := make(chan []models.SendModel)
	discovery.ListenAndStartBroadcasts(updates)
	fmt.Println("Please select a device you want to send file to:")
	device, err := tui.SelectDeviceInfo(updates)
	if err != nil {
		return err
	}
	response, err := SendFileToOtherDevicePrepare(device, path)
	if err != nil {
		return err
	}

	// 创建一个用于取消的 context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 使用共享的 HTTP 服务器来处理取消请求
	logger.Info("Registering cancel handler for session: ", response.SessionID)
	RegisterCancelHandler(response.SessionID, cancel)
	defer UnregisterCancelHandler(response.SessionID)

	var localFiles []localFile
	err = filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
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
		localFiles = append(localFiles, localFile{
			path:  filePath,
			name:  info.Name(),
			relID: relID,
		})
		return nil
	})
	if err != nil {
		return fmt.Errorf("error walking the path: %w", err)
	}

	// 遍历目录和子文件
	for _, f := range localFiles {
		fileID, token, ok := matchUploadTarget(response.Files, f)
		if !ok {
			return fmt.Errorf("error walking the path: %w", fmt.Errorf("token not found for file: %s", f.name))
		}
		if err := uploadFile(ctx, device, response.SessionID, fileID, token, f.path); err != nil {
			return fmt.Errorf("error walking the path: %w", fmt.Errorf("error uploading file: %w", err))
		}
	}

	return nil
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
func matchUploadTarget(tokens map[string]string, f localFile) (string, string, bool) {
	for _, candidate := range []string{f.name, f.relID} {
		if candidate == "" {
			continue
		}
		if token, ok := tokens[candidate]; ok {
			return candidate, token, true
		}
	}
	// 只有一个文件且接收端只回了一个 token 时，直接用它
	if len(tokens) == 1 {
		for id, token := range tokens {
			return id, token, true
		}
	}
	return "", "", false
}

func NormalSendHandler(w http.ResponseWriter, r *http.Request) {
	logger.Info("Handling upload request...") // Debug log - request start

	// 限制表单数据大小（此处设置为 10 MB，可根据需要调整）
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, fmt.Sprintf("解析表单失败: %v", err), http.StatusBadRequest)
		return
	}

	// 获取上传的目录名 (来自前端 hidden input)
	uploadedDirName := r.FormValue("directoryName")
	logger.Debugf("directoryName from form: '%s'\n", uploadedDirName) // Debug log - directoryName value

	// 获取所有上传的文件
	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		http.Error(w, "未上传任何文件", http.StatusBadRequest)
		return
	}

	uploadDir := "./uploads"    // 基础上传目录
	finalUploadDir := uploadDir // 默认最终上传目录

	// 如果前端传递了目录名且不为空，才创建以目录名命名的子目录
	if uploadedDirName != "" {
		finalUploadDir = filepath.Join(uploadDir, uploadedDirName)
	} else {
		logger.Debug("No directoryName provided, uploading to root uploads dir.") // Debug log - no directoryName
	}
	logger.Debugf("Final upload directory: '%s'\n", finalUploadDir)

	// 创建最终的上传目录（如果不存在）
	if err := os.MkdirAll(finalUploadDir, os.ModePerm); err != nil {
		http.Error(w, fmt.Sprintf("无法创建上传目录: %v", err), http.StatusInternalServerError)
		return
	}

	// 遍历所有文件进行保存
	for _, fileHeader := range files {
		// 打开上传的文件
		file, err := fileHeader.Open()
		if err != nil {
			http.Error(w, fmt.Sprintf("无法打开文件: %v", err), http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// 拼接目标路径 (使用 finalUploadDir 作为根目录)
		destPath := filepath.Join(finalUploadDir, fileHeader.Filename)
		logger.Infof("Saving file '%s' to destPath: '%s'\n", fileHeader.Filename, destPath) // Debug log - file dest path

		// 创建目标目录（如果不存在）
		if err := os.MkdirAll(filepath.Dir(destPath), os.ModePerm); err != nil {
			http.Error(w, fmt.Sprintf("无法创建目录: %v", err), http.StatusInternalServerError)
			return
		}

		// 创建目标文件
		dst, err := os.Create(destPath)
		if err != nil {
			http.Error(w, fmt.Sprintf("无法创建文件: %v", err), http.StatusInternalServerError)
			return
		}
		defer dst.Close()

		// 将上传的文件内容写入目标文件
		if _, err := io.Copy(dst, file); err != nil {
			http.Error(w, fmt.Sprintf("保存文件失败: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "文件上传成功，共计 %d 个文件，上传到目录: %s\n", len(files), finalUploadDir)
}
