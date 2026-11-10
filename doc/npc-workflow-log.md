# NPC 协作流程记录（2026-11-10 ~ 11-11）

## 结论

4 个 NPC 实现的 PR 全部完成并经过 `代码审查员` 审查，最终合并进测试分支
`test/npc-issues`（最新提交 `bcc4d87`）。构建、静态检查、单测、`-race`、三平台交叉编译全部通过。

## 仓库与角色

- 仓库：`cnb.cool/meowyyds/localsend-go`
- 角色（`.cnb/settings.yml`）：`代码审查员`、`Go 专家`、`项目助手`
- 触发：`.cnb.yml` 中 `issue.comment@npc` / `pull_request.comment@npc` → `npc:go`
- 召唤语法：`@meowyyds/localsend-go(Go 专家)`、`@meowyyds/localsend-go(代码审查员)`

## Issue → PR 映射

| Issue | 主题 | 分工 NPC | PR | 分支 |
|---|---|---|---|---|
| #1 | 发送端硬编码 https/53317 | Go 专家 | #8 | `fix/send-protocol` |
| #2 | 中文文件名 403 + token 校验 | Go 专家 | #5 | `fix/upload-token` |
| #3 | `device_name` 不生效 | Go 专家 | #6 | `fix/config-path` |
| #4 | 发送文本 / 剪贴板 | Go 专家 | #7 | `feat/send-text` |

## 合并过程

逐个 `--no-ff` 合并，顺序 `#5 → #8 → #6 → #7`（先修安全面，再改 URL 构造，最后动
`main.go` 参数解析）。实际冲突：

- `internal/handlers/send.go`：PR #5 的 `localFiles`/`matchUploadTarget` 与 PR #8 的
  `uploadFile(device, ...)`、PR #7 的 `sendSpec`/`uploadContent` 三套改动叠加。
  最终统一为 `sendSpec{prepare(device), entries}` + `uploadContent(ctx, device, ...)`，
  URL 用 `buildSendURL` + `url.Values`。
- `main.go`：PR #6 的 `--config` 与 PR #7 的 `runSendCommand` 并存。
- `internal/tui/tui_test.go`：三个 PR 各自加了一份无 TTY 跳过逻辑，合并为 `requireTTY`。

平台按钮无法完成这些语义合并，因此 PR 均为手工合并后关闭（`is_merged=false`）。

## 审查意见的复核结果

| 来源 | 意见 | 实情 |
|---|---|---|
| PR #8 审查 | `url.Values.Encode()` 会被 `net/http` 二次编码，中文名必 403 | **误报**：httptest 实测 `fileId` 原样到达 |
| PR #5 审查 | `safeJoinUnder` 可越界写到 `/tmp` | **误报**：内部先调 `isSafeBaseName`，实测已拒绝 |
| PR #5 审查 | 空格结尾文件名静默改名 | **误报**：会被 `TrimRight` 规则拒绝 |
| PR #5 审查 | 会话淘汰误删在传会话 | 属实，已改为优先淘汰最久未使用者 + 上传成功 `touch` |
| PR #5 审查 | `subtleCompare` 自研位运算脆弱 | 属实，已换 `crypto/subtle.ConstantTimeCompare` |
| PR #5 审查 | Windows 保留名 / 超长名 / Cf 格式字符未拦截 | 属实，已补充 |
| PR #5 审查 | `matchUploadTarget` 单 token 兜底跨文件边界 | 属实，已加 `localCount == 1` 条件 |
| PR #6 审查 | 显式路径不存在时默认配置写到用户目录 | 属实，已修 |
| PR #6 审查 | 解析失败回退跳过设备名解析，空 alias 广播 | 属实，已修 |
| PR #6 审查 | `WriteDefault` 覆盖用户配置、权限 0644 | 属实，改 `O_EXCL` + `0600` |
| PR #7 审查 | `uploadContent` 的 `defer resp.Body.Close()` 在 `err != nil` 时 nil deref | 属实，已修 |
| PR #7 审查 | `--text a --text b` 被静默覆盖 | 属实，已拒绝重复 flag |
| PR #7 审查 | `TruncatePreview` 非法 UTF-8 下切出半个 rune | 属实，已修 |

## 合并后额外发现并修复

1. **`--port` 与 `--config` 实际都不生效**：`flag.Parse()` 放在 `flagParse()` 里，
   而监听服务与 `config.Load()` 都更早执行。已把参数解析提到 `main` 开头。
2. **广播设备名恒为空串**：`shared.Message.Alias` 在包初始化时读
   `config.ConfigData.NameOfDevice`，那时配置还没加载。改为 `config.Load()` 后调用
   `shared.SyncFromConfig()`。这是 PR #6 声称修复的 issue #3 实际未生效的根因。
3. **HTTP 探测写死 `https://<ip>:53317`**：多播不可用的网络里，明文 http 对端完全发现不到。
   改为先按本机广播协议试、失败退回 https。
4. **`SelectDeviceInfo` 无设备时返回零值**：会拼出 `https://:53317`，报 connection refused。
   改为返回明确错误。

## 验证

```
go build ./...                              OK
go vet ./...                                OK
go test ./...                               OK
CGO_ENABLED=1 go test -race ./...           OK
GOOS=windows/amd64 darwin/arm64 linux/arm64 go build   OK
```

端到端（真实二进制 + curl）：中文文件名上传 200、错误 token 403、`../` 路径穿越 403、
`--config` 生成到指定路径、`--port` 生效、广播 alias 与配置一致。

## 遗留

- `test/npc-issues` 尚未合回 `main`。
- PR #8 审查提到的「`Port` 非法时无日志」等 P2/P3 建议未全部采纳。
- 同名文件仍会覆盖（PR #5 已列为已知限制，建议单独开 issue）。
- `internal/tui` 的 `TestSelectDevice` 在无 TTY 环境仍需 skip。
