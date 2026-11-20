package flow

import (
	"context"

	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
)

// Start 工作流入口组件：invoke 原样透传输入。
// Python: components/flow/start_comp.py:10-15 一比一
// （__init__ 仅调 super().__init__()；invoke 返回 inputs）。
// stream/collect/transform 未覆写，走基类默认未实现错误。
type Start struct {
	components.WorkflowComponent
}

// NewStart 构造 Start 组件。
func NewStart() *Start {
	return &Start{
		WorkflowComponent: components.WorkflowComponent{
			InvokeFn: func(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade) (map[string]any, error) {
				return inputs, nil
			},
		},
	}
}

// 编译期兜底：Start 满足可执行体契约（Python Start 继承 WorkflowComponent ≙ 嵌入）。
var _ graphexe.Executable[map[string]any, map[string]any] = (*Start)(nil)
