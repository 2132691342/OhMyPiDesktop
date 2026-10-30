# 会话（session）

## 定位

把引擎写在磁盘上的会话文件，变成「能列、能看、能删」的东西。它**不写**会话内容。
改名走 RPC，因为格式是引擎的私有契约。

两个引擎的会话文件同源不同代，**一套读取代码 + 三点适配**——这是本包存在的核心理由。

## 设计

```
backend/session/
├── store.go
│   ├── Index.List    // 遍历 sessions/ 下所有子目录，按 cwd 过滤
│   ├── Index.metaOf  // 逐行扫全文件：标题 / cwd / 模型 / tokens / cost / 条数 / 第一条用户消息
│   ├── Index.Detail  // 投影出「消息 + 工具结果配对」的条目
│   └── cache         // 按 (path, size, mtime) 缓存扫描结果，按 usedAt 淘汰（上限 512）
└── title.go          // 标题规则：第一条用户消息压成一行、40 字上限；无可用文本才兜底
```

`Index` 带 `Kind` 字段：同一个 `sessions/` 目录只属于一个引擎，
但上层是 `map[engine.Kind]*Index`——两个引擎各有一份索引，互不干扰。

### 为什么不按目录名猜 cwd

| | 目录编码 |
|---|---|
| pi | 恒为 `--<绝对路径，分隔符换 `-`>--` |
| omp | 三段式：home 下 `-<相对>`、temp 下 `-tmp-<相对>`、其余 `--<绝对>--` |

两套规则、还有转义歧义（路径里本来就有 `-` 怎么办）。可靠做法只有一条：
**遍历所有子目录，读每个文件拿 `cwd`，再按 `cwd` 分组**。
代价是逐行扫全文件（`metaOf` 除了 cwd 还要累计计费、取标题候选），换来对两个引擎、
两种编码同时成立。

`slugToPath` 还原出的路径平时只用于展示（`work_dir` 字段）；文件头部没有 `cwd` 时，
这个还原值顶替 cwd 参与归属过滤。slug 有歧义（真实路径里也可能有 `-`），
但比「无归属、哪个目录都筛不出来」好。

### 标题从哪来（两处不同）

| | 来源 | 代码位置 |
|---|---|---|
| omp | 首行 256B 固定宽度的 `title` 槽位 | `metaOf` 的 `case "title"` |
| pi | `session_info` 条目的 `name`（取最新一条，空串=清除） | `metaOf` / `Detail` 的 `case "session_info"` |

两个引擎默认都不写标题条目，所以列表里的标题大多由本包**自己编**
（`title.go`）：取第一条用户消息文本，把换行与连续空白压成单空格，
超过 40 字截断加省略号——第一句是用户自己写的，比模型名有用得多；
截到 40 是因为列表一行大约放得下，前 40 个字足够区分是哪条会话。
`会话 · <model>`（连模型都没有时是「未命名会话」）只是完全没有可用文本时
的最后兜底。

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
逐行扫全文件（omp 的第一行是 title 槽位、第二行才是 session 头；pi 第一行就是 session 头）
  ├─ version 不解析、不校验（引擎升 major 时旧会话照样能列能看）
  ├─ 只认这些 type，其它一律跳过（不报错、不警告）：
  │    title            omp 的固定宽度标题槽位；只判 type=="title"，不校验版本字段
  │    session          cwd 是「会话归属目录」的唯一可靠来源
  │    session_info     pi 的标题，取最新一条（空串 = 清除）
  │    message          角色 / 模型 / usage；第一条用户消息留给标题规则
  │    model_change     归一化：pi {provider, modelId}；omp {model:"provider/id"}
  │                     → provider/modelID
  │    usage            pi 的额外计费条目（与 message 内的 usage 不重叠），累计 tokens / cost
  └─ thinking_level_change 只在详情投影里认（记录最近一次的 thinkingLevel）

会话版本号（pi 的 `version`、omp 的 `__METADATA__` 之类）一律不解析、不校验：
读取只认 `type` 字段，引擎升 major 时旧会话照样能列能看，只是拿到引擎当
时写出的字段，读不到的字段回落到默认值（标题回落规则见「标题从哪来」）。
```

### 列会话

```
Index.List(workspace):
  遍历 <sessionsDir>/*/                   // 一个子目录 = 一个编码后的 cwd
    对每个 *.jsonl:
      查缓存 (path, size, mtime) 命中 → 用缓存
      否则 metaOf 逐行扫全文件
  workspace 非空时按 metaOf 得到的 cwd 过滤（不做目录名匹配；
  头部没有 cwd 时 cwd 取 slug 还原值参与过滤）
  按 mtime 倒序
```

`service.Sessions(kind, workspace)` 在 `kind` 为空时合并两个引擎的列表
再统一排序；`kind` 有值时只读那一份索引。

**缓存键必须是 `(path, size, mtime)` 三元组**：只看 mtime 在秒级分辨率下会漏掉
同一秒内的两次写入，表现为「刚聊完但列表还是旧的」。

### 删会话

```
引擎归属：按 path 落在哪个引擎的 sessions/ 下推断（不看文件名）
校验只有一条：
  目标正在跑（该引擎 Busy 且当前会话文件就是它）→ 拒绝（CodeSessionBusy）。
  引擎自己不会拦：它只管往文件追加，删了又写回来表现为「删了又回来」。

删除：
  os.Remove(<path>)
  os.RemoveAll(<path 去掉 .jsonl>)   // artifacts 目录，可能不存在
  顺序不能反：先删正文，再删附件
  Index.Invalidate(path)             // 丢索引缓存（否则列表里还留着「已经删了」的会话）
  InvalidateCounts()                 // 概览页的计数快照一并作废
  forgetHiddenTurns(path)            // 隐藏轮次账一并清（见下节）
```

**为什么先删正文**：万一只删掉附件而正文还在，用户看到的是一个「打不开的历史」；
先删正文，最坏情况是留下一个孤儿目录（gc 能清），不会给出坏会话。

### 隐藏轮次

界面上「删除一轮」不是删文件：引擎的会话账本一字不动，只是界面不再显示那一轮。

- 账本住在 `service/hidden.go`：按会话文件路径记 hid 列表，存进设置 KV
  （键统一小写，Windows 下大小写不同不会记成两本），启动时读回。
- hid 由 `service/history.go` 的 `markHidden` 生成：一轮的内容指纹（FNV）+ `#` +
  该指纹的出现序号。带序号是因为同一句话可能发两遍，只按内容指认会一次藏掉两份；
  压缩上下文后序号即使错位，错的也只是「两份一样的哪一份」。
- 历史投影返回前按账摘掉命中的轮次（`ProjectHistory` 的最后一步）。
- 本包的 `Detail` 不参与：它是会话文件的直接投影，仍然显示全量——这是有意的，
  对话页的历史投影按账摘除，会话详情页看得见。机制全貌见 [DATA-MODEL.md](../DATA-MODEL.md)。
- 删除会话时账一并清（`forgetHiddenTurns`）：账不留给不存在的会话。

### 读取纪律

| 场景 | 做法 |
|---|---|
| 列表 | 逐行扫全文件：累计 tokens / cost / 条数、记模型与 cwd、取第一条用户消息当标题候选；不投影消息正文 |
| 详情 | 只投影 `message` 条目，工具结果按 `toolCallId` 回填 |
| 大文件 | 逐行 `bufio.Scanner`，单行上限列表 8 MiB / 详情 32 MiB；超限会**终止整个文件的扫描**（Scanner 的行为），不是跳过该行——超长行之后的内容读不到 |
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
| 遍历目录读文件而不是拼目录名 | 列 500 个会话要把 500 个文件逐行读完 | 对两个引擎、任意路径都成立 |
| 一套代码 + 三点适配 | 适配点要写清（本文档就是） | 不写两套读会话的代码 |
| 只删正文+附件，不改内容 | 改名必须走 RPC（要引擎在跑） | 永远不会把引擎的文件写坏 |
| 列表逐行扫全文件但不投影正文 | 扫描是 O(文件大小)，大列表变慢 | 标题、tokens / cost / 条数都是真的 |
| 不解析会话版本号 | 引擎升 major 时旧会话照样能列能看 | 读不到的字段回落默认值，不把新格式读成垃圾 |
