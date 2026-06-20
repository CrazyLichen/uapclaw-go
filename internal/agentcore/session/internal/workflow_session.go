package internal

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowSession 工作流级内部会话，实现 BaseSession 接口。
//
// 持有工作流运行所需的基础设施组件，支持延迟注入 StreamWriterManager 和 ActorManager。
// Checkpointer 委托给 parent（通常是 AgentSession），确保父子会话共享持久化机制。
//
// 对应 Python: openjiuwen/core/session/internal/workflow.py (WorkflowSession)
type WorkflowSession struct {
	// sessionID 会话唯一标识（从 parent 继承或自动生成）
	sessionID string
	// parent 父会话（通常是 AgentSession）
	parent interfaces.BaseSession
	// config 会话配置
	config interfaces.SessionConfig
	// tracer 追踪器
	// ✅ 5.11 已回填：any → *tracer.Tracer
	tracer *tracer.Tracer
	// st 状态对象（WorkflowCommitState）
	st state.SessionState
	// streamWriterManager 流写入管理器
	// ✅ 5.10 已回填：any → *stream.StreamWriterManager
	streamWriterManager *stream.StreamWriterManager
	// actorManager Actor 管理器
	// ⤵️ 后续回填：any → ActorManager
	actorManager any
	// workflowID 工作流 ID
	workflowID string
}

// NodeSession 工作流节点级会话，实现 BaseSession 接口。
//
// 包装一个 BaseSession（通常是 WorkflowSession），通过 CreateNodeState 创建节点专属的状态视图。
// 大部分方法委托给被包装的 session，但 State() 返回节点专属视图，Close() 为空实现。
//
// 对应 Python: openjiuwen/core/session/internal/workflow.py (NodeSession)
type NodeSession struct {
	// delegate 被包装的会话（通常是 WorkflowSession）
	delegate interfaces.BaseSession
	// executableID 全局唯一可执行路径 ID（parentID + "." + nodeID）
	executableID string
	// nodeID 节点 ID
	nodeID string
	// nodeType 节点类型
	nodeType string
	// parentID 父节点 executable_id
	parentID string
	// nodeState 节点专属状态视图
	nodeState state.SessionState
	// workflowID 从父 session 继承的工作流 ID
	workflowID string
	// workflowNestingDepth 从父 session 继承的工作流嵌套深度
	workflowNestingDepth int
	// mainWorkflowID 从父 session 继承的主工作流 ID
	mainWorkflowID string
	// skipTrace 是否跳过追踪
	skipTrace bool
}

// SubWorkflowSession 子工作流会话，嵌入 NodeSession。
//
// 在 NodeSession 基础上增加自己的 ActorManager 和嵌套深度管理。
// Close() 时关闭自己的 ActorManager。
//
// 对应 Python: openjiuwen/core/session/internal/workflow.py (SubWorkflowSession)
type SubWorkflowSession struct {
	// NodeSession 嵌入节点会话
	NodeSession
	// actorManager 子工作流专属 Actor 管理器
	// ⤵️ 后续回填：any → ActorManager
	actorManager any
	// workflowNestingDepth2 工作流嵌套深度（覆盖 NodeSession 的值）
	workflowNestingDepth2 int
	// workflowID2 子工作流 ID（覆盖 NodeSession 的值）
	workflowID2 string
	// mainWorkflowID2 主工作流 ID（覆盖 NodeSession 的值）
	mainWorkflowID2 string
}

// ──────────────────────────── 枚举 ────────────────────────────

// WorkflowSessionOption WorkflowSession 构造选项函数类型
type WorkflowSessionOption func(*WorkflowSession)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkflowSession 创建内部 WorkflowSession 实例。
//
// 默认行为（对齐 Python WorkflowSession.__init__）：
//   - 有 parent 时：sessionID 继承 parent、config 继承 parent、tracer 继承 parent
//   - 无 parent 时：sessionID 自动生成 UUID、config 新建默认 Config（⤵️ 5.12 回填）、tracer 为 nil
//   - state 默认创建 InMemoryWorkflowState（workflowOnly=true）
//   - streamWriterManager 和 actorManager 初始为 nil，需外部注入
func NewWorkflowSession(opts ...WorkflowSessionOption) *WorkflowSession {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_workflow_session").
		Msg("创建内部 WorkflowSession")

	s := &WorkflowSession{
		st: state.NewInMemoryWorkflowState(),
	}
	for _, opt := range opts {
		opt(s)
	}

	// 处理默认值（对齐 Python WorkflowSession.__init__）
	if s.parent == nil {
		if s.sessionID == "" {
			s.sessionID = uuid.New().String()
		}
		// Python: self._config = Config()
		if s.config == nil {
			s.config = config.NewSessionConfig(context.Background())
		}
	} else {
		if s.sessionID == "" {
			s.sessionID = s.parent.SessionID()
		}
		if s.config == nil {
			s.config = s.parent.Config()
		}
		if s.tracer == nil {
			s.tracer = s.parent.Tracer()
		}
	}

	return s
}

// WithWorkflowParent 设置父会话的选项
func WithWorkflowParent(parent interfaces.BaseSession) WorkflowSessionOption {
	return func(s *WorkflowSession) {
		s.parent = parent
	}
}

// WithWorkflowSessionID 设置会话 ID 的选项
func WithWorkflowSessionID(id string) WorkflowSessionOption {
	return func(s *WorkflowSession) {
		s.sessionID = id
	}
}

// WithWorkflowState 设置状态的选项
func WithWorkflowState(st state.SessionState) WorkflowSessionOption {
	return func(s *WorkflowSession) {
		s.st = st
	}
}

// WithWorkflowID 设置工作流 ID 的选项
func WithWorkflowID(id string) WorkflowSessionOption {
	return func(s *WorkflowSession) {
		s.workflowID = id
	}
}

// NewNodeSession 创建节点级会话实例。
//
// 从 parent session 的 state 创建节点专属状态视图（共享底层状态，切换 nodeID/parentID）。
// executableID = parentID + "." + nodeID（parentID 为空时退化为 nodeID）。
// 类型断言 WorkflowState 失败时对齐 Python AttributeError：Log Error + Panic。
func NewNodeSession(parent interfaces.BaseSession, nodeID, nodeType string, skipTrace bool) *NodeSession {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_node_session").
		Str("node_id", nodeID).
		Str("node_type", nodeType).
		Msg("创建节点级会话")

	parentID := createParentID(parent)
	executableID := createExecutableID(nodeID, parentID)

	// 类型断言获取 WorkflowState，创建节点专属状态视图
	var nodeState state.SessionState
	ws, ok := parent.State().(state.WorkflowState)
	if !ok {
		logger.Error(logger.ComponentAgentCore).
			Str("action", "create_node_session").
			Str("state_type", fmt.Sprintf("%T", parent.State())).
			Msg("当前状态不支持 CreateNodeState，对齐 Python AttributeError")
		panic(fmt.Sprintf("当前状态 %T 不支持 CreateNodeState（未实现 WorkflowState 接口），对齐 Python AttributeError", parent.State()))
	}
	nodeState = ws.CreateNodeState(executableID, parentID)

	return &NodeSession{
		delegate:             parent,
		executableID:         executableID,
		nodeID:               nodeID,
		nodeType:             nodeType,
		parentID:             parentID,
		nodeState:            nodeState,
		workflowID:           getWorkflowID(parent),
		workflowNestingDepth: getWorkflowNestingDepth(parent),
		mainWorkflowID:       getMainWorkflowID(parent),
		skipTrace:            skipTrace,
	}
}

// NewSubWorkflowSession 创建子工作流会话实例。
//
// 嵌套深度 = 传入 NodeSession 的深度 + 1。
// 构造时以传入 NodeSession 的 parent() 作为父级 session，
// 使用原 NodeSession 的 nodeID 和 nodeType。
// 类型断言 WorkflowState 失败时对齐 Python AttributeError：Log Error + Panic。
func NewSubWorkflowSession(nodeSession *NodeSession, workflowID string, actorManager any) *SubWorkflowSession {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_sub_workflow_session").
		Str("workflow_id", workflowID).
		Msg("创建子工作流会话")

	// 使用传入 NodeSession 的 parent() 作为父级
	parentSession := nodeSession.Parent()
	parentID := createParentID(parentSession)
	executableID := createExecutableID(nodeSession.NodeID(), parentID)

	// 类型断言获取 WorkflowState，创建节点专属状态视图
	var nodeState state.SessionState
	ws, ok := parentSession.State().(state.WorkflowState)
	if !ok {
		logger.Error(logger.ComponentAgentCore).
			Str("action", "create_sub_workflow_session").
			Str("state_type", fmt.Sprintf("%T", parentSession.State())).
			Msg("当前状态不支持 CreateNodeState，对齐 Python AttributeError")
		panic(fmt.Sprintf("当前状态 %T 不支持 CreateNodeState（未实现 WorkflowState 接口），对齐 Python AttributeError", parentSession.State()))
	}
	nodeState = ws.CreateNodeState(executableID, parentID)

	return &SubWorkflowSession{
		NodeSession: NodeSession{
			delegate:             parentSession,
			executableID:         executableID,
			nodeID:               nodeSession.NodeID(),
			nodeType:             nodeSession.NodeType(),
			parentID:             parentID,
			nodeState:            nodeState,
			workflowID:           workflowID,
			workflowNestingDepth: nodeSession.WorkflowNestingDepth(),
			mainWorkflowID:       nodeSession.MainWorkflowID(),
			skipTrace:            false, // SubWorkflowSession 不传递 skipTrace
		},
		actorManager:          actorManager,
		workflowNestingDepth2: nodeSession.WorkflowNestingDepth() + 1,
		workflowID2:           workflowID,
		mainWorkflowID2:       nodeSession.MainWorkflowID(),
	}
}

// Config 获取会话配置
func (s *WorkflowSession) Config() interfaces.SessionConfig {
	return s.config
}

// State 获取会话状态
func (s *WorkflowSession) State() state.SessionState {
	return s.st
}

// Tracer 获取追踪器
// ✅ 5.11 已回填：返回类型从 any 改为 *tracer.Tracer
func (s *WorkflowSession) Tracer() *tracer.Tracer {
	return s.tracer
}

// StreamWriterManager 获取流写入管理器
// ✅ 5.10 已回填：返回类型从 any 改为 *stream.StreamWriterManager
func (s *WorkflowSession) StreamWriterManager() *stream.StreamWriterManager {
	return s.streamWriterManager
}

// SessionID 获取会话唯一标识
func (s *WorkflowSession) SessionID() string {
	return s.sessionID
}

// Checkpointer 获取检查点管理器。
// 有 parent 则委托给 parent；无 parent 则从工厂获取（懒加载）。
func (s *WorkflowSession) Checkpointer() checkpointer.Checkpointer {
	if s.parent != nil {
		return s.parent.Checkpointer()
	}
	return checkpointer.GetCheckpointer()
}

// ActorManager 获取 Actor 管理器
func (s *WorkflowSession) ActorManager() any {
	return s.actorManager
}

// Close 关闭会话。如果 actorManager 不为 nil，调用其 Shutdown。
// ⤵️ 后续回填：actorManager 类型从 any → ActorManager 后调用 Shutdown()
// 待 actorManager 接口确定后回填 Shutdown 逻辑
func (s *WorkflowSession) Close() error {
	return nil
}

// SetStreamWriterManager 幂等注入流写入管理器。已设置则不覆盖。
// ✅ 5.10 已回填：参数类型从 any 改为 *stream.StreamWriterManager
func (s *WorkflowSession) SetStreamWriterManager(mgr *stream.StreamWriterManager) {
	if s.streamWriterManager == nil {
		s.streamWriterManager = mgr
	}
}

// SetTracer 设置追踪器（无幂等保护，与 Python 一致）。
// ✅ 5.11 已回填：参数类型从 any 改为 *tracer.Tracer
func (s *WorkflowSession) SetTracer(t *tracer.Tracer) {
	s.tracer = t
}

// SetActorManager 幂等注入 Actor 管理器。已设置则不覆盖。
func (s *WorkflowSession) SetActorManager(mgr any) {
	if s.actorManager == nil {
		s.actorManager = mgr
	}
}

// SetWorkflowID 设置工作流 ID
func (s *WorkflowSession) SetWorkflowID(id string) {
	s.workflowID = id
}

// WorkflowID 返回工作流 ID
func (s *WorkflowSession) WorkflowID() string {
	return s.workflowID
}

// MainWorkflowID 返回主工作流 ID（直接返回 WorkflowID）
func (s *WorkflowSession) MainWorkflowID() string {
	return s.workflowID
}

// WorkflowNestingDepth 返回工作流嵌套深度（固定返回 0）
func (s *WorkflowSession) WorkflowNestingDepth() int {
	return 0
}

// Parent 返回父会话
func (s *WorkflowSession) Parent() interfaces.BaseSession {
	return s.parent
}

// NodeID 返回节点 ID
func (n *NodeSession) NodeID() string {
	return n.nodeID
}

// NodeType 返回节点类型
func (n *NodeSession) NodeType() string {
	return n.nodeType
}

// ExecutableID 返回全局唯一可执行路径 ID
func (n *NodeSession) ExecutableID() string {
	return n.executableID
}

// ParentID 返回父节点 executable_id
func (n *NodeSession) ParentID() string {
	return n.parentID
}

// WorkflowID 返回工作流 ID
func (n *NodeSession) WorkflowID() string {
	return n.workflowID
}

// MainWorkflowID 返回主工作流 ID
func (n *NodeSession) MainWorkflowID() string {
	return n.mainWorkflowID
}

// WorkflowNestingDepth 返回工作流嵌套深度
func (n *NodeSession) WorkflowNestingDepth() int {
	return n.workflowNestingDepth
}

// SkipTrace 返回是否跳过追踪
func (n *NodeSession) SkipTrace() bool {
	return n.skipTrace
}

// Parent 返回父 session 引用
func (n *NodeSession) Parent() interfaces.BaseSession {
	return n.delegate
}

// NodeConfig 获取节点级配置。
// 对应 Python: NodeSession.node_config() → config.get_workflow_config(workflow_id).spec.comp_configs.get(node_id)
func (n *NodeSession) NodeConfig() any {
	cfg := n.delegate.Config()
	if cfg == nil {
		return nil
	}
	wfc := cfg.GetWorkflowConfig(n.workflowID)
	if wfc == nil {
		return nil
	}
	// ⤵️ 8.15 回填：WorkflowConfig 实现后，从 spec.comp_configs 获取 nodeID 对应的配置
	return nil
}

// Config 委托给父 session
func (n *NodeSession) Config() interfaces.SessionConfig {
	return n.delegate.Config()
}

// State 返回节点专属状态视图
func (n *NodeSession) State() state.SessionState {
	return n.nodeState
}

// Tracer 委托给父 session
// ✅ 5.11 已回填：返回类型从 any 改为 *tracer.Tracer
func (n *NodeSession) Tracer() *tracer.Tracer {
	return n.delegate.Tracer()
}

// StreamWriterManager 委托给父 session
// ✅ 5.10 已回填：返回类型从 any 改为 *stream.StreamWriterManager
func (n *NodeSession) StreamWriterManager() *stream.StreamWriterManager {
	return n.delegate.StreamWriterManager()
}

// SessionID 委托给父 session
func (n *NodeSession) SessionID() string {
	return n.delegate.SessionID()
}

// Checkpointer 委托给父 session
func (n *NodeSession) Checkpointer() checkpointer.Checkpointer {
	return n.delegate.Checkpointer()
}

// ActorManager 委托给父 session
func (n *NodeSession) ActorManager() any {
	return n.delegate.ActorManager()
}

// Close 空实现，节点不拥有生命周期
func (n *NodeSession) Close() error {
	return nil
}

// WorkflowID 返回子工作流 ID（覆写 NodeSession）
func (s *SubWorkflowSession) WorkflowID() string {
	return s.workflowID2
}

// MainWorkflowID 返回主工作流 ID（覆写 NodeSession）
func (s *SubWorkflowSession) MainWorkflowID() string {
	return s.mainWorkflowID2
}

// WorkflowNestingDepth 返回工作流嵌套深度（覆写 NodeSession）
func (s *SubWorkflowSession) WorkflowNestingDepth() int {
	return s.workflowNestingDepth2
}

// ActorManager 返回自己的 ActorManager（覆写 NodeSession 的委托）
func (s *SubWorkflowSession) ActorManager() any {
	return s.actorManager
}

// SetActorManager 幂等注入 Actor 管理器。已设置则不覆盖。
func (s *SubWorkflowSession) SetActorManager(mgr any) {
	if s.actorManager == nil {
		s.actorManager = mgr
	}
}

// Close 关闭子工作流会话。如果 actorManager 不为 nil，调用其 Shutdown。
// ⤵️ 后续回填：actorManager 类型从 any → ActorManager 后调用 Shutdown()
// 待 actorManager 接口确定后回填 Shutdown 逻辑
func (s *SubWorkflowSession) Close() error {
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createParentID 计算父节点 ID。
// 如果 session 是 NodeSession，返回其 executable_id；否则返回空字符串。
func createParentID(s interfaces.BaseSession) string {
	if ns, ok := s.(*NodeSession); ok {
		return ns.ExecutableID()
	}
	return ""
}

// createExecutableID 计算全局唯一可执行路径 ID。
// parentID 非空时返回 parentID.nodeID；否则返回 nodeID。
func createExecutableID(nodeID, parentID string) string {
	if parentID != "" {
		return parentID + "." + nodeID
	}
	return nodeID
}

// getWorkflowID 从 BaseSession 获取 WorkflowID。
// 如果 session 有 WorkflowID 方法则调用，否则返回空字符串。
func getWorkflowID(s interfaces.BaseSession) string {
	if ws, ok := s.(interface{ WorkflowID() string }); ok {
		return ws.WorkflowID()
	}
	return ""
}

// getWorkflowNestingDepth 从 BaseSession 获取嵌套深度。
func getWorkflowNestingDepth(s interfaces.BaseSession) int {
	if ws, ok := s.(interface{ WorkflowNestingDepth() int }); ok {
		return ws.WorkflowNestingDepth()
	}
	return 0
}

// getMainWorkflowID 从 BaseSession 获取主工作流 ID。
func getMainWorkflowID(s interfaces.BaseSession) string {
	if ws, ok := s.(interface{ MainWorkflowID() string }); ok {
		return ws.MainWorkflowID()
	}
	return ""
}
