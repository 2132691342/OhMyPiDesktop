package skill

// 技能层只测两件会静默出错的事：只认 SKILL.md（否则这个接口就是任意文件写入），
// 文件不存在或超限时拒绝写入。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 保存技能正文的边界：只认 SKILL.md、写入内容原样保留、
// 两种拒绝（文件不存在、正文超限）。超限的后果是引擎每次启动都要读它——
// 正文膨胀会让引擎启动变慢，而界面看不出任何异常。
func TestSaveContent(t *testing.T) {
	t.Run("非 SKILL.md 不许改", func(t *testing.T) {
		dir := t.TempDir()
		other := filepath.Join(dir, "notes.md")
		if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := SaveContent(other, "hacked"); err == nil {
			t.Fatal("非 SKILL.md 应当被拒绝")
		}
	})

	t.Run("写入后读回一致", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "SKILL.md")
		original := "---\nname: demo\ndescription: 演示\n---\n\n正文\n"
		if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}
		updated := strings.Replace(original, "正文", "新正文", 1)
		if err := SaveContent(path, updated); err != nil {
			t.Fatal(err)
		}
		got, err := Content(path)
		if err != nil {
			t.Fatal(err)
		}
		if got != updated {
			t.Fatalf("写入后读到的内容不一致：\n%q\n%q", got, updated)
		}
	})

	t.Run("文件不存在与超限都拒绝", func(t *testing.T) {
		dir := t.TempDir()
		if err := SaveContent(filepath.Join(dir, "SKILL.md"), "x"); err == nil {
			t.Error("文件不存在时应当报错，而不是凭空造一个技能")
		}

		path := filepath.Join(dir, "SKILL.md")
		if err := os.WriteFile(path, []byte("---\nname: x\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := SaveContent(path, strings.Repeat("a", contentLimit+1)); err == nil {
			t.Error("超过上限的正文应当被拒绝")
		}
	})
}
