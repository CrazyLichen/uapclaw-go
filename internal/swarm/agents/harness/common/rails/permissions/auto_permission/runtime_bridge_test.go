package auto_permission

import (
	"context"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hsecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 辅助 ────────────────────────────

// newBridgeRail 构造带 fake base rail 的 AutoPermissionRail。
func newBridgeRail(t *testing.T) *AutoPermissionRail {
	t.Helper()
	rail, err := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:         &fakePermissionRail{},
		PermissionConfig: map[string]any{},
	})
	if err != nil {
		t.Fatalf("construct rail: %v", err)
	}
	return rail
}

// bridgeInvocation 构造测试用调用快照。
func bridgeInvocation(toolName string, cbc *agentinterfaces.AgentCallbackContext) ToolInvocation {
	return ToolInvocation{Ctx: cbc, ToolName: toolName, ToolArgs: map[string]any{}}
}

// ──────────────────────────── V-8 桩 ────────────────────────────

// TestEmitAudit_桩返回nil 验证 no-op 桩。
func TestEmitAudit_桩返回nil(t *testing.T) {
	rail := newBridgeRail(t)
	if got := rail.emitAudit(permissions.ToolDecisionFacts{}, auditRecord{}); got != nil {
		t.Errorf("got %v, want nil（⤵️ 9.90e 回填）", got)
	}
}

// TestEmitPermissionResultObservation_桩不panic 验证 no-op 桩。
func TestEmitPermissionResultObservation_桩不panic(t *testing.T) {
	rail := newBridgeRail(t)
	rail.emitPermissionResultObservation(permissions.ToolDecisionFacts{}, nil, "s1", "c1", "src", nil)
}

// ──────────────────────────── runtimeResult ────────────────────────────

// TestRuntimeResult_无上下文直返 验证无 cbc 时不产生副作用。
func TestRuntimeResult_无上下文直返(t *testing.T) {
	rail := newBridgeRail(t)
	result := map[string]any{"approved": true}
	got := rail.runtimeResult(bridgeInvocation("bash", nil), result)
	if got == nil {
		t.Fatal("should return result unchanged")
	}
}

// TestRuntimeResult_allow直返 验证放行不改写。
func TestRuntimeResult_allow直返(t *testing.T) {
	rail := newBridgeRail(t)
	inputs := &agentinterfaces.ToolCallInputs{ToolName: "bash"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	result := hsecurity.PermissionConfirmResponse{Approved: true}

	got := rail.runtimeResult(bridgeInvocation("bash", cbc), result)
	if got == nil {
		t.Fatal("allow result should pass through")
	}
	// 不应设置 _skip_tool
	if _, exists := cbc.Extra()[skipToolExtraKey]; exists {
		t.Error("allow should not set _skip_tool")
	}
}

// TestRuntimeResult_拒绝跳过工具 验证 denied 设 _skip_tool。
func TestRuntimeResult_拒绝跳过工具(t *testing.T) {
	rail := newBridgeRail(t)
	inputs := &agentinterfaces.ToolCallInputs{ToolName: "bash"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	result := map[string]any{"feedback": "[permission_denied] denied"}

	rail.runtimeResult(bridgeInvocation("bash", cbc), result)

	if cbc.Extra()[skipToolExtraKey] != true {
		t.Error("denied should set _skip_tool")
	}
	updated, _ := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if updated.ToolResult == nil {
		t.Error("tool_result should be written")
	}
	if updated.ToolMsg == nil {
		t.Error("tool_msg should be written")
	}
}

// TestRuntimeResult_用户拒绝跳过工具 验证 user_rejection 同样跳过。
func TestRuntimeResult_用户拒绝跳过工具(t *testing.T) {
	rail := newBridgeRail(t)
	inputs := &agentinterfaces.ToolCallInputs{ToolName: "bash"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	result := map[string]any{"feedback": "[permission_rejected] rejected"}

	rail.runtimeResult(bridgeInvocation("bash", cbc), result)
	if cbc.Extra()[skipToolExtraKey] != true {
		t.Error("user_rejection should set _skip_tool")
	}
}

// ──────────────────────────── prepareManualPendingInvocation ────────────────────────────

// TestPrepareManualPendingInvocation_无key直返 验证 key 缺失时不改。
func TestPrepareManualPendingInvocation_无key直返(t *testing.T) {
	result := map[string]any{"value": map[string]any{}}
	got := prepareManualPendingInvocation(bridgeInvocation("bash", nil), result)
	mapping, _ := got.(map[string]any)
	if _, exists := mapping["value"].(map[string]any)["tool_invocation_key"]; exists {
		t.Error("no key should not inject")
	}
}

// TestPrepareManualPendingInvocation_非map直返 验证非 map 结果。
func TestPrepareManualPendingInvocation_非map直返(t *testing.T) {
	got := prepareManualPendingInvocation(bridgeInvocation("bash", nil), "string result")
	if got != "string result" {
		t.Errorf("got %v", got)
	}
}

// ──────────────────────────── normalizeRuntimePermissionResult ────────────────────────────

// TestNormalizeRuntimePermissionResult_直返 验证 V-9 回填后的转换行为。
//
// V-9 已回填：user_rejection + manual_approval 会转成标准 denied payload，
// 详细断言见 reviewer_metadata_test.go 的
// TestNormalizeRuntimePermissionResult_用户拒绝转denied。
func TestNormalizeRuntimePermissionResult_直返(t *testing.T) {
	result := map[string]any{"feedback": "[permission_rejected] rejected"}
	got := normalizeRuntimePermissionResult(
		bridgeInvocation("bash", nil), result, permissions.ToolDecisionFacts{},
		manualApprovalDecisionSource)
	if got == nil {
		t.Fatal("should not return nil")
	}
}

// ──────────────────────────── buildRuntimeInterruptRequest ────────────────────────────

// TestBuildRuntimeInterruptRequest_基本字段 验证中断请求构造。
func TestBuildRuntimeInterruptRequest_基本字段(t *testing.T) {
	rail := newBridgeRail(t)
	result := map[string]any{"message": "approve please"}
	request := rail.buildRuntimeInterruptRequest(bridgeInvocation("bash", nil), result)
	if request == nil {
		t.Fatal("request should not be nil")
	}
	if request.Message != "approve please" {
		t.Errorf("message = %q", request.Message)
	}
	if request.PayloadSchema == nil {
		t.Error("payload_schema should be set")
	}
	if request.Metadata == nil {
		t.Error("metadata should be initialized")
	}
}

// TestBuildRuntimeInterruptRequest_autoManual 验证 auto_manual 标记。
func TestBuildRuntimeInterruptRequest_autoManual(t *testing.T) {
	rail := newBridgeRail(t)
	result := map[string]any{
		"value": map[string]any{"auto_permission_manual": true},
	}
	request := rail.buildRuntimeInterruptRequest(bridgeInvocation("bash", nil), result)
	if request.Metadata["auto_permission_manual"] != true {
		t.Errorf("auto_permission_manual = %v", request.Metadata["auto_permission_manual"])
	}
	// auto_manual 时不应生成 auto_confirm_key
	if request.AutoConfirmKey != "" {
		t.Errorf("auto_confirm_key should be empty, got %q", request.AutoConfirmKey)
	}
}

// TestBuildRuntimeInterruptRequest_无message用默认 验证默认文案。
func TestBuildRuntimeInterruptRequest_无message用默认(t *testing.T) {
	rail := newBridgeRail(t)
	request := rail.buildRuntimeInterruptRequest(bridgeInvocation("bash", nil), map[string]any{})
	if request.Message != defaultManualApprovalMessage {
		t.Errorf("message = %q", request.Message)
	}
}

// ──────────────────────────── raiseRuntimeInterrupt ────────────────────────────

// TestRaiseRuntimeInterrupt_返回error 验证 Go 侧用 error 而非 raise。
func TestRaiseRuntimeInterrupt_返回error(t *testing.T) {
	rail := newBridgeRail(t)
	err := rail.raiseRuntimeInterrupt(bridgeInvocation("bash", nil), map[string]any{})
	if err == nil {
		t.Fatal("should return error")
	}
}

// ──────────────────────────── runtimeAutoConfirmKey ────────────────────────────

// TestRuntimeAutoConfirmKey_nil调用 验证 nil 返回空串。
func TestRuntimeAutoConfirmKey_nil调用(t *testing.T) {
	rail := newBridgeRail(t)
	if got := rail.runtimeAutoConfirmKey(bridgeInvocation("bash", nil)); got != "" {
		t.Errorf("got %q", got)
	}
}

// TestRuntimeAutoConfirmKey_无能力 验证 base rail 无该方法时返回空串。
func TestRuntimeAutoConfirmKey_无能力(t *testing.T) {
	rail := newBridgeRail(t)
	invocation := ToolInvocation{ToolName: "bash", ToolCall: map[string]any{"name": "bash"}}
	if got := rail.runtimeAutoConfirmKey(invocation); got != "" {
		t.Errorf("got %q", got)
	}
}

// ──────────────────────────── callBaseRail / callBaseAfterRail ────────────────────────────

// TestCallBaseRail_转发父类 验证调用穿透。
func TestCallBaseRail_转发父类(t *testing.T) {
	base := &fakePermissionRail{}
	rail, err := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:         base,
		PermissionConfig: map[string]any{},
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	inputs := &agentinterfaces.ToolCallInputs{ToolName: "bash"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	got, err := rail.callBaseRail(context.Background(), cbc, bridgeInvocation("bash", cbc))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil（父类无副作用）", got)
	}
}

// TestCallBaseRail_nil父轨 验证兜底响应。
func TestCallBaseRail_nil父轨(t *testing.T) {
	rail := &AutoPermissionRail{}
	got, err := rail.callBaseRail(context.Background(), nil, bridgeInvocation("bash", nil))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil {
		t.Fatal("should return manual approval response")
	}
}

// TestCallBaseAfterRail_无能力 验证父类无 AfterToolCall 时返回 nil。
func TestCallBaseAfterRail_无能力(t *testing.T) {
	rail := newBridgeRail(t)
	got, err := rail.callBaseAfterRail(context.Background(), nil, bridgeInvocation("bash", nil))
	if err != nil || got != nil {
		t.Errorf("got (%v, %v)", got, err)
	}
}

// ──────────────────────────── 带 tool_invocation_key 的路径 ────────────────────────────

// cbcWithKey 构造带 tool_invocation_key 的回调上下文。
func cbcWithKey() *agentinterfaces.AgentCallbackContext {
	inputs := &agentinterfaces.ToolCallInputs{ToolName: "bash"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	cbc.SetExtra(permissions.ToolInvocationContextAttribute, &permissions.ToolInvocationKeyV1{
		InvocationID:       "inv-1",
		RootSessionID:      "root-1",
		RequestID:          "req-1",
		ExecutorKind:       "agent",
		ExecutionSessionID: "sess-1",
		ToolCallID:         "call-1",
	})
	return cbc
}

// TestPrepareManualPendingInvocation_注入key 验证 value/metadata 两段都注入。
func TestPrepareManualPendingInvocation_注入key(t *testing.T) {
	cbc := cbcWithKey()
	result := map[string]any{
		"value":    map[string]any{"existing": "keep"},
		"metadata": map[string]any{"meta": 1},
	}
	got := prepareManualPendingInvocation(bridgeInvocation("bash", cbc), result)
	mapping, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T", got)
	}
	valueSection, _ := mapping["value"].(map[string]any)
	if valueSection["tool_invocation_key"] == nil {
		t.Error("value should contain tool_invocation_key")
	}
	if valueSection["existing"] != "keep" {
		t.Error("existing value fields should be preserved")
	}
	metadataSection, _ := mapping["metadata"].(map[string]any)
	if metadataSection["tool_invocation_key"] == nil {
		t.Error("metadata should contain tool_invocation_key")
	}
	if metadataSection["meta"] != 1 {
		t.Error("existing metadata fields should be preserved")
	}
}

// TestPrepareManualPendingInvocation_无value段 验证缺 value 时新建。
func TestPrepareManualPendingInvocation_无value段(t *testing.T) {
	cbc := cbcWithKey()
	got := prepareManualPendingInvocation(bridgeInvocation("bash", cbc), map[string]any{})
	mapping, _ := got.(map[string]any)
	if _, ok := mapping["value"].(map[string]any); !ok {
		t.Error("value section should be created")
	}
	if _, ok := mapping["metadata"].(map[string]any); !ok {
		t.Error("metadata section should be created")
	}
}

// TestRuntimeResult_中断分支 验证 interrupt 走 prepareManualPending 后直返。
func TestRuntimeResult_中断分支(t *testing.T) {
	rail := newBridgeRail(t)
	cbc := cbcWithKey()
	result := map[string]any{"message": "approve?"}

	got := rail.runtimeResult(bridgeInvocation("bash", cbc), result)
	mapping, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T", got)
	}
	// interrupt 分支会注入 tool_invocation_key
	valueSection, _ := mapping["value"].(map[string]any)
	if valueSection == nil || valueSection["tool_invocation_key"] == nil {
		t.Error("interrupt should inject tool_invocation_key")
	}
	// 不应设置 _skip_tool（interrupt 不跳过）
	if _, exists := cbc.Extra()[skipToolExtraKey]; exists {
		t.Error("interrupt should not set _skip_tool")
	}
}

// TestBuildRuntimeInterruptRequest_带key 验证 resume_user_input_key 与 key 元数据。
func TestBuildRuntimeInterruptRequest_带key(t *testing.T) {
	rail := newBridgeRail(t)
	key := &permissions.ToolInvocationKeyV1{InvocationID: "inv-1", ToolCallID: "call-1"}
	result := map[string]any{
		"value": map[string]any{"tool_invocation_key": key.ToWire()},
	}
	request := rail.buildRuntimeInterruptRequest(bridgeInvocation("bash", nil), result)

	if request.Metadata["resume_user_input_key"] != permissions.RootPermissionQueueUserInputKey {
		t.Errorf("resume_user_input_key = %v", request.Metadata["resume_user_input_key"])
	}
	if request.Metadata["tool_invocation_key"] == nil {
		t.Error("tool_invocation_key should be copied into metadata")
	}
}

// TestBuildRuntimeInterruptRequest_带ToolCall 验证 BindApprovalRequest 分支。
func TestBuildRuntimeInterruptRequest_带ToolCall(t *testing.T) {
	rail := newBridgeRail(t)
	invocation := ToolInvocation{
		ToolName: "bash",
		ToolCall: &llmschema.ToolCall{ID: "c1", Name: "bash"},
	}
	request := rail.buildRuntimeInterruptRequest(invocation, map[string]any{"message": "x"})
	if request == nil {
		t.Fatal("request should not be nil")
	}
}
