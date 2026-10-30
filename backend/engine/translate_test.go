package engine

// 差异解析与协议翻译：这两处错了界面照样渲染，只是内容不对。
//
//   - 差异行号算错时，每行的编号对不上，而用户不会知道那是算错的；
//   - 块的顺序一丢就读不出先干了什么（分组会把工具卡全顶到正文前面）；
//   - 增量与全量不等价时，「正在打字」与「刷新后重看」会长得不一样。

import (
	"encoding/json"
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

// ---------------- 协议翻译 ----------------

// feed 把若干条 message_update 帧依次喂进翻译器。
func feed(tr *Translator, raws ...string) {
	for _, raw := range raws {
		tr.Apply("message_update", json.RawMessage(raw))
	}
}

// blockOrder 取一轮里各块的类型，按顺序。
func blockOrder(t *Turn) []string {
	out := make([]string, 0, len(t.Blocks))
	for _, b := range t.Blocks {
		out = append(out, b.Kind)
	}
	return out
}

// 工具块要带上差异、文件路径与状态。
//
// 差异漏了的表现是「引擎改了什么」整段消失；耗时漏了更隐蔽：
// 0 看起来像「瞬间完成」，是个会误导人的假数字，所以引擎没给时自己量。
func TestTranslateToolBlock(t *testing.T) {
	t.Run("带上差异与文件路径", func(t *testing.T) {
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
	})

	t.Run("引擎没给耗时就自己量", func(t *testing.T) {
		tr := NewTranslator()
		tr.BeginTurn("RUN_1", "跑一下", "m")
		tr.Apply("tool_execution_start", json.RawMessage(`{"toolCallId":"tc1","toolName":"bash","args":{"command":"sleep"}}`))
		time.Sleep(20 * time.Millisecond)
		up := tr.Apply("tool_execution_end", json.RawMessage(`{"toolCallId":"tc1","result":{"content":[{"type":"text","text":"ok"}]}}`))
		if up == nil || up.Block == nil {
			t.Fatal("tool_execution_end 应该有块更新")
		}
		if up.Block.DurationMS <= 0 {
			t.Fatalf("耗时不该是 0：got %d", up.Block.DurationMS)
		}
	})

	t.Run("引擎给了耗时就用引擎的", func(t *testing.T) {
		tr := NewTranslator()
		tr.BeginTurn("RUN_1", "跑一下", "m")
		tr.Apply("tool_execution_start", json.RawMessage(`{"toolCallId":"tc1","toolName":"bash","args":{}}`))
		up := tr.Apply("tool_execution_end", json.RawMessage(`{"toolCallId":"tc1","durationMs":1234,"result":{"content":[{"type":"text","text":"ok"}]}}`))
		if up == nil || up.Block == nil {
			t.Fatal("tool_execution_end 应该有块更新")
		}
		if up.Block.DurationMS != 1234 {
			t.Fatalf("该用引擎给的耗时：got %d want 1234", up.Block.DurationMS)
		}
	})
}

// 块的顺序必须跟引擎吐出来的一样。
//
// 分组会把一轮里所有工具卡顶到正文前面，顺序一丢就读不出先干了什么。
func TestTranslateBlockOrder(t *testing.T) {
	t.Run("正文与工具的相对顺序", func(t *testing.T) {
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
	})

	t.Run("思考与正文与工具交错", func(t *testing.T) {
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
	})
}

// 增量与全量两条路径必须等价：界面上「正在打字」与「刷新后重看」要长得一样。
func TestTranslateDeltaSemantics(t *testing.T) {
	t.Run("增量帧带 delta 不带整表", func(t *testing.T) {
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
	})

	t.Run("增量与全量等价", func(t *testing.T) {
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
	})

	t.Run("新助手消息重置下标", func(t *testing.T) {
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
	})

	t.Run("交出去的块是副本", func(t *testing.T) {
		tr := NewTranslator()
		tr.BeginTurn("run-1", "hi", "")
		tr.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_start","contentIndex":0}}`))
		snap := tr.Current()
		before := snap.Blocks[0].Text
		tr.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"后来加的"}}`))
		if snap.Blocks[0].Text != before {
			t.Fatalf("快照被后续帧改动了：%q → %q", before, snap.Blocks[0].Text)
		}
	})
}
