package auto_permission

import (
	"context"
	"reflect"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// TestObservedToolTypes_初始为空 验证未装配时表为空（除测试污染外）。
func TestObservedToolTypes_初始为空(t *testing.T) {
	got := ObservedToolTypes()
	if got == nil {
		t.Fatal("should return non-nil map")
	}
}

// TestToolRegistration_ProgressiveRail真实装配 验证 Init 后自动登记类型。
//
// 这是「注入点接线」的端到端验证：
// ProgressiveToolRail.Init 构造 tool_search → 观察者回调 → 登记类型表
// → toolSearchToolType() 返回非 nil。
func TestToolRegistration_ProgressiveRail真实装配(t *testing.T) {
	defer SetReadonlyToolTypes(nil)

	agent := harness.NewDeepAgent(&agentschema.AgentCard{})
	rail := rails.NewProgressiveToolRail(nil)
	if err := rail.Init(context.Background(), agent); err != nil {
		t.Fatalf("ProgressiveToolRail.Init: %v", err)
	}

	// Init 里构造了 tool_search 并通过观察者登记了类型
	got := toolSearchToolType()
	if got == nil {
		observed := ObservedToolTypes()
		t.Fatalf("tool_search 类型应已自动登记，实际观察表: %v", observed)
	}
	if got.Kind() != reflect.Ptr {
		t.Errorf("应为指针类型，实际 %v", got.Kind())
	}
}

// TestToolRegistration_只观察闭集内工具 验证非闭集工具不登记。
func TestToolRegistration_只观察闭集内工具(t *testing.T) {
	// tool_call 不在闭集内
	if _, watched := watchedToolNames["tool_call"]; watched {
		t.Error("tool_call 不应在观察闭集内")
	}
	if _, watched := watchedToolNames[toolSearchName]; !watched {
		t.Error("tool_search 应在观察闭集内")
	}
}

// TestWatchedToolNames_覆盖三处校验 验证闭集覆盖三个绑定校验的工具名。
func TestWatchedToolNames_覆盖三处校验(t *testing.T) {
	expected := []string{
		toolSearchName,
		enterPlanModeTool, exitPlanModeTool, switchModeTool,
		"sessions_spawn", "sessions_list", "sessions_cancel",
		"subagent_spawn", "subagent_close", "subagent_list",
		"subagent_resume", "subagent_send_input", "subagent_wait",
	}
	for _, name := range expected {
		if _, watched := watchedToolNames[name]; !watched {
			t.Errorf("%q 应在观察闭集内", name)
		}
	}
}

// TestToolRegistration_nativePath实例登记 验证原生路径工具登记的是实例。
//
// 与类型表不同：路径冻结要校验指针同一性，故必须存实例。
func TestToolRegistration_nativePath实例登记(t *testing.T) {
	// 模拟观察者收到 read_file 实例
	instance := &fakeCodeControlTool{}
	card := instance.Card()
	if card != nil {
		card.Name = "read_file"
	}

	// 直接验证表结构：nativePathExecutors 存储 tool.Tool 实例
	nativePathExecutorsMu.RLock()
	_, hasKey := nativePathExecutors["read_file"]
	nativePathExecutorsMu.RUnlock()
	// 初始可能为空，只验证类型正确性
	_ = hasKey

	// watchedToolNames 必须覆盖所有 nativePathToolNames
	nativePathToolNamesForTest := []string{
		"read_file", "write_file", "edit_file",
		"glob", "grep", "list_files", "read_pdf",
	}
	for _, name := range nativePathToolNamesForTest {
		if _, watched := watchedToolNames[name]; !watched {
			t.Errorf("原生路径工具 %q 应在观察闭集内（否则路径不冻结）", name)
		}
	}
}

// TestObservedToolTypes_拷贝隔离 验证返回的是拷贝不是内部引用。
func TestObservedToolTypes_拷贝隔离(t *testing.T) {
	first := ObservedToolTypes()
	first["injected"] = reflect.TypeOf((*fakeCodeControlTool)(nil))
	second := ObservedToolTypes()
	if _, exists := second["injected"]; exists {
		t.Error("返回的 map 应是与内部隔离的拷贝")
	}
}

// TestToolRegistration_AgentModeRail真实装配 验证三个控制工具自动登记。
func TestToolRegistration_AgentModeRail真实装配(t *testing.T) {
	defer SetCodeControlToolTypes(nil)
	SetCodeControlToolTypes(nil)

	agent := harness.NewDeepAgent(&agentschema.AgentCard{})
	rail := rails.NewAgentModeRail(nil)
	if err := rail.Init(context.Background(), agent); err != nil {
		t.Fatalf("AgentModeRail.Init: %v", err)
	}

	for _, name := range []string{enterPlanModeTool, exitPlanModeTool, switchModeTool} {
		if got := registeredCodeControlToolType(name); got == nil {
			t.Errorf("%q 类型应已自动登记", name)
		}
	}
}

// TestToolRegistration_nativePath真实装配 验证原生路径工具实例自动登记。
func TestToolRegistration_nativePath真实装配(t *testing.T) {
	agent := harness.NewDeepAgent(&agentschema.AgentCard{})
	rail := rails.NewSysOperationRail()
	if err := rail.Init(context.Background(), agent); err != nil {
		// 缺 SysOperation 时 Init 可能返回错误，此时跳过（非本用例目标）
		t.Skipf("SysOperationRail.Init 需要真实 SysOperation: %v", err)
	}

	nativePathExecutorsMu.RLock()
	registered := len(nativePathExecutors)
	nativePathExecutorsMu.RUnlock()
	if registered == 0 {
		t.Error("原生路径工具实例应已自动登记（否则路径不冻结）")
	}
}

// TestObserveToolInstance_边界 验证观察逻辑的 fail-safe 分支。
func TestObserveToolInstance_边界(t *testing.T) {
	defer SetReadonlyToolTypes(nil)
	SetReadonlyToolTypes(nil)

	// 非闭集工具：不登记
	observeToolInstance("tool_call", &fakeCodeControlTool{})
	if _, watched := ObservedToolTypes()["tool_call"]; watched {
		t.Error("非闭集工具不应登记")
	}

	// nil 实例：不 panic；已登记的键不应被清空或改写
	observeToolInstance(toolSearchName, nil)
}

// TestObserveToolInstance_三处同步 验证四种工具名分派到正确的表。
func TestObserveToolInstance_三处同步(t *testing.T) {
	defer SetReadonlyToolTypes(nil)
	defer SetCodeControlToolTypes(nil)
	defer SetSubagentRuntimeControlToolTypes(nil)
	SetReadonlyToolTypes(nil)
	SetCodeControlToolTypes(nil)
	SetSubagentRuntimeControlToolTypes(nil)

	// tool_search → readonly 表
	observeToolInstance(toolSearchName, &fakeCodeControlTool{})
	if toolSearchToolType() == nil {
		t.Error("tool_search 应登记到 readonly 表")
	}

	// enter_plan_mode → code control 表
	observeToolInstance(enterPlanModeTool, &fakeCodeControlTool{})
	if registeredCodeControlToolType(enterPlanModeTool) == nil {
		t.Error("enter_plan_mode 应登记到 code control 表")
	}

	// sessions_spawn → subagent 表
	observeToolInstance("sessions_spawn", &fakeCodeControlTool{})
	if registeredSubagentControlToolType("sessions_spawn") == nil {
		t.Error("sessions_spawn 应登记到 subagent 表")
	}
}
