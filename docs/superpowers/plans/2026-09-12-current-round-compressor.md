# CurrentRoundCompressor (5.25) 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 CurrentRoundCompressor 当轮增量压缩器，并将 Python util.py 中的共享函数统一提取到 Go util.go

**Architecture:** 与 Python util.py 保持一致，将所有共享工具函数迁移到 `util.go`，同时重构 `dialogue_compressor.go` 和 `full_compact_processor.go` 中已有的重复函数改为调用 util.go 导出版本。CurrentRoundCompressor 实现两阶段压缩（增量压缩 + 二次合并），输出协议化 `[CURRENT_ROUND_MEMORY_BLOCK]` 记忆块。

**Tech Stack:** Go 1.22+, llm.Model (压缩模型调用), processor.BaseProcessor (嵌入式基类), context_engine.RegisterProcessorFactory (工厂注册)

---

## Task 1: 扩充 util.go — 从 full_compact_processor.go 提取已有函数

**Files:**
- Modify: `internal/agentcore/context_engine/processor/compressor/util.go`
- Modify: `internal/agentcore/context_engine/processor/compressor/full_compact_processor.go`
- Test: `internal/agentcore/context_engine/processor/compressor/util_test.go`

将 `full_compact_processor.go` 中已有的包级函数提取到 `util.go`，改为导出版本。原有文件改为调用 util.go 导出函数。

- [ ] **Step 1: 在 util.go 中添加从 full_compact 提取的函数**

在 `util.go` 文件末尾添加以下导出函数（从 `full_compact_processor.go` 提取并导出化）：

```go
// MessageToText 提取消息纯文本内容。
//
// 对应 Python: util.message_to_text()
func MessageToText(msg llm_schema.BaseMessage) string {
	content := msg.GetContent().Text()
	if content != "" {
		return content
	}
	// 尝试从 parts 获取文本
	parts := msg.GetContent().Parts()
	if len(parts) > 0 {
		var texts []string
		for _, p := range parts {
			if p.Text != "" {
				texts = append(texts, p.Text)
			}
		}
		if len(texts) > 0 {
			return strings.Join(texts, "\n")
		}
	}
	return ""
}

// GroupCompletedAPIRoundsMessages 按已完成 API 轮次分组返回消息子列表。
//
// 对应 Python: util.group_completed_api_rounds()
func GroupCompletedAPIRoundsMessages(messages []llm_schema.BaseMessage) [][]llm_schema.BaseMessage {
	ranges := processor.GroupCompletedAPIRounds(messages)
	groups := make([][]llm_schema.BaseMessage, 0, len(ranges))
	for _, r := range ranges {
		groups = append(groups, messages[r[0]:r[1]])
	}
	return groups
}

// MessageSignature 生成消息签名（用于去重）。
//
// 对应 Python: util.message_signature()
func MessageSignature(msg llm_schema.BaseMessage) string {
	var toolCallIDs []string
	if am, ok := msg.(*llm_schema.AssistantMessage); ok {
		for _, tc := range am.ToolCalls {
			toolCallIDs = append(toolCallIDs, tc.ID)
		}
	}
	return fmt.Sprintf("%s|%s|%s", msg.GetRole().String(), MessageToText(msg), strings.Join(toolCallIDs, "|"))
}

// RoundSignature 生成轮次签名。
func RoundSignature(messages []llm_schema.BaseMessage) string {
	var sigs []string
	for _, msg := range messages {
		sigs = append(sigs, MessageSignature(msg))
	}
	return strings.Join(sigs, "|")
}

// FlattenGroups 将分组展平为单一切片。
func FlattenGroups(groups [][]llm_schema.BaseMessage) []llm_schema.BaseMessage {
	var result []llm_schema.BaseMessage
	for _, g := range groups {
		result = append(result, g...)
	}
	return result
}

// IsSkillFilePath 判断文件路径是否为 skill 文件。
//
// 对应 Python: util.is_skill_file_path()
func IsSkillFilePath(filePath string) bool {
	if filePath == "" {
		return false
	}
	normalized := strings.ReplaceAll(strings.ToLower(filePath), "\\", "/")
	return strings.HasSuffix(normalized, "/skill.md") || strings.HasSuffix(normalized, "skill.md")
}

// ExtractArgumentValue 从 JSON 参数文本中提取指定 key 的值。
//
// 先尝试从 parsedArgs 中提取，再 fallback 到正则提取 argumentsText。
// 对应 Python: util.extract_argument_value()
func ExtractArgumentValue(parsedArgs map[string]any, argumentsText string, keys ...string) string {
	// 先从 parsedArgs 提取
	if parsedArgs != nil {
		for _, key := range keys {
			if val, ok := parsedArgs[key].(string); ok && strings.TrimSpace(val) != "" {
				return strings.TrimSpace(val)
			}
		}
	}
	// 从 argumentsText 中 JSON 解析
	if argumentsText != "" {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(argumentsText), &parsed); err == nil {
			for _, key := range keys {
				if val, ok := parsed[key].(string); ok && strings.TrimSpace(val) != "" {
					return strings.TrimSpace(val)
				}
			}
		}
		// 正则 fallback
		for _, key := range keys {
			pattern := fmt.Sprintf(`"%s"\s*:\s*"([^"]+)"`, regexp.QuoteMeta(key))
			if match := regexp.MustCompile(pattern).FindStringSubmatch(argumentsText); len(match) > 1 {
				return strings.TrimSpace(match[1])
			}
		}
	}
	return ""
}

// RoundContainsSkillRead 检查轮次中是否包含 skill 文件读取。
//
// 对应 Python: util.round_contains_skill_read()
func RoundContainsSkillRead(messages []llm_schema.BaseMessage) bool {
	for _, msg := range messages {
		am, ok := msg.(*llm_schema.AssistantMessage)
		if !ok {
			continue
		}
		for _, tc := range am.ToolCalls {
			if tc.Name != "read_file" {
				continue
			}
			filePath := ExtractArgumentValue(nil, tc.Arguments, "file_path")
			if IsSkillFilePath(filePath) {
				return true
			}
		}
	}
	return false
}
```

注意：`util.go` 需要新增以下 import：
```go
import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	llm_schema "github.com/uapclaw/uap-claw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/context_engine/processor"
)
```

- [ ] **Step 2: 从 dialogue_compressor.go 迁移 EstimateContentTokens 到 util.go**

将 `dialogue_compressor.go` 中的 `EstimateContentTokens` 函数迁移到 `util.go`（它本来就在 Python 的 util.py 中）。在 `dialogue_compressor.go` 中删除该函数定义，因为同包内可以直接调用 util.go 中的版本。由于两个文件在同一个包内，函数名不变，无需额外操作——只需从 `dialogue_compressor.go` 中删除 `EstimateContentTokens` 函数定义即可。

- [ ] **Step 3: 修改 full_compact_processor.go — 改为调用 util.go 导出函数**

将以下非导出函数改为调用 util.go 的导出版本：

1. `_messageToText(msg)` → `MessageToText(msg)`
2. `groupCompletedAPIRounds(messages)` → `GroupCompletedAPIRoundsMessages(messages)`
3. `isSkillFilePath(filePath)` → `IsSkillFilePath(filePath)`
4. `extractArgumentValue(argumentsText, keys...)` → `ExtractArgumentValue(nil, argumentsText, keys...)`
5. `_messageSignature(msg)` → `MessageSignature(msg)`
6. `_roundSignature(messages)` → `RoundSignature(messages)`
7. `flattenGroups(groups)` → `FlattenGroups(groups)`
8. `roundContainsSkillRead(messages)` → `RoundContainsSkillRead(messages)`

删除 full_compact_processor.go 中这些旧的非导出函数定义。

- [ ] **Step 4: 运行 full_compact_processor 测试**

```bash
export GOPROXY=https://goproxy.cn,direct
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/compressor/... -run TestFullCompact -v -count=1
```

Expected: PASS

- [ ] **Step 5: 运行 dialogue_compressor 测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/compressor/... -run TestDialogue -v -count=1
```

Expected: PASS

- [ ] **Step 6: 为提取的函数补充 util_test.go 测试**

在 `util_test.go` 末尾补充以下测试函数：

```go
func TestMessageToText(t *testing.T) {
	t.Run("字符串内容", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("hello")
		got := MessageToText(msg)
		if got != "hello" {
			t.Errorf("MessageToText() = %q, want %q", got, "hello")
		}
	})
	t.Run("空字符串返回空", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("")
		got := MessageToText(msg)
		if got != "" {
			t.Errorf("MessageToText() = %q, want empty", got)
		}
	})
}

func TestMessageSignature(t *testing.T) {
	t.Run("UserMessage", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("hello")
		got := MessageSignature(msg)
		want := "user|hello|"
		if got != want {
			t.Errorf("MessageSignature() = %q, want %q", got, want)
		}
	})
	t.Run("AssistantMessage含ToolCalls", func(t *testing.T) {
		am := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, "thinking"),
			ToolCalls: []*llm_schema.ToolCall{{ID: "call_1"}, {ID: "call_2"}},
		}
		got := MessageSignature(am)
		if !strings.Contains(got, "call_1|call_2") {
			t.Errorf("MessageSignature() = %q, should contain tool call IDs", got)
		}
	})
}

func TestFlattenGroups(t *testing.T) {
	t.Run("多组展平", func(t *testing.T) {
		g1 := []llm_schema.BaseMessage{llm_schema.NewUserMessage("a"), llm_schema.NewUserMessage("b")}
		g2 := []llm_schema.BaseMessage{llm_schema.NewUserMessage("c")}
		got := FlattenGroups([][]llm_schema.BaseMessage{g1, g2})
		if len(got) != 3 {
			t.Errorf("FlattenGroups() = %d items, want 3", len(got))
		}
	})
	t.Run("空输入", func(t *testing.T) {
		got := FlattenGroups(nil)
		if len(got) != 0 {
			t.Errorf("FlattenGroups(nil) = %d items, want 0", len(got))
		}
	})
}

func TestIsSkillFilePath(t *testing.T) {
	t.Run("匹配skill.md", func(t *testing.T) {
		if !IsSkillFilePath("path/to/skill.md") {
			t.Error("IsSkillFilePath() = false, want true")
		}
	})
	t.Run("匹配/skill.md后缀", func(t *testing.T) {
		if !IsSkillFilePath("/home/user/skills/grep/skill.md") {
			t.Error("IsSkillFilePath() = false, want true")
		}
	})
	t.Run("不匹配非skill路径", func(t *testing.T) {
		if IsSkillFilePath("path/to/readme.md") {
			t.Error("IsSkillFilePath() = true, want false")
		}
	})
	t.Run("空路径", func(t *testing.T) {
		if IsSkillFilePath("") {
			t.Error("IsSkillFilePath('') = true, want false")
		}
	})
}

func TestExtractArgumentValue(t *testing.T) {
	t.Run("从parsedArgs提取", func(t *testing.T) {
		parsed := map[string]any{"file_path": "/tmp/test.go"}
		got := ExtractArgumentValue(parsed, "", "file_path")
		if got != "/tmp/test.go" {
			t.Errorf("ExtractArgumentValue() = %q, want %q", got, "/tmp/test.go")
		}
	})
	t.Run("从argumentsText正则提取", func(t *testing.T) {
		got := ExtractArgumentValue(nil, `{"file_path": "/tmp/test.go"}`, "file_path")
		if got != "/tmp/test.go" {
			t.Errorf("ExtractArgumentValue() = %q, want %q", got, "/tmp/test.go")
		}
	})
	t.Run("空输入", func(t *testing.T) {
		got := ExtractArgumentValue(nil, "", "file_path")
		if got != "" {
			t.Errorf("ExtractArgumentValue() = %q, want empty", got)
		}
	})
}

func TestRoundContainsSkillRead(t *testing.T) {
	t.Run("包含skill读取", func(t *testing.T) {
		am := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{Name: "read_file", Arguments: `{"file_path": "skills/grep/skill.md"}`},
			},
		}
		if !RoundContainsSkillRead([]llm_schema.BaseMessage{am}) {
			t.Error("RoundContainsSkillRead() = false, want true")
		}
	})
	t.Run("不含skill读取", func(t *testing.T) {
		am := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{Name: "read_file", Arguments: `{"file_path": "/tmp/other.txt"}`},
			},
		}
		if RoundContainsSkillRead([]llm_schema.BaseMessage{am}) {
			t.Error("RoundContainsSkillRead() = true, want false")
		}
	})
}
```

- [ ] **Step 7: 运行 util 测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/compressor/... -run TestMessageToText|TestMessageSignature|TestFlattenGroups|TestIsSkillFilePath|TestExtractArgumentValue|TestRoundContainsSkillRead -v -count=1
```

Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add -A && git commit -m "refactor: 从 full_compact_processor 和 dialogue_compressor 提取共享函数到 util.go"
```

---

## Task 2: 扩充 util.go — 新增 5.25 需要的函数

**Files:**
- Modify: `internal/agentcore/context_engine/processor/compressor/util.go`
- Test: `internal/agentcore/context_engine/processor/compressor/util_test.go`

新增 Python util.py 中 5.25 直接依赖的函数。

- [ ] **Step 1: 在 util.go 中新增以下函数**

```go
// IsSummaryMessage 判断消息是否为指定标记的摘要消息。
//
// 对应 Python: util.is_summary_message()
func IsSummaryMessage(msg llm_schema.BaseMessage, marker string) bool {
	_, ok := msg.(*llm_schema.UserMessage)
	return ok && strings.HasPrefix(msg.GetContent().Text(), marker)
}

// CollectSummaryIndices 收集所有指定标记的摘要消息索引。
//
// 对应 Python: util.collect_summary_indices()
func CollectSummaryIndices(messages []llm_schema.BaseMessage, marker string) []int {
	var indices []int
	for i, msg := range messages {
		if IsSummaryMessage(msg, marker) {
			indices = append(indices, i)
		}
	}
	return indices
}

// CountMessagesTokens 计算 Token 数，优先使用 TokenCounter，失败时降级到字符估算。
//
// 对应 Python: util.count_messages_tokens()
func CountMessagesTokens(tokenCounter iface.TokenCounter, messages []llm_schema.BaseMessage, modelName string, processorType string) int {
	if len(messages) == 0 {
		return 0
	}
	if tokenCounter != nil {
		count, err := tokenCounter.CountMessages(messages, modelName)
		if err == nil {
			return count
		}
		prefix := ""
		if processorType != "" {
			prefix = fmt.Sprintf("[%s] ", processorType)
		}
		logger.Warn(logger.ComponentAgentCore).
			Str("processor_type", processorType).
			Err(err).
			Msg(prefix + "token_counter 返回错误，降级为字符估算")
	}
	total := 0
	for _, msg := range messages {
		total += EstimateContentTokens(msg.GetContent().Text())
	}
	return total
}

// FindLastCompletedAPIRoundEndIdx 找到范围内最后一个完整 API 轮次的结束索引。
//
// 对应 Python: util.find_last_completed_api_round_end_idx()
func FindLastCompletedAPIRoundEndIdx(messages []llm_schema.BaseMessage, startIdx int, endIdx int) int {
	if endIdx < startIdx {
		return endIdx
	}
	candidateMessages := messages[startIdx : endIdx+1]
	completedRounds := processor.GroupCompletedAPIRounds(candidateMessages)
	if len(completedRounds) == 0 {
		return startIdx - 1
	}
	lastRound := completedRounds[len(completedRounds)-1]
	return startIdx + lastRound[1] - 1
}

// IterSummaryMergeRanges 返回连续摘要消息范围，用于二次合并。
//
// 对应 Python: util.iter_summary_merge_ranges()
func IterSummaryMergeRanges(messages []llm_schema.BaseMessage, marker string, minBlocks int) [][2]int {
	var ranges [][2]int
	var startIdx *int
	var previousIdx *int

	for idx, msg := range messages {
		if IsSummaryMessage(msg, marker) {
			if startIdx == nil {
				s := idx
				startIdx = &s
			}
			p := idx
			previousIdx = &p
			continue
		}
		if startIdx != nil && previousIdx != nil {
			if *previousIdx-*startIdx+1 >= minBlocks {
				ranges = append(ranges, [2]int{*startIdx, *previousIdx})
			}
			startIdx = nil
			previousIdx = nil
		}
	}

	if startIdx != nil && previousIdx != nil {
		if *previousIdx-*startIdx+1 >= minBlocks {
			ranges = append(ranges, [2]int{*startIdx, *previousIdx})
		}
	}

	return ranges
}

// ParseToolArguments 解析工具调用 JSON 参数。
//
// 对应 Python: util.parse_tool_arguments()
func ParseToolArguments(argumentsText string) map[string]any {
	if argumentsText == "" {
		return map[string]any{}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(argumentsText), &parsed); err != nil {
		return map[string]any{}
	}
	return parsed
}

// DescribeToolCall 生成工具调用的可读描述。
//
// 对应 Python: util.describe_tool_call()
func DescribeToolCall(toolName string, argumentsText string) string {
	parsed := ParseToolArguments(argumentsText)
	switch toolName {
	case "read_file":
		filePath := ExtractArgumentValue(parsed, argumentsText, "file_path")
		return fmt.Sprintf("read_file path=%s", filePathOrDefault(filePath))
	case "write_file":
		filePath := ExtractArgumentValue(parsed, argumentsText, "file_path")
		return fmt.Sprintf("write_file path=%s", filePathOrDefault(filePath))
	case "edit_file":
		filePath := ExtractArgumentValue(parsed, argumentsText, "file_path")
		return fmt.Sprintf("edit_file path=%s", filePathOrDefault(filePath))
	case "glob":
		pattern := ExtractArgumentValue(parsed, argumentsText, "pattern")
		path := ExtractArgumentValue(parsed, argumentsText, "path")
		return fmt.Sprintf("glob pattern=%s path=%s", filePathOrDefault(pattern), pathOrDefault(path))
	case "grep":
		pattern := ExtractArgumentValue(parsed, argumentsText, "pattern")
		path := ExtractArgumentValue(parsed, argumentsText, "path", "file_path")
		return fmt.Sprintf("grep pattern=%s path=%s", filePathOrDefault(pattern), filePathOrDefault(path))
	default:
		return fmt.Sprintf("%s args=%s", toolName, argumentsText)
	}
}

// FindToolResultText 根据 toolCallID 查找工具结果文本。
//
// 对应 Python: util.find_tool_result_text()
func FindToolResultText(messages []llm_schema.BaseMessage, toolCallID string) string {
	if toolCallID == "" {
		return ""
	}
	for i := len(messages) - 1; i >= 0; i-- {
		tm, ok := messages[i].(*llm_schema.ToolMessage)
		if ok && tm.ToolCallID == toolCallID {
			return MessageToText(tm)
		}
	}
	return ""
}

// ExtractToolResultHint 提取工具结果的简要提示。
//
// 对应 Python: util.extract_tool_result_hint()
func ExtractToolResultHint(toolName string, resultText string, allowedToolNames []string) string {
	if resultText == "" {
		return ""
	}
	allowed := false
	for _, name := range allowedToolNames {
		if name == toolName {
			allowed = true
			break
		}
	}
	if !allowed {
		return ""
	}
	switch toolName {
	case "read_file":
		filePathMatch := regexp.MustCompile(`"file_path"\s*:\s*"([^"]+)"`).FindStringSubmatch(resultText)
		lineCountMatch := regexp.MustCompile(`"line_count"\s*:\s*(\d+)`).FindStringSubmatch(resultText)
		var parts []string
		if len(filePathMatch) > 1 {
			parts = append(parts, fmt.Sprintf("result_path=%s", filePathMatch[1]))
		}
		if len(lineCountMatch) > 1 {
			parts = append(parts, fmt.Sprintf("lines=%s", lineCountMatch[1]))
		}
		return strings.Join(parts, " ")
	case "glob":
		countMatch := regexp.MustCompile(`"count"\s*:\s*(\d+)`).FindStringSubmatch(resultText)
		if len(countMatch) > 1 {
			return fmt.Sprintf("matches=%s", countMatch[1])
		}
	case "grep":
		countMatch := regexp.MustCompile(`"count"\s*:\s*(\d+)`).FindStringSubmatch(resultText)
		if len(countMatch) > 1 {
			return fmt.Sprintf("hits=%s", countMatch[1])
		}
	case "edit_file":
		replacementsMatch := regexp.MustCompile(`"replacements"\s*:\s*(\d+)`).FindStringSubmatch(resultText)
		if len(replacementsMatch) > 1 {
			return fmt.Sprintf("replacements=%s", replacementsMatch[1])
		}
	case "write_file":
		bytesMatch := regexp.MustCompile(`"bytes_written"\s*:\s*(\d+)`).FindStringSubmatch(resultText)
		if len(bytesMatch) > 1 {
			return fmt.Sprintf("bytes_written=%s", bytesMatch[1])
		}
	}
	return ""
}

// ExtractSkillNameFromPath 从文件路径中提取 skill 名称。
//
// 对应 Python: util.extract_skill_name_from_path()
func ExtractSkillNameFromPath(filePath string) string {
	if filePath == "" {
		return ""
	}
	normalized := strings.ReplaceAll(filePath, "\\", "/")
	normalized = strings.TrimRight(normalized, "/")
	parts := strings.Split(normalized, "/")
	if len(parts) >= 2 && strings.EqualFold(parts[len(parts)-1], "skill.md") {
		return parts[len(parts)-2]
	}
	return ""
}

// ExtractSkillFileContent 提取 skill 文件内容。
//
// truncateFn 用于截断文本，通常为 FullCompactProcessor.TruncateStateText。
// 对应 Python: util.extract_skill_file_content()
func ExtractSkillFileContent(truncateFn func(string) string, resultText string) string {
	if resultText == "" {
		return ""
	}
	contentMatch := regexp.MustCompile(`"content"\s*:\s*"((?:[^"\\]|\\.)*)"`).FindStringSubmatch(resultText)
	content := ""
	if len(contentMatch) > 1 {
		rawContent := contentMatch[1]
		var err error
		content, err = stringUnescape(rawContent)
		if err != nil {
			content = strings.ReplaceAll(strings.ReplaceAll(rawContent, `\"`, `"`), `\n`, "\n")
		}
	} else {
		content = resultText
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	if truncateFn != nil {
		return truncateFn(content)
	}
	return content
}
```

在 util.go 末尾添加辅助函数：

```go
// ──────────────────────────── 非导出函数 ────────────────────────────

func filePathOrDefault(path string) string {
	if path == "" {
		return "[unknown]"
	}
	return path
}

func pathOrDefault(path string) string {
	if path == "" {
		return "."
	}
	return path
}

// stringUnescape 对 JSON 字符串进行反转义
func stringUnescape(s string) (string, error) {
	var result string
	err := json.Unmarshal([]byte(`"`+s+`"`), &result)
	return result, err
}
```

util.go 需要额外 import：
```go
import (
	iface "github.com/uapclaw/uap-claw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uap-claw-go/internal/common/logger"
)
```

- [ ] **Step 2: 修改 dialogue_compressor.go 的 countMessagesTokens 方法改为调用 CountMessagesTokens**

将 `dialogue_compressor.go` 中的 `countMessagesTokens` 方法改为调用 util.go 的 `CountMessagesTokens`：

```go
// countMessagesTokens 计算消息列表的 Token 数。
func (dc *DialogueCompressor) countMessagesTokens(mc iface.ModelContext, messages []llm_schema.BaseMessage) int {
	modelName := ""
	if dc.model != nil && dc.model.ModelConfig != nil {
		modelName = dc.model.ModelConfig.ModelName
	}
	return CountMessagesTokens(mc.TokenCounter(), messages, modelName, dc.ProcessorType())
}
```

- [ ] **Step 3: 为新增函数补充 util_test.go 测试**

```go
func TestIsSummaryMessage(t *testing.T) {
	t.Run("是摘要消息", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("[CURRENT_ROUND_MEMORY_BLOCK]\nSummary:\ntest")
		if !IsSummaryMessage(msg, "[CURRENT_ROUND_MEMORY_BLOCK]") {
			t.Error("IsSummaryMessage() = false, want true")
		}
	})
	t.Run("不是摘要消息_标记不匹配", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("[DIALOGUE_MEMORY_BLOCK]\nSummary:\ntest")
		if IsSummaryMessage(msg, "[CURRENT_ROUND_MEMORY_BLOCK]") {
			t.Error("IsSummaryMessage() = true, want false")
		}
	})
	t.Run("不是摘要消息_非UserMessage", func(t *testing.T) {
		msg := llm_schema.NewAssistantMessage("hello")
		if IsSummaryMessage(msg, "[CURRENT_ROUND_MEMORY_BLOCK]") {
			t.Error("IsSummaryMessage() = true, want false")
		}
	})
}

func TestCollectSummaryIndices(t *testing.T) {
	t.Run("多个摘要", func(t *testing.T) {
		marker := "[CURRENT_ROUND_MEMORY_BLOCK]"
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			llm_schema.NewUserMessage(marker + "\nSummary:\ns1"),
			llm_schema.NewAssistantMessage("hi"),
			llm_schema.NewUserMessage(marker + "\nSummary:\ns2"),
		}
		got := CollectSummaryIndices(messages, marker)
		if len(got) != 2 || got[0] != 1 || got[1] != 3 {
			t.Errorf("CollectSummaryIndices() = %v, want [1 3]", got)
		}
	})
	t.Run("无摘要", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
		got := CollectSummaryIndices(messages, "[CURRENT_ROUND_MEMORY_BLOCK]")
		if len(got) != 0 {
			t.Errorf("CollectSummaryIndices() = %v, want []", got)
		}
	})
}

func TestCountMessagesTokens(t *testing.T) {
	t.Run("空消息返回0", func(t *testing.T) {
		got := CountMessagesTokens(nil, nil, "", "")
		if got != 0 {
			t.Errorf("CountMessagesTokens(nil) = %d, want 0", got)
		}
	})
	t.Run("无TokenCounter降级到字符估算", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello world")}
		got := CountMessagesTokens(nil, messages, "", "TestProcessor")
		// "hello world" = 11 chars, 11/3 = 3
		if got <= 0 {
			t.Errorf("CountMessagesTokens() = %d, want > 0", got)
		}
	})
}

func TestFindLastCompletedAPIRoundEndIdx(t *testing.T) {
	t.Run("有完成轮次", func(t *testing.T) {
		// User → Assistant(无tool_calls) = 一个完整轮次
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			llm_schema.NewAssistantMessage("hi"),
			llm_schema.NewUserMessage("world"),
		}
		got := FindLastCompletedAPIRoundEndIdx(messages, 0, 1)
		if got != 1 {
			t.Errorf("FindLastCompletedAPIRoundEndIdx() = %d, want 1", got)
		}
	})
	t.Run("无完成轮次", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
		got := FindLastCompletedAPIRoundEndIdx(messages, 0, 0)
		// 无完整轮次返回 startIdx-1
		if got != -1 {
			t.Errorf("FindLastCompletedAPIRoundEndIdx() = %d, want -1", got)
		}
	})
	t.Run("endIdx小于startIdx", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
		got := FindLastCompletedAPIRoundEndIdx(messages, 1, 0)
		if got != 0 {
			t.Errorf("FindLastCompletedAPIRoundEndIdx() = %d, want 0", got)
		}
	})
}

func TestIterSummaryMergeRanges(t *testing.T) {
	marker := "[CURRENT_ROUND_MEMORY_BLOCK]"
	t.Run("足够连续块", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage(marker + "\nSummary:\ns1"),
			llm_schema.NewUserMessage(marker + "\nSummary:\ns2"),
			llm_schema.NewUserMessage(marker + "\nSummary:\ns3"),
			llm_schema.NewAssistantMessage("break"),
			llm_schema.NewUserMessage(marker + "\nSummary:\ns4"),
		}
		got := IterSummaryMergeRanges(messages, marker, 3)
		if len(got) != 1 || got[0] != [2]int{0, 2} {
			t.Errorf("IterSummaryMergeRanges() = %v, want [[0 2]]", got)
		}
	})
	t.Run("不足连续块", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage(marker + "\nSummary:\ns1"),
			llm_schema.NewUserMessage(marker + "\nSummary:\ns2"),
		}
		got := IterSummaryMergeRanges(messages, marker, 3)
		if len(got) != 0 {
			t.Errorf("IterSummaryMergeRanges() = %v, want []", got)
		}
	})
}

func TestParseToolArguments(t *testing.T) {
	t.Run("正常JSON", func(t *testing.T) {
		got := ParseToolArguments(`{"file_path": "/tmp/test.go"}`)
		if got["file_path"] != "/tmp/test.go" {
			t.Errorf("ParseToolArguments() = %v, want file_path=/tmp/test.go", got)
		}
	})
	t.Run("空字符串", func(t *testing.T) {
		got := ParseToolArguments("")
		if len(got) != 0 {
			t.Errorf("ParseToolArguments('') = %v, want empty map", got)
		}
	})
	t.Run("非法JSON", func(t *testing.T) {
		got := ParseToolArguments("not json")
		if len(got) != 0 {
			t.Errorf("ParseToolArguments('not json') = %v, want empty map", got)
		}
	})
}

func TestDescribeToolCall(t *testing.T) {
	t.Run("read_file", func(t *testing.T) {
		got := DescribeToolCall("read_file", `{"file_path": "/tmp/test.go"}`)
		if !strings.Contains(got, "read_file") || !strings.Contains(got, "/tmp/test.go") {
			t.Errorf("DescribeToolCall() = %q", got)
		}
	})
	t.Run("未知工具", func(t *testing.T) {
		got := DescribeToolCall("custom_tool", `{"arg": "val"}`)
		if !strings.Contains(got, "custom_tool") {
			t.Errorf("DescribeToolCall() = %q", got)
		}
	})
}

func TestFindToolResultText(t *testing.T) {
	t.Run("找到结果", func(t *testing.T) {
		tm := llm_schema.NewToolMessage("call_1", "file content here")
		messages := []llm_schema.BaseMessage{tm}
		got := FindToolResultText(messages, "call_1")
		if got != "file content here" {
			t.Errorf("FindToolResultText() = %q, want %q", got, "file content here")
		}
	})
	t.Run("未找到", func(t *testing.T) {
		got := FindToolResultText(nil, "call_999")
		if got != "" {
			t.Errorf("FindToolResultText() = %q, want empty", got)
		}
	})
	t.Run("空ToolCallID", func(t *testing.T) {
		got := FindToolResultText(nil, "")
		if got != "" {
			t.Errorf("FindToolResultText() = %q, want empty", got)
		}
	})
}

func TestExtractSkillNameFromPath(t *testing.T) {
	t.Run("正常skill路径", func(t *testing.T) {
		got := ExtractSkillNameFromPath("skills/grep/skill.md")
		if got != "grep" {
			t.Errorf("ExtractSkillNameFromPath() = %q, want %q", got, "grep")
		}
	})
	t.Run("非skill路径", func(t *testing.T) {
		got := ExtractSkillNameFromPath("path/to/readme.md")
		if got != "" {
			t.Errorf("ExtractSkillNameFromPath() = %q, want empty", got)
		}
	})
	t.Run("空路径", func(t *testing.T) {
		got := ExtractSkillNameFromPath("")
		if got != "" {
			t.Errorf("ExtractSkillNameFromPath() = %q, want empty", got)
		}
	})
}

func TestExtractSkillFileContent(t *testing.T) {
	t.Run("空内容", func(t *testing.T) {
		got := ExtractSkillFileContent(nil, "")
		if got != "" {
			t.Errorf("ExtractSkillFileContent() = %q, want empty", got)
		}
	})
	t.Run("正常JSON内容", func(t *testing.T) {
		input := `"content": "hello skill"`
		got := ExtractSkillFileContent(nil, input)
		if got == "" {
			t.Errorf("ExtractSkillFileContent() = empty, want non-empty")
		}
	})
}
```

- [ ] **Step 4: 运行全部 util 测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/compressor/... -v -count=1
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat: 在 util.go 新增 5.25 需要的工具函数（IsSummaryMessage/CountMessagesTokens/FindLastCompletedAPIRoundEndIdx/IterSummaryMergeRanges 等）"
```

---

## Task 3: 实现 CurrentRoundCompressor 主体

**Files:**
- Create: `internal/agentcore/context_engine/processor/compressor/current_round_compressor.go`

创建 CurrentRoundCompressor 完整实现，包括配置、常量、提示词、核心方法和工厂注册。

- [ ] **Step 1: 创建 current_round_compressor.go**

完整实现文件，包含：
- `CurrentRoundCompressorConfig` 配置结构体（11 个字段）+ `Validate()` + `NewCurrentRoundCompressorConfig()`
- `CurrentRoundCompressor` 处理器结构体
- `CurrentRoundCompressorOption` + `WithCurrentRoundModel`
- 常量：`currentRoundMemoryBlockMarker`、`defaultCurrentRoundCompressionPrompt`、`defaultCleanPrompt`
- 核心方法：`ProcessorType`、`TriggerAddMessages`、`OnAddMessages`、`GetCompressIdx`、`MultiCompress`、`Compress`、`MergeSummaryBlocks`
- 辅助方法：`WrapCurrentRoundMemoryBlock`、`UnwrapMemoryBlockSummary`、`BuildPrompt`、`FormatRecentContext`、`FormatPriorContextAndQuery`
- `init()` 工厂注册
- 日志同步（9 个日志点）

提示词需与 Python `DEFAULT_COMPRESSION_PROMPT` 和 `CLEAN_PROMPT` 完全对齐。

此文件代码量大（约 600-700 行），需严格按照 Python `current_round_compressor.py` 的逻辑逐方法翻译，同时遵循项目编码规范（中文注释、声明排列顺序等）。

- [ ] **Step 2: 编译检查**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/context_engine/processor/compressor/...
```

Expected: 无编译错误

- [ ] **Step 3: 提交**

```bash
git add -A && git commit -m "feat: 实现 CurrentRoundCompressor 当轮增量压缩器 (5.25)"
```

---

## Task 4: 补充 CurrentRoundCompressor 测试

**Files:**
- Create: `internal/agentcore/context_engine/processor/compressor/current_round_compressor_test.go`

覆盖所有核心场景，使用 `WithCurrentRoundModel` 注入 mock LLM。

- [ ] **Step 1: 创建 current_round_compressor_test.go**

包含以下测试函数：

配置校验：
- `TestNewCurrentRoundCompressorConfig_默认值`
- `TestCurrentRoundCompressorConfig_Validate_正常`
- `TestCurrentRoundCompressorConfig_Validate_TokensThreshold零`
- `TestCurrentRoundCompressorConfig_Validate_MessagesToKeep负数`

核心方法：
- `TestCurrentRoundCompressor_ProcessorType`
- `TestCurrentRoundCompressor_TriggerAddMessages_超过阈值`
- `TestCurrentRoundCompressor_TriggerAddMessages_低于阈值`
- `TestCurrentRoundCompressor_OnAddMessages_触发压缩`（mock LLM，验证输出含 `[CURRENT_ROUND_MEMORY_BLOCK]`）
- `TestCurrentRoundCompressor_OnAddMessages_不压缩`
- `TestCurrentRoundCompressor_OnAddMessages_最后一条是UserMessage`
- `TestCurrentRoundCompressor_GetCompressIdx_找到边界`
- `TestCurrentRoundCompressor_GetCompressIdx_最后UserMessage`
- `TestCurrentRoundCompressor_GetCompressIdx_在保留区域内`

辅助方法：
- `TestWrapCurrentRoundMemoryBlock`
- `TestUnwrapMemoryBlockSummary`
- `TestFormatRecentContext_排除记忆块`
- `TestFormatPriorContextAndQuery_过滤工具消息`
- `TestFormatPriorContextAndQuery_窗口截断`

工厂注册：
- `TestCurrentRoundCompressor_工厂注册`
- `TestCurrentRoundCompressor_工厂配置类型不匹配`

mock LLM 使用 `WithCurrentRoundModel` 注入，通过 `llm.NewModel` 的 fake 实现或直接构造带有 mock `Invoke` 方法的 `*llm.Model`。

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/processor/compressor/... -run TestCurrentRound -v -count=1
```

Expected: PASS

- [ ] **Step 3: 检查测试覆盖率**

```bash
cd /home/opensource/uap-claw-go && go test -coverprofile=coverage.out ./internal/agentcore/context_engine/processor/compressor/... && go tool cover -func=coverage.out | grep current_round_compressor
```

Expected: 覆盖率 ≥ 85%

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "test: 补充 CurrentRoundCompressor 单元测试 (5.25)"
```

---

## Task 5: 更新 doc.go 和 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agentcore/context_engine/processor/compressor/doc.go`
- Modify: `internal/agentcore/context_engine/processor/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 compressor/doc.go 文件目录**

在文件目录树中添加 `current_round_compressor.go` 条目，并更新包功能概述。

- [ ] **Step 2: 更新 processor/doc.go**

如果 processor/doc.go 的文件目录中列出了 compressor/ 子目录，确认包含 `current_round_compressor.go`。

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 5.25 的状态从 `☐` 改为 `✅`，添加实现产出描述。

- [ ] **Step 4: 运行全量测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_engine/... -count=1
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "docs: 更新 doc.go 和 IMPLEMENTATION_PLAN.md (5.25 ✅)"
```
