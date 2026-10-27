package pi

import (
	"testing"

	"OhMyPiDesktop/backend/engine"
)

// CapsFor 是「进程还没起来时」的答案，Caps() 是「真的在用这个引擎时」的答案。
//
// 两份定义一旦漂移，探测期与真正跑起来的界面会自相矛盾。这里用相等性把两份钉住：
// 曾经 pi 的 CapsFor 说能清空排队、Caps() 却说不能，界面于是先显示按钮再报不支持。
func TestCapsMatchProbeAnswer(t *testing.T) {
	if got, want := engine.CapsFor(engine.KindPi), Caps(); got != want {
		t.Errorf("pi 的能力表漂移了：\n  CapsFor = %+v\n  Caps    = %+v", got, want)
	}
}
