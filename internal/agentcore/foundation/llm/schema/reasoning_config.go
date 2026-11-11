package schema

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// logComponent schema 包日志组件标识（对齐项目组件选择规则）。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// ReasoningConfig 厂商中立的思考/推理请求意图。
// Python: schema/config.py:59-66 ReasoningConfig（model_config extra="forbid"，不接受额外键）
type ReasoningConfig struct {
	// Mode 思考模式，缺省 auto；取值 auto/enabled/disabled
	Mode string `json:"mode"`
	// Effort 强度档位（厂商自定义取值，如 low/medium/high/xhigh/max）
	Effort *string `json:"effort,omitempty"`
	// BudgetTokens 思考预算（token），nil 表示未设置
	BudgetTokens *int `json:"budget_tokens,omitempty"`
}

// ReasoningIntent 区分「未显式传 reasoning」与「显式传了 reasoning（值可为 nil）」。
// Python: reasoning.py:33 UNSET_REASONING 哨兵 + reasoning.py:300-306 _explicit_reasoning_value
type ReasoningIntent struct {
	// Set 是否显式传入 reasoning
	Set bool
	// Value 传入的值；Set=true 且 Value=nil 表示显式传了 null
	Value any
}

// ──────────────────────────── 常量 ────────────────────────────

// reasoningModeAuto ReasoningConfig.Mode 的缺省值。
// Python: schema/config.py:62 Field(default="auto")
const reasoningModeAuto = "auto"

// 合法思考模式取值。
// Python: schema/config.py:62 mode: Literal["auto","enabled","disabled"]
const (
	// ReasoningModeAuto 跟随厂商默认
	ReasoningModeAuto = "auto"
	// ReasoningModeEnabled 强制开启思考
	ReasoningModeEnabled = "enabled"
	// ReasoningModeDisabled 强制关闭思考
	ReasoningModeDisabled = "disabled"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// validReasoningModes 合法 mode 集合。
// Python: schema/config.py:62 Literal["auto","enabled","disabled"]
var validReasoningModes = map[string]struct{}{
	ReasoningModeAuto:     {},
	ReasoningModeEnabled:  {},
	ReasoningModeDisabled: {},
}

// newReasoningKeys 新式 reasoning 配置允许的键集合。
// Python: reasoning.py:35 _NEW_REASONING_KEYS
var newReasoningKeys = map[string]struct{}{
	"mode":          {},
	"effort":        {},
	"budget_tokens": {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReasoningConfig 创建带缺省值的 ReasoningConfig。
// Go 的 Mode 零值是 ""，而 Python 默认 "auto"；不修正会让 resolve_reasoning_plan
// 的「mode == auto 且无 effort 且无 budget」三条件早返回失效。
// Python: schema/config.py:59-66（mode 默认 auto）
func NewReasoningConfig() *ReasoningConfig {
	return &ReasoningConfig{Mode: reasoningModeAuto}
}

// CoerceReasoningConfig 判断 reasoning 值属于新式配置还是遗留裸 dict。
// 返回 (cfg, nil) 表示新式；(nil, raw) 表示遗留，需原样透传；(nil, nil) 表示未设置。
// 判定时机与 Python 一致：每次 resolve 时判定，不在反序列化时定死。
// Python: reasoning.py:255-265 _coerce_reasoning_config
func CoerceReasoningConfig(value any) (cfg *ReasoningConfig, raw map[string]any) {
	if value == nil {
		return nil, nil
	}
	if c, ok := value.(*ReasoningConfig); ok {
		return c, nil
	}
	m, ok := value.(map[string]any)
	if !ok {
		return nil, nil
	}
	// 键集必须全部落在 {mode, effort, budget_tokens} 内才是新式；
	// 空 map 的键集是空集，是任意集合的子集，故视为新式（Python 同）。
	// Python: reasoning.py:263 set(raw).issubset(_NEW_REASONING_KEYS)
	for key := range m {
		if _, known := newReasoningKeys[key]; !known {
			return nil, m
		}
	}
	cfg = reasoningConfigFromMap(m)
	// Python: reasoning.py:264 ReasoningConfig(**raw) —— Pydantic 在此校验，
	// 非法 mode / 负 budget 抛 ValidationError。Go 无构造期钩子，在此显式校验；
	// 校验失败按「未设置」处理（不降级为遗留 raw，因为形态本身就是新式）。
	if err := cfg.Validate(); err != nil {
		logger.Warn(logComponent).Err(err).Msg("Invalid reasoning config ignored.")
		return nil, nil
	}
	return cfg, nil
}

// IsReasoningConfigIntent 判定是否为「新式配置意图」。
// Python: reasoning.py:268-270 is_reasoning_config_intent
func IsReasoningConfigIntent(value any) bool {
	cfg, _ := CoerceReasoningConfig(value)
	return cfg != nil
}

// Validate 校验字段取值，对齐 Pydantic 构造期约束。
//
// 上游 ReasoningConfig 是 BaseModel：
//
//	mode: Literal["auto","enabled","disabled"]
//	budget_tokens: Optional[int] = Field(default=None, ge=0)
//
// 非法值在构造（含 ReasoningConfig(**raw)）时抛 ValidationError。Go 无构造期钩子，
// 故由 CoerceReasoningConfig 在 map 分支显式调用本方法，调用方构造结构体时也可自行调用。
// Mode 为空视为未设置（Go 零值），不算非法。
// Python: schema/config.py:59-66
func (c *ReasoningConfig) Validate() error {
	if c == nil {
		return nil
	}
	if c.Mode != "" {
		if _, ok := validReasoningModes[c.Mode]; !ok {
			return fmt.Errorf("reasoning.mode 必须是 auto/enabled/disabled 之一，收到 %q", c.Mode)
		}
	}
	if c.BudgetTokens != nil && *c.BudgetTokens < 0 {
		return fmt.Errorf("reasoning.budget_tokens 必须 >= 0，收到 %d", *c.BudgetTokens)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// reasoningConfigFromMap 由 map 构造 ReasoningConfig（仅用于键集已受限的输入）。
// Python: reasoning.py:264 ReasoningConfig(**raw)
func reasoningConfigFromMap(m map[string]any) *ReasoningConfig {
	out := &ReasoningConfig{Mode: reasoningModeAuto}
	if v, ok := m["mode"].(string); ok {
		out.Mode = v
	}
	if v, ok := m["effort"].(string); ok {
		out.Effort = &v
	}
	if v, ok := m["budget_tokens"]; ok {
		out.BudgetTokens = toIntPtr(v)
	}
	return out
}

// toIntPtr JSON 数字反序列化后是 float64，需转 int；非数字返回 nil。
func toIntPtr(v any) *int {
	switch n := v.(type) {
	case int:
		return &n
	case int64:
		i := int(n)
		return &i
	case float64:
		i := int(n)
		return &i
	}
	return nil
}
