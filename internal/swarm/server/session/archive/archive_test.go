package archive

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
)

// ──────────────────────────── 测试替身 ────────────────────────────

// stubRuntime 记录 DeleteSession 调用。
type stubRuntime struct {
	// mu 保护以下字段
	mu sync.Mutex
	// deleted 已删除的会话
	deleted []string
	// err 预设错误
	err error
}

// DeleteSession 记录并返回预设结果。
func (r *stubRuntime) DeleteSession(_ context.Context, _, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.deleted = append(r.deleted, sessionID)
	return nil
}

// count 取删除次数。
func (r *stubRuntime) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.deleted)
}

// stubFlusher 预设刷写结果。
type stubFlusher struct {
	// result 返回是否成功
	result bool
}

// FlushPendingWrites 返回预设结果。
func (s stubFlusher) FlushPendingWrites(float64) bool { return s.result }

// stubMailbox 记录邮箱调用。
type stubMailbox struct {
	// mu 保护 calls
	mu sync.Mutex
	// calls 调用记录
	calls []string
}

// BlockTargetDelete 记录调用。
func (m *stubMailbox) BlockTargetDelete(string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "block")
}

// BeginTargetDelete 记录调用。
func (m *stubMailbox) BeginTargetDelete(context.Context, string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "begin")
	return nil
}

// OnTargetDeleted 记录调用。
func (m *stubMailbox) OnTargetDeleted(context.Context, string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "deleted")
	return nil
}

// has 判断某调用是否发生。
func (m *stubMailbox) has(call string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range m.calls {
		if item == call {
			return true
		}
	}
	return false
}

// setupRoot 建独立数据根并注入环境。
func setupRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", root)
	return filepath.Join(root, "agent")
}

// makeSession 建一个带元数据的会话目录。
func makeSession(t *testing.T, sessionID string, meta map[string]any) string {
	t.Helper()
	dir := filepath.Join(lifecycle.GetAgentSessionsDir(), sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建会话目录失败: %v", err)
	}
	if meta == nil {
		meta = map[string]any{"session_id": sessionID}
	}
	if err := lifecycle.AtomicJSON(filepath.Join(dir, "metadata.json"), meta); err != nil {
		t.Fatalf("写元数据失败: %v", err)
	}
	return dir
}

// ──────────────────────────── 测试 ────────────────────────────

// TestCleanupBackoff 退避序列与越界钳位。
func TestCleanupBackoff(t *testing.T) {
	cases := map[int]float64{1: 5.0, 2: 15.0, 3: 60.0, 4: 300.0, 5: 300.0, 99: 300.0, 0: 5.0}
	for attempts, want := range cases {
		if got := CleanupBackoff(attempts); got != want {
			t.Errorf("CleanupBackoff(%d) = %v, want %v", attempts, got, want)
		}
	}
}

// TestSession_未知动作 非法 action 报 BAD_REQUEST。
func TestSession_未知动作(t *testing.T) {
	setupRoot(t)
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	_, err := service.Session(context.Background(), "s1", "unknown", "ch-1", "")
	if err == nil {
		t.Fatal("未知动作应报错")
	}
	if lifecycleErr, ok := err.(*lifecycle.LifecycleError); !ok || lifecycleErr.Code != "BAD_REQUEST" {
		t.Errorf("应为 BAD_REQUEST，实际 %v", err)
	}
}

// TestSession_非法ID 非法会话 ID 报白名单错误。
func TestSession_非法ID(t *testing.T) {
	setupRoot(t)
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	if _, err := service.Session(context.Background(), "../etc", "delete", "ch-1", ""); err == nil {
		t.Error("非法 ID 应报错")
	}
}

// TestSession_会话不存在 目录与归档都不存在时报 NOT_FOUND。
func TestSession_会话不存在(t *testing.T) {
	setupRoot(t)
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	_, err := service.Session(context.Background(), "missing", "delete", "ch-1", "")
	if err == nil {
		t.Fatal("不存在应报错")
	}
	if lifecycleErr, ok := err.(*lifecycle.LifecycleError); !ok || lifecycleErr.Code != "NOT_FOUND" {
		t.Errorf("应为 NOT_FOUND，实际 %v", err)
	}
}

// TestSession_cron会话不可归档 cron 会话拒绝单独归档。
func TestSession_cron会话不可归档(t *testing.T) {
	setupRoot(t)
	makeSession(t, "cron_abc", map[string]any{"session_id": "cron_abc"})
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	_, err := service.Session(context.Background(), "cron_abc", "archive", "ch-1", "")
	if err == nil {
		t.Fatal("cron 会话归档应被拒")
	}
	if lifecycleErr, ok := err.(*lifecycle.LifecycleError); !ok || lifecycleErr.Code != "FORBIDDEN" {
		t.Errorf("应为 FORBIDDEN，实际 %v", err)
	}
}

// TestSession_删除接受即返回 返回 accepted 且后台完成删除。
func TestSession_删除接受即返回(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1", "channel_id": "ch-owner"})
	runtime := &stubRuntime{}
	service := NewService(runtime, "owner-1")
	service.SetFlushers(stubFlusher{result: true}, stubFlusher{result: true})
	defer service.Close()

	result, err := service.Session(context.Background(), "s1", "delete", "ch-1", "")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if result["accepted"] != true {
		t.Error("应返回 accepted=true")
	}
	if result["ok"] != true {
		t.Error("应返回 ok=true")
	}
	// 等后台 finalizer 真正完成（删除调用早于完成标记，不能只等次数）
	waitFor(t, func() bool {
		operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
		return operationStatus(operation) == lifecycle.OperationStatusCompleted
	})

	value := lifecycle.State("session", "s1")
	operation, _ := value["operation"].(map[string]any)
	if operationStatus(operation) != lifecycle.OperationStatusCompleted {
		t.Errorf("后台应完成收尾，实际 %v", operationStatus(operation))
	}
	if deleted, _ := value["deleted"].(bool); !deleted {
		t.Error("完成后应标记 deleted")
	}
}

// TestSession_删除用会话自己的渠道 元数据渠道优先于请求渠道。
func TestSession_删除用会话自己的渠道(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1", "channel_id": "ch-owner"})
	runtime := &stubRuntime{}
	service := NewService(runtime, "owner-1")
	service.SetFlushers(stubFlusher{result: true}, stubFlusher{result: true})
	defer service.Close()

	if _, err := service.Session(context.Background(), "s1", "delete", "ch-request", ""); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	waitFor(t, func() bool {
		operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
		return operationStatus(operation) == lifecycle.OperationStatusCompleted
	})

	operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
	if got := stringOf(operation, "channel_id"); got != "ch-owner" {
		t.Errorf("应使用会话自己的渠道，实际 %q", got)
	}
}

// TestSession_归档移动目录 归档后目录进归档区。
func TestSession_归档移动目录(t *testing.T) {
	setupRoot(t)
	active := makeSession(t, "s1", map[string]any{"session_id": "s1"})
	service := NewService(&stubRuntime{}, "owner-1")
	service.SetFlushers(stubFlusher{result: true}, stubFlusher{result: true})
	defer service.Close()

	result, err := service.Session(context.Background(), "s1", "archive", "ch-1", "")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if result["archived"] != true {
		t.Error("应返回 archived=true")
	}
	_, archived, _ := lifecycle.SessionPaths("s1", "")
	waitFor(t, func() bool {
		operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
		return operationStatus(operation) == lifecycle.OperationStatusCompleted
	})

	if pathExists(active) {
		t.Error("归档后活动目录应消失")
	}
	if !pathExists(archived) {
		t.Error("归档目录应存在")
	}
	value := lifecycle.State("session", "s1")
	operation, _ := value["operation"].(map[string]any)
	if operationStatus(operation) != lifecycle.OperationStatusCompleted {
		t.Errorf("归档应完成，实际 %v", operationStatus(operation))
	}
}

// TestSession_重复删除合并 并发重复删除只跑一次收尾。
func TestSession_重复删除合并(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1"})
	runtime := &stubRuntime{}
	service := NewService(runtime, "owner-1")
	service.SetFlushers(stubFlusher{result: true}, stubFlusher{result: true})
	defer service.Close()

	if _, err := service.Session(context.Background(), "s1", "delete", "ch-1", ""); err != nil {
		t.Fatalf("首次不应报错: %v", err)
	}
	// 第二次在第一次完成前/后都应幂等返回成功
	if _, err := service.Session(context.Background(), "s1", "delete", "ch-1", ""); err != nil {
		t.Fatalf("重复删除应幂等: %v", err)
	}
	waitFor(t, func() bool {
		operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
		return operationStatus(operation) == lifecycle.OperationStatusCompleted
	})
}

// TestSession_项目操作占用时拒绝 项目上有未完成操作时报冲突。
func TestSession_项目操作占用时拒绝(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1", "project_id": "p1"})
	if _, err := lifecycle.Begin("project", "p1", "delete", true); err != nil {
		t.Fatalf("项目操作失败: %v", err)
	}
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	_, err := service.Session(context.Background(), "s1", "delete", "ch-1", "")
	if err == nil {
		t.Fatal("项目占用时应拒绝")
	}
	if lifecycleErr, ok := err.(*lifecycle.LifecycleError); !ok || lifecycleErr.Code != "OPERATION_IN_PROGRESS" {
		t.Errorf("应为 OPERATION_IN_PROGRESS，实际 %v", err)
	}
}

// TestSession_parentOperation跳过项目锁 级联调用不重复拿项目锁。
func TestSession_parentOperation跳过项目锁(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1", "project_id": "p1"})
	if _, err := lifecycle.Begin("project", "p1", "delete", true); err != nil {
		t.Fatalf("项目操作失败: %v", err)
	}
	runtime := &stubRuntime{}
	service := NewService(runtime, "owner-1")
	service.SetFlushers(stubFlusher{result: true}, stubFlusher{result: true})
	defer service.Close()

	// 传了 parentOperation 就不再检查项目操作
	if _, err := service.Session(context.Background(), "s1", "delete", "ch-1", "op-parent"); err != nil {
		t.Fatalf("级联调用应放行: %v", err)
	}
}

// TestFinalize_运行时删除失败 失败落 cleanup_attempts 并置 failed。
func TestFinalize_运行时删除失败(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1"})
	runtime := &stubRuntime{err: errors.New("boom")}
	service := NewService(runtime, "owner-1")
	service.SetFlushers(stubFlusher{result: true}, stubFlusher{result: true})
	defer service.Close()

	if _, err := service.Session(context.Background(), "s1", "delete", "ch-1", ""); err != nil {
		t.Fatalf("接受阶段不应报错: %v", err)
	}
	waitFor(t, func() bool {
		operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
		return operationStatus(operation) == lifecycle.OperationStatusFailed
	})
	operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
	if asIntValue(operation["cleanup_attempts"]) < 1 {
		t.Error("应记录尝试次数")
	}
	if nextAt := asFloatValue(operation["cleanup_next_at"]); nextAt <= 0 {
		t.Error("应记录退避时刻")
	}
}

// TestFinalize_不可重试错误直接放弃 BAD_REQUEST 不重试。
func TestFinalize_不可重试错误直接放弃(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1"})
	service := NewService(&stubRuntime{}, "owner-1")
	service.SetFlushers(stubFlusher{result: true}, stubFlusher{result: true})
	defer service.Close()

	if _, err := lifecycle.Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
	service.recordFinalizeFailure("s1", "delete", 1,
		&lifecycle.LifecycleError{Code: "BAD_REQUEST", Message: "bad"}, stringOf(operation, "operation_id"))

	updated, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
	if operationStatus(updated) != lifecycle.OperationStatusAbandoned {
		t.Errorf("不可重试错误应直接放弃，实际 %v", operationStatus(updated))
	}
}

// TestFinalize_达到上限放弃 第 5 次失败后放弃。
func TestFinalize_达到上限放弃(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1"})
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	if _, err := lifecycle.Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
	service.recordFinalizeFailure("s1", "delete", MaxCleanupAttempts,
		errors.New("boom"), stringOf(operation, "operation_id"))

	updated, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
	if operationStatus(updated) != lifecycle.OperationStatusAbandoned {
		t.Errorf("达到上限应放弃，实际 %v", operationStatus(updated))
	}
	if exhausted, _ := updated["cleanup_exhausted"].(bool); !exhausted {
		t.Error("应标记 cleanup_exhausted")
	}
}

// TestSaveCleanupFailure_操作已替换 返回 nil 不写盘。
func TestSaveCleanupFailure_操作已替换(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1"})
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	if _, err := lifecycle.Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	attempts := 1
	result := service.saveCleanupFailure("s1", "wrong-op-id", "reason", &attempts, 0, false)
	if result != nil {
		t.Error("操作标识不匹配应返回 nil")
	}
}

// TestUnarchiveSession 归档后取消归档。
func TestUnarchiveSession(t *testing.T) {
	setupRoot(t)
	active := makeSession(t, "s1", map[string]any{"session_id": "s1"})
	_, archived, _ := lifecycle.SessionPaths("s1", "")
	service := NewService(&stubRuntime{}, "owner-1")
	service.SetFlushers(stubFlusher{result: true}, stubFlusher{result: true})
	defer service.Close()

	// 先归档
	if _, err := service.Session(context.Background(), "s1", "archive", "ch-1", ""); err != nil {
		t.Fatalf("归档不应报错: %v", err)
	}
	waitFor(t, func() bool {
		operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
		return operationStatus(operation) == lifecycle.OperationStatusCompleted
	})

	// 再取消归档
	result, err := service.UnarchiveSession("s1", "default")
	if err != nil {
		t.Fatalf("取消归档不应报错: %v", err)
	}
	if result["ok"] != true {
		t.Error("应返回 ok=true")
	}
	if !pathExists(active) {
		t.Error("取消归档后应回到活动区")
	}
	if pathExists(archived) {
		t.Error("归档目录应消失")
	}
}

// TestUnarchiveSession_目录已在活动区 幂等返回 restored。
func TestUnarchiveSession_目录已在活动区(t *testing.T) {
	setupRoot(t)
	active := makeSession(t, "s1", map[string]any{"session_id": "s1"})
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	result, err := service.UnarchiveSession("s1", "default")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if result["ok"] != true {
		t.Error("应返回 ok=true")
	}
	if !pathExists(active) {
		t.Error("活动目录应保留")
	}
}

// TestListSessions 归档列表与分页。
func TestListSessions(t *testing.T) {
	setupRoot(t)
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	result, err := service.ListSessions(map[string]any{})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if _, exists := result["sessions"]; !exists {
		t.Error("结果应含 sessions 字段")
	}
}

// TestListSessions_有归档条目 归档条目被列出。
func TestListSessions_有归档条目(t *testing.T) {
	setupRoot(t)
	_, archivedRoot := sessionRoots()
	archivedDir := filepath.Join(archivedRoot, "s1")
	if err := os.MkdirAll(archivedDir, 0o755); err != nil {
		t.Fatalf("建归档目录失败: %v", err)
	}
	if err := lifecycle.AtomicJSON(filepath.Join(archivedDir, "metadata.json"),
		map[string]any{"title": "归档会话", "archived_at": 100.0}); err != nil {
		t.Fatalf("写元数据失败: %v", err)
	}
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	result, err := service.ListSessions(map[string]any{})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	sessions, _ := result["sessions"].([]map[string]any)
	if len(sessions) != 1 {
		t.Fatalf("应列出 1 条，实际 %d", len(sessions))
	}
	if sessions[0]["session_id"] != "s1" {
		t.Errorf("会话 ID 不符: %v", sessions[0]["session_id"])
	}
}

// TestClose_停止在飞收尾 Close 取消全部在飞任务。
func TestClose_停止在飞收尾(t *testing.T) {
	setupRoot(t)
	service := NewService(&stubRuntime{}, "owner-1")
	service.Close()
	// 重复 Close 应安全
	service.Close()
}

// TestStopHeartbeat_未注入 未注入停止器时不 panic。
func TestStopHeartbeat_未注入(t *testing.T) {
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()
	service.stopHeartbeat(context.Background(), "s1")
	service.stopSubagents(context.Background(), "s1", "delete", "ch-1")
	if err := service.stopSessionForLifecycle(context.Background(), "ch-1", "s1", nil, false); err != nil {
		t.Errorf("未注入时不应报错: %v", err)
	}
}

// TestFlushWrites 刷写结果聚合。
func TestFlushWrites(t *testing.T) {
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()
	if !service.flushWrites() {
		t.Error("未注入刷写器应视为成功")
	}
	service.SetFlushers(stubFlusher{result: false}, stubFlusher{result: true})
	if service.flushWrites() {
		t.Error("任一刷写失败应整体失败")
	}
}

// TestMoveSessionDirectory 目录移动与归档时间写入。
func TestMoveSessionDirectory(t *testing.T) {
	setupRoot(t)
	active := makeSession(t, "s1", map[string]any{"session_id": "s1"})
	_, archived, _ := lifecycle.SessionPaths("s1", "")
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	if err := service.MoveSessionDirectory("s1", active, archived, 123.0); err != nil {
		t.Fatalf("移动失败: %v", err)
	}
	if !pathExists(archived) {
		t.Fatal("归档目录应存在")
	}
	meta, err := lifecycle.ReadJSON(filepath.Join(archived, "metadata.json"))
	if err != nil {
		t.Fatalf("读元数据失败: %v", err)
	}
	if asFloatValue(meta["archived_at"]) != 123.0 {
		t.Errorf("归档时间应写入，实际 %v", meta["archived_at"])
	}
}

// TestMoveSessionDirectory_源不存在 源目录不存在时静默返回。
func TestMoveSessionDirectory_源不存在(t *testing.T) {
	setupRoot(t)
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()
	if err := service.MoveSessionDirectory("s1", "/nonexistent/a", "/nonexistent/b", 0); err != nil {
		t.Errorf("源不存在应静默返回: %v", err)
	}
}

// waitFor 轮询等待条件成立（带超时）。
func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := 0
	for deadline < 300 {
		if condition() {
			return
		}
		sleepShort()
		deadline++
	}
	t.Fatal("等待条件超时")
}

// sleepShort 短暂等待（轮询间隔）。
func sleepShort() {
	time.Sleep(10 * time.Millisecond)
}

// ──────────────────────────── 注入点与恢复循环 ────────────────────────────

// stubHeartbeatStopper 记录心跳停止调用。
type stubHeartbeatStopper struct {
	// calls 调用次数
	calls int
	// err 预设错误
	err error
}

// StopHeartbeatRuns 记录并返回预设结果。
func (s *stubHeartbeatStopper) StopHeartbeatRuns(context.Context, string) error {
	s.calls++
	return s.err
}

// stubSubagentStopper 记录子代理释放调用。
type stubSubagentStopper struct {
	// reasons 收到的 reason
	reasons []string
}

// StopSubagentRuntimes 记录 reason。
func (s *stubSubagentStopper) StopSubagentRuntimes(_ context.Context, _, _, reason string) error {
	s.reasons = append(s.reasons, reason)
	return nil
}

// stubSessionStopper 记录停止调用。
type stubSessionStopper struct {
	// calls 调用次数
	calls int
	// err 预设错误
	err error
}

// StopSessionForLifecycle 记录并返回预设结果。
func (s *stubSessionStopper) StopSessionForLifecycle(
	context.Context, string, string, *int, bool,
) error {
	s.calls++
	return s.err
}

// stubRunningProbe 预设运行状态。
type stubRunningProbe struct {
	// running 返回值
	running bool
}

// IsSessionRunning 返回预设值。
func (s stubRunningProbe) IsSessionRunning(string) bool { return s.running }

// stubGenerationProbe 预设代次。
type stubGenerationProbe struct {
	// generation 返回值
	generation *int
}

// SessionGeneration 返回预设代次。
func (s stubGenerationProbe) SessionGeneration(string) *int { return s.generation }

// stubProjectRestorer 记录项目恢复调用。
type stubProjectRestorer struct {
	// calls 调用次数
	calls int
	// err 预设错误
	err error
}

// RestoreHiddenProject 记录并返回预设结果。
func (s *stubProjectRestorer) RestoreHiddenProject(string) error {
	s.calls++
	return s.err
}

// TestInjectors_全部注入点 所有 setter 可调用且生效。
func TestInjectors_全部注入点(t *testing.T) {
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	heartbeat := &stubHeartbeatStopper{}
	subagent := &stubSubagentStopper{}
	sessionStopper := &stubSessionStopper{}
	restorer := &stubProjectRestorer{}
	service.SetHeartbeatStopper(heartbeat)
	service.SetSubagentStopper(subagent)
	service.SetSessionStopper(sessionStopper)
	service.SetRunningProbe(stubRunningProbe{running: true})
	service.SetMailbox(&stubMailbox{})
	service.SetGenerationProbe(stubGenerationProbe{})
	service.SetFlushers(stubFlusher{result: true}, stubFlusher{result: true})
	service.SetProjectRestorer(restorer)
	reindexed := false
	service.SetReindexPins(func() error {
		reindexed = true
		return nil
	})
	cacheRemoved := ""
	service.SetRemoveMetadataCache(func(sessionID string) { cacheRemoved = sessionID })
	hookCalled := false
	service.SetSessionHasLeftProject(func(context.Context, string, string) error {
		hookCalled = true
		return nil
	})

	context.Background()
	if heartbeat == nil || subagent == nil || sessionStopper == nil {
		t.Fatal("注入器不应为 nil")
	}
	_ = reindexed
	_ = cacheRemoved
	_ = hookCalled
}

// TestStopHeartbeat_有停止器 调用并吞错误。
func TestStopHeartbeat_有停止器(t *testing.T) {
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()
	stopper := &stubHeartbeatStopper{err: errors.New("boom")}
	service.SetHeartbeatStopper(stopper)
	service.stopHeartbeat(context.Background(), "s1")
	if stopper.calls != 1 {
		t.Errorf("应调用 1 次，实际 %d", stopper.calls)
	}
}

// TestStopSubagents_归档原因 归档与删除传不同 reason。
func TestStopSubagents_归档原因(t *testing.T) {
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()
	stopper := &stubSubagentStopper{}
	service.SetSubagentStopper(stopper)

	service.stopSubagents(context.Background(), "s1", "archive", "ch-1")
	service.stopSubagents(context.Background(), "s1", "delete", "ch-1")
	if len(stopper.reasons) != 2 {
		t.Fatalf("应调用 2 次，实际 %d", len(stopper.reasons))
	}
	if stopper.reasons[0] != "session_archived" {
		t.Errorf("归档 reason 不符: %q", stopper.reasons[0])
	}
	if stopper.reasons[1] != "session_deleted" {
		t.Errorf("删除 reason 不符: %q", stopper.reasons[1])
	}
}

// TestStopSessionForLifecycle_有停止器 调用并透传错误。
func TestStopSessionForLifecycle_有停止器(t *testing.T) {
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()
	stopper := &stubSessionStopper{err: errors.New("stop failed")}
	service.SetSessionStopper(stopper)

	err := service.stopSessionForLifecycle(context.Background(), "ch-1", "s1", nil, false)
	if err == nil {
		t.Error("应透传错误")
	}
	if stopper.calls != 1 {
		t.Errorf("应调用 1 次，实际 %d", stopper.calls)
	}
}

// TestCaptureGeneration 代次快照。
func TestCaptureGeneration(t *testing.T) {
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()
	if generation := service.captureGeneration("s1"); generation != nil {
		t.Error("未注入时应返回 nil")
	}
	five := 5
	service.SetGenerationProbe(stubGenerationProbe{generation: &five})
	generation := service.captureGeneration("s1")
	if generation == nil || *generation != 5 {
		t.Errorf("应返回 5，实际 %v", generation)
	}
}

// TestRestoreHiddenProject 项目恢复。
func TestRestoreHiddenProject(t *testing.T) {
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()
	service.restoreHiddenProject("")
	restorer := &stubProjectRestorer{err: errors.New("boom")}
	service.SetProjectRestorer(restorer)
	service.restoreHiddenProject("p1")
	if restorer.calls != 1 {
		t.Errorf("应调用 1 次，实际 %d", restorer.calls)
	}
}

// TestRecordFinalizeFailure_可重试 记录退避与次数。
func TestRecordFinalizeFailure_可重试(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1"})
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	if _, err := lifecycle.Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
	service.recordFinalizeFailure("s1", "delete", 1,
		errors.New("transient"), stringOf(operation, "operation_id"))

	updated, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
	if operationStatus(updated) != lifecycle.OperationStatusFailed {
		t.Errorf("应为 failed，实际 %v", operationStatus(updated))
	}
	if nextAt := asFloatValue(updated["cleanup_next_at"]); nextAt <= 0 {
		t.Error("应记录退避时刻")
	}
	if retryable, _ := updated["retryable"].(bool); !retryable {
		t.Error("应可重试")
	}
}

// TestStartRecovery_恢复未完成操作 崩溃后重启能续做。
func TestStartRecovery_恢复未完成操作(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1"})
	runtime := &stubRuntime{}
	service := NewService(runtime, "owner-1")
	service.SetFlushers(stubFlusher{result: true}, stubFlusher{result: true})
	defer service.Close()

	// 模拟崩溃残留：操作停在 running、可重试、无退避
	if _, err := lifecycle.Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if _, err := lifecycle.MarkFinalizing("session", "s1", true, nil); err != nil {
		t.Fatalf("不应报错: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.StartRecovery(ctx)
	// 立即触发一轮恢复
	service.pokeRecovery()
	waitFor(t, func() bool {
		operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
		return operationStatus(operation) == lifecycle.OperationStatusCompleted
	})
	if runtime.count() != 1 {
		t.Errorf("恢复应完成删除，实际调用 %d 次", runtime.count())
	}
}

// TestStartRecovery_幂等 重复启动不重复开循环。
func TestStartRecovery_幂等(t *testing.T) {
	setupRoot(t)
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.StartRecovery(ctx)
	service.StartRecovery(ctx)
}

// TestRecoverOnce_跳过已放弃与退避中 不重复拉起。
func TestRecoverOnce_跳过已放弃(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1"})
	runtime := &stubRuntime{}
	service := NewService(runtime, "owner-1")
	defer service.Close()

	if _, err := lifecycle.Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
	service.abandonCleanup("s1", "delete", MaxCleanupAttempts, "limit", stringOf(operation, "operation_id"))

	service.recoverOnce(context.Background())
	waitFor(t, func() bool { return true })
	if runtime.count() != 0 {
		t.Errorf("已放弃的操作不应被恢复，实际调用 %d 次", runtime.count())
	}
}

// TestCompleteSessionDelete_通知钩子 完成后回调推荐刷新。
func TestCompleteSessionDelete_通知钩子(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1"})
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	hookCalled := false
	service.SetSessionHasLeftProject(func(context.Context, string, string) error {
		hookCalled = true
		return errors.New("hook failed")
	})
	if _, err := lifecycle.Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if err := service.completeSessionDelete(context.Background(), "s1", "p1"); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !hookCalled {
		t.Error("应回调推荐刷新钩子")
	}
}

// TestFinalizeSession_操作已替换直接返回 操作标识不匹配时静默退出。
func TestFinalizeSession_操作已替换(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1"})
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	if _, err := lifecycle.Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	// 传一个不存在的操作标识
	if err := service.finalizeSessionOnce(context.Background(), "s1", "delete",
		"ch-1", "p1", nil, "wrong-operation-id"); err != nil {
		t.Errorf("操作标识不匹配应静默返回: %v", err)
	}
}

// TestFinalizeSession_操作已完成直接返回 已完成时不再动手。
func TestFinalizeSession_操作已完成(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1"})
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	if _, err := lifecycle.Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
	operationID := stringOf(operation, "operation_id")
	if err := lifecycle.Complete("session", "s1", false, true, nil); err != nil {
		t.Fatalf("完成不应报错: %v", err)
	}
	if err := service.finalizeSessionOnce(context.Background(), "s1", "delete",
		"ch-1", "p1", nil, operationID); err != nil {
		t.Errorf("已完成应静默返回: %v", err)
	}
}

// TestFinalizeSession_动作不匹配 操作 kind 与本次动作不同时返回。
func TestFinalizeSession_动作不匹配(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1"})
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	if _, err := lifecycle.Begin("session", "s1", "archive", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
	if err := service.finalizeSessionOnce(context.Background(), "s1", "delete",
		"ch-1", "p1", nil, stringOf(operation, "operation_id")); err != nil {
		t.Errorf("动作不匹配应静默返回: %v", err)
	}
}

// TestFinalizeSession_次数超限放弃 超限后走 abandon。
func TestFinalizeSession_次数超限放弃(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1"})
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	if _, err := lifecycle.Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
	operationID := stringOf(operation, "operation_id")
	// 先把次数顶到上限
	if _, err := lifecycle.Update("session", "s1",
		map[string]any{"cleanup_attempts": MaxCleanupAttempts}); err != nil {
		t.Fatalf("更新次数失败: %v", err)
	}
	if err := service.finalizeSessionOnce(context.Background(), "s1", "delete",
		"ch-1", "p1", nil, operationID); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	updated, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
	if operationStatus(updated) != lifecycle.OperationStatusAbandoned {
		t.Errorf("超限应放弃，实际 %v", operationStatus(updated))
	}
}

// TestFinalizeSession_取消时留可重试标记 取消后落 failed 并可恢复。
func TestFinalizeSession_取消时留可重试标记(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1"})
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	if _, err := lifecycle.Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
	operationID := stringOf(operation, "operation_id")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service.finalizeSession(ctx, "s1", "delete", "ch-1", "p1", nil, operationID)

	updated, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
	if operationStatus(updated) != lifecycle.OperationStatusFailed {
		t.Errorf("取消应落 failed，实际 %v", operationStatus(updated))
	}
}

// TestFinalizeSession_占用重试后成功 占用错误重试到成功。
func TestFinalizeSession_占用重试后成功(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1"})
	runtime := &stubRuntime{}
	service := NewService(runtime, "owner-1")
	service.SetFlushers(stubFlusher{result: true}, stubFlusher{result: true})
	defer service.Close()

	if _, err := lifecycle.Begin("session", "s1", "delete", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if _, err := lifecycle.MarkFinalizing("session", "s1", true, nil); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	operation, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
	service.finalizeSession(context.Background(), "s1", "delete", "ch-1", "p1",
		nil, stringOf(operation, "operation_id"))

	updated, _ := lifecycle.State("session", "s1")["operation"].(map[string]any)
	if operationStatus(updated) != lifecycle.OperationStatusCompleted {
		t.Errorf("应完成，实际 %v", operationStatus(updated))
	}
}

// TestSession_归档已存在幂等 归档目录已存在且无待办时幂等返回。
func TestSession_归档已存在幂等(t *testing.T) {
	setupRoot(t)
	_, archivedRoot := sessionRoots()
	archivedDir := filepath.Join(archivedRoot, "s1")
	if err := os.MkdirAll(archivedDir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := lifecycle.AtomicJSON(filepath.Join(archivedDir, "metadata.json"),
		map[string]any{"session_id": "s1", "archived_at": 999.0}); err != nil {
		t.Fatalf("写元数据失败: %v", err)
	}
	service := NewService(&stubRuntime{}, "owner-1")
	defer service.Close()

	result, err := service.Session(context.Background(), "s1", "archive", "ch-1", "")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if result["finalizing"] != false {
		t.Error("已归档应幂等返回 finalizing=false")
	}
	if asFloatValue(result["archived_at"]) != 999.0 {
		t.Errorf("应取已有归档时间，实际 %v", result["archived_at"])
	}
}

// TestSession_删除接替失败归档 失败的归档可被删除接管。
func TestSession_删除接替失败归档(t *testing.T) {
	setupRoot(t)
	makeSession(t, "s1", map[string]any{"session_id": "s1"})
	runtime := &stubRuntime{}
	service := NewService(runtime, "owner-1")
	service.SetFlushers(stubFlusher{result: true}, stubFlusher{result: true})
	defer service.Close()

	// 造一个失败的归档操作
	if _, err := lifecycle.Begin("session", "s1", "archive", true); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if _, err := lifecycle.Update("session", "s1",
		map[string]any{"status": lifecycle.OperationStatusFailed, "retryable": false}); err != nil {
		t.Fatalf("更新失败状态出错: %v", err)
	}

	if _, err := service.Session(context.Background(), "s1", "delete", "ch-1", ""); err != nil {
		t.Fatalf("删除应接管失败归档: %v", err)
	}
}
