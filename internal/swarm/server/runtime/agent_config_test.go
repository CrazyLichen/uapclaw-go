package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/types"
)

// ──────────────────────────── 测试用例 ────────────────────────────

// TestAgentSource_Constants 测试 AgentSource 常量值
func TestAgentSource_Constants(t *testing.T) {
	assert.Equal(t, "builtin", types.AgentSourceBuiltin)
	assert.Equal(t, "user", types.AgentSourceUser)
	assert.Equal(t, "project", types.AgentSourceProject)
	assert.Equal(t, "local", types.AgentSourceLocal)
}

// TestBuiltinAgents 测试内置 Agent 定义
func TestBuiltinAgents(t *testing.T) {
	require.Len(t, types.BuiltinAgents, 3, "应有 3 个内置 agent")

	names := make([]string, len(types.BuiltinAgents))
	for i, a := range types.BuiltinAgents {
		names[i] = a.Name
		assert.Equal(t, types.AgentSourceBuiltin, a.Source)
		assert.NotEmpty(t, a.Description)
		assert.NotEmpty(t, a.Prompt)
		assert.NotEmpty(t, a.Tools)
	}
	assert.Contains(t, names, "general-purpose")
	assert.Contains(t, names, "Explore")
	assert.Contains(t, names, "Plan")
}

// TestCopyBuiltinAgents 测试深拷贝内置 agent 列表
func TestCopyBuiltinAgents(t *testing.T) {
	copied := copyBuiltinAgents()
	require.Len(t, copied, len(types.BuiltinAgents))

	// 修改拷贝不影响原始
	copied[0].Name = "modified"
	assert.Equal(t, "general-purpose", types.BuiltinAgents[0].Name, "原始列表不应被修改")

	// 修改 tools 切片不影响原始
	copied[0].Tools[0] = "modified-tool"
	assert.Equal(t, []string{"*"}, types.BuiltinAgents[0].Tools, "原始 tools 不应被修改")
}

// TestNewAgentConfigService 测试创建 AgentConfigService
func TestNewAgentConfigService(t *testing.T) {
	svc := NewAgentConfigService("/tmp/test-workspace")
	assert.Equal(t, "/tmp/test-workspace", svc.workspaceDir)
}

// TestAgentConfigService_DirPaths 测试目录路径计算
func TestAgentConfigService_DirPaths(t *testing.T) {
	svc := NewAgentConfigService("/workspace")

	// projectAgentsDir: <workspace>/.uapclaw/agents/
	assert.Contains(t, svc.projectAgentsDir(), ".uapclaw")
	assert.Contains(t, svc.projectAgentsDir(), "agents")
	assert.Equal(t, filepath.Join("/workspace", ".uapclaw", "agents"), svc.projectAgentsDir())

	// localAgentsDir: <workspace>/.uapclaw/agents-local/
	assert.Contains(t, svc.localAgentsDir(), "agents-local")
	assert.Equal(t, filepath.Join("/workspace", ".uapclaw", "agents-local"), svc.localAgentsDir())

	// userAgentsDir: ~/.uapclaw/agents/
	userDir := svc.userAgentsDir()
	assert.Contains(t, userDir, ".uapclaw")
	assert.Contains(t, userDir, "agents")
}

// TestAgentConfigService_ResolveLocationDir 测试位置目录解析
func TestAgentConfigService_ResolveLocationDir(t *testing.T) {
	svc := NewAgentConfigService("/workspace")

	dir, err := svc.resolveLocationDir(types.AgentSourceUser)
	assert.NoError(t, err)
	assert.Equal(t, svc.userAgentsDir(), dir)

	dir, err = svc.resolveLocationDir(types.AgentSourceProject)
	assert.NoError(t, err)
	assert.Equal(t, svc.projectAgentsDir(), dir)

	dir, err = svc.resolveLocationDir(types.AgentSourceLocal)
	assert.NoError(t, err)
	assert.Equal(t, svc.localAgentsDir(), dir)

	// 无效 location 应返回 error，对齐 Python: raise ValueError
	_, err = svc.resolveLocationDir(types.AgentSourceBuiltin)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "无效的 location")

	_, err = svc.resolveLocationDir("unknown")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "无效的 location")
}

// TestParseAgentFile_Valid 测试解析有效的 agent 文件
func TestParseAgentFile_Valid(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "my-agent.md")
	content := "---\nname: my-agent\ndescription: 测试 agent\nwhen_to_use: 当需要测试时\nmodel: qwen-max\ntools:\n  - Read\n  - Bash\ncolor: blue\npermission_mode: default\nmemory_scope: session\n---\n\n你是测试 agent。\n"
	err := os.WriteFile(filePath, []byte(content), 0o644)
	require.NoError(t, err)

	agent, err := parseAgentFile(filePath, types.AgentSourceLocal)
	require.NoError(t, err)
	require.NotNil(t, agent)

	assert.Equal(t, "my-agent", agent.Name)
	assert.Equal(t, "测试 agent", agent.Description)
	assert.Equal(t, "你是测试 agent。", agent.Prompt)
	assert.Equal(t, types.AgentSourceLocal, agent.Source)
	assert.Equal(t, filePath, agent.FilePath)
	assert.Equal(t, "qwen-max", agent.Model)
	assert.Equal(t, []string{"Read", "Bash"}, agent.Tools)
	assert.Equal(t, "blue", agent.Color)
	assert.Equal(t, "default", agent.PermissionMode)
	assert.Equal(t, "session", agent.MemoryScope)
	assert.Equal(t, "当需要测试时", agent.WhenToUse)
}

// TestParseAgentFile_NoFrontmatter 测试无 frontmatter 的文件
func TestParseAgentFile_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "no-frontmatter.md")
	err := os.WriteFile(filePath, []byte("just plain text"), 0o644)
	require.NoError(t, err)

	agent, err := parseAgentFile(filePath, types.AgentSourceLocal)
	assert.NoError(t, err)
	assert.Nil(t, agent)
}

// TestParseAgentFile_MissingDelimiter 测试缺少结束分隔符的文件
func TestParseAgentFile_MissingDelimiter(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "no-close.md")
	err := os.WriteFile(filePath, []byte("---\nname: test\n"), 0o644)
	require.NoError(t, err)

	agent, err := parseAgentFile(filePath, types.AgentSourceLocal)
	assert.NoError(t, err)
	assert.Nil(t, agent)
}

// TestParseAgentFile_NoName 测试缺少 name 字段的文件
func TestParseAgentFile_NoName(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "no-name.md")
	content := "---\ndescription: 没有 name 的 agent\n---\n\nprompt\n"
	err := os.WriteFile(filePath, []byte(content), 0o644)
	require.NoError(t, err)

	agent, err := parseAgentFile(filePath, types.AgentSourceLocal)
	assert.NoError(t, err)
	assert.Nil(t, agent)
}

// TestParseAgentFile_DefaultTools 测试默认 tools 为 ["*"]
func TestParseAgentFile_DefaultTools(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "default-tools.md")
	content := "---\nname: default-tools-agent\ndescription: 测试默认工具\n---\n\nprompt\n"
	err := os.WriteFile(filePath, []byte(content), 0o644)
	require.NoError(t, err)

	agent, err := parseAgentFile(filePath, types.AgentSourceLocal)
	require.NoError(t, err)
	require.NotNil(t, agent)
	assert.Equal(t, []string{"*"}, agent.Tools)
}

// TestParseAgentFile_MaxIterations 测试解析 max_iterations 字段
func TestParseAgentFile_MaxIterations(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "max-iter.md")
	content := "---\nname: max-iter-agent\ndescription: 测试迭代次数\nmax_iterations: 5\n---\n\nprompt\n"
	err := os.WriteFile(filePath, []byte(content), 0o644)
	require.NoError(t, err)

	agent, err := parseAgentFile(filePath, types.AgentSourceLocal)
	require.NoError(t, err)
	require.NotNil(t, agent)
	require.NotNil(t, agent.MaxIterations)
	assert.Equal(t, 5, *agent.MaxIterations)
}

// TestParseAgentFile_Skills 测试解析 skills 字段
func TestParseAgentFile_Skills(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "skills.md")
	content := "---\nname: skills-agent\ndescription: 测试 skills\nskills:\n  - commit\n  - review\n---\n\nprompt\n"
	err := os.WriteFile(filePath, []byte(content), 0o644)
	require.NoError(t, err)

	agent, err := parseAgentFile(filePath, types.AgentSourceLocal)
	require.NoError(t, err)
	require.NotNil(t, agent)
	assert.Equal(t, []string{"commit", "review"}, agent.Skills)
}

// TestFormatAgentFile_基本字段 测试从 AgentDefinition 生成文件内容
func TestFormatAgentFile_基本字段(t *testing.T) {
	def := &types.AgentDefinition{
		Name:        "test-agent",
		Description: "测试描述",
		Prompt:      "你是一个测试 agent。",
		Source:      types.AgentSourceLocal,
		Tools:       []string{"Read", "Bash"},
		WhenToUse:   "当需要测试时",
		Model:       "qwen-max",
		Color:       "green",
	}

	content := formatAgentFile(def)
	assert.Contains(t, content, "---")
	assert.Contains(t, content, "name: test-agent")
	assert.Contains(t, content, "description: 测试描述")
	assert.Contains(t, content, "when_to_use: 当需要测试时")
	assert.Contains(t, content, "model: qwen-max")
	assert.Contains(t, content, "color: green")
	assert.Contains(t, content, "你是一个测试 agent。")
}

// TestFormatAgentFile_DefaultToolsOmitted 测试 tools=["*"] 不写入 frontmatter
func TestFormatAgentFile_DefaultToolsOmitted(t *testing.T) {
	def := &types.AgentDefinition{
		Name:        "default-tools",
		Description: "默认工具",
		Prompt:      "prompt",
		Tools:       []string{"*"},
	}

	content := formatAgentFile(def)
	assert.NotContains(t, content, "tools:")
}

// TestFormatAgentFile_完整字段 测试从 AgentDefinition 生成含全部可选字段的内容
func TestFormatAgentFile_完整字段(t *testing.T) {
	maxIter := 10
	def := &types.AgentDefinition{
		Name:            "def-agent",
		Description:     "定义描述",
		Prompt:          "prompt 内容",
		WhenToUse:       "何时使用",
		Model:           "deepseek",
		Tools:           []string{"Read", "Grep"},
		DisallowedTools: []string{"Write"},
		MaxIterations:   &maxIter,
		Skills:          []string{"commit"},
	}

	content := formatAgentFile(def)
	assert.Contains(t, content, "name: def-agent")
	assert.Contains(t, content, "tools:")
	assert.Contains(t, content, "disallowed_tools:")
	assert.Contains(t, content, "max_iterations: 10")
	assert.Contains(t, content, "skills:")
	assert.Contains(t, content, "prompt 内容")
}

// TestApplyUpdateParams 测试更新参数应用
func TestApplyUpdateParams(t *testing.T) {
	agent := &types.AgentDefinition{
		Name:        "test",
		Description: "原始描述",
		Prompt:      "原始 prompt",
		Tools:       []string{"*"},
	}

	desc := "新描述"
	prompt := "新 prompt"
	model := "new-model"

	params := &UpdateAgentParams{
		Description: &desc,
		Prompt:      &prompt,
		Model:       &model,
	}

	applyUpdateParams(agent, params)
	assert.Equal(t, "新描述", agent.Description)
	assert.Equal(t, "新 prompt", agent.Prompt)
	assert.Equal(t, "new-model", agent.Model)
	// 未修改的字段保持原值
	assert.Equal(t, []string{"*"}, agent.Tools)
}

// TestApplyUpdateParams_PartialUpdate 测试部分更新
func TestApplyUpdateParams_PartialUpdate(t *testing.T) {
	agent := &types.AgentDefinition{
		Name:        "test",
		Description: "原始描述",
		Prompt:      "原始 prompt",
		Model:       "原始 model",
	}

	// 只更新 description
	desc := "新描述"
	params := &UpdateAgentParams{
		Description: &desc,
	}

	applyUpdateParams(agent, params)
	assert.Equal(t, "新描述", agent.Description)
	assert.Equal(t, "原始 prompt", agent.Prompt)
	assert.Equal(t, "原始 model", agent.Model)
}

// TestAgentConfigService_CreateAgent 测试创建 agent
func TestAgentConfigService_CreateAgent(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	params := &CreateAgentParams{
		Name:        "my-custom-agent",
		Description: "自定义测试 agent",
		Prompt:      "你是一个自定义 agent。",
		Location:    types.AgentSourceLocal,
		Tools:       []string{"Read", "Bash", "Grep"},
	}

	agent, err := svc.CreateAgent(params)
	require.NoError(t, err)
	assert.Equal(t, "my-custom-agent", agent.Name)
	assert.Equal(t, "自定义测试 agent", agent.Description)
	assert.Equal(t, "你是一个自定义 agent。", agent.Prompt)
	assert.Equal(t, types.AgentSourceLocal, agent.Source)
	assert.Equal(t, []string{"Read", "Bash", "Grep"}, agent.Tools)
	assert.NotEmpty(t, agent.FilePath)

	// 验证文件确实被创建
	_, err = os.Stat(agent.FilePath)
	assert.NoError(t, err)
}

// TestAgentConfigService_CreateAgent_InvalidName 测试名称校验
func TestAgentConfigService_CreateAgent_InvalidName(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	tests := []struct {
		name      string
		agentName string
	}{
		{"太短", "ab"},
		{"太长", "this-name-is-way-too-long-for-an-agent-name-it-exceeds-fifty-characters"},
		{"含特殊字符", "my agent!"},
		{"含空格", "my agent"},
		{"含点号", "my.agent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := &CreateAgentParams{
				Name:        tt.agentName,
				Description: "描述",
				Prompt:      "prompt",
				Location:    types.AgentSourceLocal,
			}
			_, err := svc.CreateAgent(params)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "格式无效")
		})
	}
}

// TestAgentConfigService_CreateAgent_ValidName 测试合法名称
func TestAgentConfigService_CreateAgent_ValidName(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	tests := []struct {
		name      string
		agentName string
	}{
		{"字母数字连字符", "my-agent-01"},
		{"下划线", "my_agent"},
		{"三个字符", "abc"},
		{"50字符", "a2345678901234567890123456789012345678901234567890"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := &CreateAgentParams{
				Name:        tt.agentName,
				Description: "描述",
				Prompt:      "prompt",
				Location:    types.AgentSourceLocal,
			}
			_, err := svc.CreateAgent(params)
			assert.NoError(t, err)
		})
	}
}

// TestAgentConfigService_CreateAgent_OverrideBuiltin 测试不能覆盖内置 agent
func TestAgentConfigService_CreateAgent_OverrideBuiltin(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	params := &CreateAgentParams{
		Name:        "general-purpose",
		Description: "试图覆盖内置",
		Prompt:      "prompt",
		Location:    types.AgentSourceLocal,
	}

	_, err := svc.CreateAgent(params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能覆盖内置 agent")
}

// TestAgentConfigService_CreateAgent_DefaultTools 测试创建时默认 tools
func TestAgentConfigService_CreateAgent_DefaultTools(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	params := &CreateAgentParams{
		Name:        "no-tools-agent",
		Description: "没有指定 tools",
		Prompt:      "prompt",
		Location:    types.AgentSourceLocal,
	}

	agent, err := svc.CreateAgent(params)
	require.NoError(t, err)
	assert.Equal(t, []string{"*"}, agent.Tools)
}

// TestAgentConfigService_ListAgents_BuiltinOnly 测试列出内置 agent
func TestAgentConfigService_ListAgents_BuiltinOnly(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	agents := svc.ListAgents()
	assert.GreaterOrEqual(t, len(agents), 3, "至少应有 3 个内置 agent")

	// 验证所有内置 agent 都存在
	builtinNames := make(map[string]bool)
	for _, a := range agents {
		if a.Source == types.AgentSourceBuiltin {
			builtinNames[a.Name] = true
		}
	}
	assert.True(t, builtinNames["general-purpose"])
	assert.True(t, builtinNames["Explore"])
	assert.True(t, builtinNames["Plan"])
}

// TestAgentConfigService_ListAgents_WithCustomAgents 测试包含自定义 agent 的列表
func TestAgentConfigService_ListAgents_WithCustomAgents(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	// 创建自定义 agent
	params := &CreateAgentParams{
		Name:        "custom-1",
		Description: "自定义 agent 1",
		Prompt:      "prompt 1",
		Location:    types.AgentSourceLocal,
	}
	_, err := svc.CreateAgent(params)
	require.NoError(t, err)

	agents := svc.ListAgents()
	customFound := false
	for _, a := range agents {
		if a.Name == "custom-1" {
			customFound = true
			assert.Equal(t, types.AgentSourceLocal, a.Source)
			assert.Empty(t, a.ShadowedBy)
		}
	}
	assert.True(t, customFound, "应找到自定义 agent")
}

// TestAgentConfigService_ListAgents_Shadowing 测试同名 agent 覆盖
func TestAgentConfigService_ListAgents_Shadowing(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	// 创建与内置 agent 同名的自定义 agent（使用 Explore 以外的名称避免内置覆盖检测）
	// 先创建一个非内置名称的 agent，再在另一个位置创建同名 agent

	// 在 local 位置创建
	localDir := svc.localAgentsDir()
	require.NoError(t, os.MkdirAll(localDir, 0o755))
	content := "---\nname: test-shadow\ndescription: local 版本\n---\n\nlocal prompt\n"
	require.NoError(t, os.WriteFile(filepath.Join(localDir, "test-shadow.md"), []byte(content), 0o644))

	// 在 project 位置创建同名 agent
	projectDir := svc.projectAgentsDir()
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	content2 := "---\nname: test-shadow\ndescription: project 版本\n---\n\nproject prompt\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "test-shadow.md"), []byte(content2), 0o644))

	agents := svc.ListAgents()
	var localAgent, projectAgent *types.AgentDefinition
	for _, a := range agents {
		if a.Name == "test-shadow" && a.Source == types.AgentSourceLocal {
			localAgent = a
		}
		if a.Name == "test-shadow" && a.Source == types.AgentSourceProject {
			projectAgent = a
		}
	}
	require.NotNil(t, localAgent)
	require.NotNil(t, projectAgent)

	// local 被 project 覆盖
	assert.Equal(t, types.AgentSourceProject, localAgent.ShadowedBy)
	assert.Empty(t, projectAgent.ShadowedBy)
}

// TestAgentConfigService_GetAgent 测试获取单个 agent
func TestAgentConfigService_GetAgent(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	// 获取内置 agent
	agent := svc.GetAgent("general-purpose")
	require.NotNil(t, agent)
	assert.Equal(t, "general-purpose", agent.Name)
	assert.Equal(t, types.AgentSourceBuiltin, agent.Source)
	assert.Empty(t, agent.ShadowedBy)

	// 获取不存在的 agent
	agent = svc.GetAgent("non-existent")
	assert.Nil(t, agent)
}

// TestAgentConfigService_UpdateAgent 测试更新 agent
func TestAgentConfigService_UpdateAgent(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	// 先创建
	createParams := &CreateAgentParams{
		Name:        "update-test",
		Description: "原始描述",
		Prompt:      "原始 prompt",
		Location:    types.AgentSourceLocal,
	}
	_, err := svc.CreateAgent(createParams)
	require.NoError(t, err)

	// 更新
	newDesc := "更新后的描述"
	newModel := "qwen-max"
	updateParams := &UpdateAgentParams{
		Description: &newDesc,
		Model:       &newModel,
	}
	agent, err := svc.UpdateAgent("update-test", updateParams)
	require.NoError(t, err)
	assert.Equal(t, "更新后的描述", agent.Description)
	assert.Equal(t, "qwen-max", agent.Model)
	// 未修改的字段保持不变
	assert.Equal(t, "原始 prompt", agent.Prompt)
}

// TestAgentConfigService_UpdateAgent_Builtin 测试不能更新内置 agent
func TestAgentConfigService_UpdateAgent_Builtin(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	desc := "新描述"
	params := &UpdateAgentParams{Description: &desc}
	_, err := svc.UpdateAgent("general-purpose", params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能修改内置 agent")
}

// TestAgentConfigService_UpdateAgent_NotFound 测试更新不存在的 agent
func TestAgentConfigService_UpdateAgent_NotFound(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	desc := "新描述"
	params := &UpdateAgentParams{Description: &desc}
	_, err := svc.UpdateAgent("non-existent", params)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不存在")
}

// TestAgentConfigService_DeleteAgent 测试删除 agent
func TestAgentConfigService_DeleteAgent(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	// 先创建
	createParams := &CreateAgentParams{
		Name:        "delete-test",
		Description: "待删除",
		Prompt:      "prompt",
		Location:    types.AgentSourceLocal,
	}
	created, err := svc.CreateAgent(createParams)
	require.NoError(t, err)

	// 确认文件存在
	_, err = os.Stat(created.FilePath)
	require.NoError(t, err)

	// 删除
	deleted, err := svc.DeleteAgent("delete-test")
	require.NoError(t, err)
	assert.True(t, deleted)

	// 确认文件已删除
	_, err = os.Stat(created.FilePath)
	assert.True(t, os.IsNotExist(err))

	// 确认 agent 不再被获取
	agent := svc.GetAgent("delete-test")
	assert.Nil(t, agent)
}

// TestAgentConfigService_DeleteAgent_Builtin 测试不能删除内置 agent
func TestAgentConfigService_DeleteAgent_Builtin(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	_, err := svc.DeleteAgent("general-purpose")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能删除内置 agent")
}

// TestAgentConfigService_DeleteAgent_NotFound 测试删除不存在的 agent
func TestAgentConfigService_DeleteAgent_NotFound(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	deleted, err := svc.DeleteAgent("non-existent")
	assert.NoError(t, err)
	assert.False(t, deleted)
}

// TestAgentConfigService_ListAvailableTools 测试列出可用工具
func TestAgentConfigService_ListAvailableTools(t *testing.T) {
	svc := NewAgentConfigService("/tmp")
	result := svc.ListAvailableTools()

	assert.GreaterOrEqual(t, len(result.Tools), 10, "应至少有 10 个工具")
	assert.Contains(t, result.Groups, "核心")
	assert.Contains(t, result.Groups, "搜索")
	assert.Contains(t, result.Groups, "代码智能")
	assert.Contains(t, result.Groups, "高级")
	assert.Contains(t, result.Groups, "可视化")
	assert.NotEmpty(t, result.DisallowedForSubagents, "应包含 disallowed_for_subagents")
	assert.Contains(t, result.DisallowedForSubagents, "Agent")
}

// TestAgentConfigService_LoadFromDir 测试从目录加载 agent
func TestAgentConfigService_LoadFromDir(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	// 创建测试目录
	agentsDir := filepath.Join(dir, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	// 写入有效文件
	content1 := "---\nname: agent-1\ndescription: Agent 1\n---\n\nPrompt 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "agent-1.md"), []byte(content1), 0o644))

	content2 := "---\nname: agent-2\ndescription: Agent 2\n---\n\nPrompt 2\n"
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "agent-2.md"), []byte(content2), 0o644))

	// 写入无效文件（无 frontmatter）
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "invalid.md"), []byte("no frontmatter"), 0o644))

	// 写入非 md 文件
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "notes.txt"), []byte("text file"), 0o644))

	agents := svc.loadFromDir(agentsDir, types.AgentSourceLocal)
	assert.Len(t, agents, 2, "应只加载 2 个有效 agent")
}

// TestAgentConfigService_LoadFromDir_NonExistent 测试加载不存在的目录
func TestAgentConfigService_LoadFromDir_NonExistent(t *testing.T) {
	svc := NewAgentConfigService("/non/existent/path")
	agents := svc.loadFromDir("/non/existent/path", types.AgentSourceLocal)
	assert.Nil(t, agents)
}

// TestParseAgentFile_RoundTrip 测试解析和生成的往返一致性
func TestParseAgentFile_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	// 创建并写入
	def := &types.AgentDefinition{
		Name:        "round-trip",
		Description: "往返测试",
		Prompt:      "测试 prompt",
		Source:      types.AgentSourceLocal,
		Tools:       []string{"Read", "Grep"},
		WhenToUse:   "当需要往返测试时",
		Model:       "qwen-max",
	}
	content := formatAgentFile(def)
	filePath := filepath.Join(dir, "round-trip.md")
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0o644))

	// 解析回来
	agent, err := parseAgentFile(filePath, types.AgentSourceLocal)
	require.NoError(t, err)
	require.NotNil(t, agent)

	assert.Equal(t, "round-trip", agent.Name)
	assert.Equal(t, "往返测试", agent.Description)
	assert.Equal(t, "测试 prompt", agent.Prompt)
	assert.Equal(t, []string{"Read", "Grep"}, agent.Tools)
	assert.Equal(t, "当需要往返测试时", agent.WhenToUse)
	assert.Equal(t, "qwen-max", agent.Model)
}

// TestBoolVal 测试 boolVal 辅助函数
func TestBoolVal(t *testing.T) {
	assert.True(t, boolVal(true))
	assert.False(t, boolVal(false))
	assert.True(t, boolVal("true"))
	assert.True(t, boolVal("True"))
	assert.True(t, boolVal("TRUE"))
	assert.False(t, boolVal("false"))
	assert.False(t, boolVal("anything"))
	assert.False(t, boolVal(1))
	assert.False(t, boolVal(nil))
}

// TestToStringSlice 测试 toStringSlice 辅助函数
func TestToStringSlice(t *testing.T) {
	// 正常切片
	result := toStringSlice([]any{"a", "b", "c"})
	assert.Equal(t, []string{"a", "b", "c"}, result)

	// 混合类型（只保留 string）
	result = toStringSlice([]any{"a", 1, "b", true, "c"})
	assert.Equal(t, []string{"a", "b", "c"}, result)

	// 空切片
	result = toStringSlice([]any{})
	assert.Empty(t, result)

	// 非 slice
	result = toStringSlice("not a slice")
	assert.Nil(t, result)

	// nil
	result = toStringSlice(nil)
	assert.Nil(t, result)
}

// TestAgentConfigService_CreateAndRetrieve 测试完整创建→获取流程
func TestAgentConfigService_CreateAndRetrieve(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	params := &CreateAgentParams{
		Name:        "full-flow",
		Description: "完整流程测试",
		Prompt:      "你是完整流程 agent。",
		Location:    types.AgentSourceProject,
		Tools:       []string{"Read", "Bash"},
		WhenToUse:   "当需要完整流程测试时",
		Model:       "deepseek-chat",
		Color:       "red",
	}

	created, err := svc.CreateAgent(params)
	require.NoError(t, err)

	// 通过 GetAgent 获取
	retrieved := svc.GetAgent("full-flow")
	require.NotNil(t, retrieved)
	assert.Equal(t, created.Name, retrieved.Name)
	assert.Equal(t, created.Description, retrieved.Description)
	assert.Equal(t, created.Prompt, retrieved.Prompt)
	assert.Equal(t, created.Tools, retrieved.Tools)
	assert.Equal(t, created.WhenToUse, retrieved.WhenToUse)
	assert.Equal(t, created.Model, retrieved.Model)
	assert.Equal(t, created.Color, retrieved.Color)
}

// TestAgentConfigService_CreateAndUpdateAndDelete 测试完整 CRUD 流程
func TestAgentConfigService_CreateAndUpdateAndDelete(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	// 创建
	params := &CreateAgentParams{
		Name:        "crud-agent",
		Description: "初始描述",
		Prompt:      "初始 prompt",
		Location:    types.AgentSourceLocal,
	}
	_, err := svc.CreateAgent(params)
	require.NoError(t, err)

	// 更新
	newDesc := "更新描述"
	newPrompt := "更新 prompt"
	updateParams := &UpdateAgentParams{
		Description: &newDesc,
		Prompt:      &newPrompt,
	}
	updated, err := svc.UpdateAgent("crud-agent", updateParams)
	require.NoError(t, err)
	assert.Equal(t, "更新描述", updated.Description)
	assert.Equal(t, "更新 prompt", updated.Prompt)

	// 获取确认
	agent := svc.GetAgent("crud-agent")
	require.NotNil(t, agent)
	assert.Equal(t, "更新描述", agent.Description)
	assert.Equal(t, "更新 prompt", agent.Prompt)

	// 删除
	deleted, err := svc.DeleteAgent("crud-agent")
	require.NoError(t, err)
	assert.True(t, deleted)

	// 确认删除
	agent = svc.GetAgent("crud-agent")
	assert.Nil(t, agent)
}

// TestAgentConfigService_ListAgents_SortOrder 测试 agent 排序顺序
func TestAgentConfigService_ListAgents_SortOrder(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	// 创建不同来源的 agent
	for _, loc := range []string{types.AgentSourceLocal, types.AgentSourceProject, types.AgentSourceUser} {
		targetDir, err := svc.resolveLocationDir(loc)
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(targetDir, 0o755))
		content := "---\nname: " + loc + "-agent\ndescription: " + loc + " agent\n---\n\nprompt\n"
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, loc+"-agent.md"), []byte(content), 0o644))
	}

	agents := svc.ListAgents()

	// 按来源分组统计
	sourceOrder := map[string]int{types.AgentSourceBuiltin: 0, types.AgentSourceLocal: 1, types.AgentSourceUser: 2, types.AgentSourceProject: 3}
	for i := 1; i < len(agents); i++ {
		assert.LessOrEqual(t, sourceOrder[agents[i-1].Source], sourceOrder[agents[i].Source],
			"agents 应按 source 排序: builtin < local < user < project")
	}
}

// TestAgentConfigService_CreateAgent_NameTrimSpace 测试名称前后空格处理
func TestAgentConfigService_CreateAgent_NameTrimSpace(t *testing.T) {
	dir := t.TempDir()
	svc := NewAgentConfigService(dir)

	params := &CreateAgentParams{
		Name:        "  valid-name  ",
		Description: "描述",
		Prompt:      "prompt",
		Location:    types.AgentSourceLocal,
	}

	agent, err := svc.CreateAgent(params)
	require.NoError(t, err)
	assert.Equal(t, "valid-name", agent.Name)
}
