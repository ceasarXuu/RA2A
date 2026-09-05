# Codex CLI 实验账号用量检查口径

在跑 Codex 宿主实验（V4/V7/V8、wrapper/queue 真机投递等）之前，先确认账号用量未到限流阈值，避免实验耗尽额度、触发限流横幅干扰自动化、甚至引出后端拦截。

## 检查命令

```sh
# 实验 app-server 起好后（或连接任何官方 app-server）
appserver-probe -codex codex -socket <实验-socket> -timeout 30s -account-rates
```

## 判定口径

关注 `rateLimitsByLimitId` 中的两个桶：

- `codex`（plan 级，周窗口 10080min）：**usedPercent ≥ 90 即会触发 TUI「Switch to gpt-5.6-luna」提示条；到 100 后端开始拦截 `chatgpt.com/backend-api/codex/responses`**（实测 2026-09-06：实验连跑十几轮后该桶被推满，出现 cf-ray 拦截页）。
- `codex_bengalfox` 等模型桶（如 GPT-5.3-Codex-Spark）：各模型独立额度，与 plan 桶无关；例如 2026-09-06 模型桶 63%（用户视角"剩 37%"）而 plan 桶已是 100%。

## 经验要点

1. **弹窗不是模型触发**：TUI 的 rate-limit switch prompt 只看 plan 级 `codex` 桶（阈值 ≥90，见 codex-rs `chatwidget/rate_limits.rs` `RATE_LIMIT_SWITCH_PROMPT_THRESHOLD=90`），与用户所选模型、remote/内嵌无关。
2. **提示条不阻断真实用户**（Esc 或选项 3 即关），但会挡住无人值守的 pty 自动化按键，需自动化先关掉或等额度窗口重置。
3. **实验前必须检查**：plan 桶接近/达到 90-100% 时停止真实投递实验，记录现场，等周窗口重置（`resetsAt`）再继续；不要反复触发后端拦截。
4. 实验产生的回合和 sleep 线程会真实占用账号 plan 额度，批量跑之前评估用量。