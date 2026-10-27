package session

import (
	"os"
	"path/filepath"
	"testing"

	"OhMyPiDesktop/backend/engine"
)

// 会话文件的三点适配：title 槽位、session_info 标题、model_change 字段。
// 这三点错了不会报错——只是标题为空、模型显示成空串，非常难被发现。

func writeLines(t *testing.T, dir, name string, lines []string) string {
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

// omp：首行是 256B 固定宽度的 title 槽位，真正的头部在第二行。
// 把槽位当头部解析，cwd 就取不到，那条会话会被归到错误目录（甚至被过滤掉）。
func TestOMPTitleSlotThenHeader(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "--D-proj-demo--")
	writeLines(t, dir, "2026-10-27T02-00-00-000Z_abc.jsonl", []string{
		`{"type":"title","v":1,"title":"修一下登录","source":"auto","pad":"   "}`,
		`{"type":"session","version":3,"id":"abc","timestamp":"2026-10-27T02:00:00.000Z","cwd":"D:\\proj\\demo"}`,
	})
	ix := NewIndex(filepath.Join(root), engine.KindOMP)
	list, err := ix.List("")
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
	writeLines(t, dir, "2026-10-27T03-00-00-000Z_p1.jsonl", []string{
		`{"type":"session","version":3,"id":"p1","timestamp":"2026-10-27T03:00:00.000Z","cwd":"D:\\proj\\pi"}`,
		`{"type":"session_info","id":"e1","parentId":null,"timestamp":"2026-10-27T03:00:00.000Z","name":"旧标题"}`,
		`{"type":"session_info","id":"e2","parentId":"e1","timestamp":"2026-10-27T03:05:00.000Z","name":"新标题"}`,
		`{"type":"message","id":"e3","parentId":"e2","timestamp":"2026-10-27T03:06:00.000Z","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`,
	})
	ix := NewIndex(root, engine.KindPi)
	list, err := ix.List("")
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
		{"omp", engine.KindOMP, `{"type":"model_change","model":"anthropic/claude"}` + "\n", "anthropic/claude"},
		{"pi", engine.KindPi, `{"type":"model_change","provider":"openai","modelId":"gpt-x"}` + "\n", "openai/gpt-x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "--D-x--")
			writeLines(t, dir, "2026-10-27T04-00-00-000Z_m.jsonl", []string{
				`{"type":"session","version":3,"id":"m","cwd":"D:\\x"}`,
			})
			// 把 model_change 追加进同一个文件
			path := filepath.Join(dir, "2026-10-27T04-00-00-000Z_m.jsonl")
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(tc.line); err != nil {
				t.Fatal(err)
			}
			f.Close()

			ix := NewIndex(root, tc.kind)
			list, err := ix.List("")
			if err != nil {
				t.Fatal(err)
			}
			if len(list) != 1 {
				t.Fatalf("会话数 = %d", len(list))
			}
			if list[0].Model != tc.want {
				t.Fatalf("模型 = %q，期望 %q", list[0].Model, tc.want)
			}
		})
	}
}

// 未知条目必须跳过而不是让整条会话解析失败：两个引擎各有十几种私有类型，
// 一个不认识的类型就让列表里少一条会话，是最难查的一类问题。
func TestUnknownEntriesAreSkipped(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "--D-x--")
	writeLines(t, dir, "2026-10-27T05-00-00-000Z_u.jsonl", []string{
		`{"type":"session","version":3,"id":"u","cwd":"D:\\x"}`,
		`{"type":"ttsr_injection","id":"a","somethingNew":true}`,
		`{"type":"context_edit","id":"b","targetId":"x","replacement":"y"}`,
		`{"type":"label","id":"c","label":"wip"}`,
		`{"type":"message","id":"d","message":{"role":"assistant","model":"p/m","usage":{"totalTokens":42,"cost":{"total":0.5}},"content":[{"type":"text","text":"ok"}]}}`,
	})
	ix := NewIndex(root, engine.KindOMP)
	list, err := ix.List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("会话数 = %d，期望 1（未知条目被跳过，不是让整条会话消失）", len(list))
	}
	if list[0].Tokens != 42 || list[0].Cost != 0.5 {
		t.Fatalf("用量 = %d / %.2f，期望 42 / 0.5", list[0].Tokens, list[0].Cost)
	}
	if list[0].Messages != 1 {
		t.Fatalf("消息数 = %d，期望 1", list[0].Messages)
	}
}

// 按 cwd 过滤是列表页唯一的筛选依据。它靠头部真实 cwd，不靠目录名解码——
// 目录名有歧义（路径里本来就有 '-'）。
func TestListFiltersByHeaderCWD(t *testing.T) {
	root := t.TempDir()
	writeLines(t, filepath.Join(root, "--D-a--"), "2026-10-27T06-00-00-000Z_a.jsonl", []string{
		`{"type":"session","version":3,"id":"a","cwd":"D:\\a"}`,
	})
	writeLines(t, filepath.Join(root, "--D-b--"), "2026-10-27T06-00-00-000Z_b.jsonl", []string{
		`{"type":"session","version":3,"id":"b","cwd":"D:\\b"}`,
	})
	ix := NewIndex(root, engine.KindPi)
	onlyA, err := ix.List(`D:\a`)
	if err != nil {
		t.Fatal(err)
	}
	if len(onlyA) != 1 || onlyA[0].ID != "a" {
		t.Fatalf("过滤结果 = %+v，期望只有 a", onlyA)
	}
}
