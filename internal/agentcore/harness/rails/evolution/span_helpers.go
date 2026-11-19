package evolution

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// span_helpers.go rails 的 span 模型读面助手（25.2b，12-04 迁移）。
// Python 侧等价读法：iter_spans + span_category + read_tool_call
//（agent_evolving/trajectory/{spans,team}.py）。

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

// fmtSprint 格式化助手（nil 安全）。
func fmtSprint(value any) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(toString(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

// toString 任意值字符串化。
func toString(value any) string {
	type stringer interface{ String() string }
	if s, ok := value.(stringer); ok {
		return s.String()
	}
	return textOf(value)
}

// textOf fmt.Sprint 薄封装。
func textOf(value any) string { return sprintOf(value) }

// sprintOf fmt.Sprint 薄封装。
func sprintOf(value any) string {
	type stringer interface{ String() string }
	if s, ok := value.(stringer); ok {
		return s.String()
	}
	return fmt.Sprint(value)
}

// sessionIDString 轨迹会话 ID 的可选字符串化（nil → ""）。
func sessionIDString(traj *trajectory.Trajectory) string {
	if traj == nil {
		return ""
	}
	if id := traj.SessionID(); id != nil {
		return *id
	}
	return ""
}

// emptySpanTrajectoryForRails 构造 rails 侧合法空 span 轨迹。
// 12-04 注记：step 模型字面量构造随模型迁移删除，Python 无对应实现。
func emptySpanTrajectoryForRails(sessionID, source string) (*trajectory.Trajectory, error) {
	builder, err := trajectory.NewTrajectoryBuilder(sessionID, source, nil, nil, nil, nil, 0, nil, nil)
	if err != nil {
		return nil, err
	}
	return builder.Build()
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
