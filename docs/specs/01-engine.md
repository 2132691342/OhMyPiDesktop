# SPEC-01 引擎层（engine）

## 定位

把「一个引擎子进程」变成一组可问、可发、可停的对象。它**不做**业务判断，
也**不决定**界面上什么该出现——那是 `service` 与前端按 `Caps` 各自的事。

它存在的原因只有一个：omp 与 pi 是两个引擎，但 90% 的驱动代码是同一份，
剩下 10% 必须显式分叉而不是靠 `if kind ==` 散落各处。

## 设计

```
backend/engine/                 ← 公共层，不认识任何具体引擎
├── engine.go    Kind / Caps / Identity / Launch / Frame / Dialect / Emitter
├── proc.go      Proc：进程 + 读协程 + 写锁 + 响应配对 + 分片重组
├── translate.go Translator：帧 → 块 / 增量更新
├── supervisor.go Engine / Supervisor / RunRow
├── agents.go    Agent / AgentProgress / SubagentStore / Caller
├── wire.go      两引擎共有的 RPC 视图类型
├── probe.go     探测（--version / --help 三步判定）+ CapsFor + InstallHint
├── proc_windows.go  CREATE_NO_WINDOW / taskkill /T
├── omp/         Dialect + Roster（子代理）
└── pi/          Dialect（无握手、能力降级）
```

### Dialect：唯一的分叉点

```go
type Dialect interface {
    Kind() Kind
    Caps() Caps
    Args(workDir string, extra []string) []string
    Handshake(ctx context.Context, p *Proc) error
    Consume(e *Engine, f Frame) bool   // 返回 true = 不向上派发
}
```

| 方法 | omp | pi |
|---|---|---|
| `Args` | `--mode rpc-ui --cwd <dir>` | `--mode rpc`（**不能给 `--cwd`**） |
| `Handshake` | 等 `ready`（45s）→ 广播 v2 就 `negotiate_protocol` | 主动 `get_state`（25s），有回包即就绪 |
| `Consume` | `agent_end{yielded}` / `prompt_result` / `session_settled` / `auto_compaction_*` / `session_info_update` / `available_commands_update` / `tool_stream_update` / `subagent_*` | `agent_end{willRetry}` / `agent_settled` / `compaction_*` / `session_info_changed` / `thinking_level_changed` / `bash_execution_update` |

公共层的 `onFrame` 只认两引擎共有的帧形态（`message_*`、`tool_execution_*`、
`turn_end`、`queue_update`、`auto_retry_*`、`extension_ui_request`、`extension_error`、
`notice`、`command_output`、`rpc_frame_error`、`entry_appended`、`goal_updated`）。
私有帧必须被 `Consume` 转成共有形态或就地发事件——否则翻译表会变成两份。

### Caps：所有「能力差异」的唯一去处

```go
type Caps struct {
    Subagents      bool // omp：子代理观测台
    Goals          bool // omp
    Todos          bool // omp
    Login          bool // omp：界面发起 OAuth
    Fragments      bool // omp：协议 v2 分片
    QueueEdit      bool // omp：移除 / 提前排队
    LiveSteerCount bool // omp：queue_update.liveSteered
    SkillToggle    bool // omp：frontmatter enabled
    Extensions     bool // 两者都有
    ExportHTML     bool // 两者都有
    SlashCommands  bool // omp：主动推 available_commands_update
}
```

**纪律**：`service` 与前端想分叉时只能读 `Caps`；不许再写 `if kind ==`。
`Caps` 没覆盖到的新差异，先补字段，再写代码。

### 三层职责

| 层 | 负责 | 不负责 |
|---|---|---|
| `Proc` | 起进程、按 `\n` 分帧、按 `id` 配对响应、写锁、分片重组 | 帧的意思 |
| `Translator` | 把共有帧翻成块与增量（`delta` / `block` / `blocks`） | 发命令、管生命周期 |
| `Supervisor` | 按 `(Kind, WorkDir)` 复用进程、启动/停止、并发拉起的等待 | 帧的意思 |

## 契约

```go
type Launch struct { Exe, WorkDir string; ExtraArgs, Env []string }

func Start(ctx context.Context, opts Options) (*Proc, error)
func (p *Proc) Call(ctx context.Context, command string, params, out any, timeout time.Duration) error
func (p *Proc) Send(v any) error
func (p *Proc) Close(grace time.Duration) error
func (p *Proc) Kill()
func (p *Proc) Pid() int
func (p *Proc) Caps() Caps
func (p *Proc) Protocol() int

type Supervisor struct { … }
func (s *Supervisor) Engine(kind Kind, workDir string) *Engine  // 取句柄，不拉进程
func (s *Supervisor) Ensure(ctx, kind, workDir) (*Engine, error) // 拉进程并等就绪
func (s *Supervisor) Running() []RunRow
func (s *Supervisor) StopAll()
func (s *Supervisor) StopKind(kind Kind)
func (s *Supervisor) AnyRunning() bool
```

**错误契约**：

| 情况 | 返回 |
|---|---|
| 引擎没装（`Exe == ""`） | `CodeEngineUnsupported`，**不拉起进程** |
| 等握手超时 | `CodeEngineTimeout` |
| 进程启动即退出 | `CodeEngineDown`，`detail` 带退出原因 |
| 单条命令超时 | `CodeEngineTimeout`，`message` 带命令名 |
| 引擎回 `success:false` | `*CallError{Command, Message, Code}`（pi 的 `code` 为空） |
| 帧解析失败 | 记日志 + 丢弃该帧；**不返回错误** |

## 关键流程

### 探测（`probe.go` + `env.Detect`）

```
对每个 kind（并发）：
  候选 = [设置里的 engine.path.<kind>] + [自管目录] + [PATH 上的 <kind>]
  对每个候选：
    os.Stat 是文件
    --version（5s）
      ├─ ^[A-Za-z][\w.-]*/\d+\.\d+\.\d+  → omp
      └─ ^\d+\.\d+\.\d+(-[\w.]+)?$       → 疑似 pi → --help 复核
           含 "USAGE" 或 " v<ver>"      → 其实是 omp，排除
           含 "Usage:" 或 "--mode"       → 确认 pi
```

**不能靠文件名**：`pi.cmd`、`omp.exe`、用户改过的名字都可能。也不能用退出码探针：
那会在用户机器上起一个真进程。

探测结果缓存 60 秒（`detectTTL`），两个引擎并发探；`POST /engines/detect` 强制重探。

### 启动与就绪

```
Supervisor.Engine(kind, dir) → Engine{proc:nil}
Engine.Client(ctx)
  ├─ proc 活着 → 复用
  ├─ 另一路正在 booting → 每 80ms 看一次，最多 60s（不自己再拉一个）
  └─ 新建 → Proc.Start
       Dialect.Args → exec.Command（cmd.Dir = workDir）
       读协程启动 → Dialect.Handshake
       成功 → 发 engine:ready{kind, work_dir, pid, protocol}
```

**为什么 pi 用「问一次」而不是「等首帧」**：pi 的首帧可能是一条扩展的
`extension_ui_request`，那只证明它醒了，不证明我们的写路径可用。
主动 `get_state` 拿回包，两件事同时被证明。

**为什么 pi 给 25 秒**：它是 Node 程序，冷启动 3~4 秒，装扩展更久。
3 秒会稳定失败——这是实测结论，不是保守估计。

### 一轮对话

```
Engine.Prompt(ctx, message)
  ├─ runID = run-<ms>-<seq>
  ├─ model = get_state.model（让界面在首个 token 前就能显示模型）
  ├─ translator.BeginTurn(runID, message, model)
  ├─ Emit chat:start{run_id, kind, work_dir, session_id, user_message}
  ├─ Emit chat:turn（整轮骨架）
  └─ Call("prompt", {message}, 120s)      // 只等 ack
       失败 → Emit chat:error，清 runID

读协程继续翻译并 Emit；结束时由 Dialect 调 FinishFromTurn / FinishRun。
```

### 收尾（两家判据不同，最易写错的一处）

```
omp：agent_end{yielded:true}  → FinishFromTurn（推整轮 + chat:done）
     yielded:false            → 还在重试 / 压缩，继续等
     prompt_result            → 也收尾（用 runID 幂等，不会重复发 done）

pi ：agent_end{willRetry}     → 只更新投影（取 stopReason）
     agent_settled{aborted}   → Settle + FinishFromTurn
```

`FinishRun` 用「当前 runID 是否为空」做幂等：已收过尾的重复帧不会再发 `chat:done`。

### 关闭

```
Engine.Stop()
  → Proc.Close(5s)：关 stdin → 等进程退出 → 还在就 taskkill /T
```

应用退出走 `Supervisor.StopAll()`（并发停所有键）。
`taskkill /T` 是必需的：引擎会拉起子代理与 bash 工具进程，只杀父进程会留孤儿。

## 不变量

| 不变量 | 锁死原因 |
|---|---|
| stdout 只按 `\n` 切分 | U+2028/U+2029 在 JSON 字符串里合法 |
| 读协程独占 stdout | 两处读会互相偷字节 |
| 写必须整行持有写锁 | 半行 JSON 会让引擎吞掉后续所有命令 |
| 响应按 `id` 关联 | `prompt` / `bash` 会乱序返回 |
| 未知帧忽略不报错 | 新版引擎加帧不能让旧界面起不来 |
| 握手必须带超时 | pi 没有 `ready`，无限等会永久挂起 |
| 帧解析失败只丢该帧 | 一帧坏掉不能毁掉整条流 |
| 进程死亡不自动重发命令 | 工具可能已有副作用 |
| 一个 `(Kind, WorkDir)` 一个进程 | `cwd` 与实现都是进程级属性 |
| 投影只在读协程写，对外只交副本 | 翻译器有块下标，交实时指针等于把正在追加的 slice 递给序列化 |
| 子进程一律 `CREATE_NO_WINDOW` | 否则每拉一次闪一个黑框 |

## 取舍

| 取舍 | 代价 | 收益 |
|---|---|---|
| 用 `Dialect` 接口而不是配置表 | 加引擎要写一个类型 | 启动参数、握手、帧差分都在一处，读一个文件就懂 |
| 用 `Caps` 表而不是继承 | 加能力要改结构 | 前端与 service 读同一份事实 |
| 探测写三步判据 | 一段不太漂亮的判别逻辑 | 用户改过的二进制名、omp 遗留 launch 子命令都能正确识别 |
| 探测结果缓存 60 秒 | 刚装完引擎要等一下或手动重探 | 首屏不等 Node 启动 |
| 不等整轮，只等 ack | 需要单独的「收尾判据」概念 | 界面在首个 token 前就能显示「正在思考」 |
| 关应用设 grace 上限 | 极端情况下工具被腰斩 | 用户点 ✕ 一定能在几秒内退出 |
| 不为 pi 造假子代理面板 | pi 用户少一块功能 | 不做会自己说谎的界面 |
