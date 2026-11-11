package main

import (
	"flag"
	"fmt"

	"github.com/meowrain/localsend-go/internal/version"
)

// showVersion 由全局 --version/-version 参数控制。
var showVersion bool

func init() {
	flag.BoolVar(&showVersion, "version", false, "Print version information and exit")
}

// printVersion 输出构建版本；未注入时显示 dev。
func printVersion() {
	fmt.Printf("localsend-go %s\n", version.String())
}
