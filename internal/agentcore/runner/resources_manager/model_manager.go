package resources_manager

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer/decorator"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ModelMgr 模型资源管理器，嵌入 AbstractManager 复用 provider 注册/获取/注销能力。
// GetModel 支持可选的 tracer 装饰：当 session 非 nil 时，返回装饰后的模型客户端。
//
// 对应 Python: ModelMgr (openjiuwen/core/runner/resources_manager/model_manager.py)
type ModelMgr struct {
	AbstractManager[model_clients.BaseModelClient]
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewModelMgr 创建模型资源管理器。
func NewModelMgr() ModelMgr {
	return ModelMgr{
		AbstractManager: NewAbstractManager[model_clients.BaseModelClient](),
	}
}

// AddModel 注册模型提供者。
//
// 对应 Python: ModelMgr.add_model(model_id, provider)
func (m *ModelMgr) AddModel(modelID string, provider ModelProvider) error {
	if modelID == "" {
		return exception.BuildError(exception.StatusResourceIDValueInvalid,
			exception.WithParam("resource_type", "model"),
			exception.WithParam("reason", "model id is empty"),
		)
	}
	if provider == nil {
		return exception.BuildError(exception.StatusResourceProviderInvalid,
			exception.WithParam("resource_type", "model"),
			exception.WithParam("reason", "model provider is nil"),
		)
	}

	// 将 ModelProvider 包装为 AbstractManager 所需的 func(context.Context) (T, error) 签名
	wrappedProvider := func(ctx context.Context) (model_clients.BaseModelClient, error) {
		return provider(ctx, modelID)
	}

	err := m.registerProvider(modelID, wrappedProvider)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "MODEL_ADD_ERROR").
			Str("model_id", modelID).
			Err(err).
			Msg("添加模型失败")
		return exception.BuildError(exception.StatusResourceAddError,
			exception.WithParam("card", modelID),
			exception.WithParam("reason", err.Error()),
		)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "MODEL_ADD_SUCCESS").
		Str("model_id", modelID).
		Msg("添加模型成功")
	return nil
}

// RemoveModel 注销模型提供者，返回被注销的 provider。
//
// 对应 Python: ModelMgr.remove_model(model_id)
func (m *ModelMgr) RemoveModel(modelID string) (ModelProvider, error) {
	unwrapped, err := m.unregisterProvider(modelID)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "MODEL_REMOVE_ERROR").
			Str("model_id", modelID).
			Err(err).
			Msg("移除模型失败")
		return nil, exception.BuildError(exception.StatusResourceGetError,
			exception.WithParam("resource_id", modelID),
			exception.WithParam("resource_type", "model"),
			exception.WithParam("reason", err.Error()),
		)
	}

	// 将 wrapped provider 还原为 ModelProvider
	provider := func(ctx context.Context, _ string) (model_clients.BaseModelClient, error) {
		return unwrapped(ctx)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "MODEL_REMOVE_SUCCESS").
		Str("model_id", modelID).
		Msg("移除模型成功")
	return provider, nil
}

// GetModel 获取模型实例。
// 先调用 GetResource 获取模型客户端，如果 session 非 nil 则调用 decorator.DecorateModelWithTrace 进行追踪装饰。
//
// 对应 Python: ModelMgr.get_model(model_id, session)
func (m *ModelMgr) GetModel(ctx context.Context, modelID string, session decorator.TracerSession) (model_clients.BaseModelClient, error) {
	model, err := m.getResource(ctx, modelID)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "MODEL_GET_ERROR").
			Str("model_id", modelID).
			Err(err).
			Msg("获取模型失败")
		return nil, exception.BuildError(exception.StatusResourceGetError,
			exception.WithParam("resource_id", modelID),
			exception.WithParam("resource_type", "model"),
			exception.WithParam("reason", err.Error()),
		)
	}

	// 如果 session 非 nil，进行追踪装饰
	if session != nil {
		model = decorator.DecorateModelWithTrace(model, session)
	}

	return model, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
