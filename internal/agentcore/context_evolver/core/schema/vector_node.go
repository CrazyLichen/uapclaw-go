package schema

import (
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VectorNode 向量存储标准序列化格式。
//
// 所有记忆类型（ACE/ReasoningBank/ReMe）通过此格式统一序列化。
// Embedding 使用 float64（Go 无 float32 向量库生态约束）。
//
// Python: openjiuwen/extensions/context_evolver/core/schema/vector_node.py
type VectorNode struct {
	// ID 唯一标识
	ID string `json:"id"`
	// Content 文本内容（用于 embedding）
	Content string `json:"content"`
	// Embedding 向量嵌入
	Embedding []float64 `json:"embedding,omitempty"`
	// Metadata 附加元数据
	Metadata map[string]any `json:"metadata"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVectorNode 创建 VectorNode 实例。
// 对齐 Python VectorNode(id, content, embedding, metadata)。
// metadata 为 nil 时自动初始化为空 map（对齐 Python default_factory=dict）。
func NewVectorNode(id, content string, embedding []float64, metadata map[string]any) *VectorNode {
	if metadata == nil {
		metadata = make(map[string]any)
	}
	return &VectorNode{
		ID:        id,
		Content:   content,
		Embedding: embedding,
		Metadata:  metadata,
	}
}

// VectorNodeFromDict 从字典创建 VectorNode。
// 对齐 Python VectorNode.from_dict(data)。
// 支持 JSON 反序列化后 embedding 为 []any 的场景（自动转换为 []float64）。
func VectorNodeFromDict(data map[string]any) (*VectorNode, error) {
	id, _ := data["id"].(string)
	if id == "" {
		return nil, fmt.Errorf("VectorNodeFromDict: 缺少 id 字段")
	}
	content, _ := data["content"].(string)
	if content == "" {
		return nil, fmt.Errorf("VectorNodeFromDict: 缺少 content 字段")
	}

	var embedding []float64
	if emb, ok := data["embedding"]; ok && emb != nil {
		// JSON 反序列化 []interface{} → []float64
		switch e := emb.(type) {
		case []float64:
			embedding = e
		case []any:
			embedding = make([]float64, len(e))
			for i, v := range e {
				f, ok := v.(float64)
				if !ok {
					return nil, fmt.Errorf("VectorNodeFromDict: embedding[%d] 不是 float64", i)
				}
				embedding[i] = f
			}
		}
	}

	metadata, _ := data["metadata"].(map[string]any)
	if metadata == nil {
		metadata = make(map[string]any)
	}

	return &VectorNode{
		ID:        id,
		Content:   content,
		Embedding: embedding,
		Metadata:  metadata,
	}, nil
}

// ToDict 转换为字典。
// 对齐 Python VectorNode.to_dict()，调用 Pydantic model_dump()。
// embedding 和 metadata 为空时不输出对应字段。
func (n *VectorNode) ToDict() map[string]any {
	dict := map[string]any{
		"id":      n.ID,
		"content": n.Content,
	}
	if n.Embedding != nil {
		dict["embedding"] = n.Embedding
	}
	if n.Metadata != nil {
		dict["metadata"] = n.Metadata
	}
	return dict
}

// String 实现 Stringer 接口。
// 对齐 Python VectorNode.__repr__()，content 超过 50 字符时截断。
func (n *VectorNode) String() string {
	preview := n.Content
	if len(preview) > 50 {
		preview = preview[:50] + "..."
	}
	return fmt.Sprintf("VectorNode(id=%s, content='%s')", n.ID, preview)
}
