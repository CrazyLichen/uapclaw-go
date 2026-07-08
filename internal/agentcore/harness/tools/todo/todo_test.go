package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockFsOperation 模拟文件系统操作
type mockFsOperation struct {
	// data 文件内容存储（key=路径, value=内容）
	data map[string]string
	// mu 并发保护
	mu sync.Mutex
	// readErr 模拟读取错误
	readErr error
	// writeErr 模拟写入错误
	writeErr error
}

// mockSession 模拟会话对象
type mockSession struct {
	// sessionID 会话标识
	sessionID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMockFsOperation 创建模拟文件系统操作
func NewMockFsOperation() *mockFsOperation {
	return &mockFsOperation{
		data: make(map[string]string),
	}
}

// ReadFile 实现 FsOperation 接口
func (m *mockFsOperation) ReadFile(_ context.Context, path string, _ ...sys_operation.FsOption) (*sys_operation.ReadFileResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readErr != nil {
		return nil, m.readErr
	}
	content, ok := m.data[path]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	return &sys_operation.ReadFileResult{Code: 0, Data: content}, nil
}

// WriteFile 实现 FsOperation 接口
func (m *mockFsOperation) WriteFile(_ context.Context, path string, content string, _ ...sys_operation.FsOption) (*sys_operation.WriteFileResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		return nil, m.writeErr
	}
	m.data[path] = content
	return &sys_operation.WriteFileResult{Code: 0}, nil
}

// ListFiles 实现 FsOperation 接口
func (m *mockFsOperation) ListFiles(_ context.Context, _ string, _ ...sys_operation.FsOption) (*sys_operation.ListFilesResult, error) {
	return &sys_operation.ListFilesResult{Code: 0}, nil
}

// ListDirectories 实现 FsOperation 接口
func (m *mockFsOperation) ListDirectories(_ context.Context, _ string, _ ...sys_operation.FsOption) (*sys_operation.ListDirsResult, error) {
	return &sys_operation.ListDirsResult{Code: 0}, nil
}

// SearchFiles 实现 FsOperation 接口
func (m *mockFsOperation) SearchFiles(_ context.Context, _ string, _ string, _ ...sys_operation.FsOption) (*sys_operation.SearchFilesResult, error) {
	return &sys_operation.SearchFilesResult{Code: 0}, nil
}

// ListTools 实现 FsOperation 接口
func (m *mockFsOperation) ListTools() []*tool.ToolCard {
	return nil
}

// GetSessionID 实现 session 接口
func (s *mockSession) GetSessionID() string {
	return s.sessionID
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestTodoTool 创建测试用 TodoTool
func newTestTodoTool() (TodoTool, *mockFsOperation) {
	fs := NewMockFsOperation()
	lockMgr := NewTodoLockManager()
	todoTool := newTodoTool("/tmp/workspace", fs, lockMgr)
	return todoTool, fs
}

// TestNewTodoLockManager 测试创建锁管理器
func TestNewTodoLockManager(t *testing.T) {
	mgr := NewTodoLockManager()
	if mgr == nil {
		t.Fatal("NewTodoLockManager 返回 nil")
	}
	if len(mgr.locks) != 0 {
		t.Fatalf("期望初始 locks 为空，实际 %d", len(mgr.locks))
	}
}

// TestTodoLockManager_Operation 测试获取会话锁
func TestTodoLockManager_Operation(t *testing.T) {
	mgr := NewTodoLockManager()

	lock1 := mgr.Operation("session1")
	if lock1 == nil {
		t.Fatal("Operation 返回 nil")
	}

	// 再次获取同一会话应返回同一把锁
	lock1Again := mgr.Operation("session1")
	if lock1Again != lock1 {
		t.Fatal("同一会话应返回同一把锁")
	}

	// 不同会话返回不同锁
	lock2 := mgr.Operation("session2")
	if lock2 == lock1 {
		t.Fatal("不同会话应返回不同锁")
	}
}

// TestTodoLockManager_CleanupSession 测试清除会话锁
func TestTodoLockManager_CleanupSession(t *testing.T) {
	mgr := NewTodoLockManager()
	_ = mgr.Operation("session1")
	_ = mgr.Operation("session2")

	mgr.CleanupSession("session1")

	if _, ok := mgr.locks["session1"]; ok {
		t.Fatal("session1 应被清除")
	}
	if _, ok := mgr.locks["session2"]; !ok {
		t.Fatal("session2 应保留")
	}
}

// TestTodoTool_GetFilePath 测试获取文件路径
func TestTodoTool_GetFilePath(t *testing.T) {
	todoTool, _ := newTestTodoTool()
	path := todoTool.GetFilePath("session123")
	expected := "/tmp/workspace/session123/todo.json"
	if path != expected {
		t.Fatalf("GetFilePath = %q, want %q", path, expected)
	}
}

// TestTodoTool_LoadTodos_空文件 测试加载空文件
func TestTodoTool_LoadTodos_空文件(t *testing.T) {
	todoTool, _ := newTestTodoTool()
	items, err := todoTool.LoadTodos(context.Background(), "session1")
	if err != nil {
		t.Fatalf("LoadTodos 返回错误: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("期望空列表，实际 %d 项", len(items))
	}
}

// TestTodoTool_LoadTodos_正常数据 测试加载正常数据
func TestTodoTool_LoadTodos_正常数据(t *testing.T) {
	todoTool, fs := newTestTodoTool()
	// 预写数据
	data := `[{"id":"task1","content":"任务1","activeForm":"执行任务1","description":"描述1","status":"pending"}]`
	fs.data[todoTool.GetFilePath("session1")] = data

	items, err := todoTool.LoadTodos(context.Background(), "session1")
	if err != nil {
		t.Fatalf("LoadTodos 返回错误: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("期望 1 项，实际 %d", len(items))
	}
	if items[0].ID != "task1" {
		t.Fatalf("ID = %q, want %q", items[0].ID, "task1")
	}
	if items[0].Status != hschema.TodoStatusPending {
		t.Fatalf("Status = %v, want pending", items[0].Status)
	}
}

// TestTodoTool_LoadTodos_无效JSON 测试加载无效 JSON
func TestTodoTool_LoadTodos_无效JSON(t *testing.T) {
	todoTool, fs := newTestTodoTool()
	fs.data[todoTool.GetFilePath("session1")] = "invalid json"

	_, err := todoTool.LoadTodos(context.Background(), "session1")
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestTodoTool_SaveTodos 测试保存待办事项
func TestTodoTool_SaveTodos(t *testing.T) {
	todoTool, fs := newTestTodoTool()
	items := []hschema.TodoItem{
		{
			ID:          "task1",
			Content:     "任务1",
			ActiveForm:  "执行任务1",
			Description: "描述1",
			Status:      hschema.TodoStatusInProgress,
		},
		{
			ID:          "task2",
			Content:     "任务2",
			ActiveForm:  "执行任务2",
			Description: "描述2",
			Status:      hschema.TodoStatusPending,
		},
	}

	err := todoTool.SaveTodos(context.Background(), "session1", items)
	if err != nil {
		t.Fatalf("SaveTodos 返回错误: %v", err)
	}

	// 验证文件内容
	savedData, ok := fs.data[todoTool.GetFilePath("session1")]
	if !ok {
		t.Fatal("文件未写入")
	}
	var saved []map[string]any
	if err := json.Unmarshal([]byte(savedData), &saved); err != nil {
		t.Fatalf("JSON 解码失败: %v", err)
	}
	if len(saved) != 2 {
		t.Fatalf("期望 2 项，实际 %d", len(saved))
	}
}

// TestTodoTool_SaveTodos_写入失败 测试保存写入失败
func TestTodoTool_SaveTodos_写入失败(t *testing.T) {
	todoTool, fs := newTestTodoTool()
	fs.writeErr = fmt.Errorf("disk full")

	items := []hschema.TodoItem{{ID: "task1", Content: "t1", ActiveForm: "a1", Description: "d1", Status: hschema.TodoStatusPending}}
	err := todoTool.SaveTodos(context.Background(), "session1", items)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestTodoTool_CleanupSession 测试清理会话
func TestTodoTool_CleanupSession(t *testing.T) {
	todoTool, _ := newTestTodoTool()
	todoTool.lockManager.Operation("session1")
	todoTool.CleanupSession("session1")
	if _, ok := todoTool.lockManager.locks["session1"]; ok {
		t.Fatal("session1 锁应被清除")
	}
}

// TestExtractSessionID_正常 测试提取会话 ID 正常情况
func TestExtractSessionID_正常(t *testing.T) {
	sessionOpt := tool.WithToolSession(&mockSession{sessionID: "sess123"})
	sessionID, err := extractSessionID([]tool.ToolOption{sessionOpt})
	if err != nil {
		t.Fatalf("extractSessionID 返回错误: %v", err)
	}
	if sessionID != "sess123" {
		t.Fatalf("sessionID = %q, want %q", sessionID, "sess123")
	}
}

// TestExtractSessionID_无Session 测试提取会话 ID 无 session 情况
func TestExtractSessionID_无Session(t *testing.T) {
	_, err := extractSessionID(nil)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestFormatTodoItems 测试格式化待办事项
func TestFormatTodoItems(t *testing.T) {
	items := []hschema.TodoItem{
		{ID: "t1", Content: "任务1", Status: hschema.TodoStatusInProgress},
		{ID: "t2", Content: "任务2", Status: hschema.TodoStatusPending},
	}
	result := formatTodoItems(items)
	if result == "" {
		t.Fatal("formatTodoItems 返回空字符串")
	}
}

// TestFormatTodoItems_空列表 测试格式化空列表
func TestFormatTodoItems_空列表(t *testing.T) {
	result := formatTodoItems(nil)
	if result != "" {
		t.Fatalf("期望空字符串，实际 %q", result)
	}
}

// TestValidateSingleInProgress 测试单 in_progress 校验
func TestValidateSingleInProgress(t *testing.T) {
	// 无 in_progress，新增一个：通过
	err := validateSingleInProgress(nil, []string{"task1"}, nil)
	if err != nil {
		t.Fatalf("期望通过，实际错误: %v", err)
	}

	// 已有一个 in_progress，再新增一个：失败
	existing := []hschema.TodoItem{{ID: "existing", Status: hschema.TodoStatusInProgress}}
	err = validateSingleInProgress(existing, []string{"task1"}, nil)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}

	// 已有一个 in_progress，更新同一个：通过
	err = validateSingleInProgress(existing, []string{"existing"}, nil)
	if err != nil {
		t.Fatalf("期望通过，实际错误: %v", err)
	}

	// 没有新的 in_progress：通过
	err = validateSingleInProgress(existing, nil, nil)
	if err != nil {
		t.Fatalf("期望通过，实际错误: %v", err)
	}

	// 已有一个 in_progress，但正在移除它，新增一个：通过
	err = validateSingleInProgress(existing, []string{"task1"}, map[string]struct{}{"existing": {}})
	if err != nil {
		t.Fatalf("期望通过（移除旧 in_progress 后新增），实际错误: %v", err)
	}
}

// TestValidateTargetTaskStatus 测试目标任务状态校验
func TestValidateTargetTaskStatus(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "t1", Status: hschema.TodoStatusInProgress},
		{ID: "t2", Status: hschema.TodoStatusPending},
	}

	// in_progress + 允许 [in_progress, pending]：通过
	err := validateTargetTaskStatus(todos, "t1", []hschema.TodoStatus{hschema.TodoStatusInProgress, hschema.TodoStatusPending})
	if err != nil {
		t.Fatalf("期望通过，实际错误: %v", err)
	}

	// pending + 只允许 [pending]：通过
	err = validateTargetTaskStatus(todos, "t2", []hschema.TodoStatus{hschema.TodoStatusPending})
	if err != nil {
		t.Fatalf("期望通过，实际错误: %v", err)
	}

	// in_progress + 只允许 [pending]：失败
	err = validateTargetTaskStatus(todos, "t1", []hschema.TodoStatus{hschema.TodoStatusPending})
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}

	// 不存在的 ID：失败
	err = validateTargetTaskStatus(todos, "nonexistent", []hschema.TodoStatus{hschema.TodoStatusPending})
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestValidateSingleTodoItem 测试单任务校验
func TestValidateSingleTodoItem(t *testing.T) {
	err := validateSingleTodoItem(map[string]any{"id": "task1"})
	if err != nil {
		t.Fatalf("期望通过，实际错误: %v", err)
	}

	err = validateSingleTodoItem(map[string]any{})
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestTodoModifyUpdate 测试 update 操作
func TestTodoModifyUpdate(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "t1", Content: "旧内容", Status: hschema.TodoStatusInProgress},
		{ID: "t2", Content: "任务2", Status: hschema.TodoStatusPending},
	}

	result, err := todoModifyUpdate(todos, []map[string]any{
		{"id": "t1", "status": "completed"},
		{"id": "t2", "status": "in_progress"},
	})
	if err != nil {
		t.Fatalf("todoModifyUpdate 返回错误: %v", err)
	}
	if result[0].Status != hschema.TodoStatusCompleted {
		t.Fatalf("t1 状态应为 completed")
	}
	if result[1].Status != hschema.TodoStatusInProgress {
		t.Fatalf("t2 状态应为 in_progress")
	}
}

// TestTodoModifyUpdate_重复InProgress 测试 update 操作多个 in_progress
func TestTodoModifyUpdate_重复InProgress(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "t1", Status: hschema.TodoStatusInProgress},
		{ID: "t2", Status: hschema.TodoStatusPending},
	}

	_, err := todoModifyUpdate(todos, []map[string]any{
		{"id": "t2", "status": "in_progress"},
	})
	if err == nil {
		t.Fatal("期望返回错误（多个 in_progress），实际为 nil")
	}
}

// TestTodoModifyUpdate_不存在的ID 测试 update 操作不存在的 ID
func TestTodoModifyUpdate_不存在的ID(t *testing.T) {
	todos := []hschema.TodoItem{{ID: "t1", Status: hschema.TodoStatusPending}}

	_, err := todoModifyUpdate(todos, []map[string]any{
		{"id": "nonexistent", "status": "completed"},
	})
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestTodoModifyDelete 测试 delete 操作
func TestTodoModifyDelete(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "t1", Content: "任务1"},
		{ID: "t2", Content: "任务2"},
		{ID: "t3", Content: "任务3"},
	}

	result, err := todoModifyDelete(todos, []string{"t2"})
	if err != nil {
		t.Fatalf("todoModifyDelete 返回错误: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("期望 2 项，实际 %d", len(result))
	}
	if result[0].ID != "t1" || result[1].ID != "t3" {
		t.Fatal("删除结果不正确")
	}
}

// TestTodoModifyDelete_空IDs 测试 delete 操作空 IDs
func TestTodoModifyDelete_空IDs(t *testing.T) {
	todos := []hschema.TodoItem{{ID: "t1"}}
	_, err := todoModifyDelete(todos, nil)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestTodoModifyCancel 测试 cancel 操作
func TestTodoModifyCancel(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "t1", Status: hschema.TodoStatusPending},
		{ID: "t2", Status: hschema.TodoStatusInProgress},
	}

	result, err := todoModifyCancel(todos, []string{"t1"})
	if err != nil {
		t.Fatalf("todoModifyCancel 返回错误: %v", err)
	}
	if result[0].Status != hschema.TodoStatusCancelled {
		t.Fatal("t1 应为 cancelled")
	}
	if result[1].Status != hschema.TodoStatusInProgress {
		t.Fatal("t2 应保持 in_progress")
	}
}

// TestTodoModifyCancel_不存在的ID 测试 cancel 操作不存在的 ID
func TestTodoModifyCancel_不存在的ID(t *testing.T) {
	todos := []hschema.TodoItem{{ID: "t1"}}
	_, err := todoModifyCancel(todos, []string{"nonexistent"})
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestTodoModifyAppend 测试 append 操作
func TestTodoModifyAppend(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "t1", Content: "任务1", Status: hschema.TodoStatusInProgress},
	}

	result, err := todoModifyAppend(todos, []map[string]any{
		{"id": "t2", "content": "任务2", "activeForm": "执行任务2", "description": "描述2", "status": "pending"},
	})
	if err != nil {
		t.Fatalf("todoModifyAppend 返回错误: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("期望 2 项，实际 %d", len(result))
	}
	if result[1].ID != "t2" {
		t.Fatalf("新任务 ID = %q, want %q", result[1].ID, "t2")
	}
}

// TestTodoModifyAppend_重复ID 测试 append 操作重复 ID
func TestTodoModifyAppend_重复ID(t *testing.T) {
	todos := []hschema.TodoItem{{ID: "t1", Status: hschema.TodoStatusPending}}

	_, err := todoModifyAppend(todos, []map[string]any{
		{"id": "t1", "content": "重复", "status": "pending"},
	})
	if err == nil {
		t.Fatal("期望返回错误（重复 ID），实际为 nil")
	}
}

// TestTodoModifyInsertAfter 测试 insert_after 操作
func TestTodoModifyInsertAfter(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "t1", Content: "任务1", Status: hschema.TodoStatusInProgress},
		{ID: "t3", Content: "任务3", Status: hschema.TodoStatusPending},
	}

	result, err := todoModifyInsertAfter(todos, "t1", []map[string]any{
		{"id": "t2", "content": "任务2", "status": "pending"},
	})
	if err != nil {
		t.Fatalf("todoModifyInsertAfter 返回错误: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("期望 3 项，实际 %d", len(result))
	}
	if result[1].ID != "t2" {
		t.Fatalf("插入位置不正确，result[1].ID = %q, want %q", result[1].ID, "t2")
	}
}

// TestTodoModifyInsertAfter_无效目标状态 测试 insert_after 操作目标状态无效
func TestTodoModifyInsertAfter_无效目标状态(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "t1", Content: "任务1", Status: hschema.TodoStatusCompleted},
	}

	_, err := todoModifyInsertAfter(todos, "t1", []map[string]any{
		{"id": "t2", "content": "任务2", "status": "pending"},
	})
	if err == nil {
		t.Fatal("期望返回错误（目标状态无效），实际为 nil")
	}
}

// TestTodoModifyInsertBefore 测试 insert_before 操作
func TestTodoModifyInsertBefore(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "t1", Content: "任务1", Status: hschema.TodoStatusInProgress},
		{ID: "t3", Content: "任务3", Status: hschema.TodoStatusPending},
	}

	result, err := todoModifyInsertBefore(todos, "t3", []map[string]any{
		{"id": "t2", "content": "任务2", "status": "pending"},
	})
	if err != nil {
		t.Fatalf("todoModifyInsertBefore 返回错误: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("期望 3 项，实际 %d", len(result))
	}
	if result[1].ID != "t2" {
		t.Fatalf("插入位置不正确，result[1].ID = %q, want %q", result[1].ID, "t2")
	}
}

// TestTodoModifyInsertBefore_无效目标状态 测试 insert_before 操作目标状态无效
func TestTodoModifyInsertBefore_无效目标状态(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "t1", Content: "任务1", Status: hschema.TodoStatusInProgress},
	}

	_, err := todoModifyInsertBefore(todos, "t1", []map[string]any{
		{"id": "t2", "content": "任务2", "status": "pending"},
	})
	if err == nil {
		t.Fatal("期望返回错误（目标状态无效），实际为 nil")
	}
}

// TestTodoItemFromMap 测试从 map 构造 TodoItem
func TestTodoItemFromMap(t *testing.T) {
	item := todoItemFromMap(map[string]any{
		"id":          "task1",
		"content":     "内容",
		"activeForm":  "进行中",
		"description": "描述",
		"status":      "in_progress",
	})
	if item.ID != "task1" {
		t.Fatalf("ID = %q, want %q", item.ID, "task1")
	}
	if item.Status != hschema.TodoStatusInProgress {
		t.Fatalf("Status = %v, want in_progress", item.Status)
	}
}

// TestTodoItemFromMap_缺省ID 测试从 map 构造 TodoItem 缺省 ID
func TestTodoItemFromMap_缺省ID(t *testing.T) {
	item := todoItemFromMap(map[string]any{
		"content": "内容",
	})
	if item.ID == "" {
		t.Fatal("ID 不应为空，应自动生成")
	}
	if item.Status != hschema.TodoStatusPending {
		t.Fatalf("Status = %v, want pending", item.Status)
	}
}

// TestCreateTodosTool 测试创建待办事项工具集
func TestCreateTodosTool(t *testing.T) {
	fs := NewMockFsOperation()
	tools := CreateTodosTool("/tmp/workspace", fs, "cn", "agent1")
	if len(tools) != 4 {
		t.Fatalf("期望 4 个工具，实际 %d", len(tools))
	}
	for i, tl := range tools {
		if tl == nil {
			t.Fatalf("tools[%d] 为 nil", i)
		}
	}
}

// TestTodoModifyUpdate_部分字段更新 测试 update 操作部分字段更新
func TestTodoModifyUpdate_部分字段更新(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "t1", Content: "旧内容", ActiveForm: "旧进行中", Description: "旧描述", Status: hschema.TodoStatusPending},
	}

	result, err := todoModifyUpdate(todos, []map[string]any{
		{"id": "t1", "content": "新内容"},
	})
	if err != nil {
		t.Fatalf("todoModifyUpdate 返回错误: %v", err)
	}
	if result[0].Content != "新内容" {
		t.Fatalf("Content = %q, want %q", result[0].Content, "新内容")
	}
	if result[0].ActiveForm != "旧进行中" {
		t.Fatalf("ActiveForm 不应被修改")
	}
	if result[0].Description != "旧描述" {
		t.Fatalf("Description 不应被修改")
	}
}

// TestTodoModifyUpdate_selectedModelID 测试 update 操作更新 selected_model_id
func TestTodoModifyUpdate_selectedModelID(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "t1", Content: "任务1", Status: hschema.TodoStatusPending, SelectedModelID: "fast"},
	}

	result, err := todoModifyUpdate(todos, []map[string]any{
		{"id": "t1", "selected_model_id": "smart"},
	})
	if err != nil {
		t.Fatalf("todoModifyUpdate 返回错误: %v", err)
	}
	if result[0].SelectedModelID != "smart" {
		t.Fatalf("SelectedModelID = %q, want %q", result[0].SelectedModelID, "smart")
	}
}

// TestTodoModifyUpdate_空更新列表 测试 update 操作空更新列表
func TestTodoModifyUpdate_空更新列表(t *testing.T) {
	todos := []hschema.TodoItem{{ID: "t1"}}
	_, err := todoModifyUpdate(todos, nil)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
	// 验证是 exception 错误
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatal("错误类型应为 *exception.BaseError")
	}
	_ = baseErr
}

// TestTodoModifyCancel_空IDs 测试 cancel 操作空 IDs
func TestTodoModifyCancel_空IDs(t *testing.T) {
	todos := []hschema.TodoItem{{ID: "t1"}}
	_, err := todoModifyCancel(todos, nil)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestTodoModifyAppend_空列表 测试 append 操作空列表
func TestTodoModifyAppend_空列表(t *testing.T) {
	todos := []hschema.TodoItem{{ID: "t1"}}
	_, err := todoModifyAppend(todos, nil)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestTodoModifyInsertAfter_空Items 测试 insert_after 操作空 items
func TestTodoModifyInsertAfter_空Items(t *testing.T) {
	todos := []hschema.TodoItem{{ID: "t1", Status: hschema.TodoStatusInProgress}}
	_, err := todoModifyInsertAfter(todos, "t1", nil)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestTodoModifyInsertBefore_空Items 测试 insert_before 操作空 items
func TestTodoModifyInsertBefore_空Items(t *testing.T) {
	todos := []hschema.TodoItem{{ID: "t1", Status: hschema.TodoStatusPending}}
	_, err := todoModifyInsertBefore(todos, "t1", nil)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestTodoModifyInsertAfter_不存在的目标 测试 insert_after 操作不存在的目标
func TestTodoModifyInsertAfter_不存在的目标(t *testing.T) {
	todos := []hschema.TodoItem{{ID: "t1", Status: hschema.TodoStatusInProgress}}
	_, err := todoModifyInsertAfter(todos, "nonexistent", []map[string]any{
		{"id": "t2", "content": "任务2", "status": "pending"},
	})
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestTodoModifyInsertBefore_不存在的目标 测试 insert_before 操作不存在的目标
func TestTodoModifyInsertBefore_不存在的目标(t *testing.T) {
	todos := []hschema.TodoItem{{ID: "t1", Status: hschema.TodoStatusPending}}
	_, err := todoModifyInsertBefore(todos, "nonexistent", []map[string]any{
		{"id": "t2", "content": "任务2", "status": "pending"},
	})
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestTodoModifyUpdate_无效状态 测试 update 操作无效状态
func TestTodoModifyUpdate_无效状态(t *testing.T) {
	todos := []hschema.TodoItem{{ID: "t1", Status: hschema.TodoStatusPending}}
	_, err := todoModifyUpdate(todos, []map[string]any{
		{"id": "t1", "status": "invalid_status"},
	})
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestTodoModifyUpdate_缺少ID 测试 update 操作缺少 ID
func TestTodoModifyUpdate_缺少ID(t *testing.T) {
	todos := []hschema.TodoItem{{ID: "t1"}}
	_, err := todoModifyUpdate(todos, []map[string]any{
		{"status": "completed"},
	})
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestNewTodoCreateTool_Invoke 测试 TodoCreateTool 通过 Invoke 调用
func TestNewTodoCreateTool_Invoke(t *testing.T) {
	fs := NewMockFsOperation()
	lockMgr := NewTodoLockManager()
	todoTool := newTodoTool("/tmp/workspace", fs, lockMgr)
	tl := NewTodoCreateTool(todoTool, "cn", "agent1")

	result, err := tl.Invoke(context.Background(), map[string]any{
		"tasks": []any{
			map[string]any{"id": "t1", "content": "任务1", "activeForm": "执行任务1", "description": "描述1"},
			map[string]any{"id": "t2", "content": "任务2", "activeForm": "执行任务2", "description": "描述2"},
		},
	}, tool.WithToolSession(&mockSession{sessionID: "sess1"}))
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if success, ok := result["success"].(bool); !ok || !success {
		t.Fatal("期望 success=true")
	}
}

// TestNewTodoCreateTool_Invoke_无Session 测试 TodoCreateTool 无 session 调用
func TestNewTodoCreateTool_Invoke_无Session(t *testing.T) {
	fs := NewMockFsOperation()
	lockMgr := NewTodoLockManager()
	todoTool := newTodoTool("/tmp/workspace", fs, lockMgr)
	tl := NewTodoCreateTool(todoTool, "cn", "agent1")

	_, err := tl.Invoke(context.Background(), map[string]any{
		"tasks": []any{map[string]any{"id": "t1", "content": "任务1", "activeForm": "a1", "description": "d1"}},
	})
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestNewTodoCreateTool_Invoke_校验失败 测试 TodoCreateTool 校验失败
func TestNewTodoCreateTool_Invoke_校验失败(t *testing.T) {
	fs := NewMockFsOperation()
	lockMgr := NewTodoLockManager()
	todoTool := newTodoTool("/tmp/workspace", fs, lockMgr)
	tl := NewTodoCreateTool(todoTool, "cn", "agent1")
	sessOpt := tool.WithToolSession(&mockSession{sessionID: "sess1"})

	// 空 tasks
	_, err := tl.Invoke(context.Background(), map[string]any{"tasks": []any{}}, sessOpt)
	if err == nil {
		t.Fatal("期望返回错误（空 tasks），实际为 nil")
	}

	// 缺少 content
	_, err = tl.Invoke(context.Background(), map[string]any{
		"tasks": []any{map[string]any{"id": "t1", "activeForm": "a1", "description": "d1"}},
	}, sessOpt)
	if err == nil {
		t.Fatal("期望返回错误（缺少 content），实际为 nil")
	}

	// 缺少 id
	_, err = tl.Invoke(context.Background(), map[string]any{
		"tasks": []any{map[string]any{"content": "c1", "activeForm": "a1", "description": "d1"}},
	}, sessOpt)
	if err == nil {
		t.Fatal("期望返回错误（缺少 id），实际为 nil")
	}

	// 重复 id
	_, err = tl.Invoke(context.Background(), map[string]any{
		"tasks": []any{
			map[string]any{"id": "t1", "content": "c1", "activeForm": "a1", "description": "d1"},
			map[string]any{"id": "t1", "content": "c2", "activeForm": "a2", "description": "d2"},
		},
	}, sessOpt)
	if err == nil {
		t.Fatal("期望返回错误（重复 id），实际为 nil")
	}
}

// TestNewTodoListTool_Invoke 测试 TodoListTool 通过 Invoke 调用
func TestNewTodoListTool_Invoke(t *testing.T) {
	fs := NewMockFsOperation()
	lockMgr := NewTodoLockManager()
	todoTool := newTodoTool("/tmp/workspace", fs, lockMgr)

	// 预写数据
	items := []hschema.TodoItem{
		{ID: "t1", Content: "任务1", Status: hschema.TodoStatusInProgress},
		{ID: "t2", Content: "任务2", Status: hschema.TodoStatusPending},
		{ID: "t3", Content: "任务3", Status: hschema.TodoStatusCompleted},
	}
	_ = todoTool.SaveTodos(context.Background(), "sess1", items)

	tl := NewTodoListTool(todoTool, "cn", "agent1")
	result, err := tl.Invoke(context.Background(), map[string]any{}, tool.WithToolSession(&mockSession{sessionID: "sess1"}))
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if success, ok := result["success"].(bool); !ok || !success {
		t.Fatal("期望 success=true")
	}
}

// TestNewTodoListTool_Invoke_空列表 测试 TodoListTool 空列表
func TestNewTodoListTool_Invoke_空列表(t *testing.T) {
	fs := NewMockFsOperation()
	lockMgr := NewTodoLockManager()
	todoTool := newTodoTool("/tmp/workspace", fs, lockMgr)
	tl := NewTodoListTool(todoTool, "cn", "agent1")

	result, err := tl.Invoke(context.Background(), map[string]any{}, tool.WithToolSession(&mockSession{sessionID: "sess1"}))
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if success, ok := result["success"].(bool); !ok || !success {
		t.Fatal("期望 success=true")
	}
}

// TestNewTodoGetTool_Invoke 测试 TodoGetTool 通过 Invoke 调用
func TestNewTodoGetTool_Invoke(t *testing.T) {
	fs := NewMockFsOperation()
	lockMgr := NewTodoLockManager()
	todoTool := newTodoTool("/tmp/workspace", fs, lockMgr)

	items := []hschema.TodoItem{{ID: "t1", Content: "任务1", Status: hschema.TodoStatusPending}}
	_ = todoTool.SaveTodos(context.Background(), "sess1", items)

	tl := NewTodoGetTool(todoTool, "cn", "agent1")
	result, err := tl.Invoke(context.Background(), map[string]any{"id": "t1"}, tool.WithToolSession(&mockSession{sessionID: "sess1"}))
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if success, ok := result["success"].(bool); !ok || !success {
		t.Fatal("期望 success=true")
	}
}

// TestNewTodoGetTool_Invoke_未找到 测试 TodoGetTool 未找到
func TestNewTodoGetTool_Invoke_未找到(t *testing.T) {
	fs := NewMockFsOperation()
	lockMgr := NewTodoLockManager()
	todoTool := newTodoTool("/tmp/workspace", fs, lockMgr)

	tl := NewTodoGetTool(todoTool, "cn", "agent1")
	_, err := tl.Invoke(context.Background(), map[string]any{"id": "nonexistent"}, tool.WithToolSession(&mockSession{sessionID: "sess1"}))
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestNewTodoModifyTool_Invoke 测试 TodoModifyTool 通过 Invoke 调用
func TestNewTodoModifyTool_Invoke(t *testing.T) {
	fs := NewMockFsOperation()
	lockMgr := NewTodoLockManager()
	todoTool := newTodoTool("/tmp/workspace", fs, lockMgr)
	sessOpt := tool.WithToolSession(&mockSession{sessionID: "sess1"})

	// 先创建待办事项
	createTool := NewTodoCreateTool(todoTool, "cn", "agent1")
	_, _ = createTool.Invoke(context.Background(), map[string]any{
		"tasks": []any{
			map[string]any{"id": "t1", "content": "任务1", "activeForm": "a1", "description": "d1"},
			map[string]any{"id": "t2", "content": "任务2", "activeForm": "a2", "description": "d2"},
		},
	}, sessOpt)

	modifyTool := NewTodoModifyTool(todoTool, "cn", "agent1")

	// update: t1→completed, t2→in_progress
	result, err := modifyTool.Invoke(context.Background(), map[string]any{
		"action": "update",
		"todos": []any{
			map[string]any{"id": "t1", "status": "completed"},
			map[string]any{"id": "t2", "status": "in_progress"},
		},
	}, sessOpt)
	if err != nil {
		t.Fatalf("update 操作返回错误: %v", err)
	}
	if success, ok := result["success"].(bool); !ok || !success {
		t.Fatal("期望 success=true")
	}

	// cancel
	result, err = modifyTool.Invoke(context.Background(), map[string]any{
		"action": "cancel",
		"ids":    []any{"t2"},
	}, sessOpt)
	if err != nil {
		t.Fatalf("cancel 操作返回错误: %v", err)
	}
	_ = result

	// delete — 先创建再删
	_, _ = createTool.Invoke(context.Background(), map[string]any{
		"tasks": []any{map[string]any{"id": "t3", "content": "任务3", "activeForm": "a3", "description": "d3"}},
	}, sessOpt)
	result, err = modifyTool.Invoke(context.Background(), map[string]any{
		"action": "delete",
		"ids":    []any{"t3"},
	}, sessOpt)
	if err != nil {
		t.Fatalf("delete 操作返回错误: %v", err)
	}
	_ = result

	// append
	result, err = modifyTool.Invoke(context.Background(), map[string]any{
		"action": "append",
		"todos": []any{map[string]any{"id": "t4", "content": "追加任务", "activeForm": "执行追加", "description": "追加描述", "status": "pending"}},
	}, sessOpt)
	if err != nil {
		t.Fatalf("append 操作返回错误: %v", err)
	}
	_ = result
}

// TestNewTodoModifyTool_InsertActions 测试 TodoModifyTool insert 操作
func TestNewTodoModifyTool_InsertActions(t *testing.T) {
	fs := NewMockFsOperation()
	lockMgr := NewTodoLockManager()
	todoTool := newTodoTool("/tmp/workspace", fs, lockMgr)
	sessOpt := tool.WithToolSession(&mockSession{sessionID: "sess1"})

	// 先创建
	createTool := NewTodoCreateTool(todoTool, "cn", "agent1")
	_, _ = createTool.Invoke(context.Background(), map[string]any{
		"tasks": []any{
			map[string]any{"id": "t1", "content": "任务1", "activeForm": "a1", "description": "d1"},
			map[string]any{"id": "t3", "content": "任务3", "activeForm": "a3", "description": "d3"},
		},
	}, sessOpt)

	modifyTool := NewTodoModifyTool(todoTool, "cn", "agent1")

	// insert_after
	result, err := modifyTool.Invoke(context.Background(), map[string]any{
		"action": "insert_after",
		"todo_data": map[string]any{
			"target_id": "t1",
			"items": []any{map[string]any{"id": "t2", "content": "插入任务", "status": "pending"}},
		},
	}, sessOpt)
	if err != nil {
		t.Fatalf("insert_after 操作返回错误: %v", err)
	}
	_ = result

	// insert_before
	result, err = modifyTool.Invoke(context.Background(), map[string]any{
		"action": "insert_before",
		"todo_data": map[string]any{
			"target_id": "t3",
			"items": []any{map[string]any{"id": "t2b", "content": "前插任务", "status": "pending"}},
		},
	}, sessOpt)
	if err != nil {
		t.Fatalf("insert_before 操作返回错误: %v", err)
	}
	_ = result
}

// TestNewTodoModifyTool_无效Action 测试 TodoModifyTool 无效 action
func TestNewTodoModifyTool_无效Action(t *testing.T) {
	fs := NewMockFsOperation()
	lockMgr := NewTodoLockManager()
	todoTool := newTodoTool("/tmp/workspace", fs, lockMgr)
	sessOpt := tool.WithToolSession(&mockSession{sessionID: "sess1"})

	modifyTool := NewTodoModifyTool(todoTool, "cn", "agent1")
	_, err := modifyTool.Invoke(context.Background(), map[string]any{
		"action": "invalid_action",
	}, sessOpt)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestNewTodoModifyTool_缺少TodoData 测试 TodoModifyTool 缺少 todo_data
func TestNewTodoModifyTool_缺少TodoData(t *testing.T) {
	fs := NewMockFsOperation()
	lockMgr := NewTodoLockManager()
	todoTool := newTodoTool("/tmp/workspace", fs, lockMgr)
	sessOpt := tool.WithToolSession(&mockSession{sessionID: "sess1"})

	modifyTool := NewTodoModifyTool(todoTool, "cn", "agent1")
	_, err := modifyTool.Invoke(context.Background(), map[string]any{
		"action": "insert_after",
	}, sessOpt)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestNewTodoGetTool_Invoke_空ID 测试 TodoGetTool 空 ID
func TestNewTodoGetTool_Invoke_空ID(t *testing.T) {
	fs := NewMockFsOperation()
	lockMgr := NewTodoLockManager()
	todoTool := newTodoTool("/tmp/workspace", fs, lockMgr)
	sessOpt := tool.WithToolSession(&mockSession{sessionID: "sess1"})

	tl := NewTodoGetTool(todoTool, "cn", "agent1")
	_, err := tl.Invoke(context.Background(), map[string]any{"id": ""}, sessOpt)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}
