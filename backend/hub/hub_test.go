package hub

import (
	"sync"
	"testing"
)

// 锁住「退订后被 close 的 channel 仍被写入导致 panic」这一真实故障：
// 断言只有一条——并发 Emit 与退订不 panic（丢帧由 seq 跳号与 chat:gap 兜住）。
func TestEmitAndUnsubscribeConcurrently(t *testing.T) {
	h := New(64)

	for round := 0; round < 50; round++ {
		sub, _, cancel := h.Subscribe(0)
		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				h.Emit("s1", "chat:patch", map[string]any{"i": i})
			}
		}()

		// 读侧靠 Done 退出，不依赖通道被 close——这正是修法的核心。
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-sub.Done():
					return
				case <-sub.Stream():
				}
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			cancel()
		}()

		wg.Wait()
	}
}

// 慢消费者必须丢最旧的一帧，而不是阻塞广播。
func TestSlowConsumerDropsOldest(t *testing.T) {
	h := New(64)
	sub, _, cancel := h.Subscribe(0)
	defer cancel()

	const burst = 600 // 远超通道容量 256
	for i := 0; i < burst; i++ {
		h.Emit("", "chat:patch", map[string]any{"i": i})
	}

	// 广播全程不该阻塞；缺口应被记下来，供读协程补一条 chat:gap。
	if from, to, ok := sub.TakeGap(); !ok || from == 0 || to < from {
		t.Fatalf("慢消费者应记下丢帧区间，得到 from=%d to=%d ok=%v", from, to, ok)
	}

	var last int64
	for {
		select {
		case f := <-sub.Stream():
			last = f.Seq()
			continue
		default:
		}
		break
	}
	if last != int64(burst) {
		t.Fatalf("最后一帧应是最后一个序号 %d，实际 %d", burst, last)
	}
}

// 重放窗口内的帧要补齐；窗口外不补（靠快照对账）。
func TestReplayOnlyWithinWindow(t *testing.T) {
	h := New(8)
	for i := 0; i < 20; i++ {
		h.Emit("", "chat:patch", map[string]any{"i": i})
	}

	_, replay, cancel := h.Subscribe(0) // since=0 表示不重放
	cancel()
	if len(replay) != 0 {
		t.Fatalf("since=0 不该重放，得到 %d 字节", len(replay))
	}

	_, replay, cancel = h.Subscribe(15) // 窗口内 16..20
	cancel()
	if got := countLines(replay); got != 5 {
		t.Fatalf("since=15 应重放 5 帧，实际 %d", got)
	}

	_, replay, cancel = h.Subscribe(1) // 全在窗口外，最多补满环形
	cancel()
	if got := countLines(replay); got != 8 {
		t.Fatalf("环形窗口只有 8 帧，最多重放 8 帧，实际 %d", got)
	}
}

func countLines(b []byte) int {
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}
