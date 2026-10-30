# 工程规范

本文件是唯一权威。与代码冲突时以本文件为准；要改约定先改这里。

## 0. 产品边界（先读这一条）

这是一个**引擎的桌面工作台**，支持 omp（OhMyPi）与 pi（Pi）两个引擎。

- 引擎能做的事我们一件都不重写：工具、压缩、重试、缓存、子代理、MCP、LSP 全归引擎。
- 我们只做两件事：把引擎吐的事件翻译成可读界面，把它暴露的操作变成可点控件。
- 引擎之间的能力差异**只在 `engine.Caps` 里表达**；界面按 `Caps` 隐藏入口，
  不做「灰掉的按钮」。

### 引擎从哪来（唯一一处写清「装」的边界）

引擎是**外部程序**，拥有者是用户。我们对它的处置分三档，界线是「谁在实现安装」：

| 档 | 我们做什么 | 我们绝不做什么 |
|---|---|---|
| 发现 | 探测可执行文件、版本、种类与安装渠道 | 改 PATH、写注册表启动项以外的键、装到自己的目录 |
| 代跑 | **原样转发引擎官方安装器与包管理器命令**，把输出流式打到界面 | 自己下载、解压、校验、写盘；内置运行时；自建分发源 |
| 升级 / 卸载 | 只在**能确定这条命令属于哪条渠道**时才给按钮；确定不了就只给命令原文 | 猜渠道；删一个我们不认识的目录；卸载时连带删会话与凭据 |

一句话：**我们只代跑别人写好的安装程序，从不自己当安装程序。**
这条界线让「装」这件事没有第二个真相——引擎官方怎么更新，界面就怎么更新。

没找到引擎时仍然只做两件事：说明缺什么、给出可复制的安装命令原文；
「应用」页在此之上多给一个按钮，按的也是同一条命令。

## 1. 分层与依赖方向

```
main.go ──→ backend/desktop ──→ server ──→ service ──→ engine ──→ omp / pi
     │         │            │           │           │
     │         │            │           │           └──→ pkg
     │         │            │           ├──→ env / session / skill / git
     │         │            │           └──→ repo / domain / hub / pkg
     │         │            └──→ service / hub / pkg
     │         └──→ repo / runtime / hub / pkg + wails
     └──→ pkg
```

`backend/desktop` 是唯一 import `wails` 的包：窗口、托盘、单实例、绑定给前端的
系统能力全在那里。`main.go` 只做装配（内嵌资源 + 版本号）。

桌面层也在 `backend/` 下：它依赖 `backend/*`，放外面会让人以为它是「比 backend
更高的一层」，而实际上它只是 backend 里唯一碰 `wails` 的那个包。
根目录因此只剩 `main.go` 一个 Go 文件。

硬规则：

| 包 | 允许 import | 禁止 |
|---|---|---|
| `backend/pkg` | 标准库 | 本模块任何包 |
| `backend/domain` | 标准库 | 本模块任何包 |
| `backend/hub` | 标准库 | 业务包 |
| `backend/repo` | `domain` / `pkg` | `service` / `server` |
| `backend/engine` | `pkg` | `service` / `server` / `repo` / `env` |
| `backend/engine/omp` | `engine`（抽象）/ `pkg` | `engine/pi` / `service` |
| `backend/engine/pi` | `engine`（抽象）/ `pkg` | `engine/omp` / `service` |
| `backend/env` | `pkg` / `engine`（只读 `Identity` 等结果类型） | `omp` / `pi` 的具体实现 |
| `backend/env/ompcfg` / `picfg` | `pkg` | `env` / `engine` / `service` |
| `backend/git` | `domain` / `pkg` | `service` / `engine` / `env` |
| `backend/upstream` | `pkg` | `service` / `server` / `env` / `engine` |
| `backend/platform` | `pkg` | `service` / `engine` / `env` / `domain`（只给 `backend/desktop` 与 service 用） |
| `backend/session` / `skill` | `pkg` / `domain` | `service` / `server` / `engine` 实现 |
| `backend/service` | 以上全部 | `server` / `backend/desktop` / 任何 `wails` |
| `backend/server` | `service` / `hub` / `pkg` | `wails` |
| `backend/desktop` | `backend/*` + `wails` | 被 `backend/*` 反向依赖 |
| `main.go` | `backend/desktop` / `pkg` | 堆积启动逻辑 |

- **`wails` 只能出现在 `backend/desktop` 与 `package main` 两个地方**。业务层一旦 import
  它就再也测不了——业务层要能在没有 WebView 的情况下跑测试。
- 依赖方向单向，不出现环。新增包先问：它该在哪一层，会不会造成反向依赖。
- `engine/omp` 与 `engine/pi` **互不 import**。需要共用的东西上提到 `engine`。

## 2. 一个概念一个落点

| 概念 | 唯一落点 | 不许 |
|---|---|---|
| 引擎进程 | `backend/engine` | 在 service 里 `exec.Command`（例外：跑引擎自己的 CLI 取插件列表与 MCP 状态，收口在 `service/proc.go`） |
| 「这个引擎能干什么」 | `backend/engine/engine.go` 的 `Caps` | 在 service / 前端里 `if kind ==` |
| 引擎探测（版本与种类） | `backend/engine/probe.go` | 在 service 里跑 `--version` |
| 引擎的分发事实（包名 / 安装器 / 升级 / 卸载命令） | `backend/engine/dist.go` | 在 service 或前端里拼引擎命令 |
| 引擎的安装渠道判定 | `backend/engine/channel.go` | 在 service 里猜渠道 |
| 上游最新版查询 | `backend/engine/latest.go` | 在 service 里发外网请求 |
| 装 / 升级 / 卸载的用例与并发保护 | `backend/service/apps.go` + `appexec.go` | 在 handler 里 `exec.Command` |
| 一个引擎的落盘布局 | `backend/env/layout.go` | 在别处拼 `~/.omp/...` 或 `~/.pi/...` |
| 引擎配置文件格式 | `backend/env/ompcfg`（YAML）/ `picfg`（JSONC）/ `mcpcfg`（mcp.json） | 在别处 `yaml.Unmarshal` / `json.Unmarshal` 那些文件 |
| 插件登记清单 | `backend/env/pluginreg.go` | 扫 `node_modules` 猜装了什么（会把间接依赖当插件） |
| MCP 配置读写与启用优先级 | `backend/env/mcpcfg` | 在 service 或前端里排 `disabledServers` / `enabled` 的顺序 |
| 引擎健康快照（一屏的全部事实） | `backend/service/engines.go` 的 `Health` | 前端为概览页连打四五个接口再自己拼 |
| 会话文件格式 | `backend/session` | 在 service 里解 JSONL |
| 工具差异的提取与归一 | `backend/engine/diff.go` | 在前端或 service 里解析引擎的差异格式（两个引擎格式不同） |
| 技能发现规则 | `backend/skill` | 在 service 里扫目录 |
| 事件出口 | `backend/hub` | 在 handler 里直接写 SSE 帧 |
| HTTP 响应外壳 | `backend/server` | 在 service 里构造 gin 对象 |
| 子代理字节游标 | `backend/engine/omp/roster.go`（存储）+ `service/agents.go`（用例） | 在 handler 里存游标 |
| git 调用 | `backend/git` | 在 service / handler 里 `exec.Command("git", …)` |
| 向上游服务商问模型清单 | `backend/upstream` | 在 service 里自己拼 URL 发 HTTP |
| 开机自启 / 系统唤起 | `backend/platform` | 在 service 里直接读注册表或调 `explorer` |

`service` 只编排：拿数据、做判断、调下一层。它不解析文件格式、不做 HTTP。

**一个 go 文件不超过 500 行**，超了就按职责拆开而不是先合着再说：
`service/` 与 `server/` 都按业务概念分文件（`chat.go` / `models.go` / `skills.go` …），
一个文件对应界面上的一块。拆分不新增包、不新增层——只是把同一层的函数放到各自该在的文件里。

## 3. 错误

- 所有对外错误用 `pkg.AppError`：`Code` + `Message`（给用户看）+ `Detail`（给日志看）
- `Message` 必须能直接显示给用户，说人话，且给下一步
- 错误码段位与前端处置表见 `docs/ERROR-CODES.md`（唯一落点）
- 底层错误用 `pkg.Wrap(code, message, err)` 收口，不把 `err.Error()` 直接当 message
- HTTP 恒回 200，错误在 body 的 `code` 里；前端拦截器统一抛 `ApiError`
- **命令不存在时不发出去**，直接返回 `CodeEngineUnsupported`；前端本该已隐藏入口

## 4. 并发

- 一个引擎进程 = 一个读协程（独占 stdout）+ 一把写锁（半行写入会让解析器吞掉后续命令）
- 状态用 `sync.Mutex` 保护；不在持锁时做 I/O，不在持锁时调用可能阻塞的回调
- 回调（`OnFrame` / `OnExit`）**禁止阻塞**：它们跑在读协程上
- 事件投递必须是 O(1)：慢消费者丢最旧的帧，不阻塞广播
- **永不 close 一个可能还有人写的 channel**：订阅端只置标志 + 摘除，channel 交给 GC
- 帧只序列化一次，多订阅者共享 `[]byte`
- 用 `context` 传取消，不用 `time.Sleep` 轮询代替等待
- 等引擎首帧**必须带超时**（pi 没有 `ready` 帧）

## 5. 命名

- 包名小写单词，不缩写（`session` 不写 `sess`）
- 错误码常量集中在 `pkg/errors.go`
- 设置键集中在 `domain/model.go`（`Key*`），不散落字符串
- 事件名固定 `namespace:name`（`chat:patch` / `engine:ready` / `session:info` / `subagent:progress`）
- 组件文件名 = 组件名（`ToolCallCard.vue` → `<ToolCallCard />`）
- 引擎实现包名用引擎名（`engine/omp` / `engine/pi`），不写 `ompengine`

### ID：ULID + 业务前缀

标识符一律走 `pkg.NewULID*`，形如 `RUN_01J...`：

- **不用 UUID**：无序，且读不出时间
- **不用自增**：要中心发号，多进程会撞
- **ULID** 前 48 位毫秒时间、后 80 位随机，字典序即时间序，不需要协调
- **必须带前缀**（`pkg.Prefix*`）：日志里看到 `RUN_01J...` 就知道它是一轮对话，
  不必回去翻生成它的那行代码
- 同一毫秒内连续生成用 `NewULIDMonotonic`：随机段会把先后顺序丢掉

### 字段：不许用裸名

`domain` 里的字段**不许**是 `Name` / `Code` / `ID` / `Path` 这类裸词，
一律带所属名词：`ProviderName` / `ModelID` / `FilePath` / `BranchName`。
json tag 同步用 snake_case（`branch_name`）。

理由不是好看：这些名字会一路进 JSON 与前端类型。`{"name": "main"}` 在
分支、远端、服务商、文件四种响应里都出现，读的人只能靠上下文猜；
`{"branch_name": "main"}` 不用猜。

改这些字段属于**响应形态变化**：必须 `ContractVersion` +1 并同步
`frontend/src/types/api.ts`。

## 6. 注释

- **讲为什么，不讲是什么**。`// 一个工作目录一个进程：cwd 是进程级属性` 有信息量；
  `// 遍历引擎列表` 没有
- 包级注释写这个包「不做什么」——边界比功能更容易被后人踩破
- 不写变更记录、不写「参考了某某项目」、不写待办

## 7. 前端

- 色值只在 `themes.css`；组件只写 `var(--omp-*)`
- 用户可见字符串一律走 i18n，两个语言文件结构必须一致
- 流式渲染只有一个出口（`utils/md.ts`），视图不得自己调 `marked`
- 不可信 JSON（SSE 载荷）必须窄化后取字段，禁止 `as` 一个形状直接读成员
- 组件超过 400 行先想是不是该拆；`views` 只做编排，渲染细节在 `components`
- **能力缺失不渲染，不渲染成灰按钮**
- 五个按钮状态必须齐全（见 `DESIGN.md`）；缩放节奏只在 `base.css` 定义一次

## 8. 测试

- 只测**会悄无声息错**的地方：协议翻译顺序、块下标、工具结果回填、增量与全量的等价性、
  分片重组、环形重放的跳号对账、会话文件的三点适配、配置写入不抹掉别人的键
- 不测「能不能解析 JSON」「函数有没有被调用」这类不会错的
- **单元测试不依赖本机环境**：探测引擎与查最新版各有一个接缝
  （`service.SetDetector` 与 `engine.SetLatestFetcher`），测试里换掉它们。
  不换的话，测试就变成「测这台机器装了什么、网通不通」
- **一个包一个测试文件，同一主题合成一条表驱动**。五条散落的用例要改五处、
  跑起来也慢；一条只改一处。删掉测了等于没测的用例（只断言「能解析 JSON」「函数被调用」）
  与重复覆盖同一分支的用例，只留「会悄无声息错」的长链路：跨引擎投影等价、增量与全量等价、
  分片重组丢弃残缺、实例回收有界、探测单飞。复用已有 helper（如 `fakeDialect` / `writeSession`），
  不在各测试文件里重新定义
- 后端：`go test ./backend/... -count=1`
- 前端：`vue-tsc --noEmit` 是硬门禁；没有 UI 单测（成本高、断言脆）
- 改了协议翻译或会话投影，必须先跑冒烟：真起一个引擎进程跑一轮
- **Wails 绑定是静默故障**：绑定 id 由「导入路径 + 方法名」算出，
  挪一下包的位置，全站绑定就全失效，而编译、类型检查、单元测试一律是绿的。
  所以绑定有两道门：`backend/desktop/bindings_test.go`（比对 id 与导入路径）
  与 CI 的「Verify bindings are in sync」（比对生成物有没有跟着提交）。
  改了绑定面（挪包、加方法、删方法）必须重跑
  `wails3 task common:generate:bindings` 并把结果一起提交

## 9. 提交

- 一次提交一件事；消息写清「为什么」，不写「改了哪些文件」
- 提交前：`go build ./backend/...`、`go test ./backend/... -count=1`、`cd frontend && npm run build`
- 不放宽门禁来让代码通过

## 10. 文档

文档是给人读的，讲**现状**。四条纪律：

1. **只写现状，不写历史**。每句话描述「现在是什么 / 为什么这么设计 / 代价是什么」。
   不写「这次新增了 X」「之前是 Y」「契约版本从 3 升到 4 的过程」。
   契约版本只写当前值 + 变更规则。
2. **不点外部参考项目**。只讲我们怎么设计、怎么实现。技术栈里用到的库名照写
   （那是「用什么」），但不要写「借鉴了某某项目」或指向外部源码树的路径。
3. **一个事实一个落点**。同一件事只在唯一一处写全，其余地方写「见 X」。
   平行复述必然漂移，漂移了没人知道哪份是对的。
4. **能自检**。改完文档跑一遍：

   ```bash
   grep -rn "D:\gitFiles\|packages/\|曾出现\|早期版本\|早先版本\|双引擎改造" docs/ README.md DESIGN.md AGENTS.md
   ```

   应当没有输出。

注释的尺度：**字段有注释、方法有签名、每个文件有一句大概说明**就够了。
不要把实现细节写进注释——那是文档的活。注释只讲「为什么」，不讲「是什么」。

文档的归属：

| 文档 | 讲什么 |
|---|---|
| `README.md` | 门面：是什么、能干什么、不干什么、技术栈、怎么跑起来、文档索引 |
| `AGENTS.md` | 工程规范（唯一权威） |
| `DESIGN.md` | 视觉与交互原则（为什么这样呈现） |
| `docs/ARCHITECTURE.md` | 分层、关键决策、启动时序、一次对话的链路、优势与局限 |
| `docs/ENGINE-MATRIX.md` | 引擎能力的唯一事实来源 + 已知边界 |
| `docs/PROTOCOL.md` | 与引擎的 RPC 契约与帧处置 |
| `docs/API-CONTRACT.md` | HTTP 与 SSE 契约 + 服务端实现要点 |
| `docs/DATA-MODEL.md` | 数据归属与写边界 |
| `docs/PAGE-STRUCTURE.md` | 前端页面的事实（有什么字段、什么操作） |
| `docs/DEVELOPMENT.md` | 环境、命令、调试、常见问题 |
| `docs/ERROR-CODES.md` | 错误码与前端处置表 |
| `docs/modules/*.md` | 各模块的实现说明（定位 → 设计 → 契约 → 流程 → 不变量 → 取舍） |

`docs/modules/apps.md` 是「装」的唯一落点：它同时是引擎分发的**事实来源**
（包名、安装器、升级与卸载命令）。改命令先改那里再改代码，不要在别处另写一份。

模块文档的清单与代码目录一一对应：

| 文档 | 覆盖的包 |
|---|---|
| `docs/modules/desktop.md` | `backend/desktop`（Wails 外壳：窗口 / 托盘 / 单实例 / 绑定） |
| `docs/modules/apps.md` | 引擎分发事实 + 应用管理的用例（跨 `engine` 与 `service`） |
| `docs/modules/env.md` | `env`（探测与落盘布局）+ `env/ompcfg` + `env/picfg` |
| `docs/modules/session.md` | `session`（会话文件读取与投影） |
| `docs/modules/mcp.md` | `env/mcpcfg` + `service/mcp.go`（MCP 服务器的读写与启停） |
| `docs/modules/skill.md` | `skill`（技能发现与启停）+ 插件的 CLI 装卸 |
| `docs/modules/subagents.md` | `engine/omp` 的子代理观测台 |
| `docs/modules/git.md` | `git`（工作区版本状态与白名单写入） |
| `docs/modules/upstream.md` | `upstream`（向上游服务商问模型清单） |
| `docs/modules/hub.md` | `hub`（事件总线） |
| `docs/modules/repo.md` | `repo`（设置存储）+ `platform`（注册表与系统调用） |

没有单独文档的包：`domain`（纯结构体，字段即文档）、`pkg`（叶子工具）、
`server`（见 API-CONTRACT.md）、`service`（见各模块文档的「用例」一节）、
`main.go`（只做装配）。
