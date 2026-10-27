package engine

import (
	"context"
	"testing"
)

// 能力表与命令表必须一一对得上。
//
// 这类错误不会编译失败、不会报错：发出去的一条引擎不认识的命令，
// 引擎只回 success:false 或者干脆静默丢弃，界面表现为「点了没反应」。
// 所以这里把「哪条命令属于谁」钉死，改 Caps 或改命令名时必须一起改。

// 引擎命令表里的真实条目（从 oh-my-pi / pi 的协议定义逐字抄来）。
// 只列我们真的会发的那些——没发的命令不需要在这里。
var engineCommands = map[Kind]map[string]bool{
	KindOMP: {
		"prompt": true, "steer": true, "follow_up": true, "abort": true,
		"new_session": true, "switch_session": true, "fork": true,
		"get_state": true, "get_session_stats": true, "get_messages": true,
		"set_model": true, "cycle_model": true, "get_available_models": true,
		"set_thinking_level": true, "get_available_thinking_levels": true,
		"compact": true, "set_auto_compaction": true, "set_session_name": true,
		"negotiate_protocol": true, "available_commands_update": true,
		// 子代理
		"set_subagent_subscription": true, "get_subagents": true,
		"get_subagent_messages": true, "cancel_subagent": true, "steer_subagent": true,
		// 队列：只有逐条移除，没有 clear_queue
		"remove_queued_message": true, "promote_queued_message": true,
		// 登录
		"get_login_providers": true, "login": true,
	},
	KindPi: {
		"prompt": true, "steer": true, "follow_up": true, "abort": true,
		"new_session": true, "switch_session": true, "fork": true,
		"get_state": true, "get_session_stats": true, "get_messages": true,
		"set_model": true, "cycle_model": true, "get_available_models": true,
		"set_thinking_level": true, "get_available_thinking_levels": true,
		"compact": true, "set_auto_compaction": true, "set_session_name": true,
		"get_commands": true,
		// 队列：只有一次性清空，没有逐条移除
		"clear_queue": true,
	},
}

// 每个能力开关都必须对应引擎命令表里的真实命令。
func TestCapsOnlyClaimRealCommands(t *testing.T) {
	// 每条 Caps 字段 → 它依赖的命令。空表示不依赖 RPC。
	depends := map[string]string{
		"QueueEdit":  "remove_queued_message",
		"QueueClear": "clear_queue",
	}

	for kind, cmds := range engineCommands {
		caps := CapsFor(kind)
		for field, cmd := range depends {
			on := capOn(caps, field)
			_, exists := cmds[cmd]
			if on && !exists {
				t.Errorf("%s: Caps.%s 为真，但命令表里没有 %s", kind, field, cmd)
			}
			if !on && exists {
				t.Errorf("%s: Caps.%s 为假，但命令表里明明有 %s", kind, field, cmd)
			}
		}
	}
}

// ClearQueue 在 omp 上必须拒绝，在 pi 上必须放行到发命令这一步。
func TestClearQueueRespectsCaps(t *testing.T) {
	omp := &Engine{Kind: KindOMP, dialect: fakeDialect{caps: CapsFor(KindOMP)}}
	if err := omp.ClearQueue(nil); err == nil {
		t.Fatal("omp 没有 clear_queue，ClearQueue 必须返回不支持")
	}
	pi := &Engine{Kind: KindPi, dialect: fakeDialect{caps: CapsFor(KindPi)}}
	if !pi.Caps().QueueClear {
		t.Fatal("pi 有 clear_queue，Caps.QueueClear 必须为真")
	}
}

// RemoveQueued 在 pi 上必须拒绝。
func TestRemoveQueuedRespectsCaps(t *testing.T) {
	pi := &Engine{Kind: KindPi, dialect: fakeDialect{caps: CapsFor(KindPi)}}
	if _, err := pi.RemoveQueued(nil, "m", "steering"); err == nil {
		t.Fatal("pi 没有 remove_queued_message，RemoveQueued 必须返回不支持")
	}
}

func capOn(c Caps, field string) bool {
	switch field {
	case "QueueEdit":
		return c.QueueEdit
	case "QueueClear":
		return c.QueueClear
	}
	return false
}

// fakeDialect 只用于回答能力表；不启动进程。
type fakeDialect struct{ caps Caps }

func (f fakeDialect) Kind() Kind                             { return KindPi }
func (f fakeDialect) Caps() Caps                             { return f.caps }
func (f fakeDialect) Args(string, []string) []string         { return nil }
func (f fakeDialect) Handshake(context.Context, *Proc) error { return nil }
func (f fakeDialect) Consume(*Engine, Frame) bool            { return false }
