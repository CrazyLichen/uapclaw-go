// 结构化 ask_user 交互的共享有界契约。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/ask_user_contract.py
package rails

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// MaxStructuredQuestions 单次结构化反问允许的最大问题数。
//
// root_ask_user 的 questions 数量校验、root_context 的意图轮次校验都用这个上限。
//
// Python: MAX_STRUCTURED_QUESTIONS = 4
const MaxStructuredQuestions = 4
