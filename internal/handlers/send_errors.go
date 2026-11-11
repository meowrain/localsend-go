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

// 错误码映射直接写在下面的 switch 里（protocol/README.md 4.1 / 4.2 错误表）：
// 之前还定义了一组 statusFinished / statusPINRequired 之类的常量，但没人引用，
// 只是把同一份映射写了两遍，删掉避免两处漂移。

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
