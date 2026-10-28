package runtime

// 落盘位置只测会静默出错的两件：目录必须真的建出来、WebView2 缓存必须在数据目录里
// （否则卸载时几十 MB 没人清理）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveCreatesDaemonDirs(t *testing.T) {
	base := t.TempDir()
	t.Setenv("LOCALAPPDATA", base)

	paths, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve 失败：%v", err)
	}
	for _, dir := range []string{paths.DataDir, paths.LogDir, paths.WebViewDir} {
		st, err := os.Stat(dir)
		if err != nil || !st.IsDir() {
			t.Fatalf("目录没建出来：%s（%v）", dir, err)
		}
	}
	// WebView2 缓存必须落在数据目录内部：否则卸载时没人知道要删它。
	if !strings.HasPrefix(paths.WebViewDir, paths.DataDir) {
		t.Fatalf("WebView2 缓存不在数据目录里：%s 不属于 %s", paths.WebViewDir, paths.DataDir)
	}
	if filepath.Dir(paths.SettingsFile) != paths.DataDir {
		t.Fatalf("设置文件应在数据目录下：%s", paths.SettingsFile)
	}
}

// 目录建不出来时必须报错，而不是返回一个半成品让后面的 WriteFile 各自失败。
func TestResolveFailsWhenDataDirUnwritable(t *testing.T) {
	base := t.TempDir()
	// 在 LOCALAPPDATA 的位置放一个普通文件：MkdirAll 一定失败。
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", blocker)

	if _, err := Resolve(); err == nil {
		t.Fatal("数据目录不可写时 Resolve 应该报错")
	}
}
