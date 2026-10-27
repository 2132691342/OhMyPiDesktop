package engine

import "testing"

// 探测判据的正确性只有两件事会出错，且都不报错：
//   1. 把 omp 认成 pi（或反过来）→ 界面按错的能力表渲染，控件点了没反应
//   2. 认不出来但也不报错 → 界面显示「版本未知」，用户以为装好了其实不能用
//
// 这几条判据是纯字符串函数，直接测；--help 复核的部分用固定样本喂。

func TestIsOMPVersion(t *testing.T) {
	yes := []string{"omp/18.13.1", "omp/1.0.0-beta.2"}
	no := []string{"1.4.0", "pi/1.4.0", "other/2.0.0", "", "omp"}

	for _, v := range yes {
		if !isOMPVersion(v) {
			t.Errorf("%q 应判为 omp 版本格式", v)
		}
	}
	for _, v := range no {
		if isOMPVersion(v) {
			t.Errorf("%q 不该判为 omp 版本格式", v)
		}
	}
}

// omp 与 pi 的真实 --help 首部（按源码 renderRootHelp / pi 的 printHelp 形状截取）。
const (
	helpSampleOMP = "omp v18.13.1\n\nUSAGE\n  $ omp [COMMAND]\n\nCOMMANDS\n  acp\n  auth\n"
	helpSamplePi  = "pi - AI coding assistant with read, bash, edit, write tools\n\nUsage:\n  pi [options] [--] [@files...] [messages...]\n\nCommands:\n"
)

func TestHelpIsOMP(t *testing.T) {
	if !helpIsOMP(helpSampleOMP) {
		t.Error("omp 的帮助应被判为 omp")
	}
	if helpIsOMP(helpSamplePi) {
		t.Error("pi 的帮助不该被判为 omp（pi 的帮助里出现过 omp 字样，不能靠子串 'omp' 判）")
	}
	if helpIsOMP("") {
		t.Error("空帮助不该被判为 omp")
	}
}

// matchKind 的完整判据。这里不打真实进程：把 help 复核换成固定样本，
// 只验证「版本形状 + 帮助形状」这个组合的决策表。
func TestMatchKindDecisionTable(t *testing.T) {
	// 直接调 isOMPVersion 与 helpIsOMP 的组合，等价于 matchKind 的分支。
	// omp 分支
	if !(isOMPVersion("omp/18.13.1")) {
		t.Error("omp/18.13.1 应直接确认为 omp")
	}
	if !(rePrefixed.MatchString("custom/2.0.0") && helpIsOMP(helpSampleOMP)) {
		t.Error("被改过 bin 的 omp（custom/2.0.0 + omp 帮助）应确认为 omp")
	}
	if rePrefixed.MatchString("custom/2.0.0") && helpIsOMP(helpSamplePi) {
		t.Error("custom/2.0.0 + pi 帮助 不该判为 omp")
	}

	// pi 分支：裸 semver 且帮助不是 omp 才是 pi
	if !(reBare.MatchString("1.4.0") && !helpIsOMP(helpSamplePi)) {
		t.Error("1.4.0 + pi 帮助 应确认为 pi")
	}
	if reBare.MatchString("1.4.0") && !helpIsOMP(helpSampleOMP) {
		t.Error("omp 遗留分支打裸版本号时，帮助会暴露它，不该判为 pi")
	}

	// 关键回归：`pi/1.4.0` 这种 name/semver 不能因为形状像就被当成 omp。
	if isOMPVersion("pi/1.4.0") {
		t.Error("pi/1.4.0 不是 omp 的标准格式")
	}
}
