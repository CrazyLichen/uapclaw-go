package server

import (
	"encoding/json"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/equipment"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// HandlePackageCatalogRequest 设备目录/生命周期请求统一入口。
// Python: interface.py:2980-3120 _handle_package_catalog_request——
// `id`/`name` 兼容别名（:2984-2986），逐 method 分派到 equipment 半区。
func HandlePackageCatalogRequest(request *schema.AgentRequest) *schema.AgentResponse {
	params := catalogRequestParams(request)
	// 前端契约用 `id`；兼容旧 `name` 别名（Python :2984-2986）
	name := params["id"]
	if name == nil || name == "" {
		name = params["name"]
	}
	unloadLive := equipment.NewUnloadLiveFunc()
	marketUnavailable := func() *schema.AgentResponse {
		return catalogErrorResponse(request, "专家市场服务未启用", "MARKET_UNAVAILABLE")
	}
	var payload map[string]any
	var err error
	var errCode string
	switch request.ReqMethod {
	// ─── 读路径 ───
	case schema.ReqMethodAgentTemplatesList:
		var cards []map[string]any
		cards, err = equipment.ListAgentTemplates(params)
		if err == nil {
			payload = map[string]any{"templates": toAnyList(cards)}
		}
	case schema.ReqMethodAgentTemplatesShow:
		card := equipment.ShowAgentTemplate(stringOrEmpty2(name))
		if card == nil {
			err = &catalogValueError{Message: "agent_template not found: " + stringOrEmpty2(name)}
		} else {
			payload = map[string]any{"template": card}
		}
	case schema.ReqMethodAgentTemplatesFileList:
		var tree []map[string]any
		tree, err = equipment.ListPackageFiles("agent_template", stringOrEmpty2(name))
		if err == nil {
			payload = map[string]any{"tree": toAnyList(tree)}
		}
	case schema.ReqMethodAgentTemplatesFileRead:
		payload, err = equipment.ReadPackageFile("agent_template", stringOrEmpty2(name),
			stringOrEmpty2(params["path"]))
	case schema.ReqMethodAgentGroupsList:
		var cards []map[string]any
		cards, err = equipment.ListAgentGroups(params)
		if err == nil {
			payload = map[string]any{"agentGroups": toAnyList(cards)}
		}
	case schema.ReqMethodAgentGroupsShow:
		card := equipment.ShowAgentGroup(stringOrEmpty2(name))
		if card == nil {
			err = &catalogValueError{Message: "agent_group not found: " + stringOrEmpty2(name)}
		} else {
			payload = map[string]any{"group": card}
		}
	case schema.ReqMethodAgentGroupsFileList:
		var tree []map[string]any
		tree, err = equipment.ListPackageFiles("agent_group", stringOrEmpty2(name))
		if err == nil {
			payload = map[string]any{"tree": toAnyList(tree)}
		}
	case schema.ReqMethodAgentGroupsFileRead:
		payload, err = equipment.ReadPackageFile("agent_group", stringOrEmpty2(name),
			stringOrEmpty2(params["path"]))
	// ─── 本地生命周期 ───
	case schema.ReqMethodAgentTemplatesCreate:
		err = equipment.CreateAgentTemplate(params)
		if err == nil {
			payload = map[string]any{}
		}
	case schema.ReqMethodAgentTemplatesImportLocal:
		payload, err = equipment.ImportAgentTemplate(params)
	case schema.ReqMethodAgentTemplatesUpdate:
		// Python :3106-3112：先卸载运行会话已加载模板，使更新立即生效
		err = equipment.UpdateAgentTemplate(params, unloadLive)
		if err == nil {
			payload = map[string]any{}
		}
	case schema.ReqMethodAgentTemplatesDelete:
		err = equipment.DeleteAgentTemplate(params, unloadLive)
		if err == nil {
			payload = map[string]any{}
		}
	case schema.ReqMethodAgentTemplatesUninstall:
		payload, err = equipment.UninstallEquipmentWithNotice(
			"agent_templates", params, unloadLive)
	case schema.ReqMethodAgentGroupsCreate:
		payload, err = equipment.CreateAgentGroup(params)
	case schema.ReqMethodAgentGroupsImportLocal:
		payload, err = equipment.ImportAgentGroup(params)
	case schema.ReqMethodAgentGroupsUninstall:
		err = equipment.UninstallOrganizationAgentGroup(params, unloadLive)
		if err == nil {
			payload = map[string]any{}
		}
	case schema.ReqMethodPluginPackagesUninstall:
		payload, err = equipment.UninstallEquipmentWithNotice(
			"plugin_packages", params, unloadLive)
	// ─── Hub 安装（D-1：市场服务未启用）───
	case schema.ReqMethodAgentTemplatesInstall,
		schema.ReqMethodAgentGroupsInstall,
		schema.ReqMethodPluginPackagesInstall:
		return marketUnavailable()
	default:
		return catalogErrorResponse(request,
			"unsupported package catalog method: "+string(request.ReqMethod), "BAD_REQUEST")
	}
	if err != nil {
		code := "INTERNAL_ERROR"
		var ce *catalogValueError
		var pce *equipment.PackageConflictError
		var age *equipment.AgentGroupPackageError
		switch {
		case asCatalogValueError(err, &ce):
			code = "BAD_REQUEST"
		case asPackageConflict(err, &pce):
			code = "BAD_REQUEST"
		case asAgentGroupPackageError(err, &age):
			code = age.ErrorCode()
		}
		if errCode != "" {
			code = errCode
		}
		logger.Warn(logger.ComponentAgentServer).
			Str("method", string(request.ReqMethod)).
			Err(err).
			Msg("[equipment] package catalog request failed")
		return catalogErrorResponse(request, err.Error(), code)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true),
		schema.WithPayload(payload),
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// catalogValueError 分派层 ValueError 语义（BAD_REQUEST）。
type catalogValueError struct {
	// Message 信息
	Message string
}

// Error 实现 error 接口。
func (e *catalogValueError) Error() string { return e.Message }

// catalogErrorResponse 统一错误响应。
func catalogErrorResponse(request *schema.AgentRequest, message, code string) *schema.AgentResponse {
	logger.Warn(logger.ComponentAgentServer).
		Str("method", string(request.ReqMethod)).
		Msg(message)
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(false),
		schema.WithPayload(map[string]any{"error": message, "message": message, "code": code}),
	)
}

// asCatalogValueError / asPackageConflict / codeOf 错误分类助手。
func asCatalogValueError(err error, out **catalogValueError) bool {
	if ce, ok := err.(*catalogValueError); ok {
		*out = ce
		return true
	}
	return false
}

func asPackageConflict(err error, out **equipment.PackageConflictError) bool {
	if pce, ok := err.(*equipment.PackageConflictError); ok {
		*out = pce
		return true
	}
	return false
}

func asAgentGroupPackageError(err error, out **equipment.AgentGroupPackageError) bool {
	if age, ok := err.(*equipment.AgentGroupPackageError); ok {
		*out = age
		return true
	}
	return false
}

// toAnyList map 列表转 any。
func toAnyList(items []map[string]any) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}

// stringOrEmpty2 any 字符串读取（与 repositories 的助手重名避让）。
func stringOrEmpty2(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// requestParamsMap 参数解析（与 repositories 同形；同包重名避让加前缀）。
func catalogRequestParams(request *schema.AgentRequest) map[string]any {
	var params map[string]any
	if len(request.Params) == 0 {
		return map[string]any{}
	}
	if err := json.Unmarshal(request.Params, &params); err != nil || params == nil {
		return map[string]any{}
	}
	return params
}
