package service

// 会话：列会话、读明细、改名、删除。会话文件是引擎的账本，这里只读与切换，写进它的永远只有引擎自己。
// 列会话按引擎分开：两个引擎的目录与编码规则不同，混在一张表里会让人以为能接着聊。

import (
	"context"
	"os"
	"sort"
	"strings"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
	"OhMyPiDesktop/backend/session"
)

// ---------------- 会话 ----------------

// Sessions 列出会话。kind 为空表示全部引擎；workspace 为空表示全部目录。
//
// 两个引擎的会话目录不同，但列表形态一致——前端不必按引擎分叉渲染。
func (a *App) Sessions(kind engine.Kind, workspace string) ([]session.Session, error) {
	kinds := engine.AllKinds()
	if kind.Valid() {
		kinds = []engine.Kind{kind}
	}
	out := make([]session.Session, 0, 32)
	for _, k := range kinds {
		list, err := a.Index(k).List(workspace)
		if err != nil {
			// 一个引擎的会话目录读不到不该让整个列表失败：
			// pi 没装过就永远没有这个目录，那是正常状态。
			continue
		}
		out = append(out, list...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// SessionDetail 读会话详情。会话文件在该引擎的会话目录里，靠文件名归引擎。
func (a *App) SessionDetail(kind engine.Kind, path string) (*session.Detail, error) {
	if path == "" {
		return nil, pkg.New(pkg.CodeBadRequest, "缺少会话路径", "")
	}
	eng := a.kindOfSession(kind, path)
	d, err := a.Index(eng).Detail(path)
	if err != nil {
		return nil, err
	}
	if d.CWD != "" {
		_ = a.SetWorkspace(d.CWD)
	}
	return d, nil
}

// kindOfSession 判断一个会话文件属于哪个引擎。判据是「路径在哪一侧的会话目录下」；
// 都不在（用户手动打开的文件）时退回请求给的引擎。不靠文件名猜：两个引擎的文件名格式在变。
func (a *App) kindOfSession(kind engine.Kind, path string) engine.Kind {
	for _, k := range engine.AllKinds() {
		layout, err := a.Layout(k)
		if err != nil {
			continue
		}
		if isUnder(path, layout.Sessions) {
			return k
		}
	}
	if kind.Valid() {
		return kind
	}
	return a.ActiveKind()
}

// DeleteSession 删掉会话文件。
//
// 不删正在跑的那个：引擎会往已删文件继续追加，表现为「删了又回来了」。
func (a *App) DeleteSession(kind engine.Kind, path string) error {
	eng := a.kindOfSession(kind, path)
	e := a.EngineFor(eng, dirOf(path))
	if e.Busy() && samePath(e.CurrentSessionFile(), path) {
		return pkg.New(pkg.CodeSessionBusy, "这个会话正在跑，先停下来再删", "")
	}
	if err := os.Remove(path); err != nil {
		return pkg.Wrap(pkg.CodeNotFound, "删除会话失败", err)
	}
	// 目录里的其它文件（子代理转录等）一起清掉。
	base := strings.TrimSuffix(path, ".jsonl")
	if st, err := os.Stat(base); err == nil && st.IsDir() {
		_ = os.RemoveAll(base)
	}
	// 缓存里那条不主动丢掉的话，列表会一直显示「已经删了」的会话。
	a.Index(eng).Invalidate(path)
	a.InvalidateCounts()
	// 它的隐藏账也一起清：账不留给不存在的会话。
	a.forgetHiddenTurns(path)
	return nil
}

// RenameSession 改名（走 RPC：引擎同时会更新文件头的标题槽）。
func (a *App) RenameSession(ctx context.Context, kind engine.Kind, path, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return pkg.New(pkg.CodeBadRequest, "标题不能为空", "")
	}
	eng := a.kindOfSession(kind, path)
	e := a.EngineFor(eng, dirOf(path))
	if current := e.CurrentSessionFile(); !samePath(current, path) {
		if err := e.Switch(ctx, path); err != nil {
			return err
		}
	}
	if err := e.SetSessionName(ctx, name); err != nil {
		return err
	}
	// 成功之后才丢缓存：否则列表里的旧标题会一直显示到被淘汰为止。
	// 放在成功之后是因为失败时文件根本没变，丢缓存只会多一次重新解析。
	a.Index(eng).Invalidate(path)
	return nil
}
