# localsend-go Issue 汇总

> 数据来源：GitHub API（`meowrain/localsend-go`），抓取时间 2026-11-10。
> 仓库概况：153 stars / 26 forks，共 14 个 issue（7 open / 7 closed）。

## 一览

| # | 状态 | 类型 | 标题 | 提出者 | 创建时间 |
|---|------|------|------|--------|----------|
| [#14](https://github.com/meowrain/localsend-go/issues/14) | 🟢 open | Bug | 中文文件名称的时候会有问题 报错403 | xtccc | 2026-08-29 |
| [#13](https://github.com/meowrain/localsend-go/issues/13) | 🟢 open | Feature | 添加"收藏设备"能力 | abdelkadouss | 2026-05-28 |
| [#12](https://github.com/meowrain/localsend-go/issues/12) | 🟢 open | Bug | getting server gave HTTP response to HTTPS client | nwgat | 2025-12-24 |
| [#9](https://github.com/meowrain/localsend-go/issues/9) | 🟢 open | Feature | 发送文本 | danielwerg | 2025-06-03 |
| [#8](https://github.com/meowrain/localsend-go/issues/8) | 🟢 open | Feature | 设置设备名 | danielwerg | 2025-06-03 |
| [#7](https://github.com/meowrain/localsend-go/issues/7) | 🟢 open | Feature | 支持直连 IP（除多播发现外） | tim-crisp | 2025-04-30 |
| [#1](https://github.com/meowrain/localsend-go/issues/1) | 🟢 open | 杂项 | [ImgBot] 图片压缩建议 | imgbot[bot] | 2024-06-16 |
| [#11](https://github.com/meowrain/localsend-go/issues/11) | 🔴 closed | Feature | Improve navigation（j/k、g/G 快捷键） | DNEGEL3125 | 2025-09-14 |
| [#10](https://github.com/meowrain/localsend-go/issues/10) | 🔴 closed | Docs | 文档中克隆仓库名不对 | yoyorgznxt | 2025-07-14 |
| [#6](https://github.com/meowrain/localsend-go/issues/6) | 🔴 closed | Bug | `receive` 收不到文件（server 启动顺序） | DNEGEL3125 | 2025-03-04 |
| [#5](https://github.com/meowrain/localsend-go/issues/5) | 🔴 closed | Bug | 连续失败后刷新 UDP 连接 | DNEGEL3125 | 2025-03-04 |
| [#4](https://github.com/meowrain/localsend-go/issues/4) | 🔴 closed | Feature | 支持用第一个参数指定文件 | undefined-moe | 2025-02-21 |
| [#3](https://github.com/meowrain/localsend-go/issues/3) | 🔴 closed | Question | 发送时启动 Web 服务的必要性 | ilius | 2024-10-30 |
| [#2](https://github.com/meowrain/localsend-go/issues/2) | 🔴 closed | Security | Windows 剪贴板命令注入，应换成 atotto/clipboard | ilius | 2024-10-25 |

---

## 未关闭 Issue 详情

### #14 中文文件名导致 403（Bug，最高优先）

**现象**：发送中文名文件时报

```
Send failed: error walking the path: error uploading file: invalid token or IP address
```

**原因（代码定位）**：

- `internal/handlers/send.go:154` 用 `fmt.Sprintf` 直接拼接查询参数，`fileId`/`token` 未做 URL 转义：
  ```go
  uploadURL := fmt.Sprintf("https://%s:53317/api/localsend/v2/upload?sessionId=%s&fileId=%s&token=%s",
      ip, sessionId, fileId, token)
  ```
- `internal/handlers/send.go:37` 直接把文件名当作 `fileId`（`ID: info.Name()`），于是中文名进入 URL。
- 本地实测：Go 服务端对未转义的 UTF-8 `RawQuery` 仍能解析出原值，所以该问题更容易在非 Go 实现（官方 App / Rust 等）的接收端上暴露——它们对非法未编码字符的处理不一致。

**参考修复**：Issue 作者提供了提交 `xtccc/localsend-go@90748ab`，用 `url.QueryEscape` 转义 `fileId`/`token`。

**建议**：
1. 对 `sessionId`/`fileId`/`token` 统一使用 `url.Values` 构造查询串。
2. 更彻底的做法：`fileId` 改为随机 UUID，文件名放在 metadata 里，避免重名文件和特殊字符问题。

**关联问题**（阅读代码时发现）：
- `internal/handlers/receive.go:44` 生成的 token 只是 `"token-"+fileID`，而 `ReceiveHandler`（:67-92）只校验非空，**没有校验 token 与 fileId 是否匹配、也没校验来源 IP**，与错误文案 "invalid token or IP address" 不符。
- `fileNames` 是包级 map，在 Prepare 写入时未加锁，存在并发读写风险。
- 同名文件在 `uploads/` 下会互相覆盖。

---

### #12 发送时 `server gave HTTP response to HTTPS client`（Bug，高优先）

**现象**：`localsend-go → localsend-go` 发送失败：

```
Post "https://192.168.1.2:53317/api/localsend/v2/prepare-upload": http: server gave HTTP response to HTTPS client
```

**评论补充**（varac）：
- localsend-go 发给 localsend-go 失败；
- 官方 GUI 发给 localsend-go 成功。

**原因（代码定位）**：

- `internal/handlers/send.go:74` 硬编码 `https://`，上传也硬编码 `https://`（:154）。
- 本地服务端却是 `http.ListenAndServe`（`main.go:449`），对外广播的协议是 `"http"`（`internal/discovery/shared/shared.go:26`）。
- 设备发现得到的 `Protocol`/`Port` 没有传给发送流程：`models.SendModel` 只有 `DeviceName`/`IP`（`internal/models/send_model.go`），`internal/tui/tui_list.go` 选择时丢弃了协议信息。

**建议**：让 `SendModel` 携带 `Port`/`Protocol`，发送时按对端广播的 `protocol` 选择 `http`/`https`，而不是写死。

> 与 #7 是同一根因，可合并处理。

---

### #7 支持直连 IP / 静态设备（Feature，高价值）

**诉求**：
- 除多播/扫描发现外，支持手工指定 IP 发送；
- 在配置里维护静态设备（名称 + IP），或把已发现设备"收藏"，无需等待发现；
- 跨子网场景必需（多播无法跨网段）。

**评论中的关键调研**（tim-crisp）：
- 接收端只声明 `protocol: http`，无加密；
- 官方 Android App 手动输入 IP 时其 API 只用 **v1** 端点（`/api/localsend/v1/*`），所以要在接收模式支持静态 IP，本仓库需补充 **v1 http 端点**；
- 发送端只用 https → 见 #12。

**后续诉求**（danielwerg）：非交互式调用

```bash
localsend-go send --name my-phone --timeout 5000 FILE
localsend-go send --ip 192.168.1.60 --timeout 5000 FILE
```

若给出 `--name`/`--ip`，则不应打开 TUI。

**代码现状**：`flagParse`（`main.go:371`）只识别 `web` / `send <path>` / `receive` / `help`，无 `--ip`/`--name`/`--timeout`；`SendFile`（`internal/handlers/send.go:230`）强制走 `tui.SelectDevice`。

---

### #13 收藏设备（Feature，与 #7 重复）

用户手机端想"收藏"电脑以便免确认发送/接收，但 CLI 端不支持。附带报错中的 `InvalidContentType` 来自 hyper 客户端，看起来是检查 `/api/localsend/v2/info` 时对响应的 Content-Type 或 TLS 有要求（**此为推测，尚未复现**）。

**说明**：此项与 #7 高度重叠，建议合并为一个"静态设备/收藏设备 + 直连 IP"的 feature issue。

---

### #8 设置设备名（Feature，实际已部分实现但存在配置读取 Bug）

**评论**（meowrain，2026-01-14）：该功能已实现，将在后续版本发布。

**代码现状**：`internal/config/config.go:18` 已有 `DeviceName` 配置项，`config.go:63` 会优先使用它。

**但存在 Bug**：`config.go:49` 只读 `internal/config/config.yaml`（相对**当前工作目录**），

- 仓库根目录的 `config.yaml`（含 `device_name`）**完全不会被读取**；
- 从其他目录运行二进制时读不到文件，回退到 `go:embed` 的内嵌 `internal/config/config.yaml`，而内嵌文件中没有 `device_name` 字段，于是始终使用随机名。

**建议**：读取用户配置目录（如 `~/.config/localsend-go/config.yaml`）或可执行文件同目录，并在首次运行时生成默认配置；同时把 `device_name` 写入内嵌默认配置。

---

### #9 发送文本（Feature）

截图显示了官方 GUI 的"发送文本"能力，期望 CLI 也支持。

**代码现状**：接收侧已具备相关逻辑——`internal/handlers/receive.go:48-51` 在 prepare 阶段收到 `.txt` 时会打印 preview 并复制到剪贴板。但发送侧只有 `SendMode(filePath)`，没有"发送文本/剪贴板内容"的入口。

**建议**：新增 `send --text <string>` 或 `send --clipboard`，按协议生成一个虚拟 `.txt` 文件（`Preview` 字段带文本内容）。

---

### #1 ImgBot 图片压缩（杂项）

imgbot 自动提交的优化建议，可节省约 23%（约 565 KB），主要收益在 `doc/images/logo.png`。可直接合并或忽略。

---

## 已关闭 Issue（供回顾）

| # | 结论 |
|---|------|
| #11 | `j/k` 导航、`g/G` 跳转首尾，已合并（PR #11 → commit `b833909`/`eda5c81`/`aa5ee5f`）。 |
| #10 | 文档中 `git clone` 仓库名错误。**注意：主 README 及 `doc/README_{zh,en,jp}.md` 目前仍写着 `localsend_cli`，需要再修一遍。** |
| #6 | `receive` 收不到文件：server 必须在 `flagParse` 之前启动。 |
| #5 | 网络切换后刷新 UDP 连接。 |
| #4 | 支持首个参数直接指定文件（`localsend-go send <path>` 已实现）。 |
| #3 | 发送模式下无需启动 Web 服务：现已按 `config.yaml` 的 `functions` 开关控制。 |
| #2 | Windows 剪贴板命令注入（RCE）。已改用 `github.com/atotto/clipboard`，见 `internal/utils/clipboard/clipboard.go`；但仍需注意 #14 的自动写剪贴板行为。 |

---

## 处理优先级建议

1. **#12 / #7 协议与端口硬编码** —— 直接导致 CLI ↔ CLI 传输失败，影响核心可用性；同时解决 #13。
2. **#14 中文文件名 403 + 接收端 token/IP 校验缺失** —— 修复转义，并补上 token 校验（顺带修 #2 关闭后残留的自动接收风险）。
3. **#8 配置读取路径 Bug** —— 功能看似已实现，实际用户改配置不生效，容易误判。
4. **#9 发送文本** —— 接收端逻辑已就绪，成本较低。
5. **#1 图片压缩 / 文档里的旧仓库名** —— 低成本清理项。
