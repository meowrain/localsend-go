package version

import "testing"

func TestString(t *testing.T) {
	origVersion, origCommit := Version, Commit
	t.Cleanup(func() {
		Version, Commit = origVersion, origCommit
	})

	tests := []struct {
		name    string
		version string
		commit  string
		want    string
	}{
		{name: "默认未注入显示 dev", version: "dev", commit: "", want: "dev"},
		{name: "空版本兜底为 dev", version: "", commit: "", want: "dev"},
		{name: "带提交哈希", version: "v1.2.8", commit: "a1b2c3d", want: "v1.2.8 (a1b2c3d)"},
		{name: "无提交哈希", version: "v1.2.8", commit: "", want: "v1.2.8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Version, Commit = tt.version, tt.commit
			if got := String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
