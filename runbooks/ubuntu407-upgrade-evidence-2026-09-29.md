# ubuntu407 正式服务升级记录（2026-09-29）

对应 Windows 侧同类记录：[windows-codex-cli-validation-evidence-2026-09-28.md](windows-codex-cli-validation-evidence-2026-09-28.md)。

## 变更

| 项 | 值 |
| --- | --- |
| 升级前 | `v0.0.15`，sha256 `eaca7ab7462cb3f447644aa6849465c6d2e67bbe1e86ef38bd6d62e5325b8f5e` |
| 升级后 | `main` @ `bd3618f`，sha256 `7e0f5f5621bbc0069fa5cc4005a4d14267b6611691ec6cbfe2a522f979bf7c27` |
| 回退点 | `~/.local/bin/ra2a.retired-20260929000937`（升级前二进制，未删除） |
| 重启方式 | 新二进制执行 `ra2a restart`（走 `operator.InstallAndStart`，同时重新注册 Codex MCP） |
| 正式配置 | `~/.config/ra2a/config.json` 未改动，`codex` 路径不变 |

替换二进制用 `mv` 原子替换，避免运行中写入导致的 `Text file busy`；先备份再替换。

## 会话数回归（关键指标）

| 时点 | macmini-m4 | rog306 | ubuntu407 | 合计 |
| --- | --- | --- | --- | --- |
| 升级前 | 22 | 289 | 316 | 627 |
| 重启后 4 秒 | 22 | **0（消失）** | 316 | 338 |
| 重启后 29 秒 | 22 | 289 | 316 | 627 |

重启后 4 秒的会话数塌陷**不是回归**：mDNS 发现与 peer 会话列表按 1 分钟周期刷新
（`internal/lannode/node.go` 的 `discoveryReloadInterval`），重启后首个探测窗口内
rog306 尚未重新出现在列表里。等待一个发现周期后完全恢复，且数量与升级前逐项一致。

这一点与 Windows 侧记录的真实回归（`busy` 端点被 `Endpoint.Validate` 拒绝，导致
289 个 Desktop 会话全部消失）成因不同，修复后不再复现。

## 混版本现场（升级后的真实拓扑）

```
macmini-m4   22   agent=(absent)     capabilities=0    ← 旧节点，新版正确读取为 legacy
rog306      289   agent=codex-app    capabilities=289  ← 新版 ↔ 新版
ubuntu407   316   agent=codex-app    capabilities=316
```

新版节点读取旧节点的 session 时，`agent` 与 `capabilities` 保持缺失而不是被补成
默认值；该行为由 `internal/lannode/protocol_compat_test.go` 双向钉死。

## 本次顺带发现并修复的问题

控制层 `POST /v1/send` 对本地投递与跨节点投递都返回 `{"status":"accepted"}`，
但两者含义不同：本地投递在适配器返回 `delivered` 后才应答，跨节点投递只是把消息
交给 LAN 传输层。这正是 V10 揭示的「accepted 不等于 delivered」陷阱在 RA2A 自身的
重现。现已在响应中增加 `delivery` 字段区分 `confirmed` 与 `handed_to_transport`。
