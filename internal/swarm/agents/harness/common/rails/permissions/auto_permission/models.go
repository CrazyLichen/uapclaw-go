// 自动权限审查层的共享数据契约。
// 三个类型分别是：工具调用快照、权限处理结果、权限中断请求。
// 它们都不含判定逻辑，只是 9.90d 各文件之间传递的载体。
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/_auto_permission/models.py
package auto_permission

import (
	"errors"
	"regexp"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolInvocation 从 rail 回调输入中提取的规范化工具调用。
// 四个字段的类型取舍（详见 spec §3.1.2）：
//   - Ctx 收窄为 *AgentCallbackContext：消费方全部直接使用 .Agent() / .Session()，
//     与 ToolCallInputs.Ctx 同源。
//   - ToolCall 必须保留 any：Python invocation_context.py:240 会塞
//     SimpleNamespace(name=..., arguments=...) 替身，消费方只用 getattr 式读，
//     收窄为 *llmschema.ToolCall 将无法表达该替身。
//   - ToolArgs 收窄为 map[string]any：对齐 ToolCallInputs.ToolArgs，构造点全是 dict。
//
// Python: @dataclass(frozen=True) ToolInvocation
//
//	ctx: Any | None
//	tool_call: Any | None
//	tool_name: str
//	tool_args: Any
type ToolInvocation struct {
	// Ctx 回调上下文
	Ctx *agentinterfaces.AgentCallbackContext
	// ToolCall 原始工具调用对象（可能是鸭子类型替身，故为 any）
	ToolCall any
	// ToolName 工具名称
	ToolName string
	// ToolArgs 工具参数字典
	ToolArgs map[string]any
}

// PermissionHandlingResult 已处理结果，以及该决策的可信宿主来源。
// Handled 为 true 时 DecisionSource 必须非空，用 NewPermissionHandlingResult 保证。
// Python: @dataclass(frozen=True) PermissionHandlingResult
//
//	handled: bool
//	result: Any | None
//	decision_source: str
type PermissionHandlingResult struct {
	// Handled 是否已处理（true 表示不再走后续判定）
	Handled bool
	// Result 处理结果（具体形状由决策来源决定）
	Result any
	// DecisionSource 该决策的可信宿主来源标识
	DecisionSource string
}

// PermissionInterruptRequest 保留权限元数据供 UI 渲染的中断请求。
// 构造约定：InterruptKind 置为 saschema.InterruptKindPermission。
// 若需挂动态字段，走已存在的 InterruptRequest.Metadata，不要新增字段。
// Python: class PermissionInterruptRequest(InterruptRequest)
//
//	model_config = {"extra": "allow"}
//	interrupt_kind: str = InterruptKind.PERMISSION
type PermissionInterruptRequest struct {
	saschema.InterruptRequest
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ReviewerActionTargetMaxLength 审查动作目标描述的最大长度
	// Python: _REVIEWER_ACTION_TARGET_MAX_LENGTH
	ReviewerActionTargetMaxLength = 160

	// ReviewerUITextMaxLength 审查 UI 文本的最大长度
	// Python: _REVIEWER_UI_TEXT_MAX_LENGTH
	ReviewerUITextMaxLength = 1024

	// ReviewerUIShortTextMaxLength 审查 UI 短文本的最大长度
	// Python: _REVIEWER_UI_SHORT_TEXT_MAX_LENGTH
	ReviewerUIShortTextMaxLength = 240

	// ReviewerDenialGuidance 拒绝时给模型的引导语，防止绕过拒绝
	// Python: _REVIEWER_DENIAL_GUIDANCE
	ReviewerDenialGuidance = "Do not retry, rephrase, split, or switch tools to bypass this denial. " +
		"A materially changed user intent requires a new tool call and full review."

	// ProhibitedFileDeliveryReason 用户禁止文件投递时的拒绝原因
	// Python: PROHIBITED_FILE_DELIVERY_REASON
	ProhibitedFileDeliveryReason = "file_delivery_prohibited_by_user"

	// DeterministicBoundedScopeDecisionSource 确定性有界范围判定的决策来源
	// Python: DETERMINISTIC_BOUNDED_SCOPE_DECISION_SOURCE
	DeterministicBoundedScopeDecisionSource = "deterministic_bounded_scope"

	// DeterministicReadonlyPublicWebReason 确定性放行只读公开 Web 的原因码
	// Python: DETERMINISTIC_READONLY_PUBLIC_WEB_REASON
	DeterministicReadonlyPublicWebReason = "deterministic_readonly_public_web"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ShellDisplayPathPattern 从 shell 命令里抽出需要脱敏显示的路径。
// 覆盖 macOS（/Users/、/private/、/tmp/）、Linux（/home/x/）、
// HOME 相对（~/...）与 Windows 盘符（C:\Users\、Documents and Settings）四类。
// ⤵️ Python 全仓零消费，待 9.90d 的 reviewer_redaction 启用。
// Python: _SHELL_DISPLAY_PATH_PATTERN
var ShellDisplayPathPattern = regexp.MustCompile(
	`(?:` +
		`(?:/Users/|/private/|/tmp/)[^\s\"']+` +
		`|/home/[^/\s\"']+/[^\s\"']+` +
		`|~(?:[/\\][^\s\"']+)+` +
		`|[A-Za-z]:[\\/](?:Users|Documents and Settings)[\\/]` +
		"[^'\";|&<>`,，。；、\\r\\n]+" +
		`)`,
)

// ShellDisplayPreSplitPathPattern 仅匹配 Windows 盘符路径，用于预切分。
// ⤵️ Python 全仓零消费，待 9.90d 的 reviewer_redaction 启用。
// Python: _SHELL_DISPLAY_PRE_SPLIT_PATH_PATTERN
var ShellDisplayPreSplitPathPattern = regexp.MustCompile(
	`[A-Za-z]:[\\/](?:Users|Documents and Settings)[\\/]` +
		"[^'\";|&<>`,，。；、\\r\\n]+",
)

// SensitiveDisplayPathPattern 识别敏感文件名与目录（.env / 凭证 / 密钥）。
// 三类：点文件类（.env*、credentials、id_*、known_hosts）、
// 敏感目录（.ssh / .aws / .gnupg / .jiuwenswarm/config）、
// 关键词类（secret / token / api_key / private_key / credential / password）。
// ⤵️ Python 全仓零消费，待 9.90d 的 reviewer_redaction 启用。
// Python: _SENSITIVE_DISPLAY_PATH_PATTERN
var SensitiveDisplayPathPattern = regexp.MustCompile(
	`(?i)(?:` +
		`(^|[/\\])(?:\.env(?:\.[^/\\]+)?|credentials?|id_rsa|id_dsa|id_ecdsa|` +
		`id_ed25519|known_hosts)$` +
		`|(^|[/\\])(?:\.ssh|\.aws|\.gnupg|\.jiuwenswarm[/\\]config)(?:[/\\]|$)` +
		`|(?:secret|token|api[_-]?key|private[_-]?key|credential|password)` +
		`)`,
)

// DeterministicReadonlyPublicWebFetchSourceKinds 允许确定性放行的 fetch 来源类型集合。
// 只读用途，本包内不得对其赋值。
// ⤵️ Python 全仓零消费，待 9.90d 的 deterministic 启用。
// Python: DETERMINISTIC_READONLY_PUBLIC_WEB_FETCH_SOURCE_KINDS = frozenset({"recent_search_result"})
var DeterministicReadonlyPublicWebFetchSourceKinds = map[string]struct{}{
	"recent_search_result": {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPermissionHandlingResult 构造并校验权限处理结果。
// 校验与 Python 的 __post_init__ 逐条一致：handled 为 true 而来源为空即报错，
// 防止下游拿到"已处理"却无从判断是谁处理的。
// Python: PermissionHandlingResult.__post_init__
func NewPermissionHandlingResult(handled bool, result any, decisionSource string) (*PermissionHandlingResult, error) {
	if handled && decisionSource == "" {
		return nil, errors.New("handled permission result requires a decision source")
	}
	return &PermissionHandlingResult{
		Handled:        handled,
		Result:         result,
		DecisionSource: decisionSource,
	}, nil
}
