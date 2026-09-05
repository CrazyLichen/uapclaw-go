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
// Python 中使用 inspect.isawaitable 判断回调是否需要 await，
// Go 中回调统一为 func(any) error，调用方在闭包内自行处理异步。
//
// TODO: P2/P4 实现后根据实际传入类型（EvolutionRequestResult / SimplifyRequestResult）
// 可将 request any 和回调参数收敛为具体类型或接口。
func (r *EvolutionApprovalRuntime) FinalizeStagedEvolutionRequest(
	request any,
	requiresApproval bool,
	emitApprovalRequest func(any) error,
	onAutoApproved func(any) error,
) error {
	if request == nil {
		return nil
	}

	if requiresApproval {
		if emitApprovalRequest != nil {
			return emitApprovalRequest(request)
		}
		return nil
	}

	if onAutoApproved != nil {
		return onAutoApproved(request)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
