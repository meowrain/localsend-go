<div align="center">
    <h1>LocalSend Go</h1>
    <h4>✨A LocalSend CLI Tool Implemented in Go✨</h4>
    <img src="https://forthebadge.com/images/badges/built-with-love.svg" />
    <br>
    <img src="https://counter.seku.su/cmoe?name=localsend-go&theme=mb" alt="localsend-go" />
</div>

## Introduction

LocalSend Go is a command-line implementation of the LocalSend protocol in Go, supporting cross-platform file transfer. This project provides both a simple command-line interface and a TUI (Terminal User Interface) for quick file transfers between devices.

## Features

- File sending and receiving
- Cross-platform support (Windows, Linux, macOS)
- Clean TUI interface
- Text and file transfer support
- Automatic device discovery
- Multi-language support

## Documentation

[中文](doc/README_zh.md) | [EN](doc/README_en.md) | [日本語](doc/README_jp.md)

Currently divided into version v1.1.0 and v1.2.0. For version v1.1.0 documentation, see [Localsend-Go-Version-1.1.0 doc](version1.1.0/).

The following documentation is for version v1.2.0.

## Installation

### Package Manager

#### Arch Linux
> ⚠️ Note: The arch package is still on version 1.1.0

```bash
yay -Syy
yay -S localsend-go
```

### Build from Source

1. Ensure Go 1.22 or higher is installed
2. Clone the repository
   ```bash
   git clone https://github.com/meowrain/localsend_cli.git
   cd localsend_cli
   ```

3. Build
   ```bash
   make build
   ```

The compiled binaries will be saved in the `bin` directory.

## Usage

### Basic Usage

<div align="center">
    <p><b>Main Interface</b></p>
    <img src="https://blog.meowrain.cn/api/i/2025/02/09/eHAgcd1739113761477122645.avif" width="80%" />
</div>

1. Launch the program
   - Windows: Double-click the executable or run from command line
   - Linux/macOS: Run the executable in terminal

2. Select Mode
   - Use arrow keys to select operation mode (Send/Receive)
   - Press Enter to confirm

3. Send Mode
   - Select file to send
   - Wait for receiver connection
   - Confirm transfer

   <div align="center">
       <p><b>Send Interface</b></p>
       <img src="https://blog.meowrain.cn/api/i/2025/02/09/xPUd841739113859215495111.avif" width="80%" />
       <p><b>Client Confirmation</b></p>
       <img src="https://blog.meowrain.cn/api/i/2025/02/09/mS1J3k1739113875412020167.avif" width="80%" />
   </div>

4. Receive Mode
   - Wait for sender connection
   - Automatically receive files
   - Use `Ctrl + C` to end program

   <div align="center">
       <p><b>Receive Interface</b></p>
       <img src="https://blog.meowrain.cn/api/i/2025/02/09/OZuXZu1739113816793484432.avif" width="80%" />
       <p><b>Transfer Complete</b></p>
       <img src="https://blog.meowrain.cn/api/i/2025/02/09/YjbG9f1739113834583691367.avif" width="80%" />
   </div>

### Special Notes

Linux systems require additional ping permission configuration:
```bash
sudo setcap cap_net_raw=+ep localsend_cli
```

## Project Structure

```
.
├── cmd/          # Main program entry
├── internal/     # Internal packages
│   ├── discovery/   # Device discovery
│   ├── handlers/    # Request handlers
│   ├── models/      # Data models
│   └── utils/       # Utility functions
├── static/       # Static resources
└── templates/    # Template files
```

## Development Plan

- [x] Enhanced sending functionality with text display support
- [x] TUI refresh optimization
- [ ] Complete internationalization support
- [x] Transfer progress display improvement
- [ ] File transfer resume capability

## Contributing

Issues and Pull Requests are welcome. When contributing, please:

1. Follow Go code conventions
2. Add necessary tests
3. Update relevant documentation
4. Keep code clean and clear

## License

This project is licensed under the [MIT](../LICENSE) License.

## Star History

<div align="center">
    <a href="https://star-history.com/#meowrain/localsend-go&Date">
        <img src="https://api.star-history.com/svg?repos=meowrain/localsend-go&type=Date" width="80%" />
    </a>
</div>
