package common

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestTriple_创建 测试创建三元组（对齐 test_triple.py test_create_triple）
func TestTriple_创建(t *testing.T) {
	triple := Triple{
		Subject:   "Alice",
		Predicate: "knows",
		Object:    "Bob",
	}
	if triple.Subject != "Alice" {
		t.Errorf("Subject 应为 Alice，实际 %s", triple.Subject)
	}
	if triple.Predicate != "knows" {
		t.Errorf("Predicate 应为 knows，实际 %s", triple.Predicate)
	}
	if triple.Object != "Bob" {
		t.Errorf("Object 应为 Bob，实际 %s", triple.Object)
	}
	// 对齐 assert triple.metadata == {}：Python default_factory=dict 零值为 nil，
	// Go 结构体零值 nil map 语义等价（读取/len/遍历安全，赋值前需初始化）
	if triple.Metadata != nil {
		t.Errorf("Metadata 零值应为 nil（对齐 Python 未传时 default_factory=dict 空字典的语义等价物），实际 %v", triple.Metadata)
	}
}

// TestTriple_带metadata 测试带元数据创建（对齐 test_create_triple_with_metadata）
func TestTriple_带metadata(t *testing.T) {
	metadata := map[string]any{"source": "test", "doc_id": "doc_1"}
	triple := Triple{
		Subject:   "Alice",
		Predicate: "knows",
		Object:    "Bob",
		Metadata:  metadata,
	}
	if len(triple.Metadata) != 2 {
		t.Errorf("Metadata 应有 2 个键，实际 %d", len(triple.Metadata))
	}
	if triple.Metadata["source"] != "test" {
		t.Errorf("Metadata[source] 应为 test，实际 %v", triple.Metadata["source"])
	}
	if triple.Metadata["doc_id"] != "doc_1" {
		t.Errorf("Metadata[doc_id] 应为 doc_1，实际 %v", triple.Metadata["doc_id"])
	}
}

// TestTriple_全字段 测试全字段创建（对齐 test_create_triple_with_all_fields）
func TestTriple_全字段(t *testing.T) {
	metadata := map[string]any{"source": "test"}
	triple := Triple{
		Subject:   "Alice",
		Predicate: "knows",
		Object:    "Bob",
		Metadata:  metadata,
	}
	if triple.Subject != "Alice" || triple.Predicate != "knows" || triple.Object != "Bob" {
		t.Errorf("三字段断言失败: %+v", triple)
	}
	if triple.Metadata["source"] != "test" {
		t.Errorf("Metadata[source] 应为 test，实际 %v", triple.Metadata["source"])
	}
}

// TestTriple_JSON序列化 测试 JSON tag（Go 增强项：验证 spec §4.1 无 omitempty 决策）
func TestTriple_JSON序列化(t *testing.T) {
	// marshal nil Metadata 恒输出 "metadata" 键（对齐 Python default_factory=dict
	// 序列化输出 {} 而非省略——无 omitempty 决策）
	triple := Triple{Subject: "a", Predicate: "b", Object: "c"}
	data, err := json.Marshal(triple)
	if err != nil {
		t.Fatalf("marshal 失败: %v", err)
	}
	if !strings.Contains(string(data), `"metadata"`) {
		t.Errorf("序列化应含 metadata 键（无 omitempty），实际 %s", data)
	}
}
