package engine

// 分片重组：乱序/重复/换 id/长度不符/base64 非法整段丢弃，坏序列后可重新开始。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
)

// fakeDialect 只用于回答能力表；不启动进程。
type fakeDialect struct{ caps Caps }

func (f fakeDialect) Kind() Kind                             { return KindPi }
func (f fakeDialect) Caps() Caps                             { return f.caps }
func (f fakeDialect) Args(string, []string) []string         { return nil }
func (f fakeDialect) Handshake(context.Context, *Proc) error { return nil }
func (f fakeDialect) Consume(*Engine, Frame) bool            { return false }

// ---------------- 分片重组 ----------------

func chunkFrame(t *testing.T, chunkID string, index, count int, payload string, totalLen int) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"type": "rpc_chunk", "chunkId": chunkID, "index": index, "count": count,
		"byteLength": totalLen, "data": base64.StdEncoding.EncodeToString([]byte(payload)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func newProc() *Proc { return &Proc{opts: Options{Dialect: fakeDialect{}}} }

func TestAssembleJoinsChunks(t *testing.T) {
	p := newProc()
	parts := []string{`{"type":"response",`, `"id":"x","data":{"a":1}}`}
	total := len(parts[0]) + len(parts[1])
	var out []byte
	for i, part := range parts {
		got, ok := p.assemble(chunkFrame(t, "rpc-1", i, len(parts), part, total))
		if i < len(parts)-1 {
			if ok {
				t.Fatalf("第 %d 片不应宣告完成", i)
			}
			continue
		}
		if !ok {
			t.Fatalf("最后一片应组装完成")
		}
		out = got
	}
	if want := parts[0] + parts[1]; string(out) != want {
		t.Errorf("拼出 %q，期望 %q", out, want)
	}
}

// 每一条校验分支都必须整段丢弃，不能放半截帧上去。
func TestAssembleRejectsBrokenSequences(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T) [][]byte
	}{
		{"index 乱序", func(t *testing.T) [][]byte {
			return [][]byte{chunkFrame(t, "rpc-1", 1, 2, "b", 2), chunkFrame(t, "rpc-1", 0, 2, "a", 2)}
		}},
		{"index 重复", func(t *testing.T) [][]byte {
			return [][]byte{chunkFrame(t, "rpc-1", 0, 2, "a", 2), chunkFrame(t, "rpc-1", 0, 2, "b", 2)}
		}},
		{"chunkId 中途变了", func(t *testing.T) [][]byte {
			return [][]byte{chunkFrame(t, "rpc-1", 0, 2, "a", 2), chunkFrame(t, "rpc-2", 1, 2, "b", 2)}
		}},
		{"count 不一致", func(t *testing.T) [][]byte {
			return [][]byte{chunkFrame(t, "rpc-1", 0, 3, "a", 2), chunkFrame(t, "rpc-1", 1, 2, "b", 2)}
		}},
		{"声明长度不符", func(t *testing.T) [][]byte {
			raw, _ := json.Marshal(map[string]any{"type": "rpc_chunk", "chunkId": "rpc-1", "index": 0, "count": 1, "byteLength": 10, "data": base64.StdEncoding.EncodeToString([]byte("a"))})
			return [][]byte{raw}
		}},
		{"base64 非法", func(t *testing.T) [][]byte {
			raw, _ := json.Marshal(map[string]any{"type": "rpc_chunk", "chunkId": "rpc-1", "index": 0, "count": 1, "byteLength": 3, "data": "!!!"})
			return [][]byte{raw}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newProc()
			for _, raw := range c.build(t) {
				if _, ok := p.assemble(raw); ok {
					t.Error("残缺或不合法的分片序列不得组装成功")
				}
			}
		})
	}
}

// 丢弃后要能重新开始，不能把状态卡死。
func TestAssembleResetsAfterFailure(t *testing.T) {
	p := newProc()
	p.assemble(chunkFrame(t, "rpc-1", 0, 2, "a", 2))
	p.assemble(chunkFrame(t, "rpc-2", 1, 2, "b", 2))
	if _, ok := p.assemble(chunkFrame(t, "rpc-3", 0, 2, "x", 2)); ok {
		t.Fatal("第一片不该完成")
	}
	got, ok := p.assemble(chunkFrame(t, "rpc-3", 1, 2, "y", 2))
	if !ok || string(got) != "xy" {
		t.Fatalf("坏序列之后应可重新开始，得到 %q ok=%v", string(got), ok)
	}
}
