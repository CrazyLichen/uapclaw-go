package store

import (
	"encoding/json"
	"errors"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PendingNode 未完成节点的持久化记录
// Python: store/base.py:26-29
type PendingNode struct {
	// NodeName 节点名
	NodeName string `json:"node_name"`
	// Status 任务状态（取值对齐 8.3d 的 TaskStatusInterrupt/TaskStatusError 字面量）
	Status string `json:"status"`
	// Exception 累积的异常列表（Python 默认 None，Go nil 等价；JSON 往返经 string 适配，偏差 D-8'）
	Exception []error `json:"exception,omitempty"`
}

// pendingNodeJSON PendingNode 的 JSON 形态（Exception 经 string 适配，偏差 D-8'）
type pendingNodeJSON struct {
	NodeName  string   `json:"node_name"`
	Status    string   `json:"status"`
	Exception []string `json:"exception"`
}

// GraphState 引擎检查点状态快照。
// ⚠️ 与 8.8 graph_state.GraphState（节点返回值聚合）同名不同义，Go 靠包隔离。
// Python: store/base.py:32-39
type GraphState struct {
	// Ns 命名空间
	Ns string
	// Step super-step 序号
	Step int
	// ChannelValues 通道快照（Python Dict[str, Any]，按计划行收窄，偏差 D-5）
	ChannelValues map[string][]pregel.ChannelSnapshot
	// PendingBuffer 待发消息
	PendingBuffer []pregel.Message
	// PendingNode 未完成节点表
	PendingNode map[string]PendingNode
	// NodeVersion 节点版本
	NodeVersion map[string]int
}

// Store 图状态存储抽象
// Python: store/base.py:59-68（async 三方法，Go 同步化见偏差 D-4）
type Store interface {
	// Get 读取状态，未命中返回 (nil, nil)
	Get(sessionID, ns string) (*GraphState, error)
	// Save 保存状态
	Save(sessionID, ns string, state *GraphState) error
	// Delete 删除状态；ns 为空串时删整个 session（Python Optional[str]=None，偏差 D-3）
	Delete(sessionID, ns string) error
}

// GraphStore 日志壳：包装 Store 并按 Python 逐条复刻日志与异常透传。
// Python: store/base.py:80-159
type GraphStore struct {
	// saver 被包装的真实存储
	saver Store
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件常量（规则 3）
const logComponent = logger.ComponentAgentCore

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
		c.ChannelValues = make(map[string][]pregel.ChannelSnapshot, len(s.ChannelValues))
		for k, snaps := range s.ChannelValues {
			csnaps := make([]pregel.ChannelSnapshot, len(snaps))
			for i, sn := range snaps {
				csnaps[i] = cloneSnapshot(sn)
			}
			c.ChannelValues[k] = csnaps
		}
	}
	if s.PendingBuffer != nil {
		c.PendingBuffer = make([]pregel.Message, len(s.PendingBuffer))
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

// MarshalJSON Exception []error → []string（Go error 接口 JSON 序列化静默丢内容，D-8）
func (p PendingNode) MarshalJSON() ([]byte, error) {
	aux := pendingNodeJSON{NodeName: p.NodeName, Status: p.Status}
	for _, e := range p.Exception {
		if e != nil {
			aux.Exception = append(aux.Exception, e.Error())
		}
	}
	return json.Marshal(aux)
}

// UnmarshalJSON []string → errors.New 重建；null → nil（对齐 Python exception 默认 None）
func (p *PendingNode) UnmarshalJSON(data []byte) error {
	var aux pendingNodeJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	p.NodeName, p.Status = aux.NodeName, aux.Status
	if aux.Exception == nil {
		p.Exception = nil
		return nil
	}
	p.Exception = make([]error, len(aux.Exception))
	for i, s := range aux.Exception {
		p.Exception[i] = errors.New(s)
	}
	return nil
}

// CreateState 工厂：nil 入参兜底为空容器（对齐 Python or [] / or {} 语义）
// Python: store/base.py:71-82
func CreateState(ns string, step int, channelSnapshot map[string][]pregel.ChannelSnapshot,
	pendingBuffer []pregel.Message, pendingNode map[string]PendingNode, nodeVersion map[string]int) *GraphState {
	if pendingBuffer == nil {
		pendingBuffer = []pregel.Message{}
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

// NewGraphStore 构造日志壳
// Python: store/base.py:81-83（__init__）
func NewGraphStore(saver Store) *GraphStore {
	return &GraphStore{saver: saver}
}

// Get 读取图状态：未命中记 debug、异常 error 后原样透传。
// Python: store/base.py:85-103
func (g *GraphStore) Get(sessionID, ns string) (*GraphState, error) {
	state, err := g.saver.Get(sessionID, ns)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "GRAPH_STORE_GET").
			Str("session_id", sessionID).
			Str("graph_id", ns).
			Err(err).
			Msg("Failed to get graph state")
		return nil, err
	}
	if state == nil {
		logger.Debug(logComponent).
			Str("event_type", "GRAPH_STORE_GET").
			Str("session_id", sessionID).
			Str("graph_id", ns).
			Msg("Not found graph state for session")
	}
	return state, nil
}

// Save 保存图状态：前后 debug（含 step）、异常 error 后原样透传。
// Python: store/base.py:105-130
func (g *GraphStore) Save(sessionID, ns string, state *GraphState) error {
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_STORE_SAVE").
		Str("session_id", sessionID).
		Int("step", state.Step).
		Str("graph_id", ns).
		Msg("Begin to save graph state of super-step")
	if err := g.saver.Save(sessionID, ns, state); err != nil {
		logger.Error(logComponent).
			Str("event_type", "GRAPH_STORE_SAVE").
			Str("session_id", sessionID).
			Int("step", state.Step).
			Str("graph_id", ns).
			Err(err).
			Msg("Failed to save graph state of super-step")
		return err
	}
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_STORE_SAVE").
		Str("session_id", sessionID).
		Int("step", state.Step).
		Str("graph_id", ns).
		Msg("Succeed to save graph state of super-step")
	return nil
}

// Delete 删除图状态：ns 空串打 all、前后 debug、异常走 debug 级（Python 原文即 logger.debug + re-raise，照抄）后透传。
// Python: store/base.py:132-159
func (g *GraphStore) Delete(sessionID, ns string) error {
	target := ns
	if target == "" {
		target = "all"
	}
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_STORE_DELETE").
		Str("session_id", sessionID).
		Str("graph_id", ns).
		Msg("Begin to delete " + target + " graph states for session")
	if err := g.saver.Delete(sessionID, ns); err != nil {
		logger.Debug(logComponent).
			Str("event_type", "GRAPH_STORE_DELETE").
			Str("session_id", sessionID).
			Str("graph_id", ns).
			Err(err).
			Msg("Failed delete " + target + " graph states for session")
		return err
	}
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_STORE_DELETE").
		Str("session_id", sessionID).
		Str("graph_id", ns).
		Msg("Succeed to delete " + target + " graph states for session")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cloneSnapshot 深拷贝单个通道快照（Python deepcopy 隐式承担，偏差 D-2 的组成部分）
func cloneSnapshot(sn pregel.ChannelSnapshot) pregel.ChannelSnapshot {
	c := pregel.ChannelSnapshot{}
	if sn.Messages != nil {
		c.Messages = make([]pregel.Message, len(sn.Messages))
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
