package engine

// 差异解析：行号累加、文件头不当增删、从 result.details 取差异、参数路径两种键。

import (
	"encoding/json"
	"testing"

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
