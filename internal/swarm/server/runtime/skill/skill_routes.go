package skill

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillHandler skills/plugins 请求处理函数签名。
type SkillHandler func(sm *SkillManager, ctx context.Context, params map[string]any) (map[string]any, error)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// skillRoutes skills.* 请求方法 → handler 方法名映射
	// 对应 Python: _SKILL_ROUTES (interface.py)
	skillRoutes = map[schema.ReqMethod]string{
		schema.ReqMethodSkillsList:                   "HandleSkillsList",
		schema.ReqMethodSkillsInstalled:              "HandleSkillsInstalled",
		schema.ReqMethodSkillsGet:                    "HandleSkillsGet",
		schema.ReqMethodSkillsToggle:                 "HandleSkillsToggle",
		schema.ReqMethodSkillsMarketplaceList:        "HandleSkillsMarketplaceList",
		schema.ReqMethodSkillsInstall:                "HandleSkillsInstall",
		schema.ReqMethodSkillsUninstall:              "HandleSkillsUninstall",
		schema.ReqMethodSkillsImportLocal:            "HandleSkillsImportLocal",
		schema.ReqMethodSkillsMarketplaceAdd:         "HandleSkillsMarketplaceAdd",
		schema.ReqMethodSkillsMarketplaceRemove:      "HandleSkillsMarketplaceRemove",
		schema.ReqMethodSkillsMarketplaceToggle:      "HandleSkillsMarketplaceToggle",
		schema.ReqMethodSkillsSkillnetSearch:         "HandleSkillsSkillnetSearch",
		schema.ReqMethodSkillsSkillnetInstall:        "HandleSkillsSkillnetInstall",
		schema.ReqMethodSkillsSkillnetInstallStatus:  "HandleSkillsSkillnetInstallStatus",
		schema.ReqMethodSkillsSkillnetEvaluate:       "HandleSkillsSkillnetEvaluate",
		schema.ReqMethodSkillsClawhubGetToken:        "HandleSkillsClawhubGetToken",
		schema.ReqMethodSkillsClawhubSetToken:        "HandleSkillsClawhubSetToken",
		schema.ReqMethodSkillsClawhubSearch:          "HandleSkillsClawhubSearch",
		schema.ReqMethodSkillsClawhubDownload:        "HandleSkillsClawhubDownload",
		schema.ReqMethodSkillsTeamSkillsHubInfo:      "HandleSkillsTeamSkillsHubInfo",
		schema.ReqMethodSkillsTeamSkillsHubInit:      "HandleSkillsTeamSkillsHubInit",
		schema.ReqMethodSkillsTeamSkillsHubValidate:  "HandleSkillsTeamSkillsHubValidate",
		schema.ReqMethodSkillsTeamSkillsHubPack:      "HandleSkillsTeamSkillsHubPack",
		schema.ReqMethodSkillsTeamSkillsHubSearch:    "HandleSkillsTeamSkillsHubSearch",
		schema.ReqMethodSkillsTeamSkillsHubInstall:   "HandleSkillsTeamSkillsHubInstall",
		schema.ReqMethodSkillsTeamSkillsHubPublish:   "HandleSkillsTeamSkillsHubPublish",
		schema.ReqMethodSkillsTeamSkillsHubDelete:    "HandleSkillsTeamSkillsHubDelete",
		schema.ReqMethodSkillsEvolutionStatus:        "HandleSkillsEvolutionStatus",
		schema.ReqMethodSkillsEvolutionGet:           "HandleSkillsEvolutionGet",
		schema.ReqMethodSkillsEvolutionSave:          "HandleSkillsEvolutionSave",
	}

	// pluginRoutes plugins.* 请求方法 → handler 方法名映射
	// 对应 Python: _PLUGIN_ROUTES (interface.py)
	pluginRoutes = map[schema.ReqMethod]string{
		schema.ReqMethodPluginsList:      "HandlePluginsList",
		schema.ReqMethodPluginsInstall:   "HandlePluginsInstall",
		schema.ReqMethodPluginsUninstall: "HandlePluginsUninstall",
		schema.ReqMethodPluginsEnable:    "HandlePluginsEnable",
		schema.ReqMethodPluginsDisable:   "HandlePluginsDisable",
		schema.ReqMethodPluginsReload:    "HandlePluginsReload",
	}

	// needsRebuildMethods 触发 Agent rebuild 的请求方法集合
	// 对应 Python: needs_rebuild 判断逻辑
	needsRebuildMethods = map[schema.ReqMethod]bool{
		schema.ReqMethodSkillsInstall:                true,
		schema.ReqMethodSkillsUninstall:              true,
		schema.ReqMethodSkillsImportLocal:            true,
		schema.ReqMethodSkillsToggle:                 true,
		schema.ReqMethodSkillsSkillnetInstall:        true,
		schema.ReqMethodSkillsClawhubDownload:        true,
		schema.ReqMethodSkillsTeamSkillsHubInstall:   true,
		schema.ReqMethodPluginsInstall:               true,
		schema.ReqMethodPluginsUninstall:             true,
		schema.ReqMethodPluginsReload:                true,
	}

	// skilldevMethods skilldev.* 请求方法集合
	skilldevMethods = map[schema.ReqMethod]bool{
		schema.ReqMethodSkilldevStart:    true,
		schema.ReqMethodSkilldevRespond:  true,
		schema.ReqMethodSkilldevStatus:   true,
		schema.ReqMethodSkilldevDownload: true,
		schema.ReqMethodSkilldevCancel:   true,
		schema.ReqMethodSkilldevFileList: true,
		schema.ReqMethodSkilldevFileRead: true,
	}

	// SkillRoutes skills.* 请求方法 → handler 函数映射（导出，供 UapClaw 调用）。
	SkillRoutes = map[schema.ReqMethod]SkillHandler{
		schema.ReqMethodSkillsList:                   (*SkillManager).HandleSkillsList,
		schema.ReqMethodSkillsInstalled:              (*SkillManager).HandleSkillsInstalled,
		schema.ReqMethodSkillsGet:                    (*SkillManager).HandleSkillsGet,
		schema.ReqMethodSkillsToggle:                 (*SkillManager).HandleSkillsToggle,
		schema.ReqMethodSkillsMarketplaceList:        (*SkillManager).HandleSkillsMarketplaceList,
		schema.ReqMethodSkillsInstall:                (*SkillManager).HandleSkillsInstall,
		schema.ReqMethodSkillsUninstall:              (*SkillManager).HandleSkillsUninstall,
		schema.ReqMethodSkillsImportLocal:            (*SkillManager).HandleSkillsImportLocal,
		schema.ReqMethodSkillsMarketplaceAdd:         (*SkillManager).HandleSkillsMarketplaceAdd,
		schema.ReqMethodSkillsMarketplaceRemove:      (*SkillManager).HandleSkillsMarketplaceRemove,
		schema.ReqMethodSkillsMarketplaceToggle:      (*SkillManager).HandleSkillsMarketplaceToggle,
		schema.ReqMethodSkillsSkillnetSearch:         (*SkillManager).HandleSkillsSkillnetSearch,
		schema.ReqMethodSkillsSkillnetInstall:        (*SkillManager).HandleSkillsSkillnetInstall,
		schema.ReqMethodSkillsSkillnetInstallStatus:  (*SkillManager).HandleSkillsSkillnetInstallStatus,
		schema.ReqMethodSkillsSkillnetEvaluate:       (*SkillManager).HandleSkillsSkillnetEvaluate,
		schema.ReqMethodSkillsClawhubGetToken:        (*SkillManager).HandleSkillsClawhubGetToken,
		schema.ReqMethodSkillsClawhubSetToken:        (*SkillManager).HandleSkillsClawhubSetToken,
		schema.ReqMethodSkillsClawhubSearch:          (*SkillManager).HandleSkillsClawhubSearch,
		schema.ReqMethodSkillsClawhubDownload:        (*SkillManager).HandleSkillsClawhubDownload,
		schema.ReqMethodSkillsTeamSkillsHubInfo:      (*SkillManager).HandleSkillsTeamSkillsHubInfo,
		schema.ReqMethodSkillsTeamSkillsHubInit:      (*SkillManager).HandleSkillsTeamSkillsHubInit,
		schema.ReqMethodSkillsTeamSkillsHubValidate:  (*SkillManager).HandleSkillsTeamSkillsHubValidate,
		schema.ReqMethodSkillsTeamSkillsHubPack:      (*SkillManager).HandleSkillsTeamSkillsHubPack,
		schema.ReqMethodSkillsTeamSkillsHubSearch:    (*SkillManager).HandleSkillsTeamSkillsHubSearch,
		schema.ReqMethodSkillsTeamSkillsHubInstall:   (*SkillManager).HandleSkillsTeamSkillsHubInstall,
		schema.ReqMethodSkillsTeamSkillsHubPublish:   (*SkillManager).HandleSkillsTeamSkillsHubPublish,
		schema.ReqMethodSkillsTeamSkillsHubDelete:    (*SkillManager).HandleSkillsTeamSkillsHubDelete,
		schema.ReqMethodSkillsEvolutionStatus:        (*SkillManager).HandleSkillsEvolutionStatus,
		schema.ReqMethodSkillsEvolutionGet:           (*SkillManager).HandleSkillsEvolutionGet,
		schema.ReqMethodSkillsEvolutionSave:          (*SkillManager).HandleSkillsEvolutionSave,
	}

	// PluginRoutes plugins.* 请求方法 → handler 函数映射（导出，供 UapClaw 调用）。
	PluginRoutes = map[schema.ReqMethod]SkillHandler{
		schema.ReqMethodPluginsList:      (*SkillManager).HandlePluginsList,
		schema.ReqMethodPluginsInstall:   (*SkillManager).HandlePluginsInstall,
		schema.ReqMethodPluginsUninstall: (*SkillManager).HandlePluginsUninstall,
		schema.ReqMethodPluginsEnable:    (*SkillManager).HandlePluginsEnable,
		schema.ReqMethodPluginsDisable:   (*SkillManager).HandlePluginsDisable,
		schema.ReqMethodPluginsReload:    (*SkillManager).HandlePluginsReload,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SkillRouteHandler 返回 skills.* 请求方法对应的 handler 方法名
// 对应 Python: _SKILL_ROUTES[request.req_method]
func SkillRouteHandler(method schema.ReqMethod) (string, bool) {
	handler, ok := skillRoutes[method]
	return handler, ok
}

// PluginRouteHandler 返回 plugins.* 请求方法对应的 handler 方法名
// 对应 Python: _PLUGIN_ROUTES[request.req_method]
func PluginRouteHandler(method schema.ReqMethod) (string, bool) {
	handler, ok := pluginRoutes[method]
	return handler, ok
}

// NeedsRebuild 判断请求方法是否触发 Agent rebuild
// 对应 Python: needs_rebuild 判断
func NeedsRebuild(method schema.ReqMethod) bool {
	return needsRebuildMethods[method]
}

// IsSkillMethod 判断请求方法是否属于 skills.* 路由
func IsSkillMethod(method schema.ReqMethod) bool {
	_, ok := skillRoutes[method]
	return ok
}

// IsPluginMethod 判断请求方法是否属于 plugins.* 路由
func IsPluginMethod(method schema.ReqMethod) bool {
	_, ok := pluginRoutes[method]
	return ok
}

// SkillRouteCount 返回 skills.* 路由数量
func SkillRouteCount() int {
	return len(skillRoutes)
}

// PluginRouteCount 返回 plugins.* 路由数量
func PluginRouteCount() int {
	return len(pluginRoutes)
}

// IsSkillDevMethod 判断请求方法是否属于 skilldev.* 路由。
func IsSkillDevMethod(method schema.ReqMethod) bool {
	return skilldevMethods[method]
}
