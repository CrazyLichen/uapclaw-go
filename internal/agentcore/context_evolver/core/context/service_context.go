package context

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
)

// ──────────────────────────── 接口 ────────────────────────────

// LLMService LLM 服务本地接口。
// 对齐 Python OpenAILLMWrapper.async_generate(prompt, system_prompt?, temperature?, max_tokens?)。
// P6 的 OpenAILLMWrapper 实现此接口。
type LLMService interface {
	// Generate 调用 LLM 生成文本响应。
	// 对齐 Python async_generate(prompt) → str。
	Generate(ctx context.Context, prompt string, opts ...GenerateOption) (string, error)
}

// EmbeddingService Embedding 模型本地接口。
// 对齐 Python OpenAIEmbeddingWrapper.async_embed(text)/async_embed_batch(texts)。
// P6 的 OpenAIEmbeddingWrapper 实现此接口。
type EmbeddingService interface {
	// Embed 生成单文本的向量嵌入。
	// 对齐 Python async_embed(text) → List[float]。
	Embed(ctx context.Context, text string) ([]float64, error)
	// EmbedBatch 批量生成文本的向量嵌入。
	// 对齐 Python async_embed_batch(texts) → List[List[float]]。
	EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
}

// VectorStoreService 向量存储服务本地接口。
// 对齐 Python ServiceContext.vector_store 属性返回 Optional[Any]，
// Go 改为返回本地接口以提供编译期类型安全。
// 此接口在 context 包定义，避免 context → vector_store 单向依赖，
// MemoryVectorStore 隐式实现此接口（duck typing）。
//
// Python: openjiuwen/extensions/context_evolver/core/context/service_context.py
type VectorStoreService interface {
	// Upsert 插入或更新向量节点
	Upsert(ctx context.Context, node *schema.VectorNode) error
	// Search 向量相似度搜索
	Search(ctx context.Context, embedding []float64, topK int, metadataFilter map[string]any) ([]*schema.VectorNode, error)
	// Delete 删除向量节点
	Delete(ctx context.Context, nodeID string) (bool, error)
	// Clear 清除所有向量
	Clear()
	// Count 获取向量数量
	Count() int
	// GetAll 获取所有向量节点，可选按 metadata 过滤。
	// 对齐 Python MemoryVectorStore.get_all(metadata_filter)。
	// MemoryDeduplicationOp 和 PersistMemoryOp 依赖此方法。
	GetAll(metadataFilter map[string]any) []*schema.VectorNode
}

// AgentFlowService Agent 执行服务接口。
// MaTTS 的 ParallelScalingOp 通过此接口执行 Agent 轨迹，
// 解耦 Op 与具体 Agent 实现的依赖。
type AgentFlowService interface {
	// Execute 执行一次 Agent 推理，返回轨迹结果。
	// opts 可传入 RetrievalQuery、LLMTemperature 等 AgentFlowOption。
	// 对齐 Python: invoke_inputs = {"query": ..., "retrieval_query": ..., "llm_temperature": ...}
	Execute(ctx context.Context, query string, sessionID string, opts ...AgentFlowOption) (*TrajectoryResult, error)
}

// ──────────────────────────── 结构体 ────────────────────────────

// AgentFlowConfig Agent 执行选项配置。
type AgentFlowConfig struct {
	// RetrievalQuery 记忆检索使用的查询；空字符串时默认使用 query。
	// 对齐 Python: invoke_inputs["retrieval_query"] = question
	RetrievalQuery string
	// LLMTemperature LLM 生成温度；0 表示未设置，使用模型默认值。
	// 对齐 Python: llm.temperature = self.temperature
	LLMTemperature float64
}

// AgentFlowOption Agent 执行选项函数。
type AgentFlowOption func(*AgentFlowConfig)

// WithRetrievalQuery 设置记忆检索查询。
func WithRetrievalQuery(q string) AgentFlowOption {
	return func(c *AgentFlowConfig) { c.RetrievalQuery = q }
}

// WithLLMTemperature 设置 LLM 生成温度。
func WithLLMTemperature(t float64) AgentFlowOption {
	return func(c *AgentFlowConfig) { c.LLMTemperature = t }
}

// TrajectoryResult 单次 Agent 执行的轨迹结果。
type TrajectoryResult struct {
	// Answer Agent 最终回答
	Answer string
	// Steps 执行步骤
	Steps []any
	// Success 是否成功
	Success bool
	// Trajectory 格式化后的轨迹文本
	Trajectory string
}

// GenerateConfig LLM 调用的可选配置。
type GenerateConfig struct {
	// SystemPrompt 系统提示词，空字符串表示未设置
	SystemPrompt string
	// Temperature 生成温度，0 表示未设置，使用模型默认值
	Temperature float64
	// MaxTokens 最大生成 token 数，0 表示未设置，使用模型默认值
	MaxTokens int
}

// GenerateOption LLM 调用选项函数。
type GenerateOption func(*GenerateConfig)

// ServiceContext 管理共享服务（LLM/Embedding/VectorStore/AgentFlow）的上下文。
//
// 非并发安全：需在初始化阶段完成所有 RegisterService 调用后，才能并发读取。
// Python 用 __new__ 单例模式（初始化后只读），Go 改为依赖注入——
// 由调用方构造并传入。如果 P6 的 reconfigure 需要运行时修改，届时需加锁。
//
// Python: openjiuwen/extensions/context_evolver/core/context/service_context.py
type ServiceContext struct {
	services map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewServiceContext 创建空的服务上下文。
func NewServiceContext() *ServiceContext {
	return &ServiceContext{services: make(map[string]any)}
}

// WithSystemPrompt 设置 LLM 调用的系统提示词。
func WithSystemPrompt(s string) GenerateOption {
	return func(c *GenerateConfig) { c.SystemPrompt = s }
}

// WithTemperature 设置 LLM 调用的生成温度。
func WithTemperature(f float64) GenerateOption {
	return func(c *GenerateConfig) { c.Temperature = f }
}

// WithMaxTokens 设置 LLM 调用的最大生成 token 数。
func WithMaxTokens(n int) GenerateOption {
	return func(c *GenerateConfig) { c.MaxTokens = n }
}

// RegisterService 注册服务。
// 对齐 Python ServiceContext.register_service(name, service)。
// 非并发安全，需在初始化阶段调用。
func (sc *ServiceContext) RegisterService(name string, svc any) {
	sc.services[name] = svc
}

// GetService 获取已注册的服务，不存在时返回 nil。
// 对齐 Python ServiceContext.get_service(name)。
func (sc *ServiceContext) GetService(name string) any {
	return sc.services[name]
}

// LLM 获取 LLM 服务。
// 对齐 Python ServiceContext.llm 属性。
// 未注册或类型不匹配时返回 nil。
func (sc *ServiceContext) LLM() LLMService {
	svc := sc.GetService("llm")
	if svc == nil {
		return nil
	}
	llm, ok := svc.(LLMService)
	if !ok {
		return nil
	}
	return llm
}

// EmbeddingModel 获取 Embedding 模型服务。
// 对齐 Python ServiceContext.embedding_model 属性。
// 未注册或类型不匹配时返回 nil。
func (sc *ServiceContext) EmbeddingModel() EmbeddingService {
	svc := sc.GetService("embedding_model")
	if svc == nil {
		return nil
	}
	emb, ok := svc.(EmbeddingService)
	if !ok {
		return nil
	}
	return emb
}

// VectorStore 获取 VectorStore 服务。
// 对齐 Python ServiceContext.vector_store 属性（返回 Optional[Any]）。
// Go 改为返回 VectorStoreService 本地接口，提供编译期类型安全。
// 注册时需传入实现 VectorStoreService 接口的类型（如 *MemoryVectorStore）。
func (sc *ServiceContext) VectorStore() VectorStoreService {
	svc := sc.GetService("vector_store")
	if svc == nil {
		return nil
	}
	vs, ok := svc.(VectorStoreService)
	if !ok {
		return nil
	}
	return vs
}

// AgentFlow 获取 Agent 执行服务。
// 未注册或类型不匹配时返回 nil。
func (sc *ServiceContext) AgentFlow() AgentFlowService {
	svc := sc.GetService("agent_flow")
	if svc == nil {
		return nil
	}
	af, ok := svc.(AgentFlowService)
	if !ok {
		return nil
	}
	return af
}

// RegisterAgentFlow 注册 Agent 执行服务。
func (sc *ServiceContext) RegisterAgentFlow(af AgentFlowService) {
	sc.RegisterService("agent_flow", af)
}

// Clear 清除所有已注册的服务。
// 对齐 Python ServiceContext.clear()。
func (sc *ServiceContext) Clear() {
	sc.services = make(map[string]any)
}

// String 实现 Stringer 接口。
// 对齐 Python ServiceContext.__repr__()。
func (sc *ServiceContext) String() string {
	names := make([]string, 0, len(sc.services))
	for k := range sc.services {
		names = append(names, k)
	}
	return fmt.Sprintf("ServiceContext(services=%v)", names)
}
