// Package server 提供会话消息邮箱的持久化存储（跨会话 Agent 消息信箱）。
//
// 对应 Python 代码：jiuwenswarm/server/runtime/session/session_message_store.py（971 行）。
// Python 侧 normalize_journal_mode 位于 jiuwenswarm/common/sqlite_config.py:8-12，
// Go 侧因仅本文件消费，随本文件落（一文件对一文件的例外已登记 spec §5）。
package server

// ──────────────────────────── 导入 ────────────────────────────

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// driverName SQLite 驱动（仓库统一 mattn/go-sqlite3）
	driverName = "sqlite3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionMessageStoreError 信箱存储基础错误。
// Python: session_message_store.py:24 SessionMessageStoreError
type SessionMessageStoreError struct {
	// Msg 错误描述
	Msg string
}

// Error 实现 error 接口。
func (e *SessionMessageStoreError) Error() string { return e.Msg }

// SessionMessageLimitExceeded 信箱或链路容量超限。
// Python: session_message_store.py:27 SessionMessageLimitExceeded
type SessionMessageLimitExceeded struct{ SessionMessageStoreError }

// SessionMessageIdempotencyConflict 同一幂等键以不同参数复用。
// Python: session_message_store.py:30 SessionMessageIdempotencyConflict
type SessionMessageIdempotencyConflict struct{ SessionMessageStoreError }

// SessionMessageRecord 一条持久的跨会话消息。
// Python: session_message_store.py:38 SessionMessageRecord（frozen dataclass，全字段）
type SessionMessageRecord struct {
	// Sequence 自增序号
	Sequence int64 `json:"sequence"`
	// MessageID 消息标识
	MessageID string `json:"message_id"`
	// OwnerScopeID 归属用户域
	OwnerScopeID string `json:"owner_scope_id"`
	// SourceSessionID 发送会话
	SourceSessionID string `json:"source_session_id"`
	// SourceTitleSnapshot 源会话标题快照
	SourceTitleSnapshot string `json:"source_title_snapshot"`
	// SourceRequestID 源请求标识
	SourceRequestID string `json:"source_request_id"`
	// SourceToolCallID 源工具调用标识
	SourceToolCallID string `json:"source_tool_call_id"`
	// TargetSessionID 目标会话
	TargetSessionID string `json:"target_session_id"`
	// Content 消息内容
	Content string `json:"content"`
	// ChainID 跨会话链路标识
	ChainID string `json:"chain_id"`
	// ParentMessageID 父消息标识
	ParentMessageID string `json:"parent_message_id"`
	// HopCount 转发深度
	HopCount int `json:"hop_count"`
	// Status 状态
	Status string `json:"status"`
	// ExecutionRequestID 执行请求标识（claim 写入）
	ExecutionRequestID string `json:"execution_request_id"`
	// RuntimeRunID 投递运行标识
	RuntimeRunID string `json:"runtime_run_id"`
	// InterruptRequestID HITL 交互标识
	InterruptRequestID string `json:"interrupt_request_id"`
	// InterruptSource HITL 交互来源
	InterruptSource string `json:"interrupt_source"`
	// CreatedAt 落库时间（epoch 秒）
	CreatedAt float64 `json:"created_at"`
	// StartedAt 开始执行时间
	StartedAt *float64 `json:"started_at"`
	// FinishedAt 终态时间
	FinishedAt *float64 `json:"finished_at"`
	// UpdatedAt 最近更新时间
	UpdatedAt float64 `json:"updated_at"`
	// LastErrorCode 最近错误码
	LastErrorCode string `json:"last_error_code"`
	// LastError 最近错误描述
	LastError string `json:"last_error"`
	// ResolvedAt 未知态裁决时间
	ResolvedAt *float64 `json:"resolved_at"`
	// Resolution 未知态裁决结论
	Resolution string `json:"resolution"`
	// RetryOf 重试源消息标识
	RetryOf string `json:"retry_of"`
	// InputMode 生效投递模式（""/steer；defer 后被改写）
	InputMode string `json:"input_mode"`
	// QueueReleasedAt 排队闸门释放时间
	QueueReleasedAt *float64 `json:"queue_released_at"`
	// RequestedInputMode 请求时原始模式
	RequestedInputMode string `json:"requested_input_mode"`
}

// SessionMessageStore SQLite 持久化的会话信箱，带事务性 claim 与去重。
// Python: session_message_store.py:75 SessionMessageStore
type SessionMessageStore struct {
	// path 数据库文件路径
	path string
	// journalMode WAL / DELETE
	journalMode string
	// schemaReady ensure_schema 一次性守卫
	schemaReady bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// StatusQueued 已排队
	StatusQueued = "queued"
	// StatusRunning 执行中
	StatusRunning = "running"
	// StatusWaitingUser 等待用户应答
	StatusWaitingUser = "waiting_user"
	// StatusUnknown 结果未知
	StatusUnknown = "unknown"
	// StatusSucceeded 已成功
	StatusSucceeded = "succeeded"
	// StatusFailed 已失败
	StatusFailed = "failed"
	// StatusCancelled 已取消
	StatusCancelled = "cancelled"
	// StatusDelivered 已投递
	StatusDelivered = "delivered"
)

// sessionMessageActiveStatuses 活跃状态集合。
// Python: session_message_store.py:17 ACTIVE_SESSION_MESSAGE_STATUSES
var sessionMessageActiveStatuses = []string{
	StatusQueued, StatusRunning, StatusWaitingUser, StatusUnknown,
}

// sessionMessageTerminalStatuses 终态集合。
// Python: session_message_store.py:18 TERMINAL_SESSION_MESSAGE_STATUSES
var sessionMessageTerminalStatuses = []string{
	StatusSucceeded, StatusFailed, StatusCancelled, StatusDelivered,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionMessageStore 创建信箱存储。
// Python: session_message_store.py:78 __init__
func NewSessionMessageStore(path string, journalMode string) *SessionMessageStore {
	return &SessionMessageStore{
		path:        path,
		journalMode: normalizeJournalMode(journalMode),
	}
}

// Exists 判断数据库文件是否已存在。
// Python: session_message_store.py:83 exists
func (s *SessionMessageStore) Exists() bool {
	return fileExists(s.path)
}

// Get 按消息标识取记录。
// Python: session_message_store.py:370 get
func (s *SessionMessageStore) Get(messageID string) (*SessionMessageRecord, error) {
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return scanRecord(db.QueryRow(
		"SELECT "+sessionMessageColumns+" FROM session_messages WHERE message_id = ?", messageID))
}

// QueuedTargets 列出存在 queued 消息的目标会话（崩溃恢复补扫）。
// Python: session_message_store.py:380 queued_targets
func (s *SessionMessageStore) QueuedTargets() ([]string, error) {
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(
		"SELECT DISTINCT target_session_id FROM session_messages " +
			"WHERE status = 'queued' ORDER BY target_session_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var targets []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

// QueuedForTarget 目标会话的 queued 消息（FIFO 序）。
// Python: session_message_store.py:391 queued_for_target
func (s *SessionMessageStore) QueuedForTarget(ownerScopeID, targetSessionID string) ([]*SessionMessageRecord, error) {
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(
		"SELECT "+sessionMessageColumns+" FROM session_messages "+
			"WHERE owner_scope_id = ? AND target_session_id = ? AND status = 'queued' "+
			"ORDER BY sequence", ownerScopeID, targetSessionID)
	if err != nil {
		return nil, err
	}
	return scanRecords(rows)
}

// ContinueQueuedForTarget 仅对存在 queued 工作的投递道释放不确定阻塞。
// Python: session_message_store.py:407 continue_queued_for_target
func (s *SessionMessageStore) ContinueQueuedForTarget(ownerScopeID, targetSessionID string) (int, error) {
	if err := s.EnsureSchema(); err != nil {
		return 0, err
	}
	db, err := s.open()
	if err != nil {
		return 0, err
	}
	defer db.Close()
	now := nowFloat()
	res, err := db.Exec(
		"UPDATE session_messages AS blocked "+
			"SET queue_released_at = ?, updated_at = ? "+
			"WHERE owner_scope_id = ? AND target_session_id = ? "+
			"AND status = 'unknown' AND resolved_at IS NULL AND queue_released_at IS NULL "+
			"AND EXISTS (SELECT 1 FROM session_messages AS queued "+
			"WHERE queued.owner_scope_id = blocked.owner_scope_id "+
			"AND queued.target_session_id = blocked.target_session_id "+
			"AND queued.input_mode = blocked.input_mode "+
			"AND queued.status = 'queued')",
		now, now, ownerScopeID, targetSessionID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// NextQueued 返回同一信箱单个投递道的 FIFO 队头。
// Python: session_message_store.py:434 next_queued
// steer 必须能到达运行中的目标（即使普通消息在等准入）；
// 不确定投递仍阻塞自己的道；defer_steering 在同事务内先归一化再取队头。
func (s *SessionMessageStore) NextQueued(targetSessionID, inputMode string, deferSteering bool) (*SessionMessageRecord, error) {
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if deferSteering && inputMode == "" {
		// 在取 task 队头前归一化（即使 steer worker 尚未跑）；
		// 保留不确定投递的屏障。
		// Python: next_queued :451-463
		if _, err := tx.Exec(
			"UPDATE session_messages SET input_mode = '', updated_at = ? "+
				"WHERE target_session_id = ? AND input_mode = 'steer' AND status = 'queued' "+
				"AND NOT EXISTS (SELECT 1 FROM session_messages "+
				"WHERE target_session_id = ? AND input_mode = 'steer' "+
				"AND (status = 'waiting_user' OR (status = 'unknown' "+
				"AND resolved_at IS NULL AND queue_released_at IS NULL)))",
			nowFloat(), targetSessionID, targetSessionID); err != nil {
			return nil, err
		}
	}
	// 屏障：running/waiting_user 阻塞（steer+running 例外——service 串行化 steer 回执）；
	// 未裁决的 unknown 也阻塞。
	// Python: next_queued :465-478
	var blocker int
	err = tx.QueryRow(
		"SELECT 1 FROM session_messages "+
			"WHERE target_session_id = ? AND input_mode = ? "+
			"AND ((status IN ('running', 'waiting_user') "+
			"AND NOT (input_mode = 'steer' AND status = 'running')) "+
			"OR (status = 'unknown' AND resolved_at IS NULL "+
			"AND queue_released_at IS NULL)) LIMIT 1",
		targetSessionID, inputMode).Scan(&blocker)
	if err == nil {
		// 有屏障 → 返回 nil
		return nil, nil
	}
	if err.Error() != "sql: no rows in result set" {
		return nil, err
	}
	rec, err := scanRecord(tx.QueryRow(
		"SELECT "+sessionMessageColumns+" FROM session_messages "+
			"WHERE target_session_id = ? AND status = 'queued' AND input_mode = ? "+
			"ORDER BY sequence LIMIT 1", targetSessionID, inputMode))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return rec, nil
}

// WaitingForTarget 目标会话的第一条 waiting_user 记录。
// Python: session_message_store.py:494 waiting_for_target
func (s *SessionMessageStore) WaitingForTarget(targetSessionID string) (*SessionMessageRecord, error) {
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rec, err := scanRecord(db.QueryRow(
		"SELECT "+sessionMessageColumns+" FROM session_messages "+
			"WHERE target_session_id = ? AND status = 'waiting_user' "+
			"ORDER BY sequence LIMIT 1", targetSessionID))
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// WaitingForPause 返回问题属于某次暂停交互集合的 waiting 记录。
// Python: session_message_store.py:507 waiting_for_pause
func (s *SessionMessageStore) WaitingForPause(targetSessionID string, interactionIDs []string) (*SessionMessageRecord, error) {
	ids := sortedUnique(interactionIDs)
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	query := "SELECT " + sessionMessageColumns + " FROM session_messages " +
		"WHERE target_session_id = ? AND status = 'waiting_user' " +
		"AND interrupt_request_id IN (" + placeholders(len(ids)) + ") " +
		"ORDER BY sequence LIMIT 1"
	args := []any{targetSessionID}
	for _, id := range ids {
		args = append(args, id)
	}
	rec, err := scanRecord(db.QueryRow(query, args...))
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// SupersedeWaitingForTarget 将用户旁路（普通轮开启）的 waiting 记录置 failed。
// Python: session_message_store.py:528 supersede_waiting_for_target
// 仅触碰 waiting_user 行；queued 后继保持顺序。
func (s *SessionMessageStore) SupersedeWaitingForTarget(targetSessionID string) ([]*SessionMessageRecord, error) {
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(
		"SELECT message_id FROM session_messages "+
			"WHERE target_session_id = ? AND status = 'waiting_user' ORDER BY sequence",
		targetSessionID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, tx.Commit()
	}
	now := nowFloat()
	update := "UPDATE session_messages " +
		"SET status = 'failed', finished_at = ?, updated_at = ?, " +
		"last_error_code = 'SUPERSEDED_BY_USER_TURN', last_error = ? " +
		"WHERE message_id IN (" + placeholders(len(ids)) + ") AND status = 'waiting_user'"
	args := []any{now, now, "target user started a new turn before answering " +
		"the cross-session question"}
	for _, id := range ids {
		args = append(args, id)
	}
	if _, err := tx.Exec(update, args...); err != nil {
		return nil, err
	}
	selectQ := "SELECT " + sessionMessageColumns + " FROM session_messages " +
		"WHERE message_id IN (" + placeholders(len(ids)) + ") AND status = 'failed' " +
		"ORDER BY sequence"
	sargs := make([]any, 0, len(ids))
	for _, id := range ids {
		sargs = append(sargs, id)
	}
	srows, err := tx.Query(selectQ, sargs...)
	if err != nil {
		return nil, err
	}
	records, err := scanRecords(srows)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return records, nil
}

// DeferSteering 把未提交的 steer 移入 task 队列且不改变顺序。
// Python: session_message_store.py:582 defer_steering
func (s *SessionMessageStore) DeferSteering(messageID string) (*SessionMessageRecord, error) {
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	res, err := db.Exec(
		"UPDATE session_messages "+
			"SET input_mode = '', status = 'queued', execution_request_id = '', "+
			"runtime_run_id = '', started_at = NULL, updated_at = ? "+
			"WHERE message_id = ? AND input_mode = 'steer' "+
			"AND status IN ('queued', 'running')",
		nowFloat(), messageID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, nil
	}
	rec, err := s.Get(messageID)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// Claim 乐观占用：仅 status='queued' 时写执行标识并置 running。
// Python: session_message_store.py:602 claim
func (s *SessionMessageStore) Claim(messageID, executionRequestID, runtimeRunID string, expectedInputMode *string) (*SessionMessageRecord, error) {
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	now := nowFloat()
	var res sql.Result
	if expectedInputMode == nil {
		res, err = db.Exec(
			"UPDATE session_messages "+
				"SET status = 'running', execution_request_id = ?, runtime_run_id = ?, "+
				"started_at = ?, updated_at = ? "+
				"WHERE message_id = ? AND status = 'queued'",
			executionRequestID, runtimeRunID, now, now, messageID)
	} else {
		res, err = db.Exec(
			"UPDATE session_messages "+
				"SET status = 'running', execution_request_id = ?, runtime_run_id = ?, "+
				"started_at = ?, updated_at = ? "+
				"WHERE message_id = ? AND status = 'queued' AND input_mode = ?",
			executionRequestID, runtimeRunID, now, now, messageID, *expectedInputMode)
	}
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, nil
	}
	rec, err := s.Get(messageID)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// TransitionStatus 仅从 expected_statuses 之一原子更新状态。
// Python: session_message_store.py:631 transition_status
func (s *SessionMessageStore) TransitionStatus(messageID, status string, expectedStatuses []string, errorCode, errorText string) (*SessionMessageRecord, error) {
	allowed := make(map[string]bool)
	for _, st := range append(append([]string{}, sessionMessageActiveStatuses...), sessionMessageTerminalStatuses...) {
		allowed[st] = true
	}
	if !allowed[status] {
		return nil, &SessionMessageStoreError{Msg: "unsupported session message status: " + status}
	}
	if len(expectedStatuses) == 0 {
		return nil, &SessionMessageStoreError{Msg: "expected_statuses must contain supported statuses"}
	}
	for _, item := range expectedStatuses {
		if !allowed[item] {
			return nil, &SessionMessageStoreError{Msg: "expected_statuses must contain supported statuses"}
		}
	}
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	now := nowFloat()
	terminal := map[string]bool{}
	for _, st := range sessionMessageTerminalStatuses {
		terminal[st] = true
	}
	var finishedAt any
	if terminal[status] || status == StatusUnknown {
		finishedAt = now
	}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	res, err := db.Exec(
		"UPDATE session_messages "+
			"SET status = ?, finished_at = ?, updated_at = ?, last_error_code = ?, last_error = ? "+
			"WHERE message_id = ? AND status IN ("+placeholders(len(expectedStatuses))+")",
		append([]any{status, finishedAt, now, errorCode, errorText, messageID},
			toAnySlice(expectedStatuses)...)...)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, nil
	}
	rec, err := s.Get(messageID)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// MarkWaiting 在把 HITL 身份暴露给客户端前精确落库。
// Python: session_message_store.py:675 mark_waiting
func (s *SessionMessageStore) MarkWaiting(messageID, interruptRequestID, interruptSource string) (*SessionMessageRecord, error) {
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	res, err := db.Exec(
		"UPDATE session_messages "+
			"SET status = 'waiting_user', updated_at = ?, "+
			"interrupt_request_id = ?, interrupt_source = ?, "+
			"finished_at = NULL, last_error_code = '', last_error = '' "+
			"WHERE message_id = ? AND status = 'running'",
		nowFloat(), interruptRequestID, interruptSource, messageID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, nil
	}
	rec, err := s.Get(messageID)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// ReplaceWaitingCorrelation 把已恢复项移到下一个精确 HITL 问题。
// Python: session_message_store.py:702 replace_waiting_correlation
func (s *SessionMessageStore) ReplaceWaitingCorrelation(messageID, interruptRequestID, interruptSource string) (*SessionMessageRecord, error) {
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	res, err := db.Exec(
		"UPDATE session_messages "+
			"SET updated_at = ?, interrupt_request_id = ?, interrupt_source = ?, "+
			"last_error_code = '', last_error = '' "+
			"WHERE message_id = ? AND status = 'waiting_user'",
		nowFloat(), interruptRequestID, interruptSource, messageID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, nil
	}
	rec, err := s.Get(messageID)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// ResolveUnknown 裁决不确定结果或释放其后继。
// Python: session_message_store.py:729 resolve_unknown
func (s *SessionMessageStore) ResolveUnknown(messageID, ownerScopeID, participantSessionID, resolution string) (*SessionMessageRecord, error) {
	if resolution != StatusSucceeded && resolution != StatusCancelled && resolution != "continue_queued" {
		return nil, &SessionMessageStoreError{Msg: "unsupported unknown-message resolution"}
	}
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	now := nowFloat()
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if resolution == "continue_queued" {
		// 释放排队闸门：COALESCE 保留既有释放时间
		if _, err := db.Exec(
			"UPDATE session_messages "+
				"SET queue_released_at = COALESCE(queue_released_at, ?), updated_at = ? "+
				"WHERE message_id = ? AND owner_scope_id = ? "+
				"AND (source_session_id = ? OR target_session_id = ?) "+
				"AND status = 'unknown' AND resolved_at IS NULL",
			now, now, messageID, ownerScopeID, participantSessionID, participantSessionID); err != nil {
			return nil, err
		}
		rec, err := scanRecord(db.QueryRow(
			"SELECT "+sessionMessageColumns+" FROM session_messages "+
				"WHERE message_id = ? AND owner_scope_id = ? "+
				"AND (source_session_id = ? OR target_session_id = ?) "+
				"AND status = 'unknown' AND resolved_at IS NULL",
			messageID, ownerScopeID, participantSessionID, participantSessionID))
		if err != nil {
			return nil, err
		}
		return rec, nil
	}
	storedResolution := resolution + "_by_user"
	errorCode := ""
	errorText := ""
	if resolution != StatusSucceeded {
		errorCode = "UNKNOWN_CANCELLED"
		errorText = "uncertain execution cancelled by user"
	}
	res, err := db.Exec(
		"UPDATE session_messages "+
			"SET status = ?, finished_at = ?, updated_at = ?, resolved_at = ?, "+
			"resolution = ?, last_error_code = ?, last_error = ? "+
			"WHERE message_id = ? AND owner_scope_id = ? "+
			"AND (source_session_id = ? OR target_session_id = ?) "+
			"AND status = 'unknown' AND resolved_at IS NULL",
		resolution, now, now, now, storedResolution, errorCode, errorText,
		messageID, ownerScopeID, participantSessionID, participantSessionID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, nil
	}
	rec, err := s.Get(messageID)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// RecoverInflightAsUnknown 宿主重启后保守隔离结果不确定的在途工作。
// Python: session_message_store.py:793 recover_inflight_as_unknown
func (s *SessionMessageStore) RecoverInflightAsUnknown() (int, error) {
	if err := s.EnsureSchema(); err != nil {
		return 0, err
	}
	db, err := s.open()
	if err != nil {
		return 0, err
	}
	defer db.Close()
	now := nowFloat()
	res, err := db.Exec(
		"UPDATE session_messages "+
			"SET status = 'unknown', finished_at = ?, updated_at = ?, "+
			"last_error_code = 'HOST_RESTARTED', "+
			"last_error = 'execution outcome is unknown after host restart' "+
			"WHERE status IN ('running', 'waiting_user')",
		now, now)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// CancelPendingForTarget 目标删除后取消无法继续的记录（内容清空）。
// Python: session_message_store.py:806 cancel_pending_for_target
func (s *SessionMessageStore) CancelPendingForTarget(targetSessionID string) ([]*SessionMessageRecord, error) {
	return s.CancelPendingForTargetWithReason(targetSessionID, "TARGET_DELETED", "target Session was deleted")
}

// CancelPendingForTargetWithReason 带自定义错误码/描述的删除取消。
// Python: cancel_pending_for_target 的 error_code/error 关键字参数
func (s *SessionMessageStore) CancelPendingForTargetWithReason(targetSessionID, errorCode, errorText string) ([]*SessionMessageRecord, error) {
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(
		"SELECT message_id FROM session_messages "+
			"WHERE target_session_id = ? "+
			"AND status IN ('queued', 'running', 'waiting_user', 'unknown') "+
			"ORDER BY sequence", targetSessionID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	now := nowFloat()
	if _, err := tx.Exec(
		"UPDATE session_messages "+
			"SET status = 'cancelled', finished_at = ?, updated_at = ?, "+
			"last_error_code = ?, last_error = ?, content = '', "+
			"resolved_at = CASE WHEN status = 'unknown' THEN ? ELSE resolved_at END, "+
			"resolution = CASE WHEN status = 'unknown' THEN 'target_deleted' ELSE resolution END "+
			"WHERE target_session_id = ? "+
			"AND status IN ('queued', 'running', 'waiting_user', 'unknown')",
		now, now, errorCode, errorText, now, targetSessionID); err != nil {
		return nil, err
	}
	// 第二条：无条件清空目标会话全部内容（Python :855-856）
	if _, err := tx.Exec(
		"UPDATE session_messages SET content = '' WHERE target_session_id = ?",
		targetSessionID); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	selectQ := "SELECT " + sessionMessageColumns + " FROM session_messages " +
		"WHERE message_id IN (" + placeholders(len(ids)) + ") ORDER BY sequence"
	sargs := make([]any, 0, len(ids))
	for _, id := range ids {
		sargs = append(sargs, id)
	}
	srows, err := tx.Query(selectQ, sargs...)
	if err != nil {
		return nil, err
	}
	records, err := scanRecords(srows)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return records, nil
}

// PendingCounts 目标会话的活跃（未终态）消息计数。
// Python: session_message_store.py:867 pending_counts
func (s *SessionMessageStore) PendingCounts(targetSessionIDs []string) (map[string]int, error) {
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	if len(targetSessionIDs) == 0 {
		return map[string]int{}, nil
	}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	statuses := append([]string{}, sessionMessageActiveStatuses...)
	query := "SELECT target_session_id, COUNT(*) FROM session_messages " +
		"WHERE target_session_id IN (" + placeholders(len(targetSessionIDs)) + ") " +
		"AND status IN (" + placeholders(len(statuses)) + ") " +
		"AND (status != 'unknown' OR (resolved_at IS NULL AND queue_released_at IS NULL)) " +
		"GROUP BY target_session_id"
	args := make([]any, 0, len(targetSessionIDs)+len(statuses))
	for _, id := range targetSessionIDs {
		args = append(args, id)
	}
	for _, st := range statuses {
		args = append(args, st)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var sid string
		var cnt int
		if err := rows.Scan(&sid, &cnt); err != nil {
			return nil, err
		}
		out[sid] = cnt
	}
	return out, rows.Err()
}

// BlockingStates 会话列表投影用的持久 waiting/unknown 状态。
// Python: session_message_store.py:897 blocking_states
func (s *SessionMessageStore) BlockingStates(targetSessionIDs []string) (map[string]string, error) {
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	if len(targetSessionIDs) == 0 {
		return map[string]string{}, nil
	}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(
		"SELECT target_session_id, status FROM session_messages "+
			"WHERE target_session_id IN ("+placeholders(len(targetSessionIDs))+") "+
			"AND (status = 'waiting_user' OR (status = 'unknown' "+
			"AND resolved_at IS NULL AND queue_released_at IS NULL)) "+
			"ORDER BY sequence", toAnySlice(targetSessionIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := map[string]string{}
	for rows.Next() {
		var sid, status string
		if err := rows.Scan(&sid, &status); err != nil {
			return nil, err
		}
		// unknown 优先；同一会话取第一条非 unknown
		if status == StatusUnknown || states[sid] == "" {
			states[sid] = status
		}
	}
	return states, rows.Err()
}

// ListMessages 会话可见的信箱记录（发送或接收，倒序分页）。
// Python: session_message_store.py:925 list_messages
func (s *SessionMessageStore) ListMessages(ownerScopeID, sessionID string, limit, offset int) ([]*SessionMessageRecord, error) {
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(
		"SELECT "+sessionMessageColumns+" FROM session_messages "+
			"WHERE owner_scope_id = ? AND (source_session_id = ? OR target_session_id = ?) "+
			"ORDER BY sequence DESC LIMIT ? OFFSET ?",
		ownerScopeID, sessionID, sessionID, limit, offset)
	if err != nil {
		return nil, err
	}
	return scanRecords(rows)
}

// PendingTargetsForSession 即使新完成消息填满一页也能找到未决工作。
// Python: session_message_store.py:945 pending_targets_for_session
func (s *SessionMessageStore) PendingTargetsForSession(ownerScopeID, sessionID string, limit int) ([]string, error) {
	if err := s.EnsureSchema(); err != nil {
		return nil, err
	}
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(
		"SELECT target_session_id FROM session_messages "+
			"WHERE owner_scope_id = ? AND (source_session_id = ? OR target_session_id = ?) "+
			"AND (status IN ('queued', 'running', 'waiting_user') "+
			"OR (status = 'unknown' AND resolved_at IS NULL AND queue_released_at IS NULL)) "+
			"GROUP BY target_session_id ORDER BY MIN(sequence) LIMIT ?",
		ownerScopeID, sessionID, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// EnsureSchema 建表与索引（一次性守卫）。
// Python: session_message_store.py:107 ensure_schema
func (s *SessionMessageStore) EnsureSchema() error {
	if s.schemaReady {
		return nil
	}
	db, err := s.open()
	if err != nil {
		return err
	}
	defer db.Close()
	// 校验 journal mode（Python :112-115：actual != expected → OperationalError）
	var actual string
	if err := db.QueryRow("PRAGMA journal_mode=" + s.journalMode).Scan(&actual); err != nil {
		return err
	}
	if upper(actual) != s.journalMode {
		return &SessionMessageStoreError{Msg: "could not select SQLite journal mode"}
	}
	script := `
CREATE TABLE IF NOT EXISTS session_messages (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id TEXT NOT NULL UNIQUE,
    owner_scope_id TEXT NOT NULL,
    source_session_id TEXT NOT NULL,
    source_title_snapshot TEXT NOT NULL DEFAULT '',
    source_request_id TEXT NOT NULL,
    source_tool_call_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    target_session_id TEXT NOT NULL,
    content TEXT NOT NULL,
    chain_id TEXT NOT NULL,
    parent_message_id TEXT NOT NULL DEFAULT '',
    hop_count INTEGER NOT NULL,
    status TEXT NOT NULL,
    execution_request_id TEXT NOT NULL DEFAULT '',
    runtime_run_id TEXT NOT NULL DEFAULT '',
    interrupt_request_id TEXT NOT NULL DEFAULT '',
    interrupt_source TEXT NOT NULL DEFAULT '',
    created_at REAL NOT NULL,
    started_at REAL,
    finished_at REAL,
    updated_at REAL NOT NULL,
    last_error_code TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    resolved_at REAL,
    resolution TEXT NOT NULL DEFAULT '',
    retry_of TEXT NOT NULL DEFAULT '',
    queue_released_at REAL,
    input_mode TEXT NOT NULL DEFAULT '',
    requested_input_mode TEXT NOT NULL DEFAULT '',
    CHECK (hop_count >= 1)
);
CREATE INDEX IF NOT EXISTS idx_session_messages_target_queue
    ON session_messages(target_session_id, status, sequence);
CREATE INDEX IF NOT EXISTS idx_session_messages_owner_created
    ON session_messages(owner_scope_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_session_messages_chain
    ON session_messages(chain_id, sequence);
`
	if _, err := db.Exec(script); err != nil {
		return err
	}
	s.schemaReady = true
	return nil
}

// Enqueue 提交一条消息，返回 (record, created)。
// Python: session_message_store.py:247 enqueue
// 幂等键同参数复用 → 返回既有记录 created=false；
// 同键不同参数 → IdempotencyConflict；目标信箱满/链路满 → LimitExceeded。
func (s *SessionMessageStore) Enqueue(params SessionMessageEnqueueParams) (*SessionMessageRecord, bool, error) {
	if params.InputMode != "" && params.InputMode != "steer" {
		return nil, false, &SessionMessageStoreError{Msg: "unsupported cross-Session input mode"}
	}
	deliveryMode := params.InputMode
	if params.EffectiveInputMode != nil {
		deliveryMode = *params.EffectiveInputMode
	}
	if deliveryMode != "" && deliveryMode != params.InputMode {
		return nil, false, &SessionMessageStoreError{Msg: "unsupported effective cross-Session input mode"}
	}
	if err := s.EnsureSchema(); err != nil {
		return nil, false, err
	}
	now := nowFloat()
	messageID := "sm_" + newUUIDHex()
	effectiveChainID := params.ChainID
	if effectiveChainID == "" {
		effectiveChainID = messageID
	}
	// Python: with self._session() as conn, conn: BEGIN IMMEDIATE
	db, err := s.openImmediate()
	if err != nil {
		return nil, false, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	// 幂等检查（Python :279-303）
	existing, err := scanRecord(tx.QueryRow(
		"SELECT "+sessionMessageColumns+" FROM session_messages WHERE idempotency_key = ?",
		params.IdempotencyKey))
	if err != nil && err.Error() != "sql: no rows in result set" {
		return nil, false, err
	}
	if existing != nil {
		sameRequest := existing.OwnerScopeID == params.OwnerScopeID &&
			existing.SourceSessionID == params.SourceSessionID
		sameDelivery := existing.TargetSessionID == params.TargetSessionID &&
			existing.Content == params.Content &&
			existing.RequestedInputMode == params.InputMode
		if !sameRequest || !sameDelivery {
			return nil, false, &SessionMessageIdempotencyConflict{
				SessionMessageStoreError{Msg: "idempotency key was reused with different arguments"}}
		}
		return existing, false, tx.Commit()
	}
	// 目标信箱容量（Python :305-320，active 集合 + unknown 屏障条件）
	counts := append([]string{}, sessionMessageActiveStatuses...)
	sortStrings(counts)
	pendingArgs := []any{params.TargetSessionID}
	for _, st := range counts {
		pendingArgs = append(pendingArgs, st)
	}
	var pending int
	if err := tx.QueryRow(
		"SELECT COUNT(*) FROM session_messages "+
			"WHERE target_session_id = ? "+
			"AND status IN ("+placeholders(len(counts))+") "+
			"AND (status != 'unknown' OR (resolved_at IS NULL AND queue_released_at IS NULL))",
		pendingArgs...).Scan(&pending); err != nil {
		return nil, false, err
	}
	if pending >= params.MaxPendingPerTarget {
		return nil, false, &SessionMessageLimitExceeded{
			SessionMessageStoreError{Msg: "target mailbox is full"}}
	}
	// 链路容量（Python :322-329）
	var chainCount int
	if err := tx.QueryRow(
		"SELECT COUNT(*) FROM session_messages WHERE chain_id = ?",
		effectiveChainID).Scan(&chainCount); err != nil {
		return nil, false, err
	}
	if chainCount >= params.MaxMessagesPerChain {
		return nil, false, &SessionMessageLimitExceeded{
			SessionMessageStoreError{Msg: "message chain limit exceeded"}}
	}
	// 插入（Python :331-361；status 固定 'queued'）
	if _, err := tx.Exec(
		"INSERT INTO session_messages ("+
			"message_id, owner_scope_id, source_session_id, source_title_snapshot, "+
			"source_request_id, source_tool_call_id, idempotency_key, target_session_id, "+
			"content, chain_id, parent_message_id, hop_count, status, "+
			"created_at, updated_at, retry_of, input_mode, requested_input_mode) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'queued', ?, ?, ?, ?, ?)",
		messageID, params.OwnerScopeID, params.SourceSessionID, params.SourceTitleSnapshot,
		params.SourceRequestID, params.SourceToolCallID, params.IdempotencyKey,
		params.TargetSessionID, params.Content, effectiveChainID, params.ParentMessageID,
		params.HopCount, now, now, params.RetryOf, deliveryMode, params.InputMode); err != nil {
		return nil, false, err
	}
	rec, err := scanRecord(tx.QueryRow(
		"SELECT "+sessionMessageColumns+" FROM session_messages WHERE message_id = ?",
		messageID))
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return rec, true, nil
}

// SessionMessageEnqueueParams Enqueue 的参数包。
// Python: enqueue 的关键字参数（Go 无关键字参数，用具名结构体保持可读对应）
type SessionMessageEnqueueParams struct {
	// OwnerScopeID 归属用户域
	OwnerScopeID string
	// SourceSessionID 发送会话
	SourceSessionID string
	// SourceTitleSnapshot 源标题快照
	SourceTitleSnapshot string
	// SourceRequestID 源请求标识
	SourceRequestID string
	// SourceToolCallID 源工具调用标识
	SourceToolCallID string
	// IdempotencyKey 幂等键
	IdempotencyKey string
	// TargetSessionID 目标会话
	TargetSessionID string
	// Content 消息内容
	Content string
	// ChainID 链路标识（空则取 message_id）
	ChainID string
	// ParentMessageID 父消息标识
	ParentMessageID string
	// HopCount 转发深度（默认 1）
	HopCount int
	// RetryOf 重试源
	RetryOf string
	// InputMode 原始模式（""/steer）
	InputMode string
	// EffectiveInputMode 生效模式（nil=取 InputMode）
	EffectiveInputMode *string
	// MaxPendingPerTarget 目标信箱容量上限（默认 100）
	MaxPendingPerTarget int
	// MaxMessagesPerChain 链路容量上限（默认 16）
	MaxMessagesPerChain int
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sessionMessageColumns 查询列清单（与 ensure_schema 列序一致）。
const sessionMessageColumns = "sequence, message_id, owner_scope_id, source_session_id, " +
	"source_title_snapshot, source_request_id, source_tool_call_id, target_session_id, " +
	"content, chain_id, parent_message_id, hop_count, status, execution_request_id, " +
	"runtime_run_id, interrupt_request_id, interrupt_source, created_at, started_at, " +
	"finished_at, updated_at, last_error_code, last_error, resolved_at, resolution, " +
	"retry_of, input_mode, queue_released_at, requested_input_mode"

// normalizeJournalMode 归一化 SQLite journal mode。
// Python: jiuwenswarm/common/sqlite_config.py:8-12（仅 WAL/DELETE 合法）
func normalizeJournalMode(value string) string {
	trimmed := upper(trimSpace(value))
	if trimmed != "WAL" && trimmed != "DELETE" {
		panic("SQLite journal_mode must be WAL or DELETE")
	}
	return trimmed
}

// open 打开一条带 PRAGMA 的连接（对齐 Python _connect :86-102）。
func (s *SessionMessageStore) open() (*sql.DB, error) {
	if err := ensureParentDir(s.path); err != nil {
		return nil, err
	}
	dsn := s.path + "?_busy_timeout=30000&_foreign_keys=on&_synchronous=FULL"
	if s.journalMode == "DELETE" {
		dsn += "&_journal_mode=DELETE"
	}
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}
	return db, nil
}

// openImmediate 打开带 _txlock=immediate 的连接（对齐 enqueue 的 BEGIN IMMEDIATE）。
func (s *SessionMessageStore) openImmediate() (*sql.DB, error) {
	if err := ensureParentDir(s.path); err != nil {
		return nil, err
	}
	return sql.Open(driverName,
		s.path+"?_busy_timeout=30000&_foreign_keys=on&_synchronous=FULL&_txlock=immediate")
}

// ──────────────────────────── 非导出函数（胶水助手） ────────────────────────────

// fileExists 判断文件存在。
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// ensureParentDir 确保父目录存在（对齐 _connect 的 mkdir parents=True）。
func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

// nowFloat 当前 epoch 秒（对齐 time.time() float 语义）。
func nowFloat() float64 { return float64(time.Now().UnixNano()) / 1e9 }

// newUUIDHex 无连字符 uuid（对齐 uuid.uuid4().hex）。
func newUUIDHex() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 失败极罕见；与 Python uuid4 行为一致地向上暴露
		panic("crypto/rand unavailable: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x%x%x%x%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// placeholders 生成 N 个 SQL 占位符。
func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?, ", n), ", ") }

// sortedUnique 排序去重（对齐 waiting_for_pause 的 sorted(set(ids))）。
func sortedUnique(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// sortStrings 原地排序。
func sortStrings(s []string) { sort.Strings(s) }

// toAnySlice []string → []any。
func toAnySlice(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// toFloatPtr any(float64) → *float64。
func toFloatPtr(v any) *float64 {
	if f, ok := v.(float64); ok {
		return &f
	}
	return nil
}

// upper 大写。
func upper(s string) string { return strings.ToUpper(s) }

// trimSpace 去首尾空白。
func trimSpace(s string) string { return strings.TrimSpace(s) }

// scanRecord 行 → 记录（对齐 _row_to_record :192-226）。
func scanRecord(row *sql.Row) (*SessionMessageRecord, error) {
	var r SessionMessageRecord
	var startedAt, finishedAt, resolvedAt, queueReleasedAt any
	err := row.Scan(
		&r.Sequence, &r.MessageID, &r.OwnerScopeID, &r.SourceSessionID,
		&r.SourceTitleSnapshot, &r.SourceRequestID, &r.SourceToolCallID,
		&r.TargetSessionID, &r.Content, &r.ChainID, &r.ParentMessageID,
		&r.HopCount, &r.Status, &r.ExecutionRequestID, &r.RuntimeRunID,
		&r.InterruptRequestID, &r.InterruptSource, &r.CreatedAt,
		&startedAt, &finishedAt, &r.UpdatedAt, &r.LastErrorCode, &r.LastError,
		&resolvedAt, &r.Resolution, &r.RetryOf, &r.InputMode,
		&queueReleasedAt, &r.RequestedInputMode)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	r.StartedAt = toFloatPtr(startedAt)
	r.FinishedAt = toFloatPtr(finishedAt)
	r.ResolvedAt = toFloatPtr(resolvedAt)
	r.QueueReleasedAt = toFloatPtr(queueReleasedAt)
	return &r, nil
}

// scanRecords 多行 → 记录列表（对齐 _rows_to_records :237-245）。
func scanRecords(rows *sql.Rows) ([]*SessionMessageRecord, error) {
	defer rows.Close()
	var out []*SessionMessageRecord
	for rows.Next() {
		var r SessionMessageRecord
		var startedAt, finishedAt, resolvedAt, queueReleasedAt any
		if err := rows.Scan(
			&r.Sequence, &r.MessageID, &r.OwnerScopeID, &r.SourceSessionID,
			&r.SourceTitleSnapshot, &r.SourceRequestID, &r.SourceToolCallID,
			&r.TargetSessionID, &r.Content, &r.ChainID, &r.ParentMessageID,
			&r.HopCount, &r.Status, &r.ExecutionRequestID, &r.RuntimeRunID,
			&r.InterruptRequestID, &r.InterruptSource, &r.CreatedAt,
			&startedAt, &finishedAt, &r.UpdatedAt, &r.LastErrorCode, &r.LastError,
			&resolvedAt, &r.Resolution, &r.RetryOf, &r.InputMode,
			&queueReleasedAt, &r.RequestedInputMode); err != nil {
			return nil, err
		}
		r.StartedAt = toFloatPtr(startedAt)
		r.FinishedAt = toFloatPtr(finishedAt)
		r.ResolvedAt = toFloatPtr(resolvedAt)
		r.QueueReleasedAt = toFloatPtr(queueReleasedAt)
		out = append(out, &r)
	}
	return out, rows.Err()
}
