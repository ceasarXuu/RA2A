# Linux 矩阵沙盒：无凭据隔离预检查

2026-10-12本机实践验证，完整命令与适用边界见 [experiments/sandbox](../experiments/sandbox/README.md)。只验证基础原语；不启动Agent、不导入Auth、不消费模型，不要求Docker。

## 已证操作经验

- 先在unshare user+net外层中建立bridge，再给子进程各自net/veth。不能在宿主执行ip link创建测试网络，也不能在宿主直接启动会发布mDNS的lannode测试。
- 新user namespace中的进程可能无法readlink `/proc/1/ns/net`（host root未映射），不要为了诊断提升权限。由外层启动器记录宿主namespace，再核新net与明确选定子net；这个诊断检查不替代正式运行器隔离边界。
- bwrap最小文件系统缺passwd会让dbus-daemon报UID查询/Memory allocation failure。只在私有/etc生成当前UID/GID映射即可，不需要把宿主HOME/keyring/DBus挂入。
- 使用Xephyr displayfd动态选择空闲显示。为新显示生成独立cookie，只绑定新显示路径socket和私有cookie到bwrap；不能将宿主X0/X1、全X目录或正式Xauthority交给沙盒客户端。精确列socket名单并test -S，随后私有D-Bus里运行xdpyinfo验证实际连接。
- wait自己的client/server，保存signal/exit原值；只移除自建cookie。namespace和veth应随所有自有进程退出回收，终点在宿主确认没有测试bridge/临时显示socket。异常时保留原始错误与资源清单，不按名称批量kill。

以上通过不证明Electron/App单实例、Secret Service/token复用、真实TUI显示或额度账本可用。不得因隔离探针成功直接加载正式认证或将当前所有权根治标为通过。
