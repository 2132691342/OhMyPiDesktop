# SPEC-05 HTTP 与 SSE（server / hub）

## 定位

`server` 是唯一的 HTTP 出口：路由、统一响应外壳、回环与令牌校验。
`hub` 是唯一的事件出口：`seq` 排列、环形重放、慢消费者丢帧。

两者都**不做业务判断**——不会因为「这个操作不该允许」而拒绝，那由 `service` 返回错误码。

## 设计

```
server
├── 中间件：CORS → 回环校验 → 令牌校验 → 方法白名单（只 GET/POST）
├── run()：把 handler 的 (data, error) 变成统一外壳
└── SSE：/events → 订阅 hub

hub
├── seq      单调递增（进程内唯一）
├── ring     1024 帧环形缓冲（断线重放）
└── fanout   每个订阅者一个有界 channel；满了丢最旧并发 chat:gap
```

### 中间件顺序是硬要求

```
r.Use(s.cors())    // 处理 OPTIONS 预检并结束
r.Use(s.guard())   // 回环 / 令牌 / 方法白名单
```

浏览器的自定义头请求（`X-Desktop-Token`）会先发 `OPTIONS` 预检。
`guard` 只放行 GET/POST，**顺序反了，每个带令牌的请求都会先失败在预检上**，
前端看到的是 `Network Error`——而不是 403，所以很难联想到是预检。

## 契约

### 响应外壳

```json
{ "code": "0", "message": "", "detail": "", "data": { } }
```

- `code` 是数字字符串；`"0"` 成功
- HTTP 恒 200（除 SSE 与鉴权失败 403）
- 切片字段永远不是 `null`

### 事件

```json
{"seq":42,"event":"chat:patch","data":{…},"sent_at":1793000000000}
```

- **没有** `event:` 帧头，事件名在 `data.event` 里
- 每 20 秒一行 `: ping` 保活
- 开场帧 `stream:open{seq}`

### hub 接口

```go
type Hub struct { … }

func (h *Hub) Emit(sessionID, name string, data any)
func (h *Hub) Subscribe(buf int) (*Sub, uint64)   // 返回开场 seq
func (h *Hub) Sub.Close()

type Sub struct { … }   // 有界 channel，满则丢最旧
```

## 关键流程

### 一次 Emit

```
Emit(sessionID, name, data):
  seq := atomic.AddUint64(&h.seq, 1)
  frame := Frame{Seq: seq, Event: name, Data: data, SentAt: now}
  raw := marshal(frame)              // 只序列化一次
  h.mu.RLock()
  for _, sub := range h.subs:
      sub.push(raw, seq)             // 非阻塞；持有读锁
  h.mu.RUnlock()
  h.ring.push(raw)                   // 环形，容量满则覆盖最旧
```

**为什么序列化只做一次**：如果有 5 个订阅者，逐个序列化就是 5 倍 CPU，
而且大帧（整表 `blocks`）会让广播延迟随帧大小线性增长。序列化一次、共享 `[]byte`，
投递才是 O(订阅者数)。

**为什么 push 必须非阻塞**：

```
select {
case sub.ch <- raw:
default:
    // 满：丢最旧，再塞；仍然塞不下就丢这一帧并发 chat:gap
}
```

**绝不能**写成阻塞发送。一个卡住的前端连接会让整个后端停摆。

### 关闭订阅（这是唯一可能 panic 的地方）

```
错误做法（曾出现）：
  Emit 里 if !sub.dead { sub.ch <- raw }   // 查完 dead 之后 sub.ch 被 close
                                            // → send on closed channel，整个进程崩

正确做法：
  1. 订阅者 channel 永远不由订阅端 close；
  2. Sub.Close() 只置 dead 标志并从 h.subs 摘除（持写锁）；
  3. Emit 持**读锁**遍历——摘除需要写锁，因此「遍历中摘除」不会发生；
  4. channel 交给 GC。
```

一句话：**永不 close 一个可能还有人写的 channel**。

### 断线重连

```
GET /events?since=41
  ├─ since 在环形窗口内 → 先补发 42..last
  └─ since 太旧（窗口外）→ 直接发 stream:open{seq} 让前端自己拉快照
```

**为什么不补齐一切**：假装补全会让界面带着半截状态继续跑，比明确对账更危险。

### 前端对账

```
前端维护 lastSeq
  收帧：seq == lastSeq+1 → 接受，lastSeq = seq
        seq >  lastSeq+1 → 跳号，接受该帧，并触发一次全量对账（拉 chat/state）
        seq <= lastSeq   → 丢弃（重连重复）
```

## 约束

| 约束 | 原因 |
|---|---|
| 只允许 GET / POST | 没有 PUT/DELETE 就没有「顺手加个删除接口」的入口 |
| 除 `/health` 外都要令牌 | 回环端口也可能被本机其它进程扫描 |
| SSE 用 `?token=` | `EventSource` 不能设请求头 |
| 恒 200 + body 里的 `code` | 前端拦截器统一抛 `ApiError`，一处处理 |
| Emit 序列化一次共享字节 | 大帧广播不能变成 O(订阅者 × 帧大小) |
| push 非阻塞 | 一个卡住的前端不能让后端停摆 |
| 永不 close 订阅 channel | 唯一的 panic 面 |
| 环形窗口 1024 帧 | 内存恒定；窗口外靠快照对账 |
| 切片字段不为 null | 前端不用到处判空 |

## 取舍

| 取舍 | 代价 | 收益 |
|---|---|---|
| HTTP 恒 200 | 不符合 REST 洁癖 | 前端一个拦截器处理所有错误 |
| 只 GET/POST | 语义不直观（`/sessions/delete`） | 写操作可枚举，审计一眼看完 |
| 丢帧而不是阻塞 | 慢消费者会丢数据 | 一个前端卡住不影响别人 |
| 窗口外不补 | 长断线要走快照 | 内存恒定，状态可对账 |
| 单一 hub 无优先级 | 大帧和小帧同样待遇 | 实现简单，行为可预测 |
