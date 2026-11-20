// budget_model.go 提供 RSI 请求策略模型包装（不在本地施加模型容量限制）。
// Python: openjiuwen/rsi/harness_rsi/member_optimizer/budget_model.py（全量对齐，24 行）。
// 容量校验交给 provider，保留输入证据；显式配置的输出限额不被抹除。

package memberoptimizer

import (
	"context"

	llm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BudgetedRsiModel 预算化 RSI 模型包装。
// Python: budget_model.py BudgetedRsiModel(Model)——构造期把 model_config 经
// with_rsi_output_budget 预算化后交给父类；调用期经 _budget_options 把
// rsi_output_options(options) 套在每次 invoke/stream 的参数上。
//
// 已知偏差（spec §4 D-1）：Python 的 model_config 为可携带显式 None 键的 dict，
// 构造期/调用期都需 map 剔除；Go 的 ModelRequestConfig 用指针承载可选性
// （MaxTokens nil 即"未设置，交由 provider"），非 nil 即"显式配置，保留"——
// Python 预算化后的目标状态在 Go 类型层天然成立，故构造期转调父类、
// 调用期仅剔除非正 MaxTokens 选项。
type BudgetedRsiModel struct {
	// Model 被包装的底层模型
	*llm.Model
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBudgetedRsiModel 构造预算化模型包装。
// Python: budget_model.py __init__——request = with_rsi_output_budget(
// {"model_request_config": model_config.model_dump()})["model_request_config"]；
// Go 侧指针可选性已承载同一语义（见类型注释偏差 D-1），直接转调父类构造。
func NewBudgetedRsiModel(clientConfig *llmschema.ModelClientConfig, requestConfig *llmschema.ModelRequestConfig) (*BudgetedRsiModel, error) {
	model, err := llm.NewModel(clientConfig, requestConfig)
	if err != nil {
		return nil, err
	}
	return &BudgetedRsiModel{Model: model}, nil
}

// Invoke 非流式调用（预算化参数后转调父类）。
// Python: budget_model.py invoke——await super().invoke(messages=messages, **self._budget_options(...))。
func (b *BudgetedRsiModel) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return b.Model.Invoke(ctx, messages, BudgetOptions(opts...)...)
}

// Stream 流式调用（预算化参数后转调父类）。
// Python: budget_model.py stream——async for chunk in super().stream(messages=messages, **self._budget_options(...))。
func (b *BudgetedRsiModel) Stream(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return b.Model.Stream(ctx, messages, BudgetStreamOptions(opts...)...)
}

// BudgetStreamOptions 流式版预算（与 BudgetOptions 同语义，作用于 StreamOption）。
// Python _budget_options 对 invoke/stream 共用一份 rsi_output_options。
func BudgetStreamOptions(opts ...model_clients.StreamOption) []model_clients.StreamOption {
	out := make([]model_clients.StreamOption, 0, len(opts))
	for _, opt := range opts {
		params := model_clients.NewStreamParams(opt)
		if params.MaxTokens != nil && *params.MaxTokens <= 0 {
			continue
		}
		out = append(out, opt)
	}
	return out
}

// BudgetOptions 套用 RSI 输出预算（Python _budget_options → rsi_output_options）。
// 保留显式 MaxTokens（Python 语义：已配置的 max_tokens 不被抹除），
// 剔除非正值（Python 中 None 键被线上省略；Go 侧 0/负值即退化等价物）。
func BudgetOptions(opts ...model_clients.InvokeOption) []model_clients.InvokeOption {
	out := make([]model_clients.InvokeOption, 0, len(opts))
	for _, opt := range opts {
		params := model_clients.NewInvokeParams(opt)
		if params.MaxTokens != nil && *params.MaxTokens <= 0 {
			continue
		}
		out = append(out, opt)
	}
	return out
}
