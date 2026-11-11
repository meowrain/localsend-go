package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/meowrain/localsend-go/internal/discovery"
	"github.com/meowrain/localsend-go/internal/discovery/shared"
	"github.com/meowrain/localsend-go/internal/models"
	"github.com/meowrain/localsend-go/internal/utils/logger"
	"github.com/meowrain/localsend-go/internal/utils/sha256"
)

// Web 端主动发送：
//
//	GET  /api/web/devices          列出局域网内已发现的设备
//	POST /api/web/send             把 uploads 下的文件/目录或一段文本发给指定设备
//	GET  /api/web/send/status?id=  轮询任务进度
//	POST /api/web/send/cancel?id=  取消任务
//
// 发送任务在后台 goroutine 里执行，HTTP 请求立即返回任务 ID；
// 进度通过轮询读取，避免在 HTTP 层长时间挂起连接。

// webDevice 是给前端展示的设备信息。
type webDevice struct {
	Alias       string `json:"alias"`
	DeviceModel string `json:"deviceModel"`
	DeviceType  string `json:"deviceType"`
	IP          string `json:"ip"`
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`
}

// WebDevices 返回当前发现的设备列表。
func WebDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeWebError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	shared.DevicesMutex.RLock()
	devices := make([]webDevice, 0, len(shared.DiscoveredDevices))
	for ip, d := range shared.DiscoveredDevices {
		devices = append(devices, webDevice{
			Alias:       d.Alias,
			DeviceModel: d.DeviceModel,
			DeviceType:  d.DeviceType,
			IP:          ip,
			Port:        d.Port,
			Protocol:    d.Protocol,
		})
	}
	shared.DevicesMutex.RUnlock()
	sort.Slice(devices, func(i, j int) bool {
		return strings.ToLower(devices[i].Alias) < strings.ToLower(devices[j].Alias)
	})
	writeWebJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

// ensureDiscovery 保证 Web 模式下设备发现只启动一次。
// 传 nil 频道：发现结果已经写进 shared.DiscoveredDevices，设备列表直接读它。
var ensureDiscovery sync.Once

// EnsureDiscovery 启动设备发现（幂等）。
func EnsureDiscovery() {
	ensureDiscovery.Do(func() {
		discovery.ListenAndStartBroadcasts(nil)
	})
}

// webSendFileStatus 是任务里单个文件的进度。
type webSendFileStatus struct {
	Name  string `json:"name"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

// sendJob 是一次 Web 发送任务的进度记录。
type sendJob struct {
	id string

	mu      sync.Mutex
	state   string // preparing / transferring / done / error
	message string
	files   []*webSendFileStatus
	// index 把 progress 回调里的 fileID 映射到 files 条目
	index map[string]*webSendFileStatus
	total int64 // 全部文件总字节数
	sent  int64 // 已发送字节数（跨文件累计）
	// cancel 在任务开始时设置；cancelRequested 覆盖「还没开始就点取消」
	cancel          context.CancelFunc
	cancelRequested bool
	started         time.Time
}

func (j *sendJob) setState(state, msg string) {
	j.mu.Lock()
	j.state = state
	j.message = msg
	j.mu.Unlock()
}

// snapshot 返回给前端的进度视图。
func (j *sendJob) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	files := make([]webSendFileStatus, 0, len(j.files))
	for _, f := range j.files {
		files = append(files, *f)
	}
	return map[string]any{
		"id":      j.id,
		"state":   j.state,
		"message": j.message,
		"files":   files,
		"total":   j.total,
		"sent":    j.sent,
		"elapsed": time.Since(j.started).Seconds(),
	}
}

// progress 更新某个文件的进度。
// fileID 是 prepare 阶段提交的 id（相对路径或文件名），保证与 index 的键一致。
func (j *sendJob) progress(fileID string, done, total int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	f, ok := j.index[fileID]
	if !ok {
		return
	}
	delta := done - f.Done
	f.Done = done
	if total > 0 {
		f.Total = total
	}
	if delta > 0 {
		j.sent += delta
	}
}

var (
	sendJobsMu sync.Mutex
	sendJobs   = map[string]*sendJob{}
)

func registerSendJob(j *sendJob) {
	sendJobsMu.Lock()
	sendJobs[j.id] = j
	sendJobsMu.Unlock()
}

func lookupSendJob(id string) *sendJob {
	sendJobsMu.Lock()
	defer sendJobsMu.Unlock()
	return sendJobs[id]
}

func removeSendJob(id string) {
	sendJobsMu.Lock()
	delete(sendJobs, id)
	sendJobsMu.Unlock()
}

// webSendRequest 是发送请求体。
type webSendRequest struct {
	Device webDevice `json:"device"`
	Paths  []string  `json:"paths"`
	Text   string    `json:"text"`
}

// WebSend 发起一次发送任务，立即返回任务 ID，进度通过 status 接口轮询。
func WebSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeWebError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req webSendRequest
	if err := decodeWebJSON(w, r, &req); err != nil {
		writeWebError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	if strings.TrimSpace(req.Device.IP) == "" {
		writeWebError(w, http.StatusBadRequest, "缺少目标设备")
		return
	}
	if strings.TrimSpace(req.Text) == "" && len(req.Paths) == 0 {
		writeWebError(w, http.StatusBadRequest, "没有要发送的内容")
		return
	}

	device := models.SendModel{
		DeviceName: req.Device.Alias,
		IP:         req.Device.IP,
		Port:       req.Device.Port,
		Protocol:   req.Device.Protocol,
	}

	// 先解析待发送条目并统计总大小，任何错误都同步返回给前端
	var (
		spec sendSpec
		err  error
	)
	if strings.TrimSpace(req.Text) != "" {
		spec, err = specForText(req.Text)
	} else {
		spec, err = specForUploadPaths(req.Paths)
	}
	if err != nil {
		logWebError(w, "send prepare", err)
		return
	}

	job := &sendJob{
		id:      newWebToken(),
		state:   "preparing",
		index:   map[string]*webSendFileStatus{},
		started: time.Now(),
	}
	for _, entry := range spec.entries {
		// prepare 与 upload 都用 relID 作为 fileId（见 prepareFilesForPaths），
		// 因此进度回调里的 id 也一定能在 index 里查到。
		key := entry.file.relID
		if key == "" {
			key = entry.file.name
		}
		st := &webSendFileStatus{Name: entry.file.name}
		job.files = append(job.files, st)
		job.index[key] = st
		job.total += entry.fileSize
	}
	registerSendJob(job)
	go runSendJob(job, device, spec)

	writeWebJSON(w, http.StatusAccepted, map[string]string{"id": job.id})
}

// runSendJob 在后台执行发送；结束后保留任务 10 分钟供前端读取最终状态。
func runSendJob(job *sendJob, device models.SendModel, spec sendSpec) {
	time.AfterFunc(10*time.Minute, func() { removeSendJob(job.id) })

	ctx, cancel := context.WithCancel(context.Background())
	job.mu.Lock()
	job.cancel = cancel
	cancelNow := job.cancelRequested
	job.mu.Unlock()
	defer cancel()
	if cancelNow {
		job.setState("error", "已取消")
		return
	}

	spec.ctx = ctx
	spec.progress = func(fileID string, done, total int64) {
		job.progress(fileID, done, total)
	}
	job.setState("transferring", "")

	err := prepareAndUpload(spec, &device, nil)
	switch {
	case err == nil:
		job.setState("done", "发送完成")
		recordWebSend(spec, device)
	case errors.Is(err, context.Canceled) || ctx.Err() != nil:
		job.setState("error", "已取消")
	default:
		logger.Errorf("Web send failed: %v", err)
		job.setState("error", err.Error())
	}
}

// recordWebSend 把一次成功的 Web 发送写进传输历史。
func recordWebSend(spec sendSpec, device models.SendModel) {
	peer := device.IP
	if device.DeviceName != "" {
		peer = device.DeviceName + " (" + device.IP + ")"
	}
	for _, entry := range spec.entries {
		recordHistory(historyEntry{
			Direction: "send",
			FileName:  entry.file.name,
			Size:      entry.fileSize,
			Peer:      peer,
			SavedPath: entry.file.relID,
			OK:        true,
		})
	}
}

// WebSendStatus 返回任务进度。
func WebSendStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeWebError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	job := lookupSendJob(r.URL.Query().Get("id"))
	if job == nil {
		writeWebError(w, http.StatusNotFound, "任务不存在")
		return
	}
	writeWebJSON(w, http.StatusOK, job.snapshot())
}

// WebSendCancel 取消任务。
func WebSendCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeWebError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	job := lookupSendJob(r.URL.Query().Get("id"))
	if job == nil {
		writeWebError(w, http.StatusNotFound, "任务不存在")
		return
	}
	job.mu.Lock()
	job.cancelRequested = true
	cancel := job.cancel
	job.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	writeWebJSON(w, http.StatusOK, map[string]string{"id": job.id})
}

// webSendFile 是待发送的本地文件条目。
type webSendFile struct {
	abs   string // 绝对路径
	name  string // basename
	relID string // 相对 uploads 的路径，作为 fileId（保证不同目录下同名文件不冲突）
	size  int64
}

// collectWebSendFiles 把路径列表展开成文件条目（目录递归），跳过隐藏文件。
func collectWebSendFiles(paths []string) ([]webSendFile, error) {
	baseAbs, err := filepath.Abs(uploadBaseDir)
	if err != nil {
		return nil, err
	}
	var out []webSendFile
	for _, raw := range paths {
		clean, err := webRelPath(raw)
		if err != nil || clean == "" {
			return nil, fmt.Errorf("%w: %s", ErrInvalidFileName, raw)
		}
		abs, err := webFilePath(clean)
		if err != nil {
			return nil, err
		}
		st, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			rel, err := filepath.Rel(baseAbs, abs)
			if err != nil {
				return nil, err
			}
			out = append(out, webSendFile{abs: abs, name: st.Name(), relID: filepath.ToSlash(rel), size: st.Size()})
			continue
		}
		err = filepath.Walk(abs, func(p string, fi os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if fi.IsDir() || strings.HasPrefix(fi.Name(), ".") {
				return nil
			}
			rel, err := filepath.Rel(baseAbs, p)
			if err != nil {
				return err
			}
			out = append(out, webSendFile{abs: p, name: fi.Name(), relID: filepath.ToSlash(rel), size: fi.Size()})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, errors.New("没有可发送的文件")
	}
	return out, nil
}

// prepareFilesForWebSend 为 uploads 下的文件构造 prepare 请求。
// fileId 使用相对 uploads 的路径：接收端按 fileId 下发 token，上传时也用同一个值匹配。
func prepareFilesForWebSend(device models.SendModel, files []webSendFile) (*models.PrepareReceiveResponse, error) {
	infos := make(map[string]models.FileInfo, len(files))
	for _, f := range files {
		sha, err := sha256.CalculateSHA256(f.abs)
		if err != nil {
			return nil, fmt.Errorf("计算 %s 的 SHA256 失败: %w", f.relID, err)
		}
		infos[f.relID] = models.FileInfo{
			ID:       f.relID,
			FileName: f.name,
			Size:     f.size,
			FileType: mimeTypeForPath(f.abs),
			SHA256:   sha,
			Metadata: &models.FileMetadata{Modified: modTimeOf(f.abs)},
		}
	}
	return sendPrepareRequest(device, baseRequest(infos))
}

// modTimeOf 取文件的修改时间（RFC3339），失败时留空。
func modTimeOf(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return st.ModTime().UTC().Format(time.RFC3339)
}

// specForUploadPaths 构造「发送 uploads 下若干路径」的 sendSpec。
func specForUploadPaths(paths []string) (sendSpec, error) {
	files, err := collectWebSendFiles(paths)
	if err != nil {
		return sendSpec{}, err
	}
	entries := make([]uploadEntry, 0, len(files))
	for _, f := range files {
		abs := f.abs
		entries = append(entries, uploadEntry{
			file:     localFile{path: abs, name: f.name, relID: f.relID},
			fileSize: f.size,
			open: func() (io.ReadCloser, int64, error) {
				fh, err := os.Open(abs)
				if err != nil {
					return nil, 0, err
				}
				st, err := fh.Stat()
				if err != nil {
					fh.Close()
					return nil, 0, err
				}
				return fh, st.Size(), nil
			},
		})
	}
	return sendSpec{
		prepare: func(device models.SendModel) (*models.PrepareReceiveResponse, error) {
			return prepareFilesForWebSend(device, files)
		},
		entries: entries,
	}, nil
}

// specForText 构造「发送一段文本」的 sendSpec。
func specForText(text string) (sendSpec, error) {
	if strings.TrimSpace(text) == "" {
		return sendSpec{}, ErrEmptyText
	}
	content := []byte(text)
	fileName := textFileName(time.Now())
	fileInfo := models.FileInfo{
		ID:       fileName,
		FileName: fileName,
		Size:     int64(len(content)),
		FileType: defaultTextMimeType,
		SHA256:   sha256OfBytes(content),
		Preview:  TruncatePreview(text),
		Metadata: &models.FileMetadata{Modified: time.Now().UTC().Format(time.RFC3339)},
	}
	return sendSpec{
		prepare: func(device models.SendModel) (*models.PrepareReceiveResponse, error) {
			return sendPrepareRequest(device, baseRequest(map[string]models.FileInfo{
				fileInfo.ID: fileInfo,
			}))
		},
		entries: []uploadEntry{{
			file:     localFile{name: fileName, relID: fileName},
			fileSize: int64(len(content)),
			open: func() (io.ReadCloser, int64, error) {
				return io.NopCloser(bytes.NewReader(content)), int64(len(content)), nil
			},
		}},
	}, nil
}
