package tools

import (
	"fmt"
	"strings"
	"sync"

	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/google/uuid"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// registry 工具名称到提供者的映射
	registry sync.Map
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterToolProvider 注册工具元数据提供者
func RegisterToolProvider(provider ToolMetadataProvider) {
	registry.Store(provider.GetName(), provider)
}

// GetToolProvider 按名称查找工具元数据提供者
func GetToolProvider(name string) (ToolMetadataProvider, bool) {
	v, ok := registry.Load(name)
	if !ok {
		return nil, false
	}
	return v.(ToolMetadataProvider), true
}

// AllProviders 返回所有已注册的工具元数据提供者
func AllProviders() []ToolMetadataProvider {
	var result []ToolMetadataProvider
	registry.Range(func(_, v any) bool {
		result = append(result, v.(ToolMetadataProvider))
		return true
	})
	return result
}

// BuildToolCard 统一建卡函数。从 registry 获取 description + inputParams，
// 转换为 []*schema.Param 后构建 ToolCard。
//
// 对齐 Python: openjiuwen/harness/prompts/tools/__init__.py build_tool_card()
func BuildToolCard(
	name string,
	toolID string,
	language string,
	formatArgs map[string]string,
	agentID string,
) (*tool.ToolCard, error) {
	// 1. 从 registry 查找 provider
	provider, ok := GetToolProvider(name)
	if !ok {
		return nil, fmt.Errorf("tool %q not registered in prompts/tools registry", name)
	}

	// 2. 获取 description
	description := provider.GetDescription(language)

	// 3. 如果有 formatArgs，填充描述中的占位符
	for key, value := range formatArgs {
		description = strings.ReplaceAll(description, "{"+key+"}", value)
	}

	// 4. 获取 input_params 并转换
	inputParamsMap := provider.GetInputParams(language)
	inputParams, err := cschema.ParseJSONSchemaMap(inputParamsMap)
	if err != nil {
		return nil, fmt.Errorf("tool %q: parse input params failed: %w", name, err)
	}

	// 5. 生成 tool_id
	var finalID string
	if agentID != "" {
		finalID = toolID + "_" + agentID
	} else {
		finalID = toolID + "_" + uuid.New().String()[:8]
	}

	// 6. 构建 ToolCard
	card := tool.NewToolCardWithID(finalID, name, description, inputParams, nil)
	return card, nil
}
