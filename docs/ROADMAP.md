# 落地状态

本文件只记录**现在是什么状态**与**明确不做什么**。不记录改动过程。

## 已完成

| 领域 | 内容 |
|---|---|
| 引擎抽象 | `backend/engine`：`Kind` / `Caps` / `Identity` / `Dialect` / `Proc` / `Translator` / `Supervisor` / `SubagentStore` / 探测 |
| 双引擎 | `engine/omp`（`ready` 握手 + v2 分片 + 子代理 + OMP 私有帧）、`engine/pi`（无握手 + 能力降级 + PI 私有帧） |
| 探测 | 三步判据（`--version` 正则 → `--help` 复核），一分钟缓存，两引擎并发，装配阶段不探 |
| 会话 | 一套读取代码 + 三点适配（title 槽位 / 未知条目 / `model_change` 字段），`map[Kind]*Index` |
| 配置 | `env/ompcfg`（YAML 节点级）、`env/picfg`（JSONC 键级），统一视图在 `domain.ProviderView` |
| 技能 | 一套扫描 + 两套根 + 两种启停键（`enabled` / `disable-model-invocation`） |
| 插件 | 走引擎 CLI：`omp plugin list/install/uninstall`、`pi list/install/remove` |
| 工作区 | `backend/git`（status / diff / 历史 / 分支 / 白名单写入）+ 文件面板（只读） |
| 子代理 | omp 的 roster / 进度 / 字节游标对话 / 插话 / 取消；pi 下整块不渲染 |
| 事件出口 | `hub`：`seq` + 1024 帧环形重放 + 慢消费者丢最旧 + `chat:gap` |
| HTTP | 回环 + 随机端口 + 启动令牌 + 只允许 GET/POST；CORS 在 guard 之前 |
| 前端 | 八页（对话 / 工作区 / 会话 / 模型 / 技能 / 插件 / 用量 / 设置），引擎选择器，caps 驱动隐藏 |
| 视觉 | 五档靛蓝/石板、六级表面阶梯、按钮五态、UnoCSS 刻度接令牌 |

**验收证据**（真机跑过，不是推理）：

- omp：`chat:start → turn → patch → turn_end → turn → done`，回复内容正确，用量与上下文水位正确
- pi：同一句提问，回复正确，上下文 `38.9k / 1.00M`，会话落在 `~/.pi/agent/sessions/`
- 工作区：真实仓库显示 17 个未跟踪文件、逐文件 diff、分支列表
- 探测：两个引擎同时被识别（`omp/18.13.1` / `1.4.0`）

## 明确不做

| 不做 | 原因 |
|---|---|
| 引擎安装 / 升级 / 回滚 | 用户自己做；我们只探测 + 给命令原文 |
| 团队编排器 | omp 没有 spawn RPC，pi 连接口都没有 |
| 内置 Node / Bun 运行时 | 引擎自己内嵌了；再嵌几十 MB 没有收益 |
| 自建 MCP 管理界面 | 两引擎都没有 MCP 的 RPC 面；改配置要重启，收益低 |
| LSP / DAP 面板 | 协议层完全没有可观测面 |
| 微前端（Qiankun）/ 工作流（BPMN）/ 富文本编辑器 | 与「极简个人工作台」无关，不引入 |
| 在界面里编辑文件 | 写路径只留一条（引擎的工具）；两条写路径就说不清「谁改的」 |
| 另存会话 / 用量 | 引擎的 JSONL 是唯一账本；两本账迟早分叉 |

## 已知边界

| 项 | 现状 | 为什么先不做 |
|---|---|---|
| pi 的技能「彻底禁用」 | 只能摘出系统提示（`disable-model-invocation`），仍可 `/skill:name` 调 | 引擎没有这个开关；界面不发明 |
| pi 的排队条目编辑 | 只能「清空排队」 | 引擎没有移除单条的 RPC |
| pi 的命令清单 | 只能拉取，不订阅推送 | 引擎不推 |
| 会话导出 HTML | 引擎支持（`export_html`），界面未接入 | 使用频率低，先不做 |
| 非 Windows | 只有 `proc_windows.go` | 项目只面向 Windows |
| omp profile 切换 | 设置里可填，但不重建已拉起的进程 | 换 profile 要重开进程；当前要用户点「重启引擎」 |

## 门禁

任何改动结束都要跑：

```bash
go build ./backend/...
go test  ./backend/... -count=1
cd frontend && npm run build      # vue-tsc --noEmit 是硬门禁
```

改了协议翻译或会话投影后，**必须真起一个引擎跑一轮冒烟**：

```bash
go run ./tools/smoke -engine omp -msg "Reply with exactly: OK"
go run ./tools/smoke -engine pi  -msg "Reply with exactly: OK"
```
