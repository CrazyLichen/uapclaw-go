package sections

import (
	"os"
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildIdentitySection_基本验证 测试身份节基本属性
func TestBuildIdentitySection_基本验证(t *testing.T) {
	s := BuildIdentitySection()
	if s.Name != SectionIdentity {
		t.Errorf("Name = %q, want %q", s.Name, SectionIdentity)
	}
	if s.Priority != 10 {
		t.Errorf("Priority = %d, want 10", s.Priority)
	}
	if s.Content["cn"] == "" {
		t.Error("Content[cn] 不应为空")
	}
	if s.Content["en"] == "" {
		t.Error("Content[en] 不应为空")
	}
}

// TestBuildSafetySection_基本验证 测试安全节基本属性
func TestBuildSafetySection_基本验证(t *testing.T) {
	s := BuildSafetySection()
	if s.Name != SectionSafety {
		t.Errorf("Name = %q, want %q", s.Name, SectionSafety)
	}
	if s.Priority != 20 {
		t.Errorf("Priority = %d, want 20", s.Priority)
	}
	if s.Content["cn"] == "" {
		t.Error("Content[cn] 不应为空")
	}
	if s.Content["en"] == "" {
		t.Error("Content[en] 不应为空")
	}
	if !strings.Contains(s.Content["cn"], "安全原则") {
		t.Error("Content[cn] 应包含 '安全原则'")
	}
	if !strings.Contains(s.Content["en"], "Safety") {
		t.Error("Content[en] 应包含 'Safety'")
	}
}

// TestBuildToolsSection_基本验证 测试工具节基本属性
func TestBuildToolsSection_基本验证(t *testing.T) {
	tools := map[string]string{
		"bash": "执行 Shell 命令",
		"glob": "文件搜索",
		"grep": "内容搜索",
	}
	s := BuildToolsSection(tools, "cn")
	if s == nil {
		t.Fatal("期望 BuildToolsSection 返回非 nil")
	}
	if s.Name != SectionTools {
		t.Errorf("Name = %q, want %q", s.Name, SectionTools)
	}
	if s.Priority != 30 {
		t.Errorf("Priority = %d, want 30", s.Priority)
	}
}

// TestBuildContextSection_基本验证 测试上下文节基本属性
func TestBuildContextSection_基本验证(t *testing.T) {
	s := BuildContextSection(map[string]string{}, "cn")
	if s.Name != SectionContext {
		t.Errorf("Name = %q, want %q", s.Name, SectionContext)
	}
	if s.Priority != 80 {
		t.Errorf("Priority = %d, want 80", s.Priority)
	}
}

// TestBuildContextSection_包含文件内容 测试上下文节包含文件内容
func TestBuildContextSection_包含文件内容(t *testing.T) {
	files := map[string]string{
		"AGENT.md": "agent content",
	}
	s := BuildContextSection(files, "cn")
	if !strings.Contains(s.Content["cn"], "agent content") {
		t.Error("Content[cn] 应包含 'agent content'")
	}
}

// TestBuildSkillsSection_所有模式 测试技能节所有模式
func TestBuildSkillsSection_所有模式(t *testing.T) {
	for _, mode := range []string{"all", "auto_list", "no_skill"} {
		for _, lang := range []string{"cn", "en"} {
			s := BuildSkillsSection(mode, []string{"skill1"}, lang)
			if s.Name != SectionSkills {
				t.Errorf("mode=%s lang=%s: Name = %q, want %q", mode, lang, s.Name, SectionSkills)
			}
			if s.Priority != 40 {
				t.Errorf("mode=%s lang=%s: Priority = %d, want 40", mode, lang, s.Priority)
			}
			if s.Content[lang] == "" {
				t.Errorf("mode=%s lang=%s: Content 不应为空", mode, lang)
			}
		}
	}
}

// TestBuildSkillsSection_all模式带技能 测试 all 模式带技能路径
func TestBuildSkillsSection_all模式带技能(t *testing.T) {
	s := BuildSkillsSection("all", []string{"1. foo: bar", "2. baz: qux"}, "cn")
	if !strings.Contains(s.Content["cn"], "foo") {
		t.Error("all 模式应包含技能名称")
	}
}

// TestBuildSkillsSection_all模式空技能 测试 all 模式空技能列表回退到 no_skill
func TestBuildSkillsSection_all模式空技能(t *testing.T) {
	s := BuildSkillsSection("all", nil, "cn")
	if !strings.Contains(s.Content["cn"], "没有选择任何技能") {
		t.Error("all 模式空技能列表应回退到 no_skill 提示")
	}
}

// TestBuildMemorySection_主动模式 测试记忆节主动模式
func TestBuildMemorySection_主动模式(t *testing.T) {
	for _, lang := range []string{"cn", "en"} {
		s := BuildMemorySection("proactive", "2026-01-01", lang)
		if s.Name != SectionMemory {
			t.Errorf("lang=%s: Name = %q, want %q", lang, s.Name, SectionMemory)
		}
		if s.Priority != 50 {
			t.Errorf("lang=%s: Priority = %d, want 50", lang, s.Priority)
		}
		if s.Content[lang] == "" {
			t.Errorf("lang=%s: Content 不应为空", lang)
		}
	}
}

// TestBuildMemorySection_不同模式输出不同 测试不同模式产生不同输出
func TestBuildMemorySection_不同模式输出不同(t *testing.T) {
	proactive := BuildMemorySection("proactive", "2026-01-01", "cn")
	inactive := BuildMemorySection("inactive", "2026-01-01", "cn")
	readOnly := BuildMemorySection("read_only", "2026-01-01", "cn")

	if proactive.Content["cn"] == inactive.Content["cn"] {
		t.Error("proactive 和 inactive 模式应产生不同输出")
	}
	if proactive.Content["cn"] == readOnly.Content["cn"] {
		t.Error("proactive 和 read_only 模式应产生不同输出")
	}
	if inactive.Content["cn"] == readOnly.Content["cn"] {
		t.Error("inactive 和 read_only 模式应产生不同输出")
	}
}

// TestBuildMemorySection_只读模式包含关键词 测试只读模式包含关键词
func TestBuildMemorySection_只读模式包含关键词(t *testing.T) {
	s := BuildMemorySection("read_only", "2026-01-01", "cn")
	if !strings.Contains(s.Content["cn"], "只读模式") {
		t.Error("read_only 模式 Content[cn] 应包含 '只读模式'")
	}
}

// TestBuildMemorySection_主动模式包含日期 测试主动模式包含日期替换
func TestBuildMemorySection_主动模式包含日期(t *testing.T) {
	s := BuildMemorySection("proactive", "2026-07-04", "cn")
	if !strings.Contains(s.Content["cn"], "2026-07-04") {
		t.Error("proactive 模式应包含替换后的日期")
	}
}

// TestBuildExternalMemorySection_非空 测试外部记忆节非空内容
func TestBuildExternalMemorySection_非空(t *testing.T) {
	s := BuildExternalMemorySection("ext content", "cn")
	if s == nil {
		t.Fatal("非空 promptBlock 应返回非 nil")
	}
	if s.Name != SectionExternalMemory {
		t.Errorf("Name = %q, want %q", s.Name, SectionExternalMemory)
	}
	if s.Priority != 55 {
		t.Errorf("Priority = %d, want 55", s.Priority)
	}
}

// TestBuildExternalMemorySection_空 测试外部记忆节空内容返回 nil
func TestBuildExternalMemorySection_空(t *testing.T) {
	s := BuildExternalMemorySection("", "cn")
	if s != nil {
		t.Error("空 promptBlock 应返回 nil")
	}
}

// TestBuildWorkspaceSection_基本验证 测试工作空间节基本属性
func TestBuildWorkspaceSection_基本验证(t *testing.T) {
	s := BuildWorkspaceSection("/home/user", "", "cn")
	if s.Name != SectionWorkspace {
		t.Errorf("Name = %q, want %q", s.Name, SectionWorkspace)
	}
	if s.Priority != 70 {
		t.Errorf("Priority = %d, want 70", s.Priority)
	}
	if !strings.Contains(s.Content["cn"], "/home/user") {
		t.Error("Content[cn] 应包含工作目录路径")
	}
}

// TestBuildWorkspaceSection_双语言 测试工作空间节双语言
func TestBuildWorkspaceSection_双语言(t *testing.T) {
	cn := BuildWorkspaceSection("/root", "", "cn")
	en := BuildWorkspaceSection("/root", "", "en")
	if cn.Content["cn"] == "" || en.Content["en"] == "" {
		t.Error("双语言内容均不应为空")
	}
	if !strings.Contains(cn.Content["cn"], "工作目录") {
		t.Error("CN 版本应包含 '工作目录'")
	}
	if !strings.Contains(en.Content["en"], "working directory") {
		t.Error("EN 版本应包含 'working directory'")
	}
}

// TestBuildNavigationSection_基本验证 测试工具导航节基本属性
func TestBuildNavigationSection_基本验证(t *testing.T) {
	s := BuildNavigationSection([]string{"entry1", "entry2"}, "cn")
	if s.Name != SectionToolNavigation {
		t.Errorf("Name = %q, want %q", s.Name, SectionToolNavigation)
	}
	if s.Priority != 70 {
		t.Errorf("Priority = %d, want 70", s.Priority)
	}
	if !strings.Contains(s.Content["cn"], "entry1") {
		t.Error("Content[cn] 应包含导航条目")
	}
}

// TestBuildNavigationSection_空条目 测试工具导航节空条目
func TestBuildNavigationSection_空条目(t *testing.T) {
	s := BuildNavigationSection(nil, "cn")
	if !strings.Contains(s.Content["cn"], "当前无可展示的导航条目") {
		t.Error("空条目应包含提示信息")
	}
}

// TestBuildProgressiveToolRulesSection_基本验证 测试渐进式工具规则节
func TestBuildProgressiveToolRulesSection_基本验证(t *testing.T) {
	s := BuildProgressiveToolRulesSection("cn")
	if s.Name != SectionProgressiveToolRules {
		t.Errorf("Name = %q, want %q", s.Name, SectionProgressiveToolRules)
	}
	if s.Priority != 75 {
		t.Errorf("Priority = %d, want 75", s.Priority)
	}
	if s.Content["cn"] == "" {
		t.Error("Content[cn] 不应为空")
	}
}

// TestBuildHeartbeatSection_基本验证 测试心跳节基本属性
func TestBuildHeartbeatSection_基本验证(t *testing.T) {
	s := BuildHeartbeatSection("test heartbeat", "cn")
	if s.Name != SectionHeartbeat {
		t.Errorf("Name = %q, want %q", s.Name, SectionHeartbeat)
	}
	if s.Priority != 80 {
		t.Errorf("Priority = %d, want 80", s.Priority)
	}
}

// TestBuildHeartbeatSection_替换占位符 测试心跳节替换 heartbeat_section 占位符
func TestBuildHeartbeatSection_替换占位符(t *testing.T) {
	s := BuildHeartbeatSection("my task content", "cn")
	if !strings.Contains(s.Content["cn"], "my task content") {
		t.Error("Content[cn] 应包含替换后的心跳内容")
	}
	if strings.Contains(s.Content["cn"], "{heartbeat_section}") {
		t.Error("Content[cn] 不应包含未替换的占位符")
	}
}

// TestBuildHeartbeatSection_空内容 测试心跳节空内容
func TestBuildHeartbeatSection_空内容(t *testing.T) {
	s := BuildHeartbeatSection("", "cn")
	if strings.Contains(s.Content["cn"], "{heartbeat_section}") {
		t.Error("空心跳内容也应替换占位符")
	}
}

// TestBuildCodingMemorySection_读写模式 测试编码记忆节读写模式
func TestBuildCodingMemorySection_读写模式(t *testing.T) {
	s := BuildCodingMemorySection("coding_memory/", false, "cn")
	if s.Priority != 85 {
		t.Errorf("Priority = %d, want 85", s.Priority)
	}
	if s.Content["cn"] == "" {
		t.Error("Content[cn] 不应为空")
	}
}

// TestCodingMemorySection_只读模式包含关键词 测试编码记忆节只读模式包含"只读"
func TestCodingMemorySection_只读模式包含关键词(t *testing.T) {
	s := BuildCodingMemorySection("coding_memory/", true, "cn")
	if !strings.Contains(s.Content["cn"], "只读") {
		t.Error("readOnly=true 时 Content[cn] 应包含 '只读'")
	}
}

// TestBuildCodingMemorySection_替换目录占位符 测试编码记忆节替换目录占位符
func TestBuildCodingMemorySection_替换目录占位符(t *testing.T) {
	s := BuildCodingMemorySection("/app/mem", false, "cn")
	if !strings.Contains(s.Content["cn"], "/app/mem") {
		t.Error("Content[cn] 应包含替换后的记忆目录路径")
	}
	if strings.Contains(s.Content["cn"], "{memory_dir}") {
		t.Error("Content[cn] 不应包含未替换的占位符")
	}
}

// TestBuildSessionToolsSection_基本验证 测试会话工具节
func TestBuildSessionToolsSection_基本验证(t *testing.T) {
	for _, lang := range []string{"cn", "en"} {
		s := BuildSessionToolsSection(lang)
		if s.Name != SectionSessionTools {
			t.Errorf("lang=%s: Name = %q, want %q", lang, s.Name, SectionSessionTools)
		}
		if s.Priority != 85 {
			t.Errorf("lang=%s: Priority = %d, want 85", lang, s.Priority)
		}
		if s.Content[lang] == "" {
			t.Errorf("lang=%s: Content 不应为空", lang)
		}
	}
}

// TestBuildPlanModeSection_基本验证 测试计划模式节基本属性
func TestBuildPlanModeSection_基本验证(t *testing.T) {
	s := BuildPlanModeSection("/tmp/plan.md", true, "cn")
	if s.Name != SectionModeInstructions {
		t.Errorf("Name = %q, want %q", s.Name, SectionModeInstructions)
	}
	if s.Priority != 85 {
		t.Errorf("Priority = %d, want 85", s.Priority)
	}
}

// TestBuildPlanModeSection_替换占位符 测试计划模式节替换占位符
func TestBuildPlanModeSection_替换占位符(t *testing.T) {
	s := BuildPlanModeSection("/tmp/plan.md", true, "cn")
	// enter_plan_mode_status 和 plan_file_info 已被内部函数生成并替换
	if strings.Contains(s.Content["cn"], "{enter_plan_mode_status}") {
		t.Error("Content[cn] 不应包含未替换的 {enter_plan_mode_status}")
	}
	if strings.Contains(s.Content["cn"], "{plan_file_info}") {
		t.Error("Content[cn] 不应包含未替换的 {plan_file_info}")
	}
	if !strings.Contains(s.Content["cn"], "/tmp/plan.md") {
		t.Error("Content[cn] 应包含计划文件路径")
	}
}

// TestBuildTaskToolSection_基本验证 测试任务工具节
func TestBuildTaskToolSection_基本验证(t *testing.T) {
	for _, lang := range []string{"cn", "en"} {
		s := BuildTaskToolSection(lang)
		if s.Name != SectionTaskTool {
			t.Errorf("lang=%s: Name = %q, want %q", lang, s.Name, SectionTaskTool)
		}
		if s.Priority != 85 {
			t.Errorf("lang=%s: Priority = %d, want 85", lang, s.Priority)
		}
		if s.Content[lang] == "" {
			t.Errorf("lang=%s: Content 不应为空", lang)
		}
	}
}

// TestBuildCompletionSignalSection_基本验证 测试完成信号节
func TestBuildCompletionSignalSection_基本验证(t *testing.T) {
	s := BuildCompletionSignalSection("DONE", "cn")
	if s.Name != SectionCompletionSignal {
		t.Errorf("Name = %q, want %q", s.Name, SectionCompletionSignal)
	}
	if s.Priority != 85 {
		t.Errorf("Priority = %d, want 85", s.Priority)
	}
}

// TestBuildCompletionSignalSection_替换占位符 测试完成信号节替换 promise 占位符
func TestBuildCompletionSignalSection_替换占位符(t *testing.T) {
	s := BuildCompletionSignalSection("MY_TOKEN", "cn")
	if !strings.Contains(s.Content["cn"], "MY_TOKEN") {
		t.Error("Content[cn] 应包含替换后的 promise 令牌")
	}
	if strings.Contains(s.Content["cn"], "{promise}") {
		t.Error("Content[cn] 不应包含未替换的占位符")
	}
	if !strings.Contains(s.Content["cn"], "<promise>MY_TOKEN</promise>") {
		t.Error("Content[cn] 应包含完整的 promise 标签")
	}
}

// TestBuildTodoSection_基本验证 测试待办节
func TestBuildTodoSection_基本验证(t *testing.T) {
	for _, lang := range []string{"cn", "en"} {
		s := BuildTodoSection("", lang)
		if s.Name != SectionTodo {
			t.Errorf("lang=%s: Name = %q, want %q", lang, s.Name, SectionTodo)
		}
		if s.Priority != 90 {
			t.Errorf("lang=%s: Priority = %d, want 90", lang, s.Priority)
		}
		if s.Content[lang] == "" {
			t.Errorf("lang=%s: Content 不应为空", lang)
		}
	}
}

// TestBuildProgressReminderUserPrompt_替换占位符 测试进度提醒替换占位符
func TestBuildProgressReminderUserPrompt_替换占位符(t *testing.T) {
	result := BuildProgressReminderUserPrompt("task list", "current task", "cn")
	if !strings.Contains(result, "task list") {
		t.Error("结果应包含任务列表")
	}
	if !strings.Contains(result, "current task") {
		t.Error("结果应包含当前执行任务")
	}
}

// TestBuildModelSelectionPrompt_非空 测试模型选择提示词非空
func TestBuildModelSelectionPrompt_非空(t *testing.T) {
	result := BuildModelSelectionPrompt("model1: fast\nmodel2: powerful", "cn")
	if result == "" {
		t.Error("非空模型列表应返回非空提示词")
	}
	if !strings.Contains(result, "model1") {
		t.Error("结果应包含模型信息")
	}
}

// TestBuildModelSelectionPrompt_空 测试模型选择提示词空输入
func TestBuildModelSelectionPrompt_空(t *testing.T) {
	result := BuildModelSelectionPrompt("", "cn")
	if result != "" {
		t.Error("空模型列表应返回空字符串")
	}
}

// TestBuildReloadSection_基本验证 测试上下文压缩节
func TestBuildReloadSection_基本验证(t *testing.T) {
	for _, lang := range []string{"cn", "en"} {
		s := BuildReloadSection(lang)
		if s.Name != "offload" {
			t.Errorf("lang=%s: Name = %q, want 'offload'", lang, s.Name)
		}
		if s.Priority != 90 {
			t.Errorf("lang=%s: Priority = %d, want 90", lang, s.Priority)
		}
		if s.Content[lang] == "" {
			t.Errorf("lang=%s: Content 不应为空", lang)
		}
	}
}

// TestSectionName常量 测试节名称常量值
func TestSectionName常量(t *testing.T) {
	names := map[string]string{
		SectionIdentity:             "identity",
		SectionSafety:               "safety",
		SectionSkills:               "skills",
		SectionTools:                "tools",
		SectionTodo:                 "todo",
		SectionTaskTool:             "task_tool",
		SectionToolNavigation:       "tool_navigation",
		SectionProgressiveToolRules: "progressive_tool_rules",
		SectionRuntime:              "runtime",
		SectionMemory:               "memory",
		SectionSessionTools:         "session_tools",
		SectionModeInstructions:     "mode_instructions",
		SectionWorkspace:            "workspace",
		SectionHeartbeat:            "heartbeat",
		SectionContext:              "context",
		SectionExternalMemory:       "external_memory",
		SectionCompletionSignal:     "completion_signal",
		SectionVerificationContract: "verification_contract",
	}
	for got, want := range names {
		if got != want {
			t.Errorf("节名称常量 = %q, want %q", got, want)
		}
	}
}

// TestBuildNavigationEntry_中文冒号 测试导航条目中文冒号
func TestBuildNavigationEntry_中文冒号(t *testing.T) {
	cn := BuildNavigationEntry("tool1", "group1", "loaded", "desc1", "cn")
	if !strings.Contains(cn, "：") {
		t.Error("CN 导航条目应使用中文冒号")
	}
	en := BuildNavigationEntry("tool1", "group1", "loaded", "desc1", "en")
	if !strings.Contains(en, ":") {
		t.Error("EN 导航条目应使用英文冒号")
	}
}

// TestGetListSkillSystemPrompt_双语言 测试技能选择系统提示词双语言
func TestGetListSkillSystemPrompt_双语言(t *testing.T) {
	cn := GetListSkillSystemPrompt("cn")
	en := GetListSkillSystemPrompt("en")
	if cn == "" || en == "" {
		t.Error("技能选择系统提示词不应为空")
	}
	if !strings.Contains(cn, "技能选择器") {
		t.Error("CN 提示词应包含 '技能选择器'")
	}
	if !strings.Contains(en, "list_skill selector") {
		t.Error("EN 提示词应包含 'list_skill selector'")
	}
}

// TestBuildMemoryMgmtPrompt_双语言 测试存储管理规范提示词
func TestBuildMemoryMgmtPrompt_双语言(t *testing.T) {
	cn := BuildMemoryMgmtPrompt("cn")
	en := BuildMemoryMgmtPrompt("en")
	if cn == "" || en == "" {
		t.Error("存储管理规范提示词不应为空")
	}
}

// TestBuildMemoryDatePrompt_日期替换 测试日期提示词替换
func TestBuildMemoryDatePrompt_日期替换(t *testing.T) {
	result := BuildMemoryDatePrompt("2026-07-04", "cn")
	if !strings.Contains(result, "2026-07-04") {
		t.Error("日期提示词应包含替换后的日期")
	}
	if strings.Contains(result, "{today_date}") {
		t.Error("日期提示词不应包含未替换的占位符")
	}
}

// TestBuildHeartbeatSection_英文 测试心跳节英文
func TestBuildHeartbeatSection_英文(t *testing.T) {
	s := BuildHeartbeatSection("test task", "en")
	if s.Content["en"] == "" {
		t.Error("Content[en] 不应为空")
	}
	if !strings.Contains(s.Content["en"], "test task") {
		t.Error("Content[en] 应包含心跳内容")
	}
}

// TestBuildHeartbeatSection_清理注释 测试心跳节清理 HTML 注释
func TestBuildHeartbeatSection_清理注释(t *testing.T) {
	s := BuildHeartbeatSection("<!-- comment -->\ntask content", "cn")
	if strings.Contains(s.Content["cn"], "<!-- comment -->") {
		t.Error("应移除 HTML 注释")
	}
	if !strings.Contains(s.Content["cn"], "task content") {
		t.Error("应保留非注释内容")
	}
}

// TestBuildCodingMemorySection_英文只读 测试编码记忆节英文只读
func TestBuildCodingMemorySection_英文只读(t *testing.T) {
	s := BuildCodingMemorySection("/mem", true, "en")
	if !strings.Contains(s.Content["en"], "read-only") {
		t.Error("readOnly=true EN 模式应包含 'read-only'")
	}
}

// TestBuildContextSection_英文 测试上下文节英文
func TestBuildContextSection_英文(t *testing.T) {
	s := BuildContextSection(map[string]string{}, "en")
	if s.Content["en"] == "" {
		t.Error("Content[en] 不应为空")
	}
}

// TestBuildContextSection_每日记忆 测试上下文节包含每日记忆
func TestBuildContextSection_每日记忆(t *testing.T) {
	files := map[string]string{
		"daily_memory":      "daily content here",
		"daily_memory_date": "2026-07-04",
	}
	s := BuildContextSection(files, "cn")
	if !strings.Contains(s.Content["cn"], "daily content here") {
		t.Error("Content[cn] 应包含每日记忆内容")
	}
}

// TestBuildPlanModeSection_英文 测试计划模式节英文
func TestBuildPlanModeSection_英文(t *testing.T) {
	s := BuildPlanModeSection("/tmp/plan.md", false, "en")
	if s.Content["en"] == "" {
		t.Error("Content[en] 不应为空")
	}
	if !strings.Contains(s.Content["en"], "Plan mode is active") {
		t.Error("EN 版本应包含 'Plan mode is active'")
	}
	if !strings.Contains(s.Content["en"], "/tmp/plan.md") {
		t.Error("EN 版本应包含计划文件路径")
	}
}

// TestBuildCompletionSignalSection_英文 测试完成信号节英文
func TestBuildCompletionSignalSection_英文(t *testing.T) {
	s := BuildCompletionSignalSection("TOKEN", "en")
	if !strings.Contains(s.Content["en"], "<promise>TOKEN</promise>") {
		t.Error("EN 版本应包含完整 promise 标签")
	}
}

// TestBuildNavigationSection_英文 测试导航节英文
func TestBuildNavigationSection_英文(t *testing.T) {
	s := BuildNavigationSection([]string{"entry"}, "en")
	if s.Content["en"] == "" {
		t.Error("Content[en] 不应为空")
	}
	if !strings.Contains(s.Content["en"], "entry") {
		t.Error("Content[en] 应包含导航条目")
	}
}

// TestBuildProgressiveToolRulesSection_英文 测试渐进式工具规则节英文
func TestBuildProgressiveToolRulesSection_英文(t *testing.T) {
	s := BuildProgressiveToolRulesSection("en")
	if s.Content["en"] == "" {
		t.Error("Content[en] 不应为空")
	}
}

// TestBuildWorkspaceSection_英文内容 测试工作空间节英文内容
func TestBuildWorkspaceSection_英文内容(t *testing.T) {
	s := BuildWorkspaceSection("/home/user", "", "en")
	if !strings.Contains(s.Content["en"], "/home/user") {
		t.Error("Content[en] 应包含工作目录路径")
	}
}

// TestScanDirectoryStructure_超出最大深度 验证超出最大深度返回 nil。
func TestScanDirectoryStructure_超出最大深度(t *testing.T) {
	result := ScanDirectoryStructure("/tmp", 3, 2, "cn")
	if result != nil {
		t.Errorf("超出最大深度应返回 nil，实际 %v", result)
	}
}

// TestScanDirectoryStructure_不存在的路径 验证不存在的路径返回 nil。
func TestScanDirectoryStructure_不存在的路径(t *testing.T) {
	result := ScanDirectoryStructure("/nonexistent_path_12345", 0, 2, "cn")
	if result != nil {
		t.Errorf("不存在的路径应返回 nil，实际 %v", result)
	}
}

// TestScanDirectoryStructure_临时目录 验证扫描临时目录。
func TestScanDirectoryStructure_临时目录(t *testing.T) {
	dir := t.TempDir()
	// 创建子目录和文件
	if err := os.Mkdir(dir+"/subdir", 0o755); err != nil {
		t.Fatalf("创建子目录失败: %v", err)
	}
	if err := os.WriteFile(dir+"/file.txt", []byte("test"), 0o644); err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}

	result := ScanDirectoryStructure(dir, 0, 2, "cn")
	if len(result) == 0 {
		t.Error("应至少返回 1 个节点")
	}
}

// TestGetDirectoryDescription_中文 验证中文描述。
func TestGetDirectoryDescription_中文(t *testing.T) {
	desc := GetDirectoryDescription("src", "cn")
	// 不论是否有描述，都不应 panic
	_ = desc
}

// TestGetDirectoryDescription_英文 验证英文描述。
func TestGetDirectoryDescription_英文(t *testing.T) {
	desc := GetDirectoryDescription("src", "en")
	_ = desc
}

// TestGetDirectoryDescription_未知目录 验证未知目录返回空字符串。
func TestGetDirectoryDescription_未知目录(t *testing.T) {
	desc := GetDirectoryDescription("unknown_dir_xyz", "cn")
	if desc != "" {
		t.Errorf("未知目录应返回空字符串，实际 %q", desc)
	}
}

// TestFormatTree_空列表 验证空列表返回空字符串。
func TestFormatTree_空列表(t *testing.T) {
	result := FormatTree(nil, "cn")
	if result != "" {
		t.Errorf("空列表应返回空字符串，实际 %q", result)
	}
}

// TestFormatTree_单节点 验证单节点格式化。
func TestFormatTree_单节点(t *testing.T) {
	nodes := []DirNode{
		{Name: "src", Path: "/src", Description: "源码", IsFile: false},
	}
	result := FormatTree(nodes, "cn")
	if !strings.Contains(result, "src/") {
		t.Errorf("应包含 src/，实际 %q", result)
	}
	if !strings.Contains(result, "源码") {
		t.Errorf("应包含描述，实际 %q", result)
	}
}

// TestFormatTree_文件节点 验证文件节点格式化。
func TestFormatTree_文件节点(t *testing.T) {
	nodes := []DirNode{
		{Name: "main.go", Path: "/main.go", IsFile: true},
	}
	result := FormatTree(nodes, "cn")
	if !strings.Contains(result, "main.go") {
		t.Errorf("应包含 main.go，实际 %q", result)
	}
}

// TestFormatTree_带子节点 验证带子节点格式化。
func TestFormatTree_带子节点(t *testing.T) {
	nodes := []DirNode{
		{
			Name: "src", Path: "/src", Description: "源码", IsFile: false,
			Children: []DirNode{
				{Name: "main.go", Path: "/src/main.go", IsFile: true},
			},
		},
	}
	result := FormatTree(nodes, "cn")
	if !strings.Contains(result, "src/") {
		t.Errorf("应包含 src/，实际 %q", result)
	}
	if !strings.Contains(result, "main.go") {
		t.Errorf("应包含 main.go，实际 %q", result)
	}
}

// TestBuildToolsContent_空工具 验证空工具列表返回空字符串。
func TestBuildToolsContent_空工具(t *testing.T) {
	result := BuildToolsContent(nil, "cn")
	if result != "" {
		t.Errorf("空工具列表应返回空字符串，实际 %q", result)
	}
}

// TestBuildToolsContent_有工具中文 验证中文工具列表。
func TestBuildToolsContent_有工具中文(t *testing.T) {
	tools := map[string]string{"bash": "执行命令", "grep": "搜索"}
	result := BuildToolsContent(tools, "cn")
	if !strings.Contains(result, "可用工具") {
		t.Errorf("应包含可用工具标题，实际 %q", result)
	}
}

// TestBuildToolsContent_有工具英文 验证英文工具列表。
func TestBuildToolsContent_有工具英文(t *testing.T) {
	tools := map[string]string{"bash": "execute commands", "grep": "search"}
	result := BuildToolsContent(tools, "en")
	if !strings.Contains(result, "Available Tools") {
		t.Errorf("应包含 Available Tools 标题，实际 %q", result)
	}
}

// TestExtractTaskToolAgentLines_空描述 验证空描述返回 nil。
func TestExtractTaskToolAgentLines_空描述(t *testing.T) {
	result := extractTaskToolAgentLines("", "cn")
	if result != nil {
		t.Errorf("空描述应返回 nil，实际 %v", result)
	}
}

// TestExtractTaskToolAgentLines_无标记 验证不含标记返回 nil。
func TestExtractTaskToolAgentLines_无标记(t *testing.T) {
	result := extractTaskToolAgentLines("普通描述", "cn")
	if result != nil {
		t.Errorf("不含标记应返回 nil，实际 %v", result)
	}
}

// TestExtractTaskToolAgentLines_中文 验证中文提取。
func TestExtractTaskToolAgentLines_中文(t *testing.T) {
	desc := "可用代理类型及对应工具：\n- code: 编码\n- plan: 规划\n重要：注意"
	result := extractTaskToolAgentLines(desc, "cn")
	if len(result) == 0 {
		t.Error("应至少提取 1 行")
	}
}

// TestExtractTaskToolAgentLines_英文 验证英文提取。
func TestExtractTaskToolAgentLines_英文(t *testing.T) {
	desc := "Available agent types and the tools they have access to:\n- code: coding\n- plan: planning\nImportant: note"
	result := extractTaskToolAgentLines(desc, "en")
	if len(result) == 0 {
		t.Error("应至少提取 1 行")
	}
}
