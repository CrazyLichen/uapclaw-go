package evaluator

import (
	"context"
	"fmt"
	"math"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/output_parsers"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/evaluator/metrics"
	"golang.org/x/sync/errgroup"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseEvaluator 抽象评估器接口。
//
// 对应 Python: openjiuwen/agent_evolving/evaluator/evaluator.py BaseEvaluator
type BaseEvaluator interface {
	// Evaluate 评估单个样本
	Evaluate(ctx context.Context, case_ dataset.Case, predict map[string]any) (*dataset.EvaluatedCase, error)
	// BatchEvaluate 并行批量评估
	BatchEvaluate(ctx context.Context, cases []dataset.Case, predicts []map[string]any, numParallel int) ([]*dataset.EvaluatedCase, error)
}

// DefaultEvaluator 使用 LLM-as-Judge 评估模型输出一致性。
//
// 判定模型输出与期望答案的语义一致性，映射为 0/1 分数。
// 解析失败时使用重试模板再调一次 LLM。
//
// 对应 Python: openjiuwen/agent_evolving/evaluator/evaluator.py DefaultEvaluator
type DefaultEvaluator struct {
	// model LLM 模型实例
	model *llm.Model
	// metricTemplate 已填充 user_metrics 的评估模板
	metricTemplate *prompt.PromptTemplate
	// parser JSON 输出解析器
	parser *output_parsers.JsonOutputParser
}

// MetricEvaluator 多指标聚合评估器。
//
// 遍历所有 Metric 计算 score，支持 per-metric 分解和可配置聚合方式。
//
// 对应 Python: openjiuwen/agent_evolving/evaluator/evaluator.py MetricEvaluator
type MetricEvaluator struct {
	// metrics 指标列表
	metrics []metrics.Metric
	// aggregate 聚合方式，"mean" 或 "first"
	aggregate string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDefaultEvaluator 创建 DefaultEvaluator 实例。
//
// 传入 ModelClientConfig + ModelRequestConfig，内部创建 llm.Model。
// metric 为自定义验证规则字符串，会注入到评估模板中。
//
// 对应 Python: DefaultEvaluator(model_config, model_client_config, metric)
func NewDefaultEvaluator(
	clientConfig llmschema.ModelClientConfig,
	requestConfig llmschema.ModelRequestConfig,
	metric string,
) (*DefaultEvaluator, error) {
	model, err := llm.NewModel(&clientConfig, &requestConfig)
	if err != nil {
		return nil, fmt.Errorf("创建 LLM 模型失败: %w", err)
	}

	// 填充 user_metrics 到模板
	tmpl, err := metrics.LLMMetricTemplate.Format(map[string]any{"user_metrics": metric})
	if err != nil {
		return nil, fmt.Errorf("格式化评估模板失败: %w", err)
	}
	if tmpl == nil {
		tmpl = metrics.LLMMetricTemplate
	}

	return &DefaultEvaluator{
		model:          model,
		metricTemplate: tmpl,
		parser:         output_parsers.NewJsonOutputParser(),
	}, nil
}

// NewMetricEvaluator 创建 MetricEvaluator 实例。
//
// metrics 为指标列表，aggregate 为聚合方式（"mean" 或 "first"）。
//
// 对应 Python: MetricEvaluator(metrics, aggregate)
func NewMetricEvaluator(ms []metrics.Metric, aggregate string) *MetricEvaluator {
	if aggregate == "" {
		aggregate = "mean"
	}
	return &MetricEvaluator{metrics: ms, aggregate: aggregate}
}

// Evaluate 使用 LLM-as-Judge 评估单个样本。
//
// 对应 Python: DefaultEvaluator.evaluate(case, predict)
func (d *DefaultEvaluator) Evaluate(ctx context.Context, case_ dataset.Case, predict map[string]any) (*dataset.EvaluatedCase, error) {
	ec := dataset.NewEvaluatedCase(case_, predict)

	// 格式化模板
	formatted, err := d.metricTemplate.Format(map[string]any{
		"question":        fmt.Sprintf("%v", case_.Inputs),
		"expected_answer": fmt.Sprintf("%v", case_.Label),
		"model_answer":    fmt.Sprintf("%v", predict),
	})
	if err != nil {
		ec.Reason = "格式化评估模板失败"
		return ec, nil
	}

	// 转为消息列表
	msgs, err := formatted.ToMessages()
	if err != nil {
		ec.Reason = "将模板转换为消息列表失败"
		return ec, nil
	}

	// 调用 LLM
	response, err := d.model.Invoke(ctx, model_clients.NewMessagesParam(msgs...))
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "DefaultEvaluator.Evaluate").
			Err(err).
			Msg("LLM 调用失败")
		ec.Reason = "因模型错误导致评估样本失败"
		return ec, nil
	}

	// 提取响应文本
	responseText := response.Content.Text()

	// 解析评估结果
	evaluatedResult := d.extractEvaluateResult(ctx, responseText, case_, predict)
	if evaluatedResult == nil {
		ec.Reason = "因解析错误导致评估样本失败"
		return ec, nil
	}

	result := evaluatedResult["result"]
	reason, _ := evaluatedResult["reason"].(string)

	ec.SetScore(mapBoolToScore(result))
	ec.Reason = reason
	return ec, nil
}

// BatchEvaluate 并行批量评估。
//
// 使用 errgroup.Group + SetLimit(numParallel) 控制并发。
//
// 对应 Python: BaseEvaluator.batch_evaluate(cases, predicts, num_parallel)
func (d *DefaultEvaluator) BatchEvaluate(ctx context.Context, cases []dataset.Case, predicts []map[string]any, numParallel int) ([]*dataset.EvaluatedCase, error) {
	if err := validateBatchArgs(len(cases), len(predicts), numParallel); err != nil {
		return nil, err
	}

	results := make([]*dataset.EvaluatedCase, len(cases))
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(numParallel)

	for i := range cases {
		i := i
		g.Go(func() error {
			ec, err := d.Evaluate(gCtx, cases[i], predicts[i])
			if err != nil {
				return err
			}
			results[i] = ec
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

// Evaluate 使用多指标聚合评估单个样本。
//
// 对应 Python: MetricEvaluator.evaluate(case, predict)
func (m *MetricEvaluator) Evaluate(_ context.Context, case_ dataset.Case, predict map[string]any) (*dataset.EvaluatedCase, error) {
	ec := dataset.NewEvaluatedCase(case_, predict)
	perMetric := make(map[string]float64)
	var scores []float64

	for _, metric := range m.metrics {
		result, err := metric.Compute(predict, case_.Label, metrics.WithQuestion(case_.Inputs), metrics.WithCase(case_))
		if err != nil {
			logger.Warn(logComponent).
				Str("metric_name", metric.Name()).
				Err(err).
				Msg("指标计算失败")
			continue
		}
		for k, v := range result {
			vf := safeConvert(v)
			perMetric[k] = vf
			scores = append(scores, vf)
		}
	}

	ec.SetScore(aggScore(scores, m.aggregate))
	if len(perMetric) > 0 {
		ec.PerMetric = perMetric
	}
	return ec, nil
}

// BatchEvaluate 并行批量评估。
func (m *MetricEvaluator) BatchEvaluate(ctx context.Context, cases []dataset.Case, predicts []map[string]any, numParallel int) ([]*dataset.EvaluatedCase, error) {
	if err := validateBatchArgs(len(cases), len(predicts), numParallel); err != nil {
		return nil, err
	}

	results := make([]*dataset.EvaluatedCase, len(cases))
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(numParallel)

	for i := range cases {
		i := i
		g.Go(func() error {
			ec, err := m.Evaluate(gCtx, cases[i], predicts[i])
			if err != nil {
				return err
			}
			results[i] = ec
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractEvaluateResult 解析 LLM 评估响应，解析失败时用重试模板再调一次 LLM。
//
// 对应 Python: DefaultEvaluator._extract_evaluate_result(response, case, predict)
func (d *DefaultEvaluator) extractEvaluateResult(ctx context.Context, response string, case_ dataset.Case, predict map[string]any) map[string]any {
	// 第一次解析
	parsed, err := d.parser.Parse(response)
	if err == nil {
		if data, ok := parsed.(map[string]any); ok {
			if _, hasResult := data["result"]; hasResult {
				if _, hasReason := data["reason"]; hasReason {
					return data
				}
			}
		}
	}

	// 解析失败，使用重试模板
	logger.Warn(logComponent).
		Str("event_type", "LLM_CALL_ERROR").
		Str("method", "DefaultEvaluator.extractEvaluateResult").
		Msg("首次解析评估结果失败，尝试重试模板")

	retryFormatted, err := metrics.LLMMetricRetryTemplate.Format(map[string]any{
		"question":                     fmt.Sprintf("%v", case_.Inputs),
		"expected_answer":              fmt.Sprintf("%v", case_.Label),
		"model_answer":                 fmt.Sprintf("%v", predict),
		"nonstandard_evaluated_result": response,
	})
	if err != nil {
		return nil
	}

	retryMessages, err := retryFormatted.ToMessages()
	if err != nil {
		return nil
	}

	retryResponse, err := d.model.Invoke(ctx, model_clients.NewMessagesParam(retryMessages...))
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "DefaultEvaluator.extractEvaluateResult").
			Err(err).
			Msg("重试 LLM 调用失败")
		return nil
	}

	retryText := retryResponse.Content.Text()
	retryParsed, err := d.parser.Parse(retryText)
	if err != nil {
		return nil
	}
	if data, ok := retryParsed.(map[string]any); ok {
		return data
	}
	return nil
}

// mapBoolToScore 将 result 字段映射为 0.0 或 1.0。
//
// 对应 Python: DefaultEvaluator._is_pass_result(result)
func mapBoolToScore(result any) float64 {
	if metrics.IsPassResult(result) {
		return 1.0
	}
	return 0.0
}

// aggScore 聚合分数。
//
// 对应 Python: _agg_score(results, aggregate)
func aggScore(results []float64, aggregate string) float64 {
	if len(results) == 0 {
		return 0.0
	}
	switch aggregate {
	case "first":
		return results[0]
	case "mean":
		fallthrough
	default:
		sum := 0.0
		for _, v := range results {
			sum += v
		}
		return sum / float64(len(results))
	}
}

// safeConvert 安全转换 metric 值为 float64。
//
// 对应 Python: MetricEvaluator._safe_convert(num)
func safeConvert(num float64) float64 {
	if math.IsNaN(num) || math.IsInf(num, 0) {
		logger.Warn(logComponent).
			Float64("value", num).
			Msg("metric 值为 NaN 或 Inf，已转换为 0.0")
		return 0.0
	}
	return num
}

// validateBatchArgs 校验批量评估参数。
func validateBatchArgs(casesLen, predictsLen, numParallel int) error {
	if casesLen != predictsLen {
		return exception.NewBaseError(
			exception.StatusToolchainEvaluatorExecutionError,
			exception.WithMsg(fmt.Sprintf(
				"样本数量 %d 与预测数量 %d 不一致",
				casesLen, predictsLen,
			)),
		)
	}
	return nil
}
