package evolution

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionApprovalRuntime 共享审批生命周期辅助，绑定到一个轨道实例。
// 对齐 Python: EvolutionApprovalRuntime
type EvolutionApprovalRuntime struct {
	// manager 审批管理器
	// 对齐 Python: self._manager: ApprovalManagerProtocol
	manager ApprovalManager
	// pendingApprovalSnapshots 暂存审批快照映射
	// 对齐 Python: self._pending_approval_snapshots: PendingApprovalSnapshotStore
	pendingApprovalSnapshots PendingApprovalSnapshotStore
}

// ──────────────────────────── 枚举────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEvolutionApprovalRuntime 创建审批运行时。
// 对齐 Python: EvolutionApprovalRuntime.__init__(*, manager, pending_approval_snapshots)
func NewEvolutionApprovalRuntime(manager ApprovalManager, pendingApprovalSnapshots PendingApprovalSnapshotStore) *EvolutionApprovalRuntime {
	return &EvolutionApprovalRuntime{
		manager:                  manager,
		pendingApprovalSnapshots: pendingApprovalSnapshots,
	}
}

// LookupPendingApprovalSnapshot 解析一个具体轨道拥有的暂存审批快照。
// 对齐 Python: EvolutionApprovalRuntime.lookup_pending_approval_snapshot(self, request_id, *, rail_name, action_name)
func (r *EvolutionApprovalRuntime) LookupPendingApprovalSnapshot(requestID, railName, actionName string) *experience.PendingChange {
	// 对齐 Python L37: pending = self._pending_approval_snapshots.get(request_id)
	pending := r.pendingApprovalSnapshots[requestID]
	if pending == nil {
		// 对齐 Python L38-39: logger.warning("[%s] %s: unknown request_id=%s", rail_name, action_name, request_id)
		logger.Warn(logger.ComponentAgentCore).
			Str("rail_name", railName).
			Str("action_name", actionName).
			Str("request_id", requestID).
			Msg("unknown request_id")
	}
	return pending
}

// ApprovePendingRequest 通过共享管理器生命周期批准一个暂存请求。
// 对齐 Python: EvolutionApprovalRuntime.approve_pending_request(self, request_id, *, rail_name, action_name)
func (r *EvolutionApprovalRuntime) ApprovePendingRequest(
	ctx context.Context,
	requestID, railName, actionName string,
) (*experience.PendingChange, *experience.ExperienceApplyResult, error) {
	// 对齐 Python L50-53: 查找 pending
	pending := r.LookupPendingApprovalSnapshot(requestID, railName, actionName)
	if pending == nil {
		// 对齐 Python L55-56: return None, None
		return nil, nil, nil
	}

	// 对齐 Python L58: result = await self._manager.approve_request(request_id)
	result, err := r.manager.ApproveRequest(ctx, requestID)
	if err != nil {
		return pending, nil, err
	}

	// 对齐 Python L59: pending_count = getattr(result, "pending_count", 0)
	if result.PendingCount > 0 {
		// 对齐 Python L60-72: 部分失败时记录 Warn 日志
		logger.Warn(logger.ComponentAgentCore).
			Str("rail_name", railName).
			Str("action_name", actionName).
			Int("applied_count", result.AppliedCount).
			Int("pending_count", result.PendingCount).
			Str("skill_name", pending.SkillName).
			Str("request_id", requestID).
			Msg("partial failure: some records not written, retry to complete")
	}

	// 对齐 Python L73: return pending, result
	return pending, &result, nil
}

// RejectPendingRequest 通过共享管理器生命周期拒绝一个暂存请求。
// 对齐 Python: EvolutionApprovalRuntime.reject_pending_request(self, request_id, *, rail_name, action_name)
func (r *EvolutionApprovalRuntime) RejectPendingRequest(
	ctx context.Context,
	requestID, railName, actionName string,
) (*experience.PendingChange, *experience.ExperienceApplyResult, error) {
	// 对齐 Python L83-86: 查找 pending
	pending := r.LookupPendingApprovalSnapshot(requestID, railName, actionName)
	if pending == nil {
		// 对齐 Python L88-89: return None, None
		return nil, nil, nil
	}

	// 对齐 Python L90: result = await self._manager.reject_request(request_id)
	result, err := r.manager.RejectRequest(ctx, requestID)
	if err != nil {
		return pending, nil, err
	}

	// 对齐 Python L91: return pending, result
	return pending, &result, nil
}

// FinalizeStagedEvolutionRequest 将暂存请求路由到审批缓冲或自动审批副作用。
// 对齐 Python: EvolutionApprovalRuntime.finalize_staged_evolution_request(self, request, *, requires_approval, emit_approval_request, on_auto_approved=None)
//
// Python 中使用 inspect.isawaitable 判断回调是否需要 await，
// Go 中回调统一为 func(any) error，调用方在闭包内自行处理异步。
func (r *EvolutionApprovalRuntime) FinalizeStagedEvolutionRequest(
	request any,
	requiresApproval bool,
	emitApprovalRequest func(any) error,
	onAutoApproved func(any) error,
) error {
	// 对齐 Python L101-103: if request is None: return None
	if request == nil {
		return nil
	}

	// 对齐 Python L105-109: if requires_approval:
	if requiresApproval {
		// 对齐 Python L106: outcome = emit_approval_request(request)
		if emitApprovalRequest != nil {
			// 对齐 Python L107-108: if inspect.isawaitable(outcome): await outcome
			return emitApprovalRequest(request)
		}
		return nil
	}

	// 对齐 Python L111-114: if on_auto_approved is not None:
	if onAutoApproved != nil {
		// 对齐 Python L112: outcome = on_auto_approved(request)
		// 对齐 Python L113-114: if inspect.isawaitable(outcome): await outcome
		return onAutoApproved(request)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
