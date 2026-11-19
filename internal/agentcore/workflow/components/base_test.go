package components

import "testing"

// TestWorkflowComponentMetadata_字段 验证元数据结构字段可读写。
func TestWorkflowComponentMetadata_字段(t *testing.T) {
	m := WorkflowComponentMetadata{NodeID: "n1", NodeType: "End", NodeName: "结束"}
	if m.NodeID != "n1" || m.NodeType != "End" || m.NodeName != "结束" {
		t.Fatalf("字段读写不符: %+v", m)
	}
}

// TestComponentConfig_默认 验证配置零值 Metadata 为 nil。
func TestComponentConfig_默认(t *testing.T) {
	c := ComponentConfig{}
	if c.Metadata != nil {
		t.Fatalf("零值 Metadata 应为 nil: %+v", c)
	}
}

// TestComponentState_默认 验证状态结构零值可用。
func TestComponentState_默认(t *testing.T) {
	s := ComponentState{CompID: "c1"}
	if s.CompID != "c1" || s.Status != nil {
		t.Fatalf("状态字段不符: %+v", s)
	}
}
