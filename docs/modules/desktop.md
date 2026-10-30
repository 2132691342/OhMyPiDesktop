# desktop：Wails 外壳

## 定位

全项目唯一 import `wails` 的包。它提供「界面外面的那一层」：窗口、托盘、单实例、
以及前端通过绑定直接调用的几个系统能力。`main.go` 只把内嵌资源与版本号交给它。

`backend/` 全部不知道 Wails 存在——业务只通过回环 HTTP 与前端说话。这条界线让
业务层能在没有 WebView 的机器上跑测试，也让换桌面框架版本时不牵动任何业务代码。

## 文件

| 文件 | 回答什么 |
|---|---|
| `app.go` | 怎么装配并运行一个桌面应用（窗口、托盘、生命周期、装配顺序） |
| `bindings.go` | 前端能直接调哪些系统能力（端口、目录选择框、窗控、退出） |
| `tray.go` | 关窗之后，托盘菜单能做什么 |
| `dialog_windows.go` / `dialog_other.go` | 界面起来之前怎么把致命错误告诉用户（Windows 弹原生消息框，其余平台只写日志） |
| `scale.go` | 屏幕缩放比的平台无关入口，建窗尺寸换算用 |
| `scale_windows.go` / `scale_other.go` | 主屏缩放比怎么读（Windows 走原生 DPI API，其余平台恒 100%） |

## 窗口：为什么等前端 Ready() 才显示

窗口以 `Hidden: true` 建出来，装配阶段一律不碰它。显示发生在前端画完首屏、
调用绑定方法 `Ready()` 的那一刻。

这不是保守，是 v3 的时序决定的：

1. `Window.NewWithOptions` 只登记参数，**窗口在 `app.Run()` 里才真正创建**。
2. `ServiceStartup`（装配）跑在 `app.Run()` 里，但排在窗口创建之前。
3. 此刻调 `Show()` 会走「先建窗、再 return」的分支：窗口被建出来了，但仍然隐藏。
4. 紧随其后的 `Focus()` 会访问尚未初始化的 WebView2 controller——直接 panic。

所以「装配完成后显示窗口」既显示不出来，还可能崩掉整个进程。等前端 Ready()：

- 窗口已经建好并加载完页面，`Show + Restore + Focus` 三步都落到真窗口上；
- 用户第一眼看到的是画好的界面，没有白屏；
- 装配失败时前端照样能起来，把失败原因显示在启动失败页上——界面上没有比这更早的观测点。

`Ready()` 用 `revealed` 原子量挡住重复调用：前端每次热重载都会再调一次，
重复 `Show` 会让窗口在用户眼前闪。

开机自启（`--hidden`）时 `Ready()` 先重铺一次托盘菜单（建托盘时工作目录与最近列表
还没载入），再直接返回：只上托盘，不弹窗。

## 窗口尺寸与位置

逻辑尺寸 **800×600**（CSS 像素），与前端的容器查询断点配套：800 宽时对话侧栏收窄、
工作区三栏压缩。Wails v3 把建窗声明的 `Width/Height` 当**物理像素**用，所以建窗时
按主屏缩放比把逻辑尺寸放大再声明（缩放比读自原生 DPI API，见 `scale_windows.go`）；
`Ready()` 里再用逻辑值 `SetSize` 一次，由 Wails 按当前 DPI 换算回来。

**不设最小尺寸**。Wails 的最小尺寸走 `WM_GETMINMAXINFO`，它按窗口 DPI 换算，
而声明的尺寸是物理像素——两者基准不一致时窗口会被系统强行撑大。
布局尺寸交给前端：`.shell-main` 是容器查询的锚点，窄窗口下侧栏与三栏自己收窄。

**位置分两步定**：建窗时声明 `InitialPosition: WindowCentered`，窗口先落在屏幕正中
（不设则 Windows 走级联默认位置，首次启动窗口会靠到屏幕右上角）；`Ready()` 里改完
尺寸，再用 `centerOnPrimaryScreen()` 按主屏工作区精调一次。

精调不直接调 Wails 的 `Center()`：它取尺寸用 `GetClientRect`、取监视器用
`GetMonitorInfoForWindow`，对这个被 `SetSize` 改过尺寸的无边框窗口，两个基准
不一致（客户区 vs 窗口区），结果会偏。`centerOnPrimaryScreen()` 直接用主屏
`WorkArea` 与 `win.Size()` 算，两个量是同一套基准；拿不到屏幕信息才退回 `Center()`。

窄窗口下视图靠**容器查询**收窄，不用媒体查询：`.shell-main` 声明
`container-type: inline-size`，对话页侧栏从 232px 收到 188px，
工作区三栏从 `210/280/320` 收到 `168/200/240`。窗口是最外层容器，两种写法
在这里等效；用容器查询是为了以后把视图嵌进别的布局时不会失配。

**浮层也要夹回视口**：模型面板 440px 宽，芯片在底栏中部，800px 窗口下默认从芯片
左缘往右长会直接溢出屏幕右边。打开时算一次水平偏移把它夹回来
（`ModelPicker.clampToViewport`）。

## 唤醒窗口：三步缺一不可

`showWindow()` 依次做 `Show → Restore → Focus`。窗口可能同时在隐藏、最小化、
最大化三种状态中的任意一种，只做一半唤不回来。托盘单击、托盘菜单「显示窗口」、
第二次启动，走的都是这一个函数。

## 单实例

交给 v3 的 `SingleInstanceOptions`（`UniqueID = "com.ohmypi.desktop"`）：

- 首次启动拿到命名互斥量，正常跑；
- 第二次启动在 `application.New` 内部就完成「通知首个实例 → 退出」，走不到建窗。

首个实例收到通知后调 `showWindow()`——否则用户从托盘隐藏状态下双击图标，
看到的是「什么都没发生」。

## 托盘

菜单项由后端定，文案不进前端：语言切换时后端菜单不会跟着变，前后端各准备一份
只会让它们不同步。

| 项 | 行为 |
|---|---|
| 显示窗口 | `showWindow()` |
| 最近工作目录 | 铺最多 5 条；点一条就切目录并把窗口带出来；空列表时留一条禁用说明项 |
| 打开数据目录 | `service.OpenDataDir()`，用户排障时最快的一条路 |
| 退出 | 置 `quitting` 后 `app.Quit()`，让 `WindowClosing` 钩子放行 |

托盘单击 = 显示窗口，右键 = 菜单。

## 关窗不退出

关窗行为由设置里的 `close_action` 决定，`WindowClosing` 钩子的两条分支都先 `e.Cancel()`
拦下这次关闭：

- **tray（默认）**：窗口收进托盘（`Hide()`）。第一次收托盘先发事件让界面弹选择框——
  静默躲起来在用户眼里就是「关不掉」。
- **quit**：拦截之后主动走 `requestQuit()` 真退出。不能放行让窗口自然关：窗口关闭后
  托盘还挂着，只剩托盘时消息循环不会自己结束，进程就「关不掉」。

所有退出路径（托盘菜单「退出」、绑定 `Quit()`、quit 分支的关窗）都汇到 `requestQuit()`：
置 `quitting` 标志后异步 `app.Quit()`，`WindowClosing` 钩子看到该标志就不再拦截，
窗口正常关闭。

## WebView2 缓存放哪

`Windows.WebviewUserDataPath` 指向 `%LOCALAPPDATA%\OhMyPiDesktop\webview`。
默认位置是 `%APPDATA%\<exe 名>`——一个几十 MB 的目录，卸载时没人清理，因为
安装器不认识它。放进自己的数据目录，卸载时勾一下就能一起删干净。

这个路径必须在 `application.New` **之前**解析好并建出来：Wails 拿到不存在的路径
会弹错误框然后退出。

## 绑定面

只有 10 个方法。少一个绑定方法就少一处需要重新生成的东西。

| 方法 | 用途 |
|---|---|
| `Endpoint()` | 返回端口、令牌、版本、契约版本、基地址。会等到服务真的在监听 |
| `Ready()` | 前端首屏画完，显示窗口 |
| `PickFolder(defaultDir)` | 系统目录选择框。取消返回空串，不是错误 |
| `PickFiles(title)` | 文件选择框，可多选。取消返回空列表 |
| `OpenExternal(url)` | 用默认浏览器打开链接 |
| `WindowMinimize()` / `WindowToggleMaximize()` / `WindowClose()` | 自绘标题栏的三个按钮 |
| `Quit()` | 退出（托盘菜单与本方法共用） |
| `ApplyCloseDecision(action, remember)` | 首次收到「关窗收托盘」提示时，记录用户的选择（tray / quit，remember 为真则写进设置） |

其余一切业务走 HTTP。为什么不把 gin handler 嵌进来：嵌入会把它们的导出方法
一起绑成前端永远调不通的 JS 方法（签名带 `*gin.Context`）。

## 绑定会漂移，而且漂移是静默的

v3 的绑定 id 是 `FNV32a(导入路径.类型.方法)`。导入路径里含**包在模块内的位置**：
`desktop` 在仓库根时是 `OhMyPiDesktop/desktop`，挪到 `backend/` 下就变成
`OhMyPiDesktop/backend/desktop`。包一挪，id 全变。

后果分两半：

- **导入路径那半**：前端 import 的模块不存在，类型检查直接报「找不到模块」。这是响的那半。
- **id 那半**：生成物还在、id 全是死数字。运行时按 id 查表查不到，
  前端每个绑定调用都失败，**且没有任何报错**。表现是「点任何按钮都没反应」。

响的那半会把人引向「路径写错了」；不响的那半才是真问题。编译通过、类型检查通过、
单元测试全绿——只有真的在 WebView 里点一下才知道。

所以有两道门：

| 门 | 位置 | 拦什么 |
|---|---|---|
| `backend/desktop/bindings_test.go` | `go test ./backend/...` | 导入路径漂移 + 每个方法的 id 对不上 |
| CI 的「Verify bindings are in sync」 | `.github/workflows/ci.yml` | 生成物没跟着代码一起提交 |

判定依据里有两样是写死的：包在模块内的路径（`backend/desktop`）与模块名
（`OhMyPiDesktop`）。绑定方法清单只增不减——新增绑定方法要加进清单（否则不会被
检查），删方法要同步删条目（否则门会失败）。挪包或改绑定面时这道门失败，
那正是它该失败的时候，失败信息里写着怎么重新生成、import 改到哪。

改包的位置、给绑定加方法、删绑定方法，都要重跑：

```bash
wails3 task common:generate:bindings
```

生成物的目录名跟着包的位置走，所以重新生成之后前端 import 也要跟着改；
`bindings_test.go` 会把这件事报出来。

## 装配顺序

```
runtime.Resolve()           数据目录（含 webview 缓存路径）
application.New()           单实例 + 绑定 + 资源
Window.NewWithOptions()     登记窗口参数（Hidden + 居中初值 + 按缩放比放大的物理尺寸）
RegisterHook(WindowClosing) 关窗按 close_action 收托盘或退出
SystemTray.New()            托盘与菜单
app.Run()                   → ServiceStartup（装配业务）→ 建窗 → 消息循环
                                前端 mount → Endpoint() → server.Start()
                                → Ready()（重铺托盘菜单；非 --hidden 时显示窗口）
```

## 不变量

- 装配阶段绝不碰窗口对象。碰了就是「显示不出来」或者 panic。
- `backend/desktop` 不被 `backend/` 其他任何包 import。
- 绑定方法只做系统能力，不承载业务判断。
- 托盘与界面看到同一份「当前工作目录」：托盘切目录走 `service.SetWorkspace`，
  与界面 HTTP 那条路是同一条。
