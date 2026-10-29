package service

// 探测状态的语义：界面据此决定「能不能说『没装』」。
//
// 这一层最容易出错的地方是「还没探到」被当成「没装」——
// 首发时探测要几秒（pi 的 --help 起一次 Node），
// 而那几秒正好是用户第一眼看到的时刻。

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"OhMyPiDesktop/backend/engine"
)

// 探完之后才有 detected_at；探之前它是 0。
//
// 判据用「完成时刻」而不是「响应时刻」：后者会让界面永远显示
// 「刚刚检测过」，用户就没法判断屏幕上那个版本号可信不可信。
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
//
// 之前只有用户手动点「重新检测」才会更新，于是首发时概览页一直显示
// 「未安装」——而那只是因为探测还在跑，不是一个事实。
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

// eventName 从一帧原始字节里取出事件名。
//
// 帧的形状是 {seq, event, data, sent_at}，这里只关心 event。
func eventName(raw []byte) string {
	var f struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return ""
	}
	return f.Event
}
