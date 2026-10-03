package interaction

// ──────────────────────────── 结构体 ────────────────────────────

// InteractInput 交互输入，统一 TeamManager.Interact 的参数类型。
// 对齐 Python: TeamManager.interact(session_id, user_input) 中的 user_input 参数。
//
// Python 端 user_input 类型为 Any，实际运行时可以是 string / dict / InteractPayload / InteractiveInput。
// Go 端将多种输入统一包装为 InteractInput，外层调用者负责将其他类型自行包装。
//
// TeamRuntimeManager.Interact 内部根据 Raw 的具体类型分发：
//   - *sessioninteraction.InteractiveInput → 恢复中断
//   - string → ParseInteractStr → payloads
//   - InteractPayload → 直接分发
type InteractInput struct {
	// Raw 原始输入内容。
	// 支持 string / map[string]any / InteractPayload / *sessioninteraction.InteractiveInput。
	Raw any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInteractInput 创建 InteractInput。
func NewInteractInput(raw any) *InteractInput {
	return &InteractInput{Raw: raw}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
