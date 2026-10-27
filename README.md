# OhMyPiDesktop

**omp 与 pi 的桌面工作台。** 它不重写 Agent——它把 `omp`（OhMyPi）和 `pi`（Pi）这两个
只会在终端里跑的 coding agent，变成一个能看、能点、能管的工作台。

```
引擎负责思考与干活，OhMyPiDesktop 负责让你看见并控制它是怎么干的。
```

两个引擎可同时在管：输入框上方选一个，其余界面按它的能力自动调整。

## 能力

| | 说明 |
|---|---|
| 对话 | 流式正文、思考段、工具卡（参数 / 输出 / 耗时）按引擎吐出的**原始顺序**内联；插话、排队、停止、自动重试提示、提问卡片 |
| 工作区 | 文件树（只读）、改动清单、逐文件 diff、暂存 / 提交、分支切换、冲突处置 |
| 引擎 | 自动探测 `omp` / `pi`（版本 + 种类 + 能力表）；一个工作目录一个进程；界面上切引擎 |
| 会话 | 直接读引擎的会话目录：终端里聊过的这里也看得到，点开接着聊 |
| 子代理 | 仅 omp：roster、进度、成本、上下文水位、子代理对话（字节游标增量）、插话、取消 |
| 模型 | 读写引擎的模型配置：这里加的服务商，终端里立刻也认；OAuth 登录在界面里发起（仅 omp） |
| 技能 | 列出引擎真正会加载的技能（用户 / 托管 / 工作区 / 插件）；omp 改 `enabled`，pi 改 `disable-model-invocation` |
| 插件 | 列表 / 安装 / 卸载走引擎自己的 CLI，不自己解 `node_modules` |
| 用量 | token 与花费，全部来自引擎自己写的会话文件 |

## 它不做什么

- **不安装引擎**。不做下载、不改 PATH、不写注册表、不弹 UAC。
  没找到引擎时只说缺什么、给出可复制的安装命令原文。
- 不重写 Agent 内核：工具、压缩、重试、缓存、编辑、MCP、扩展、子代理全由引擎演进
- 不造团队编排层：omp 没有 spawn 的 RPC，pi 连子代理都没有；观测台是上限
- 不改引擎的默认行为，不另装审批闸门
- 不另存一份会话与用量：那是两本账，两本账迟早分叉
- 不做引擎没提供的花活：界面只渲染引擎给的提问基元，不发明新的

## 装引擎（用户自己做）

```powershell
# omp
irm https://omp.sh/install.ps1 | iex

# pi
npm i -g @earendil-works/pi-coding-agent
```

装完重开终端，让 PATH 生效；应用里点「重新检测」。

## 技术栈

| 层 | 选型 |
|---|---|
| 语言 | Go 1.24（单 module，无 CGO） |
| 桌面壳 | Wails v2（无边框，前端自绘标题栏） |
| 业务 API | gin（回环 + 随机端口 + 启动令牌，只允许 GET / POST） |
| 实时通信 | SSE（`/api/v1/events`，带序号与环形重放） |
| 存储 | SQLite（WAL，pure-Go 无 CGO）：**只存界面设置** |
| 工作区 | 调系统 `git`（不引入 Go 的 git 实现，行为与终端一致） |
| 协议 | 一行一条 JSON 走 stdio；omp 有 `ready` 握手与 v2 分片，pi 无握手且不接受 `--cwd` |
| 前端 | Vue 3 + TypeScript + Vite + Pinia + Vue Router（hash） |
| UI | Element Plus（基元）+ UnoCSS（原子类，刻度接 `--omp-*`） |
| 图表 | ECharts（按需注册，色值取 `--omp-ch-*`） |
| 渲染 | marked（唯一出口 `utils/md.ts`，块级增量缓存） |
| 国际化 | Vue I18n（zh-CN 默认 + en） |

## 目录

```
OhMyPiDesktop/
├── main.go                 入口：embed + wails.Run + 装配
├── backend/
│   ├── engine/             引擎抽象：Kind / Caps / Proc / Translator / Supervisor / Probe
│   │   ├── omp/            omp：ready 握手、v2 分片、子代理、私有帧
│   │   └── pi/             pi：无握手、能力降级、私有帧
│   ├── env/                引擎探测（不安装）+ layout（落盘布局）
│   │   ├── ompcfg/         models.yml 的 YAML 节点级读写
│   │   └── picfg/          models.json 的 JSONC 键级读写
│   ├── session/            会话文件：一套读取代码 + 三点适配
│   ├── skill/              技能发现与启停（两套根、两种开关键）
│   ├── git/                工作区版本状态：status / diff / 历史 / 白名单写入
│   ├── service/            用例编排（会话 / 对话 / 模型 / 技能 / 插件 / 子代理 / 工作区）
│   ├── server/             gin 路由 / SSE / 统一响应
│   ├── hub/                事件总线（seq + 环形重放）
│   ├── repo/               GORM（只有设置 KV）
│   ├── domain/ runtime/ pkg/
├── frontend/src/           api / components / composables / locales / stores / types / utils / views + CSS
├── tools/
│   ├── smoke/              真起引擎跑一轮，打印事件序列与投影结果
│   └── devserver/          只起后端，供浏览器直连调试
└── docs/                   工程文档
```

## 开发

```bash
wails dev                        # 热重载
go test ./backend/... -count=1   # 后端测试
go build ./backend/...           # 只验后端
cd frontend && npm run build     # 前端类型检查 + 打包
wails build                      # 产物 build/bin/OhMyPiDesktop.exe
```

只调后端时不必开 Wails：

```bash
go run ./tools/devserver -token dev    # 打印端口与 localStorage 设置命令
cd frontend && npm run dev             # Vite
# 浏览器控制台里设 localStorage 的 omp.dev.port / omp.dev.token 即可直连
```

真起引擎验一轮（改了协议翻译后必做）：

```bash
go run ./tools/smoke -engine omp -msg "Reply with exactly: OK"
go run ./tools/smoke -engine pi  -msg "Reply with exactly: OK"
```

构建需要 Go 1.24+、Node 18+、Wails CLI。**引擎不必预装**——没有引擎也能起来，
界面会显示「没找到引擎」并给出安装命令。

## 文档

| 文档 | 内容 |
|---|---|
| [AGENTS.md](AGENTS.md) | 工程规范（唯一权威：边界 / 分包 / 依赖方向 / 错误码 / 测试 / 命名） |
| [DESIGN.md](DESIGN.md) | 视觉语言与交互原则（配色、五态、动效、内容渲染） |
| [docs/ENGINE-MATRIX.md](docs/ENGINE-MATRIX.md) | **引擎能力的唯一事实来源**：omp 与 pi 的全量差异 |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | 分层、启动时序、一次对话的链路、事件出口 |
| [docs/PROTOCOL.md](docs/PROTOCOL.md) | 与两个引擎的 RPC 契约、握手差异、翻译表、不变量 |
| [docs/DATA-MODEL.md](docs/DATA-MODEL.md) | 哪些数据属于引擎、哪些属于本应用 |
| [docs/API-CONTRACT.md](docs/API-CONTRACT.md) | HTTP 与 SSE 路径表 |
| [docs/PAGE-STRUCTURE.md](docs/PAGE-STRUCTURE.md) | 八个页面的骨架与引擎差异 |
| [docs/ERROR-CODES.md](docs/ERROR-CODES.md) | 错误码段位与**前端处置表** |
| [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) | 环境、命令、调试真实引擎、改代码时改什么 |
| [docs/MODULES.md](docs/MODULES.md) | 模块 → 职责 → 规格索引 |
| [docs/specs/](docs/specs/) | 子系统规格（01 引擎 / 02 环境 / 03 会话 / 04 技能 / 05 HTTP+SSE / 06 子代理 / 07 工作区） |

未正式发行。
