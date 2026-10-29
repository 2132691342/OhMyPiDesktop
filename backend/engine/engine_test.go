package engine

// 只测会悄无声息出错的地方：判种/能力、协议翻译顺序/增量/回填、分片残缺丢弃、实例有界回收。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"OhMyPiDesktop/backend/domain"
)

// ---------------- 差异解析 ----------------

// 差异解析是最会「悄无声息错」的一环：行号算错时界面照样渲染，
// 只是每行的编号对不上，而用户不会知道那是算错的。
func TestParseToolDiff(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []domain.DiffLineVO
	}{
		{
			name:  "unified 差异：行号按区块头累加",
			input: "@@ -12,7 +12,9 @@\n ctx line\n+added line\n-removed line\n",
			want: []domain.DiffLineVO{
				{LineKind: DiffKindHunk, LineText: "@@ -12,7 +12,9 @@"},
				{LineKind: DiffKindCtx, OldNo: 12, NewNo: 12, LineText: "ctx line"},
				{LineKind: DiffKindAdd, NewNo: 13, LineText: "added line"},
				{LineKind: DiffKindDel, OldNo: 13, LineText: "removed line"},
			},
		},
		{
			name:  "omp 编号差异：竖线分隔，行号取自带值",
			input: "@@ -1,3 +1,4 @@\n 1|unchanged\n+2|new one\n-2|old one\n",
			want: []domain.DiffLineVO{
				{LineKind: DiffKindHunk, LineText: "@@ -1,3 +1,4 @@"},
				{LineKind: DiffKindCtx, OldNo: 1, NewNo: 1, LineText: "unchanged"},
				{LineKind: DiffKindAdd, NewNo: 2, LineText: "new one"},
				{LineKind: DiffKindDel, OldNo: 2, LineText: "old one"},
			},
		},
		{
			name:  "pi 编号差异：空格分隔",
			input: "@@ -1,2 +1,2 @@\n 1 unchanged\n+2 added\n",
			want: []domain.DiffLineVO{
				{LineKind: DiffKindHunk, LineText: "@@ -1,2 +1,2 @@"},
				{LineKind: DiffKindCtx, OldNo: 1, NewNo: 1, LineText: "unchanged"},
				{LineKind: DiffKindAdd, NewNo: 2, LineText: "added"},
			},
		},
		{
			name:  "多个区块：行号从新区块头重新起算",
			input: "@@ -1,2 +1,2 @@\n-a\n+a\n@@ -20,2 +20,2 @@\n-b\n+b\n",
			want: []domain.DiffLineVO{
				{LineKind: DiffKindHunk, LineText: "@@ -1,2 +1,2 @@"},
				{LineKind: DiffKindDel, OldNo: 1, LineText: "a"},
				{LineKind: DiffKindAdd, NewNo: 1, LineText: "a"},
				{LineKind: DiffKindHunk, LineText: "@@ -20,2 +20,2 @@"},
				{LineKind: DiffKindDel, OldNo: 20, LineText: "b"},
				{LineKind: DiffKindAdd, NewNo: 20, LineText: "b"},
			},
		},
		{
			name:  "空差异",
			input: "",
			want:  []domain.DiffLineVO{},
		},
		{
			// 文件头若被当成增删行，用户会看到文件头被涂成红绿两色，
			// 而那是根本不存在的改动。
			name:  "unified 文件头不当成增删行",
			input: "--- hello.txt\n+++ hello.txt\n@@ -1,3 +1,3 @@\n line one\n-line two\n+line 2\n",
			want: []domain.DiffLineVO{
				{LineKind: DiffKindFile, LineText: "--- hello.txt"},
				{LineKind: DiffKindFile, LineText: "+++ hello.txt"},
				{LineKind: DiffKindHunk, LineText: "@@ -1,3 +1,3 @@"},
				{LineKind: DiffKindCtx, OldNo: 1, NewNo: 1, LineText: "line one"},
				{LineKind: DiffKindDel, OldNo: 2, LineText: "line two"},
				{LineKind: DiffKindAdd, NewNo: 2, LineText: "line 2"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseToolDiff(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("行数不对：got %d want %d\ngot=%+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("第 %d 行不对：\n got %+v\nwant %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// 引擎把差异放在 result.details 里，而不在 content 里。
// 漏了这一步的表现是：界面永远看不到「引擎改了什么」。
func TestExtractToolDiff(t *testing.T) {
	cases := []struct {
		name     string
		result   string
		wantDiff bool
		wantPath string
	}{
		{
			name:     "pi 的 edit：details.patch 是标准 unified diff",
			result:   `{"content":[{"type":"text","text":"Successfully replaced 1 block(s)"}],"details":{"diff":"+1 x","patch":"@@ -1 +1 @@\n-a\n+a","path":"/w/a.go"}}`,
			wantDiff: true,
			wantPath: "/w/a.go",
		},
		{
			name:     "omp 的 edit：details.diff 是编号差异",
			result:   `{"content":[{"type":"text","text":"edited"}],"details":{"diff":"@@ -1 +1 @@\n+1|x","path":"/w/b.go"}}`,
			wantDiff: true,
			wantPath: "/w/b.go",
		},
		{
			name:     "omp 的多文件改动：逐文件给差异",
			result:   `{"details":{"perFileResults":[{"path":"/w/c.go","diff":"@@ -1 +1 @@\n+1|y"},{"path":"/w/d.go","diff":"@@ -1 +1 @@\n+1|z"}]}}`,
			wantDiff: true,
			wantPath: "/w/c.go",
		},
		{
			name:     "read 工具没有差异",
			result:   `{"content":[{"type":"text","text":"file body"}]}`,
			wantDiff: false,
		},
		{
			// 历史路径传的是 details 本身，不是包着 details 的 result。
			// 两种形状都要认，否则「刷新一下差异就没了」。
			name:     "details 本身就是差异对象",
			result:   `{"diff":"@@ -1 +1 @@\n-a\n+b","path":"/w/e.go"}`,
			wantDiff: true,
			wantPath: "/w/e.go",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diff, path := ExtractToolDiff(json.RawMessage(tc.result))
			if tc.wantDiff && diff == "" {
				t.Fatal("没取到差异")
			}
			if !tc.wantDiff && diff != "" {
				t.Fatalf("不该取到差异，却拿到：%q", diff)
			}
			if path != tc.wantPath {
				t.Errorf("路径不对：got %q want %q", path, tc.wantPath)
			}
		})
	}
}

// pi 的 edit 把路径放在参数里，omp 放在 details.path。两条路都要认。
func TestToolFilePath(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"path 参数", `{"path":"/w/a.go"}`, "/w/a.go"},
		{"file_path 参数", `{"file_path":"/w/b.go"}`, "/w/b.go"},
		{"没有路径", `{"command":"ls"}`, ""},
		{"空参数", ``, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ToolFilePath(json.RawMessage(tc.args)); got != tc.want {
				t.Fatalf("路径不对：got %q want %q", got, tc.want)
			}
		})
	}
}

// ---------------- 协议翻译：工具差异与耗时 ----------------

// 工具执行结束后，块里要带上差异与被改动的文件。
func TestTranslateToolCarriesDiff(t *testing.T) {
	tr := NewTranslator()
	tr.BeginTurn("RUN_1", "改一下", "m")

	start := `{"toolCallId":"tc1","toolName":"edit","args":{"path":"a.go"},"intent":"改一行"}`
	if up := tr.Apply("tool_execution_start", json.RawMessage(start)); up == nil {
		t.Fatal("tool_execution_start 应该有更新")
	}

	end := `{"toolCallId":"tc1","result":{"content":[{"type":"text","text":"edited"}],` +
		`"details":{"diff":"@@ -1,2 +1,2 @@\n-a\n+b","path":"/w/a.go"}}}`
	up := tr.Apply("tool_execution_end", json.RawMessage(end))
	if up == nil || up.Block == nil {
		t.Fatal("tool_execution_end 应该有块更新")
	}
	if len(up.Block.Diff) != 3 {
		t.Fatalf("差异行数不对：got %d want 3", len(up.Block.Diff))
	}
	if up.Block.FilePath != "/w/a.go" {
		t.Errorf("文件路径不对：got %q", up.Block.FilePath)
	}
	if up.Block.Status != "done" {
		t.Errorf("状态不对：got %q", up.Block.Status)
	}
}

// omp 不给耗时，界面却要显示「这个工具跑了多久」——所以自己量。
// 不量的话耗时永远是 0，而 0 看起来像「瞬间完成」，是个会误导人的假数字。
func TestTranslateToolMeasuresDuration(t *testing.T) {
	tr := NewTranslator()
	tr.BeginTurn("RUN_1", "跑一下", "m")

	start := `{"toolCallId":"tc1","toolName":"bash","args":{"command":"sleep"}}`
	tr.Apply("tool_execution_start", json.RawMessage(start))
	// 睡一会儿再结束：耗时必须大于 0。
	time.Sleep(20 * time.Millisecond)

	end := `{"toolCallId":"tc1","result":{"content":[{"type":"text","text":"ok"}]}}`
	up := tr.Apply("tool_execution_end", json.RawMessage(end))
	if up == nil || up.Block == nil {
		t.Fatal("tool_execution_end 应该有块更新")
	}
	if up.Block.DurationMS <= 0 {
		t.Fatalf("耗时不该是 0：got %d", up.Block.DurationMS)
	}
}

// pi 在帧顶层给 durationMs：那个值优先于自己量的。
func TestTranslateToolPrefersEngineDuration(t *testing.T) {
	tr := NewTranslator()
	tr.BeginTurn("RUN_1", "跑一下", "m")

	start := `{"toolCallId":"tc1","toolName":"bash","args":{}}`
	tr.Apply("tool_execution_start", json.RawMessage(start))

	end := `{"toolCallId":"tc1","durationMs":1234,"result":{"content":[{"type":"text","text":"ok"}]}}`
	up := tr.Apply("tool_execution_end", json.RawMessage(end))
	if up == nil || up.Block == nil {
		t.Fatal("tool_execution_end 应该有块更新")
	}
	if up.Block.DurationMS != 1234 {
		t.Fatalf("该用引擎给的耗时：got %d want 1234", up.Block.DurationMS)
	}
}

// 块的顺序必须跟引擎吐出来的一样。
// 分组会把一轮里所有工具卡顶到正文前面，顺序一丢就读不出先干了什么。
func TestTranslatePreservesBlockOrder(t *testing.T) {
	tr := NewTranslator()
	tr.BeginTurn("RUN_1", "q", "m")

	tr.Apply("message_update", json.RawMessage(`{"messageId":"m1","message":{"role":"assistant"},`+
		`"assistantMessageEvent":{"type":"text_start","contentIndex":0}}`))
	tr.Apply("message_update", json.RawMessage(`{"messageId":"m1","message":{"role":"assistant"},`+
		`"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"先说一句"}}`))
	tr.Apply("tool_execution_start", json.RawMessage(`{"toolCallId":"tc1","toolName":"bash","args":{}}`))
	tr.Apply("tool_execution_end", json.RawMessage(`{"toolCallId":"tc1","result":{"content":[]}}`))

	turn := tr.Current()
	if turn == nil {
		t.Fatal("当前轮不该是 nil")
	}
	if len(turn.Blocks) != 2 {
		t.Fatalf("块数不对：got %d want 2", len(turn.Blocks))
	}
	if turn.Blocks[0].Kind != BlockText {
		t.Errorf("第一块应是正文：got %q", turn.Blocks[0].Kind)
	}
	if turn.Blocks[1].Kind != BlockTool {
		t.Errorf("第二块应是工具：got %q", turn.Blocks[1].Kind)
	}
}

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

// 同级 node_modules 的 shim 只看路径段会判成未知，必须靠同级目录识别。
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

// Caps 是按钮是否出现的唯一依据，钉死「哪条命令属于谁」。
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

// 增量与全量必须等价：两条路径累加出的块必须一致。
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

// 交出去的 blocks 必须是副本，不能把正在被追加的 slice 递给序列化。
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

// 反复取不同键，实例数必须有界；超上限在取的时候就地淘汰。
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

// 正在回答的引擎绝不能被回收，否则打断用户正在看的那一轮。
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

// 上限在取引擎时就生效，不能只靠外部 Sweep。
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

// 引擎进程死在对话中途时，那一轮必须自己收尾：发出 chat:done + chat:error，清空 runID。
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

// 空闲时进程退出不该发出收尾事件，否则界面白白收掉一轮不存在的话。
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
