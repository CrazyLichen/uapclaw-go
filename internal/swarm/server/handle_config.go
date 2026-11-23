package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	rt "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/control"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/control/configpanel"
)

// logComponentAgentServer server 包配置分派日志组件。
const logComponentAgentServer = logger.ComponentAgentServer

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleConfigCacheClear 处理 config.cache_clear 请求。清除配置缓存，返回 ok=true。
func (s *AgentServer) handleConfigCacheClear(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	if s.config != nil {
		// 重新加载配置，等效于清除缓存
		_ = s.config.Reload()
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok": true,
		}),
	), nil
}

// handleAgentReloadConfig 处理 agent.reload_config 请求。
// 从 request.Params 提取 config / env / target_channel_id / target_session_id /
// reload_scopes，调用 AgentManager.ReloadAgentsConfig。
// Python: agent_ws_server.py:12860-12889 _handle_agent_reload_config
// （reload_scopes 只接受字符串列表；合法域校验在 AgentManager 侧按交集判断）。
func (s *AgentServer) handleAgentReloadConfig(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var configPayload map[string]any
	var envOverrides map[string]string
	opts := rt.ReloadOptions{}

	if len(request.Params) > 0 {
		var params map[string]any
		if err := json.Unmarshal(request.Params, &params); err == nil {
			if c, ok := params["config"]; ok {
				if m, ok := c.(map[string]any); ok {
					configPayload = m
				}
			}
			if e, ok := params["env"]; ok {
				if m, ok := e.(map[string]any); ok {
					envOverrides = anyMapToStringMap(m)
				}
			}
			// Python: agent_ws_server.py:12866-12878 target/scope 解析
			//（scope 合法域 {model, multimodal, search, team, permissions, agent_runtime}）
			if tc, ok := params["target_channel_id"]; ok {
				opts.TargetChannelID = strings.TrimSpace(fmt.Sprintf("%v", tc))
				if opts.TargetChannelID == "<nil>" {
					opts.TargetChannelID = ""
				}
			}
			if ts, ok := params["target_session_id"]; ok {
				opts.TargetSessionID = strings.TrimSpace(fmt.Sprintf("%v", ts))
				if opts.TargetSessionID == "<nil>" {
					opts.TargetSessionID = ""
				}
			}
			if rawScopes, ok := params["reload_scopes"].([]any); ok {
				for _, scope := range rawScopes {
					if str, isStr := scope.(string); isStr && str != "" {
						opts.ReloadScopes = append(opts.ReloadScopes, str)
					}
				}
			}
		}
	}

	// Python: agent_ws_server.py:12908-12921
	// should_warm_tokenizers = True（scopes 为空）或 "model" in reload_scopes
	shouldWarmTokenizers := len(opts.ReloadScopes) == 0
	for _, scope := range opts.ReloadScopes {
		if scope == "model" {
			shouldWarmTokenizers = true
			break
		}
	}
	// Python: agent_ws_server.py:12912-12921 模型配置变了就重探图像模态：
	// 同 (api_base, model_name) 背后可能已换端点/密钥/后端，旧结论不能留。
	// 统一槽位调度——上一轮未探完就被取消，reset 缓存后按最新配置重探。
	if shouldWarmTokenizers {
		s.scheduleImageModalityWarmup("model config change", true)
	}

	if err := s.agentManager.ReloadAgentsConfig(ctx, configPayload, envOverrides, opts); err != nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": err.Error(),
			}),
		), nil
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"reloaded": true,
		}),
	), nil
}

// handleConfigPanelRequest 配置面板 RPC 分派（27.4；Python: config_adapter.py:175
// ConfigAdapter.handle）。分支顺序与 Python 一致：CONFIG_MOA_STATUS →
// MODELS_ZEN_ENTRIES → MODELS_ZEN_WARM → LOCALE_GET/SET → PATH_GET/SET →
// PERMISSIONS_OWNER_SCOPES_GET/SET → MEMORY_FORBIDDEN_GET/SET →
// config.get/set/save_all/validate_model、models.list/validate/replace_all。
// 写链（config.set/save_all/validate_model、locale.set_conf、models.validate/
// replace_all、memory/permissions 的 set）随 configpanel 写入层落地，未落地前
// 进入 default 回 BAD_REQUEST（IMPLEMENTATION_PLAN.md 27.4 行 ⤵️ 登记）。
// command.model 按 spec D-1 豁免（既有 handleCommandModel 为 TUI 独立链，不属本分派）。
func (s *AgentServer) handleConfigPanelRequest(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var params map[string]any
	if len(request.Params) > 0 {
		_ = json.Unmarshal(request.Params, &params)
	}
	okResp := func(payload map[string]any) *schema.AgentResponse {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(payload))
	}
	badReq := func() *schema.AgentResponse {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": "unsupported control config method",
				"code":  "BAD_REQUEST",
			}))
	}

	// Python: config_adapter.py:180-189 CONFIG_MOA_STATUS → moa_status(get_config(), selection)。
	// 依赖 moa_runtime（build_moa_runtime/resolve_moa_settings，Python
	// server/runtime/moa_runtime.py 222 行），Go 侧无对应物 → 本批按 Python
	// except 兜底形返回 {"configured": false}；缺口在 IMPLEMENTATION_PLAN.md
	// 27.4 行登记 ⤵️ 待回填（回填点=MOA 运行时域落地节）。
	if request.ReqMethod == schema.ReqMethodConfigMoaStatus {
		return okResp(map[string]any{"configured": false}), nil
	}

	// Python: config_adapter.py:191-200 MODELS_ZEN_ENTRIES → public_zen_free_model_rows()
	if request.ReqMethod == schema.ReqMethodModelsZenEntries {
		return okResp(map[string]any{"models": configpanel.PublicZenFreeModelRows()}), nil
	}
	// Python: config_adapter.py:202-210 MODELS_ZEN_WARM → warm_zen_free_models(reason="config-toggle")
	if request.ReqMethod == schema.ReqMethodModelsZenWarm {
		configpanel.WarmZenFreeModels("config-toggle", false)
		return okResp(map[string]any{"ok": true}), nil
	}

	// Python: config_set_handlers.py:2224 _config_set / :2233 _config_save_all
	// （apply_config_payload + apply_config_change_set）。
	if request.ReqMethod == schema.ReqMethodConfigSet || request.ReqMethod == schema.ReqMethodConfigSaveAll {
		force := request.ReqMethod == schema.ReqMethodConfigSaveAll
		result, applyErr := configpanel.ApplyConfigSet(ctx, params, resolveServerCrypto())
		if applyErr != nil {
			return applyErrorResponse(request, applyErr), nil
		}
		if err := configpanel.CommitPendingPermissionProfile(result); err != nil {
			return applyErrorResponse(request, err), nil
		}
		changeSet := configpanel.ConfigChangeSet{
			EnvUpdates:  result.EnvUpdates,
			YAMLUpdated: result.YAMLUpdated,
			Force:       force,
		}
		// Python: apply_config_change_set → _clear_agent_config_cache / on_config_saved
		// （AgentServer 进程内直调 AgentManager 重载，对齐 reload_options 语义）
		scopes := changeSet.ReloadScopes()
		if changeSet.Changed() && configpanel.NeedsAgentReload(scopes) {
			opts := rt.ReloadOptions{ReloadScopes: scopes}
			if err := s.agentManager.ReloadAgentsConfig(ctx, nil, result.EnvUpdates, opts); err != nil {
				logger.Warn(logComponentAgentServer).Err(err).Msg("config reload after save failed")
			}
		}
		payload := map[string]any{"ok": true}
		if result.CanonicalConfig != nil {
			for k, v := range result.CanonicalConfig {
				payload[k] = v
			}
		}
		return okResp(payload), nil
	}

	// Python: models_handlers.py:803/667 models.list / config.validate_model / models.validate
	if request.ReqMethod == schema.ReqMethodModelsList {
		return s.HandleModelsList(ctx, request)
	}
	if request.ReqMethod == schema.ReqMethodModelsValidate || request.ReqMethod == schema.ReqMethodConfigValidateModel {
		return s.HandleModelsValidate(ctx, request)
	}
	if request.ReqMethod == schema.ReqMethodModelsReplaceAll {
		return s.HandleModelsReplaceAll(ctx, request)
	}

	// Python: config_adapter.py:212-236 LOCALE_SET_CONF
	if request.ReqMethod == schema.ReqMethodLocaleSetConf {
		if err := configpanel.LocaleSetConf(params); err != nil {
			return applyErrorResponse(request, err), nil
		}
		return okResp(map[string]any{"preferred_language": orStr(params["preferred_language"])}), nil
	}
	// Python: config_adapter.py:333-388 PATH_SET（decision 族字段 ⤵️ 待回填）
	if request.ReqMethod == schema.ReqMethodPathSet {
		chromePath, headless, err := configpanel.PathSet(params)
		if err != nil {
			return applyErrorResponse(request, err), nil
		}
		return okResp(map[string]any{"chrome_path": chromePath, "headless": headless}), nil
	}
	// Python: config_adapter.py:393-427 PERMISSIONS_OWNER_SCOPES_GET/SET
	if request.ReqMethod == schema.ReqMethodPermissionsOwnerScopesGet {
		return okResp(configpanel.GetPermissionsOwnerScopes()), nil
	}
	if request.ReqMethod == schema.ReqMethodPermissionsOwnerScopesSet {
		if params == nil {
			return applyErrorResponse(request, &configpanel.ConfigPanelBadRequest{Message: "params must be object"}), nil
		}
		configpanel.UpdatePermissionsOwnerScopes(params["owner_scopes"], params["deny_guidance_message"])
		// Python: A5 写入后 server 侧 in-process reload（reload_scopes=["permissions"]）
		if err := s.agentManager.ReloadAgentsConfig(ctx, nil, nil, rt.ReloadOptions{ReloadScopes: []string{"permissions"}}); err != nil {
			logger.Warn(logComponentAgentServer).Err(err).Msg("permissions scope reload failed")
		}
		return okResp(map[string]any{"ok": true, "applied_without_restart": true}), nil
	}
	// Python: config_adapter.py:431-446 MEMORY_FORBIDDEN_SET
	if request.ReqMethod == schema.ReqMethodMemoryForbiddenSet {
		if params == nil {
			return applyErrorResponse(request, &configpanel.ConfigPanelBadRequest{Message: "params must be object"}), nil
		}
		configpanel.MemoryForbiddenSet(params)
		return okResp(map[string]any{"ok": true}), nil
	}

	// 其余读链方法经 control.HandleConfigRequest 分派（config.get / locale.get_conf /
	// path.get / memory.forbidden.get；其余 unsupported → BAD_REQUEST）。
	payload, ok := control.HandleConfigRequest(string(request.ReqMethod), params)
	if !ok {
		return badReq(), nil
	}
	return okResp(payload), nil
}

// orStr any→string（nil 回空串）。
func orStr(v any) string {
	if s, isStr := v.(string); isStr {
		return s
	}
	return ""
}

// applyErrorResponse 错误分类响应：400 类回 ok=false+error 文案，500 类同形
// （Python build_error_response / ConfigPanelBadRequest→BAD_REQUEST、
// ConfigPanelInternalError→INTERNAL_ERROR）。
func applyErrorResponse(request *schema.AgentRequest, err error) *schema.AgentResponse {
	code := "INTERNAL_ERROR"
	var badReq *configpanel.ConfigPanelBadRequest
	if errors.As(err, &badReq) {
		code = "BAD_REQUEST"
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(false),
		schema.WithPayload(map[string]any{"error": err.Error(), "code": code}))
}

// resolveServerCrypto server 侧 crypto provider（Python: _get_crypto_provider
// 经 common 钩子取，未注册回 nil → 明文落库，spec D-3）。
func resolveServerCrypto() crypto.CryptoProvider {
	return crypto.GetCryptoProvider()
}

// anyMapToStringMap 将 map[string]any 转为 map[string]string，
// 用于从 JSON 反序列化的 env 参数适配 AgentManager.ReloadAgentsConfig 的 string 类型签名。
func anyMapToStringMap(m map[string]any) map[string]string {
	if m == nil {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		if v == nil {
			result[k] = ""
			continue
		}
		if s, ok := v.(string); ok {
			result[k] = s
		} else {
			result[k] = fmt.Sprintf("%v", v)
		}
	}
	return result
}
