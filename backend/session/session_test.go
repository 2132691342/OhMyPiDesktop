package session

// 只测读引擎私有文件时静默出错的点：跨引擎等价投影、标题读取、未知条目跳过、缓存有界。

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"OhMyPiDesktop/backend/engine"
)

// writeSession 把若干行写成一个会话文件（每条会话单独占一个子目录，List 据此读取）。
func writeSession(t *testing.T, dir, name string, lines []string) string {
	t.Helper()
	sub := filepath.Join(dir, strings.TrimSuffix(name, ".jsonl"))
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(sub, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, ln := range lines {
		if _, err := f.WriteString(ln + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// 跨引擎等价投影：同一段对话，omp/pi 两种日志格式投影出来必须一致。
func TestSessionSnapshotEquivalence(t *testing.T) {
	roundtrip := func(name string, lines []string, kind engine.Kind) (string, string, int, int, float64, int) {
		dir := t.TempDir()
		writeSession(t, dir, name, lines)
		idx := NewIndex(dir, kind)
		list, err := idx.List("")
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 {
			t.Fatalf("%s: 期望 1 个会话，得到 %d", name, len(list))
		}
		s := list[0]
		d, err := idx.Detail(s.Path)
		if err != nil || d == nil {
			t.Fatalf("%s: Detail 为空: %v", name, err)
		}
		var sb strings.Builder
		for _, it := range d.Items {
			sb.WriteString(it.Role)
			if it.Text != "" {
				sb.WriteString(":")
				sb.WriteString(it.Text)
			}
			sb.WriteString("|")
		}
		return s.Title, strings.TrimSuffix(sb.String(), "|"), d.Messages, d.Tokens, d.Cost, len(d.Items)
	}

	piLines := []string{
		`{"type":"session","session_id":"SID","version":"2.0.0","cwd":"/repo","model":"provider-x/model-y"}`,
		`{"type":"session_info","name":"主题标题"}`,
		`{"type":"message","message":{"role":"user","content":"hi","timestamp":1000}}`,
		`{"type":"message","message":{"role":"assistant","content":"hello","timestamp":2000,"usage":{"totalTokens":30,"cost":{"total":0.5}}}}`,
	}
	title, proj, msgs, toks, cost, n := roundtrip("pi.jsonl", piLines, engine.KindPi)
	if title != "主题标题" {
		t.Errorf("pi 标题 = %q，期望 session_info 里的 主题标题", title)
	}
	if proj != "user:hi|assistant:hello" {
		t.Errorf("pi 投影 = %q", proj)
	}
	if n != 2 {
		t.Errorf("pi 条目数 = %d，期望 2", n)
	}
	if toks != 30 || cost != 0.5 {
		t.Errorf("pi 用量 = (%d,%v)，期望 (30,0.5)", toks, cost)
	}

	ompLines := []string{
		`{"type":"title","title":"主题标题"}`,
		`{"type":"session","cwd":"/repo","model":"provider-x/model-y"}`,
		`{"type":"message","message":{"role":"user","content":"hi","timestamp":1000}}`,
		`{"type":"message","message":{"role":"assistant","content":"hello","timestamp":2000,"usage":{"totalTokens":30,"cost":{"total":0.5}}}}`,
	}
	title2, proj2, msgs2, toks2, cost2, n2 := roundtrip("omp.jsonl", ompLines, engine.KindOMP)
	if title2 != "主题标题" {
		t.Errorf("omp 标题 = %q，期望 title 条目里的 主题标题", title2)
	}
	if proj2 != "user:hi|assistant:hello" {
		t.Errorf("omp 投影 = %q", proj2)
	}
	if n2 != 2 {
		t.Errorf("omp 条目数 = %d，期望 2", n2)
	}
	// 两种格式的同段对话，投影出的内容与用量必须一致。
	if proj != proj2 {
		t.Errorf("跨引擎投影不一致：\n pi=%q\nomp=%q", proj, proj2)
	}
	if msgs != msgs2 || toks != toks2 || cost != cost2 {
		t.Errorf("跨引擎用量不一致：pi(%d,%d,%v) omp(%d,%d,%v)", msgs, toks, cost, msgs2, toks2, cost2)
	}
}

// 两种日志格式都要读得出来标题。
func TestReadOmpAndPiBothFormats(t *testing.T) {
	omp := t.TempDir()
	writeSession(t, omp, "a.jsonl", []string{
		`{"type":"title","title":"我的会话"}`,
		`{"type":"session","cwd":"/r","model":"p/m"}`,
		`{"type":"message","message":{"role":"user","content":"x","timestamp":1}}`,
	})
	list, err := NewIndex(omp, engine.KindOMP).List("")
	if err != nil || len(list) != 1 || list[0].Title != "我的会话" {
		t.Fatalf("omp 标题读取失败: %v %+v", err, list)
	}

	pi := t.TempDir()
	writeSession(t, pi, "b.jsonl", []string{
		`{"type":"session","session_id":"s","version":"2.0.0","cwd":"/r","model":"p/m"}`,
		`{"type":"session_info","name":"第一个问题"}`,
		`{"type":"message","message":{"role":"user","content":"x","timestamp":1}}`,
	})
	list2, err := NewIndex(pi, engine.KindPi).List("")
	if err != nil || len(list2) != 1 || list2[0].Title != "第一个问题" {
		t.Fatalf("pi 标题读取失败: %v %+v", err, list2)
	}
}

// 读不出来的私有条目一律跳过，不能报错也不能漏掉有用会话。
func TestSkipUnknownEntriesDoesNotError(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "a.jsonl", []string{
		`{"type":"session","session_id":"s","version":"2.0.0","cwd":"/r","model":"p/m"}`,
		`{"type":"totally_unknown_thing","payload":{"x":1}}`,
		`{"type":"message","message":{"role":"user","content":"hi","timestamp":1}}`,
		`{"type":"another_private_type","foo":"bar"}`,
		`{"type":"message","message":{"role":"assistant","content":"ok","timestamp":2}}`,
	})
	d, err := NewIndex(dir, engine.KindPi).Detail(filepath.Join(dir, "a", "a.jsonl"))
	if err != nil || d == nil {
		t.Fatal(err)
	}
	if len(d.Items) != 2 {
		t.Fatalf("未知条目应被跳过，剩下 %d 条，期望 2", len(d.Items))
	}
}

// 没有 usage 行的会话，费用默认 0，且不能从别的会话累加。
func TestUsageDefaultsWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "a.jsonl", []string{
		`{"type":"session","session_id":"s","version":"2.0.0","cwd":"/r","model":"p/m"}`,
		`{"type":"message","message":{"role":"user","content":"hi","timestamp":1}}`,
	})
	d, err := NewIndex(dir, engine.KindPi).Detail(filepath.Join(dir, "a", "a.jsonl"))
	if err != nil || d == nil || d.Tokens != 0 || d.Cost != 0 {
		t.Fatalf("无 usage 应为 0/0，得到 %+v err=%v", d, err)
	}
}

// 文件缓存有界：反复读超过 cacheMax 个会话文件，缓存不得无限增长。
func TestIndexFileCacheBounded(t *testing.T) {
	dir := t.TempDir()
	total := cacheMax + 200
	for i := 0; i < total; i++ {
		name := "s" + strconv.Itoa(i) + ".jsonl"
		writeSession(t, dir, name, []string{
			`{"type":"session","session_id":"s","version":"2.0.0","cwd":"/r","model":"p/m"}`,
			`{"type":"message","message":{"role":"user","content":"hi","timestamp":1}}`,
		})
	}
	idx := NewIndex(dir, engine.KindPi)
	list, err := idx.List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != total {
		t.Fatalf("列表条数 = %d，期望 %d", len(list), total)
	}
	if got := idx.CacheLen(); got > cacheMax {
		t.Fatalf("文件缓存 %d 超过上限 %d", got, cacheMax)
	}
}

// 会话改名/删除后必须让该文件缓存失效，否则读到旧标题。
func TestIndexInvalidate(t *testing.T) {
	dir := t.TempDir()
	p := writeSession(t, dir, "a.jsonl", []string{
		`{"type":"session_info","name":"旧标题"}`,
		`{"type":"session","session_id":"s","version":"2.0.0","cwd":"/r","model":"p/m"}`,
		`{"type":"message","message":{"role":"user","content":"hi","timestamp":1}}`,
	})
	idx := NewIndex(dir, engine.KindPi)
	if _, err := idx.List(""); err != nil {
		t.Fatal(err)
	}
	before := idx.CacheLen()
	idx.Invalidate(p)
	if got := idx.CacheLen(); got != before-1 {
		t.Errorf("失效后缓存应减少 1（%d → %d），实际 %d", before, before-1, got)
	}
}
