// 冒烟：真起一个引擎进程跑一轮对话，确认协议翻译与会话投影没坏。
//
// 为什么需要它：单元测试里引擎是桩，而「一轮对话能不能真的收尾」
// 只在真进程上成立——握手、帧顺序、结束帧、会话落盘，这四件事任一坏了
// 单元测试都是绿的。改了协议翻译或会话投影之后跑一次。
//
// 用法（本机装了 pi 或 omp）：
//
//	go run ./tools/smoke -engine pi -message "Reply with exactly: OK"
//
// 它只读设置里的引擎路径、只跑一轮、只打印帧序列；不改任何设置文件，
// 也不走 HTTP。会话文件是引擎自己写的，所以这条路径同时验证了
// 「界面读到的历史」与「引擎写下的账本」是同一份。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
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

	mu.Lock()
	defer mu.Unlock()
	fmt.Printf("共 %d 帧，事件顺序：\n", len(order))
	for _, name := range order {
		fmt.Println("  ", name)
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
