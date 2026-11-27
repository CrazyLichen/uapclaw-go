package schema

import (
	"encoding/json"
	"testing"
)

// TestAllReqMethods 验证 AllReqMethods 返回全部枚举值
// （27.4 新增 14 个配置面板方法 + 27.8 媒体/文件域十常量 + 27-A B2 workspace 四常量：192；
// 2026-11-27 +10：session.assets+项目控制面 → 203；+20：扩展设备管理 → 223；
// 24.3 +26：personal_context → 249）
func TestAllReqMethods(t *testing.T) {
	methods := AllReqMethods()
	if len(methods) != 249 {
		t.Fatalf("AllReqMethods() 返回 %d 个方法，want 249", len(methods))
	}

	// 验证无重复
	seen := make(map[ReqMethod]bool)
	for _, m := range methods {
		if seen[m] {
			t.Errorf("重复方法: %q", m)
		}
		seen[m] = true
	}

	// 验证包含关键方法
	keyMethods := []ReqMethod{
		ReqMethodInitialize,
		ReqMethodChatSend,
		ReqMethodChatResume,
		ReqMethodChatCancel,
		ReqMethodChatAnswer,
		ReqMethodHistoryGet,
		ReqMethodSessionList,
		ReqMethodConfigGet,
	}
	for _, km := range keyMethods {
		if !seen[km] {
			t.Errorf("缺少关键方法: %q", km)
		}
	}
}

// TestParseReqMethod_合法值 验证解析合法值成功
func TestParseReqMethod_合法值(t *testing.T) {
	tests := []struct {
		input string
		want  ReqMethod
	}{
		{"initialize", ReqMethodInitialize},
		{"chat.send", ReqMethodChatSend},
		{"chat.interrupt", ReqMethodChatCancel},
		{"chat.user_answer", ReqMethodChatAnswer},
		{"config.get", ReqMethodConfigGet},
		{"session.list", ReqMethodSessionList},
		{"skills.list", ReqMethodSkillsList},
		{"permissions.tools.get", ReqMethodPermissionsToolsGet},
		{"schedule.create", ReqMethodScheduleCreate},
		{"harness.packages.scan", ReqMethodHarnessPackagesScan},
	}
	for _, tt := range tests {
		got, err := ParseReqMethod(tt.input)
		if err != nil {
			t.Errorf("ParseReqMethod(%q) 返回错误: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseReqMethod(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestParseReqMethod_非法值 验证解析非法值返回错误
func TestParseReqMethod_非法值(t *testing.T) {
	invalidInputs := []string{
		"",
		"unknown.method",
		"chat.",
		".send",
		"CHAT_SEND",
		"chat/send",
		"foo.bar.baz.qux",
	}
	for _, input := range invalidInputs {
		_, err := ParseReqMethod(input)
		if err == nil {
			t.Errorf("ParseReqMethod(%q) 应返回错误，但返回 nil", input)
		}
	}
}

// TestIsValid 验证 IsValid 对合法/非法值的判断
func TestIsValid(t *testing.T) {
	// 合法值
	if !IsValid("chat.send") {
		t.Error(`IsValid("chat.send") = false, want true`)
	}
	if !IsValid("initialize") {
		t.Error(`IsValid("initialize") = false, want true`)
	}
	if !IsValid("permissions.approval_overrides.delete") {
		t.Error(`IsValid("permissions.approval_overrides.delete") = false, want true`)
	}

	// 非法值
	if IsValid("") {
		t.Error(`IsValid("") = true, want false`)
	}
	if IsValid("nonexistent.method") {
		t.Error(`IsValid("nonexistent.method") = true, want false`)
	}
}

// TestReqMethodString 验证 String() 返回原始字符串值
func TestReqMethodString(t *testing.T) {
	if got := ReqMethodChatSend.String(); got != "chat.send" {
		t.Errorf("ReqMethodChatSend.String() = %q, want %q", got, "chat.send")
	}
	if got := ReqMethodChatCancel.String(); got != "chat.interrupt" {
		t.Errorf("ReqMethodChatCancel.String() = %q, want %q", got, "chat.interrupt")
	}
}

// TestReqMethodGoString 验证 GoString() 格式
func TestReqMethodGoString(t *testing.T) {
	if got := ReqMethodChatSend.GoString(); got != `schema.ReqMethod("chat.send")` {
		t.Errorf("ReqMethodChatSend.GoString() = %q, want %q", got, `schema.ReqMethod("chat.send")`)
	}
}

// TestReqMethodJSON序列化往返 验证 JSON marshal/unmarshal 往返一致
func TestReqMethodJSON序列化往返(t *testing.T) {
	methods := []ReqMethod{
		ReqMethodInitialize,
		ReqMethodChatSend,
		ReqMethodChatCancel,
		ReqMethodSessionCreate,
		ReqMethodScheduleDelete,
	}
	for _, m := range methods {
		data, err := json.Marshal(m)
		if err != nil {
			t.Errorf("json.Marshal(%q) 错误: %v", m, err)
			continue
		}
		var got ReqMethod
		if err := json.Unmarshal(data, &got); err != nil {
			t.Errorf("json.Unmarshal(%s) 错误: %v", data, err)
			continue
		}
		if got != m {
			t.Errorf("JSON 往返: got %q, want %q", got, m)
		}
	}
}

// TestReqMethod常量值与Python对齐 验证核心常量字符串值与 Python ReqMethod 完全对齐
func TestReqMethod常量值与Python对齐(t *testing.T) {
	// Python: jiuwenswarm/common/schema/message.py (ReqMethod)
	tests := []struct {
		got  ReqMethod
		want string
	}{
		{ReqMethodInitialize, "initialize"},
		{ReqMethodACPToolResponse, "acp.tool_response"},
		{ReqMethodChatSend, "chat.send"},
		{ReqMethodChatResume, "chat.resume"},
		{ReqMethodChatCancel, "chat.interrupt"},
		{ReqMethodChatAnswer, "chat.user_answer"},
		{ReqMethodCommandAddDir, "command.add_dir"},
		{ReqMethodCommandChrome, "command.chrome"},
		{ReqMethodCommandCompact, "command.compact"},
		{ReqMethodCommandContext, "command.context"},
		{ReqMethodCommandRecap, "command.recap"},
		{ReqMethodCommandDiff, "command.diff"},
		{ReqMethodCommandMCP, "command.mcp"},
		{ReqMethodCommandModel, "command.model"},
		{ReqMethodCommandResume, "command.resume"},
		{ReqMethodCommandSandbox, "command.sandbox"},
		{ReqMethodCommandSession, "command.session"},
		{ReqMethodCommandStatus, "command.status"},
		{ReqMethodConfigGet, "config.get"},
		{ReqMethodConfigSet, "config.set"},
		{ReqMethodChannelGet, "channel.get"},
		{ReqMethodSessionList, "session.list"},
		{ReqMethodSessionCreate, "session.create"},
		{ReqMethodSessionSwitch, "session.switch"},
		{ReqMethodSessionDelete, "session.delete"},
		{ReqMethodSessionRename, "session.rename"},
		{ReqMethodSessionFork, "session.fork"},
		{ReqMethodSessionRewind, "session.rewind"},
		{ReqMethodSessionRewindAndRestore, "session.rewind_and_restore"},
		{ReqMethodSessionRewindContext, "session.rewind_context"},
		{ReqMethodSessionRestoreFiles, "session.restore_files"},
		{ReqMethodHistoryGet, "history.get"},
		{ReqMethodHistoryListTurns, "history.list_turns"},
		{ReqMethodTeamDelete, "team.delete"},
		{ReqMethodTeamSnapshot, "team.snapshot"},
		{ReqMethodTeamHistoryGet, "team.history.get"},
		{ReqMethodPathGet, "path.get"},
		{ReqMethodPathSet, "path.set"},
		{ReqMethodFilesList, "files.list"},
		{ReqMethodFilesGet, "files.get"},
		{ReqMethodTTSSynthesize, "tts.synthesize"},
		{ReqMethodMemoryCompute, "memory.compute"},
		{ReqMethodBrowserStart, "browser.start"},
		{ReqMethodBrowserRuntimeRestart, "browser.runtime_restart"},
		{ReqMethodConfigCacheClear, "config.cache_clear"},
		{ReqMethodAgentReloadConfig, "agent.reload_config"},
		{ReqMethodAgentsList, "agents.list"},
		{ReqMethodAgentsGet, "agents.get"},
		{ReqMethodAgentsCreate, "agents.create"},
		{ReqMethodAgentsUpdate, "agents.update"},
		{ReqMethodAgentsDelete, "agents.delete"},
		{ReqMethodAgentsEnable, "agents.enable"},
		{ReqMethodAgentsDisable, "agents.disable"},
		{ReqMethodAgentsToolsList, "agents.tools_list"},
		{ReqMethodSkillsMarketplaceList, "skills.marketplace.list"},
		{ReqMethodSkillsList, "skills.list"},
		{ReqMethodSkillsInstalled, "skills.installed"},
		{ReqMethodSkillsGet, "skills.get"},
		{ReqMethodSkillsToggle, "skills.toggle"},
		{ReqMethodSkillsInstall, "skills.install"},
		{ReqMethodSkillsImportLocal, "skills.import_local"},
		{ReqMethodSkillsMarketplaceAdd, "skills.marketplace.add"},
		{ReqMethodSkillsMarketplaceRemove, "skills.marketplace.remove"},
		{ReqMethodSkillsMarketplaceToggle, "skills.marketplace.toggle"},
		{ReqMethodSkillsUninstall, "skills.uninstall"},
		{ReqMethodSkillsSkillnetSearch, "skills.skillnet.search"},
		{ReqMethodSkillsSkillnetInstall, "skills.skillnet.install"},
		{ReqMethodSkillsSkillnetInstallStatus, "skills.skillnet.install_status"},
		{ReqMethodSkillsSkillnetEvaluate, "skills.skillnet.evaluate"},
		{ReqMethodSkillsClawhubGetToken, "skills.clawhub.get_token"},
		{ReqMethodSkillsClawhubSetToken, "skills.clawhub.set_token"},
		{ReqMethodSkillsClawhubSearch, "skills.clawhub.search"},
		{ReqMethodSkillsClawhubDownload, "skills.clawhub.download"},
		{ReqMethodSkillsTeamSkillsHubInfo, "skills.teamskillshub.info"},
		{ReqMethodSkillsTeamSkillsHubInit, "skills.teamskillshub.init"},
		{ReqMethodSkillsTeamSkillsHubValidate, "skills.teamskillshub.validate"},
		{ReqMethodSkillsTeamSkillsHubPack, "skills.teamskillshub.pack"},
		{ReqMethodSkillsTeamSkillsHubSearch, "skills.teamskillshub.search"},
		{ReqMethodSkillsTeamSkillsHubInstall, "skills.teamskillshub.install"},
		{ReqMethodSkillsTeamSkillsHubPublish, "skills.teamskillshub.publish"},
		{ReqMethodSkillsTeamSkillsHubDelete, "skills.teamskillshub.delete"},
		{ReqMethodSkillsEvolutionStatus, "skills.evolution.status"},
		{ReqMethodSkillsEvolutionGet, "skills.evolution.get"},
		{ReqMethodSkillsEvolutionSave, "skills.evolution.save"},
		{ReqMethodPluginsList, "plugins.list"},
		{ReqMethodPluginsInstall, "plugins.install"},
		{ReqMethodPluginsUninstall, "plugins.uninstall"},
		{ReqMethodPluginsEnable, "plugins.enable"},
		{ReqMethodPluginsDisable, "plugins.disable"},
		{ReqMethodPluginsReload, "plugins.reload"},
		// 技能开发
		{ReqMethodSkilldevStart, "skilldev.start"},
		{ReqMethodSkilldevRespond, "skilldev.respond"},
		{ReqMethodSkilldevStatus, "skilldev.status"},
		{ReqMethodSkilldevDownload, "skilldev.download"},
		{ReqMethodSkilldevCancel, "skilldev.cancel"},
		{ReqMethodSkilldevFileList, "skilldev.file.list"},
		{ReqMethodSkilldevFileRead, "skilldev.file.read"},
		{ReqMethodExtensionsList, "extensions.list"},
		{ReqMethodExtensionsImport, "extensions.import"},
		{ReqMethodExtensionsDelete, "extensions.delete"},
		{ReqMethodExtensionsToggle, "extensions.toggle"},
		{ReqMethodHooksList, "hooks.list"},
		{ReqMethodHeartbeatGetConf, "heartbeat.get_conf"},
		{ReqMethodHeartbeatSetConf, "heartbeat.set_conf"},
		{ReqMethodPermissionsToolsGet, "permissions.tools.get"},
		{ReqMethodPermissionsToolsSet, "permissions.tools.set"},
		{ReqMethodPermissionsToolsUpdate, "permissions.tools.update"},
		{ReqMethodPermissionsToolsDelete, "permissions.tools.delete"},
		{ReqMethodPermissionsRulesGet, "permissions.rules.get"},
		{ReqMethodPermissionsRulesCreate, "permissions.rules.create"},
		{ReqMethodPermissionsRulesUpdate, "permissions.rules.update"},
		{ReqMethodPermissionsRulesDelete, "permissions.rules.delete"},
		{ReqMethodPermissionsApprovalOverridesGet, "permissions.approval_overrides.get"},
		{ReqMethodPermissionsApprovalOverridesDelete, "permissions.approval_overrides.delete"},
		{ReqMethodChannelFeishuGetConf, "channel.feishu.get_conf"},
		{ReqMethodChannelFeishuSetConf, "channel.feishu.set_conf"},
		{ReqMethodChannelXiaoyiGetConf, "channel.xiaoyi.get_conf"},
		{ReqMethodChannelXiaoyiSetConf, "channel.xiaoyi.set_conf"},
		{ReqMethodChannelTelegramGetConf, "channel.telegram.get_conf"},
		{ReqMethodChannelTelegramSetConf, "channel.telegram.set_conf"},
		{ReqMethodChannelDingtalkGetConf, "channel.dingtalk.get_conf"},
		{ReqMethodChannelDingtalkSetConf, "channel.dingtalk.set_conf"},
		{ReqMethodChannelWhatsAppGetConf, "channel.whatsapp.get_conf"},
		{ReqMethodChannelWhatsAppSetConf, "channel.whatsapp.set_conf"},
		{ReqMethodChannelWechatGetConf, "channel.wechat.get_conf"},
		{ReqMethodChannelWechatSetConf, "channel.wechat.set_conf"},
		{ReqMethodChannelWechatGetLoginUI, "channel.wechat.get_login_ui"},
		{ReqMethodChannelWechatUnbind, "channel.wechat.unbind"},
		{ReqMethodUpdaterGetStatus, "updater.get_status"},
		{ReqMethodUpdaterCheck, "updater.check"},
		{ReqMethodUpdaterDownload, "updater.download"},
		{ReqMethodUpdaterGetConf, "updater.get_conf"},
		{ReqMethodUpdaterSetConf, "updater.set_conf"},
		{ReqMethodHarnessPackagesGet, "harness.packages.get"},
		{ReqMethodHarnessPackagesScan, "harness.packages.scan"},
		{ReqMethodHarnessPackagesActivate, "harness.packages.activate"},
		{ReqMethodHarnessPackagesDeactivate, "harness.packages.deactivate"},
		{ReqMethodHarnessPackagesDelete, "harness.packages.delete"},
		{ReqMethodHarnessPackagesImport, "harness.packages.import"},
		{ReqMethodHarnessPackagesExport, "harness.packages.export"},
		{ReqMethodScheduleCheckConfig, "schedule.check_config"},
		{ReqMethodScheduleUpdateConfig, "schedule.update_config"},
		{ReqMethodScheduleCreate, "schedule.create"},
		{ReqMethodScheduleRun, "schedule.run"},
		{ReqMethodScheduleList, "schedule.list"},
		{ReqMethodScheduleStatus, "schedule.status"},
		{ReqMethodScheduleLogs, "schedule.logs"},
		{ReqMethodScheduleCancel, "schedule.cancel"},
		{ReqMethodScheduleDelete, "schedule.delete"},
		{ReqMethodRecommendation, "recommendation.job"},
		{ReqMethodPromptSuggestionCancel, "prompt_suggestion.cancel"},
	}
	for _, tt := range tests {
		if string(tt.got) != tt.want {
			t.Errorf("常量值 = %q, want %q", tt.got, tt.want)
		}
	}
	// 验证测试覆盖全部 151 个常量（149 + 10.7.2 新增 2）
	if len(tests) != 151 {
		t.Errorf("Python 对齐测试覆盖 %d 个常量，want 149", len(tests))
	}
}

// TestReqMethod_推荐面板两方法 10.7.2 新增两方法的值与解析。
// Python: message.py:59-60
func TestReqMethod_推荐面板两方法(t *testing.T) {
	if string(ReqMethodRecommendation) != "recommendation.job" {
		t.Errorf("ReqMethodRecommendation = %q, want recommendation.job", string(ReqMethodRecommendation))
	}
	if string(ReqMethodPromptSuggestionCancel) != "prompt_suggestion.cancel" {
		t.Errorf("ReqMethodPromptSuggestionCancel = %q, want prompt_suggestion.cancel", string(ReqMethodPromptSuggestionCancel))
	}
	m, err := ParseReqMethod("recommendation.job")
	if err != nil || m != ReqMethodRecommendation {
		t.Errorf("ParseReqMethod(recommendation.job) = %q, %v", string(m), err)
	}
	m, err = ParseReqMethod("prompt_suggestion.cancel")
	if err != nil || m != ReqMethodPromptSuggestionCancel {
		t.Errorf("ParseReqMethod(prompt_suggestion.cancel) = %q, %v", string(m), err)
	}
}

// TestReqMethod_MediaWireValues 媒体/文件域十常量 wire 值逐字断言。
// Python: gateway/channel_manager/web/http_routes/mapped.py:796-811 +
// server/runtime/gateway_adapter/workspace_file_adapter.py:109-127 methods frozenset。
// im.file_persist 与 workspace.* 四值不注册（spec D-27.8-1/2：归 IM 渠道域与企业版挂账）。
func TestReqMethod_MediaWireValues(t *testing.T) {
	cases := map[ReqMethod]string{
		ReqMethodMediaPersist:              "media.persist",
		ReqMethodMediaDiscard:              "media.discard",
		ReqMethodDocumentPersist:           "document.persist",
		ReqMethodDocumentFormats:           "document.formats",
		ReqMethodPathSelectDirectory:       "path.select_directory",
		ReqMethodPathSelectFiles:           "path.select_files",
		ReqMethodFileImportURL:             "file.import_url",
		ReqMethodFileUploadChunk:           "file.upload_chunk",
		ReqMethodFileReadUploadChunk:       "file.read_upload_chunk",
		ReqMethodFileDownloadVerifiedChunk: "file.download_verified_chunk",
	}
	for m, want := range cases {
		if string(m) != want {
			t.Errorf("ReqMethod %q != 期望 wire 值 %q", string(m), want)
		}
	}
}

// TestReqMethod_PersonalContextWireValues 锁定 24.3 personal_context 26 个 method 的 wire 字面值
// （Python ReqMethod 枚举；run_all/stream_tree 已在 jiuwenswarm gateway 测试 test_app_web_handlers.py:4766/4772 实证）。
func TestReqMethod_PersonalContextWireValues(t *testing.T) {
	cases := map[ReqMethod]string{
		ReqMethodPersonalContextRuntimeStatus:               "personal_context.runtime.status",
		ReqMethodPersonalContextRuntimeStartCollection:      "personal_context.runtime.start_collection",
		ReqMethodPersonalContextRuntimeStopCollection:       "personal_context.runtime.stop_collection",
		ReqMethodPersonalContextRuntimeStartAgentUse:        "personal_context.runtime.start_agent_use",
		ReqMethodPersonalContextRuntimeStopAgentUse:         "personal_context.runtime.stop_agent_use",
		ReqMethodPersonalContextRuntimeSetMasterEnabled:     "personal_context.runtime.set_master_enabled",
		ReqMethodPersonalContextRuntimeGetConfig:            "personal_context.runtime.get_config",
		ReqMethodPersonalContextRuntimePatchConfig:          "personal_context.runtime.patch_config",
		ReqMethodPersonalContextRuntimeSelectModel:          "personal_context.runtime.select_model",
		ReqMethodPersonalContextFetchListServices:           "personal_context.fetch.list_services",
		ReqMethodPersonalContextFetchCreateService:          "personal_context.fetch.create_service",
		ReqMethodPersonalContextFetchDeleteService:          "personal_context.fetch.delete_service",
		ReqMethodPersonalContextFetchPatchService:           "personal_context.fetch.patch_service",
		ReqMethodPersonalContextFetchStartService:           "personal_context.fetch.start_service",
		ReqMethodPersonalContextFetchStopService:            "personal_context.fetch.stop_service",
		ReqMethodPersonalContextFetchRunAll:                 "personal_context.fetch.run_all",
		ReqMethodPersonalContextFetchRunOne:                 "personal_context.fetch.run_one",
		ReqMethodPersonalContextFetchStopRun:                "personal_context.fetch.stop_run",
		ReqMethodPersonalContextFetchGetRunStatus:           "personal_context.fetch.get_run_status",
		ReqMethodPersonalContextFetchGetAuthorizationStatus: "personal_context.fetch.get_authorization_status",
		ReqMethodPersonalContextFetchAuthorizeProvider:      "personal_context.fetch.authorize_provider",
		ReqMethodPersonalContextContextStreamGraph:          "personal_context.context.stream_graph",
		ReqMethodPersonalContextContextStreamTree:           "personal_context.context.stream_tree",
		ReqMethodPersonalContextContextSearchPages:          "personal_context.context.search_pages",
		ReqMethodPersonalContextContextGetNode:              "personal_context.context.get_node",
		ReqMethodPersonalContextContextGetSource:            "personal_context.context.get_source",
	}
	for m, want := range cases {
		if string(m) != want {
			t.Errorf("ReqMethod %q != 期望 wire 值 %q", string(m), want)
		}
	}
}
