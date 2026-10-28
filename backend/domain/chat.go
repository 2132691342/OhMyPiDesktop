// 对话的入参形态：一轮提问带什么、附件长什么样。
package domain

// ChatSendREQ 发送一轮提问。
//
// 附件分两类，走两条完全不同的路：
//   - 工作区内的文件 → 只拼路径清单进正文，由引擎自己的读文件工具去看；
//   - 图片 → 走引擎 prompt 的 images 字段，这是唯一能传二进制的通道。
//
// 为什么不把文件内容内联进正文：一本账不该记两份。引擎读文件时会把内容记进
// 它自己的上下文与缓存，我们再塞一份进 prompt 只会重复计费、并让「引擎看过什么」
// 变得没法追溯。
type ChatSendREQ struct {
	// Message 用户输入的正文
	Message string `json:"message"`
	// Attachments 附件清单；为空表示纯文本
	Attachments []ChatAttachmentREQ `json:"attachments,omitempty"`
}

// ChatAttachmentREQ 一个附件。按 Kind 决定走哪条路，两个字段互斥。
type ChatAttachmentREQ struct {
	// Kind 附件种类：file（工作区文件引用） / image（图片）
	Kind string `json:"kind"`
	// FilePath 相对工作区的路径（Kind=file 时必填）
	FilePath string `json:"file_path,omitempty"`
	// FileName 展示名（Kind=file 时可省略，后端按 FilePath 推）
	FileName string `json:"file_name,omitempty"`
	// ImageBase64 图片的 base64 字节，不含 data: 前缀（Kind=image 时必填）
	ImageBase64 string `json:"image_base64,omitempty"`
	// ImageMIME 图片类型，如 image/png；留空时按字节头识别
	ImageMIME string `json:"image_mime,omitempty"`
}

// 附件种类的取值。用常量而不是字面量：前端与 handler 都按它分支，
// 拼错一个字母的后果是「附件静默丢失」——最难查的那一类。
const (
	AttachmentKindFile  = "file"
	AttachmentKindImage = "image"
)

// ChatAttachmentVO 附件的展示形态（历史消息里回显用）。
type ChatAttachmentVO struct {
	// Kind 附件种类
	Kind string `json:"kind"`
	// FilePath 相对工作区的路径（Kind=file）
	FilePath string `json:"file_path,omitempty"`
	// ImageBase64 图片字节（Kind=image）；只在当轮事件里带，不落库
	ImageBase64 string `json:"image_base64,omitempty"`
	// ImageMIME 图片类型（Kind=image）
	ImageMIME string `json:"image_mime,omitempty"`
}
