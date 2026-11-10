package harness_protocol

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFreezeJsonValue_标量透传 验证 None/string/bool/int 原样返回。
// Python: models.py:30-34（None/str/bool/int 直返）
func TestFreezeJsonValue_标量透传(t *testing.T) {
	for _, in := range []any{nil, "s", true, false, 7, int64(9)} {
		got, err := FreezeJsonValue(in)
		require.NoError(t, err)
		assert.Equal(t, in, got)
	}
}

// TestFreezeJsonValue_拒绝非有限浮点 验证 NaN/Inf 被拒。
// Python: models.py:35-38 "JSON numbers must be finite"
func TestFreezeJsonValue_拒绝非有限浮点(t *testing.T) {
	for _, in := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := FreezeJsonValue(in)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "JSON numbers must be finite")
	}
}

// TestFreezeJsonValue_有限浮点通过 验证普通浮点放行。
func TestFreezeJsonValue_有限浮点通过(t *testing.T) {
	got, err := FreezeJsonValue(1.5)
	require.NoError(t, err)
	assert.Equal(t, 1.5, got)
}

// TestFreezeJsonValue_切片深拷贝为不共享 验证 list/tuple 递归且结果不与入参共享底层数组。
// Python: models.py:39-40（list/tuple → tuple 递归，不可变）
func TestFreezeJsonValue_切片深拷贝为不共享(t *testing.T) {
	src := []any{1, []any{"a"}}
	got, err := FreezeJsonValue(src)
	require.NoError(t, err)
	src[0] = 999
	// 冻结结果不受外部修改影响
	assert.Equal(t, []any{1, []any{"a"}}, got)
}

// TestFreezeJsonValue_映射深拷贝 验证 map 递归深拷贝，改源不影响结果。
// Python: models.py:41-47（Mapping → MappingProxyType 递归）
func TestFreezeJsonValue_映射深拷贝(t *testing.T) {
	src := map[string]any{"k": []any{1}}
	got, err := FreezeJsonValue(src)
	require.NoError(t, err)
	src["k"].([]any)[0] = 999
	obj := got.(map[string]any)
	assert.Equal(t, []any{1}, obj["k"])
}

// TestFreezeJsonValue_拒绝非字符串键 验证 map 键必须是 string。
// Python: models.py:44-45 "JSON object keys must be strings"
func TestFreezeJsonValue_拒绝非字符串键(t *testing.T) {
	_, err := FreezeJsonValue(map[int]any{1: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON object keys must be strings")
}

// TestFreezeJsonValue_拒绝非JSON类型 验证 struct/chan 等被拒。
// Python: models.py:48 "value of type X is not JSON-compatible"
func TestFreezeJsonValue_拒绝非JSON类型(t *testing.T) {
	_, err := FreezeJsonValue(struct{ A int }{1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not JSON-compatible")
}

// TestFreezeJsonObject_非映射报错 验证入参不是映射时失败。
// Python: models.py:51-57 "JSON object must be a mapping"
func TestFreezeJsonObject_非映射报错(t *testing.T) {
	_, err := FreezeJsonObject(nil)
	require.Error(t, err)
}

// TestFreezeJsonObject_空映射返回空非nil 验证 nil 入参返回空 map。
func TestFreezeJsonObject_空映射(t *testing.T) {
	got, err := FreezeJsonObject(map[string]any{})
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestJsonValueToBuiltin_还原为可序列化容器 验证冻结值可转回标准容器。
// Python: models.py:60-67 json_value_to_builtin
func TestJsonValueToBuiltin_还原为可序列化容器(t *testing.T) {
	frozen, err := FreezeJsonValue(map[string]any{"a": []any{1, "b"}})
	require.NoError(t, err)
	got := JsonValueToBuiltin(frozen)
	assert.Equal(t, map[string]any{"a": []any{1, "b"}}, got)
}

// ──────────── HarnessCard ────────────

// TestNewHarnessCard_默认值 验证协议版本与兼容集默认取 PROTOCOL_VERSION，能力集为空。
// Python: models.py:144-168（默认 protocol_version=PROTOCOL_VERSION, compatible 含自身）
func TestNewHarnessCard_默认值(t *testing.T) {
	c, err := NewHarnessCard("my-agent", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, ProtocolVersion, c.ProtocolVersion)
	assert.True(t, c.CompatibleProtocolVersions[ProtocolVersion])
	assert.Empty(t, c.Capabilities)
	assert.Empty(t, c.RequiredHostCapabilities)
	assert.Empty(t, c.OptionalHostCapabilities)
	assert.Empty(t, c.ForkModes)
}

// TestNewHarnessCard_拒绝空标识 验证 name/version 非空。
// Python: models.py:169-170 "harness card identity and versions must not be empty"
func TestNewHarnessCard_拒绝空标识(t *testing.T) {
	_, err := NewHarnessCard("", "1.0.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")

	_, err = NewHarnessCard("x", "")
	require.Error(t, err)
}

// TestNewHarnessCard_协议版本必须在兼容集 验证不匹配即拒。
// Python: models.py:171-172
func TestNewHarnessCard_协议版本必须在兼容集(t *testing.T) {
	_, err := NewHarnessCardWithOptions("x", "1", CardOptions{
		ProtocolVersion:            "1.0",
		CompatibleProtocolVersions: map[string]bool{"1.1": true},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compatible_protocol_versions")
}

// TestNewHarnessCard_必需与可选能力不得重叠 验证交集为空。
// Python: models.py:173-176 "host capabilities cannot be both required and optional"
func TestNewHarnessCard_必需与可选能力不得重叠(t *testing.T) {
	_, err := NewHarnessCardWithOptions("x", "1", CardOptions{
		RequiredHostCapabilities: map[HostCapability]bool{HostCapabilityHooks: true},
		OptionalHostCapabilities: map[HostCapability]bool{HostCapabilityHooks: true},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be both required and optional")
}

// TestHarnessCard_Supports 验证能力声明查询。
// Python: models.py:178-180 supports()
func TestHarnessCard_Supports(t *testing.T) {
	c, err := NewHarnessCardWithOptions("x", "1", CardOptions{
		Capabilities: map[HarnessCapability]bool{HarnessCapabilitySteer: true},
	})
	require.NoError(t, err)
	assert.True(t, c.Supports(HarnessCapabilitySteer))
	assert.False(t, c.Supports(HarnessCapabilityPauseResume))
}

// TestHarnessCard_ValidateHost 验证协议版本与必需能力两处 fail fast。
// Python: models.py:182-195 validate_host
func TestHarnessCard_ValidateHost(t *testing.T) {
	c, err := NewHarnessCardWithOptions("x", "1", CardOptions{
		RequiredHostCapabilities: map[HostCapability]bool{HostCapabilityToolApproval: true},
	})
	require.NoError(t, err)

	// 协议版本不支持
	err = c.ValidateHost("9.9", map[HostCapability]bool{HostCapabilityToolApproval: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not supported by")

	// 缺必需能力
	err = c.ValidateHost(ProtocolVersion, map[HostCapability]bool{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "host is missing required capabilities")

	// 满足
	require.NoError(t, c.ValidateHost(ProtocolVersion, map[HostCapability]bool{HostCapabilityToolApproval: true}))
}

// ──────────── HarnessInput / SendReceipt ────────────

// TestNewHarnessInput_冻结content与metadata 验证两字段都被冻结。
// Python: models.py:198-207
func TestNewHarnessInput_冻结content与metadata(t *testing.T) {
	in, err := NewHarnessInput(map[string]any{"text": "hi"}, map[string]any{"k": "v"})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"text": "hi"}, in.Content)
	assert.Equal(t, map[string]any{"k": "v"}, in.Metadata)
}

// TestNewHarnessInput_非JSON内容报错 验证 NaN 被挡。
func TestNewHarnessInput_非JSON内容报错(t *testing.T) {
	_, err := NewHarnessInput(math.NaN(), nil)
	require.Error(t, err)
}

// TestNewSendReceipt_字段非空 验证 message_id / turn_id 必填。
// Python: models.py:210-222
func TestNewSendReceipt_字段非空(t *testing.T) {
	_, err := NewSendReceipt("", "t1", DeliveryModeAuto)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "message_id must not be empty")

	_, err = NewSendReceipt("m1", "", DeliveryModeAuto)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "turn_id must not be empty")

	r, err := NewSendReceipt("m1", "t1", DeliveryModeSteer)
	require.NoError(t, err)
	assert.Equal(t, DeliveryModeSteer, r.AcceptedMode)
}

// ──────────── 模型选择族 ────────────

// TestNewModelSelection_至少一个且非空 验证 model/effort 规则。
// Python: models.py:225-244
func TestNewModelSelection_至少一个且非空(t *testing.T) {
	_, err := NewModelSelection(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must set a model, an effort, or both")

	blank := " "
	_, err = NewModelSelection(&blank, nil)
	require.Error(t, err)

	m := "sonnet"
	s, err := NewModelSelection(&m, nil)
	require.NoError(t, err)
	assert.Equal(t, "sonnet", *s.Model)
	assert.Nil(t, s.Effort)
}

// TestNewModelEndpoint_至少一个且非空 验证 api_base/provider 规则。
// Python: models.py:254-273
func TestNewModelEndpoint_至少一个且非空(t *testing.T) {
	_, err := NewModelEndpoint(nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api_base, a provider, or both")

	base := "https://x/v1"
	e, err := NewModelEndpoint(&base, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "https://x/v1", *e.APIBase)
}

// TestNewModelChoice_isBuiltin 验证 endpoint 为空即内置。
// Python: models.py:276-299（is_builtin property）
func TestNewModelChoice_isBuiltin(t *testing.T) {
	m := "sonnet"
	c, err := NewModelChoice(&m, nil, nil)
	require.NoError(t, err)
	assert.True(t, c.IsBuiltin())

	base := "https://x"
	ep, err := NewModelEndpoint(&base, nil, nil)
	require.NoError(t, err)
	c2, err := NewModelChoice(&m, nil, &ep)
	require.NoError(t, err)
	assert.False(t, c2.IsBuiltin())

	blank := " "
	_, err = NewModelChoice(&blank, nil, nil)
	require.Error(t, err)
}

// TestNewModelOption_efforts与defaultEffort 验证 tuple 化与成员校验。
// Python: models.py:302-327
func TestNewModelOption_efforts与defaultEffort(t *testing.T) {
	_, err := NewModelOption("", "", "", nil, nil, false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model_id must not be empty")

	high := "high"
	o, err := NewModelOption("sonnet", "Sonnet", "", []string{"low", "high"}, &high, true, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"low", "high"}, o.Efforts)
	assert.Equal(t, "high", *o.DefaultEffort)

	// default_effort 不在 efforts 内
	max := "max"
	_, err = NewModelOption("sonnet", "", "", []string{"low"}, &max, false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default_effort")

	// efforts 含空串
	_, err = NewModelOption("sonnet", "", "", []string{"low", ""}, nil, false, nil)
	require.Error(t, err)
}

// TestNewModelOption_冻结extensions 验证扩展数据被冻结且不与入参共享。
// Python: models.py:317,327（freeze_json_object）
func TestNewModelOption_冻结extensions(t *testing.T) {
	ext := map[string]any{"a": []any{1}}
	o, err := NewModelOption("m", "", "", nil, nil, false, ext)
	require.NoError(t, err)
	ext["a"].([]any)[0] = 999
	assert.Equal(t, []any{1}, o.Extensions["a"])
}

// ──────────── HarnessContext ────────────

// TestNewHarnessContext_必填与非空 验证 4 个必填字段。
// Python: models.py:358-369
func TestNewHarnessContext_必填与非空(t *testing.T) {
	_, err := NewHarnessContext("", "a1", "s1", "sys", ContextOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent_name must not be empty")

	c, err := NewHarnessContext("agent", "a1", "s1", "sys", ContextOptions{})
	require.NoError(t, err)
	assert.Equal(t, ProtocolVersion, c.ProtocolVersion)
	assert.Equal(t, ResumePolicyNew, c.ResumePolicy)
}

// TestNewHarnessContext_env必须是字符串映射 验证类型校验。
// Python: models.py:368-369 "harness context env must map strings to strings"
func TestNewHarnessContext_env必须是字符串映射(t *testing.T) {
	_, err := NewHarnessContextWithEnv("agent", "a1", "s1", "sys", map[string]any{"K": 1})
	require.Error(t, err)
}

// TestNewHarnessContext_冻结metadata 验证 metadata 深拷贝。
// Python: models.py:373
func TestNewHarnessContext_冻结metadata(t *testing.T) {
	md := map[string]any{"k": []any{1}}
	c, err := NewHarnessContext("agent", "a1", "s1", "sys", ContextOptions{Metadata: md})
	require.NoError(t, err)
	md["k"].([]any)[0] = 999
	assert.Equal(t, []any{1}, c.Metadata["k"])
}
