package service

// 编排层只测会悄无声息出错的地方：探测的单飞与不阻塞、探测状态的语义（「还没探到」不能被当成「没装」）。
// 探测引擎与查最新版各有接缝（SetDetector / engine.SetLatestFetcher），测试里换掉它们，
// 不换的话测试就变成「测这台机器装了什么」。

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/hub"
	"OhMyPiDesktop/backend/repo"
)

// newTestApp 造一个不依赖本机引擎的 App：探测动作被换成可控的桩。
func newTestApp(t *testing.T) *App {
	t.Helper()
	store, err := repo.Open(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(Options{Store: store, Hub: hub.New(16), DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

// waitDetect 等后台补验跑完（首屏不阻塞，测试需等它落地）。
func (a *App) waitDetect(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !a.bgDetecting.Load() {
			// 拿一次探测锁：后台协程是「先拿锁再置标志」，
			// 所以能拿到就说明它真的退完了。
			a.detectMu.Lock()
			a.detectMu.Unlock()
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("后台探测 10 秒内没有结束")
}

// fakeDetector 造一个可控的探测桩。
//
// 探到的引擎返回「能用的」结论；没探到的返回原因串（空串表示结论可信）。
func fakeDetector(probes *atomic.Int64, reason string) func(context.Context, string, engine.Kind) (engine.Identity, string) {
	return func(_ context.Context, _ string, k engine.Kind) (engine.Identity, string) {
		if probes != nil {
			probes.Add(1)
		}
		if reason != "" {
			return engine.Identity{}, reason
		}
		return engine.Identity{Kind: k, Exe: "x.exe", Version: "1.0.0", Caps: engine.CapsFor(k)}, ""
	}
}

// ---------------- 探测：并发与缓存 ----------------

// 探测的并发与缓存语义一条钉全。首屏并发打多个探测请求时，单飞锁把它们合成
// 每引擎一次——不合并的话 Bootstrap / Health / EngineStatus 三个请求各起一轮探测，
// 多出六个子进程查同一件事；缓存命中不重探、强制重探绕过缓存是同一条流水线的两端。
func TestDetectSingleFlight(t *testing.T) {
	t.Run("并发请求合成一轮探测", func(t *testing.T) {
		app := newTestApp(t)
		var probes atomic.Int64
		restore := SetDetector(fakeDetector(&probes, ""))
		defer restore()

		const n = 12
		done := make(chan struct{}, n)
		for i := 0; i < n; i++ {
			go func() {
				app.detectAll(context.Background(), false)
				done <- struct{}{}
			}()
		}
		for i := 0; i < n; i++ {
			<-done
		}
		app.waitDetect(t)

		// 单飞把 12 个并发请求合成了 1 轮探测；每轮按引擎数探测一次，
		// 因此总调用数 = 2（两个引擎各一次），而不是 12 轮 × 2。
		if got := probes.Load(); got != int64(len(engine.AllKinds())) {
			t.Fatalf("单飞失效：探测了 %d 次，期望 %d（每轮两个引擎各一次）", got, len(engine.AllKinds()))
		}
	})

	t.Run("缓存命中不重探，强制重探绕过", func(t *testing.T) {
		app := newTestApp(t)
		var probes atomic.Int64
		restore := SetDetector(fakeDetector(&probes, ""))
		defer restore()

		// 先走一轮，把 detectedAt 填上；缓存新鲜时再打一次非强制请求，应直接命中。
		app.detectAll(context.Background(), false)
		app.waitDetect(t)
		app.detectAll(context.Background(), false)
		app.waitDetect(t)
		if got := probes.Load(); got != int64(len(engine.AllKinds())) {
			t.Fatalf("缓存命中又探了 %d 次，期望仍 %d", got, len(engine.AllKinds()))
		}
		// 强制重探：这条路径是同步的（用户就是来看结果的）。
		app.detectAll(context.Background(), true)
		if got := probes.Load(); got != 2*int64(len(engine.AllKinds())) {
			t.Fatalf("强制重探后共 %d 次，期望 %d", got, 2*len(engine.AllKinds()))
		}
	})
}

// 探测永远不挡路：首屏不等探测，读操作不等探测，探测期间还能读到旧结论。
// 这类错只表现为「打开应用卡几秒」：探测在后台跑、写回结果时不持锁，
// 编译与其余测试全是绿的。
func TestDetectDoesNotBlock(t *testing.T) {
	t.Run("首屏调用立即返回，探测在后台跑", func(t *testing.T) {
		app := newTestApp(t)
		release := make(chan struct{})
		restore := SetDetector(func(_ context.Context, _ string, _ engine.Kind) (engine.Identity, string) {
			<-release // 卡住探测，模拟「pi 正在起 Node」
			return engine.Identity{}, ""
		})
		defer restore()

		start := time.Now()
		app.detectAll(context.Background(), false)
		if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
			t.Fatalf("首屏探测耗时 %v：它必须立即返回", elapsed)
		}
		close(release)
		app.waitDetect(t)
	})

	t.Run("探测卡住时读操作照常返回", func(t *testing.T) {
		app := newTestApp(t)
		release := make(chan struct{})
		restore := SetDetector(func(_ context.Context, _ string, _ engine.Kind) (engine.Identity, string) {
			<-release
			return engine.Identity{Exe: "x", Caps: engine.CapsFor(engine.KindPi)}, ""
		})
		defer restore()

		app.detectAll(context.Background(), false)
		done := make(chan struct{})
		go func() {
			defer close(done)
			// 探测卡在 release 上时，这些读必须全部立即返回。
			_ = app.Identity(engine.KindOMP)
			_ = app.Workspace()
			_ = app.Profile()
			_ = app.ActiveKind()
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatal("探测期间读操作被阻塞：写回结果时不能持锁等子进程")
		}
		close(release)
		app.waitDetect(t)
	})

	t.Run("探测期间读到旧结论或空", func(t *testing.T) {
		app := newTestApp(t)
		app.mu.Lock()
		app.ids[engine.KindPi] = engine.Identity{Kind: engine.KindPi, Exe: "pi", Caps: engine.CapsFor(engine.KindPi)}
		app.detectedAt = app.detectedAt.Add(-2 * detectTTL) // 让缓存过期
		app.mu.Unlock()
		restore := SetDetector(fakeDetector(nil, "timeout"))
		defer restore()

		if got := app.Identity(engine.KindPi); got.Exe != "pi" {
			t.Fatalf("探测期间应读出旧结论，得到 %+v", got)
		}
		if got := app.Identity(engine.KindOMP); got.Exe != "" {
			t.Fatalf("没装过的引擎应读出空，得到 %+v", got)
		}
	})
}

// ---------------- 探测：状态语义 ----------------

// 探测状态的语义：界面据此决定「能不能说『没装』」。
// 这一层最容易出错的地方是「还没探到」被当成「没装」——
// 首发时探测要几秒（pi 的 --help 起一次 Node），而那几秒正好是用户第一眼看到的时刻。
func TestDetectStatusSemantics(t *testing.T) {
	t.Run("探完之后才有 detected_at", func(t *testing.T) {
		app := newTestApp(t)
		restore := SetDetector(fakeDetector(nil, ""))
		defer restore()

		if got := app.DetectedAt(); got != 0 {
			t.Fatalf("还没探测时 detected_at 应为 0，实际 %d", got)
		}
		app.detectAll(context.Background(), true)
		if got := app.DetectedAt(); got == 0 {
			t.Fatal("探测完成后 detected_at 不该是 0")
		}
	})

	t.Run("探测完成要广播事件", func(t *testing.T) {
		app := newTestApp(t)
		restore := SetDetector(fakeDetector(nil, ""))
		defer restore()

		sub, _, cancel := app.Hub.Subscribe(0)
		defer cancel()

		app.detectAll(context.Background(), true)

		deadline := time.After(2 * time.Second)
		for {
			select {
			case f := <-sub.Stream():
				if eventName(f.Raw()) == "engine:detected" {
					return
				}
			case <-deadline:
				t.Fatal("探测完成后没有收到 engine:detected 事件")
			}
		}
	})

	t.Run("没探过时状态是「还在探测」", func(t *testing.T) {
		app := newTestApp(t)
		restore := SetDetector(fakeDetector(nil, ""))
		defer restore()

		status, err := app.EngineStatus(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !status.Detecting {
			t.Fatal("还没探过时 Detecting 应为真")
		}
		// 等前面那次后台补验跑完：否则它仍占着 bgDetecting，
		// 后面同步强探完之后 Detecting 还会被它误报成真。
		app.waitDetect(t)

		app.detectAll(context.Background(), true)
		status, err = app.EngineStatus(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if status.Detecting {
			t.Fatal("探完之后 Detecting 应为假")
		}
	})

	t.Run("探测失败也算探过了", func(t *testing.T) {
		app := newTestApp(t)
		restore := SetDetector(fakeDetector(nil, "timeout"))
		defer restore()

		app.detectAll(context.Background(), true)
		if got := app.DetectedAt(); got == 0 {
			t.Fatal("探测失败也算探过了，detected_at 不该是 0")
		}
		status, err := app.EngineStatus(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if status.Detecting {
			t.Fatal("探测失败之后不该还在「检测中」——没装是一个事实")
		}
	})
}
