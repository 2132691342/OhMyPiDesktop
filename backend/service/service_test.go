package service

// 编排层只测会悄无声息出错的地方：探测的单飞与不阻塞、探测状态的语义
// （「还没探到」不能被当成「没装」）。
//
// 探测引擎与查最新版各有一个接缝（SetDetector / engine.SetLatestFetcher），
// 测试里换掉它们。不换的话，测试就变成「测这台机器装了什么」。

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

// 首屏并发打多个探测请求时，单飞锁把它们合成每引擎一次。
//
// 不合并的话，Bootstrap / Health / EngineStatus 三个请求会各起一轮探测，
// 机器上多出六个子进程，而它们查的是同一件事。
func TestDetectSingleFlight(t *testing.T) {
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
}

// 强制重探绕过缓存；缓存新鲜时的非强制请求直接命中、不再探测。
func TestDetectForceBypassesCache(t *testing.T) {
	app := newTestApp(t)
	var probes atomic.Int64
	restore := SetDetector(fakeDetector(&probes, ""))
	defer restore()

	// 先走一轮，把 detectedAt 填上。
	app.detectAll(context.Background(), false)
	app.waitDetect(t)
	if got := probes.Load(); got != int64(len(engine.AllKinds())) {
		t.Fatalf("首轮探测 %d 次，期望 %d", got, len(engine.AllKinds()))
	}
	// 缓存新鲜时再打一次非强制请求，应直接命中、不再探测。
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
}

// 首屏路径永不阻塞在探测上。
//
// 这条很容易被写回去，而它只表现为「打开应用卡几秒」：
// 探测在后台跑，编译与其余测试全是绿的。
func TestDetectNeverBlocksFirstScreen(t *testing.T) {
	app := newTestApp(t)
	release := make(chan struct{})
	restore := SetDetector(func(_ context.Context, _ string, _ engine.Kind) (engine.Identity, string) {
		<-release // 卡住探测，模拟「pi 正在起 Node」
		return engine.Identity{}, ""
	})
	defer restore()

	start := time.Now()
	app.detectAll(context.Background(), false)
	elapsed := time.Since(start)

	// 探针被卡着也拿到结论：说明它根本没同步等探测。
	if elapsed > 200*time.Millisecond {
		t.Fatalf("首屏探测耗时 %v：它必须立即返回", elapsed)
	}
	close(release)
	app.waitDetect(t)
}

// 探测进行中读 Identity 必须立即返回：写回结果时不能持锁等子进程。
//
// 持锁等 channel 会让所有读 Identity / Workspace / Index 的请求堵在 RLock 上，
// 界面上表现就是「打开应用卡四秒」。
func TestIdentityNotBlockedByDetect(t *testing.T) {
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
}

// 探测进行中，界面应立刻拿到旧结论（或空），而不是卡在探测上。
func TestIdentityReadableDuringDetect(t *testing.T) {
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

	t.Run("没探到时原因要能说清下一步", func(t *testing.T) {
		app := newTestApp(t)

		// 显式路径配了一个不存在的文件 —— 该去改路径，不是去安装。
		app.mu.Lock()
		app.probeReason = map[engine.Kind]string{engine.KindPi: "bad-path"}
		app.mu.Unlock()
		if got := app.ProbeReason(engine.KindPi); got != "bad-path" {
			t.Fatalf("没探到时应报出原因，实际 %q", got)
		}

		// 探到了就没什么要解释的：结论可信。
		app.mu.Lock()
		app.ids[engine.KindPi] = engine.Identity{Kind: engine.KindPi, Exe: "pi.exe", Caps: engine.CapsFor(engine.KindPi)}
		app.mu.Unlock()
		if got := app.ProbeReason(engine.KindPi); got != "" {
			t.Fatalf("探到了就不该给原因，实际 %q", got)
		}
	})
}
