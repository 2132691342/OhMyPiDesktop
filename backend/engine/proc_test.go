package engine

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// 协议 v2 的分片重组：引擎把超大帧切成 rpc_chunk 序列，客户端按 index 拼回。
//
// 这里错了不会报错，只会「少一帧」或「拼出半截 JSON」：解析失败会被静默丢弃，
// 界面上表现为工具输出凭空少一截、或者长回复卡在半路。所以每个校验分支都要守住：
// 乱序、缺片、序列交错、长度不符，都必须**整段丢弃**，绝不能放半截帧上去层。

// chunkFrame 造一个 rpc_chunk 帧。
//
// byteLength 按协议是**整段 JSON 的总字节数**（每一片都带同一个值），
// 不是这一片自己的长度——引擎就是这么发的（rpc-frame.ts:154）。
func chunkFrame(t *testing.T, chunkID string, index, count int, payload string, totalLen int) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"type":       "rpc_chunk",
		"chunkId":    chunkID,
		"index":      index,
		"count":      count,
		"byteLength": totalLen,
		"data":       base64.StdEncoding.EncodeToString([]byte(payload)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// newProc 造一个只用于 assemble 的 Proc。
func newProc() *Proc {
	return &Proc{opts: Options{Dialect: fakeDialect{}}}
}

// TestAssembleJoinsChunks 守住正常路径：按 index 拼回完整帧。
func TestAssembleJoinsChunks(t *testing.T) {
	p := newProc()
	parts := []string{`{"type":"response",`, `"id":"x","data":{"a":1}}`}
	total := len(parts[0]) + len(parts[1])
	var out []byte
	for i, part := range parts {
		raw := chunkFrame(t, "rpc-1", i, len(parts), part, total)
		got, ok := p.assemble(raw)
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
	want := parts[0] + parts[1]
	if string(out) != want {
		t.Errorf("拼出 %q，期望 %q", out, want)
	}
}

// TestAssembleRejectsBrokenSequences 守住每一条校验分支：
// 任何一条不满足都整段丢弃，不能放半截帧上去。
func TestAssembleRejectsBrokenSequences(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T) [][]byte
	}{
		{"index 乱序（先到 1 再到 0）", func(t *testing.T) [][]byte {
			return [][]byte{
				chunkFrame(t, "rpc-1", 1, 2, "b", 2),
				chunkFrame(t, "rpc-1", 0, 2, "a", 2),
			}
		}},
		{"index 重复", func(t *testing.T) [][]byte {
			return [][]byte{
				chunkFrame(t, "rpc-1", 0, 2, "a", 2),
				chunkFrame(t, "rpc-1", 0, 2, "b", 2),
			}
		}},
		{"chunkId 中途变了", func(t *testing.T) [][]byte {
			return [][]byte{
				chunkFrame(t, "rpc-1", 0, 2, "a", 2),
				chunkFrame(t, "rpc-2", 1, 2, "b", 2),
			}
		}},
		{"count 不一致", func(t *testing.T) [][]byte {
			return [][]byte{
				chunkFrame(t, "rpc-1", 0, 3, "a", 2),
				chunkFrame(t, "rpc-1", 1, 2, "b", 2),
			}
		}},
		{"总长与 byteLength 不符", func(t *testing.T) [][]byte {
			// byteLength 声明 10，实际只给 1 个字符
			raw, _ := json.Marshal(map[string]any{
				"type": "rpc_chunk", "chunkId": "rpc-1", "index": 0, "count": 1,
				"byteLength": 10, "data": base64.StdEncoding.EncodeToString([]byte("a")),
			})
			return [][]byte{raw}
		}},
		{"base64 非法", func(t *testing.T) [][]byte {
			raw, _ := json.Marshal(map[string]any{
				"type": "rpc_chunk", "chunkId": "rpc-1", "index": 0, "count": 1,
				"byteLength": 3, "data": "!!!不是 base64!!!",
			})
			return [][]byte{raw}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newProc()
			frames := c.build(t)
			var completed bool
			for _, raw := range frames {
				if _, ok := p.assemble(raw); ok {
					completed = true
				}
			}
			if completed {
				t.Error("残缺或不合法的分片序列不得组装成功")
			}
		})
	}
}

// TestAssembleRejectsGarbage 守住「不是 JSON 的行」直接丢弃。
func TestAssembleRejectsGarbage(t *testing.T) {
	p := newProc()
	if _, ok := p.assemble([]byte("这不是 JSON")); ok {
		t.Error("非法 JSON 不得组装成功")
	}
}

// TestAssembleResetsAfterFailure 守住「丢弃后要能重新开始」：
// 一次坏序列不能把状态卡死，否则之后所有分片都拼不出来。
func TestAssembleResetsAfterFailure(t *testing.T) {
	p := newProc()
	// 先来一个坏序列
	p.assemble(chunkFrame(t, "rpc-1", 0, 2, "a", 2))
	p.assemble(chunkFrame(t, "rpc-2", 1, 2, "b", 2)) // chunkId 变了 → 丢弃
	// 再来一个好序列
	got, ok := p.assemble(chunkFrame(t, "rpc-3", 0, 2, "x", 2))
	if ok {
		t.Fatal("第一片不该完成")
	}
	got, ok = p.assemble(chunkFrame(t, "rpc-3", 1, 2, "y", 2))
	if !ok {
		t.Fatal("坏序列之后应能重新开始组装")
	}
	if string(got) != "xy" {
		t.Errorf("拼出 %q，期望 xy", got)
	}
}
