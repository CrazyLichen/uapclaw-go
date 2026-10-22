// 本文件提供 ModelBackupRail 模型备份护栏（一比一复刻 Python）。

package rail

import (
	"context"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// modelSwitcher 模型切换接口。
// Python: hasattr(ctx.agent, "set_llm") 动态能力检测的 Go 等价，
// 沿用 harness/rails/task_planning.go 同名接口先例（consumer-side 小接口）。
type modelSwitcher interface {
	// SetLLM 设置预构建的 LLM 模型实例
	SetLLM(model *llm.Model)
	// GetLLM 返回 LLM 模型实例
	GetLLM() (*llm.Model, error)
}

// ModelBackupRail 模型备份护栏，LLM 调用异常时按序切换备用模型并请求重试。
//
// Python: ModelBackupRail (openjiuwen/core/single_agent/rail/model_backup.py)
//
// 流程位置：ReAct 循环 Think 步骤的 ModelCallRail.Execute 内，
// LLM 调用抛异常 → fire ON_MODEL_EXCEPTION → 本护栏切换备用模型
// → RequestRetry → RailExecutor 吞异常重试（此时已换备用模型）。
// 备用耗尽后不再请求重试，异常正常上抛。
type ModelBackupRail struct {
	agentinterfaces.BaseRail
	// backupModels 备用模型链，按序依次切换
	// Python: backup_models
	backupModels []*llm.Model
	// index 下一个待切换的备用模型下标
	// Python: index；无锁对齐 Python（ReAct 循环内串行触发）
	index int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewModelBackupRail 创建模型备份护栏。
// Python: ModelBackupRail(backup_models)
func NewModelBackupRail(backupModels []*llm.Model) *ModelBackupRail {
	return &ModelBackupRail{
		BaseRail:     *agentinterfaces.NewBaseRail(),
		backupModels: backupModels,
	}
}

// OnModelException LLM 调用异常时切换到下一个备用模型并请求重试。
// Python: on_model_exception (L15-18)
func (r *ModelBackupRail) OnModelException(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// Python L16: if hasattr(ctx.agent, "set_llm") and self.index < len(self.backup_models)
	switcher, ok := cbc.Agent().(modelSwitcher)
	if !ok || r.index >= len(r.backupModels) {
		// agent 不支持切换或备用耗尽 → 不请求重试，异常正常上抛
		return nil
	}
	switcher.SetLLM(r.backupModels[r.index])
	r.index++
	// Python: ctx.request_retry()（默认 delay_seconds=0.0）
	cbc.RequestRetry(0)
	return nil
}

// GetCallbacks 返回已覆盖的钩子方法映射。
// Go 无 Python 的覆写检测，需显式声明（CallbackFrom + BuildCallbacks）。
func (r *ModelBackupRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(agentinterfaces.CallbackOnModelException, func(ctx context.Context, railCtx any) error {
			return r.OnModelException(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
		}),
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
