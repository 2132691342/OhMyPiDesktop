# 架构

## 一句话

`main.go` 是 Wails 壳；壳里起一个回环 HTTP 服务；前端只跟这个服务说话；
服务后面是一个**引擎抽象层**，它按用户选中的引擎（omp 或 pi）拉起子进程并翻译其协议。

```
┌──────────────────── Wails 窗口（WebView2）────────────────────┐
│  Vue 3 前端                                                   │
│    ├─ 一问一答：axios → http://127.0.0.1:<随机端口>/api/v1/…   │
│    └─ 流式：EventSource → /api/v1/events（seq + 环形重放）      │
└───────────────────────────┬───────────────────────────────────┘
                            │
┌───────────────────────────▼───────────────────────────────────┐
│  Go 后端                                                       │
│  server(gin) ──→ service(编排) ──→ engine(抽象 + 能力表)        │
│       │                ├──→ engine/omp   （omp 子进程 + 协议）  │
│       │                ├──→ engine/pi    （pi  子进程 + 协议）  │
│       │                ├──→ session（会话文件：一套读取代码）    │
│       │                ├──→ skill  （两套发现规则）             │
│       │                ├──→ git    （工作区版本状态与白名单写入）│
│       │                └──→ env    （配置读写：ompcfg / picfg） │
│       └──→ hub(事件总线，seq + 环形重放)                        │
└───────────────────────────┬───────────────────────────────────┘
                            │ stdio（一行一条 JSON）
┌───────────────────────────▼───────────────────────────────────┐
│  omp --mode rpc-ui   /   pi --mode rpc      （一个工作目录一个）│
│    工具 · 压缩 · 重试 · 缓存 · 扩展 · 子代理 · MCP —— 引擎自己的事 │
└───────────────────────────────────────────────────────────────┘
```

## 为什么是「代理」而不是「重写」

引擎的演进速度远高于界面。凡是引擎自己能做的事（工具、压缩、重试、缓存、
子代理、MCP、LSP），我们**一个都不重写**——重写就意味着永久追着它跑。
我们只做两件事：**把它吐出的事件翻译成可读的界面**，**把它暴露的操作变成可点的控件**。

这条边界决定了下面所有取舍。

## 为什么双引擎而不是只支持一个

omp 是 pi 的 fork，两者协议家族相同（同一套 JSONL 信封、同一套会话文件血统），
但能力差得很远：omp 有子代理、目标、待办、审批、登录向导、分块协议；pi 没有。

所以正确的形态不是「抽象成共同最小集」（那会把 omp 的能力全砍掉），
而是**统一可统一的部分，分叉真正不同的部分，能力不够就在界面上显式降级**。

| 层 | 统一 | 分叉 |
|---|---|---|
| 进程与传输 | 拉起、cwd、JSONL 分帧、id 关联 | 握手（omp 等 `ready` 并协商 v2；pi 无握手） |
| 会话文件 | 目录遍历、头部解析、用量累加 | 标题来源（omp 256B 槽位 / pi `session_info` 条目）、目录编码 |
| 事件翻译 | 正文/思考/工具/用量/压缩/重试 | 结束判据（omp `yielded` / pi `willRetry`）、十来个帧名 |
| 配置 | 无 | omp YAML 节点编辑 / pi JSON 只改掌管键 |
| 能力门 | 由 `Caps` 表统一回答 | 子代理/目标/待办/旁问/语音/审批/登录 = 仅 omp |

细节见 [ENGINE-MATRIX.md](ENGINE-MATRIX.md)，那是唯一的事实基线。

## 为什么业务走 HTTP 而不是 Wails 绑定

绑定调用是一问一答，而对话必须能流式。SSE 让「增量到达」成为默认形态；
绑定方法只留系统能力（端口、对话框、窗控、打开链接）。

## 为什么回环 + 随机端口 + 启动令牌

端口随机，外部网页猜不到；`X-Desktop-Token` 由启动时生成、只经 Wails 绑定交给前端；
只允许 GET / POST——没有 PUT/DELETE，就没有「顺手写一个删除接口」的入口，
所有写操作都是 POST + 动作段，审计一眼看完。

## 为什么一个工作目录一个引擎进程

`cwd` 是进程级属性，共进程会让工具写到错的目录。
同一目录的多个会话在同一个进程里 `switch_session` 切换（串行）。

**引擎与目录共同构成进程键**：`(Kind, WorkDir)`（`engine.Supervisor` 的 map 键是
`kind + "\x00" + 规范化目录`）。同一个目录下 omp 与 pi 是两个进程，互不干扰；
用户在输入框里切引擎，切的是「跟哪个进程说话」，不是「换一个目录」。

进程键还带一个推论：**同一目录两个引擎各自持有一份会话索引**，
所以 `App.indexes` 是 `map[engine.Kind]*session.Index`，不是单个索引。

## 为什么不自己存会话

引擎的 JSONL 是唯一账本。再存一份，两本账迟早分叉——
表现是「界面上有、终端里没有」或者反过来。

## 启动时序

```
wails.Run
  └─ OnStartup        → 读数据目录 / 打开设置 / 装 hub / 建 service / 建 server
     （失败写 startErr，但一定关闭 startDone，让后面有确定的等待点）
     **不探测引擎**：探测要起子进程（pi 要起 Node），装配阶段不该等它
  └─ OnDomReady       → 等 startDone（上限 30s）→ server.Start()（回环随机端口）
                        → 发 app:ready{port, token, version, contract} → 显示窗口
     前端 mount       → app.load() → getEndpoint()（Wails 绑定）→ GET /bootstrap
                        → 打开 EventSource → 拉会话与引擎状态
  └─ OnBeforeClose    → 有对话在跑时先 abort 并等 agent_end（有上限），再退
  └─ OnShutdown       → 停引擎（进程树）→ 停 HTTP → 关数据库
```

`app:ready` 是 Wails 事件总线上的一帧，只用来兜「首屏早于服务就绪」的情况；
前端真正拿端口还是调 `Endpoint()` 绑定方法——两条路都通，先到先用。

## 引擎探测（不做安装）

应用不安装任何引擎。第一次需要显示时才探测，之后缓存一分钟：

```
对每个引擎 kind ∈ {omp, pi}（并发）：
  候选 = [设置里的 engine.path.<kind>] + [自管目录] + [PATH 上的 <kind>]
  对每个候选：--version → 正则判种类 → （疑似 pi 时）--help 复核
```

探测结果进 `Bootstrap.engines`，界面据此决定：显示哪个引擎可用、输入框里给几个选项、
哪些页面要显示「该引擎不支持」。三步判据、缓存策略与坑见
[ENGINE-MATRIX.md §5](ENGINE-MATRIX.md)。

**缺引擎时的界面行为**：输入框禁用 + 一句话「没找到引擎」+ 一行可复制的安装命令。
不弹窗、不引导、不代装。

## 一次对话的链路

```
前端输入（带 engine + session + message）→ POST /chat/send
  service.Send: 解析 (Kind, WorkDir) → 取/拉对应引擎进程 →（历史会话先 switch_session）
    → engine.Turn.Prompt
  Prompt:
     ├─ 先 get_state 拿模型名（界面在首个 token 前就能显示模型）
     ├─ 开一轮投影（translator.BeginTurn）
     ├─ 发 chat:start + chat:turn（整轮骨架）
     └─ 发 RPC prompt（等 ack，不等整轮）
                                                    ┐
读协程（引擎 → Go）                                  │
  message_update.text_delta → chat:patch{delta}     │ 前端只追加尾部
  tool_execution_start/end  → chat:patch{block}     │ 前端只换那一块
  turn_end / agent_end      → chat:turn_end         │ 用量与收尾
  prompt_result             → chat:prompt_result    │ omp 独有；pi 靠 settled 推断
  session_settled / agent_settled → chat:settled    │ 整段静默
  subagent_*                → subagent:*            │ 仅 omp
                                                    ┘
前端 chat store 按 run_id 认领事件，别的 run 的事件直接丢弃
```

**为什么分 delta / block / blocks 三种更新**：正文是逐字长出来的，整块重发就是 O(字数²)；
工具卡是状态跳变，整块替换最省事；换回合才需要整表。

**一轮结束时谁收尾**（两家判据不同，这是最容易写错的一处）：

```
omp：agent_end{yielded:true}  → FinishFromTurn（推整轮 + chat:done）
     yielded:false            → 还在重试 / 压缩 / stop-time reminder，继续等
     另外 prompt_result 也会收尾（它比 agent_end 更晚到，重复时有 runID 保护）

pi ：agent_end{willRetry}     → 只更新投影（取 stopReason），不收尾
     agent_settled{aborted}   → chat:settled + FinishFromTurn
```

`FinishRun` 用「当前 runID 是否为空」做幂等：已经收过尾的重复帧不会再发一次
`chat:done`。这条保护是必需的——两家都会在结束附近发多个帧。

## 事件出口

`hub` 是唯一出口。它做三件事：

1. **排列**：`seq` 单调递增，前端发现跳号就拉一次快照对账
2. **重放**：环形窗口（1024 帧）内断线重连能补齐；窗口外不补，那部分必须走快照
3. **不阻塞**：投递必须 O(1)——慢消费者丢最旧的帧，绝不阻塞广播

**为什么不做「补齐一切」**：假装补全会让界面带着半截状态继续跑，比明确对账更危险。

## 引擎监督者

```go
Supervisor                      // 按 (Kind, WorkDir) 管理 Engine
  ├─ Engine{Kind, WorkDir}      // 一个键一个进程
  │    ├─ proc  *engine.Proc    // 进程 + 传输（启动参数、握手由 Dialect 决定）
  │    ├─ tr    *Translator     // 当前轮投影（读协程独占，对外只交副本）
  │    ├─ subs  SubagentStore   // 仅 omp 有实现；pi 为 nil
  │    └─ session/runID         // 当前会话与当前轮
  ├─ Dialect（omp / pi）         // 只有 4 个方法：Args / Handshake / Caps / Consume
  └─ engine.Proc
       ├─ 读协程（独占 stdout，按 id 唤醒等待者）
       ├─ 写锁（行分隔协议，半行写入会卡死后续命令）
       └─ v2 分片重组（Caps.Fragments 为真时）
```

**Dialect 是唯一的分叉点。** 它只有四个方法：

| 方法 | omp | pi |
|---|---|---|
| `Args` | `--mode rpc-ui --cwd <dir>` | `--mode rpc`（**不能给 `--cwd`**） |
| `Handshake` | 等 `ready`，再 `negotiate_protocol 2` | 主动 `get_state`，有回包即就绪 |
| `Caps` | 全开 | 子代理/目标/待办/登录/分片/排队编辑全关 |
| `Consume` | 处理 `agent_end{yielded}` / `prompt_result` / `subagent_*` / `tool_stream_update` | 处理 `agent_end{willRetry}` / `agent_settled` / `compaction_*` / `session_info_changed` / `bash_execution_update` |

`Consume` 返回 `true` 表示这一帧不往上派发。公共层的 `onFrame` 只认两引擎共有的帧形态——
私有帧必须先被 Dialect 转成共有形态（或就地发出事件），否则翻译表会变成两份。

关键约束（改了就会出问题）：

| 约束 | 原因 |
|---|---|
| stdout 只按 `\n` 切分 | JSON 字符串里可以有 U+2028/U+2029，按 Unicode 行分隔切会截断记录 |
| 响应按 `id` 关联 | `prompt` / `bash` / `predict_word` 会乱序返回，按到达顺序配会配错 |
| 未知帧忽略，不报错 | 装扩展的环境首帧就是 `extension_ui_request`；新版引擎加帧不能让旧界面起不来 |
| `ready` 只有 omp 有 | pi 没有握手帧，**等首帧必须带超时**，否则永久挂起 |
| omp：`chat:done` 只看 `agent_end.yielded` | 重试 / 压缩都挂在 `agent_end` 之后，提前完成会让界面自相矛盾 |
| pi：`chat:done` 看 `agent_settled` | pi 没有 `prompt_result`，也没有 `yielded` |
| 停止走 `abort` | 排队消息要跟着停，否则用户排的那句话凭空消失 |
| 进程死亡不自动重发 | 工具可能已有副作用，重发等于执行两次 |
| 子进程 `CREATE_NO_WINDOW` | 否则每拉一次闪一个黑框 |
| 杀进程走 `taskkill /T` | 引擎会拉起子代理与 bash 工具进程，只杀父进程会留孤儿 |
| 投影只在一个协程里写 | 翻译器跨协程共享必须加锁，否则块下标会错位 |

## 引擎层的错误契约

| 情况 | 返回 |
|---|---|
| 引擎没装（`Exe == ""`） | `CodeEngineUnsupported`，**不拉起进程** |
| 等握手超时 | `CodeEngineTimeout` |
| 进程启动即退出 | `CodeEngineDown`，`detail` 带退出原因 |
| 单条命令超时 | `CodeEngineTimeout`，`message` 带命令名 |
| 引擎回 `success:false` | `*CallError{Command, Message, Code}`（pi 的 `code` 为空） |
| 帧解析失败 | 记日志 + 丢弃该帧；**不返回错误** |

**帧解析失败不返回错误**是刻意的：引擎可能发我们还不认识的新帧，
让它触发一次错误只会让整个会话失败，忽略未知帧才是向前兼容的做法。

## 前端状态归属

见 [PAGE-STRUCTURE.md](PAGE-STRUCTURE.md) 的「状态归属」一节。

## 优势与局限

### 优势

| 优势 | 来源 |
|---|---|
| 引擎能力自动跟随 | 界面按 `Caps` 渲染，引擎新增能力只要补一条能力表，不用改界面逻辑 |
| 终端与界面看到的是同一份东西 | 会话、模型、技能、插件都读写引擎自己的落盘位置，不存第二份 |
| 强引擎的能力不被砍掉 | 双引擎分别实现 `Dialect`，共同部分复用，分叉部分显式 |
| 长会话不卡 | 增量渲染 + 块级 markdown 缓存 + O(1) 事件投递，三处都不随字数平方增长 |
| 前端很薄 | 历史轮由后端投影，块顺序与工具配对只有一份实现 |
| 改起来不慌 | 协议翻译、会话投影、配置读写各有唯一落点，改一处不会牵出别处 |
| 断线能自愈 | 事件带序号，环形窗口内可补发，超窗则明确要求前端拉快照对账 |

### 局限

| 局限 | 原因 |
|---|---|
| 跨目录会话不能同时跑 | 一个目录一个进程，`cwd` 是进程级属性 |
| pi 的部分能力弱于 omp | 引擎侧限制，见 [ENGINE-MATRIX.md](ENGINE-MATRIX.md) 的「已知边界」 |
| 只支持 Windows | 进程管理依赖 `taskkill /T` 与 `CREATE_NO_WINDOW` |
| 换 omp profile 要重启进程 | profile 隔离了设置、凭据、会话、缓存 |
| 引擎升级可能带来意外 | 协议是引擎的私有契约，引擎改了帧名我们要跟着改（靠「未知帧忽略」兜底） |
| 不能编排子代理团队 | omp 没有 spawn RPC，pi 连子代理都没有 |

## 关键取舍

| 取舍 | 代价 | 收益 |
|---|---|---|
| 不安装引擎，只探测 | 用户得自己先装一次 | 不碰 PATH / 注册表 / 提权，不背分发服务可用性 |
| 双引擎而非只支持 omp | 两个实现、两张能力表 | pi 用户也能用；omp 的能力不被砍成最小集 |
| 只读会话文件，不写入 | 改名必须走 RPC（要引擎在跑） | 不会写出引擎读不动的文件 |
| 引擎一个目录一个进程 | 跨目录会话不能同时跑 | 工具落到对的目录 |
| 只允许 GET / POST | 不符合 REST 洁癖 | 写操作全部可枚举 |
| 事件环形重放只留 1024 帧 | 长断线要拉快照 | 内存恒定，状态可对账 |
| 子代理只做观测台 | 不能「安排一个团队」 | 不与模型自己的 `task` 调用打架 |
