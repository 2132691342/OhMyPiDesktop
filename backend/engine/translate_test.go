package engine

import (
	"encoding/json"
	"testing"
)

// 翻译器的三条不变量：原始顺序、增量与整表的等价性、上下文隔离。
// 这三件事错了不会编译失败、不会报错，只会让界面显示的顺序或内容悄悄不对。

func blockOrder(t *Turn) []string {
	out := make([]string, 0, len(t.Blocks))
	for _, b := range t.Blocks {
		out = append(out, b.Kind)
	}
	return out
}

// 思考 / 正文 / 工具在一条助手消息里可能交错出现，顺序必须与引擎吐出的顺序一致。
// 按字段类型分组会把所有工具卡顶到正文前面，那时就读不出「先想了什么、再干了什么」。
func TestTranslateKeepsOriginalOrder(t *testing.T) {
	tr := NewTranslator()
	tr.BeginTurn("run-1", "hi", "")
	feed := []string{
		`{"assistantMessageEvent":{"type":"thinking_start","contentIndex":0}}`,
		`{"assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"想一下"}}`,
		`{"assistantMessageEvent":{"type":"thinking_end","contentIndex":0,"content":"想一下"}}`,
		`{"assistantMessageEvent":{"type":"text_start","contentIndex":1}}`,
		`{"assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"先说"}}`,
		`{"assistantMessageEvent":{"type":"toolcall_start","contentIndex":2}}`,
		`{"assistantMessageEvent":{"type":"toolcall_end","contentIndex":2,"toolCall":{"id":"t1","name":"bash","arguments":{"cmd":"ls"}}}}`,
		`{"assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"再说"}}`,
	}
	for _, raw := range feed {
		tr.Apply("message_update", json.RawMessage(raw))
	}
	turn := tr.Current()
	got := blockOrder(turn)
	want := []string{BlockThinking, BlockText, BlockTool}
	if len(got) != len(want) {
		t.Fatalf("块数 = %d (%v)，期望 %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 块 = %s，期望 %s（顺序 %v）", i, got[i], want[i], got)
		}
	}
	// text_start 之后被工具调用打断，再来的 delta 仍要落在同一块上。
	if turn.Blocks[1].Text != "先说再说" {
		t.Fatalf("正文 = %q，期望两段 delta 累加为「先说再说」", turn.Blocks[1].Text)
	}
}

// 增量必须真的是增量：delta 只报尾部增长，不整表重发。
// 整表重发的代价是 O(字数²)，在长回答上就是肉眼可见的卡顿。
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

// 工具结果按 toolCallId 回填到同一块上，而不是新开一块。
// 配对错的表现是「工具卡一直转圈，输出挂在别处」。
func TestTranslateBackfillsToolResult(t *testing.T) {
	tr := NewTranslator()
	tr.BeginTurn("run-1", "hi", "")
	tr.Apply("tool_execution_start", json.RawMessage(`{"toolCallId":"t1","toolName":"bash","args":{"cmd":"ls"}}`))
	tr.Apply("tool_execution_end", json.RawMessage(`{"toolCallId":"t1","result":{"content":[{"type":"text","text":"a\nb"}],"details":{"wallTimeMs":12}},"isError":false}`))
	turn := tr.Current()
	if len(turn.Blocks) != 1 {
		t.Fatalf("块数 = %d，期望 1（结果应回填，不该新开块）", len(turn.Blocks))
	}
	b := turn.Blocks[0]
	if b.Status != "done" || b.Output != "a\nb" || b.DurationMS != 12 {
		t.Fatalf("工具块 = %+v，期望 done / 输出 a\\nb / 12ms", b)
	}
}

// 交出去的 blocks 必须是副本：调用方拿到的切片不能被后续帧改动。
// 交实时指针等于把正在被追加的 slice 递给 JSON 序列化——那是竞态，且会读出半截数据。
func TestTranslateHandsOutCopies(t *testing.T) {
	tr := NewTranslator()
	tr.BeginTurn("run-1", "hi", "")
	tr.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_start","contentIndex":0}}`))
	snap := tr.Current()
	before := snap.Blocks[0].Text

	tr.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"后来加的"}}`))

	if snap.Blocks[0].Text != before {
		t.Fatalf("先前拿到的快照被后续帧改动了：%q → %q", before, snap.Blocks[0].Text)
	}
	if now := tr.Current(); now.Blocks[0].Text != before+"后来加的" {
		t.Fatalf("当前轮 = %q，期望尾部追加", now.Blocks[0].Text)
	}
}

// 换会话后必须清干净：否则旧轮的块会挂在新会话的第一条回答前面。
func TestTranslateResetClearsTurn(t *testing.T) {
	tr := NewTranslator()
	tr.BeginTurn("run-1", "hi", "")
	tr.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_start","contentIndex":0}}`))
	tr.Reset()
	if tr.Current() != nil {
		t.Fatal("Reset 之后 Current 应为 nil")
	}
	if tr.CurrentRunID() != "" {
		t.Fatalf("Reset 之后 runID = %q，期望空", tr.CurrentRunID())
	}
}

// 增量与全量必须等价：同一条消息，引擎可能先发全量快照再发增量，
// 也可能只发增量。两种路径累加出来的块必须一模一样。
//
// 这条错了不会报错，只会让界面在某些模型上把正文显示两遍或显示一半。
func TestTranslateDeltaEqualsFullSnapshot(t *testing.T) {
	// 路径一：逐段增量
	inc := NewTranslator()
	inc.BeginTurn("run-1", "hi", "")
	inc.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_start","contentIndex":0}}`))
	inc.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"你好"}}`))
	inc.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"，世界"}}`))

	// 路径二：直接给全量（text_end 带完整 content）
	full := NewTranslator()
	full.BeginTurn("run-1", "hi", "")
	full.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_start","contentIndex":0}}`))
	full.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_end","contentIndex":0,"content":"你好，世界"}}`))

	gotInc := inc.Current()
	gotFull := full.Current()
	if len(gotInc.Blocks) != len(gotFull.Blocks) {
		t.Fatalf("块数不同：增量 %d，全量 %d", len(gotInc.Blocks), len(gotFull.Blocks))
	}
	if gotInc.Blocks[0].Text != gotFull.Blocks[0].Text {
		t.Errorf("正文不同：增量 %q，全量 %q", gotInc.Blocks[0].Text, gotFull.Blocks[0].Text)
	}
}

// 一条新的助手消息要重置 contentIndex 映射：新消息的 index 从 0 重新开始，
// 不重置就会把新消息的第一段正文追加到上一段的块里。
func TestTranslateNewAssistantMessageResetsIndices(t *testing.T) {
	tr := NewTranslator()
	tr.BeginTurn("run-1", "hi", "")
	tr.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_start","contentIndex":0}}`))
	tr.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"第一段"}}`))

	up := tr.Apply("message_start", json.RawMessage(`{"message":{"role":"assistant","model":"p/m"}}`))
	if up == nil || up.Mode != "blocks" {
		t.Fatalf("新助手消息应产出整表更新，实际 %+v", up)
	}

	tr.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_start","contentIndex":0}}`))
	tr.Apply("message_update", json.RawMessage(`{"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"第二段"}}`))
	turn := tr.Current()
	if len(turn.Blocks) != 2 {
		t.Fatalf("块数 = %d，期望 2（新消息的 index 0 应开新块）", len(turn.Blocks))
	}
	if turn.Blocks[0].Text != "第一段" || turn.Blocks[1].Text != "第二段" {
		t.Fatalf("两块 = %q / %q", turn.Blocks[0].Text, turn.Blocks[1].Text)
	}
}

// 工具块的流式文本（omp 的 tool_stream_update / pi 的 bash_execution_update）
// 被两家的 Dialect 归到同一个入口，且必须追加而不是覆盖。
func TestToolDeltaAppends(t *testing.T) {
	tr := NewTranslator()
	tr.BeginTurn("run-1", "hi", "")
	tr.Apply("tool_execution_start", json.RawMessage(`{"toolCallId":"t1","toolName":"bash"}`))
	tr.ToolDelta("t1", "第一行\n", true)
	up := tr.ToolDelta("t1", "第二行\n", true)
	if up == nil || up.Block == nil {
		t.Fatalf("工具流更新 = %+v，期望带 block", up)
	}
	if want := "第一行\n第二行\n"; up.Block.Output != want {
		t.Fatalf("输出 = %q，期望 %q", up.Block.Output, want)
	}
	if up.Block.Status != "running" {
		t.Fatalf("状态 = %q，期望 running", up.Block.Status)
	}
}
