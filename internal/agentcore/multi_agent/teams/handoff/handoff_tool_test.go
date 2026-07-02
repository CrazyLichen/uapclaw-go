package handoff

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewHandoffTool_基本创建 测试基本创建 HandoffTool
func TestNewHandoffTool_基本创建(t *testing.T) {
	ht := NewHandoffTool("agent_b", "代码审查")
	card := ht.Card()

	// 验证工具名称
	expectedName := "transfer_to_agent_b"
	if card.Name != expectedName {
		t.Errorf("工具名称: 期望 %s，实际 %s", expectedName, card.Name)
	}

	// 验证描述包含 targetDescription
	expectedDescPrefix := "Transfer the current task to agent_b for processing."
	if card.Description != expectedDescPrefix+" 代码审查" {
		t.Errorf("描述: 期望 %q，实际 %q", expectedDescPrefix+" 代码审查", card.Description)
	}
}

// TestNewHandoffTool_无targetDescription 测试无 targetDescription 时描述不含额外内容
func TestNewHandoffTool_无targetDescription(t *testing.T) {
	ht := NewHandoffTool("agent_c", "")
	card := ht.Card()

	expectedName := "transfer_to_agent_c"
	if card.Name != expectedName {
		t.Errorf("工具名称: 期望 %s，实际 %s", expectedName, card.Name)
	}

	expectedDesc := "Transfer the current task to agent_c for processing."
	if card.Description != expectedDesc {
		t.Errorf("描述: 期望 %q，实际 %q", expectedDesc, card.Description)
	}
}

// TestHandoffTool_Invoke_正常调用 测试 Invoke 正常调用返回交接信号
func TestHandoffTool_Invoke_正常调用(t *testing.T) {
	ht := NewHandoffTool("agent_b", "")
	result, err := ht.Invoke(context.Background(), map[string]any{
		"reason":  "需要专家处理",
		"message": "当前上下文信息",
	})
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}

	// 验证 __handoff_to__
	if result[HandoffTargetKey] != "agent_b" {
		t.Errorf("HandoffTargetKey: 期望 agent_b，实际 %v", result[HandoffTargetKey])
	}

	// 验证 __handoff_message__
	if result[HandoffMessageKey] != "当前上下文信息" {
		t.Errorf("HandoffMessageKey: 期望 当前上下文信息，实际 %v", result[HandoffMessageKey])
	}

	// 验证 __handoff_reason__
	if result[HandoffReasonKey] != "需要专家处理" {
		t.Errorf("HandoffReasonKey: 期望 需要专家处理，实际 %v", result[HandoffReasonKey])
	}
}

// TestHandoffTool_Invoke_缺少reason 测试 Invoke inputs 缺少 reason 时返回空 reason
func TestHandoffTool_Invoke_缺少reason(t *testing.T) {
	ht := NewHandoffTool("agent_b", "")
	result, err := ht.Invoke(context.Background(), map[string]any{
		"message": "仅含消息",
	})
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}

	if result[HandoffReasonKey] != "" {
		t.Errorf("HandoffReasonKey: 期望空字符串，实际 %v", result[HandoffReasonKey])
	}
}

// TestHandoffTool_Invoke_缺少message 测试 Invoke inputs 缺少 message 时返回空 message
func TestHandoffTool_Invoke_缺少message(t *testing.T) {
	ht := NewHandoffTool("agent_b", "")
	result, err := ht.Invoke(context.Background(), map[string]any{
		"reason": "仅有原因",
	})
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}

	if result[HandoffMessageKey] != "" {
		t.Errorf("HandoffMessageKey: 期望空字符串，实际 %v", result[HandoffMessageKey])
	}
}

// TestHandoffTool_Invoke_inputs为nil 测试 Invoke inputs 为 nil 时
func TestHandoffTool_Invoke_inputs为nil(t *testing.T) {
	ht := NewHandoffTool("agent_b", "")
	result, err := ht.Invoke(context.Background(), nil)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}

	if result[HandoffTargetKey] != "agent_b" {
		t.Errorf("HandoffTargetKey: 期望 agent_b，实际 %v", result[HandoffTargetKey])
	}
	if result[HandoffReasonKey] != "" {
		t.Errorf("HandoffReasonKey: 期望空字符串，实际 %v", result[HandoffReasonKey])
	}
	if result[HandoffMessageKey] != "" {
		t.Errorf("HandoffMessageKey: 期望空字符串，实际 %v", result[HandoffMessageKey])
	}
}

// TestHandoffTool_Card_返回正确ToolCard 测试 Card() 返回正确 ToolCard
func TestHandoffTool_Card_返回正确ToolCard(t *testing.T) {
	ht := NewHandoffTool("agent_b", "代码审查")
	card := ht.Card()

	if card == nil {
		t.Fatal("Card() 返回 nil")
	}

	// 验证名称
	if card.Name != "transfer_to_agent_b" {
		t.Errorf("Name: 期望 transfer_to_agent_b，实际 %s", card.Name)
	}

	// 验证 ID 非空（NewToolCard 自动生成 UUID）
	if card.ID == "" {
		t.Error("ID 不应为空")
	}

	// 验证 InputParams 包含 reason 和 message
	if len(card.InputParams) != 2 {
		t.Fatalf("InputParams 长度: 期望 2，实际 %d", len(card.InputParams))
	}

	reasonParam := card.InputParams[0]
	if reasonParam.Name != "reason" {
		t.Errorf("第一个参数名: 期望 reason，实际 %s", reasonParam.Name)
	}
	if !reasonParam.Required {
		t.Error("reason 参数应为必填")
	}

	messageParam := card.InputParams[1]
	if messageParam.Name != "message" {
		t.Errorf("第二个参数名: 期望 message，实际 %s", messageParam.Name)
	}
	if messageParam.Required {
		t.Error("message 参数不应为必填")
	}
}

// TestHandoffTool_Stream_正常调用 测试 Stream 正常调用返回交接信号
func TestHandoffTool_Stream_正常调用(t *testing.T) {
	ht := NewHandoffTool("agent_b", "")
	ch, err := ht.Stream(context.Background(), map[string]any{
		"reason":  "流式交接原因",
		"message": "流式交接消息",
	})
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	// 读取数据块
	var dataChunk tool.StreamChunk
	var doneChunk tool.StreamChunk
	count := 0
	for chunk := range ch {
		switch count {
		case 0:
			dataChunk = chunk
		case 1:
			doneChunk = chunk
		}
		count++
	}

	if count != 2 {
		t.Fatalf("流块数量: 期望 2，实际 %d", count)
	}

	// 验证数据块
	if dataChunk.Done {
		t.Error("第一个块不应标记为 Done")
	}
	if dataChunk.Data[HandoffTargetKey] != "agent_b" {
		t.Errorf("HandoffTargetKey: 期望 agent_b，实际 %v", dataChunk.Data[HandoffTargetKey])
	}
	if dataChunk.Data[HandoffReasonKey] != "流式交接原因" {
		t.Errorf("HandoffReasonKey: 期望 流式交接原因，实际 %v", dataChunk.Data[HandoffReasonKey])
	}

	// 验证结束块
	if !doneChunk.Done {
		t.Error("第二个块应标记为 Done")
	}
}

// TestHandoffTool_Invoke_结果可被ExtractHandoffSignal解析 测试 Invoke 结果可被 ExtractHandoffSignal 解析
func TestHandoffTool_Invoke_结果可被ExtractHandoffSignal解析(t *testing.T) {
	ht := NewHandoffTool("agent_b", "")
	result, err := ht.Invoke(context.Background(), map[string]any{
		"reason":  "可解析原因",
		"message": "可解析消息",
	})
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}

	signal := ExtractHandoffSignal(result, nil)
	if signal == nil {
		t.Fatal("ExtractHandoffSignal 返回 nil，期望返回 HandoffSignal")
	}
	if signal.Target != "agent_b" {
		t.Errorf("Target: 期望 agent_b，实际 %s", signal.Target)
	}
	if signal.Reason != "可解析原因" {
		t.Errorf("Reason: 期望 可解析原因，实际 %s", signal.Reason)
	}
	if signal.Message != "可解析消息" {
		t.Errorf("Message: 期望 可解析消息，实际 %s", signal.Message)
	}
}
