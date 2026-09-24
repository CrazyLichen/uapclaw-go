package evolution

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionApprovalRuntime 共享审批生命周期辅助，绑定到一个轨道实例。
type EvolutionApprovalRuntime struct {
	// manager 审批管理器
	manager ApprovalManager
	// pendingApprovalSnapshots 暂存审批快照映射
	pendingApprovalSnapshots PendingApprovalSnapshotStore
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEvolutionApprovalRuntime 创建审批运行时。
func NewEvolutionApprovalRuntime(manager ApprovalManager, pendingApprovalSnapshots PendingApprovalSnapshotStore) *EvolutionApprovalRuntime {
	return &EvolutionApprovalRuntime{
		manager:                  manager,
		pendingApprovalSnapshots: pendingApprovalSnapshots,
	}
}

// LookupPendingApprovalSnapshot 解析一个具体轨道拥有的暂存审批快照。
func (r *EvolutionApprovalRuntime) LookupPendingApprovalSnapshot(requestID, railName, actionName string) *experience.PendingChange {
	pending := r.pendingApprovalSnapshots[requestID]
	if pending == nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("rail_name", railName).
			Str("action_name", actionName).
			Str("request_id", requestID).
			Msg("unknown request_id")
	}
	return pending
}

// ApprovePendingRequest 通过共享管理器生命周期批准一个暂存请求。
func (r *EvolutionApprovalRuntime) ApprovePendingRequest(
	ctx context.Context,
	requestID, railName, actionName string,
) (*experience.PendingChange, *experience.ExperienceApplyResult, error) {
	pending := r.LookupPendingApprovalSnapshot(requestID, railName, actionName)
	if pending == nil {
		return nil, nil, nil
	}

	result, err := r.manager.ApproveRequest(ctx, requestID)
	if err != nil {
		return pending, nil, err
	}

	if result.PendingCount > 0 {
		logger.Warn(logger.ComponentAgentCore).
			Str("rail_name", railName).
			Str("action_name", actionName).
			Int("applied_count", result.AppliedCount).
			Int("pending_count", result.PendingCount).
			Str("skill_name", pending.SkillName).
			Str("request_id", requestID).
			Msg("partial failure: some records not written, retry to complete")
	}

	return pending, &result, nil
}

// RejectPendingRequest 通过共享管理器生命周期拒绝一个暂存请求。
func (r *EvolutionApprovalRuntime) RejectPendingRequest(
	ctx context.Context,
	requestID, railName, actionName string,
) (*experience.PendingChange, *experience.ExperienceApplyResult, error) {
	pending := r.LookupPendingApprovalSnapshot(requestID, railName, actionName)
	if pending == nil {
		return nil, nil, nil
	}

	result, err := r.manager.RejectRequest(ctx, requestID)
	if err != nil {
		return pending, nil, err
	}

	return pending, &result, nil
}

// FinalizeStagedEvolutionRequest 将暂存请求路由到审批缓冲或自动审批副作用。
//
// Python: finalize_staged_evolution_request(request, requires_approval, emit_fn, on_auto_approved)
// requires_approval=True 时调用 emit_approval_request(request)，无论回调是否异常都返回 request。
// requires_approval=False 时调用 on_auto_approved(request)，同样无论回调是否异常都返回 request。
//
// 对齐 Python 行为：回调异常时记录日志但不中断流程，始终返回 request（而非 error）。
func (r *EvolutionApprovalRuntime) FinalizeStagedEvolutionRequest(
	request *experience.ExperienceApprovalRequest,
	requiresApproval bool,
	emitApprovalRequest func(*experience.ExperienceApprovalRequest) error,
	onAutoApproved func(*experience.ExperienceApprovalRequest) error,
) error {
	if request == nil {
		return nil
	}

	if requiresApproval {
		// Python: outcome = emit_approval_request(request); if awaitable: await outcome; return request
		// Python 不关心回调是否抛异常，始终返回 request
		if emitApprovalRequest != nil {
			if err := emitApprovalRequest(request); err != nil {
				logger.Warn(logComponent).Err(err).
					Msg("[EvolutionApprovalRuntime] emitApprovalRequest 失败，仍返回 request")
			}
		}
		return nil
	}

	// Python: if on_auto_approved: outcome = on_auto_approved(request); if awaitable: await outcome; return request
	if onAutoApproved != nil {
		if err := onAutoApproved(request); err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[EvolutionApprovalRuntime] onAutoApproved 失败，仍返回 request")
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
