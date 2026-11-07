package adapter

import (
	"context"
	"path/filepath"

	skillrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RefreshSkillRails 技能环境变化后重载技能 rail 并清会话基线。
// 触发点：技能安装/卸载（SkillToolkit）、MCP 服务增删（syncMcpServersForRuntime）。
// 配置热更新**不**触发——Python `_update_runtime_config`（interface_deep.py:14671）
// 整段不碰技能 rail；禁用开关对存量会话的即时生效等 27 章 visibility 写 handler 落地后补。
// Python: JiuWenSwarmDeepAdapter.refresh_skill_rails (interface_deep.py:11154-11260)
func (d *DeepAdapter) RefreshSkillRails(ctx context.Context) error {
	rail, ok := d.skillRail.(*skillrails.SkillUseRail)
	if !ok || rail == nil {
		return nil
	}

	// 1. 重算扫描根（绑定包目录前置 + workspace 技能目录，去重）。
	// 扫描根为空也照常往下走：Python 侧无条件执行「更新 roots → 更新 disabled →
	// reload（空目录只告警）→ 清基线」，提前 return 会让技能安装/卸载后的
	// 清基线静默失效（技能视图仍是旧基线）。
	dirs := d.skillScanDirs()
	rail.SetSkillsDir(dirs)

	// 2. 更新 disabled 列表
	if d.skillManager != nil {
		rail.SetDisabledSkills(d.skillManager.ListExecutionDisabledSkills())
	}

	// 3. 重载技能
	if err := rail.ReloadSkills(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Msg("skill rail reload failed")
	}

	// 4. 清会话基线，强制下轮按新集合重拍。
	// Python: _clear_skill_session_baseline (interface_deep.py:11261-11274)；
	// 无活跃会话时本就是 no-op（上游 getattr(instance, "_loop_session", None) 为 None 直接 return）
	if d.instance != nil {
		// 会话判空交给 rail（ClearSessionBaseline 内部按 typed-nil 安全返回），
		// 与 Python "loop_session 为 None 直接 return" 等价
		rail.ClearSessionBaseline(d.instance.LoopSession())
	}

	logger.Info(logComponent).
		Int("scan_dirs", len(dirs)).
		Msg("skill rails refreshed")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// skillScanDirs 汇总技能扫描根：绑定包目录在前，workspace 技能目录在后，去重。
// Python: _skill_scan_dirs (interface_deep.py:11190-11220)
func (d *DeepAdapter) skillScanDirs() []string {
	seen := map[string]struct{}{}
	var dirs []string
	appendDir := func(raw string) {
		if raw == "" {
			return
		}
		key := raw
		if abs, err := filepath.Abs(raw); err == nil {
			key = abs
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		dirs = append(dirs, raw)
	}

	// 绑定包目录优先于同名 workspace 技能（Python: bound package dirs prepend）
	switch bound := d.instanceOverrides["skills"].(type) {
	case string:
		appendDir(bound)
	case []string:
		for _, item := range bound {
			appendDir(item)
		}
	case []any:
		for _, item := range bound {
			if text, ok := item.(string); ok {
				appendDir(text)
			}
		}
	}
	if ws := workspace.AgentSkillsDir(); ws != "" {
		appendDir(ws)
	}
	return dirs
}
