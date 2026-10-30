package service

// 隐藏轮次账本：用户在界面上「删掉」的消息轮次，按会话文件路径记账。语义是隐藏不是删除：
// 引擎的会话文件与内存上下文一个字都不改；历史投影按账把轮次从返回里摘掉（见 history.go 的 markHidden），
// 会话详情页仍走直接投影看全量。账本存设置 KV（路径 → hid 列表），键统一小写，避免 Windows 下大小写不同记成两本。

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/pkg"
)

// HiddenTurns 是按会话记账的隐藏清单。零值可用，账目在首次使用时读入。
type HiddenTurns struct {
	mu     sync.Mutex
	byPath map[string]map[string]bool
	loaded bool
}

// hiddenKey 把会话路径规范成账本键：Windows 的路径大小写不敏感。
func hiddenKey(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

// load 把账本从设置 KV 读进内存。调用方必须持有 h.mu。
func (h *HiddenTurns) load() {
	h.loaded = true
	h.byPath = map[string]map[string]bool{}
}

// SetFor 返回某会话的隐藏集合（投影过滤用）。没有就返回 nil。
func (h *HiddenTurns) SetFor(path string) map[string]bool {
	if path == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.byPath[hiddenKey(path)]
}

// Hide 记一笔。hid 是历史投影生成的轮次标识。
func (h *HiddenTurns) Hide(path, hid string) error {
	if path == "" || hid == "" {
		return pkg.New(pkg.CodeBadRequest, "缺少会话路径或轮次标识", "")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.loaded {
		h.load()
	}
	key := hiddenKey(path)
	set, ok := h.byPath[key]
	if !ok {
		set = map[string]bool{}
		h.byPath[key] = set
	}
	set[hid] = true
	return nil
}

// Unhide 撤一笔。返回是否真的撤掉了（撤没有的不算错，撤销按钮要能随便点）。
func (h *HiddenTurns) Unhide(path, hid string) (bool, error) {
	if path == "" || hid == "" {
		return false, pkg.New(pkg.CodeBadRequest, "缺少会话路径或轮次标识", "")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	key := hiddenKey(path)
	set, ok := h.byPath[key]
	if !ok || !set[hid] {
		return false, nil
	}
	delete(set, hid)
	if len(set) == 0 {
		delete(h.byPath, key)
	}
	return true, nil
}

// Forget 删掉一个会话的整本账（会话被删除时调用，账不留给不存在的会话）。
func (h *HiddenTurns) Forget(path string) {
	if path == "" {
		return
	}
	h.mu.Lock()
	delete(h.byPath, hiddenKey(path))
	h.mu.Unlock()
}

// Marshal 实现与设置 KV 的往返：值是一段 JSON。
func (h *HiddenTurns) Marshal() (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string][]string{}
	for path, set := range h.byPath {
		list := make([]string, 0, len(set))
		for hid := range set {
			list = append(list, hid)
		}
		out[path] = list
	}
	if len(out) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// Unmarshal 从设置 KV 的值恢复账本。
func (h *HiddenTurns) Unmarshal(raw string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.loaded = true
	h.byPath = map[string]map[string]bool{}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var parsed map[string][]string
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return pkg.Wrap(pkg.CodeInternal, "隐藏轮次账本读不出来", err)
	}
	for path, list := range parsed {
		set := make(map[string]bool, len(list))
		for _, hid := range list {
			set[hid] = true
		}
		h.byPath[path] = set
	}
	return nil
}

// ---------------- App 侧用例：账本 + 落盘 ----------------

// HiddenSetFor 返回某会话的隐藏集合（历史投影过滤用）。没有就返回 nil。
func (a *App) HiddenSetFor(path string) map[string]bool {
	return a.hidden.SetFor(path)
}

// HideTurn 记一笔并落盘。
func (a *App) HideTurn(path, hid string) error {
	if err := a.hidden.Hide(path, hid); err != nil {
		return err
	}
	return a.saveHidden()
}

// UnhideTurn 撤一笔并落盘。撤没有的那笔不算错：撤销按钮要能随便点。
func (a *App) UnhideTurn(path, hid string) error {
	if _, err := a.hidden.Unhide(path, hid); err != nil {
		return err
	}
	return a.saveHidden()
}

// forgetHiddenTurns 删会话时清账。落盘失败不影响删除本身：账多留一页无碍。
func (a *App) forgetHiddenTurns(path string) {
	a.hidden.Forget(path)
	_ = a.saveHidden()
}

// saveHidden 把账本写回设置 KV；空账写入空串（Store 视为删除该键）。
func (a *App) saveHidden() error {
	raw, err := a.hidden.Marshal()
	if err != nil {
		return err
	}
	return a.Store.Set(domain.KeyHiddenTurns, raw)
}
