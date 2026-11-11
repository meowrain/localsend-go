package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// withTempCwd 把工作目录切到临时目录，避免测试间互相影响，也不会读到仓库里的 config.yaml
func withTempCwd(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	})
	return dir
}

// isolateConfigEnv 隔离所有会影响配置查找的环境变量
func isolateConfigEnv(t *testing.T, configDir string) {
	t.Helper()
	t.Setenv(EnvConfigDir, configDir)
	t.Setenv(EnvConfigPath, "")
	t.Setenv("HOME", configDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	// SetPath 设置的显式路径是包级状态，测试之间必须清掉，否则会泄漏到后续用例
	mu.Lock()
	explicitPath = ""
	mu.Unlock()
	reset()
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestResolveDeviceName(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string // 期望的原样返回；为空表示期望随机生成的名称
	}{
		{name: "camel case preserved", input: "MeowRain Device", want: "MeowRain Device"},
		{name: "snake value preserved", input: "my_macbook_pro", want: "my_macbook_pro"},
		{name: "chinese preserved", input: "喵雨的笔记本", want: "喵雨的笔记本"},
		{name: "trim spaces", input: "  Kept Name  ", want: "Kept Name"},
		{name: "empty falls back to random", input: "", want: ""},
		{name: "blank falls back to random", input: "   \t\n", want: ""},
	}

	randomPattern := regexp.MustCompile(`^` + strings.Join(adjectives, "|") + ` (` + strings.Join(nouns, "|") + `)$`)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveDeviceName(tc.input)
			if tc.want != "" {
				if got != tc.want {
					t.Fatalf("resolveDeviceName(%q) = %q, want %q", tc.input, got, tc.want)
				}
				return
			}
			if got == "" {
				t.Fatalf("resolveDeviceName(%q) returned empty name", tc.input)
			}
			if !randomPattern.MatchString(got) {
				t.Fatalf("resolveDeviceName(%q) = %q, not a generated name", tc.input, got)
			}
		})
	}
}

func TestLoadMissingConfigGeneratesDefault(t *testing.T) {
	withTempCwd(t)
	configDir := t.TempDir()
	isolateConfigEnv(t, configDir)

	Load()
	if err := LoadError(); err != nil {
		t.Fatalf("load should not fail, got %v", err)
	}

	// 默认值：设备名随机生成，两个功能开关默认开启
	if ConfigData.NameOfDevice == "" {
		t.Fatal("device name should be generated when config file is missing")
	}
	if !ConfigData.HttpFileServerEnabled() {
		t.Fatal("http_file_server should default to true")
	}
	if !ConfigData.LocalSendServerEnabled() {
		t.Fatal("local_send_server should default to true")
	}

	// 首次运行应该把默认配置写到用户配置目录
	want := filepath.Join(configDir, configFileName)
	if ConfigPath != want {
		t.Fatalf("ConfigPath = %q, want %q", ConfigPath, want)
	}
	generated, err := Read(want)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	if generated.Functions.HttpFileServer == nil || generated.Functions.LocalSendServer == nil {
		t.Fatalf("generated config should contain functions switches, got %+v", generated.Functions)
	}
}

func TestLoadPriority(t *testing.T) {
	wd := withTempCwd(t)
	configDir := t.TempDir()
	isolateConfigEnv(t, configDir)

	// 同时准备当前目录、用户配置目录和旧路径三份配置
	writeFile(t, filepath.Join(wd, configFileName), "device_name: \"from cwd\"\n")
	writeFile(t, filepath.Join(configDir, configFileName), "device_name: \"from user dir\"\n")
	writeFile(t, legacyPath, "device_name: \"from legacy\"\n")

	Load()
	if err := LoadError(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if ConfigData.NameOfDevice != "from cwd" {
		t.Fatalf("cwd config should win, got device name %q", ConfigData.NameOfDevice)
	}

	// 显式路径优先级最高
	explicit := filepath.Join(t.TempDir(), "explicit.yaml")
	writeFile(t, explicit, "device_name: \"from explicit\"\n")
	SetPath(explicit)
	Load()
	if err := LoadError(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if ConfigData.NameOfDevice != "from explicit" {
		t.Fatalf("explicit config should win, got device name %q", ConfigData.NameOfDevice)
	}
}

func TestLoadUserConfigDirWhenCwdHasNoConfig(t *testing.T) {
	withTempCwd(t)
	configDir := t.TempDir()
	isolateConfigEnv(t, configDir)
	writeFile(t, filepath.Join(configDir, configFileName), "device_name: \"user dir only\"\n")

	Load()
	if err := LoadError(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if ConfigData.NameOfDevice != "user dir only" {
		t.Fatalf("device name = %q, want user dir config", ConfigData.NameOfDevice)
	}
}

func TestLoadLegacyPath(t *testing.T) {
	// 旧路径是相对工作目录的 internal/config/config.yaml
	wd := withTempCwd(t)
	configDir := t.TempDir()
	isolateConfigEnv(t, configDir)
	writeFile(t, filepath.Join(wd, legacyPath), "device_name: \"legacy device\"\n")

	Load()
	if err := LoadError(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if ConfigData.NameOfDevice != "legacy device" {
		t.Fatalf("device name = %q, want legacy config", ConfigData.NameOfDevice)
	}
	// 旧配置里没有 functions 段，应该补齐默认开关而不是全部关闭
	if !ConfigData.HttpFileServerEnabled() || !ConfigData.LocalSendServerEnabled() {
		t.Fatalf("legacy config should inherit default switches, got %+v", ConfigData.Functions)
	}
}

func TestLoadInvalidYamlReportsPath(t *testing.T) {
	withTempCwd(t)
	configDir := t.TempDir()
	isolateConfigEnv(t, configDir)
	broken := filepath.Join(configDir, configFileName)
	writeFile(t, broken, "device_name: [unterminated\n")

	Load()
	err := LoadError()
	if err == nil {
		t.Fatal("expected error for invalid yaml")
	}
	message := FormatLoadError(broken, err).Error()
	if !strings.Contains(message, broken) {
		t.Fatalf("formatted error should mention config path, got %q", message)
	}
	// 解析失败时仍然使用默认值，程序可以继续运行
	if !ConfigData.HttpFileServerEnabled() {
		t.Fatal("fallback config should keep default switches")
	}
}

func TestApplyDefaults(t *testing.T) {
	def := builtinDefault()

	var empty Config
	applyDefaults(&empty, def)
	if !empty.HttpFileServerEnabled() || !empty.LocalSendServerEnabled() {
		t.Fatalf("missing switches should fall back to default, got %+v", empty.Functions)
	}

	// 显式关闭的开关不能被默认值覆盖
	var disabled Config
	off := false
	disabled.Functions.HttpFileServer = &off
	applyDefaults(&disabled, def)
	if disabled.HttpFileServerEnabled() {
		t.Fatal("explicitly disabled switch should stay disabled")
	}
	if !disabled.LocalSendServerEnabled() {
		t.Fatal("unspecified switch should fall back to default")
	}
}
