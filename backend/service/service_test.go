package service

// 编排层只测不依赖本机环境的高价值用例：探测单飞、强制重探、探测不阻塞读、附件装配边界。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"OhMyPiDesktop/backend/domain"
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

// 首屏并发打多个探测请求时，单飞锁把它们合成每引擎一次。
func TestDetectSingleFlight(t *testing.T) {
	app := newTestApp(t)
	var probes atomic.Int64
	restore := SetDetector(func(_ string, _ engine.Kind) (engine.Identity, error) {
		probes.Add(1)
		return engine.Identity{Kind: engine.KindPi, Exe: "pi", Caps: engine.CapsFor(engine.KindPi)}, nil
	})
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
	restore := SetDetector(func(_ string, _ engine.Kind) (engine.Identity, error) {
		probes.Add(1)
		return engine.Identity{Kind: engine.KindPi, Exe: "pi", Caps: engine.CapsFor(engine.KindPi)}, nil
	})
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

// 首屏必须立即返回：探测在后台跑，不能同步等它。
func TestDetectNeverBlocksFirstScreen(t *testing.T) {
	app := newTestApp(t)
	release := make(chan struct{})
	restore := SetDetector(func(_ string, _ engine.Kind) (engine.Identity, error) {
		<-release // 卡住探测，模拟“pi 正在起 Node”
		return engine.Identity{}, nil
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
func TestIdentityNotBlockedByDetect(t *testing.T) {
	app := newTestApp(t)
	release := make(chan struct{})
	restore := SetDetector(func(_ string, _ engine.Kind) (engine.Identity, error) {
		<-release
		return engine.Identity{Exe: "x", Caps: engine.CapsFor(engine.KindPi)}, nil
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
	restore := SetDetector(func(_ string, _ engine.Kind) (engine.Identity, error) {
		return engine.Identity{}, context.DeadlineExceeded
	})
	defer restore()

	if got := app.Identity(engine.KindPi); got.Exe != "pi" {
		t.Fatalf("探测期间应读出旧结论，得到 %+v", got)
	}
	if got := app.Identity(engine.KindOMP); got.Exe != "" {
		t.Fatalf("没装过的引擎应读出空，得到 %+v", got)
	}
}

// 一次发送的图片有上限：超过则整条拒绝，而不是悄悄丢几张。
func TestBuildPromptImageLimit(t *testing.T) {
	img := tinyPNG()
	atts := make([]domain.ChatAttachmentREQ, 0, 10)
	for i := 0; i < 10; i++ {
		atts = append(atts, domain.ChatAttachmentREQ{
			Kind:        domain.AttachmentKindImage,
			ImageBase64: base64.StdEncoding.EncodeToString(img),
			ImageMIME:   "image/png",
		})
	}
	if _, err := buildPrompt("看图", atts); err == nil {
		t.Fatal("超过 8 张图片应被拒绝")
	}
}

// 文件清单必须附在正文换行之后，而不是吞掉正文。
func TestBuildPromptAppendsFiles(t *testing.T) {
	atts := []domain.ChatAttachmentREQ{
		{Kind: domain.AttachmentKindFile, FilePath: "/repo/a.go"},
		{Kind: domain.AttachmentKindFile, FilePath: "/repo/b.go"},
	}
	payload, err := buildPrompt("改这两个文件", atts)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Message != "改这两个文件\n\n这次要看这些文件：\n- /repo/a.go\n- /repo/b.go" {
		t.Fatalf("正文 + 文件清单拼接错误：%q", payload.Message)
	}
}

// 有正文无附件可发；正文与附件全空必须被拦下。
func TestBuildPromptEmptyRejected(t *testing.T) {
	if _, err := buildPrompt("   ", nil); err == nil {
		t.Fatal("正文与附件全空应被拒绝")
	}
	if _, err := buildPrompt("说点什么", nil); err != nil {
		t.Fatalf("有正文无附件不应报错：%v", err)
	}
}

// 非图片字节且不带 mime 时认不出类型，必须被 reject。
func TestDecodeImageRejectsInvalid(t *testing.T) {
	if _, err := decodeImage(domain.ChatAttachmentREQ{
		Kind:        domain.AttachmentKindImage,
		ImageBase64: base64.StdEncoding.EncodeToString([]byte("not an image")),
	}); err == nil {
		t.Fatal("非图片内容且未声明 mime 时应被拒绝")
	}
}

// tinyPNG 返回一段合法的 1x1 PNG 字节，供图片附件测试当素材。
func tinyPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // 签名
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
		0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
		0x42, 0x60, 0x82,
	}
}

// ---------------- 探测状态语义 ----------------

// 探测状态的语义：界面据此决定「能不能说『没装』」。
// 这一层最容易出错的地方是「还没探到」被当成「没装」——
// 首发时探测要几秒（pi 的 --help 起一次 Node），而那几秒正好是用户第一眼看到的时刻。

// 探完之后才有 detected_at；探之前它是 0。
// 判据用「完成时刻」而不是「响应时刻」：后者会让界面永远显示「刚刚检测过」。
func TestDetectedAtOnlyAfterProbe(t *testing.T) {
	app := newTestApp(t)
	restore := SetDetector(func(_ string, k engine.Kind) (engine.Identity, error) {
		return engine.Identity{Kind: k, Exe: "x", Caps: engine.CapsFor(k)}, nil
	})
	defer restore()

	if got := app.DetectedAt(); got != 0 {
		t.Fatalf("还没探测时 detected_at 应为 0，实际 %d", got)
	}
	app.detectAll(context.Background(), true)
	if got := app.DetectedAt(); got == 0 {
		t.Fatal("探测完成后 detected_at 不该是 0")
	}
}

// 探测完成要广播事件：界面靠它把「检测中」换成真结果。
func TestDetectEmitsEvent(t *testing.T) {
	app := newTestApp(t)
	restore := SetDetector(func(_ string, k engine.Kind) (engine.Identity, error) {
		return engine.Identity{Kind: k, Exe: "x", Caps: engine.CapsFor(k)}, nil
	})
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
}

// 从没探过时，引擎状态要报「还在探测」而不是「没装」。
func TestEngineStatusDetectingBeforeProbe(t *testing.T) {
	app := newTestApp(t)
	restore := SetDetector(func(_ string, k engine.Kind) (engine.Identity, error) {
		return engine.Identity{Kind: k, Exe: "x", Caps: engine.CapsFor(k)}, nil
	})
	defer restore()

	status, err := app.EngineStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Detecting {
		t.Fatal("还没探过时 Detecting 应为真")
	}

	app.detectAll(context.Background(), true)
	status, err = app.EngineStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Detecting {
		t.Fatal("探完之后 Detecting 应为假")
	}
}

// 探测失败（引擎没装）也要算「探过了」：否则每次请求都会重探。
func TestDetectFailureCountsAsProbed(t *testing.T) {
	app := newTestApp(t)
	restore := SetDetector(func(_ string, k engine.Kind) (engine.Identity, error) {
		return engine.Identity{}, context.Canceled
	})
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
}

// eventName 从一帧原始字节里取出事件名。帧的形状是 {seq, event, data, sent_at}，这里只关心 event。
func eventName(raw []byte) string {
	var f struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return ""
	}
	return f.Event
}
