# 环境（env）

## 定位

回答三个问题：**引擎在哪、它的东西落在哪、它认什么配置**。
它不装引擎——怎么装、怎么升级、怎么卸载在 [apps.md](apps.md)；
这里只管「装完之后怎么找到它」。

「不自己实现安装」不是省事，是边界：一旦我们下载二进制、改 PATH、写注册表，
这个只读代理就变成了系统安装器，风险面（提权、半途失败、回滚）与收益完全不成比例。
代跑官方安装器与自建安装器的区别见 [apps.md](apps.md) 的「定位」一节。
## 设计

```
backend/env/
├── layout.go     Layout：一个引擎的落盘布局（按 kind 分叉的唯一处）
│                  + InstallCandidates：官方安装器的落点候选
├── detect.go     Detect：候选路径 → engine.Identity（复用 engine.Detect）
├── ompcfg/       models.yml 的 YAML 节点级读写
└── picfg/        models.json 的 JSONC 键级读写
```
### 探测

```
对每个 kind：候选 = [设置里的 engine.path.<kind>]
                   + [npm 全局 shim（%APPDATA%\npm\<bin>.cmd）]
                   + [PATH 上的 <kind>]
                   + [InstallCandidates(kind)：官方安装器的落点]
对每个候选跑 engine.Detect → Identity（判据见 [ENGINE-MATRIX.md](../ENGINE-MATRIX.md)）
最后按可执行文件的位置判安装渠道（engine.DetectChannel）
```

**为什么必须有「安装落点」这一档**：omp 的安装器把它放进
`%LOCALAPPDATA%\omp\omp.exe` 且**不往 PATH 里写东西**；pi 的官方安装器用
托管布局（`<root>/releases/<version>/node_modules/.bin/pi.cmd`，当前版本写在
`current-version`）。只查 PATH 时这两个都可能被漏掉——而用户看到的是一台装好的
机器上写着「未安装」。落点只按官方规则推确切路径、不扫盘，
整条链有 6 秒总预算（`detectBudget`）。

三种结果，界面各有各的样子：

| 结果 | 界面 |
|---|---|
| 找到 | `omp 18.13.1`（绿点）+ 来源标签 `path` / `custom` + 渠道标签 |
| 没找到 | 灰点 + **一行可复制的安装命令** + 「去应用页安装」 |
| 路径无效 | 仍显示「没找到」，但输入框里保留用户填的路径供修正 |

安装命令原文与实际执行的是同一串，收口在 `backend/engine/dist.go`，
清单见 [apps.md](apps.md) 的「分发事实」。它同时做两件事：给人复制，
给应用页当真正要跑的命令。渠道判定与代跑流程也在 [apps.md](apps.md)。

### Layout：两个引擎的落盘位置

```go
type Layout struct {
    Kind       engine.Kind
    Root       string // ~/.omp 或 ~/.pi
    Agent      string // 当前 profile 的 agent 目录
    Sessions   string
    Skills     string
    Managed    string // 仅 omp 有
    Plugins    string
    Models     string
    Settings   string
    AuthFile   string // 仅 pi 有
    ProjectDir string // 工作区里的项目级目录名（.omp / .pi）
}
```

| | omp | pi |
|---|---|---|
| 根 | `~/.omp`（或 `OMP_CONFIG_DIR`） | `~/.pi` |
| agent | `~/.omp/agent`，profile 生效时 `~/.omp/profiles/<name>/agent` | `~/.pi/agent`（或 `PI_CODING_AGENT_DIR`） |
| 模型 | `models.yml` | `models.json` |
| 主设置 | `config.yml` | `settings.json` |
| 凭据 | `agent.db`（SQLite，不碰） | `auth.json`（不碰） |
| 技能 | `agent/skills` + `agent/managed-skills` | `agent/skills` |
| 插件 | `~/.omp/plugins` | `agent/npm` |

**两个引擎的环境变量名不同**：`OMP_CONFIG_DIR` 属 omp，`PI_CODING_AGENT_DIR` 属 pi。
前缀按引擎归属，不可混用。

`ResolveLayout` 按 kind 分叉，上层只拿这个结构、不再拼路径。
**这是「路径形状」的唯一落点。**

## 契约

```go
func ResolveLayout(kind engine.Kind, profile string) (Layout, error)
func Layouts(profile string) map[engine.Kind]Layout
func (l Layout) Exists() bool

func Detect(customExe string, kind engine.Kind) (engine.Identity, error)
func MatchKind(kind engine.Kind, exe string) bool
```

`Detect` 找不到时返回 `Exe == ""` 的 Identity，**不是错误**：
「没装」是正常状态，界面据此显示安装命令。

### 两套配置读写

```go
// ompcfg：YAML 节点级
func ReadModels(path string) (*ModelsFile, error)
func UpsertProvider(path, name string, p Provider) error
func DeleteProvider(path, name string) error
func MergeProvider(prev, next Provider) Provider
func (m *ModelsFile) View(path string) ProvidersView

// picfg：JSONC 键级
func Read(path string) (*File, error)
func Write(path string, f *File) error
func Upsert(path, name string, p Provider) error
func Delete(path, name string) error
func Merge(prev, next Provider) Provider
func StripJSONC(raw []byte) []byte
```

**两个包不共享任何类型**：`models.yml` 与 `models.json` 的 schema 零重叠
（omp 的 provider 有 `auth`/`headers`，pi 的有 `oauth`/`modelOverrides`；
omp 的模型条目有 `input`，pi 的有 `inputLimits`/`thinkingLevelMap`）。
合成一套「通用 provider」只会两头都对不上。

上层要的**统一形态**在 `domain.ProviderView`：`service.Providers` 把两边的
读结果映射过去，界面只认这一种。

### 密钥契约

| 方向 | 规则 |
|---|---|
| 读到前端 | `api_key` 打码（前 6 + 8 个 • + 后 4）；`has_key` 表示配了没 |
| 从前端写回 | 空字符串 = **保留原值**；非空 = 覆盖 |

空值不当「删除」是刻意的：界面上密钥是打码的，用户「什么都没改只点保存」
时传回来必然是空。把它当删除，一次保存就毁掉一个能用的服务商。

## 关键流程

### 写 omp 的 models.yml

```
读全文件 → yaml.Unmarshal 到 yaml.Node（保注释、保顺序）
定位 providers 映射
  ├─ 存在 → 只替换该 provider 节点
  └─ 缺失 → 追加
空密钥 / 空 auth / 空 headers → 从 prev 继承（MergeProvider）
原子写：临时文件 → fsync → rename
```

### 写 pi 的 models.json

```
读全文件 → 去 JSONC 注释与尾逗号 → map[string]json.RawMessage
providers 段同样按 RawMessage 保留
只替换我们写的 name，其余 provider 原样带回去
原子写：临时文件 → rename，0644
```

**为什么不用「解成完整 struct 再序列化」**：那会抹掉我们不认识的键
（omp 的 `modelRoles` / `presets`、pi 的 `packages` / `cacheWarming`）。
用户看到的现象是「我设的偏好不见了」。

### JSONC 解析

pi 的 `models.json` 允许注释与尾逗号。`StripJSONC` 手写扫描而不是正则：

- 字符串里的 `//` 必须保留——`baseUrl` 里的 `https://…` 就长这样
- 尾逗号只在后面紧跟 `}` / `]` 时才算尾逗号

## 不变量

| 不变量 | 原因 |
|---|---|
| **不下载、不安装、不改 PATH、不写注册表** | 边界；一旦做，风险面与收益不成比例。装 / 升降 / 卸在 [apps.md](apps.md)，代跑的也是官方命令 |
| 探测结果缓存 60 秒 | `pi --help` 要起一次 Node（约 3.5 秒） |
| 只写我们掌管的键 | 引擎与用户还在里面写别的 |
| 空密钥不覆盖已存值 | 界面掩码是空的，用户没动就不该毁凭据 |
| 原子写（临时文件 + rename） | 半写坏的配置文件比没有配置文件更糟 |
| 两个引擎的环境变量名不混用 | `PI_*` 属于 pi，`OMP_*` 属于 omp |
| `Layout` 是路径形状的唯一落点 | 散落拼路径 = 换一个引擎要改十几个地方 |

## 取舍

| 取舍 | 代价 | 收益 |
|---|---|---|
| 不自己实现安装 | 装不上时只能让用户自己跑命令 | 不背分发服务可用性与提权风险 |
| 两套配置读写而非统一抽象 | 两倍代码 | 各自的格式差异本来就无法统一（YAML 节点 vs JSONC 键） |
| 节点级 YAML 编辑 | 实现比「解 map」复杂 | 保留注释与顺序，用户改的东西不会凭空消失 |
| 探测靠 `--version` 正则 + `--help` 复核 | 两次 spawn（缓存后一分钟一次） | 不靠文件名（用户可能软链/改名） |
| 打码而不是不返回密钥 | 前端看不到真实值 | 密钥永远不出本机文件 |
| 统一形态放在 `domain` 而不是 env | 多一层映射 | env 不需要知道界面长什么样 |
