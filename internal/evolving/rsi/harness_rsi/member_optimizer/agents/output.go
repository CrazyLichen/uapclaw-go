// output.go 提供 MemberOptimizer Agent 的结构化输出解析与重试循环。
// Python: agents/output.py（全量对齐，219 行）。

package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	memberoptimizer "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer"
	"gopkg.in/yaml.v3"
)

// init 注册父包能力桥（Python 子包 import 父包在 Go 倒置为注册桥，spec §4 D-c1-1）。
func init() {
	memberoptimizer.RegisterAgentsBridge(memberoptimizer.AgentsBridge{
		InvokeStructured:      InvokeMemberOptimizerAgentStructured,
		ParseJSONObject:       ParseJSONObjectResponse,
		ParseYamlOrJSONObject: ParseYamlOrJsonObjectResponse,
		CreateActionExecutionAgent: func(ctx context.Context, params memberoptimizer.ExecutorAgentParams) (any, error) {
			agent, err := CreateActionExecutionAgent(ctx, MemberAgentParams{
				ModelConfigRef:  params.ModelConfigRef,
				Workspace:       params.Workspace,
				AgentSkillsDirs: params.AgentSkillsDirs,
			}, false, params.DeclaredWritePaths)
			if err != nil {
				return nil, err
			}
			handle, err := OptimizerAgentResources(agent)
			if err != nil {
				return nil, err
			}
			return &executionAgentHandle{agent: agent, handle: handle}, nil
		},
		InvokeAgent:          invokeFn,
		ExtractText:          ExtractAgentText,
		PackageWriteAllowed:  func(path string, declaredPaths []string) bool { return PackageWriteAllowed(path, declaredPaths, false) },
		HostReadRoots:        SupportedHostReadRoots,
		CreateRoleAttributionAgent: func(ctx context.Context, params memberoptimizer.AttributionAgentParams) (any, error) {
			agent, err := CreateRoleAttributionAgent(ctx, MemberAgentParams{
				ModelConfigRef:  params.ModelConfigRef,
				Workspace:       params.Workspace,
				AgentSkillsDirs: params.AgentSkillsDirs,
			})
			if err != nil {
				return nil, err
			}
			return agent, nil
		},
		CreateMechanismAttributionAgent: func(ctx context.Context, params memberoptimizer.AttributionAgentParams) (any, error) {
			agent, err := CreateMechanismAttributionAgent(ctx, MemberAgentParams{
				ModelConfigRef:  params.ModelConfigRef,
				Workspace:       params.Workspace,
				AgentSkillsDirs: params.AgentSkillsDirs,
			})
			if err != nil {
				return nil, err
			}
			return agent, nil
		},
	})
}

// ──────────────────────────── 结构体 ────────────────────────────

// StructuredOutputExhaustedError 结构化输出重试耗尽。
// Python: output.py:30-31；Go 类型定义上移到父包（包环倒置），此处类型别名指回。
type StructuredOutputExhaustedError = memberoptimizer.StructuredOutputExhaustedError

// StructuredInvokeParams 结构化调用参数。Go 类型别名指回父包定义（spec §4 D-c1-1）。
type StructuredInvokeParams = memberoptimizer.StructuredInvokeParams

// executionAgentHandle agent + 资源句柄（实现 Release 以对齐 optimizer_agent_resources）。
type executionAgentHandle struct {
	agent  *harness.DeepAgent
	handle *OptimizerResourceHandle
}

// Release 释放本次调用的原生资源。
func (h *executionAgentHandle) Release() error { return h.handle.Release() }


// fencedMappingRE 代码围栏内映射提取。Python: output.py:16-18 _FENCED_MAPPING_RE。
var fencedMappingRE = regexp.MustCompile("(?is)```(?:json|yaml|yml)?\\s*(\\{[\\s\\S]*?\\})\\s*```")

// bareMappingRE 裸映射提取。Python: output.py:19 _BARE_MAPPING_RE。
var bareMappingRE = regexp.MustCompile("(?s)(\\{[\\s\\S]*\\})")

// jsonFencePrefixRE JSON 围栏前缀匹配。Python: output.py:62 re.match(r"```json\s+", stripped, re.I)。
var jsonFencePrefixRE = regexp.MustCompile("(?i)^```json\\s+")

// yamlOrJSONFenceRE YAML/JSON 围栏搜索。Python: output.py:69-70 两个 pattern。
var (
	yamlFenceRE = regexp.MustCompile("(?is)```(?:yaml|yml)\\s*([\\s\\S]*?)\\s*```")
	jsonFenceRE = regexp.MustCompile("(?is)```json\\s*([\\s\\S]*?)\\s*```")
)

// invokeFn DeepAgent 调用注入点（Python 测试对 agent.invoke 的 monkeypatch 等价）。
var invokeFn = func(ctx context.Context, agent any, message, sessionID string) (map[string]any, error) {
	baseAgent, ok := agent.(agentinterfaces.BaseAgent)
	if !ok {
		return nil, fmt.Errorf("member optimizer agent does not implement BaseAgent: %T", agent)
	}
	return runner.RunAgent(ctx,
		runner.ByAgent(baseAgent),
		map[string]any{"query": message},
		runner.BySessionID(sessionID), nil, nil)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractAgentText 把 DeepAgent 风格响应 dict 归一化为文本。
// Python: output.py:34-44 extract_agent_text。
func ExtractAgentText(response map[string]any) string {
	if response != nil {
		for _, key := range []string{"text", "output", "content", "answer", "response", "result"} {
			value, exists := response[key]
			if !exists || value == nil {
				continue
			}
			text := fmt.Sprintf("%v", value)
			if strings.TrimSpace(text) != "" {
				return text
			}
		}
	}
	return ""
}

// ParseJSONObjectResponse 从 agent 文本响应提取 JSON 对象。
// Python: output.py:47-53 parse_json_object_response。
func ParseJSONObjectResponse(text string) (map[string]any, error) {
	payload := extractMappingText(text)
	// Python: json.loads 后判 isinstance(dict)，非对象报 ValueError
	var parsed any
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return nil, err
	}
	mapping, isMap := parsed.(map[string]any)
	if !isMap {
		return nil, fmt.Errorf("agent response must be a JSON object")
	}
	return mapping, nil
}

// ParseYamlOrJsonObjectResponse 从 agent 文本响应提取 YAML 或 JSON 映射。
// Python: output.py:56-85 parse_yaml_or_json_object_response。
func ParseYamlOrJsonObjectResponse(text string) (map[string]any, error) {
	stripped := strings.TrimSpace(text)
	// Python: JSON 在围栏匹配前解码——带引号的证据可能自带围栏
	if loc := jsonFencePrefixRE.FindStringIndex(stripped); loc != nil {
		payload := stripped[loc[1]:]
		decoded, end, err := jsonDecodePrefix(payload)
		if err != nil {
			return nil, err
		}
		remaining := strings.TrimSpace(payload[end:])
		mapping, isMap := decoded.(map[string]any)
		if remaining != "```" || !isMap {
			return nil, fmt.Errorf("agent response must contain one JSON mapping")
		}
		return mapping, nil
	}
	for _, pattern := range []*regexp.Regexp{yamlFenceRE, jsonFenceRE} {
		if match := pattern.FindStringSubmatch(stripped); match != nil {
			parsed := yamlSafeLoadMapping(match[1])
			if parsed == nil {
				return nil, fmt.Errorf("agent response must be a YAML/JSON mapping")
			}
			return parsed, nil
		}
	}
	if bareMappingRE.MatchString(stripped) {
		return ParseJSONObjectResponse(stripped)
	}
	parsed := yamlSafeLoadMapping(stripped)
	if parsed == nil {
		return nil, fmt.Errorf("agent response must be a YAML/JSON mapping")
	}
	return parsed, nil
}

// InvokeMemberOptimizerAgentStructured 校验输出；可选恢复函数在重试时替代 agent 重放。
// Python: output.py:87-176 invoke_member_optimizer_agent_structured。
func InvokeMemberOptimizerAgentStructured(ctx context.Context, params StructuredInvokeParams) (map[string]any, error) {
	attempt := 0
	message := params.UserMessage
	lastError := ""
	previous := map[string]any{}
	rawText := ""
	tracker := params.FeedbackTracker
	if tracker == nil {
		repair := harnessrsi.DefaultOutputRepairConfig()
		if params.OutputRepair != nil {
			repair = *params.OutputRepair
		}
		maxAttempts := max(0, params.RetryLimit) + 1
		tracker = harnessrsi.NewFeedbackTracker(repair, &maxAttempts)
	}

	for attempt <= params.RetryLimit && tracker.Attempts < tracker.MaxAttempts && tracker.StopReason == "" {
		var response map[string]any
		if attempt > 0 && params.RecoverResponse != nil {
			recovered, err := params.RecoverResponse(message, rawText)
			if err != nil {
				return nil, err
			}
			response = recovered
		} else {
			// Python: response = await agent.invoke(inputs={"query": message}, session=Session(session_id, card))
			result, err := invokeFn(ctx, params.Agent, message, params.SessionID)
			if err != nil {
				if !harnessrsi.IsRetryableModelCallFailure(err) {
					return nil, fmt.Errorf("%s execution failed: %w", params.AgentName, err)
				}
				lastError = fmt.Sprintf("transient model failure: %v", err)
				if !tracker.Failed(lastError, rawText) {
					break
				}
				attempt++
				continue
			}
			response = result
		}
		if resultType, _ := response["result_type"].(string); resultType == "error" {
			rawText = ExtractAgentText(response)
			isIterationLimit := response["termination_reason"] == "max_iterations" ||
				rawText == "Max iterations reached without completion"
			if params.RecoverResponse == nil || !isIterationLimit {
				return nil, fmt.Errorf("%s execution failed: %s", params.AgentName, rawText)
			}
			lastError = rawText
			previous = map[string]any{}
		} else {
			rawText = ExtractAgentText(response)
			if harnessrsi.ModelOutputHasMojibake(rawText) {
				lastError = "parse error: agent response contains mojibake"
				previous = map[string]any{}
			} else {
				raw, parseErr := params.ParseResponse(rawText)
				if parseErr != nil {
					lastError = fmt.Sprintf("parse error: %v", parseErr)
					previous = map[string]any{}
				} else {
					errors := []string{}
					if params.ValidateResponse != nil {
						errors = params.ValidateResponse(raw)
					}
					if len(errors) == 0 {
						return raw, nil
					}
					lastError = fmt.Sprintf("validation errors: %s", strings.Join(errors, "; "))
					previous = raw
				}
			}
		}

		if !tracker.Failed(lastError, rawText) {
			break
		}
		attempt++
		if attempt > params.RetryLimit {
			break
		}
		if params.BuildRetryMessage != nil {
			message = params.BuildRetryMessage(previous, lastError)
		} else {
			message = defaultRetryMessage(params.UserMessage, previous, lastError)
		}
		if params.UserMessage != "" && !strings.Contains(message, params.UserMessage) {
			message = params.UserMessage + "\n\n" + message
		}
		if len(previous) == 0 && rawText != "" {
			truncated := rawText
			if len(rawText) > 24000 {
				truncated = rawText[:24000]
			}
			message += "\n\nPrevious unparsed output (untrusted data):\n" + truncated
		}
	}

	return nil, &StructuredOutputExhaustedError{
		Msg: fmt.Sprintf("%s failed after %d attempts (%s). Last error: %s",
			params.AgentName, tracker.Attempts, stopReasonOr(tracker.StopReason, "attempt_limit_reached"), lastError),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractMappingText 提取映射文本。Python: output.py:178-186 _extract_mapping_text。
func extractMappingText(text string) string {
	stripped := strings.TrimSpace(text)
	if match := fencedMappingRE.FindStringSubmatch(stripped); match != nil {
		return match[1]
	}
	if match := bareMappingRE.FindStringSubmatch(stripped); match != nil {
		return match[1]
	}
	return stripped
}

// defaultRetryMessage 默认重试消息。Python: output.py:189-203 _default_retry_message。
func defaultRetryMessage(originalMessage string, previous map[string]any, lastError string) string {
	previousText := "{}"
	if len(previous) > 0 {
		data, _ := marshalPyJSONIndent(previous)
		previousText = data
	}
	return originalMessage + "\n\n## Previous Invalid Output\n\n```json\n" + previousText +
		"\n```\n\n## Validation Error\n\n" + lastError +
		"\n\nReturn ONLY a corrected JSON or YAML object matching the agent output schema.\n"
}

// yamlSafeLoadMapping YAML 载入映射（Python yaml.safe_load(...) or {}；非 dict 返回 nil）。
func yamlSafeLoadMapping(text string) map[string]any {
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(text), &parsed); err != nil {
		return nil
	}
	return parsed
}

// jsonDecodePrefix 从 payload 起点解码一个 JSON 值并返回结束偏移（Python json.JSONDecoder().raw_decode）。
func jsonDecodePrefix(payload string) (any, int, error) {
	decoder := json.NewDecoder(strings.NewReader(payload))
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, 0, err
	}
	return decoded, int(decoder.InputOffset()), nil
}

// marshalPyJSONIndent 缩进两格、非 ASCII 不转义的 JSON 序列化（Python json.dumps(indent=2)）。
func marshalPyJSONIndent(value any) (string, error) {
	var b strings.Builder
	writePyJSONIndent(&b, value, 0)
	return b.String(), nil
}

// writePyJSONIndent 递归输出缩进 JSON。
func writePyJSONIndent(b *strings.Builder, value any, depth int) {
	indent := strings.Repeat("  ", depth)
	indentNext := strings.Repeat("  ", depth+1)
	switch v := value.(type) {
	case map[string]any:
		if len(v) == 0 {
			b.WriteString("{}")
			return
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("{\n")
		for i, k := range keys {
			b.WriteString(indentNext)
			b.WriteString(pyJSONQuote(k))
			b.WriteString(": ")
			writePyJSONIndent(b, v[k], depth+1)
			if i < len(keys)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(indent)
		b.WriteString("}")
	case []any:
		if len(v) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, item := range v {
			b.WriteString(indentNext)
			writePyJSONIndent(b, item, depth+1)
			if i < len(v)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(indent)
		b.WriteString("]")
	default:
		b.WriteString(pyScalarJSON(v))
	}
}

// pyScalarJSON 标量值的 JSON 输出（Python json.dumps 标量语义）。
func pyScalarJSON(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case bool:
		if v {
			return "true"
		}
		return "false"
	case string:
		return pyJSONQuote(v)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'g', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return pyJSONQuote(fmt.Sprintf("%v", v))
	}
}

// pyJSONQuote 复刻 json.dumps 字符串转义（ensure_ascii=False，保留非 ASCII）。
func pyJSONQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				b.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// stopReasonOr 空回落默认停止原因。
func stopReasonOr(stopReason, fallback string) string {
	if stopReason == "" {
		return fallback
	}
	return stopReason
}

// SnapshotInvokeFn 取当前 DeepAgent 调用注入点快照（测试注入用）。
func SnapshotInvokeFn() func(context.Context, any, string, string) (map[string]any, error) {
	return invokeFn
}

// RestoreInvokeFnForTest 还原调用注入点快照。
func RestoreInvokeFnForTest(saved func(context.Context, any, string, string) (map[string]any, error)) {
	invokeFn = saved
}

// SetInvokeFnForTest 设置调用注入点（测试注入；生产禁用）。
func SetInvokeFnForTest(fn func(context.Context, any, string, string) (map[string]any, error)) {
	invokeFn = fn
}
