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

	if _, beginErr := lifecycle.Begin("session", sessionID, operationKindUnarchive, true); beginErr != nil {
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
	rootResolved := resolvePath(archivedRoot)
	items := make([]map[string]any, 0, len(entries))
	for _, name := range entries {
		directory := filepath.Join(archivedRoot, name)
		// 符号链接 / junction 不得把管理根之外的内容暴露成会话条目。
		// 权限错误在此逐条隔离，不能整批失败。
		if !listableArchivedDirectory(directory, rootResolved) {
			continue
		}
		meta, readErr := lifecycle.ReadJSON(filepath.Join(directory, "metadata.json"))
		if readErr != nil {
			meta = map[string]any{}
		}
		// 只读取值：列表路径不加会话锁、不 fsync
		value := lifecycle.State("session", name)
		items = append(items, map[string]any{
			"session_id":  name,
			"title":       meta["title"],
			"name":        meta["title"],
			"archived_at": listingArchivedAt(directory, meta, value),
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

// listableArchivedDirectory 归档根下的条目是否为根内普通目录。
// Python: session_archive.py _listable_archived_directory（:1317）
//
// 符号链接直接拒；junction（Windows 上 is_symlink 识别不到的 reparse point）
// 通过"解析后父目录 == 解析后的根"拒——与 session_paths 的逃逸防护同构。
// 权限错误向上抛，由调用方按条目隔离。
func listableArchivedDirectory(directory, rootResolved string) bool {
	info, err := os.Lstat(directory)
	if err != nil {
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if !info.IsDir() {
		return false
	}
	return filepath.Dir(resolvePath(directory)) == rootResolved
}

// listingArchivedAt 列表用的只读 archived_at 取值。
// Python: session_archive.py _listing_archived_at（:1359）
//
// 三级回退：元数据 → 操作记录 / 修复标记 → 目录 mtime。
// 请求路径不加会话资源锁、不做 fsync——那些属于搬移事务与启动回填。
func listingArchivedAt(directory string, meta, value map[string]any) float64 {
	if stamp := asFloatValue(meta["archived_at"]); stamp > 0 {
		return stamp
	}
	if operation, ok := value["operation"].(map[string]any); ok {
		if stamp := asFloatValue(operation["archived_at"]); stamp > 0 {
			return stamp
		}
	}
	if stamp := asFloatValue(value["archive_time_repair"]); stamp > 0 {
		return stamp
	}
	if info, err := os.Stat(directory); err == nil {
		return float64(info.ModTime().UnixNano()) / 1e9
	}
	return 0
}

// BackfillArchiveTimes 启动时一次性回填历史会话的 archived_at。
// Python: session_archive.py _backfill_archive_times（:1330）
//
// 列表只读解析 archived_at，因此字段出现之前归档的会话必须在这里补一次
// （加锁写入只属于这个迁移与搬移事务，绝不属于列表）。
func (s *Service) BackfillArchiveTimes() {
	_, archivedRoot := sessionRoots()
	entries, err := filepathGlob(archivedRoot)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("archived_at backfill scan failed")
		return
	}
	rootResolved := resolvePath(archivedRoot)
	for _, name := range entries {
		directory := filepath.Join(archivedRoot, name)
		if !listableArchivedDirectory(directory, rootResolved) {
			continue
		}
		meta, readErr := lifecycle.ReadJSON(filepath.Join(directory, "metadata.json"))
		if readErr != nil || len(meta) == 0 {
			continue
		}
		if stamp := asFloatValue(meta["archived_at"]); stamp > 0 {
			continue
		}
		meta["archived_at"] = s.archiveTime(name, directory, meta)
		if writeErr := lifecycle.AtomicJSON(
			filepath.Join(directory, "metadata.json"), meta); writeErr != nil {
			logger.Debug(logComponent).Err(writeErr).Str("session_id", name).
				Msg("archived_at backfill deferred")
		}
	}
}

// resolvePath 解析符号链接（失败时回退为 Clean 后的原路径）。
func resolvePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
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
