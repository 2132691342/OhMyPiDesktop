package service

// 附件装配是「静默丢东西」的高危区：文件清单拼错、图片错进正文、base64 换行没清都不会抛错，
// 所以钉住这几种失败模式与数量/模型能力上限。

import (
	"encoding/base64"
	"strings"
	"testing"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
)

// 1x1 的 PNG，够用来测解码与类型识别。
const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestBuildPromptFilesBecomePathList(t *testing.T) {
	payload, err := buildPrompt("改一下这个", []domain.ChatAttachmentREQ{
		{Kind: domain.AttachmentKindFile, FilePath: `src\utils\md.ts`},
		{Kind: domain.AttachmentKindFile, FilePath: "README.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 正文必须留着：用户说的话不能因为带附件就丢。
	if !strings.HasPrefix(payload.Message, "改一下这个") {
		t.Fatalf("正文丢了：%q", payload.Message)
	}
	// 路径统一成斜杠：引擎那边按 posix 路径匹配，反斜杠会让它找不到文件。
	if !strings.Contains(payload.Message, "- src/utils/md.ts") {
		t.Fatalf("文件清单没拼进正文：%q", payload.Message)
	}
	if !strings.Contains(payload.Message, "- README.md") {
		t.Fatalf("第二个文件漏了：%q", payload.Message)
	}
	// 文件走正文，不能同时进 images——那会让引擎收到一个空图片。
	if len(payload.Images) != 0 {
		t.Fatalf("文件引用不该产生图片：%d", len(payload.Images))
	}
}

func TestBuildPromptImageGoesToImagesField(t *testing.T) {
	payload, err := buildPrompt("看图", []domain.ChatAttachmentREQ{
		{Kind: domain.AttachmentKindImage, ImageBase64: tinyPNG},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Images) != 1 {
		t.Fatalf("图片必须进 images 字段，实际 %d", len(payload.Images))
	}
	img := payload.Images[0]
	if img.Type != "image" {
		t.Fatalf("type 必须是 image：%q", img.Type)
	}
	if img.MimeType != "image/png" {
		t.Fatalf("类型识别错了：%q", img.MimeType)
	}
	if _, err := base64.StdEncoding.DecodeString(img.Data); err != nil {
		t.Fatalf("data 不是合法 base64：%v", err)
	}
	// 图片不该污染正文。
	if strings.Contains(payload.Message, tinyPNG) {
		t.Fatal("base64 被塞进正文了——模型看不见，还会吃掉几万 token")
	}
}

// data URL 与带换行的 base64 都要能吃下：前者是浏览器粘贴的原始形态，
// 后者是某些工具导出的形态。
func TestDecodeImageAcceptsDataURLAndWhitespace(t *testing.T) {
	cases := []struct {
		what  string
		input string
	}{
		{"data URL", "data:image/png;base64," + tinyPNG},
		{"带换行", tinyPNG[:20] + "\n" + tinyPNG[20:40] + "\r\n" + tinyPNG[40:]},
	}
	for _, tc := range cases {
		t.Run(tc.what, func(t *testing.T) {
			img, err := decodeImage(domain.ChatAttachmentREQ{
				Kind: domain.AttachmentKindImage, ImageBase64: tc.input,
			})
			if err != nil {
				t.Fatalf("%s 解码失败：%v", tc.what, err)
			}
			if img.MimeType != "image/png" {
				t.Fatalf("%s 类型识别错了：%q", tc.what, img.MimeType)
			}
		})
	}
}

// 不是图片的东西必须被拒绝，而且要拒绝在发送之前。
func TestDecodeImageRejectsNonImage(t *testing.T) {
	notImage := base64.StdEncoding.EncodeToString([]byte("this is plain text, not an image"))
	if _, err := decodeImage(domain.ChatAttachmentREQ{
		Kind: domain.AttachmentKindImage, ImageBase64: notImage,
	}); err == nil {
		t.Fatal("非图片内容应该报错")
	}
	if _, err := decodeImage(domain.ChatAttachmentREQ{
		Kind: domain.AttachmentKindImage, ImageBase64: "@@@not-base64@@@",
	}); err == nil {
		t.Fatal("非法 base64 应该报错")
	}
}

// 空发送要拦住；只有附件没有正文是合法的。
func TestBuildPromptEmptyRules(t *testing.T) {
	if _, err := buildPrompt("   ", nil); err == nil {
		t.Fatal("纯空白应该报错")
	}
	if _, err := buildPrompt("", []domain.ChatAttachmentREQ{
		{Kind: domain.AttachmentKindImage, ImageBase64: tinyPNG},
	}); err != nil {
		t.Fatalf("只有图片没有正文应该允许：%v", err)
	}
	if _, err := buildPrompt("", []domain.ChatAttachmentREQ{
		{Kind: domain.AttachmentKindFile, FilePath: "a.ts"},
	}); err != nil {
		t.Fatalf("只有文件引用应该允许：%v", err)
	}
}

// 数量上限要生效，否则用户能一次贴几十张图上去。
func TestBuildPromptEnforcesLimits(t *testing.T) {
	manyImages := make([]domain.ChatAttachmentREQ, 0, maxImagesPerMessage+1)
	for i := 0; i <= maxImagesPerMessage; i++ {
		manyImages = append(manyImages, domain.ChatAttachmentREQ{
			Kind: domain.AttachmentKindImage, ImageBase64: tinyPNG,
		})
	}
	if _, err := buildPrompt("x", manyImages); err == nil {
		t.Fatalf("超过 %d 张图应该被拒", maxImagesPerMessage)
	}

	manyFiles := make([]domain.ChatAttachmentREQ, 0, maxFilesPerMessage+1)
	for i := 0; i <= maxFilesPerMessage; i++ {
		manyFiles = append(manyFiles, domain.ChatAttachmentREQ{
			Kind: domain.AttachmentKindFile, FilePath: "f.txt",
		})
	}
	if _, err := buildPrompt("x", manyFiles); err == nil {
		t.Fatalf("超过 %d 个文件应该被拒", maxFilesPerMessage)
	}
}

// 模型不收图时必须拦在发送前。判错方向的代价：
// 拦了 → 用户换模型就好；没拦 → 引擎静默丢图，用户以为功能坏了。
func TestEnsureImagesAllowed(t *testing.T) {
	textOnly := &engine.Model{ID: "glm-4", Provider: "zhipu", Input: []string{"text"}}
	if err := ensureImagesAllowed(textOnly, 1); err == nil {
		t.Fatal("纯文本模型带图应该被拒")
	}
	vision := &engine.Model{ID: "gpt-4o", Provider: "openai", Input: []string{"text", "image"}}
	if err := ensureImagesAllowed(vision, 1); err != nil {
		t.Fatalf("支持图片的模型不该被拦：%v", err)
	}
	// 没图时一律放行：这条路径不该因为模型信息而变复杂。
	if err := ensureImagesAllowed(textOnly, 0); err != nil {
		t.Fatalf("没带图时不该拦：%v", err)
	}
	// 拿不到模型信息时不拦：读不到状态不等于不支持。
	if err := ensureImagesAllowed(nil, 1); err != nil {
		t.Fatalf("拿不到模型信息时不该拦：%v", err)
	}
	// input 为空（引擎没报这个字段）同样不拦。
	if err := ensureImagesAllowed(&engine.Model{ID: "x"}, 1); err != nil {
		t.Fatalf("input 为空时不该拦：%v", err)
	}
}
