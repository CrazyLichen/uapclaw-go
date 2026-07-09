package skill

import (
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

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
