package service

// 删除会话与工作区文件搜索：这两个动作都直接碰用户磁盘，
// 断言集中在「删得对、删得干净、搜得有边界」——错一处都是静默丢数据或全盘遍历。

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
)

// TestDeleteSession 删除的三条硬保证：文件与 sidecar 一起没、隐藏账跟着清、
// 再删一次不算错（列表与真实磁盘之间有时间差，幂等是常态而不是异常）。
func TestDeleteSession(t *testing.T) {
	app := newTestApp(t)
	root := t.TempDir()

	write := func(rel string) string {
		t.Helper()
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(`{"type":"session"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		return full
	}

	t.Run("文件与同名目录一起删，再删一次幂等", func(t *testing.T) {
		path := write("ws-a/20260101_aaa.jsonl")
		// sidecar：子代理转录等，和会话文件同名（去掉 .jsonl）。
		side := filepath.Join(root, "ws-a", "20260101_aaa")
		if err := os.MkdirAll(filepath.Join(side, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		err := app.DeleteSession(context.Background(), engine.KindPi, path)
		if err != nil {
			t.Fatalf("删除不该失败：%v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("会话文件应已删除：%v", err)
		}
		if _, err := os.Stat(side); !os.IsNotExist(err) {
			t.Fatalf("同名目录应一起删除：%v", err)
		}
		// 幂等：文件已经不在了，再删一次不该报错——列表刷新前再点一次删除是正常操作。
		if err := app.DeleteSession(context.Background(), engine.KindPi, path); err != nil {
			t.Fatalf("重复删除应幂等：%v", err)
		}
	})

	t.Run("隐藏账跟着会话一起清", func(t *testing.T) {
		path := write("ws-b/20260102_bbb.jsonl")
		if err := app.HideTurn(path, "fp#1"); err != nil {
			t.Fatal(err)
		}
		if app.HiddenSetFor(path) == nil {
			t.Fatal("前置条件失败：账应先记上")
		}
		if err := app.DeleteSession(context.Background(), engine.KindPi, path); err != nil {
			t.Fatalf("删除不该失败：%v", err)
		}
		if got := app.HiddenSetFor(path); got != nil {
			t.Fatalf("账不留给不存在的会话：%v", got)
		}
	})

	t.Run("删不掉时给会话段的删除失败码", func(t *testing.T) {
		// 目录当会话文件删：os.Remove 对非空目录必失败，且不是 IsNotExist。
		dir := filepath.Join(root, "ws-c", "20260103_ccc.jsonl")
		if err := os.MkdirAll(filepath.Join(dir, "keep"), 0o755); err != nil {
			t.Fatal(err)
		}
		err := app.DeleteSession(context.Background(), engine.KindPi, dir)
		var appErr *pkg.AppError
		if !errors.As(err, &appErr) || appErr.Code != pkg.CodeSessionDeleteFailed {
			t.Fatalf("应回 6003，得到：%v", err)
		}
	})
}

// TestSearchFiles 搜索的边界：跳过依赖与隐藏目录、大小写不敏感、空词拒绝。
// 限界上限（深度 / 扫描数）在生产里由真实工作区检验，这里只钉住规则本身。
func TestSearchFiles(t *testing.T) {
	app := newTestApp(t)
	root := t.TempDir()
	if err := app.SetWorkspace(root); err != nil {
		t.Fatal(err)
	}
	write := func(rel string) {
		t.Helper()
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("readme.md")
	write("src/main.go")
	write("src/util.go")
	write("docs/deep/nested/notes.md")
	write("node_modules/pkg/index.js")
	write(".git/config")

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"按文件名命中", "main", []string{"src/main.go"}},
		{"大小写不敏感", "MAIN", []string{"src/main.go"}},
		{"多个命中按路径深度排序", "go", []string{"src/main.go", "src/util.go"}},
		{"依赖目录跳过", "index", nil},
		{"隐藏目录跳过", "config", nil},
		{"深层文件也够得着", "notes", []string{"docs/deep/nested/notes.md"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := app.SearchFiles(tc.query, 0)
			if err != nil {
				t.Fatalf("搜索不该失败：%v", err)
			}
			got := make([]string, 0, len(res.Hits))
			for _, h := range res.Hits {
				got = append(got, h.FilePath)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("命中 = %v，期望 %v", got, tc.want)
			}
			for i := range tc.want {
				if filepath.ToSlash(got[i]) != tc.want[i] {
					t.Fatalf("命中[%d] = %s，期望 %s", i, got[i], tc.want[i])
				}
			}
		})
	}

	t.Run("空词拒绝", func(t *testing.T) {
		_, err := app.SearchFiles("   ", 0)
		var appErr *pkg.AppError
		if !errors.As(err, &appErr) || appErr.Code != pkg.CodeBadRequest {
			t.Fatalf("空词应回 7001，得到：%v", err)
		}
	})
}
