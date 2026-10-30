package session

// 会话标题：引擎默认不写标题，于是标题得从「第一条用户消息」推。
//
// 为什么单独一个文件：这是唯一一处「猜标题」的规则，而它有两个容易错的边界
// （content 可能是字符串也可能是块数组；一句话里的换行会把列表行撑成三行）。
// 与读文件的代码放在一起时，这两条会被当成实现细节看漏。
//
// 本文件不做什么：不读文件、不判断引擎种类。

import (
	"encoding/json"
	"strings"
)

// titleMaxRunes 是「拿第一句话当标题」的字数上限。
//
// 列表一行大约能放下这么多字；超过就截。取 40 是因为用户的第一句话
// 常常是一整段需求，前 40 个字已经足够区分是哪条会话。
const titleMaxRunes = 40

// defaultTitle 是完全没有可用文本时的兜底。
func defaultTitle(s Session) string {
	if s.Model != "" {
		return "会话 · " + s.Model
	}
	return "未命名会话"
}

// slugToPath 把目录 slug 还原成路径（--D--GoFiles-x-- → D:\GoFiles\x）。
//
// slug 有歧义（真实路径里也可能有 -），所以只用于展示；
// 真实路径从文件头的 cwd 取，那条才可信。
func slugToPath(slug string) string {
	s := strings.TrimPrefix(slug, "--")
	s = strings.TrimSuffix(s, "--")
	if len(s) >= 3 && s[0] == '-' {
		drive := s[1:2]
		rest := s[3:]
		return strings.ToUpper(drive) + ":\\" + strings.ReplaceAll(rest, "-", "\\")
	}
	return strings.ReplaceAll(s, "-", "/")
}

// firstUserText 从一条用户消息的 content 里取第一段正文。
//
// content 可能是字符串，也可能是块数组（pi 在带图片时总会用数组）。
// 只要第一段文本——候选标题不需要完整正文。
func firstUserText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			return b.Text
		}
	}
	return ""
}

// titleFromText 把一句话压成一行候选标题。
//
// 换行与连续空白全部压成单空格：JSONL 里的一条用户消息经常带换行与缩进，
// 原样放进列表会让一行变成三行。压完为空则回到「会话 · 模型名」。
func titleFromText(text string, s Session) string {
	flat := strings.Join(strings.Fields(text), " ")
	if flat == "" {
		return defaultTitle(s)
	}
	runes := []rune(flat)
	if len(runes) > titleMaxRunes {
		return string(runes[:titleMaxRunes]) + "…"
	}
	return flat
}
