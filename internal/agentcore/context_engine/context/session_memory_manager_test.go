package context

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── SessionMemoryConfig 测试 ────────────────────────────

// TestNewSessionMemoryConfig 测试默认配置值
func TestNewSessionMemoryConfig(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	if cfg.TriggerTokens != 10000 {
		t.Errorf("TriggerTokens 期望 10000，实际 %d", cfg.TriggerTokens)
	}
	if cfg.TriggerAddTokens != 5000 {
		t.Errorf("TriggerAddTokens 期望 5000，实际 %d", cfg.TriggerAddTokens)
	}
	if cfg.ToolMin != 3 {
		t.Errorf("ToolMin 期望 3，实际 %d", cfg.ToolMin)
	}
	if cfg.UpdateMode != "direct_replace" {
		t.Errorf("UpdateMode 期望 direct_replace，实际 %q", cfg.UpdateMode)
	}
	if cfg.DirectReplaceMaxRetries != 2 {
		t.Errorf("DirectReplaceMaxRetries 期望 2，实际 %d", cfg.DirectReplaceMaxRetries)
	}
	if cfg.Model != nil {
		t.Errorf("Model 期望 nil，实际 %v", cfg.Model)
	}
	if cfg.ModelClient != nil {
		t.Errorf("ModelClient 期望 nil，实际 %v", cfg.ModelClient)
	}
}

// TestSessionMemoryConfig_Validate_合法配置 测试合法配置校验通过
func TestSessionMemoryConfig_Validate_合法配置(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("合法配置校验应通过，实际返回错误: %v", err)
	}
}

// TestSessionMemoryConfig_Validate_agent_edit模式 测试 agent_edit 模式校验通过
func TestSessionMemoryConfig_Validate_agent_edit模式(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	cfg.UpdateMode = "agent_edit"
	if err := cfg.Validate(); err != nil {
		t.Errorf("agent_edit 模式应通过校验，实际返回错误: %v", err)
	}
}

// TestSessionMemoryConfig_Validate_TriggerTokens非法 测试 TriggerTokens <= 0
func TestSessionMemoryConfig_Validate_TriggerTokens非法(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	cfg.TriggerTokens = 0
	if err := cfg.Validate(); err == nil {
		t.Error("TriggerTokens=0 应返回错误")
	}
}

// TestSessionMemoryConfig_Validate_TriggerAddTokens非法 测试 TriggerAddTokens <= 0
func TestSessionMemoryConfig_Validate_TriggerAddTokens非法(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	cfg.TriggerAddTokens = -1
	if err := cfg.Validate(); err == nil {
		t.Error("TriggerAddTokens=-1 应返回错误")
	}
}

// TestSessionMemoryConfig_Validate_ToolMin非法 测试 ToolMin <= 0
func TestSessionMemoryConfig_Validate_ToolMin非法(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	cfg.ToolMin = 0
	if err := cfg.Validate(); err == nil {
		t.Error("ToolMin=0 应返回错误")
	}
}

// TestSessionMemoryConfig_Validate_UpdateMode非法 测试无效 UpdateMode
func TestSessionMemoryConfig_Validate_UpdateMode非法(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	cfg.UpdateMode = "invalid"
	if err := cfg.Validate(); err == nil {
		t.Error("UpdateMode=invalid 应返回错误")
	}
}

// ──────────────────────────── SessionMemoryDirectUpdater 测试 ────────────────────────────

// TestNewSessionMemoryDirectUpdater 测试创建 direct_replace 更新器
func TestNewSessionMemoryDirectUpdater(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	updater := NewSessionMemoryDirectUpdater(cfg)
	if updater == nil {
		t.Fatal("NewSessionMemoryDirectUpdater 不应返回 nil")
		return
	}
	if updater.config.TriggerTokens != cfg.TriggerTokens {
		t.Errorf("config 未正确传递")
	}
	if updater.model != nil {
		t.Error("新建时 model 应为 nil")
	}
	if updater.inheritedSystemPrompt != "" {
		t.Error("新建时 inheritedSystemPrompt 应为空")
	}
}

// TestSessionMemoryDirectUpdater_BindModelDefaults 测试绑定默认模型配置
func TestSessionMemoryDirectUpdater_BindModelDefaults(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	updater := NewSessionMemoryDirectUpdater(cfg)

	modelConfig := &llm_schema.ModelRequestConfig{ModelName: "test-model"}
	clientConfig := &llm_schema.ModelClientConfig{ClientProvider: "test-provider"}

	updater.BindModelDefaults(modelConfig, clientConfig)

	if updater.config.Model != modelConfig {
		t.Error("BindModelDefaults 未设置 Model")
	}
	if updater.config.ModelClient != clientConfig {
		t.Error("BindModelDefaults 未设置 ModelClient")
	}
}

// TestSessionMemoryDirectUpdater_BindModelDefaults_已有配置时不覆盖 测试已有配置不被覆盖
func TestSessionMemoryDirectUpdater_BindModelDefaults_已有配置时不覆盖(t *testing.T) {
	existingModel := &llm_schema.ModelRequestConfig{ModelName: "existing"}
	existingClient := &llm_schema.ModelClientConfig{ClientProvider: "existing-provider"}

	cfg := NewSessionMemoryConfig()
	cfg.Model = existingModel
	cfg.ModelClient = existingClient

	updater := NewSessionMemoryDirectUpdater(cfg)

	newModel := &llm_schema.ModelRequestConfig{ModelName: "new-model"}
	newClient := &llm_schema.ModelClientConfig{ClientProvider: "new-provider"}

	updater.BindModelDefaults(newModel, newClient)

	if updater.config.Model != existingModel {
		t.Error("已有 Model 不应被覆盖")
	}
	if updater.config.ModelClient != existingClient {
		t.Error("已有 ModelClient 不应被覆盖")
	}
}

// TestSessionMemoryDirectUpdater_SetInheritedSystemPrompt 测试设置继承系统提示词
func TestSessionMemoryDirectUpdater_SetInheritedSystemPrompt(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	updater := NewSessionMemoryDirectUpdater(cfg)

	prompt := "你是一个智能助手"
	updater.SetInheritedSystemPrompt(prompt)

	if updater.inheritedSystemPrompt != prompt {
		t.Errorf("inheritedSystemPrompt 期望 %q，实际 %q", prompt, updater.inheritedSystemPrompt)
	}
}

// TestSessionMemoryDirectUpdater_Invoke_模型未配置时返回错误 测试未配置模型客户端时 Invoke 报错
func TestSessionMemoryDirectUpdater_Invoke_模型未配置时返回错误(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	updater := NewSessionMemoryDirectUpdater(cfg)

	err := updater.Invoke(context.Background(), SessionMemoryUpdateOptions{
		FullContextMessages: []llm_schema.BaseMessage{},
		NotesPath:           "/tmp/test_notes.md",
		CurrentNotes:        "test",
	})
	if err == nil {
		t.Error("未配置模型客户端时 Invoke 应返回错误")
	}
}

// ──────────────────────────── SessionMemoryAgentUpdater 测试 ────────────────────────────

// TestNewSessionMemoryAgentUpdater 测试创建 agent_edit 更新器
func TestNewSessionMemoryAgentUpdater(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	updater := NewSessionMemoryAgentUpdater(cfg)
	if updater == nil {
		t.Fatal("NewSessionMemoryAgentUpdater 不应返回 nil")
		return
	}
	if updater.config.TriggerTokens != cfg.TriggerTokens {
		t.Errorf("config 未正确传递")
	}
}

// TestSessionMemoryAgentUpdater_Invoke_尚未实现 测试 agent_edit 模式 Invoke 返回错误
func TestSessionMemoryAgentUpdater_Invoke_尚未实现(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	updater := NewSessionMemoryAgentUpdater(cfg)

	err := updater.Invoke(context.Background(), SessionMemoryUpdateOptions{})
	if err == nil {
		t.Error("agent_edit 模式 Invoke 应返回错误")
	}
	if !strings.Contains(err.Error(), "尚未实现") {
		t.Errorf("错误信息应包含\"尚未实现\"，实际: %v", err)
	}
}

// TestSessionMemoryAgentUpdater_BindModelDefaults 测试空操作
func TestSessionMemoryAgentUpdater_BindModelDefaults(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	updater := NewSessionMemoryAgentUpdater(cfg)
	// 应不 panic
	updater.BindModelDefaults(nil, nil)
}

// TestSessionMemoryAgentUpdater_SetInheritedSystemPrompt 测试空操作
func TestSessionMemoryAgentUpdater_SetInheritedSystemPrompt(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	updater := NewSessionMemoryAgentUpdater(cfg)
	// 应不 panic
	updater.SetInheritedSystemPrompt("test")
}

// ──────────────────────────── SessionMemoryManager 测试 ────────────────────────────

// TestNewSessionMemoryManager_direct_replace模式 测试默认模式创建
func TestNewSessionMemoryManager_direct_replace模式(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	mgr := NewSessionMemoryManager(cfg)
	if mgr == nil {
		t.Fatal("NewSessionMemoryManager 不应返回 nil")
	}
	// 默认 direct_replace 模式，updater 应为 SessionMemoryDirectUpdater
	if _, ok := mgr.updater.(*SessionMemoryDirectUpdater); !ok {
		t.Error("默认模式应为 SessionMemoryDirectUpdater")
	}
}

// TestNewSessionMemoryManager_agent_edit模式 测试 agent_edit 模式创建
func TestNewSessionMemoryManager_agent_edit模式(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	cfg.UpdateMode = "agent_edit"
	mgr := NewSessionMemoryManager(cfg)
	if _, ok := mgr.updater.(*SessionMemoryAgentUpdater); !ok {
		t.Error("agent_edit 模式应为 SessionMemoryAgentUpdater")
	}
}

// TestSessionMemoryManager_Shutdown 测试关闭管理器
func TestSessionMemoryManager_Shutdown(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	mgr := NewSessionMemoryManager(cfg)
	// 无运行任务时 Shutdown 不应 panic
	mgr.Shutdown()
	if len(mgr.tasks) != 0 {
		t.Error("Shutdown 后 tasks 应为空")
	}
}

// TestSessionMemoryManager_BindModelDefaults 测试透传绑定模型配置
func TestSessionMemoryManager_BindModelDefaults(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	mgr := NewSessionMemoryManager(cfg)

	modelConfig := &llm_schema.ModelRequestConfig{ModelName: "test-model"}
	clientConfig := &llm_schema.ModelClientConfig{ClientProvider: "test-provider"}
	mgr.BindModelDefaults(modelConfig, clientConfig)

	// 透传给 updater，验证 updater 已接收
	updater := mgr.updater.(*SessionMemoryDirectUpdater)
	if updater.config.Model != modelConfig {
		t.Error("BindModelDefaults 未透传到 updater")
	}
}

// TestSessionMemoryManager_MaybeScheduleUpdate_参数为空时不调度 测试 nil 参数安全退出
func TestSessionMemoryManager_MaybeScheduleUpdate_参数为空时不调度(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	mgr := NewSessionMemoryManager(cfg)

	// sess 为 nil 不应 panic
	mgr.MaybeScheduleUpdate(context.Background(), nil, nil, "")
}

// TestSessionMemoryManager_CollectContextWindow_参数为nil 测试 nil ModelContext
func TestSessionMemoryManager_CollectContextWindow_参数为nil(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	mgr := NewSessionMemoryManager(cfg)

	window := mgr.CollectContextWindow(nil)
	if window == nil {
		t.Fatal("CollectContextWindow(nil) 不应返回 nil")
	}
	if len(window.ContextMessages) != 0 {
		t.Error("nil ModelContext 返回的 ContextMessages 应为空")
	}
}

// TestSessionMemoryManager_UpdateInheritedSystemPrompt 测试更新继承系统提示词
func TestSessionMemoryManager_UpdateInheritedSystemPrompt(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	mgr := NewSessionMemoryManager(cfg)

	messages := []llm_schema.BaseMessage{
		llm_schema.NewSystemMessage("你是助手"),
		llm_schema.NewUserMessage("你好"),
	}
	mgr.UpdateInheritedSystemPrompt(messages)

	updater := mgr.updater.(*SessionMemoryDirectUpdater)
	if updater.inheritedSystemPrompt != "你是助手" {
		t.Errorf("inheritedSystemPrompt 期望 \"你是助手\"，实际 %q", updater.inheritedSystemPrompt)
	}
}

// TestSessionMemoryManager_ShouldUpdate_参数为nil 测试 nil 参数返回 false
func TestSessionMemoryManager_ShouldUpdate_参数为nil(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	mgr := NewSessionMemoryManager(cfg)

	window := iface.NewContextWindow()
	if mgr.ShouldUpdate(nil, nil, window) {
		t.Error("参数为 nil 时 ShouldUpdate 应返回 false")
	}
}

// ──────────────────────────── GetSessionMemoryRuntime 测试 ────────────────────────────

// TestGetSessionMemoryRuntime_session为nil 测试 session 为 nil 时返回默认状态
func TestGetSessionMemoryRuntime_session为nil(t *testing.T) {
	runtime := GetSessionMemoryRuntime(nil)
	if runtime == nil {
		t.Fatal("GetSessionMemoryRuntime(nil) 不应返回 nil")
	}
	if runtime["initialized"] != false {
		t.Error("默认 initialized 应为 false")
	}
	if runtime["is_extracting"] != false {
		t.Error("默认 is_extracting 应为 false")
	}
	if runtime["tokens_at_last_update"] != 0 {
		t.Error("默认 tokens_at_last_update 应为 0")
	}
}

// TestGetSessionMemoryRuntime_真实session 测试真实 session 读写
func TestGetSessionMemoryRuntime_真实session(t *testing.T) {
	sess := session.NewSession()
	runtime := GetSessionMemoryRuntime(sess)
	if runtime == nil {
		t.Fatal("GetSessionMemoryRuntime 不应返回 nil")
	}
	if runtime["initialized"] != false {
		t.Error("默认 initialized 应为 false")
	}
}

// TestUpdateSessionMemoryRuntime_真实session 测试更新运行时状态
func TestUpdateSessionMemoryRuntime_真实session(t *testing.T) {
	sess := session.NewSession()
	updateSessionMemoryRuntime(sess, map[string]any{
		"initialized":   true,
		"is_extracting": true,
	})

	runtime := GetSessionMemoryRuntime(sess)
	if runtime["initialized"] != true {
		t.Error("initialized 应为 true")
	}
	if runtime["is_extracting"] != true {
		t.Error("is_extracting 应为 true")
	}
}

// TestUpdateSessionMemoryRuntime_session为nil 测试 nil 安全
func TestUpdateSessionMemoryRuntime_session为nil(t *testing.T) {
	// 不应 panic
	updateSessionMemoryRuntime(nil, map[string]any{"key": "value"})
}

// TestInvalidateSessionMemoryAnchor_真实session 测试基线重置
func TestInvalidateSessionMemoryAnchor_真实session(t *testing.T) {
	sess := session.NewSession()
	// 先设置一些基线值
	updateSessionMemoryRuntime(sess, map[string]any{
		"tokens_at_last_update":         5000,
		"last_summarized_message_count": 10,
		"notes_upto_message_id":         "msg-123",
	})

	InvalidateSessionMemoryAnchor(sess)

	runtime := GetSessionMemoryRuntime(sess)
	if runtime["tokens_at_last_update"] != 0 {
		t.Errorf("基线重置后 tokens_at_last_update 应为 0，实际 %v", runtime["tokens_at_last_update"])
	}
	if runtime["last_summarized_message_count"] != 0 {
		t.Errorf("基线重置后 last_summarized_message_count 应为 0，实际 %v", runtime["last_summarized_message_count"])
	}
	if runtime["notes_upto_message_id"] != "" {
		t.Errorf("基线重置后 notes_upto_message_id 应为空，实际 %v", runtime["notes_upto_message_id"])
	}
}

// TestInvalidateSessionMemoryAnchor_session为nil 测试 nil 安全
func TestInvalidateSessionMemoryAnchor_session为nil(t *testing.T) {
	// 不应 panic
	InvalidateSessionMemoryAnchor(nil)
}

// ──────────────────────────── 路径函数测试 ────────────────────────────

// TestGetSessionMemoryPath 测试会话记忆文件路径拼接
func TestGetSessionMemoryPath(t *testing.T) {
	result := GetSessionMemoryPath("/workspace", "sess-123")
	expected := filepath.Join("/workspace", "context", "sess-123_context", "session_memory", "session_context.md")
	if result != expected {
		t.Errorf("GetSessionMemoryPath 期望 %q，实际 %q", expected, result)
	}
}

// TestGetSessionMemoryPath_空参数 测试空参数
func TestGetSessionMemoryPath_空参数(t *testing.T) {
	result := GetSessionMemoryPath("", "")
	expected := filepath.Join("context", "_context", "session_memory", "session_context.md")
	if result != expected {
		t.Errorf("GetSessionMemoryPath 空参数期望 %q，实际 %q", expected, result)
	}
}

// TestGetPendingSessionMemoryPath 测试 pending 路径生成
func TestGetPendingSessionMemoryPath(t *testing.T) {
	result := getPendingSessionMemoryPath("/workspace/context/sess-123_context/session_memory/session_context.md")
	expected := filepath.Join("/workspace/context/sess-123_context/session_memory", "session_context.pending.md")
	if result != expected {
		t.Errorf("getPendingSessionMemoryPath 期望 %q，实际 %q", expected, result)
	}
}

// ──────────────────────────── ReadOrInitSessionMemory 测试 ────────────────────────────

// TestReadOrInitSessionMemory_文件不存在时创建默认模板 测试文件不存在时初始化
func TestReadOrInitSessionMemory_文件不存在时创建默认模板(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "context", "sess_context", "session_memory", "session_context.md")

	result := readOrInitSessionMemory(path)
	if result != defaultSessionMemoryTemplate {
		t.Error("文件不存在时应返回默认模板")
	}

	// 验证文件已创建
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("文件应已创建，读取失败: %v", err)
	}
	if string(data) != defaultSessionMemoryTemplate {
		t.Error("创建的文件内容应与默认模板一致")
	}
}

// TestReadOrInitSessionMemory_文件已存在时读取 测试文件已存在时直接读取
func TestReadOrInitSessionMemory_文件已存在时读取(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "session_context.md")
	content := "# 自定义笔记内容"

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}

	result := readOrInitSessionMemory(path)
	if result != content {
		t.Errorf("文件已存在时应返回文件内容，期望 %q，实际 %q", content, result)
	}
}

// ──────────────────────────── 提示词构建函数测试 ────────────────────────────

// TestBuildSessionMemoryPrompt 测试 agent_edit 提示词模板替换
func TestBuildSessionMemoryPrompt(t *testing.T) {
	result := buildSessionMemoryPrompt("/path/to/notes.md", "当前笔记内容")
	if !strings.Contains(result, "/path/to/notes.md") {
		t.Error("提示词应包含 notesPath")
	}
	if !strings.Contains(result, "当前笔记内容") {
		t.Error("提示词应包含 currentNotes")
	}
	if strings.Contains(result, "{{notesPath}}") {
		t.Error("占位符 {{notesPath}} 应被替换")
	}
	if strings.Contains(result, "{{currentNotes}}") {
		t.Error("占位符 {{currentNotes}} 应被替换")
	}
}

// TestBuildDirectSessionMemoryPrompt 测试 direct_replace 提示词模板替换
func TestBuildDirectSessionMemoryPrompt(t *testing.T) {
	result := buildDirectSessionMemoryPrompt("/path/notes.md", "笔记")
	if !strings.Contains(result, "/path/notes.md") {
		t.Error("提示词应包含 notesPath")
	}
	if !strings.Contains(result, "笔记") {
		t.Error("提示词应包含 currentNotes")
	}
	if strings.Contains(result, "{{notesPath}}") {
		t.Error("占位符 {{notesPath}} 应被替换")
	}
	if strings.Contains(result, "{{currentNotes}}") {
		t.Error("占位符 {{currentNotes}} 应被替换")
	}
}

// TestBuildSystemPromptText_有系统消息 测试提取第一条系统消息
func TestBuildSystemPromptText_有系统消息(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewSystemMessage("系统提示词"),
		llm_schema.NewUserMessage("用户消息"),
	}
	result := buildSystemPromptText(messages)
	if result != "系统提示词" {
		t.Errorf("期望 \"系统提示词\"，实际 %q", result)
	}
}

// TestBuildSystemPromptText_无系统消息 测试无系统消息返回空
func TestBuildSystemPromptText_无系统消息(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("用户消息"),
		llm_schema.NewAssistantMessage("助手回复"),
	}
	result := buildSystemPromptText(messages)
	if result != "" {
		t.Errorf("无系统消息时应返回空字符串，实际 %q", result)
	}
}

// TestBuildSystemPromptText_空消息列表 测试空列表返回空
func TestBuildSystemPromptText_空消息列表(t *testing.T) {
	result := buildSystemPromptText(nil)
	if result != "" {
		t.Errorf("空列表应返回空字符串，实际 %q", result)
	}
}

// ──────────────────────────── API 轮次分组测试 ────────────────────────────

// TestGroupCompletedAPIRounds_完整轮次 测试完整的 Human→Assistant(无tool_calls) 轮次
func TestGroupCompletedAPIRounds_完整轮次(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	rounds := groupCompletedAPIRounds(messages)
	if len(rounds) != 1 {
		t.Fatalf("期望 1 轮，实际 %d 轮", len(rounds))
	}
	if rounds[0][0] != 0 || rounds[0][1] != 2 {
		t.Errorf("轮次范围期望 [0,2)，实际 [%d,%d)", rounds[0][0], rounds[0][1])
	}
}

// TestGroupCompletedAPIRounds_带工具调用 测试带 tool_calls 的轮次
func TestGroupCompletedAPIRounds_带工具调用(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查天气"),
		llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				llm_schema.NewToolCall("tc-1", "get_weather", `{"city":"北京"}`),
			}),
		),
		llm_schema.NewToolMessage("tc-1", "晴天 25°C"),
	}
	rounds := groupCompletedAPIRounds(messages)
	if len(rounds) != 1 {
		t.Fatalf("期望 1 轮，实际 %d 轮", len(rounds))
	}
	if rounds[0][0] != 0 || rounds[0][1] != 3 {
		t.Errorf("轮次范围期望 [0,3)，实际 [%d,%d)", rounds[0][0], rounds[0][1])
	}
}

// TestGroupCompletedAPIRounds_多轮对话 测试多轮对话分组
func TestGroupCompletedAPIRounds_多轮对话(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("第一轮"),
		llm_schema.NewAssistantMessage("回复1"),
		llm_schema.NewUserMessage("第二轮"),
		llm_schema.NewAssistantMessage("回复2"),
	}
	rounds := groupCompletedAPIRounds(messages)
	if len(rounds) != 2 {
		t.Fatalf("期望 2 轮，实际 %d 轮", len(rounds))
	}
}

// TestGroupCompletedAPIRounds_空消息列表 测试空消息列表
func TestGroupCompletedAPIRounds_空消息列表(t *testing.T) {
	rounds := groupCompletedAPIRounds(nil)
	if len(rounds) != 0 {
		t.Errorf("空列表应返回 0 轮，实际 %d 轮", len(rounds))
	}
}

// TestGroupCompletedAPIRounds_工具调用未完成 测试 tool_calls 未被 ToolMessage 回复
func TestGroupCompletedAPIRounds_工具调用未完成(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查天气"),
		llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				llm_schema.NewToolCall("tc-1", "get_weather", `{"city":"北京"}`),
			}),
		),
		// 缺少 ToolMessage 回复
	}
	rounds := groupCompletedAPIRounds(messages)
	if len(rounds) != 0 {
		t.Errorf("未完成的工具调用不应构成完整轮次，实际 %d 轮", len(rounds))
	}
}

// TestFindLastCompletedAPIRoundEnd_有完整轮次 测试返回最后轮次结束索引
func TestFindLastCompletedAPIRoundEnd_有完整轮次(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("第一轮"),
		llm_schema.NewAssistantMessage("回复1"),
		llm_schema.NewUserMessage("第二轮"),
		llm_schema.NewAssistantMessage("回复2"),
	}
	idx := findLastCompletedAPIRoundEnd(messages)
	if idx != 4 {
		t.Errorf("期望 4，实际 %d", idx)
	}
}

// TestFindLastCompletedAPIRoundEnd_空消息 测试空消息返回 0
func TestFindLastCompletedAPIRoundEnd_空消息(t *testing.T) {
	idx := findLastCompletedAPIRoundEnd(nil)
	if idx != 0 {
		t.Errorf("空消息应返回 0，实际 %d", idx)
	}
}

// TestFindLastCompletedAPIRoundEnd_无完整轮次 测试无完整轮次返回 0
func TestFindLastCompletedAPIRoundEnd_无完整轮次(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("只有用户消息"),
	}
	idx := findLastCompletedAPIRoundEnd(messages)
	if idx != 0 {
		t.Errorf("无完整轮次应返回 0，实际 %d", idx)
	}
}

// ──────────────────────────── GetContextMessageID 测试 ────────────────────────────

// TestGetContextMessageID_有ID 测试从 metadata 提取 context_message_id
func TestGetContextMessageID_有ID(t *testing.T) {
	msg := llm_schema.NewUserMessage("hello",
		llm_schema.WithMetadata(map[string]any{ContextMessageIDKey: "msg-001"}),
	)
	id := GetContextMessageID(msg)
	if id != "msg-001" {
		t.Errorf("期望 msg-001，实际 %q", id)
	}
}

// TestGetContextMessageID_无ID 测试无 context_message_id 返回空
func TestGetContextMessageID_无ID(t *testing.T) {
	msg := llm_schema.NewUserMessage("hello")
	id := GetContextMessageID(msg)
	if id != "" {
		t.Errorf("无 ID 时应返回空字符串，实际 %q", id)
	}
}

// TestGetContextMessageID_空metadata 测试 metadata 为空 map
func TestGetContextMessageID_空metadata(t *testing.T) {
	msg := llm_schema.NewUserMessage("hello",
		llm_schema.WithMetadata(map[string]any{}),
	)
	id := GetContextMessageID(msg)
	if id != "" {
		t.Errorf("空 metadata 应返回空字符串，实际 %q", id)
	}
}

// TestGetContextMessageID_ID为非字符串 测试 ID 类型错误返回空
func TestGetContextMessageID_ID为非字符串(t *testing.T) {
	msg := llm_schema.NewUserMessage("hello",
		llm_schema.WithMetadata(map[string]any{ContextMessageIDKey: 12345}),
	)
	id := GetContextMessageID(msg)
	if id != "" {
		t.Errorf("非字符串 ID 应返回空字符串，实际 %q", id)
	}
}

// TestGetContextMessageID_ID为空字符串 测试空字符串 ID 返回空
func TestGetContextMessageID_ID为空字符串(t *testing.T) {
	msg := llm_schema.NewUserMessage("hello",
		llm_schema.WithMetadata(map[string]any{ContextMessageIDKey: ""}),
	)
	id := GetContextMessageID(msg)
	if id != "" {
		t.Errorf("空字符串 ID 应返回空字符串，实际 %q", id)
	}
}

// ──────────────────────────── 辅助函数测试 ────────────────────────────

// TestNormalizeDirectResponseContent_无代码块 测试普通文本不过处理
func TestNormalizeDirectResponseContent_无代码块(t *testing.T) {
	input := "# 标题\n内容"
	result := normalizeDirectResponseContent(input)
	if result != input {
		t.Errorf("无代码块时不应修改内容，期望 %q，实际 %q", input, result)
	}
}

// TestNormalizeDirectResponseContent_markdown代码块 测试去掉 markdown 代码块包裹
func TestNormalizeDirectResponseContent_markdown代码块(t *testing.T) {
	input := "```markdown\n# 标题\n内容\n```"
	result := normalizeDirectResponseContent(input)
	expected := "# 标题\n内容"
	if result != expected {
		t.Errorf("期望 %q，实际 %q", expected, result)
	}
}

// TestNormalizeDirectResponseContent_空字符串 测试空字符串
func TestNormalizeDirectResponseContent_空字符串(t *testing.T) {
	result := normalizeDirectResponseContent("")
	if result != "" {
		t.Errorf("空字符串应返回空字符串，实际 %q", result)
	}
}

// TestGetIntFromMap 测试从 map 安全获取 int
func TestGetIntFromMap(t *testing.T) {
	m := map[string]any{
		"int":    42,
		"int64":  int64(100),
		"float":  float64(3.14),
		"zero":   0,
		"nil":    nil,
		"string": "not-int",
	}
	if v := getIntFromMap(m, "int"); v != 42 {
		t.Errorf("int 期望 42，实际 %d", v)
	}
	if v := getIntFromMap(m, "int64"); v != 100 {
		t.Errorf("int64 期望 100，实际 %d", v)
	}
	if v := getIntFromMap(m, "float"); v != 3 {
		t.Errorf("float 期望 3，实际 %d", v)
	}
	if v := getIntFromMap(m, "zero"); v != 0 {
		t.Errorf("zero 期望 0，实际 %d", v)
	}
	if v := getIntFromMap(m, "nil"); v != 0 {
		t.Errorf("nil 期望 0，实际 %d", v)
	}
	if v := getIntFromMap(m, "string"); v != 0 {
		t.Errorf("string 期望 0，实际 %d", v)
	}
	if v := getIntFromMap(m, "missing"); v != 0 {
		t.Errorf("missing 期望 0，实际 %d", v)
	}
}

// TestGetBoolFromMap 测试从 map 安全获取 bool
func TestGetBoolFromMap(t *testing.T) {
	m := map[string]any{
		"true":   true,
		"false":  false,
		"nil":    nil,
		"string": "not-bool",
	}
	if v := getBoolFromMap(m, "true"); v != true {
		t.Errorf("true 期望 true，实际 %v", v)
	}
	if v := getBoolFromMap(m, "false"); v != false {
		t.Errorf("false 期望 false，实际 %v", v)
	}
	if v := getBoolFromMap(m, "nil"); v != false {
		t.Errorf("nil 期望 false，实际 %v", v)
	}
	if v := getBoolFromMap(m, "string"); v != false {
		t.Errorf("string 期望 false，实际 %v", v)
	}
	if v := getBoolFromMap(m, "missing"); v != false {
		t.Errorf("missing 期望 false，实际 %v", v)
	}
}

// TestGetStringFromMap 测试从 map 安全获取 string
func TestGetStringFromMap(t *testing.T) {
	m := map[string]any{
		"str":   "hello",
		"empty": "",
		"nil":   nil,
		"int":   42,
	}
	if v := getStringFromMap(m, "str"); v != "hello" {
		t.Errorf("str 期望 hello，实际 %q", v)
	}
	if v := getStringFromMap(m, "empty"); v != "" {
		t.Errorf("empty 期望空字符串，实际 %q", v)
	}
	if v := getStringFromMap(m, "nil"); v != "" {
		t.Errorf("nil 期望空字符串，实际 %q", v)
	}
	if v := getStringFromMap(m, "int"); v != "" {
		t.Errorf("int 期望空字符串，实际 %q", v)
	}
	if v := getStringFromMap(m, "missing"); v != "" {
		t.Errorf("missing 期望空字符串，实际 %q", v)
	}
}

// TestBuildSessionMemoryRuntime 测试初始运行时状态结构
func TestBuildSessionMemoryRuntime(t *testing.T) {
	runtime := buildSessionMemoryRuntime()
	if runtime["memory_path"] != "" {
		t.Errorf("memory_path 应为空字符串")
	}
	if runtime["pending_memory_path"] != "" {
		t.Errorf("pending_memory_path 应为空字符串")
	}
	if runtime["initialized"] != false {
		t.Errorf("initialized 应为 false")
	}
	if runtime["is_extracting"] != false {
		t.Errorf("is_extracting 应为 false")
	}
	if runtime["tokens_at_last_update"] != 0 {
		t.Errorf("tokens_at_last_update 应为 0")
	}
	if runtime["tool_calls_at_last_update"] != 0 {
		t.Errorf("tool_calls_at_last_update 应为 0")
	}
	if runtime["last_summarized_message_count"] != 0 {
		t.Errorf("last_summarized_message_count 应为 0")
	}
	if runtime["notes_upto_message_id"] != "" {
		t.Errorf("notes_upto_message_id 应为空字符串")
	}
}

// TestProviderName 测试从 ModelClientConfig 获取 ClientProvider
func TestProviderName(t *testing.T) {
	if v := providerName(nil); v != "" {
		t.Errorf("nil 配置应返回空字符串，实际 %q", v)
	}
	cfg := &llm_schema.ModelClientConfig{ClientProvider: "openai"}
	if v := providerName(cfg); v != "openai" {
		t.Errorf("期望 openai，实际 %q", v)
	}
}

// TestCountToolCalls 测试统计 tool_calls 数量
func TestCountToolCalls(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				llm_schema.NewToolCall("tc-1", "tool1", "{}"),
				llm_schema.NewToolCall("tc-2", "tool2", "{}"),
			}),
		),
		llm_schema.NewToolMessage("tc-1", "结果1"),
		llm_schema.NewToolMessage("tc-2", "结果2"),
		llm_schema.NewAssistantMessage("总结"),
	}
	count := countToolCalls(messages)
	if count != 2 {
		t.Errorf("期望 2 个 tool_calls，实际 %d", count)
	}
}

// TestCountToolCalls_无工具调用 测试无 tool_calls
func TestCountToolCalls_无工具调用(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	count := countToolCalls(messages)
	if count != 0 {
		t.Errorf("期望 0 个 tool_calls，实际 %d", count)
	}
}

// ──────────────────────────── pending 文件操作测试 ────────────────────────────

// TestPreparePendingSessionMemory 测试准备 pending 文件
func TestPreparePendingSessionMemory(t *testing.T) {
	tmpDir := t.TempDir()
	activePath := filepath.Join(tmpDir, "session_context.md")
	pendingPath := filepath.Join(tmpDir, "session_context.pending.md")

	// active 文件不存在时，使用 currentNotes
	preparePendingSessionMemory(activePath, pendingPath, "默认笔记内容")
	data, err := os.ReadFile(pendingPath)
	if err != nil {
		t.Fatalf("pending 文件应已创建: %v", err)
	}
	if string(data) != "默认笔记内容" {
		t.Errorf("pending 文件内容期望 \"默认笔记内容\"，实际 %q", string(data))
	}
}

// TestPreparePendingSessionMemory_active文件存在 测试 active 文件存在时复制
func TestPreparePendingSessionMemory_active文件存在(t *testing.T) {
	tmpDir := t.TempDir()
	activePath := filepath.Join(tmpDir, "session_context.md")
	pendingPath := filepath.Join(tmpDir, "session_context.pending.md")

	content := "已有笔记内容"
	if err := os.WriteFile(activePath, []byte(content), 0644); err != nil {
		t.Fatalf("写入 active 文件失败: %v", err)
	}

	preparePendingSessionMemory(activePath, pendingPath, "默认内容")
	data, err := os.ReadFile(pendingPath)
	if err != nil {
		t.Fatalf("pending 文件应已创建: %v", err)
	}
	if string(data) != content {
		t.Errorf("pending 文件应复制 active 内容，期望 %q，实际 %q", content, string(data))
	}
}

// TestCommitPendingSessionMemory 测试提交 pending 文件
func TestCommitPendingSessionMemory(t *testing.T) {
	tmpDir := t.TempDir()
	pendingPath := filepath.Join(tmpDir, "session_context.pending.md")
	activePath := filepath.Join(tmpDir, "session_context.md")

	content := "更新后的内容"
	if err := os.WriteFile(pendingPath, []byte(content), 0644); err != nil {
		t.Fatalf("写入 pending 文件失败: %v", err)
	}

	if err := commitPendingSessionMemory(pendingPath, activePath); err != nil {
		t.Fatalf("提交失败: %v", err)
	}

	data, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatalf("active 文件应已创建: %v", err)
	}
	if string(data) != content {
		t.Errorf("active 文件内容期望 %q，实际 %q", content, string(data))
	}

	// pending 文件应已不存在
	if _, err := os.Stat(pendingPath); !os.IsNotExist(err) {
		t.Error("pending 文件应已被移除")
	}
}

// TestCommitPendingSessionMemory_pending不存在 测试 pending 文件不存在时报错
func TestCommitPendingSessionMemory_pending不存在(t *testing.T) {
	err := commitPendingSessionMemory("/nonexistent/pending.md", "/nonexistent/active.md")
	if err == nil {
		t.Error("pending 文件不存在时应返回错误")
	}
}

// ──────────────────────────── ContextWindow 截断测试 ────────────────────────────

// TestTruncateContextWindowToCompletedAPIRound_有完整轮次 测试截断到完整轮次
func TestTruncateContextWindowToCompletedAPIRound_有完整轮次(t *testing.T) {
	window := iface.NewContextWindow()
	window.ContextMessages = []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("第一轮"),
		llm_schema.NewAssistantMessage("回复1"),
		llm_schema.NewUserMessage("第二轮"),
		llm_schema.NewAssistantMessage("回复2"),
		llm_schema.NewUserMessage("第三轮（未完成）"),
	}

	result := truncateContextWindowToCompletedAPIRound(window)
	if len(result.ContextMessages) != 4 {
		t.Errorf("期望 4 条消息，实际 %d", len(result.ContextMessages))
	}
}

// TestTruncateContextWindowToCompletedAPIRound_无完整轮次 测试无完整轮次返回空窗口
func TestTruncateContextWindowToCompletedAPIRound_无完整轮次(t *testing.T) {
	window := iface.NewContextWindow()
	window.ContextMessages = []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("只有用户消息"),
	}

	result := truncateContextWindowToCompletedAPIRound(window)
	if len(result.ContextMessages) != 0 {
		t.Errorf("无完整轮次应返回空 ContextMessages，实际 %d", len(result.ContextMessages))
	}
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// ──────────────────────────── getRuntimeState / setRuntimeState 测试 ────────────────────────────

// TestGetRuntimeState_真实session 测试获取运行时状态
func TestGetRuntimeState_真实session(t *testing.T) {
	sess := session.NewSession()
	runtime := getRuntimeState(sess)
	if runtime == nil {
		t.Fatal("getRuntimeState 不应返回 nil")
	}
	if runtime["initialized"] != false {
		t.Error("默认 initialized 应为 false")
	}
	if runtime["tokens_at_last_update"] != 0 {
		t.Error("默认 tokens_at_last_update 应为 0")
	}
}

// TestGetRuntimeState_session为nil 测试 nil session 返回默认值
func TestGetRuntimeState_session为nil(t *testing.T) {
	runtime := getRuntimeState(nil)
	if runtime == nil {
		t.Fatal("getRuntimeState(nil) 不应返回 nil")
	}
	if runtime["initialized"] != false {
		t.Error("nil session 的 initialized 应为 false")
	}
}

// TestSetRuntimeState_真实session 测试设置运行时状态
func TestSetRuntimeState_真实session(t *testing.T) {
	sess := session.NewSession()
	setRuntimeState(sess, map[string]any{
		"initialized":           true,
		"tokens_at_last_update": 5000,
	})
	runtime := getRuntimeState(sess)
	if runtime["initialized"] != true {
		t.Error("setRuntimeState 后 initialized 应为 true")
	}
	if runtime["tokens_at_last_update"] != 5000 {
		t.Errorf("期望 tokens_at_last_update=5000，实际 %v", runtime["tokens_at_last_update"])
	}
}

// ──────────────────────────── countTokens 测试 ────────────────────────────

// mockModelContextForCountTokens 模拟 ModelContext 用于 countTokens 测试
type mockModelContextForCountTokens struct {
	messages     []llm_schema.BaseMessage
	tokenCounter token.TokenCounter
}

func (m *mockModelContextForCountTokens) Len() int { return len(m.messages) }
func (m *mockModelContextForCountTokens) GetMessages(_ int, _ bool) ([]llm_schema.BaseMessage, error) {
	return m.messages, nil
}
func (m *mockModelContextForCountTokens) SetMessages(_ []llm_schema.BaseMessage, _ bool) {}
func (m *mockModelContextForCountTokens) PopMessages(_ int, _ bool) []llm_schema.BaseMessage {
	return nil
}
func (m *mockModelContextForCountTokens) ClearMessages(_ context.Context, _ bool, _ ...iface.Option) error {
	return nil
}
func (m *mockModelContextForCountTokens) AddMessages(_ context.Context, _ llm_schema.BaseMessage, _ ...iface.Option) ([]llm_schema.BaseMessage, error) {
	return nil, nil
}
func (m *mockModelContextForCountTokens) GetContextWindow(_ context.Context, _ []llm_schema.BaseMessage, _ []commonschema.ToolInfoInterface, _ int, _ int, _ ...iface.Option) (*iface.ContextWindow, error) {
	return nil, nil
}
func (m *mockModelContextForCountTokens) Statistic() *iface.ContextStats {
	return &iface.ContextStats{}
}
func (m *mockModelContextForCountTokens) SessionID() string { return "" }
func (m *mockModelContextForCountTokens) ContextID() string { return "" }
func (m *mockModelContextForCountTokens) TokenCounter() token.TokenCounter {
	return m.tokenCounter
}
func (m *mockModelContextForCountTokens) ReloaderTool() tool.Tool                              { return nil }
func (m *mockModelContextForCountTokens) WorkspaceDir() string                                 { return "" }
func (m *mockModelContextForCountTokens) SetSessionRef(_ sessioninterfaces.SessionFacade)      {}
func (m *mockModelContextForCountTokens) GetSessionRef() sessioninterfaces.SessionFacade       { return nil }
func (m *mockModelContextForCountTokens) OffloadMessages(_ string, _ []llm_schema.BaseMessage) {}
func (m *mockModelContextForCountTokens) SaveState() map[string]any                            { return nil }
func (m *mockModelContextForCountTokens) LoadState(_ map[string]any)                           {}
func (m *mockModelContextForCountTokens) CompressContext(_ context.Context, _ ...iface.CompressContextOption) (string, error) {
	return "", nil
}

// TestCountTokens_有TokenCounter 测试有 tokenCounter 时使用计数器
func TestCountTokens_有TokenCounter(t *testing.T) {
	tc := &fakeTokenCounterForSessionMemory{count: 42}
	mc := &mockModelContextForCountTokens{tokenCounter: tc}
	window := &iface.ContextWindow{
		SystemMessages:  []llm_schema.BaseMessage{llm_schema.NewSystemMessage("sys")},
		ContextMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")},
	}
	result := countTokens(mc, window)
	if result != 42 {
		t.Errorf("期望 42，实际 %d", result)
	}
}

// TestCountTokens_无TokenCounter 测试无 tokenCounter 时使用估算
func TestCountTokens_无TokenCounter(t *testing.T) {
	mc := &mockModelContextForCountTokens{tokenCounter: nil}
	window := &iface.ContextWindow{
		ContextMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello world!!!")}, // 14 chars → ceil(14/4) = 4
	}
	result := countTokens(mc, window)
	if result <= 0 {
		t.Errorf("估算结果应 > 0，实际 %d", result)
	}
}

// fakeTokenCounterForSessionMemory 模拟 token 计数器
type fakeTokenCounterForSessionMemory struct {
	count int
	err   error
}

func (f *fakeTokenCounterForSessionMemory) Count(text string, model string) (int, error) {
	return f.count, f.err
}
func (f *fakeTokenCounterForSessionMemory) CountMessages(messages []llm_schema.BaseMessage, model string) (int, error) {
	return f.count, f.err
}
func (f *fakeTokenCounterForSessionMemory) CountTools(tools []commonschema.ToolInfoInterface, model string) (int, error) {
	return f.count, f.err
}

// ──────────────────────────── ShouldUpdate 完整流程测试 ────────────────────────────

// TestShouldUpdate_首次触发 测试首次达到阈值触发更新
func TestShouldUpdate_首次触发(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	cfg.TriggerTokens = 10 // 低阈值方便测试
	cfg.TriggerAddTokens = 5
	cfg.ToolMin = 1
	mgr := NewSessionMemoryManager(cfg)

	sess := session.NewSession()
	mc := &mockModelContextForCountTokens{tokenCounter: &fakeTokenCounterForSessionMemory{count: 100}}

	// 构造含 tool_calls 的消息
	msgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
		llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				llm_schema.NewToolCall("tc-1", "tool1", "{}"),
			}),
		),
		llm_schema.NewToolMessage("tc-1", "result"),
		llm_schema.NewAssistantMessage("done"),
	}

	window := &iface.ContextWindow{ContextMessages: msgs}
	result := mgr.ShouldUpdate(sess, mc, window)
	if !result {
		t.Error("首次达到阈值时应触发更新")
	}
}

// TestShouldUpdate_未达到首次阈值 测试首次未达到阈值
func TestShouldUpdate_未达到首次阈值(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	cfg.TriggerTokens = 100000 // 高阈值
	cfg.TriggerAddTokens = 5000
	cfg.ToolMin = 3
	mgr := NewSessionMemoryManager(cfg)

	sess := session.NewSession()
	mc := &mockModelContextForCountTokens{tokenCounter: &fakeTokenCounterForSessionMemory{count: 50}}
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
	window := &iface.ContextWindow{ContextMessages: msgs}

	result := mgr.ShouldUpdate(sess, mc, window)
	if result {
		t.Error("未达到首次阈值不应触发更新")
	}
}

// TestShouldUpdate_空消息 测试空消息不触发
func TestShouldUpdate_空消息(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	mgr := NewSessionMemoryManager(cfg)
	sess := session.NewSession()
	mc := &mockModelContextForCountTokens{}
	window := &iface.ContextWindow{ContextMessages: nil}

	result := mgr.ShouldUpdate(sess, mc, window)
	if result {
		t.Error("空消息不应触发更新")
	}
}

// ──────────────────────────── MaybeScheduleUpdate 测试 ────────────────────────────

// TestMaybeScheduleUpdate_workspaceDir为空 测试空 workspaceDir 不调度
func TestMaybeScheduleUpdate_workspaceDir为空(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	mgr := NewSessionMemoryManager(cfg)
	sess := session.NewSession()
	// workspaceDir 为空，不应调度
	mgr.MaybeScheduleUpdate(context.Background(), sess, nil, "")
}

// ──────────────────────────── Shutdown 有任务时取消测试 ────────────────────────────

// TestShutdown_有运行中任务 测试关闭时取消运行中的任务
func TestShutdown_有运行中任务(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	mgr := NewSessionMemoryManager(cfg)
	// 手动添加一个 cancel 函数模拟运行中的任务
	ctx, cancel := context.WithCancel(context.Background())
	mgr.tasks["session-1"] = cancel
	// 验证 context 未取消
	if ctx.Err() != nil {
		t.Error("初始 context 不应已取消")
	}
	mgr.Shutdown()
	// 验证 context 已取消
	if ctx.Err() == nil {
		t.Error("Shutdown 后 context 应已取消")
	}
	if len(mgr.tasks) != 0 {
		t.Error("Shutdown 后 tasks 应为空")
	}
}

// ──────────────────────────── getIntFromMap json.Number 测试 ────────────────────────────

// TestGetIntFromMap_jsonNumber 测试 json.Number 类型
func TestGetIntFromMap_jsonNumber(t *testing.T) {
	m := map[string]any{
		"json_num": json.Number("42"),
	}
	if v := getIntFromMap(m, "json_num"); v != 42 {
		t.Errorf("json.Number 期望 42，实际 %d", v)
	}
}

// TestGetIntFromMap_jsonNumber无效 测试无效 json.Number 返回 0
func TestGetIntFromMap_jsonNumber无效(t *testing.T) {
	m := map[string]any{
		"json_num": json.Number("not-a-number"),
	}
	if v := getIntFromMap(m, "json_num"); v != 0 {
		t.Errorf("无效 json.Number 应返回 0，实际 %d", v)
	}
}

// ──────────────────────────── readOrInitSessionMemory 模板文件测试 ────────────────────────────

// TestReadOrInitSessionMemory_模板文件存在 测试模板文件存在时使用模板
func TestReadOrInitSessionMemory_模板文件存在(t *testing.T) {
	tmpDir := t.TempDir()
	// path 的结构：{workspaceDir}/context/{sessionID}_context/session_memory/session_context.md
	// 模板路径 = Dir(Dir(Dir(path))) + "/session_memory.md"
	// 即 {workspaceDir}/context/session_memory.md
	contextDir := filepath.Join(tmpDir, "context")
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	templatePath := filepath.Join(contextDir, "session_memory.md")
	templateContent := "# 自定义模板内容"
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("写入模板文件失败: %v", err)
	}

	// sessionDir 路径需符合三级 Dir 的要求
	sessionDir := filepath.Join(contextDir, "sess_context", "session_memory")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	path := filepath.Join(sessionDir, "session_context.md")
	result := readOrInitSessionMemory(path)
	if result != templateContent {
		t.Errorf("模板文件存在时应使用模板内容，期望 %q，实际前20字符 %q", templateContent, result[:min(20, len(result))])
	}
}

// ──────────────────────────── Offload 非内存类型测试 ────────────────────────────

// TestOffloadMessageBuffer_Offload非内存类型 测试非内存类型卸载
func TestOffloadMessageBuffer_Offload非内存类型(t *testing.T) {
	buf := NewOffloadMessageBuffer(nil)
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("test")}
	buf.Offload("handle1", "filesystem", msgs)
	// filesystem 类型暂不处理，不应存入 inMemoryMessages
	all := buf.GetAll()
	if len(all) != 0 {
		t.Errorf("filesystem 类型不应存入内存 map，实际 %d 个", len(all))
	}
}

// TestOffloadMessageBuffer_Reload不支持的类型 测试不支持的卸载类型
func TestOffloadMessageBuffer_Reload不支持的类型(t *testing.T) {
	buf := NewOffloadMessageBuffer(nil)
	result := buf.Reload("handle1", "unknown_type")
	if result != nil {
		t.Errorf("不支持的类型应返回 nil，实际 %v", result)
	}
}

// TestOffloadMessageBuffer_Reload从文件系统读取失败 测试文件系统读取无效 JSON
func TestOffloadMessageBuffer_Reload从文件系统读取失败(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "session1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	// 写入无效 JSON
	filePath := filepath.Join(sessionDir, "myhandle.json")
	if err := os.WriteFile(filePath, []byte("invalid json"), 0644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	buf := NewOffloadMessageBuffer(nil)
	buf.SetWorkspaceInfo(tmpDir, "session1")
	loaded := buf.Reload("myhandle", "filesystem")
	if loaded != nil {
		t.Errorf("无效 JSON 应返回 nil，实际 %v", loaded)
	}
}

// TestOffloadMessageBuffer_Clear非内存类型 测试清除非内存类型（空操作）
func TestOffloadMessageBuffer_Clear非内存类型(t *testing.T) {
	buf := NewOffloadMessageBuffer(nil)
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("test")}
	buf.Offload("handle1", "in_memory", msgs)
	// 清除非内存类型不应影响内存中的数据
	buf.Clear("handle1", "filesystem")
	all := buf.GetAll()
	if len(all) != 1 {
		t.Errorf("filesystem 类型 clear 不应影响内存数据，实际 %d 个", len(all))
	}
}

// ──────────────────────────── preparePendingSessionMemory 目录创建失败测试 ────────────────────────────

// TestPreparePendingSessionMemory_active文件存在时复制 测试 active 文件存在时优先复制
func TestPreparePendingSessionMemory_active文件存在时复制2(t *testing.T) {
	tmpDir := t.TempDir()
	activePath := filepath.Join(tmpDir, "session_context.md")
	pendingPath := filepath.Join(tmpDir, "subdir", "session_context.pending.md")

	content := "已有笔记"
	if err := os.WriteFile(activePath, []byte(content), 0644); err != nil {
		t.Fatalf("写入 active 文件失败: %v", err)
	}

	preparePendingSessionMemory(activePath, pendingPath, "默认")
	data, err := os.ReadFile(pendingPath)
	if err != nil {
		t.Fatalf("pending 文件应已创建: %v", err)
	}
	if string(data) != content {
		t.Errorf("应复制 active 内容，期望 %q，实际 %q", content, string(data))
	}
}

// ──────────────────────────── FormatReloadedMessages 序列化失败测试 ────────────────────────────

// TestFormatReloadedMessages_序列化失败 测试序列化失败时的处理
func TestFormatReloadedMessages_序列化失败(t *testing.T) {
	// 创建一条序列化会失败的消息（理论上不会，但测试分支覆盖）
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
	}
	result := FormatReloadedMessages("handle-123", messages)
	if !strings.Contains(result, "handle=handle-123") {
		t.Errorf("输出应包含 handle，实际: %s", result)
	}
}

// ──────────────────────────── SessionMemoryAgentUpdater 方法覆盖 ────────────────────────────

// TestSessionMemoryAgentUpdater_BindModelDefaults_实际调用 测试 BindModelDefaults 方法
func TestSessionMemoryAgentUpdater_BindModelDefaults_实际调用(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	cfg.UpdateMode = "agent_edit"
	updater := NewSessionMemoryAgentUpdater(cfg)
	// 不应 panic，空操作
	updater.BindModelDefaults(
		&llm_schema.ModelRequestConfig{ModelName: "test"},
		&llm_schema.ModelClientConfig{ClientProvider: "test"},
	)
}

// TestSessionMemoryAgentUpdater_SetInheritedSystemPrompt_实际调用 测试 SetInheritedSystemPrompt 方法
func TestSessionMemoryAgentUpdater_SetInheritedSystemPrompt_实际调用(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	cfg.UpdateMode = "agent_edit"
	updater := NewSessionMemoryAgentUpdater(cfg)
	// 不应 panic，空操作
	updater.SetInheritedSystemPrompt("测试系统提示词")
}

// ──────────────────────────── CollectContextWindow 测试 ────────────────────────────

// TestCollectContextWindow_有ModelContext 测试有 ModelContext 时收集消息
func TestCollectContextWindow_有ModelContext(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	mgr := NewSessionMemoryManager(cfg)
	msgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
		llm_schema.NewAssistantMessage("hi"),
	}
	mc := &mockModelContextForCountTokens{messages: msgs}
	window := mgr.CollectContextWindow(mc)
	if window == nil {
		t.Fatal("CollectContextWindow 不应返回 nil")
	}
	if len(window.ContextMessages) != 2 {
		t.Errorf("期望 2 条消息，实际 %d", len(window.ContextMessages))
	}
}

// ──────────────────────────── ShouldUpdate 增量触发测试 ────────────────────────────

// TestShouldUpdate_增量触发 测试增量达到阈值时触发更新
func TestShouldUpdate_增量触发(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	cfg.TriggerTokens = 10
	cfg.TriggerAddTokens = 5
	cfg.ToolMin = 1
	mgr := NewSessionMemoryManager(cfg)

	sess := session.NewSession()
	// 先初始化
	updateSessionMemoryRuntime(sess, map[string]any{
		"initialized":               true,
		"tokens_at_last_update":     5,
		"tool_calls_at_last_update": 0,
	})

	tc := &fakeTokenCounterForSessionMemory{count: 100}
	mc := &mockModelContextForCountTokens{tokenCounter: tc}

	msgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
		llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				llm_schema.NewToolCall("tc-1", "tool1", "{}"),
			}),
		),
		llm_schema.NewToolMessage("tc-1", "result"),
		llm_schema.NewAssistantMessage("done"),
	}

	window := &iface.ContextWindow{ContextMessages: msgs}
	result := mgr.ShouldUpdate(sess, mc, window)
	if !result {
		t.Error("增量达到阈值时应触发更新")
	}
}

// TestShouldUpdate_增量未达到阈值 测试增量未达到 token 阈值
func TestShouldUpdate_增量未达到阈值(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	cfg.TriggerTokens = 10
	cfg.TriggerAddTokens = 5000 // 很高的增量阈值
	cfg.ToolMin = 1
	mgr := NewSessionMemoryManager(cfg)

	sess := session.NewSession()
	updateSessionMemoryRuntime(sess, map[string]any{
		"initialized":               true,
		"tokens_at_last_update":     80,
		"tool_calls_at_last_update": 0,
	})

	tc := &fakeTokenCounterForSessionMemory{count: 100}
	mc := &mockModelContextForCountTokens{tokenCounter: tc}

	msgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
		llm_schema.NewAssistantMessage("hi"),
	}

	window := &iface.ContextWindow{ContextMessages: msgs}
	result := mgr.ShouldUpdate(sess, mc, window)
	if result {
		t.Error("增量未达到 token 阈值时不应触发更新")
	}
}

// TestShouldUpdate_增量toolCall未达到阈值 测试增量 tool_call 未达到阈值
func TestShouldUpdate_增量toolCall未达到阈值(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	cfg.TriggerTokens = 10
	cfg.TriggerAddTokens = 5
	cfg.ToolMin = 100 // 很高的 tool_call 阈值
	mgr := NewSessionMemoryManager(cfg)

	sess := session.NewSession()
	updateSessionMemoryRuntime(sess, map[string]any{
		"initialized":               true,
		"tokens_at_last_update":     5,
		"tool_calls_at_last_update": 0,
	})

	tc := &fakeTokenCounterForSessionMemory{count: 100}
	mc := &mockModelContextForCountTokens{tokenCounter: tc}

	msgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
		llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				llm_schema.NewToolCall("tc-1", "tool1", "{}"),
			}),
		),
		llm_schema.NewToolMessage("tc-1", "result"),
		llm_schema.NewAssistantMessage("done"),
	}

	window := &iface.ContextWindow{ContextMessages: msgs}
	result := mgr.ShouldUpdate(sess, mc, window)
	if result {
		t.Error("增量 tool_call 未达到阈值时不应触发更新")
	}
}

// TestShouldUpdate_基线重置token下降 测试 token 下降时基线重置
func TestShouldUpdate_基线重置token下降(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	cfg.TriggerTokens = 10
	cfg.TriggerAddTokens = 5
	cfg.ToolMin = 1
	mgr := NewSessionMemoryManager(cfg)

	sess := session.NewSession()
	updateSessionMemoryRuntime(sess, map[string]any{
		"initialized":               true,
		"tokens_at_last_update":     200, // 比当前高，触发 token 基线重置
		"tool_calls_at_last_update": 0,
	})

	tc := &fakeTokenCounterForSessionMemory{count: 100}
	mc := &mockModelContextForCountTokens{tokenCounter: tc}

	msgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
		llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				llm_schema.NewToolCall("tc-1", "tool1", "{}"),
			}),
		),
		llm_schema.NewToolMessage("tc-1", "result"),
		llm_schema.NewAssistantMessage("done"),
	}

	window := &iface.ContextWindow{ContextMessages: msgs}
	// 基线重置后 tokens_at_last_update 变为 0，增量变为 100 >= 5
	// tool_calls 增量 1 >= 1
	result := mgr.ShouldUpdate(sess, mc, window)
	if !result {
		t.Error("基线重置后应满足增量阈值条件")
	}
}

// ──────────────────────────── MaybeScheduleUpdate 更完整测试 ────────────────────────────

// TestMaybeScheduleUpdate_workspaceDir为空不调度 测试空 workspaceDir 时不调度
func TestMaybeScheduleUpdate_workspaceDir为空不调度(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	mgr := NewSessionMemoryManager(cfg)
	sess := session.NewSession()
	// workspaceDir 为空
	mgr.MaybeScheduleUpdate(context.Background(), sess, nil, "")
	if len(mgr.tasks) != 0 {
		t.Error("workspaceDir 为空时不应调度任务")
	}
}

// TestMaybeScheduleUpdate_session为nil 测试 session 为 nil 时不调度
func TestMaybeScheduleUpdate_session为nil(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	mgr := NewSessionMemoryManager(cfg)
	// sess 为 nil
	mgr.MaybeScheduleUpdate(context.Background(), nil, nil, "/workspace")
	if len(mgr.tasks) != 0 {
		t.Error("session 为 nil 时不应调度任务")
	}
}

// ──────────────────────────── updateBackground 测试 ────────────────────────────

// TestUpdateBackground_session为nil 测试 session 为 nil 时直接返回
func TestUpdateBackground_session为nil(t *testing.T) {
	cfg := NewSessionMemoryConfig()
	mgr := NewSessionMemoryManager(cfg)
	window := iface.NewContextWindow()
	// 不应 panic
	mgr.updateBackground(context.Background(), nil, nil, window, "/tmp/notes.md", "/tmp/notes.pending.md")
}

// TestUpdateBackground_基本执行 测试基本的后台更新流程
func TestUpdateBackground_基本执行(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := NewSessionMemoryConfig()
	cfg.UpdateMode = "agent_edit" // agent_edit 模式的 Invoke 会返回错误，但 updateBackground 应处理
	mgr := NewSessionMemoryManager(cfg)

	sess := session.NewSession()
	window := iface.NewContextWindow()
	window.ContextMessages = []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
	}

	notesPath := filepath.Join(tmpDir, "session_context.md")
	pendingPath := filepath.Join(tmpDir, "session_context.pending.md")

	// 执行更新（agent_edit 的 Invoke 会失败，但不应 panic）
	mgr.updateBackground(context.Background(), sess, nil, window, notesPath, pendingPath)

	// 验证 is_extracting 被重置
	runtime := GetSessionMemoryRuntime(sess)
	if getBoolFromMap(runtime, "is_extracting") {
		t.Error("更新完成后 is_extracting 应被重置为 false")
	}
}

// TestUpdateBackground_成功更新 测试使用 fakeUpdater 成功更新
func TestUpdateBackground_成功更新(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := NewSessionMemoryConfig()
	mgr := NewSessionMemoryManager(cfg)

	// 替换 updater 为 fake 成功 updater
	mgr.updater = &fakeSuccessUpdater{}

	sess := session.NewSession()
	window := iface.NewContextWindow()
	window.ContextMessages = []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hello"),
	}

	notesPath := filepath.Join(tmpDir, "session_context.md")
	pendingPath := filepath.Join(tmpDir, "session_context.pending.md")

	// 先创建 notes 文件
	if err := os.WriteFile(notesPath, []byte("# 原始笔记"), 0644); err != nil {
		t.Fatalf("写入笔记文件失败: %v", err)
	}

	mgr.updateBackground(context.Background(), sess, nil, window, notesPath, pendingPath)

	// 验证 is_extracting 被重置
	runtime := GetSessionMemoryRuntime(sess)
	if getBoolFromMap(runtime, "is_extracting") {
		t.Error("更新完成后 is_extracting 应被重置为 false")
	}
	// 验证运行时状态已更新
	if getIntFromMap(runtime, "tool_calls_at_last_update") != 0 {
		t.Errorf("tool_calls_at_last_update 应为 0，实际 %d", getIntFromMap(runtime, "tool_calls_at_last_update"))
	}
}

// fakeSuccessUpdater 模拟成功的 SessionMemoryUpdater
type fakeSuccessUpdater struct{}

func (f *fakeSuccessUpdater) Invoke(_ context.Context, _ SessionMemoryUpdateOptions) error {
	return nil
}
func (f *fakeSuccessUpdater) BindModelDefaults(_ *llm_schema.ModelRequestConfig, _ *llm_schema.ModelClientConfig) {
}
func (f *fakeSuccessUpdater) SetInheritedSystemPrompt(_ string) {}
