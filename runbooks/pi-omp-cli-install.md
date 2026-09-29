# Pi / oh-my-pi(omp) CLI 安装记录

时间：2026-09-29，环境：Ubuntu + nvm + bun 1.3.12。

## 结论

| 工具 | 命令 | 版本 | 安装方式 | 落地位置 | 配置目录 |
| --- | --- | --- | --- | --- | --- |
| Pi Coding Agent | `pi` | 0.87.1 | `npm i -g --ignore-scripts @earendil-works/pi-coding-agent@latest` | `~/.nvm/versions/node/<ver>/bin/pi` | `~/.pi/agent` |
| oh-my-pi | `omp` | 18.4.3 | `curl -fsSL https://omp.sh/install \| sh --binary` | `~/.local/bin/omp` | `~/.omp/agent` |

包名注意：上游已迁移到 `@earendil-works/pi-coding-agent`（0.87.x），旧的
`@mariozechner/pi-coding-agent` 停在 0.73.x，不要再装。

配套的运行时升级：nvm node 22.14.0 → **22.23.3**（default alias 已改），
npm 11.7.0 → **12.1.0**，13 个全局包在新 node 下全部重装。

## 踩坑记录

### 1. 裸装 pi 会静默降级到 0.74.2

`npm i -g @earendil-works/pi-coding-agent`（不写版本号）装出来的是 **0.74.2**，
正好等于 registry 上的 `legacy-node20` dist-tag；本地 node 22.14.0 低于 0.87.1 要求的
`node >= 22.19.0`，npm 解析时回落到了旧 dist-tag，且只有 `EBADENGINE` 警告。

规避：显式写 `@latest`，并把 node 升到 22.19+（本次升到 22.23.3），警告消失。

### 2. `curl https://omp.sh/install | sh` 会因 bun 版本过低直接失败

安装脚本默认逻辑是「本机有 bun 且架构匹配 → 走 bun 源码安装」，于是先做
bun 版本检查。本机 bun 1.3.12 < 要求的 1.3.14，脚本直接退出：

```
Bun 1.3.14 or newer is required. Current version: 1.3.12
```

并不会自动回落到预编译二进制。规避：加 `--binary` 强制走 GitHub Releases 预编译包
（`omp-linux-x64`，落到 `~/.local/bin/omp`，脚本自带 `omp --version` 冒烟校验）。
`--source` 才是源码/bun 安装模式。

### 3. npm 12 默认不执行 install scripts，全局包会静默半残

npm 12 起新增 `allow-scripts` 开关，默认**不跑**任何 install/postinstall 脚本，
只打印 warn。后果（本次全部踩到）：

| 包 | 症状 |
| --- | --- |
| `cline` | tarball 里 `bin/cline` 是 macOS Mach-O，postinstall 才会换成本平台二进制 → `无法执行二进制文件：可执行文件格式错误` |
| `bun` | `Error: Bun's postinstall script was not run.` |
| `node-pty` | 原生绑定不构建，依赖它的终端类功能运行期报错 |
| `tree-sitter-bash` | 同上（预编译 prebuilds 齐全，影响较小） |

规避：显式开白名单，然后重装受影响包。

```bash
npm config set allow-scripts=node-pty,tree-sitter-bash,bun,cline --location=user
npm i -g cline@1.0.1 bun@1.3.12
```

`protobufjs` / `@google/genai` / `yarn` 的脚本是 no-op 或纯提示，保持不授权即可。
注意：用旧 npm（11.x）写入的 `allow-scripts` 会被新 npm 报 `Unknown user config`
警告；反过来用 npm 12 写、用 npm 11 读也会告警，属预期。

### 4. nvm 只在「PATH 里没有 nvm 目录」时才自动切 default

`nvm use` 不是 shell 初始化必做的：`nvm.sh` 检测到 PATH 中已有某个 nvm 版本目录
（`NVM_CURRENT != none`）就**不会**切到 default。所以从旧终端 / IDE / 已运行的
agent 进程里派生出来的子进程，会继续用旧 node，表现为「明明装了新包，命令还是旧的」。

规避：`~/.bashrc` 的 nvm 加载块后面显式钉死默认版本。

```bash
nvm use --silent default >/dev/null 2>&1 || true
```

验证要用干净环境，否则父进程 PATH 会污染结论：

```bash
env -i HOME=$HOME TERM=xterm bash -ic 'node -v; npm -v'
```

### 5. `~/.local/bin/codex` wrapper 硬编码了旧 node 路径

原脚本写死 `/home/zhangxu/.nvm/versions/node/v22.14.0/bin/codex`，换 node 后会永远
调用旧安装。已改为动态解析：优先取 `command -v node` 同级目录下的 `codex`（wrapper
本身在 `~/.local/bin`，不会遮蔽 `node`），失败再回退到 nvm 下版本排序最高的那个。
备份在 `~/.local/bin/codex.bak-20260929`。

### 6. pnpm 是 corepack shim，换 node 后要重新 enable

旧 node 的 `pnpm`/`pnpx` 是指向 `corepack/dist/*.js` 的软链，新 node 下缺失。
`corepack enable pnpm` 重新生成（corepack 首次运行会自行下载对应 pnpm 版本）。

### 7. 其他已确认无害的现象

- `kilo` / `kilocode`：旧 node 里就是悬空软链（`@kilocode/cli` 早已不在），与本次无关。
- `tailwindcss`：npm 全局包本身没有 `bin` 字段，命令来自 `~/.local/bin/tailwindcss`。
- 旧 node 22.14.0 目录**故意保留未卸载**：仍有进程继承它的 PATH，删掉会让那些
  进程连 `node` 都找不到。确认没有引用后再 `nvm uninstall 22.14.0`。

## 一次性验证命令

```bash
pi --version                 # 0.87.1
pi --help                    # CLI 完整加载通过
pi auth                      # 子命令可用，会初始化 ~/.pi/agent/auth.json
omp --version                # omp/18.4.3
omp config path              # /home/zhangxu/.omp/agent
env -i HOME=$HOME TERM=xterm bash -ic 'node -v; npm -v; pi --version; omp --version'
```

## 后续可做

- 补 shell 补全：`eval "$(omp completions bash)"` 写进 `~/.bashrc`。
- 用 `rules-install` 技能把全局 AGENTS.md 同步进 `~/.pi/agent/AGENTS.md` 与
  `~/.omp/agent/AGENTS.md`。
- pi 想跑后台 subagent 必须保留 npm 包目录（独立二进制没有 package 目录），
  这也是本次 pi 选 npm 全局安装的原因。
- bun 仍是 1.3.12（< omp 要求的 1.3.14）。想用 `omp --source` 源码安装路径的话，
  需要先 `npm i -g bun@latest`（`bun` 已在 allow-scripts 白名单里）。
