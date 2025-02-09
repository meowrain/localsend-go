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
