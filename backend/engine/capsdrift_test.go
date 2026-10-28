package engine_test

// 探测期（engine.CapsFor 硬编码）与运行期（各 Dialect.Caps）的能力表必须一致：
// 两份定义分处不同包（engine 不能 import engine/omp），用这条外部测试钉住，
// 否则界面会在启动前后表现不一致——按钮先出现、点下去报不支持。

import (
	"testing"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/engine/omp"
	"OhMyPiDesktop/backend/engine/pi"
)

func TestCapsMatchProbeAnswer(t *testing.T) {
	cases := []struct {
		kind engine.Kind
		runs engine.Caps
	}{
		{engine.KindOMP, omp.Caps()},
		{engine.KindPi, pi.Caps()},
	}
	for _, c := range cases {
		if got := engine.CapsFor(c.kind); got != c.runs {
			t.Errorf("%s 的能力表漂移了：\n  探测期 CapsFor = %+v\n  运行期 Caps    = %+v", c.kind, got, c.runs)
		}
	}
}
