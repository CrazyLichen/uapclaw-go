// 模型目录 web RPC 的十个入口（23.4d，27.4 回填闭环）。
// 对应 Python 代码：gateway/channel_manager/web/app_web_handlers.py:5629-5678（注册名）、
// :5870-5949 _model_resource_rpc、:2570-2580 _vendors_list、:2586-2785 _vendors_fetch_models、
// jiuwenswarm/common/config_panel/model_resource_handlers.py:101-123 session_selection_handler。
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	modelcommon "github.com/uapclaw/uapclaw-go/internal/swarm/common"
	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// fetchModelsHTTPClient vendors.fetch_models 的远端拉取客户端。
// Python: app_web_handlers.py httpx.get(endpoint, headers=hdrs, timeout=12.0)。
var fetchModelsHTTPClient = &http.Client{Timeout: 12 * time.Second}

// ──────────────────────────── 非导出函数 ────────────────────────────

// requestParamsMap 解析请求参数为 map。
func requestParamsMap(request *schema.AgentRequest) map[string]any {
	if len(request.Params) == 0 {
		return map[string]any{}
	}
	var params map[string]any
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return map[string]any{}
	}
	return params
}

// modelSelectionErrorResponse ModelSelectionError → 错误响应。
// Python: app_web_handlers.py:5933-5935 ok=False, error=str(exc), code=exc.code, payload=exc.to_payload()。
func modelSelectionErrorResponse(request *schema.AgentRequest, err error) *schema.AgentResponse {
	payload := map[string]any{"error": err.Error(), "code": "BAD_REQUEST"}
	if se, ok := err.(*modelcommon.ModelSelectionError); ok {
		payload["code"] = se.Code
		for k, v := range se.ToPayload() {
			payload[k] = v
		}
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(false),
		schema.WithPayload(payload),
	)
}

// selectionFromParams 从参数解析 ModelSelection。
// key 非空时取 params[key] 子对象（model_resource_handlers.py:113
// ModelSelection.model_validate(params.get("model_selection"))）；
// key 为空时按 Python models.references 分支（app_web_handlers.py:5890
// ModelSelection.model_validate(params)）直接对整个 params 校验。
func selectionFromParams(params map[string]any, key string) (*modelcommon.ModelSelection, error) {
	var raw map[string]any
	if key == "" {
		raw = params
	} else {
		raw, _ = params[key].(map[string]any)
	}
	if raw == nil {
		return nil, fmt.Errorf("model_selection is required")
	}
	selection := &modelcommon.ModelSelection{
		Type:    asServerString(raw["type"]),
		ID:      asServerString(raw["id"]),
		RouteID: asServerString(raw["route_id"]),
	}
	if err := selection.Validate(); err != nil {
		return nil, err
	}
	return selection, nil
}

// asServerString any → string（nil → ""）。
func asServerString(v any) string {
	s, _ := v.(string)
	return s
}

// ──────────────────────────── 导出函数 ────────────────────────────

// serverCatalog 基于 AgentServer 的配置实例构建目录。
// Python: app_web_handlers.py ModelCatalog()（get_config() 全局）；
// Go 侧 config 为实例型，取 s.config 当前数据。
func (s *AgentServer) serverCatalog() (*modelcommon.ModelCatalog, error) {
	data, err := s.config.Load()
	if err != nil {
		return nil, err
	}
	return modelcommon.NewModelCatalog(data), nil
}

// HandleModelsGet 处理 models.get：单个模型详情。
// Python: app_web_handlers.py:5887-5889 payload = {"model": catalog.get_public_model_detail(model_id)}。
func (s *AgentServer) HandleModelsGet(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := requestParamsMap(request)
	catalog, catErr := s.serverCatalog()
	if catErr != nil {
		return modelSelectionErrorResponse(request, catErr), nil
	}
	modelID := strings.TrimSpace(asServerString(params["model_id"]))
	detail, err := catalog.GetPublicModelDetail(modelID, false)
	if err != nil {
		return modelSelectionErrorResponse(request, err), nil
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{"model": detail}),
	), nil
}

// HandleModelsUpsert 处理 models.upsert。
// Python: app_web_handlers.py:5919-5921 saved = upsert_model_resource(...); payload = {"model": detail}；
// 外层 model_resource_handler:86-89 upsert 后经 _apply_models_change 重载 → applied_without_restart。
func (s *AgentServer) HandleModelsUpsert(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := requestParamsMap(request)
	entry, _ := params["model"].(map[string]any)
	if entry == nil {
		entry = map[string]any{}
	}
	saved, err := modelcommon.UpsertModelResource(s.config, entry)
	if err != nil {
		return modelSelectionErrorResponse(request, err), nil
	}
	catalog, catErr := s.serverCatalog()
	if catErr != nil {
		return modelSelectionErrorResponse(request, catErr), nil
	}
	detail, err := catalog.GetPublicModelDetail(asServerString(saved["model_id"]), false)
	if err != nil {
		return modelSelectionErrorResponse(request, err), nil
	}
	payload := map[string]any{"model": detail}
	// Python: model_resource_handlers.py:86-89 upsert/delete 后重载配置
	payload["applied_without_restart"] = s.reloadAgentsAfterModelsChange(ctx)
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(payload),
	), nil
}

// reloadAgentsAfterModelsChange 模型变更后的配置重载（Python _apply_models_change 等价）。
// Python: config_panel/models_handlers.py _apply_models_change(on_config_saved, agent_client, changed_keys)。
// 重载失败不回滚写入，仅反映在 applied_without_restart=false（前端提示需重启）。
func (s *AgentServer) reloadAgentsAfterModelsChange(ctx context.Context) bool {
	if err := s.reloadAgentsConfig(ctx); err != nil {
		return false
	}
	return true
}

// HandleModelsDelete 处理 models.delete。
// Python: app_web_handlers.py:5903-5917 —— 引用扫描非空 → REFERENCED；未命中 → NOT_FOUND；
// 成功 payload = {key: resource_id, "deleted": true}。
func (s *AgentServer) HandleModelsDelete(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return s.handleResourceDelete(ctx, request, "model")
}

// HandleModelGroupsDelete 处理 model_groups.delete。
func (s *AgentServer) HandleModelGroupsDelete(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return s.handleResourceDelete(ctx, request, "model_group")
}

// handleResourceDelete models.delete / groups.delete 共用实现。
// Python: app_web_handlers.py:5903-5917。
func (s *AgentServer) handleResourceDelete(ctx context.Context, request *schema.AgentRequest, resourceType string) (*schema.AgentResponse, error) {
	params := requestParamsMap(request)
	key := "model_id"
	if resourceType == "model_group" {
		key = "model_group_id"
	}
	resourceID := strings.TrimSpace(asServerString(params[key]))
	catalog, catErr := s.serverCatalog()
	if catErr != nil {
		return modelSelectionErrorResponse(request, catErr), nil
	}
	// Python: :5909-5912 删除前引用扫描
	selection := &modelcommon.ModelSelection{Type: resourceType, ID: resourceID}
	references, err := catalog.FindReferences(selection, true)
	if err != nil {
		return modelSelectionErrorResponse(request, err), nil
	}
	if len(references) > 0 {
		refs := make([]map[string]any, 0, len(references))
		for _, r := range references {
			refs = append(refs, map[string]any{"scope": r.Scope, "scope_id": r.ScopeID})
		}
		return modelSelectionErrorResponse(request, modelcommon.NewModelSelectionError(
			modelcommon.ModelSelectionReferenced, "resource is still referenced",
			map[string]any{"references": refs},
		)), nil
	}
	deleted, err := modelcommon.DeleteModelResource(s.config, resourceType, resourceID)
	if err != nil {
		return modelSelectionErrorResponse(request, err), nil
	}
	if !deleted {
		return modelSelectionErrorResponse(request, modelcommon.NewModelSelectionError(
			modelcommon.ModelSelectionNotFound, "resource does not exist", nil,
		)), nil
	}
	payload := map[string]any{key: resourceID, "deleted": true}
	payload["applied_without_restart"] = s.reloadAgentsAfterModelsChange(ctx)
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(payload),
	), nil
}

// HandleModelsReferences 处理 models.references。
// Python: app_web_handlers.py:5890-5892 payload = {"references": [r.__dict__ ...]}。
func (s *AgentServer) HandleModelsReferences(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := requestParamsMap(request)
	selection, err := selectionFromParams(params, "")
	if err != nil {
		return modelSelectionErrorResponse(request, err), nil
	}
	catalog, catErr := s.serverCatalog()
	if catErr != nil {
		return modelSelectionErrorResponse(request, catErr), nil
	}
	references, err := catalog.FindReferences(selection, true)
	if err != nil {
		return modelSelectionErrorResponse(request, err), nil
	}
	refs := make([]map[string]any, 0, len(references))
	for _, r := range references {
		refs = append(refs, map[string]any{"scope": r.Scope, "scope_id": r.ScopeID})
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{"references": refs}),
	), nil
}

// HandleModelGroupsList 处理 model_groups.list。
// Python: app_web_handlers.py:5885-5886 payload = {"groups": catalog.list_public_groups()}。
func (s *AgentServer) HandleModelGroupsList(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	catalog, catErr := s.serverCatalog()
	if catErr != nil {
		return modelSelectionErrorResponse(request, catErr), nil
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{"groups": catalog.ListPublicGroups()}),
	), nil
}

// HandleModelGroupsUpsert 处理 model_groups.upsert。
// Python: app_web_handlers.py:5923-5924 payload = {"group": upsert_model_group_resource(...)}。
func (s *AgentServer) HandleModelGroupsUpsert(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := requestParamsMap(request)
	group, _ := params["group"].(map[string]any)
	if group == nil {
		group = map[string]any{}
	}
	saved, err := modelcommon.UpsertModelGroupResource(s.config, group)
	if err != nil {
		return modelSelectionErrorResponse(request, err), nil
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"group":                   saved,
			"applied_without_restart": s.reloadAgentsAfterModelsChange(ctx),
		}),
	), nil
}

// HandleVendorsList 处理 vendors.list。
// Python: app_web_handlers.py:2570-2580 payload = {"vendors": to_frontend_payload()}。
func (s *AgentServer) HandleVendorsList(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{"vendors": modelcommon.ToFrontendPayload()}),
	), nil
}

// HandleVendorsFetchModels 处理 vendors.fetch_models。
// Python: app_web_handlers.py:2586-2785 _vendors_fetch_models。
// 语义：按预设的 models_needs_key 决定是否带 Authorization；普通列表失败回退预设；
// 火山生成模型按 domain 返回完整远端目录，失败时返回空列表供前端手填。
// 已知偏差 E-7：Python 远端解析走 core parse_openai_account_model_ids（priority/
// visibility/forward-compat 全语义，属 23.6 领域）；本批按 data[].id 提取 + 去重，
// 完整解析 ⤵️ 23.6。
func (s *AgentServer) HandleVendorsFetchModels(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := requestParamsMap(request)
	vendorKey := strings.TrimSpace(asServerString(params["vendor_key"]))
	planRaw := strings.TrimSpace(asServerString(params["plan"]))
	apiKey := strings.TrimSpace(asServerString(params["api_key"]))
	modality := strings.TrimSpace(asServerString(params["modality"]))
	preset := modelcommon.GetPreset(vendorKey, modelcommon.PlanKind(planRaw))
	if preset == nil {
		// Python: :2650-2658 unknown vendor/plan → BAD_REQUEST
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": fmt.Sprintf("unknown vendor/plan: %s/%s", vendorKey, planRaw),
				"code":  "BAD_REQUEST",
			}),
		), nil
	}
	// Python: :2662-2679 modality 仅火山 custom_api 的 image/video 支持
	modelDomain := ""
	endpoint := preset.ModelsEndpoint
	if modality != "" {
		if vendorKey != "volcengine" || planRaw != string(modelcommon.PlanKindCustomAPI) || (modality != "image" && modality != "video") {
			return schema.NewAgentResponse(request.RequestID, request.ChannelID,
				schema.WithResponseOK(false),
				schema.WithPayload(map[string]any{"error": "unsupported model modality", "code": "BAD_REQUEST"}),
			), nil
		}
		apiBase := asServerString(params["api_base"])
		if apiBase == "" {
			apiBase = preset.APIBase
		}
		normalized, normErr := modelcommon.NormalizeArkAPIBase(apiBase)
		if normErr != nil {
			return schema.NewAgentResponse(request.RequestID, request.ChannelID,
				schema.WithResponseOK(false),
				schema.WithPayload(map[string]any{"error": normErr.Error(), "code": "BAD_REQUEST"}),
			), nil
		}
		endpoint = normalized + "/models"
		if modality == "image" {
			modelDomain = "ImageGeneration"
		} else {
			modelDomain = "VideoGeneration"
		}
	}
	// Python: :2682-2688 无远端端点 → 直接回退预设
	if endpoint == "" {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"models": preset.ModelOptions,
				"source": "preset",
				"reason": "no remote models endpoint",
			}),
		), nil
	}
	headers := map[string]string{"Accept": "application/json"}
	if preset.ModelsNeedsKey {
		if apiKey == "" {
			// Python: :2698-2706 需 key 未提供 → 回退预设
			models := preset.ModelOptions
			source := "preset"
			if modelDomain != "" {
				models = []string{}
				source = "unavailable"
			}
			return schema.NewAgentResponse(request.RequestID, request.ChannelID,
				schema.WithPayload(map[string]any{
					"models": models, "source": source, "reason": "api_key required for fetch",
				}),
			), nil
		}
		headers["Authorization"] = "Bearer " + apiKey
	}
	remoteIDs, remoteReason := fetchRemoteModelIDs(endpoint, headers, modelDomain)
	// Python: :2747-2758 alibaba 远端目录按套餐白名单过滤（保序）
	if vendorKey == "alibaba" && len(remoteIDs) > 0 {
		remoteSet := map[string]bool{}
		for _, id := range remoteIDs {
			remoteSet[id] = true
		}
		filtered := []string{}
		for _, modelID := range preset.ModelOptions {
			if remoteSet[modelID] {
				filtered = append(filtered, modelID)
			}
		}
		remoteIDs = filtered
	}
	if len(remoteIDs) > 0 {
		// Python: :2760-2764 成功
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{"models": remoteIDs, "source": "remote"}),
		), nil
	}
	// Python: :2766-2776 远端失败/空 → 回退预设（reason 透传前端联调）
	models := preset.ModelOptions
	source := "preset"
	if modelDomain != "" {
		models = []string{}
		source = "unavailable"
	}
	reason := remoteReason
	if reason == "" {
		reason = "remote fetch failed or empty"
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{"models": models, "source": source, "reason": reason}),
	), nil
}

// fetchRemoteModelIDs 同步拉取并解析 /models。
// Python: app_web_handlers.py:2613-2645 _fetch_remote_sync。
// 成功返回 (模型 ID 列表, "")；任何失败返回 ([], 原因)。永不返回 error。
func fetchRemoteModelIDs(endpoint string, headers map[string]string, modelDomain string) ([]string, string) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return []string{}, "remote fetch error: RequestError"
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := fetchModelsHTTPClient.Do(req)
	if err != nil {
		// Python: :2624-2627 网络异常返回类型名
		return []string{}, fmt.Sprintf("remote fetch error: %T", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Python: :2628-2631 非 200 状态码透传（401/403=鉴权、429=限流、5xx=上游故障）
	if resp.StatusCode != http.StatusOK {
		return []string{}, fmt.Sprintf("remote returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return []string{}, "remote returned non-JSON body"
	}
	var payloadJSON map[string]any
	if err := json.Unmarshal(raw, &payloadJSON); err != nil {
		return []string{}, "remote returned non-JSON body"
	}
	if modelDomain != "" {
		// Python: :2635-2649 生成模型按 domain 过滤完整目录
		entries, _ := payloadJSON["data"].([]any)
		ids := []string{}
		seen := map[string]bool{}
		for _, item := range entries {
			entry, ok := item.(map[string]any)
			if !ok || asServerString(entry["domain"]) != modelDomain {
				continue
			}
			modelID := asServerString(entry["id"])
			modelID = strings.TrimSpace(modelID)
			if modelID == "" || seen[modelID] {
				continue
			}
			seen[modelID] = true
			ids = append(ids, modelID)
		}
		if len(ids) == 0 {
			return []string{}, "remote returned empty generation model list"
		}
		return ids, ""
	}
	// Python: :2650-2659 parse_openai_account_model_ids —— 偏差 E-7：按 data[].id 提取去重
	ids := []string{}
	seen := map[string]bool{}
	entries, _ := payloadJSON["data"].([]any)
	for _, item := range entries {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		modelID := strings.TrimSpace(asServerString(entry["id"]))
		if modelID == "" || seen[modelID] {
			continue
		}
		seen[modelID] = true
		ids = append(ids, modelID)
	}
	if len(ids) == 0 {
		return []string{}, "remote returned empty model list"
	}
	return ids, ""
}

// HandleSessionSelectionSet 处理 session.selection.set。
// Python: config_panel/model_resource_handlers.py:101-123 session_selection_handler。
// 语义：target = params.session_id || 会话 id；校验选择；model_group 拒绝定时任务会话；
// resolve 校验通过后写入；响应 {"model_selection": {...}}。
func (s *AgentServer) HandleSessionSelectionSet(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := requestParamsMap(request)
	// Python: :104 target = str(params.get("session_id") or session_id or "")
	target := strings.TrimSpace(asServerString(params["session_id"]))
	if target == "" && request.SessionID != nil {
		target = strings.TrimSpace(*request.SessionID)
	}
	selection, err := selectionFromParams(params, "model_selection")
	if err != nil {
		return modelSelectionErrorResponse(request, err), nil
	}
	// Python: :107-111 model_group 拒绝定时任务会话（"Scheduled task sessions only support a single model"）
	if selection.Type == "model_group" {
		meta := swarmsession.GetSessionMetadata(target)
		if asServerString(meta["cron_id"]) != "" {
			return modelSelectionErrorResponse(request, fmt.Errorf("Scheduled task sessions only support a single model")), nil
		}
	}
	// Python: :112 ModelSelectionResolver().resolve(selection) 准入校验
	cfgData, cfgErr := s.config.Load()
	if cfgErr != nil {
		return modelSelectionErrorResponse(request, cfgErr), nil
	}
	resolver := modelcommon.NewModelSelectionResolver(modelcommon.NewModelCatalog(cfgData))
	if _, err := resolver.Resolve(selection, nil); err != nil {
		return modelSelectionErrorResponse(request, err), nil
	}
	// Python: :113 set_session_model_selection(target, selection)
	if err := modelcommon.SetSessionModelSelection(target, selection); err != nil {
		return modelSelectionErrorResponse(request, err), nil
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{"model_selection": map[string]any{
			"type": selection.Type, "id": selection.ID, "route_id": selection.RouteID,
		}}),
	), nil
}
