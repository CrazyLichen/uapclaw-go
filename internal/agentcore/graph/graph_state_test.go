package graph

import (
	"slices"
	"testing"
)

// TestGraphState_零值 验证零值构造
func TestGraphState_零值(t *testing.T) {
	var s GraphState
	if s.SourceNodeID != nil {
		t.Fatalf("期望零值 nil，实际 %v", s.SourceNodeID)
	}
}

// TestGraphState_SourceNodeID追加语义 验证 operator.add（列表拼接）语义的字段级锚点
func TestGraphState_SourceNodeID追加语义(t *testing.T) {
	s := GraphState{}
	// 模拟两个节点执行后各自写入的追加（Python: {"source_node_id": ["n1"]} + {"source_node_id": ["n2"]}）
	s.SourceNodeID = append(s.SourceNodeID, "n1")
	s.SourceNodeID = append(s.SourceNodeID, "n2")
	if !slices.Equal(s.SourceNodeID, []string{"n1", "n2"}) {
		t.Fatalf("期望 [n1 n2]，实际 %v", s.SourceNodeID)
	}
}
