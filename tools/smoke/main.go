// 冒烟：真起一个引擎进程跑一轮对话，确认协议翻译与会话投影没坏。单元测试里引擎是桩，而握手、帧顺序、结束帧、会话落盘任一坏了单元测试都是绿的——改了协议翻译或会话投影之后跑一次。
// 它只读设置里的引擎路径、只跑一轮、只打印结果；会话文件是引擎自己写的，这条路径同时验证「界面读到的历史」与「引擎写下的账本」是同一份。
// 用法（本机装了 pi 或 omp）：go run ./tools/smoke -engine pi -message "Reply with exactly: OK"
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/engine/omp"
	"OhMyPiDesktop/backend/engine/pi"
	"OhMyPiDesktop/backend/env"
)

func main() {
	kindName := flag.String("engine", "pi", "引擎：omp / pi")
	message := flag.String("message", "Reply with exactly: OK", "要发的一句话")
	dir := flag.String("dir", "", "工作目录（默认当前目录）")
	timeout := flag.Duration("timeout", 180*time.Second, "整轮超时")
	flag.Parse()

	kind := engine.Kind(*kindName)
	if !kind.Valid() {
		fmt.Println("未知引擎：", *kindName)
		os.Exit(1)
	}
	workDir := *dir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	// order 记这一轮的事件顺序：它直接暴露「一轮怎么算完」用对了判据没有。
	var mu sync.Mutex
	var order []string
	done := make(chan struct{})
	var once sync.Once
	emit := func(_ string, event string, data any) {
		mu.Lock()
		order = append(order, event)
		mu.Unlock()
		switch event {
		case "chat:start", "chat:done", "chat:error", "chat:settled", "engine:ready", "engine:exit":
			fmt.Printf("  帧 %-14s %s\n", event, brief(data))
		}
		if event == "chat:done" {
			once.Do(func() { close(done) })
		}
	}

	sup := engine.NewSupervisor(emit,
		map[engine.Kind]engine.Dialect{
			engine.KindOMP: omp.Dialect{},
			engine.KindPi:  pi.Dialect{},
		},
		func(k engine.Kind) (string, []string, []string) {
			id, err := env.Detect(context.Background(), "", k)
			if err != nil || id.Exe == "" {
				return "", nil, nil
			}
			return id.Exe, nil, nil
		},
		func(engine.Kind) engine.SubagentStore { return nil },
	)
	defer sup.Shutdown()
	defer sup.StopAll()

	ctx := context.Background()
	e := sup.Engine(kind, workDir)
	if _, err := e.Client(ctx); err != nil {
		fmt.Println("拉起引擎失败：", err)
		os.Exit(1)
	}
	fmt.Printf("引擎 %s 已就绪，目录=%s\n", kind, e.WorkDir)

	runID, err := e.Prompt(ctx, engine.PromptPayload{Message: *message})
	if err != nil {
		fmt.Println("发送失败：", err)
		os.Exit(1)
	}
	fmt.Printf("run_id = %s\n发送：%s\n", runID, *message)

	select {
	case <-done:
		fmt.Println("一轮收尾：chat:done")
	case <-time.After(*timeout):
		fmt.Println("超时：这一轮没有收尾")
		os.Exit(1)
	}

	report(e, ctx, order)
}

// report 打印这一轮的事实：事件顺序、会话文件、用量与最后一轮的块。
// 这几项是「协议翻译与会话投影没坏」的直接证据：顺序对说明一轮的收尾判据用对了；
// 会话文件对说明引擎写下的账本能对回去；用量与块对说明翻译层没有把内容吞掉。
func report(e *engine.Engine, ctx context.Context, order []string) {
	fmt.Println("\n--- 事件顺序 ---")
	fmt.Printf("共 %d 帧\n", len(order))
	for _, name := range order {
		fmt.Println("  ", name)
	}

	if file := e.CurrentSessionFile(); file != "" {
		fmt.Printf("\n会话文件：%s\n", file)
	}

	if stats, err := e.Stats(ctx); err == nil {
		fmt.Printf("\n用量：%d 条消息，token %d（入 %d / 出 %d / 缓存读 %d / 缓存写 %d），花费 %.4f\n",
			stats.TotalMessages, stats.Tokens.Total,
			stats.Tokens.Input, stats.Tokens.Output,
			stats.Tokens.CacheRead, stats.Tokens.CacheWrite, stats.Cost)
	} else {
		fmt.Printf("\n用量：读不到（%v）\n", err)
	}

	fmt.Println("\n--- 最后一轮的块 ---")
	turn := e.Translator().Current()
	if turn == nil {
		fmt.Println("（没有当前轮）")
		return
	}
	fmt.Printf("模型：%s\n块数：%d\n", turn.Model, len(turn.Blocks))
	for i, b := range turn.Blocks {
		text := strings.TrimSpace(b.Text)
		if len(text) > 120 {
			text = text[:120] + "…"
		}
		switch b.Kind {
		case engine.BlockTool:
			fmt.Printf("  [%d] 工具 %s 状态=%s 耗时=%dms %s\n",
				i, b.ToolName, b.Status, b.DurationMS, text)
		case engine.BlockThinking:
			fmt.Printf("  [%d] 思考 %s\n", i, text)
		default:
			fmt.Printf("  [%d] 正文 %s\n", i, text)
		}
	}
}

// brief 只留载荷里最有用的几个键，把日志压在单行。
func brief(data any) string {
	m, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	out := ""
	for _, key := range []string{"run_id", "status", "message", "reason"} {
		if v, ok := m[key]; ok && v != nil {
			out += fmt.Sprintf("%s=%v ", key, v)
		}
	}
	return out
}
