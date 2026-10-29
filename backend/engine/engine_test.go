package engine

// 这一层只测会「悄无声息出错」的地方：引擎判种与能力漂移、协议翻译的
// 顺序/增量/回填、分片重组的残缺丢弃、以及引擎实例绝不会无限增长。
// 这些点错了不会 panic，只会让界面显示错数据或机器越来越卡。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeDialect 只用于回答能力表；不启动进程。
type fakeDialect struct{ caps Caps }

func (f fakeDialect) Kind() Kind                             { return KindPi }
func (f fakeDialect) Caps() Caps                             { return f.caps }
func (f fakeDialect) Args(string, []string) []string         { return nil }
func (f fakeDialect) Handshake(context.Context, *Proc) error { return nil }
func (f fakeDialect) Consume(*Engine, Frame) bool            { return false }

// ---------------- 判种 / 渠道 / 能力 ----------------

func TestMatchKind(t *testing.T) {
	const helpOMP = "omp v18.13.1\n\nUSAGE\n  $ omp [COMMAND]\n\nCOMMANDS\n  acp\n  auth\n"
	const helpPi = "pi - AI coding assistant with read, bash, edit, write tools\n\nUsage:\n  pi [options] [--] [@files...]\n"
	cases := []struct {
		name, kind, version, help string
		want                      bool
	}{
		{"omp 标准版本号", string(KindOMP), "omp/18.13.1", helpOMP, true},
		{"改了 bin 名但帮助是 pi", string(KindOMP), "custom/2.0.0", helpPi, false},
		{"pi 的版本号不是 omp 形状", string(KindOMP), "pi/1.4.0", helpPi, false},
		{"pi 裸版本号", string(KindPi), "1.4.0", helpPi, true},
		{"pi 带预发布后缀", string(KindPi), "1.4.0-beta.2", helpPi, true},
		{"omp 裸版本号不算 pi", string(KindPi), "1.4.0", helpOMP, false},
		{"pi 不接受带前缀版本", string(KindPi), "omp/18.13.1", helpOMP, false},
		{"帮助跑不出来不认", string(KindPi), "1.4.0", "", false},
	}
	for _, c := range cases {
		if got := matchKindWithHelp(Kind(c.kind), c.version, c.help); got != c.want {
			t.Errorf("%s: matchKindWithHelp = %v，期望 %v", c.name, got, c.want)
		}
	}
}

func TestDetectChannel(t *testing.T) {
	home := `C:\Users\me`
	local := `C:\Users\me\AppData\Local`
	cases := []struct {
		name, kind, exe string
		want            Channel
	}{
		{"npm 全局", string(KindPi), `C:\nvm\node_global\node_modules\@earendil-works\pi-coding-agent\dist\cli.js`, ChannelNPM},
		{"pnpm 全局", string(KindPi), `C:\Users\me\AppData\Local\pnpm\node_modules\.pnpm\@earendil-works+pi\dist\cli.js`, ChannelPNPM},
		{"bun 全局", string(KindPi), `C:\Users\me\.bun\install\global\node_modules\@earendil-works\pi-coding-agent\dist\cli.js`, ChannelBun},
		{"omp 官方安装器", string(KindOMP), `C:\Users\me\AppData\Local\omp\omp.exe`, ChannelOMPInstaller},
		{"别处的 omp 不是安装器", string(KindOMP), `D:\tools\omp\omp.exe`, ChannelUnknown},
		{"空路径", string(KindOMP), "", ChannelUnknown},
	}
	for _, c := range cases {
		if got := DetectChannel(Kind(c.kind), c.exe, home, local); got != c.want {
			t.Errorf("%s: DetectChannel = %q，期望 %q", c.name, got, c.want)
		}
	}
}

// 全局 shim 是 <prefix>\pi.cmd + 同级 node_modules，只看路径段会全判成未知。
func TestDetectChannelSiblingModules(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "node_modules", "@earendil-works", "pi-coding-agent"), 0o755)
	exe := filepath.Join(root, "pi.cmd")
	if got := DetectChannel(KindPi, exe, "", ""); got != ChannelNPM {
		t.Fatalf("同级 node_modules 的 shim 应判为 npm，得到 %q", got)
	}
	_ = os.MkdirAll(filepath.Join(root, "node_modules", ".pnpm"), 0o755)
	if got := DetectChannel(KindPi, exe, "", ""); got != ChannelPNPM {
		t.Fatalf("带 .pnpm 存储的应判为 pnpm，得到 %q", got)
	}
}

// 能力表是「按钮是否出现」的唯一依据。与真实命令表对不上时，界面会给出
// 点了没反应的按钮，所以钉死「哪条命令属于谁」。
func TestCapsMatchCommandTable(t *testing.T) {
	cases := []struct {
		kind, cmd string
		want      bool
	}{
		{string(KindOMP), "remove_queued_message", true},
		{string(KindOMP), "clear_queue", false},
		{string(KindPi), "remove_queued_message", false},
		{string(KindPi), "clear_queue", true},
	}
	for _, c := range cases {
		caps := CapsFor(Kind(c.kind))
		got := caps.QueueEdit
		if c.cmd == "clear_queue" {
			got = caps.QueueClear
		}
		if got != c.want {
			t.Errorf("%s %s 存在性 = %v，期望 %v", c.kind, c.cmd, got, c.want)
		}
	}
}

func TestUninstallCommandMatchesChannel(t *testing.T) {
	channels := []Channel{ChannelNPM, ChannelPNPM, ChannelBun, ChannelOMPInstaller, ChannelPiInstaller, ChannelUnknown}
	for _, kind := range AllKinds() {
		for _, ch := range channels {
			hasCmd := len(UninstallCommand(kind, ch)) > 0
			if hasCmd != CanUninstall(ch) {
				t.Fatalf("%s/%s: CanUninstall=%v 但命令存在=%v", kind, ch, CanUninstall(ch), hasCmd)
			}
		}
	}
}

// ---------------- 协议翻译 ----------------

func blockOrder(t *Turn) []string {
	out := make([]string, 0, len(t.Blocks))
	for _, b := range t.Blocks {
		out = append(out, b.Kind)
	}
	return out
}

func feed(tr *Translator, raws ...string) {
	for _, raw := range raws {
		tr.Apply("message_update", json.RawMessage(raw))
	}
}

// 思考 / 正文 / 工具在一条助手消息里交错时，顺序必须和引擎吐的一致。
func TestTranslateKeepsOriginalOrder(t *testing.T) {
	tr := NewTranslator()
	tr.BeginTurn("run-1", "hi", "")
	feed(tr,
		`{"assistantMessageEvent":{"type":"thinking_start","contentIndex":0}}`,
		`{"assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"想一下"}}`,
		`{"assistantMessageEvent":{"type":"thinking_end","contentIndex":0,"content":"想一下"}}`,
		`{"assistantMessageEvent":{"type":"text_start","contentIndex":1}}`,
		`{"assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"先说"}}`,
		`{"assistantMessageEvent":{"type":"toolcall_start","contentIndex":2}}`,
		`{"assistantMessageEvent":{"type":"toolcall_end","contentIndex":2,"toolCall":{"id":"t1","name":"bash","arguments":{"cmd":"ls"}}}}`,
		`{"assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"再说"}}`,
	)
	got := blockOrder(tr.Current())
	want := []string{BlockThinking, BlockText, BlockTool}
	if len(got) != len(want) {
		t.Fatalf("块数 = %d (%v)，期望 %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 块 = %s，期望 %s（顺序 %v）", i, got[i], want[i], got)
		}
	}
	if text := tr.Current().Blocks[1].Text; text != "先说再说" {
		t.Fatalf("正文 = %q，期望两段 delta 累加", text)
	}
}

func TestTranslateDeltaIsIncremental(t *testing.T) {
	tr := NewTranslator()
	tr.BeginTurn("run-1", "hi", "")
	tr.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_start","contentIndex":0}}`))
	up := tr.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"abc"}}`))
	if up == nil || up.Mode != "delta" || up.Delta != "abc" || up.Index != 0 {
		t.Fatalf("增量更新 = %+v，期望 mode=delta index=0 delta=abc", up)
	}
	if up.Blocks != nil {
		t.Fatalf("delta 模式下不应带整表，实际带了 %d 块", len(up.Blocks))
	}
}

// 增量与全量必须等价：两种路径累加出来的块必须一模一样，否则界面会出现
// 重复或残缺的正文。
func TestTranslateDeltaEqualsFullSnapshot(t *testing.T) {
	inc := NewTranslator()
	inc.BeginTurn("run-1", "hi", "")
	feed(inc,
		`{"assistantMessageEvent":{"type":"text_start","contentIndex":0}}`,
		`{"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"你好"}}`,
		`{"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"，世界"}}`,
	)
	full := NewTranslator()
	full.BeginTurn("run-1", "hi", "")
	full.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_start","contentIndex":0}}`))
	full.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_end","contentIndex":0,"content":"你好，世界"}}`))
	gotInc, gotFull := inc.Current(), full.Current()
	if len(gotInc.Blocks) != len(gotFull.Blocks) {
		t.Fatalf("块数不同：增量 %d，全量 %d", len(gotInc.Blocks), len(gotFull.Blocks))
	}
	if gotInc.Blocks[0].Text != gotFull.Blocks[0].Text {
		t.Errorf("正文不同：增量 %q，全量 %q", gotInc.Blocks[0].Text, gotFull.Blocks[0].Text)
	}
}

// 一条新助手消息要重置 contentIndex：否则新消息的第一段会被追加到上一段里。
func TestTranslateNewAssistantMessageResetsIndices(t *testing.T) {
	tr := NewTranslator()
	tr.BeginTurn("run-1", "hi", "")
	feed(tr,
		`{"assistantMessageEvent":{"type":"text_start","contentIndex":0}}`,
		`{"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"第一段"}}`,
	)
	tr.Apply("message_start", json.RawMessage(`{"message":{"role":"assistant","model":"p/m"}}`))
	feed(tr,
		`{"assistantMessageEvent":{"type":"text_start","contentIndex":0}}`,
		`{"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"第二段"}}`,
	)
	turn := tr.Current()
	if len(turn.Blocks) != 2 {
		t.Fatalf("块数 = %d，期望 2", len(turn.Blocks))
	}
	if turn.Blocks[0].Text != "第一段" || turn.Blocks[1].Text != "第二段" {
		t.Fatalf("两块 = %q / %q", turn.Blocks[0].Text, turn.Blocks[1].Text)
	}
}

// 交出去的 blocks 必须是副本：交实时指针等于把正在被追加的 slice 递给序列化。
func TestTranslateHandsOutCopies(t *testing.T) {
	tr := NewTranslator()
	tr.BeginTurn("run-1", "hi", "")
	tr.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_start","contentIndex":0}}`))
	snap := tr.Current()
	before := snap.Blocks[0].Text
	tr.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"后来加的"}}`))
	if snap.Blocks[0].Text != before {
		t.Fatalf("快照被后续帧改动了：%q → %q", before, snap.Blocks[0].Text)
	}
}

// ---------------- 分片重组 ----------------

func chunkFrame(t *testing.T, chunkID string, index, count int, payload string, totalLen int) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"type": "rpc_chunk", "chunkId": chunkID, "index": index, "count": count,
		"byteLength": totalLen, "data": base64.StdEncoding.EncodeToString([]byte(payload)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func newProc() *Proc { return &Proc{opts: Options{Dialect: fakeDialect{}}} }

func TestAssembleJoinsChunks(t *testing.T) {
	p := newProc()
	parts := []string{`{"type":"response",`, `"id":"x","data":{"a":1}}`}
	total := len(parts[0]) + len(parts[1])
	var out []byte
	for i, part := range parts {
		got, ok := p.assemble(chunkFrame(t, "rpc-1", i, len(parts), part, total))
		if i < len(parts)-1 {
			if ok {
				t.Fatalf("第 %d 片不应宣告完成", i)
			}
			continue
		}
		if !ok {
			t.Fatalf("最后一片应组装完成")
		}
		out = got
	}
	if want := parts[0] + parts[1]; string(out) != want {
		t.Errorf("拼出 %q，期望 %q", out, want)
	}
}

// 每一条校验分支都必须整段丢弃，不能放半截帧上去。
func TestAssembleRejectsBrokenSequences(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T) [][]byte
	}{
		{"index 乱序", func(t *testing.T) [][]byte {
			return [][]byte{chunkFrame(t, "rpc-1", 1, 2, "b", 2), chunkFrame(t, "rpc-1", 0, 2, "a", 2)}
		}},
		{"index 重复", func(t *testing.T) [][]byte {
			return [][]byte{chunkFrame(t, "rpc-1", 0, 2, "a", 2), chunkFrame(t, "rpc-1", 0, 2, "b", 2)}
		}},
		{"chunkId 中途变了", func(t *testing.T) [][]byte {
			return [][]byte{chunkFrame(t, "rpc-1", 0, 2, "a", 2), chunkFrame(t, "rpc-2", 1, 2, "b", 2)}
		}},
		{"count 不一致", func(t *testing.T) [][]byte {
			return [][]byte{chunkFrame(t, "rpc-1", 0, 3, "a", 2), chunkFrame(t, "rpc-1", 1, 2, "b", 2)}
		}},
		{"声明长度不符", func(t *testing.T) [][]byte {
			raw, _ := json.Marshal(map[string]any{"type": "rpc_chunk", "chunkId": "rpc-1", "index": 0, "count": 1, "byteLength": 10, "data": base64.StdEncoding.EncodeToString([]byte("a"))})
			return [][]byte{raw}
		}},
		{"base64 非法", func(t *testing.T) [][]byte {
			raw, _ := json.Marshal(map[string]any{"type": "rpc_chunk", "chunkId": "rpc-1", "index": 0, "count": 1, "byteLength": 3, "data": "!!!"})
			return [][]byte{raw}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newProc()
			for _, raw := range c.build(t) {
				if _, ok := p.assemble(raw); ok {
					t.Error("残缺或不合法的分片序列不得组装成功")
				}
			}
		})
	}
}

// 丢弃后要能重新开始，不能把状态卡死。
func TestAssembleResetsAfterFailure(t *testing.T) {
	p := newProc()
	p.assemble(chunkFrame(t, "rpc-1", 0, 2, "a", 2))
	p.assemble(chunkFrame(t, "rpc-2", 1, 2, "b", 2))
	if _, ok := p.assemble(chunkFrame(t, "rpc-3", 0, 2, "x", 2)); ok {
		t.Fatal("第一片不该完成")
	}
	got, ok := p.assemble(chunkFrame(t, "rpc-3", 1, 2, "y", 2))
	if !ok || string(got) != "xy" {
		t.Fatalf("坏序列之后应可重新开始，得到 %q ok=%v", string(got), ok)
	}
}

// ---------------- 实例回收 ----------------

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
	s := NewSupervisor(
		func(string, string, any) {},
		map[Kind]Dialect{KindOMP: fakeDialect{}, KindPi: fakeDialect{}},
		func(Kind) (string, []string, []string) { return "", nil, nil },
		nil,
	)
	s.SetEmitter(emit)
	return s
}

func tempDir(i int) string { return "/tmp/probe-" + string(rune('a'+i)) }

func countEngines(s *Supervisor) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.engines)
}

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

// 反复按不同键取引擎，实例数必须有界：不限的话切十次目录就常驻二十个进程。
// evictLocked 在 Engine() 里就地淘汰，所以建完就已经卡在上限了。
func TestEnginesAreRecycled(t *testing.T) {
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
}

// 正在流式回答的引擎绝不能被回收：回收等于打断用户正在看的那一轮。
func TestBusyEngineNeverRecycled(t *testing.T) {
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
}

// 上限必须在取引擎时就生效，不能只靠外部调 Sweep。
func TestHardLimitOnEngineCreate(t *testing.T) {
	s := newTestSupervisor()
	defer s.Shutdown()
	for i := 0; i < maxEngines*4; i++ {
		s.Engine(KindOMP, tempDir(i))
	}
	if got := countEngines(s); got > maxEngines {
		t.Errorf("取 %d 次后实例数 %d 超过上限 %d", maxEngines*4, got, maxEngines)
	}
}

// 并发取同一个键必须返回同一个实例：否则两个 goroutine 各拉一个进程抢同一个 cwd。
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

// StopAll / StopKind 之后实例表必须清空，否则是持有已死进程引用的僵尸句柄。
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

// 引擎进程死在对话中途时，那一轮必须自己收尾。
//
// 这是会悄无声息错的地方：正常路径的结束帧（agent_end / agent_settled）
// 永远不会再来了，而收尾函数靠 runID 幂等——进程死的时候 runID 已经被清掉，
// 于是那一轮永远收不了尾。界面于是停在「正在回答」：输入框禁用、
// 停止按钮一直转圈，用户只能重启应用。
//
// 所以这里钉死：进程退出后必须发出 chat:done（外加一条 chat:error 说明原因）。
func TestDeadEngineFinishesTurn(t *testing.T) {
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
}

// 空闲时进程退出（回收、手动停止）不该发出收尾事件。
//
// 没有轮要收的时候发 chat:done，会让界面白白收掉一轮不存在的话。
func TestIdleEngineExitEmitsNoTurnEvents(t *testing.T) {
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
}
