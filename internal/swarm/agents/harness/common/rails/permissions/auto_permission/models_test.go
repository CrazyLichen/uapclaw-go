package auto_permission

import (
	"testing"

	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// TestToolInvocation_字段可赋值 验证四个字段的类型与读写
func TestToolInvocation_字段可赋值(t *testing.T) {
	inv := ToolInvocation{
		Ctx:      nil,
		ToolCall: map[string]any{"name": "read_file"},
		ToolName: "read_file",
		ToolArgs: map[string]any{"path": "/tmp/a.txt"},
	}
	if inv.ToolName != "read_file" {
		t.Fatalf("ToolName 应为 read_file，实际 %q", inv.ToolName)
	}
	if got := inv.ToolArgs["path"]; got != "/tmp/a.txt" {
		t.Fatalf("ToolArgs[path] 应为 /tmp/a.txt，实际 %v", got)
	}
}

// TestToolInvocation_ToolCall接受鸭子类型 验证 ToolCall 是全 any
//
// Python invocation_context.py:240 会塞 SimpleNamespace 替身而非真 ToolCall，
// 故 Go 侧必须是 any。
func TestToolInvocation_ToolCall接受鸭子类型(t *testing.T) {
	type simpleNamespace struct {
		name      string
		arguments map[string]any
	}
	inv := ToolInvocation{
		ToolCall: simpleNamespace{name: "read_file", arguments: map[string]any{"p": "x"}},
		ToolName: "read_file",
	}
	if _, ok := inv.ToolCall.(simpleNamespace); !ok {
		t.Fatalf("ToolCall 应能承载任意类型（鸭子类型替身），实际 %T", inv.ToolCall)
	}
}

// TestNewPermissionHandlingResult_正常构造 验证未处理且无来源时可构造
func TestNewPermissionHandlingResult_正常构造(t *testing.T) {
	res, err := NewPermissionHandlingResult(false, nil, "")
	if err != nil {
		t.Fatalf("handled=false 且 source 为空应允许，实际报错 %v", err)
	}
	if res.Handled {
		t.Fatalf("Handled 应为 false")
	}
	if res.Result != nil {
		t.Fatalf("Result 应为 nil")
	}
	if res.DecisionSource != "" {
		t.Fatalf("DecisionSource 应为空串")
	}
}

// TestNewPermissionHandlingResult_handled无来源报错 验证 post_init 校验
//
// Python: if self.handled and not self.decision_source: raise ValueError
func TestNewPermissionHandlingResult_handled无来源报错(t *testing.T) {
	res, err := NewPermissionHandlingResult(true, nil, "")
	if err == nil {
		t.Fatalf("handled=true 且 source 为空必须报错")
	}
	if res != nil {
		t.Fatalf("报错时应返回 nil，实际 %v", res)
	}
	if err.Error() != "handled permission result requires a decision source" {
		t.Fatalf("错误文案应与 Python 逐字一致，实际 %q", err.Error())
	}
}

// TestNewPermissionHandlingResult_handled有来源 验证正常路径
func TestNewPermissionHandlingResult_handled有来源(t *testing.T) {
	res, err := NewPermissionHandlingResult(true, map[string]any{"ok": true}, "deterministic_bounded_scope")
	if err != nil {
		t.Fatalf("handled=true 且有来源应允许，实际报错 %v", err)
	}
	if !res.Handled || res.DecisionSource != "deterministic_bounded_scope" {
		t.Fatalf("字段值不符：%+v", res)
	}
}

// TestPermissionInterruptRequest_嵌入字段可见 验证嵌入 InterruptRequest 后字段可读写
func TestPermissionInterruptRequest_嵌入字段可见(t *testing.T) {
	req := &PermissionInterruptRequest{
		InterruptRequest: saschema.InterruptRequest{
			Message:       "Approve",
			InterruptKind: saschema.InterruptKindPermission,
		},
	}
	if req.Message != "Approve" {
		t.Fatalf("嵌入字段 Message 应可见，实际 %q", req.Message)
	}
	if req.InterruptKind != saschema.InterruptKindPermission {
		t.Fatalf("InterruptKind 应为 permission，实际 %q", req.InterruptKind)
	}
}

// TestPermissionInterruptRequest_满足InterruptRequester 验证可当接口用
func TestPermissionInterruptRequest_满足InterruptRequester(t *testing.T) {
	req := &PermissionInterruptRequest{
		InterruptRequest: saschema.InterruptRequest{InterruptKind: saschema.InterruptKindPermission},
	}
	var _ saschema.InterruptRequester = req
	if req.GetInterruptKind() != saschema.InterruptKindPermission {
		t.Fatalf("GetInterruptKind 应为 permission，实际 %q", req.GetInterruptKind())
	}
}
