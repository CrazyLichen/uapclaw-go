package harness_config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestValidateHarnessConfig_正常 校验正常配置
func TestValidateHarnessConfig_正常(t *testing.T) {
	cfg := &HarnessConfig{SchemaVersion: DefaultSchemaVersion}
	err := ValidateHarnessConfig(cfg)
	assert.NoError(t, err)
}

// TestValidateHarnessConfig_nil 校验 nil 配置
func TestValidateHarnessConfig_nil(t *testing.T) {
	err := ValidateHarnessConfig(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

// TestValidateHarnessConfig_空版本 校验空版本
func TestValidateHarnessConfig_空版本(t *testing.T) {
	cfg := &HarnessConfig{SchemaVersion: ""}
	err := ValidateHarnessConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "schema_version")
}

// TestValidateHarnessConfig_不支持的版本 校验不支持的版本
func TestValidateHarnessConfig_不支持的版本(t *testing.T) {
	cfg := &HarnessConfig{SchemaVersion: "harness_config.v0.2"}
	err := ValidateHarnessConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不支持的 schema_version")
}

// TestHarnessConfig_ToYAML_基本 测试基本 YAML 序列化
func TestHarnessConfig_ToYAML_基本(t *testing.T) {
	cfg := &HarnessConfig{
		SchemaVersion: DefaultSchemaVersion,
		Language:      "cn",
	}
	yamlStr, err := cfg.ToYAML()
	require.NoError(t, err)
	assert.Contains(t, yamlStr, "schema_version")
	assert.Contains(t, yamlStr, "harness_config.v0.1")
}

// TestHarnessConfig_ToYAML_带ID 测试带 ID 的 YAML 序列化
func TestHarnessConfig_ToYAML_带ID(t *testing.T) {
	id := "my-agent"
	name := "My Agent"
	desc := "测试描述"
	cfg := &HarnessConfig{
		SchemaVersion: DefaultSchemaVersion,
		ID:            &id,
		Name:          &name,
		Description:   &desc,
		Language:      "cn",
	}
	yamlStr, err := cfg.ToYAML()
	require.NoError(t, err)
	assert.Contains(t, yamlStr, "my-agent")
	assert.Contains(t, yamlStr, "My Agent")
	assert.Contains(t, yamlStr, "测试描述")
}

// TestHarnessConfig_ToYAML_写入文件 测试 YAML 序列化并写入文件
func TestHarnessConfig_ToYAML_写入文件(t *testing.T) {
	cfg := &HarnessConfig{
		SchemaVersion: DefaultSchemaVersion,
		Language:      "cn",
	}
	outputPath := t.TempDir() + "/output.yaml"
	yamlStr, err := cfg.ToYAML(outputPath)
	require.NoError(t, err)
	assert.Contains(t, yamlStr, "schema_version")

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "schema_version")
}

// TestHarnessConfig_完整结构 测试完整结构序列化
func TestHarnessConfig_完整结构(t *testing.T) {
	id := "test-agent"
	name := "测试Agent"
	cfg := &HarnessConfig{
		SchemaVersion: DefaultSchemaVersion,
		Meta: &MetaSchema{
			Owner:      "test-owner",
			Tags:       []string{"test", "demo"},
			Visibility: "internal",
		},
		ID:          &id,
		Name:        &name,
		Language:    "en",
		Workspace:   &WorkspaceSchema{RootPath: "/tmp/workspace"},
		MaxIterations: func() *int { v := 20; return &v }(),
		CompletionTimeout: func() *float64 { v := 300.0; return &v }(),
		Prompts: &PromptsSchema{
			Sections: []SectionSchema{
				{
					Name:    "identity",
					Content: map[string]string{"cn": "你是一个助手", "en": "You are an assistant"},
				},
			},
		},
		Resources: &ResourcesSchema{
			Tools: []ToolResourceSchema{
				{Type: "builtin", Names: []string{"filesystem", "shell"}},
			},
			Rails: []RailResourceSchema{
				{Type: "builtin", Name: func() *string { v := "task_planning"; return &v }()},
			},
			Skills: &SkillsSchema{Dirs: []string{"./skills"}, Mode: "all"},
			Mcps: []McpResourceSchema{
				{Type: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem"}},
			},
		},
	}

	yamlStr, err := cfg.ToYAML()
	require.NoError(t, err)
	assert.Contains(t, yamlStr, "test-agent")
	assert.Contains(t, yamlStr, "filesystem")
	assert.Contains(t, yamlStr, "task_planning")
	assert.Contains(t, yamlStr, "npx")
}

// TestMetaSchema_默认值 测试 MetaSchema 默认值
func TestMetaSchema_默认值(t *testing.T) {
	meta := MetaSchema{}
	assert.Equal(t, "", meta.Owner)
	assert.Nil(t, meta.Tags)
	assert.Equal(t, "", meta.Visibility)
}

// TestToolResourceSchema_YAML标签 测试 YAML 标签映射
func TestToolResourceSchema_YAML标签(t *testing.T) {
	className := "MyTool"
	spec := ToolResourceSchema{
		Type:      "package",
		ClassName: &className,
	}
	yamlBytes, err := yaml.Marshal(&spec)
	require.NoError(t, err)
	// ClassName 应映射为 "class" 在 YAML 中
	assert.Contains(t, string(yamlBytes), "class:")
}

// TestRailResourceSchema_YAML标签 测试 Rail YAML 标签映射
func TestRailResourceSchema_YAML标签(t *testing.T) {
	className := "MyRail"
	spec := RailResourceSchema{
		Type:      "package",
		ClassName: &className,
	}
	yamlBytes, err := yaml.Marshal(&spec)
	require.NoError(t, err)
	assert.Contains(t, string(yamlBytes), "class:")
}

// TestMcpResourceSchema_默认类型 测试 MCP 默认类型
func TestMcpResourceSchema_默认类型(t *testing.T) {
	spec := McpResourceSchema{}
	assert.Equal(t, "", spec.Type) // 零值，默认值由 setDefaults 设置
}

// TestWorkspaceSchema_默认路径 测试工作空间默认路径
func TestWorkspaceSchema_默认路径(t *testing.T) {
	ws := WorkspaceSchema{}
	assert.Equal(t, "", ws.RootPath) // 零值，默认值由 setDefaults 设置
}

// TestSetDefaults 测试默认值设置
func TestSetDefaults_完整(t *testing.T) {
	cfg := &HarnessConfig{}
	setDefaults(cfg)
	assert.Equal(t, DefaultSchemaVersion, cfg.SchemaVersion)
	assert.Equal(t, DefaultLanguage, cfg.Language)
}

// TestSetDefaults_已有值 测试默认值不覆盖已有值
func TestSetDefaults_已有值(t *testing.T) {
	cfg := &HarnessConfig{
		SchemaVersion: "harness_config.v0.1",
		Language:      "en",
	}
	setDefaults(cfg)
	assert.Equal(t, "harness_config.v0.1", cfg.SchemaVersion)
	assert.Equal(t, "en", cfg.Language)
}

// TestSetDefaults_嵌套默认值 测试嵌套结构默认值
func TestSetDefaults_嵌套默认值(t *testing.T) {
	cfg := &HarnessConfig{
		Meta:      &MetaSchema{},
		Workspace: &WorkspaceSchema{},
		Resources: &ResourcesSchema{
			Skills: &SkillsSchema{},
			Mcps:   []McpResourceSchema{{}},
		},
	}
	setDefaults(cfg)
	assert.Equal(t, DefaultVisibility, cfg.Meta.Visibility)
	assert.Equal(t, DefaultWorkspaceRootPath, cfg.Workspace.RootPath)
	assert.Equal(t, DefaultSkillsMode, cfg.Resources.Skills.Mode)
	assert.Equal(t, DefaultMcpType, cfg.Resources.Mcps[0].Type)
}
