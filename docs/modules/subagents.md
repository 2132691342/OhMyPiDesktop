# 子代理观测台（仅 omp）

## 定位

把 omp 的 `task` 子代理变成**看得见、能干预**的面板。它**不做调度**。

这不是功能裁剪，是能力边界：omp 没有 `spawn_agent` 这类 RPC，
子代理只能由模型自己调 `task` 工具触发。宿主能做的是三件事——
**看见名单、看见进度、在跑偏时插一句话或掐掉它**。

自造一层「并行代理编排」会和模型自己的 `task` 批量调用重复且互相踩，
维护成本极高而没有协议支撑。所以本规格明确排除编排。

## 设计

```
service/agents
├── roster      // get_subagents 快照（权威）
├── progress    // subagent_progress 帧（增量）
├── transcript  // get_subagent_messages 字节游标
└── control     // steer / cancel
```

### 为什么需要两条数据源

| 源 | 特点 | 用途 |
|---|---|---|
| `get_subagents` | 拉取式、权威、有完整字段 | 对账（帧会丢） |
| `subagent_progress` | 推送式、高频 | 进度条、当前工具、成本 |

只靠推送会漏（终态帧之后该 id 就从 roster 消失）；
只靠拉取会卡（进度需要秒级更新）。**两者并用**：推送驱动 UI，
拉取在「开局 / 每次 prompt 之后 / 收到任意 lifecycle 之后」纠正。

### 为什么 transcript 用字节游标而不是事件流

`set_subagent_subscription{level:"events"}` 会把 N 个子代理的 `message_update`
全部灌进同一条 stdout，带宽随子代理数量线性增长。
而 `get_subagent_messages{subagentId, fromByte}` 让我们按需只拉「用户正在看的那个」。

## 契约

```go
type Agent struct {
    ID              string          `json:"id"`
    Index           int             `json:"index"`
    Name            string          `json:"agent"`
    Source          string          `json:"agent_source"`   // bundled|user|project
    Description     string          `json:"description"`
    Status          string          `json:"status"`         // pending|running|completed|failed|aborted
    Task            string          `json:"task"`
    SessionFile     string          `json:"session_file"`
    ParentToolCall  string          `json:"parent_tool_call_id"`
    LastUpdate      int64           `json:"last_update"`
    Progress        AgentProgress   `json:"progress"`
}

type AgentProgress struct {
    Completion  int      `json:"completion"`    // 0-100 自评
    Stage       string   `json:"stage"`
    CurrentTool string   `json:"current_tool"`
    RecentTools []string `json:"recent_tools"`
    ToolCount   int      `json:"tool_count"`
    Tokens      int64    `json:"tokens"`
    ContextUsed int      `json:"context_used"`
    ContextMax  int      `json:"context_max"`
    Cost        float64  `json:"cost"`
    DurationMS  int64    `json:"duration_ms"`
    Model       string   `json:"model"`
}

type TranscriptPage struct {
    Entries []json.RawMessage `json:"entries"`
    Messages []json.RawMessage `json:"messages"`
    Next    int64              `json:"next"`     // 下一次的 fromByte
    Reset   bool               `json:"reset"`    // true 时要清空本地重放
}
```

## 关键流程

### 打开观测

```
引擎就绪（omp）
  → set_subagent_subscription{level:"progress"}    // 默认是 off，不打开永远是空的
  → get_subagents                                   // 首次对账
```

**这一条不做，整个面板永远是空的**，而且没有任何报错提示。
它是本规格里最容易漏掉的一步。

### 收帧

```
subagent_lifecycle{id, status:"started"}     → 插入/更新 roster；拉一次 get_subagents
subagent_lifecycle{id, status:终态}          → 标记终态；该 id 即将从 roster 消失
subagent_progress{id, progress}              → 覆盖进度；把该 id 标脏
subagent_event{id, event}                    → 仅"events"档；直接转发给已打开的详情
```

### 拉 transcript

```
脏集合（每 500ms 合并一次）：
  for id in dirty:
      page := get_subagent_messages{id, fromByte: cursor[id]}
      if page.Reset:
          清空本地该 id 的 transcript
          cursor[id] = 0
          重拉
      else:
          追加 page.Entries / page.Messages
          cursor[id] = page.Next
```

**为什么 500ms**：子代理的一个工具调用可能产生几十条增量，
每条都拉就是几十次 IPC。500ms 合并后用户看不出延迟。

### 干预

```
steer:   steer_subagent{id, message}
         成功 = 已入队或已开新轮（不等轮次结束）
         失败 = "Subagent not running: <id>" / "refused: <reason>"
cancel:  cancel_subagent{id}
         cancelled:false 是幂等答案（未知/已结束/已取消），不是错误
```

### 会话切换必须清空

```
new_session / switch_session / branch / fork / open_session 成功后：
  roster = {}
  cursor = {}
  emit("agents:cleared")
```

引擎内部在这几条命令成功后会 `clear()` 并把旧 id 放进 256 条墓碑表，
之后到达的旧 id 帧会被丢弃。**我们不清空的话，界面会一直显示已经消失的子代理。**

## 约束

| 约束 | 原因 |
|---|---|
| 必须显式 `set_subagent_subscription` | 默认 `off` |
| 每次 prompt / lifecycle 后 `get_subagents` 对账 | 帧不是可靠流，终态只出现一次 |
| 不默认升到 `events` 档 | 带宽随子代理数线性增长 |
| transcript 用字节游标 | 增量、便宜、可按需只拉一个 |
| `reset:true` 必须清空重放 | 否则本地会比磁盘多出一截 |
| 会话切换必须清 roster 与游标 | 引擎已丢弃旧 id 的帧 |
| `cancelled:false` 当幂等 | 它覆盖「已结束但终态帧还没到」这个正常竞态 |
| pi 下整块不渲染 | pi 无此能力，不做假面板 |

## 取舍

| 取舍 | 代价 | 收益 |
|---|---|---|
| 只做观测，不做编排 | 不能「安排一个团队」 | 不与模型自己的 `task` 调用打架 |
| 双源（推 + 拉） | 两套代码 | 既不漏也不卡 |
| transcript 轮询而非订阅 | 有 500ms 延迟 | 带宽不随子代理数量爆炸 |
| 不默认开 `events` | 详情页的流式是「按游标轮询」的近似 | 一个跑 10 个子代理的会话不会把 stdout 打满 |
| `pi` 下不做模拟 | pi 用户少一块 | 不做一个会说谎的界面 |
