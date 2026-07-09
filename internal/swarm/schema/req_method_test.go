package schema

import (
	"encoding/json"
	"testing"
)

// TestAllReqMethods 验证 AllReqMethods 返回全部 142 个枚举值
func TestAllReqMethods(t *testing.T) {
	methods := AllReqMethods()
	if len(methods) != 149 {
		t.Fatalf("AllReqMethods() 返回 %d 个方法，want 149", len(methods))
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
	// 对应 Python: jiuwenswarm/common/schema/message.py (ReqMethod)
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
	}
	for _, tt := range tests {
		if string(tt.got) != tt.want {
			t.Errorf("常量值 = %q, want %q", tt.got, tt.want)
		}
	}
	// 验证测试覆盖全部 149 个常量
	if len(tests) != 149 {
		t.Errorf("Python 对齐测试覆盖 %d 个常量，want 149", len(tests))
	}
}
