// 文件投递的精确授权。
//
// 对应 Python 代码：_auto_permission/artifact_authorization.py（132 行 / 6 函数 + 1 方法）
//
// 两块职责：
//  1. 识别「用户明确禁止文件投递」与「本次动作确实在投递本地文件」
//  2. 对已放行的 send_file_to_user 签发精确授权（grant），保证「批准的文件集合」
//     与「实际发送的文件集合」完全一致
//
// ## 偏差 V-13：grant 读取需要显式 ctx
//
// Python 的 `current_send_file_execution_grant()` 从 ContextVar 隐式取；
// Go 的 `CurrentSendFileExecutionGrant(ctx)` / `PublishSendFileExecutionGrant(ctx, grant)`
// 需要显式传 ctx，故两个函数都多一个 ctx 参数。

package auto_permission

import (
	"context"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sendFileToolNameForGrant 授权签发针对的工具名
	//
	// Python: "send_file_to_user"
	sendFileToolNameForGrant = "send_file_to_user"

	// sendMessageToolName 需要结构判定的消息工具
	//
	// Python: "send_message"
	sendMessageToolName = "send_message"

	// externalSendSideEffect 外部发送副作用标记
	//
	// Python: "external_send"
	externalSendSideEffect = "external_send"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// fileDeliveryProhibitionPatterns 禁止文件投递的中英文模式。
	//
	// Python: _FILE_DELIVERY_PROHIBITION_PATTERNS
	// 逐字复制上游正则。
	fileDeliveryProhibitionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:do\s+not|don't|dont|never|no)[\s-]+` +
			`(?:send|attach(?:ment)?s?|deliver(?:y)?|share|download(?:s| link)?|` +
			`upload(?:s)?|file[\s-]+delivery)\b`),
		regexp.MustCompile(`(?:不要|别|无需|不需要)(?:发送|发给我|上传|传文件|附上|附件|交付|分享|下载|给下载链接)`),
	}

	// fileDeliveryTools 文件投递工具集合。
	//
	// Python: _FILE_DELIVERY_TOOLS
	fileDeliveryTools = map[string]struct{}{
		"send_file_to_user": {},
		"upload_file":       {},
		"upload_photo":      {},
	}

	// fileDeliveryArgKeys 文件投递相关参数键集合。
	//
	// Python: _FILE_DELIVERY_ARG_KEYS
	fileDeliveryArgKeys = map[string]struct{}{
		"attachment":     {},
		"attachments":    {},
		"file":           {},
		"files":          {},
		"file_path":      {},
		"file_paths":     {},
		"abs_file_path":  {},
		"abs_file_paths": {},
		"download_link":  {},
		"download_links": {},
		"download_url":   {},
		"download_urls":  {},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// HasUserFileDeliveryProhibition 识别明确的文件投递禁止（英文或中文）。
//
// Python: has_user_file_delivery_prohibition(text)
func HasUserFileDeliveryProhibition(text string) bool {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return false
	}
	for _, pattern := range fileDeliveryProhibitionPatterns {
		if pattern.MatchString(normalized) {
			return true
		}
	}
	return false
}

// IsFileDeliveryAction 本次动作是否在结构上传输本地文件。
//
// Python: is_file_delivery_action(facts)
// 三条判定：投递类工具直接命中 / send_message 看读路径或参数 / 其余看副作用与上传特征。
func IsFileDeliveryAction(facts permissions.ToolDecisionFacts) bool {
	toolName := facts.Capability.ToolName
	if _, isDeliveryTool := fileDeliveryTools[toolName]; isDeliveryTool {
		return true
	}
	if toolName == sendMessageToolName {
		return len(facts.ReadPaths) > 0 || hasFileDeliveryField(facts.UntrustedArgs)
	}
	if _, hasExternalSend := facts.Capability.StaticSideEffects[externalSendSideEffect]; !hasExternalSend {
		return false
	}
	if len(facts.ReadPaths) > 0 {
		return true
	}
	return permissions.InspectNetworkScope(facts).UploadLike
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// hasFileDeliveryField 递归检查参数里是否含投递字段。
//
// Python: _has_file_delivery_field(value)
// map 分支：键在投递键集合且值非空，或嵌套值递归命中。
func hasFileDeliveryField(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for rawKey, nested := range typed {
			if _, isDeliveryKey := fileDeliveryArgKeys[anyToStringValue(rawKey)]; isDeliveryKey &&
				hasDeliveryValue(nested) {
				return true
			}
			if hasFileDeliveryField(nested) {
				return true
			}
		}
		return false
	case []any:
		for _, item := range typed {
			if hasFileDeliveryField(item) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// hasDeliveryValue 投递字段的值是否「有内容」。
//
// Python: _has_delivery_value(value)
// nil 为假；字符串看去空白后是否非空；容器看是否非空；其余看真值。
func hasDeliveryValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case map[string]any:
		return len(typed) > 0
	case []any:
		return len(typed) > 0
	case bool:
		return typed
	case int:
		return typed != 0
	case float64:
		return typed != 0
	default:
		return true
	}
}

// requestedFilePaths 把读路径归一成绝对路径。
//
// Python: _requested_file_paths(facts)
// Path(path).expanduser().resolve(strict=False)
func requestedFilePaths(facts permissions.ToolDecisionFacts) []string {
	out := make([]string, 0, len(facts.ReadPaths))
	for _, rawPath := range facts.ReadPaths {
		expanded := pathutil.ExpandHome(rawPath)
		if resolved, err := permissions.ResolvePathStrictFalse(expanded); err == nil {
			out = append(out, resolved)
			continue
		}
		out = append(out, filepath.Clean(expanded))
	}
	return out
}

// sendFileExecutionGrantForAllow 为「已放行」的调用解析当前任务的精确授权。
//
// Python: _send_file_execution_grant_for_allow(facts)
// 返回 (grant, 错误原因码)；原因码非空表示校验失败。
func sendFileExecutionGrantForAllow(
	ctx context.Context,
	facts permissions.ToolDecisionFacts,
) (*permissions.SendFileExecutionGrant, string) {
	if facts.Capability.ToolName != sendFileToolNameForGrant {
		return nil, ""
	}
	requestedPaths := requestedFilePaths(facts)
	if len(requestedPaths) == 0 {
		return nil, "send_file_authorization_missing_path"
	}
	expected := permissions.CreateSendFileExecutionGrant(
		requestedPaths, facts.UntrustedArgs["target_channels"])
	grant := permissions.CurrentSendFileExecutionGrant(ctx)
	if grant == nil {
		return nil, "send_file_authorization_missing"
	}
	if !reflect.DeepEqual(*grant, expected) {
		return nil, "send_file_authorization_mismatch"
	}
	return grant, ""
}

// issueSendFileAuthorization 为已批准的 send_file 调用签发精确授权。
//
// Python: AutoPermissionArtifactAuthorizationMixin._issue_send_file_authorization(facts)
// 返回 (失败原因码, 派生 ctx)。原因码为空表示成功或无需授权。
//
// 与 Python 的签名差异（偏差 V-13 的延伸）：Python 的
// `publish_send_file_execution_grant` 写 ContextVar 全局生效，调用方无需接收
// 返回值；Go 的 `PublishSendFileExecutionGrant` 返回携带授权的新 ctx，
// **必须由调用方继续使用**，否则授权丢失。故多返回一个 ctx。
func issueSendFileAuthorization(
	ctx context.Context,
	facts permissions.ToolDecisionFacts,
) (string, context.Context) {
	if facts.Capability.ToolName != sendFileToolNameForGrant {
		return "", ctx
	}
	requestedPaths := requestedFilePaths(facts)
	if len(requestedPaths) == 0 {
		return "send_file_authorization_missing_path", ctx
	}
	// Python 捕获 create 抛出的 ValueError；Go 侧该函数 panic，用 recover 等价
	reason := ""
	func() {
		defer func() {
			if recover() != nil {
				reason = "send_file_authorization_not_file"
			}
		}()
		grant := permissions.CreateSendFileExecutionGrant(
			requestedPaths, facts.UntrustedArgs["target_channels"])
		ctx = permissions.PublishSendFileExecutionGrant(ctx, grant)
	}()
	return reason, ctx
}
