package handlers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/meowrain/localsend-go/internal/models"
)

// isolateSend 关掉上传进度条，并把发送端要用的对端 PIN 清空。
func isolateSend(t *testing.T) {
	t.Helper()
	oldBar := uploadProgressBar
	uploadProgressBar = func() bool { return false }
	t.Cleanup(func() { uploadProgressBar = oldBar })
}

// uploadTarget 起一个假接收端，按脚本返回状态码，并记录收到的请求数。
func uploadTarget(t *testing.T, statuses ...int) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(atomic.AddInt32(&hits, 1))
		_, _ = io.Copy(io.Discard, r.Body)
		code := statuses[len(statuses)-1]
		if n-1 < len(statuses) {
			code = statuses[n-1]
		}
		w.WriteHeader(code)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// deviceFor 把 httptest 的地址拆成发送端需要的 SendModel。
func deviceFor(t *testing.T, srv *httptest.Server) models.SendModel {
	t.Helper()
	var host string
	var port int
	hostPort := srv.URL[len("http://"):]
	idx := strings.LastIndex(hostPort, ":")
	if idx < 0 {
		t.Fatalf("unexpected server url %q", srv.URL)
	}
	host = hostPort[:idx]
	p, err := strconv.Atoi(hostPort[idx+1:])
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	port = p
	return models.SendModel{IP: host, Port: port, Protocol: "http"}
}

// TestUploadRetryStopsAtLimit 422 会重试，但最多 uploadRetryMax 次（官方 3 次）。
func TestUploadRetryStopsAtLimit(t *testing.T) {
	isolateSend(t)
	srv, hits := uploadTarget(t, http.StatusUnprocessableEntity)

	err := uploadWithRetry(context.Background(), deviceFor(t, srv), "session-1", "a.txt", "token-a.txt",
		func() (io.ReadCloser, int64, error) {
			return io.NopCloser(bytes.NewReader([]byte("payload"))), 7, nil
		}, nil)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := atomic.LoadInt32(hits); got != uploadRetryMax {
		t.Fatalf("sender attempted %d times, want %d", got, uploadRetryMax)
	}
}

// TestUploadRetrySucceedsOnSecondTry 第一次 422、第二次 200，最终算成功。
func TestUploadRetrySucceedsOnSecondTry(t *testing.T) {
	isolateSend(t)
	srv, hits := uploadTarget(t, http.StatusUnprocessableEntity, http.StatusOK)

	err := uploadWithRetry(context.Background(), deviceFor(t, srv), "session-1", "a.txt", "token-a.txt",
		func() (io.ReadCloser, int64, error) {
			return io.NopCloser(bytes.NewReader([]byte("payload"))), 7, nil
		}, nil)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 2 {
		t.Fatalf("sender attempted %d times, want 2", got)
	}
}

// TestUploadRetrySkipsNon422 非 422 的失败不重试：403/409 重传结果一样。
func TestUploadRetrySkipsNon422(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusConflict, http.StatusInternalServerError} {
		isolateSend(t)
		srv, hits := uploadTarget(t, status)
		err := uploadWithRetry(context.Background(), deviceFor(t, srv), "session-1", "a.txt", "token-a.txt",
			func() (io.ReadCloser, int64, error) {
				return io.NopCloser(bytes.NewReader([]byte("payload"))), 7, nil
			}, nil)
		if err == nil {
			t.Fatalf("status %d: expected error", status)
		}
		if got := atomic.LoadInt32(hits); got != 1 {
			t.Fatalf("status %d: sender attempted %d times, want 1", status, got)
		}
	}
}

// TestUploadRetryReopensSource 重试必须重新打开内容源：上一次的 ReadCloser 已经读到底。
func TestUploadRetryReopensSource(t *testing.T) {
	isolateSend(t)
	srv, _ := uploadTarget(t, http.StatusUnprocessableEntity, http.StatusOK)

	var opened int32
	err := uploadWithRetry(context.Background(), deviceFor(t, srv), "session-1", "a.txt", "token-a.txt",
		func() (io.ReadCloser, int64, error) {
			atomic.AddInt32(&opened, 1)
			return io.NopCloser(bytes.NewReader([]byte("payload"))), 7, nil
		}, nil)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if got := atomic.LoadInt32(&opened); got != 2 {
		t.Fatalf("source opened %d times, want 2", got)
	}
}
