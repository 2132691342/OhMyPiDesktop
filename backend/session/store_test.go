package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"OhMyPiDesktop/backend/engine"
)

// 会话索引是列表页唯一的来源，错的表现是「标题不对 / 用量是 0 / 打开了错的会话」。
// 这里只测这三件事：标题槽、cwd 归属、用量累加。

const headerLine = `{"type":"title","v":1,"title":"修一下登录","source":"auto","pad":"     "}`

const sessionLine = `{"type":"session","version":3,"id":"abc123","timestamp":"2026-10-27T02:00:00.000Z","cwd":"D:\\proj\\demo"}`

func writeSession(t *testing.T, dir, name string, lines []string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestListAccumulatesUsage(t *testing.T) {
	root := t.TempDir()
	writeSession(t, filepath.Join(root, "--D-proj-demo--"), "a_abc123.jsonl", []string{
		headerLine,
		sessionLine,
		`{"type":"message","id":"e1","message":{"role":"assistant","content":[{"type":"text","text":"hi"}],"usage":{"totalTokens":100,"cost":{"total":0.5}}}}`,
		`{"type":"message","id":"e2","message":{"role":"assistant","content":[{"type":"text","text":"again"}],"usage":{"totalTokens":250,"cost":{"total":0.25}}}}`,
		`{"type":"message","id":"e3","message":{"role":"user","content":[{"type":"text","text":"q"}]}}`,
	})

	list, err := NewIndex(root, engine.KindOMP).List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("会话数 = %d", len(list))
	}
	if list[0].Tokens != 350 {
		t.Errorf("tokens = %d，期望 350", list[0].Tokens)
	}
	if list[0].Cost < 0.7499 || list[0].Cost > 0.7501 {
		t.Errorf("花费 = %v，期望 0.75", list[0].Cost)
	}
	if list[0].Messages != 2 {
		t.Errorf("消息数 = %d，期望只数带用量的助手消息", list[0].Messages)
	}
}

// 详情里工具结果必须回到发起它的那条助手消息的块里，而不是变成孤立条目。
func TestDetailKeepsToolCallOrder(t *testing.T) {
	root := t.TempDir()
	writeSession(t, filepath.Join(root, "--D-proj-demo--"), "a_abc123.jsonl", []string{
		headerLine,
		sessionLine,
		`{"type":"message","id":"u1","message":{"role":"user","content":[{"type":"text","text":"跑一下"}]}}`,
		`{"type":"message","id":"a1","message":{"role":"assistant","content":[{"type":"thinking","thinking":"先跑"},{"type":"toolCall","id":"call-1","name":"bash","arguments":{"command":"echo hi"}}],"usage":{"totalTokens":10,"cost":{"total":0.01}}}}`,
		`{"type":"message","id":"t1","message":{"role":"toolResult","toolCallId":"call-1","toolName":"bash","isError":false,"content":[{"type":"text","text":"hi\n"}]}}`,
	})

	d, err := NewIndex(root, engine.KindOMP).Detail(filepath.Join(root, "--D-proj-demo--", "a_abc123.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Items) != 3 {
		t.Fatalf("条目数 = %d，期望 3（用户 / 助手 / 工具结果）", len(d.Items))
	}
	if d.Items[0].Kind != "user" {
		t.Errorf("第一条 = %q，期望 user", d.Items[0].Kind)
	}
	if d.Items[1].Kind != "assistant" {
		t.Errorf("第二条 = %q，期望 assistant", d.Items[1].Kind)
	}
	if len(d.Items[1].Blocks) != 2 {
		t.Fatalf("助手块数 = %d，期望思考 + 工具调用两块", len(d.Items[1].Blocks))
	}
	if d.Items[2].Kind != "tool" || d.Items[2].ToolCall != "call-1" {
		t.Errorf("工具结果没带上 toolCallId: %+v", d.Items[2])
	}
	if d.Items[2].Text != "hi\n" {
		t.Errorf("工具输出 = %q", d.Items[2].Text)
	}
}

// 缓存按「mtime + 大小」判定命中。改了内容却保持这两者不变时，
// 必须仍然返回旧结果——否则用户改了会话标题，列表却一直显示旧的。
func TestListCacheHit(t *testing.T) {
	root := t.TempDir()
	path := writeSession(t, filepath.Join(root, "--D-proj-demo--"), "a_abc123.jsonl", []string{headerLine, sessionLine})
	ix := NewIndex(root, engine.KindOMP)
	first, _ := ix.List("")
	if len(first) != 1 {
		t.Fatalf("首次列出 %d 条", len(first))
	}

	// 先记下首次读取时的 mtime：写完要把它拨回去，否则缓存当然不命中。
	orig, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	// 换成另一个标题，但字节长度相同（让文件大小不变）；mtime 不动。
	// 标题里的中文是 UTF-8 多字节，所以按字节数对齐，不按字符数。
	other := strings.Replace(headerLine, "修一下登录", "改一下登录", 1)
	if len(other) != len(headerLine) {
		t.Fatalf("测试前提不成立：两个标题字节长度不同 %d vs %d", len(other), len(headerLine))
	}
	if err := os.WriteFile(path, []byte(other+"\n"+sessionLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 写完把 mtime 拨回原值——这是「文件看起来没变」的真实情形。
	_ = os.Chtimes(path, orig.ModTime(), orig.ModTime())

	second, _ := ix.List("")
	if len(second) != 1 {
		t.Fatalf("二次列出 %d 条", len(second))
	}
	if second[0].Title != first[0].Title {
		t.Errorf("缓存应命中（mtime 与大小未变），却重读了：标题 %q → %q",
			first[0].Title, second[0].Title)
	}
}
