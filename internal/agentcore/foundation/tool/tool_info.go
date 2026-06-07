package tool

import (
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ToolInfo 从 ToolCard 生成工具描述信息，供 LLM function calling 消费。
//
// 将 InputParams ([]*Param) 转换为 JSON Schema map，构造 ToolInfo 返回。
//
// 对应 Python: ToolCard.tool_info() -> ToolInfo(name=..., description=..., parameters=...)
func (c *ToolCard) ToolInfo() *schema.ToolInfo {
	parameters := schema.ToJSONSchemaMap(c.InputParams)
	return schema.NewToolInfo(c.Name, c.Description, parameters)
}
