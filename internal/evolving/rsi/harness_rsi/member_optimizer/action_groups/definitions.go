// definitions.go 动作定义装载。
// Python: action_groups/definitions.py（全量对齐，109 行）。

package actiongroups

import (
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"gopkg.in/yaml.v3"
)

const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// builtinGroupOperations 内置符号组的操作展开。
// Python: definitions.py:14-20 _BUILTIN_GROUP_OPERATIONS。
var builtinGroupOperations = map[string][]string{
	"config": {"add", "modify", "remove"},
	"prompt": {"add", "modify", "remove"},
	"rail":   {"add", "modify", "remove"},
	"skill":  {"add", "modify", "remove"},
	"tool":   {"add", "modify", "remove"},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadActionDefinitions 从配置装载策略兼容的动作定义。
// 配置文件可声明支持的动作组，但不能把可执行策略扩展到当前 Auto Harness
// 包面之外。
// Python: definitions.py:31-64 load_action_definitions。
func LoadActionDefinitions(configPaths []string) []harnessrsi.ActionDefinition {
	// Python: definitions: list[ActionDefinition] = []
	var definitions []harnessrsi.ActionDefinition
	// Python: for config_path in config_paths: ...
	for _, configPath := range configPaths {
		if configPath == "" {
			continue
		}
		// Python: symbolic_group = str(config_path).strip().lower(); if in builtin: 展开
		symbolicGroup := strings.ToLower(strings.TrimSpace(configPath))
		if _, isBuiltin := builtinGroupOperations[symbolicGroup]; isBuiltin {
			definitions = append(definitions, builtinActionDefinitions(symbolicGroup)...)
			continue
		}
		// Python: path = Path(config_path).expanduser().resolve(); if not is_file(): continue
		path := filepath.Clean(configPath)
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			continue
		}
		// Python: try: data = _load_json_or_yaml(path) except (...): logger.warning; data = {}
		data, err := loadJSONOrYAML(path)
		if err != nil {
			logger.Warn(logComponent).
				Str("path", path).
				Err(err).
				Msg("failed to load action definitions")
			data = map[string]any{}
		}
		// Python: actions = data.get("actions", []); for action_data: ...
		actions, _ := data["actions"].([]any)
		for _, actionData := range actions {
			actionMap, ok := actionData.(map[string]any)
			if !ok {
				continue
			}
			// Python: definition = ActionDefinition(...) except: logger.warning("ignored invalid ...")
			definition, convErr := actionDefinitionFromMap(actionMap)
			if convErr != nil {
				logger.Warn(logComponent).
					Str("path", path).
					Err(convErr).
					Msg("ignored invalid action definition")
				continue
			}
			definitions = append(definitions, *definition)
		}
	}
	// Python: return filter_action_definitions(definitions)
	return FilterActionDefinitions(definitions)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// builtinActionDefinitions 构建一个内置符号组的可执行定义。
// Python: definitions.py:23-28 _builtin_action_definitions。
func builtinActionDefinitions(group string) []harnessrsi.ActionDefinition {
	var out []harnessrsi.ActionDefinition
	// Python: for operation in _BUILTIN_GROUP_OPERATIONS[group]: ActionDefinition(name=f"{group}_{operation}", ...)
	for _, operation := range builtinGroupOperations[group] {
		out = append(out, harnessrsi.ActionDefinition{
			Name:          group + "_" + operation,
			Group:         group,
			Operation:     operation,
			Function:      operation + "_" + group,
			Purpose:       fmt.Sprintf("%s a package-local %s surface", operationTitle(operation), group),
			IsDestructive: operation == "remove",
		})
	}
	return out
}

// actionDefinitionFromMap map → 定义载体（字段缺失回落 Python 默认值）。
func actionDefinitionFromMap(actionMap map[string]any) (*harnessrsi.ActionDefinition, error) {
	// Python: 关键字构造——类型不符抛 TypeError/ValueError（Go 以断言失败等价）
	optimizableModules := []string{}
	if list, ok := actionMap["optimizable_modules"].([]any); ok {
		for _, item := range list {
			optimizableModules = append(optimizableModules, fmt.Sprintf("%v", item))
		}
	}
	dependencyResources := map[string]string{}
	if m, ok := actionMap["dependency_resources"].(map[string]any); ok {
		for key, value := range m {
			dependencyResources[key] = fmt.Sprintf("%v", value)
		}
	}
	parametersSchema := map[string]any{}
	if m, ok := actionMap["parameters_schema"].(map[string]any); ok {
		parametersSchema = m
	}
	validationRules := []string{}
	if list, ok := actionMap["validation_rules"].([]any); ok {
		for _, item := range list {
			validationRules = append(validationRules, fmt.Sprintf("%v", item))
		}
	}
	return &harnessrsi.ActionDefinition{
		Name:                strField(actionMap, "name"),
		Group:               strField(actionMap, "group"),
		Operation:           strField(actionMap, "operation"),
		Function:            strField(actionMap, "function"),
		Purpose:             strField(actionMap, "purpose"),
		OptimizableModules:  optimizableModules,
		RequiresSearch:      boolField(actionMap, "requires_search"),
		RequiresInstall:     boolField(actionMap, "requires_install"),
		DependencyResources: dependencyResources,
		ParametersSchema:    parametersSchema,
		IsDestructive:       boolField(actionMap, "is_destructive"),
		ValidationRules:     validationRules,
	}, nil
}

// loadJSONOrYAML 按后缀装载 YAML 或 JSON。
// Python: definitions.py:98-103 _load_json_or_yaml。
func loadJSONOrYAML(path string) (map[string]any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".yaml" || ext == ".yml" {
		if err := yaml.Unmarshal(content, &data); err != nil {
			return nil, err
		}
	} else {
		if err := json.Unmarshal(content, &data); err != nil {
			return nil, err
		}
	}
	if data == nil {
		data = map[string]any{}
	}
	return data, nil
}

// strField 字符串字段（nil → ""）。
func strField(m map[string]any, key string) string {
	if v, ok := m[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// boolField 布尔字段（nil → false）。
func boolField(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

// operationTitle 等价 Python str.title()。
func operationTitle(operation string) string {
	if operation == "" {
		return ""
	}
	return strings.ToUpper(operation[:1]) + strings.ToLower(operation[1:])
}
