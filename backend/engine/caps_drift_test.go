package engine_test

import (
	"testing"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/engine/omp"
	"OhMyPiDesktop/backend/engine/pi"
)

// 探测期的能力表必须与运行期完全一致。
//
// 引擎探测要发生在进程拉起之前，所以 engine.CapsFor 只能按引擎种类硬编码一份；
// 而真正跑起来时用的是各引擎 Dialect 自己的 Caps()。两份定义一旦漂移，
// 界面就会在「启动前」和「启动后」表现不一致：按钮先出现，点下去报不支持。
//
// 这两份定义必须分处两个包（engine 不能 import engine/omp，否则反向依赖），
// 所以用这条测试把它们钉在一起。
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
