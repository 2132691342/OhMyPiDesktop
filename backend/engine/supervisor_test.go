package engine

// 实例回收：实例数有界、忙引擎不被回收、并发同键同实例、停止清空、退出收尾。
// 这一层错了的两种表现都很安静：实例无限增长（每个占着一个子进程与一份内存），
// 或者正在回答的那一轮被回收掉（用户看到的是回答突然断了）。

import (
	"testing"
	"time"
)

// ---------------- 实例回收 ----------------

// newTestSupervisor 建一个不带事件出口的监督者。
func newTestSupervisor() *Supervisor {
	return NewSupervisor(
		func(string, string, any) {},
		map[Kind]Dialect{KindOMP: fakeDialect{}, KindPi: fakeDialect{}},
		func(Kind) (string, []string, []string) { return "", nil, nil },
		nil,
	)
}

// newTestSupervisorWith 建一个能收事件的监督者：事件出口在装配完成后才注入。
func newTestSupervisorWith(emit Emitter) *Supervisor {
	s := newTestSupervisor()
	s.SetEmitter(emit)
	return s
}

// tempDir 造一个互不相同的工作目录（进程键的一部分）。
func tempDir(i int) string { return "/tmp/probe-" + string(rune('a'+i)) }

func countEngines(s *Supervisor) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.engines)
}

// backdate 把所有实例的最后使用时间往前推，模拟空闲。
func backdate(s *Supervisor, d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := time.Now().Add(-d)
	for _, e := range s.engines {
		e.mu.Lock()
		e.lastUsed = old
		e.mu.Unlock()
	}
}

// 实例数必须有界，且正在回答的引擎绝不能被回收。
// 回收掉正在回答的引擎会打断用户正在看的那一轮；不回收则实例无限增长，
// 每个进程都占着一个子进程与一份内存。
func TestSupervisorRecyclesEngines(t *testing.T) {
	t.Run("反复取不同键时实例数有界", func(t *testing.T) {
		s := newTestSupervisor()
		defer s.Shutdown()
		for i := 0; i < 6; i++ {
			s.Engine(KindOMP, tempDir(i))
			s.Engine(KindPi, tempDir(i))
		}
		if got := countEngines(s); got > maxEngines {
			t.Fatalf("建出 %d 个实例，超过上限 %d", got, maxEngines)
		}
		backdate(s, idleTTL+time.Minute)
		if n := s.Sweep(); n == 0 {
			t.Fatal("Sweep 没回收任何实例")
		}
		if got := countEngines(s); got != 0 {
			t.Errorf("全部空闲超时后应清空，实际剩 %d", got)
		}
	})

	t.Run("上限在取引擎时就生效", func(t *testing.T) {
		s := newTestSupervisor()
		defer s.Shutdown()
		for i := 0; i < maxEngines*4; i++ {
			s.Engine(KindOMP, tempDir(i))
		}
		if got := countEngines(s); got > maxEngines {
			t.Errorf("取 %d 次后实例数 %d 超过上限 %d", maxEngines*4, got, maxEngines)
		}
	})

	t.Run("正在回答的引擎不被回收", func(t *testing.T) {
		s := newTestSupervisor()
		defer s.Shutdown()
		e := s.Engine(KindOMP, tempDir(1))
		e.mu.Lock()
		e.runID = "RUN_BUSY"
		e.mu.Unlock()
		backdate(s, idleTTL+time.Hour)
		if n := s.Sweep(); n != 0 {
			t.Errorf("正在回答的引擎被回收了（%d 个）", n)
		}
	})
}

// 并发取同一键必须返回同一实例，否则两个 goroutine 各拉一个进程抢同一 cwd。
func TestConcurrentEngineSameInstance(t *testing.T) {
	s := newTestSupervisor()
	defer s.Shutdown()
	dir := tempDir(9)
	const n = 32
	got := make([]*Engine, n)
	done := make(chan int, n)
	for i := 0; i < n; i++ {
		go func(i int) { got[i] = s.Engine(KindOMP, dir); done <- i }(i)
	}
	for i := 0; i < n; i++ {
		<-done
	}
	for i := 1; i < n; i++ {
		if got[i] != got[0] {
			t.Fatalf("第 %d 次取到的是另一个实例", i)
		}
	}
	if c := countEngines(s); c != 1 {
		t.Errorf("并发取同一键应只建一个实例，实际 %d", c)
	}
}

// StopAll / StopKind 之后实例表必须清空，不留已死进程的僵尸句柄。
func TestStopClearsRegistry(t *testing.T) {
	s := newTestSupervisor()
	defer s.Shutdown()
	s.Engine(KindOMP, tempDir(1))
	s.Engine(KindPi, tempDir(1))
	s.StopAll()
	if got := countEngines(s); got != 0 {
		t.Errorf("StopAll 后实例表应为空，实际 %d", got)
	}

	s2 := newTestSupervisor()
	defer s2.Shutdown()
	s2.Engine(KindOMP, tempDir(1))
	s2.Engine(KindPi, tempDir(1))
	s2.StopKind(KindPi)
	if _, ok := s2.engines[key(KindPi, tempDir(1))]; ok {
		t.Error("pi 的实例没被摘除")
	}
	if got := countEngines(s2); got != 1 {
		t.Errorf("应只剩 omp 一个实例，实际 %d", got)
	}
}

// Shutdown 可重复调用，不 panic。
func TestShutdownIdempotent(t *testing.T) {
	s := newTestSupervisor()
	s.Engine(KindOMP, tempDir(1))
	s.Shutdown()
	s.Shutdown()
}

// 进程退出时的收尾：正在回答的那一轮要自己发 done 与 error；
// 空闲时退出则不该发——否则界面白白收掉一轮不存在的话。
func TestEngineExitFinishesTurn(t *testing.T) {
	t.Run("死掉的引擎收尾那一轮", func(t *testing.T) {
		events := make(chan [2]any, 8)
		s := newTestSupervisorWith(func(_ string, name string, data any) {
			events <- [2]any{name, data}
		})
		defer s.Shutdown()

		e := s.Engine(KindOMP, tempDir(2))
		runID, _, _, _ := e.beginRun()
		e.tr.BeginTurn(runID, "在吗", "omp/x")

		// 模拟进程死亡：OnExit 的那条路径。
		e.AbortRun("引擎进程意外退出")

		var sawDone, sawError bool
		for i := 0; i < 4; i++ {
			select {
			case ev := <-events:
				name, _ := ev[0].(string)
				switch name {
				case "chat:done":
					sawDone = true
				case "chat:error":
					sawError = true
				}
			default:
			}
		}
		if !sawDone {
			t.Error("引擎死亡后没有发出 chat:done：界面会永远停在「正在回答」")
		}
		if !sawError {
			t.Error("引擎死亡后没有发出 chat:error：用户看不到这一轮为什么断")
		}
		if e.RunID() != "" {
			t.Errorf("收尾后 runID 应清空，实际 %q", e.RunID())
		}
	})

	t.Run("空闲时退出不发收尾事件", func(t *testing.T) {
		events := make(chan [2]any, 8)
		s := newTestSupervisorWith(func(_ string, name string, data any) {
			events <- [2]any{name, data}
		})
		defer s.Shutdown()

		e := s.Engine(KindOMP, tempDir(3))
		e.AbortRun("引擎已停止")

		for {
			select {
			case ev := <-events:
				if name, _ := ev[0].(string); name == "chat:done" || name == "chat:error" {
					t.Errorf("空闲时不应发 %s", name)
				}
			default:
				return
			}
		}
	})
}
