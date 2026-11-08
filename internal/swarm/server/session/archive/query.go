package archive

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// UnarchiveSession 把归档会话同步恢复回活动区。
// Python: session_archive.py _unarchive_session（:1029）
//
// 恢复是撤销操作，从不停运行时，因此同步完成、不进后台队列。
// 三种入口状态：
//  1. 有失败/放弃的归档残留 → 先关闭它再接管（否则永远无法取消归档）
//  2. 有正在跑的归档清理 → 拒绝（会与新操作抢目录）
//  3. 目录已在活动区 → 只需解围栏并恢复隐藏项目
func (s *Service) UnarchiveSession(sessionID, projectID string) (map[string]any, error) {
	release, err := s.lockWait("session", sessionID)
	if err != nil {
		return nil, err
	}
	defer release()

	active, archived, pathErr := lifecycle.SessionPaths(sessionID, "")
	if pathErr != nil {
		return nil, pathErr
	}
	previous, _ := lifecycle.State("session", sessionID)["operation"].(map[string]any)
	pending := previous != nil &&
		operationStatus(previous) != lifecycle.OperationStatusCompleted

	return s.unarchiveSession(sessionID, projectID, active, archived, previous, pending, false)
}

// ListSessions 列出归档会话（过滤 + 排序 + 分页）。
// Python: session_archive.py list_sessions
func (s *Service) ListSessions(params map[string]any) (map[string]any, error) {
	_, archivedRoot := sessionRoots()
	items, err := s.archivedEntries(archivedRoot)
	if err != nil {
		return map[string]any{
			"sessions": []map[string]any{}, "total": 0, "limit": 20,
			"offset": 0, "has_more": false,
		}, nil
	}
	return lifecycle.Page(items, params, "sessions", 200)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// unarchiveSession 取消归档的主体（调用方已持会话锁）。
func (s *Service) unarchiveSession(
	sessionID, projectID, active, archived string,
	previous map[string]any, pending bool, pinReindexRequired bool,
) (map[string]any, error) {
	tookOverFailedArchive := false
	if pending && previous != nil && operationKind(previous) == operationKindArchive {
		if isFailedOrAbandoned(previous) {
			// 失败的清理不能让会话变成"无法取消归档"：
			// 先关闭残留操作，再接管
			if _, abandonErr := lifecycle.AbandonFailedOperation(
				"session", sessionID, operationKindArchive); abandonErr != nil {
				return nil, abandonErr
			}
			previous, _ = lifecycle.State("session", sessionID)["operation"].(map[string]any)
			pending = previous != nil &&
				operationStatus(previous) != lifecycle.OperationStatusCompleted
			tookOverFailedArchive = true
		} else if operationStatus(previous) != lifecycle.OperationStatusCompleted {
			return nil, &lifecycle.LifecycleError{
				Code: "OPERATION_IN_PROGRESS", Message: "session archive cleanup is still running"}
		}
	}

	// 目录已在活动区：可能是被放弃的归档留下的，此时项目可能还是隐藏的。
	// 取消归档是恢复动作，必须把项目也带回来，否则会话离开归档页后
	// 在工作区里依然找不到。
	if !pending && pathExists(active) {
		s.restoreHiddenProject(projectID)
		if visErr := lifecycle.SetVisibility("session", sessionID, false, nil, false); visErr != nil {
			return nil, visErr
		}
		return map[string]any{
			"session_id": sessionID,
			"ok":         true,
			"restored":   tookOverFailedArchive,
			"project_id": projectID,
		}, nil
	}

	operation, beginErr := lifecycle.Begin("session", sessionID, operationKindUnarchive, true)
	if beginErr != nil {
		return nil, beginErr
	}
	operation, claimErr := lifecycle.ClaimOperation("session", sessionID, s.ownerID)
	if claimErr != nil {
		return nil, claimErr
	}
	if stored, ok := operation["pin_reindex_required"].(bool); ok && stored {
		pinReindexRequired = true
	}
	if _, updateErr := lifecycle.Update("session", sessionID, map[string]any{
		"project_id":           projectID,
		"pin_reindex_required": pinReindexRequired,
	}); updateErr != nil {
		return nil, updateErr
	}

	// 项目被移除时，归档区是它唯一还能被看到的内容。先把项目恢复出来，
	// 否则会话离开归档页后在工作区里也找不到。
	s.restoreHiddenProject(projectID)

	if _, updateErr := lifecycle.Update("session", sessionID, map[string]any{
		"phase":  "move_directory",
		"status": lifecycle.OperationStatusRunning,
	}); updateErr != nil {
		return nil, updateErr
	}
	if moveErr := s.MoveSessionDirectory(sessionID, archived, active, 0); moveErr != nil {
		s.recordFinalizeFailure(sessionID, operationKindUnarchive, 1, moveErr,
			stringOf(operation, "operation_id"))
		return nil, moveErr
	}

	s.mu.Lock()
	reindex := s.reindexPins
	s.mu.Unlock()
	if pinReindexRequired && reindex != nil {
		if reindexErr := reindex(); reindexErr != nil {
			logger.Warn(logComponent).Err(reindexErr).Str("session_id", sessionID).
				Msg("session unarchive could not reindex pins")
		}
	}
	// 物理搬移已成功：无论后续步骤如何，会话都必须可见
	if visErr := lifecycle.SetVisibility("session", sessionID, false, nil, false); visErr != nil {
		return nil, visErr
	}
	payload := map[string]any{
		"session_id": sessionID,
		"ok":         true,
		"restored":   tookOverFailedArchive,
		"project_id": projectID,
	}
	if completeErr := lifecycle.Complete("session", sessionID, false, false, payload); completeErr != nil {
		return nil, completeErr
	}
	return payload, nil
}

// restoreHiddenProject 取消项目隐藏。
// Python: session_archive.py _restore_hidden_project
func (s *Service) restoreHiddenProject(projectID string) {
	if strings.TrimSpace(projectID) == "" {
		return
	}
	s.mu.Lock()
	restorer := s.projectRestorer
	s.mu.Unlock()
	if restorer == nil {
		return
	}
	if err := restorer.RestoreHiddenProject(projectID); err != nil {
		logger.Warn(logComponent).Err(err).Str("project_id", projectID).
			Msg("session unarchive could not restore hidden project")
	}
}

// archivedEntries 枚举归档区会话条目。
// Python: session_archive.py _archived_entries
func (s *Service) archivedEntries(archivedRoot string) ([]map[string]any, error) {
	entries, err := filepathGlob(archivedRoot)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(entries))
	for _, name := range entries {
		meta, readErr := lifecycle.ReadJSON(filepath.Join(archivedRoot, name, "metadata.json"))
		if readErr != nil {
			meta = map[string]any{}
		}
		items = append(items, map[string]any{
			"session_id":  name,
			"title":       meta["title"],
			"name":        meta["title"],
			"archived_at": asFloatValue(meta["archived_at"]),
			"work_mode":   meta["work_mode"],
			"project_id":  lifecycle.ProjectIDFor(meta, nil),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		leftID, _ := items[i]["session_id"].(string)
		rightID, _ := items[j]["session_id"].(string)
		return leftID < rightID
	})
	return items, nil
}

// filepathGlob 列出目录下的子目录名（不存在返回空）。
// Python: session_archive.py _archived_entries 的 root.iterdir()
func filepathGlob(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}
