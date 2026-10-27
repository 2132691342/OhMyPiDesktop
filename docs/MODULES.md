# 模块与规格索引

目录顺序即依赖顺序（上层在先、叶子在后）。每个模块只指向一份规格。
**一个概念只有一个落点**——想加功能先在这里找到它属于哪个模块。

| 模块 | 负责什么 | 不负责什么 | 规格 |
|---|---|---|---|
| `backend/pkg` | `AppError` / 错误码 / 原子写 / 环形日志 / `SafeJoin` | 任何业务判断 | — |
| `backend/domain` | 跨层模型：设置键、服务商视图、git 视图、文件视图 | 任何 IO | — |
| `backend/hub` | 事件总线：`seq`、环形重放、慢消费者丢帧 | 事件语义 | [SPEC-HTTP](specs/05-http-sse.md) |
| `backend/runtime` | 应用落盘路径 | 目录内容 | — |
| `backend/repo` | GORM + SQLite，只有设置 KV 一张表 | 引擎数据 | — |
| `backend/engine` | 引擎抽象：`Kind` / `Caps` / `Probe` / `Proc` / `Translator` / `Supervisor` | 具体命令语义 | [SPEC-ENGINE](specs/01-engine.md) |
| `backend/engine/omp` | omp 的启动参数、握手、v2 分片、私有帧、子代理 roster | pi 的任何东西 | [SPEC-ENGINE](specs/01-engine.md) |
| `backend/engine/pi` | pi 的启动参数、无握手、私有帧、能力降级 | 子代理 / 目标 / 待办 | [SPEC-ENGINE](specs/01-engine.md) |
| `backend/env` | 落盘布局（`Layout`）、探测（不安装） | 拉起进程 | [SPEC-ENV](specs/02-env.md) |
| `backend/env/ompcfg` | `models.yml` 的 YAML 节点级读写 | pi 的 JSON | [SPEC-ENV](specs/02-env.md) |
| `backend/env/picfg` | `models.json` 的 JSONC 读写 | omp 的 YAML | [SPEC-ENV](specs/02-env.md) |
| `backend/session` | 会话文件：目录遍历、头部、标题、用量累加、三点适配 | RPC | [SPEC-SESSION](specs/03-session.md) |
| `backend/skill` | 技能发现与启停（两套规则） | 会话 | [SPEC-SKILL](specs/04-skill.md) |
| `backend/git` | 工作区版本状态：status / diff / 历史 / 分支 / 白名单写入 | 任何 UI 决策 | [SPEC-GIT](specs/07-git.md) |
| `backend/service` | 用例编排：会话 / 对话 / 模型 / 技能 / 插件 / 子代理 / 工作区 | 解析文件格式、构造 HTTP | — |
| `backend/server` | gin 路由 / SSE / 统一响应外壳 / 回环与令牌 | 业务判断 | [SPEC-HTTP](specs/05-http-sse.md) |
| `frontend/src` | 见 [PAGE-STRUCTURE](PAGE-STRUCTURE.md) | — | — |

## 依赖方向（硬规则）

```
main ──→ server ──→ service ──→ engine ──→ pkg
  │        │           ├──→ git     ──→ domain / pkg
  │        │           ├──→ env     ──→ engine（只读类型）/ pkg
  │        │           ├──→ session / skill / repo / hub ──→ pkg / domain
  │        │           └──→ domain
  │        └──→ hub
  └──→ service
```

- `engine/omp` 与 `engine/pi` **互不 import**；共用部分上提到 `engine`
- `wails` 只允许在 `main.go` 出现
- `engine` 不认识 `service` / `server` / `repo` / `env`

## 引擎能力的唯一事实来源

任何「这个能力能不能做」的问题，答案在 [ENGINE-MATRIX.md](ENGINE-MATRIX.md)，
代码里的落点是 `backend/engine/engine.go` 的 `Caps`。两引擎差异集中在五处：

1. 握手（omp 有 `ready`，pi 没有）
2. 一轮结束判据（omp `yielded`，pi `agent_settled`）
3. 启动参数（pi 不接受 `--cwd`）
4. 配置格式（YAML vs JSONC）
5. 能力有无（子代理/目标/待办/登录/分片/排队编辑 = 仅 omp）

## 规格编号规则

- 编号不复用；删掉的规格留空号
- 每篇固定六节：**定位 → 设计 → 契约 → 关键流程 → 不变量 → 取舍**
- 「取舍」必须写代价，不写收益单边账
