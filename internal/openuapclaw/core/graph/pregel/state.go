package pregel

// 图引擎检查点状态的地基类型。原定义于 internal/openuapclaw/core/graph/store/base.go
//（对应 Python store/base.py:32-82），2026-11-15 8.3c 实施时迁入本包——
// engine.py:20 直接消费 GraphState/Store/CreateState，而 store 包已依赖 pregel
//（GraphState 字段用 Message/ChannelSnapshot/PendingNode），Go 编译期环以类型迁移
// 为忠实等价物（spec 8.3c 偏差 B5，与 8.3b D6 PendingNode 迁移同源）。
// store 包保留 GraphStore 日志壳 / InMemoryStore / serde，改引用本包类型。

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// GraphState 引擎检查点状态快照。
// ⚠️ 与 8.8 graph_state.GraphState（节点返回值聚合）同名不同义，Go 靠包隔离。
// Python: store/base.py:32-39
type GraphState struct {
	// Ns 命名空间
	Ns string
	// Step super-step 序号
	Step int
	// ChannelValues 通道快照（Python Dict[str, Any]，按计划行收窄，偏差 D-5）
	ChannelValues map[string][]ChannelSnapshot
	// PendingBuffer 待发消息
	PendingBuffer []Message
	// PendingNode 未完成节点表
	PendingNode map[string]PendingNode
	// NodeVersion 节点版本
	NodeVersion map[string]int
}

// Store 图状态存储抽象
// Python: store/base.py:59-68（async 三方法，Go 显式 ctx 传参见偏差 D-13）
type Store interface {
	// Get 读取状态，未命中返回 (nil, nil)
	Get(ctx context.Context, sessionID, ns string) (*GraphState, error)
	// Save 保存状态
	Save(ctx context.Context, sessionID, ns string, state *GraphState) error
	// Delete 删除状态；ns 为空串时删整个 session（Python Optional[str]=None，偏差 D-3）
	Delete(ctx context.Context, sessionID, ns string) error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsResumable 有未完成节点/待发消息或 channel 值时引擎 init 才走恢复分支。
// 恢复判定必须与 8.3c PregelLoop._is_resume 同口径，两处经本方法共享，避免口径漂移。
// Python: store/base.py:42-47
func (s *GraphState) IsResumable() bool {
	return len(s.PendingNode) > 0 || len(s.PendingBuffer) > 0 || len(s.ChannelValues) > 0
}

// Clone 返回 GraphState 的深拷贝（Python copy.deepcopy 等价物，偏差 D-2）。
// nil 接收者返回 nil。
func (s *GraphState) Clone() *GraphState {
	if s == nil {
		return nil
	}
	c := &GraphState{Ns: s.Ns, Step: s.Step}
	if s.ChannelValues != nil {
		c.ChannelValues = make(map[string][]ChannelSnapshot, len(s.ChannelValues))
		for k, snaps := range s.ChannelValues {
			csnaps := make([]ChannelSnapshot, len(snaps))
			for i, sn := range snaps {
				csnaps[i] = cloneSnapshot(sn)
			}
			c.ChannelValues[k] = csnaps
		}
	}
	if s.PendingBuffer != nil {
		c.PendingBuffer = make([]Message, len(s.PendingBuffer))
		for i, m := range s.PendingBuffer {
			c.PendingBuffer[i] = m.Copy()
		}
	}
	if s.PendingNode != nil {
		c.PendingNode = make(map[string]PendingNode, len(s.PendingNode))
		for k, pn := range s.PendingNode {
			if pn.Exception != nil {
				exc := make([]error, len(pn.Exception))
				copy(exc, pn.Exception)
				pn.Exception = exc
			}
			c.PendingNode[k] = pn
		}
	}
	if s.NodeVersion != nil {
		c.NodeVersion = make(map[string]int, len(s.NodeVersion))
		for k, v := range s.NodeVersion {
			c.NodeVersion[k] = v
		}
	}
	return c
}

// CreateState 工厂：nil 入参兜底为空容器（对齐 Python or [] / or {} 语义）
// Python: store/base.py:71-82
func CreateState(ns string, step int, channelSnapshot map[string][]ChannelSnapshot,
	pendingBuffer []Message, pendingNode map[string]PendingNode, nodeVersion map[string]int) *GraphState {
	if pendingBuffer == nil {
		pendingBuffer = []Message{}
	}
	if pendingNode == nil {
		pendingNode = map[string]PendingNode{}
	}
	if nodeVersion == nil {
		nodeVersion = map[string]int{}
	}
	return &GraphState{
		Ns:            ns,
		Step:          step,
		ChannelValues: channelSnapshot,
		PendingBuffer: pendingBuffer,
		PendingNode:   pendingNode,
		NodeVersion:   nodeVersion,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cloneSnapshot 深拷贝单个通道快照（Python deepcopy 隐式承担，偏差 D-2 的组成部分）
func cloneSnapshot(sn ChannelSnapshot) ChannelSnapshot {
	c := ChannelSnapshot{}
	if sn.Messages != nil {
		c.Messages = make([]Message, len(sn.Messages))
		for i, m := range sn.Messages {
			c.Messages[i] = m.Copy()
		}
	}
	if sn.Received != nil {
		c.Received = make([]string, len(sn.Received))
		copy(c.Received, sn.Received)
	}
	return c
}
