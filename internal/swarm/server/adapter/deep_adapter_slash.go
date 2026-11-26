package adapter

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/signal"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleSlashCommand 处理斜杠命令。
// Python: _handle_slash_command() (line 4305-4382)
//
// 按 query 前缀 /evolve* 分发到具体处理器。
// ✅ 已回填：依赖 SkillEvolutionRail
func (d *DeepAdapter) handleSlashCommand(
	ctx context.Context, query string, sessionID string, mode string, channelID string,
) (map[string]any, error) {
	// Python: stripped = query.strip() 后按前缀匹配
	stripped := strings.TrimSpace(query)
	if stripped == "" || !strings.HasPrefix(stripped, "/") {
		return nil, nil // 非 slash 命令
	}

	// Python: interface_deep.py:4321 —— goal 斜杠文本只是 TUI 便利，Web 等通道
	// 必须走结构化 command.goal / attach_goal，普通聊天文本保持原义。
	if strings.ToLower(strings.TrimSpace(channelID)) == "tui" {
		if result := d.handleGoalSlashCommand(ctx, stripped, sessionID); result != nil {
			return result, nil
		}
	}

	// Python: :17485-17491 slash 门（ensure 演进 rail 栈 + skill_create 懒建）
	// → :17530 handle_evolution_slash_command(stripped, EvolutionSlashContext)
	if strings.HasPrefix(stripped, "/evolve") {
		if errMsg := d.ensureEvolutionRailForSlash(ctx, mode); errMsg != "" {
			return map[string]any{"output": errMsg, "result_type": "error"}, nil
		}
		// Python: handle_evolution_slash_command（rail-independent 新版，evolution_slash.py:51-102）
		// Python: skills_dir=str(get_agent_skills_dir())（:17530）。Go 取值面等价：
		// 生产 rail 的 store 根目录即 AgentSkillsDir（ensure/build 同源），
		// 已挂 rail 时直接复用其根目录（测试替身注入自定义根目录也因此生效）。
		skillsDir := workspace.AgentSkillsDir()
		if d.skillEvolutionRail != nil && d.skillEvolutionRail.EvolutionStore() != nil &&
			len(d.skillEvolutionRail.EvolutionStore().BaseDirs()) > 0 {
			skillsDir = d.skillEvolutionRail.EvolutionStore().BaseDirs()[0]
		}
		result := HandleEvolutionSlashCommand(ctx, stripped, EvolutionSlashContext{
			Mode:             mode,
			SessionID:        sessionID,
			SkillsDir:        skillsDir,
			EvolutionEnabled: skillEvolutionEnabledFrom(d.configBaseSnapshot(), d.configCacheSnapshot()),
			Language:         d.resolveRuntimeLanguage(),
			ReviewAgentName:  defaultReviewAgentName,
		})
		return annotateEvolveSlashResult(stripped, result), nil
	}
	return nil, nil
}

// ensureEvolutionRailForSlash 检查演进功能可用性，必要时配置并注册演进 rail 栈。
// Python: _ensure_evolution_rail_for_slash() (interface_deep.py:17474-17498)
//
// 步骤：
//
//	Python: 1. mode 白名单 {agent, agent.plan, agent.work.normal, agent.work.plan}
//	   （旧集合不含 team；code 系 / team.work.* 均不放行）
//	Python: 2. if not get_skill_evolution_enabled(config_base or config_cache): "演进功能未启用。"
//	Python: 3. await self._ensure_active_evolution_rails_registered()
//	Python: 4. if self._skill_evolution_rail is None: "演进功能初始化失败。"
//	Python: 5. skill_create rail 缺失则懒构建（:17494-17497）
func (d *DeepAdapter) ensureEvolutionRailForSlash(ctx context.Context, mode string) string {
	// Python: 1. 白名单
	switch mode {
	case "agent", "agent.plan", "agent.work.normal", "agent.work.plan":
	default:
		displayMode := mode
		if displayMode == "" {
			displayMode = "当前"
		}
		return displayMode + " 模式下演进功能不可用。"
	}
	// Python: 2. enabled 门
	if !skillEvolutionEnabledFrom(d.configBaseSnapshot(), d.configCacheSnapshot()) {
		return "演进功能未启用。"
	}
	// Python: 3. 配置并注册演进 rail 栈（含审查子 agent 重挂与引用刷新）
	d.ensureActiveEvolutionRailsRegistered(ctx)
	// Python: 4.
	if d.skillEvolutionRail == nil {
		return "演进功能初始化失败。"
	}
	// Python: 5. skill_create rail 懒构建
	if d.skillCreateRail == nil {
		d.ensureSkillCreateRail(ctx)
	}
	return ""
}


// collectMessagesForEvolve 收集当前会话的缓存消息用于演进信号检测。
// 对齐 Python: _collect_messages_for_evolve(session_id) (L4003-4019)
//
// 从 ReActAgent 的 ContextEngine 取 default_context 会话上下文，
// 读取消息（含历史）并归一化为 map 列表（含 tool_calls）。
// 任一环节缺失（实例/agent/context）或读取失败均返回 nil（Python 同样降级为空列表）。
func (d *DeepAdapter) collectMessagesForEvolve(sessionID string) []map[string]any {
	if d.instance == nil {
		return nil
	}
	reactAgent := d.instance.ReactAgent()
	if reactAgent == nil {
		return nil
	}
	contextEngine := reactAgent.ContextEngine()
	if contextEngine == nil {
		return nil
	}
	// Go 创建侧 context ID 为 "default_context"（react_helpers.go initContext），
	// 非 Python 默认值 "default_context_id"——跟随 Go 创建侧
	mc := contextEngine.GetContext("default_context", sessionID)
	if mc == nil {
		return nil
	}
	// size=0 不限制 + with_history=true，对齐 Python context.get_messages() 默认参数
	rawMessages, err := mc.GetMessages(0, true)
	if err != nil {
		logger.Debug(logComponent).Err(err).Str("session_id", sessionID).
			Msg("collectMessagesForEvolve failed")
		return nil
	}
	return signal.ParseMessages(rawMessages)
}








// skillBodyVersions 过滤出技能主体归档版本（Python: [a for a in archives if a.startswith("SKILL.v")]）。
func skillBodyVersions(archives []string) []string {
	out := make([]string, 0, len(archives))
	for _, a := range archives {
		if strings.HasPrefix(a, "SKILL.v") {
			out = append(out, a)
		}
	}
	return out
}

// formatArchiveTimestamp 归档文件名转可读时间（Python: SKILL.v20261030_120000.md → 2026-10-30 12:00:00 UTC）。
func formatArchiveTimestamp(archive string) string {
	ts := strings.ReplaceAll(strings.ReplaceAll(archive, "SKILL.v", ""), ".md", "")
	if len(ts) < 15 {
		return ts
	}
	return fmt.Sprintf("%s-%s-%s %s:%s:%s UTC", ts[0:4], ts[4:6], ts[6:8], ts[9:11], ts[11:13], ts[13:15])
}

// handleGovernanceApproval 处理治理审批。
// ✅ 已回填（对齐 Python: _handle_governance_approval()）
// 根据 request_id 前缀路由到 OnApproveSimplify / OnRejectSimplify
func (d *DeepAdapter) handleGovernanceApproval(ctx context.Context, requestID string, answers []ApprovalAnswer, approvalType string) bool {
	if d.skillEvolutionRail == nil {
		logger.Warn(logComponent).Str("request_id", requestID).Msg("handleGovernanceApproval: SkillEvolutionRail not initialized")
		return false
	}

	switch approvalType {
	case "approve":
		result, err := d.skillEvolutionRail.OnApproveSimplify(ctx, requestID)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("handleGovernanceApproval: OnApproveSimplify failed")
			return false
		}
		if result != nil {
			logger.Info(logComponent).Str("request_id", requestID).Any("result", result).Msg("handleGovernanceApproval: simplify executed")
		}
		return true
	case "reject":
		d.skillEvolutionRail.OnRejectSimplify(requestID)
		logger.Info(logComponent).Str("request_id", requestID).Msg("handleGovernanceApproval: simplify rejected")
		return true
	default:
		logger.Warn(logComponent).Str("approval_type", approvalType).Msg("handleGovernanceApproval: unknown approval type")
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
