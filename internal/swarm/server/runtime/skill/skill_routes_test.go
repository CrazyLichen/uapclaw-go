package skill

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestSkillRouteHandler_核心方法 验证核心 skills.* 路由
func TestSkillRouteHandler_核心方法(t *testing.T) {
	tests := []struct {
		method     schema.ReqMethod
		wantMethod string
	}{
		{schema.ReqMethodSkillsList, "HandleSkillsList"},
		{schema.ReqMethodSkillsInstalled, "HandleSkillsInstalled"},
		{schema.ReqMethodSkillsGet, "HandleSkillsGet"},
		{schema.ReqMethodSkillsToggle, "HandleSkillsToggle"},
		{schema.ReqMethodSkillsMarketplaceList, "HandleSkillsMarketplaceList"},
		{schema.ReqMethodSkillsInstall, "HandleSkillsInstall"},
		{schema.ReqMethodSkillsUninstall, "HandleSkillsUninstall"},
		{schema.ReqMethodSkillsImportLocal, "HandleSkillsImportLocal"},
		{schema.ReqMethodSkillsMarketplaceAdd, "HandleSkillsMarketplaceAdd"},
		{schema.ReqMethodSkillsMarketplaceRemove, "HandleSkillsMarketplaceRemove"},
		{schema.ReqMethodSkillsMarketplaceToggle, "HandleSkillsMarketplaceToggle"},
		{schema.ReqMethodSkillsSkillnetSearch, "HandleSkillsSkillnetSearch"},
		{schema.ReqMethodSkillsSkillnetInstall, "HandleSkillsSkillnetInstall"},
		{schema.ReqMethodSkillsSkillnetInstallStatus, "HandleSkillsSkillnetInstallStatus"},
		{schema.ReqMethodSkillsSkillnetEvaluate, "HandleSkillsSkillnetEvaluate"},
		{schema.ReqMethodSkillsClawhubGetToken, "HandleSkillsClawhubGetToken"},
		{schema.ReqMethodSkillsClawhubSetToken, "HandleSkillsClawhubSetToken"},
		{schema.ReqMethodSkillsClawhubSearch, "HandleSkillsClawhubSearch"},
		{schema.ReqMethodSkillsClawhubDownload, "HandleSkillsClawhubDownload"},
		{schema.ReqMethodSkillsTeamSkillsHubInfo, "HandleSkillsTeamSkillsHubInfo"},
		{schema.ReqMethodSkillsTeamSkillsHubInit, "HandleSkillsTeamSkillsHubInit"},
		{schema.ReqMethodSkillsTeamSkillsHubValidate, "HandleSkillsTeamSkillsHubValidate"},
		{schema.ReqMethodSkillsTeamSkillsHubPack, "HandleSkillsTeamSkillsHubPack"},
		{schema.ReqMethodSkillsTeamSkillsHubSearch, "HandleSkillsTeamSkillsHubSearch"},
		{schema.ReqMethodSkillsTeamSkillsHubInstall, "HandleSkillsTeamSkillsHubInstall"},
		{schema.ReqMethodSkillsTeamSkillsHubPublish, "HandleSkillsTeamSkillsHubPublish"},
		{schema.ReqMethodSkillsTeamSkillsHubDelete, "HandleSkillsTeamSkillsHubDelete"},
		{schema.ReqMethodSkillsEvolutionStatus, "HandleSkillsEvolutionStatus"},
		{schema.ReqMethodSkillsEvolutionGet, "HandleSkillsEvolutionGet"},
		{schema.ReqMethodSkillsEvolutionSave, "HandleSkillsEvolutionSave"},
	}
	for _, tt := range tests {
		handler, ok := SkillRouteHandler(tt.method)
		if !ok {
			t.Errorf("SkillRouteHandler(%q) 未找到", tt.method)
		}
		if handler != tt.wantMethod {
			t.Errorf("SkillRouteHandler(%q) = %q, want %q", tt.method, handler, tt.wantMethod)
		}
	}
}

// TestSkillRouteCount_30 验证 skills.* 路由总数为 30
func TestSkillRouteCount_30(t *testing.T) {
	if count := SkillRouteCount(); count != 30 {
		t.Errorf("SkillRouteCount() = %d, want 30", count)
	}
}

// TestPluginRouteHandler_所有方法 验证所有 plugins.* 路由
func TestPluginRouteHandler_所有方法(t *testing.T) {
	tests := []struct {
		method     schema.ReqMethod
		wantMethod string
	}{
		{schema.ReqMethodPluginsList, "HandlePluginsList"},
		{schema.ReqMethodPluginsInstall, "HandlePluginsInstall"},
		{schema.ReqMethodPluginsUninstall, "HandlePluginsUninstall"},
		{schema.ReqMethodPluginsEnable, "HandlePluginsEnable"},
		{schema.ReqMethodPluginsDisable, "HandlePluginsDisable"},
		{schema.ReqMethodPluginsReload, "HandlePluginsReload"},
	}
	for _, tt := range tests {
		handler, ok := PluginRouteHandler(tt.method)
		if !ok {
			t.Errorf("PluginRouteHandler(%q) 未找到", tt.method)
		}
		if handler != tt.wantMethod {
			t.Errorf("PluginRouteHandler(%q) = %q, want %q", tt.method, handler, tt.wantMethod)
		}
	}
}

// TestPluginRouteCount_6 验证 plugins.* 路由总数为 6
func TestPluginRouteCount_6(t *testing.T) {
	if count := PluginRouteCount(); count != 6 {
		t.Errorf("PluginRouteCount() = %d, want 6", count)
	}
}

// TestNeedsRebuild_触发方法 验证需要 rebuild 的方法
func TestNeedsRebuild_触发方法(t *testing.T) {
	rebuildMethods := []schema.ReqMethod{
		schema.ReqMethodSkillsInstall,
		schema.ReqMethodSkillsUninstall,
		schema.ReqMethodSkillsImportLocal,
		schema.ReqMethodSkillsToggle,
		schema.ReqMethodSkillsSkillnetInstall,
		schema.ReqMethodSkillsClawhubDownload,
		schema.ReqMethodSkillsTeamSkillsHubInstall,
		schema.ReqMethodPluginsInstall,
		schema.ReqMethodPluginsUninstall,
		schema.ReqMethodPluginsReload,
	}
	for _, method := range rebuildMethods {
		if !NeedsRebuild(method) {
			t.Errorf("NeedsRebuild(%q) 应为 true", method)
		}
	}
}

// TestNeedsRebuild_不触发方法 验证不需要 rebuild 的方法
func TestNeedsRebuild_不触发方法(t *testing.T) {
	noRebuildMethods := []schema.ReqMethod{
		schema.ReqMethodSkillsList,
		schema.ReqMethodSkillsInstalled,
		schema.ReqMethodSkillsGet,
		schema.ReqMethodSkillsMarketplaceList,
		schema.ReqMethodPluginsList,
		schema.ReqMethodPluginsEnable,
		schema.ReqMethodPluginsDisable,
	}
	for _, method := range noRebuildMethods {
		if NeedsRebuild(method) {
			t.Errorf("NeedsRebuild(%q) 应为 false", method)
		}
	}
}

// TestIsSkillMethod 验证技能方法判断
func TestIsSkillMethod(t *testing.T) {
	if !IsSkillMethod(schema.ReqMethodSkillsList) {
		t.Error("skills.list 应为 skill method")
	}
	if IsSkillMethod(schema.ReqMethodPluginsList) {
		t.Error("plugins.list 不应为 skill method")
	}
	if IsSkillMethod(schema.ReqMethodChatSend) {
		t.Error("chat.send 不应为 skill method")
	}
}

// TestIsPluginMethod 验证插件方法判断
func TestIsPluginMethod(t *testing.T) {
	if !IsPluginMethod(schema.ReqMethodPluginsList) {
		t.Error("plugins.list 应为 plugin method")
	}
	if IsPluginMethod(schema.ReqMethodSkillsList) {
		t.Error("skills.list 不应为 plugin method")
	}
	if IsPluginMethod(schema.ReqMethodChatSend) {
		t.Error("chat.send 不应为 plugin method")
	}
}

// TestSkillRouteHandler_不存在 验证不存在的路由
func TestSkillRouteHandler_不存在(t *testing.T) {
	_, ok := SkillRouteHandler(schema.ReqMethodChatSend)
	if ok {
		t.Error("chat.send 不应在 skill 路由中")
	}
}

// TestPluginRouteHandler_不存在 验证不存在的路由
func TestPluginRouteHandler_不存在(t *testing.T) {
	_, ok := PluginRouteHandler(schema.ReqMethodChatSend)
	if ok {
		t.Error("chat.send 不应在 plugin 路由中")
	}
}
