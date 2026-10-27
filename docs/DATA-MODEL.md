# 数据归属

一句话：**引擎拥有它自己的一切，我们只在它的账本上读，只在自己的表里存界面偏好。**

再存一份会话或用量，就是两本账；两本账迟早分叉，表现是「界面上有、终端里没有」。

## 两个引擎的账本在不同位置

| 内容 | omp | pi |
|---|---|---|
| 配置主文件 | `~/.omp/agent/config.yml`（YAML） | `~/.pi/agent/settings.json`（JSON） |
| 模型 | `~/.omp/agent/models.yml`（YAML） | `~/.pi/agent/models.json`（JSON） |
| 凭据 | `~/.omp/agent/agent.db`（SQLite） | `~/.pi/agent/auth.json`（0600，`api_key` / `oauth` 两类） |
| MCP | `~/.omp/agent/mcp.json` + `.omp/mcp.json` | `~/.pi/agent/mcp.json` + `.pi/mcp.json` |
| 技能 | `~/.omp/agent/skills/<name>/SKILL.md`（9 级发现链） | `~/.pi/agent/skills/`、`.pi/skills/` |
| 插件 / 扩展 | `~/.omp/plugins/`（npm 结构 + lock） | `~/.pi/agent/npm/`（`pi install` 装）+ `settings.json` 的扩展源 |
| 会话 | `~/.omp/agent/sessions/<编码cwd>/*.jsonl` | `~/.pi/agent/sessions/<编码cwd>/*.jsonl` |
| 提示历史 | `~/.omp/agent/history.db` | 无 |

**这些一律不手写**，除了下表明确标注的三处。

## 属于引擎（我们只读，除了显式标注的写）

| 内容 | 我们写吗 | 写的方式 |
|---|---|---|
| 服务商与模型 | **写** | omp：YAML 节点级编辑（`backend/env/ompcfg`）；pi：JSONC 解析 + 只覆盖掌管的 provider 键（`backend/env/picfg`） |
| 引擎设置主文件 | **几乎不写** | 只有「引擎目录」「分发源」这类我们自己的项才写，且写之前先读 |
| 会话 | **只删，不改内容** | 成对删 `.jsonl` + 同名 artifacts 目录 |
| 凭据 | 不读不写 | omp 走 RPC 登录；pi 提示用户跑 `pi auth` |
| 提示历史 / 会话标题索引 | 不写 | omp 的 `history.db` 只读快路径可选 |
| 插件 / 扩展 | 走 CLI | omp：`omp plugin install/uninstall`；pi：`pi install/remove` |
| 技能启停 | **写 frontmatter** | omp 改 `enabled`；pi 改 `disable-model-invocation`（它没有 enabled）。都不移目录 |

### 为什么改 omp 的配置用 YAML 节点编辑

`config.yml` / `models.yml` 里还有 omp 与用户写的其它键（`modelRoles` / `presets` /
项目级覆盖 / 注释）。整体重写会把这些抹掉，而用户看到的现象是「我设的偏好不见了」。
所以我们用 `yaml.Node` 只改要改的那个键。

### 为什么 pi 的 JSON 只改掌管的键

同一道理。pi 的 `settings.json` 里有 `packages` / `cacheWarming` / `codemode` 等
我们不认识的键，解成 map 再整体写回会把它们连同格式一起改掉。
做法是两级 `map[string]json.RawMessage`：顶层其余键原样保留，只替换我们管的那个。

### 为什么技能的启停是改字段而不是移目录

移目录会让引擎的发现缓存键变化，也让「我明明装过」的困惑变多。改 frontmatter
用的是引擎自己认的开关，改它等于用引擎的语言说话。

**两个引擎的开关不是同一个键**：omp 认 `enabled: false`；pi 只有
`disable-model-invocation: true`，语义是「不让模型自己调用，但仍可 `/skill:name` 显式用」。
所以 pi 的「关闭」不能让技能彻底消失——那是引擎设计的差异，界面如实呈现。

## 属于本应用

| 内容 | 位置 | 用途 |
|---|---|---|
| 界面设置 | `%LOCALAPPDATA%\OhMyPiDesktop\settings.json` | 主题 / 语言 / 字号 / 减少动效 / 上次工作目录 / **当前引擎** / 各引擎显式路径 |
| 日志 | 进程内环形缓冲（最近 500 行） | 设置页「日志」，进程重启即清 |

**设置就是一个 JSON 对象**：

```json
{
  "app.engine": "pi",
  "ui.lang": "zh-CN",
  "ui.theme": "dark"
}
```

为什么不用数据库：这里存的全是「删了不丢任何对话」的东西——二三十个键、
没有查询、没有关联、没有并发写者。为此背一个 SQLite（CGO 或纯 Go 移植几百 KB）
不划算。JSON 还有个附带好处：用户出问题时能直接打开看、改、备份。

写入纪律：内存读、落盘写；落盘走「临时文件 + Rename」，避免断电留下半个 JSON。
键排序后序列化，让文件对人可读、对 diff 友好。空值等于删除。
文件损坏时**改名留档为 `settings.json.broken` 并用空设置继续**——绝不能因为
设置坏了就让应用起不来。

设置文件损坏时会被改名为 `settings.json.broken` 留档，应用用空设置继续启动——
设置坏了不该让应用起不来。

`key` 常量集中在 `backend/domain/model.go` 的 `Key*`。当前用到的：

| 键 | 含义 | 取值 |
|---|---|---|
| `ui.theme` | 主题 | `light` / `dark` / `system` |
| `ui.lang` | 界面语言 | `zh-CN` / `en` |
| `ui.font_scale` | 字号缩放 | `0.9` ~ `1.3` |
| `ui.reduce_motion` | 减少动效 | `1` / `0` |
| `app.workspace` | 上次用的工作目录 | 绝对路径 |
| `app.recent_workspaces` | 最近用过的目录（JSON 数组，最多 12 条，新的在前） | `WorkspaceRow[]` |
| `app.engine` | 当前选中的引擎 | `omp` / `pi` |
| `engine.path.omp` | omp 的显式路径（空=自动探测） | 绝对路径 |
| `engine.path.pi` | pi 的显式路径（空=自动探测） | 绝对路径 |
| `app.last_session.omp` / `.pi` | 上次打开的会话（按引擎分开） | 会话文件路径 |
| `app.profile` | omp 的 profile 名（pi 无此概念） | 目录名 / 空 |

### 为什么「最近工作目录」要单独存一份

它有两个来源，缺一个都不对：

| 来源 | 特点 | 缺了会怎样 |
|---|---|---|
| `app.recent_workspaces`（我们记的） | 切目录时写入，读取不用扫盘 | 没有它，首屏要扫几百个会话文件头才知道「以前在哪干过」 |
| 会话文件里的 `cwd`（推出来的） | 覆盖「在别处聊过、这里还没切过」的目录 | 没有它，换台机器就一条都没有 |

取并集后按 12 条截断。**已经在磁盘上消失的目录直接丢掉而不是标灰**：
拔掉的 U 盘、删掉的项目永久占着列表，用户点下去只会见到一个「切不过去」的报错。

### 为什么不把这些也塞进引擎配置

引擎的配置文件属于引擎，它会被引擎自己迁移、被用户手动编辑。
界面偏好（比如字号）写进去，等于把我们的概念塞进别人的 schema。

### 为什么显式路径要按引擎分开存

用户可能 omp 装在 `C:\tools\omp.exe`、pi 走 npm 全局在
`%APPDATA%\npm\pi.cmd`。共用一个 `engine.exe` 键会让切换引擎时路径互相覆盖。

## 会话文件的读法

两个引擎的会话文件同源不同代，**一套读取代码 + 三点适配**：

```
<编码cwd>/<时间戳>_<uuid>.jsonl
├─ omp 第 1 行：type=title     256 字节固定宽度标题槽（pi 没有这一行）
├─ 头部：      type=session    version / id / timestamp / cwd / title? / parentSession?
└─ 其余：条目                 每条都有 {type, id, parentId, timestamp}
```

三点适配：

1. **首行可能是 title 槽位**：`type==="title" && v===1` 时取 `title/source` 覆盖头部标题，
   然后下一行才是头部。
2. **未知条目跳过不报错**：omp 16 种、pi 11 种，各有私有类型。只认我们需要的几种。
3. **`model_change` 字段不同**：pi `{provider, modelId}`，omp `{model:"provider/id", role}`，
   归一化成 `(provider, modelId)`。

### 目录编码不能自己拼

| | 规则 |
|---|---|
| pi | 恒为 `--<绝对路径，分隔符换 `-`>--` |
| omp | 三段式：home 下 `-<相对>`、temp 下 `-tmp-<相对>`、其余 `--<绝对>--` |

可靠做法是**遍历所有子目录，读头部拿 `cwd`，再按 `cwd` 分组**。
代价是一次头部扫描，换来对两个引擎、两种编码规则同时成立。

### 我们用到的条目

| `type` | 用途 |
|---|---|
| `title` | omp 的列表标题（槽位） |
| `session` | cwd、id、父会话 |
| `session_info` | pi 的标题 |
| `message` | 消息本体；`role` 取 `user` / `assistant` / `toolResult` / `bashExecution` |
| `model_change` | 换过的模型（两引擎字段不同，归一化后使用） |
| `thinking_level_change` | 思考档 |

消息里的用量：`message.usage = {input, output, cacheRead, cacheWrite, totalTokens, cost:{…total}}`。
列表页把每条助手消息的 `totalTokens` 与 `cost.total` 累加，就是这条会话的用量——
不另建用量表。

### 读的纪律

- 列表只读头两行 + 逐行累加用量，不整文件解析（一个会话可能几 MB）
- 按 `(size, mtime)` 缓存，文件没变不重读
- 文件里 `blob:sha256:…` 这类外置图片引用不解析，原样留在块里

## 删除会话：必须成对删

```
sessions/<编码cwd>/<ts>_<id>.jsonl     ← 会话正文
sessions/<编码cwd>/<ts>_<id>/          ← artifacts（子代理 transcript、patch、draft）
```

只删 `.jsonl` 会留下一个孤儿目录，越积越多。只删目录会留下一个指向不存在 artifacts 的会话。

**删除前必须确认**：

1. 目标不是当前进程正在用的 `sessionFile`
2. 该会话空闲（omp 有跨进程写租约，被别的实例持有时我们会看到文件被改名成兄弟文件）
3. 有子代理在跑时不许删它所属的会话

## 一次写入的边界

| 动作 | 我们写什么 | 我们不写什么 |
|---|---|---|
| 新增服务商 | `models.yml`（omp）/ `models.json`（pi）的一个 provider 键 | `modelRoles` / `presets` |
| 改技能启停 | `SKILL.md` 的 `enabled` 行 | 目录结构 |
| 装插件 | 交给引擎自己的 CLI | `node_modules` 里的任何文件 |
| 删会话 | 那个 `.jsonl` 与同名目录 | 别的会话 |
| 引擎二进制 | **完全不写** | 不装、不升级、不打补丁 |

## 备份与恢复

要备份用户状态，只需两处（引擎自己的账本由引擎负责）：

1. `%LOCALAPPDATA%\OhMyPiDesktop\settings.json`（界面偏好，删了不丢任何对话）
2. 引擎各自的目录：`~/.omp/`、`~/.pi/`（删了等于换一台干净的机器）
