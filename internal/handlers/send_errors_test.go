package handlers

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func makeResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestDescribePrepareError(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantSub []string
	}{
		{"204 finished", http.StatusNoContent, "", []string{"finished"}},
		{"400 invalid body", http.StatusBadRequest, "", []string{"400", "invalid body"}},
		{"401 pin required", http.StatusUnauthorized, "", []string{"401", "PIN required"}},
		{"403 rejected", http.StatusForbidden, "", []string{"403", "rejected"}},
		{"409 blocked", http.StatusConflict, "", []string{"409", "blocked by another session"}},
		{"429 too many", http.StatusTooManyRequests, "", []string{"429", "too many requests"}},
		{"500 receiver", http.StatusInternalServerError, "", []string{"500", "unknown error by receiver"}},
		{"unknown code", http.StatusTeapot, "", []string{"418"}},
		{"json message wins", http.StatusForbidden, `{"message":"nope, busy"}`, []string{"403", "nope, busy"}},
		{"plain text message", http.StatusInternalServerError, "boom\n", []string{"500", "boom"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := describePrepareError(makeResponse(tc.status, tc.body))
			if err == nil {
				t.Fatal("expect error, got nil")
			}
			msg := err.Error()
			for _, want := range tc.wantSub {
				if !strings.Contains(msg, want) {
					t.Fatalf("error %q does not contain %q", msg, want)
				}
			}
		})
	}
}

func TestDescribeUploadError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   []string
	}{
		{"400 missing", http.StatusBadRequest, "", []string{"400", "missing parameters"}},
		{"403 token", http.StatusForbidden, "", []string{"403", "invalid token or IP address"}},
		{"409 blocked", http.StatusConflict, "", []string{"409", "blocked by another session"}},
		{"422 checksum", http.StatusUnprocessableEntity, "", []string{"422", "checksum mismatch"}},
		{"500 receiver", http.StatusInternalServerError, "", []string{"500", "unknown error by receiver"}},
		{"429 unknown here", http.StatusTooManyRequests, "", []string{"429"}},
		{"json message", http.StatusUnprocessableEntity, `{"message":"sha256 mismatch for a.txt"}`, []string{"422", "sha256 mismatch for a.txt"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := describeUploadError(makeResponse(tc.status, tc.body))
			if err == nil {
				t.Fatal("expect error, got nil")
			}
			msg := err.Error()
			for _, want := range tc.want {
				if !strings.Contains(msg, want) {
					t.Fatalf("error %q does not contain %q", msg, want)
				}
			}
		})
	}
}

// TestStatusErrorKeepsCode 保证调用方能从 error 里取出状态码（便于上层分支处理）。
func TestStatusErrorKeepsCode(t *testing.T) {
	err := describeUploadError(makeResponse(http.StatusUnprocessableEntity, ""))
	var sce *statusCodeError
	if !errors.As(err, &sce) {
		t.Fatalf("error type = %T, want *statusCodeError", err)
	}
	if sce.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", sce.Status)
	}
}

// TestParseErrorMessageFallback 覆盖「解析失败降级为原始文本 / 空串」。
func TestParseErrorMessageFallback(t *testing.T) {
	cases := map[string]string{
		"":                        "",
		"   ":                     "",
		`{"message":"hello"}`:     "hello",
		`{"message":""}`:          `{"message":""}`, // 空 message 视作没解析出来，降级为原始文本
		`{"error":"not message"}`: `{"error":"not message"}`,
		"  plain text  ":          "plain text",
		"line1\nline2":            "line1 line2",
	}
	for in, want := range cases {
		if got := parseErrorMessage([]byte(in)); got != want {
			t.Errorf("parseErrorMessage(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestParseErrorMessageTruncates 避免对端返回巨大响应体时把错误信息撑爆。
func TestParseErrorMessageTruncates(t *testing.T) {
	got := parseErrorMessage([]byte(strings.Repeat("x", maxErrorBodyBytes*2)))
	if len(got) > maxErrorBodyBytes {
		t.Fatalf("message len = %d, want <= %d", len(got), maxErrorBodyBytes)
	}
}
