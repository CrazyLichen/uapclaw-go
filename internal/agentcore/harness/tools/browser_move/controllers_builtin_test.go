package browser_move

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newBuiltinController 构造已注册全部内置动作的控制器。
func newBuiltinController(t *testing.T) *ActionController {
	t.Helper()
	ctl := NewActionController()
	ctl.RegisterBuiltinActions()
	return ctl
}

// ──────────────────────────── 用例 ────────────────────────────

// TestBrowserDragAndDrop_缺少输入 选择器与坐标都缺失时拒绝执行。
func TestBrowserDragAndDrop_缺少输入(t *testing.T) {
	ctl := newBuiltinController(t)
	// 绑定运行时以通过 RunAction 的运行时门禁，进入参数校验分支
	ctl.BindRuntimeRunner(func(context.Context, string, string, string, *int) ActionResult {
		return ActionResult{"ok": true}
	})

	result := ctl.RunAction(context.Background(), "browser_drag_and_drop", "sess", "", map[string]any{})
	assert.NotEqual(t, true, result["ok"], "缺少拖拽输入必须失败: %v", result)
}

// TestBrowserDragAndDrop_未绑定运行时 有选择器但既无执行器也无运行时。
func TestBrowserDragAndDrop_未绑定运行时(t *testing.T) {
	ctl := newBuiltinController(t)

	result := ctl.RunAction(context.Background(), "browser_drag_and_drop", "sess", "req", map[string]any{
		"element_source": "#a",
		"element_target": "#b",
	})
	assert.Equal(t, false, result["ok"])
	assert.Contains(t, result["error"], "runtime_not_bound")
}

// TestBrowserDragAndDrop_代码执行成功 执行器返回合法 JSON 时回传结果字段。
func TestBrowserDragAndDrop_代码执行成功(t *testing.T) {
	ctl := newBuiltinController(t)
	ctl.BindCodeExecutor(func(_ context.Context, jsCode string) (any, error) {
		assert.Contains(t, jsCode, "async (page)")
		return `{"ok":true,"message":"dragged","source":"#a","target":"#b","steps":10,"delay_ms":5}`, nil
	})

	result := ctl.RunAction(context.Background(), "browser_drag_and_drop", "sess", "", map[string]any{
		"element_source": "#a",
		"element_target": "#b",
	})
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "dragged", result["message"])
}

// TestBrowserDragAndDrop_执行器报错 执行器错误被包装为失败结果。
func TestBrowserDragAndDrop_执行器报错(t *testing.T) {
	ctl := newBuiltinController(t)
	ctl.BindCodeExecutor(func(_ context.Context, _ string) (any, error) {
		return nil, errors.New("boom")
	})

	result := ctl.RunAction(context.Background(), "browser_drag_and_drop", "sess", "req", map[string]any{
		"coord_source_x": 1, "coord_source_y": 2, "coord_target_x": 3, "coord_target_y": 4,
	})
	assert.Equal(t, false, result["ok"])
	assert.Contains(t, result["error"], "browser_run_code failed")
}

// TestBrowserDragAndDrop_输出不可解析 非 JSON 输出给出解析失败。
func TestBrowserDragAndDrop_输出不可解析(t *testing.T) {
	ctl := newBuiltinController(t)
	ctl.BindCodeExecutor(func(_ context.Context, _ string) (any, error) {
		return "not-json", nil
	})

	result := ctl.RunAction(context.Background(), "browser_drag_and_drop", "sess", "", map[string]any{
		"element_source": "#a",
		"element_target": "#b",
	})
	assert.Equal(t, false, result["ok"])
	assert.Nil(t, result["source"], "不可解析输出不得回传结果字段")
}

// TestBrowserSetInputFiles_缺少路径 paths 为空时拒绝执行。
func TestBrowserSetInputFiles_缺少路径(t *testing.T) {
	ctl := newBuiltinController(t)

	result := ctl.RunAction(context.Background(), "browser_set_input_files", "sess", "", map[string]any{
		"selector": "#file",
		"paths":    []any{"  ", ""},
	})
	assert.Equal(t, false, result["ok"])
	assert.Contains(t, result["error"], "paths is required")
}

// TestBrowserSetInputFiles_未绑定运行时 有路径但无执行器/运行时。
func TestBrowserSetInputFiles_未绑定运行时(t *testing.T) {
	ctl := newBuiltinController(t)

	result := ctl.RunAction(context.Background(), "browser_set_input_files", "sess", "", map[string]any{
		"paths": []string{"/tmp/a.png"},
	})
	assert.Equal(t, false, result["ok"])
	assert.Contains(t, result["error"], "runtime_not_bound")
}

// TestBrowserSetInputFiles_代码执行成功 执行器返回合法 JSON 时回传选择器与路径。
func TestBrowserSetInputFiles_代码执行成功(t *testing.T) {
	ctl := newBuiltinController(t)
	ctl.BindCodeExecutor(func(_ context.Context, jsCode string) (any, error) {
		assert.Contains(t, jsCode, "setInputFiles")
		return `{"ok":true,"selector":"#file","paths":["/tmp/a.png"]}`, nil
	})

	result := ctl.RunAction(context.Background(), "browser_set_input_files", "sess", "", map[string]any{
		"selector": "#file",
		"paths":    []any{"/tmp/a.png"},
	})
	assert.Equal(t, true, result["ok"])
	assert.Equal(t, "#file", result["selector"])
}

// TestBrowserGetElementCoordinates_缺少输入 选择器与文本都缺失时拒绝。
func TestBrowserGetElementCoordinates_缺少输入(t *testing.T) {
	ctl := newBuiltinController(t)

	result := ctl.RunAction(context.Background(), "browser_get_element_coordinates", "sess", "", map[string]any{})
	assert.Equal(t, false, result["ok"])
	assert.NotEmpty(t, result["error"])
}

// TestBrowserGetElementCoordinates_未绑定运行时 有输入但无执行器/运行时。
func TestBrowserGetElementCoordinates_未绑定运行时(t *testing.T) {
	ctl := newBuiltinController(t)

	result := ctl.RunAction(context.Background(), "browser_get_element_coordinates", "sess", "", map[string]any{
		"element": "#btn",
	})
	assert.Equal(t, false, result["ok"])
	assert.Contains(t, result["error"], "runtime_not_bound")
}

// TestBuiltinActions_动作清单 内置动作全部登记且可查规格。
func TestBuiltinActions_动作清单(t *testing.T) {
	ctl := newBuiltinController(t)

	names := ctl.ListActions()
	require.NotEmpty(t, names)
	specs := ctl.DescribeActions()
	for _, want := range []string{
		"browser_drag_and_drop",
		"browser_set_input_files",
		"browser_get_element_coordinates",
	} {
		assert.Contains(t, names, want)
		assert.NotEmpty(t, specs[want].Summary)
	}
}

// TestBuildScriptHelpers_纯函数 脚本包装与拖拽脚本包含关键片段。
func TestBuildScriptHelpers_纯函数(t *testing.T) {
	assert.Contains(t, wrapPageScript("body();"), "async (page) => {")
	assert.Contains(t, buildDragScript(map[string]any{"element_source": "#a"}), "async (page) => {")
	assert.Contains(t, buildCoordinateScript(map[string]any{"x": 1}), "async (page) => {")
	assert.False(t, hasSourceSelector(map[string]any{}))
	assert.True(t, hasSourceSelector(map[string]any{"element_source": "#a"}))
}
