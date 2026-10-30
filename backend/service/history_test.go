package service

// 历史投影与附件装配：两条会静默出错的长链路。
//
//   - 历史投影不并轮时，界面上一次回答会出现两行角色头、两行用量；
//   - 附件超限时悄悄丢几张，模型会说「没看到」而用户不知道图丢了。

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

// 历史投影的合并规则：一次回答里的多条助手消息并成一轮；
// 每一次提问都要断开合并。
//
// 不并的后果是静默的：界面上一次回答会出现两行角色头、两行用量，
// 而直播那一轮也会因为块结构对不上历史而被重复渲染一遍。
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
		turns := ProjectHistory(msgs)
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
		turns := ProjectHistory(msgs)
		if len(turns) != 4 {
			t.Fatalf("两问两答应是四轮，得到 %d", len(turns))
		}
		if turns[1].Blocks[0].Text != "第一答" || turns[3].Blocks[0].Text != "第二答" {
			t.Fatalf("两轮回答被混在一起：%+v", turns)
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
