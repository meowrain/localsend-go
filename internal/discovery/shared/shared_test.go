package shared

import "testing"

// TestProtocolVersionIs22 锁死协议版本：本 Issue 只对齐 v2.2（v3 尚未定稿）。
// 有人要改成 3.x 时，这个测试会强制他先读 Issue 里的判断依据。
func TestProtocolVersionIs22(t *testing.T) {
	if ProtocolVersion != "2.2" {
		t.Fatalf("ProtocolVersion = %q, want 2.2", ProtocolVersion)
	}
	if Message.Version != ProtocolVersion {
		t.Fatalf("Message.Version = %q, want %q", Message.Version, ProtocolVersion)
	}
}

// TestDownloadIsDisabledUntilImplemented 广播里的 download 必须与实现一致：
// 下载 API（协议 5.2 / 5.3）未实现，就不能对外报 true。
func TestDownloadIsDisabledUntilImplemented(t *testing.T) {
	if Message.Download {
		t.Fatal("Message.Download = true, but the download API is not implemented")
	}
}

func TestSetListenPort(t *testing.T) {
	old := Message.Port
	t.Cleanup(func() { Message.Port = old })

	SetListenPort(53318)
	if Message.Port != 53318 {
		t.Fatalf("Port = %d, want 53318", Message.Port)
	}

	// 非法端口不应该覆盖已同步的值
	SetListenPort(0)
	if Message.Port != 53318 {
		t.Fatalf("Port = %d, want unchanged 53318", Message.Port)
	}
	SetListenPort(-1)
	if Message.Port != 53318 {
		t.Fatalf("Port = %d, want unchanged 53318", Message.Port)
	}
}
