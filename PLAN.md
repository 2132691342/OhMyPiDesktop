# OhMyPiDesktop 改造：从「能跑」到「成熟稳定」

执行顺序（按你的选择）：**第一批先修后端检测与稳定性**，其余三批紧随。

- 照搬尺度：搬 WorkBaby 的交互与信息架构，**代码重写**，走 `--omp-*` 令牌与 i18n
- 缺引擎：如实显示「未安装」+ 给可复制的安装命令（维持现有设计）
- 规范：改造到哪个文件就顺手拆到 500（Go）/ 400（Vue）行以内

---

## 已定位的四个真实缺陷（按代码链路走出来的，不是猜的）

### 缺陷 1：「数据不准」的根因 —— `healthOf` 里越权的 early return

`backend/service/engines.go` 的 `healthOf`：

```go
layout, err := a.Layout(kind)
if err != nil {
    return row          // 计数全留零值就返回了
}
// ↓ 下面才是四个计数的赋值
sessions, skills, plugins, mcp := a.countsFor(kind)
```

`Layout` 一旦出错，会话 / 技能 / 插件 / MCP 四个计数**全部返回 0**，
而 `installed` 可能是 `true`。这台机器正在发生：`~/.omp/` 是个残缺目录。

于是概览页出现最坏的一种错：**言之凿凿显示「已安装，0 个会话」，
而磁盘上其实有 90 个会话**。这不是「没刷新」，是说了一个反的谎。
拿不到计数时必须标记「未知」，不能拿 0 当 0。

### 缺陷 2：「切引擎卡」的根因 —— `switchEngine` 串行等一个慢往返

`frontend/src/stores/chat.ts`：

```ts
const state = await api.chatState(next, undefined)  // 可能拉起进程（pi 起 Node ≈3s）
applyState(state)
void loadModels()                                    // 而 chatState 内部还会再触发一次
```

切引擎要**串行等**一次可能冷启动进程的 `chatState`；而 `loadModels`
在 `chatState` 的 `refresh` 路径里**还会被触发第二次**。
用户点了引擎下拉然后盯三秒转圈 —— 这就是「切换很卡」。

切引擎的正确形态是**瞬时**：本地已有分桶缓存的模型清单立刻换上，
真正的往返全部后台补。

### 缺陷 3：「检测不到位」的根因 —— 探测是拉模型，且结果帧可能发在无订阅者时

现状 `Health` / `Bootstrap` 各调 `detectAll(ctx, false)`；缓存不新时立刻返回
（可能是空值）+ 后台补验。问题在**探测回来之后没有任何机制保证已展示的页面刷新**：

- `emitDetected` 确实广播 `engine:detected`
- 但 SSE 在 `app.load()` 里的 `connect()`，而 **Bootstrap 自己也会触发探测**
- 于是存在一条真实时序：`Bootstrap` 触发的探测在 `connect()` 之前就跑完了，
  `engine:detected` 发在**还没有订阅者**的总线上 → 这一帧被静默丢弃
- 前端唯一的兜底就是「收到 `engine:detected` 才 `refreshEngines`」；
  帧丢了就再也不刷，概览页停在旧值（通常是「未安装」）不动

这不是偶发，是**首发必现**。

### 缺陷 4：探测链路缺超时，且两个文件已超 500 行

- `backend/env/detect.go` 的 `Detect` 用 `context.Background()`，
  **没有超时**。`ProbeTimeout` 定义在 engine 层的 `runCLI` 里，
  但从 `env.Detect` 这条路径进来时用不上 —— PATH 上有个卡死的 `omp.cmd`
  时整条探测链无限悬挂。
- `service/app.go` 516 行、`service/engines.go` 423 行（前者已超标）。

---

## 第一批：后端检测与稳定性（本次开工）

### 1.1 计数拿不到就承认「不知道」（修缺陷 1）

`backend/service/engines.go`：

- 拆掉那个越权的 early return：`Layout` 失败只跳过**路径类字段**
  （`root_dir` / `agent_dir` / `config_files`），**不跳过计数**
- `domain.EngineHealthVO` 加一组 `*_known bool`
  （`session_count_known` / `skill_count_known` / `plugin_count_known` / `mcp_count_known`）；
  拿不到置 false，前端显示 `—` 而不是 `0`
- `countsFor` 现在返回 4 个裸 int，改成返回结构 + known 标记
- 顺手拆文件：`service/app.go`（516 行）把「最新版落盘/读回」那段抽到
  `service/latest.go`；`service/engines.go` 保持 500 以内

**ContractVersion +1**，同步 `frontend/src/types/api.ts`。

### 1.2 切引擎改成瞬时（修缺陷 2）

`frontend/src/stores/chat.ts` 的 `switchEngine`：

- 不等 `chatState`：先把本地 `kind` / `models` / `levels` 换成目标引擎的分桶缓存值，
  UI 立刻就是那个引擎的样子
- `chatState` 挪到后台 `void` 调用，回来再 `applyState` 覆盖
- 去掉 `chatState` 的 `refresh` 路径里对 `loadModels` 的隐式触发，
  避免一次切换触发两次模型清单往返

### 1.3 探测改成「推得动」（修缺陷 3）

三处改动，缺一不可：

- `service/app.go`：`App` 上挂 `detectWait chan struct{}`，探测完成后关闭
  （下一轮重新开）。想阻塞的调用方 `<-detectWait` 一眼 ——
  给「愿意等」的调用方一个正确的等待点，不必全靠 SSE 帧
- `backend/server`：SSE 端点接上时**补拉一次** `EngineStatus`，
  覆盖「Bootstrap 的探测早于 connect」那条时序
- 前端兜底从「只认 `engine:detected` 帧」改成**另外还比对 `detected_at`**；
  帧丢了也能靠时间戳自己决定重拉

### 1.4 探测加超时 + 说清「为什么不知道」（修缺陷 4）

- `env.Detect` 接 `context` 并套 `engine.ProbeTimeout`（这条路径现在漏了）
- `Health` 加 `probe_unknown_reason`：区分「没装」/「路径失效」/「探测超时」——
  三种情况给用户的下一步完全不同，现在全混成「未安装」

### 1.5 第一批的测试（只测会悄无声息错的地方）

`backend/service/engines_test.go` 一条表驱动：

| 用例 | 断言 |
|---|---|
| Layout 失败时 | 计数 `*_known=false`，且不许被当 0 展示 |
| Layout 成功时 | known=true 且有值 |
| 探测未完成时取 health | `detecting=true`，`installed` 不许给出 false 语义 |
| `detected_at` 变化后 | 重拉判定正确 |

复用已有 helper（`SetDetector` / `fakeDialect`），不新增包、不引入新依赖。

### 第一批完成判据 —— 已达成

- `go build ./backend/...`、`go vet ./backend/...`、`go test ./backend/... -count=1` 全绿
- `npm run build`（含 `vue-tsc --noEmit`）通过
- **真实机器冒烟已跑**（输出见本节末）：pi 装了、omp 没装 ——
  pi 卡给出真实会话数 1（与磁盘逐个数出来的结果一致，不再是 0），
  omp 卡显示「未安装」且 `probe_unknown_reason=missing` + 可复制的安装命令
- 文件行数收敛：`service/app.go` 588 → 499（抽出 `service/cache.go`，110 行）

### 第一批实测输出（本机跑用例层，真探测本机 pi）

```
=== 探测之后 === detecting=false detected_at=1793289271748
  omp  installed=false exe= version=
  pi   installed=true  exe=C:\CodingSoftware\nodejs\pi.cmd version=1.4.0

=== 首屏 Health === detecting=false
  omp  sessions=0(known=true) skills=0(known=true) plugins=0(known=true) mcp=1(known=true) layout=true
  pi   sessions=1(known=true) skills=9(known=true) plugins=12(known=true) mcp=0(known=true) layout=true

totals_partial=false total_sessions=1
```

「pi · 1 个会话」与磁盘上 `~/.pi/agent/sessions` 实际数出来的 1 个 `.jsonl` 一致 ——
这正是修之前会显示成 `0` 的那一格。

---

## 第二批：聊天输入区 + 消息列表

> 前提：第一批已合并。WorkBaby 的 `ChatInput.vue` 是 1189 行，
> 我们搬的是它的**分区与密度**，不是它的行数。

### 2.1 输入区重写（`components/chat/ChatComposer.vue` 477 行 → 拆）

| WorkBaby 的分区 | 我们落地 |
|---|---|
| `ta-shell`：输入框 68px min-height，点空白即落光标 | ElInput 现在太矮，这是「不够条理」的主因 |
| `composer-bar is-divided`：工具行与输入块用一条分隔线切开 | 这是「美观有条理」的关键；现在工具行在抢输入框横向空间 |
| `attach-row`：待发送文件 chip，超出两行内部滚动 | 沿用现有 `AttachStrip`，改 chip 组样式 |
| 运行态 `run-hint` + `steer/queue` 单选组 | 已有对应逻辑，补提示行 |
| `queue-dock` 排队条 | 现有队列逻辑补出来 |
| 键盘高亮**自算 scrollTop**（不用 `scrollIntoView`） | 现在没这条，↑↓ 导航会失效 |
| `keepFocus`：点菜单不让输入框丢焦点 | 同上，现在是 bug |

拆成：`ChatComposer.vue`（编排，< 200 行）+ `ComposerMenu.vue`（斜杠 / @ 菜单）
+ `ComposerBar.vue`（工具行）+ `QueuedDock.vue`（排队条）。

### 2.2 消息列表紧凑化（`views/ChatView.vue` 499 行 → 拆）

- 现在每个 user / assistant 都渲染一个带 badge 的 `msg-head`；
  照搬 WorkBaby：**同一角色连续的消息只出一次头**
- 工具卡默认收起、diff 到货自动展开（`ToolCallCard` 已有此逻辑，保留）
- 抽出 `MessageTurn.vue` + `HistoryList.vue`，`ChatView.vue` 降到 200 行内

### 2.3 消息里看 diff（补齐两点）

- 现在只有「改文件类」工具带 diff；照搬：**凡触及文件的工具都给差异入口**
- 「看工作区」按钮现在被 `canPeek` 的 `file_path` 判据挡在
  「文件不在当前工作区」的情况之外 —— 那个判据太严，放宽

---

## 第三批：工作区 Git 三栏 + 管理页 UI

### 3.1 Git 三栏重做（`views/WorkspaceView.vue` 674 行 → 拆）

**已完成**（WorkspaceView 674→371）：

- 改动清单按目录分组折叠（`GitChangesPane`），目录头带增删合计
- 差异视图（`DiffView`）补两件事：折叠未改动区段、并排 / 内联切换
- `GitCommitPanel` 独立，git 写动作全收口进 `useGitActions`

### 3.2 管理页 UI 对齐

**已完成**。所有 `.vue` 均已降到 400 行以内（原先 8 个超标）：

| 文件 | 前 | 后 |
|---|---|---|
| SettingsView | 941 | 303 |
| OverviewView | 790 | 378 |
| ModelsView | 755 | 321 |
| WorkspaceView | 674 | 371 |
| AppCard | 594 | 351 |
| SkillsView | 554 | 342 |
| McpView | 482 | 370 |
| AgentPanel | 431 | 393 |

---

## 第四批：UI 收尾（五态 + 极简靛蓝）

### 4.1 按钮五态收口

**已完成**。五态本来就齐，但补了三处：

| 缺口 | 修法 |
|---|---|
| 缩放值硬编码在 base.css 与 ep-theme.css 四处 | 收成 `--omp-press-hover` / `--omp-press-active` / `--omp-press-row-active` 三档令牌 |
| 加载态没挡指针事件 | 补 `pointer-events: none`（转圈时再点会发第二个请求） |
| reduce-motion 覆盖面 | 本已全局接管，确认无需再改 |

### 4.2 极简靛蓝 + 动效统一

`themes.css` 开头已标注 `--saas_dashboard-color-1..5` 五档来源，映射已就位。
补两件事：

- `--omp-dur-*` 动效令牌全站统一（现状有组件写裸 ms）
- reduce-motion 开关现状只对少数组件生效，补齐到全站

---

## 全局纪律（贯穿四批）

- 一个概念一个落点（AGENTS.md §2）；新增能力先问它该在哪一层
- `ContractVersion` 变了就 +1 并同步 `frontend/src/types/api.ts`
- 每批跑：`go build ./backend/...`、`go test ./backend/... -count=1`、
  `cd frontend && npm run build`（含 `vue-tsc --noEmit`）
- 改了绑定面就重跑 `wails3 task common:generate:bindings` 并提交生成物
- 改了协议翻译 / 会话投影 → **真起一个 pi 进程跑一轮**（本机装了 pi，有现成条件）
- 每个 Go 文件 ≤ 500 行，每个 Vue 组件 ≤ 400 行，连续注释 ≤ 3 行
- 注释只讲「为什么」；不写变更记录、不点外部参考项目
- 不放宽门禁来让代码通过

---

## 验收清单

**第一批（已完成）**

- [x] pi 卡显示真实会话数（不是 0）；omp 卡显示未安装 + 安装命令
- [x] 切引擎瞬时完成，无可感知的多余往返
- [x] `go build ./backend/...` 与 `go test ./backend/... -count=1` 全绿
- [x] 探测结果帧即使早于 SSE 连接也不会丢（概览页不会停在旧值）

**第二批（已完成）**

- [x] 输入区有输入框 / 工具行分隔（`.ta-shell` 68px 最小高度 + `.is-divided` 分隔线）
- [x] `@` 与 `/` 菜单键盘导航不失效：高亮自算 `scrollTop`、菜单内 `keepFocus` 保焦
- [x] 消息列表同角色不再重复出头（`HistoryList` 算 `continued` 标记）
- [x] 凡触及文件的工具卡都给差异入口；文件在工作区外也能看真实差异

第二批拆出的新文件：`ComposerMenu.vue`(229) / `MessageTurn.vue`(169) /
`HistoryList.vue`(88) / `ChatHeader.vue`(183) / `useComposerActions.ts`。
`ChatView.vue` 499 → 315 行，`ChatComposer.vue` 477 → 395 行。

**第三批**

- [ ] Git 差异支持折叠与并排；提交 pane 带 diff 预览；文件树按目录折叠
- [ ] 所有 Vue 组件 < 400 行

**第四批**

- [ ] 按钮五态（悬停放大 / 按下缩小）全站一致，含 loading 禁用 pointer
- [ ] reduce-motion 全站生效

**全程**

- [ ] `vue-tsc --noEmit` 与 `go build` 无退步
- [ ] 绑定的两道门（比对 id + 生成物已提交）都是绿的
