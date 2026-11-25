// Package tools 提供 agent 演进工具面（主 agent 六工具 + 审查子 agent 五只读工具）。
//
// Python 真相源：openjiuwen/agent_evolving/tools/{base,skill,review}.py。
// 公共行为（BaseEvolutionTool）承载 ToolOutput 转换、失败渲染与
// __mapped_content 注入（Go 侧 render_for_llm 等价通道，先例 D-15.16-b8）。
//
// 文件目录：
//
//	tools/
//	├── doc.go    # 包文档
//	├── base.go   # BaseEvolutionTool 公共行为
//	├── skill.go  # 主 agent 六工具（create_main_evolution_tools）
//	└── review.go # 审查子 agent 五只读工具（create_evolution_review_tools）
//
// 对应 Python 代码：openjiuwen/agent_evolving/tools/
package tools

import (
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	harnessTools "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools"
)

// ──────────────────────────── 常量 ────────────────────────────

// MappedContentKey 模型可见文本注入键（render_for_llm 等价通道）。
// 复用 agent_teams/tools 的既有约定键；此处仅引用同名常量语义。
const MappedContentKey = "__mapped_content"

// ──────────────────────────── 结构体 ────────────────────────────

// BaseEvolutionTool 演进工具公共行为（嵌入到各具体工具）。
// Python: BaseEvolutionTool (tools/base.py:14-49)。
//
// Go 形态：Python 类属性 tool_name/tool_id/failure_message 由各具体工具
// 以字段声明并初始化（Go 无类属性继承）。
type BaseEvolutionTool struct {
	// ToolName 工具注册名（Python tool_name 类属性）。
	ToolName string
	// ToolID 工具 id 前缀（Python tool_id 类属性）。
	ToolID string
	// FailureMessage 失败兜底文案（Python failure_message 类属性）。
	FailureMessage string
	// card 工具配置卡。
	card *tool.ToolCard
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Card 返回工具配置卡（tool.Tool 接口实现）。
func (t *BaseEvolutionTool) Card() *tool.ToolCard { return t.card }

// InputsDict 将入参归一为 map（非 dict 入参返回空 map）。
// Python: inputs_dict (tools/base.py:26-27)。
func InputsDict(inputs any) map[string]any {
	if m, ok := inputs.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// ToOutput 将服务结果转换为 ToolOutput。
// Python: to_output (tools/base.py:29-35)——成功透传；失败以 errors join 或
// failure_message 兜底，payload 仍保留。
func (t *BaseEvolutionTool) ToOutput(result map[string]any) harnessTools.ToolOutput {
	success := true
	if v, ok := result["success"].(bool); ok {
		success = v
	}
	if success {
		return harnessTools.ToolOutput{Success: true, Data: result}
	}
	rawErrors, _ := result["errors"].([]any)
	errorParts := make([]string, 0, len(rawErrors))
	for _, e := range rawErrors {
		errorParts = append(errorParts, fmt.Sprintf("%v", e))
	}
	message := t.FailureMessage
	if len(errorParts) > 0 {
		message = joinErrors(errorParts)
	}
	return harnessTools.ToolOutput{Success: false, Data: result, Error: message}
}

// Failure 异常兜底输出。
// Python: failure (tools/base.py:37-39)。
func Failure(err error) harnessTools.ToolOutput {
	return harnessTools.ToolOutput{Success: false, Error: err.Error()}
}

// RenderForLLM 渲染模型可见文本；失败时 error 后保留 payload。
// Python: render_for_llm (tools/base.py:41-49)——partial evolve/simplify
// 结果只带通用 error，status/counts/retry ids 活在 payload 中必须可见。
func (t *BaseEvolutionTool) RenderForLLM(output harnessTools.ToolOutput) string {
	if output.Success || output.Data == nil {
		return renderPayloadText(output.Data)
	}
	return output.Error + "\n" + renderPayloadText(output.Data)
}

// InjectMappedContent 将模型可见文本注入返回 map（render_for_llm 等价通道）。
// Go 侧消费：BuildToolMessageContent 优先读取 __mapped_content（先例 D-15.16-b8）。
func InjectMappedContent(result map[string]any, output harnessTools.ToolOutput) {
	if text := renderForLLMText(output); text != "" {
		result[MappedContentKey] = text
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// joinErrors 分号连接错误列表。
// Python: "; ".join(str(error) for error in errors) (tools/base.py:34)。
func joinErrors(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "; "
		}
		out += p
	}
	return out
}

// renderPayloadText 渲染 payload 为模型可见文本。
// Python: render_payload_text (foundation/tool/base.py:33-49)——
// 字符串原样；带 content 键的 dict 只渲染该值；其余 JSON 序列化。
func renderPayloadText(data any) string {
	if data == nil {
		return ""
	}
	if text, ok := data.(string); ok {
		return text
	}
	if m, ok := data.(map[string]any); ok {
		if content, ok := m["content"].(string); ok {
			return content
		}
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Sprintf("%v", data)
	}
	return string(encoded)
}

// renderForLLMText 分发渲染（基类规则；特殊工具在各自文件覆写）。
func renderForLLMText(output harnessTools.ToolOutput) string {
	base := &BaseEvolutionTool{}
	return base.RenderForLLM(output)
}
