# v0.0.18 发布核验（2026-10-04）

## 发布结果

| 项目 | 结果 |
| --- | --- |
| 正式版本/tag | v0.0.18，指向8db1a9480fea81bc406ded56fea61f83fce33523；没有移动既有tag |
| Release | https://github.com/ceasarXuu/RA2A/releases/tag/v0.0.18 ，Latest、非draft、非prerelease；2026-10-04T13:26:02Z发布 |
| GitHub Actions | https://github.com/ceasarXuu/RA2A/actions/runs/37205443947 ，版本匹配、全量测试/vet、六组构建及发布成功 |
| 正式资产 | 六组OS/架构×三个可执行文件×二进制/校验和=36项，加两个安装器/校验和=4项，共40项；下载名称集合精确匹配预期 |
| 下载完整性 | 下载全部40项，20组SHA256逐对通过；主程序实际version=v0.0.18 |
| 安装实测 | 使用下载的Linux/amd64正式二进制，通过未改动的Release下载器在隔离HOME安装，RA2A版本与两个native launcher版本透传通过 |
| latest链接 | Unix和Windows安装器均实测HTTP200 |

## 发布前门禁

- Go1.24.0，最终源码`go test -count=1 -json ./...`通过：22个包，363个pass测试事件、fail0、4个opt-in skip。`go vet ./...`通过。原生CLI/OpenCode的显式隔离验证及现场人工结果见对应验收记录，未把skip冒充通过。
- 按Release workflow参数构建darwin/linux/windows各amd64/arm64、RA2A/Codex wrapper/OpenCode wrapper共18项，全部成功；生成安装器与SHA256共40项。实际构建资产的隔离下载器安装测试通过。
- HOME/USERPROFILE/LOCALAPPDATA/APPDATA/CODEX_HOME/XDG/TEMP均隔离，GOPROXY=off，复用已有Go缓存；本轮未运行本机生产安装器或重启正式服务。
- 首次全量测试原始FAIL保留：隔离TEMP放在长证据路径下，Unix socket超过路径预算；改为短随机`/tmp/r18-*`，保持全部profile隔离。另一PowerShell安装器夹具继承外部CODEX_HOME，却将standalone创建在夹具HOME下；只让该测试显式设置自身CODEX_HOME，不修改生产安装逻辑。修正后全量测试和vet通过。
- 本机官方daemon15822持续存活。旧保护基线中config.toml的变化时间为2026-10-04T13:09:06.801241Z，早于本轮发布测试13:20:47Z，保留该既有配置，没有覆盖/恢复；后续校验时该mtime仍相同。其他旧基线保护文件元数据未变，不将旧基线误当本轮开始时的配置快照。

## 来源与证据

- 原始日志与资产保存于忽略目录`.cache/release-v0.0.18/`：首次tests.stdout、最终tests-final.stdout/vet-final.stdout、validation-final.json、build.json、local-assets-smoke.log、ci.json、release.json、download-checksums.json、published-assets-smoke.log及下载资产。
- 发布Linux/amd64主程序buildinfo中的vcs.revision为上述tag提交，vcs.modified=true，原始信息保留在published-buildinfo.txt。Workflow会在未忽略的dist目录生成构建资产，因此该标志不单独用于声明源码清洁；来源依据为tag、CI checkout/执行记录与正式下载SHA256绑定。
- 不更改tag以包含事后文档提交；main上的发布记录可继续补充。

## 升级与支持边界

- 标准Release安装器同时更新core与两个wrapper；OpenCode动态租约修复要求core与OpenCode wrapper一起更新。已有TUI由用户主动重开/resume；正式配置、认证、模型和代理保持各自原有设置。
- CLI↔CLI人工验收通过，ROG OpenCode重开/resume后原地址恢复、消息/回复可见且能继续输入已获Owner确认。
- CLI/OpenCode仍为预览；未测全App交叉方向、物理断网/真实OpenCode server崩溃恢复等边界与平台skip保留。发布为修复版，不扩大为全部宿主/平台正式支持。
- [CLI验收](../docs/v0.0.18/codex-cli-full-acceptance.md)、[CLI/OpenCode验收](../docs/v0.0.18/codex-cli-opencode-acceptance.md)、[发布说明](../releases/v0.0.18.md)。
