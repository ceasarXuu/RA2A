# PRD：RA2A 命令行引导、服务生命周期与 GitHub Release

- 状态：Ready for implementation
- 创建日期：2026-09-01
- 更新日期：2026-09-01
- 发起人：用户
- 原始诉求：用 `ra2a` 完成一次性交互式初始化，并提供配置、重启、版本、自更新和 GitHub Release 发版能力。
- 产品权威：`Confirmed Product Decisions` 章节

## 请求方评审摘要

- 首次执行 `ra2a` 时完成名称输入、PIN 生成、MCP 注册、用户级后台服务安装与启动，然后明确报告运行状态并退出终端。
- 后续执行 `ra2a` 只确保服务运行并显示状态，不重复初始化或改变 PIN。
- 提供 `restart`、`pin`、`name`、`version`、`update` 子命令。
- `update` 从 GitHub Releases 获取对应平台的预编译产物；GitHub Release 是正式发版主流程。
- 当前产品版本为 `v0.0.3`。
- 阻塞产品决策已经确认，本文可直接进入实现。

## 1. 背景与目标

现有安装脚本同时承担构建、配置、MCP 注册和后台服务安装，参数较长，也无法通过已安装的 `ra2a` 命令修改配置或更新版本。目标是让安装脚本退回到“放置可执行文件”，由单一 `ra2a` 命令管理当前用户的配置与服务生命周期。

## 2. 范围

### 包含

- macOS、Linux、Windows 一致的 CLI 语义。
- 首次交互式引导和后续幂等启动。
- 名称、长期 6 位 PIN 的持久化修改。
- 用户级后台服务和 Codex MCP 注册。
- GitHub Release 预编译产物、校验文件、自动发布和客户端原地更新。
- 安装脚本保留无交互参数入口，供 Agent 和自动化部署使用。

### 不包含

- 自动更新后台任务或强制更新。
- 发布通道、降级、夜间版或增量补丁。
- PIN 安全模型升级或设备配对协议。

## 3. 用户旅程

### 首次运行

1. 用户先通过平台安装脚本把 `ra2a` 放入用户 PATH。
2. 用户执行 `ra2a`。
3. CLI 请求设备显示名称，直接回车使用主机名。
4. CLI 自动生成长期 6 位 PIN，保存配置并明确显示 PIN。
5. CLI 注册 Codex MCP，安装并启动当前用户后台服务。
6. CLI确认服务正常运行后退出，不继续占用终端。

### 后续运行与管理

- `ra2a`：确保服务运行，显示名称、版本和运行状态后退出；不重新生成 PIN。
- `ra2a restart`：重启后台服务，确认运行后退出。
- `ra2a pin [PIN]`：未给参数时交互输入，给参数时无交互设置；校验后保存并重启。
- `ra2a name [NAME]`：未给参数时交互输入，给参数时无交互设置；保存并重启。
- `ra2a version`：输出 `v0.0.3`。
- `ra2a update`：检查 GitHub 最新正式 Release；校验并替换当前平台可执行文件，重启服务，报告结果。

## 4. 规则与异常

- 配置仅属于当前操作系统用户；升级和重启不得改变名称、节点 ID 或 PIN。
- 首次引导失败必须返回非零退出码并说明恢复方式；不得留下“显示成功但服务未运行”的状态。
- PIN 必须是 6 位字母或数字；交互输入无效时明确拒绝。
- `name` 不能为空。
- `update` 必须校验发布资产的 SHA-256；下载、校验或替换失败时保留旧版本可用。
- 没有更新时明确报告当前已是最新版。
- 安装脚本传入完整参数时仍可无交互完成配置和启动，满足 Agent 部署。

## 5. GitHub Release 发版规则

- 正式版本以 `v*` Git tag 触发。
- tag 必须与程序内版本完全一致，否则发布失败。
- 每个 Release 至少包含 macOS、Linux、Windows 的 amd64 与 arm64 预编译文件，以及 SHA-256 校验文件。
- Release 由 GitHub Actions 构建并发布，不接受需要开发者手工拼装的正式产物。
- `ra2a update` 只消费非草稿、非预发布的 latest Release。

## 6. 验收标准

1. Given 尚未配置，when 用户执行 `ra2a` 并输入名称，then CLI 生成并显示 6 位 PIN、启动后台服务、报告正常运行并在有限时间内退出。
2. Given 已完成配置，when 再次执行 `ra2a`，then 原名称和 PIN 不变，服务运行并立即退出。
3. Given 服务正在运行，when 执行 `ra2a restart`，then 服务进程被重启且 CLI 确认新服务可用后退出。
4. Given 有效的新 PIN 或名称，when 执行对应子命令，then 配置持久化并在重启后生效。
5. Given 用户执行 `ra2a version`，then 只输出 `v0.0.3`。
6. Given GitHub 有更高正式版本，when 执行 `ra2a update`，then 下载匹配平台的预编译资产、通过 SHA-256 校验、保留原配置并完成服务重启。
7. Given 下载、校验或替换失败，then 旧可执行文件仍可运行，并返回明确错误。
8. Given 推送与程序版本一致的 `v*` tag，then GitHub 自动发布六个平台架构产物及校验文件。
9. Given 安装脚本收到完整参数，then 可无交互安装、配置并启动；未收到参数时只安装命令并提示执行 `ra2a`。

## Confirmed Product Decisions

> PROTECTED USER-AUTHORITY SECTION
> 本节中的行，未经用户对具体决策变更的明确批准，不得创建、修改、删除、重新解释或替代。Agent 不得自行批准。

| ID | Confirmed Decision | Must Do | Must Not Do | Rationale | Violation Signal | Confirmation | Status |
|---|---|---|---|---|---|---|---|
| PD19 | 首次执行 `ra2a` 完成一次性交互引导 | 询问名称、生成并显示 PIN、注册并启动后台服务、确认运行后退出 | 不得让 daemon 前台占用当前终端，也不得在后续运行重复生成 PIN | 降低首次部署认知和命令成本 | 首次运行后终端仍被占用，或第二次运行改变 PIN | user-confirmed-direct: 对推荐首次运行流程回复“都确认” | active |
| PD20 | 提供独立生命周期与配置命令 | 实现 `restart`、`pin`、`name`、`version`、`update` | 不得要求用户重新运行长安装命令来完成日常管理 | 让已安装产品可以自管理 | 修改名称/PIN或重启必须回到源码目录 | user-confirmed-direct: “单独支持命令 ra2a restart…ra2a pin…ra2a name…ra2a version” | active |
| PD21 | 当前版本为 `v0.0.3` | 程序与发布流程使用同一版本来源 | 不得发布与程序报告不一致的 tag | 版本是更新与发版的共同契约 | `ra2a version` 与 Release tag 不一致 | user-confirmed-direct: “当前版本号为 v0.0.3” | active |
| PD22 | `ra2a update` 使用 GitHub Releases 预编译包 | 按平台下载并校验正式 Release 资产后更新 | 不得要求已安装用户保留 Go 或源码仓库才能更新 | 降低用户更新依赖和失败面 | `update` 实际执行 git pull 或本机构建 | user-confirmed-direct: 对 GitHub Releases 预编译更新建议回复“都确认” | active |
| PD23 | GitHub Release 是正式发版主流程 | tag 触发三平台双架构构建、校验与发布 | 不得依赖手工拼装正式 Release | 保证发布可重复且与自更新契约一致 | Release 缺平台资产、校验或版本门禁 | user-confirmed-direct: “把发版流程也管理一下，目前主要通过 github 发布 release” | active |

## 7. 开放问题与风险

- Windows 正在运行的可执行文件不能直接覆盖；更新器需在当前进程退出后完成替换和服务重启。
- GitHub 不可达时更新失败，但不得影响现有 daemon 和配置。
