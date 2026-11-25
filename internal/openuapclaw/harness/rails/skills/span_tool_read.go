// skills 包 span 模型工具读面（skill_create_rail 系）。
// Python 侧等价读法：iter_spans + span_category + read_tool_call
// （skill_create_rail.py:431/:458 内联消费；Go 抽为包级助手随 rail 归属迁移）。
package skills

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
)

// toolSpanInfo 一个工具 span 的规范化读面（名称 + 参数映射）。
type toolSpanInfo struct {
	// Name 工具名（gen_ai.tool.name，缺省回落 span 名）。
	Name string
	// Args 解码后的调用参数映射（gen_ai.tool.call.arguments）。
	Args map[string]any
	// Input 参数原始值（JSON 解码后的任意形态）。
	Input any
	// Output 结果原始值（gen_ai.tool.call.result，缺省 nil）。
	Output any
}

// toolSpansOf 提取 builder 轨迹中的全部工具 span。
// Python: iter_spans(traj) 过滤 category=="tool" 后 read_tool_call。
func toolSpansOf(builder *trajectory.TrajectoryBuilder) []toolSpanInfo {
	if builder == nil {
		return nil
	}
	traj, err := builder.Build()
	if err != nil || traj == nil {
		return nil
	}
	var result []toolSpanInfo
	for _, span := range trajectory.IterSpans(traj) {
		if trajectory.SpanCategory(span) != "tool" {
			continue
		}
		call := trajectory.ReadToolCall(span)
		name, _ := call["name"].(string)
		if name == "" {
			name = strings.TrimSpace(fmtSprint(span["name"]))
		}
		args, _ := call["input"].(map[string]any)
		result = append(result, toolSpanInfo{Name: name, Args: args, Input: call["input"], Output: call["output"]})
	}
	return result
}

// argsJSONString 参数映射 → JSON 字符串。
func argsJSONString(args map[string]any) string {
	data, err := json.Marshal(args)
	if err != nil {
		return fmt.Sprint(args)
	}
	return string(data)
}

// argsJSONAnyString 任意值 → JSON 字符串（字符串原样）。
func argsJSONAnyString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

// fmtSprint 格式化助手（nil 安全）。
func fmtSprint(value any) string {
	if value == nil {
		return ""
	}
	switch value.(type) {
	case string:
		return value.(string)
	default:
		return fmt.Sprint(value)
	}
}

// mustNewTestBuilder 测试用组装器构造（新签名简化封装）。
func mustNewTestBuilder(sessionID, source string) *trajectory.TrajectoryBuilder {
	builder, err := trajectory.NewTrajectoryBuilder(sessionID, source, nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		panic(err)
	}
	return builder
}
