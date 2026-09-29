# Pi / oh-my-pi(omp) CLI 安装记录

时间：2026-09-29，环境：Ubuntu + nvm node v22.14.0 + bun 1.3.12。

## 结论

| 工具 | 命令 | 安装方式 | 落地位置 | 配置目录 |
| --- | --- | --- | --- | --- |
| Pi Coding Agent 0.87.1 | `pi` | `npm i -g --ignore-scripts @earendil-works/pi-coding-agent@latest` | `~/.nvm/versions/node/v22.14.0/bin/pi` | `~/.pi/agent` |
| oh-my-pi 18.4.3 | `omp` | `curl -fsSL https://omp.sh/install \| sh --binary` | `~/.local/bin/omp` | `~/.omp/agent` |

包名注意：上游已迁移到 `@earendil-works/pi-coding-agent`（0.87.x），旧的
`@mariozechner/pi-coding-agent` 停在 0.73.x，不要再装。

## 踩坑记录

### 1. 裸装 pi 会静默降级到 0.74.2

`npm i -g @earendil-works/pi-coding-agent`（不写版本号）装出来的是 **0.74.2**，
正好等于 registry 上的 `legacy-node20` dist-tag；本地 node 22.14.0 低于 0.87.1 要求的
`node >= 22.19.0`，npm 解析时回落到了旧 dist-tag，且只有 `EBADENGINE` 警告。

规避：显式写 `@latest`。实测 0.87.1 在 node 22.14.0 上 `pi --version` / `pi --help`
都能正常起来，引擎声明偏保守，可以先不升级 node；若后续出现运行期异常，
再 `nvm install 22.19+` 并重装该包（注意切换 node 会让旧 node 目录下的全局包
从 PATH 消失，需要一并重装）。

### 2. `curl https://omp.sh/install | sh` 会因 bun 版本过低直接失败

安装脚本默认逻辑是「本机有 bun 且架构匹配 → 走 bun 源码安装」，于是先做
bun 版本检查。本机 bun 1.3.12 < 要求的 1.3.14，脚本直接退出：

```
Bun 1.3.14 or newer is required. Current version: 1.3.12
```

并不会自动回落到预编译二进制。规避：加 `--binary` 强制走 GitHub Releases 预编译包
（`omp-linux-x64`，落到 `~/.local/bin/omp`，脚本自带 `omp --version` 冒烟校验）。
`--source` 才是源码/bun 安装模式。

### 3. 一次性验证命令

```bash
pi --version                 # 0.87.1
pi --help                    # CLI 完整加载通过
pi auth                      # 子命令可用，会初始化 ~/.pi/agent/auth.json
omp --version                # omp/18.4.3
omp config path              # /home/zhangxu/.omp/agent
```

## 后续可做

- 补 shell 补全：`eval "$(omp completions bash)"` 写进 `~/.bashrc`。
- 用 `rules-install` 技能把全局 AGENTS.md 同步进 `~/.pi/agent/AGENTS.md` 与
  `~/.omp/agent/AGENTS.md`。
- pi 想跑后台 subagent 必须保留 npm 包目录（独立二进制没有 package 目录），
  这也是本次 pi 选 npm 全局安装的原因。
