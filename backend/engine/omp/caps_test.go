package omp

import (
	"testing"

	"OhMyPiDesktop/backend/engine"
)

// CapsFor 是「进程还没起来时」的答案，Caps() 是「真的在用这个引擎时」的答案。
//
// 两份定义一旦漂移，探测期与真正跑起来的界面会自相矛盾：比如探测期说能移除
// 排队条目、跑起来又不行。能力表在两个包里各写一份是分层逼出来的（engine 不能
// import omp，否则反向依赖），所以用这条测试把两份钉在一起。
func TestCapsMatchProbeAnswer(t *testing.T) {
	if got, want := engine.CapsFor(engine.KindOMP), Caps(); got != want {
		t.Errorf("omp 的能力表漂移了：\n  CapsFor = %+v\n  Caps    = %+v", got, want)
	}
}
