# LocalSend CLI

LocalSend CLI


## 安装

### 先决条件

- [Go](https://golang.org/dl/) 1.16 或更高版本

### 克隆仓库

```sh
git clone https://github.com/yourusername/localsend_cli.git
cd localsend_cli
```

### 编译

使用 `Makefile` 来编译程序。

```sh
make build
```

这将会为所有支持的平台生成二进制文件，并保存在 `bin` 目录中。

## 使用

### 运行程序

```sh
.\localsend_cli-windows-amd64.exe -mode receive
```

根据你的操作系统和架构选择相应的二进制文件运行。

### 功能


## 代码结构

- `cmd/main.go`：程序入口
- `internal/config`：配置相关代码
- `internal/discovery`：设备发现相关代码
  - `http.go`：HTTP 广播相关代码
  - `udp.go`：UDP 广播相关代码
  - `shared/shared.go`：共享代码
- `internal/handlers`：HTTP 请求处理相关代码
- `internal/models`：数据模型相关代码
- `internal/utils`：工具类代码

## 贡献

欢迎提交 issue 和 pull request 来帮助改进这个项目。

## 许可证

<!-- [MIT](LICENSE) -->

# Todo

发送功能
