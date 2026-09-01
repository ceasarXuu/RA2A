# 局域网发现生命周期

RA2A 使用 DNS-SD 发现节点，使用 DTLS/CoAP 按消息建立连接。发现记录不是永久节点目录，也不应通过长期持有 DTLS 连接表达在线状态。

## 当前机制

- 发现实现固定为 `github.com/betamos/zeroconf v0.1.7`。
- peer 记录使用 30 秒有效期；实现按 RFC 6762 在有效期的 80%–95% 主动查询刷新。
- `Added` 和 `Updated` 更新节点 ID、名称与端点。
- `Removed`、goodbye 或记录过期会立即删除 peer，避免向 ghost endpoint 拨号。
- 每分钟调用一次 `Reload`，重新加载网络接口并重置查询退避，覆盖 Wi-Fi 切换和系统唤醒。
- DTLS 握手在投递前完成；握手失败不会自动重复投递消息。重新发现后只安全重试握手前失败。
- 刷新允许返回相同 IP/端口，因为服务可能在原端点恢复；短暂等待窗口优先接收端点更新。

## 依赖升级注意事项

不要使用：

```text
go get github.com/betamos/zeroconf@latest
```

该仓库历史上的 `v1.0.0` 标签声明的是旧模块路径 `github.com/grandcat/zeroconf`。RA2A 应显式使用：

```text
go get github.com/betamos/zeroconf@v0.1.7
```

升级前必须验证模块路径、`OpRemoved`、TTL live-check、goodbye、`Reload`，并在 macOS、Linux、Windows 重新执行双节点发现、退出淘汰和同端点恢复测试。

## 故障判断

- DNS-SD 当前无节点且 RA2A 列表仍有节点：检查 remove/expiry 事件是否正常消费。
- DNS-SD 有节点但 DTLS 超时：再检查 PIN、防火墙和远端监听状态。
- 休眠或切换网络后无法发现：确认定时 `Reload` 仍在运行，并检查系统是否允许 mDNS 多播。
