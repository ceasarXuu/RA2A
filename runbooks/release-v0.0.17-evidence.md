# v0.0.17 发布核验（2026-10-01）

## 发布结果

| 项 | 证据 |
| --- | --- |
| 正式版本 | `v0.0.17`，tag 指向 `7e3fd551d18b3e8727240b4fa306e4975d9b716b` |
| Release | https://github.com/ceasarXuu/RA2A/releases/tag/v0.0.17 ，`Latest`、非 draft、非 prerelease |
| GitHub Actions | https://github.com/ceasarXuu/RA2A/actions/runs/36753459866 ，验证 tag、全量测试、构建资产、发布四步均成功 |
| 资产 | 6 个 OS/架构目标 × RA2A/Codex wrapper/OpenCode wrapper × 二进制及 SHA256 = 36 项；Unix/Windows 安装器及 SHA256 = 4 项；合计 40 项 |
| latest 安装链接 | Unix：`https://github.com/ceasarXuu/RA2A/releases/latest/download/install-ra2a.sh`；Windows：`https://github.com/ceasarXuu/RA2A/releases/latest/download/install-ra2a.ps1`；两条链接实测 HTTP 200 |

## 测试和资产实测

- 本机 Go 1.24 执行 `go test -count=1 ./...`、针对安装器和相关适配器的 `go test -race -count=1`、`go vet ./...` 均通过；在**没有 OpenCode 的隔离 PATH** 中重跑全量测试也通过。
- 按 Release workflow 同一方式交叉构建 6 组目标、每组 3 个可执行文件，18 个构建均成功。隔离的原生 OpenCode 1.18.33 权限 ask→reply 测试通过。
- 从 GitHub Release 下载 Linux/amd64 的三种可执行文件、安装器及各自的 `.sha256`，四份 `sha256sum --check` 均通过。RA2A 实际发布二进制执行 `version` 输出 `v0.0.17`。
- 使用**下载的 GitHub Release 二进制**而非本机 dev 构建，让未改动的正式 Unix 下载器从本地 HTTP/动态 SHA256 服务器安装到隔离 HOME；自动安装 Codex 和 OpenCode 两种 launcher，`ra2a version` 与两种原生命令的版本透传均通过。

| Linux/amd64 下载资产 | SHA256 |
| --- | --- |
| `ra2a-v0.0.17-linux-amd64` | `68be60ef11a4fab34b75ef85faa840483372474a18ccd6a51e362e896f6f3e4e` |
| `codex-wrapper-v0.0.17-linux-amd64` | `b391d52e628647672373cc2777f073355131b3048b7ed202f5f783590280bc05` |
| `opencode-wrapper-v0.0.17-linux-amd64` | `2c4b8e4dcb9846ce374bb423ad3ae1af221296d9b7fac8c367a83e2fbf47b529` |
| `install-ra2a.sh` | `252bdedff6c7a06a57d12258f7ad7743cb7529107086fd0cde00697f3498b993` |

## 未发布标签与复盘

`v0.0.16` tag 指向 `685b431`，其 Release run `36752668456` 在 `go test ./...` 阶段失败，**未生成 GitHub Release**。失败发生在 `TestNativeExecutableNeverReturnsTheWrapperItself`：测试不隔离 PATH，误假定 CI 机器预装 OpenCode；产品代码在没有 native 时返回空路径是正确行为。

修复提交 `7ce575d` 让测试使用独立 PATH，明确断言“没有原生 OpenCode 时安全失败”；随后把正式承载版本设为 `v0.0.17`。没有移动或强推已公开的 `v0.0.16` tag，也没有绕过 CI。后续针对安装、环境探测、宿主依赖的测试必须覆盖“CI 没装该宿主”的空环境，而不是只在开发机上通过。

## 升级注意

从 v0.0.15 升级需**重新执行一次 v0.0.17 正式安装器**：旧版 `ra2a update` 只会替换主程序，不会安装新 wrapper 资产。无需传 wrapper 专用参数。已经运行的 OpenCode TUI 仍由用户主动重开；OpenCode server 的 MCP 配置变更要在 server 重启后加载。
