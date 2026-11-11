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

## 后续：合回 main 与协议适配

2026-11-11 已把 `test/npc-issues` 整体 `--no-ff` 合回 `main`（提交 `01a1741`）并推送到 GitHub / CNB。

对照 `protocol/` 下的 LocalSend v2.2 规范复核后，确认 v3 尚未定稿（官方 `constants.dart` 仍是
`protocolVersion = '2.2'`，App 全部使用 `LsHttpClientVersion.v2`，服务端 v3 的 register 标注
`TODO: not wired up yet`，协议仓库 `v3/` 只有两张 mermaid 图），因此本轮只对齐 v2.2，拆成两个 Issue：

| Issue | 范围 | 优先级 | 分支 |
|---|---|---|---|
| #9 | 设备发现对齐（`/v2/register`、UDP 回 register、版本号 2.2、`download` 置 false、`fileType` 改 MIME、补齐 401/409/429/422） | P0 | `fix/protocol-discovery` |
| #10 | 接收端 v2.1/2.2 能力（PIN、metadata、`sha256` 校验返回 422、204/409） | P1 | `feat/protocol-receive-v22` |

两个 Issue 均已完成并合并：PR #11 → `a967b04`，PR #12 → `eaf4128`。

## 两轮协议适配的审查记录

每轮都做了「独立 worktree 跑测试 + 真实二进制端到端 + 临时测试复现缺陷」三步，临时测试均未提交。

**PR #11（设备发现）三轮**

1. 阻断 ×3：指纹是常量 `"random-string"`，两台机器互判为自己 → 回发被跳过；`deviceType` 当必填把官方公告整个丢弃；HTTP 探测成功后不回填 scheme，发送端回退 https。
2. 阻断 ×1：修 `announce` 时把它改成 `false`，而 `shared.Message` 同时是 UDP 公告载荷 → 公告不再触发任何对端响应，双向发现两个方向全断。
3. 通过。关键修法是把「公告」和「register 请求体」拆成两份载荷（`shared.Message` / `shared.RegisterRequest()`）。

**PR #12（接收端能力）三轮**

1. 阻断 ×1：传输中断后文件不落终态，会话永久占用，其他设备 prepare 永远 409；高 ×1：已完成会话允许 token 重放覆盖文件；中 ×1：`--pin` 把本机 PIN 与对端 PIN 混成一个值。
2. 阻断 ×1：`pending` 表用 `sessionId+fileId` 做 key，同一文件的并发上传互相覆盖终态——失败请求会把已成功落盘的文件标成 `failed=true`，而 `failed` 正是「允许重传」的信号，重放防护被打开；并发测试实测 8 个请求全部 200。
3. 通过。修法是 `inProgress` 状态机 + 请求实例级记账 key，`finishUpload` 在同一把锁内完成「读结论 → 落终态 → 清 inProgress」。

共同教训：这类缺陷单线程用例查不出来，必须用并发 + 断言「状态与磁盘事实一致」，而不是只断言状态码。

## 遗留

- #13 `logger.checkLogger()` 数据竞争（既有缺陷，`main` 上就存在，只是并发测试变多后才暴露）。
- #14 被放弃的会话会一直占用通道（与官方行为一致，建议加空闲超时）。
- PR #8 审查提到的「`Port` 非法时无日志」等 P2/P3 建议未全部采纳。
- 同名文件仍会覆盖（PR #5 已列为已知限制，建议单独开 issue）。
- `internal/tui` 的 `TestSelectDevice` 在无 TTY 环境仍需 skip。
