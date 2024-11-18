<div align="center">
    <h1>LocalSend Go</h1>
    <h4>✨CLI for localsend implemented in Go✨</h4>
    <img src="https://forthebadge.com/images/badges/built-with-love.svg" />
    <br>
    <img src="https://counter.seku.su/cmoe?name=localsend-go&theme=mb" alt="localsend-go" />
</div>

## 文档 | Document

[中文](doc/README_zh.md) | [EN](doc/README_en.md) | [日本語](doc/README_jp.md)

现分为v1.1.0版本和v1.2.0版本，v1.1.0版本文档见 [Localsend-Go-Version-1.1.0 doc](version1.1.0/)

下面为v1.2.0版本文档

## 安装|Install

### Arch Linux

```bash
yay -Syy
yay -S localsend-go
```

> 😊可以下载Release中的可执行文件，找到你对应的平台即可。

### 先决条件

- [Go](https://golang.org/dl/) 1.16 或更高版本

### 克隆仓库

```sh
git clone https://github.com/meowrain/localsend_cli.git
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

Windows 下直接双击应用就可以了。

![Windows](doc/images/windows.png)

或者

```sh
.\localsend_cli-windows-amd64.exe
```

![Version 1.2](doc/images/v1.2.png)

现在只需要使用键盘选择你要执行的模式，就会自动启动相应的模式了。

![Windows Run](doc/images/windows_run.png)

> 接收模式下接收完成后请使用 `Ctrl + C` 结束程序，不要直接关闭窗口，Windows 直接关闭窗口并不会结束程序。

根据你的操作系统和架构选择相应的二进制文件运行。

Linux 下需要执行这个命令，启用其 ping 功能：

```sh
sudo setcap cap_net_raw=+ep localsend_cli
```

## 贡献

> 感谢下面的小伙伴们的支持！

> <a href="https://github.com/meowrain/doc-for-sxau/graphs/contributors">
> <img src="https://contrib.rocks/image?repo=meowrain/localsend-go" />
> </a>

欢迎提交 issue 和 pull request 来帮助改进这个项目。

## 许可证

[MIT](LICENSE)

## Todo

- [x] 发送功能完善，发送文字可以在设备上直接显示
- [ ] TUI 刷新问题
- [ ] 国际化（i18n）

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=meowrain/localsend-go&type=Date)](https://star-history.com/#meowrain/localsend-go&Date)
