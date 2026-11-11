package shared

import "testing"

// TestFingerprintIsNotPlaceholder 防止指纹退回常量占位值。
// 写死 "random-string" 时，两台 localsend-go 的指纹相同 → 互判为「自己」→
// 跳过 register 回发，双向发现在真实双机场景下彻底失效。
func TestFingerprintIsNotPlaceholder(t *testing.T) {
	fp := Message.Fingerprint
	if fp == "" {
		t.Fatal("fingerprint is empty, crypto/rand failed?")
	}
	if fp == "random-string" {
		t.Fatal(`fingerprint is the old placeholder "random-string"`)
	}
	if len(fp) != 32 {
		t.Fatalf("fingerprint = %q (len %d), want 32 hex chars", fp, len(fp))
	}
	for _, r := range fp {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			t.Fatalf("fingerprint %q contains non-hex char %q", fp, r)
		}
	}
}

// TestNewFingerprintIsRandom 多次生成必须互不相同（crypto/rand 生效的直接证据）。
func TestNewFingerprintIsRandom(t *testing.T) {
	seen := make(map[string]struct{}, 64)
	for i := 0; i < 64; i++ {
		fp := NewFingerprint()
		if fp == "" {
			t.Fatal("NewFingerprint returned empty string")
		}
		if _, dup := seen[fp]; dup {
			t.Fatalf("NewFingerprint produced duplicate %q on iteration %d", fp, i)
		}
		seen[fp] = struct{}{}
	}
}

// TestFingerprintDiffersPerProcess 语义上的「两台机器」：各自独立生成一次必须不同。
func TestFingerprintDiffersPerProcess(t *testing.T) {
	a, b := NewFingerprint(), NewFingerprint()
	if a == b {
		t.Fatalf("two independent fingerprints are identical: %q", a)
	}
}
