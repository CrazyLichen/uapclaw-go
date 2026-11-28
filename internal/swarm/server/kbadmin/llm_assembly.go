package kbadmin

// llm_assembly 30.3 caption 视觉 LLM 装配。
// 复用平台既有默认模型装配路径（与 sweeper/evolution_rails 同款：
// config.GetDefaultModels → ModelClientConfig 反序列化 → llm.NewModel），
// 不新造装配面。无可用模型 → (nil, nil)：captioner 禁用态软失败，
// 解析不阻塞（spec §6 拍板）。

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// resolveVisionLLM 从平台默认模型配置解析视觉 LLM 客户端（caption 用）。
// 装配失败 Warn 日志 + 返回 nil（captioner 禁用态），不阻塞解析。
func resolveVisionLLM() *llm.Model {
	// Python: from jiuwenswarm.common.config import get_default_models
	entries := config.GetDefaultModels(nil)
	if len(entries) == 0 {
		logger.Warn(logComponent).Msg("no default models configured, image caption disabled")
		return nil
	}

	entry := entries[0]
	// Python: mcc = entry.get("model_client_config", {})
	mcc, _ := entry["model_client_config"].(map[string]any)
	if mcc == nil {
		mcc = map[string]any{}
	}
	// Python: model_name = mcc.get("model_name", "")
	modelName, _ := mcc["model_name"].(string)

	// ModelClientConfig 自定义 UnmarshalJSON，支持 known/extra 字段拆分
	var clientConfig llmschema.ModelClientConfig
	if mccData, err := json.Marshal(mcc); err == nil {
		if err := json.Unmarshal(mccData, &clientConfig); err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("ModelClientConfig deserialization failed, image caption disabled")
			return nil
		}
	}
	// 回退默认值（对齐 sweeper 装配先例）
	if clientConfig.ClientProvider == "" {
		clientConfig.ClientProvider = "OpenAI"
	}
	if clientConfig.Timeout <= 0 {
		clientConfig.Timeout = 60.0
	}
	if clientConfig.MaxRetries <= 0 {
		clientConfig.MaxRetries = 3
	}
	if clientConfig.ClientID == "" {
		clientConfig.ClientID = uuid.New().String()
	}

	modelConfig := llmschema.NewModelRequestConfig(
		llmschema.WithModelName(modelName),
	)
	model, err := llm.NewModel(&clientConfig, modelConfig)
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("model_name", modelName).
			Msg("vision LLM assembly failed, image caption disabled")
		return nil
	}
	return model
}
