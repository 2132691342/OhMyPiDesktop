# 技能与插件（skill / plugin）

## 定位

技能：把引擎真正会加载的 `SKILL.md` 找出来，允许启停与新建。
插件：列表与装卸全部走引擎自己的 CLI，**我们不解析 `node_modules`**。

两者共同的纪律：**发现规则属于引擎，我们只复述它的结论**。自己发明一套发现规则，
必然与引擎的结论不一致，表现是「界面上有、终端里没有」。

## 设计

```
skill
├── Sources   // 两引擎各自的发现顺序
├── Parse     // frontmatter（name / description / enabled / …）
└── Toggle    // 只改 enabled 一行，原子写

plugin（在 service 层，无独立包）
├── omp: omp plugin list --json / install / uninstall
└── pi : 读 settings.json 的扩展源 + pi install / remove
```

### 一套扫描 + 不同的根

`skill.Scan(Roots)` 的扫描逻辑只有一份（`<dir>/<name>/SKILL.md`，同名后者覆盖前者），
不同的是**传进去的根**与**启停写哪个键**。根由 `env.Layout` 决定：

| | omp | pi |
|---|---|---|
| 用户级 | `~/.omp/agent/skills` | `~/.pi/agent/skills` |
| 托管级 | `~/.omp/agent/managed-skills` | —（`ManagedDir` 为空，不扫） |
| 项目级 | 工作区逐级 `.omp/skills` | 工作区 `.pi/skills` |
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

type Plugin struct {
    Name     string
    Version  string
    Source   string   // "npm" | "marketplace" | "settings"
    Location string
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

### 新建

```
Create(name, description, body):
  1. name 校验：小写字母/数字/连字符，无路径分隔符
  2. 目标 = <用户技能根>/<name>/SKILL.md，必须不存在
  3. 写 frontmatter（name / description / enabled: true）+ 正文
  4. 不回写任何索引文件
```

### 插件装卸

```
omp: omp plugin install <spec> --json
     omp plugin uninstall <name> --json
pi : pi install <spec> / pi remove <name>
```

**长任务**：安装可能要几十秒。按钮进 loading（锁尺寸），下方逐行追加 stdout，
完成或失败都刷新列表。**不要用固定时长超时**——大包会超；用 context 取消 +
用户可点的「取消」。

## 约束

| 约束 | 原因 |
|---|---|
| 只改 `enabled` 一行 | 别的内容属于用户与引擎 |
| `Editable` 由后端判定 | 前端不该重新推导目录归属 |
| 删除只允许用户目录 | 删掉插件带的技能会破坏插件 |
| 新建不覆盖已存在 | 覆盖等于无声毁掉用户写的技能 |
| 插件装卸走引擎 CLI | 我们不解析 `node_modules`，不造第二套真相 |
| 不写受管技能目录 | 那是 auto-learn 的领地 |
| 插件安装不用固定超时 | 网络与包大小不可预测 |

## 取舍

| 取舍 | 代价 | 收益 |
|---|---|---|
| 两套发现规则 | 两份代码 | 各自与引擎结论一致 |
| 只按 `Editable` 开放 | 用户不能在工作区技能上点开关 | 不会出现「点了被引擎忽略」的假开关 |
| 插件列表来自 CLI | 依赖 CLI 的 JSON 稳定 | 装了什么就是什么，不会读错 `node_modules` 结构 |
| 新建只写 SKILL.md | 没有模板库 | 结构简单，用户能直接用编辑器改 |
| 安装可取消而不是超时 | 要多一层 context 传递 | 大包不会因为「等太久」被误判失败 |
