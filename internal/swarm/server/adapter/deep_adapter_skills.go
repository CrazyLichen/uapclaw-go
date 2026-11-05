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
//
// 三类触发点：技能安装/卸载（SkillToolkit）、MCP 服务增删（syncMcpServersForRuntime）、
// 配置热更新（updateRuntimeConfig）。
//
// Python: JiuWenSwarmDeepAdapter.refresh_skill_rails (interface_deep.py:11154-11260)
func (d *DeepAdapter) RefreshSkillRails(ctx context.Context) error {
	rail, ok := d.skillRail.(*skillrails.SkillUseRail)
	if !ok || rail == nil {
		return nil
	}

	// 1. 重算扫描根（绑定包目录前置 + workspace 技能目录，去重）
	dirs := d.skillScanDirs()
	if len(dirs) == 0 {
		return nil
	}
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
		if sess := d.instance.LoopSession(); sess != nil {
			if err := rail.ClearSessionBaseline(sess); err != nil {
				logger.Warn(logComponent).Err(err).Msg("clear skill session baseline failed")
			}
		}
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
