package auto_permission

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// TestDeniedPermissionResponse_固定字段 验证 9 个固定字段。
func TestDeniedPermissionResponse_固定字段(t *testing.T) {
	got := deniedPermissionResponse("reviewer_deny", map[string]any{})
	expected := map[string]any{
		"approved":            false,
		"auto_confirm":        false,
		"feedback":            "[PERMISSION_DENIED] reviewer_deny",
		"reason":              "reviewer_deny",
		"success":             false,
		"status":              "denied",
		"permission_decision": "deny",
		"permission_status":   "denied",
	}
	for key, want := range expected {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
}

// TestDeniedPermissionResponse_空原因兜底 验证默认原因码。
func TestDeniedPermissionResponse_空原因兜底(t *testing.T) {
	for _, reason := range []string{"", "   "} {
		got := deniedPermissionResponse(reason, map[string]any{})
		if got["reason"] != defaultPermissionDeniedReason {
			t.Errorf("reason=%q -> %v", reason, got["reason"])
		}
		if got["feedback"] != "[PERMISSION_DENIED] permission_denied" {
			t.Errorf("feedback = %v", got["feedback"])
		}
	}
}

// TestDeniedPermissionResponse_metadata深拷贝 验证不共享引用。
func TestDeniedPermissionResponse_metadata深拷贝(t *testing.T) {
	source := map[string]any{"k": "v"}
	got := deniedPermissionResponse("r", source)
	metadata := got["reviewer_metadata"].(map[string]any)
	metadata["k"] = "changed"
	if source["k"] != "v" {
		t.Error("metadata should be copied")
	}
}

// TestPermissionPromptPayload_基本 验证提示载荷结构。
func TestPermissionPromptPayload_基本(t *testing.T) {
	facts := permissions.ToolDecisionFacts{
		Capability:    permissions.ToolCapability{ToolName: "bash"},
		UntrustedArgs: map[string]any{"command": "ls"},
	}
	got := permissionPromptPayload(facts, "c1")
	if got["tool_name"] != "bash" {
		t.Errorf("tool_name = %v", got["tool_name"])
	}
	args, ok := got["tool_args"].(map[string]any)
	if !ok || args["command"] != "ls" {
		t.Errorf("tool_args = %v", got["tool_args"])
	}
	if got["tool_call_id"] != "c1" {
		t.Errorf("tool_call_id = %v", got["tool_call_id"])
	}
}

// TestPermissionPromptPayload_无toolCallID 验证空 ID 不写入该键。
func TestPermissionPromptPayload_无toolCallID(t *testing.T) {
	facts := permissions.ToolDecisionFacts{Capability: permissions.ToolCapability{ToolName: "bash"}}
	got := permissionPromptPayload(facts, "")
	if _, exists := got["tool_call_id"]; exists {
		t.Error("empty tool_call_id should be omitted")
	}
}

// TestPermissionPromptPayload_args深拷贝 验证不共享引用。
func TestPermissionPromptPayload_args深拷贝(t *testing.T) {
	facts := permissions.ToolDecisionFacts{
		Capability:    permissions.ToolCapability{ToolName: "bash"},
		UntrustedArgs: map[string]any{"command": "ls"},
	}
	got := permissionPromptPayload(facts, "")
	args := got["tool_args"].(map[string]any)
	args["command"] = "changed"
	if facts.UntrustedArgs["command"] != "ls" {
		t.Error("tool_args should be copied")
	}
}
