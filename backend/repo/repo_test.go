package repo

// 设置存储只测静默出错的三条：写后读回（含重启）、空值等于删除、
// 坏文件不让应用起不来。这一层是「用户唯一能自己动手改的磁盘文件」：
// 它坏了，用户看到的现象是「我的偏好全没了」，而界面不该因此起不来。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// 设置的写与读：写后读回、重新打开仍在、All 返回副本、空值等于删除。
func TestStoreReadWrite(t *testing.T) {
	t.Run("写后读回与重新打开", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "settings.json")

		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Set("ui.theme", "dark"); err != nil {
			t.Fatal(err)
		}
		if err := s.SetMany(map[string]string{"app.engine": "pi", "ui.lang": "zh-CN"}); err != nil {
			t.Fatal(err)
		}

		// All 返回副本：调用方改了不该影响存储。
		all, err := s.All()
		if err != nil {
			t.Fatal(err)
		}
		all["ui.theme"] = "被调用方改了"
		if got := s.Get("ui.theme"); got != "dark" {
			t.Errorf("All 必须返回副本，实际读到了 %q", got)
		}

		// 重新打开：模拟下次启动。
		again, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for k, want := range map[string]string{
			"ui.theme": "dark", "app.engine": "pi", "ui.lang": "zh-CN",
		} {
			if got := again.Get(k); got != want {
				t.Errorf("%s = %q，期望 %q", k, got, want)
			}
		}
	})

	t.Run("空值等于删除", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "settings.json")
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Set("ui.lang", "en"); err != nil {
			t.Fatal(err)
		}
		if err := s.Set("ui.lang", ""); err != nil {
			t.Fatal(err)
		}
		if got := s.Get("ui.lang"); got != "" {
			t.Errorf("空值应删除，实际 = %q", got)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		if _, ok := m["ui.lang"]; ok {
			t.Error("删除的键不应留在文件里")
		}
	})
}

// 坏文件不能让应用起不来：改名留档 + 用空设置继续，用户至少还能打开去改回来。
func TestOpenBrokenFileDoesNotFail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte("{这不是 JSON"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("坏文件不应让 Open 失败：%v", err)
	}
	if got := s.Get("ui.theme"); got != "" {
		t.Errorf("坏文件应退回空设置，实际读到了 %q", got)
	}
	if _, err := os.Stat(path + ".broken"); err != nil {
		t.Errorf("坏文件应改名留档：%v", err)
	}
}
