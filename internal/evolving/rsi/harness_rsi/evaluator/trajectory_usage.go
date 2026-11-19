package evaluator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CollectSuccessfulToolNames 只收集成功完成的 Tool 名。
// Python: trajectory_usage.py collect_successful_tool_names。
func CollectSuccessfulToolNames(value any, names map[string]struct{}) {
	for _, step := range successfulToolSteps(value) {
		detail := step["detail"].(map[string]any)
		toolName := trimSpaces(toSignalString(detail["tool_name"]))
		if toolName != "" {
			names[toolName] = struct{}{}
		}
	}
}

// CollectSuccessfulSkillNames 收集成功完成的 skill_tool 调用装载的 Skill 名。
// Python: trajectory_usage.py collect_successful_skill_names。
func CollectSuccessfulSkillNames(value any, names map[string]struct{}) {
	for _, step := range successfulToolSteps(value) {
		detail := step["detail"].(map[string]any)
		toolName := trimSpaces(toSignalString(detail["tool_name"]))
		if canonicalToolName(toolName) != "skill" {
			continue
		}
		for _, key := range []string{"call_args", "arguments", "tool_input", "input"} {
			addSkillNameFromArgs(detail[key], names)
		}
	}
}

// CollectJSONLSuccessfulUsage 从原生 Team 成员轨迹文件收集完成用量。
// Python: trajectory_usage.py collect_jsonl_successful_usage。
func CollectJSONLSuccessfulUsage(path string, toolNames, skillNames map[string]struct{}) {
	payloads := readJSONLPayloads(path)
	for _, payload := range payloads {
		if toolNames != nil {
			CollectSuccessfulToolNames(payload, toolNames)
		}
		if skillNames != nil {
			CollectSuccessfulSkillNames(payload, skillNames)
		}
	}
}

// CollectPreEditSuccessfulUsage 收集首次编辑前（或后续纠正编辑前）的用量。
// Python: trajectory_usage.py collect_pre_edit_successful_usage——
// 总是返回首次编辑步号供诊断；晚期干预要求本轨迹内有后续成功编辑；
// 仅在工作完成后打开 Skill 不算数。这建立的是时间资格，而非因果证明。
func CollectPreEditSuccessfulUsage(
	value any,
	toolNames, skillNames map[string]struct{},
	allowLaterEdit bool,
) *int {
	firstEditStep := -1
	pendingTools := map[string]struct{}{}
	pendingSkills := map[string]struct{}{}
	steps := orderedSteps(value)
	for stepIndex, step := range steps {
		if !toolStepSucceeded(step) {
			continue
		}
		if firstEditStep < 0 || allowLaterEdit {
			detail := step["detail"].(map[string]any)
			toolName := trimSpaces(toSignalString(detail["tool_name"]))
			if toolName != "" {
				pendingTools[toolName] = struct{}{}
			}
			collectSkillNamesFromDetail(detail, pendingSkills)
		}
		isEdit := isPersistentEditStep(step)
		if !allowLaterEdit || isEdit {
			if toolNames != nil {
				for name := range pendingTools {
					toolNames[name] = struct{}{}
				}
			}
			if skillNames != nil {
				for name := range pendingSkills {
					skillNames[name] = struct{}{}
				}
			}
		}
		if firstEditStep < 0 && isEdit {
			firstEditStep = stepIndex
		}
	}
	if firstEditStep < 0 {
		return nil
	}
	return &firstEditStep
}

// CollectJSONLPreEditSuccessfulUsage 从原生轨迹 JSONL 流收集编辑前用量。
// Python: trajectory_usage.py collect_jsonl_pre_edit_successful_usage。
func CollectJSONLPreEditSuccessfulUsage(
	path string,
	toolNames, skillNames map[string]struct{},
	allowLaterEdit bool,
) *int {
	payloads := readJSONLPayloads(path)
	steps := []any{}
	for _, payload := range payloads {
		for _, step := range orderedSteps(payload) {
			steps = append(steps, step)
		}
	}
	return CollectPreEditSuccessfulUsage(
		map[string]any{"steps": steps},
		toolNames, skillNames, allowLaterEdit,
	)
}

// CollectPreInvestigationSuccessfulSkillUsage 收集首次成功取证调用前装载的 Skill。
// Python: trajectory_usage.py collect_pre_investigation_successful_skill_usage——
// 求解器开始读文件/跑探针后装载的 Skill 不能归因于塑造初始假设；
// 该边界刻意比编辑前门更严：首个成功的非 Skill Tool 完成即开启取证。
func CollectPreInvestigationSuccessfulSkillUsage(value any, skillNames map[string]struct{}) *int {
	firstInvestigationStep := -1
	steps := orderedSteps(value)
	for stepIndex, step := range steps {
		if !toolStepSucceeded(step) {
			continue
		}
		detail := step["detail"].(map[string]any)
		toolName := canonicalToolName(trimSpaces(toSignalString(detail["tool_name"])))
		if toolName == "skill" && firstInvestigationStep < 0 {
			collectSkillNamesFromDetail(detail, skillNames)
			continue
		}
		if toolName != "skill" && firstInvestigationStep < 0 {
			firstInvestigationStep = stepIndex
		}
	}
	if firstInvestigationStep < 0 {
		return nil
	}
	return &firstInvestigationStep
}

// CollectJSONLPreInvestigationSuccessfulSkillUsage 从轨迹 JSONL 流收集取证前 Skill 用量。
// Python: trajectory_usage.py collect_jsonl_pre_investigation_successful_skill_usage。
func CollectJSONLPreInvestigationSuccessfulSkillUsage(path string, skillNames map[string]struct{}) *int {
	payloads := readJSONLPayloads(path)
	steps := []any{}
	for _, payload := range payloads {
		for _, step := range orderedSteps(payload) {
			steps = append(steps, step)
		}
	}
	return CollectPreInvestigationSuccessfulSkillUsage(
		map[string]any{"steps": steps}, skillNames,
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// successfulToolSteps 成功的 Tool 步（Python: _successful_tool_steps 生成器 → 切片）。
func successfulToolSteps(value any) []map[string]any {
	result := []map[string]any{}
	for _, step := range orderedSteps(value) {
		if !toolStepSucceeded(step) {
			continue
		}
		if detail, ok := step["detail"].(map[string]any); ok {
			result = append(result, map[string]any{"step": step, "detail": detail})
		}
	}
	return result
}

// orderedSteps 按执行顺序产出顶层轨迹步（Python: _ordered_steps 生成器 → 切片）。
func orderedSteps(value any) []map[string]any {
	result := []map[string]any{}
	appendOrderedSteps(value, &result)
	return result
}

// appendOrderedSteps orderedSteps 的递归实现（保持 Python 生成器产出顺序）。
func appendOrderedSteps(value any, out *[]map[string]any) {
	switch typed := value.(type) {
	case map[string]any:
		if steps, ok := typed["steps"].([]any); ok {
			for _, step := range steps {
				if stepMap, ok := step.(map[string]any); ok {
					*out = append(*out, stepMap)
				}
			}
			return
		}
		if messages, ok := typed["messages"].([]any); ok {
			for _, message := range messages {
				messageMap, ok := message.(map[string]any)
				if !ok {
					continue
				}
				toolCalls, _ := messageMap["tool_calls"].([]any)
				for _, call := range toolCalls {
					callMap, ok := call.(map[string]any)
					if !ok {
						continue
					}
					errorValue := toSignalString(callMap["error"])
					var errorAny any
					if errorValue != "" {
						errorAny = errorValue
					}
					*out = append(*out, map[string]any{
						"kind":  "tool",
						"error": errorAny,
						"detail": map[string]any{
							"tool_name":   toSignalString(callMap["name"]),
							"call_args":   callMap["input"],
							"call_result": callMap["output"],
						},
					})
				}
			}
			return
		}
		if _, hasKind := typed["kind"]; hasKind {
			if _, hasDetail := typed["detail"]; hasDetail {
				*out = append(*out, typed)
				return
			}
		}
		if _, hasType := typed["type"]; hasType {
			if _, hasDetail := typed["detail"]; hasDetail {
				*out = append(*out, typed)
				return
			}
		}
		// Python: for nested in value.values()——map 迭代序非插入序；
		// 键排序近似（偏差 D-d5：多嵌套分支顺序敏感场景极罕见）
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sortStrings(keys)
		for _, key := range keys {
			appendOrderedSteps(typed[key], out)
		}
	case []any:
		for _, nested := range typed {
			appendOrderedSteps(nested, out)
		}
	}
}

// toolStepSucceeded 结构化 Tool 步是否真实成功（Python: _tool_step_succeeded）。
func toolStepSucceeded(step map[string]any) bool {
	stepType := step["kind"]
	if stepType == nil {
		stepType = step["type"]
	}
	if strings.ToLower(trimSpaces(toSignalString(stepType))) != "tool" {
		return false
	}
	detail, ok := step["detail"].(map[string]any)
	if !ok {
		return false
	}
	if trimSpaces(toSignalString(detail["tool_name"])) == "" {
		return false
	}
	if _, hasResult := detail["call_result"]; !hasResult {
		return false
	}
	if step["error"] != nil {
		return false
	}
	callResult := detail["call_result"]
	if callResult == nil {
		return false
	}
	if resultMap, ok := callResult.(map[string]any); ok {
		if resultMap["success"] == false || toSignalString(resultMap["error"]) != "" {
			return false
		}
		status := strings.ToLower(trimSpaces(toSignalString(resultMap["status"])))
		if status == "error" || status == "failed" || status == "failure" || status == "skipped" {
			return false
		}
	} else if resultStr, ok := callResult.(string); ok {
		lower := strings.ToLower(trimSpaces(resultStr))
		for _, prefix := range []string{"error", "exception", "failed", "[reliability]"} {
			if strings.HasPrefix(lower, prefix) {
				return false
			}
		}
	}
	return true
}

// persistentEditToolNames 持久编辑工具名集合（Python: _is_persistent_edit_step 内联集合）。
var persistentEditToolNames = map[string]struct{}{
	"apply_patch": {}, "code_edit": {}, "create_file": {}, "edit_file": {},
	"file_edit": {}, "patch_file": {}, "replace_in_file": {},
	"str_replace_editor": {}, "write_file": {},
}

// shellLikeToolNames shell 类工具名集合。
var shellLikeToolNames = map[string]struct{}{
	"bash": {}, "code": {}, "python": {}, "shell": {}, "shell_command": {},
}

// mutationPatterns 持久编辑命令模式（Python: _is_persistent_edit_step 的 mutation_patterns）。
var mutationPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|[;&|]\s*)apply_patch(?:\s|$)`),
	regexp.MustCompile(`(?i)(?:^|[;&|]\s*)git\s+apply(?:\s|$)`),
	regexp.MustCompile(`(?i)(?:^|[;&|]\s*)patch(?:\s|$)`),
	regexp.MustCompile(`(?i)(?:^|[;&|]\s*)(?:sed|perl)\s+[^\n;&|]*-(?:i|pi)\b`),
	regexp.MustCompile(`(?i)(?:^|[;&|]\s*)(?:cp|mv|rm|touch|truncate)\s+`),
	regexp.MustCompile(`(?i)(?:^|[;&|]\s*)tee\s+`),
	// 偏差 D-d6：Python 负向前瞻 (?![&]|/dev/...) Go RE2 不支持——
	// 改为捕获目标 + redirectMutation 代码过滤（语义等价，见该函数）
	regexp.MustCompile(`(?i)(?:^|\s)(?:>|>>)\s*(\S+)`),
	regexp.MustCompile(`(?i)\bopen\s*\([^\n)]*,\s*['"](?:w|a|x|[rwa]b|[rwa]\+)['"]`),
	regexp.MustCompile(`(?i)\.(?:write_text|write_bytes|touch|unlink|rename|replace)\s*\(`),
	regexp.MustCompile(`(?i)\bshutil\.(?:copy|copy2|copyfile|move|rmtree)\s*\(`),
	regexp.MustCompile(`(?i)\.(?:save|to_csv|to_excel|to_json|to_parquet)\s*\(`),
}

// isPersistentEditStep 是否持久编辑步（Python: _is_persistent_edit_step）。
func isPersistentEditStep(step map[string]any) bool {
	detail, ok := step["detail"].(map[string]any)
	if !ok {
		return false
	}
	toolName := canonicalToolName(trimSpaces(toSignalString(detail["tool_name"])))
	if _, ok := persistentEditToolNames[toolName]; ok {
		return true
	}
	if _, ok := shellLikeToolNames[toolName]; !ok {
		return false
	}
	command := commandFromArgs(detail)
	if command == "" {
		return false
	}
	for i, pattern := range mutationPatterns {
		if i == 6 { // 重定向模式（索引 6）：需代码过滤前瞻语义
			if redirectMutation(command) {
				return true
			}
			continue
		}
		if pattern.MatchString(command) {
			return true
		}
	}
	return false
}

// redirectMutation 重定向写判定（等价 Python 负向前瞻 (?![&]|/dev/(?:null|stdout|stderr))）。
func redirectMutation(command string) bool {
	match := mutationPatterns[6].FindStringSubmatch(command)
	if match == nil {
		return false
	}
	target := match[1]
	if strings.HasPrefix(target, "&") {
		return false
	}
	lower := strings.ToLower(target)
	if lower == "/dev/null" || lower == "/dev/stdout" || lower == "/dev/stderr" {
		return false
	}
	return true
}

// commandFromArgs 从调用参数提取命令（Python: _command_from_args）。
func commandFromArgs(detail map[string]any) string {
	for _, key := range []string{"call_args", "arguments", "tool_input", "input"} {
		payload := detail[key]
		if payloadStr, ok := payload.(string); ok {
			var decoded any
			if err := json.Unmarshal([]byte(payloadStr), &decoded); err == nil {
				if decodedMap, ok := decoded.(map[string]any); ok {
					payload = decodedMap
				} else if key == "tool_input" || key == "input" {
					return payloadStr
				}
			} else if key == "tool_input" || key == "input" {
				return payloadStr
			}
		}
		if payloadMap, ok := payload.(map[string]any); ok {
			command := payloadMap["command"]
			if command == nil {
				command = payloadMap["cmd"]
			}
			if command == nil {
				command = payloadMap["code"]
			}
			if commandStr, ok := command.(string); ok {
				return commandStr
			}
		}
	}
	return ""
}

// collectSkillNamesFromDetail 从 Tool 步详情收集 Skill 名（Python: _collect_skill_names_from_detail）。
func collectSkillNamesFromDetail(detail map[string]any, names map[string]struct{}) {
	toolName := trimSpaces(toSignalString(detail["tool_name"]))
	if canonicalToolName(toolName) != "skill" {
		return
	}
	for _, key := range []string{"call_args", "arguments", "tool_input", "input"} {
		addSkillNameFromArgs(detail[key], names)
	}
}

// addSkillNameFromArgs 从调用参数提取 skill_name（Python: _add_skill_name_from_args）。
func addSkillNameFromArgs(rawArgs any, names map[string]struct{}) {
	payload := rawArgs
	if payloadStr, ok := payload.(string); ok {
		var decoded any
		if err := json.Unmarshal([]byte(payloadStr), &decoded); err != nil {
			return
		}
		payload = decoded
	}
	payloadMap, ok := payload.(map[string]any)
	if !ok {
		return
	}
	skillName := trimSpaces(toSignalString(payloadMap["skill_name"]))
	if skillName != "" {
		names[skillName] = struct{}{}
	}
}

// canonicalToolName 规范工具名（Python: _canonical_tool_name——小写、中划线转下划线、去 _tool 后缀）。
func canonicalToolName(value string) string {
	value = strings.ReplaceAll(strings.ToLower(trimSpaces(value)), "-", "_")
	return strings.TrimSuffix(value, "_tool")
}

// readJSONLPayloads 读 JSONL 行为载荷（OSError 吞错语义一比一）。
func readJSONLPayloads(path string) []any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	payloads := []any{}
	for _, line := range strings.Split(string(data), "\n") {
		if trimSpaces(line) == "" {
			continue
		}
		var payload any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}
		payloads = append(payloads, payload)
	}
	return payloads
}

// sortStrings 字符串排序（避免各文件重复 import sort 的别名差异）。
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// trajectoryFileExists JSONL 文件存在性（glob 用）。
func trajectoryFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// trajectoryGlobJSONL 列目录下 *.jsonl（Python: trajectory_dir.glob("*.jsonl")）。
func trajectoryGlobJSONL(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".jsonl" {
			out = append(out, filepath.Join(dir, entry.Name()))
		}
	}
	return out
}
