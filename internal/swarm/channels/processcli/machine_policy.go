package processcli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RunLimits 从 Runtime 模型用量事件计量一次运行，不共享状态。
// Python: machine_policy.py:55-91 RunLimits
type RunLimits struct {
	// MaxTurns 最大模型调用次数（nil = 不限）
	MaxTurns *int
	// MaxBudgetUSD 最大费用（nil = 不限）
	MaxBudgetUSD *float64
	// modelCalls 已发生的模型调用次数
	modelCalls int
	// totalCost 当前累计费用
	totalCost float64
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// CodeBudgetMeterUnavailable 用量缺失导致无法计量预算。
	// Python: machine_policy.py:72,77,79 "BUDGET_METER_UNAVAILABLE"
	CodeBudgetMeterUnavailable = "BUDGET_METER_UNAVAILABLE"
	// CodeBudgetExceeded 超出预算。
	// Python: machine_policy.py:82 "BUDGET_EXCEEDED"
	CodeBudgetExceeded = "BUDGET_EXCEEDED"
	// CodeTurnLimitExceeded 超出轮次上限。
	// Python: machine_policy.py:84 "TURN_LIMIT_EXCEEDED"
	CodeTurnLimitExceeded = "TURN_LIMIT_EXCEEDED"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateOutputSchema 在启动 Agent 或调用模型计费之前拒绝畸形 schema。
// Python: machine_policy.py:19-24 validate_output_schema
func ValidateOutputSchema(schema map[string]any) error {
	if _, err := compileSchema(schema); err != nil {
		return fmt.Errorf("output_schema is not a valid JSON Schema")
	}
	return nil
}

// StructuredPrompt 要求模型返回 JSON 对象，同时保留调用方的原始请求。
// Python: machine_policy.py:27-35 structured_prompt
//
// 提示词逐字复刻上游英文原文，不翻译、不改空格。
func StructuredPrompt(inputText string, schema map[string]any) string {
	// Python: :29 contract = json.dumps(thaw(schema), ensure_ascii=False, separators=(",", ":"))
	contract := compactJSON(schema)
	return inputText + "\n\n" +
		"Return exactly one JSON object conforming to this JSON Schema. " +
		"Do not use Markdown fences or text outside the JSON object.\n" +
		"JSON Schema: " + contract
}

// ParseStructuredOutput 只有完全合法的 JSON 对象才能让结构化运行成功。
// Python: machine_policy.py:38-52 parse_structured_output
func ParseStructuredOutput(output *string, schema map[string]any) (map[string]any, error) {
	// Python: :42-45 value = json.loads(output or "")
	raw := ""
	if output != nil {
		raw = *output
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, fmt.Errorf("Agent output is not a JSON object")
	}
	// Python: :46-47 if not isinstance(value, dict): raise
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Agent output is not a JSON object")
	}
	// Python: :48-52 Draft202012Validator(schema).validate(value)
	validator, err := compileSchema(schema)
	if err != nil {
		return nil, fmt.Errorf("Agent output is not a JSON object")
	}
	if validateErr := validator.Validate(value); validateErr != nil {
		return nil, fmt.Errorf("Agent output does not match output_schema")
	}
	return obj, nil
}

// NewRunLimits 构造限额计量器。
// Python: machine_policy.py:58-62 RunLimits.__init__
func NewRunLimits(maxTurns *int, maxBudgetUSD *float64) *RunLimits {
	return &RunLimits{MaxTurns: maxTurns, MaxBudgetUSD: maxBudgetUSD}
}

// ModelCalls 已发生的模型调用次数。
func (l *RunLimits) ModelCalls() int { return l.modelCalls }

// TotalCost 当前累计费用。
func (l *RunLimits) TotalCost() float64 { return l.totalCost }

// Observe 在配置的限额被超出时返回稳定的失败码（nil 表示未超限）。
// Python: machine_policy.py:64-85 RunLimits.observe
func (l *RunLimits) Observe(event *swruntime.RuntimeEvent) *string {
	// Python: :66-67 if event.event_type != "chat.usage_metadata": return None
	if event == nil || event.EventType() != "chat.usage_metadata" {
		return nil
	}
	// Python: :68-70 metadata = payload.get("metadata"); usage = metadata.get("usage_metadata")
	payload := event.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	metadata, _ := payload["metadata"].(map[string]any)
	var usage map[string]any
	if metadata != nil {
		usage, _ = metadata["usage_metadata"].(map[string]any)
	}
	// Python: :71-72 if not isinstance(usage, Mapping):
	//               return UNAVAILABLE if max_budget is not None else None
	if usage == nil {
		if l.MaxBudgetUSD != nil {
			return codePtr(CodeBudgetMeterUnavailable)
		}
		return nil
	}
	// Python: :73 self.model_calls += 1
	l.modelCalls++
	// Python: :74-82 预算计量
	if l.MaxBudgetUSD != nil {
		raw := usage["total_cost"]
		// Python: :76-79 bool 不算数字；必须有限且非负
		cost, ok := finiteNonNegative(raw)
		if !ok {
			return codePtr(CodeBudgetMeterUnavailable)
		}
		l.totalCost += cost
		// Python: :81-82 if self.total_cost > self.max_budget_usd: return BUDGET_EXCEEDED
		if l.totalCost > *l.MaxBudgetUSD {
			return codePtr(CodeBudgetExceeded)
		}
	}
	// Python: :83-84 if max_turns is not None and model_calls > max_turns
	if l.MaxTurns != nil && l.modelCalls > *l.MaxTurns {
		return codePtr(CodeTurnLimitExceeded)
	}
	return nil
}

// AddUsage 在用量快照上附加 model_calls（仅在设了限额时）。
// Python: machine_policy.py:87-91 RunLimits.add_usage
func (l *RunLimits) AddUsage(usage map[string]any) map[string]any {
	// Python: :88 result = deepcopy(dict(usage))
	result := make(map[string]any, len(usage)+1)
	for k, v := range usage {
		result[k] = deepCopyValue(v)
	}
	// Python: :89-90 if max_turns is not None or max_budget_usd is not None:
	//                   result["model_calls"] = self.model_calls
	if l.MaxTurns != nil || l.MaxBudgetUSD != nil {
		result["model_calls"] = l.modelCalls
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// compactJSON 紧凑序列化（无空格、不转义 HTML）。
// Python: json.dumps(..., ensure_ascii=False, separators=(",", ":"))
//
// Go 的 map 序列化按 key 字典序，与 Python 的插入序不同；
// 但 schema 契约只要求是合法紧凑 JSON，不要求键序一致（上游也未承诺）。
func compactJSON(value map[string]any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return "{}"
	}
	// Encode 会追加换行，去掉
	return strings.TrimSuffix(buf.String(), "\n")
}

// compileSchema 编译 Draft 2020-12 校验器。
// Python: machine_policy.py:13,22,49 Draft202012Validator
func compileSchema(schema map[string]any) (*jsonschema.Schema, error) {
	raw := compactJSON(schema)
	decoded, err := jsonschema.UnmarshalJSON(strings.NewReader(raw))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	// Python: :22 check_schema 只校验 schema 本身是否合法
	url := "mem://processcli/output_schema.json"
	if err := compiler.AddResource(url, decoded); err != nil {
		return nil, err
	}
	return compiler.Compile(url)
}

// finiteNonNegative 取有限非负数值（bool 不算数字）。
// Python: machine_policy.py:76-79
func finiteNonNegative(value any) (float64, bool) {
	switch v := value.(type) {
	case bool:
		return 0, false
	case int:
		if v < 0 {
			return 0, false
		}
		return float64(v), true
	case int64:
		if v < 0 {
			return 0, false
		}
		return float64(v), true
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return 0, false
		}
		return v, true
	default:
		return 0, false
	}
}

// codePtr 字符串取地址。
func codePtr(code string) *string { return &code }

// deepCopyValue 递归拷贝 JSON 值（对齐 deepcopy 对 JSON 结构的语义）。
func deepCopyValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = deepCopyValue(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = deepCopyValue(item)
		}
		return out
	default:
		return value
	}
}
