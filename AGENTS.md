# 工程规范

本文件是唯一权威。与代码冲突时以本文件为准；要改约定先改这里。

## 0. 产品边界（先读这一条）

这是一个**引擎的桌面工作台**，支持 omp（OhMyPi）与 pi（Pi）两个引擎。

- 引擎必须先由用户装好。**我们不下载、不安装、不升级、不改 PATH、不写注册表。**
  没找到引擎时只做两件事：说明缺什么、给出可复制的安装命令原文。
- 引擎能做的事我们一件都不重写：工具、压缩、重试、缓存、子代理、MCP、LSP 全归引擎。
- 我们只做两件事：把引擎吐的事件翻译成可读界面，把它暴露的操作变成可点控件。
- 引擎之间的能力差异**只在 `engine.Caps` 里表达**；界面按 `Caps` 隐藏入口，
  不做「灰掉的按钮」。

## 1. 分层与依赖方向

```
main.go ──→ service ──→ engine ──→ omp / pi
   │           │           │
   │           │           └──→ pkg
   │           ├──→ env / session / skill / git
   │           └──→ repo / domain / hub / pkg
   └──→ server ──→ service
```

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
| `backend/session` / `skill` | `pkg` / `domain` | `service` / `server` / `engine` 实现 |
| `backend/service` | 以上全部 | `server` / `main` / 任何 `wails` |
| `backend/server` | `service` / `hub` / `pkg` | `wails` |
| `main.go` | 全部 + `wails` | — |

- **`wails` 只能出现在 `main.go`**。业务层一旦 import 它就再也测不了。
- 依赖方向单向，不出现环。新增包先问：它该在哪一层，会不会造成反向依赖。
- `engine/omp` 与 `engine/pi` **互不 import**。需要共用的东西上提到 `engine`。

## 2. 一个概念一个落点

| 概念 | 唯一落点 | 不许 |
|---|---|---|
| 引擎进程 | `backend/engine` | 在 service 里 `exec.Command`（例外：跑引擎自己的 CLI 取插件列表，收口在 `service/proc.go`） |
| 「这个引擎能干什么」 | `backend/engine/caps.go` | 在 service / 前端里 `if kind ==` |
| 引擎探测（版本与种类） | `backend/engine/probe.go` | 在 service 里跑 `--version` |
| 一个引擎的落盘布局 | `backend/env/layout.go` | 在别处拼 `~/.omp/...` 或 `~/.pi/...` |
| 引擎配置文件格式 | `backend/env/ompcfg`（YAML）/ `picfg`（JSONC） | 在别处 `yaml.Unmarshal` / `json.Unmarshal` 那些文件 |
| 会话文件格式 | `backend/session` | 在 service 里解 JSONL |
| 技能发现规则 | `backend/skill` | 在 service 里扫目录 |
| 事件出口 | `backend/hub` | 在 handler 里直接写 SSE 帧 |
| HTTP 响应外壳 | `backend/server` | 在 service 里构造 gin 对象 |
| 子代理字节游标 | `backend/engine/omp/roster.go`（存储）+ `service/agents.go`（用例） | 在 handler 里存游标 |
| git 调用 | `backend/git` | 在 service / handler 里 `exec.Command("git", …)` |

`service` 只编排：拿数据、做判断、调下一层。它不解析文件格式、不做 HTTP。

## 3. 错误

- 所有对外错误用 `pkg.AppError`：`Code` + `Message`（给用户看）+ `Detail`（给日志看）
- `Message` 必须能直接显示给用户，说人话，且给下一步
- 错误码段位（全表见 `docs/ERROR-CODES.md`）：

| 段 | 域 | 段 | 域 |
|---|---|---|---|
| 1xxx | 内部 | 5xxx | 技能 / 插件 / 能力不支持 |
| 2xxx | 启动 | 6xxx | 会话 |
| 3xxx | 引擎进程 / 协议 | 7xxx | 入参 / 鉴权 |
| 4xxx | 模型 / 凭据 | 8xxx | 文件 / 路径 |

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
  分片重组、环形重放的跳号对账、会话文件的三点适配
- 不测「能不能解析 JSON」「函数有没有被调用」这类不会错的
- 后端：`go test ./backend/... -count=1`
- 前端：`vue-tsc --noEmit` 是硬门禁；没有 UI 单测（成本高、断言脆）
- 改了协议翻译或会话投影，必须先跑冒烟：真起一个引擎进程跑一轮

## 9. 提交

- 一次提交一件事；消息写清「为什么」，不写「改了哪些文件」
- 提交前：`go build ./backend/...`、`go test ./backend/... -count=1`、`cd frontend && npm run build`
- 不放宽门禁来让代码通过
