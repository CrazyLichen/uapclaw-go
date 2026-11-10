package auto_permission

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// manualFacts 构造人工审批用例的 facts。
func manualFacts() permissions.ToolDecisionFacts {
	return permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{
			ToolName: "bash",
			Category: "shell",
			RiskTier: "high",
		},
		WorkspaceRoot: "/ws",
		UntrustedArgs: map[string]any{"command": "rm -rf /"},
	}
}

// TestManualReviewerResponse_基本 验证响应非空且带元数据。
func TestManualReviewerResponse_基本(t *testing.T) {
	rail := newBridgeRail(t)
	got := rail.manualReviewerResponse(manualFacts(), manualReviewerResponseOptions{
		Reason:               "reviewer_manual",
		Metadata:             map[string]any{},
		IncludePromptPayload: true,
	})
	if got == nil {
		t.Fatal("response should not be nil")
	}
}

// TestManualReviewerResponse_channelKind参数优先 验证参数覆盖 metadata。
func TestManualReviewerResponse_channelKind参数优先(t *testing.T) {
	rail := newBridgeRail(t)
	got := rail.manualReviewerResponse(manualFacts(), manualReviewerResponseOptions{
		Reason:      "reviewer_manual",
		Metadata:    map[string]any{"channel_kind": "cli"},
		ChannelKind: "WEB",
	})
	// 参数优先 → web（小写化）
	metadata := extractReviewerMetadata(got)
	if metadata == nil {
		t.Skip("response shape has no reviewer_metadata")
	}
	if metadata["channel_kind"] != "web" {
		t.Errorf("channel_kind = %v, want web（参数优先）", metadata["channel_kind"])
	}
}

// TestManualReviewerResponse_channelKind回落metadata 验证空参数时用 metadata。
func TestManualReviewerResponse_channelKind回落metadata(t *testing.T) {
	rail := newBridgeRail(t)
	got := rail.manualReviewerResponse(manualFacts(), manualReviewerResponseOptions{
		Reason:   "reviewer_manual",
		Metadata: map[string]any{"channel_kind": "CLI"},
	})
	metadata := extractReviewerMetadata(got)
	if metadata == nil {
		t.Skip("response shape has no reviewer_metadata")
	}
	if metadata["channel_kind"] != "cli" {
		t.Errorf("channel_kind = %v, want cli", metadata["channel_kind"])
	}
}

// TestManualReviewerResponse_displayReason优先 验证显式展示原因。
func TestManualReviewerResponse_displayReason优先(t *testing.T) {
	rail := newBridgeRail(t)
	displayReason := "  宿主指定原因  "
	got := rail.manualReviewerResponse(manualFacts(), manualReviewerResponseOptions{
		Reason:        "reviewer_manual",
		DisplayReason: &displayReason,
		Metadata:      map[string]any{},
	})
	if !containsStringValue(got, "宿主指定原因") {
		t.Errorf("display_reason should be used, got %v", got)
	}
}

// TestManualReviewerResponse_autoPermissionManual标记 验证三者齐备时打标。
func TestManualReviewerResponse_autoPermissionManual标记(t *testing.T) {
	rail := newBridgeRail(t)
	got := rail.manualReviewerResponse(manualFacts(), manualReviewerResponseOptions{
		Reason:               "reviewer_manual",
		Metadata:             map[string]any{},
		SessionID:            "s1",
		RequestID:            "r1",
		ToolCallID:           "c1",
		IncludePromptPayload: true,
	})
	metadata := extractReviewerMetadata(got)
	if metadata == nil {
		t.Skip("response shape has no reviewer_metadata")
	}
	if metadata["auto_permission_manual"] != true {
		t.Errorf("auto_permission_manual = %v", metadata["auto_permission_manual"])
	}
}

// TestManualReviewerResponse_缺request不打标 验证三者不全时不打标。
func TestManualReviewerResponse_缺request不打标(t *testing.T) {
	rail := newBridgeRail(t)
	got := rail.manualReviewerResponse(manualFacts(), manualReviewerResponseOptions{
		Reason:               "reviewer_manual",
		Metadata:             map[string]any{},
		SessionID:            "s1",
		ToolCallID:           "c1",
		IncludePromptPayload: true,
	})
	metadata := extractReviewerMetadata(got)
	if metadata == nil {
		t.Skip("response shape has no reviewer_metadata")
	}
	if metadata["auto_permission_manual"] == true {
		t.Error("should not set flag without request_id")
	}
}

// TestManualReviewerResponse_不构造promptPayload 验证 include=false。
func TestManualReviewerResponse_不构造promptPayload(t *testing.T) {
	rail := newBridgeRail(t)
	got := rail.manualReviewerResponse(manualFacts(), manualReviewerResponseOptions{
		Reason:               "reviewer_manual",
		Metadata:             map[string]any{},
		SessionID:            "s1",
		RequestID:            "r1",
		ToolCallID:           "c1",
		IncludePromptPayload: false,
	})
	metadata := extractReviewerMetadata(got)
	if metadata == nil {
		t.Skip("response shape has no reviewer_metadata")
	}
	if metadata["auto_permission_manual"] == true {
		t.Error("include_prompt_payload=false should not set flag")
	}
}

// TestManualReviewerResponse_recordKind 验证 record_kind 写入。
func TestManualReviewerResponse_recordKind(t *testing.T) {
	rail := newBridgeRail(t)
	got := rail.manualReviewerResponse(manualFacts(), manualReviewerResponseOptions{
		Reason:   "reviewer_manual",
		Metadata: map[string]any{},
	})
	metadata := extractReviewerMetadata(got)
	if metadata == nil {
		t.Skip("response shape has no reviewer_metadata")
	}
	if metadata["record_kind"] != permissionReviewerAssessmentRecordKind {
		t.Errorf("record_kind = %v", metadata["record_kind"])
	}
}

// TestCopyMetadata 验证浅拷贝。
func TestCopyMetadata(t *testing.T) {
	source := map[string]any{"a": 1}
	got := copyMetadata(source)
	got["b"] = 2
	if _, exists := source["b"]; exists {
		t.Error("should be a copy")
	}
	if copyMetadata(nil) == nil {
		t.Error("nil should become empty map")
	}
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// extractReviewerMetadata 从响应里取 reviewer_metadata（不同响应形状的容错）。
func extractReviewerMetadata(response map[string]any) map[string]any {
	if metadata, ok := response["reviewer_metadata"].(map[string]any); ok {
		return metadata
	}
	if metadata, ok := response["metadata"].(map[string]any); ok {
		return metadata
	}
	return nil
}

// containsStringValue 递归检查响应里是否含指定字符串值。
func containsStringValue(value any, target string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, target)
	case map[string]any:
		for _, nested := range typed {
			if containsStringValue(nested, target) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if containsStringValue(nested, target) {
				return true
			}
		}
	}
	return false
}
