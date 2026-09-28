# RA2A 信箱（mailbox）—— 测试期临时手段

> **定位更正（2026-09-29）**：信箱是实现期为传递部署与联调指令而临时引入的测试
> 手段，**不是正式通信路径**。正式路径是 session 直投（`list_targets` 返回的
> 地址），agent 之间一律直投，与 codex↔codex 同构。
>
> 本文档保留是为了记录其实现与使用方式，便于在需要「零成本、零适配」的场合
> 临时使用。**不得以信箱投递主张互通结论。**

## 为什么需要它

RA2A 往一个已适配 agent 投递消息，语义必然是「在对方 agent 里 `turn/start`」。因此**每条消息都消耗一次模型调用**。这带来两个结构性问题：

1. **协调类通信在经济上不可行。** 两个 agent 互相传一句「做完了没」，各自烧一次额度，而信息量几乎为零。
2. **未适配的 agent 无法参与网状通信。** 每次接新宿主都要写适配器，而在此之前它连收发一条消息都做不到 —— 适配器写好之前，它在网里是完全缺席的。

信箱是节点基础设施，不是适配器。它没有会话语义、没有归属、没有模型调用。

## 地址

```
ra2a://<node-id>/mailbox/<recipient>
```

三段式，与两段式端点地址互不误认。收件人规则：`[A-Za-z0-9._@+-]{1,128}`。信箱**不进入 `list_targets`** —— 它不是会话。

## 语义

| 性质 | 取值 | 说明 |
|---|---|---|
| 投递成本 | **零模型调用** | 落盘即完成，响应 `delivery: stored` |
| 幂等 | 按 message ID | 发送方在结果不确定时重试不会重复投递 |
| 读 | 轮询式 | `--peek` 不消费；消费式读返回本批并标记已读 |
| 顺序 | 到达序 | 消费式读按到达顺序返回，最旧在前 |
| 保留 | 单收件人 500 条 | 超限丢最旧 |
| 持久化 | `~/.config/ra2a/mailbox/<recipient>.jsonl` | 文件 0600，目录 0700，重启不丢 |
| 读权限 | 仅本节点 | 需 loopback 控制面权限，与投递同源 |

## 安全边界（不夸大）

- **对端节点可以往你的信箱写，但只有你的节点能读出。**
- 节点内读信箱 = loopback 控制面访问权限，与投递已有权限相同。
- **这不是新的安全边界。** 共享 PIN 模型的既有约束原样适用。
- 投递方地址必须被本节点发布过，否则 `CALLER_SESSION_UNKNOWN` —— 适配器无法把邮件归到一个没人能回复的地址上。

## 用法

```bash
# 读（消费式）
ra2a mailbox read --to harness-u407

# 读（不消费）
ra2a mailbox read --to harness-u407 --peek

# 列出本节点信箱
ra2a mailbox list

# 本地投递
ra2a mailbox send --to harness-u407 --from "ra2a://rog306/cli-1" --message "..."

# 跨节点投递（走 LAN，由对端落盘）
ra2a send --peer rog306 --session mailbox/ra2a-u407 --message "..."
```

HTTP（loopback）：

```
POST /v1/mailbox          {"to","text","from"}     落盘
POST /v1/mailbox/read     {"to","limit","peek"}    读
GET  /v1/mailbox?to=X&peek=true                    读
GET  /v1/mailboxes                                 列出收件人
```

任何能跑 `curl` 或 CLI 的程序都能用，不需要适配器。

## 与投递语义的分工

| 场景 | 用什么 | 模型调用 |
|---|---|---|
| 「帮我做这件事」 | `ra2a send --session <endpoint>` | 1 次（目标 agent 真的执行） |
| 「做完了吗 / 错误是什么」 | `ra2a mailbox` | **0 次** |
| 未适配的 agent 想参与 | `ra2a mailbox` | 0 次 |

## 已知限制

- **跨节点信箱要求对端在同一 wire capability 之上。** 对端跑旧二进制时会以
  `DELIVERY_UNKNOWN: target "..." must be ra2a://node/endpoint` 明确拒绝 —— 错误是
  可机读的，不是静默失败。实测：向 rog306 投递跨节点信箱，LAN 往返成功、由对端解析器
  给出结构化拒绝，说明地址已送达对端。
- 收件人无身份校验，同一节点内任何能读控制面的人都能读任意信箱。
- 无附件、无富文本，仅纯文本。
- 未实现跨节点**读取**（设计上就是：信箱由所属节点读出）。
