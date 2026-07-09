package skilldev

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestService_Handle_Dispatch 测试 Handle 方法分发。
func TestService_Handle_Dispatch(t *testing.T) {
	tmpDir := t.TempDir()
	deps := &SkillDevDeps{
		StateStore:        NewStateStore(tmpDir),
		WorkspaceProvider: NewWorkspaceProvider(tmpDir),
	}
	svc := NewSkillDevService(deps)

	// 测试未知 method
	req := &schema.AgentRequest{
		RequestID: "req-1",
		ChannelID: "ch-1",
		ReqMethod: schema.ReqMethod("unknown.method"),
	}
	results, err := svc.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle 返回错误: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("期望有结果，实际为空")
	}
	if results[0]["event_type"] != "skilldev.error" {
		t.Errorf("期望 event_type skilldev.error, 实际: %v", results[0]["event_type"])
	}
}

// TestService_Handle_Status_ListTasks 测试 handleStatus 列出任务。
func TestService_Handle_Status_ListTasks(t *testing.T) {
	tmpDir := t.TempDir()
	deps := &SkillDevDeps{
		StateStore:        NewStateStore(tmpDir),
		WorkspaceProvider: NewWorkspaceProvider(tmpDir),
	}
	svc := NewSkillDevService(deps)

	// 先保存一个任务状态
	state := NewSkillDevState("task-1")
	state.Stage = SkillDevStagePlanConfirm
	if err := deps.StateStore.SaveState("task-1", state); err != nil {
		t.Fatalf("SaveState 失败: %v", err)
	}

	// 不传 task_id → 返回任务列表
	params := map[string]any{}
	results, err := svc.handleStatus(context.Background(), params, "req-1", "ch-1")
	if err != nil {
		t.Fatalf("handleStatus 返回错误: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("期望有结果，实际为空")
	}
	if results[0]["ok"] != true {
		t.Errorf("期望 ok=true, 实际: %v", results[0]["ok"])
	}
	tasks, ok := results[0]["tasks"].([]string)
	if !ok {
		t.Fatalf("tasks 类型错误: %T", results[0]["tasks"])
	}
	if len(tasks) != 1 || tasks[0] != "task-1" {
		t.Errorf("期望 tasks=[task-1], 实际: %v", tasks)
	}
}

// TestService_Handle_Status_SingleTask 测试 handleStatus 查询单个任务。
func TestService_Handle_Status_SingleTask(t *testing.T) {
	tmpDir := t.TempDir()
	deps := &SkillDevDeps{
		StateStore:        NewStateStore(tmpDir),
		WorkspaceProvider: NewWorkspaceProvider(tmpDir),
	}
	svc := NewSkillDevService(deps)

	// 保存任务状态
	state := NewSkillDevState("task-2")
	state.Stage = SkillDevStageGenerate
	if err := deps.StateStore.SaveState("task-2", state); err != nil {
		t.Fatalf("SaveState 失败: %v", err)
	}

	// 传 task_id → 返回单个任务状态
	params := map[string]any{"task_id": "task-2"}
	results, err := svc.handleStatus(context.Background(), params, "req-2", "ch-2")
	if err != nil {
		t.Fatalf("handleStatus 返回错误: %v", err)
	}
	if results[0]["ok"] != true {
		t.Errorf("期望 ok=true, 实际: %v", results[0]["ok"])
	}
	if results[0]["stage"] != "generate" {
		t.Errorf("期望 stage=generate, 实际: %v", results[0]["stage"])
	}
}

// TestService_Handle_Status_TaskNotFound 测试 handleStatus 任务不存在。
func TestService_Handle_Status_TaskNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	deps := &SkillDevDeps{
		StateStore:        NewStateStore(tmpDir),
		WorkspaceProvider: NewWorkspaceProvider(tmpDir),
	}
	svc := NewSkillDevService(deps)

	params := map[string]any{"task_id": "nonexistent"}
	results, err := svc.handleStatus(context.Background(), params, "req-3", "ch-3")
	if err != nil {
		t.Fatalf("handleStatus 返回错误: %v", err)
	}
	if results[0]["ok"] != false {
		t.Errorf("期望 ok=false, 实际: %v", results[0]["ok"])
	}
}

// TestService_Handle_Cancel 测试 handleCancel。
func TestService_Handle_Cancel(t *testing.T) {
	tmpDir := t.TempDir()
	deps := &SkillDevDeps{
		StateStore:        NewStateStore(tmpDir),
		WorkspaceProvider: NewWorkspaceProvider(tmpDir),
	}
	svc := NewSkillDevService(deps)

	params := map[string]any{"task_id": "cancel-1"}
	results, err := svc.handleCancel(context.Background(), params, "req-4", "ch-4")
	if err != nil {
		t.Fatalf("handleCancel 返回错误: %v", err)
	}
	if results[0]["ok"] != true {
		t.Errorf("期望 ok=true, 实际: %v", results[0]["ok"])
	}
}

// TestService_Handle_FileList 测试 handleFileList。
func TestService_Handle_FileList(t *testing.T) {
	tmpDir := t.TempDir()
	deps := &SkillDevDeps{
		StateStore:        NewStateStore(tmpDir),
		WorkspaceProvider: NewWorkspaceProvider(tmpDir),
	}
	svc := NewSkillDevService(deps)

	// 创建 skill 目录和文件
	taskDir := filepath.Join(tmpDir, "task-files", "skill")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "SKILL.md"), []byte("# Test Skill"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	params := map[string]any{"task_id": "task-files"}
	results, err := svc.handleFileList(context.Background(), params, "req-5", "ch-5")
	if err != nil {
		t.Fatalf("handleFileList 返回错误: %v", err)
	}
	if results[0]["ok"] != true {
		t.Errorf("期望 ok=true, 实际: %v", results[0]["ok"])
	}
	tree, ok := results[0]["tree"].([]map[string]any)
	if !ok {
		t.Fatalf("tree 类型错误: %T", results[0]["tree"])
	}
	if len(tree) == 0 {
		t.Error("期望文件树非空")
	}
}

// TestService_Handle_FileList_NoTaskID 测试 handleFileList 缺少 task_id。
func TestService_Handle_FileList_NoTaskID(t *testing.T) {
	tmpDir := t.TempDir()
	deps := &SkillDevDeps{
		StateStore:        NewStateStore(tmpDir),
		WorkspaceProvider: NewWorkspaceProvider(tmpDir),
	}
	svc := NewSkillDevService(deps)

	params := map[string]any{}
	results, err := svc.handleFileList(context.Background(), params, "req-6", "ch-6")
	if err != nil {
		t.Fatalf("handleFileList 返回错误: %v", err)
	}
	if results[0]["ok"] != false {
		t.Errorf("期望 ok=false, 实际: %v", results[0]["ok"])
	}
}

// TestService_Handle_FileRead 测试 handleFileRead。
func TestService_Handle_FileRead(t *testing.T) {
	tmpDir := t.TempDir()
	deps := &SkillDevDeps{
		StateStore:        NewStateStore(tmpDir),
		WorkspaceProvider: NewWorkspaceProvider(tmpDir),
	}
	svc := NewSkillDevService(deps)

	// 创建 skill 目录和文件
	taskDir := filepath.Join(tmpDir, "task-read", "skill")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "SKILL.md"), []byte("# Test Content"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	params := map[string]any{"task_id": "task-read", "path": "SKILL.md"}
	results, err := svc.handleFileRead(context.Background(), params, "req-7", "ch-7")
	if err != nil {
		t.Fatalf("handleFileRead 返回错误: %v", err)
	}
	if results[0]["ok"] != true {
		t.Errorf("期望 ok=true, 实际: %v", results[0]["ok"])
	}
	if results[0]["content"] != "# Test Content" {
		t.Errorf("期望 content='# Test Content', 实际: %v", results[0]["content"])
	}
}

// TestService_Handle_FileRead_PathTraversal 测试 handleFileRead 路径越界防护。
func TestService_Handle_FileRead_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	deps := &SkillDevDeps{
		StateStore:        NewStateStore(tmpDir),
		WorkspaceProvider: NewWorkspaceProvider(tmpDir),
	}
	svc := NewSkillDevService(deps)

	// 创建 skill 目录
	taskDir := filepath.Join(tmpDir, "task-traversal", "skill")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	// 尝试路径越界
	params := map[string]any{"task_id": "task-traversal", "path": "../../etc/passwd"}
	results, err := svc.handleFileRead(context.Background(), params, "req-8", "ch-8")
	if err != nil {
		t.Fatalf("handleFileRead 返回错误: %v", err)
	}
	if results[0]["ok"] != false {
		t.Errorf("期望 ok=false（路径越界），实际: %v", results[0]["ok"])
	}
}

// TestService_Handle_Status_WithReqMethod 测试通过 Handle 入口调用 status。
func TestService_Handle_Status_WithReqMethod(t *testing.T) {
	tmpDir := t.TempDir()
	deps := &SkillDevDeps{
		StateStore:        NewStateStore(tmpDir),
		WorkspaceProvider: NewWorkspaceProvider(tmpDir),
	}
	svc := NewSkillDevService(deps)

	// 保存任务状态
	state := NewSkillDevState("task-handle")
	state.Stage = SkillDevStagePackage
	if err := deps.StateStore.SaveState("task-handle", state); err != nil {
		t.Fatalf("SaveState 失败: %v", err)
	}

	// 通过 Handle 入口调用
	paramsJSON, _ := json.Marshal(map[string]any{"task_id": "task-handle"})
	req := &schema.AgentRequest{
		RequestID: "req-9",
		ChannelID: "ch-9",
		ReqMethod: schema.ReqMethodSkilldevStatus,
		Params:    paramsJSON,
	}
	results, err := svc.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle 返回错误: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("期望有结果")
	}
	if results[0]["ok"] != true {
		t.Errorf("期望 ok=true, 实际: %v", results[0]["ok"])
	}
}

// TestService_Download_NoZip 测试 handleDownload 未打包。
func TestService_Download_NoZip(t *testing.T) {
	tmpDir := t.TempDir()
	deps := &SkillDevDeps{
		StateStore:        NewStateStore(tmpDir),
		WorkspaceProvider: NewWorkspaceProvider(tmpDir),
	}
	svc := NewSkillDevService(deps)

	// 保存未打包的任务状态
	state := NewSkillDevState("task-nozip")
	state.Stage = SkillDevStageGenerate
	if err := deps.StateStore.SaveState("task-nozip", state); err != nil {
		t.Fatalf("SaveState 失败: %v", err)
	}

	params := map[string]any{"task_id": "task-nozip"}
	results, err := svc.handleDownload(context.Background(), params, "req-10", "ch-10")
	if err != nil {
		t.Fatalf("handleDownload 返回错误: %v", err)
	}
	if results[0]["ok"] != false {
		t.Errorf("期望 ok=false（未打包），实际: %v", results[0]["ok"])
	}
}

// TestBuildFileTree 测试文件树构建。
func TestBuildFileTree(t *testing.T) {
	tmpDir := t.TempDir()

	// 创建目录结构
	skillDir := filepath.Join(tmpDir, "skill")
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Test"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "helper.py"), []byte("print('hi')"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
	// 隐藏文件应被跳过
	if err := os.WriteFile(filepath.Join(skillDir, ".hidden"), []byte("hidden"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	tree := buildFileTree(skillDir, skillDir)

	// 应有 2 个顶层条目：scripts/ 和 SKILL.md（隐藏文件被跳过）
	if len(tree) != 2 {
		t.Fatalf("期望 2 个顶层条目，实际: %d", len(tree))
	}

	// 第一个应为目录（排序：目录在前）
	if tree[0]["type"] != "dir" {
		t.Errorf("期望第一个为 dir, 实际: %v", tree[0]["type"])
	}
	if tree[0]["path"] != "scripts/" {
		t.Errorf("期望 path=scripts/, 实际: %v", tree[0]["path"])
	}

	// 第二个应为文件
	if tree[1]["type"] != "file" {
		t.Errorf("期望第二个为 file, 实际: %v", tree[1]["type"])
	}
	if tree[1]["path"] != "SKILL.md" {
		t.Errorf("期望 path=SKILL.md, 实际: %v", tree[1]["path"])
	}

	// scripts/ 下应有 helper.py
	children, ok := tree[0]["children"].([]map[string]any)
	if !ok {
		t.Fatalf("children 类型错误: %T", tree[0]["children"])
	}
	if len(children) != 1 {
		t.Fatalf("期望 1 个子文件，实际: %d", len(children))
	}
	if children[0]["path"] != "scripts/helper.py" {
		t.Errorf("期望 path=scripts/helper.py, 实际: %v", children[0]["path"])
	}
}

// TestEventToPayload 测试事件转负载。
func TestEventToPayload(t *testing.T) {
	evt := SkillDevEvent{
		EventType: SkillDevEventTypeStageChanged,
		Payload: map[string]any{
			"task_id": "t1",
			"stage":   "init",
		},
		TaskID: "t1",
	}

	payload := eventToPayload(evt)
	if payload["event_type"] != "skilldev.stage_changed" {
		t.Errorf("期望 event_type=skilldev.stage_changed, 实际: %v", payload["event_type"])
	}
	if payload["stage"] != "init" {
		t.Errorf("期望 stage=init, 实际: %v", payload["stage"])
	}
}

// TestErrorChunk 测试错误分块构造。
func TestErrorChunk(t *testing.T) {
	chunk := errorChunk("req-1", "ch-1", "测试错误")
	if chunk["event_type"] != "skilldev.error" {
		t.Errorf("期望 event_type=skilldev.error, 实际: %v", chunk["event_type"])
	}
	if chunk["error"] != "测试错误" {
		t.Errorf("期望 error=测试错误, 实际: %v", chunk["error"])
	}
	if chunk["is_complete"] != true {
		t.Errorf("期望 is_complete=true, 实际: %v", chunk["is_complete"])
	}
}
