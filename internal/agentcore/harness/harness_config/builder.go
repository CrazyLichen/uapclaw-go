package harness_config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	sasc "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// toolGroupDef 内置工具组定义
type toolGroupDef struct {
	// ModulePath 点分模块路径
	ModulePath string
	// ClassNames 类名列表
	ClassNames []string
	// NeedsSysOp 是否需要 SysOperation
	NeedsSysOp bool
}

// HarnessConfigBuilder 将 ResolvedHarnessConfig 转换为配置好的 DeepAgent
type HarnessConfigBuilder struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

// builtinToolGroups 内置工具组注册表
//
// 每个条目：(点分模块路径, 类名列表, 是否需要 SysOperation)
var builtinToolGroups = map[string]toolGroupDef{
	"filesystem": {
		ModulePath: "openjiuwen.harness.tools.filesystem",
		ClassNames: []string{"ReadFileTool", "WriteFileTool", "EditFileTool", "ListDirTool", "GlobTool", "GrepTool"},
		NeedsSysOp: true,
	},
	"shell": {
		ModulePath: "openjiuwen.harness.tools.shell",
		ClassNames: []string{"BashTool"},
		NeedsSysOp: true,
	},
	"code": {
		ModulePath: "openjiuwen.harness.tools.code",
		ClassNames: []string{"CodeTool"},
		NeedsSysOp: true,
	},
	"web_search": {
		ModulePath: "openjiuwen.harness.tools.web_tools",
		ClassNames: []string{"WebFreeSearchTool", "WebPaidSearchTool"},
		NeedsSysOp: false,
	},
	"web_fetch": {
		ModulePath: "openjiuwen.harness.tools.web_tools",
		ClassNames: []string{"WebFetchWebpageTool"},
		NeedsSysOp: false,
	},
}

// builtinRailRegistry 内置 Rail 注册表
var builtinRailRegistry = map[string]string{
	"task_planning": "openjiuwen.harness.rails.task_planning_rail.TaskPlanningRail",
}

// toolDottedToGroup 反转注册表：工具点分路径 → 组名
var toolDottedToGroup = buildToolDottedToGroup()

// railDottedToName 反转注册表：Rail 点分路径 → 名称
var railDottedToName = buildRailDottedToName()

// ──────────────────────────── 导出函数 ────────────────────────────

// Build 将 ResolvedHarnessConfig 转换为 CreateDeepAgentParams，
// 供调用方传入 CreateDeepAgent() 工厂创建 DeepAgent。
//
// 对齐 Python: HarnessConfigBuilder.build → create_deep_agent(config)。
// 注意：此方法返回 CreateDeepAgentParams 而非直接创建 DeepAgent，
// 以避免 harness_config → harness 循环依赖。
func (b HarnessConfigBuilder) Build(resolved *ResolvedHarnessConfig, model *llm.Model, workspaceRoot ...string) (*CreateDeepAgentParams, error) {
	if resolved == nil || resolved.Config == nil {
		return nil, fmt.Errorf("resolved 配置不能为空")
	}

	config := resolved.Config
	language := config.Language

	// ── 1. Agent 卡片 ──
	var agentName, agentDesc, agentID string
	if config.Name != nil {
		agentName = *config.Name
	}
	if config.Description != nil {
		agentDesc = *config.Description
	}
	if config.ID != nil {
		agentID = *config.ID
	}
	card := sasc.NewAgentCard(
		sasc.WithAgentName(agentName),
		sasc.WithAgentDescription(agentDesc),
		sasc.WithAgentID(agentID),
	)

	// ── 2. 语言解析（校验 + 回退） ──
	resolvedLanguage := hprompts.ResolveLanguage(language)

	// ── 3. 工作空间 ──
	wsRoot := "./"
	if len(workspaceRoot) > 0 && workspaceRoot[0] != "" {
		wsRoot = workspaceRoot[0]
	}
	ws := workspace.NewWorkspace(wsRoot, resolvedLanguage)

	// ── 4. 解析资源（tools/rails/mcps/skills） ──
	// ⤵️ 9.38 回填：工具实例化实现后补全
	resources := config.Resources
	var tools []*tool.ToolCard
	var extraRails []interfaces.AgentRail
	var mcps []*mcptypes.McpServerConfig
	var skills []string
	var sysOperation sysop.SysOperation
	if resources != nil {
		resolvedTools, toolErr := resolveTools(resources, sysOperation)
		if toolErr != nil {
			return nil, fmt.Errorf("解析工具失败: %w", toolErr)
		}
		tools = resolvedTools

		resolvedRails, railErr := resolveRails(resources)
		if railErr != nil {
			return nil, fmt.Errorf("解析 Rails 失败: %w", railErr)
		}
		extraRails = resolvedRails

		resolvedMcps, mcpErr := resolveMcps(resources)
		if mcpErr != nil {
			return nil, fmt.Errorf("解析 MCPs 失败: %w", mcpErr)
		}
		mcps = resolvedMcps

		if resources.Skills != nil {
			skills = resources.Skills.Dirs
		}
	}

	// ── 5. 构建 CreateDeepAgentParams ──
	var systemPrompt string
	if resolved.SystemPrompt != nil {
		systemPrompt = *resolved.SystemPrompt
	}

	maxIter := 15
	if config.MaxIterations != nil {
		maxIter = *config.MaxIterations
	}

	result := &CreateDeepAgentParams{
		Model:             model,
		Card:              card,
		SystemPrompt:      systemPrompt,
		Mcps:              mcps,
		Rails:             extraRails,
		MaxIterations:     maxIter,
		Workspace:         ws,
		Skills:            skills,
		SysOperation:      sysOperation,
		Language:          resolvedLanguage,
	}

	// RestrictToWorkDir 默认 true，对齐 Python: restrict_to_work_dir: bool = True
	restrictToWorkDir := true
	result.RestrictToWorkDir = &restrictToWorkDir

	// 将解析出的 ToolCard 传递给 ToolCards，供 CreateDeepAgent 注册到 AbilityManager
	if len(tools) > 0 {
		result.ToolCards = tools // ToolCard → AbilityManager schema 注册
		// ToolInstances 留空，ToolCard → Tool 实例化待 9.38 回填
	}

	return result, nil
}

// GenerateHarnessConfigYAML 从 create_deep_agent 风格的参数生成 harness_config.yaml 字符串。
// 若提供 outputPath，同时将内容写入该文件。
func GenerateHarnessConfigYAML(
	card *sasc.AgentCard,
	systemPrompt any,
	tools []*tool.ToolCard,
	rails []interfaces.AgentRail,
	language string,
	maxIterations *int,
	completionTimeout *float64,
	extraSections []map[string]any,
	outputPath ...string,
) (string, error) {
	data := map[string]any{"schema_version": DefaultSchemaVersion}

	if card != nil {
		if card.ID != "" {
			data["id"] = card.ID
		}
		if card.Name != "" {
			data["name"] = card.Name
		}
		if card.Description != "" {
			data["description"] = card.Description
		}
	}

	data["language"] = language
	if maxIterations != nil {
		data["max_iterations"] = *maxIterations
	}
	if completionTimeout != nil {
		data["completion_timeout"] = *completionTimeout
	}

	// 提示词段
	sections := []map[string]any{}
	if systemPrompt != nil {
		var content map[string]string
		switch v := systemPrompt.(type) {
		case string:
			content = map[string]string{"cn": v, "en": v}
		case map[string]string:
			content = v
		default:
			content = map[string]string{}
		}
		sections = append(sections, map[string]any{
			"name":     "identity",
			"priority": 10,
			"content":  content,
		})
	}
	sections = append(sections, extraSections...)
	if len(sections) > 0 {
		data["prompts"] = map[string]any{"sections": sections}
	}

	// 资源
	resources := map[string]any{}
	if len(tools) > 0 {
		toolSpecs := toolsToYAMLSpecs(tools)
		if len(toolSpecs) > 0 {
			resources["tools"] = toolSpecs
		}
	}
	if len(rails) > 0 {
		railSpecs := railsToYAMLSpecs(rails)
		if len(railSpecs) > 0 {
			resources["rails"] = railSpecs
		}
	}
	if len(resources) > 0 {
		data["resources"] = resources
	}

	yamlBytes, err := yaml.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("序列化 harness_config YAML 失败: %w", err)
	}
	yamlStr := string(yamlBytes)

	if len(outputPath) > 0 && outputPath[0] != "" {
		if err := os.WriteFile(outputPath[0], yamlBytes, 0644); err != nil {
			return yamlStr, fmt.Errorf("写入 YAML 文件失败: %w", err)
		}
	}

	return yamlStr, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveBuiltinTools 按组名实例化内置工具。
//
// ⤵️ 9.38 回填：内置工具集实现后补全实例化逻辑。
// 对齐 Python: resolve_builtin_tools → entry_point/module 工具加载。
func resolveBuiltinTools(groupName string, sysOperation sysop.SysOperation) ([]*tool.ToolCard, error) {
	entry, ok := builtinToolGroups[groupName]
	if !ok {
		validGroups := make([]string, 0, len(builtinToolGroups))
		for k := range builtinToolGroups {
			validGroups = append(validGroups, k)
		}
		sort.Strings(validGroups)
		return nil, fmt.Errorf("未知的内置工具组: '%s'，有效组: %v", groupName, validGroups)
	}
	// ⤵️ 9.38 回填：实际工具实例化
	return nil, fmt.Errorf("内置工具组 '%s' 实例化尚未实现（模块: %s，类: %v，需SysOp: %v），⤵️ 9.38 回填",
		groupName, entry.ModulePath, entry.ClassNames, entry.NeedsSysOp)
}

// resolveTools 从 resources.Tools 解析所有工具实例
func resolveTools(resources *ResourcesSchema, sysOperation sysop.SysOperation) ([]*tool.ToolCard, error) {
	if resources == nil {
		return nil, nil
	}
	var tools []*tool.ToolCard
	for _, spec := range resources.Tools {
		switch spec.Type {
		case "builtin":
			names := spec.Names
			if len(names) == 0 && spec.Name != nil {
				names = []string{*spec.Name}
			}
			for _, group := range names {
				groupTools, err := resolveBuiltinTools(group, sysOperation)
				if err != nil {
					return nil, err
				}
				tools = append(tools, groupTools...)
			}
		case "package":
			// ⤵️ 9.38 回填：包级工具加载
			return nil, fmt.Errorf("package 类型工具加载尚未实现，⤵️ 9.38 回填")
		case "entry_point":
			// ⤵️ 9.38 回填：entry_point 类型工具加载
			return nil, fmt.Errorf("entry_point 类型工具加载尚未实现，⤵️ 9.38 回填")
		}
	}
	return tools, nil
}

// createSysOperation 创建并注册本地 SysOperation，以 AgentCard 为键
//
// 对齐 Python: create_sys_operation → LocalSysOperation(card, workspace)。
func createSysOperation(card *sasc.AgentCard) (sysop.SysOperation, error) {
	sysOpCard := sysop.NewSysOperationCard(sysop.WithSysOpMode(sysop.OperationModeLocal))
	return sysop.NewLocalSysOperation(sysOpCard), nil
}

// resolveRails 从 resources.Rails 解析所有 Rail 实例
func resolveRails(resources *ResourcesSchema) ([]interfaces.AgentRail, error) {
	if resources == nil {
		return nil, nil
	}
	var rails []interfaces.AgentRail
	for _, spec := range resources.Rails {
		switch spec.Type {
		case "builtin":
			name := ""
			if spec.Name != nil {
				name = *spec.Name
			}
			_, ok := builtinRailRegistry[name]
			if !ok {
				validNames := make([]string, 0, len(builtinRailRegistry))
				for k := range builtinRailRegistry {
					validNames = append(validNames, k)
				}
				sort.Strings(validNames)
				return nil, fmt.Errorf("未知的内置 Rail: '%s'，有效名称: %v", name, validNames)
			}
			// ⤵️ 9.19-9.24 回填：内置 Rail 实例化
			return nil, fmt.Errorf("内置 Rail '%s' 实例化尚未实现，⤵️ 9.19-9.24 回填", name)
		case "package":
			// ⤵️ 9.19-9.24 回填：包级 Rail 加载
			return nil, fmt.Errorf("package 类型 Rail 加载尚未实现，⤵️ 9.19-9.24 回填")
		case "entry_point":
			// ⤵️ 9.19-9.24 回填：entry_point 类型 Rail 加载
			return nil, fmt.Errorf("entry_point 类型 Rail 加载尚未实现，⤵️ 9.19-9.24 回填")
		}
	}
	return rails, nil
}

// resolveMcps 将 MCP 规格转换为 McpServerConfig
func resolveMcps(resources *ResourcesSchema) ([]*mcptypes.McpServerConfig, error) {
	if resources == nil {
		return nil, nil
	}
	var mcps []*mcptypes.McpServerConfig
	for _, spec := range resources.Mcps {
		// 拼接命令和参数
		var serverPath string
		if spec.Command != "" {
			parts := []string{spec.Command}
			parts = append(parts, spec.Args...)
			serverPath = strings.Join(parts, " ")
		}

		// 环境变量转为 params
		params := make(map[string]any, len(spec.Env))
		for k, v := range spec.Env {
			params[k] = v
		}

		serverName := spec.Command
		if serverName == "" {
			serverName = "mcp_server"
		}

		cfg := mcptypes.NewMcpServerConfig(
			serverName,
			serverPath,
			spec.Type,
			mcptypes.WithParams(params),
		)
		mcps = append(mcps, cfg)
	}
	return mcps, nil
}

// writeFileSections 将文件型提示词段写入工作空间目录
func writeFileSections(fileSections []ResolvedFileSection, workspaceRoot string, language string) error {
	if err := os.MkdirAll(workspaceRoot, 0755); err != nil {
		return fmt.Errorf("创建工作空间目录失败: %w", err)
	}
	for _, fs := range fileSections {
		content := ""
		if text, ok := fs.Content[language]; ok && text != "" {
			content = text
		} else if text, ok := fs.Content["cn"]; ok && text != "" {
			content = text
		} else if text, ok := fs.Content["en"]; ok && text != "" {
			content = text
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		filePath := filepath.Join(workspaceRoot, fs.Filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("写入文件段 %s 失败: %w", filePath, err)
		}
	}
	return nil
}

// toolsToYAMLSpecs 将工具实例反向映射为 YAML ToolResourceSchema 字典
func toolsToYAMLSpecs(tools []*tool.ToolCard) []map[string]any {
	builtinGroups := []string{}
	unknownSpecs := []map[string]any{}

	for _, t := range tools {
		key := fmt.Sprintf("%s.%s", t.Name, t.ID)
		if group, ok := toolDottedToGroup[key]; ok {
			found := false
			for _, g := range builtinGroups {
				if g == group {
					found = true
					break
				}
			}
			if !found {
				builtinGroups = append(builtinGroups, group)
			}
		} else {
			unknownSpecs = append(unknownSpecs, map[string]any{
				"type":   "package",
				"module": t.Name,
				"class":  t.ID,
			})
		}
	}

	specs := []map[string]any{}
	if len(builtinGroups) > 0 {
		specs = append(specs, map[string]any{
			"type":  "builtin",
			"names": builtinGroups,
		})
	}
	specs = append(specs, unknownSpecs...)
	return specs
}

// railsToYAMLSpecs 将 Rail 实例反向映射为 YAML RailResourceSchema 字典
func railsToYAMLSpecs(rails []interfaces.AgentRail) []map[string]any {
	specs := []map[string]any{}
	for _, r := range rails {
		dotted := fmt.Sprintf("%T", r)
		if name, ok := railDottedToName[dotted]; ok {
			specs = append(specs, map[string]any{
				"type": "builtin",
				"name": name,
			})
		} else {
			specs = append(specs, map[string]any{
				"type":   "package",
				"module": dotted,
			})
		}
	}
	return specs
}

// buildToolDottedToGroup 构建反转工具注册表
func buildToolDottedToGroup() map[string]string {
	result := make(map[string]string)
	for group, def := range builtinToolGroups {
		for _, clsName := range def.ClassNames {
			key := def.ModulePath + "." + clsName
			result[key] = group
		}
	}
	return result
}

// buildRailDottedToName 构建反转 Rail 注册表
func buildRailDottedToName() map[string]string {
	result := make(map[string]string, len(builtinRailRegistry))
	for k, v := range builtinRailRegistry {
		result[v] = k
	}
	return result
}
