package common

import (
	"slices"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TripleMemory 多跳检索的事实记忆（AgenticRetriever 消费）。
//
// includedTriples 以小写化 join 字符串为键跨轮去重；memory 保序累积；
// TriplesStr 渲染为提示词注入串。单 goroutine 使用（Python 无锁，
// 13.25 消费同为单协程迭代循环），不加 mutex（spec 决策 7）。
//
// Python: openjiuwen/core/retrieval/common/triple_memory.py
type TripleMemory struct {
	// includedTriples 去重键集合（tuple2str：逐项 lower 后空格 join）
	includedTriples map[string]struct{}
	// memory 保序累积的事实列表
	memory []TripleFact
}

// ──────────────────────────── 枚举 ────────────────────────────

// TripleFact 读取侧三元组事实（定长 3 元：主语/谓语/宾语）。
//
// Python 对应物是 tuple[str, ...] 变长元组——_read LLM 输出经
// len(triple) == 3 过滤后恒为 3 元，Go 定长数组编译期收窄（偏差
// 归档 spec §8-2）。写入侧结构化模型见 Triple（13.17，带 Metadata），
// 两侧管线分立不复用。
type TripleFact = [3]string

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTripleMemory 创建空记忆（includedTriples 集合初始化；零值结构体
// 不可用，map 未初始化写入会 panic）。
func NewTripleMemory() *TripleMemory {
	return &TripleMemory{
		includedTriples: make(map[string]struct{}),
	}
}

// Len 返回已累积事实数（对齐 __len__）。
func (m *TripleMemory) Len() int {
	return len(m.memory)
}

// Memory 返回事实列表副本（浅拷贝密封，偏差归档 spec §8-1）。
func (m *TripleMemory) Memory() []TripleFact {
	return slices.Clone(m.memory)
}

// TriplesStr 渲染记忆为多行字符串，每行 "(s p o)"（空格 join 三元素 +
// 圆括号包裹 + 换行连接，对齐 triples_str property）。不做小写化——
// 与去重键 tuple2str 两套逻辑，展示串保留原大小写（Python 同款
// 不对称，测试 M7 钉死）。
func (m *TripleMemory) TriplesStr() string {
	formatted := make([]string, 0, len(m.memory))
	for _, triple := range m.memory {
		formatted = append(formatted, "("+strings.Join(triple[:], " ")+")")
	}
	return strings.Join(formatted, "\n")
}

// ExtendMemory 追加单条事实：小写化 join 键已存在则跳过（去重），
// 否则入集合并保序追加（对齐 extend_memory）。
func (m *TripleMemory) ExtendMemory(newTriple TripleFact) {
	newTripleStr := tuple2str(newTriple)
	if _, ok := m.includedTriples[newTripleStr]; !ok {
		m.includedTriples[newTripleStr] = struct{}{}
		m.memory = append(m.memory, newTriple)
	}
}

// BatchExtendMemory 批量追加（循环委托 ExtendMemory，对齐 batch_extend_memory）。
func (m *TripleMemory) BatchExtendMemory(newTriples []TripleFact) {
	for _, tmpTriple := range newTriples {
		m.ExtendMemory(tmpTriple)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// tuple2str 三元组转去重键字符串：逐项 lower 后空格 join
// （对齐 _tuple2str 的 item.lower() 语义——非整体 lower）。
func tuple2str(newTriple TripleFact) string {
	lowered := make([]string, len(newTriple))
	for i, item := range newTriple {
		lowered[i] = strings.ToLower(item)
	}
	return strings.Join(lowered, " ")
}
