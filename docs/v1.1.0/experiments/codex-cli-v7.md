# Codex CLI V7 活跃回合投递实验

- 执行日期：2026-09-03
- 执行阶段：Phase 0
- 平台：macOS 26.5.2（arm64）
- Codex CLI：`0.152.1`
- 关联决策：PD31、PD32

## 结论

V7 在 macOS 上通过。单一 App Server owner 可以在 remote TUI 的 turn 执行期间接受外部 follow-up；消息在当前 turn 完成后作为同一 thread 的下一 turn 恰好执行一次，TUI 实时显示，随后仍可人工继续交互。

CLI 的活跃回合语义与 Codex Desktop 不同：`codex queue` 在 active turn 期间接受并排队消息，而不是把消息 steer 到当前 turn。CLI 适配器必须吸收这个差异，路由层只表达一次统一投递。

## 隔离门禁

实验前确认本机已安装的 RA2A 正式版 daemon 和 Codex App Server 正在运行。实验没有调用 RA2A 的 `setup`、`restart`、`update` 或 daemon 命令，也没有使用正式版配置、控制地址、节点身份或 App Server socket。

实验 App Server 只监听工作区内的独立地址：

```text
unix:///Volumes/inwolf-4T/projects/RA2A/.cache/v1.1.0-v7-20260903/app-server.sock
```

实验结束后该 socket 自动清理；正式版 RA2A daemon 和 Codex App Server 的 PID 保持不变。

## 方法

1. 在工作区 `.cache/` 下启动独立 App Server。
2. 使用 `codex --remote` 连接该 socket 并创建全新测试 thread。
3. 首条消息要求执行 `sleep 30`，形成可观察的 active turn。
4. TUI 显示 `Working` 且命令仍在运行时，从另一进程执行：

   ```sh
   codex queue --remote <实验-socket> \
     --thread 'Run V7 active-turn test' \
     --message '<follow-up>'
   ```

5. 等待 primary 和 follow-up 完成，再从原 TUI 输入人工继续消息。
6. 正常退出 TUI 和实验 App Server，核对 socket 与正式版进程。

## 观察

- 外部命令在 primary turn 仍为 `Working` 时返回：

  ```text
  Queued message 01a062e1-624c-7fa2-8d8e-d6aa36cf4512 for thread 01a062e0-a76c-7d20-acbe-21c262027ab0.
  ```

- primary turn 完成并准确回复 `V7_PRIMARY_DONE`。
- TUI 随即显示外部 follow-up，并准确回复 `V7_FOLLOWUP_DONE`。
- follow-up 只观察到一次，没有第二 writer、重复消息、永久 thinking 或断连。
- 原 TUI 随后接受人工输入并准确回复 `V7_MANUAL_CONTINUES_OK`。
- App Server 退出后实验 socket 不再存在；正式版进程持续运行。

## 对实现的约束

1. CLI 适配器应连接拥有 TUI 的同一个 App Server，不能使用第二个 `resume` writer。
2. active turn 时应使用宿主的 queue 语义，让 follow-up 在当前 turn 后精确执行一次。
3. `delivered` 表示宿主明确接受消息，不要求所有宿主都把消息追加到当前 turn。
4. TUI 可见性和人工继续必须作为真实验收项，不能只验证后台 thread history。
5. V7 只完成 macOS 首轮验证；三平台、版本契约和 PD31 全交叉矩阵仍未完成。
