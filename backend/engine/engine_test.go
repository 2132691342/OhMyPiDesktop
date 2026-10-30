package engine

// 引擎抽象层只测会悄无声息出错的地方：判种、渠道判定、能力表、分片重组。
//
// 同一主题合成一条表驱动：五条散落的用例要改五处、跑起来也慢；一条只改一处。
// 不测「能不能解一个 JSON」「函数有没有被调用」这类不会错的东西。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fakeDialect 只用于回答能力表与握手；不启动进程。
type fakeDialect struct{ caps Caps }

func (f fakeDialect) Kind() Kind                             { return KindPi }
func (f fakeDialect) Caps() Caps                             { return f.caps }
func (f fakeDialect) Args(string, []string) []string         { return nil }
func (f fakeDialect) Handshake(context.Context, *Proc) error { return nil }
func (f fakeDialect) Consume(*Engine, Frame) bool            { return false }

// ---------------- 判种 / 渠道 / 能力 ----------------

// 判种是一串条件分支，只在 matchKind 里就没法直接测（每次验证都要真起一个进程）。
// 判错的代价是把两个引擎认反：界面对着一个引擎发另一个引擎的命令，引擎静默不认。
func TestMatchKind(t *testing.T) {
	const helpOMP = "omp v18.13.1\n\nUSAGE\n  $ omp [COMMAND]\n\nCOMMANDS\n  acp\n  auth\n"
	const helpPi = "pi - AI coding assistant with read, bash, edit, write tools\n\nUsage:\n  pi [options] [--] [@files...]\n"
	cases := []struct {
		name, kind, version, help string
		want                      bool
	}{
		{"omp 标准版本号", string(KindOMP), "omp/18.13.1", helpOMP, true},
		{"改了 bin 名但帮助是 pi", string(KindOMP), "custom/2.0.0", helpPi, false},
		{"pi 的版本号不是 omp 形状", string(KindOMP), "pi/1.4.0", helpPi, false},
		{"pi 裸版本号", string(KindPi), "1.4.0", helpPi, true},
		{"pi 带预发布后缀", string(KindPi), "1.4.0-beta.2", helpPi, true},
		{"omp 裸版本号不算 pi", string(KindPi), "1.4.0", helpOMP, false},
		{"pi 不接受带前缀版本", string(KindPi), "omp/18.13.1", helpOMP, false},
		{"帮助跑不出来不认", string(KindPi), "1.4.0", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchKindWithHelp(Kind(c.kind), c.version, c.help); got != c.want {
				t.Fatalf("matchKindWithHelp = %v，期望 %v", got, c.want)
			}
		})
	}
}

// 渠道判错的后果很具体：判成 npm 会给出一条错误的卸载命令，
// 判不出来才会只给手动路径。所以每一档落点都要认准。
func TestDetectChannel(t *testing.T) {
	home := `C:\Users\me`
	local := `C:\Users\me\AppData\Local`
	cases := []struct {
		name, kind, exe string
		want            Channel
	}{
		{"npm 全局", string(KindPi), `C:\nvm\node_global\node_modules\@earendil-works\pi-coding-agent\dist\cli.js`, ChannelNPM},
		{"pnpm 全局", string(KindPi), `C:\Users\me\AppData\Local\pnpm\node_modules\.pnpm\@earendil-works+pi\dist\cli.js`, ChannelPNPM},
		{"bun 全局", string(KindPi), `C:\Users\me\.bun\install\global\node_modules\@earendil-works\pi-coding-agent\dist\cli.js`, ChannelBun},
		{"omp 官方安装器", string(KindOMP), `C:\Users\me\AppData\Local\omp\omp.exe`, ChannelOMPInstaller},
		{"别处的 omp 不是安装器", string(KindOMP), `D:\tools\omp\omp.exe`, ChannelUnknown},
		{"空路径", string(KindOMP), "", ChannelUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DetectChannel(Kind(c.kind), c.exe, home, local); got != c.want {
				t.Fatalf("DetectChannel = %q，期望 %q", got, c.want)
			}
		})
	}

	// 同级 node_modules 的 shim 只看路径段会判成未知，必须靠同级目录识别。
	// 这是 npm / pnpm 全局安装最常见的形态，判错就等于没有卸载按钮。
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@earendil-works", "pi-coding-agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "pi.cmd")
	if got := DetectChannel(KindPi, exe, "", ""); got != ChannelNPM {
		t.Fatalf("同级 node_modules 的 shim 应判为 npm，得到 %q", got)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", ".pnpm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DetectChannel(KindPi, exe, "", ""); got != ChannelPNPM {
		t.Fatalf("带 .pnpm 存储的应判为 pnpm，得到 %q", got)
	}
}

// Caps 是按钮是否出现的唯一依据：改错一个位，就等于给用户开一个点不动的入口。
// 每一项都钉死，改能力时必须同时改这张表。
func TestCapsMatchCommands(t *testing.T) {
	want := map[Kind]Caps{
		KindOMP: {
			Subagents: true, Goals: true, Todos: true, Login: true, Fragments: true,
			QueueEdit: true, LiveSteerCount: true, SkillToggle: true,
			Extensions: true, ExportHTML: true, SlashCommands: true,
		},
		KindPi: {Extensions: true, ExportHTML: true, QueueClear: true},
	}
	for kind, w := range want {
		if got := CapsFor(kind); got != w {
			t.Errorf("%s 的能力表 = %+v，期望 %+v", kind, got, w)
		}
	}
}

// 「能不能卸」与「有没有卸载命令」必须一致：两者不一致时，界面要么给一个
// 点了报错的按钮，要么把一条能用的命令藏起来。
func TestUninstallCommandMatchesChannel(t *testing.T) {
	channels := []Channel{ChannelNPM, ChannelPNPM, ChannelBun, ChannelOMPInstaller, ChannelPiInstaller, ChannelUnknown}
	for _, kind := range AllKinds() {
		for _, ch := range channels {
			hasCmd := len(UninstallCommand(kind, ch)) > 0
			if hasCmd != CanUninstall(ch) {
				t.Fatalf("%s/%s: CanUninstall=%v 但命令存在=%v", kind, ch, CanUninstall(ch), hasCmd)
			}
		}
	}
}

// ---------------- 分片重组 ----------------

// chunkFrame 造一片分片帧。
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

// newTestProc 造一个只用于分片重组的进程壳。
func newTestProc() *Proc { return &Proc{opts: Options{Dialect: fakeDialect{}}} }

// 分片重组：乱序 / 重复 / 换 id / 长度不符 / base64 非法整段丢弃，
// 坏序列之后要能重新开始。
//
// 丢弃必须是整段的：放半截帧上去，解析器会把后面的命令也一起吞掉，
// 表现出来是「所有命令都超时」。
func TestAssembleChunks(t *testing.T) {
	t.Run("正常拼合", func(t *testing.T) {
		p := newTestProc()
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
				t.Fatal("最后一片应组装完成")
			}
			out = got
		}
		if want := parts[0] + parts[1]; string(out) != want {
			t.Errorf("拼出 %q，期望 %q", out, want)
		}
	})

	bad := []struct {
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
	for _, c := range bad {
		t.Run("残缺序列整段丢弃："+c.name, func(t *testing.T) {
			p := newTestProc()
			for _, raw := range c.build(t) {
				if _, ok := p.assemble(raw); ok {
					t.Error("残缺或不合法的分片序列不得组装成功")
				}
			}
		})
	}

	t.Run("丢弃后能重新开始", func(t *testing.T) {
		p := newTestProc()
		p.assemble(chunkFrame(t, "rpc-1", 0, 2, "a", 2))
		p.assemble(chunkFrame(t, "rpc-2", 1, 2, "b", 2))
		if _, ok := p.assemble(chunkFrame(t, "rpc-3", 0, 2, "x", 2)); ok {
			t.Fatal("第一片不该完成")
		}
		got, ok := p.assemble(chunkFrame(t, "rpc-3", 1, 2, "y", 2))
		if !ok || string(got) != "xy" {
			t.Fatalf("坏序列之后应可重新开始，得到 %q ok=%v", string(got), ok)
		}
	})
}
