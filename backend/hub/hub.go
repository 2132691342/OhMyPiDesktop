// Package hub 是事件总线：引擎与安装进度从后端推给前端的唯一出口（SSE）。
//
// 它不做业务判断，也不解释事件语义——只负责三件事：给帧编号、留一段可重放的窗口、
// 把帧非阻塞地分发给每个连接。
package hub

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"
)

// Event 是推给前端的一帧。
type Event struct {
	Seq    int64           `json:"seq"`
	Event  string          `json:"event"`
	Data   json.RawMessage `json:"data"`
	SentAt int64           `json:"sent_at"`
}

// frame 是通道里传的东西：raw 已经序列化好，seq 用于合成丢帧通知。
//
// 序列化只做一次、多连接共享同一个 []byte：否则大帧（整表 blocks）的广播成本
// 会随连接数与帧大小相乘。seq 单独存是因为丢帧时要拿它合成 chat:gap。
type frame struct {
	seq int64
	raw []byte
}

// subscriber 是一个 SSE 连接。
//
// ch 永不 close——订阅端摘除只置 dead 并关闭 done。close 一个可能还有人写的
// channel 会 panic，而 panic 在广播路径上等于整个后端退出。
type subscriber struct {
	ch   chan frame
	done chan struct{}
	dead atomic.Bool

	gapMu  sync.Mutex
	gapFrom int64 // 已丢弃的最小 seq
	gapTo   int64 // 已丢弃的最大 seq
}

// Hub 是带环形重放的广播器。
//
// 为什么要重放：窗口失焦 / WebView 休眠会断流，重连后前端不该丢掉断流期间
// 的正文增量。环形只保留最近 N 帧——会话历史走文件快照，不走重放。
type Hub struct {
	mu   sync.RWMutex
	subs map[*subscriber]struct{}
	ring []frame
	seq  int64
	ringN int
}

// New 构造总线。ring 是重放窗口大小。
func New(ring int) *Hub {
	if ring <= 0 {
		ring = 512
	}
	return &Hub{subs: map[*subscriber]struct{}{}, ringN: ring}
}

// Emit 广播一帧。sessionID 为空表示全局通道。
//
// 事件名固定成 `namespace:name` 形态，前端按名字分流；
// session_id 不进通道名——一个窗口只显示一个会话，按通道分会让重放变复杂。
func (h *Hub) Emit(sessionID, event string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		raw = []byte(`{"message":"事件编码失败"}`)
	}

	// 会话 id 塞进信封：前端据此丢弃别的会话的事件。
	// 只在需要时重编码一次，且发生在加锁之前——持锁做 marshal 会让广播延迟随帧大小线性增长。
	if sessionID != "" {
		var probe map[string]any
		if json.Unmarshal(raw, &probe) == nil && probe != nil {
			probe["session_id"] = sessionID
			if merged, err := json.Marshal(probe); err == nil {
				raw = merged
			}
		}
	}

	h.mu.Lock()
	h.seq++
	seq := h.seq
	ev := Event{Seq: seq, Event: event, Data: json.RawMessage(raw), SentAt: time.Now().UnixMilli()}
	payload, err := json.Marshal(ev)
	if err != nil {
		h.mu.Unlock()
		return
	}
	h.ring = append(h.ring, frame{seq: seq, raw: payload})
	if len(h.ring) > h.ringN {
		h.ring = h.ring[len(h.ring)-h.ringN:]
	}
	subs := make([]*subscriber, 0, len(h.subs))
	for s := range h.subs {
		subs = append(subs, s)
	}
	h.mu.Unlock()

	f := frame{seq: seq, raw: payload}
	for _, s := range subs {
		s.push(f)
	}
}

// Subscribe 注册一个连接，返回订阅者、重放帧与取消函数。
func (h *Hub) Subscribe(sinceSeq int64) (*subscriber, []byte, func()) {
	s := &subscriber{ch: make(chan frame, 256), done: make(chan struct{})}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	var replay []frame
	if sinceSeq > 0 {
		for _, f := range h.ring {
			if f.seq > sinceSeq {
				replay = append(replay, f)
			}
		}
	}
	h.mu.Unlock()

	var replayBytes []byte
	for _, f := range replay {
		replayBytes = append(replayBytes, f.raw...)
		replayBytes = append(replayBytes, '\n')
	}
	return s, replayBytes, func() {
		h.mu.Lock()
		delete(h.subs, s)
		h.mu.Unlock()
		s.kill()
	}
}

// Seq 返回当前序号。
func (h *Hub) Seq() int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.seq
}

// Stream 返回帧通道。通道永不关闭，退出靠 Done。
func (s *subscriber) Stream() <-chan frame { return s.ch }

// Done 在订阅被摘除时关闭。读协程同时等它和 Stream，才能确定地退出。
func (s *subscriber) Done() <-chan struct{} { return s.done }

// Seq 是这一帧的序号。
func (f frame) Seq() int64 { return f.seq }

// Raw 是已序列化好的 SSE data 内容。
func (f frame) Raw() []byte { return f.raw }

// TakeGap 取出并清空累积的缺口。读协程写出一帧之前先调它。
func (s *subscriber) TakeGap() (int64, int64, bool) { return s.takeGap() }

// push 非阻塞投递。满了就丢最旧的，并记下丢了哪一段——
// 读协程下次写盘前会先补一条 chat:gap，让前端去做一次快照对账。
func (s *subscriber) push(f frame) {
	if s.dead.Load() {
		return
	}
	select {
	case s.ch <- f:
		return
	default:
	}

	var dropped frame
	select {
	case dropped = <-s.ch:
	default:
	}
	s.noteGap(dropped.seq, dropped.seq)

	select {
	case s.ch <- f:
	default:
		// 极端情况（读协程卡住很久）：连这一帧也放不进去。
		// 只记缺口，不阻塞广播——前端据跳号自行对账。
		s.noteGap(f.seq, f.seq)
	}
}

func (s *subscriber) noteGap(from, to int64) {
	s.gapMu.Lock()
	defer s.gapMu.Unlock()
	if s.gapFrom == 0 || from < s.gapFrom {
		s.gapFrom = from
	}
	if to > s.gapTo {
		s.gapTo = to
	}
}

// takeGap 取出并清空累积的缺口。读协程写出一帧之前先调它。
func (s *subscriber) takeGap() (int64, int64, bool) {	s.gapMu.Lock()
	defer s.gapMu.Unlock()
	from, to := s.gapFrom, s.gapTo
	if from == 0 {
		return 0, 0, false
	}
	s.gapFrom, s.gapTo = 0, 0
	return from, to, true
}

// GapFrame 合成一条 chat:gap。seq 用「被丢弃的最后一帧」的序号，
// 前端看到跳号就会去拉快照——这正是我们想要的效果。
func GapFrame(seq, from, to int64) []byte {
	ev := Event{
		Seq:    seq,
		Event:  "chat:gap",
		Data:   json.RawMessage(`{"from":` + itoa(from) + `,"to":` + itoa(to) + `}`),
		SentAt: time.Now().UnixMilli(),
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		return []byte(`{"seq":0,"event":"chat:gap","data":{},"sent_at":0}`)
	}
	return raw
}

func (s *subscriber) kill() {
	if s.dead.CompareAndSwap(false, true) {
		close(s.done)
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
