package service

// 插件与扩展：列出、安装、卸载。全部走引擎自己的 CLI。
//
// 不自己解 node_modules：包的布局是引擎的私有契约，
// 照着解出来的结果会在引擎下一次改布局时静默错掉。

import (
	"context"
	"strings"
	"time"

	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/pkg"
)

// ---------------- 插件 / 扩展 ----------------

// PluginRow 是一条已装的插件/扩展。
type PluginRow struct {
	Kind    engine.Kind `json:"kind"`
	Name    string      `json:"name"`
	Version string      `json:"version"`
	Source  string      `json:"source"`
	Path    string      `json:"path"`
}

// Plugins 列出已装的插件。
//
// 都走引擎自己的 CLI：omp 有 `omp plugin list --json`，pi 有 `pi list --json`。
// 不自己解 node_modules——包的布局是引擎的私有契约。
func (a *App) Plugins(ctx context.Context, kind engine.Kind) ([]PluginRow, error) {
	kinds := engine.AllKinds()
	if kind.Valid() {
		kinds = []engine.Kind{kind}
	}
	out := []PluginRow{}
	for _, k := range kinds {
		if !a.Identity(k).Caps.Extensions {
			continue
		}
		list, err := a.pluginsOf(ctx, k)
		if err != nil {
			continue
		}
		out = append(out, list...)
	}
	return out, nil
}

func (a *App) pluginsOf(ctx context.Context, kind engine.Kind) ([]PluginRow, error) {
	id := a.Identity(kind)
	if id.Exe == "" {
		return nil, engine.ErrMissing(kind)
	}
	args := []string{"plugin", "list", "--json"}
	if kind == engine.KindPi {
		args = []string{"list", "--json"}
	}
	var raw []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Source  string `json:"source"`
		Spec    string `json:"spec"`
		Path    string `json:"path"`
	}
	if err := runJSON(ctx, id.Exe, &raw, args...); err != nil {
		return nil, err
	}
	out := make([]PluginRow, 0, len(raw))
	for _, p := range raw {
		source := firstNonEmpty(p.Source, p.Spec, p.Path)
		out = append(out, PluginRow{
			Kind: kind, Name: p.Name, Version: p.Version, Source: source, Path: p.Path,
		})
	}
	return out, nil
}

// InstallPlugin 装一个插件/扩展（走引擎 CLI）。
func (a *App) InstallPlugin(ctx context.Context, kind engine.Kind, source string) (string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", pkg.New(pkg.CodeBadRequest, "给出包名或仓库地址", "")
	}
	k := a.KindOr(string(kind))
	id := a.Identity(k)
	if id.Exe == "" {
		return "", engine.ErrMissing(k)
	}
	args := []string{"plugin", "install", source}
	if k == engine.KindPi {
		args = []string{"install", source}
	}
	out, err := runText(ctx, id.Exe, 10*time.Minute, args...)
	if err != nil {
		return out, pkg.Wrap(pkg.CodePluginFailed, "安装失败", err)
	}
	return out, nil
}

// UninstallPlugin 卸一个插件/扩展。
func (a *App) UninstallPlugin(ctx context.Context, kind engine.Kind, source string) (string, error) {
	k := a.KindOr(string(kind))
	id := a.Identity(k)
	if id.Exe == "" {
		return "", engine.ErrMissing(k)
	}
	args := []string{"plugin", "uninstall", source}
	if k == engine.KindPi {
		args = []string{"remove", source}
	}
	out, err := runText(ctx, id.Exe, 3*time.Minute, args...)
	if err != nil {
		return out, pkg.Wrap(pkg.CodePluginFailed, "卸载失败", err)
	}
	return out, nil
}
