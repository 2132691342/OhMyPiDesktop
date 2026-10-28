package service

// 附件装配：把界面送来的附件清单变成一次引擎调用要发的内容。
//
// 两类附件走两条不同的路，混淆的后果不对称：
//   - 工作区文件只拼路径清单。引擎自己的读文件工具会去读，上下文按需增长。
//   - 图片必须走 prompt 的 images 字段。塞进正文只是一段 base64 文本，
//     模型看不见，而且会瞬间吃掉几万 token。
//
// 图片多走的这一步要多一层判断：当前模型不一定收图片。提前拦住并说清原因，
// 比让引擎回一个看不懂的错误好。

import (
	"encoding/base64"
	"path/filepath"
	"strings"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
)

// 单次发送的图片与文件上限。
//
// 上限存在的理由是界面的可用性而不是性能：一个塞了 20 张图的输入区，
// 用户自己也看不清发了什么，而模型那边还要按图片数计费。
const (
	maxImagesPerMessage = 8
	maxFilesPerMessage  = 40
	// maxImageBytes 单张图片的字节上限（原文件，不是 base64 后的长度）。
	// 4MB 够放一张 4K 截图；再大基本是用户在发原图，该先压一下。
	maxImageBytes = 4 << 20
)

// buildPrompt 把正文与附件拼成一次引擎调用的载荷。
//
// message 为空但有附件是合法的：用户可能只贴一张图说「照这个改」。
func buildPrompt(message string, attachments []domain.ChatAttachmentREQ) (engine.PromptPayload, error) {
	payload := engine.PromptPayload{Message: strings.TrimSpace(message)}
	var files []string
	var images []engine.ImageContent

	for _, item := range attachments {
		switch item.Kind {
		case domain.AttachmentKindFile:
			path := strings.TrimSpace(item.FilePath)
			if path == "" {
				continue
			}
			if len(files) >= maxFilesPerMessage {
				return engine.PromptPayload{}, pkg.New(pkg.CodeBadRequest,
					"一次最多引用 40 个文件", "超出的部分被丢掉")
			}
			files = append(files, filepath.ToSlash(path))

		case domain.AttachmentKindImage:
			if len(images) >= maxImagesPerMessage {
				return engine.PromptPayload{}, pkg.New(pkg.CodeBadRequest,
					"一次最多发送 8 张图片", "超出的部分被丢掉")
			}
			img, err := decodeImage(item)
			if err != nil {
				return engine.PromptPayload{}, err
			}
			images = append(images, img)

		default:
			return engine.PromptPayload{}, pkg.New(pkg.CodeBadRequest,
				"不认识的附件类型："+item.Kind, "只支持 file / image")
		}
	}

	if payload.Message == "" && len(files) == 0 && len(images) == 0 {
		return engine.PromptPayload{}, pkg.New(pkg.CodeBadRequest, "说点什么再发送", "")
	}

	// 文件清单附在正文后面。写成句子而不是 JSON：引擎读到的是自然语言上下文，
	// JSON 反而更容易被它当成要处理的代码。
	if len(files) > 0 {
		var b strings.Builder
		b.WriteString(payload.Message)
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("这次要看这些文件：\n")
		for _, f := range files {
			b.WriteString("- ")
			b.WriteString(f)
			b.WriteString("\n")
		}
		payload.Message = strings.TrimRight(b.String(), "\n")
	}
	payload.Images = images
	return payload, nil
}

// decodeImage 校验并解码一张图片。
func decodeImage(item domain.ChatAttachmentREQ) (engine.ImageContent, error) {
	raw := strings.TrimSpace(item.ImageBase64)
	if raw == "" {
		return engine.ImageContent{}, pkg.New(pkg.CodeBadRequest, "这张图片没有内容", "")
	}
	// 粘贴来的可能是整条 data URL；只取逗号后面那一段。
	if idx := strings.Index(raw, ","); idx >= 0 && strings.HasPrefix(raw, "data:") {
		raw = raw[idx+1:]
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return engine.ImageContent{}, pkg.New(pkg.CodeBadRequest,
			"图片内容不是合法的 base64", "重新复制一次图片再试")
	}
	if len(data) > maxImageBytes {
		return engine.ImageContent{}, pkg.New(pkg.CodeBadRequest,
			"单张图片不能超过 4MB", "截图或压缩后再发")
	}
	mime := strings.TrimSpace(item.ImageMIME)
	if mime == "" || !strings.HasPrefix(mime, "image/") {
		mime = sniffImageMIME(data)
	}
	if !strings.HasPrefix(mime, "image/") {
		return engine.ImageContent{}, pkg.New(pkg.CodeBadRequest,
			"这不是一张图片", "只支持 png / jpeg / gif / webp")
	}
	// 统一重新编码：前端可能送来带换行的 base64，引擎那边不一定容忍。
	return engine.NewImage(mime, base64.StdEncoding.EncodeToString(data)), nil
}

// sniffImageMIME 按字节头认图片类型。认不出返回空串。
//
// 不引第三方库：只有四种格式需要认，魔数一共十几个字节。
func sniffImageMIME(data []byte) string {
	switch {
	case len(data) >= 8 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G':
		return "image/png"
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 6 && string(data[0:4]) == "GIF8":
		return "image/gif"
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

// ensureImagesAllowed 确认当前模型收图片。
//
// 判据是模型描述里的 input 数组含 "image"。不带时提前拒绝并说清原因——
// 把图片发给一个纯文本模型，引擎要么静默丢掉，要么回一个看不懂的错，
// 两种都让用户以为程序坏了。
//
// model 为 nil（引擎没起、或拿不到状态）时不拦：宁可让引擎自己判断，
// 也不要因为读不到状态就把功能禁掉。
func ensureImagesAllowed(model *engine.Model, count int) error {
	if count == 0 || model == nil {
		return nil
	}
	// input 为空说明引擎没报这个字段（老版本），同样不拦。
	if len(model.Input) == 0 {
		return nil
	}
	for _, in := range model.Input {
		if in == "image" {
			return nil
		}
	}
	return pkg.New(pkg.CodeBadRequest,
		"当前模型不收图片（"+model.Provider+"/"+model.ID+"）",
		"换一个支持图片输入的模型再发，或先去掉图片附件")
}
