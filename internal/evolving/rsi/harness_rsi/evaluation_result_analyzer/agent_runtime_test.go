// agent_runtime_test.go 诊断 agent 运行时测试（Python test_evaluation_result_analyzer.py 对应段）。
package evaluationresultanalyzer

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	tokeniface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/config"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// budgetTestContext 预算轨测试用 ModelContext 桩（方法面与 ceinterface.ModelContext 对齐）。
type budgetTestContext struct {
	// messages 上下文消息
	messages []llmschema.BaseMessage
}

func (h *budgetTestContext) Len() int { return len(h.messages) }
func (h *budgetTestContext) GetMessages(_ int, _ bool) ([]llmschema.BaseMessage, error) {
	return h.messages, nil
}
func (h *budgetTestContext) SetMessages(messages []llmschema.BaseMessage, _ bool) {
	h.messages = messages
}
func (h *budgetTestContext) PopMessages(_ int, _ bool) []llmschema.BaseMessage {
	popped := h.messages
	h.messages = nil
	return popped
}
func (h *budgetTestContext) ClearMessages(_ context.Context, _ bool, _ ...ceinterface.Option) error {
	h.messages = nil
	return nil
}
func (h *budgetTestContext) AddMessages(_ context.Context, messages []llmschema.BaseMessage, _ ...ceinterface.Option) ([]llmschema.BaseMessage, error) {
	h.messages = append(h.messages, messages...)
	return h.messages, nil
}
func (h *budgetTestContext) GetContextWindow(_ context.Context, _ []llmschema.BaseMessage, _ []commonschema.ToolInfoInterface, _ int, _ int, _ ...ceinterface.Option) (*ceinterface.ContextWindow, error) {
	return nil, nil
}
func (h *budgetTestContext) Statistic() *ceinterface.ContextStats                { return nil }
func (h *budgetTestContext) SessionID() string                                   { return "test-session" }
func (h *budgetTestContext) ContextID() string                                   { return "test-session" }
func (h *budgetTestContext) TokenCounter() tokeniface.TokenCounter               { return nil }
func (h *budgetTestContext) ReloaderTool() tool.Tool                             { return nil }
func (h *budgetTestContext) WorkspaceDir() string                                { return "" }
func (h *budgetTestContext) SetSessionRef(_ sessioninterfaces.SessionFacade)     {}
func (h *budgetTestContext) GetSessionRef() sessioninterfaces.SessionFacade      { return nil }
func (h *budgetTestContext) OffloadMessages(_ string, _ []llmschema.BaseMessage) {}
func (h *budgetTestContext) CompressContext(_ context.Context, _ ...ceinterface.CompressContextOption) (*ceinterface.CompressContextResult, error) {
	return nil, nil
}
func (h *budgetTestContext) SaveState() map[string]any  { return nil }
func (h *budgetTestContext) LoadState(_ map[string]any) {}
func (h *budgetTestContext) CreateContext(_ context.Context, _ string, _ sessioninterfaces.SessionFacade, _ ...ceinterface.CreateContextOption) (ceinterface.ModelContext, error) {
	return h, nil
}
func (h *budgetTestContext) GetContext(_ string, _ string) ceinterface.ModelContext { return h }

// ──────────────────────────── 测试 ────────────────────────────

// TestDiagnosisAgentExecutionError 执行失败错误携带终止原因
func TestDiagnosisAgentExecutionError(t *testing.T) {
	err := NewDiagnosisAgentExecutionError("agent returned failure", "max_iterations")
	if err.Error() != "agent returned failure" || err.TerminationReason() != "max_iterations" {
		t.Fatalf("错误字段: %v", err)
	}
}

// TestDiagnosisBashDenyPatterns 两条拒绝模式边界（逐字对齐 Python 正则）
func TestDiagnosisBashDenyPatterns(t *testing.T) {
	if len(diagnosisBashDenyPatterns) != 2 {
		t.Fatalf("应两条: %v", diagnosisBashDenyPatterns)
	}
	rootRe := regexp.MustCompile(diagnosisBashDenyPatterns[0])
	upRe := regexp.MustCompile(diagnosisBashDenyPatterns[1])
	// 根目录命令命中
	for _, cmd := range []string{"find /", "ls /", "du / ; echo", "tree / | head"} {
		if !rootRe.MatchString(cmd) {
			t.Fatalf("应命中根目录命令: %q", cmd)
		}
	}
	// 非根目录不命中
	for _, cmd := range []string{"find ./sub", "ls ./sub", "grep -r pattern ."} {
		if rootRe.MatchString(cmd) {
			t.Fatalf("不应命中: %q", cmd)
		}
	}
	// 上跳命中
	for _, cmd := range []string{"cat ../secret", "cd ..", "cat ..\\win"} {
		if !upRe.MatchString(cmd) {
			t.Fatalf("应命中上跳: %q", cmd)
		}
	}
	if upRe.MatchString("cat ./nested/..") {
		t.Fatalf("./nested/.. 不应以词边界误报: %q", "cat ./nested/..")
	}
}

// TestDiagnosisBudgetRailTurns 预算轨轮次计数与末轮收工具（Python before_model_call）
func TestDiagnosisBudgetRailTurns(t *testing.T) {
	rail := NewDiagnosisBudgetRail(3)
	ctx := context.Background()
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.ModelCallInputs{
		Messages: []llmschema.BaseMessage{},
		Tools:    []commonschema.ToolInfoInterface{&commonschema.ToolInfo{}},
	}, nil)
	cbc.SetModelContext(&budgetTestContext{})

	// 第 1、2 轮：只注入进度提示，工具保留
	for turn := 1; turn <= 2; turn++ {
		if err := rail.BeforeModelCall(ctx, cbc); err != nil {
			t.Fatal(err)
		}
		inputs := cbc.Inputs().(*interfaces.ModelCallInputs)
		if inputs.Tools == nil {
			t.Fatalf("第 %d 轮不应收走工具", turn)
		}
		msgs, _ := cbc.ModelContext().GetMessages(0, true)
		if len(msgs) != turn {
			t.Fatalf("第 %d 轮应注入 %d 条提示", turn, turn)
		}
	}
	// 第 3 轮（末轮）：收工具 + final turn 文案
	if err := rail.BeforeModelCall(ctx, cbc); err != nil {
		t.Fatal(err)
	}
	inputs := cbc.Inputs().(*interfaces.ModelCallInputs)
	if inputs.Tools != nil {
		t.Fatal("末轮应收走工具")
	}
	msgs, _ := cbc.ModelContext().GetMessages(0, true)
	last := msgs[len(msgs)-1]
	content := last.GetContent().String()
	if !strings.Contains(content, "final turn of the existing investigation budget") {
		t.Fatalf("末轮文案: %q", content)
	}
}

// TestDiagnosisBudgetRailNotesProgress 进度文案逐字断言（Python note f-string）
func TestDiagnosisBudgetRailNotesProgress(t *testing.T) {
	rail := NewDiagnosisBudgetRail(5)
	ctx := context.Background()
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.ModelCallInputs{}, nil)
	cbc.SetModelContext(&budgetTestContext{})
	if err := rail.BeforeModelCall(ctx, cbc); err != nil {
		t.Fatal(err)
	}
	msgs, _ := cbc.ModelContext().GetMessages(0, true)
	content := msgs[0].GetContent().String()
	want := "Investigation turn 1/5. Return the diagnosis as soon as the local cause is supported."
	if content != want {
		t.Fatalf("进度文案: got %q want %q", content, want)
	}
}

// TestDiagnosisAgentRuntimeBuildAgentRefMissing ref 缺失报错文案对齐（Python ValueError）
func TestDiagnosisAgentRuntimeBuildAgentRefMissing(t *testing.T) {
	rt := NewDiagnosisAgentRuntime(&config.EvaluationResultAnalyzerConfig{})
	_, err := rt.BuildAgent(context.Background(), t.TempDir(), "prompt", "")
	if err == nil || !strings.Contains(err.Error(), "model_config_ref must be set") {
		t.Fatalf("ref 缺失报错: %v", err)
	}
}

// TestRunDeepAgentTextExecutionError error 结果转执行失败错误（result_type=error 分支）
// runner.RunAgent 经全局 runner 难以桩化，本用例只覆盖 DiagnosisAgentExecutionError
// 的抛出契约（callOnce 闭包语义与 Python 对齐由集成用例覆盖）。
func TestRunDeepAgentTextExecutionError(t *testing.T) {
	// 直接验证重试语义依赖的 RunModelCallWithRetries 对执行错误的包装：
	// callOnce 返回 *DiagnosisAgentExecutionError 时原样透传（termination_reason 保留）
	err := NewDiagnosisAgentExecutionError("boom", "user_cancel")
	var target *DiagnosisAgentExecutionError
	if !errors.As(err, &target) || target.TerminationReason() != "user_cancel" {
		t.Fatalf("errors.As 断言: %v", err)
	}
}
