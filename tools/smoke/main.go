// 冒烟：真起一个引擎进程，跑一轮对话，打印事件目录与投影结果。
//
// 用法：
//
//	go run ./tools/smoke -engine omp -msg "Reply with exactly: OK"
//	go run ./tools/smoke -engine pi  -dir C:\path\to\repo -msg "hi"
//
// 它不写测试断言：这里要回答的是「真引擎跑通了吗、事件顺序对不对」，
// 那件事只能靠人看一眼输出。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/hub"
	"OhMyPiDesktop/backend/pkg"
	"OhMyPiDesktop/backend/repo"
	"OhMyPiDesktop/backend/service"
)

func main() {
	engineName := flag.String("engine", "omp", "引擎种类：omp / pi")
	dir := flag.String("dir", os.TempDir(), "工作目录")
	msg := flag.String("msg", "Reply with exactly: OK", "发出去的那句话")
	exe := flag.String("exe", "", "引擎可执行文件路径（默认从 PATH 探测）")
	flag.Parse()

	kind := engine.Kind(*engineName)
	if !kind.Valid() {
		fmt.Println("未知引擎:", *engineName)
		os.Exit(2)
	}
	dirPath, msgText, exePath := *dir, *msg, *exe

	dbPath := dirPath + string(os.PathSeparator) + "smoke-" + string(kind) + ".json"
	_ = os.Remove(dbPath)
	store, err := repo.Open(dbPath)
	must(err)
	defer store.Close()
	h := hub.New(512)
	svc, err := service.New(service.Options{Store: store, Hub: h, DataDir: dirPath, Version: "smoke"})
	must(err)

	if exePath != "" {
		must(store.Set(domain.KeyEngineExePrefix+string(kind), exePath))
	}
	must(svc.SetWorkspace(dirPath))
	must(svc.SetActive(kind))

	events := map[string]int{}
	var order []string
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	sub, _, cancelSub := h.Subscribe(0)
	defer cancelSub()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var frame []byte
			select {
			case f := <-sub.Stream():
				frame = f.Raw()
			case <-sub.Done():
				return
			}
			var ev struct {
				Event string `json:"event"`
			}
			if json.Unmarshal(frame, &ev) != nil {
				continue
			}
			events[ev.Event]++
			if ev.Event == "chat:blocks" {
				continue
			}
			order = append(order, ev.Event)
		}
	}()

	runID, err := svc.Send(ctx, kind, "", domain.ChatSendREQ{Message: msgText})
	fmt.Println("send ->", runID, "err:", errText(err))
	if err != nil {
		os.Exit(1)
	}

	deadline := time.Now().Add(150 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(400 * time.Millisecond)
		st, err := svc.ChatState(ctx, kind, "")
		if err != nil {
			fmt.Println("state err:", errText(err))
			continue
		}
		if !st.Streaming && len(st.Messages) > 0 {
			break
		}
	}

	st, err := svc.ChatState(ctx, kind, "")
	must(err)
	fmt.Printf("\n事件计数: %v\n", events)
	fmt.Printf("事件顺序: %s\n", strings.Join(dedupe(order), " → "))
	fmt.Printf("会话: %s  文件: %s\n", st.Session, st.SessionFile)
	if st.Stats != nil {
		fmt.Printf("用量: in=%d out=%d cacheRead=%d 花费=%.6f 上下文=%v\n",
			st.Stats.Tokens.Input, st.Stats.Tokens.Output, st.Stats.Tokens.CacheRead,
			st.Stats.Cost, percent(st.Context))
	}
	fmt.Printf("消息条数: %d  历史轮次: %d\n", len(st.Messages), len(st.History))
	if len(st.History) > 0 {
		last := st.History[len(st.History)-1]
		fmt.Printf("最后一轮: role=%s blocks=%d\n", last.Role, len(last.Blocks))
		for _, b := range last.Blocks {
			preview := b.Text
			if b.Kind == engine.BlockTool {
				preview = b.ToolName + " " + b.Status
			}
			fmt.Printf("  - %-8s %s\n", b.Kind, truncate(preview, 120))
		}
	}
	svc.Engine.StopAll()
	cancelSub()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

func percent(c *engine.ContextUsage) string {
	if c == nil || c.Percent == nil {
		return "?"
	}
	return fmt.Sprintf("%.0f%%", *c.Percent)
}

func dedupe(in []string) []string {
	var out []string
	for _, s := range in {
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}

func must(err error) {
	if err != nil {
		fmt.Println("fatal:", err)
		os.Exit(1)
	}
}

func errText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return pkg.As(err).Message + " " + pkg.As(err).Detail
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
