package rails

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/project_memory"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeBaseAgentForPMR 用于测试的 mock BaseAgent
type fakeBaseAgentForPMR struct {
	sb saprompt.SystemPromptBuilderInterface
}

func (f *fakeBaseAgentForPMR) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (f *fakeBaseAgentForPMR) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeBaseAgentForPMR) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (f *fakeBaseAgentForPMR) Card() *agentschema.AgentCard                            { return nil }
func (f *fakeBaseAgentForPMR) Config() agentinterfaces.AgentConfig                     { return nil }
func (f *fakeBaseAgentForPMR) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (f *fakeBaseAgentForPMR) CallbackManager() *agentinterfaces.AgentCallbackManager  { return nil }
func (f *fakeBaseAgentForPMR) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return f.sb
}
func (f *fakeBaseAgentForPMR) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ callback.PerAgentCallbackFunc, _ ...callback.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgentForPMR) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...callback.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgentForPMR) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

// ──────────────────────────── NewProjectMemoryRail 测试 ────────────────────────────

// TestNewProjectMemoryRail_默认值 验证构造函数默认值
func TestNewProjectMemoryRail_默认值(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "", 0, nil)
	if r.workspacePath != "/tmp/ws" {
		t.Errorf("workspacePath = %q, 期望 %q", r.workspacePath, "/tmp/ws")
	}
	if r.language != "cn" {
		t.Errorf("language = %q, 期望 %q", r.language, "cn")
	}
	if r.maxChars != project_memory.DefaultMaxChars {
		t.Errorf("maxChars = %d, 期望 %d", r.maxChars, project_memory.DefaultMaxChars)
	}
	if len(r.additionalDirectories) != 0 {
		t.Errorf("additionalDirectories 长度 = %d, 期望 0", len(r.additionalDirectories))
	}
}

// TestNewProjectMemoryRail_自定义值 验证构造函数自定义值
func TestNewProjectMemoryRail_自定义值(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "en", 30000, []string{"/extra"})
	if r.language != "en" {
		t.Errorf("language = %q, 期望 %q", r.language, "en")
	}
	if r.maxChars != 30000 {
		t.Errorf("maxChars = %d, 期望 30000", r.maxChars)
	}
	if len(r.additionalDirectories) != 1 || r.additionalDirectories[0] != "/extra" {
		t.Errorf("additionalDirectories = %v, 期望 [/extra]", r.additionalDirectories)
	}
}

// ──────────────────────────── Init/Uninit 测试 ────────────────────────────

// TestProjectMemoryRail_Init_正常 验证 Init 正常注入 systemPromptBuilder
func TestProjectMemoryRail_Init_正常(t *testing.T) {
	builder := saprompt.NewSystemPromptBuilder()
	agent := &fakeBaseAgentForPMR{sb: builder}
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, nil)

	err := r.Init(context.Background(), agent)
	if err != nil {
		t.Fatalf("Init 返回错误: %v", err)
	}
	if r.systemPromptBuilder == nil {
		t.Error("Init 后 systemPromptBuilder 不应为 nil")
	}
}

// TestProjectMemoryRail_Init_无Builder 验证 agent 无 SystemPromptBuilder 时不崩溃
func TestProjectMemoryRail_Init_无Builder(t *testing.T) {
	agent := &fakeBaseAgentForPMR{sb: nil}
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, nil)

	err := r.Init(context.Background(), agent)
	if err != nil {
		t.Fatalf("Init 返回错误: %v", err)
	}
	if r.systemPromptBuilder != nil {
		t.Error("无 SystemPromptBuilder 时 systemPromptBuilder 应为 nil")
	}
}

// TestProjectMemoryRail_Uninit_清除Section 验证 Uninit 清除 project_memory section
func TestProjectMemoryRail_Uninit_清除Section(t *testing.T) {
	builder := saprompt.NewSystemPromptBuilder()
	builder.AddSection(saprompt.PromptSection{
		Name:     project_memory.SectionName,
		Content:  map[string]string{"cn": "test", "en": "test"},
		Priority: 120,
	})

	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, nil)
	r.systemPromptBuilder = builder

	err := r.Uninit(nil)
	if err != nil {
		t.Fatalf("Uninit 返回错误: %v", err)
	}
	if builder.HasSection(project_memory.SectionName) {
		t.Error("Uninit 后 project_memory section 应被移除")
	}
}

// TestProjectMemoryRail_Uninit_无Builder 验证 systemPromptBuilder 为 nil 时不崩溃
func TestProjectMemoryRail_Uninit_无Builder(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, nil)
	// systemPromptBuilder 为 nil
	err := r.Uninit(nil)
	if err != nil {
		t.Fatalf("Uninit 返回错误: %v", err)
	}
}

// ──────────────────────────── SetLanguage/GetLanguage 测试 ────────────────────────────

// TestProjectMemoryRail_SetLanguage 验证语言更新
func TestProjectMemoryRail_SetLanguage(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, nil)

	r.SetLanguage("en")
	if r.language != "en" {
		t.Errorf("language = %q, 期望 %q", r.language, "en")
	}

	// 空字符串不变
	r.SetLanguage("")
	if r.language != "en" {
		t.Errorf("空字符串时 language = %q, 期望 %q", r.language, "en")
	}

	// 相同值不变
	r.SetLanguage("en")
	if r.language != "en" {
		t.Errorf("相同值时 language = %q, 期望 %q", r.language, "en")
	}
}

// TestProjectMemoryRail_GetLanguage 验证获取语言
func TestProjectMemoryRail_GetLanguage(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "en", 60000, nil)
	if r.GetLanguage() != "en" {
		t.Errorf("GetLanguage = %q, 期望 %q", r.GetLanguage(), "en")
	}
}

// ──────────────────────────── SetAdditionalDirectories 测试 ────────────────────────────

// TestProjectMemoryRail_SetAdditionalDirectories_合并 验证额外目录合并去重
func TestProjectMemoryRail_SetAdditionalDirectories_合并(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, []string{"/a", "/b"})

	r.SetAdditionalDirectories([]string{"/c", "/a"})

	if len(r.additionalDirectories) != 3 {
		t.Errorf("additionalDirectories 长度 = %d, 期望 3（/a 去重）", len(r.additionalDirectories))
	}
}

// TestProjectMemoryRail_SetAdditionalDirectories_nil 验证 nil 输入不崩溃
func TestProjectMemoryRail_SetAdditionalDirectories_nil(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, []string{"/a"})

	r.SetAdditionalDirectories(nil)

	if len(r.additionalDirectories) != 1 {
		t.Errorf("nil 输入后 additionalDirectories 长度 = %d, 期望 1", len(r.additionalDirectories))
	}
}

// ──────────────────────────── ResolveWorkspacePath 测试 ────────────────────────────

// TestProjectMemoryRail_ResolveWorkspacePath_构造期路径 验证使用构造期路径
func TestProjectMemoryRail_ResolveWorkspacePath_构造期路径(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, nil)
	if r.ResolveWorkspacePath() != "/tmp/ws" {
		t.Errorf("ResolveWorkspacePath = %q, 期望 %q", r.ResolveWorkspacePath(), "/tmp/ws")
	}
}

// TestProjectMemoryRail_ResolveWorkspacePath_Workspace注入 验证 Workspace 注入优先
func TestProjectMemoryRail_ResolveWorkspacePath_Workspace注入(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, nil)
	// 模拟 DeepAgent.register_rail 注入 Workspace
	w := workspace.NewWorkspace("/injected/ws", "cn")
	r.SetWorkspace(w)

	result := r.ResolveWorkspacePath()
	if result != "/injected/ws" {
		t.Errorf("ResolveWorkspacePath = %q, 期望 %q", result, "/injected/ws")
	}
}

// TestProjectMemoryRail_ResolveWorkspacePath_Workspace空路径 验证 Workspace RootPath 为空时回退
func TestProjectMemoryRail_ResolveWorkspacePath_Workspace空路径(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, nil)
	// Workspace 存在但 RootPath 为空
	w := &workspace.Workspace{RootPath: ""}
	r.SetWorkspace(w)

	result := r.ResolveWorkspacePath()
	if result != "/tmp/ws" {
		t.Errorf("RootPath 为空时 ResolveWorkspacePath = %q, 期望 %q", result, "/tmp/ws")
	}
}

// ──────────────────────────── BeforeModelCall 测试 ────────────────────────────

// TestProjectMemoryRail_BeforeModelCall_无Builder 验证无 builder 时跳过
func TestProjectMemoryRail_BeforeModelCall_无Builder(t *testing.T) {
	r := NewProjectMemoryRail("/nonexistent", "cn", 60000, nil)
	// systemPromptBuilder 为 nil
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	err := r.BeforeModelCall(context.Background(), cbc)
	if err != nil {
		t.Fatalf("BeforeModelCall 返回错误: %v", err)
	}
}

// TestProjectMemoryRail_BeforeModelCall_有UAPCLAWSWARMMD 验证存在 UAPCLAWSWARM.md 时注入 section
func TestProjectMemoryRail_BeforeModelCall_有UAPCLAWSWARMMD(t *testing.T) {
	// 创建临时目录和 UAPCLAWSWARM.md（需要 .git 目录作为项目根标记）
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	mdPath := filepath.Join(tmpDir, "UAPCLAWSWARM.md")
	if err := os.WriteFile(mdPath, []byte("# Test Memory\nHello from test"), 0644); err != nil {
		t.Fatalf("写入 UAPCLAWSWARM.md 失败: %v", err)
	}

	// 清理缓存
	project_memory.ClearProjectMemoryCache(tmpDir)

	builder := saprompt.NewSystemPromptBuilder()
	agent := &fakeBaseAgentForPMR{sb: builder}
	r := NewProjectMemoryRail(tmpDir, "cn", 60000, nil)
	r.systemPromptBuilder = builder

	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)
	err := r.BeforeModelCall(context.Background(), cbc)
	if err != nil {
		t.Fatalf("BeforeModelCall 返回错误: %v", err)
	}

	// 验证 project_memory section 已注入
	if !builder.HasSection(project_memory.SectionName) {
		t.Error("存在 UAPCLAWSWARM.md 时应注入 project_memory section")
	}

	section := builder.GetSection(project_memory.SectionName)
	if section == nil {
		t.Fatal("section 不应为 nil")
	}
	if section.Priority != sectionPriority {
		t.Errorf("section priority = %d, 期望 %d", section.Priority, sectionPriority)
	}
	// 验证 CN 内容包含 UAPCLAWSWARM.md 内容
	cnContent := section.Content["cn"]
	if cnContent == "" {
		t.Error("CN 内容不应为空")
	}
}

// TestProjectMemoryRail_BeforeModelCall_无记忆文件 验证无记忆文件时不注入 section
func TestProjectMemoryRail_BeforeModelCall_无记忆文件(t *testing.T) {
	tmpDir := t.TempDir()
	// 创建 .git 标记但不创建记忆文件
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	project_memory.ClearProjectMemoryCache(tmpDir)

	builder := saprompt.NewSystemPromptBuilder()
	agent := &fakeBaseAgentForPMR{sb: builder}
	r := NewProjectMemoryRail(tmpDir, "cn", 60000, nil)
	r.systemPromptBuilder = builder

	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)
	err := r.BeforeModelCall(context.Background(), cbc)
	if err != nil {
		t.Fatalf("BeforeModelCall 返回错误: %v", err)
	}

	if builder.HasSection(project_memory.SectionName) {
		t.Error("无记忆文件时不应注入 project_memory section")
	}
}

// TestProjectMemoryRail_BeforeModelCall_刷新旧Section 验证连续调用时清除旧 section 再注入
func TestProjectMemoryRail_BeforeModelCall_刷新旧Section(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	mdPath := filepath.Join(tmpDir, "UAPCLAWSWARM.md")
	if err := os.WriteFile(mdPath, []byte("initial content"), 0644); err != nil {
		t.Fatalf("写入 UAPCLAWSWARM.md 失败: %v", err)
	}
	project_memory.ClearProjectMemoryCache(tmpDir)

	builder := saprompt.NewSystemPromptBuilder()
	agent := &fakeBaseAgentForPMR{sb: builder}
	r := NewProjectMemoryRail(tmpDir, "cn", 60000, nil)
	r.systemPromptBuilder = builder

	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)

	// 第一次调用
	r.BeforeModelCall(context.Background(), cbc)
	if !builder.HasSection(project_memory.SectionName) {
		t.Fatal("第一次调用后应存在 project_memory section")
	}

	// 修改文件内容
	if err := os.WriteFile(mdPath, []byte("updated content"), 0644); err != nil {
		t.Fatalf("更新 UAPCLAWSWARM.md 失败: %v", err)
	}
	project_memory.ClearProjectMemoryCache(tmpDir)

	// 第二次调用：应刷新 section
	r.BeforeModelCall(context.Background(), cbc)
	if !builder.HasSection(project_memory.SectionName) {
		t.Error("第二次调用后应存在 project_memory section")
	}
}

// ──────────────────────────── AfterToolCall 测试 ────────────────────────────

// TestProjectMemoryRail_AfterToolCall_写工具 验证写操作工具触发缓存清除
func TestProjectMemoryRail_AfterToolCall_写工具(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	mdPath := filepath.Join(tmpDir, "UAPCLAWSWARM.md")
	if err := os.WriteFile(mdPath, []byte("test content"), 0644); err != nil {
		t.Fatalf("写入 UAPCLAWSWARM.md 失败: %v", err)
	}

	// 预热缓存
	_, _ = project_memory.DiscoverAndLoadMemoryFiles(context.Background(), tmpDir, tmpDir, nil)

	r := NewProjectMemoryRail(tmpDir, "cn", 60000, nil)

	for _, toolName := range []string{"write_file", "edit_file", "delete_file", "write", "delete", "move_file", "rename_file", "write_text_file"} {
		// 重新预热缓存
		_, _ = project_memory.DiscoverAndLoadMemoryFiles(context.Background(), tmpDir, tmpDir, nil)

		toolInputs := &agentinterfaces.ToolCallInputs{ToolName: toolName}
		cbc := agentinterfaces.NewAgentCallbackContext(nil, toolInputs, nil)

		err := r.AfterToolCall(context.Background(), cbc)
		if err != nil {
			t.Errorf("AfterToolCall(%s) 返回错误: %v", toolName, err)
		}
	}
}

// TestProjectMemoryRail_AfterToolCall_非写工具 验证非写操作工具不触发缓存清除
func TestProjectMemoryRail_AfterToolCall_非写工具(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, nil)

	toolInputs := &agentinterfaces.ToolCallInputs{ToolName: "read_file"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, toolInputs, nil)

	err := r.AfterToolCall(context.Background(), cbc)
	if err != nil {
		t.Fatalf("AfterToolCall 返回错误: %v", err)
	}
	// 无 panic 即可
}

// TestProjectMemoryRail_AfterToolCall_nilCbc 验证 cbc 为 nil 时不崩溃
func TestProjectMemoryRail_AfterToolCall_nilCbc(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, nil)
	err := r.AfterToolCall(context.Background(), nil)
	if err != nil {
		t.Fatalf("AfterToolCall(nil) 返回错误: %v", err)
	}
}

// TestProjectMemoryRail_AfterToolCall_非ToolCallInputs 验证非 ToolCallInputs 时跳过
func TestProjectMemoryRail_AfterToolCall_非ToolCallInputs(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, nil)

	modelInputs := &agentinterfaces.ModelCallInputs{}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, modelInputs, nil)

	err := r.AfterToolCall(context.Background(), cbc)
	if err != nil {
		t.Fatalf("AfterToolCall 返回错误: %v", err)
	}
}

// ──────────────────────────── GetCallbacks 测试 ────────────────────────────

// TestProjectMemoryRail_GetCallbacks 验证 GetCallbacks 注册了 before_model_call + after_tool_call
func TestProjectMemoryRail_GetCallbacks(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, nil)
	callbacks := r.GetCallbacks()

	if _, ok := callbacks[agentinterfaces.CallbackBeforeModelCall]; !ok {
		t.Error("GetCallbacks 缺少 before_model_call")
	}
	if _, ok := callbacks[agentinterfaces.CallbackAfterToolCall]; !ok {
		t.Error("GetCallbacks 缺少 after_tool_call")
	}
}

// ──────────────────────────── writeLikeTools 测试 ────────────────────────────

// TestWriteLikeTools_完整性 验证 writeLikeTools 包含所有 Python 定义的写工具
func TestWriteLikeTools_完整性(t *testing.T) {
	expected := []string{"write_file", "edit_file", "write_text_file", "write", "delete_file", "delete", "move_file", "rename_file"}
	for _, name := range expected {
		if _, ok := writeLikeTools[name]; !ok {
			t.Errorf("writeLikeTools 缺少 %q", name)
		}
	}
	if len(writeLikeTools) != len(expected) {
		t.Errorf("writeLikeTools 长度 = %d, 期望 %d", len(writeLikeTools), len(expected))
	}
}

// ──────────────────────────── safeResolveDir 测试 ────────────────────────────

// TestSafeResolveDir_存在目录 验证解析存在的目录
func TestSafeResolveDir_存在目录(t *testing.T) {
	tmpDir := t.TempDir()
	result := safeResolveDir(tmpDir)
	if result == "" {
		t.Error("存在目录应返回非空路径")
	}
}

// TestSafeResolveDir_不存在 验证不存在的路径返回空
func TestSafeResolveDir_不存在(t *testing.T) {
	result := safeResolveDir("/nonexistent/path/xyz")
	if result != "" {
		t.Errorf("不存在路径应返回空，实际 %q", result)
	}
}

// TestSafeResolveDir_空字符串 验证空输入返回空
func TestSafeResolveDir_空字符串(t *testing.T) {
	result := safeResolveDir("")
	if result != "" {
		t.Errorf("空字符串应返回空，实际 %q", result)
	}
}

// TestSafeResolveDir_文件路径 验证文件路径返回空
func TestSafeResolveDir_文件路径(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatalf("创建临时文件失败: %v", err)
	}
	result := safeResolveDir(tmpFile)
	if result != "" {
		t.Errorf("文件路径应返回空，实际 %q", result)
	}
}

// ──────────────────────────── 编译时接口验证 ────────────────────────────

// TestProjectMemoryRail_满足AgentRail接口 编译时验证
func TestProjectMemoryRail_满足AgentRail接口(t *testing.T) {
	// 此测试确保 ProjectMemoryRail 满足 AgentRail 接口
	var _ agentinterfaces.AgentRail = (*ProjectMemoryRail)(nil)
}

// ──────────────────────────── 集成：完整生命周期测试 ────────────────────────────

// TestProjectMemoryRail_完整生命周期 验证 Init → BeforeModelCall → AfterToolCall → Uninit 完整流程
func TestProjectMemoryRail_完整生命周期(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	mdPath := filepath.Join(tmpDir, "UAPCLAWSWARM.md")
	if err := os.WriteFile(mdPath, []byte("# Lifecycle Test\nProject memory content"), 0644); err != nil {
		t.Fatalf("写入 UAPCLAWSWARM.md 失败: %v", err)
	}
	project_memory.ClearProjectMemoryCache(tmpDir)

	builder := saprompt.NewSystemPromptBuilder()
	agent := &fakeBaseAgentForPMR{sb: builder}
	r := NewProjectMemoryRail(tmpDir, "cn", 60000, []string{})

	// 1. Init
	if err := r.Init(context.Background(), agent); err != nil {
		t.Fatalf("Init 失败: %v", err)
	}

	// 2. BeforeModelCall
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ModelCallInputs{}, nil)
	if err := r.BeforeModelCall(context.Background(), cbc); err != nil {
		t.Fatalf("BeforeModelCall 失败: %v", err)
	}
	if !builder.HasSection(project_memory.SectionName) {
		t.Error("BeforeModelCall 后应存在 project_memory section")
	}

	// 3. AfterToolCall（写工具）
	toolInputs := &agentinterfaces.ToolCallInputs{ToolName: "write_file"}
	cbc2 := agentinterfaces.NewAgentCallbackContext(nil, toolInputs, nil)
	if err := r.AfterToolCall(context.Background(), cbc2); err != nil {
		t.Fatalf("AfterToolCall 失败: %v", err)
	}

	// 4. 再次 BeforeModelCall（应重新加载）
	project_memory.ClearProjectMemoryCache(tmpDir)
	if err := r.BeforeModelCall(context.Background(), cbc); err != nil {
		t.Fatalf("第二次 BeforeModelCall 失败: %v", err)
	}
	if !builder.HasSection(project_memory.SectionName) {
		t.Error("第二次 BeforeModelCall 后应存在 project_memory section")
	}

	// 5. Uninit
	if err := r.Uninit(nil); err != nil {
		t.Fatalf("Uninit 失败: %v", err)
	}
	if builder.HasSection(project_memory.SectionName) {
		t.Error("Uninit 后 project_memory section 应被移除")
	}
}

// TestProjectMemoryRail_AfterToolCall_ToolCallInputsNone 验证 ToolCallInputs 中 ToolName 为空时跳过
func TestProjectMemoryRail_AfterToolCall_ToolName为空(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, nil)
	toolInputs := &agentinterfaces.ToolCallInputs{ToolName: ""}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, toolInputs, nil)

	err := r.AfterToolCall(context.Background(), cbc)
	if err != nil {
		t.Fatalf("AfterToolCall 返回错误: %v", err)
	}
}

// TestProjectMemoryRail_sectionPriority 验证 section 优先级对齐 Python
func TestProjectMemoryRail_sectionPriority(t *testing.T) {
	if sectionPriority != 120 {
		t.Errorf("sectionPriority = %d, 期望 120（对齐 Python SECTION_PRIORITY）", sectionPriority)
	}
}

// TestProjectMemoryRail_GetCallbacks_可调用 验证 GetCallbacks 返回的函数可执行
func TestProjectMemoryRail_GetCallbacks_可调用(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, nil)
	callbacks := r.GetCallbacks()

	// 验证 before_model_call 回调可执行
	bmcFn, ok := callbacks[agentinterfaces.CallbackBeforeModelCall]
	if !ok {
		t.Fatal("GetCallbacks 缺少 before_model_call")
	}
	// 在无 builder 情况下执行应返回 nil（不崩溃）
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{}, nil)
	if err := bmcFn(context.Background(), cbc); err != nil {
		t.Errorf("before_model_call 回调执行返回错误: %v", err)
	}

	// 验证 after_tool_call 回调可执行
	atcFn, ok := callbacks[agentinterfaces.CallbackAfterToolCall]
	if !ok {
		t.Fatal("GetCallbacks 缺少 after_tool_call")
	}
	toolInputs := &agentinterfaces.ToolCallInputs{ToolName: "read_file"}
	cbc2 := agentinterfaces.NewAgentCallbackContext(nil, toolInputs, nil)
	if err := atcFn(context.Background(), cbc2); err != nil {
		t.Errorf("after_tool_call 回调执行返回错误: %v", err)
	}
}

// TestProjectMemoryRail_Uninit_panicRecover 验证 Uninit 中 remove_section panic 时防御恢复
func TestProjectMemoryRail_Uninit_panicRecover(t *testing.T) {
	// 使用一个会在 RemoveSection 时 panic 的 builder
	panicBuilder := &panicRemoveBuilder{}
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, nil)
	r.systemPromptBuilder = panicBuilder

	err := r.Uninit(nil)
	if err != nil {
		t.Fatalf("Uninit 在 panic 时应返回 nil（防御恢复），实际: %v", err)
	}
}

// panicRemoveBuilder 在 RemoveSection 时 panic 的 mock builder
type panicRemoveBuilder struct{}

func (p *panicRemoveBuilder) AddSection(_ saprompt.PromptSection) *saprompt.SystemPromptBuilder {
	return nil
}
func (p *panicRemoveBuilder) RemoveSection(_ string) *saprompt.SystemPromptBuilder {
	panic("intentional panic for test")
}
func (p *panicRemoveBuilder) Language() string                            { return "cn" }
func (p *panicRemoveBuilder) SetLanguage(_ string)                        {}
func (p *panicRemoveBuilder) GetSection(_ string) *saprompt.PromptSection { return nil }
func (p *panicRemoveBuilder) HasSection(_ string) bool                    { return false }

// TestProjectMemoryRail_SetAdditionalDirectories_空白路径 验证空白路径跳过
func TestProjectMemoryRail_SetAdditionalDirectories_空白路径(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, []string{"/a"})
	r.SetAdditionalDirectories([]string{"  ", ""})

	// 只有 /a，空白被跳过
	if len(r.additionalDirectories) != 1 {
		t.Errorf("additionalDirectories 长度 = %d, 期望 1", len(r.additionalDirectories))
	}
}

// TestProjectMemoryRail_BeforeModelCall_panicRecover 验证 BeforeModelCall 中 remove_section panic 时防御恢复
func TestProjectMemoryRail_BeforeModelCall_panicRecover(t *testing.T) {
	panicBuilder := &panicRemoveBuilder{}
	r := NewProjectMemoryRail("/nonexistent", "cn", 60000, nil)
	r.systemPromptBuilder = panicBuilder

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{}, nil)
	// 不应崩溃
	err := r.BeforeModelCall(context.Background(), cbc)
	if err != nil {
		t.Fatalf("BeforeModelCall 在 panic 时应返回 nil（防御恢复），实际: %v", err)
	}
}

// TestProjectMemoryRail_Init_nilAgent 验证 agent 为 nil 时不崩溃
func TestProjectMemoryRail_Init_nilAgent(t *testing.T) {
	r := NewProjectMemoryRail("/tmp/ws", "cn", 60000, nil)
	err := r.Init(context.Background(), nil)
	if err != nil {
		t.Fatalf("Init(nil agent) 返回错误: %v", err)
	}
	if r.systemPromptBuilder != nil {
		t.Error("nil agent 时 systemPromptBuilder 应为 nil")
	}
}
