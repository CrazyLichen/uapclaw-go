package adapter

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleSlashCommand 处理斜杠命令。
// Python: _handle_slash_command() (line 3769-3830)
//
// 按 query 前缀 /evolve* 分发到具体处理器。
// ✅ 已回填：依赖 SkillEvolutionRail
func (d *DeepAdapter) handleSlashCommand(ctx context.Context, query string, sessionID string, mode string) (map[string]any, error) {
	if query == "" || !strings.HasPrefix(query, "/") {
		return nil, nil // 非 slash 命令
	}

	// Python: 按 query 前缀分发
	switch {
	case strings.HasPrefix(query, "/evolve_simplify"):
		return d.handleEvolveSimplifyCommand(ctx, query, sessionID)
	case strings.HasPrefix(query, "/evolve_rebuild"):
		return d.handleEvolveRebuildCommand(ctx, query, sessionID)
	case strings.HasPrefix(query, "/evolve_rollback"):
		return d.handleEvolveRollbackCommand(ctx, query, sessionID)
	case strings.HasPrefix(query, "/evolve_list"):
		return d.handleEvolveListCommand(ctx, sessionID)
	case strings.HasPrefix(query, "/evolve"):
		return d.handleEvolveCommand(ctx, query, sessionID)
	default:
		return nil, nil
	}
}

// handleEvolveCommand 处理 /evolve 命令。
// ✅ 已回填（对齐 Python: _handle_evolve_command()）
//
// 格式：/evolve [skill_name] [query]
// 无 skill_name 时 fallback 到 handleEvolveListCommand
func (d *DeepAdapter) handleEvolveCommand(ctx context.Context, query string, sessionID string) (map[string]any, error) {
	if d.skillEvolutionRail == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("handleEvolveCommand: SkillEvolutionRail 未初始化")
		return nil, nil
	}

	// 解析命令：/evolve [skill_name] [query]
	parts := strings.Fields(strings.TrimPrefix(query, "/evolve"))
	skillName := ""
	userIntent := ""
	if len(parts) >= 1 {
		skillName = parts[0]
	}
	if len(parts) >= 2 {
		userIntent = strings.Join(parts[1:], " ")
	}

	if skillName == "" {
		// /evolve 无参数 → 列出可用技能
		return d.handleEvolveListCommand(ctx, sessionID)
	}

	// Python: result = await self._skill_evolution_rail.request_user_evolution(skill_name, user_intent, auto_approve=False)
	result, err := d.skillEvolutionRail.RequestUserEvolution(ctx, skillName, userIntent, false)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("skill", skillName).Msg("handleEvolveCommand: RequestUserEvolution 失败")
		return map[string]any{"status": "failed", "error": err.Error()}, nil
	}

	if result.ApprovalEvent != nil {
		return map[string]any{
			"action":          "approval_required",
			"approval_chunks": []any{result.ApprovalEvent},
			"request_id":      ptrToStr(result.RequestID),
			"skill_name":      result.SkillName,
		}, nil
	}

	return map[string]any{
		"status":        "ok",
		"skill_name":    result.SkillName,
		"auto_approved": result.AutoApproved,
	}, nil
}

// handleEvolveListCommand 处理 /evolve_list 命令。
// ✅ 已回填（对齐 Python: _handle_evolve_list_command()）
//
// 列出所有技能及其演进经验摘要
func (d *DeepAdapter) handleEvolveListCommand(ctx context.Context, sessionID string) (map[string]any, error) {
	if d.skillEvolutionRail == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("handleEvolveListCommand: SkillEvolutionRail 未初始化")
		return nil, nil
	}

	store := d.skillEvolutionRail.EvolutionStore()
	skillNames := store.ListSkillNames(ctx)

	skills := make([]map[string]any, 0, len(skillNames))
	for _, name := range skillNames {
		skill := map[string]any{
			"name":            name,
			"has_evolution":   store.SkillExists(ctx, name),
			"hint_simplify":   d.skillEvolutionRail.ShouldHintSimplifyOrRebuild(name),
		}
		skills = append(skills, skill)
	}

	return map[string]any{
		"action": "evolve_list",
		"skills": skills,
	}, nil
}

// handleEvolveSimplifyCommand 处理 /evolve_simplify 命令。
// ✅ 已回填（对齐 Python: _handle_evolve_simplify_command()）
//
// 格式：/evolve_simplify skill_name [user_intent]
func (d *DeepAdapter) handleEvolveSimplifyCommand(ctx context.Context, query string, sessionID string) (map[string]any, error) {
	if d.skillEvolutionRail == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("handleEvolveSimplifyCommand: SkillEvolutionRail 未初始化")
		return nil, nil
	}

	// 解析命令：/evolve_simplify skill_name [user_intent]
	parts := strings.Fields(strings.TrimPrefix(query, "/evolve_simplify"))
	if len(parts) == 0 {
		return map[string]any{"status": "failed", "error": "missing skill_name"}, nil
	}
	skillName := parts[0]
	var userIntent *string
	if len(parts) >= 2 {
		intent := strings.Join(parts[1:], " ")
		userIntent = &intent
	}

	// Python: result = await self._skill_evolution_rail.request_simplify(skill_name, user_intent=user_intent)
	result, err := d.skillEvolutionRail.RequestSimplify(ctx, skillName, userIntent)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("skill", skillName).Msg("handleEvolveSimplifyCommand: RequestSimplify 失败")
		return map[string]any{"status": "failed", "error": err.Error()}, nil
	}

	if result.ApprovalEvent != nil {
		return map[string]any{
			"action":          "approval_required",
			"approval_chunks": []any{result.ApprovalEvent},
			"request_id":      ptrToStr(result.RequestID),
			"skill_name":      result.SkillName,
			"actions":         result.Actions,
		}, nil
	}

	return map[string]any{
		"status":     "ok",
		"skill_name": result.SkillName,
		"actions":    result.Actions,
	}, nil
}

// handleEvolveRebuildCommand 处理 /evolve_rebuild 命令。
// ✅ 已回填（对齐 Python: _handle_evolve_rebuild_command()）
//
// 格式：/evolve_rebuild skill_name [user_intent]
// 返回 followup_prompt 用于注入 agent loop 执行 skill-creator
func (d *DeepAdapter) handleEvolveRebuildCommand(ctx context.Context, query string, sessionID string) (map[string]any, error) {
	if d.skillEvolutionRail == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("handleEvolveRebuildCommand: SkillEvolutionRail 未初始化")
		return nil, nil
	}

	// 解析命令：/evolve_rebuild skill_name [user_intent]
	parts := strings.Fields(strings.TrimPrefix(query, "/evolve_rebuild"))
	if len(parts) == 0 {
		return map[string]any{"status": "failed", "error": "missing skill_name"}, nil
	}
	skillName := parts[0]
	var userIntent *string
	if len(parts) >= 2 {
		intent := strings.Join(parts[1:], " ")
		userIntent = &intent
	}

	// Python: followup_prompt = await self._skill_evolution_rail.request_rebuild(skill_name, user_intent=user_intent, min_score=0.5)
	followupPrompt, err := d.skillEvolutionRail.RequestRebuild(ctx, skillName, userIntent, 0.5)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("skill", skillName).Msg("handleEvolveRebuildCommand: RequestRebuild 失败")
		return map[string]any{"status": "failed", "error": err.Error()}, nil
	}

	if followupPrompt == "" {
		return map[string]any{"status": "no_rebuild", "skill_name": skillName}, nil
	}

	// Python: return {"action": "run_rebuild_followup", "followup_prompt": followup_prompt}
	return map[string]any{
		"action":         "run_rebuild_followup",
		"followup_prompt": followupPrompt,
		"skill_name":     skillName,
	}, nil
}

// handleEvolveRollbackCommand 处理 /evolve_rollback 命令。
// ✅ 已回填（对齐 Python: _handle_evolve_rollback_command()）
//
// 格式：/evolve_rollback skill_name [version]
func (d *DeepAdapter) handleEvolveRollbackCommand(ctx context.Context, query string, sessionID string) (map[string]any, error) {
	if d.skillEvolutionRail == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("handleEvolveRollbackCommand: SkillEvolutionRail 未初始化")
		return nil, nil
	}

	// 解析命令：/evolve_rollback skill_name [version]
	parts := strings.Fields(strings.TrimPrefix(query, "/evolve_rollback"))
	if len(parts) == 0 {
		return map[string]any{"status": "failed", "error": "missing skill_name"}, nil
	}
	skillName := parts[0]
	var version *string
	if len(parts) >= 2 {
		v := parts[1]
		version = &v
	}

	// Python: success = await self._skill_evolution_rail.rollback_skill(skill_name, version=version)
	success, err := d.skillEvolutionRail.RollbackSkill(ctx, skillName, version)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("skill", skillName).Msg("handleEvolveRollbackCommand: RollbackSkill 失败")
		return map[string]any{"status": "failed", "error": err.Error()}, nil
	}

	return map[string]any{
		"status":     fmt.Sprintf("%v", success),
		"skill_name": skillName,
	}, nil
}

// handleGovernanceApproval 处理治理审批。
// ✅ 已回填（对齐 Python: _handle_governance_approval()）
//
// 根据 request_id 前缀路由到 OnApproveSimplify / OnRejectSimplify
func (d *DeepAdapter) handleGovernanceApproval(requestID string, answers any, approvalType string) bool {
	if d.skillEvolutionRail == nil {
		logger.Warn(logComponent).Str("request_id", requestID).Msg("handleGovernanceApproval: SkillEvolutionRail 未初始化")
		return false
	}

	ctx := context.Background()

	switch approvalType {
	case "approve":
		result, err := d.skillEvolutionRail.OnApproveSimplify(ctx, requestID)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("handleGovernanceApproval: OnApproveSimplify 失败")
			return false
		}
		if result != nil {
			logger.Info(logComponent).Str("request_id", requestID).Any("result", result).Msg("handleGovernanceApproval: simplify 已执行")
		}
		return true
	case "reject":
		d.skillEvolutionRail.OnRejectSimplify(requestID)
		logger.Info(logComponent).Str("request_id", requestID).Msg("handleGovernanceApproval: simplify 已拒绝")
		return true
	default:
		logger.Warn(logComponent).Str("approval_type", approvalType).Msg("handleGovernanceApproval: 未知审批类型")
		return false
	}
}

// ptrToStr 将字符串指针转为字符串，nil 返回空字符串。
func ptrToStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
