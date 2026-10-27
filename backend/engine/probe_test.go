package engine

import "testing"

// 引擎判种只有两件事会出错，且都不报错：
//   1. 把 omp 认成 pi（或反过来）→ 界面按错的能力表渲染，控件点了没反应
//   2. 认不出来 → 界面显示「没找到」，用户以为装好了其实不能用
//
// 判据是一串条件分支，拆成了纯函数 matchKindWithHelp，所以这里能直接打真身。

// 各引擎帮助首部的真实形状（按各自的渲染逻辑截取）。
const (
	helpSampleOMP = "omp v18.13.1\n\nUSAGE\n  $ omp [COMMAND]\n\nCOMMANDS\n  acp\n  auth\n"
	helpSamplePi  = "pi - AI coding assistant with read, bash, edit, write tools\n\nUsage:\n  pi [options] [--] [@files...] [messages...]\n\nCommands:\n"
)

// TestMatchKind 是判种决策表：每一行都是一种真实存在的引擎输出组合。
func TestMatchKind(t *testing.T) {
	cases := []struct {
		name    string
		kind    Kind
		version string
		help    string
		want    bool
	}{
		{"omp 标准版本号", KindOMP, "omp/18.13.1", helpSampleOMP, true},
		{"omp 改了 bin 名", KindOMP, "custom/2.0.0", helpSampleOMP, true},
		{"omp 改了 bin 名但帮助是 pi", KindOMP, "custom/2.0.0", helpSamplePi, false},
		{"pi 的版本号不是 omp 形状", KindOMP, "pi/1.4.0", helpSamplePi, false},
		{"裸版本号不是 omp", KindOMP, "1.4.0", helpSamplePi, false},

		{"pi 裸版本号", KindPi, "1.4.0", helpSamplePi, true},
		{"pi 带预发布后缀", KindPi, "1.4.0-beta.2", helpSamplePi, true},
		{"omp 遗留分支打裸版本号", KindPi, "1.4.0", helpSampleOMP, false},
		{"pi 不可能是带前缀的版本", KindPi, "omp/18.13.1", helpSampleOMP, false},
		{"帮助跑不出来就不认", KindPi, "1.4.0", "", false},
	}
	for _, c := range cases {
		if got := matchKindWithHelp(c.kind, c.version, c.help); got != c.want {
			t.Errorf("%s: matchKindWithHelp(%s, %q) = %v，期望 %v",
				c.name, c.kind, c.version, got, c.want)
		}
	}
}

// TestHelpIsOMP 单独守「不靠子串 'omp' 判种」这条纪律。
//
// pi 的帮助里 'omp' 出现十几次（completion、compile 等词），子串匹配必然误判。
func TestHelpIsOMP(t *testing.T) {
	if !helpIsOMP(helpSampleOMP) {
		t.Error("omp 的帮助应被判为 omp")
	}
	if helpIsOMP(helpSamplePi) {
		t.Error("pi 的帮助不该被判为 omp")
	}
	if helpIsOMP("") {
		t.Error("空帮助不该被判为 omp")
	}
}
