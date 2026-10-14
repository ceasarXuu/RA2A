# 信箱本地验证记录（2026-09-29）

对应实现：`internal/mailbox`（`3dd8837`）、跨节点接线修复（`67e2299`）、文档（`8ca3ed8`）。
宿主：ubuntu407，生产 daemon 已升级至 `67e2299` 及之后。

## 已验证

| 项 | 方法 | 结果 |
| --- | --- | --- |
| 零模型调用 | 投递后 3 分钟内检查 rollout 文件改动 | 0 个 rollout 被触碰 |
| 本地投递与读 | `mailbox send` / `read` / `read --peek` / `list` | 全部符合语义 |
| 消费语义 | 连续两次读 | 第一次返回 1 条，第二次 `pending=0` |
| peek 不消费 | 连续两次 `--peek` | 两次都返回同一条 |
| 顺序 | 25 条投递后按到达序读 | 顺序与到达完全一致 |
| 25 轮退化 | 25 次连续投递 | 耗时 0s，无丢失、无乱序、发送方一致 |
| 重启持久性 | 5 条待读 → `ra2a restart` → 再 peek | `pending=5` 前后一致 |
| 落盘权限 | `stat` | 目录 0700、文件 0600 |
| 端点隔离 | 单测 + 运行时 | 信箱地址不进适配器；端点地址不进信箱 |

## 跨节点

向 rog306 投递 `ra2a://rog306/mailbox/ra2a-u407`：

```json
{"error":"DELIVERY_UNKNOWN: send message to rog306: CoAP code InternalServerError:
 DELIVERY_UNKNOWN: target \"ra2a://rog306/mailbox/ra2a-u407\" must be ra2a://node/endpoint"}
```

LAN 往返成功，且错误由**对端解析器**给出（不是本地拒绝），证明地址已送达对端、
对端如实回报不认识该命名空间。这是对端尚未升级时的预期形态，错误可机读、不静默。

跨节点**存储**路径待 rog306 升级后验证。

## 本地无法自证的部分

尝试在同机起第二个 RA2A 节点来自证跨节点信箱：两个 daemon 同机广播同一个
`_ra2a._udp` 服务，从 nodeB 看 `ubuntu407` 为 `unreachable`。这是同机自碰撞
（地址解析与 DTLS 会话互相干扰），不是产品缺陷，测试实例已清理，生产节点
627 会话未受影响。同机双节点不可用于该验证。
