package engine

import (
	"encoding/json"
	"testing"
	"time"
)

// 翻译器是「引擎帧 → 界面块」的唯一通路。
//
// 这里测的是会悄无声息错的两件事：差异有没有被取出来、耗时是不是真的算出来了。
// 取错了不会报错，只会让界面上少一块内容或显示一个 0。

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
//
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
//
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
