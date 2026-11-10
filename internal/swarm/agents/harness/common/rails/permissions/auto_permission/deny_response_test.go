package auto_permission

import (
	"strings"
	"testing"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// denyFacts 构造拒绝响应用例的 facts。
func denyFacts() permissions.ToolDecisionFacts {
	return permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{
			ToolName: "bash",
			Category: "shell",
			RiskTier: "high",
		},
		WorkspaceRoot: "/ws",
	}
}

// TestReviewerDenyResponse_基本结构 验证响应字段与决策来源。
func TestReviewerDenyResponse_基本结构(t *testing.T) {
	rail := newBridgeRail(t)
	response, source := rail.reviewerDenyResponse(denyFacts(), "web", map[string]any{})

	if source != "auto_reviewer" {
		t.Errorf("decision_source = %q", source)
	}
	if response["status"] != "denied" {
		t.Errorf("status = %v", response["status"])
	}
	if response["permission_decision"] != "deny" {
		t.Errorf("permission_decision = %v", response["permission_decision"])
	}
	if response["reason"] != reviewerDenyReason {
		t.Errorf("reason = %v", response["reason"])
	}
	if response["reviewer_metadata"] == nil {
		t.Error("reviewer_metadata should be set")
	}
}

// TestReviewerDenyResponse_feedback含引导语 验证拒绝引导语拼接。
func TestReviewerDenyResponse_feedback含引导语(t *testing.T) {
	rail := newBridgeRail(t)
	response, _ := rail.reviewerDenyResponse(denyFacts(), "web", map[string]any{})

	feedback, _ := response["feedback"].(string)
	if !strings.HasPrefix(feedback, "[PERMISSION_DENIED] reviewer_deny") {
		t.Errorf("feedback prefix = %q", feedback)
	}
	if !strings.Contains(feedback, ReviewerDenialGuidance) {
		t.Error("feedback should contain denial guidance")
	}
}

// TestReviewerDenyResponse_带摘要 验证摘要拼入 feedback。
func TestReviewerDenyResponse_带摘要(t *testing.T) {
	rail := newBridgeRail(t)
	response, _ := rail.reviewerDenyResponse(denyFacts(), "web", map[string]any{
		"reviewer_reason_summary": "too risky",
	})
	feedback, _ := response["feedback"].(string)
	if !strings.Contains(feedback, ": too risky.") {
		t.Errorf("feedback = %q", feedback)
	}
}

// TestReviewerDenyResponse_摘要回落evidence 验证 evidence_summary 兜底。
func TestReviewerDenyResponse_摘要回落evidence(t *testing.T) {
	rail := newBridgeRail(t)
	response, _ := rail.reviewerDenyResponse(denyFacts(), "web", map[string]any{
		"evidence_summary": "evidence text",
	})
	feedback, _ := response["feedback"].(string)
	if !strings.Contains(feedback, "evidence text") {
		t.Errorf("feedback = %q", feedback)
	}
}

// TestReviewerDenyResponse_channelKind归一 验证通道类型进元数据。
func TestReviewerDenyResponse_channelKind归一(t *testing.T) {
	rail := newBridgeRail(t)
	response, _ := rail.reviewerDenyResponse(denyFacts(), "  WEB  ", map[string]any{})
	metadata, _ := response["reviewer_metadata"].(map[string]any)
	if metadata["channel_kind"] != "web" {
		t.Errorf("channel_kind = %v", metadata["channel_kind"])
	}
}

// TestRecordReviewerSuccessMetadata_nilCtx 验证 nil 安全。
func TestRecordReviewerSuccessMetadata_nilCtx(t *testing.T) {
	recordReviewerSuccessMetadata(nil, denyFacts(), "c1", "reason", map[string]any{})
}

// TestRecordReviewerSuccessMetadata_写入extra 验证元数据写入。
func TestRecordReviewerSuccessMetadata_写入extra(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	recordReviewerSuccessMetadata(cbc, denyFacts(), "c1", "reviewer_allow_once", map[string]any{
		"action_summary": "bash (shell, high risk)",
	})
	if len(cbc.Extra()) == 0 {
		t.Error("extra should be written")
	}
}
