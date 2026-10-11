# Linux 单机矩阵测试套件

不需要 Docker。基础预检查不消费额度；真实运行使用本机当前凭据的私有副本，刷新不写回正式认证。每轮每类Agent所有会话共享10条收件，启动输入、工具回信和人工输入也占额度；失败/unknown不退、不自动重放。缺凭据/版本/适配器时明确阻塞，不切换provider制造通过。

## 真实矩阵

```sh
python3 experiments/sandbox/run.py --output .cache/sandbox/matrix-new
# 人工操作留在自动阶段之后：
python3 experiments/sandbox/run.py --output .cache/sandbox/matrix-manual-new --manual
```

目录必须不存在。已有Python3/Go、unshare/nsenter/ip/bwrap以及官方CLI/OpenCode/Pi；不会安装软件或改正式服务。启动器构建本次源码的测试node/RA2A/OC wrapper，创建三类独立namespace、短私有HOME、PTY和真实LAN。退出码0仅整体PASS；失败、阻塞或依赖未运行均非0。`summary.json`、`report.md`、`ledger.jsonl`可核对，每条收件与native最终完成分别记录。

`--manual`最后逐类显示私有PTY，按控制器提示确认、提交一次固定标记、观察输入出现/回复出现/输入栏可继续，填写PASS。预算先预留，满额时解释并拒绝输入。此步骤不操作正式终端；不要绕过控制器键入模型提示。未执行则MANUAL_PENDING。

目前App安全actual-owner入口未接入，App格与holder交接为BLOCKED_ADAPTER。同类self格仅单会话回送；不表示两会话隔离。预检查可以另行观察真实App启动窗，不能把它当完整App矩阵或GUI通过。当前实际运行仍暴露模型/客户端问题，整体INCOMPLETE是有效失败报告，不能称所有Agent兼容验收通过。

## 认证与出口边界

只投影当前provider最小auth/model资料，不复制正式MCP/插件/任意命令。正式keyring不共享；不支持的认证明确BLOCKED_AUTH。节点无默认外部路由，模型HTTPS通过私有UDS+loopback broker。直接出口固定已验证公网IP；配置本机HTTP代理时只向该可信上游CONNECT白名单域名:443，由上游解析DNS，不能宣称已验证其最终IP。TLS仍由真实客户端校验。正式HOME、代理端口、预算UDS和controller不直接暴露给模型Agent。

正常结束回收namespace/PID后删除本轮凭据树。超时/中断TERM→KILL→wait；归属不完整时保留私有恢复目录，不删潜在使用中的认证。正式auth/config终点哈希检查覆盖报告列出的八文件，不是全过程监控或账户侧token影响的证明。

## 无模型测试

```sh
RA2A_TEST_LINUX_RUNTIME=1 python3 -m unittest discover -s experiments/sandbox -p 'test_*.py'
GOPROXY=off go test -race ./internal/sandbox ./cmd/ra2a-sandbox-node -count=1 -timeout=60s
```

最新真实报告matrix-006：六个基础格原生收件/final通过，CLI/Pi忙时追加、Pi恢复和ACK丢失不重放取得原生证据；CLI6/OC1/Pi7预留。OpenCode模型错误、CLI恢复未通过、App阻塞和人工未执行使整体INCOMPLETE。active汇总漏置成功状态已修正，原报告和qualification解释分开保留；运行期间源码变化也单列，不能作为最终冻结发布验收。正式认证配置终点哈希不变，自有资源与凭据副本已回收。以下旧记录为前置阶段证据。

---

# Linux 无 Docker 沙盒隔离预验证

- 日期：2026-10-12；依据 [矩阵沙盒方案](../../docs/matrix-sandbox/plan.md)。
- 仅诊断：默认不启动 Agent；`--app` 观察真实 App 无凭据、无网络启动。不导入凭据、不请求模型、不安装软件、不启动正式服务。不是付费矩阵运行器。
- 本机结果：namespace/bwrap、三节点真实LAN、嵌套显示与独立D-Bus通过；真实 App 私有窗口已观察，完整就绪和认证未验证。

## 一条命令完成预检查

```sh
python3 experiments/sandbox/preflight.py \
  --output .cache/sandbox-discovery/preflight-new --app
```

输出目录必须不存在；启动器生成 `summary.json` 与中文 `report.md`，分开列真实三节点 LAN、私有显示/DBus、App 观察。`matrixStatus=NOT_RUN`、`paidInputs=0`；App 窗口出现不等于认证成功或完整矩阵 PASS。缺依赖只报告，不自动安装。正式认证尚未导入。

## 复现

现有依赖：Go、Python3、unshare/nsenter/ip、bwrap、Xephyr、xauth、xdpyinfo、dbus-run-session；必须已有宿主图形会话和允许user namespace。本探针不自动安装。先确保工作树变化来源明确，建立私有日志目录。

```sh
mkdir -p .cache/sandbox-discovery
GOPROXY=off go build -o .cache/sandbox-discovery/netprobe ./experiments/sandbox
RA2A_DISCOVERY_HOST_NETNS="$(readlink /proc/self/ns/net)" \
  unshare --user --map-root-user --net \
  python3 experiments/sandbox/network_probe.py .cache/sandbox-discovery/netprobe \
  > .cache/sandbox-discovery/network.json
python3 experiments/sandbox/display_probe.py .cache/sandbox-discovery/display-new
```

display-new必须不存在，不覆盖旧证据。网络探针要求父进程已经位于私有user+net namespace；启动器从外部记录宿主namespace给诊断子进程，不读取正式PID的namespace句柄。内部再创建三个net namespace、私有bridge/veth、198.18.0.1..3/24；该网段仅存在于全新外层私有net，没有外网默认路由或宿主接口。不能去掉unshare直接执行脚本。

Go probe也要求当前net namespace与外部记录宿主不同且匹配明确选择；正式运行时不得使用该诊断可配置检查替代安全边界。节点用固定测试PIN T3ST42，ID sandbox-probe-1..3，不使用正式PIN。真实lannode执行mDNS发现、CoAP会话查询、DTLS消息1→2→3→1，每节点回报3个peer及一次原始接收message-id；handler只接入内存channel，不接Agent。

## 本机观察

- unshare --user/--mount/--pid/--net/--ipc均可运行；映射root仅属于新user namespace。私有net最初只有down的loopback，无宿主网卡/路由。
- bwrap清空环境并只挂运行库，创建private HOME/proc/dev/tmp/run；正式/home/zhangxu/.codex不存在。第一次D-Bus失败缺UID passwd映射，私有合成passwd/group后独立bus成功；没有挂宿主passwd/keyring或D-Bus。
- 三节点各exit0/pass；外层net:[4026533078]、宿主net:[4026531833]（一次样本，不固定ID），peer地址为198.18.0.x，互相收到唯一环形消息。无外网路由；末端所有probe/sleep子进程wait退出，namespace随退出回收，宿主没有probe-br。
- Xephyr通过displayfd分配新显示:2；只将其路径socket和一次性新显示cookie绑定进bwrap，未将整个宿主X目录/正式cookie交给隔离客户端。私有dbus-run-session内xdpyinfo通过，socket名单恰为X2，正式HOME不可见。Xephyr本身连接宿主显示，是刻意保留的显示桥；不据此保证对恶意GUI的强安全防护。
- 首个显示探针的find计数断言exit8，旧结果保留；后用精确basename名单及test -S门禁通过。未宣称该旧断言错误的具体find机制已证。
- 原始结果留在忽略目录.cache/sandbox-discovery/network.json及display-001/002/003；不提交cookie。显示探针停止并wait自己的client/server、移除仅自己生成的cookie，保留日志与清理记录。正式X0/X1仍在，临时X2不存在。

## 未证明与下一门禁

这些是隔离原语/真实网络验证，不是完整矩阵：尚未验证 TUI、真实认证、模型出口、额度账本或 actual-holder 切换。未以hash快照证明正式配置全过程不变，只证明诊断未导入/写入正式配置、未启动相关客户端。D2真实App启动与单实例安全仍需隔离运行验证；Q1/Q2未确认前不开展付费认证阶段。

下一步可沿用namespace+bwrap，不需要先引入Docker。若App实际依赖或认证不能正确复用，再据证据修改方案，不直接加--no-sandbox、host network、host DBus或宿主HOME来使测试表面通过。

## 无凭据真实 App 观察（2026-10-12）

`display_probe.py <新目录> --app` 保留 Electron sandbox，运行真实 `/usr/lib/chatgpt/ChatGPT`，只挂系统库、字体配置、os-release、新显示 socket 和私有 HOME；不挂正式 Home/DBus/keyring。`--password-store=basic` 仅用于这次无认证诊断，不能据此声称已验证真实登录兼容。新建私有 user/mount/PID/network/IPC，未提供外网路由。

- App 启动进程、zygote/GPU/utility 与新 ChatGPT 窗口被实际观察；窗口截图原文为 Organization settings could not be loaded，App 暂停等待组织设置。没有 renderer、Codex backend 或 CODEX_HOME IPC 就绪证据。因此是启动门禁观察，不是完整 App/UI PASS。
- 最初私有 gcr-prompter 报 MIT-SHM BadValue。Xephyr 与 bwrap IPC 分离；为显示桥关闭 Xephyr 的 MIT-SHM 扩展后，私有提示不再出现该错误。没有加入 `--no-sandbox` 或共享宿主 IPC。
- 最初8秒观察采用 timeout 产生124；后改为观察指定子 PID、窗口树、私有 socket 和 namespace 后停止自己的 App，分别记录观察与资源回收。不能把 timeout 124 当自然退出或把进程存活当 ready。
- 窗口截图与日志在忽略目录 app-005；私有用户数据实际落于 home/config/Codex，不能用设置了环境变量推定特定 userdata 变量已经生效。没有导入秘密；截图只包含启动错误。
- 每次 client/server 已 wait，独立 cookie 已移除；宿主 X0/X1 保留、X2 消失，没有测试进程残留。报告保留 cleanup 字段。剩余私有无认证用户数据与日志保留供核验。
