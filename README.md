# OhMyPiDesktop

**omp 与 pi 的桌面工作台。** 它不重写 Agent——它把 `omp`（OhMyPi）和 `pi`（Pi）这两个
只会在终端里跑的 coding agent，变成一个能看、能点、能管的工作台。

```
引擎负责思考与干活，OhMyPiDesktop 负责让你看见并控制它是怎么干的。
```

两个引擎可同时在管：输入框上方选一个，其余界面按它的能力自动调整。

## 设计主张

**做代理，不做替代品。** 引擎的演进速度远高于界面。凡是引擎自己能做的事
（工具、压缩、重试、缓存、扩展、子代理、MCP、LSP），本应用**一个都不重写**——
重写就意味着永久追着它跑。这里只做两件事：把引擎吐出的事件翻译成可读的界面，
把它暴露的操作变成可点的控件。

**能力差异显式化，不用灰按钮。** 两个引擎的能力差得很远。正确的形态不是
「抽象成共同最小集」（那会把强引擎的能力全砍掉），而是统一可统一的部分、
分叉真正不同的部分，并把差异集中到一张能力表里。界面按这张表**隐藏**入口，
而不是渲染一个点不动的按钮——灰按钮会被当成「我还没权限」。

**账本只有一份。** 引擎的会话文件是唯一账本。再存一份，两本账迟早分叉，
表现是「界面上有、终端里没有」或者反过来。所以本应用只写自己的界面设置，
一个 JSON 文件。

**一个工作目录一个引擎进程。** `cwd` 是进程级属性，共进程会让工具写到错的目录。
引擎与目录共同构成进程键 `(Kind, WorkDir)`：同一目录下 omp 与 pi 是两个进程，
用户在输入框里切引擎，切的是「跟哪个进程说话」，不是「换一个目录」。

## 能力

| 领域 | 内容 |
|---|---|
| 对话 | 流式正文、思考段、工具卡（参数 / 输出 / 耗时）按引擎吐出的**原始顺序**内联；插话、排队、停止、自动重试提示、提问卡片、上下文水位 |
| 输入区 | `/` 命令（内置 7 条 + 引擎命令合成一张表）、`@` 工作区文件引用、粘贴截图、选择文件；草稿按会话保存 |
| 工作区 | 文件树（只读）、改动清单、逐文件 diff、暂存 / 提交、分支切换、冲突处置、**提交详情与文件时间线** |
| 引擎 | 自动探测 `omp` / `pi`（版本 + 种类 + 安装渠道 + 能力表）；一个工作目录一个进程；界面上切引擎；配置原文只读查看 |
| 应用 | 装 / 升级 / 卸载两个引擎：**代跑引擎自己的官方安装器与包管理器命令**，输出逐行推到界面；版本对比与渠道识别；管不了的只给手动路径 |
| 会话 | 直接读引擎的会话目录：终端里聊过的这里也看得到，点开接着聊；**启动时回到上次那个** |
| 子代理 | 仅 omp：roster、进度、成本、上下文水位、子代理对话（字节游标增量）、插话、取消 |
| 模型 | 读写引擎的模型配置：这里加的服务商，终端里立刻也认；OAuth 登录在界面里发起（仅 omp） |
| 技能 | 列出引擎真正会加载的技能（用户 / 托管 / 工作区 / 插件）；omp 改 `enabled`，pi 改 `disable-model-invocation`；**用户目录那份可直接编辑** |
| 插件 | 列表 / 安装 / 卸载走引擎自己的 CLI，不自己解 `node_modules` |
| 用量 | token 与花费，全部来自引擎自己写的会话文件 |
| 常驻 | 关窗收托盘；托盘菜单可回到最近工作目录、打开数据目录；可选开机自启 |

哪些能力属于哪个引擎，见 [docs/ENGINE-MATRIX.md](docs/ENGINE-MATRIX.md)。

## 边界

- **不自建安装器**。装、升级、卸载都**代跑引擎自己的官方安装器与包管理器命令**，
  界面上显示的命令与真正执行的是同一串。我们不下载、不解压、不校验、不内置运行时、
  不改 PATH、不弹 UAC。（唯一会写注册表的地方是「开机自启」，写的是 HKCU 下自己的
  启动项，与引擎安装无关，不需要管理员权限。）
  渠道判不出来时不给卸载按钮，只给手动路径——不猜、不删不认识的目录。
  详见 [docs/modules/apps.md](docs/modules/apps.md)。
- **不重写 Agent 内核**：工具、压缩、重试、缓存、编辑、MCP、扩展、子代理全由引擎演进。
- **不造团队编排层**：omp 没有 spawn 的 RPC，pi 连子代理都没有；子代理面板是能力上限。
- **不改引擎的默认行为**，不另装审批闸门。
- **不另存一份会话与用量**：那是两本账，两本账迟早分叉。
- **不做引擎没提供的花活**：界面只渲染引擎给的提问基元，不发明新的。
- **只面向 Windows**。

## 技术栈

| 层 | 选型 |
|---|---|
| 语言 | Go 1.26（单 module，无 CGO） |
| 桌面壳 | Wails v3（无边框，前端自绘标题栏；内置单实例与托盘） |
| 业务 API | gin（回环 + 随机端口 + 启动令牌，只允许 GET / POST） |
| 实时通信 | SSE（`/api/v1/events`，带序号与环形重放） |
| 存储 | 一个 JSON 文件：只存界面设置 |
| 工作区 | 调系统 `git`（不引入 Go 的 git 实现，行为与终端一致） |
| 协议 | 一行一条 JSON 走 stdio；omp 有 `ready` 握手与 v2 分片，pi 无握手且不接受 `--cwd` |
| 附件 | 文件只拼路径清单（引擎自己读），图片走 `prompt.images`（唯一能传二进制的通道） |
| 前端 | Vue 3 + TypeScript + Vite + Pinia + Vue Router（hash） |
| UI | Element Plus（基元）+ UnoCSS（原子类，刻度接 `--omp-*`） |
| 编辑 | CodeMirror 6（技能正文与引擎配置原文；按扩展名给 markdown / json / yaml） |
| 图表 | ECharts（按需注册，色值取 `--omp-ch-*`） |
| 渲染 | marked（唯一出口 `utils/md.ts`，块级增量缓存） |
| 国际化 | Vue I18n（zh-CN 默认 + en） |

## 目录

```
OhMyPiDesktop/
├── main.go                 入口：embed 前端产物 + 版本号，交给 backend/desktop
├── backend/
│   ├── desktop/            Wails 外壳：窗口 / 托盘 / 单实例 / 绑定给前端的系统能力
│   ├── engine/             引擎抽象：Kind / Caps / Proc / Translator / Supervisor / Probe
│   │   │                   + 分发事实（dist.go / channel.go / latest.go）
│   │   ├── omp/            omp：ready 握手、v2 分片、子代理、私有帧
│   │   └── pi/             pi：无握手、能力降级、私有帧
│   ├── env/                引擎探测 + layout（落盘布局）
│   │   ├── ompcfg/         models.yml 的 YAML 节点级读写
│   │   └── picfg/          models.json 的 JSONC 键级读写
│   ├── session/            会话文件：一套读取代码 + 三点适配
│   ├── skill/              技能发现与启停（两套根、两种开关键）
│   ├── git/                工作区版本状态：status / diff / 历史 / 白名单写入
│   ├── service/            用例编排（会话 / 对话 / 模型 / 技能 / 插件 / 应用 / 偏好 / 工作区）
│   ├── server/             gin 路由 / SSE / 统一响应
│   ├── hub/                事件总线（seq + 环形重放）
│   ├── repo/               设置 KV：一个 JSON 文件
│   ├── platform/           开机自启（HKCU 启动项）与「在资源管理器里定位」
│   ├── domain/ runtime/ pkg/
├── frontend/src/           api / components / composables / locales / stores / types / utils / views + CSS
├── tools/
│   ├── smoke/              真起引擎跑一轮，打印事件顺序、会话文件、用量与块
│   ├── devserver/          只起后端，供浏览器直连调试
│   └── check-i18n.mjs      校验两个语言文件的键结构一致
└── docs/                   工程文档
```

## 装引擎

两种做法都行，效果一样（应用页跑的就是下面这两行）：

```powershell
# 做法一：在应用里点「应用 → 安装」

# 做法二：自己跑一行
irm https://omp.sh/install.ps1 | iex            # omp
npm i -g --ignore-scripts @earendil-works/pi-coding-agent   # pi
```

自己装完重开终端让 PATH 生效，回来点「重新检测」。
**引擎不必预装**——没有引擎应用也能起来，界面会显示「没找到」并给两条路：
去应用页装，或复制命令自己跑。

## 开发

```bash
wails3 task dev                 # 热重载
task check                      # 跑完所有门禁（格式 / vet / 测试 / i18n 结构）
go build ./backend/...          # 只验后端
cd frontend && npm run build    # 前端类型检查 + 打包
wails3 build                    # 产物 bin/OhMyPiDesktop.exe（便携版）
wails3 package                  # 构建并打包成 NSIS 安装器 bin/OhMyPiDesktop-amd64-installer.exe
```

只调后端时不必开 Wails：

```bash
task dev:server                 # 打印端口与 localStorage 设置命令
cd frontend && npm run dev             # Vite
# 浏览器控制台里设 localStorage 的 omp.dev.port / omp.dev.token 即可直连
```

真起引擎验一轮（改了协议翻译后必做）：

```bash
task smoke -- -engine omp -message "Reply with exactly: OK"
task smoke -- -engine pi  -message "Reply with exactly: OK"
```

构建需要 Go 1.26+、Node 18+、Wails v3 CLI（`wails3`），打安装器还要 `makensis`。

### 安装器与数据

安装器是 NSIS，装到 `%LOCALAPPDATA%\Programs\OhMyPiDesktop`（`INSTALL_SCOPE=user`）
或 `Program Files`（`machine`，默认，需 UAC）。安装与卸载都带一个**默认不勾**的
「删除本机数据」勾选框：

| 场景 | 行为和结果 |
|---|---|
| 图形界面安装 / 卸载 | 勾了才删 `%LOCALAPPDATA%\OhMyPiDesktop`（设置、日志、WebView2 缓存） |
| 静默（`/S`） | 永远不删数据——无人值守不能删别人的东西 |
| 静默要删 | `installer.exe /S /CLEANDATA` / `uninstall.exe /S /CLEANDATA` |

引擎自己的会话与配置（`~/.omp/agent/`、`~/.pi/agent/`）**永远不删**：那是引擎的账本，
删了等于把用户的对话抹掉。

覆盖安装前安装器会先 `taskkill` 掉运行中的实例（托盘常驻让「窗口关了」不等于
「进程退了」，exe 会被句柄占住，覆写报错对用户是莫名其妙的失败）。

## 文档

| 文档 | 内容 |
|---|---|
| [AGENTS.md](AGENTS.md) | 工程规范（唯一权威：边界 / 分包 / 依赖方向 / 错误码 / 测试 / 命名 / 文档写法） |
| [DESIGN.md](DESIGN.md) | 视觉语言与交互原则（配色、五态、动效、内容渲染） |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | 分层、关键决策、启动时序、一次对话的链路、事件出口 |
| [docs/ENGINE-MATRIX.md](docs/ENGINE-MATRIX.md) | **引擎能力的唯一事实来源**：omp 与 pi 的全量差异与已知边界 |
| [docs/PROTOCOL.md](docs/PROTOCOL.md) | 与两个引擎的 RPC 契约、握手差异、帧处置、不变量 |
| [docs/API-CONTRACT.md](docs/API-CONTRACT.md) | HTTP 与 SSE 契约、SSE 服务端实现要点 |
| [docs/DATA-MODEL.md](docs/DATA-MODEL.md) | 哪些数据属于引擎、哪些属于本应用、写边界 |
| [docs/PAGE-STRUCTURE.md](docs/PAGE-STRUCTURE.md) | 十一个页面的骨架与引擎差异 |
| [docs/ERROR-CODES.md](docs/ERROR-CODES.md) | 错误码段位与**前端处置表** |
| [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) | 环境、命令、调试真实引擎、改代码时改什么 |
| [docs/modules/desktop.md](docs/modules/desktop.md) | 桌面壳：窗口为什么等前端 Ready()、托盘、单实例、绑定面 |
| [docs/modules/env.md](docs/modules/env.md) | 环境层：探测、落盘布局、两套配置读写 |
| [docs/modules/apps.md](docs/modules/apps.md) | 应用管理：分发事实、渠道判定、代跑安装器、最新版查询 |
| [docs/modules/hub.md](docs/modules/hub.md) | 事件总线：序号、环形重放、非阻塞投递 |
| [docs/modules/repo.md](docs/modules/repo.md) | 设置存储（一个 JSON）与系统能力（注册表） |
| [docs/modules/session.md](docs/modules/session.md) | 会话层：一套读取代码 + 三点适配 |
| [docs/modules/skill.md](docs/modules/skill.md) | 技能与插件：发现规则、启停语义、CLI 装卸 |
| [docs/modules/mcp.md](docs/modules/mcp.md) | MCP 服务器：共用的 mcp.json 读写、启用优先级、启停分叉 |
| [docs/modules/subagents.md](docs/modules/subagents.md) | 子代理观测台：双源数据、字节游标、干预 |
| [docs/modules/git.md](docs/modules/git.md) | 工作区：版本状态、白名单写入、文件面板 |

## 协议

[MIT](LICENSE)。你可以在保持协议声明的前提下自由使用、修改与分发。

## 发布

版本号跟随 tag（`v<major>.<minor>.<patch>`）。在 tag 上推送会触发发版流水线，
构建并打包成 NSIS 安装器，连同便携 exe 一起挂到 GitHub Release：

```bash
git tag -a v1.0.0 -m "v1.0.0"
git push origin main
git push origin v1.0.0
```

`main` 上的每次推送都会跑质量门禁（gofmt / vet / 全量测试 / 前端类型检查）。
