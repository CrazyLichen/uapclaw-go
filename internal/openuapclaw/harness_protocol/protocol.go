package harness_protocol

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// HarnessProtocol 第三方 agent 必须满足的并发安全 harness 行为契约。
//
// Events 与 TurnEvents 是同一逻辑单消费者观测通道的两种视图；
// 实现必须自行串行化状态转换，调用方可从不同协程发起命令。
// Python: protocol.py:28-126 HarnessProtocol
type HarnessProtocol interface {
	// Card 返回静态实现身份与已声明能力
	Card() HarnessCard
	// State 返回当前高层生命周期相位
	State() HarnessState
	// ProviderSessionID 返回 provider 原生会话标识（尚不可用时为 nil）
	ProviderSessionID() *string
	// EventBufferConfig 返回观测使用的有界背压策略
	EventBufferConfig() EventBufferConfig
	// Start 校验宿主兼容性、开启一次周期并稳定在 idle
	Start(ctx context.Context, harnessContext HarnessContext) error
	// Stop 停止周期、关闭事件并稳定在 terminated（幂等）
	Stop(ctx context.Context) error
	// Events 返回周期级有序观测流；已有活跃迭代器时须返回 HarnessStateError
	Events() HarnessEventCursor
	// TurnEvents 返回单轮有限观测流；turnID 为 nil 表示下一轮未消费的轮次
	TurnEvents(turnID *string) HarnessEventCursor
	// Send 接受输入并返回其 message→turn 关联；不等待该轮结束
	Send(ctx context.Context, content HarnessInput, mode DeliveryMode) (SendReceipt, error)
	// Abort 按已声明能力中止在飞轮次
	Abort(ctx context.Context, mode AbortMode) error
	// Pause 在支持 pause_resume 时暂停在飞轮次
	Pause(ctx context.Context) error
	// Resume 恢复热的或被 checkpoint 恢复的暂停轮次
	Resume(ctx context.Context, query *HarnessInput) error
	// ExportCheckpoint 返回本 agent 最新的带版本 provider 快照
	ExportCheckpoint(ctx context.Context) (*HarnessCheckpoint, error)
}

// HarnessModelControl harness 可与 HarnessProtocol 并列实现的可选模型控制面。
// 宿主调用前须检查 card.Supports(MODEL_DISCOVERY / MODEL_SELECTION)；
// 未声明该能力的实现须返回 UnsupportedHarnessCapabilityError。
// Python: protocol.py:129-156 HarnessModelControl
type HarnessModelControl interface {
	// ListModels 探测 provider 当前提供的模型（已启动或未启动均可）
	ListModels(ctx context.Context) ([]ModelOption, error)
	// SetModel 切换后续轮次的模型与推理强度
	SetModel(ctx context.Context, selection ModelSelection) error
}

// HarnessProvider 发现并构造第三方 harness 的工厂 SPI。
// Python: protocol.py:159-170 HarnessProvider
type HarnessProvider interface {
	// Card 返回本 provider 所创建 harness 的元数据
	Card() HarnessCard
	// Create 校验 provider 配置并创建一个未启动的 harness
	Create(config JsonObject) (HarnessProtocol, error)
}

// ModelConfigProvider 把宿主模型选择映射到 provider 配置的可选钩子。
// HarnessProvider 可与 Create 并列实现它，宿主因此无需知道 provider 的配置字段。
// 带 Endpoint 的选择要求 card 声明 ENDPOINT_MODELS。
// Python: protocol.py:173-197 ModelConfigProvider
type ModelConfigProvider interface {
	// ModelConfig 返回运行该选择的配置片段
	ModelConfig(choice ModelChoice, fallback *ModelChoice) (JsonObject, error)
}
