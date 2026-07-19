package llm_call

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator/llm_call"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewInstructionOptimizer(t *testing.T) {
	opt := NewInstructionOptimizer(nil)
	if opt == nil {
		t.Fatal("NewInstructionOptimizer 返回 nil")
	}
	if opt.Domain() != "llm" {
		t.Errorf("Domain() = %q, expected %q", opt.Domain(), "llm")
	}
}

func TestInstructionOptimizer_SelectSignals(t *testing.T) {
	opt := NewInstructionOptimizer(nil)

	tests := []struct {
		name      string
		signals   []*signal.EvolutionSignal
		wantCount int
	}{
		{
			name: "execution_failure 信号保留",
			signals: []*signal.EvolutionSignal{
				signal.MakeEvolutionSignal("execution_failure", "", "", signal.WithContext(map[string]any{})),
			},
			wantCount: 1,
		},
		{
			name: "low_score 信号保留",
			signals: []*signal.EvolutionSignal{
				signal.MakeEvolutionSignal("low_score", "", "", signal.WithContext(map[string]any{})),
			},
			wantCount: 1,
		},
		{
			name: "user_correction 信号保留",
			signals: []*signal.EvolutionSignal{
				signal.MakeEvolutionSignal("user_correction", "", "", signal.WithContext(map[string]any{})),
			},
			wantCount: 1,
		},
		{
			name: "collaboration_failure 信号保留",
			signals: []*signal.EvolutionSignal{
				signal.MakeEvolutionSignal("collaboration_failure", "", "", signal.WithContext(map[string]any{})),
			},
			wantCount: 1,
		},
		{
			name: "score==0 的信号保留",
			signals: []*signal.EvolutionSignal{
				signal.MakeEvolutionSignal("other_type", "", "", signal.WithContext(map[string]any{"score": 0})),
			},
			wantCount: 1,
		},
		{
			name: "score!=0 的非失败类型信号过滤",
			signals: []*signal.EvolutionSignal{
				signal.MakeEvolutionSignal("positive_signal", "", "", signal.WithContext(map[string]any{"score": 1})),
			},
			wantCount: 0,
		},
		{
			name: "混合信号",
			signals: []*signal.EvolutionSignal{
				signal.MakeEvolutionSignal("execution_failure", "", "", signal.WithContext(map[string]any{})),
				signal.MakeEvolutionSignal("positive_signal", "", "", signal.WithContext(map[string]any{"score": 1})),
				signal.MakeEvolutionSignal("low_score", "", "", signal.WithContext(map[string]any{})),
			},
			wantCount: 2,
		},
		{
			name:      "空信号列表",
			signals:   []*signal.EvolutionSignal{},
			wantCount: 0,
		},
		{
			name: "无 context 信号的 score 判断",
			signals: []*signal.EvolutionSignal{
				signal.MakeEvolutionSignal("other_type", "", ""), // 无 context
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selected := opt.SelectSignals(tt.signals)
			if len(selected) != tt.wantCount {
				t.Errorf("SelectSignals 选中 %d 个, expected %d", len(selected), tt.wantCount)
			}
		})
	}
}

func TestInstructionOptimizer_Step_无优化结果(t *testing.T) {
	opt := NewInstructionOptimizer(nil)
	op := llm_call.NewLLMCallOperator(
		[]llmschema.BaseMessage{llmschema.NewSystemMessage("test")},
		nil,
		llm_call.WithFreezeUserPrompt(false),
	)
	operators := map[string]operator.Operator{"llm_call": op}
	count := opt.Bind(operators, []string{"system_prompt", "user_prompt"}, nil)
	if count != 1 {
		t.Fatalf("Bind() = %d, expected 1", count)
	}

	updates := opt.Step()
	if len(updates) != 0 {
		t.Errorf("Step() 无优化结果时应返回空 map, got %d entries", len(updates))
	}
}

func TestInstructionOptimizer_Bind(t *testing.T) {
	opt := NewInstructionOptimizer(nil)
	op := llm_call.NewLLMCallOperator(
		[]llmschema.BaseMessage{llmschema.NewSystemMessage("sys")},
		nil,
		llm_call.WithFreezeUserPrompt(false),
	)
	operators := map[string]operator.Operator{"llm_call": op}

	t.Run("显式targets", func(t *testing.T) {
		count := opt.Bind(operators, []string{"system_prompt", "user_prompt"}, nil)
		if count != 1 {
			t.Errorf("Bind() = %d, expected 1", count)
		}
	})
}

func TestExtractTag(t *testing.T) {
	tests := []struct {
		name     string
		response string
		tag      string
		expected string
	}{
		{
			name:     "正常提取",
			response: "<PROMPT_OPTIMIZED>optimized content</PROMPT_OPTIMIZED>",
			tag:      "PROMPT_OPTIMIZED",
			expected: "optimized content",
		},
		{
			name:     "多行内容",
			response: "before\n<PROMPT_OPTIMIZED>\nline1\nline2\n</PROMPT_OPTIMIZED>\nafter",
			tag:      "PROMPT_OPTIMIZED",
			expected: "\nline1\nline2\n",
		},
		{
			name:     "标签不存在",
			response: "no tag here",
			tag:      "PROMPT_OPTIMIZED",
			expected: "",
		},
		{
			name:     "去除prompt_base标签",
			response: "<SYSTEM_PROMPT_OPTIMIZED><prompt_base>base</prompt_base>content</SYSTEM_PROMPT_OPTIMIZED>",
			tag:      "SYSTEM_PROMPT_OPTIMIZED",
			expected: "basecontent",
		},
		{
			name:     "USER_PROMPT_OPTIMIZED标签",
			response: "<USER_PROMPT_OPTIMIZED>user prompt here</USER_PROMPT_OPTIMIZED>",
			tag:      "USER_PROMPT_OPTIMIZED",
			expected: "user prompt here",
		},
		{
			name:     "THINKING标签",
			response: "<THINKING>analysis text</THINKING><PROMPT_OPTIMIZED>result</PROMPT_OPTIMIZED>",
			tag:      "PROMPT_OPTIMIZED",
			expected: "result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTag(tt.response, tt.tag)
			if result != tt.expected {
				t.Errorf("extractTag() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestAppendMissingPlaceholders(t *testing.T) {
	result := appendMissingPlaceholders("optimized prompt", []string{"name", "age"})
	if result != "optimized prompt\n{{name}}\n{{age}}" {
		t.Errorf("appendMissingPlaceholders() = %q, expected %q", result, "optimized prompt\n{{name}}\n{{age}}")
	}
}

func TestContainsTarget(t *testing.T) {
	targets := []string{"system_prompt", "user_prompt"}
	if !containsTarget(targets, "system_prompt") {
		t.Error("应包含 system_prompt")
	}
	if containsTarget(targets, "other") {
		t.Error("不应包含 other")
	}
}
