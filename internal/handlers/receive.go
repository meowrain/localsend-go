package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/meowrain/localsend-go/internal/config"
	"github.com/meowrain/localsend-go/internal/models"

	"github.com/meowrain/localsend-go/internal/utils/clipboard"
	"github.com/meowrain/localsend-go/internal/utils/logger"
	sha256util "github.com/meowrain/localsend-go/internal/utils/sha256"
	"github.com/schollz/progressbar/v3"
)

// uploadBaseDir 是接收文件的落盘根目录，实际写入的绝对路径都会校验在它之下。
const uploadBaseDir = "uploads"

// maxPreviewClipboardSize 限制写入剪贴板/日志的预览内容大小。
const maxPreviewClipboardSize = 64 << 10

// maxPrepareBodyBytes 限制 prepare-upload 请求体大小（只有元数据，不含文件内容）。
const maxPrepareBodyBytes = 1 << 20

// uploadProgressBar 控制接收端的进度条显示。进度条会往 stderr 刷 ANSI 控制字符，
// 在后台/管道/测试里既没用又会污染输出，因此非终端场景直接关掉。
// 同时它也是测试的旁路开关：进度条有自己的后台刷新 goroutine，跑 -race 时没必要引入。
var uploadProgressBar = func() bool {
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// receiveFile 是会话里单个文件的上传进度。
// 会话是否有活跃会话（409）、是否已完成（释放给下一个发送端）都由它推导出来。
//
// 三个状态位互斥地描述文件所处的阶段：
//
//	全 false     Pending：还没人传，或上一次传输失败、可以重传
//	inProgress   Uploading：已有请求占住这次上传，其他人一律拒绝
//	done         Finished：终态，不再受理任何上传（token 重放防护靠它）
//
// 之所以不用一个枚举，是因为 done 还需要区分「成功」和「失败」：
// 失败终态在官方语义里等价于回到 Failed，发送端补传是合法的。
type receiveFile struct {
	name   string // 清理后的落盘文件名
	token  string
	sha256 string // prepare 阶段声明的校验和，空表示对端没提供（协议 nullable）
	size   int64
	// done 表示该文件已经「处理完毕」：上传成功、校验和不匹配、传输中断，或确认无需传输。
	// 失败的文件也算结束——否则一次 422 或一次断连会让会话永久占用，后续发送端全部 409。
	// done 一旦置位就不再翻转：受理上传前会检查它，避免 token 重放覆盖已落盘的文件。
	done bool
	// failed 表示 done 是「失败」造成的终态（中断 / 422），内容并没有落盘。
	// 这种文件虽然也不再 Pending，但重传是合法的：对端本来就是来补传的。
	failed bool
	// inProgress 表示这次上传已经被某个请求实例占住（beginUpload 置位，finishUpload 清除）。
	// 它必须是会话锁下的字段：只用 done/failed 判断的话，两个并发请求会同时看到
	// 「failed、可以重传」，然后各自 rename 同一个目标文件、各自落终态，
	// 后结束的那个（可能是失败的那个）会把已经落盘成功的文件标成 failed=true，
	// 而 failed=true 正是「允许重传」的信号，token 重放防护就这么被打开了。
	inProgress bool
}

// terminal 表示该文件已经进入终态：不再占用会话。
func (f *receiveFile) terminal() bool {
	return f.done
}

// receiveSession 是一次 prepare-upload 建立的传输会话。
type receiveSession struct {
	id       string
	files    map[string]*receiveFile // fileID -> 文件进度
	peerIP   string                  // prepare-upload 请求的来源 IP
	created  time.Time
	lastUsed time.Time // 最近一次活动（上传成功 / cancel）的时间，用于淘汰时优先保留活跃会话
	// ended 是会话的显式结束标记：所有文件都已离开 Pending，或收到 /cancel。
	// 会话结束后不再占用「唯一活跃会话」的位置（4.1 的 409），
	// 也不会再参与 204 判断（文件已存在不代表会话仍未使用）。
	//
	// 注意：单个文件失败不会置位。官方语义是「单个文件 Failed、会话在所有文件都不再
	// Pending 时才释放」，多文件传输里第一个文件 422 不该连坐后面的文件——把它们
	// 全部打回 403 只会让发送端把已经成功的会话重头再来一遍。
	ended bool
}

// receiveStore 保存 prepare 阶段下发的会话信息。
// PrepareReceive / ReceiveHandler / HandleCancel 都会读写，可能来自不同连接（并发），必须加锁。
type receiveStore struct {
	mu       sync.RWMutex
	sessions map[string]*receiveSession
	// pending 记录「正在处理」的上传实例，key 见 uploadKey。
	// 它只在单个请求的生命周期内存在，和会话表分开：会话可能中途被清理，
	// 但这次请求仍然需要知道自己该落成功还是失败终态。
	//
	// key 必须带上实例 ID，不能只用 sessionId+fileId：同一文件的并发上传会
	// 共用同一个 key，后写入的记录会覆盖先写入的终态（一个成功、一个失败时
	// 会把已落盘的文件记成失败），这是「4 个并发请求全部 200、文件最后被标成
	// failed」的根因。
	pending map[string]*uploadAttempt
}

// uploadAttempt 是一次 /upload 请求实例的私有记账，只有它可以写。
// 请求之间互不可见，因此一个请求收尾绝不会改掉另一个请求的结论。
type uploadAttempt struct {
	sessionID string
	fileID    string
	// completed 表示本次请求的内容已经 rename 落到 uploads/ 下。
	completed bool
}

var (
	sessionIDCounter atomic.Uint64
	uploadIDCounter  atomic.Uint64
	// 仅保留最近若干个会话，避免长期运行后无限增长
	maxSessions = 64
	store       = &receiveStore{sessions: make(map[string]*receiveSession), pending: make(map[string]*uploadAttempt)}
)

// newUploadID 给每次 /upload 请求分配一个实例 ID。
func newUploadID() string {
	return fmt.Sprintf("upload-%d", uploadIDCounter.Add(1))
}

func newSessionID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "session-" + hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("session-%d-%d", sessionIDCounter.Add(1), time.Now().UnixNano())
}

// add 把会话放进表里。
//
// 淘汰优先级：已结束（finished）的会话最先被替换，其次是最久没有活动的会话。
// 协议 v2 规定「同一时间只允许一个活跃会话」，所以正常情况下表里最多只有
// 一个未结束的会话；其余都是已经传完、只等下一次 prepare 来挤掉的历史记录。
// 这里的表格上限是兜底（例如对端突然消失、既不上传也不 cancel），
// 不能让长跑进程被一堆僵尸会话撑满内存。
func (s *receiveStore) add(sess *receiveSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) >= maxSessions {
		var finished, oldest *receiveSession
		for _, v := range s.sessions {
			if v.finished() {
				if finished == nil || v.idleSince().Before(finished.idleSince()) {
					finished = v
				}
				continue
			}
			if oldest == nil || v.idleSince().Before(oldest.idleSince()) {
				oldest = v
			}
		}
		victim := finished
		if victim == nil {
			victim = oldest
		}
		if victim != nil {
			delete(s.sessions, victim.id)
		}
	}
	s.sessions[sess.id] = sess
}

// idleSince 返回会话「最后一次活动」的时间：上传过就用 lastUsed，否则退回 created。
func (s *receiveSession) idleSince() time.Time {
	if s.lastUsed.After(s.created) {
		return s.lastUsed
	}
	return s.created
}

// finished 表示该会话不再占用传输通道：收到过 cancel，或所有文件都已离开 Pending。
// 没有任何文件、又还没被处理过的会话不算结束——它同样占着唯一会话的位置。
func (s *receiveSession) finished() bool {
	if s.ended {
		return true
	}
	if len(s.files) == 0 {
		return false
	}
	for _, f := range s.files {
		if !f.done {
			return false
		}
	}
	return true
}

// activeSessionLocked 返回当前未结束的会话（调用方需持有读锁）。
// 协议 4.1 的 409 只针对「另一个会话正在进行」的情况。
func (s *receiveStore) activeSessionLocked() *receiveSession {
	for _, sess := range s.sessions {
		if !sess.finished() {
			return sess
		}
	}
	return nil
}

// activeSession 返回当前未结束的会话，供 PrepareReceive 决定是否返回 409。
// 返回的是指针：调用方只读导出字段，修改一律走 finishFile / cancel。
func (s *receiveStore) activeSession() *receiveSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeSessionLocked()
}

// touch 记录一次成功的上传，避免该会话成为淘汰时最优先的候选。
func (s *receiveStore) touch(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.lastUsed = time.Now()
	}
}

// finishFileLocked 把文件落成功终态并清除 inProgress（调用方需持写锁）。
// failed 一并清掉：上一次传输失败留下的痕迹不该影响这次成功的结果。
func finishFileLocked(sess *receiveSession, fileID string) {
	if f, ok := sess.files[fileID]; ok {
		f.done = true
		f.failed = false
		f.inProgress = false
	}
	sess.lastUsed = time.Now()
	markEndedIfCompleteLocked(sess)
}

// failFileLocked 把文件落失败终态并清除 inProgress（调用方需持写锁）。
//
// 失败路径必须落终态：官方接收端的 UploadGuard 在 Drop 时以 Failed 调 finalize_file，
// 效果等价于「没有文件停在 Pending」。这里漏掉一步就会出现——发送端断连后
// 没有任何人再碰这个会话，它永远 finished() == false，其他设备的 prepare 一直 409，
// 只能重启进程才能恢复。
//
// 但失败只作用于这一个文件：会话是否释放由「还有没有 Pending 的文件」决定
// （官方 is_complete() 语义）。多文件传输里第一个文件 422，后面几个文件仍然可以传。
func failFileLocked(sess *receiveSession, fileID string) {
	if f, ok := sess.files[fileID]; ok {
		f.done = true
		f.failed = true
		f.inProgress = false
	}
	sess.lastUsed = time.Now()
	markEndedIfCompleteLocked(sess)
}

// markEndedIfCompleteLocked 在所有文件都离开 Pending 时结束会话（调用方需持写锁）。
// 传完的会话先留在表里：它已经让位给下一个发送端（不再 409），
// 但还要能被「token 重放」认出来并拒掉，否则旧 token 可以反复覆盖文件。
// 表有 maxSessions 上限兜底，不会无限增长。
func markEndedIfCompleteLocked(sess *receiveSession) {
	if sess.finished() {
		sess.ended = true
	}
}

var (
	// errNotPending 表示文件已经离开 Pending（内容已落盘），不能再接受上传。
	errNotPending = errors.New("file is no longer pending")
	// errInProgress 表示同一文件的另一次上传正在进行，本次请求不该动手落盘。
	errInProgress = errors.New("file upload already in progress")
	// errNoSession / errNoFile 表示会话或文件在 prepare 之后被淘汰了。
	errNoSession = errors.New("unknown session")
	errNoFile    = errors.New("unknown fileId")
)

// uploadKey 标识一次上传请求实例。
//
// 必须带实例 ID：只用 sessionId+fileId 会让同一文件的并发上传共用一个记账位，
// 一个请求的收尾记录会覆盖另一个的结论（见 uploadAttempt 的注释）。
func uploadKey(uploadID, sessionID, fileID string) string {
	return uploadID + "\x00" + sessionID + "\x00" + fileID
}

// beginUpload 在真正开始接收内容前占位：锁内判锁并置位 inProgress。
//
// validateUpload 读的是快照，检查完到落盘之间有一个窗口：对端的重试请求可能
// 同时到达，两边都看到「失败终态、可以重传」。因此「谁先拿到锁谁传」必须在
// 写锁内完成，并且要真的留下「传输中」的标记——只清 failed 而不置位的话，
// 第二个请求照样能通过检查，两个 rename 会互相覆盖同一个目标文件。
//
// 返回本次请求实例的记账 key，收尾时交给 finishUpload。
func (s *receiveStore) beginUpload(sessionID, fileID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		return "", fmt.Errorf("%w: %q", errNoSession, sessionID)
	}
	f, ok := sess.files[fileID]
	if !ok {
		return "", fmt.Errorf("%w: %q", errNoFile, fileID)
	}
	if f.done && !f.failed {
		return "", fmt.Errorf("%w: %q", errNotPending, fileID)
	}
	if f.inProgress {
		return "", fmt.Errorf("%w: %q", errInProgress, fileID)
	}
	f.inProgress = true
	f.failed = false
	sess.lastUsed = time.Now()

	key := uploadKey(newUploadID(), sessionID, fileID)
	s.pending[key] = &uploadAttempt{sessionID: sessionID, fileID: fileID}
	return key, nil
}

// settle 记录本次请求的落盘结果（completed 表示内容已经落到 uploads/ 下）。
// 只改自己实例的记账，因此并发请求之间不会互相覆盖终态。
func (s *receiveStore) settle(key string, completed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if att, ok := s.pending[key]; ok {
		att.completed = completed
	}
}

// finishUpload 在请求结束时收尾：内容落盘了才落成功终态，其他情况一律失败终态。
// 一次上传无论怎么退出（成功 / 422 / 500 / 断连）都必然落到这里，
// 因此文件不会停在 Pending（inProgress 也不会漏清）。
//
// 整个「读结果 → 落终态 → 清 inProgress」在同一把锁内完成：读取结论和标记终态
// 之间一旦放开锁，另一个请求就可能插进来，把终态改成它的结论。
//
// 会话如果在请求期间被淘汰/取消了，这里只负责释放占位，不再改会话状态。
func (s *receiveStore) finishUpload(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	att, ok := s.pending[key]
	if !ok {
		return
	}
	delete(s.pending, key)
	sess, ok := s.sessions[att.sessionID]
	if !ok {
		return
	}
	if att.completed {
		finishFileLocked(sess, att.fileID)
		return
	}
	failFileLocked(sess, att.fileID)
}

// cancel 释放会话占位：把所有未完成的文件标成结束，会话随即变成 finished，
// 不再阻挡后续 prepare（否则发送端一取消，本机就永久 409）。
// 返回该会话是否存在。
func (s *receiveStore) cancel(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return false
	}
	// 正在上传的文件也一并落终态：请求退出时 finishUpload 找不到会话就不会再动它，
	// 这里必须把 inProgress 一起清掉，避免留下「永远传输中」的占位。
	for _, f := range sess.files {
		f.done = true
		f.inProgress = false
	}
	sess.ended = true
	sess.lastUsed = time.Now()
	return true
}

// lookup 返回会话快照，避免调用方持有锁期间被并发修改。
// 文件进度用值拷贝，调用方无法通过它改回实际状态。
type receiveSnapshot struct {
	id     string
	peerIP string
	files  map[string]receiveFile
	ended  bool
}

func (s *receiveStore) lookup(id string) (receiveSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	if !ok {
		return receiveSnapshot{}, false
	}
	snap := receiveSnapshot{
		id:     sess.id,
		peerIP: sess.peerIP,
		ended:  sess.finished(),
		files:  make(map[string]receiveFile, len(sess.files)),
	}
	for k, v := range sess.files {
		snap.files[k] = *v
	}
	return snap, true
}

func (s *receiveStore) reset() {
	s.mu.Lock()
	s.sessions = make(map[string]*receiveSession)
	s.pending = make(map[string]*uploadAttempt)
	s.mu.Unlock()
}

// fileToken 生成 prepare 阶段下发的 token。
// 协议只要求它是不透明字符串；保留 "token-<fileID>" 前缀以兼容旧版 localsend-go 发送端。
func fileToken(fileID string) string {
	return "token-" + fileID
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func PrepareReceive(w http.ResponseWriter, r *http.Request) {
	peer := clientIP(r)

	// PIN 校验先于解析 body：未授权的请求不应该消耗解析开销，也不该留下任何日志噪音。
	// 计数按来源 IP 记录，成功一次即清零（见 pin.go）。
	if ok, status, msg := checkPIN(peer, r.URL.Query().Get("pin")); !ok {
		logger.Warnf("Rejected prepare-upload from %s: %s", peer, msg)
		http.Error(w, msg, status)
		return
	}

	var req models.PrepareReceiveRequest
	// 限制 body 大小：prepare 只带元数据，1 MiB 足够放下几百个文件的清单，
	// 防止对端用超大 JSON 把内存吃满。
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPrepareBodyBytes)).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	logger.Infof("Received request from %s,device is %s", req.Info.Alias, req.Info.DeviceModel)

	sess := &receiveSession{
		id:      newSessionID(),
		files:   make(map[string]*receiveFile, len(req.Files)),
		peerIP:  peer,
		created: time.Now(),
	}

	for fileID, fileInfo := range req.Files {
		// 文件名必须能安全落盘，非法文件名直接在 prepare 阶段拒绝，避免后续写到目录外
		name, err := sanitizeFileName(fileInfo.FileName)
		if err != nil {
			logger.Errorf("Reject prepare request: invalid file name %q (fileId %q)", fileInfo.FileName, fileID)
			http.Error(w, "Invalid file name", http.StatusBadRequest)
			return
		}
		sess.files[fileID] = &receiveFile{
			name:   name,
			token:  fileToken(fileID),
			sha256: strings.ToLower(strings.TrimSpace(fileInfo.SHA256)),
			size:   fileInfo.Size,
		}

		if strings.HasSuffix(name, ".txt") && len(fileInfo.Preview) > 0 {
			preview := fileInfo.Preview
			if len(preview) > maxPreviewClipboardSize {
				preview = preview[:maxPreviewClipboardSize]
			}
			logger.Success("TXT file content preview:", string(preview))
			clipboard.WriteToClipBoard(preview)
		}
	}

	// 协议 v2 规定同一时间只允许一个活跃会话（4.1 的 409）。
	// 先于 204 判断：已有会话在传时，不能因为「文件已存在」就抢先回 204。
	if active := store.activeSession(); active != nil {
		// 同一来源重复 prepare 视为重试（对端可能没收到上一次的响应）：
		// 直接结束旧会话让路，否则发送端一次重试就会把自己锁在 409 里。
		if active.peerIP == peer {
			logger.Debugf("Replacing stale session %s from same peer %s", active.id, peer)
			store.cancel(active.id)
		} else {
			logger.Warnf("Reject prepare from %s: blocked by another session", peer)
			http.Error(w, "Blocked by another session", statusBlockedBySession)
			return
		}
	}

	// 204：本次要传的文件都已在 uploads/ 且内容一致，对端无需再传。
	if shouldSkipTransfer(sess) {
		logger.Infof("All %d file(s) already present, replying 204", len(sess.files))
		w.WriteHeader(statusFinished)
		return
	}

	store.add(sess)

	resp := models.PrepareReceiveResponse{
		SessionID: sess.id,
		Files:     store.fileTokens(sess.id),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// fileTokens 返回会话的 fileID -> token 映射，供 prepare 响应使用。
func (s *receiveStore) fileTokens(id string) map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	if !ok {
		return map[string]string{}
	}
	out := make(map[string]string, len(sess.files))
	for fileID, f := range sess.files {
		out[fileID] = f.token
	}
	return out
}

// shouldSkipTransfer 判断是否可以回 204（协议 4.1：Finished, No file transfer needed）。
//
// 判定条件刻意保守，任何一条不满足都退回正常 200 流程：
//   - 配置里没有关掉 skip_existing（默认开启，误判时用户能自救）；
//   - 本次至少有一个文件（空列表没有「无需传输」可言）；
//   - 每个文件都声明了 sha256：没声明就无从比较，不能猜「大小一样就算一致」；
//   - 目标文件已在 uploads/ 下、且磁盘上的内容 sha256 与声明值相同。
//
// 只比 sha256 不比 size：sha256 相同即内容相同，size 只是冗余信息。
// 路径仍然走 safeJoinUnder，与真正落盘时用的是同一套防护，不会因为「只是读一下」
// 就绕过目录穿越校验。
func shouldSkipTransfer(sess *receiveSession) bool {
	if !config.ConfigData.SkipExistingEnabled() || len(sess.files) == 0 {
		return false
	}
	for _, f := range sess.files {
		if f.sha256 == "" || !fileMatchesSHA256(f) {
			return false
		}
	}
	return true
}

// existingFilePath 返回 uploads/ 下与 f 对应的路径，并确认它没跑出 uploads/。
//
// 直接复用 safeJoinUnder，而不是自己 Join：万一 sanitizeFileName 将来被改松，
// 这里仍然是最后一道目录穿越防护。调用方拿到的是一个已经过校验的绝对路径，
// 不会再拿用户输入的文件名去拼第二次。
func existingFilePath(f *receiveFile) (string, error) {
	path, err := safeJoinUnder(uploadBaseDir, f.name)
	if err != nil {
		logger.Warnf("Rejected unsafe file name %q: %v", f.name, err)
		return "", err
	}
	return path, nil
}

// fileMatchesSHA256 判断 uploads/ 下已有文件的 sha256 是否与声明值一致。
func fileMatchesSHA256(f *receiveFile) bool {
	path, err := existingFilePath(f)
	if err != nil {
		return false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		// 不存在、是目录、是符号链接：都不算「已存在且一致」。
		// 用 Lstat 而不是 Stat：符号链接的内容指向别处，不该被当成同名校验通过。
		return false
	}
	got, err := sha256util.CalculateSHA256(path)
	if err != nil {
		logger.Warnf("Cannot hash existing file %s: %v", path, err)
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(f.sha256)) == 1
}

// validateUpload 校验上传请求的 sessionId/fileId/token/来源 IP。
// 返回的 error 只用于日志，响应内容保持协议常量。
func validateUpload(r *http.Request) (receiveSnapshot, string, receiveFile, error) {
	sessionID := r.URL.Query().Get("sessionId")
	fileID := r.URL.Query().Get("fileId")
	token := r.URL.Query().Get("token")

	if sessionID == "" || fileID == "" || token == "" {
		return receiveSnapshot{}, "", receiveFile{}, fmt.Errorf("missing parameters")
	}

	sess, ok := store.lookup(sessionID)
	if !ok {
		return receiveSnapshot{}, "", receiveFile{}, fmt.Errorf("unknown session %q", sessionID)
	}

	file, ok := sess.files[fileID]
	if !ok {
		return receiveSnapshot{}, "", receiveFile{}, fmt.Errorf("unknown fileId %q in session %q", fileID, sessionID)
	}
	// 长度不一致时直接比较不等，避免同一长度假设
	if len(token) != len(file.token) || subtleCompare(token, file.token) != 1 {
		return receiveSnapshot{}, "", receiveFile{}, fmt.Errorf("token mismatch for fileId %q", fileID)
	}

	// 已经传完的文件不再受理上传：官方要求 file.status == Pending 才接受，
	// 否则拿着旧 token 重放就能把 uploads/ 下的文件覆盖掉。
	// 失败终态（中断 / 422）的文件例外——它没有落盘，重传只会被当成一次新上传。
	//
	// 这里读的是快照，只是快速失败；真正的判定在 beginUpload 的写锁内重做一遍。
	// 「是否有请求正在传」不在这里判：errInProgress 只由持锁的 beginUpload 产生。
	if file.terminal() && !file.failed {
		return receiveSnapshot{}, "", receiveFile{}, errNotPending
	}

	// 只拒绝明显的跨主机请求；同网段 NAT / 容器环境下的地址变化放行，避免破坏兼容性
	if sess.peerIP != "" {
		peer := net.ParseIP(sess.peerIP)
		cur := net.ParseIP(clientIP(r))
		if peer != nil && cur != nil && !sameHost(peer, cur) {
			return receiveSnapshot{}, "", receiveFile{}, fmt.Errorf("ip mismatch: prepared from %s, upload from %s", sess.peerIP, clientIP(r))
		}
	}

	if file.name == "" {
		return receiveSnapshot{}, "", receiveFile{}, fmt.Errorf("no file name for fileId %q", fileID)
	}
	return sess, fileID, file, nil
}

// subtleCompare 常量时间比较，避免 token 校验被计时侧信道利用。
// 直接用标准库实现，避免自研位运算在长度不等时越界或误判。
func subtleCompare(a, b string) int {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b))
}

func sameHost(a, b net.IP) bool {
	if a.Equal(b) {
		return true
	}
	return a.IsLoopback() && b.IsLoopback()
}

// rejectUpload 把校验/占位失败翻译成协议状态码，全部回 403 + 官方文案
// invalid_token_error（协议 4.2 的「Invalid token or IP address」）：
//   - token 不匹配、来源 IP 不符：本来就是这条；
//   - 文件已传完（token 重放）、同一文件已有请求在传：本质都是「这个 token 现在
//     不再有效」，回 409「被其他会话阻止」会让发送端误以为是别的设备的会话占了通道，
//     而实际上它等多久都不会通。
//   - 会话在 prepare 之后被淘汰 / 同源重试换了 session id：对端该重新 prepare。
func rejectUpload(w http.ResponseWriter) {
	http.Error(w, "Invalid token or IP address", statusRejected)
}

func ReceiveHandler(w http.ResponseWriter, r *http.Request) {
	snapshot, fileID, file, err := validateUpload(r)
	if err != nil {
		logger.Warnf("Rejected upload: %v", err)
		rejectUpload(w)
		return
	}

	// 抢占这次上传：在会话锁内判「是否已经有请求在传」，并置位 inProgress。
	// 校验与落盘之间有窗口，并发请求会同时走到这里；只有先拿到锁的那个能往下走，
	// 其余直接 403，不会出现两个 rename 互相覆盖同一个目标文件。
	// 这里也是官方「file.status == Pending 才受理上传」的落点：
	// 已经传完的文件（token 重放）会被判成 errNotPending。
	sessID := snapshot.id
	key, err := store.beginUpload(sessID, fileID)
	if err != nil {
		logger.Warnf("Rejected upload: %v", err)
		rejectUpload(w)
		return
	}

	// 任何未走到落盘的退出（400/422/500/断连）都落失败终态并清除 inProgress；
	// 成功路径在 rename 成功后把 completed 置位。
	// 记账按请求实例隔离（key 带实例 ID），并发请求不会互相覆盖结论。
	completed := false
	defer func() {
		store.settle(key, completed)
		store.finishUpload(key)
	}()

	if err := os.MkdirAll(uploadBaseDir, 0o755); err != nil {
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		logger.Errorf("Error creating directory: %v", err)
		return
	}

	// 再次校验落盘路径没跑出 uploads/（临时文件与最终文件都用这个结果拼出来，
	// 不会再拼一次用户输入的文件名）
	filePath, err := existingFilePath(&file)
	if err != nil {
		http.Error(w, "Invalid file name", http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		logger.Errorf("Error creating directory: %v", err)
		return
	}

	// 先写临时文件：校验和不匹配 / 传输中断时直接删掉，不会覆盖同名旧文件，
	// 也不会留下「半截」的可读文件。临时文件必须落在目标目录里——
	// 放到系统临时目录再 rename 会在跨文件系统（不同挂载点、容器 volume）时
	// 报 EXDEV。
	tmp, err := os.CreateTemp(filepath.Dir(filePath), ".localsend-*.part")
	if err != nil {
		http.Error(w, "Failed to create file", http.StatusInternalServerError)
		logger.Errorf("Error creating temp file: %v", err)
		return
	}
	tmpPath := tmp.Name()
	// 失败路径统一清理；成功路径在 rename 前把 committed 置位。
	committed := false
	defer func() {
		tmp.Close()
		if !committed {
			os.Remove(tmpPath)
		}
	}()

	// prepare 声明了 sha256 才校验（协议标注 nullable，未提供就跳过）
	var hasher hash.Hash
	if file.sha256 != "" {
		hasher = sha256.New()
	}

	var bar *progressbar.ProgressBar
	// 内容长度为未知（chunked）时 progressbar 也能工作，只是没有百分比
	if r.ContentLength > 0 && uploadProgressBar() {
		bar = newReceiveProgressBar(file.name, r.ContentLength)
	}

	written, copyErr := copyWithProgress(r.Body, tmp, hasher, bar)

	if copyErr != nil {
		// 请求被取消（发送端 /cancel 或直接断开）或读写出错：删临时文件，不落盘任何内容。
		// 文件由 defer 标成失败终态——否则这个会话永远「未结束」，别的设备 prepare 永久 409。
		logger.Errorf("Transfer aborted for %s: %v", file.name, copyErr)
		// 连接已断时写响应没有意义，但仍显式给出状态码，便于本地调试
		if r.Context().Err() != nil {
			return
		}
		http.Error(w, "Failed to receive file", http.StatusInternalServerError)
		return
	}

	if hasher != nil {
		got := hex.EncodeToString(hasher.Sum(nil))
		if subtle.ConstantTimeCompare([]byte(got), []byte(file.sha256)) != 1 {
			// 422：内容已收完但不匹配。临时文件会被 defer 删掉，
			// uploads/ 下不会出现半截或错误内容，同名旧文件也保持原样。
			// 同样落失败终态（defer 里做）：内容收完了但不可用，会话不该继续占着通道。
			logger.Errorf("Checksum mismatch for %s: got %s, want %s", file.name, got, file.sha256)
			http.Error(w, "Checksum mismatch (sha256)", statusChecksumMismatch)
			return
		}
	}

	// 内容已完整收完且校验通过，直接落盘；落盘成功才把 completed 置位，
	// 由 defer 里的 finishUpload 落成功终态。
	if err := tmp.Sync(); err != nil {
		logger.Warnf("Sync temp file %s failed: %v", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		http.Error(w, "Failed to write file", http.StatusInternalServerError)
		logger.Errorf("Error closing temp file: %v", err)
		return
	}
	// rename 在同一目录内是原子的：对端要么看到旧文件，要么看到完整的新文件。
	if err := os.Rename(tmpPath, filePath); err != nil {
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		logger.Errorf("Error renaming %s to %s: %v", tmpPath, filePath, err)
		return
	}
	committed = true

	logger.Success("File saved to:", filePath)
	logger.Debugf("Upload finished: session=%s fileId=%s bytes=%d", sessID, fileID, written)
	// 标记该文件处理完毕；整张会话都完成后它会自动让位给下一个发送端（不再 409）
	completed = true
	w.WriteHeader(http.StatusOK)
}

// newReceiveProgressBar 创建接收进度条（stdout 已被 TUI / 日志占用时仍写到 stderr）。
func newReceiveProgressBar(fileName string, size int64) *progressbar.ProgressBar {
	return progressbar.NewOptions64(
		size,
		progressbar.OptionSetDescription(fmt.Sprintf("下载 %s", fileName)),
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

// copyWithProgress 边读边写边算哈希，返回写入的字节数。
// 同步读、同步写：不再额外起 goroutine，请求取消通过 r.Body 的读取错误体现
// （http server 在连接断开时会关闭 body），因此不会出现写协程泄漏。
func copyWithProgress(body io.Reader, dst io.Writer, hasher hash.Hash, bar *progressbar.ProgressBar) (int64, error) {
	writers := []io.Writer{dst}
	if hasher != nil {
		writers = append(writers, hasher)
	}
	if bar != nil {
		writers = append(writers, bar)
	}
	n, err := io.Copy(io.MultiWriter(writers...), body)
	if err != nil {
		return n, err
	}
	if bar != nil {
		_ = bar.Finish()
	}
	return n, nil
}
