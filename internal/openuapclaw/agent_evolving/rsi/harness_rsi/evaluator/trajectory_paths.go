// trajectory_paths.go ACH 评估的用例级短轨迹路径与角色归档。
// Python: openjiuwen/rsi/harness_rsi/evaluator/trajectory_paths.py（298 行全量对齐）。
// 17.6 前置切片：Task 7 case_backend 消费 RoleFileTrajectoryStore 与
// role_execution_evidence，本文件整件提前落地（原计划 Task 9）。

package evaluator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	obs "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/observability"
)

// ──────────────────────────── 常量 ────────────────────────────

// RoleTrajectoryDirName 角色轨迹目录名（Python: ROLE_TRAJECTORY_DIR_NAME）。
const RoleTrajectoryDirName = "tr"

// TrajectoryEventsFileName 轨迹事件文件名。
const TrajectoryEventsFileName = "trajectory_events.jsonl"

// 裁剪上限三常量（Python 模块常量）。
const (
	// MaxSavedLLMMessages LLM 消息保留条数上限
	MaxSavedLLMMessages = 4
	// MaxSavedTextChars 文本截断上限
	MaxSavedTextChars = 1200
	// MaxSavedToolResultChars 工具载荷截断上限
	MaxSavedToolResultChars = 1200
)

// ──────────────────────────── 结构体 ────────────────────────────

// RoleFileTrajectoryStore 把成员的有界最新轨迹存为 `<case>/tr/<role>.jsonl`。
// Python: trajectory_paths.py:42 RoleFileTrajectoryStore(FileTrajectoryStore)——
// ACH 评估器只需角色轨迹供诊断与归因；通用 FileTrajectoryStore 追加全量快照
// （重复提示历史 + 工具 schema），ReAct 评估运行可膨胀到数 GB。保留相同 JSONL
// 形态供读取，但以紧凑最新快照覆写（有界消息 + 紧凑工具元数据 + 截断载荷）。
type RoleFileTrajectoryStore struct {
	*trajectory.FileTrajectoryStore
	// roleName 角色文件干（Python self.role_name）
	roleName string
	// mergeInvocations 是否跨 invoke 合并（Python self._merge_invocations）
	mergeInvocations bool
	// executionSteps 合并模式下的步骤累积（Python self._execution_steps）
	executionSteps []map[string]any
	// mu 覆写互斥
	mu sync.Mutex
	// baseDir 归档根目录（Python self._base_dir）
	baseDir string
}

var _ trajectory.TrajectoryStore = (*RoleFileTrajectoryStore)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRoleFileTrajectoryStore 构造角色归档存储。
// Python: trajectory_paths.py:50-55 __init__。
func NewRoleFileTrajectoryStore(baseDir, roleName string, mergeInvocations bool) *RoleFileTrajectoryStore {
	return &RoleFileTrajectoryStore{
		FileTrajectoryStore: trajectory.NewFileTrajectoryStore(baseDir),
		roleName:            SafeRoleFileStem(roleName),
		mergeInvocations:    mergeInvocations,
		executionSteps:      []map[string]any{},
		baseDir:             baseDir,
	}
}

// Save 覆写写有界角色轨迹。
// Python: trajectory_paths.py:60-68 save。
func (s *RoleFileTrajectoryStore) Save(traj *trajectory.Trajectory, version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filePath := s.getFilePath(version)
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	data := BoundedTrajectoryDict(RoleTraceDict(traj))
	if s.mergeInvocations {
		s.executionSteps = append(s.executionSteps, data["steps"].([]map[string]any)...)
		data["steps"] = s.executionSteps
	}
	payload, err := marshalCompact(data)
	if err != nil {
		return err
	}
	// Python: open(..., "w") 单行 JSONL 覆写。
	return os.WriteFile(filePath, append(payload, '\n'), 0o644)
}

// RoleExecutionEvidence 报告原生归档能否支撑负向能力结论。
// Python: trajectory_paths.py:227 role_execution_evidence——缺失或不可读证据
// 的调用计数未知；有界载荷保留工具身份但无法确立全部时序编辑边界。
func RoleExecutionEvidence(traceRoot, roleName string, rootRecording bool,
	captureIssues []string, expectedModelCalls, expectedToolCalls any) map[string]any {
	path := filepath.Join(traceRoot, SafeRoleFileStem(roleName)+".jsonl")
	reasons := map[string]struct{}{}
	for _, issue := range captureIssues {
		reasons[issue] = struct{}{}
	}
	if !rootRecording {
		reasons["run_root_not_recording"] = struct{}{}
	}
	evidence := map[string]any{
		"status":          "incomplete",
		"reasons":         []string{},
		"role_trace_path": path,
		"llm_call_count":  nil,
		"tool_call_count": nil,
		"timing_complete": false,
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		// Python: FileNotFoundError → missing_role_archive；其余 OSError 同归 invalid。
		if os.IsNotExist(err) {
			reasons["missing_role_archive"] = struct{}{}
		} else {
			reasons["invalid_role_archive"] = struct{}{}
		}
	} else if steps, ok := parseSteps(raw); ok {
		modelCalls, toolCalls := 0, 0
		missingResult, missingArgs, redactedArgs, payloadsTruncated := false, false, false, false
		for _, step := range steps {
			kind, _ := step["kind"].(string)
			detail, _ := step["detail"].(map[string]any)
			if kind == "llm" {
				modelCalls++
			} else if kind == "tool" {
				toolCalls++
				if _, exists := detail["call_result"]; !exists || detail["call_result"] == nil {
					missingResult = true
				}
				if _, exists := detail["call_args"]; !exists || detail["call_args"] == nil {
					missingArgs = true
				}
				if args, isStr := detail["call_args"].(string); isStr && strings.HasPrefix(args, "sha256:") {
					redactedArgs = true
				}
				if toolPayloadTruncated(detail) {
					payloadsTruncated = true
				}
			}
		}
		evidence["llm_call_count"] = modelCalls
		evidence["tool_call_count"] = toolCalls
		if modelCalls == 0 {
			reasons["missing_model_calls"] = struct{}{}
		}
		if expectedModelCalls != nil && toIntPointer(expectedModelCalls) != nil && *toIntPointer(expectedModelCalls) != modelCalls {
			reasons["model_call_count_mismatch"] = struct{}{}
		}
		if expectedToolCalls != nil && toIntPointer(expectedToolCalls) != nil && *toIntPointer(expectedToolCalls) != toolCalls {
			reasons["tool_call_count_mismatch"] = struct{}{}
		}
		if missingResult {
			reasons["missing_tool_result"] = struct{}{}
		}
		if missingArgs {
			reasons["missing_tool_arguments"] = struct{}{}
		}
		if redactedArgs {
			reasons["redacted_tool_arguments"] = struct{}{}
		}
		evidence["payloads_truncated"] = payloadsTruncated
		evidence["timing_complete"] = len(reasons) == 0 && !payloadsTruncated
		if len(reasons) == 0 {
			evidence["status"] = "complete"
		} else {
			evidence["status"] = "incomplete"
		}
	} else {
		reasons["invalid_role_archive"] = struct{}{}
	}
	evidence["reasons"] = sortedReasonKeys(reasons)
	return evidence
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getFilePath 归档文件路径（Python: _get_file_path——version 后缀可省）。
func (s *RoleFileTrajectoryStore) getFilePath(version string) string {
	suffix := ""
	if version != "" {
		suffix = "." + SafeRoleFileStem(version)
	}
	return filepath.Join(s.baseDir, s.roleName+suffix+".jsonl")
}

// RoleTraceDict 把规范 span 投影成紧凑 RSI 角色轨迹工件。
// Python: trajectory_paths.py:70 _role_trace_dict。
func RoleTraceDict(traj *trajectory.Trajectory) map[string]any {
	steps := []map[string]any{}
	for _, span := range trajectory.IterSpans(traj.ToOTLP()) {
		category := trajectory.SpanCategory(span)
		var step map[string]any
		if category == "llm" {
			attributes := trajectory.SpanAttributes(span)
			prompts, completions := trajectory.ReadLLMExchange(span)
			lastResponse := map[string]any{}
			if len(completions) > 0 {
				lastResponse = completions[len(completions)-1]
			}
			tools, _ := attributes[obs.GenAIToolDefinitions].([]any)
			step = map[string]any{
				"kind": category,
				"detail": map[string]any{
					"model":    stringOrTP(attributes[obs.GenAIRequestModel], ""),
					"messages": prompts,
					"response": lastResponse,
					"tools":    tools,
				},
			}
		} else if category == "tool" {
			toolCall := trajectory.ReadToolCall(span)
			detail := map[string]any{
				"tool_name": stringOrTP(toolCall["name"], ""),
			}
			if input, exists := toolCall["input"]; exists {
				detail["call_args"] = input
			}
			if output, exists := toolCall["output"]; exists {
				detail["call_result"] = output
			}
			step = map[string]any{"kind": category, "detail": detail}
			if errText := stringOrTP(toolCall["error"], ""); errText != "" {
				step["error"] = errText
			}
		} else {
			continue
		}
		steps = append(steps, step)
	}
	sessionID := ""
	if sid := traj.SessionID(); sid != nil {
		sessionID = *sid
	}
	return map[string]any{
		"execution_id": traj.TrajectoryID(),
		"session_id":   sessionID,
		"steps":        steps,
	}
}

// SafeRoleFileStem 文件干净化（Python: trajectory_paths.py:103
// _safe_role_file_stem——非字母数字下划线点连字符转下划线，首尾 ._- 剥离）。
func SafeRoleFileStem(value string) string {
	if value == "" {
		value = "unknown"
	}
	stem := roleStemPattern.ReplaceAllString(value, "_")
	stem = strings.Trim(stem, "._-")
	if stem == "" {
		return "unknown"
	}
	return stem
}

// roleStemPattern 非安全字符模式（Python re.sub(r"[^A-Za-z0-9_.-]+", "_", ...)）。
var roleStemPattern = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// BoundedTrajectoryDict 有界化轨迹字典。
// Python: trajectory_paths.py:110 _bounded_trajectory_dict。
func BoundedTrajectoryDict(data map[string]any) map[string]any {
	boundedAny := truncateJSONLike(data, MaxSavedTextChars)
	bounded, isMap := boundedAny.(map[string]any)
	if !isMap {
		return data
	}
	steps, _ := bounded["steps"].([]any)
	for _, item := range steps {
		step, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		detail, isMap := step["detail"].(map[string]any)
		if !isMap {
			continue
		}
		if _, hasMessages := detail["messages"]; hasMessages {
			boundLLMDetail(detail)
		} else if _, hasToolName := detail["tool_name"]; hasToolName {
			boundToolDetail(detail)
		}
	}
	return bounded
}

// boundLLMDetail LLM 步骤有界化（Python: _bound_llm_detail）。
func boundLLMDetail(detail map[string]any) {
	if raw, ok := detail["messages"].([]any); ok {
		kept := boundedMessages(raw)
		omitted := len(raw) - len(kept)
		detail["messages"] = kept
		if omitted > 0 {
			meta, isMap := detail["meta"].(map[string]any)
			if !isMap {
				meta = map[string]any{}
				detail["meta"] = meta
			}
			meta["omitted_message_count"] = omitted
		}
	}
	if raw, ok := detail["tools"].([]any); ok {
		tools := make([]any, 0, len(raw))
		for _, item := range raw {
			tools = append(tools, toolSummary(item))
		}
		detail["tools"] = tools
	}
	if _, exists := detail["response"]; exists {
		detail["response"] = truncateJSONLike(detail["response"], MaxSavedTextChars)
	}
}

// boundToolDetail 工具步骤有界化（Python: _bound_tool_detail）。
func boundToolDetail(detail map[string]any) {
	if _, exists := detail["call_args"]; exists {
		detail["call_args"] = truncateJSONLike(detail["call_args"], MaxSavedToolResultChars)
	}
	if _, exists := detail["call_result"]; exists {
		detail["call_result"] = truncateJSONLike(detail["call_result"], MaxSavedToolResultChars)
	}
	if _, exists := detail["tool_description"]; exists {
		detail["tool_description"] = truncateText(detail["tool_description"], MaxSavedTextChars)
	}
	if _, exists := detail["tool_schema"]; exists {
		detail["tool_schema"] = map[string]any{"omitted": true}
	}
}

// boundedMessages 消息头尾保留（Python: _bounded_messages——head=MAX/2，
// tail=MAX-head）。
func boundedMessages(messages []any) []any {
	var kept []any
	if len(messages) <= MaxSavedLLMMessages {
		kept = messages
	} else {
		headCount := MaxSavedLLMMessages / 2
		tailCount := MaxSavedLLMMessages - headCount
		kept = append(append([]any{}, messages[:headCount]...), messages[len(messages)-tailCount:]...)
	}
	result := make([]any, 0, len(kept))
	for _, message := range kept {
		result = append(result, truncateJSONLike(message, MaxSavedTextChars))
	}
	return result
}

// toolSummary 工具定义紧凑摘要（Python: _tool_summary）。
func toolSummary(raw any) map[string]any {
	tool, isMap := raw.(map[string]any)
	if !isMap {
		return map[string]any{"name": truncateText(fmt.Sprint(raw), MaxSavedTextChars)}
	}
	name := ""
	if fn, ok := tool["function"].(map[string]any); ok {
		if fnName, ok := fn["name"].(string); ok && fnName != "" {
			name = fnName
		}
	}
	if name == "" {
		for _, key := range []string{"name", "tool_name"} {
			if v, ok := tool[key].(string); ok && v != "" {
				name = v
				break
			}
		}
	}
	if name == "" {
		name = "unknown"
	}
	summary := map[string]any{"name": name}
	if toolType, ok := tool["type"]; ok && toolType != nil {
		summary["type"] = fmt.Sprint(toolType)
	}
	return summary
}

// truncateJSONLike 递归截断字符串值（Python: _truncate_json_like）。
func truncateJSONLike(value any, maxTextChars int) any {
	switch v := value.(type) {
	case string:
		return truncateText(v, maxTextChars)
	case []any:
		result := make([]any, 0, len(v))
		for _, item := range v {
			result = append(result, truncateJSONLike(item, maxTextChars))
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			result[fmt.Sprint(key)] = truncateJSONLike(item, maxTextChars)
		}
		return result
	default:
		return value
	}
}

// truncateText 文本截断（Python: _truncate_text——非字符串原样；超长追加
// "...[truncated N chars]"）。
func truncateText(value any, maxChars int) any {
	text, ok := value.(string)
	if !ok || len(text) <= maxChars {
		return value
	}
	return fmt.Sprintf("%s...[truncated %d chars]", text[:maxChars], len(text)-maxChars)
}

// toolPayloadTruncated 检测工具载荷中的有界拷贝标记。
// Python: trajectory_paths.py:209 _tool_payload_truncated。
func toolPayloadTruncated(detail map[string]any) bool {
	for _, key := range []string{"call_args", "call_result"} {
		rendered := ""
		if raw, err := marshalCompact(detail[key]); err == nil {
			rendered = string(raw)
		} else {
			rendered = fmt.Sprint(detail[key])
		}
		if strings.Contains(rendered, "...[truncated ") {
			return true
		}
	}
	return false
}

// marshalCompact 紧凑 JSON 序列化（Python json.dumps(ensure_ascii=False)）。
func marshalCompact(value any) ([]byte, error) {
	return json.Marshal(value)
}

// parseSteps 解析归档 JSONL 的 steps（严格校验，Python try/except 等价）。
func parseSteps(raw []byte) ([]map[string]any, bool) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, false
	}
	rawSteps, ok := payload["steps"].([]any)
	if !ok {
		return nil, false
	}
	steps := make([]map[string]any, 0, len(rawSteps))
	for _, item := range rawSteps {
		step, isMap := item.(map[string]any)
		if !isMap {
			return nil, false
		}
		kind, _ := step["kind"].(string)
		_, isDetail := step["detail"].(map[string]any)
		if (kind != "llm" && kind != "tool") || !isDetail {
			return nil, false
		}
		steps = append(steps, step)
	}
	return steps, true
}

// sortedReasonKeys 原因键排序输出（Python sorted(reasons)）。
func sortedReasonKeys(reasons map[string]struct{}) []string {
	keys := make([]string, 0, len(reasons))
	for key := range reasons {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// stringOr 非空字符串取值（Python attrs.get(key, "")）。
func stringOrTP(value any, fallback string) string {
	if text, ok := value.(string); ok && text != "" {
		return text
	}
	return fallback
}

// toIntPointer 期望调用计数的 Optional[int] 语义（nil=未提供）。
func toIntPointer(value any) *int {
	switch v := value.(type) {
	case int:
		return &v
	case float64:
		iv := int(v)
		return &iv
	default:
		return nil
	}
}
