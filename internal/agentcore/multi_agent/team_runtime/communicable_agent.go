package team_runtime

import (
	"context"
	"fmt"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CommunicableAgent 可通信 Agent 实现，嵌入 Agent 结构体获得 P2P/Pub-Sub 通信能力。
//
// Agent 通过组合此结构体，在 BindRuntime 被调用后即可使用 Send/Publish/Subscribe/Unsubscribe。
// 外部通过类型断言 agent.(Communicable) 获取通信接口。
//
// 对应 Python: CommunicableAgent (openjiuwen/core/multi_agent/team_runtime/communicable_agent.py)
type CommunicableAgent struct {
	// runtime 团队运行时引用，BindRuntime 注入
	runtime *TeamRuntime
	// agentID Agent 标识，BindRuntime 注入
	agentID string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// errRuntimeNotBound 运行时未绑定错误
var errRuntimeNotBound = fmt.Errorf("运行时未绑定，请先调用 BindRuntime")

// 编译时验证 CommunicableAgent 满足 Communicable 接口
var _ Communicable = (*CommunicableAgent)(nil)

// 编译时验证 CommunicableAgent 满足 RuntimeBindable 接口
var _ RuntimeBindable = (*CommunicableAgent)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCommunicableAgent 创建可通信 Agent 实例。
func NewCommunicableAgent() *CommunicableAgent {
	return &CommunicableAgent{}
}

// IsBound 判断是否已绑定运行时。
//
// 对应 Python: CommunicableAgent.is_bound 属性
func (c *CommunicableAgent) IsBound() bool {
	return c.runtime != nil && c.agentID != ""
}

// BindRuntime 绑定团队运行时，注入运行时引用和 Agent 标识。
// 实现 RuntimeBindable 接口。
//
// 幂等性：相同 runtime 和 agentID 时静默跳过。
// 重绑定：已绑定到不同 runtime 或 agentID 时记录 warning 日志。
//
// 注意：此方法假设在 Agent 创建时单次调用，无并发场景。
// 若未来需要并发绑定，应添加 sync.RWMutex 保护 runtime/agentID 字段。
//
// 对应 Python: CommunicableAgent.bind_runtime(runtime, agent_id)
func (c *CommunicableAgent) BindRuntime(runtime *TeamRuntime, agentID string) {
	if agentID == "" {
		panic("BindRuntime: agentID 不能为空字符串，agent 标识必须非空")
	}
	if c.IsBound() {
		if c.runtime == runtime && c.agentID == agentID {
			// 相同 runtime 和 agentID — 幂等，静默跳过
			return
		}
		// 不同 runtime 或 agentID — 重绑定，记录 warning
		logger.Warn(logComponent).
			Str("event_type", "RUNTIME_REBIND").
			Str("class_name", "CommunicableAgent").
			Str("agent_id", c.agentID).
			Msg("Agent 已绑定到运行时，重新绑定可能导致意外行为")
	}
	c.runtime = runtime
	c.agentID = agentID
}

// Send P2P 发送消息到指定接收者，等待响应。
// 实现 Communicable 接口。
//
// 对应 Python: CommunicableAgent.send(message, recipient, opts)
func (c *CommunicableAgent) Send(ctx context.Context, message map[string]any, recipient string, opts ...maschema.TeamOption) (any, error) {
	if c.runtime == nil {
		return nil, errRuntimeNotBound
	}
	return c.runtime.Send(ctx, message, recipient, c.agentID, opts...)
}

// Publish Pub-Sub 发布消息到指定主题，发后即忘。
// 实现 Communicable 接口。
//
// 对应 Python: CommunicableAgent.publish(message, topic_id, opts)
func (c *CommunicableAgent) Publish(ctx context.Context, message map[string]any, topicID string, opts ...maschema.TeamOption) error {
	if c.runtime == nil {
		return errRuntimeNotBound
	}
	return c.runtime.Publish(ctx, message, topicID, c.agentID, opts...)
}

// Subscribe 订阅主题。
// 实现 Communicable 接口。
//
// 对应 Python: CommunicableAgent.subscribe(topic)
func (c *CommunicableAgent) Subscribe(ctx context.Context, topic string) error {
	if c.runtime == nil {
		return errRuntimeNotBound
	}
	return c.runtime.Subscribe(ctx, c.agentID, topic)
}

// Unsubscribe 取消订阅主题。
// 实现 Communicable 接口。
//
// 对应 Python: CommunicableAgent.unsubscribe(topic)
func (c *CommunicableAgent) Unsubscribe(ctx context.Context, topic string) error {
	if c.runtime == nil {
		return errRuntimeNotBound
	}
	return c.runtime.Unsubscribe(ctx, c.agentID, topic)
}

// Runtime 返回绑定的团队运行时引用。
func (c *CommunicableAgent) Runtime() *TeamRuntime {
	return c.runtime
}
