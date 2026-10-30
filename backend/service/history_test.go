package service

// 历史投影、附件装配与隐藏账本：三条会静默出错的长链路。投影不并轮则一次回答出两行角色头两行用量；
// 附件超限悄悄丢几张则模型说「没看到」而用户不知道图丢了；隐藏账本串会或丢账则藏过的轮次自己回来或串到别人头上。

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"OhMyPiDesktop/backend/domain"
)

// ---------------- 历史投影 ----------------

// rawMessages 把 JSON 文本转成投影层的入参形态。
func rawMessages(t *testing.T, items ...string) []json.RawMessage {
	t.Helper()
	out := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		if !json.Valid([]byte(item)) {
			t.Fatalf("测试数据不是合法 JSON：%s", item)
		}
		out = append(out, json.RawMessage(item))
	}
	return out
}

// 历史投影的合并规则：一次回答里的多条助手消息并成一轮，每一次提问都要断开合并。
// 不并的后果是静默的：一次回答出两行角色头、两行用量，直播那一轮也会因块结构对不上历史被重复渲染。
func TestProjectHistoryMergesAssistantMessages(t *testing.T) {
	t.Run("同一次回答的多条助手消息并成一轮", func(t *testing.T) {
		msgs := rawMessages(t,
			`{"role":"user","content":[{"type":"text","text":"读一下 go.mod"}],"timestamp":1000}`,
			`{"role":"assistant","content":[{"type":"toolCall","id":"c1","name":"read"}],`+
				`"usage":{"input":100,"output":5},"timestamp":4000}`,
			`{"role":"toolResult","toolCallId":"c1","toolName":"read","content":[{"type":"text","text":"module X"}],"timestamp":4100}`,
			`{"role":"assistant","content":[{"type":"text","text":"读完了"}],`+
				`"usage":{"input":150,"output":9},"timestamp":5000}`,
		)
		turns := ProjectHistory(msgs, nil)
		if len(turns) != 2 {
			t.Fatalf("一次提问应投影成两轮（user + assistant），得到 %d 轮", len(turns))
		}
		if turns[1].Role != "assistant" || len(turns[1].Blocks) != 2 {
			t.Fatalf("助手轮应含工具块与正文块，得到 %+v", turns[1].Blocks)
		}
		if turns[1].Blocks[0].Output != "module X" {
			t.Fatalf("工具结果没回填到工具块：%q", turns[1].Blocks[0].Output)
		}
		if turns[1].Blocks[1].Text != "读完了" {
			t.Fatalf("正文块丢失：%+v", turns[1].Blocks[1])
		}
		// 用量取最后一次（引擎报的是累计值），耗时为「提问到答案」的毫秒差。
		if u := turns[1].Usage; u == nil || u.InputTokens != 150 || u.OutputTokens != 9 {
			t.Fatalf("用量应取最后一条助手消息：%+v", u)
		} else if u.ElapsedMS != 4000 {
			t.Fatalf("耗时应是末条时间戳减提问时刻：%d", u.ElapsedMS)
		}
	})

	t.Run("新提问要断开合并", func(t *testing.T) {
		msgs := rawMessages(t,
			`{"role":"user","content":[{"type":"text","text":"第一问"}]}`,
			`{"role":"assistant","content":[{"type":"text","text":"第一答"}]}`,
			`{"role":"user","content":[{"type":"text","text":"第二问"}]}`,
			`{"role":"assistant","content":[{"type":"text","text":"第二答"}]}`,
		)
		turns := ProjectHistory(msgs, nil)
		if len(turns) != 4 {
			t.Fatalf("两问两答应是四轮，得到 %d", len(turns))
		}
		if turns[1].Blocks[0].Text != "第一答" || turns[3].Blocks[0].Text != "第二答" {
			t.Fatalf("两轮回答被混在一起：%+v", turns)
		}
	})
}

// 历史投影的隐藏过滤：「删除一轮消息」是界面层的隐藏，投影按账本摘轮。
//
// 错了是静默的：藏错一轮会把没删的对话显示成删过，或者反过来删不掉。
func TestProjectHistoryHidden(t *testing.T) {
	// 两问两答，其中第一问与第二问内容相同——
	// 用户确实会把同一句话发两遍，隐藏必须能区分「哪一份」。
	msgs := rawMessages(t,
		`{"role":"user","content":[{"type":"text","text":"继续"}]}`,
		`{"role":"assistant","content":[{"type":"toolCall","id":"c1","name":"read"}]}`,
		`{"role":"toolResult","toolCallId":"c1","toolName":"read","content":[{"type":"text","text":"ok"}]}`,
		`{"role":"assistant","content":[{"type":"text","text":"好了"}]}`,
		`{"role":"user","content":[{"type":"text","text":"继续"}]}`,
		`{"role":"assistant","content":[{"type":"toolCall","id":"c2","name":"read"}]}`,
		`{"role":"toolResult","toolCallId":"c2","toolName":"read","content":[{"type":"text","text":"ok2"}]}`,
		`{"role":"assistant","content":[{"type":"text","text":"又好了"}]}`,
	)
	all := ProjectHistory(msgs, nil)
	if len(all) != 4 {
		t.Fatalf("应投影成四轮（两组「提问 + 合并后的助手轮」），得到 %d", len(all))
	}
	for i, turn := range all {
		if turn.Hid == "" {
			t.Fatalf("第 %d 轮没有 Hid：界面将无法指认它", i)
		}
	}

	t.Run("hid 按内容与出现序号区分同文轮", func(t *testing.T) {
		// 两条「继续」内容相同，hid 必须不同；内容不同的轮更不能撞。
		if all[0].Hid == all[2].Hid {
			t.Fatalf("同文两轮的 hid 应带不同序号：%s", all[0].Hid)
		}
		if all[0].Hid == all[1].Hid {
			t.Fatalf("内容不同的轮 hid 不应相同")
		}
	})

	t.Run("隐藏一轮后投影不再返回它", func(t *testing.T) {
		hidden := map[string]bool{all[1].Hid: true}
		turns := ProjectHistory(msgs, hidden)
		if len(turns) != 3 {
			t.Fatalf("藏一轮后应剩三轮，得到 %d", len(turns))
		}
		for _, turn := range turns {
			if turn.Hid == all[1].Hid {
				t.Fatal("被隐藏的轮还在返回里")
			}
		}
	})

	t.Run("工具结果跟着整轮一起消失", func(t *testing.T) {
		// 被藏的助手轮带着工具块与回填结果；过滤发生在投影之后，
		// 不能出现「轮被摘了、结果变成孤儿工具轮」。
		hidden := map[string]bool{all[1].Hid: true}
		turns := ProjectHistory(msgs, hidden)
		for _, turn := range turns {
			if turn.Role == "tool" {
				t.Fatalf("工具结果应回填进所属轮而不是单独成轮：%+v", turn.Blocks)
			}
		}
	})

	t.Run("同文两轮只藏指定的那一份", func(t *testing.T) {
		hidden := map[string]bool{all[2].Hid: true}
		turns := ProjectHistory(msgs, hidden)
		if len(turns) != 3 {
			t.Fatalf("藏第二份「继续」后应剩三轮，得到 %d", len(turns))
		}
		if turns[0].Hid != all[0].Hid {
			t.Fatal("第一份「继续」不该被牵连")
		}
	})

	t.Run("空账本等于不过滤", func(t *testing.T) {
		turns := ProjectHistory(msgs, map[string]bool{})
		if len(turns) != len(all) {
			t.Fatalf("空账本不应摘掉任何轮：得到 %d", len(turns))
		}
	})
}

// ---------------- 附件装配 ----------------

// tinyPNG 返回一段合法的 1x1 PNG 字节，供图片附件测试当素材。
func tinyPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // 签名
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
		0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
		0x42, 0x60, 0x82,
	}
}

// 附件装配的边界：超限要整条拒绝而不是悄悄丢几张；文件清单要附在正文之后。
func TestBuildPrompt(t *testing.T) {
	t.Run("图片超限整条拒绝", func(t *testing.T) {
		img := tinyPNG()
		atts := make([]domain.ChatAttachmentREQ, 0, 10)
		for i := 0; i < 10; i++ {
			atts = append(atts, domain.ChatAttachmentREQ{
				Kind:        domain.AttachmentKindImage,
				ImageBase64: base64.StdEncoding.EncodeToString(img),
				ImageMIME:   "image/png",
			})
		}
		if _, err := buildPrompt("看图", atts); err == nil {
			t.Fatal("超过 8 张图片应被拒绝")
		}
	})

	t.Run("文件清单附在正文换行之后", func(t *testing.T) {
		atts := []domain.ChatAttachmentREQ{
			{Kind: domain.AttachmentKindFile, FilePath: "/repo/a.go"},
			{Kind: domain.AttachmentKindFile, FilePath: "/repo/b.go"},
		}
		payload, err := buildPrompt("改这两个文件", atts)
		if err != nil {
			t.Fatal(err)
		}
		if payload.Message != "改这两个文件\n\n这次要看这些文件：\n- /repo/a.go\n- /repo/b.go" {
			t.Fatalf("正文 + 文件清单拼接错误：%q", payload.Message)
		}
	})

	t.Run("正文与附件全空要拦下", func(t *testing.T) {
		if _, err := buildPrompt("   ", nil); err == nil {
			t.Fatal("正文与附件全空应被拒绝")
		}
		if _, err := buildPrompt("说点什么", nil); err != nil {
			t.Fatalf("有正文无附件不应报错：%v", err)
		}
	})

	t.Run("非图片字节且不带 mime 要拒绝", func(t *testing.T) {
		if _, err := decodeImage(domain.ChatAttachmentREQ{
			Kind:        domain.AttachmentKindImage,
			ImageBase64: base64.StdEncoding.EncodeToString([]byte("not an image")),
		}); err == nil {
			t.Fatal("非图片内容且未声明 mime 时应被拒绝")
		}
	})
}

// ---------------- 隐藏账本 ----------------

// 账本的两条静默失败路径：跨会话串账 / 重启丢账。
// 撤销没记过的账必须不报错（撤销按钮要能随便点）。
func TestHiddenTurnsLedger(t *testing.T) {
	t.Run("按会话记账与撤销，Windows 路径大小写折叠", func(t *testing.T) {
		var h HiddenTurns
		_ = h.Hide(`D:\sessions\a.jsonl`, "fp#1")
		_ = h.Hide(`D:\sessions\b.jsonl`, "fp#2")
		// Windows 路径大小写不敏感：换一种写法必须查到同一本账。
		if got := h.SetFor(`d:\SESSIONS\A.jsonl`); len(got) != 1 || !got["fp#1"] {
			t.Fatalf("a 的账丢了或串了：%v", got)
		}
		if got := h.SetFor(`D:\sessions\b.jsonl`); len(got) != 1 || !got["fp#2"] {
			t.Fatalf("b 的账丢了或串了：%v", got)
		}
		removed, err := h.Unhide(`d:\sessions\A.jsonl`, "fp#1")
		if err != nil || !removed {
			t.Fatalf("记过的账应撤得掉：removed=%v err=%v", removed, err)
		}
		if removed, err = h.Unhide(`D:\sessions\a.jsonl`, "fp#1"); err != nil || removed {
			t.Fatalf("撤没有的应返回 false 且不报错：removed=%v err=%v", removed, err)
		}
		// 会话删掉时整本账清掉：账不留给不存在的会话。
		h.Forget(`D:\SESSIONS\B.jsonl`)
		if got := h.SetFor(`D:\sessions\b.jsonl`); got != nil {
			t.Fatalf("Forget 之后不该还有账：%v", got)
		}
	})

	t.Run("序列化往返不走样，空账是空串", func(t *testing.T) {
		var h HiddenTurns
		raw, err := h.Marshal()
		if err != nil || raw != "" {
			t.Fatalf("空账应序列化成空串（Store 视为删除该键）：raw=%q err=%v", raw, err)
		}
		_ = h.Hide(`D:\s\a.jsonl`, "fp#1")
		_ = h.Hide(`D:\s\a.jsonl`, "fp#2")
		_ = h.Hide(`D:\s\b.jsonl`, "fp#3")
		raw, err = h.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		var again HiddenTurns
		if err := again.Unmarshal(raw); err != nil {
			t.Fatal(err)
		}
		if got := again.SetFor(`D:\s\a.jsonl`); len(got) != 2 || !got["fp#1"] || !got["fp#2"] {
			t.Fatalf("a 的账往返后丢了：%v", got)
		}
		if got := again.SetFor(`D:\s\b.jsonl`); len(got) != 1 || !got["fp#3"] {
			t.Fatalf("b 的账往返后丢了：%v", got)
		}
	})
}

// eventName 从一帧原始字节里取出事件名。帧的形状是 {seq, event, data, sent_at}，这里只关心 event。
func eventName(raw []byte) string {
	var f struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return ""
	}
	return f.Event
}
