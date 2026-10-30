# 工作区（git 与文件）

## 定位

回答一个真问题：**「助手到底改了什么，我能不能收成一次提交」**。

它**不做**：不做提交信息的自动生成（那是模型的事）、不做 diff 的语法高亮
（界面的事）、不缓存状态（每次都问 git）。

存在的原因：引擎的工具会改用户的真实工程文件，而「改了什么」这件事
只有 git 知道。界面必须能把它显示出来，否则用户只能去终端里 `git status`。

## 设计

```
backend/git/                 ← 能力域，只依赖 domain + pkg
├── git.go        status / diff / scope（工作区到仓库根的映射）、porcelain 解析
├── write.go      stage / unstage / discard / commit / branch / merge / push / pull / …
├── history.go    提交列表、单提交详情、文件历史
└── exec_windows.go  hideConsole

backend/service/workspace.go ← 编排：把「当前工作目录」拼上 git 调用
backend/domain/git.go        ← 视图类型（Status / Diff / Commit / Branch）
backend/domain/git_write.go  ← 写入入参与回执
backend/domain/filetree.go   ← 目录列举与文件预览视图
```

### 为什么用 `os/exec` 调系统 git

| 方案 | 问题 |
|---|---|
| 纯 Go 的 git 库（go-git 等） | 与真 git 的行为差异是长期的坑（`core.autocrlf`、`core.ignorecase`、hooks、sparse-checkout、LFS、worktree） |
| 直接读 `.git` 目录 | 只读还行，写就是重新实现 git 的对象模型 |
| **调系统 git** | 需要用户装 git（可接受：用 agent 改代码的人基本都有；没装时界面给明确原因） |

### 作用域（`scope`）

工作目录可能是仓库的**子目录**，也可能是仓库本身。所有路径都要两级换算：

```
workspace 相对路径  ──映射──→  仓库根相对路径   （git 只认这个）
        ↑                               │
        └───────── 反向换算 ────────────┘
```

`scope` 保存：`ws`（工作区绝对路径）、`root`（仓库根）、`gitDir`。
`relFromRoot` / `relToWorkspace` / `pathspec` 三件工具做换算。
**diff 头里的路径是仓库根相对的**，直接当工作区相对路径用会拼出错误文件。

### 写入是白名单

只有这些动作能写，且每个都由用户点下去：

| 动作 | 底层命令 | 危险度 |
|---|---|---|
| `Stage` / `Unstage` | `git add` / `git restore --staged` | 低（可逆） |
| `ResolveConflict` | `git add <conflicted>` | 低 |
| `Discard` | `git restore` / `git clean` | **高**（不可撤销，界面二次确认） |
| `Commit` | `git commit -m` | 中（进历史） |
| `CreateBranch` / `SwitchBranch` / `DeleteBranch` | `git branch` / `git switch` | 中 |
| `Merge` | `git merge` / `git rebase` | 中（可能进冲突态） |
| `UpdateState` | `git merge --continue/--abort` 等 | 中 |
| `Init` / `AddRemote` | `git init` / `git remote add` | 低 |
| `Push` | `git push`（`none｜lease｜force` 三级） | **高**（`force` 要输分支名确认） |
| `Pull` / `Fetch` | `git pull` / `git fetch` | 低（可能进冲突态） |

**没有「任意 git 命令」入口**。有那个入口，就等于把一个提权 shell 交给前端。

### 为什么每次写后重读 status

写入的返回值是**重读后的 `GitStatusVO`**，不是「成功/失败」。

理由：界面不该自己推断「刚才那个文件现在在哪」。比如 `discard` 之后一个未跟踪文件
会从列表里彻底消失，而一个已跟踪文件会变成「已修改但内容为空」——
这两种结果下前端要做的更新完全不同，让后端给一份权威快照最省事。

## 契约

```go
// 读
func Status(dir string) (*domain.GitStatusVO, error)
func Diff(dir, rel string) (*domain.GitDiffVO, error)
func History(dir string, limit int) ([]domain.GitCommitVO, error)
func CommitDetail(dir, hash string) (*domain.GitCommitDetailVO, error)
func FileHistory(dir, rel string, limit int) ([]domain.GitCommitVO, error)
func Branches(dir string) ([]domain.GitBranchVO, error)
func Version() string
func Available() bool

// 写（全部返回重读后的状态）
func Stage(dir string, paths []string) (*domain.GitStatusVO, error)
func Unstage(dir string, paths []string) (*domain.GitStatusVO, error)
func ResolveConflict(dir string, paths []string) (*domain.GitStatusVO, error)
func Discard(dir string, paths []string) (*domain.GitStatusVO, error)
func Commit(dir, message string, amend bool) (*domain.GitCommitRESP, error)
func CreateBranch(dir, name string) (*domain.GitStatusVO, error)
func SwitchBranch(dir, name string) (*domain.GitStatusVO, error)
func DeleteBranch(dir, name string) (*domain.GitStatusVO, error)
func Merge(dir, name string, rebase bool) (*domain.GitStatusVO, error)
func UpdateState(dir, action string) (*domain.GitStatusVO, error)
func Init(dir, branch string) (*domain.GitStatusVO, error)
func AddRemote(dir, name, url string) (*domain.GitStatusVO, error)
func Pull(dir, remote, branch string, rebase bool) (*domain.GitStatusVO, error)
func Push(dir string, req domain.GitPushREQ) (*domain.GitStatusVO, error)
func Fetch(dir, remote string) (*domain.GitStatusVO, error)
```

**超时**：status 10s / diff 15s / 写 60s / push 120s。仓库再大也只给这么久，
界面不能为一个状态卡住。

**上限**：diff 3000 行 / 单行 256KB / 未跟踪文件 8MB。截断时 `truncated + dropped`
都要报给界面——不说「后面还有」，用户会以为文件就这么长。

**错误契约**：

| 情况 | 返回 |
|---|---|
| 系统里没有 git | `Status.Available=false` + `reason`，**不是错误** |
| 目录不在仓库里 | `Status.IsRepo=false` + `reason`，**不是错误** |
| 路径越界 | `pkg.CodePathEscape`（`SafeJoin` 拦下） |
| 文件不存在 | `pkg.CodeFileMissing` |
| git 命令超时 | `pkg.CodeGitTimeout` |
| git 命令失败 | `pkg.CodeGitUnavailable`，`detail` 带 git 的 stderr 首行 |
| 分支名 / 远端名不合法 | `pkg.CodeGitBadInput` |
| git 拒绝了这次操作 | `pkg.CodeGitRejected` |
| 推送被拒 / 认证失败 / 连不上 | `pkg.CodePushRejected` / `CodePushAuth` / `CodePushNetwork` |

每个码的前端处置见 [ERROR-CODES.md](../ERROR-CODES.md)。

## 关键流程

### status

```
insideWorkTree(dir)
  → git status --porcelain=v1 -z（--untracked-files=all）
  → parsePorcelainZ：XY 两位 → {staged, status, conflict}
  → git symbolic-ref --short HEAD（分支）
  → 读 .git 里的 MERGE_HEAD / rebase-merge / CHERRY_PICK_HEAD → state
  → git rev-parse --abbrev-ref @{upstream}（上游）
  → git rev-list --left-right --count @{upstream}...HEAD（ahead/behind）
  → numstat 汇总每个文件的增删
```

**`-z` 是必需的**：默认输出里文件名含空格、中文、换行都会被引号包起来
（`"\346\226\207\344\273\266"` 这种八进制转义）。`-z` 给的是 NUL 分隔的原始字节。

### diff

```
rel == ""  → 整区：diffAll
rel != ""  → 单文件：先判 tracked
                已跟踪  → git diff HEAD -- <path>（含暂存与工作区两侧）
                未跟踪  → 整篇读成「全新增」
```

未跟踪文件**必须单独处理**：`git diff` 对未跟踪文件什么都不输出，
而用户最想看的恰恰是「这个新文件里写了什么」。

二进制文件识别：头部有 NUL → 只给一行 `(binary)`。不识别的话会把 PNG/EXE
读成 UTF-8 乱码，看起来像「文件坏了」而不是「这是二进制」。

### porcelain 状态码

| XY | 含义 |
|---|---|
| `M ` / ` M` / `MM` | 已暂存修改 / 工作区修改 / 两者都有 |
| `A ` | 新增（已暂存） |
| `D ` / ` D` | 已暂存删除 / 工作区删除 |
| `R ` | 改名（带原路径） |
| `??` | 未跟踪 |
| `UU` / `AA` / `DD` / `AU` / `UA` / `DU` / `UD` | 冲突（七种，归一成 `U`） |

`classify(x, y)` 把两位归一成 `{staged, status, conflict}`，界面按 `status`
首字母染色。**冲突必须单独标出来**：有未解决冲突时 git 拒绝大多数写入，
界面要显示横幅与「标记已解决 / 继续 / 中止」。

### 文件面板

```
GET /files?path=<相对目录>
  → pkg.SafeJoin(workspace, path)     // 越界 → 1004
  → os.ReadDir 单层
  → 过滤 . 开头与 $ 开头（系统噪音）
  → 目录在前、名称排序、500 条上限
```

**只读**。写文件是助手的事（它用引擎自己的工具）。
有第二个写入口，就有一天「谁改的这一行」说不清。

## 不变量

| 不变量 | 锁死原因 |
|---|---|
| 所有路径过 `SafeJoin` | 前端可以传 `..\..\Windows\System32` |
| porcelain 用 `-z` | 非 NUL 分隔时中文/空格文件名会被转义成八进制 |
| 工作区相对路径与仓库根相对路径必须换算 | 工作区是仓库子目录时两者不等 |
| 写后重读 status | 前端推断「文件现在在哪」一定会漂移 |
| `Discard` 必须二次确认 | 不可撤销，且用户常常以为「丢弃」=「取消暂存」 |
| `Push --force` 要输分支名 | 它会静默覆盖别人的提交 |
| diff 截断要报 `dropped` | 不报等于骗人说「文件就这么长」 |
| 二进制不逐行渲染 | 乱码会让人以为文件坏了 |
| 文件面板只读 | 避免第二套写入口 |

## 取舍

| 取舍 | 代价 | 收益 |
|---|---|---|
| 调系统 git | 用户必须装 git | 行为与终端完全一致，不受 `autocrlf` / hooks 差异影响 |
| 写后重读 status | 每次写多一次 git 往返（~50ms） | 前端不做状态推断，不会出现「列表说有、实际没有」 |
| diff 上限 3000 行 | 超大改动看不全 | 一次渲染 10 万行会让 WebView 卡死 |
| 未跟踪文件整篇读 | 大文件要截断（8MB 上限） | 新文件内容可见，这是最常见的「改了什么」 |
| 文件面板只读 | 不能在界面里直接改文件 | 写路径只有一条（引擎的工具），责任清晰 |
| 不做提交信息自动生成 | 用户得自己写一句 | 那是模型的事；界面代写会让「谁决定的」变模糊 |
