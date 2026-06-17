package interaction

import (
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InteractiveInput 用户交互输入容器。
// 支持两种输入模式：RawInputs（未绑定节点 ID，首次交互）和 UserInputs（按节点 ID 绑定）。
// 两者互斥：RawInputs 已设置时不能调用 Update。
//
// 对应 Python: openjiuwen/core/session/interaction/interactive_input.py (InteractiveInput)
type InteractiveInput struct {
	// UserInputs 按节点 ID 绑定的输入映射
	UserInputs map[string]any
	// RawInputs 未绑定节点 ID 的原始输入（首次交互使用）
	RawInputs any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInteractiveInput 创建交互输入实例。
// rawInputs 为可选参数：不传则 RawInputs 为 nil（标记为"未提供"），
// 传入 nil 则返回错误（与 Python 一致：raw_inputs=None 被拒绝）。
// 对应 Python: InteractiveInput.__init__(raw_inputs)
func NewInteractiveInput(rawInputs ...any) (*InteractiveInput, error) {
	input := &InteractiveInput{
		UserInputs: make(map[string]any),
	}
	if len(rawInputs) == 0 {
		// 未提供 rawInputs，RawInputs 保持 nil
		return input, nil
	}
	if rawInputs[0] == nil {
		// 显式传入 nil，与 Python 一致拒绝
		return nil, exception.RaiseError(exception.StatusInteractionInputInvalid,
			exception.WithParam("reason", "value of raw_inputs is none"),
		)
	}
	input.RawInputs = rawInputs[0]
	return input, nil
}

// ──────────────────────────── InteractiveInput 方法 ────────────────────────────

// Update 添加节点绑定的输入。
// RawInputs 已设置时返回错误（互斥约束），value 为 nil 时返回错误。
// 注意：与 Python 对齐，nodeID 允许空字符串（Python 只拒绝 node_id is None，不拒绝 ""）。
// 对应 Python: InteractiveInput.update(node_id, value)
func (i *InteractiveInput) Update(nodeID string, value any) error {
	if i.RawInputs != nil {
		return exception.RaiseError(exception.StatusInteractionInputInvalid,
			exception.WithParam("reason", "raw_inputs existed, update is invalid"),
		)
	}
	if value == nil {
		return exception.RaiseError(exception.StatusInteractionInputInvalid,
			exception.WithParam("reason", "value is none"),
		)
	}
	i.UserInputs[nodeID] = value
	return nil
}
