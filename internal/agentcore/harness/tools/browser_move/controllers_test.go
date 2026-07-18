package browser_move

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewActionController 测试创建动作控制器
func TestNewActionController(t *testing.T) {
	ctl := NewActionController()
	if ctl == nil {
		t.Fatal("NewActionController 返回 nil")
	}
	if len(ctl.ListActions()) != 0 {
		t.Error("初始动作列表应为空")
	}
}

// TestActionController_RegisterAction 测试注册动作
func TestActionController_RegisterAction(t *testing.T) {
	ctl := NewActionController()

	handler := func(_ context.Context, _ string, _ string, _ map[string]any) ActionResult {
		return ActionResult{"ok": true}
	}

	if err := ctl.RegisterAction("test_action", handler, true); err != nil {
		t.Fatalf("RegisterAction 失败: %v", err)
	}

	actions := ctl.ListActions()
	if len(actions) != 1 || actions[0] != "test_action" {
		t.Errorf("ListActions = %v, want [test_action]", actions)
	}
}

// TestActionController_RegisterAction_空名称 测试空名称报错
func TestActionController_RegisterAction_空名称(t *testing.T) {
	ctl := NewActionController()
	handler := func(_ context.Context, _ string, _ string, _ map[string]any) ActionResult {
		return ActionResult{"ok": true}
	}
	if err := ctl.RegisterAction("", handler, true); err == nil {
		t.Error("空名称应返回错误")
	}
}

// TestActionController_RegisterAction_不可覆盖 测试不可覆盖
func TestActionController_RegisterAction_不可覆盖(t *testing.T) {
	ctl := NewActionController()
	handler := func(_ context.Context, _ string, _ string, _ map[string]any) ActionResult {
		return ActionResult{"ok": true}
	}
	ctl.RegisterAction("test", handler, true)

	if err := ctl.RegisterAction("test", handler, false); err == nil {
		t.Error("不可覆盖模式下重复注册应返回错误")
	}
}

// TestActionController_RegisterActionSpec 测试注册动作规格
func TestActionController_RegisterActionSpec(t *testing.T) {
	ctl := NewActionController()
	handler := func(_ context.Context, _ string, _ string, _ map[string]any) ActionResult {
		return ActionResult{"ok": true}
	}
	ctl.RegisterAction("ping", handler, true)
	ctl.RegisterActionSpec("ping", ActionSpec{
		Summary:   "Test action",
		WhenToUse: "When testing",
		Params:    map[string]string{"key": "string: a key"},
	})

	descs := ctl.DescribeActions()
	if descs["ping"].Summary != "Test action" {
		t.Errorf("Summary = %q, want Test action", descs["ping"].Summary)
	}
}

// TestActionController_RunAction_未知动作 测试未知动作
func TestActionController_RunAction_未知动作(t *testing.T) {
	ctl := NewActionController()
	result := ctl.RunAction(context.Background(), "nonexistent", "", "", nil)
	if result["ok"] != false {
		t.Error("未知动作应返回 ok=false")
	}
	if !strings.Contains(result["error"].(string), "unknown action") {
		t.Errorf("error = %v, want unknown action", result["error"])
	}
}

// TestActionController_RunAction_递归阻止 测试递归调用阻止
func TestActionController_RunAction_递归阻止(t *testing.T) {
	ctl := NewActionController()
	handler := func(_ context.Context, _ string, _ string, _ map[string]any) ActionResult {
		return ActionResult{"ok": true}
	}
	ctl.RegisterAction("browser_task", handler, true)

	ctx := WithBrowserWorkerAction(context.Background())
	result := ctl.RunAction(ctx, "browser_task", "", "", nil)
	if result["ok"] != false {
		t.Error("递归调用应返回 ok=false")
	}
	if !strings.Contains(result["error"].(string), "recursive_browser_task_blocked") {
		t.Errorf("error = %v, want recursive_browser_task_blocked", result["error"])
	}
}

// TestActionController_BuiltinActions 测试内置动作注册
func TestActionController_BuiltinActions(t *testing.T) {
	ctl := NewActionController()
	ctl.RegisterBuiltinActions()

	actions := ctl.ListActions()
	expectedActions := []string{
		"browser_drag_and_drop",
		"browser_get_element_coordinates",
		"browser_set_input_files",
		"browser_task",
		"echo",
		"list_upload_files",
		"ping",
		"run_browser_task",
	}

	if len(actions) != len(expectedActions) {
		t.Errorf("内置动作数量 = %d, want %d", len(actions), len(expectedActions))
	}

	for _, name := range expectedActions {
		found := false
		for _, a := range actions {
			if a == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("缺少内置动作: %s", name)
		}
	}
}

// TestActionController_Ping 测试 ping 动作
func TestActionController_Ping(t *testing.T) {
	ctl := NewActionController()
	ctl.RegisterBuiltinActions()

	result := ctl.RunAction(context.Background(), "ping", "s1", "r1", map[string]any{"extra": "data"})
	if result["ok"] != true {
		t.Error("ping 应返回 ok=true")
	}
	if result["pong"] != true {
		t.Error("ping 应返回 pong=true")
	}
}

// TestActionController_Echo 测试 echo 动作
func TestActionController_Echo(t *testing.T) {
	ctl := NewActionController()
	ctl.RegisterBuiltinActions()

	result := ctl.RunAction(context.Background(), "echo", "", "", map[string]any{"text": "hello"})
	if result["ok"] != true {
		t.Error("echo 应返回 ok=true")
	}
	if result["text"] != "hello" {
		t.Errorf("text = %v, want hello", result["text"])
	}
}

// TestActionController_BrowserTask_未绑定 测试 browser_task 未绑定运行时
func TestActionController_BrowserTask_未绑定(t *testing.T) {
	ctl := NewActionController()
	ctl.RegisterBuiltinActions()

	result := ctl.RunAction(context.Background(), "browser_task", "", "", map[string]any{"task": "do something"})
	if result["ok"] != false {
		t.Error("未绑定运行时 browser_task 应返回 ok=false")
	}
	if !strings.Contains(result["error"].(string), "runtime_not_bound") {
		t.Errorf("error = %v, want runtime_not_bound", result["error"])
	}
}

// TestActionController_BrowserTask_缺参数 测试 browser_task 缺少 task 参数
func TestActionController_BrowserTask_缺参数(t *testing.T) {
	ctl := NewActionController()
	ctl.RegisterBuiltinActions()
	ctl.BindRuntimeRunner(func(_ context.Context, _ string, _ string, _ string, _ *int) ActionResult {
		return ActionResult{"ok": true}
	})

	result := ctl.RunAction(context.Background(), "browser_task", "", "", map[string]any{})
	if result["ok"] != false {
		t.Error("缺少 task 参数应返回 ok=false")
	}
	if !strings.Contains(result["error"].(string), "missing required parameter: task") {
		t.Errorf("error = %v, want missing required parameter: task", result["error"])
	}
}

// TestActionController_ListUploadFiles_未配置 测试 list_upload_files 未配置
func TestActionController_ListUploadFiles_未配置(t *testing.T) {
	ctl := NewActionController()
	ctl.RegisterBuiltinActions()

	// 清除环境变量
	os.Unsetenv("BROWSER_UPLOAD_ROOT")
	os.Unsetenv("PLAYWRIGHT_UPLOAD_ROOT")

	result := ctl.RunAction(context.Background(), "list_upload_files", "", "", nil)
	if result["ok"] != false {
		t.Error("未配置上传根目录应返回 ok=false")
	}
}

// TestActionController_ListUploadFiles_有目录 测试 list_upload_files 有配置目录
func TestActionController_ListUploadFiles_有目录(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("BROWSER_UPLOAD_ROOT", tmpDir)
	defer os.Unsetenv("BROWSER_UPLOAD_ROOT")

	// 创建测试文件
	os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("hello"), 0o644)

	ctl := NewActionController()
	ctl.RegisterBuiltinActions()

	result := ctl.RunAction(context.Background(), "list_upload_files", "", "", nil)
	if result["ok"] != true {
		t.Error("有配置目录应返回 ok=true")
	}
	files, ok := result["files"].([]map[string]any)
	if !ok || len(files) != 1 {
		t.Errorf("files = %v, want 1 file", result["files"])
	}
}

// TestActionController_BindCodeExecutor 测试绑定代码执行器
func TestActionController_BindCodeExecutor(t *testing.T) {
	ctl := NewActionController()

	if ctl.CodeExecutor() != nil {
		t.Error("初始 CodeExecutor 应为 nil")
	}

	executor := func(_ context.Context, jsCode string) (any, error) {
		return jsCode, nil
	}
	ctl.BindCodeExecutor(executor)

	if ctl.CodeExecutor() == nil {
		t.Error("绑定后 CodeExecutor 不应为 nil")
	}

	ctl.ClearCodeExecutor()
	if ctl.CodeExecutor() != nil {
		t.Error("清除后 CodeExecutor 应为 nil")
	}
}

// TestNormalizeActionName 测试动作名称规范化
func TestNormalizeActionName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello", "hello"},
		{"  TEST  ", "test"},
		{"", ""},
		{"Browser_Task", "browser_task"},
	}
	for _, tt := range tests {
		if got := normalizeActionName(tt.input); got != tt.want {
			t.Errorf("normalizeActionName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestIsBrowserWorkerAction 测试上下文标记
func TestIsBrowserWorkerAction(t *testing.T) {
	ctx := context.Background()
	if IsBrowserWorkerAction(ctx) {
		t.Error("默认上下文不应标记为 browser worker action")
	}

	ctx = WithBrowserWorkerAction(ctx)
	if !IsBrowserWorkerAction(ctx) {
		t.Error("WithBrowserWorkerAction 后应返回 true")
	}
}

// TestBuildRunCodeTask 测试 run_code 任务提示构建
func TestBuildRunCodeTask(t *testing.T) {
	result := buildRunCodeTask("console.log(1)", "test purpose")
	if !strings.Contains(result, "Execute this browser operation: test purpose.") {
		t.Error("应包含操作目的")
	}
	if !strings.Contains(result, "Call browser_run_code exactly once") {
		t.Error("应包含 browser_run_code 调用指令")
	}
	if !strings.Contains(result, `"code":`) {
		t.Error("应包含 code 字段")
	}
}

// TestBuildDragPayload 测试拖拽载荷构建
func TestBuildDragPayload(t *testing.T) {
	kwargs := map[string]any{
		"element_source": "  #source  ",
		"element_target": "#target",
		"coord_source_x": 100,
		"coord_source_y": 200,
		"steps":          5,
	}

	payload := buildDragPayload(kwargs)
	if payload["element_source"] != "#source" {
		t.Errorf("element_source = %v, want #source", payload["element_source"])
	}
	if payload["element_target"] != "#target" {
		t.Errorf("element_target = %v, want #target", payload["element_target"])
	}
}

// TestHasSelectorInputs 测试选择器输入检查
func TestHasSelectorInputs(t *testing.T) {
	if hasSelectorInputs(map[string]any{"element_source": "a", "element_target": "b"}) != true {
		t.Error("两个都有时应返回 true")
	}
	if hasSelectorInputs(map[string]any{"element_source": "a"}) != false {
		t.Error("只有一个时应返回 false")
	}
}

// TestHasCoordinateInputs 测试坐标输入检查
func TestHasCoordinateInputs(t *testing.T) {
	if hasCoordinateInputs(map[string]any{
		"coord_source_x": 1, "coord_source_y": 2,
		"coord_target_x": 3, "coord_target_y": 4,
	}) != true {
		t.Error("四个都有时应返回 true")
	}
	if hasCoordinateInputs(map[string]any{
		"coord_source_x": 1, "coord_source_y": 2,
	}) != false {
		t.Error("缺少部分时应返回 false")
	}
}

// TestBuildSetInputFilesScript 测试文件输入脚本构建
func TestBuildSetInputFilesScript(t *testing.T) {
	script := buildSetInputFilesScript("#upload", []string{"/tmp/file.txt"})
	if !strings.Contains(script, "async (page) =>") {
		t.Error("应包含 async (page) => 入口")
	}
	if !strings.Contains(script, "#upload") {
		t.Error("应包含选择器")
	}
	if !strings.Contains(script, "/tmp/file.txt") {
		t.Error("应包含文件路径")
	}
}

// TestToIntOrNone 测试整数转换
func TestToIntOrNone(t *testing.T) {
	if v, _ := toIntOrNone(nil); v != nil {
		t.Error("nil 应返回 nil")
	}
	if v, _ := toIntOrNone(42); *v != 42 {
		t.Errorf("42 转换后 = %d, want 42", *v)
	}
	if v, _ := toIntOrNone(float64(3.14)); *v != 3 {
		t.Errorf("3.14 转换后 = %d, want 3", *v)
	}
	if v, _ := toIntOrNone("not a number"); v != nil {
		t.Error("字符串应返回 nil")
	}
}

// TestResolveUploadRoot 测试上传根目录解析
func TestResolveUploadRoot(t *testing.T) {
	os.Unsetenv("BROWSER_UPLOAD_ROOT")
	os.Unsetenv("PLAYWRIGHT_UPLOAD_ROOT")
	if root := resolveUploadRoot(); root != "" {
		t.Error("未配置时应返回空字符串")
	}

	os.Setenv("BROWSER_UPLOAD_ROOT", "/tmp/uploads")
	defer os.Unsetenv("BROWSER_UPLOAD_ROOT")
	if root := resolveUploadRoot(); root != "/tmp/uploads" {
		t.Errorf("root = %q, want /tmp/uploads", root)
	}
}

// TestListDirFiles 测试目录文件列表
func TestListDirFiles(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("bb"), 0o644)
	os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0o755)

	files := listDirFiles(tmpDir)
	if len(files) != 2 {
		t.Errorf("文件数量 = %d, want 2（目录不应计入）", len(files))
	}
}

// TestListDirFiles_不存在的目录 测试不存在目录
func TestListDirFiles_不存在的目录(t *testing.T) {
	files := listDirFiles("/nonexistent/path")
	if len(files) != 0 {
		t.Error("不存在目录应返回空列表")
	}
}
