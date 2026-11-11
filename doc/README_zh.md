<div align="center">
    <h1>LocalSend Go</h1>
    <h4>✨使用Go实现的LocalSend命令行工具✨</h4>
    <img src="https://forthebadge.com/images/badges/built-with-love.svg" />
    <br>
    <img src="https://counter.seku.su/cmoe?name=localsend-go&theme=mb" alt="localsend-go" />
</div>

## 项目简介

LocalSend Go 是一个使用Go语言实现的LocalSend协议命令行工具，支持跨平台文件传输。本项目提供了简单的命令行界面和TUI界面，方便用户在不同设备间快速传输文件。

## 功能特点

- 支持文件发送和接收
- 跨平台支持（Windows, Linux, macOS）
- 简洁的TUI界面
- 支持文本和文件传输
- 自动设备发现
- 内置 Web 页面：浏览器上传/下载/管理文件，并可反向发送到局域网设备
- 多语言支持

## 安装方法

### 从包管理器安装

#### Arch Linux
> ⚠️ arch包还没有更新，还是第一版

```bash
yay -Syy
yay -S localsend-go
```

### 从源码编译

1. 确保已安装Go 1.22或更高版本
2. 克隆仓库
   ```bash
   git clone https://github.com/meowrain/localsend_cli.git
   cd localsend_cli
   ```

3. 编译
   ```bash
   make build
   ```

编译后的二进制文件将保存在 `bin` 目录中。

## 使用说明

### 基本用法

<div align="center">
    <p><b>主界面</b></p>
    <img src="https://blog.meowrain.cn/api/i/2025/02/09/eHAgcd1739113761477122645.avif" width="80%" />
</div>

1. 启动程序
   - Windows: 双击可执行文件或在命令行中运行
   - Linux/macOS: 在终端中运行可执行文件

2. 选择模式
   - 使用方向键选择运行模式（发送/接收）
   - 按Enter确认选择

3. 发送模式
   - 选择要发送的文件
   - 等待接收端连接
   - 确认传输

   <div align="center">
       <p><b>发送界面</b></p>
       <img src="https://blog.meowrain.cn/api/i/2025/02/09/xPUd841739113859215495111.avif" width="80%" />
       <p><b>客户端确认</b></p>
       <img src="https://blog.meowrain.cn/api/i/2025/02/09/mS1J3k1739113875412020167.avif" width="80%" />
   </div>

4. 接收模式
   - 等待发送端连接
   - 自动接收文件
   - 使用 `Ctrl + C` 结束程序

   <div align="center">
       <p><b>接收界面</b></p>
       <img src="https://blog.meowrain.cn/api/i/2025/02/09/OZuXZu1739113816793484432.avif" width="80%" />
       <p><b>接收完成</b></p>
       <img src="https://blog.meowrain.cn/api/i/2025/02/09/YjbG9f1739113834583691367.avif" width="80%" />
   </div>

### 命令行发送文本 / 剪贴板

除了发送文件，也可以把一段文本封装成虚拟 `.txt` 文件发出去（对端会打印预览并写入剪贴板）：

```bash
localsend-go send --text "验证码 123456"
localsend-go send --clipboard          # 发送本机剪贴板内容
localsend-go send ./some/file.pdf      # 原来的文件发送方式
```

`<file_path>`、`--text`、`--clipboard` 三者互斥，同时给出会报错并以非 0 退出码结束。

### Web 模式（浏览器传输）

```bash
localsend-go web            # 默认端口 53317
localsend-go --port 8080 web
```

启动后终端会打印二维码，手机扫码即可打开页面；页面上可以：

- **上传**：拖拽 / 选择文件 / 选择整个文件夹（保留目录结构）/ 粘贴上传，逐文件进度与速度，
  支持取消；超过 16 MiB 的文件自动走分片上传（4 MiB 一片，刷新后可断点续传）；
  同名文件可选择覆盖、跳过或自动改名
- **浏览与管理**：面包屑导航、搜索、按名称/时间/大小排序、多选；重命名（支持撤销）、
  移动、删除（带确认框与文件清单）、新建文件夹；单文件下载、多选或整个目录打包成 zip 下载
- **操作反馈**：删除/重命名/新建都有确认框、提交中状态与成功提示；提示条支持关闭、
  悬停暂停倒计时、「撤销」快捷操作
- **预览**：图片打开内置查看器（滚轮/双指/双击缩放、拖动平移、旋转、适应窗口/原始大小、
  全屏、左右切换、滑动切换、保存）；音视频、PDF、常见文本/代码文件在线预览
  （HTML 等危险类型强制下载）
- **发送到设备**：列出局域网内发现的 LocalSend 设备，把文件、目录或一段文本直接发过去，
  带进度与取消；桌面浏览器原生 `navigator.share`
- **分享**：生成当前页面的二维码与链接；「传输记录」页查看最近的收/发历史
- **外观主题**：靛蓝（默认）、森林、暖橙、海盐青、玫瑰、赛博紫 6 套配色，每套都有浅色 /
  深色两版，外加「跟随系统」；移动端适配、离线可用（不依赖任何 CDN）

#### 外观主题

点顶栏的调色板按钮打开主题面板：上面一行选浅色 / 深色 / 系统，下面选主题。选择立即生效，
并存在 `localStorage` 的 `localsendgo-theme` 里（格式 `主题名:明暗`，例如 `forest:dark`）。
`<head>` 里的小段内联脚本会在首屏绘制前把值写到 `<html>` 上，所以刷新不会先闪一下默认主题。
老版本存下的 `light` / `dark` 会被读成「默认主题 + 对应明暗」，老用户外观不变。面板支持点击
外部或 `Esc` 关闭，也可以用 `↑` / `↓` + `Enter` 选择。

配置项（`config.yaml`）：

```yaml
functions:
  http_file_server: true     # 关闭后整个 Web 模式不注册路由
  pin: ""                    # 非空时 Web 页面/接口都需要 PIN
  web_upload_limit_mb: 4096  # 单次上传上限（MiB），0 表示不限
```

> 安全提示：Web 模式默认信任局域网（与 LocalSend 协议一致）。如果担心同网段其他人访问，
> 请设置 `pin`（或启动时加 `--pin`）：页面会要求登录，写操作还额外需要页面内的
> CSRF token。不要把 Web 模式直接暴露到公网。

### 特殊说明

Linux系统需要额外配置ping权限：
```bash
sudo setcap cap_net_raw=+ep localsend_cli
```

## 项目结构

```
.
├── cmd/          # 主程序入口
├── internal/     # 内部包
│   ├── discovery/   # 设备发现
│   ├── handlers/    # 请求处理
│   ├── models/      # 数据模型
│   └── utils/       # 工具函数
├── static/       # 静态资源
└── templates/    # 模板文件
```

## 开发计划

- [x] 发送功能完善，支持文本直接显示
- [x] TUI界面刷新优化
- [ ] 完整的国际化支持
- [x] 传输进度显示优化
- [ ] 文件传输断点续传

## 贡献指南

欢迎提交Issue和Pull Request。贡献时请注意：

1. 遵循Go代码规范
2. 添加必要的测试
3. 更新相关文档
4. 保持代码简洁清晰

## 许可证

本项目采用 [MIT](../LICENSE) 许可证。

## Star History

<div align="center">
    <a href="https://star-history.com/#meowrain/localsend-go&Date">
        <img src="https://api.star-history.com/svg?repos=meowrain/localsend-go&type=Date" width="80%" />
    </a>
</div>
