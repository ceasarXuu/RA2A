# Codex CLI V5 App Server 契约实验

- 执行日期：2026-09-03
- 执行阶段：Phase 0
- 平台：macOS 26.5.2（arm64）
- Codex CLI：`0.152.1`
- 关联决策：PD29、PD31、PD32
- 对比版本：`0.151.0`、`0.152.1`
- 状态：macOS 通过

## 当前结论

Codex App Server `0.151.0` 和 `0.152.1` 提供了足以建立显式兼容门槛的运行时证据：

1. `initialize` 响应的 `userAgent` 包含实际 App Server 版本，同时返回平台信息。
2. 官方 schema 生成器能输出当前二进制的协议契约。
3. CLI 安全投递需要的 `thread/queue/*` 属于实验能力；客户端未声明 `experimentalApi` 时，服务端会明确拒绝。
4. 声明 `experimentalApi` 后，queue 查询返回结构化结果，可用于启动时能力探测。

两版在 RA2A 所需的 15 个请求/响应 schema 投影上完全一致，且运行时能力门禁行为一致。因此 V5 在 macOS 上通过。`0.151.0` 可作为当前契约最低候选，但它尚未完成 V7 真实投递和跨平台验证，不能仅凭 V5 直接冻结为最终发布下限。

## 隔离方式

- schema 只生成到工作区 `.cache/v0.0.15-v5-0.152.1/`。
- `0.151.0` 通过 npm 发布包下载并解包到工作区 `.cache/v0.0.15-v5-0.151.0/`；npm cache 也显式位于该目录。
- 运行时探测使用短生命周期的 `codex app-server --stdio` 子进程。
- 未执行全局安装或 `codex update`，未替换 PATH 中的本机 CLI，未启动 RA2A 开发 daemon。
- 未调用正式版 RA2A 的安装、更新、停止或重启入口。
- 报告不记录 `codexHome`、installation ID 或本机会话标题等环境信息。

## 方法与观察

### 1. 版本与平台探测

分别向两个隔离 stdio App Server 发送 `initialize`，客户端标识为 `ra2a-v5-probe/0.0.0`。响应中的 `userAgent` 分别明确包含 `0.151.0`、`0.152.1`，并返回 `platformFamily=unix`、`platformOs=macos`。

这说明适配器可以从实际连接的 App Server 获取版本，不需要假设 PATH 中的 `codex --version` 与 socket owner 一定相同。

### 2. schema 契约

执行：

```sh
codex app-server generate-json-schema --experimental --out <工作区缓存目录>
```

两个版本生成的请求契约均包含：

- `thread/list`
- `thread/resume`
- `turn/start`
- `turn/steer`
- `thread/queue/add`
- `thread/queue/list`
- `thread/queue/start`

其中关键约束为：

- `thread/queue/add` 要求 `threadId`、`clientUserMessageId` 和 `input`，响应包含可追踪的 queued submission。
- `thread/queue/start` 以 `threadId` 和可选 queued submission ID 启动下一 turn。
- `turn/steer` 要求 `expectedTurnId`，服务端以此拒绝对错误 active turn 的竞态写入。
- `turn/start` 和 queue start 的成功响应均包含 turn；steer 成功响应包含 `turnId`。

对以下 15 个 schema 的顶层 `properties` 与 `required` 进行规范化散列比较，两个版本全部一致：initialize、thread list/resume、thread queue add/list/start、turn start/steer 的参数与响应。

### 3. 能力协商

两个版本分别以 `experimentalApi=false` 初始化后调用只读的 `thread/queue/list`：

```text
error code -32600: thread/queue/list requires experimentalApi capability
```

改为 `experimentalApi=true` 后，两版的同一请求都成功返回空队列和空分页 cursor。失败是明确、可判定的协议拒绝，没有污染路由层或正式版状态。

## 对实现的约束

1. CLI 适配器初始化时读取实际 App Server `userAgent`，解析并记录版本。
2. 初始化必须显式协商 `experimentalApi`，随后用无副作用请求验证 queue 能力。
3. 版本满足但能力拒绝时返回统一 `unsupported`，不得降级到 direct resume。
4. queue/add 写出后若断连或超时，结果为 `unknown`，不得自动重试；成功时保留 queued submission ID 作为诊断依据。
5. 版本和 schema 差异只影响 CLI 适配器及契约测试，不进入 LAN 或统一路由协议。

## 后续门槛

- 尚未验证 Linux、Windows 的 `initialize` 和 queue 能力响应。
- `0.151.0` 尚未完成 V7 active-turn 真实投递；最终最低支持版本不得仅凭 schema 一致性确定。
