# MCP（mcpcfg / service/mcp.go）

## 定位

MCP 服务器给引擎额外的工具。两个引擎在**这一处共用同一个文件形状**
（`mcp.json` 的 `mcpServers` 映射），所以读写只有一份代码。

差别只有两个：

| | omp | pi |
|---|---|---|
| 落点 | `~/.omp/agent/mcp.json`（profile 下是 `~/.omp/profiles/<n>/agent/mcp.json`） | `~/.pi/agent/mcp.json` |
| 停用一个服务器写哪个键 | 顶层 `disabledServers` 名单 | 条目上的 `enabled` |

项目级配置（`.omp/mcp.json` / `.pi/mcp.json`）只读不写：往一个随便打开的目录里
塞项目配置等于往别人的文件夹里写东西，所以「新增」只在那个目录已经被引擎
当成项目（存在 `.omp` 或 `.pi` 目录）时才允许。

## 设计

```
env/mcpcfg          // 文件的读、写、启停、删除。只改掌管的键
├── File            // mcpServers + disabledServers + enabledServers + Rest
├── Entry           // 已知字段 + Rest（headers / auth / oauth / timeout …）
├── Read / Write    // 未知键原样带回
├── Upsert / Remove
└── SetEnabled      // 按引擎分叉：名单 or 条目

service/mcp.go      // 用例：列（可带引擎实时状态）、存、启停、删
```

### 为什么写文件而不是转发 CLI

`pi mcp add` 的参数面比界面能表达的大得多：OAuth 客户端 id / secret、
回调端口、头部、 bearer 环境变量、暴露模式 ……
转发意味着引擎每加一个新参数，前端就要跟着改一遍。

而写文件只改掌管的那几个键（名字 / 连接方式 / 启停），
用户手配的 `headers`、`auth`、`oauth`、`timeout`、`toolExposure`
全部原样保留——**这比转发 CLI 更安全**。

代价是：OAuth 登录这类需要交互的动作，界面不做（交给引擎自己的 `pi mcp login`）。

### 启用的优先级

```
顶层 disabledServers > 顶层 enabledServers > 条目 enabled > 默认开
```

算在 `File.IsEnabled(name)` 一处。界面不排这三者的顺序——
排错的后果是「界面显示已启用、引擎根本不连」。

被名单压住时返回 `disabled_by_deny=true`，界面要说清为什么点不动，
而不是让用户以为开关坏了。

### 未知字段怎么保留

`Entry.Rest` 存 `map[string]json.RawMessage`。写回时先整个读成
`map[string]json.RawMessage`，只替换掌管的键，再整体序列化。

不解成 struct 再序列化——那样会把引擎明天新增的字段静默删掉。

## 契约

```go
// backend/env/mcpcfg
func Read(path string) (*File, error)
func Write(path string, f *File) error
func Upsert(path, name string, entry Entry) error
func Remove(path, name string) error
func SetEnabled(path, name string, enabled, useDenyList bool) error
func (f *File) IsEnabled(name string) (enabled, byDeny bool)
func (e Entry) Transport() string // 显式 type > url → http > command → stdio

// backend/domain/mcp.go
type McpServerVO struct {
    ServerName     string
    TransportKind  string   // stdio / http / sse
    CommandLine    string   // stdio 的命令 + 参数，给人看
    ServerURL      string
    Desc           string
    Enabled        bool
    DisabledByDeny bool
    ScopeKind      string   // user / project / extension
    ConfigPath     string
    LiveStatus     string   // 引擎报告的状态；只有探测时才有
    LiveError      string
    ToolNames      []string
}
```

## 关键流程

### 列（带实时状态）

```
McpList(kind, live):
  1. 读用户级 mcp.json
  2. 当前目录存在 .omp / .pi 时再读项目级，同名覆盖用户级
  3. live 且引擎是 pi → 跑 `pi mcp list --json`，把 state / tools / error 并到行上
  4. 引擎报了配置里没有的服务器（扩展注册的）→ 补一行，scope = extension
```

探测失败不算错误：配置照样给，状态栏留空。
**「引擎没起」不该让整页打不开。**

### 启停

```
McpToggle(kind, name, enabled):
  1. 服务器必须存在（不存在是 CodeNotFound，不是静默成功）
  2. omp → SetEnabled(..., useDenyList=true)
     pi  → SetEnabled(..., useDenyList=false)
  3. 返回整个清单（不是被改的那一行）
```

### 删除

删条目时连带把它从 `disabledServers` / `enabledServers` 里摘掉——
否则同名残留会变成一条永远匹配不到任何服务器的幽灵规则。

## 约束

| 约束 | 原因 |
|---|---|
| 未知字段原样带回 | 引擎的 MCP 配置项比界面能表达的多 |
| 服务器名 ≤ 100 字符 | 与引擎自己的校验一致 |
| 命令与 URL 至少要有一个 | 写一条连不上的条目只会让引擎启动报错 |
| 删除连带清名单 | 残留名单是幽灵规则 |
| 项目级只在 `.omp` / `.pi` 存在时可写 | 否则是往别人的目录里塞配置 |
| 探测失败不算错误 | 引擎没起是正常状态 |

## 取舍

| 取舍 | 代价 | 收益 |
|---|---|---|
| 写文件而不是转发 CLI | 覆盖不到 CLI 的全部参数 | 不随 CLI 参数变动漂移，未知字段不丢 |
| 不做 OAuth 登录 | 需要登录的服务器得去终端 | 交互流程属于引擎，界面不重造 |
| 启停分叉写在不同键 | `mcpcfg` 里有一个 bool 参数 | 界面只发「启 / 停」，不知道引擎差异 |
| `live` 单独一个按钮 | 用户要多点一次 | 探测要起子进程（几秒），不该跟着列表自动跑 |
