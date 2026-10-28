package session

// 会话文件只测会静默出错的两类：三点适配（title 槽位 / session_info 标题 / model_change 字段）
// 与投影（用量累加、工具结果回填、按头 cwd 过滤、缓存命中）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"OhMyPiDesktop/backend/engine"
)

// headerLine 是 omp 的 256 字节固定宽度 title 槽位。
const headerLine = `{"type":"title","v":1,"title":"修一下登录","source":"auto","pad":"     "}`

// sessionLine 是两个引擎共用的会话头部。
const sessionLine = `{"type":"session","version":3,"id":"abc123","timestamp":"2026-10-27T02:00:00.000Z","cwd":"D:\\proj\\demo"}`

// writeSession 写一个会话文件，返回路径。
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

// ---------------- 三点适配 ----------------

// omp：首行是 title 槽位，真正的头部在第二行。
// 把槽位当头部解析，cwd 就取不到，那条会话会被归到错误目录（甚至被过滤掉）。
func TestOMPTitleSlotThenHeader(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "--D-proj-demo--")
	writeSession(t, dir, "2026-10-27T02-00-00-000Z_abc.jsonl", []string{
		headerLine,
		sessionLine,
	})

	list, err := NewIndex(root, engine.KindOMP).List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("会话数 = %d，期望 1", len(list))
	}
	if list[0].Title != "修一下登录" {
		t.Fatalf("标题 = %q，期望来自 title 槽位", list[0].Title)
	}
	if list[0].CWD != `D:\proj\demo` {
		t.Fatalf("cwd = %q，期望来自第二行的头部", list[0].CWD)
	}
}

// pi：没有 title 槽位，首行就是头部；标题在 session_info 条目的 name 上，
// 且以最新一条为准（用户可能改过好几次）。
func TestPiTitleFromSessionInfo(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "--D-proj-pi--")
	writeSession(t, dir, "2026-10-27T03-00-00-000Z_p1.jsonl", []string{
		`{"type":"session","version":3,"id":"p1","timestamp":"2026-10-27T03:00:00.000Z","cwd":"D:\\proj\\pi"}`,
		`{"type":"session_info","id":"e1","parentId":null,"timestamp":"2026-10-27T03:00:00.000Z","name":"旧标题"}`,
		`{"type":"session_info","id":"e2","parentId":"e1","timestamp":"2026-10-27T03:05:00.000Z","name":"新标题"}`,
		`{"type":"message","id":"e3","parentId":"e2","timestamp":"2026-10-27T03:06:00.000Z","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`,
	})

	list, err := NewIndex(root, engine.KindPi).List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("会话数 = %d，期望 1", len(list))
	}
	if list[0].Title != "新标题" {
		t.Fatalf("标题 = %q，期望取最新一条 session_info", list[0].Title)
	}
	if list[0].CWD != `D:\proj\pi` {
		t.Fatalf("cwd = %q", list[0].CWD)
	}
}

// model_change 两家字段不同：omp 给 "provider/id"，pi 给 {provider, modelId}。
// 归一化失败的表现是「会话列表里模型那一列空白」。
func TestModelChangeBothShapes(t *testing.T) {
	cases := []struct {
		name string
		kind engine.Kind
		line string
		want string
	}{
		{"omp", engine.KindOMP, `{"type":"model_change","model":"anthropic/claude"}`, "anthropic/claude"},
		{"pi", engine.KindPi, `{"type":"model_change","provider":"openai","modelId":"gpt-x"}`, "openai/gpt-x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "--D-x--")
			path := writeSession(t, dir, "2026-10-27T04-00-00-000Z_m.jsonl", []string{
				`{"type":"session","version":3,"id":"m","cwd":"D:\\x"}`,
			})
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(tc.line + "\n"); err != nil {
				t.Fatal(err)
			}
			f.Close()

			list, err := NewIndex(root, tc.kind).List("")
			if err != nil {
				t.Fatal(err)
			}
			if len(list) != 1 {
				t.Fatalf("会话数 = %d", len(list))
			}
			if list[0].Model != tc.want {
				t.Errorf("模型 = %q，期望 %q", list[0].Model, tc.want)
			}
		})
	}
}

// 未知条目要跳过而不是报错：omp 有 16 种条目、pi 有 11 种，各有私有类型。
// 引擎加一种新条目不该让整个会话列表读不出来。
func TestUnknownEntriesAreSkipped(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "--D-proj-demo--")
	writeSession(t, dir, "2026-10-27T05-00-00-000Z_u.jsonl", []string{
		headerLine,
		sessionLine,
		`{"type":"omp_private_thing","payload":{"whatever":1}}`,
		`{"type":"message","id":"m1","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"another_unknown","x":2}`,
	})

	list, err := NewIndex(root, engine.KindOMP).List("")
	if err != nil {
		t.Fatalf("未知条目不该让列表失败：%v", err)
	}
	if len(list) != 1 {
		t.Fatalf("会话数 = %d，期望 1", len(list))
	}
}

// 目录编码两家不同（pi 恒为 --<绝对路径>--，omp 是三段式），
// 所以不能自己拼目录名去猜，要靠读每个文件头部的 cwd 分组。
// 按工作目录过滤错的表现是「切了目录但会话列表还是上一个目录的」。
func TestListFiltersByHeaderCWD(t *testing.T) {
	root := t.TempDir()
	writeSession(t, filepath.Join(root, "--D-a--"), "2026-10-27T06-00-00-000Z_a.jsonl", []string{
		`{"type":"session","version":3,"id":"a","cwd":"D:\\a"}`,
	})
	writeSession(t, filepath.Join(root, "--D-b--"), "2026-10-27T06-00-00-000Z_b.jsonl", []string{
		`{"type":"session","version":3,"id":"b","cwd":"D:\\b"}`,
	})

	onlyA, err := NewIndex(root, engine.KindPi).List(`D:\a`)
	if err != nil {
		t.Fatal(err)
	}
	if len(onlyA) != 1 || onlyA[0].ID != "a" {
		t.Fatalf("过滤结果 = %+v，期望只有 a", onlyA)
	}
}

// ---------------- 投影 ----------------

// 列表要累加助手消息的用量：用量页的数字全部来自这里。
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
		t.Errorf("缓存应命中（mtime 与大小未变），却重读了：标题 %q → %q", first[0].Title, second[0].Title)
	}
}
