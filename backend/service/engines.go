package service

// 引擎状态：探测结果、落盘布局、重启。
//
// 探测不安装。缺引擎时这里只回答「缺什么」与「装它的命令原文」，
// 界面的其余部分照常可用。

import (
	"context"

	"OhMyPiDesktop/backend/engine"
)

// ---------------- 引擎状态 ----------------

// EngineStatus 是「设置 · 引擎」页需要的全部事实。
type EngineStatus struct {
	Engines []EngineInfo      `json:"engines"`
	Running []engine.RunRow   `json:"running"`
	Install map[string]string `json:"install"`
}

// EngineInfo 是单个引擎的探测结果与落盘位置。
type EngineInfo struct {
	engine.Identity
	Installed bool        `json:"installed"`
	Layout    envLayoutVO `json:"layout"`
}

type envLayoutVO struct {
	Root     string `json:"root"`
	Agent    string `json:"agent"`
	Sessions string `json:"sessions"`
	Skills   string `json:"skills"`
	Plugins  string `json:"plugins"`
	Models   string `json:"models"`
	Settings string `json:"settings"`
	Exists   bool   `json:"exists"`
}

// EngineStatus 组装引擎状态。
func (a *App) EngineStatus(ctx context.Context) (*EngineStatus, error) {
	a.detectAll(ctx, false)
	out := &EngineStatus{
		Engines: []EngineInfo{},
		Running: a.Engine.Running(),
		Install: map[string]string{},
	}
	if out.Running == nil {
		out.Running = []engine.RunRow{}
	}
	for _, kind := range engine.AllKinds() {
		id := a.Identity(kind)
		info := EngineInfo{Identity: id, Installed: id.Exe != ""}
		if layout, err := a.Layout(kind); err == nil {
			info.Layout = envLayoutVO{
				Root: layout.Root, Agent: layout.Agent, Sessions: layout.Sessions,
				Skills: layout.Skills, Plugins: layout.Plugins, Models: layout.Models,
				Settings: layout.Settings, Exists: layout.Exists(),
			}
		}
		out.Engines = append(out.Engines, info)
		out.Install[string(kind)] = engine.InstallHint(kind)
	}
	return out, nil
}

// DetectEngines 强制重新探测（设置页的「重新检测」）。
func (a *App) DetectEngines(ctx context.Context) (*EngineStatus, error) {
	a.detectAll(ctx, true)
	return a.EngineStatus(ctx)
}

// RestartEngines 重启引擎（有进程在跑时停掉后重新拉起）。
func (a *App) RestartEngines(ctx context.Context, kind engine.Kind) error {
	if kind.Valid() {
		a.Engine.StopKind(kind)
		_, err := a.Engine.Ensure(ctx, kind, a.Workspace())
		return err
	}
	a.Engine.StopAll()
	for _, k := range engine.AllKinds() {
		if a.Identity(k).Exe == "" {
			continue
		}
		if _, err := a.Engine.Ensure(ctx, k, a.Workspace()); err != nil {
			return err
		}
	}
	return nil
}
