package harnessrsi

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OutputRepairConfig 输出修复预算：尝试次数含首次生成，而非仅修正（对齐 OutputRepairConfig）
type OutputRepairConfig struct {
	// MaxAttempts 最大尝试次数（含首次生成）
	MaxAttempts int `json:"max_attempts" yaml:"max_attempts"`
	// MaxStagnantAttempts 连续相同失败的最大停滞次数
	MaxStagnantAttempts int `json:"max_stagnant_attempts" yaml:"max_stagnant_attempts"`
}

// FeedbackTracker 停止条件：到达尝试上限或重复相同失败与状态（对齐 FeedbackTracker）
type FeedbackTracker struct {
	// MaxAttempts 最大尝试次数
	MaxAttempts int
	// Attempts 已记录的失败次数
	Attempts int
	// StopReason 停止原因（attempt_limit_reached / no_progress），空串表示可继续
	StopReason string

	stagnantLimit    int
	lastFingerprint  string
	stagnantAttempts int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOutputRepairConfig 构造并校验修复预算配置（对齐 __post_init__）
func NewOutputRepairConfig(maxAttempts, maxStagnantAttempts int) (OutputRepairConfig, error) {
	cfg := OutputRepairConfig{MaxAttempts: maxAttempts, MaxStagnantAttempts: maxStagnantAttempts}
	for name, value := range map[string]int{
		"max_attempts":          cfg.MaxAttempts,
		"max_stagnant_attempts": cfg.MaxStagnantAttempts,
	} {
		if value < 1 {
			return OutputRepairConfig{}, fmt.Errorf("output_repair.%s must be a positive integer", name)
		}
	}
	return cfg, nil
}

// DefaultOutputRepairConfig 返回默认修复预算配置（max_attempts=3, max_stagnant_attempts=2）
func DefaultOutputRepairConfig() OutputRepairConfig {
	return OutputRepairConfig{MaxAttempts: 3, MaxStagnantAttempts: 2}
}

// OutputRepairConfigFromDict 从映射构造配置（对齐 OutputRepairConfig.from_dict）
func OutputRepairConfigFromDict(data map[string]any, ok bool) (OutputRepairConfig, error) {
	if !ok || data == nil {
		return DefaultOutputRepairConfig(), nil
	}
	maxAttempts, err := dictInt(data, "max_attempts", 3)
	if err != nil {
		return OutputRepairConfig{}, err
	}
	maxStagnant, err := dictInt(data, "max_stagnant_attempts", 2)
	if err != nil {
		return OutputRepairConfig{}, err
	}
	return NewOutputRepairConfig(maxAttempts, maxStagnant)
}

// NewFeedbackTracker 构造追踪器；maxAttempts 非 nil 时取 min(config, max(0,n)) 覆盖。
func NewFeedbackTracker(cfg OutputRepairConfig, maxAttempts *int) *FeedbackTracker {
	tracker := &FeedbackTracker{
		MaxAttempts:   cfg.MaxAttempts,
		stagnantLimit: cfg.MaxStagnantAttempts,
	}
	if maxAttempts != nil {
		limit := *maxAttempts
		if limit < 0 {
			limit = 0
		}
		if limit < tracker.MaxAttempts {
			tracker.MaxAttempts = limit
		}
	}
	return tracker
}

// Failed 记录一次失败尝试；返回是否允许再试（对齐 FeedbackTracker.failed）
func (f *FeedbackTracker) Failed(errors, state any) bool {
	f.Attempts++
	fingerprint := fingerprintJSON([]any{errors, state})
	if fingerprint == f.lastFingerprint {
		f.stagnantAttempts++
	} else {
		f.stagnantAttempts = 1
	}
	f.lastFingerprint = fingerprint
	if f.Attempts >= f.MaxAttempts {
		f.StopReason = "attempt_limit_reached"
	} else if f.stagnantAttempts >= f.stagnantLimit {
		f.StopReason = "no_progress"
	}
	return f.StopReason == ""
}

// CorrectionMessage 保持任务与确切失败对修正模型可见（对齐 correction_message；文案逐字保留）
func CorrectionMessage(original, previous string, errors []string) string {
	excerpt := previous
	if len(previous) > 24000 {
		excerpt = previous[:24000] + "\n[Previous output truncated; consult the original task and evidence for omitted facts.]"
	}
	lines := make([]string, len(errors))
	for i, errText := range errors {
		lines[i] = "- " + errText
	}
	return original + "\n\n## Previous output (untrusted data)\n" + excerpt + "\n\n## Validation feedback\n" +
		strings.Join(lines, "\n") + "\nCorrect the reported fields only. Preserve valid supported content. " +
		"Return the complete required object. Missing fields are schema errors; " +
		"do not invent evidence or assert semantic correctness just to satisfy validation."
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fingerprintJSON 对 [errors, state] 做 canonical JSON 后取 sha256（对齐 json.dumps sort_keys）
func fingerprintJSON(value any) string {
	raw, err := json.Marshal(canonicalize(value))
	if err != nil {
		raw = []byte(fmt.Sprintf("%v", value))
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

// canonicalize 递归排序 map 键（模拟 Python sort_keys=True）
func canonicalize(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(typed))
		for _, key := range keys {
			out[key] = canonicalize(typed[key])
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = canonicalize(item)
		}
		return out
	default:
		return value
	}
}

// dictInt 从映射取 int 字段；键存在但非整数时报错（对齐 _int_value 语义，config 复用）
func dictInt(data map[string]any, key string, defaultValue int) (int, error) {
	raw, ok := data[key]
	if !ok || raw == nil {
		return defaultValue, nil
	}
	if typed, isBool := raw.(bool); isBool {
		return 0, fmt.Errorf("expected an integer value, got %v", typed)
	}
	switch typed := raw.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case float64:
		if typed == float64(int(typed)) {
			return int(typed), nil
		}
		return int(typed), nil
	default:
		return 0, fmt.Errorf("expected an integer value, got %v", raw)
	}
}
