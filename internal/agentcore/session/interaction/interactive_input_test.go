package interaction

import (
	"testing"
)

// ──────────────────────────── NewInteractiveInput 测试 ────────────────────────────

// TestNewInteractiveInput_无参数 测试不传参数时 RawInputs 为 nil
func TestNewInteractiveInput_无参数(t *testing.T) {
	input, err := NewInteractiveInput()
	if err != nil {
		t.Fatalf("不应返回错误：%v", err)
	}
	if input.RawInputs != nil {
		t.Errorf("RawInputs 应为 nil，实际=%v", input.RawInputs)
	}
	if input.UserInputs == nil {
		t.Error("UserInputs 不应为 nil")
	}
}

// TestNewInteractiveInput_有值 测试传入有效值时 RawInputs 被设置
func TestNewInteractiveInput_有值(t *testing.T) {
	input, err := NewInteractiveInput("user_response")
	if err != nil {
		t.Fatalf("不应返回错误：%v", err)
	}
	if input.RawInputs != "user_response" {
		t.Errorf("RawInputs 期望 'user_response'，实际=%v", input.RawInputs)
	}
}

// TestNewInteractiveInput_传入nil返回错误 测试显式传入 nil 被拒绝
func TestNewInteractiveInput_传入nil返回错误(t *testing.T) {
	_, err := NewInteractiveInput(nil)
	if err == nil {
		t.Fatal("传入 nil 时应返回错误")
	}
}

// ──────────────────────────── Update 测试 ────────────────────────────

// TestInteractiveInput_Update_正常 测试正常 Update
func TestInteractiveInput_Update_正常(t *testing.T) {
	input, _ := NewInteractiveInput()
	err := input.Update("node1", "value1")
	if err != nil {
		t.Fatalf("Update 不应返回错误：%v", err)
	}
	if input.UserInputs["node1"] != "value1" {
		t.Errorf("UserInputs['node1'] 期望 'value1'，实际=%v", input.UserInputs["node1"])
	}
}

// TestInteractiveInput_Update_RawInputs已存在 测试 RawInputs 存在时 Update 被拒绝
func TestInteractiveInput_Update_RawInputs已存在(t *testing.T) {
	input, _ := NewInteractiveInput("raw_data")
	err := input.Update("node1", "value1")
	if err == nil {
		t.Fatal("RawInputs 已存在时 Update 应返回错误")
	}
}

// TestInteractiveInput_Update_nodeID为空 测试 nodeID 为空字符串时允许通过（对齐 Python：只拒绝 None，不拒绝 ""）
func TestInteractiveInput_Update_nodeID为空(t *testing.T) {
	input, _ := NewInteractiveInput()
	err := input.Update("", "value1")
	if err != nil {
		t.Fatalf("nodeID 为空字符串时 Update 不应返回错误（对齐 Python）： %v", err)
	}
	if input.UserInputs[""] != "value1" {
		t.Errorf("UserInputs[''] 期望 'value1'，实际=%v", input.UserInputs[""])
	}
}

// TestInteractiveInput_Update_value为nil 测试 value 为 nil 时返回错误
func TestInteractiveInput_Update_value为nil(t *testing.T) {
	input, _ := NewInteractiveInput()
	err := input.Update("node1", nil)
	if err == nil {
		t.Fatal("value 为 nil 时 Update 应返回错误")
	}
}

// TestInteractiveInput_Update_多次 测试多次 Update 追加不同节点
func TestInteractiveInput_Update_多次(t *testing.T) {
	input, _ := NewInteractiveInput()
	_ = input.Update("node1", "value1")
	_ = input.Update("node2", "value2")

	if len(input.UserInputs) != 2 {
		t.Errorf("UserInputs 长度应为 2，实际=%d", len(input.UserInputs))
	}
	if input.UserInputs["node1"] != "value1" {
		t.Errorf("UserInputs['node1'] 期望 'value1'，实际=%v", input.UserInputs["node1"])
	}
	if input.UserInputs["node2"] != "value2" {
		t.Errorf("UserInputs['node2'] 期望 'value2'，实际=%v", input.UserInputs["node2"])
	}
}
