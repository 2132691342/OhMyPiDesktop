package service

// 编排层只保留「不依赖本机装了什么」的高价值用例：
// 探测的单飞（首屏多请求只探一轮）、强制重探绕过缓存、探测期间旧结论可读、
// 以及附件装配的两条边界（图片上限 / 空输入拒绝）。其余纯函数不测。

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"testing"

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

// 首屏会同时打 Bootstrap / Health / EngineStatus，三个请求都发现缓存过期
// 就各自起一轮探测——机器上多出多个子进程，而查的是同一件事。单飞锁把它们合成一次。
func TestDetectSingleFlight(t *testing.T) {
	app := newTestApp(t)
	var probes int
	restore := SetDetector(func(_ string, _ engine.Kind) (engine.Identity, error) {
		probes++
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
	// 单飞把 12 个并发请求合成了 1 轮探测；每轮按引擎数探测一次，
	// 因此总调用数 = 2（两个引擎各一次），而不是 12 轮 × 2。
	if probes != len(engine.AllKinds()) {
		t.Fatalf("单飞失效：探测了 %d 次，期望 %d（每轮两个引擎各一次）", probes, len(engine.AllKinds()))
	}
}

// 显式「重新检测」必须绕过缓存，无论 detectFresh 说什么。
func TestDetectForceBypassesCache(t *testing.T) {
	app := newTestApp(t)
	var probes int
	restore := SetDetector(func(_ string, _ engine.Kind) (engine.Identity, error) {
		probes++
		return engine.Identity{Kind: engine.KindPi, Exe: "pi", Caps: engine.CapsFor(engine.KindPi)}, nil
	})
	defer restore()

	// 先走一轮，把 detectedAt 填上。
	app.detectAll(context.Background(), false)
	if probes != len(engine.AllKinds()) {
		t.Fatalf("首轮探测 %d 次，期望 %d", probes, len(engine.AllKinds()))
	}
	// 缓存新鲜时再打一次非强制请求，应直接命中、不再探测。
	app.detectAll(context.Background(), false)
	if probes != len(engine.AllKinds()) {
		t.Fatalf("缓存命中又探了 %d 次，期望仍 %d", probes, len(engine.AllKinds()))
	}
	// 强制重探必须再探一轮。
	app.detectAll(context.Background(), true)
	if probes != 2*len(engine.AllKinds()) {
		t.Fatalf("强制重探只做了 %d 次，期望 %d", probes, 2*len(engine.AllKinds()))
	}
}

// 探测进行中，界面应当能立刻拿到上一次的结论（或空），而不是卡在探测上。
func TestIdentityReadableDuringDetect(t *testing.T) {
	app := newTestApp(t)
	// 先落一个旧结论，再让探针慢吞吞地跑。
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

// 既有文字又有文件时，文件清单必须附在正文的换行之后，而不是吞掉正文。
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

// 只有正文没有附件必须能发；而全空必须被拦下。
func TestBuildPromptEmptyRejected(t *testing.T) {
	if _, err := buildPrompt("   ", nil); err == nil {
		t.Fatal("正文与附件全空应被拒绝")
	}
	if _, err := buildPrompt("说点什么", nil); err != nil {
		t.Fatalf("有正文无附件不应报错：%v", err)
	}
}

// 非图片字节且不带 mime 时，sniff 认不出类型，必须被 reject。
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
