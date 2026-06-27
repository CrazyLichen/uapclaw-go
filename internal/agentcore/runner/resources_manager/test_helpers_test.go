package resources_manager

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer/decorator"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// stubBaseAgent 用于测试的 BaseAgent 桩实现
type stubBaseAgent struct{}

// stubBaseModelClient 用于测试的 BaseModelClient 桩实现
type stubBaseModelClient struct{}

// stubWorkflow 用于测试的 Workflow 桩实现
type stubWorkflow struct {
	// card 工作流卡片
	card *schema.WorkflowCard
}

// stubTracerSession 用于测试的 TracerSession 桩实现
type stubTracerSession struct {
	// tracerVal 追踪器
	tracerVal *tracer.Tracer
	// spanVal 追踪跨度
	spanVal *tracer.TraceAgentSpan
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// --- stubBaseAgent 实现 interfaces.BaseAgent ---

func (s *stubBaseAgent) Configure(_ context.Context, _ sainterfaces.AgentConfig) error {
	return nil
}

func (s *stubBaseAgent) Invoke(_ context.Context, _ map[string]any, _ ...sainterfaces.AgentOption) (any, error) {
	return nil, nil
}

func (s *stubBaseAgent) Stream(_ context.Context, _ map[string]any, _ ...sainterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}

func (s *stubBaseAgent) Card() *agentschema.AgentCard {
	return &agentschema.AgentCard{}
}

func (s *stubBaseAgent) Config() sainterfaces.AgentConfig {
	return nil
}

func (s *stubBaseAgent) AbilityManager() any {
	return nil
}

func (s *stubBaseAgent) CallbackManager() *rail.AgentCallbackManager {
	return nil
}

func (s *stubBaseAgent) RegisterCallback(_ context.Context, _ any, _ any, _ ...callback.CallbackOption) error {
	return nil
}

func (s *stubBaseAgent) RegisterRail(_ context.Context, _ rail.AgentRail, _ ...callback.CallbackOption) error {
	return nil
}

func (s *stubBaseAgent) UnregisterRail(_ context.Context, _ rail.AgentRail) error {
	return nil
}

// --- stubBaseModelClient 实现 model_clients.BaseModelClient ---

func (s *stubBaseModelClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return &llmschema.AssistantMessage{}, nil
}

func (s *stubBaseModelClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, nil
}

func (s *stubBaseModelClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, nil
}

func (s *stubBaseModelClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, nil
}

func (s *stubBaseModelClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, nil
}

func (s *stubBaseModelClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

func (s *stubBaseModelClient) SupportsKVCacheRelease() bool {
	return false
}

// --- stubWorkflow 实现 interfaces.Workflow ---

func (s *stubWorkflow) Invoke(_ context.Context, _ map[string]any, _ ...sainterfaces.WorkflowOption) (any, error) {
	return nil, nil
}

func (s *stubWorkflow) Stream(_ context.Context, _ map[string]any, _ ...sainterfaces.WorkflowOption) (<-chan stream.Schema, error) {
	return nil, nil
}

func (s *stubWorkflow) Card() *schema.WorkflowCard {
	return s.card
}

// --- stubTracerSession 实现 decorator.TracerSession ---

func (s *stubTracerSession) Tracer() *tracer.Tracer {
	return s.tracerVal
}

func (s *stubTracerSession) AgentSpan() *tracer.TraceAgentSpan {
	return s.spanVal
}

// 确保 stubTracerSession 满足 decorator.TracerSession 接口
var _ decorator.TracerSession = (*stubTracerSession)(nil)
