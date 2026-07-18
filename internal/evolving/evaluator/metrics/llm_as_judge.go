package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/output_parsers"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LLMAsJudgeMetric 使用 LLM 作为裁判判断语义一致性的指标。
//
// 通过模板生成评估 prompt，调用 LLM 判断 prediction 与 label 的语义一致性，
// 返回 {"llm_as_judge": 1.0}（通过）或 {"llm_as_judge": 0.0}（失败）。
//
// 对应 Python: openjiuwen/agent_evolving/evaluator/metrics/llm_as_judge.py LLMAsJudgeMetric
type LLMAsJudgeMetric struct {
	// model LLM 模型实例
	model *llm.Model
	// template 已填充 user_metrics 的评估模板
	template *prompt.PromptTemplate
	// parser JSON 输出解析器
	parser *output_parsers.JsonOutputParser
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	metricLogComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLLMAsJudgeMetric 创建 LLMAsJudgeMetric 实例。
//
// 传入 ModelClientConfig + ModelRequestConfig，内部创建 llm.Model。
// userMetrics 为自定义验证规则，会注入到评估模板中。
//
// 对应 Python: LLMAsJudgeMetric(model_config, model_client_config, user_metrics)
func NewLLMAsJudgeMetric(
	clientConfig llmschema.ModelClientConfig,
	requestConfig llmschema.ModelRequestConfig,
	userMetrics string,
) (*LLMAsJudgeMetric, error) {
	model, err := llm.NewModel(&clientConfig, &requestConfig)
	if err != nil {
		return nil, fmt.Errorf("创建 LLM 模型失败: %w", err)
	}

	// 填充 user_metrics 到模板
	tmpl, err := LLMMetricTemplate.Format(map[string]any{"user_metrics": userMetrics})
	if err != nil {
		return nil, fmt.Errorf("格式化评估模板失败: %w", err)
	}
	if tmpl == nil {
		tmpl = LLMMetricTemplate
	}

	return &LLMAsJudgeMetric{
		model:    model,
		template: tmpl,
		parser:   output_parsers.NewJsonOutputParser(),
	}, nil
}

// Name 返回指标名称 "llm_as_judge"。
func (m *LLMAsJudgeMetric) Name() string { return "llm_as_judge" }

// HigherIsBetter 返回 true。
func (m *LLMAsJudgeMetric) HigherIsBetter() bool { return true }

// Compute 使用 LLM-as-Judge 计算语义一致性分数。
//
// 对应 Python: LLMAsJudgeMetric.compute(prediction, label, question=None)
func (m *LLMAsJudgeMetric) Compute(ctx context.Context, prediction, label any, opts ...MetricOption) (MetricResult, error) {
	mc := applyMetricOptions(opts...)

	// 格式化模板
	formatted, err := m.template.Format(map[string]any{
		"question":        formatValue(mc.question),
		"expected_answer": formatValue(label),
		"model_answer":    formatValue(prediction),
	})
	if err != nil {
		logger.Warn(metricLogComponent).Err(err).Msg("格式化 LLM 评估模板失败")
		return MetricResult{"llm_as_judge": 0.0}, nil
	}

	// 转为消息列表
	messages, err := formatted.ToMessages()
	if err != nil {
		logger.Warn(metricLogComponent).Err(err).Msg("转换评估模板为消息列表失败")
		return MetricResult{"llm_as_judge": 0.0}, nil
	}

	// 调用 LLM
	response, err := m.model.Invoke(ctx, model_clients.NewMessagesParam(messages...))
	if err != nil {
		logger.Warn(metricLogComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "LLMAsJudgeMetric.Compute").
			Err(err).
			Msg("LLM 调用失败")
		return MetricResult{"llm_as_judge": 0.0}, nil
	}

	// 解析响应
	return m.parseResult(response), nil
}

// ComputeBatch 批量计算语义一致性分数。
func (m *LLMAsJudgeMetric) ComputeBatch(ctx context.Context, predictions, labels []any, opts ...MetricOption) ([]MetricResult, error) {
	return DefaultComputeBatch(m, ctx, predictions, labels, opts...)
}

// IsPassResult 判断评估结果是否通过。
//
// 对应 Python: DefaultEvaluator._is_pass_result(result)
func IsPassResult(result any) bool {
	if result == true {
		return true
	}
	if s, ok := result.(string); ok {
		return strings.EqualFold(strings.TrimSpace(s), "true")
	}
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseResult 解析 LLM 评估结果，返回 1.0（通过）或 0.0（失败）。
//
// 对应 Python: LLMAsJudgeMetric._parse_result(response)
func (m *LLMAsJudgeMetric) parseResult(response *llmschema.AssistantMessage) MetricResult {
	text := response.Content.Text()

	parsed, err := m.parser.Parse(text)
	if err != nil {
		logger.Warn(metricLogComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "LLMAsJudgeMetric.parseResult").
			Err(err).
			Msg("解析 LLM 评估结果失败")
		return MetricResult{"llm_as_judge": 0.0}
	}

	data, ok := parsed.(map[string]any)
	if !ok {
		return MetricResult{"llm_as_judge": 0.0}
	}

	result, exists := data["result"]
	if !exists {
		return MetricResult{"llm_as_judge": 0.0}
	}

	// 判断 result：true 或 "true" → 1.0，其余 → 0.0
	if IsPassResult(result) {
		return MetricResult{"llm_as_judge": 1.0}
	}
	return MetricResult{"llm_as_judge": 0.0}
}

// formatValue 将任意值序列化为字符串，用于 LLM 模板填充。
// nil 或零值 → 空字符串，非空 → JSON 序列化。
// 对齐 Python 的 str(value or "") 语义。
func formatValue(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		if val == "" {
			return ""
		}
		return val
	case map[string]any:
		if len(val) == 0 {
			return ""
		}
	case []any:
		if len(val) == 0 {
			return ""
		}
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}
