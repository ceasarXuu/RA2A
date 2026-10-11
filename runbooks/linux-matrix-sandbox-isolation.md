# Linux 矩阵沙盒：无凭据隔离预检查

2026-10-12本机实践验证，完整命令与适用边界见 [experiments/sandbox](../experiments/sandbox/README.md)。默认验证基础原语；可选观察真实 App 的无凭据启动门禁，不导入 Auth、不消费模型，不要求 Docker。

## 已证操作经验

- 先在unshare user+net外层中建立bridge，再给子进程各自net/veth。不能在宿主执行ip link创建测试网络，也不能在宿主直接启动会发布mDNS的lannode测试。
- 新user namespace中的进程可能无法readlink `/proc/1/ns/net`（host root未映射），不要为了诊断提升权限。由外层启动器记录宿主namespace，再核新net与明确选定子net；这个诊断检查不替代正式运行器隔离边界。
- bwrap最小文件系统缺passwd会让dbus-daemon报UID查询/Memory allocation failure。只在私有/etc生成当前UID/GID映射即可，不需要把宿主HOME/keyring/DBus挂入。
- 使用Xephyr displayfd动态选择空闲显示。为新显示生成独立cookie，只绑定新显示路径socket和私有cookie到bwrap；不能将宿主X0/X1、全X目录或正式Xauthority交给沙盒客户端。精确列socket名单并test -S，随后私有D-Bus里运行xdpyinfo验证实际连接。
- wait自己的client/server，保存signal/exit原值；只移除自建cookie。namespace和veth应随所有自有进程退出回收，终点在宿主确认没有测试bridge/临时显示socket。异常时保留原始错误与资源清单，不按名称批量kill。

以上通过不证明Electron/App单实例、Secret Service/token复用、真实TUI显示或额度账本可用。不得因隔离探针成功直接加载正式认证或将当前所有权根治标为通过。

## 真实 App 无凭据检查

- 独立 IPC 的 bwrap 客户端不能可靠使用 Xephyr 的 MIT-SHM 共享内存扩展；本机 gcr-prompter 实际报 BadValue 后，在 Xephyr 端 `-extension MIT-SHM` 消除该错误，无需共享宿主 IPC 或关闭 Electron sandbox。
- App 存活、窗口存在、backend/IPC 就绪是三项不同证据。无网络启动实际停在组织设置加载错误，不能把该窗口计入完整 UI 验收。
- 截图工具 `/usr/bin/import` 通过未挂载的 `/etc/alternatives` 解析，在最小文件系统不可用。本机使用实际映像 `/usr/bin/import-im6.q16`；不要为此把整个 `/etc` 挂入。
- `preflight.py --output <新目录> --app` 将独立检查与报告汇总为一条命令。已有证据目录拒绝覆盖；真实额度保持0，完整矩阵状态保持 NOT_RUN。

## 真实套件的已证边界

- 同类node/原生TUI必须共用PID身份空间；各类分别独立PID空间。budget worker与PID1设置dumpable0，Agent不给SYS_PTRACE；通过真实/proc拒绝测试确认预算socket不能经worker root越界访问。
- 只挂单个Codex binary会使daemon start报没有完整local package；应挂固定releases/<version>完整官方包，校验marker/白名单与无Auth/逃逸symlink。不能上扩到正式.codex。
- PTY固定30×120，回复DSR/DA/OSC10/11终端查询不属于模型输入。模型bootstrap须单独预留额度。
- Codex final必须匹配原生turn并且phase=final_answer/task_complete；Pi按parent链+stop关联；OpenCode按原生parentID/completed关联。commentary、其他turn结束、地址正文引用均不当完成证据。
- 私有controller监督outer PID1，超时整组TERM→KILL→wait；真实测试覆盖内部setsid后代随PIDnamespace消失。清理失败保留证据和凭据恢复路径，报告不完整，不按名称杀正式进程。

- 官方Codex0.162.1 control socket可能是指向`/tmp/codex-daemon-<uid>/...`的symlink。同kind各进程必须共享**本轮私有tmp**；各进程各自tmpfs会让TUI看不到已启动daemon。只绑定该kind新建tmp，不能共享宿主/tmp或其他kind。
- 官方TUI将paste burst期间Enter视为换行。预算先reserve，文本仅写一次，等待0.5秒再单独Enter；本机真实诊断已生成session/task_started，不以此代替最终模型完成。固定来源：`rust-v0.162.1/codex-rs/tui/src/bottom_pane/paste_burst.rs`。
- 关闭恢复必须先观察旧attachment从发现结果撤回，再接受相同原生ID的新端点；sessionsStale=true不算ready。超时止步，不能靠历史ID冒充新TUI所有权。

- control.Coordinator.Send跨节点重新生成message-id；调用方自填ID不等于线上ID。测试正文首token放唯一R2S marker，按完整source+首token关联并保存真实envelope ID；不要用引用ACK文本的substring匹配。ACK损失钩子只消费一次首token，已通过真实Pi接收唯一完成且发送UNKNOWN验证。

## 失败分类与恢复门禁（2026-10-12已验证）

- App桥仅用于新私有App的`CODEX_CLI_PATH`，每个wrapper绑定自己新spawn的native；真实App可能启动多个wrapper，不能固定一个全局backend。保护worker的`/sandbox/identity`返回共享PID命名空间中的PID与创建ticks，不能用宿主启动器PID代替。私有socket可见性不代表授权，桥逐请求核对SO_PEERCRED和创建ticks；投递仍须经过worker预算包装。仅该安全模块通过不代表运行器GUI矩阵接入完成。

- OpenCode模型异常应保存固定类型、HTTP状态、是否可重试及白名单类别；不保留错误原文、响应headers/body或thinking。收件成功但模型被拒绝，应分别报告，禁止以更换模型或自动重试填PASS。本机唯一输入确证`opencode-go/space-bunny-free`返回HTTP400/MODEL_NOT_AVAILABLE；模型可用性需要Owner选择。
- 官方Codex0.162.1默认`thread_unload_delay_secs=60`。隔离对照证明断开创建者后约60秒仍持真实writer，配置1秒后约1秒释放；实际RA2A start/steer不使其永久订阅。关闭恢复等待须从本轮私有配置派生，继续观察端点撤回；180秒上限内未撤回报告`NATIVE_OWNER_STILL_HELD`，禁止强制注销、unsubscribe或停daemon来制造睡眠。Pi/OpenCode租约型端仍使用原短观察期。

## 私有App界面驱动的已证边界

- 本机bwrap0.9.0不支持preserve-fds；CDP使用标准输入/输出穿过内部launcher，再映射真实App fd3/4。不启动debug TCP端口。独立D-Bus的地址走另一条pipe、服务stdout/stderr走私有日志，不能污染CDP framing。
- 额外user namespace和cap-drop ALL不足：实际同节点Agent仍可重开App fd3/4。PR_SET_DUMPABLE0若在ELF constructor立即执行，真实Electron报No usable sandbox。因此采用renderer body初始化后，可信controller触发只保护browser的guard，再逐次实际验证同节点Agent两FD均PermissionError；没有ACK/拒绝证据则不发GUI模型输入。正式App不加载此guard，真实Electron sandbox保持开启。
- 模型控制指令与基础矩阵格共享一次接收输入；工具业务回信独立核验、独立占接收Agent额度。额度按kind合并所有会话，不能用新session规避10条。人工阶段达到上限时明确不执行，不能为补人工验证自动开新轮。

- dumpable保护不等于整个proc/root文件系统封闭。实际Agent可经App main root读取X11 cookie并认证，故每App只挂新客户端auth副本；界面连接后controller清空该副本inode。显示服务器原auth必须保留且不挂给App，清空它会让服务器重新允许无cookie，已有失败诊断已证。修正后实际无cookieX11 setup首字节0拒绝，原界面和从新副本重开的界面均可用。

## 真实客户端的初始化与完成语义

- Linux Desktop实际启动多个backend，其中部分成功initialize之后不会发initialized通知。stdio桥应以原App成功initialize响应确认就绪；不得补发二次初始化。失败响应保持拒绝。真实App0.160.0无通知native对照和GUI收件/关闭撤回已通过，不能根据首个backend行为假定全部一致。
- OpenCode1.18.34忙时追加输入后，后续原工作工具和final的parentID可能切到新用户输入。保留两个原生user和四次工具的实际关联；只有唯一final精确双ACK才记一个组合完成，不伪造BASE独立final、不借用Codex同turn概念。单ACK、无关parent或重复输入保持不通过。
- 共享CODEX_HOME的新CLI启动前先记录已有rollout；新启动注册只接受后来新增的会话，避免App历史被误认。native source=vscode不能替代实际终端归属证明。
