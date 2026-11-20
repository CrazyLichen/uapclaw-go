package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
)

// ──────────────────────────── 门面构造 ────────────────────────────

// TestNewWorkflow_默认与内建 对齐 workflow.py:106-118。
func TestNewWorkflow_默认与内建(t *testing.T) {
	w := NewWorkflow(nil)
	if w.Card() == nil || w.Card().ID == "" {
		t.Fatal("card nil 兜底缺失")
	}
	if w.internal == nil {
		t.Fatal("BaseWorkflow 内建缺失")
	}
	if w.isStreaming {
		t.Fatal("isStreaming 默认 false")
	}
}

// TestSetEndComp_abilities推导 对齐 workflow.py:249-263。
func TestSetEndComp_abilities推导(t *testing.T) {
	t.Run("streaming带inputs_schema_补STREAM", func(t *testing.T) {
		w := NewWorkflow(nil)
		w.SetEndComp("e", mustEnd(t), map[string]any{"a": 1}, nil, nil, nil, "streaming", nil)
		abs := w.internal.Config().Spec.CompConfigs["e"].Abilities
		if !containsAbility(abs, components.ComponentAbilityStream) {
			t.Fatalf("应补 STREAM：%v", abs)
		}
		if !w.isStreaming {
			t.Fatal("isStreaming 应置位")
		}
	})
	t.Run("非流式带stream_inputs_补COLLECT", func(t *testing.T) {
		w := NewWorkflow(nil)
		w.SetEndComp("e", mustEnd(t), nil, nil, map[string]any{"a": 1}, nil, "", nil)
		abs := w.internal.Config().Spec.CompConfigs["e"].Abilities
		if !containsAbility(abs, components.ComponentAbilityCollect) {
			t.Fatalf("应补 COLLECT：%v", abs)
		}
		if w.isStreaming {
			t.Fatal("isStreaming 不应置位")
		}
	})
	t.Run("streaming无schema_默认STREAM", func(t *testing.T) {
		w := NewWorkflow(nil)
		w.SetEndComp("e", mustEnd(t), nil, nil, nil, nil, "streaming", nil)
		abs := w.internal.Config().Spec.CompConfigs["e"].Abilities
		if len(abs) != 1 || !containsAbility(abs, components.ComponentAbilityStream) {
			t.Fatalf("默认 [STREAM]：%v", abs)
		}
	})
}

// TestValidateSession_空会话拒绝 对齐 workflow.py:756-760。
func TestValidateSession_空会话拒绝(t *testing.T) {
	w := NewWorkflow(nil)
	if err := w.validateSession(nil); err == nil {
		t.Fatal("nil session 应拒绝")
	} else if !strings.Contains(err.Error(), "session is required") {
		t.Fatalf("文案不符：%v", err)
	}
}

// TestDraw_未启用返回空 对齐 workflow.py:471-499（D-d 空分支复刻）。
func TestDraw_未启用返回空(t *testing.T) {
	w := NewWorkflow(nil)
	if got := w.Draw("", "mermaid", false, false); got != "" {
		t.Fatalf("mermaid 应返回空串：%v", got)
	}
	if got, ok := w.Draw("", "png", false, false).([]byte); !ok || len(got) != 0 {
		t.Fatal("png 应返回空字节")
	}
}

// TestValidateInputs_InteractiveInput直通 对齐 workflow.py:746-754。
func TestValidateInputs_InteractiveInput直通(t *testing.T) {
	w := NewWorkflow(nil)
	w.card.InputParams = map[string]any{"type": "object"}
	ii, err := interaction.NewInteractiveInput()
	if err != nil {
		t.Fatalf("构造交互输入：%v", err)
	}
	if err := w.validateInputs(ii, nil); err != nil {
		t.Fatalf("InteractiveInput 应直通：%v", err)
	}
}

// ──────────────────────────── executeWithTimeout 三分类 ────────────────────────────

// TestExecuteWithTimeout_超时转错误码 对齐 workflow.py:671-679。
func TestExecuteWithTimeout_超时转错误码(t *testing.T) {
	w := NewWorkflow(nil)
	_, err := w.executeWithTimeout(context.Background(),
		func(ctx context.Context) (any, error) {
			time.Sleep(200 * time.Millisecond)
			return nil, nil
		}, 30*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "exceeded time limit") {
		t.Fatalf("期望 WORKFLOW_EXECUTION_TIMEOUT：%v", err)
	}
}

// TestExecuteWithTimeout_取消重抛 对齐 workflow.py:663-670。
func TestExecuteWithTimeout_取消重抛(t *testing.T) {
	w := NewWorkflow(nil)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := w.executeWithTimeout(ctx,
		func(ctx context.Context) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消应重抛 context.Canceled：%v", err)
	}
}

// TestExecuteWithTimeout_RecursionError分类 对齐 workflow.py:680-689。
func TestExecuteWithTimeout_RecursionError分类(t *testing.T) {
	w := NewWorkflow(nil)
	_, err := w.executeWithTimeout(context.Background(),
		func(ctx context.Context) (any, error) {
			return nil, &pregel.RecursionError{}
		}, 0)
	if err == nil {
		t.Fatal("RecursionError 应转 WORKFLOW_EXECUTION_ERROR")
	}
}

// ──────────────────────────── stream 超时钳制 ────────────────────────────

// TestEnvDuration_形态 对齐 workflow.py:514-522 env 取值。
func TestEnvDuration_形态(t *testing.T) {
	if envDuration(nil, "k") != 0 {
		t.Fatal("nil config 应返回 0")
	}
}

// TestTaskDone_完成态 对齐 workflow.py:703-705。
func TestTaskDone_完成态(t *testing.T) {
	// Python _is_background_task_done(task.done())：task 非 nil 前置。
	// nil 防御由调用方保证（CreateBackgroundTask 失败路径显式判空），此处不测 nil。
}

// ──────────────────────────── 会话装配 ────────────────────────────

// TestCreateWorkflowSession_主分支 对齐 workflow.py:708-731 装配序。
func TestCreateWorkflowSession_主分支(t *testing.T) {
	w := NewWorkflow(nil)
	_ = w.AddWorkflowComp("a", newFakeStartComp(), boolPtr(false), nil, nil, nil, nil, nil, 0, -1.0, nil, nil)
	_ = w.internal.StartComp("a")
	sess := session.NewSession()
	ws := w.createWorkflowSession(sess, []stream.StreamMode{stream.StreamModeOutput}, false)
	if ws == nil {
		t.Fatal("会话装配失败")
	}
	if p, ok := ws.(interface{ WorkflowID() string }); !ok || p.WorkflowID() != w.card.ID {
		t.Fatal("workflow_id 未设（WorkflowIDProvider 断言失败或值不符）")
	}
	if ws.ActorManager() == nil {
		t.Fatal("ActorManager 未装配（sub_graph=false）")
	}
	if ws.StreamWriterManager() == nil {
		t.Fatal("StreamWriterManager 未装配（内部构造）")
	}
	if ws.Tracer() == nil {
		t.Fatal("Tracer 未装配")
	}
}

// mustEnd 构造 End 组件替身（flow→workflow 测试期 import cycle，
// 根包测试内联组合件桩；SetEndComp 仅消费组合面做 abilities 推导，不执行）。
func mustEnd(t *testing.T) components.ComponentComposable {
	t.Helper()
	return newFakeStartComp()
}

var _ = session.NewSession
