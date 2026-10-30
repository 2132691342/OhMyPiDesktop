# 技能与插件（skill / plugin）

## 定位

技能：把引擎真正会加载的 `SKILL.md` 找出来，允许启停与新建。
插件：列表与装卸全部走引擎自己的 CLI，**我们不解析 `node_modules`**。

两者共同的纪律：**发现规则属于引擎，我们只复述它的结论**。自己发明一套发现规则，
必然与引擎的结论不一致，表现是「界面上有、终端里没有」。

## 设计

```
skill
├── Roots     // 调用方填的发现根：用户 / 托管 / 项目（每一级）/ 插件目录
├── Scan      // 一份扫描逻辑：<dir>/<name>/SKILL.md，同名后者覆盖前者
│   └── parseSkill  // frontmatter（name / description / enabled / globs / …）
└── Toggle / TogglePi  // 只改启用键一行，原子写（omp 改 enabled，pi 改 disable-model-invocation）

plugin（读取在 env 层，用例在 service 层）
├── 列：读引擎自己的登记清单
│   ├── pi : ~/.pi/agent/settings.json 的 packages 数组
│   └── omp: ~/.omp/plugins/package.json 的 dependencies
└── 装 / 卸：代跑引擎 CLI
    ├── omp: omp plugin install / uninstall
    └── pi : pi install / remove
```

MCP 独立成篇，见 [mcp.md](mcp.md)。

### 一套扫描 + 不同的根

`skill.Scan(Roots)` 的扫描逻辑只有一份（`<dir>/<name>/SKILL.md`，同名后者覆盖前者），
不同的是**传进去的根**与**启停写哪个键**。根由 `env.Layout` 决定：

| | omp | pi |
|---|---|---|
| 用户级 | `~/.omp/agent/skills` | `~/.pi/agent/skills` |
| 托管级 | `~/.omp/agent/managed-skills` | —（`ManagedDir` 为空，不扫） |
| 项目级 | 工作区逐级向上找最多 8 层：`.omp/skills` | 同左：`.pi/skills` |
| 插件级 | `~/.omp/plugins/node_modules/*/skills` | `~/.pi/agent/npm/node_modules/*/skills` |

同名覆盖顺序：`project → user → managed → plugin`（后者覆盖前者），
与引擎自己的优先级一致。

### 启停：两个引擎的键不同

| | 键 | 语义 |
|---|---|---|
| omp | `enabled: false` | 引擎不加载这条技能 |
| pi | `disable-model-invocation: true` | 引擎不把技能放进系统提示，但仍可 `/skill:name` 显式调用 |

`skill.Toggle` 改 omp 的键，`skill.TogglePi` 改 pi 的键。
**不假装两家有同一个开关**：pi 没有「彻底禁用」的概念，界面如实呈现。

## 契约

```go
type Skill struct {
    Name        string
    Description string
    Path        string   // SKILL.md 绝对路径
    Dir         string
    Source      string   // "user" | "project" | "plugin" | "managed"
    Globs       []string
    AlwaysApply bool
    Hidden      bool
    Enabled     bool
    Editable    bool     // 只有 user 可改可删
}

func Scan(roots Roots) ([]Skill, error)      // Roots 见「一套扫描」一节
func Toggle(path string, enabled bool) error  // omp：enabled
func TogglePi(path string, enabled bool) error // pi：disable-model-invocation
func Create(rootDir, name, description, body string) (string, error)
func DeleteByPath(path string) error
func Content(path string) (string, error)
func SaveContent(path, text string) error     // 只认 SKILL.md；256 KB 上限；原子写
```

正文编辑是一条独立用例：`SkillContent` 读原文（含 frontmatter，界面直接显示），
`SaveSkillContent` 先验归属（只允许用户技能目录，工作区与插件来的那份跟着
仓库或包版本走）再调 `skill.SaveContent`——只认文件名 `SKILL.md`，
正文上限 256 KB，原子写（临时文件 + rename：写到一半断电会留下半个
SKILL.md，而引擎启动时一定会读它）。

插件的读取在 `env` 层，视图形态在 `domain`：

```go
// backend/env/pluginreg.go
func ReadPluginEntries(kind string, layout Layout) ([]PluginEntry, string, error)
func ReadPluginDetail(modulesDir string, entry PluginEntry) PluginDetail
func ParsePackageSource(source string) PackageSpec

// backend/domain/plugin.go
type PluginRowVO struct {
    EngineKind    string   // omp / pi
    PluginName    string
    PluginVersion string
    PluginSource  string   // 清单里那一行的原文；卸载要原样交回给引擎
    PluginPath    string   // 空表示目录不在盘上
    PluginDesc    string
    ResourceKinds []string // extensions / skills / prompts / themes
    ScopeKind     string   // user / project
    Enabled       bool
    Installed     bool     // 目录是否存在
}
```

`Editable` 是**关键字段**：工作区/插件/受管目录的技能一律 `false`，
前端据它把开关灰掉并给原因。**不要在前端重新推导目录归属**——那是第二套规则。

## 关键流程

### 启停

```
Toggle(path, enabled):
  1. 校验 path 在「用户技能根」下（不允许改工作区/插件/受管）
  2. 读文件，定位 frontmatter（首个 --- 到第二个 --- 之间）
  3. 有 enabled: 行 → 替换该行
     没有      → 在 frontmatter 末尾插入 enabled: <bool>
  4. 原子写（临时文件 + rename）
  5. 失败 → 返回原文，界面把开关拨回去
```

**为什么改字段而不是移目录**：移目录会让引擎的发现缓存键变化，
也让「我明明装过」的困惑变多；`enabled: false` 是引擎自己认的开关。
pi 的 `TogglePi` 同理：关 = 写 `disable-model-invocation: true`，开 = 删掉这个键
（不写这个键就是默认的「让模型看见」）。

### 新建

```
Create(name, description, body):
  1. name 校验：非空、不含 `\/:*?"<>|`（路径分隔符与 Windows 非法字符）；
     大写、空格、点都放行
  2. 目标 = <用户技能根>/<name>/SKILL.md，必须不存在
  3. 写 frontmatter（name / description，不写 enabled——默认就是启用）+ 正文；
     正文为空时写一份默认模板（标题 + 什么时候用 + 步骤）
  4. 不回写任何索引文件
```

### 插件的「列」为什么读文件

pi 的 `list` 子命令不接受 `--json`，也没有稳定的机器可读输出。
用 CLI 取列表的话，命令失败就会让整个分支被跳过——**界面显示为空，
而用户明明装了十几个包**。

读登记清单不依赖子命令的参数稳定性，也不必为每次列表起一次
Node 子进程（约 3 秒）。清单与 CLI 的关系是：

| 动作 | 走哪条路 | 理由 |
|---|---|---|
| 列 | 读登记文件 | 快、不依赖 CLI 参数、不把间接依赖当插件 |
| 装 | 代跑 CLI | 引擎要装包、要写自己的锁文件 |
| 卸 | 代跑 CLI，传**清单里那一行原文** | pi 的 `packages` 记的是 `npm:name@1.2.3`，只给裸名匹配不到 |

登记清单与包目录可能不一致（换机器、手删目录、包管理器只删了目录）。
这时 `Installed=false`，界面给「重装」而不是「卸载」。

### 插件装卸

```
omp: omp plugin install <spec>
     omp plugin uninstall <name>
pi : pi install <spec> / pi remove <name>
```

**长任务**：安装可能要几十秒。按钮进 loading（锁尺寸），命令的合并输出随响应返回，
完成或失败都刷新列表。装卸有**固定上限**：安装 10 分钟、卸载 3 分钟
（`runText` 的 timeout 参数），叠加请求自己的 context 取消——卡死的 CLI 不能
永久占位，用户取消也要立即生效。

## 约束

| 约束 | 原因 |
|---|---|
| 只改启用键那一行 | 别的内容属于用户与引擎 |
| `Editable` 由后端判定 | 前端不该重新推导目录归属 |
| 删除只允许用户目录 | 删掉插件带的技能会破坏插件 |
| 新建不覆盖已存在 | 覆盖等于无声毁掉用户写的技能 |
| 插件装卸走引擎 CLI | 我们不解析 `node_modules`，不造第二套真相 |
| 不写受管技能目录 | 那是 auto-learn 的领地 |
| 插件装卸有固定上限（装 10 分钟 / 卸 3 分钟） | 卡死的 CLI 不能永久占位；请求取消仍然立即生效 |

## 取舍

| 取舍 | 代价 | 收益 |
|---|---|---|
| 两套发现规则 | 两份代码 | 各自与引擎结论一致 |
| 只按 `Editable` 开放 | 用户不能在工作区技能上点开关 | 不会出现「点了被引擎忽略」的假开关 |
| 插件列表读登记文件 | 依赖引擎的清单格式（两处各一份） | 不受 CLI 参数变动影响，不把间接依赖当插件 |
| 新建只写 SKILL.md | 没有模板库 | 结构简单，用户能直接用编辑器改 |
| 装卸固定上限叠加可取消 | 要多一层 context 传递 | 卡死有底线，取消立即生效，时限内的正常安装不误判 |
