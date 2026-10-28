package repo

// 设置存储的三条不变量：
//   1. 写下去的键读得回来（含进程重启——设置的全部意义就在这往返上）；
//   2. 删掉的键不再出现（空值等于删除）；
//   3. 文件坏了不能让应用起不来。
//
// 其余（"能不能 Unmarshal 一个 map"）不会错，不测。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSetThenReopen(t *testing.T) {
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
}

// 空值等于删除：否则文件里会堆一串 "key": ""，界面上还得再判断一次空。
func TestSetEmptyDeletes(t *testing.T) {
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
}

// 设置文件坏了不能让应用起不来：改名留档 + 用空设置继续，
// 用户至少还能打开应用去改回来。
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
