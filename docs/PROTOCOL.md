# 与引擎的协议

本文件记录「我们怎么驱动 omp 与 pi、怎么翻译它们的话」。
事实基线（谁有什么命令、哪个帧叫什么）在 [ENGINE-MATRIX.md](ENGINE-MATRIX.md)，
这里只写**我们怎么用**与**我们必须守住的约束**。

## 传输

```
omp --mode rpc-ui --cwd <工作目录>
pi  --mode rpc              # 没有 --cwd
```

stdout / stdin 上一行一条 JSON（**不是** JSON-RPC）。

- 为什么 omp 用 `rpc-ui` 而不是 `rpc`：`rpc` 会把扩展的提问全部按默认值自动回答，
  「要不要装这个包」这类确认框根本到不了界面。pi 只有 `rpc`，没有这个选择。
- 为什么 pi 不能传 `rpc-ui`：pi 的 `--mode` 只接受 `text|json|rpc`，
  传 `rpc-ui` 会直接退出 1。
- **pi 不接受 `--cwd`**：传了会直接以 `Unknown option: --cwd` 退出。
  工作目录对 pi 只能是进程 cwd（`cmd.Dir`）。omp 两个都给，让「引擎眼里的目录」
  与「进程实际所在目录」不可能不一致。

## 握手（两个引擎不同，这是最容易踩的坑）

**omp**：首帧是 `ready`，广播 `supportedProtocolVersions: [1,2]` 时我们发
`negotiate_protocol {protocolVersion:2}` 升到 v2，之后超大帧以 `rpc_chunk` 序列送来。

**pi**：**没有握手帧**，写完参数后第一帧就是业务帧。

所以「就绪」不能统一写成「等 ready 帧」：

```
omp：等 ready 帧（45s 上限）→ 广播了 v2 就 negotiate_protocol 升到 2
pi ：主动发一次 get_state（25s 上限）→ 有回包就证明读循环与写路径都通了
     不靠「等首帧」判断：pi 的首帧可能是一条扩展的 extension_ui_request，
     那只能证明它醒了，不能证明我们的写路径可用
```

25 秒不是随便取的：pi 是 Node 程序，冷启动在这台机器上要 3~4 秒，
装了扩展更久。等一次往返比等「它自己说话」可靠得多。

**不要按 `--version` 输出决定走哪条路**，也不要按文件名判断——
探测结论只用来决定「启动参数」与「界面能力」，握手必须**看行为**。

## 图片附件

`prompt` / `steer` / `follow_up` 三条命令都带 `images` 字段，形状两边一致：

```json
{"message": "这张图是什么颜色？",
 "images": [{"type": "image", "data": "<base64>", "mimeType": "image/png"}]}
```

- `type` 恒为 `"image"`，是引擎侧的判别字段。
- `data` 是**不带 `data:` 前缀**的 base64。
- 图片为空时整个字段不发（`omitempty`），避免引擎收到一个空数组。

这是协议里唯一能传二进制的通道。**文件附件不走这里**——它们只把路径清单
拼进 `message`，由引擎自己的读文件工具去看。两条路的理由见
[API-CONTRACT.md 的附件一节](API-CONTRACT.md#附件)。

模型收不收图片由模型的 `input` 数组决定（含 `"image"` 才收）。
我们提前拦一道，见 `service/attachment.go`。

## 我们发的命令

只列跨引擎共有的；分叉的单独标。

| 场景 | 命令 | 参数差异 |
|---|---|---|
| 发一轮 | `prompt` | 共有。omp 返回 `{agentInvoked}`，pi 返回 `{disposition}`；参数字段两边完全一致 |
| 插话（当前轮补一句） | `steer` | 共有，参数同 `prompt` |
| 排队（这一轮结束后再说） | `follow_up` | 共有，参数同 `prompt` |
| 停止 | `abort` | 共有 |
| 会话快照 | `get_state` | 共有，但 **pi 缺 `contextUsage` / `queuedMessages` / `todoPhases` / `tokensPerSecond`**；上下文水位 pi 只能从 `get_session_stats.contextUsage` 取 |
| 会话用量 | `get_session_stats` | 共有 |
| 消息列表 | `get_messages` / `get_messages_page` | 分页**仅 omp**；pi 只能用 `get_messages` |
| 换会话 | `switch_session` | 共有（`{sessionPath}`）。pi 同样有 `fork` / `clone` / `get_tree` / `get_entries` |
| 新会话 | `new_session` | 共有 |
| 改名 | `set_session_name` | 共有 |
| 换模型 | `set_model` | 共有（`{provider, modelId}`） |
| 换思考档 | `set_thinking_level` | 共有 |
| 思考档可选值 | `get_available_thinking_levels` | 共有 |
| 可用模型 | `get_available_models` | 共有 |
| 压缩上下文 | `compact` | 共有 |
| 自动压缩开关 | `set_auto_compaction` | 共有 |
| fork / 树 | `fork` / `get_tree` | pi 的 `fork` 的 `entryId` 必填 |
| 命令清单 | `get_available_commands`（omp）/ `get_commands`（pi） | **名字不同** |

仅 omp 的：`negotiate_protocol`、`open_session`、`branch`、`handoff`、
`remove_queued_message`、`promote_queued_message`、`abort_and_prompt`、
`abort_and_restore_queue`、`set_event_filter`、`set_ask_dialog`、`set_interrupt_mode`、
`set_fast_mode`、`set_slow_mode`、`set_cache_warming`、`goal`、`set_todos`、
`set_host_tools`、`get_login_providers` / `login` / `logout`、
`predict_word`、`btw`、`live_*`，以及子代理那 5 条（见 §子代理）。

仅 pi 的：`clear_queue`。**omp 的命令表里没有 `clear_queue`**——它只提供
`remove_queued_message` 逐条移除。两者是不同的能力，所以分成
`Caps.QueueEdit`（omp）与 `Caps.QueueClear`（pi），不能共用一个入口。

**参数名陷阱**：协议里的参数名不统一，猜错不会报错，引擎只会当成缺参而静默不做。
已踩过的：

| 命令 | 正确参数 | 错误写法 |
|---|---|---|
| `steer_subagent` / `cancel_subagent` / `get_subagent_messages` | `subagentId` | `id` |
| `login` | `providerId` | `provider` |
| `get_subagent_messages` 的返回游标 | `nextByte` | `next` |

改这些命令时，以引擎自带的命令表定义为准逐字核对：参数名写错不会报错，
引擎只会当成缺参，于是界面表现为「点了没反应」。

**调用纪律**：发命令前先查对应的 `Caps` 位。命令不存在时
`engine.Engine` 直接返回 `CodeEngineUnsupported`，界面据此隐藏控件——
**不要发出去再处理报错**，那会让用户看到「点了没反应」和一堆超时。

能力位与守卫的位置（守卫分两层，一层在引擎层，一层在 service 层）：

| 能力位 | 守卫位置 | 覆盖 |
|---|---|---|
| `Caps.QueueClear` | `engine.Engine.ClearQueue` | `clear_queue` |
| `Caps.QueueEdit` | `engine.Engine.RemoveQueued` | `remove_queued_message` |
| `Caps.Fragments` | `engine.readloop` | 收 `rpc_chunk` 做分片重组 |
| `Caps.Goals` | `engine.frames` | `chat:goal` 事件 |
| `Caps.Login` | `service.LoginProviders` / `service.Login` | `login` / `logout` 与登录向导 |
| `Caps.Subagents` | `service.Agents` 等 | 子代理那 5 条 |
| `Caps.SlashCommands` | `service.ChatState` | 命令清单（omp 推 / pi 拉） |

## 我们收的帧

| 帧 | omp | pi | 我们的处置 |
|---|---|---|---|
| `ready` | ✅ | — | 记协议版本，协商 v2 |
| `response` | ✅ | ✅ | 按 `id` 唤醒等待者；`success:false` 变成 `CallError` |
| `agent_start` | ✅ | ✅ | → `chat:agent_start` |
| `message_start/update/end` | ✅ | ✅ | 进投影层，产出 `chat:patch`（delta / block）或 `chat:blocks` |
| `tool_execution_start/_update/_end` | ✅ | ✅ | 进投影层，产出 `chat:patch`（block） |
| `turn_end` | ✅ | ✅ | → `chat:turn_end`（带 usage） |
| `agent_end` | `yielded` | `willRetry` | 见下方「一轮什么时候算完」 |
| `session_settled` | ✅ | — | → `chat:settled` |
| `agent_settled` | — | ✅ | → `chat:settled`，**并在这一帧收尾整轮** |
| `prompt_result` | ✅ | — | → `chat:prompt_result` |
| `queue_update` | ✅ | ✅ | → `chat:queue` |
| `auto_compaction_start/end` / `compaction_start/end` | ✅ | ✅ | → `chat:compaction` |
| `auto_retry_start/end` | ✅ | ✅ | → `chat:retry` / `chat:retry_end` |
| `available_commands_update` | ✅ | — | → `engine:commands` |
| `extension_ui_request` | 13 种 method | 9 种 | 见 §提问基元 |
| `extension_error` | ✅ | ✅ | 只进日志：那是扩展自己的问题，弹给用户也没法处理 |
| `notice` / `command_output` | ✅ | ✅ | → `chat:notice` / `chat:command_output`（通用处理，两家都会转发） |
| `rpc_frame_error` | ✅ | — | → `chat:gap`（超大帧被丢，需要 `get_state` 对账） |
| `subagent_lifecycle/progress/event` | ✅ | — | → `subagent:*`（见 §子代理） |
| 其他（btw / live / goal / todo / irc / advisor / ttsr / cache_warming） | ✅ | — | 静默丢弃：翻译层不处理、不报错（未知帧一律忽略） |

### 工具结果里的差异

工具执行结束时，帧里带着 `result`。`result.content` 是**给模型看的一句人话**
（"Successfully replaced 1 block(s)"），真正的改动在 `result.details` 里。

| 引擎 | 差异字段 | 格式 | 文件路径 |
|---|---|---|---|
| omp | `details.diff` | 编号差异：`@@ -1,2 +1,2 @@` 加 `+2|正文` 这样的行 | `details.path` |
| pi | `details.patch` | 标准 unified diff | 工具参数里的 `path` |
| pi | `details.diff` | 编号差异（空格分隔：`+2 正文`） | 工具参数里的 `path` |

**为什么不直接透传引擎的原文**：两种格式的差别不只是分隔符——
编号差异的行号是引擎自己算的（删行给旧行号、增行给新行号、上下文行给旧行号），
unified diff 的行号由区块头累加。两种都要能读，且读出来的行结构必须一样，
否则前端就要为两个引擎写两套渲染。

于是归一在 `backend/engine/diff.go`：
`ExtractToolDiff` 取出差异原文与文件路径（`patch` 优先于 `diff`，
文件路径在 `details.path` 与工具参数两条路里都找），
`ParseToolDiff` 把它拆成 `{line_kind, old_no, new_no, line_text}` 的行。

`details` 在流式帧里嵌在 `result` 里，在会话消息里本身就是那个对象——
两种形状都要认，否则「刚聊完能看到差异，刷新一下就没了」。

### 工具耗时

pi 在 `tool_execution_end` 帧顶层给 `durationMs`；omp 一个字段都没有。
界面上「这个工具跑了多久」对两个引擎都得有，所以引擎没给时自己量：
记 `tool_execution_start` 的时刻（omp 还给了 `startedAt`，跨进程时钟更准），
到结束帧相减。

自己量的值不是引擎的权威值，但 0 更糟——0 看起来像「瞬间完成」，
是个会误导人的假数字。

### 一轮什么时候算完（两个引擎的判据不同，不能混）

```
omp：agent_end{yielded:true}  → chat:done
     yielded:false            → 还在重试 / 压缩 / stop-time reminder，继续等
pi ：agent_settled{aborted}   → chat:done
     agent_end{willRetry:true}→ 还有重试，继续等
```

**为什么不能统一看 `agent_end`**：omp 的重试与压缩都挂在 `agent_end` 之后，
提前宣布完成会让界面显示「已结束」但内容还在长；pi 的重试信号是 `willRetry`，
和 omp 的 `yielded` 语义相反（一个是「还要继续」，一个是「真的完了」）。

## 提问基元

omp 有 13 种 `extension_ui_request` method，pi 只有 9 种。界面**不自己发明第六种**。

| 基元 | omp | pi | 回写 |
|---|---|---|---|
| `confirm` | ✅ | ✅ | `{id, confirmed}` |
| `select` | ✅ | ✅ | `{id, value}` |
| `input` | ✅ | ✅ | `{id, value}` |
| `editor` | ✅ | ✅ | `{id, value}` |
| `notify` | ✅ | ✅ | fire-and-forget |
| `setStatus` | ✅ | ✅ | fire-and-forget |
| `setWidget` | ✅ | ✅ | fire-and-forget（只支持字符串数组） |
| `setTitle` | ✅ | ✅ | fire-and-forget |
| `set_editor_text` | ✅ | ✅ | fire-and-forget |
| `ask` | ✅ | — | `{id, answers:[{id, selectedOptions, customInput?}]}` |
| `cancel` | ✅ | — | 无需回写 |
| `open_url` | ✅ | — | fire-and-forget；**优先让用户复制 `launchUrl`** |
| 取消 | 两个引擎通用 | | `{id, cancelled:true}` |

- `ask` 只在 omp 且 `set_ask_dialog{enabled:true}` 之后才发；否则引擎自己走
  `select` + `editor{promptStyle:true}` 回退。**我们要主动打开它**，否则多选问卷会退化成
  一串单选。
- pi 没有 `open_url`：登录类流程在 pi 下只能提示用户去终端跑 `pi auth`。
- `select` 里长得像「自己写」的那一行不当选项：点它展开输入行，交回用户写的那句。

## 审批（仅 omp）

omp **没有独立的审批帧**，工具审批走的是 `select` 基元的固定标签集合：

```
Approve File / Always Approve File / Approve Once / Approve <x> for this session / Deny
```

界面按标签前缀识别并染成危险色。`--no-ui` 下没有审批 dialog，需要审批的工具会直接
fail closed——所以**永远不要用 `--no-ui` 启动 omp**。

pi 没有运行期审批（只有启动期的项目信任），相关控件在 pi 下隐藏。

## 子代理（仅 omp）

omp 没有编排器。多代理 = 模型自己调 `task` 工具 spawn，宿主只能观测与干预。

| 我们做 | 怎么做 |
|---|---|
| 打开订阅 | 引擎就绪后立刻 `set_subagent_subscription{level:"progress"}`（默认 `off`，不打开永远是空的） |
| 名单对账 | 每次 `prompt` 之前、每条 `subagent_lifecycle` 之后 `get_subagents` |
| 进度 | 只读 `subagent_progress.progress`（`completionPercent` / `stage` / `currentTool` / `cost` / `tokens` / `contextTokens` / `contextWindow`） |
| 子代理对话 | `get_subagent_messages{subagentId, fromByte}` 轮询；脏标记 + 500ms 节流；`cursor = nextByte`；`reset:true` 就清空重放 |
| 干预 | `steer_subagent`（不打断父轮次）/ `cancel_subagent`（`false` 当幂等） |
| 清空 | `new_session` / `switch_session` / `fork` 成功后清 roster 与游标 |

**不升到 `events` 档**：`progress` 档足够驱动脏标记，`events` 档会把 N 个子代理的
`message_update` 全灌进同一条 stdout，带宽不可控。只有用户点开某个子代理、
要求逐字看时才临时升档。

**为什么不对 pi 做子代理面板**：pi 没有 `task` 工具、没有 `get_subagents`。
它的 `subagent` 只是示例扩展，spawn 一个独立 `pi` 进程，宿主完全看不见。
面板在 pi 下整块隐藏，不做「假装支持」。

## 不变量

| 不变量 | 锁死原因 |
|---|---|
| stdout 只按 `\n` 切分 | U+2028/U+2029 在 JSON 字符串里合法，按 Unicode 行分隔切会截断记录 |
| 未知帧忽略、不报错 | 装扩展的环境首帧就是 `extension_ui_request`；新版引擎加帧不能让旧界面起不来 |
| 响应按 `id` 关联 | `prompt` / `bash` / `predict_word` 乱序返回 |
| 等首帧必须带超时 | pi 没有 `ready`，无限等会永久挂起 |
| stdin 必须完整写入 | 半条 JSON 会让解析器吞掉后续命令，表现为「命令全部超时」 |
| 不写引擎的 JSONL | 格式是引擎的私有契约；改名 / 换模型一律走 RPC |
| 会话运行中不删它的文件 | 不等整轮结束就删，引擎会往已删文件继续追加 |
| 一个 (引擎, 目录) 一个进程 | `cwd` 与实现都是进程级属性 |
| 切会话前确认空闲 | 否则两个对话的回答会串 |
| 进程死亡不自动重发 | 工具可能已有副作用 |
| 投影只在读协程写 | 翻译器有块下标，跨协程共享会错位 |
| 命令必先查 `Caps` | 发不存在的命令等于给用户一个卡住的界面 |
