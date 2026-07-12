package adapter

import (
	"context"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// watchEvolutionAndPush 启动 evolution 观察任务。
// 对齐 Python: _watch_evolution_and_push() (line 5725-5923)
// ⤵️ 10.6.3-10: 依赖 SkillEvolutionRail
func (d *DeepAdapter) watchEvolutionAndPush(ctx context.Context, sessionID string, requestID string) error {
	// ⤵️ 10.6.3-10: 实现 evolution watcher
	logger.Info(logComponent).Str("session_id", sessionID).Msg("watchEvolutionAndPush 等待 10.6.3-10 回填")
	return nil
}

// onEvolutionWatcherDone evolution 观察任务完成回调。
// 对齐 Python: _on_evolution_watcher_done()
// ⤵️ 10.6.3-10: 依赖 SkillEvolutionRail
func (d *DeepAdapter) onEvolutionWatcherDone(sessionID string) {
	// ⤵️ 10.6.3-10: 清理 evolution watcher
	logger.Info(logComponent).Str("session_id", sessionID).Msg("onEvolutionWatcherDone 等待 10.6.3-10 回填")
}

// isApprovalEvent 检查 request_id 是否为审批事件。
// 对齐 Python: is_approval_event() — 检查前缀
func (d *DeepAdapter) isApprovalEvent(requestID string) bool {
	return strings.HasPrefix(requestID, "skill_evolve_") ||
		strings.HasPrefix(requestID, "evolve_simplify_") ||
		strings.HasPrefix(requestID, "team_skill_evolve_")
}

// handleEvolutionApproval 处理演进审批。
// 对齐 Python: _handle_evolution_approval() (line 3626-3648)
// ⤵️ 10.6.3-10: 依赖 SkillEvolutionRail
func (d *DeepAdapter) handleEvolutionApproval(requestID string, answers any) bool {
	// ⤵️ 10.6.3-10: 实现 evolution 审批
	logger.Info(logComponent).Str("request_id", requestID).Msg("handleEvolutionApproval 等待 10.6.3-10 回填")
	return false
}

// ──────────────────────────── ContextCompressor 接口实现 ────────────────────────────

// CompressContext 触发上下文压缩。
// 对齐 Python: compress_context() (line 5380-5570)
// ⤵️ SessionHistory(JSONL): 需要会话历史持久化
func (d *DeepAdapter) CompressContext(ctx context.Context, sessionID string, session any, returnState bool) (map[string]any, error) {
	// ⤵️ SessionHistory(JSONL): 实现上下文压缩
	logger.Info(logComponent).Str("session_id", sessionID).Bool("return_state", returnState).Msg("CompressContext 等待 SessionHistory 回填")
	return nil, nil
}

// GetContextUsage 获取上下文窗口占用率。
// 对齐 Python: get_context_usage() (line 5572-5588)
func (d *DeepAdapter) GetContextUsage(ctx context.Context, sessionID string) (map[string]any, error) {
	if d.instance == nil {
		return nil, nil
	}
	// 对齐 Python: 直接调 instance.get_context_usage()
	usage, err := d.instance.GetContextUsage(ctx, sessionID, "")
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("GetContextUsage 失败")
		return nil, err
	}
	return usage, nil
}

// GenerateRecap 生成会话回顾摘要。
// 对齐 Python: generate_recap() (line 5590-5663)
// ⤵️ SessionHistory(JSONL): 需要会话历史持久化
func (d *DeepAdapter) GenerateRecap(ctx context.Context, sessionID string) (map[string]any, error) {
	// ⤵️ SessionHistory(JSONL): 实现 recap 生成
	logger.Info(logComponent).Str("session_id", sessionID).Msg("GenerateRecap 等待 SessionHistory 回填")
	return nil, nil
}

// ──────────────────────────── Recap 辅助方法 ────────────────────────────

// getRecentMessages 获取最近消息列表。
// 对齐 Python: _get_recent_messages() (line 5665-5690)
// ⤵️ SessionHistory(JSONL)
func (d *DeepAdapter) getRecentMessages(sessionID string) ([]map[string]any, error) {
	// ⤵️ SessionHistory(JSONL): 从 JSONL 读取最近消息
	return nil, nil
}

// callModelForRecap 调用模型生成 recap。
// 对齐 Python: _call_model_for_recap() (line 5691-5723)
// ⤵️ SessionHistory(JSONL)
func (d *DeepAdapter) callModelForRecap(ctx context.Context, messages []map[string]any) (string, error) {
	// ⤵️ SessionHistory(JSONL): 调用模型生成摘要
	return "", nil
}

// countFullContextTokens 计算完整上下文 token 数。
// 对齐 Python: _count_full_context_tokens() (line 5665-5723)
// ⤵️ SessionHistory(JSONL)
func (d *DeepAdapter) countFullContextTokens(ctx context.Context, sessionID string) (int, error) {
	// ⤵️ SessionHistory(JSONL): 计算上下文 token 总数
	return 0, nil
}
