# 开发与排障

## 环境要求

| 项 | 版本 | 说明 |
|---|---|---|
| Go | 1.24+ | 单 module，无 CGO |
| Node | 18+ | 只用于构建前端 |
| Wails CLI | v2.12+ | `wails dev` / `wails build` |
| WebView2 | 系统自带 | Win10 1803+ 一般已有 |
| 引擎 | **用户自己装** | `omp` 或 `pi`，在 PATH 上或被显式指定 |

引擎不是构建依赖。开发时本地没有引擎也能跑起来——界面会显示「没找到引擎」，
这条路径本身就是需要被验证的一种状态。

## 常用命令

```bash
wails dev                          # 热重载（前端 + 后端）
go build ./backend/...             # 只验后端能编译
go test  ./backend/... -count=1    # 后端测试
go vet   ./backend/...             # 静态检查
cd frontend && npm run build       # vue-tsc --noEmit + vite build
wails build                        # 产物 build/bin/OhMyPiDesktop.exe
```

提交前的三道门（缺一不可）：

```bash
go build ./backend/...
go test  ./backend/... -count=1
cd frontend && npm run build
```

## 验证真实引擎

改了协议翻译或会话投影后，必须真起一个引擎跑一轮——单元测试测不到
「引擎真的这么吐帧」。`tools/smoke` 做这件事：

```bash
go run ./tools/smoke -engine omp -msg "Reply with exactly: OK"
go run ./tools/smoke -engine pi  -msg "Reply with exactly: OK"
```

它打印事件计数、事件顺序、会话文件位置、用量与最后一轮的块。
**要看的三处**：

1. 事件顺序里 `chat:start → chat:turn → … → chat:turn_end → chat:settled → chat:done`
   齐全且顺序正确——这是「一轮怎么算完」的直接体现。
2. 最后一轮的块与引擎实际说的一致（正文、思考、工具卡都在）。
3. 用量与上下文水位非零。

`tools/smoke` 也可以指定工作目录与引擎路径：

```bash
go run ./tools/smoke -engine pi -dir D:\gitFiles\pi -msg "hi"
```

## 调试真实引擎

界面里发生的每一件事，都能在终端里复现。这是排障最快的路。

```bash
# omp：界面用的就是这条
omp --mode rpc-ui --cwd <某个目录>

# pi：注意不能给 --cwd（会报 Unknown option），工作目录靠进程 cwd
cd <某个目录> && pi --mode rpc
```

然后手敲 JSON 行：

```json
{"id":1,"type":"prompt","message":"hi"}
{"id":2,"type":"get_state"}
```

引擎回什么，界面就应该显示什么。**如果终端里有、界面上没有，问题在我们的翻译层或 SSE；
如果终端里也没有，问题在引擎或参数。**

### 看到原始帧

把日志级别调到调试（设置页 → 关于 → 日志抽屉 → 打开调试），
所有收发的帧会进环形日志。日志里搜 `<-` 是收到的，`->` 是发出的。

### 只在某个引擎上复现的问题

先确认 `Caps` 里那个能力是不是 `true`：

```bash
curl -H "X-Desktop-Token: <token>" http://127.0.0.1:<port>/api/v1/engines
```

`caps.subagents=false` 时子代理面板不渲染是**正确行为**，不是 bug。

## 数据落在哪

| 内容 | 位置 |
|---|---|
| 界面设置 | `%LOCALAPPDATA%\OhMyPiDesktop\settings.json` |
| 日志 | 进程内环形缓冲（最近 500 行），重启即清 |
| omp 的配置与会话 | `~/.omp/agent/` |
| pi 的配置与会话 | `~/.pi/agent/` |

**排障第一步永远是看这三处。** 界面上的「会话列表是空的」有 90% 的可能是
`~/.omp/agent/sessions/` 下真的没有文件（用户没聊过，或者用的是另一个 profile）。

## 改后端时

| 要加的东西 | 放哪 | 检查 |
|---|---|---|
| 一个新的引擎命令 | 公共的放 `engine/supervisor.go`，私有的放 `engine/<kind>/dialect.go` | 能力差异先进 `Caps`，再写调用 |
| 一个新的 UI 事件 | `engine/Translator` | 同步 `docs/API-CONTRACT.md` 事件表 + 前端 `stores/app.ts` 的 case |
| 一个新的 HTTP 路由 | `server/routes.go` | 同步契约文档；写操作必须是 POST |
| 一个新的错误码 | `pkg/errors.go` | 同步 `docs/ERROR-CODES.md` 的处置表 |
| 一个新的设置键 | `domain/model.go` 的 `Key*` | 同步 `docs/DATA-MODEL.md` 的表 |
| 一个新的引擎能力差异 | `engine.Caps` | **不要**在 service / 前端里写 `if kind ==` |
| 一个新的落盘位置 | `env/layout.go` | 别在业务代码里拼 `~/.omp` / `~/.pi` |
| 一个新的 git 动作 | `git/write.go` 的白名单 | 写后必须重读 status；危险动作要二次确认 |

## 改前端时

| 要加的东西 | 放哪 | 检查 |
|---|---|---|
| 一个新页面 | `views/` + `router/index.ts` | 同步 `docs/PAGE-STRUCTURE.md` 的路由表 |
| 一个新组件 | `components/<域>/` | 文件名 = 组件名 |
| 一个新色值 | **禁止** | 只能加进 `themes.css` 的令牌 |
| 一个新用户可见字符串 | `locales/zh-CN.ts` + `locales/en.ts` | 两个文件结构必须一致 |
| 一次异步动作 | — | 五态齐全，加载态锁尺寸，失败给原文 |

**自检**：`grep -rn "scale(1\.0" frontend/src` 应该只命中 `base.css` 里的共享类，
如果出现在某个组件的 scoped 样式里，说明五态又被复制了一份。

**只调后端**：`go run ./tools/devserver -token dev` 会打印端口与一行
`localStorage.setItem(...)`；在浏览器控制台里执行它，就能用 Vite 直连真实后端。

## 常见问题

**界面起来了但什么都不显示**
先看页头的连接灯。`已连接` 说明 HTTP 通了但引擎没起来，去设置页看引擎卡。

**「没找到引擎」但我明明装了**
在终端跑 `omp --version` 看有没有输出。没有就是 PATH 没进（装完要重开终端）。
有输出但在应用里找不到，去设置页填显式路径，点「重新检测」。

**流式输出卡住不动**
看日志里最后一条 `<-` 是什么。如果停在 `tool_execution_start` 而没有 `_end`，
是工具卡住了（不是界面卡了）；点停止或重启引擎。

**切到 pi 之后子代理面板没了**
这是设计。pi 没有子代理。

**会话列表里少了几个会话**
列表按 `cwd` 过滤，用的是会话文件头部的 `cwd`。如果那个会话是在别的目录开的，
切到那个工作目录才看得到。**这是对的**，不要改成「全列」。
另外列表只显示**当前引擎**的会话——切引擎会换一批。

**工作区页说「不是 Git 仓库」**
那是真的。点「初始化仓库」，或者先在终端里 `git init`。

**pi 起不来，报启动超时**
pi 是 Node 程序，冷启动 3~4 秒；装了很多扩展会更久。握手给的是 25 秒。
如果还超时，直接在终端跑 `pi --mode rpc` 看它是不是真的能起来。

**删除会话报「正在使用」**
该会话在当前进程里跑着。先停止当前轮，或者换一个会话再删。
