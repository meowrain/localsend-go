// Package version 保存构建期注入的版本信息。
//
// 发版构建通过
//
//	go build -ldflags "-X github.com/meowrain/localsend-go/internal/version.Version=v1.2.8"
//
// 注入版本号；直接 go build / go install 时回退到 Go 构建信息，
// 都取不到时保留 "dev"，避免 `--version` 输出空白。
package version

import (
	"fmt"
	"runtime/debug"
)

var (
	// Version 是构建版本，默认 "dev"。
	Version = "dev"
	// Commit 是构建时的 git 提交短哈希，未注入时为空。
	Commit = ""
)

func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	// go install module@version 时，主模块版本会写进构建信息。
	if (Version == "" || Version == "dev") && info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}
	if Commit == "" {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				Commit = s.Value[:7]
			}
		}
	}
}

// String 返回展示用的版本字符串，例如 "v1.2.8 (a1b2c3d)"。
// Commit 为空时只返回版本号。
func String() string {
	v := Version
	if v == "" {
		v = "dev"
	}
	if Commit != "" {
		return fmt.Sprintf("%s (%s)", v, Commit)
	}
	return v
}
