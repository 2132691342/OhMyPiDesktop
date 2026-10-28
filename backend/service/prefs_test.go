package service

import "testing"

// 会话键是「引擎 + 路径」拼出来的一个字符串。它一旦拆错，
// 「启动时恢复上次会话」就会打开另一个引擎的会话文件——
// 这类错误在界面上表现为「点开之后历史对不上」，不可能靠肉眼发现。
func TestSplitSessionKey(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantKind string
		wantPath string
	}{
		{
			name: "带引擎前缀", raw: "omp\x00C:/Users/x/.omp/agent/sessions/s1.jsonl",
			wantKind: "omp", wantPath: "C:/Users/x/.omp/agent/sessions/s1.jsonl",
		},
		{
			name: "pi", raw: "pi\x00C:/Users/x/.pi/agent/sessions/s2.jsonl",
			wantKind: "pi", wantPath: "C:/Users/x/.pi/agent/sessions/s2.jsonl",
		},
		{name: "无分隔符", raw: "C:/only/path.jsonl", wantPath: "C:/only/path.jsonl"},
		{name: "空", raw: "", wantPath: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kind, path := splitSessionKey(c.raw)
			if string(kind) != c.wantKind {
				t.Fatalf("kind = %q, want %q", kind, c.wantKind)
			}
			if path != c.wantPath {
				t.Fatalf("path = %q, want %q", path, c.wantPath)
			}
		})
	}
}

// Windows 路径里的反斜杠与冒号不该被误当成键分隔符：只有 \x00 是分隔符。
func TestSplitSessionKeyWindowsPath(t *testing.T) {
	kind, path := splitSessionKey("pi\x00" + `C:\Users\x\.pi\agent\sessions\s.jsonl`)
	if kind != "pi" || path != `C:\Users\x\.pi\agent\sessions\s.jsonl` {
		t.Fatalf("unexpected split: %q %q", kind, path)
	}
}
