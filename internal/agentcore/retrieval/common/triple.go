package common

// ──────────────────────────── 结构体 ────────────────────────────

// Triple 三元组数据模型，OpenIE 抽取的最小知识单元。
//
// Python: openjiuwen/core/retrieval/common/triple.py (Triple)
// pydantic 必填校验（ValidationError）无 Go 显式对应物——
// str(x).strip() 构造点保证字段非空天然满足（偏差归档 spec §8-4）。
type Triple struct {
	// Subject 主语
	Subject string `json:"subject"`
	// Predicate 谓语
	Predicate string `json:"predicate"`
	// Object 宾语
	Object string `json:"object"`
	// Metadata 元数据（对齐 Python default_factory=dict；无 omitempty——
	// 序列化恒输出 metadata 键，nil 序列化为 null 对齐 Python 空字典场景由
	// 构造方初始化保证）
	Metadata map[string]any `json:"metadata"`
}
