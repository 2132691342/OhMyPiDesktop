# HTTP 与 SSE 契约

## 统一外壳

```json
{ "code": "0", "message": "", "detail": "", "data": { } }
```

- `code` 是**数字字符串**：`"0"` 成功，其余是 `pkg.AppError` 的错误码（见 `docs/ERROR-CODES.md`）
- HTTP 状态码恒为 200（除 SSE 与鉴权失败）；前端按 `code` 分流
- 契约里的切片字段永远不是 `null`（构造处一律初始化）

## 中间件顺序（是硬要求）

```
r.Use(s.cors())    // 处理 OPTIONS 预检并结束
r.Use(s.guard())   // 回环 / 令牌 / 方法白名单
```

浏览器的自定义头请求（`X-Desktop-Token`）会先发 `OPTIONS` 预检。
`guard` 只放行 GET/POST，**顺序反了，每个带令牌的请求都会先失败在预检上**，
前端看到的是 `Network Error` 而不是 403——很难联想到是预检的问题。

## 路径约定

`/api/v1/{resource}/{action}`：动作段在最后。只允许 **GET 与 POST**。

## 鉴权与来源

- 只接受回环来源（`127.0.0.1` / `::1`）
- 除 `/health` 外都要 `X-Desktop-Token`（SSE 用 `?token=`，因为 `EventSource` 不能设头）
- 令牌在启动时随机生成，只通过 Wails 绑定 `Endpoint()` 交给前端
- 只允许 GET / POST：没有 PUT/DELETE，就没有「顺手加一个删除接口」的入口

**中间件顺序是硬要求**：`cors` 必须在 `guard` 之前。浏览器的自定义头请求会先发
`OPTIONS` 预检，而 `guard` 只放行 GET/POST——顺序反了，每个带令牌的请求都会先失败在预检上。

## 引擎标识

凡是「跟某个引擎说话」的接口都带 `engine` 参数（`omp` / `pi`）。
省略时用当前选中引擎（设置键 `app.engine`），会话相关接口还会退化成
「按会话文件所在目录推断引擎」。

能力差异由 `Bootstrap.caps` 与 `ChatState.caps` 表达，前端据此隐藏控件——
**不要靠接口报错来发现「不支持」**。

## 路由表

### 系统

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/health` | 存活探测（不校验令牌） |
| GET | `/bootstrap` | 首屏聚合：版本、引擎清单与能力、当前引擎、工作目录、设置、各引擎落盘布局、最近工作区、技能数、会话数、安装命令原文 |
| GET | `/settings` | 界面设置 KV |
| POST | `/settings/save` | 批量保存设置 `{key: value}` |
| POST | `/settings/workspace` | 切工作目录 `{path}`；成功后该目录进「最近」并置顶 |
| POST | `/settings/workspace/forget` | 从「最近」里去掉 `{path}` → `{workspaces}`（只动记忆，不动目录） |
| POST | `/settings/engine` | 切当前引擎 `{engine}` |
| GET | `/logs` | 最近 500 行日志 |
| GET | `/prefs` | 应用偏好快照：设置 KV、`auto_start`（**真读注册表**）、`restore_session`、`last_session_file` 与标题、`data_dir`、`log_dir` |
| POST | `/prefs/autostart` | 开关开机自启 `{enabled}` → 同 `/prefs`（写 `HKCU\...\Run`，不需要管理员） |
| POST | `/prefs/restore` | 开关「启动时回到上次会话」`{enabled}` → 同 `/prefs` |
| POST | `/prefs/session` | 记住当前会话 `{engine_kind, session_file}` → 同 `/prefs` |
| POST | `/prefs/data-dir` | 在资源管理器里打开数据目录 |
| GET | `/config/files?engine=` | 引擎配置文件清单（`file_path / file_label / exists / size_bytes`） |
| GET | `/config/text?engine=&file=` | 某份配置的原文（**只读**；`file` 必须是清单里的那个，否则拒绝） |
| POST | `/system/reveal` | 在资源管理器里定位 `{path}` |
| POST | `/system/open` | 用默认程序打开 `{path}` |

### 引擎（探测）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/engines` | 引擎状态：每个引擎的 `kind / exe / version / source / channel / caps / installed / layout`，加上在跑的进程 `running[]` |
| POST | `/engines/detect` | 重新探测（忽略一分钟缓存），返回同 `/engines` |
| POST | `/engines/restart` | 重启 `{engine?}`；省略则重启全部 |

### 应用（装 / 升级 / 卸载）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/apps` | 两行：`kind / app_name / installed / version / exe / source / channel / channel_label / latest_version / latest_note / outdated / install_command / update_command / uninstall_command / docs_url / can_install / can_update / can_uninstall / blocker / busy`。`refresh=1` 忽略 30 分钟的「最新版」缓存 |
| POST | `/apps/install` | `{engine}` → 同 `/apps` |
| POST | `/apps/update` | `{engine}` → 同 `/apps` |
| POST | `/apps/uninstall` | `{engine}` → 同 `/apps` |

三条动作都**同步等到命令跑完**才回响应，进度通过 SSE 的 `app:progress` 逐行到达。
返回值是整个清单而不是被改的那一行：装完两个引擎的相对状态可能都变了
（比如 omp 装上了，界面里「缺引擎」的横幅就该消失）。

> **边界：“能装”不等于“我们自己实现安装”。** 三个动作跑的都是引擎官方发布的
> 安装器与包管理器命令，且与 `install_command` 里展示的那串**完全是同一行**。
> 没有 `/apps/rollback`、没有内置运行时、没有自建分发源。
> 渠道判不出来时 `can_uninstall=false` 且 `blocker` 给一句手动路径——
> 界面那时**不渲染**卸载按钮。详见 [modules/apps.md](modules/apps.md)。

### 会话

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/sessions?engine=&workspace=` | 会话列表，来自该引擎的会话目录。`engine` 省略时合并两个引擎 |
| GET | `/sessions/detail?engine=&path=` | 会话详情（元信息 + 投影条目） |
| POST | `/sessions/delete` | 删除会话 `{engine?, path}`（成对删 `.jsonl` 与同名 artifacts 目录） |
| POST | `/sessions/rename` | 改名 `{engine?, path, name}`（走 RPC，需要引擎在跑） |

### 对话

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/chat/state?engine=&session=` | 对话页快照：模型 / 思考档 / 上下文 / 用量 / 历史轮 / 命令 / 队列 / **caps** |
| POST | `/chat/send` | 发一轮 `{engine, session, message}`，返回 `run_id` |
| POST | `/chat/stop` | 停止 `{engine, session}` |
| POST | `/chat/steer` | 插话 `{engine, session, message}` |
| POST | `/chat/followup` | 排队 `{engine, session, message}` |
| POST | `/chat/clear-queue` | 清空排队 `{engine, session}`（仅 `caps.queue_clear`） |
| POST | `/chat/remove-queued` | 移除单条排队 `{engine, session, message, queue}` → `{removed}`（仅 `caps.queue_edit`） |
| POST | `/chat/compact` | 压缩上下文 `{engine, session, instructions?}` |
| POST | `/chat/answer` | 回答提问 `{engine, session, request}` |
| POST | `/chat/new` | 在当前目录开新会话 `{engine}` |
| POST | `/chat/model` | 换模型 `{engine, session, provider, model}` |
| POST | `/chat/thinking` | 换思考档 `{engine, session, level}` |
| POST | `/chat/autocompact` | 自动压缩开关 `{engine, session, enabled}` |

### 子代理（仅 omp）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/agents?engine=` | roster 快照（`get_subagents` 直出 + 本地字节游标） |
| GET | `/agents/messages?engine=&id=&from=` | 子代理对话增量 `{entries, messages, next, reset}` |
| POST | `/agents/steer` | `{engine, id, message}` |
| POST | `/agents/cancel` | `{engine, id}`（返回 `cancelled:false` 是幂等，不是错误） |
| POST | `/agents/subscribe` | `{engine, level}`（`off｜progress｜events`） |

> pi 下这五条返回 `code=5001`（`CodeEngineUnsupported`）。前端据 `caps.subagents`
> 根本不去调——**先看能力，再看错误**。

### 模型与服务商（配置格式分叉）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/models?engine=` | 引擎眼下的可用模型（`get_available_models`） |
| GET | `/providers?engine=` | 服务商视图（密钥打码，附 `has_key` / `api_key`） |
| POST | `/providers/upsert` | 写一个服务商 `{engine, name, provider}` |
| POST | `/providers/delete` | 删一个服务商 `{engine, name}` |
| GET | `/login/providers?engine=` | 可登录服务商与登录态（**仅 omp**） |
| POST | `/login/start` | 发起 OAuth `{engine, provider}`（**仅 omp**） |

> omp 写 `models.yml`（YAML 节点编辑，保留注释与未知键）；
> pi 写 `models.json`（JSONC 读取，只覆盖我们掌管的 provider 键）。
> 两者 schema 零重叠，所以是两个包：`backend/env/ompcfg` 与 `backend/env/picfg`。

### 技能与插件

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/skills?engine=` | 技能列表（带 `kind` 字段区分归属） |
| GET | `/skills/content?path=` | 技能原文 |
| POST | `/skills/save` | 覆写技能正文 `{engine_kind, file_path, text}` → `{content}`（只允许用户目录里的 `SKILL.md`，≤256 KB） |
| POST | `/skills/toggle` | 启停 `{engine, path, enabled}`（只允许用户目录） |
| POST | `/skills/create` | 新建 `{engine, name, description, body}` |
| POST | `/skills/delete` | 删除 `{engine, path}`（只允许用户目录） |
| GET | `/plugins?engine=` | 已装插件（omp: `omp plugin list --json`；pi: `pi list --json`） |
| POST | `/plugins/install` | 装 `{engine, source}` |
| POST | `/plugins/uninstall` | 卸 `{engine, source}` |

### 工作区（文件与 Git）

所有路径都是**相对当前工作目录**的，越界由 `pkg.SafeJoin` 拦住（含符号链接）。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/files?path=` | 列目录（单层，500 条上限，目录在前） |
| GET | `/files/read?path=` | 单文件预览（512 KB 上限，二进制只给标记） |
| GET | `/git/status?path=` | 版本状态；不是仓库或没装 git 时 `available/is_repo` 为假并给 `reason` |
| GET | `/git/diff?path=` | 差异行；`path` 为空表示整个工作区 |
| GET | `/git/history?limit=` | 提交列表（≤200） |
| GET | `/git/commit?hash=` | 单次提交的完整内容（元信息 + 改动文件 + 整份差异） |
| GET | `/git/file-history?path=&limit=` | 某个文件的提交历史（≤200） |
| GET | `/git/file-diff?hash=&path=` | 一次提交里某个文件的逐行差异 |
| GET | `/git/branches` | 本地 + 远端分支 |
| POST | `/git/stage` | `{paths}` |
| POST | `/git/unstage` | `{paths}` |
| POST | `/git/discard` | `{paths}`（不可撤销，界面二次确认） |
| POST | `/git/resolve` | `{paths}` 标记冲突已解决 |
| POST | `/git/commit` | `{message, amend}` |
| POST | `/git/branch` | 新建 `{name}` |
| POST | `/git/switch` | 切换 `{name}` |
| POST | `/git/branch/delete` | 删除 `{name}` |
| POST | `/git/merge` | `{name, rebase}` |
| POST | `/git/state` | `{action: continue｜abort}` |
| POST | `/git/init` | `{branch}` |
| POST | `/git/remote` | `{name, url}` |
| POST | `/git/push` | `{remote, branch, force}`（`none｜lease｜force` 分级） |
| POST | `/git/pull` | `{remote, branch, rebase}` |
| POST | `/git/fetch` | `{remote}` |

> 写操作全部是「白名单动作」，且每次都重新读一遍 `status` 返回。
> 界面不该自己推断「刚才那个文件现在在哪」——那会造出第二套状态。

### 事件流

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/events?since=&token=` | SSE 事件流 |

## SSE

```
id: 42
data: {"seq":42,"event":"chat:patch","data":{…},"sent_at":1793000000000}

```

- **没有** `event:` 帧头：事件名在 `data.event` 里。这样前端只需要一个 `onmessage`，
  也不会因为浏览器对自定义事件名的处理差异而出错。
- `seq` 单调递增。断开重连带 `?since=<lastSeq>` 或 `Last-Event-ID`，
  后端先补发环形窗口（1024 帧）内的帧；窗口外不补，前端据 `seq` 跳号自行拉快照。
- 每 20 秒发一行注释 `: ping` 保活。
- 开场帧 `stream:open` 带当前 `seq`。
- **投递必须 O(1)**：慢消费者丢最旧的帧并发 `chat:gap`，绝不阻塞广播。

### 服务端实现要点

事件总线（`backend/hub`）是唯一出口，对外只有三个动作：

```go
func (h *Hub) Emit(sessionID, name string, data any)
func (h *Hub) Subscribe(buf int) (*Sub, uint64)   // 返回开场 seq
func (h *Hub) Sub.Close()
```

`Sub` 持一个有界 channel；满了就丢最旧的帧并发 `chat:gap`。
帧只序列化一次，多个订阅者共享同一份 `[]byte`——大帧广播不能变成
O(订阅者数 × 帧大小)。

**关闭订阅的规则**（唯一的 panic 面）：

1. 订阅者的 channel 永远不由订阅端 `close`；
2. `Sub.Close()` 只置 `dead` 标志并从集合摘除（持写锁）；
3. `Emit` 持**读锁**遍历——摘除需要写锁，因此「遍历中摘除」不会发生；
4. channel 交给 GC。

一句话：**永不 close 一个可能还有人写的 channel**。

### 断线重连与前端对账

```
GET /events?since=41
  ├─ since 在环形窗口内 → 先补发 42..last
  └─ since 太旧（窗口外）→ 直接发 stream:open{seq}，让前端自己拉快照
```

前端维护 `lastSeq`：

| 收到的 seq | 动作 |
|---|---|
| `seq == lastSeq+1` | 接受，`lastSeq = seq` |
| `seq > lastSeq+1` | 跳号：接受该帧，并触发一次全量对账（拉 `chat/state`） |
| `seq <= lastSeq` | 丢弃（重连重复） |

**为什么不补齐一切**：假装补全会让界面带着半截状态继续跑，比明确对账更危险。

### 事件表

| 事件 | 载荷要点 |
|---|---|
| `stream:open` | `seq`：当前序号 |
| `chat:start` | `run_id` / `kind` / `work_dir` / `session_id` / `user_message` |
| `chat:turn` | 整轮骨架 `{run_id, user_message, blocks, model}` |
| `chat:patch` | **增量**：`{run_id, mode, index, kind}`，`mode=delta` 带 `delta`，`mode=block` 带 `block` |
| `chat:blocks` | 整表替换（换助手消息时） |
| `chat:turn_end` | `{run_id, usage?}` |
| `chat:done` | `{run_id, stop_reason}`：omp 看 `yielded`，pi 看 `agent_settled` |
| `chat:settled` | 整段静默 |
| `chat:prompt_result` | `{run_id, status, settled, message?, code?}`（仅 omp） |
| `chat:error` | `{run_id, message}` |
| `chat:agent_start` | `{run_id}` |
| `chat:ask` | 提问（`method` + 该基元的字段） |
| `chat:notify` | `{level, message}` |
| `chat:status` | `{key, text}`（空 text 即清除） |
| `chat:widget` | `{key, lines, placement}` |
| `chat:queue` | `{steering, followUp, liveSteered}` |
| `chat:retry` / `chat:retry_end` | 自动重试进度 |
| `chat:compaction` | `{state: start｜end}` |
| `chat:notice` | 引擎的 notice 帧 |
| `chat:command_output` | 内置斜杠命令的输出 |
| `chat:open_url` | `{url, instructions?}`（仅 omp） |
| `chat:editor_text` | 引擎请求回填输入框 |
| `chat:goal` | 目标更新（仅 omp） |
| `chat:gap` | 丢帧通知（慢消费者 / 超大帧） |
| `engine:ready` | 引擎就绪 `{kind, work_dir, pid, protocol}` |
| `engine:exit` | 引擎退出 `{kind, work_dir, reason}` |
| `engine:commands` | 可用斜杠命令列表（仅 omp 主动推） |
| `engine:config_changed` | 模型 / 思考档变化 |
| `engine:extension_error` | 扩展报错，只进日志 |
| `session:info` / `session:title` | 会话标题变化（omp / pi 帧名不同，事件名统一） |
| `session:entry` | pi 的 `entry_appended`（会话条目追加） |
| `subagent:lifecycle` | `{id, agent, agent_source, status, index, description?, session_file?, parent_tool_call_id?}`（仅 omp） |
| `subagent:progress` | `{id, index, agent, task, progress{…}}`（仅 omp） |
| `subagent:event` | 子代理原始会话事件（仅 omp，`events` 档） |
| `agents:cleared` | roster 与游标已清空（会话切换后由后端主动发） |
| `app:progress` | 装 / 升 / 卸的进度：`{kind, action, phase, line?, command?, message?}`。`phase` = `start｜line｜done｜fail`；`start` 帧带 `command`（要跑的命令原文），`line` 帧带一行输出 |

## 契约版本

`backend/server.ContractVersion` 与 `frontend/src/types/api.ts` 手工对齐；
改响应字段形态必须同时 +1。

当前：`7`。响应字段的**形态**变化（增删字段、改名、变类型）必须同时 +1；
只增可选字段不需要。

新增端点本身不需要升版本（前端没调它就不受影响）；
但改了已有端点的响应字段名就必须升——那是「已经写好的前端读完会拿到 undefined」。
