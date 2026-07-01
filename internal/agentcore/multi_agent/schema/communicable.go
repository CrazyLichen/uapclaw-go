package schema

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// Communicable 可通信接口，Agent 实现此接口即可使用 P2P/Pub-Sub 通信。
//
// 对应 Python: CommunicableAgent 的 send/publish/subscribe/unsubscribe 方法集。
// Agent 通过嵌入 team_runtime.CommunicableAgent 结构体获得此接口的默认实现，
// 外部需要通信方法时，通过类型断言 agent.(Communicable) 获取。
type Communicable interface {
	// Send P2P 发送消息到指定接收者，等待响应。
	Send(ctx context.Context, message any, recipient string, opts ...TeamOption) (any, error)
	// Publish Pub-Sub 发布消息到指定主题，发后即忘。
	Publish(ctx context.Context, message any, topicID string, opts ...TeamOption) error
	// Subscribe 订阅主题。
	Subscribe(ctx context.Context, topic string) error
	// Unsubscribe 取消订阅主题。
	Unsubscribe(ctx context.Context, topic string) error
}
