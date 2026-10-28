package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 保存技能正文的可靠性在于两件事：只认 SKILL.md、写坏了不留下半个文件。
// 后者由 WriteFileAtomic 保证，这里测的是「拒绝改别人的文件」。
func TestSaveContentRejectsOtherFiles(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveContent(other, "hacked"); err == nil {
		t.Fatal("非 SKILL.md 应当被拒绝")
	}
}

func TestSaveContentWritesAndKeepsFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	original := "---\nname: demo\ndescription: 演示\n---\n\n正文\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	// 只改正文，frontmatter 由调用方原样带回来——这里验证写进去的就是给的原文。
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
}

func TestSaveContentMissingFile(t *testing.T) {
	dir := t.TempDir()
	if err := SaveContent(filepath.Join(dir, "SKILL.md"), "x"); err == nil {
		t.Fatal("文件不存在时应当报错，而不是凭空造一个技能")
	}
}

// 超大正文要在落盘之前拦住：写进去之后引擎每次启动都会读它。
func TestSaveContentTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: x\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("a", contentLimit+1)
	if err := SaveContent(path, big); err == nil {
		t.Fatal("超过上限的正文应当被拒绝")
	}
}
