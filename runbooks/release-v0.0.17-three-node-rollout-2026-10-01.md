# v0.0.17 正式版三节点升级记录（2026-10-01）

发布核验与历史失败标签见 [release-v0.0.17-evidence.md](release-v0.0.17-evidence.md)。本文件仅记录正式 Release 在三台正在使用的节点上的安装及**运行进程**证据；不将磁盘版本或投递受理当作部署完成。

## ubuntu407（本机）

- 使用已从 Release 下载、且 `sha256sum --check` 通过的 `install-ra2a.sh` 执行 `RA2A_VERSION=v0.0.17 sh install-ra2a.sh`；安装器自动检测 Codex CLI（`~/.local/bin/codex.bin`）和 OpenCode（`~/.opencode/bin/opencode`），安装两个 launcher 后自动重启 daemon。没有使用旧版 `ra2a update`。
- daemon 旧 PID `2748315` → 新 PID `2794403`，`systemctl --user show` 为 active。`/proc/2794403/exe` 与 `~/.local/bin/ra2a` 的 SHA256 同为 `68be60ef11a4fab34b75ef85faa840483372474a18ccd6a51e362e896f6f3e4e`，与 GitHub Release 文件一致；实际运行映像 `vcs.revision=7e3fd551d18b3e8727240b4fa306e4975d9b716b`，`ra2a version=v0.0.17`。
- Codex wrapper `b391d52e628647672373cc2777f073355131b3048b7ed202f5f783590280bc05`；OpenCode wrapper `2c4b8e4dcb9846ce374bb423ad3ae1af221296d9b7fac8c367a83e2fbf47b529`；原有自定义 Codex 启动脚本的 `codex.bin` SHA256 `54b67dadd227bddb30fd6414aacb4b4ed688a6e7ffe29d82655d924b758b20bf` 未改变。原生命令透传 `codex-cli 0.155.1`、`opencode 1.18.33`。
- `config.json` 保留 Codex/OpenCode 路径、OpenCode MCP 注册有效；三节点均可发现，本机 OpenCode 活跃会话仍发布。当前 TUI 不被安装器强制关闭；用户下次主动重开才能加载新 wrapper/MCP 工具定义。

## rog306（Windows，目标会话回报）

- 目标会话：`ra2a://rog306/01a05d7e-b958-7df2-bd67-6eba9636fad6`（“测试session消息注入”）；回报标记 `ROG306-RELEASE-v0.0.17-DONE`。
- GitHub Release `install-ra2a.ps1` 和 `.sha256` 校验通过（脚本 SHA256 `F11C8C6B445A725097E1776404120C4DD54CDF0C2786BFBC1874889DF3CB781B`），以 `-Version v0.0.17` 执行下载的脚本；自动检测 Codex Desktop bundled native CLI 与 OpenCode npm `.cmd`，并安装三种已校验的资产。
- daemon 旧 PID `35296` 已退出 → 新 PID `64072`；实际运行 exe 的 `vcs.revision=7e3fd55`、`version=v0.0.17`。对端报告已安装文件与 Release 的 `.sha256` 均匹配：RA2A `D84F84AEC8E9190252205FD0E215B750022849DFBDF9172A8BC111CDF55ADFF4`，Codex wrapper `AFDD012B3C133E333CFDB571A643437ABF58B0BF96289E06239C4BEC4CDF20CD`，OpenCode wrapper `4C870308A61A852FADB134DE03E03B4A6350068F403D43CCCDEA556384E1C71D`。
- 本端再次确认 rog306 `ready`、`sessionsStale=false`；Codex/OpenCode 原生命令透传分别为 `0.159.0`、`1.18.33`；未强制重启旧 TUI，本地仓库原有 `.commandcode/` 未触碰。

## macmini-m4（macOS，目标会话回报）

- 目标会话：`ra2a://macmini-m4/01a0eebf-9e9b-7f61-bb50-df18279b9085`（“配置联调专用 session”）。下载 GitHub Release 的 `install-ra2a.sh` 与 `.sha256`，`shasum -a 256 -c` 输出 OK，执行 `RA2A_VERSION=v0.0.17 sh install-ra2a.sh`，没有使用源码仓库，因此保留当地分叉 `main`（`ffb52fd`）及未跟踪文件。
- 安装器检测 Codex CLI（`codex.bin`）与 Homebrew OpenCode，自动安装并校验两种 wrapper，只重启 RA2A daemon。旧 PID `38770` → 新 PID `46633`；远端核对 `lsof` 运行映像 inode `34820707` 与安装文件一致，`ra2a version=v0.0.17`，实际 `vcs.revision=7e3fd55`。
- 三种已安装资产均与 Release `.sha256` 一致：RA2A `c275e47f1ba34215923a3e04968a5ead045065f466cae2e31413c5411092c747`，Codex wrapper `40ac0cd41c66e6cbfe78605f4fc8eb78bf0df799eaf73d697efd916a62eece54`，OpenCode wrapper `eb8a8bf92880005d4c7802bf639fe3ad7ff6c689db8970ebcc2e577ff4afc249`。节点 `ready`、`sessionsStale=false`、30 个会话；原生/包装器版本均为 Codex `0.155.1`、OpenCode `1.18.33`。旧 TUI 未被强制重启。

## 发布操作经验

1. Release CI 在构建资产前创建 `dist/`，因此发布资产的 Go 构建信息显示 `vcs.modified=true`；三机使用**发布资产 SHA256 + tag 提交的 `vcs.revision` + 实际运行 PID/映像**共同确认，不以该布尔值单独判定源码被修改。
2. `ra2a update` 的旧版只能替换 RA2A 主二进制。从 v0.0.15 转正式 v0.0.17，应重跑发布安装器一次以同步 Codex/OpenCode wrapper；无需源码、Go 或额外 wrapper 标志。
3. macmini-m4 本地源码分叉不阻止发布资产安装；不用强行快进或重置仓库。正在工作的 Codex/OpenCode TUI 不属于 daemon 重启范围，后续由用户主动重开。
