package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxErrorBodyBytes 限制读取错误响应体的长度，避免对端返回巨大内容时把内存吃满。
const maxErrorBodyBytes = 4 << 10

// 接收端产生这些状态码的地方（prepare-upload / upload）统一用这里的常量，
// 避免发送端与接收端各写一份字面量、改一处漏一处。
const (
	statusFinished           = http.StatusNoContent           // 204：无需传输
	statusPINRequired        = http.StatusUnauthorized        // 401：需要 PIN
	statusInvalidPIN         = http.StatusUnauthorized        // 401：PIN 错误
	statusRejected           = http.StatusForbidden           // 403：被接收端拒绝
	statusBlockedBySession   = http.StatusConflict            // 409：已有活跃会话
	statusChecksumMismatch   = http.StatusUnprocessableEntity // 422：sha256 不匹配
	statusTooManyRequests    = http.StatusTooManyRequests     // 429：PIN 尝试过多
	statusReceiverErrUnknown = http.StatusInternalServerError
)

// statusCodeError 表示对端返回了非预期的 HTTP 状态码。
// 带上 Message 是为了让上层能把对端的说明透出来；
// 对端用 http.Error 返回的是纯文本，解析不到 JSON 时 Message 为空，只能降级成状态码文案。
type statusCodeError struct {
	Status  int    // HTTP 状态码
	Message string // 对端响应体里的 message（可能为空）
}

func (e *statusCodeError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = "no message from receiver"
	}
	return fmt.Sprintf("received status code %d: %s", e.Status, msg)
}

// parseErrorMessage 尽量从错误响应体里取出可读文案。
//
// 对端可能是：
//   - 官方 App：JSON `{"message": "..."}`；
//   - 旧版 localsend-go：http.Error 写出的纯文本；
//   - 空响应体。
//
// 因此先按 JSON 的 message 字段取，取不到就用裁剪后的原始文本，再不行返回空串。
func parseErrorMessage(body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return ""
	}
	if msg := jsonMessageField(text); msg != "" {
		return msg
	}
	return truncateMessage(text)
}

// jsonMessageField 从 JSON 对象里取 message 字段，失败返回空串。
func jsonMessageField(text string) string {
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return ""
	}
	return truncateMessage(strings.TrimSpace(payload.Message))
}

// truncateMessage 把过长的错误文案裁到 maxErrorBodyBytes 以内，并去掉换行。
func truncateMessage(msg string) string {
	msg = strings.ReplaceAll(msg, "\r\n", " ")
	msg = strings.ReplaceAll(msg, "\n", " ")
	if len(msg) > maxErrorBodyBytes {
		msg = msg[:maxErrorBodyBytes]
	}
	return msg
}

// describePrepareError 把 prepare-upload 的失败响应翻译成用户能看懂的错误。
// 已知状态码给出协议定义的语义，未知状态码回退成带状态码与对端文案的错误。
func describePrepareError(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusNoContent:
		return errors.New("finished (No file transfer needed)")
	case http.StatusBadRequest:
		return newStatusError(resp, "invalid body")
	case http.StatusUnauthorized:
		return newStatusError(resp, "PIN required / Invalid PIN")
	case http.StatusForbidden:
		return newStatusError(resp, "rejected")
	case http.StatusConflict:
		return newStatusError(resp, "blocked by another session")
	case http.StatusTooManyRequests:
		return newStatusError(resp, "too many requests")
	case http.StatusInternalServerError:
		return newStatusError(resp, "unknown error by receiver")
	}
	return newStatusError(resp, "")
}

// describeUploadError 把 upload 的失败响应翻译成用户能看懂的错误。
// 422 是 2.2 新增的校验和不匹配，之前会被当成未知状态码。
func describeUploadError(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusBadRequest:
		return newStatusError(resp, "missing parameters")
	case http.StatusForbidden:
		return newStatusError(resp, "invalid token or IP address")
	case http.StatusConflict:
		return newStatusError(resp, "blocked by another session")
	case http.StatusUnprocessableEntity:
		return newStatusError(resp, "checksum mismatch (sha256)")
	case http.StatusInternalServerError:
		return newStatusError(resp, "unknown error by receiver")
	}
	return newStatusError(resp, "")
}

// newStatusError 构造 statusCodeError。
// 对端给了 message 就用对端的，否则用协议定义的语义文案（fallback 为空时，
// 错误信息里只剩状态码，调用方仍然能判断是哪个码）。
func newStatusError(resp *http.Response, fallback string) error {
	msg := ""
	if resp != nil && resp.Body != nil {
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		if err == nil {
			msg = parseErrorMessage(body)
		}
	}
	if msg == "" {
		msg = fallback
	}
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	return &statusCodeError{Status: status, Message: msg}
}

// ErrNothingToTransfer 表示接收端回了 204：它已持有本次要传的全部文件，
// 发送端不需要做任何事。这是正常结束，调用方应当当作成功处理。
var ErrNothingToTransfer = errors.New("nothing to transfer")

// prepareErrorHint 给 prepare-upload 的失败补一句「接下来该怎么办」。
// 协议只定义状态码语义，用户看到 401/409/429 很难知道该改什么参数。
func prepareErrorHint(err error) string {
	var sce *statusCodeError
	if !errors.As(err, &sce) {
		return ""
	}
	switch sce.Status {
	case http.StatusNoContent:
		return "对方判断这些文件已存在且内容一致，本次传输已完成"
	case http.StatusUnauthorized:
		// 本机的 --pin 同时用于接收端与发送端，所以这里提示全局参数写法
		return "对方开启了 PIN 校验：请用全局参数 --pin <对方显示的 PIN> 重新发送"
	case http.StatusForbidden:
		return "对方拒绝了本次传输（可能正在等待用户确认，或已手动拒绝）"
	case http.StatusConflict:
		return "对方正忙于另一个传输会话：等当前会话结束后重试，或让对方取消"
	case http.StatusTooManyRequests:
		return "PIN 尝试次数过多，已被暂时限制：等几分钟后重试，并确认 PIN 输入正确"
	}
	return ""
}
