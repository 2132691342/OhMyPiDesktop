# 双引擎事实基线：omp 与 pi 的能力矩阵

本文件是**唯一的引擎能力事实来源**。所有「该能力能不能做」的判断以这里为准；
未在此列出的能力，一律视为「两引擎都没有」。

- `omp` = OhMyPi，bin `omp`，包 `@oh-my-pi/pi-coding-agent`（pi 的 fork）
- `pi` = Pi，bin `pi`，包 `@earendil-works/pi-coding-agent`
- 引用 `OMP/...` 指 `D:\gitFiles\oh-my-pi`，`PI/...` 指 `D:\gitFiles\pi`

> 本应用**不安装**任何引擎。引擎必须由用户自己装好并在 `PATH`（或被显式指定路径）上。
> 缺少引擎时我们只做两件事：**说明缺什么**、**给出安装命令原文**。

## 1. 快速对照

| 维度 | omp | pi |
|---|---|---|
| 版本串 | `omp/18.13.1`（`OMP packages/utils/src/cli.ts:460`） | `1.4.0` 裸版本号（`PI .../src/main.ts:635`） |
| `--help` 首行 | `${bin} v${version}` + `USAGE` | `${APP_NAME} - AI coding assistant …` + `Usage:` |
| `--mode` 取值 | `text｜json｜rpc｜acp｜rpc-ui` | `text｜json｜rpc` |
| 启动参数 `--cwd` | **接受**，与进程 cwd 二者皆可 | **不接受**（`Unknown option: --cwd`），只用进程 cwd |
| 首帧 `ready` | 有（含 `protocolVersion` / `maxFrameBytes`） | **无**（直接进读循环） |
| 协议 v2 + 分片 | 有 `negotiate_protocol` + `rpc_chunk` | **无** |
| 命令数 | 约 50 | 34（`PI .../rpc-types.ts:24-70`） |
| 子代理 | **有**（`task` 工具 + 5 个 RPC + 3 个帧） | **无** |
| 上下文用量 | `get_state.contextUsage` | `get_session_stats.contextUsage`（`get_state` 里没有） |
| 配置文件 | `~/.omp/agent/{config.yml,models.yml,mcp.json}` + `~/.omp/plugins/` | `~/.pi/agent/{settings.json,models.json,auth.json,mcp.json}` + `npm/` |
| 会话目录 | `~/.omp/agent/sessions/`，三段式编码，256B title 槽位 | `~/.pi/agent/sessions/`，恒定 `--abs--` 编码，无槽位 |
| 会话标题 | 首行 title 槽位 | `session_info` 条目的 `name` |
| 技能开关 | frontmatter `enabled` | frontmatter `disable-model-invocation` |
| CLI `--json` 面 | 广（models/plugin/skill/config/usage/stats/collab/setup/gc） | 窄（`install/list/remove`、`auth check --json`） |

## 2. 能力矩阵：`engine.Caps` 的每一项

`engine.Caps` 是界面「渲染什么」的唯一依据。下表每一项都对应一个真实分叉：

| Caps 字段 | omp | pi | 界面影响 |
|---|---|---|---|
| `subagents` | ✅ | — | 右栏子代理面板整块不渲染 |
| `goals` | ✅ | — | `chat:goal` 事件不订阅 |
| `todos` | ✅ | — | 不显示待办阶段 |
| `login` | ✅ | — | 模型页「登录」区与 `chat:open_url` 抽屉不渲染 |
| `fragments` | ✅ | — | 收 `rpc_chunk` 做分片重组 |
| `queue_edit` | ✅ | — | 排队条目可逐条移除（`remove_queued_message`）；pi 没有这条命令 |
| `queue_clear` | — | ✅ | 一次性清空排队（`clear_queue`）；omp 的命令表里没有这条 |
| `live_steer_count` | ✅ | — | 不读 `queue_update.liveSteered` |
| `skill_toggle` | ✅ | — | 技能行不显示开关（pi 走另一条路，见 §6） |
| `extensions` | ✅ | ✅ | 插件页可管理 |
| `export_html` | ✅ | ✅ | 会话可导出 |
| `slash_commands` | ✅ | — | pi 只能主动拉 `get_commands`，不订阅推送帧 |

## 3. 事件差分表（只列不一致的）

| 语义 | omp 帧 | pi 帧 | 差异 |
|---|---|---|---|
| 本轮结束 | `agent_end{yielded}` | `agent_end{willRetry}` + `agent_settled` | omp 看 `yielded===true` 收尾；pi 的 `agent_end` 只更新投影，**收尾在 `agent_settled`** |
| 整段静默 | `session_settled` | `agent_settled{aborted}` | 名字不同 |
| 增量消息 | `message_update{messageId, assistantMessageEvent}` | `message_update{assistantMessageEvent}` | pi 无 `messageId`，按 `contentIndex` 归并 |
| 工具内流 | `tool_stream_update` | `bash_execution_update`（仅 bash） | 都归到 `Translator.ToolDelta` |
| 压缩 | `auto_compaction_start/end` | `compaction_start/end{reason}` | 名字不同，pi 多 `reason` |
| 队列 | `queue_update{steering,followUp,liveSteered}` | `queue_update{steering,followUp}` | pi 无 `liveSteered` |
| 会话标题 | `session_info_update` | `session_info_changed{name}` | 名字与字段都不同，事件名统一为 `session:info` / `session:title` |
| 思考档 | `thinking_level_changed{thinkingLevel,configured,resolved}` | `thinking_level_changed{level}` | 字段不同 |
| 提示结果 | `prompt_result{status,settled}` | 无 | pi 只能靠 `agent_settled` 推断 |
| 命令清单 | `available_commands_update`（推） | `get_commands`（拉） | pi 不推 |
| 会话条目 | 无 | `entry_appended` | → `session:entry` |

> 翻译层纪律：**未知帧一律忽略，不报错**。这样新版引擎加帧不会让旧界面起不来。

## 4. 会话文件：一套读取代码 + 3 点适配

两个引擎的会话文件是**同源不同代**，一套代码可读，但必须处理三处：

1. **首行可能不是头**。omp 首行是 256 字节固定宽度的 `title` 槽位
   （`OMP .../session-title-slot.ts:84`），第一行 `type==="title" && v===1`；
   pi 首行就是 `{type:"session",version:3,…}`（`PI .../session-manager.ts:41-49`）。
   适配：读到 `title` 槽位就取 `title` 覆盖头部标题，然后继续读下一行当头部。
2. **未知条目跳过而不是报错**。omp 有 16 种条目、pi 有 11 种，各有私有类型
   （omp: `model_usage` / `title_change` / `mode_change` / `ttsr_injection` …；
   pi: `usage` / `session_info` / `custom_message` / `context_edit` / `label` …）。
   只认我们需要的几种，其余忽略。
3. **`model_change` 字段不同**。pi `{provider, modelId}`，omp `{model:"provider/id", role}`，
   归一化成 `(provider, modelId)` 后用于重建「历史里换过哪些模型」。

目录编码差异（影响「按工作目录列会话」）：

| | 规则 |
|---|---|
| pi | 恒为 `--<绝对路径，`/` `\` `:` 换成 `-`>--` |
| omp | 三段式：home 下 `-<相对>`，temp 下 `-tmp-<相对>`，其余 `--<绝对>--` |

**因此不能自己拼目录名去猜**。可靠做法：遍历 `sessions/` 下所有子目录，
读每个文件头部拿 `cwd`，再按 `cwd` 分组。代价是一次头部扫描，换来对两个引擎、
两种编码规则同时成立。

## 5. 版本与种类探测（必须可靠，不能靠文件名）

```
1) <bin> --version            // 5s 超时
   ^[A-Za-z][\w.-]*/\d+\.\d+\.\d+  → omp（形如 omp/18.13.1）
   ^\d+\.\d+\.\d+(-[\w.]+)?$       → 疑似 pi
2) 疑似 pi 时复核 <bin> --help
   含 "USAGE" 或 " v<version>"  → 其实是 omp（omp 遗留 launch 子命令会打裸版本号）
3) 都没有结论就不猜：界面显示「版本未知」，仍允许显式指定路径
```

补充判据（可交叉验证，不能单独使用）：

- 目录存在性：`~/.omp/agent` vs `~/.pi/agent`（用户可能只装了一个，但两个目录都有残留）
- 运行期自证：omp 会发首帧 `ready`；pi 不会。**pi 侧必须给「握手」设超时**

**探测很贵**：`omp --version` 约 100ms，`pi --version` 约 350ms 且 `pi --help` 要起一次
Node（约 3.5 秒）。所以探测结果缓存一分钟，两个引擎并发探测，装配阶段不探、只探测
第一次要显示时探。强制重探走 `POST /engines/detect`。

## 6. 技能发现（两套规则）

| | omp | pi |
|---|---|---|
| 文件名 | `SKILL.md` | `SKILL.md` |
| 用户目录 | `~/.omp/agent/skills/` | `~/.pi/agent/skills/` |
| 托管目录 | `~/.omp/agent/managed-skills/` | — |
| 项目目录 | 工作区逐级 `.omp/skills/` | 工作区 `.pi/skills/` |
| 插件目录 | `~/.omp/plugins/node_modules/*/skills` | `~/.pi/agent/npm/node_modules/*/skills` |
| frontmatter | `name` / `description` / `globs` / `alwaysApply` / `enabled` | `name` / `description` / `disable-model-invocation` |
| 启停 | `enabled: false` | `disable-model-invocation: true` |

界面同构，只是来源标签与开关语义按引擎填充。

## 7. 子代理（仅 omp）：观测台的全部依据

**omp 没有编排器。** 多代理 = 模型自己调 `task` 工具 spawn 子代理。
宿主能做的只有四件事：

| 目标 | 手段 |
|---|---|
| 有没有子代理在跑 | `set_subagent_subscription{level:"progress"}`（默认 `off`） |
| 权威名单 | `get_subagents` → `RpcSubagentSnapshot[]`：`id/index/agent/agentSource/description/status/task/assignment/sessionFile/lastUpdate/progress/parentToolCallId` |
| 进度 | `subagent_progress.progress`：`completionPercent/stage/currentTool/cost/tokens/contextTokens/contextWindow/durationMs` |
| 子代理对话 | `get_subagent_messages{subagentId,fromByte}` → 字节游标增量 |
| 干预 | `steer_subagent{id,message}` / `cancel_subagent{id}`（`false` 是幂等答案） |

必须处理的三处坑：

1. **`set_subagent_subscription` 默认 `off`** —— 不显式打开，界面永远是空的。
2. **会话切换会静默清空 roster**。`new_session` / `switch_session` / `fork` 成功后
   引擎内部 `clear()`，旧 id 进 256 条墓碑表，之后的帧被丢弃。我们必须在这几条命令
   成功后**主动清空本地 roster 与字节游标**。
3. **帧不是可靠流**。终态 `subagent_lifecycle` 到达时该 id 同时被移出 roster，
   所以「终态帧」可能是我们最后一次见到它。开局与每次 `prompt` 之后都要对账一次。

## 8. 不做的三件事（明确写死）

| 不做 | 原因 |
|---|---|
| 自造团队编排层 | omp 没有 spawn RPC，pi 连子代理都没有；自造一层会和模型自己的 `task` 调用重复且互相踩 |
| 帮用户装引擎 / 改 PATH / 写注册表 | 提权把一个只读代理变成系统安装器；装不装、装哪里是用户的事 |
| 手写引擎的 SQLite / blobs | 多写者库 + 内容寻址存储，一次写坏是静默的 |
