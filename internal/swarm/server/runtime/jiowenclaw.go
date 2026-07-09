package runtime

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// JiuWenClaw Agent 门面（stub，10.3.2）。
//
// 统一 Agent 请求入口，管理会话队列和 SDK 适配器路由。
// 当前为 stub 实现，后续替换为完整逻辑。
// 对齐 Python JiuWenClaw：jiuwenswarm/server/runtime/agent_adapter/interface.py
type JiuWenClaw struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewJiuWenClaw 创建 JiuWenClaw stub 实例。
func NewJiuWenClaw() *JiuWenClaw {
	return &JiuWenClaw{}
}

// ProcessMessage 处理非流式 Agent 请求。
// 对齐 Python JiuWenClaw.process_message(request) -> AgentResponse。
// stub：返回固定响应 {accepted: true}。
func (jw *JiuWenClaw) ProcessMessage(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true),
		schema.WithPayload(map[string]any{"accepted": true}),
	), nil
}

// ProcessMessageStream 处理流式 Agent 请求。
// 对齐 Python JiuWenClaw.process_message_stream(request) -> AsyncIterator[AgentResponseChunk]。
// stub：发送一个 stub chunk + terminal chunk，然后关闭 channel。
func (jw *JiuWenClaw) ProcessMessageStream(_ context.Context, request *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
	ch := make(chan *schema.AgentResponseChunk, 2)
	// 发送 stub 内容 chunk
	ch <- schema.NewAgentResponseChunk(request.RequestID, request.ChannelID,
		map[string]any{
			"event_type": "chat.delta",
			"content":    "[stub] Agent 处理中",
		},
		schema.WithChunkIsComplete(false),
	)
	// 发送终止标记
	ch <- schema.NewTerminalChunk(request.RequestID, request.ChannelID)
	close(ch)
	return ch, nil
}

// ProcessInterrupt 处理中断请求。
// 对齐 Python JiuWenClaw cancel_inflight_work + process_message(interrupt)。
// stub：返回 ok=true。
func (jw *JiuWenClaw) ProcessInterrupt(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true),
	), nil
}

// GetContextUsage 获取上下文使用量。
// 对齐 Python JiuWenClaw.get_context_usage(session_id)。
// stub：返回 {usage: 0, limit: 0}。
func (jw *JiuWenClaw) GetContextUsage(_ string) (map[string]any, error) {
	return map[string]any{"usage": 0, "limit": 0}, nil
}

// CompressContext 压缩上下文。
// 对齐 Python JiuWenClaw.compress_context(session_id)。
// stub：返回 {ok: true, compressed: false}。
func (jw *JiuWenClaw) CompressContext(_ string) (map[string]any, error) {
	return map[string]any{"ok": true, "compressed": false}, nil
}

// GenerateRecap 生成会话回顾。
// 对齐 Python JiuWenClaw.generate_recap(session_id)。
// stub：返回空回顾。
func (jw *JiuWenClaw) GenerateRecap(_ string) (map[string]any, error) {
	return map[string]any{"recap": ""}, nil
}

// SwitchMode 切换运行模式。
// 对齐 Python JiuWenClaw / DeepAdapter switch_mode。
// stub：直接返回 nil。
func (jw *JiuWenClaw) SwitchMode(_, _ string) error {
	return nil
}

// CreateInstance 创建 Agent 实例。
// 对齐 Python JiuWenClaw.create_instance(config, mode, sub_mode)。
// stub：直接返回 nil。
func (jw *JiuWenClaw) CreateInstance(_ map[string]any, _ string, _ string) error {
	return nil
}

// ReloadAgentConfig 重载 Agent 配置。
// 对齐 Python JiuWenClaw.reload_agent_config(config_base, env_overrides)。
// stub：直接返回 nil。
func (jw *JiuWenClaw) ReloadAgentConfig(_, _ map[string]any) error {
	return nil
}

// CancelInflightWork 取消在途任务。
// 对齐 Python JiuWenClaw.cancel_inflight_work()。
// stub：直接返回 nil。
func (jw *JiuWenClaw) CancelInflightWork() error {
	return nil
}

// Cleanup 清理资源。
// 对齐 Python JiuWenClaw.cleanup()。
// stub：直接返回 nil。
func (jw *JiuWenClaw) Cleanup() error {
	return nil
}

// GetInstance 获取底层 Agent 实例。
// 对齐 Python JiuWenClaw.get_instance()。
// stub：返回 nil。
func (jw *JiuWenClaw) GetInstance() any {
	return nil
}
