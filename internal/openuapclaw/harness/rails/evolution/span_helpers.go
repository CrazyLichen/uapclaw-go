package evolution

import (
	"encoding/json"
	"fmt"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
)

// span_helpers.go rails 的 span 模型读面助手（25.2b，12-04 迁移）。
// Python 侧等价读法：iter_spans + span_category + read_tool_call
//（agent_evolving/trajectory/{spans,team}.py）。

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

// testTrajectoryFromSteps 构造 span 模型测试轨迹（llmschema 消息 → 扁平 map）。
// 迁移期测试支持；Python 无对应实现。
func testTrajectoryFromSteps(sessionID string, steps []trajectory.TestStep) *trajectory.Trajectory {
	traj, err := trajectory.NewTestTrajectory(sessionID, nil, steps)
	if err != nil {
		panic(err)
	}
	return traj
}

// baseMsgMaps BaseMessage 列表 → 扁平消息 map 列表。
func baseMsgMaps(messages []llmschema.BaseMessage) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		if msgMap := llmschema.BaseMessageToMap(msg); msgMap != nil {
			result = append(result, msgMap)
		}
	}
	return result
}

// llmStep LLM 测试步骤（消息集）。
func llmStep(messages ...llmschema.BaseMessage) trajectory.TestStep {
	return trajectory.TestStep{Kind: "llm", Messages: baseMsgMaps(messages)}
}

// toolStep 工具测试步骤。
func toolStep(toolName string, callArgs map[string]any) trajectory.TestStep {
	return trajectory.TestStep{Kind: "tool", ToolName: toolName, CallArgs: callArgs}
}
