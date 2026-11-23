// Package server 配置面板模型读写三方法（27.4）。
// 对齐 Python common/config_panel/models_handlers.py 的 models_list_handler /
// config_validate_model_handler / models_replace_all_handler（含
// build_models_defaults_from_frontend 与 _merge_models_for_replace_all）。
package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	model_clients "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llmSchema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	common "github.com/uapclaw/uapclaw-go/internal/swarm/common"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/control/configpanel"
	rt "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
)

// logComponentModelsPanel 模型面板日志组件。
const logComponentModelsPanel = logger.ComponentAgentServer

// loggerWarn 简化 Warn。
func loggerWarn(msg string, err error) {
	logger.Warn(logComponentModelsPanel).Err(err).Msg(msg)
}

// llmSchemaNewModel NewModel 别名（对齐 Python build_probe_model_client_config 后的探活装配）。
func llmSchemaNewModel(clientConfig *llmSchema.ModelClientConfig, modelConfig *llmSchema.ModelRequestConfig) (llmModel, error) {
	return llm.NewModel(clientConfig, modelConfig)
}

// llmModel 探活模型实例接口面（llm.NewModel 返回类型的窄化别名；
// MessagesParam 为包内具体类型，直接取 model_clients.Invoke 签名形状）。
type llmModel interface {
	Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmSchema.AssistantMessage, error)
}

// configMapForCatalog 取当前配置表（serverCatalog 同源）。
func configMapForCatalog(s *AgentServer) map[string]any {
	data, err := s.config.Load()
	if err != nil {
		return map[string]any{}
	}
	return data
}

// rtReloadOptionsModel models.defaults 重载选项（Python _models_reload_options(["models.defaults"])）。
func rtReloadOptionsModel() rt.ReloadOptions {
	return rt.ReloadOptions{ReloadScopes: []string{"model"}}
}

// orString any→string（nil 回空）。
func orString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// orAny 首选非空（Python mcc.get("vendor_key") or entry.get("vendor_key")）。
func orAny(primary, fallback any) any {
	if primary == nil || primary == "" {
		return fallback
	}
	return primary
}

// boolOr any→bool。
func boolOr(v any) bool {
	b, _ := v.(bool)
	return b
}

// toStringOr any→string 带默认。
func toStringOr(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

// HandleModelsList 处理 models.list（Python: models_handlers.py:803
// models_list_handler）。defaults 序列化 + zen 追加 + 模型组 + active_model。
func (s *AgentServer) HandleModelsList(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	_ = requestParamsMap(request)
	cfg := configMapForCatalog(s)
	models := config.GetDefaultModels(cfg)
	result := make([]map[string]any, 0, len(models))
	for idx, entry := range models {
		// Python: 非 dict 条目跳过以保持 origin_index 对齐（:840-845 脏配置防御）
		mcc, _ := entry["model_client_config"].(map[string]any)
		mco, _ := entry["model_config_obj"].(map[string]any)
		if mcc == nil {
			mcc = map[string]any{}
		}
		if mco == nil {
			mco = map[string]any{}
		}
		isDefault, _ := entry["is_default"].(bool)
		modelName := strings.TrimSpace(toStringOr(mcc["model_name"], ""))
		resultEntry := map[string]any{
			"model_id":         orString(entry["model_id"]),
			"model_name":       modelName,
			"api_base":         orString(mcc["api_base"]),
			"api_key":          orString(mcc["api_key"]),
			"has_api_key":      orString(mcc["api_key"]) != "",
			"model_provider":   orString(mcc["client_provider"]),
			"temperature":      mco["temperature"],
			"reasoning_level":  configpanel.ReasoningLevelDisplay(mco["reasoning_level"]),
			"is_default":       isDefault,
			"is_agentos":       orString(mco["_source"]) == "agentos",
			"is_free":          boolOr(entry["is_free"]),
			"alias":            orString(entry["alias"]),
			"origin_index":     idx,
			"vendor_key":       orString(orAny(mcc["vendor_key"], entry["vendor_key"])),
			"plan":             orString(orAny(mcc["plan"], entry["plan"])),
			"endpoint_profile": orString(mcc["endpoint_profile"]),
		}
		if modelName != "" {
			cw := configpanel.ParsePositiveInt(mco["context_window"])
			if cw == 0 {
				cw = configpanel.DefaultContextWindowTokens
			}
			resultEntry["context_window_tokens"] = cw
		}
		if configpanel.IsLoginModel(entry) {
			resultEntry["source"] = entry["source"]
			resultEntry["read_only"] = true
		}
		result = append(result, resultEntry)
	}
	// Python: zen 追加（:869-878）——跳过同名，is_default 保持调用方给的值
	existing := map[string]bool{}
	for _, item := range result {
		existing[orString(item["model_name"])] = true
	}
	for _, row := range configpanel.AppendZenFreeModels(nil) {
		name := strings.TrimSpace(orString(row["model_name"]))
		if name == "" || existing[name] {
			continue
		}
		result = append(result, row)
		existing[name] = true
	}
	activeModel := ""
	if len(result) > 0 {
		activeModel = orString(result[0]["model_name"])
	}
	catalog := common.NewModelCatalog(cfg)
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"models":       result,
			"model_groups": catalog.ListPublicGroups(),
			"active_model": activeModel,
		})), nil
}

// HandleModelsValidate 处理 models.validate / config.validate_model
// （Python: models_handlers.py:667 config_validate_model_handler：草稿字段
// 发最小补全 "Hi"，max_tokens 3 失败回退 16）。
func (s *AgentServer) HandleModelsValidate(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := requestParamsMap(request)
	infimum, supremum := 3, 16
	apiBase := strings.TrimSpace(orString(params["api_base"]))
	apiKey := strings.TrimSpace(orString(params["api_key"]))
	model := strings.TrimSpace(orString(params["model"]))
	modelProvider := strings.TrimSpace(orString(params["model_provider"]))
	needsAPIKey := modelProvider != "OpenAIAccount"
	if apiBase == "" || model == "" || modelProvider == "" || (needsAPIKey && apiKey == "") {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": "api_base, model, model_provider, and api_key for non-OAuth providers are required",
				"code":  "BAD_REQUEST",
			})), nil
	}
	if !configpanel.IsAvailableProvider(modelProvider) {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": fmt.Sprintf("Model provider must be one of: %v", configpanel.AvailableModelProviders()),
				"code":  "BAD_REQUEST",
			})), nil
	}
	apiBase = strings.TrimRight(apiBase, "/")
	verifySSL := false
	if v, ok := params["verify_ssl"].(bool); ok {
		verifySSL = v
	}
	clientConfig := &llmSchema.ModelClientConfig{
		ClientProvider: modelProvider,
		APIKey:         apiKey,
		APIBase:        apiBase,
		Timeout:        60.0,
		MaxRetries:     3,
		VerifySSL:      verifySSL,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	invoke := func(maxTokens int) (string, error) {
		modelConfig := llmSchema.NewModelRequestConfig()
		modelConfig.ModelName = model
		modelConfig.MaxTokens = &maxTokens
		zero := 0.0
		modelConfig.Temperature = &zero
		modelInst, err := llmSchemaNewModel(clientConfig, modelConfig)
		if err != nil {
			return "", err
		}
		messages := model_clients.NewMessagesParam(llmSchema.NewUserMessage("Hi"))
		resp, err := modelInst.Invoke(ctx, messages)
		if err != nil {
			return "", err
		}
		if resp == nil || resp.Content.Text() == "" {
			return "", fmt.Errorf("模型返回空响应")
		}
		return resp.Content.Text(), nil
	}
	if _, err := invoke(infimum); err != nil {
		if _, err2 := invoke(supremum); err2 != nil {
			return schema.NewAgentResponse(request.RequestID, request.ChannelID,
				schema.WithResponseOK(false),
				schema.WithPayload(map[string]any{"error": err2.Error(), "code": "LLM_ERROR"})), nil
		}
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{"ok": true, "model_provider": modelProvider})), nil
}

// HandleModelsReplaceAll 处理 models.replace_all（Python: models_handlers.py:911
// models_replace_all_handler：整体原子替换 models.defaults）。
func (s *AgentServer) HandleModelsReplaceAll(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := requestParamsMap(request)
	newModels, err := configpanel.BuildModelsDefaultsFromFrontend(params["models"], resolveServerCrypto())
	if err != nil {
		return applyErrorResponse(request, err), nil
	}
	// Python: candidate 合成 + 四连归一/校验（:925-930）
	candidate := configpanel.ReadRawConfig()
	modelsMap, _ := candidate["models"].(map[string]any)
	if modelsMap == nil {
		modelsMap = map[string]any{}
	}
	modelsMap["defaults"] = newModels
	candidate["models"] = modelsMap
	common.RecoverExistingModelFields(candidate)
	common.NormalizeLegacyModelNulls(candidate)
	common.EnsureModelBusinessIDs(candidate)
	if err := common.RaiseIfInvalid(candidate); err != nil {
		return applyErrorResponse(request, err), nil
	}
	// Python: update_default_models_in_config（SetRoundTrip 保注释写回）
	if err := configpanel.ReplaceModelsDefaults(candidate["models"]); err != nil {
		return applyErrorResponse(request, &configpanel.ConfigPanelInternalError{Message: "failed to update models.defaults"}), nil
	}
	// Python: _apply_models_change(changed_keys=["models.defaults"], force=True)
	if err := s.agentManager.ReloadAgentsConfig(ctx, nil, nil, rtReloadOptionsModel()); err != nil {
		loggerWarn("models.replace_all reload failed", err)
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"count":                   len(newModels),
			"applied_without_restart": true,
		})), nil
}
