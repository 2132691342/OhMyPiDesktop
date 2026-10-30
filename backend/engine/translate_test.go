package engine

// 协议翻译：工具差异/耗时回填、思考/正文/工具交错顺序、增量与全量等价、新消息重置下标、交付副本。

import (
	"encoding/json"
	"testing"
	"time"
)

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
