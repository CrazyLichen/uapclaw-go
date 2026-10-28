package acp

import (
	"testing"
)

// TestBuildAcpInitializeResult 结构验证。
func TestBuildAcpInitializeResult(t *testing.T) {
	r := BuildAcpInitializeResult()
	if r["protocolVersion"] != 1 {
		t.Errorf("protocolVersion = %v", r["protocolVersion"])
	}
	agentInfo, ok := r["agentInfo"].(map[string]any)
	if !ok {
		t.Fatalf("agentInfo 类型错误: %T", r["agentInfo"])
	}
	if agentInfo["name"] != "jiuwenswarm" || agentInfo["title"] != "JiuwenSwarm" {
		t.Errorf("agentInfo = %v", agentInfo)
	}
	if agentInfo["version"] == "" {
		t.Error("version 应非空")
	}
	caps, ok := r["agentCapabilities"].(map[string]any)
	if !ok {
		t.Fatalf("agentCapabilities 类型错误")
	}
	if caps["loadSession"] != false {
		t.Errorf("loadSession = %v", caps["loadSession"])
	}
	promptCaps, ok := caps["promptCapabilities"].(map[string]any)
	if !ok || promptCaps["image"] != false || promptCaps["embeddedContext"] != false {
		t.Errorf("promptCapabilities = %v", caps["promptCapabilities"])
	}
	mcpCaps, ok := caps["mcpCapabilities"].(map[string]any)
	if !ok || mcpCaps["http"] != false || mcpCaps["sse"] != false {
		t.Errorf("mcpCapabilities = %v", caps["mcpCapabilities"])
	}
	if _, ok := r["authMethods"].([]any); !ok {
		t.Errorf("authMethods 应为 []any")
	}
}

// TestAcpDefaultCapabilities_拷贝语义 验证返回的是深拷贝。
func TestAcpDefaultCapabilities_拷贝语义(t *testing.T) {
	c1 := AcpDefaultCapabilities()
	// 修改嵌套层不影响后续取值
	if ai, ok := c1["agentInfo"].(map[string]any); ok {
		ai["name"] = "mutated"
	}
	c2 := AcpDefaultCapabilities()
	ai, _ := c2["agentInfo"].(map[string]any)
	if ai["name"] != "jiuwenswarm" {
		t.Errorf("嵌套层应深拷贝, got %v", ai["name"])
	}
}

// TestBuildAcpSessionNewResult 结构验证。
func TestBuildAcpSessionNewResult(t *testing.T) {
	r := BuildAcpSessionNewResult("  s1  ")
	if r["sessionId"] != "s1" {
		t.Errorf("sessionId = %v", r["sessionId"])
	}
	if co, ok := r["configOptions"].([]any); !ok || len(co) != 0 {
		t.Errorf("configOptions = %v", r["configOptions"])
	}
}

// TestBuildAcpSessionListResult trim+去重验证。
func TestBuildAcpSessionListResult(t *testing.T) {
	r := BuildAcpSessionListResult([]string{" s1 ", "", "s1", "s2"})
	sessions, ok := r["sessions"].([]any)
	if !ok {
		t.Fatalf("sessions 类型错误")
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %v", sessions)
	}
	first, _ := sessions[0].(map[string]any)
	if first["sessionId"] != "s1" {
		t.Errorf("sessions[0] = %v", sessions[0])
	}
}

// TestBuildAcpPromptResult userMessageID 空时不带键。
func TestBuildAcpPromptResult(t *testing.T) {
	r := BuildAcpPromptResult("end_turn", "  msg-1  ")
	if r["stopReason"] != "end_turn" {
		t.Errorf("stopReason = %v", r["stopReason"])
	}
	if r["userMessageId"] != "msg-1" {
		t.Errorf("userMessageId = %v", r["userMessageId"])
	}
	r = BuildAcpPromptResult("cancelled", "  ")
	if _, ok := r["userMessageId"]; ok {
		t.Error("空白 userMessageId 不应带键")
	}
}
