package config

import (
	"embed"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/meowrain/localsend-go/internal/utils/logger"
	"gopkg.in/yaml.v2"
)

//go:embed config.yaml
var embeddedConfig embed.FS

// 配置查找优先级（从高到低）：
//  1. 显式指定的路径（通过 SetPath 传入，或者环境变量 LOCALSEND_CONFIG）
//  2. 当前工作目录下的 config.yaml
//  3. 用户配置目录下的 localsend-go/config.yaml
//     - Linux: $XDG_CONFIG_HOME/localsend-go/config.yaml 或 ~/.config/localsend-go/config.yaml
//     - macOS: ~/Library/Application Support/localsend-go/config.yaml
//     - Windows: %AppData%\localsend-go\config.yaml
//     （设置了 LOCALSEND_CONFIG_DIR 时优先使用该目录）
//  4. 向后兼容：旧路径 internal/config/config.yaml（仓库内运行、或从 release 包解压运行）
//  5. 编译时内嵌的 internal/config/config.yaml
//
// 以上都不存在时，会使用内嵌默认值，并把默认配置写入用户配置目录（优先级 3 的路径）。

// EnvConfigPath 指定配置文件路径的环境变量
const EnvConfigPath = "LOCALSEND_CONFIG"

// EnvConfigDir 指定配置目录的环境变量
const EnvConfigDir = "LOCALSEND_CONFIG_DIR"

// 配置文件与目录名
const (
	configDirName  = "localsend-go"
	configFileName = "config.yaml"
)

// legacyPath 是旧版本读取的配置路径，保留是为了向后兼容
var legacyPath = filepath.Join("internal", "config", configFileName)

// Functions 是功能开关；用指针区分「配置里没写」和「显式写了 false」
type Functions struct {
	HttpFileServer  *bool `yaml:"http_file_server"`
	LocalSendServer *bool `yaml:"local_send_server"`
	// PIN 是接收端的访问密码（协议 4.1 的 ?pin= 查询参数）。
	// 空串表示不启用，发送端无需携带 pin 即可 prepare-upload。
	PIN string `yaml:"pin"`
	// SkipExisting 控制 204 优化：本次 prepare 的全部文件都已在 uploads/
	// 且 sha256 一致时，回 204 让发送端跳过传输。默认开启，设为 false 可关闭。
	SkipExisting *bool `yaml:"skip_existing"`
}

// Config 是配置文件的映射结构
type Config struct {
	DeviceName   string    `yaml:"device_name"`
	NameOfDevice string    `yaml:"-"` // Actual device name used in runtime
	Functions    Functions `yaml:"functions"`
}

// DefaultConfig 返回内嵌的默认配置，内嵌文件不可用时退回硬编码值
func DefaultConfig() Config {
	var cfg Config
	data, err := embeddedConfig.ReadFile(configFileName)
	if err != nil {
		logger.Warnf("Can not read embedded config file, falling back to builtin defaults: %v", err)
		return builtinDefault()
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		logger.Warnf("Can not parse embedded config file, falling back to builtin defaults: %v", err)
		return builtinDefault()
	}
	applyDefaults(&cfg, builtinDefault())
	return cfg
}

// builtinDefault 是最后一层兜底默认值
func builtinDefault() Config {
	enabled := true
	return Config{
		Functions: Functions{
			HttpFileServer:  &enabled,
			LocalSendServer: &enabled,
		},
	}
}

// HttpFileServerEnabled 返回是否启用 HTTP 文件服务
func (c Config) HttpFileServerEnabled() bool {
	return c.Functions.HttpFileServer == nil || *c.Functions.HttpFileServer
}

// LocalSendServerEnabled 返回是否启用 LocalSend 接收服务
func (c Config) LocalSendServerEnabled() bool {
	return c.Functions.LocalSendServer == nil || *c.Functions.LocalSendServer
}

// SkipExistingEnabled 返回是否启用「已存在且校验和一致就回 204」的优化。
func (c Config) SkipExistingEnabled() bool {
	return c.Functions.SkipExisting == nil || *c.Functions.SkipExisting
}

// SetPIN 设置接收端 PIN，供 --pin 命令行参数覆盖配置里的值。
func (c *Config) SetPIN(pin string) {
	c.Functions.PIN = strings.TrimSpace(pin)
}

// random device name
var (
	adjectives = []string{
		"Happy", "Swift", "Silent", "Clever", "Brave",
		"Gentle", "Wise", "Calm", "Lucky", "Proud",
	}
	nouns = []string{
		"Phoenix", "Wolf", "Eagle", "Lion", "Owl",
		"Shark", "Tiger", "Bear", "Hawk", "Fox",
	}
)

var (
	// ConfigData 是进程内全局配置
	ConfigData Config
	// ConfigPath 是实际加载的配置文件路径，使用内嵌默认值时为 "<embedded>"
	ConfigPath = "<embedded>"

	explicitPath string
	loadOnce     sync.Once
	loadErr      error
	mu           sync.Mutex
)

// SetPath 显式指定配置文件路径，必须在第一次访问 ConfigData 之前调用（例如 main 的参数解析阶段）。
// 多次调用只有最后一次生效，并会丢弃之前加载的配置。
func SetPath(path string) {
	if path == "" || explicitPath == path {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	explicitPath = path
	reset()
}

// reset 重置加载状态，调用方需持有 mu
func reset() {
	loadOnce = sync.Once{}
	loadErr = nil
	ConfigData = Config{}
	ConfigPath = "<embedded>"
}

// FormatLoadError 格式化配置加载错误，保证日志里带上出错的文件路径
func FormatLoadError(path string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("配置文件 %s 解析失败：%v（请检查 YAML 缩进和字段名，或删除该文件让程序重新生成）", path, err)
}

// LoadError 返回加载配置时遇到的错误（例如文件格式错误）
func LoadError() error {
	Load()
	return loadErr
}

// Load 加载配置，重复调用只会生效一次
func Load() {
	loadOnce.Do(func() {
		cfg, path, err := load()
		ConfigData = cfg
		ConfigPath = path
		loadErr = err
		if err != nil {
			logger.Failedf("%v", FormatLoadError(path, err))
			return
		}
		logger.Debug("Loaded config from " + path)
		ConfigData.NameOfDevice = resolveDeviceName(ConfigData.DeviceName)
	})
}

// CandidatePaths 返回按优先级排序的候选配置路径（不含内嵌默认值）
func CandidatePaths() []string {
	paths := make([]string, 0, 4)
	if explicitPath != "" {
		paths = append(paths, explicitPath)
	} else if env := os.Getenv(EnvConfigPath); env != "" {
		paths = append(paths, env)
	}
	if wd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(wd, configFileName))
	}
	if dir, err := UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(dir, configFileName))
	}
	paths = append(paths, legacyPath)
	return paths
}

// UserConfigDir 返回用户配置目录下的 localsend-go 目录
func UserConfigDir() (string, error) {
	if dir := os.Getenv(EnvConfigDir); dir != "" {
		return dir, nil
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, configDirName), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		// 部分精简环境没有 HOME，退化到当前目录，保证程序仍可运行
		if wd, wdErr := os.Getwd(); wdErr == nil {
			return filepath.Join(wd, "."+configDirName), nil
		}
		return "", fmt.Errorf("determine user config dir: %w", err)
	}
	return filepath.Join(base, configDirName), nil
}

// Read 从指定路径读取配置；文件不存在时返回 os.ErrNotExist 包装的错误
func Read(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse yaml: %w", err)
	}
	return cfg, nil
}

// load 按优先级查找并加载配置；没有找到任何配置文件时使用默认值并写出默认配置
func load() (Config, string, error) {
	// 显式指定的路径（--config / LOCALSEND_CONFIG）必须严格对待：
	// 文件不存在时把默认配置写到该路径，而不是静默落到用户配置目录，
	// 否则用户按自己给的路径改配置永远不生效。
	explicit := explicitPath
	if explicit == "" {
		explicit = os.Getenv(EnvConfigPath)
	}
	if explicit != "" {
		cfg, err := Read(explicit)
		switch {
		case err == nil:
			applyDefaults(&cfg, DefaultConfig())
			return cfg, explicit, nil
		case errors.Is(err, os.ErrNotExist):
			cfg = DefaultConfig()
			if werr := WriteDefault(explicit, cfg); werr != nil {
				logger.Warnf("Config %s does not exist and cannot be created: %v", explicit, werr)
				return cfg, "<embedded>", nil
			}
			logger.Infof("Config %s did not exist, generated default config there", explicit)
			return cfg, explicit, nil
		default:
			return DefaultConfig(), explicit, err
		}
	}

	for _, path := range CandidatePaths() {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		cfg, err := Read(path)
		if err != nil {
			wd, _ := os.Getwd()
			abs := path
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(wd, path)
			}
			// 解析失败也要走同一套设备名解析，否则会以空 alias 广播
			cfg = DefaultConfig()
			cfg.NameOfDevice = resolveDeviceName(cfg.DeviceName)
			return cfg, abs, err
		}
		// 旧版内嵌配置没有 functions 段，补齐默认开关，保证旧路径仍然可用
		applyDefaults(&cfg, DefaultConfig())
		return cfg, path, nil
	}

	cfg := DefaultConfig()
	dir, err := UserConfigDir()
	if err != nil {
		logger.Warnf("No config file found and user config dir unavailable: %v", err)
		return cfg, "<embedded>", nil
	}
	out := filepath.Join(dir, configFileName)
	if err := WriteDefault(out, cfg); err != nil {
		logger.Warnf("No config file found, failed to create default config at %s: %v", out, err)
		return cfg, "<embedded>", nil
	}
	logger.Infof("No config file found, generated default config at %s", out)
	return cfg, out, nil
}

// WriteDefault 把配置写到指定路径
func WriteDefault(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	header := "# localsend-go configuration\n" +
		"# device_name: leave empty to use a randomly generated name\n"
	content := append([]byte(header), data...)
	// O_EXCL：目标已存在时绝不覆盖用户手写的配置；权限 0600，配置里可能含主机名/路径。
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		return err
	}
	return nil
}

// applyDefaults 用内嵌默认值补齐配置文件缺失的字段。
// 这里按指针区分「没写」和「显式写了 false」，同时兼容旧版没有 device_name 的配置文件。
func applyDefaults(cfg *Config, def Config) {
	if cfg.DeviceName == "" {
		cfg.DeviceName = def.DeviceName
	}
	if cfg.Functions.HttpFileServer == nil && def.Functions.HttpFileServer != nil {
		v := *def.Functions.HttpFileServer
		cfg.Functions.HttpFileServer = &v
	}
	if cfg.Functions.LocalSendServer == nil && def.Functions.LocalSendServer != nil {
		v := *def.Functions.LocalSendServer
		cfg.Functions.LocalSendServer = &v
	}
	if cfg.Functions.SkipExisting == nil && def.Functions.SkipExisting != nil {
		v := *def.Functions.SkipExisting
		cfg.Functions.SkipExisting = &v
	}
}

// resolveDeviceName 设备名为空或只有空白字符时生成随机名称
func resolveDeviceName(name string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		logger.Debug("Using configured device name: " + trimmed)
		return trimmed
	}
	random := generateRandomName()
	logger.Debug("Using randomly generated device name: " + random)
	return random
}

// random device name generator
func generateRandomName() string {
	localRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	adj := adjectives[localRand.Intn(len(adjectives))]
	noun := nouns[localRand.Intn(len(nouns))]
	return fmt.Sprintf("%s %s", adj, noun)
}
