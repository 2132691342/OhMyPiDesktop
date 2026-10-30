package service

// 插件与扩展：列、装、卸。「列」读引擎自己的登记清单（pi 是 settings.json 的 packages，
// omp 是 plugins/package.json 的 dependencies），不跑 CLI、不扫 node_modules——扫目录会把几百个间接依赖当成插件。
// 「装 / 卸」代跑引擎自己的 CLI：只有引擎知道怎么装进私有目录、同步锁文件，自己去改目录会留下「配置说装了、目录里没有」的脏状态。

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"OhMyPiDesktop/backend/domain"
	"OhMyPiDesktop/backend/engine"
	"OhMyPiDesktop/backend/env"
	"OhMyPiDesktop/backend/pkg"
)

// ---------------- 插件 / 扩展 ----------------

// PluginList 列出某引擎（或全部）的插件。两个引擎一起查时并发执行：
// 各自的清单在不同盘上不同目录里，串行只是让首屏多等一次文件读取。
func (a *App) PluginList(kind engine.Kind) (*domain.PluginListVO, error) {
	if kind.Valid() {
		return a.pluginsOf(kind)
	}
	out := &domain.PluginListVO{Plugins: []domain.PluginRowVO{}}
	kinds := engine.AllKinds()
	results := make([]*domain.PluginListVO, len(kinds))
	errs := make([]error, len(kinds))
	done := make(chan int, len(kinds))
	for i, k := range kinds {
		go func(i int, k engine.Kind) {
			results[i], errs[i] = a.pluginsOf(k)
			done <- i
		}(i, k)
	}
	for range kinds {
		<-done
	}
	var readErrs []string
	for i := range kinds {
		if errs[i] != nil {
			readErrs = append(readErrs, string(kinds[i])+": "+pkg.As(errs[i]).Message)
			continue
		}
		if results[i] == nil {
			continue
		}
		out.Plugins = append(out.Plugins, results[i].Plugins...)
		if out.RegistryPath == "" {
			out.RegistryPath = results[i].RegistryPath
		}
		if results[i].ReadError != "" {
			readErrs = append(readErrs, string(kinds[i])+": "+results[i].ReadError)
		}
	}
	if len(readErrs) > 0 {
		out.ReadError = strings.Join(readErrs, "；")
	}
	return out, nil
}

// pluginsOf 列一个引擎的插件：登记清单 + 包目录里的实际内容。
func (a *App) pluginsOf(kind engine.Kind) (*domain.PluginListVO, error) {
	layout, err := a.Layout(kind)
	if err != nil {
		return nil, err
	}
	entries, registry, err := env.ReadPluginEntries(string(kind), layout)
	if err != nil {
		return nil, err
	}
	out := &domain.PluginListVO{Plugins: []domain.PluginRowVO{}, RegistryPath: registry}
	// pi 把包装进 agent/npm/node_modules；omp 装进 plugins/node_modules
	modules := packageModulesDir(kind, layout)
	for _, entry := range entries {
		detail := env.ReadPluginDetail(modules, entry)
		row := domain.PluginRowVO{
			EngineKind:    string(kind),
			PluginName:    entry.Name,
			PluginVersion: detail.Version,
			PluginSource:  entry.Source,
			PluginPath:    detail.Path,
			PluginDesc:    detail.Desc,
			ResourceKinds: detail.Resources,
			ScopeKind:     entry.Scope,
			Enabled:       true,
			Installed:     detail.Path != "",
		}
		if row.ResourceKinds == nil {
			row.ResourceKinds = []string{}
		}
		out.Plugins = append(out.Plugins, row)
	}
	return out, nil
}

// packageModulesDir 返回该引擎装包的 node_modules。
func packageModulesDir(kind engine.Kind, layout env.Layout) string {
	if kind == engine.KindPi {
		return filepath.Join(layout.Plugins, "node_modules")
	}
	return filepath.Join(layout.Plugins, "node_modules")
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
	out, err := runText(ctx, id.Exe, 10*time.Minute, pluginArgs(k, "install", source)...)
	if err != nil {
		return out, pkg.Wrap(pkg.CodePluginFailed, "安装失败", err)
	}
	a.InvalidateCounts()
	return out, nil
}

// UninstallPlugin 卸一个插件/扩展（走引擎 CLI）。传清单里那一行原文而不是裸包名：
// pi 的 packages 记的是 `npm:name@1.2.3`，只给裸名会匹配不到。
func (a *App) UninstallPlugin(ctx context.Context, kind engine.Kind, source string) (string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", pkg.New(pkg.CodeBadRequest, "要卸哪个插件？", "")
	}
	k := a.KindOr(string(kind))
	id := a.Identity(k)
	if id.Exe == "" {
		return "", engine.ErrMissing(k)
	}
	out, err := runText(ctx, id.Exe, 3*time.Minute, pluginArgs(k, "uninstall", source)...)
	if err != nil {
		return out, pkg.Wrap(pkg.CodePluginFailed, "卸载失败", err)
	}
	a.InvalidateCounts()
	return out, nil
}

// pluginArgs 把动作翻成该引擎的 CLI 参数。omp 用 `plugin install`，pi 用 `install` / `remove`
// （uninstall 是 remove 的别名，统一发 remove——与 pi 帮助里列出的第一个名字一致）。
func pluginArgs(kind engine.Kind, action, source string) []string {
	if kind == engine.KindPi {
		if action == "uninstall" {
			return []string{"remove", source}
		}
		return []string{"install", source}
	}
	return []string{"plugin", action, source}
}
