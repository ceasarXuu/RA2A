# 局域网发现与安全连接开源方案实验

- 日期：2026-08-31
- 产品权威：[RA2A 极简 PRD](../../prd/2026-08-31-ra2a-minimal-model.md#12-confirmed-product-decisions)
- 目标：优先验证成熟开源网络栈，不自行实现发现、加密、握手、重传或分片协议。
- 环境：macOS arm64、Go 1.26.5；Linux/Windows 结果仅为 amd64 交叉编译。

## 候选

| 候选 | 锁定版本 | 复用能力 | 许可证 |
|---|---|---|---|
| 完整 libp2p | `go-libp2p v0.49.0` | mDNS、Peer ID、TCP、Noise、yamux、私网 PSK、连接重建 | MIT |
| 轻量成熟组合 | `libp2p/zeroconf v2.2.0`、`go-coap v3.5.4`、`Pion DTLS v3.1.8` | mDNS/DNS-SD、CoAP 请求响应与分块传输、DTLS-PSK 加密认证 | MIT / Apache-2.0 |

两个候选使用独立 Go module，避免依赖进入 RA2A 主模块：

```text
experiments/network/libp2p
experiments/network/zeroconf-dtls
```

## 实机结果

| 验证项 | libp2p | zeroconf + CoAP + DTLS |
|---|---:|---:|
| 同 PSK 建立安全连接并回显消息 | 通过 | 通过 |
| 不同 PSK 拒绝连接 | 通过 | 通过 |
| 本机 mDNS/DNS-SD 发现 | 通过 | 通过 |
| 主动关闭连接后重新建立并再次传输 | 通过 | 通过 |
| 20 KB 消息分块传输 | 未单独验证 | 通过，由 CoAP Block-Wise 承担 |
| macOS arm64 本机构建与运行 | 通过 | 通过 |
| Linux amd64 交叉编译 | 通过 | 通过 |
| Windows amd64 交叉编译 | 通过 | 通过 |

交叉编译只证明源码和依赖可构建，不证明目标系统上的组播、防火墙、网卡切换或后台服务行为。

## 量化结果

使用 `-trimpath -ldflags="-s -w"` 构建，运行一次“注册 → 发现 → 安全连接 → 消息往返 → 退出”自检：

| 指标 | libp2p | zeroconf + CoAP + DTLS |
|---|---:|---:|
| macOS arm64 二进制 | 23,369,458 B | 6,327,058 B |
| Linux amd64 二进制 | 24,850,594 B | 6,672,546 B |
| Windows amd64 二进制 | 25,165,312 B | 6,832,640 B |
| Go module 数 | 88 | 13 |
| 非标准库 package 数 | 329 | 75 |
| 最低 Go module 基线 | Go 1.25.7 | Go 1.24.0 |
| 自检最大 RSS | 28,426,240 B | 14,696,448 B |
| 自检墙钟时间 | 0.82 s | 0.78 s |

这些是一次性 PoC 数值，不等于常驻 daemon 的空闲资源数据。发布预算仍需在真实 daemon 上测量。

## 关键发现

1. `go-libp2p` 的功能完整且连接恢复自然，但即使裁剪为 TCP + Noise + yamux + mDNS + 私网 PSK，依赖图和产物仍明显偏大，并把构建基线提高到 Go 1.25.7。
2. libp2p 私网 PSK 使用 `pnet`；上游存在未关闭的弃用提案，因此不宜把它作为 RA2A 第一版长期安全基线。
3. 原始 DTLS 只解决安全数据报。若 RA2A 自行补可靠请求、确认、重传和分片，会形成自定义网络协议，因此不采用“裸 DTLS + 自定义 JSON 数据报”。
4. CoAP 已提供标准请求响应、Confirmable Message 和 Block-Wise Transfer；与 DTLS-PSK 组合后，可以继续保留 `GET /v1/sessions`、`POST /v1/messages` 的极简资源模型。go-coap 上游还包含“重传不重复执行 handler”的去重测试，并按 Message ID 缓存响应。
5. Pion DTLS 早期版本存在 GCM nonce 安全公告；本实验锁定已修复的 `v3.1.8`，并使用 `TLS_PSK_WITH_AES_128_GCM_SHA256`。

## 当前推荐

第一版优先采用：

```text
libp2p/zeroconf v2.2.0
        +
plgd-dev/go-coap v3.5.4
        +
Pion DTLS v3.1.8（PSK）
```

RA2A 只定义两个 CoAP 资源的 JSON 业务内容和错误码，不实现密码算法、可靠传输、分片协议或通用 P2P 网络栈。

本实验使用固定 32 字节 PSK 验证传输，没有自行决定 6 位 PIN 到 PSK 的映射方式。后续只能选用成熟 KDF 实现并单独验证；无论如何，6 位 PIN 仍然只有低强度信任门槛，不能抵抗有意的字典攻击，成熟协议也不会提高 PIN 本身的熵。

## 尚未证明

- 两台真实设备跨 Wi-Fi/有线网卡互相发现和往返消息。
- 网络接口关闭再开启、DHCP 地址变化后的持续发现恢复。
- 两台真实设备丢包条件下 CoAP Confirmable Message 的实际延迟；上游去重测试已通过，但 RA2A 尚未做端到端故障注入。
- Windows 防火墙和 Linux 发行版上的 mDNS/DTLS 实机行为。
- 长期常驻后的空闲 RSS、CPU、句柄和 goroutine 稳定性。

## Product Decision Delta

本阶段没有改变用户可见产品语义。节点仍然只有“列出 session”和“投递消息”两个接口；变化仅是工程实现从候选 HTTP/自定义加密封装收敛为成熟 CoAP-over-DTLS 协议栈，分类为 `engineering-only`。CoAP 对同一 Confirmable Message ID 的传输重发由接收库去重，不得被实现成新的应用层 `POST`，从而继续遵守 PD13 的“结果未知不自动再次投递”。

## 上游依据

- [go-libp2p](https://github.com/libp2p/go-libp2p)
- [libp2p zeroconf v2](https://github.com/libp2p/zeroconf)
- [go-coap](https://github.com/plgd-dev/go-coap)
- [go-coap 重传去重测试](https://github.com/plgd-dev/go-coap/blob/v3.5.4/udp/client/conn_test.go)
- [Pion DTLS](https://github.com/pion/dtls)
- [Pion DTLS GCM nonce 安全公告](https://github.com/pion/dtls/security/advisories/GHSA-9f3f-wv7r-qc8r)
- [libp2p pnet 弃用提案](https://github.com/libp2p/specs/issues/489)
