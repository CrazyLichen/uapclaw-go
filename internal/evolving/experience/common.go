package experience

import (
	"context"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// MakePendingChange 构建暂存演进快照。
// 对应 Python: make_pending_change()
func MakePendingChange(
	skillName string,
	records []checkpointing.EvolutionRecord,
	requestIDPrefix *string,
	trajectory *trajectory.Trajectory,
	messages []map[string]any,
	isSharedRecords bool,
) *PendingChange {
	pending := checkpointing.NewPendingChange(skillName, records, trajectory, messages)
	pending.IsSharedRecords = isSharedRecords
	if requestIDPrefix != nil && *requestIDPrefix != "" {
		pending.ChangeID = fmt.Sprintf("%s_%s", *requestIDPrefix, generateShortID())
	}
	return pending
}

// RejectPendingChange 构建拒绝结果，不修改持久状态。
// 对应 Python: reject_pending_change()
func RejectPendingChange(pending *PendingChange) ExperienceApplyResult {
	return ExperienceApplyResult{
		SkillName:     pending.SkillName,
		RejectedCount: len(pending.Payload),
	}
}

// CommitPendingChange 持久化一条暂存变更，失败时保留未写入尾部。
// 对应 Python: commit_pending_change()
func CommitPendingChange(
	ctx context.Context,
	pendingByID map[string]*PendingChange,
	changeID string,
	store *checkpointing.EvolutionStore,
) (PendingCommitResult, error) {
	pending, ok := pendingByID[changeID]
	if !ok {
		return PendingCommitResult{}, fmt.Errorf("change_id %s 不存在", changeID)
	}

	if pending.ChangeType != schema.SkillExperienceEntry && pending.ChangeType != schema.ExperienceEntry {
		return PendingCommitResult{}, fmt.Errorf("change_type %s 不匹配", pending.ChangeType)
	}

	appliedCount := 0
	remainingRecords := pending.Payload

	for i, record := range pending.Payload {
		if err := store.AppendRecord(ctx, pending.SkillName, record); err != nil {
			// 对齐 Python: 任何一条失败时保留剩余记录
			remainingRecords = pending.Payload[i:]
			logger.Error(logger.ComponentAgentCore).
				Str("skill", pending.SkillName).
				Str("record_id", record.ID).
				Err(err).
				Msg("[experience.common] commit_pending_change: AppendRecord 失败")
			break
		}
		appliedCount++
		// 成功时逐渐缩减 remainingRecords
		remainingRecords = pending.Payload[i+1:]
	}

	// 对齐 Python: 全部成功时 remainingRecords 为空
	if appliedCount == len(pending.Payload) {
		remainingRecords = nil
	}

	pending.Payload = remainingRecords
	if len(remainingRecords) == 0 {
		pendingByID[changeID] = nil
		delete(pendingByID, changeID)
	}

	return PendingCommitResult{
		AppliedCount: appliedCount,
		PendingCount: len(remainingRecords),
	}, nil
}

// ExecuteSimplifyActions 执行经验库整理操作。
// 对应 Python: execute_simplify_actions()
func ExecuteSimplifyActions(
	ctx context.Context,
	store *checkpointing.EvolutionStore,
	skillName string,
	actions []map[string]any,
) map[string]int {
	counts := map[string]int{
		"deleted": 0,
		"merged":  0,
		"refined": 0,
		"kept":    0,
		"errors":  0,
	}

	for _, action := range actions {
		actionType := getStrFromAny(action, "action", "KEEP")
		recordID := getStrFromAny(action, "record_id", "")

		switch actionType {
		case "DELETE":
			deleted, err := store.DeleteRecords(ctx, skillName, []string{recordID})
			if err != nil {
				logger.Error(logger.ComponentAgentCore).
					Str("action", "DELETE").
					Str("record_id", recordID).
					Str("skill", skillName).
					Err(err).
					Msg("[experience.common] execute_simplify_actions 失败")
				counts["errors"]++
			} else if deleted > 0 {
				counts["deleted"]++
			} else {
				counts["errors"]++
			}

		case "MERGE":
			mergeRemoveIDs := getStrSliceFromAny(action, "merge_remove_ids")
			newContent := getStrFromAny(action, "new_content", "")
			result, err := store.MergeRecords(ctx, skillName, recordID, mergeRemoveIDs, newContent, nil)
			if err != nil {
				logger.Error(logger.ComponentAgentCore).
					Str("action", "MERGE").
					Str("record_id", recordID).
					Str("skill", skillName).
					Err(err).
					Msg("[experience.common] execute_simplify_actions 失败")
				counts["errors"]++
			} else if result != nil {
				counts["merged"]++
			} else {
				counts["errors"]++
			}

		case "REFINE":
			newContent := getStrFromAny(action, "new_content", "")
			newScore := getFloatPtrFromAny(action, "new_score")
			result, err := store.UpdateRecordContent(ctx, skillName, recordID, newContent, newScore)
			if err != nil {
				logger.Error(logger.ComponentAgentCore).
					Str("action", "REFINE").
					Str("record_id", recordID).
					Str("skill", skillName).
					Err(err).
					Msg("[experience.common] execute_simplify_actions 失败")
				counts["errors"]++
			} else if result != nil {
				counts["refined"]++
			} else {
				counts["errors"]++
			}

		case "KEEP":
			counts["kept"]++

		default:
			logger.Warn(logger.ComponentAgentCore).
				Str("action_type", actionType).
				Msg("[experience.common] 未知 action_type")
			counts["errors"]++
		}
	}

	logger.Info(logger.ComponentAgentCore).
		Str("skill", skillName).
		Int("deleted", counts["deleted"]).
		Int("merged", counts["merged"]).
		Int("refined", counts["refined"]).
		Int("kept", counts["kept"]).
		Int("errors", counts["errors"]).
		Msg("[experience.common] execute_simplify_actions 完成")

	return counts
}

// RequestRebuildContext 归档当前状态、过滤重建输入、构建重建提示词。
// 对应 Python: request_rebuild_context()
func RequestRebuildContext(
	ctx context.Context,
	store *checkpointing.EvolutionStore,
	request RebuildRequest,
	formatRecords func([]checkpointing.EvolutionRecord) string,
	defaultIntent string,
	template string,
	archiveEvolutionsOnSuccess bool,
) (map[string]any, error) {
	exists := store.SkillExists(ctx, request.SkillName)
	if !exists {
		return nil, nil
	}

	// 对齐 Python: archive_skill_body（忽略错误，只记录日志）
	_, _ = store.ArchiveSkillBody(ctx, request.SkillName)

	var evoArchive string
	var archiveError error

	evoArchiveResult, err := store.ArchiveEvolutions(ctx, request.SkillName)
	if err != nil {
		archiveError = err
		logger.Warn(logger.ComponentAgentCore).
			Str("skill", request.SkillName).
			Err(err).
			Msg("[experience.common] archive_evolutions 失败")
	} else {
		evoArchive = evoArchiveResult
	}

	recordsLog := store.LoadFullEvolutionLog(ctx, request.SkillName)
	filteredRecords := []checkpointing.EvolutionRecord{}
	for _, record := range recordsLog.Entries {
		if record.Score < request.MinScore {
			continue
		}
		if record.Change.SkipReason != nil && *record.Change.SkipReason != "" {
			continue
		}
		filteredRecords = append(filteredRecords, record)
	}

	userIntent := defaultIntent
	if request.UserIntent != nil && *request.UserIntent != "" {
		userIntent = *request.UserIntent
	}

	prompt := fmt.Sprintf(template,
		request.MinScore,
		formatRecords(filteredRecords),
		userIntent,
	)

	if archiveEvolutionsOnSuccess && evoArchive != "" {
		if err := store.ClearEvolutions(ctx, request.SkillName); err != nil {
			logger.Warn(logger.ComponentAgentCore).
				Str("skill", request.SkillName).
				Err(err).
				Msg("[experience.common] clear_evolutions 失败")
		}
	}

	return map[string]any{
		"skill_name":       request.SkillName,
		"records_log":      recordsLog,
		"filtered_records": filteredRecords,
		"prompt":           prompt,
		"archive_path":     evoArchive,
		"archive_error":    archiveError,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// generateShortID 生成短随机标识（8 位 hex）。
// 对齐 Python: uuid.uuid4().hex[:8]
func generateShortID() string {
	return fmt.Sprintf("%08x", time.Now().UTC().UnixNano()%0xFFFFFFFF)
}

// getStrFromAny 从 map[string]any 中获取字符串值。
func getStrFromAny(m map[string]any, key string, defaultVal string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

// getStrSliceFromAny 从 map[string]any 中获取字符串切片。
func getStrSliceFromAny(m map[string]any, key string) []string {
	if v, ok := m[key]; ok {
		if slice, ok := v.([]any); ok {
			result := make([]string, 0, len(slice))
			for _, item := range slice {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
		if slice, ok := v.([]string); ok {
			return slice
		}
	}
	return nil
}

// getFloatPtrFromAny 从 map[string]any 中获取 float64 指针。
func getFloatPtrFromAny(m map[string]any, key string) *float64 {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return &f
		}
	}
	return nil
}
