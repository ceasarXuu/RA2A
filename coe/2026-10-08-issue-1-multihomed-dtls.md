# Problem P-001

- 状态：诊断完成，未修复；纳入 v0.0.19。
- 来源：https://github.com/ceasarXuu/RA2A/issues/1。
- 症状：同子网双网卡 Mac 对 Wi-Fi 地址的 DTLS 请求从 Ethernet 回复，connected UDP 客户端握手超时。
- 基准：v0.0.18 与 origin/main e891277；本轮未改正式配置、网络或生产代码。
- 已知事实：issue 的地址已匿名化；探针由 issue 作者记录，本会话没有重复执行双机探针。
- 修复标准：发布地址回复源 IP 与目标 IP 一致；跨平台、loopback、接口变化及授权双向消息通过。

## Hypothesis H-001

- 状态：confirmed。
- 主张：wildcard listener 丢失本地目标 IP，WriteTo 依回程路由选择另一源 IP，connected UDP 对端过滤该回复。
- 预测：依赖读写路径不保存/指定本地目标 IP；issue 的未连接 UDP 探针可收到不同源 IP 的回复。
- 诊断证据计划：主路径对照正式版 listener/client；独立子 agent 追踪锁定依赖 socket 行为，对照 issue 双地址探针。若依赖已经保留目标 IP 并指定源地址则反驳。
- 证据：E-001、E-002。

## Evidence E-001

- 对应：H-001 探针预测；类型：issue 转述的用户反馈，非本轮新实测。
- 结果：目标 Ethernet → Ethernet 回包；目标 Wi-Fi → Ethernet 回包，两次 48 字节 HelloVerifyRequest。
- 含义：直接支持源地址偏移，不能仅用 PIN 或超时解释。

## Evidence E-002

- 对应：H-001 源码预测；类型：独立依赖路径调查。
- 结果：node.go:146 绑定 0.0.0.0:0，:389 connected UDP；go-coap v3.5.4 net/dtlslistener.go:38 → Pion dtls v3.1.8 listener.go:56 → internal/net/udp/packet_conn.go:177 ListenPacket、:227 ReadFrom、:361 WriteTo。不保存本地目标 IP，不指定回复源地址。正式版和 main 未改变该路径。
- 含义：源码与 E-001 相互支持；尚无修复后证据。

## Evidence E-003

- 对应：H-001 后续修复可行性；类型：依赖源码事实。
- 结果：x/net v0.49.0 在 Darwin/Linux 支持 pktinfo；ipv4/control_windows.go:9 返回 errNotImplemented。Pion WithListenConfig 改变 socket 配置但不改变上述读写。连接路由还需考虑同一远端访问两个本地地址。
- 含义：packet-info 不能直接作为三平台统一方案；按地址绑定 listener 候选需验证共用端口和广告生命周期。详细验收见 docs/v0.0.19/issue-triage.md。
