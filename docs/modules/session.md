# 会话（session）

## 定位

把引擎写在磁盘上的会话文件，变成「能列、能看、能删」的东西。它**不写**会话内容。
改名走 RPC，因为格式是引擎的私有契约。

两个引擎的会话文件同源不同代，**一套读取代码 + 三点适配**——这是本包存在的核心理由。

## 设计

```
backend/session/store.go
├── Index.List    // 遍历 sessions/ 下所有子目录，按头部 cwd 过滤
├── Index.metaOf  // 读头部（含 omp 的 256B title 槽位、pi 的 session_info）
├── Index.Detail  // 投影出「消息 + 工具结果配对」的条目
└── cache         // 按 (path, size, mtime) 缓存头部与累计
```

`Index` 带 `Kind` 字段：同一个 `sessions/` 目录只属于一个引擎，
但上层是 `map[engine.Kind]*Index`——两个引擎各有一份索引，互不干扰。

### 为什么不按目录名猜 cwd

| | 目录编码 |
|---|---|
| pi | 恒为 `--<绝对路径，分隔符换 `-`>--` |
| omp | 三段式：home 下 `-<相对>`、temp 下 `-tmp-<相对>`、其余 `--<绝对>--` |

两套规则、还有转义歧义（路径里本来就有 `-` 怎么办）。可靠做法只有一条：
**遍历所有子目录，读每个文件头部拿 `cwd`，再按 `cwd` 分组**。
代价是一次头部扫描（只读前两行），换来对两个引擎、两种编码同时成立。

`slugToPath` 仍然存在，但只用于**展示**（头部没有 `cwd` 时的兜底），
从不参与「这条会话属于哪个目录」的判断。

### 标题从哪来（两处不同）

| | 来源 | 代码位置 |
|---|---|---|
| omp | 首行 256B 固定宽度的 `title` 槽位 | `metaOf` 的 `case "title"` |
| pi | `session_info` 条目的 `name`（取最新一条，空串=清除） | `metaOf` / `Detail` 的 `case "session_info"` |

都没有标题时回落到 `会话 · <model>`。**不自己编标题**：
编出来的标题会让「终端里看到的」与「这里看到的」不一致。

## 契约

```go
func NewIndex(sessionsDir string, kind engine.Kind) *Index
func (ix *Index) List(workspace string) ([]Session, error)
func (ix *Index) Detail(path string) (*Detail, error)

type Session struct {
    ID        string    `json:"id"`
    Path      string    `json:"path"`
    Title     string    `json:"title"`
    CWD       string    `json:"cwd"`
    WorkDir   string    `json:"work_dir"` // 目录 slug 还原出的路径
    Model     string    `json:"model"`
    UpdatedAt time.Time `json:"updated_at"`
    Size      int64     `json:"size"`
    Messages  int       `json:"messages"`
    Tokens    int       `json:"tokens"`
    Cost      float64   `json:"cost"`
}

type Detail struct {
    Session
    Thinking string `json:"thinking"` // 最近一次 thinking_level_change
    Items    []Item `json:"items"`
}

type Item struct {
    Kind      string            `json:"kind"`    // user / assistant / tool / system
    Role      string            `json:"role"`
    Text      string            `json:"text,omitempty"`
    Thinking  string            `json:"thinking,omitempty"`
    Blocks    []json.RawMessage `json:"blocks,omitempty"`
    Model     string            `json:"model,omitempty"`
    ToolName  string            `json:"tool_name,omitempty"`
    ToolCall  string            `json:"tool_call_id,omitempty"`
    IsError   bool              `json:"is_error,omitempty"`
    Timestamp int64             `json:"timestamp,omitempty"`
    Cost      float64           `json:"cost,omitempty"`
    Tokens    int               `json:"tokens,omitempty"`
}
```

## 关键流程

### 三点适配

```
读第一行
  ├─ type=="title" && v==1   → omp：取 title/source，继续读下一行当头部
  └─ 其它                     → pi ：这一行就是头部
读头部（type=="session"）
  ├─ version 不解析、不校验（引擎升 major 时旧会话照样能列能看）
  ├─ cwd / id / timestamp / parentSession
  └─ title / titleSource（omp 头部也可能带）
逐行读条目
  ├─ 只认 session / message / model_change / thinking_level_change / session_info
  ├─ 其它 type 一律跳过（不报错、不警告）
  └─ model_change 归一化：
       pi  {provider, modelId}
       omp {model:"provider/id", role}
       → (provider, modelID)

会话版本号（pi 的 `version`、omp 的 `__METADATA__` 之类）一律不解析、不校验：
读取只认 `type` 字段，引擎升 major 时旧会话照样能列能看，只是拿到引擎当
时写出的字段，读不到的字段回落到默认值（标题回落到 `会话 · <model>`）。
```

### 列会话

```
Index.List(workspace):
  遍历 <sessionsDir>/*/                   // 一个子目录 = 一个编码后的 cwd
    对每个 *.jsonl:
      查缓存 (path, size, mtime) 命中 → 用缓存
      否则读头部 + 逐行累加
  workspace 非空时按 header.cwd 过滤（不做目录名匹配）
  按 mtime 倒序
```

`service.Sessions(kind, workspace)` 在 `kind` 为空时合并两个引擎的列表
再统一排序；`kind` 有值时只读那一份索引。

**缓存键必须是 `(path, size, mtime)` 三元组**：只看 mtime 在秒级分辨率下会漏掉
同一秒内的两次写入，表现为「刚聊完但列表还是旧的」。

### 删会话

```
引擎归属：按 path 落在哪个引擎的 sessions/ 下推断（不看文件名）
校验：
  1. path != 当前进程正在用的 sessionFile（且该引擎空闲）
  2. 该会话没有正在跑的子代理

删除：
  os.Remove(<path>)                 // 用户正在跑的会话，引擎自己会拦？不会，所以上面先判
  os.RemoveAll(<path 去掉 .jsonl>)   // artifacts 目录，可能不存在
  顺序不能反：先删正文，再删附件
  删完丢掉该文件的索引缓存（否则列表里还留着「已经删了」的会话）
```

**为什么先删正文**：万一只删掉附件而正文还在，用户看到的是一个「打不开的历史」；
先删正文，最坏情况是留下一个孤儿目录（gc 能清），不会给出坏会话。

### 读取纪律

| 场景 | 做法 |
|---|---|
| 列表 | 只读头部 + 逐行累加用量，不解析消息体 |
| 详情 | 只解析 `message` 条目，工具结果按 `toolCallId` 回填 |
| 大文件 | 逐行 `bufio.Scanner`，单行上限 8 MiB（超过就跳过该行并记日志） |
| 外置图片 | `blob:sha256:…` 原样留在块里，不解析 |
| 未知条目 | 跳过 |

## 约束

| 约束 | 原因 |
|---|---|
| 不写会话内容 | 格式是引擎的私有契约 |
| 不按目录名推 cwd | 两引擎编码规则不同，且有转义歧义 |
| 不解析会话版本号 | 读只认 `type`；引擎升 major 时旧会话照样能列能看，读不到的字段回落默认值，不会把新格式读成垃圾 |
| 未知条目跳过不报错 | 新版引擎会加类型 |
| 删必须成对（正文 + artifacts） | 否则越积越脏 |
| 删除前必须确认会话空闲 | 正在用的会话不能删 |
| 缓存键含 size | 秒级 mtime 会漏更新 |
| 单行上限 | 一个畸形文件不能把内存吃光 |

## 取舍

| 取舍 | 代价 | 收益 |
|---|---|---|
| 遍历目录读头部而不是拼目录名 | 列 500 个会话要开 500 个文件头 | 对两个引擎、任意路径都成立 |
| 一套代码 + 三点适配 | 适配点要写清（本文档就是） | 不写两套读会话的代码 |
| 只删正文+附件，不改内容 | 改名必须走 RPC（要引擎在跑） | 永远不会把引擎的文件写坏 |
| 列表不解析消息体 | 列表页看不到第一句用户消息 | 500 个会话的列表仍然秒开 |
| 不解析会话版本号 | 引擎升 major 时旧会话照样能列能看 | 读不到的字段回落默认值，不把新格式读成垃圾 |
