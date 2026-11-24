// Package common 提供 Symphony 检索域的应用中立数据模型。
//
// 对应 Python 代码：openjiuwen/symphony/retrieval/common/models.py（68 行，一比一对齐）。
// 六个 frozen dataclass → Go 只读结构体；RetrieverTrace 带锁事件流。
//
// 文件目录：
//
//	common/
//	├── doc.go        # 包文档
//	└── models.go    # RetrieverCandidate/Item/Choice/Node/TraceEvent/Trace
package common

import "sync"

// RetrieverCandidate 检索候选（rank + 所属分支路径）
type RetrieverCandidate struct {
	// Rank 排名
	Rank int
	// ItemID 候选条目标识
	ItemID string
	// Payload 负载串（通常为 worker_id）
	Payload string
	// BranchPath 分支路径段
	BranchPath []string
	// Label 展示名（缺省空）
	Label string
	// Description 描述（缺省空）
	Description string
}

// RetrieverItem 检索条目
type RetrieverItem struct {
	// ItemID 条目标识
	ItemID string
	// Payload 负载串
	Payload string
	// Label 展示名（缺省空）
	Label string
	// Description 描述（缺省空）
	Description string
}

// RetrieverChoice 检索选择项
type RetrieverChoice struct {
	// ChoiceID 选择标识
	ChoiceID string
	// Payload 负载串
	Payload string
	// Description 描述（缺省空）
	Description string
}

// RetrieverNode 检索树节点（children 为子节点元组）
type RetrieverNode struct {
	// NodeID 节点标识
	NodeID string
	// Label 展示名
	Label string
	// Description 描述（缺省空）
	Description string
	// Children 子节点
	Children []RetrieverNode
	// Items 直挂条目
	Items []RetrieverItem
}

// RetrieverTraceEvent 追踪事件
type RetrieverTraceEvent struct {
	// EventType 事件类型
	EventType string
	// NodeID 节点标识
	NodeID string
	// Depth 深度
	Depth int
	// Detail 事件明细
	Detail map[string]any
}

// RetrieverTrace 追踪事件流（带锁追加）
type RetrieverTrace struct {
	// Events 事件列表
	Events []RetrieverTraceEvent

	mu sync.Mutex
}

// Record 追加一条追踪事件（并发安全，detail 值拷贝防逃逸修改）
func (t *RetrieverTrace) Record(eventType string, nodeID string, depth int, detail map[string]any) {
	payload := make(map[string]any, len(detail))
	for k, v := range detail {
		payload[k] = v
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Events = append(t.Events, RetrieverTraceEvent{EventType: eventType, NodeID: nodeID, Depth: depth, Detail: payload})
}
