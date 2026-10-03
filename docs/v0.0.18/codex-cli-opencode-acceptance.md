# Codex CLI↔OpenCode 验收（2026-10-04）

- 状态：执行中。Owner在CLI↔CLI人工验收通过后要求继续本方向测试。
- Codex CLI：`ra2a://ubuntu407/01a0f6ff-b902-7970-acba-ef8d3451c6f7`。
- OpenCode：Owner指定 `ra2a://rog306/ses_efcdeaba1ffeWmQEng78CCAXRO`；首次发现node ready/stale=false、agent=opencode/session ready。
- 收件、业务回信、任务完成分别记录；accepted不等于任务完成。不确定写入不重发。
- OpenCode只声明receiveText/replyAddress，未声明steerActiveTurn/interactiveSafe；活跃投递按其prompt_async宿主受理及独立执行观察，不套用Codex同turn steer语义。

## 验证矩阵

| ID | 项目 | 状态/证据要求 |
| --- | --- | --- |
| O01 | 适配器、租约、共享server、wrapper相关race回归 | 待执行；skip不计PASS |
| O02 | 双向基本收件与业务回信 | 前置投递accepted；原始接收、回复、执行待核 |
| O03 | CLI→OpenCode 22条串行空闲输入 | 每次发现ready、工具原始收件、宿主消息/精确ACK、缺失重复独立核验 |
| O04 | OpenCode→CLI 22条串行活跃收件 | Ubuntu持续测试工作中接收；同一活跃任务收件不冒充22个完成任务 |
| O05 | 双向工作中收件 | OpenCode宿主受理/排队与CLI活跃追加分开；不等待模型完成才收件 |
| O06 | 单次有界交错业务回复 | 固定次数send，无无限回投或自动重试 |
| O07 | 多端点归属/故障恢复边界 | 现有隔离夹具，未附着不发布、post未知不重放；不关闭正式server |
| U01 | 人工显示与继续输入 | 自动最后给清单，不能由后台日志替代 |
| P01 | CLI/App/OpenCode/认证/代理保护 | 各阶段前后元数据与PID记录；不部署/重启/改正式配置 |

## 执行纪律

- 仅对已发现且agent匹配的完整地址投递，from显式填写。
- 多轮API仅等收件，测试为构造独立空闲任务另行观察ready；每条至少8秒间隔，unknown/错误即止步，不重试。
- 保留所有既有修改、故障原始结果与证据。隔离HOME/USERPROFILE/LOCALAPPDATA/APPDATA/CODEX_HOME及XDG变量，GOPROXY=off；正式CLI/App与OpenCode配置不动。
- OpenCode会话存储条目不等于执行权，确认live附着租约与共享server。保护正常TUI，不自动重启。
- 本方向仅测试已声明能力；人工操作最后进行，未回报前不宣称完整验收。

## 原始证据

Ubuntu证据存忽略目录 `.cache/cli-opencode-acceptance/`；ROG目录由接收端报告。前置任务RA2A_CLI_OC_PRECHECK_001已accepted，未重发。

### Ubuntu前置结果

- 来源853c967（测试期间仅文档变化，四包测试源码未改），四个相关包race exit0，41个pass测试事件、fail0、1个opt-in native skip；随后显式native OpenCode1.18.34权限session隔离测试race PASS1/fail0/skip0。日志 `.cache/cli-opencode-acceptance/linux-race.jsonl`、`linux-native.jsonl`，隔离profile与包清单见local-test-manifest.json。不将native权限回复测试视为真实模型消息完成；现场投递另核。
- 前置控制send仅等收件，原始入参/结果/耗时在precheck-send.json；ROG的原生记录与独立回信待到达，不因短暂无回信重投。
