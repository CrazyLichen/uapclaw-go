package tool_call

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// TestNewBaseMethod 测试 BaseMethod 构造
func TestNewBaseMethod(t *testing.T) {
	config := map[string]any{
		"verbose":       1,
		"gen_model_id":  "test-model",
		"eval_model_id": "test-eval",
	}
	m := NewBaseMethod(config, nil)
	if !m.verbose {
		t.Error("expected verbose=true for verbose=1")
	}
	if m.config["gen_model_id"] != "test-model" {
		t.Error("gen_model_id not set correctly")
	}
}

// TestNewBaseMethod_VerboseBool 测试 bool 类型 verbose
func TestNewBaseMethod_VerboseBool(t *testing.T) {
	config := map[string]any{"verbose": true}
	m := NewBaseMethod(config, nil)
	if !m.verbose {
		t.Error("expected verbose=true")
	}
}

// TestNewBaseMethod_VerboseFalse 测试默认 verbose 为 false
func TestNewBaseMethod_VerboseFalse(t *testing.T) {
	config := map[string]any{}
	m := NewBaseMethod(config, nil)
	if m.verbose {
		t.Error("expected verbose=false by default")
	}
}

// TestGetConfigString 测试配置获取
func TestGetConfigString(t *testing.T) {
	config := map[string]any{"key": "value"}
	if got := getConfigString(config, "key"); got != "value" {
		t.Errorf("got %q, want %q", got, "value")
	}
	if got := getConfigString(config, "missing"); got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

// TestGetConfigInt 测试整数配置获取
func TestGetConfigInt(t *testing.T) {
	config := map[string]any{"num": 5}
	if got := getConfigInt(config, "num"); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
	config2 := map[string]any{"num": float64(3)}
	if got := getConfigInt(config2, "num"); got != 3 {
		t.Errorf("got %d, want 3", got)
	}
}

// TestGetConfigFloat 测试浮点配置获取
func TestGetConfigFloat(t *testing.T) {
	config := map[string]any{"weight": 0.4}
	if got := getConfigFloat(config, "weight"); got != 0.4 {
		t.Errorf("got %f, want 0.4", got)
	}
	config2 := map[string]any{"weight": 1}
	if got := getConfigFloat(config2, "weight"); got != 1.0 {
		t.Errorf("got %f, want 1.0", got)
	}
}

// TestNewToolOptimizerBase 测试 ToolOptimizerBase 构造及默认值
func TestNewToolOptimizerBase(t *testing.T) {
	base := NewToolOptimizerBase(nil)
	if base.Domain() != "tool" {
		t.Errorf("期望 Domain=tool, 实际=%s", base.Domain())
	}
	if base.DefaultTargets()[0] != "tool_description" {
		t.Errorf("期望 DefaultTargets[0]=tool_description, 实际=%s", base.DefaultTargets()[0])
	}
	if !base.RequiresForwardData() {
		t.Error("期望 RequiresForwardData=true，对齐 Python BaseOptimizer 默认值")
	}
	if base.maxTurns != 5 {
		t.Errorf("期望 maxTurns=5, 实际=%d", base.maxTurns)
	}
}

// TestNewToolOptimizerBase_WithOptions 测试 ToolOptimizerBase 带选项构造
func TestNewToolOptimizerBase_WithOptions(t *testing.T) {
	base := NewToolOptimizerBase(nil,
		WithMaxTurns(10),
		WithLLMAPIKey("test-key"),
		WithPathSaveDir("/tmp/results"),
		WithToolName("my_tool"),
	)
	if base.maxTurns != 10 {
		t.Errorf("期望 maxTurns=10, 实际=%d", base.maxTurns)
	}
	if base.llmAPIKey != "test-key" {
		t.Errorf("期望 llmAPIKey=test-key, 实际=%s", base.llmAPIKey)
	}
	if base.pathSaveDir != "/tmp/results" {
		t.Errorf("期望 pathSaveDir=/tmp/results, 实际=%s", base.pathSaveDir)
	}
	if base.toolName != "my_tool" {
		t.Errorf("期望 toolName=my_tool, 实际=%s", base.toolName)
	}
}

// TestToolOptimizerBase_Backward 测试 Backward 模板方法流程
func TestToolOptimizerBase_Backward(t *testing.T) {
	base := NewToolOptimizerBase(nil)
	ops := map[string]operator.Operator{"desc": &fakeToolOpOperator{id: "desc"}}
	base.Bind(ops, []string{"tool_description"}, map[string]any{})

	err := base.Backward(context.Background(), nil)
	if err != nil {
		t.Errorf("期望 Backward 返回 nil, 实际=%v", err)
	}
	// 验证 SelectedSignals 已设置（空信号列表）
	selected := base.SelectedSignals()
	if len(selected) != 0 {
		t.Errorf("期望 SelectedSignals 长度 0, 实际=%d", len(selected))
	}
}

// TestToolOptimizerBase_Backward_未绑定时panic 测试 Backward 未绑定参数时 panic
func TestToolOptimizerBase_Backward_未绑定时panic(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("期望 Backward 未绑定时 panic, 实际未 panic")
		}
	}()
	base := NewToolOptimizerBase(nil)
	base.Backward(context.Background(), nil)
}

// TestToolOptimizerBase_Backward_信号选择 测试 Backward 正确选择信号
func TestToolOptimizerBase_Backward_信号选择(t *testing.T) {
	base := NewToolOptimizerBase(nil)
	ops := map[string]operator.Operator{"desc": &fakeToolOpOperator{id: "desc"}}
	base.Bind(ops, []string{"tool_description"}, map[string]any{})

	sig1 := signal.MakeEvolutionSignal("execution_failure", "Test", "excerpt1")
	sig2 := signal.MakeEvolutionSignal("low_score", "Test", "excerpt2")
	err := base.Backward(context.Background(), []*signal.EvolutionSignal{sig1, sig2})
	if err != nil {
		t.Errorf("期望 Backward 返回 nil, 实际=%v", err)
	}
	selected := base.SelectedSignals()
	if len(selected) != 2 {
		t.Errorf("期望 SelectedSignals 长度 2, 实际=%d", len(selected))
	}
}

// TestToolOptimizerBase_Step 测试 Step 模板方法流程
func TestToolOptimizerBase_Step(t *testing.T) {
	base := NewToolOptimizerBase(nil)
	ops := map[string]operator.Operator{"desc": &fakeToolOpOperator{id: "desc"}}
	base.Bind(ops, []string{"tool_description"}, map[string]any{})

	updates := base.Step()
	if len(updates) != 0 {
		t.Errorf("期望 Step 返回空 map, 实际=%d 项", len(updates))
	}
}

// TestToolOptimizerBase_Step_未绑定时panic 测试 Step 未绑定参数时 panic
func TestToolOptimizerBase_Step_未绑定时panic(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("期望 Step 未绑定时 panic, 实际未 panic")
		}
	}()
	base := NewToolOptimizerBase(nil)
	base.Step()
}

// TestToolOptimizerBase_Step_清空轨迹 测试 Step 后轨迹被清空
func TestToolOptimizerBase_Step_清空轨迹(t *testing.T) {
	base := NewToolOptimizerBase(nil)
	ops := map[string]operator.Operator{"desc": &fakeToolOpOperator{id: "desc"}}
	base.Bind(ops, []string{"tool_description"}, map[string]any{})

	base.AddTrajectory(&trajectory.Trajectory{ExecutionID: "test"})
	if len(base.GetTrajectories()) != 1 {
		t.Errorf("添加轨迹后期望 1 条, 实际=%d", len(base.GetTrajectories()))
	}

	base.Step()
	if len(base.GetTrajectories()) != 0 {
		t.Errorf("Step 后期望轨迹清空, 实际=%d 条", len(base.GetTrajectories()))
	}
}

// TestToolOptimizerBase_AddTrajectory 测试添加和获取轨迹
func TestToolOptimizerBase_AddTrajectory(t *testing.T) {
	base := NewToolOptimizerBase(nil)
	traj := &trajectory.Trajectory{}
	base.AddTrajectory(traj)
	trajs := base.GetTrajectories()
	if len(trajs) != 1 {
		t.Errorf("期望 1 条轨迹, 实际=%d", len(trajs))
	}
	base.ClearTrajectories()
	trajs = base.GetTrajectories()
	if len(trajs) != 0 {
		t.Errorf("期望清空后 0 条轨迹, 实际=%d", len(trajs))
	}
}

// TestToolOptimizerBase_SelectSignals 测试信号选择
func TestToolOptimizerBase_SelectSignals(t *testing.T) {
	base := NewToolOptimizerBase(nil)
	signals := []*signal.EvolutionSignal{
		{SignalType: "tool_call"},
		{SignalType: "llm_call"},
	}
	selected := base.SelectSignals(signals)
	if len(selected) != 2 {
		t.Errorf("期望选择 2 条信号, 实际=%d", len(selected))
	}
}

// TestToolOptimizerBase_Parameters 测试获取参数
func TestToolOptimizerBase_Parameters(t *testing.T) {
	base := NewToolOptimizerBase(nil)
	params := base.Parameters()
	if params == nil {
		t.Error("期望 Parameters 返回非 nil")
	}
}

// TestExtractLastDescription 测试从 resultDescs 提取最终描述
func TestExtractLastDescription(t *testing.T) {
	// 空列表
	if desc := extractLastDescription(nil); desc != "" {
		t.Errorf("期望空列表返回空串, 实际=%q", desc)
	}
	// 有描述的情况
	result := [][][]map[string]any{
		{{{"description": "最终描述", "other": "value"}}},
	}
	if desc := extractLastDescription(result); desc != "最终描述" {
		t.Errorf("期望返回'最终描述', 实际=%q", desc)
	}
	// 无 description 字段
	result2 := [][][]map[string]any{{{{"other": "value"}}}}
	if desc := extractLastDescription(result2); desc != "" {
		t.Errorf("期望无 description 返回空串, 实际=%q", desc)
	}
}

// fakeToolOpOperator 用于测试 Bind 的模拟 Operator
type fakeToolOpOperator struct {
	id string
}

func (o *fakeToolOpOperator) OperatorID() string { return o.id }
func (o *fakeToolOpOperator) GetTunables() map[string]operator.TunableSpec {
	return map[string]operator.TunableSpec{
		"tool_description": {Name: "tool_description"},
	}
}
func (o *fakeToolOpOperator) GetState() map[string]any     { return nil }
func (o *fakeToolOpOperator) SetParameter(_ string, _ any) {}
func (o *fakeToolOpOperator) ApplyUpdate(target string, update schema.UpdateValue) schema.ApplyResult {
	return schema.ApplyResult{OperatorID: o.id, Target: target, Applied: true}
}
func (o *fakeToolOpOperator) LoadState(_ map[string]any) {}

// ──────────────────────────── ToolOptimizerBase 测试 ────────────────────────────

// TestWithConfigEg 设置 Example Stage 配置
func TestWithConfigEg(t *testing.T) {
	cfg := map[string]any{"num_init_loop": 3}
	b := NewToolOptimizerBase(nil, WithConfigEg(cfg))
	if b.configEg["num_init_loop"] != 3 {
		t.Errorf("WithConfigEg 未生效，configEg = %v", b.configEg)
	}
}

// TestWithConfigDesc 设置 Description Stage 配置
func TestWithConfigDesc(t *testing.T) {
	cfg := map[string]any{"num_examples_for_desc": 5}
	b := NewToolOptimizerBase(nil, WithConfigDesc(cfg))
	if b.configDesc["num_examples_for_desc"] != 5 {
		t.Errorf("WithConfigDesc 未生效，configDesc = %v", b.configDesc)
	}
}

// TestWithMaxTurns 设置最大迭代轮次
func TestWithMaxTurns(t *testing.T) {
	b := NewToolOptimizerBase(nil, WithMaxTurns(3))
	if b.maxTurns != 3 {
		t.Errorf("WithMaxTurns 未生效，maxTurns = %d, 期望 3", b.maxTurns)
	}
}

// TestWithLLMAPIKey 设置 LLM API 密钥
func TestWithLLMAPIKey(t *testing.T) {
	b := NewToolOptimizerBase(nil, WithLLMAPIKey("test-key"))
	if b.llmAPIKey != "test-key" {
		t.Errorf("WithLLMAPIKey 未生效，llmAPIKey = %q", b.llmAPIKey)
	}
}

// TestWithPathSaveDir 设置结果保存目录
func TestWithPathSaveDir(t *testing.T) {
	b := NewToolOptimizerBase(nil, WithPathSaveDir("/tmp/test_results"))
	if b.pathSaveDir != "/tmp/test_results" {
		t.Errorf("WithPathSaveDir 未生效，pathSaveDir = %q", b.pathSaveDir)
	}
}

// TestWithToolName 设置工具名称
func TestWithToolName(t *testing.T) {
	b := NewToolOptimizerBase(nil, WithToolName("grep"))
	if b.toolName != "grep" {
		t.Errorf("WithToolName 未生效，toolName = %q", b.toolName)
	}
}

// TestToolOptimizerBase_Bind 绑定目标
func TestToolOptimizerBase_Bind(t *testing.T) {
	b := NewToolOptimizerBase(nil)
	ops := map[string]operator.Operator{"desc": &fakeToolOpOperator{id: "desc"}}
	n := b.Bind(ops, []string{"tool_description"}, map[string]any{})
	if n != 1 {
		t.Errorf("Bind 返回 %d, 期望 1", n)
	}
}

// TestToolOptimizerBase_Bind_空目标使用默认 绑定空目标使用默认
func TestToolOptimizerBase_Bind_空目标使用默认(t *testing.T) {
	b := NewToolOptimizerBase(nil)
	ops := map[string]operator.Operator{"desc": &fakeToolOpOperator{id: "desc"}}
	n := b.Bind(ops, nil, map[string]any{})
	if n != 1 {
		t.Errorf("Bind(空目标) 返回 %d, 期望 1（使用默认目标）", n)
	}
}
