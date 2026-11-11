# 项目名（与 CI 产物名、deb 打包脚本保持一致）
PROJECT_NAME := localsend-go

# 源代码目录
SRC_DIR := .

# 输出目录
OUT_DIR := ./bin

# Go 编译器
GO := go

# 版本注入：默认取最近的 git tag；不在 git 仓库（如源码 tarball）或 git 因
# 目录所有权检查失败（CI 容器）时退回 dev / 空。
# 发版流水线可显式覆盖：make build VERSION=v1.2.8 COMMIT=a1b2c3d
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "")
# git 不可用时兜底，避免 -X 注入空字符串把版本清空
ifeq ($(strip $(VERSION)),)
VERSION := dev
endif

VERSION_PKG := github.com/meowrain/localsend-go/internal/version
LDFLAGS := -X $(VERSION_PKG).Version=$(VERSION) -X $(VERSION_PKG).Commit=$(COMMIT)

# 目标平台
PLATFORMS := linux/amd64 linux/arm64 linux/riscv64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

# 默认目标
.PHONY: all
all: clean build

# 清理
.PHONY: clean
clean:
	rm -rf $(OUT_DIR)

# 创建输出目录
.PHONY: create-out-dir
create-out-dir:
	mkdir -p $(OUT_DIR)

# 构建
.PHONY: build
build: create-out-dir $(PLATFORMS)

# 针对每个平台编译
$(PLATFORMS):
	GOOS=$(word 1, $(subst /, ,$@)) GOARCH=$(word 2, $(subst /, ,$@)) \
	$(GO) build -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/$(PROJECT_NAME)-$(word 1, $(subst /, ,$@))-$(word 2, $(subst /, ,$@))$(if $(findstring windows,$@),.exe) $(SRC_DIR)

# 测试
.PHONY: test
test:
	$(GO) test ./...

# 安装依赖
.PHONY: deps
deps:
	$(GO) mod tidy

.PHONY:format
format:
	go fmt ./...

# 使用方法
.PHONY: help
help:

	@echo "Usage:"
	@echo "  make            - 编译所有平台的可执行文件"
	@echo "  make clean      - 清理输出目录"
	@echo "  make build      - 编译所有平台的可执行文件"
	@echo "  make test       - 运行测试"
	@echo "  make deps       - 安装依赖"
	@echo "  make help       - 显示此帮助信息"
	@echo ""
	@echo "版本：$(VERSION) ($(COMMIT))"
