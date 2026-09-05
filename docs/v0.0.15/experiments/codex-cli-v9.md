# Codex CLI V9 daemon 自动挂接路线验证与源码研究

- 状态：macOS 首轮未通过（自动挂接未生效）
- 执行日期：2026-09-06
- 平台：macOS 26.5（arm64），macmini-m4
- 关联决策：PD29、PD31、PD32（本轮按用户决定放宽独立 CODEX_HOME；生产 RA2A daemon 全程不动）
- 前置：用户明确否决「要求用户以 `--remote` 启动 TUI」（产品体验要求与 Desktop 一致：用户正常启动 codex 即可）

## 路线定义

V9 验证「用户零动作」的唯一官方机制：**本机共享 app-server daemon + 正常启动的 codex TUI 自动挂接**，RA2A 作为 daemon 的另一客户端投递：

```text
codex app-server daemon（官方托管 app-server，监听 $CODEX_HOME/app-server-control/app-server-control.sock）
   │                         ▲                    ▲
   │   TUI 自动 discovery      │   RA2A（clientInfo=ra2a）│
   └── codex 正常启动（无参数）   └── thread/queue/* 投递
```

## 实测（0.153.4，standalone managed 安装）

| 步骤 | 结果 | 证据 |
| --- | --- | --- |
| `codex app-server daemon start` | ✅ | JSON 返回 status=started，`~/.codex/app-server-control/app-server-control.sock` 可连 |
| 外部协议客户端连接 daemon | ✅ | appserver-probe 直连列出同一 `~/.codex` store 线程 |
| TUI `--remote unix://<daemon socket>` | ✅ | 走通到创建会话流程（目录信任提示出现） |
| **普通 `codex`（无任何参数）自动挂接 daemon** | ❌ | 交叉 lsof 确认 TUI 进程与 daemon 进程无任何 socketpair 连接；TUI 走内嵌 app-server（自身 socketpair） |

结论：**在 macOS 0.153.4 上，普通启动的 codex TUI 没有自动挂到正在运行的官方 daemon。**

## 源码研究（openai/codex `codex-rs`，含发布 tag rust-v0.153.4）

### 1. Desktop 的 follower 通道不在开源 CLI 中

`thread-follower-steer-turn` / `thread-follower-start-turn`（RA2A desktopipc 所用）在开源 codex-rs **不存在**。它们属于官方 Desktop 私有 IPC，开源 CLI 没有任何等价「正常运行即暴露外部 follower 注入通道」的机制。→ **「像 Desktop 一样从外部注入正在运行的 TUI」在开源 CLI 上无官方入口。**

### 2. 正常启动默认内嵌 app-server，不暴露外部 socket

`codex` 无 `--remote` 时 TUI 使用 `AppServerClient::InProcess`（`tui/src/app_server_session.rs`），内嵌 app-server 以同进程 tokio 任务运行（`app-server-client/src/lib.rs` `InProcessAppServerClient::start`），只经内部传输，不绑定任何对外地址。→ 第三方进程没有可连接的地址。

### 3. 「零动作」自动挂接机制确实存在（0.153.4 已含），条件苛刻

`tui/src/startup_orchestration.rs`：

```rust
let reuse_implicit_local_daemon = !workload_identity_selected
    && (cli.agents_overview
        || can_reuse_implicit_local_daemon(&cli_kv_overrides, &launch_loader_overrides,
                                           strict_config, cli.bypass_hook_trust));
let default_daemon = if explicit_remote_endpoint.is_none() && reuse_implicit_local_daemon {
    startup_draft.run_until(maybe_probe_default_daemon_socket(&codex_home)).await?
} else { None };
```

`can_reuse_implicit_local_daemon` 要求：**无 `-c` 配置覆盖、loader overrides 全默认、非 strict_config、无 `--bypass-hook-trust`、无 workload identity、无 exec-server URL**。命中后 target=LocalDaemon，TUI 作为 daemon 客户端（macOS 非 Windows 路径与 `--remote` 同一 `connect_remote_app_server`）。

实测普通 `codex`（上述条件表面全满足）仍未挂接；0.153.4 二进制决策日志走内部 log_db，回退原因未能从外部日志确认。**该机制较新、条件苛刻、行为验证不足。**

### 4. 两个 app-server 可并存，但每个 thread 单 writer

`thread-store` 有跨进程 `writer_lock_coordinator`（`thread-store/src`），即 V3 的 `already has an active writer` 之来源。Desktop 自己进程 / RA2A 托管 app-server / codex daemon 可共用一个 store，但任一 thread 同时只允许一个活跃 writer。→ V6「多 server 共存」的写安全由官方 store 锁保证，难点只在「谁能成为已加载 thread 的 owner」。

### 5. 官方 daemon 需要 standalone managed 安装

`codex app-server daemon start` 要求 `$CODEX_HOME/packages/standalone/current/codex`（官方 install.sh 安装），npm/homebrew 安装不可用。Windows 支持官方文档自相矛盾（app-server-daemon README 称 Unix-only；app-server README 称 Unix+Windows）。

## 影响与决策窗口

- 「用户零动作、正常启动即接入」路线：**唯一官方机制是自动挂接 daemon，但 0.153.4 实测未生效，可靠性未经证实**。v0.0.15 若押注此路线，需先以可观测方式复验（捕获 log_db 决策日志）或等待官方完善（main 分支仍在演进 onboarding 等门禁）。
- `--remote` 路线（V4/V7 已验证）技术上成立，但用户已否决此机型 UX；如需保留，只能由 RA2A 安装链路提供启动包装/后台 app-server 代传 `--remote`（用户无感），这是「零动作体验」的工程折中。
- 来源身份（V1）、queue active-turn 语义（V7）、cross-store 写锁（V3）等既有结论不随本实验改变。

## 追加：wrapper 路线原型与 queue 投递链（2026-09-06 晚）

Wrapper 原型（`cmd/codex-wrapper`，commit `6774ad1`）已端到端验证：

| 场景 | 结果 |
| --- | --- |
| lease 存在 + 普通 `codex` 启动 | ✅ 注入 `--remote unix://<RA2A 托管 socket>` |
| RA2A 不可用（无 lease / socket 不可连） | ✅ 完整透传原生 codex |
| 用户显式 `--remote`（任意位置） | ✅ 不注入，透传用户参数 |

Queue 投递链（`thread/queue/add` 客户端，commit `1a5d83e`）：wrapper-attached TUI 活跃回合期间入队 → 消息在 TUI 实时显示（`› ...`）→ 当前回合结束后精确执行一次并给出唯一回复；全程单一 app-server、单一 writer，submission ID 留存（如 `01a0738f-ae47-7a21-a989-cd8f7de22029`）。

实验旁证与限制：

- **账号限流横幅是 plan 级 `codex` 周桶（10080min）≥90% 触发**（源码 `RATE_LIMIT_SWITCH_PROMPT_THRESHOLD=90`），与模型/remote 无关；模型独立桶（如 codex_bengalfox=63%）不参与触发。横幅不阻断真实用户（Esc/选项 3 可关），但会挡 pty 自动化按键；实验连跑会把 plan 周桶推满（100%），随后后端拦截 `chatgpt.com/backend-api/codex/responses`（cf-ray 拦截页）。**实验前须查 `account/rateLimits/read`**（见 `runbooks/codex-account-usage-check.md`，probe `--account-rates`）。
- 实验线程会按用户全局 config 加载 MCP 服务器（状态栏可见 `Starting MCP RA2A`）——实验/适配器与生产 RA2A MCP 存在共享配置接触面，需在适配器设计中留意。
- 「人工续聊」自动验证被限流横幅与后端拦截打断（TUI 级行为，V7 已覆盖同一栈），待 plan 周桶重置后补自动收尾证据。

## 环境改动与清理

- 本轮安装了 standalone codex 0.153.4 并接管 `~/.local/bin/codex`、启动官方 daemon，随后按用户决定全部回退：daemon stop、删除 `~/.codex/packages`、删除 `~/.local/bin/codex` shim、移除 `.zprofile` 安装器 PATH 行；`codex` 恢复 npm 全局版（0.153.3）。生产 RA2A daemon 与 `.ra2a-<pid>.sock` 全程未动。
- 实验期间在共享 store 创建了带 `V6R2`、`V9DIAG` 标记的持久测试 thread；按评审惯例保留、未删除、未修改。