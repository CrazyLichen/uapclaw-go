package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// ──────────────────────────── 测试替身 ────────────────────────────

// testRailInput 模拟一个 rail 的双源构造输入（上游 20 字段模型的缩小版）。
//
// 字段来源与上游 TeamPolicyInput 同构：三个来自 context，五个来自 params。
type testRailInput struct {
	// Role 来自 context
	Role string
	// MemberName 来自 context
	MemberName string
	// Language 来自 context
	Language string
	// Lifecycle 来自 params
	Lifecycle string
	// DispatchMode 来自 params
	DispatchMode string
	// BasePrompt 来自 params（可选，nil 表示未提供）
	BasePrompt *string
	// ExcludeTools 来自 params
	ExcludeTools []string
	// MaxIterations 来自 params（用于验证类型不符）
	MaxIterations int
	// HittEnabled 来自 params（布尔）
	HittEnabled bool
	// Extra 来自 params（映射）
	Extra map[string]any
}

// Resolve 手写实现双源取值（对齐上游 ConstructionInput.resolve）。
//
// 每个字段的"取不到"语义必须与上游一致：用默认值，而非零值。
func (in *testRailInput) Resolve(params map[string]any, ctx schema.BuildContextInterface) error {
	in.Role = schema.ContextString(ctx, "Role", "leader")
	in.MemberName = schema.ContextString(ctx, "MemberName", "")
	in.Language = schema.ContextString(ctx, "Language", "cn")
	in.Lifecycle = schema.ParamString(params, "lifecycle", "temporary")
	in.DispatchMode = schema.ParamString(params, "dispatch_mode", "autonomous")
	in.BasePrompt = schema.ParamStringPtr(params, "base_prompt")
	in.ExcludeTools = schema.ParamStringSlice(params, "exclude_tools")
	in.HittEnabled = schema.ParamBool(params, "hitt_enabled", false)
	in.Extra = schema.ParamMap(params, "extra")
	var err error
	in.MaxIterations, err = schema.ParamInt(params, "max_iterations", 15)
	return err
}

// ──────────────────────────── 测试 ────────────────────────────

// TestConstructionInput_ParamSource_缺失用默认 params 无该键时取默认值。
//
// 对齐上游 inputs.py:121-123：`if params and name in params`——键不存在就不设值，
// 让字段默认值生效。
func TestConstructionInput_ParamSource_缺失用默认(t *testing.T) {
	in := &testRailInput{}
	err := in.Resolve(map[string]any{}, &schema.BuildContext{})

	require.NoError(t, err)
	assert.Equal(t, "temporary", in.Lifecycle, "缺失用默认")
	assert.Equal(t, "autonomous", in.DispatchMode)
	assert.Nil(t, in.BasePrompt, "可选字段缺失为 nil")
	assert.Empty(t, in.ExcludeTools)
	assert.Equal(t, 15, in.MaxIterations, "缺失时用传入的默认值")
}

// TestConstructionInput_ParamSource_nilParams安全 params 为 nil 时全部走默认。
func TestConstructionInput_ParamSource_nilParams安全(t *testing.T) {
	in := &testRailInput{}
	err := in.Resolve(nil, &schema.BuildContext{})

	require.NoError(t, err)
	assert.Equal(t, "temporary", in.Lifecycle)
	assert.Equal(t, 15, in.MaxIterations)
}

// TestConstructionInput_ParamSource_显式传值 params 有值时覆盖默认。
func TestConstructionInput_ParamSource_显式传值(t *testing.T) {
	in := &testRailInput{}
	err := in.Resolve(map[string]any{
		"lifecycle":      "persistent",
		"dispatch_mode":  "scheduled",
		"base_prompt":    "自定义",
		"exclude_tools":  []string{"spawn_member"},
		"max_iterations": 8,
		"hitt_enabled":   true,
		"extra":          map[string]any{"k": "v"},
	}, &schema.BuildContext{})

	require.NoError(t, err)
	assert.Equal(t, "persistent", in.Lifecycle)
	assert.Equal(t, "scheduled", in.DispatchMode)
	require.NotNil(t, in.BasePrompt)
	assert.Equal(t, "自定义", *in.BasePrompt)
	assert.Equal(t, []string{"spawn_member"}, in.ExcludeTools)
	assert.Equal(t, 8, in.MaxIterations)
	assert.True(t, in.HittEnabled)
	assert.Equal(t, "v", in.Extra["k"])
}

// TestConstructionInput_ParamSource_显式nil用默认 键存在但值为 nil 时取默认。
//
// 对齐上游 inputs.py:128：`None` 结果被丢弃，让字段默认值生效。
func TestConstructionInput_ParamSource_显式nil用默认(t *testing.T) {
	in := &testRailInput{Lifecycle: "untouched"}
	err := in.Resolve(map[string]any{"lifecycle": nil}, &schema.BuildContext{})

	require.NoError(t, err)
	assert.Equal(t, "temporary", in.Lifecycle, "nil 值应回落默认")
}

// TestConstructionInput_ContextSource_取值 context 字段按 attr 名读取。
//
// 对齐上游 inputs.py:126-127：`getattr(context, extra["context_attr"], None)`。
func TestConstructionInput_ContextSource_取值(t *testing.T) {
	in := &testRailInput{}
	ctx := &schema.BuildContext{Role: "teammate", MemberName: "alice", Language: "en"}
	err := in.Resolve(map[string]any{}, ctx)

	require.NoError(t, err)
	assert.Equal(t, "teammate", in.Role)
	assert.Equal(t, "alice", in.MemberName)
	assert.Equal(t, "en", in.Language)
}

// TestConstructionInput_ContextSource_空用默认 context 字段为空时用默认。
func TestConstructionInput_ContextSource_空用默认(t *testing.T) {
	in := &testRailInput{}
	err := in.Resolve(map[string]any{}, &schema.BuildContext{Role: "", Language: ""})

	require.NoError(t, err)
	assert.Equal(t, "leader", in.Role, "空串回落默认")
	assert.Equal(t, "cn", in.Language)
}

// TestConstructionInput_ContextSource_未知attr返回默认 未知属性名不 panic，回落默认。
func TestConstructionInput_ContextSource_未知attr返回默认(t *testing.T) {
	ctx := &schema.BuildContext{Language: "en"}

	assert.Equal(t, "fallback", schema.ContextString(ctx, "NoSuchAttr", "fallback"))
}

// TestConstructionInput_双源混合 两个源互不干扰。
func TestConstructionInput_双源混合(t *testing.T) {
	in := &testRailInput{}
	ctx := &schema.BuildContext{Role: "teammate", MemberName: "bob", Language: "cn"}
	err := in.Resolve(map[string]any{
		"lifecycle":     "persistent",
		"dispatch_mode": "scheduled",
	}, ctx)

	require.NoError(t, err)
	// context 源
	assert.Equal(t, "teammate", in.Role)
	assert.Equal(t, "bob", in.MemberName)
	// params 源
	assert.Equal(t, "persistent", in.Lifecycle)
	assert.Equal(t, "scheduled", in.DispatchMode)
}

// TestConstructionInput_类型不符报错 params 整型类型不匹配时返回 error 而非 panic。
func TestConstructionInput_类型不符报错(t *testing.T) {
	in := &testRailInput{}
	err := in.Resolve(map[string]any{"max_iterations": "not-a-number"}, &schema.BuildContext{})

	assert.Error(t, err, "类型不符必须报错（静默会掩盖配置错误）")
	assert.Contains(t, err.Error(), "max_iterations")
}

// TestConstructionInput_浮点整值接受 JSON 反序列化的 float64 整值。
func TestConstructionInput_浮点整值接受(t *testing.T) {
	got, err := schema.ParamInt(map[string]any{"n": float64(8)}, "n", 15)

	require.NoError(t, err)
	assert.Equal(t, 8, got)
}

// TestConstructionInput_浮点非整值报错 非整值的 float 视为配置错误。
func TestConstructionInput_浮点非整值报错(t *testing.T) {
	_, err := schema.ParamInt(map[string]any{"n": 8.5}, "n", 15)

	assert.Error(t, err)
}

// TestConstructionInput_nilContext安全 ctx 为 nil 时不 panic。
func TestConstructionInput_nilContext安全(t *testing.T) {
	in := &testRailInput{}
	err := in.Resolve(map[string]any{}, nil)

	require.NoError(t, err)
	assert.Equal(t, "leader", in.Role, "nil ctx 回落默认")
}

// TestConstructionInput_StringSlice接受any切片 JSON 反序列化的 []any 也能取。
func TestConstructionInput_StringSlice接受any切片(t *testing.T) {
	got := schema.ParamStringSlice(map[string]any{
		"tools": []any{"a", "b", 123},
	}, "tools")

	assert.Equal(t, []string{"a", "b"}, got, "非字符串元素被跳过")
}

// TestConstructionInput_ContextExtras nil ctx 返回空 map 而非 nil。
func TestConstructionInput_ContextExtras(t *testing.T) {
	assert.Empty(t, schema.ContextExtras(nil))

	ctx := &schema.BuildContext{}
	assert.Empty(t, schema.ContextExtras(ctx))

	ctx.GetExtras()["k"] = "v"
	assert.Equal(t, "v", schema.ContextExtras(ctx)["k"])
}
