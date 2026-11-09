// 把多路径文件投递适配到 OpenJiuwen file_guard 的读判定。
//
// 对应 Python 代码：
// jiuwenswarm/agents/harness/common/rails/permissions/send_file_path_guard.py（315 行）
//
// 职责：send_file_to_user 一次可带多个源路径，本模块把每个规范化后的源路径
// 逐个送进 file_guard 的读判定，聚合成一条「最严」结论（allow/ask/deny），
// 并收集命中规则与需要询问的访问项。
//
// 品牌映射：Python SEND_FILE_PATH_ARGUMENT = "__jiuwenswarm_file_guard_read_path"
// → Go SendFilePathArgument = "__uapclawswarm_file_guard_read_path"
//（与 UAPCLAW_DATA_DIR 等同款映射，见 9.90d-1 spec §12.8）。
//
// 依赖：agentcore/harness/security/fileguard 的 BuildFileGuardChecker /
// FileToolSpec / LookupFileToolSpecs / RegisterFileTool（9.90b 已落地）。

package permissions

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/fileguard"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SendFileReadAccess 一个需要询问的读访问项。
//
// Python: FileGuardReadAccess = tuple[str, Literal["read"]]
type SendFileReadAccess struct {
	// Path 规范化路径
	Path string
	// Action 访问动作（恒为 read）
	Action string
}

// SendFilePathGuardResult 所有 send 源路径判定的不可变聚合。
//
// Python: @dataclass(frozen=True) SendFilePathGuardResult
type SendFilePathGuardResult struct {
	// Status 评估状态（neutral / evaluated / unevaluable）
	Status string
	// Level 判定级别（neutral / allow / ask / deny）
	Level string
	// Reason 原因
	Reason string
	// CanonicalPaths 规范化路径
	CanonicalPaths []string
	// MatchedRules 命中的规则
	MatchedRules []string
	// AskAccesses 需要询问的读访问
	AskAccesses []SendFileReadAccess
}

// SendFilePathGuardEvaluator 把每个规范化后的 send 源路径送进路径策略评估。
//
// Python: class SendFilePathGuardEvaluator
// Python 注释说明 evaluate 保持实例方法（evaluator 替换是宿主接缝的一部分），
// Go 同样用方法而非包级函数。
type SendFilePathGuardEvaluator struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SendFileToolName send_file 工具名
	SendFileToolName = "send_file_to_user"
	// SendFilePathArgument 原生 schema 里承载路径的参数名
	// Python: SEND_FILE_PATH_ARGUMENT（品牌映射 jiuwenswarm→uapclawswarm）
	SendFilePathArgument = "__uapclawswarm_file_guard_read_path"

	// SendFileGuardStatusNeutral 无意见
	SendFileGuardStatusNeutral = "neutral"
	// SendFileGuardStatusEvaluated 已评估
	SendFileGuardStatusEvaluated = "evaluated"
	// SendFileGuardStatusUnevaluable 不可评估
	SendFileGuardStatusUnevaluable = "unevaluable"

	// SendFileGuardLevelNeutral 无意见级别
	SendFileGuardLevelNeutral = "neutral"

	// sendFileGuardLogComponent 日志组件
	sendFileGuardLogComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// nativeSendFileSpec 原生 send_file schema
	// Python: _NATIVE_SEND_FILE_SPEC = FileToolSpec(SEND_FILE_TOOL_NAME, SEND_FILE_PATH_ARGUMENT, "read")
	nativeSendFileSpec = fileguard.FileToolSpec{
		ToolName: SendFileToolName,
		ArgName:  SendFilePathArgument,
		Action:   fileguard.FileActionRead,
	}

	// nativeSendFileSchemaRegistered 原生 schema 是否已注册成功
	// Python: _NATIVE_SEND_FILE_SCHEMA_REGISTERED = _register_native_send_file_schema()
	nativeSendFileSchemaRegistered bool

	// sendFileGuardPrecedence 级别优先级（越大越严）
	sendFileGuardPrecedence = map[string]int{"allow": 0, "ask": 1, "deny": 2}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSendFilePathGuardEvaluator 构造评估器。
func NewSendFilePathGuardEvaluator() *SendFilePathGuardEvaluator {
	return &SendFilePathGuardEvaluator{}
}

// Evaluate 返回最严的 file_guard 读判定。
//
// Python: SendFilePathGuardEvaluator.evaluate(*, permissions, workspace_root,
// trusted_dirs, raw_paths)
// 步骤与 Python 逐条对应：路径解析 → permissions 校验 → workspace/trusted
// 规范化 → checker 构造 → mode 分派 → 逐路径评估聚合。
func (e *SendFilePathGuardEvaluator) Evaluate(
	permissions map[string]any,
	workspaceRoot any,
	trustedDirs any,
	rawPaths any,
) SendFilePathGuardResult {
	canonicalPaths, parseError := canonicalSendPaths(rawPaths)
	if parseError != "" {
		return unevaluable(parseError, nil)
	}
	if permissions == nil {
		return unevaluable("send_file_path_guard_permissions_unavailable", canonicalPaths)
	}

	workspace, workspaceError := canonicalWorkspace(workspaceRoot)
	trusted, trustedError := canonicalTrustedDirs(trustedDirs)
	if workspaceError != "" || trustedError != "" {
		reason := workspaceError
		if reason == "" {
			reason = trustedError
		}
		return unevaluable(reason, canonicalPaths)
	}

	checker := buildSendFileGuardChecker(permissions, workspace, trusted)
	if checker == nil {
		return SendFilePathGuardResult{
			Status:         SendFileGuardStatusNeutral,
			Level:          SendFileGuardLevelNeutral,
			Reason:         "send_file_path_guard_disabled",
			CanonicalPaths: canonicalPaths,
		}
	}
	if workspace == "" {
		return unevaluable("send_file_path_guard_workspace_unavailable", canonicalPaths)
	}

	var toolName, argumentName string
	switch strings.ToLower(strings.TrimSpace(checker.Mode())) {
	case "native":
		if !nativeSendFileSchemaRegistered || !nativeSendFileSchemaIsValid() {
			return unevaluable("send_file_path_guard_native_schema_unavailable", canonicalPaths)
		}
		toolName = SendFileToolName
		argumentName = SendFilePathArgument
	case "legacy":
		toolName = "read_file"
		argumentName = "file_path"
	default:
		return unevaluable("send_file_path_guard_mode_unknown", canonicalPaths)
	}

	return evaluateSendFilePaths(checker, canonicalPaths, toolName, argumentName)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册原生 send_file schema（对齐 Python 模块级
// `_NATIVE_SEND_FILE_SCHEMA_REGISTERED = _register_native_send_file_schema()`）。
func init() {
	nativeSendFileSchemaRegistered = registerNativeSendFileSchema()
}

// registerNativeSendFileSchema 注册防冲突的单读 schema，且只注册一次。
//
// Python: _register_native_send_file_schema()
// 已存在同 arg_name 的 spec 时不重复注册，直接返回是否与原生 spec 相等。
func registerNativeSendFileSchema() (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(sendFileGuardLogComponent).
				Err(fmt.Errorf("%v", r)).
				Msg("SendFilePathGuard native schema registration failed")
			ok = false
		}
	}()
	for _, spec := range lookupSendFileSpecs() {
		if spec.ArgName != SendFilePathArgument {
			continue
		}
		return spec == nativeSendFileSpec
	}
	fileguard.RegisterFileTool(nativeSendFileSpec)
	return nativeSendFileSchemaIsValid()
}

// nativeSendFileSchemaIsValid 校验原生 schema 恰好注册为一条且内容一致。
//
// Python: _native_send_file_schema_is_valid()
func nativeSendFileSchemaIsValid() (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(sendFileGuardLogComponent).
				Err(fmt.Errorf("%v", r)).
				Msg("SendFilePathGuard native schema lookup failed")
			ok = false
		}
	}()
	matches := []fileguard.FileToolSpec{}
	for _, spec := range lookupSendFileSpecs() {
		if spec.ArgName == SendFilePathArgument {
			matches = append(matches, spec)
		}
	}
	return len(matches) == 1 && matches[0] == nativeSendFileSpec
}

// canonicalSendPaths 把任意输入归一为去重保序的规范化路径列表。
//
// Python: _canonical_send_paths(raw_paths) -> (tuple[str, ...], str)
// 第二个返回值非空表示失败原因。
func canonicalSendPaths(rawPaths any) ([]string, string) {
	parsed := rawPaths
	if s, ok := rawPaths.(string); ok {
		stripped := strings.TrimSpace(s)
		if stripped == "" {
			return nil, "send_file_path_guard_paths_empty"
		}
		if strings.HasPrefix(stripped, "[") {
			var decoded any
			if err := json.Unmarshal([]byte(stripped), &decoded); err != nil {
				return nil, "send_file_path_guard_paths_invalid_json"
			}
			parsed = decoded
		} else {
			parsed = stripped
		}
	}

	var candidates []any
	switch v := parsed.(type) {
	case string:
		candidates = []any{v}
	case []any:
		candidates = v
	case []string:
		candidates = make([]any, len(v))
		for i, item := range v {
			candidates[i] = item
		}
	default:
		return nil, "send_file_path_guard_paths_invalid_type"
	}
	if len(candidates) == 0 {
		return nil, "send_file_path_guard_paths_empty"
	}

	canonical := []string{}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		text, ok := candidate.(string)
		if !ok {
			return nil, "send_file_path_guard_path_not_string"
		}
		stripped := strings.TrimSpace(text)
		if stripped == "" {
			return nil, "send_file_path_guard_path_empty"
		}
		if strings.ContainsRune(stripped, 0) {
			return nil, "send_file_path_guard_path_contains_nul"
		}
		resolved, resolveErr := resolveSendFilePathStrictFalse(stripped)
		if resolveErr != nil {
			return nil, "send_file_path_guard_path_unresolvable"
		}
		if !seen[resolved] {
			seen[resolved] = true
			canonical = append(canonical, resolved)
		}
	}
	return canonical, ""
}

// canonicalWorkspace 规范化工作区根。
//
// Python: _canonical_workspace(workspace_root) -> (Path | None, str)
// 返回空串表示 nil。
func canonicalWorkspace(workspaceRoot any) (string, string) {
	if workspaceRoot == nil {
		return "", ""
	}
	text := strings.TrimSpace(anyToStringValue(workspaceRoot))
	if text == "" {
		return "", ""
	}
	resolved, err := resolveSendFilePathStrictFalse(text)
	if err != nil {
		return "", "send_file_path_guard_workspace_unresolvable"
	}
	return resolved, ""
}

// canonicalTrustedDirs 规范化受信目录列表。
//
// Python: _canonical_trusted_dirs(trusted_dirs) -> (tuple[Path, ...], str)
// 单个字符串视为非法（Python 显式拒绝 str/bytes）。
func canonicalTrustedDirs(trustedDirs any) ([]string, string) {
	if trustedDirs == nil {
		return nil, ""
	}
	var raw []any
	switch v := trustedDirs.(type) {
	case string:
		return nil, "send_file_path_guard_trusted_dirs_invalid"
	case []any:
		raw = v
	case []string:
		raw = make([]any, len(v))
		for i, item := range v {
			raw[i] = item
		}
	default:
		return nil, "send_file_path_guard_trusted_dirs_invalid"
	}
	trusted := make([]string, 0, len(raw))
	for _, item := range raw {
		resolved, err := resolveSendFilePathStrictFalse(anyToStringValue(item))
		if err != nil {
			return nil, "send_file_path_guard_trusted_dirs_unresolvable"
		}
		trusted = append(trusted, resolved)
	}
	return trusted, ""
}

// evaluateSendFilePaths 逐路径评估并聚合最严结论。
//
// Python: _evaluate_paths(checker, canonical_paths, *, tool_name, argument_name)
func evaluateSendFilePaths(
	checker *fileguard.FileGuardChecker,
	canonicalPaths []string,
	toolName, argumentName string,
) SendFilePathGuardResult {
	strictestLevel := "allow"
	strictestReason := "send_file_path_guard_allowed"
	matchedRules := []string{}
	askAccesses := []SendFileReadAccess{}
	strictestRank := 0

	for _, path := range canonicalPaths {
		result, failed := evaluateSendFilePathSafely(checker, toolName, argumentName, path)
		if failed {
			// evaluate 抛异常的分支（Python 的 except 分支）
			return unevaluable("send_file_path_guard_evaluation_failed", canonicalPaths)
		}
		level := permissionResultLevel(result)
		if level == "" && result == nil {
			// OpenJiuwen 在全部访问都是 ALLOW 或路径层无异议时返回 None
			level = "allow"
		} else if _, ok := sendFileGuardPrecedence[level]; !ok {
			return unevaluable("send_file_path_guard_result_unknown", canonicalPaths)
		}
		levelRank := sendFileGuardPrecedence[level]
		if level == "ask" {
			askAccesses = append(askAccesses, SendFileReadAccess{Path: path, Action: "read"})
		}
		// result 可能为 nil（合法 allow 结果）。Python 用 getattr 兜住 None，
		// Go 必须显式判空，否则 nil 解引用。
		matchedRule := ""
		resultReason := ""
		if result != nil {
			matchedRule = result.MatchedRule
			resultReason = result.Reason
		}
		if matchedRule != "" && !containsString(matchedRules, matchedRule) {
			matchedRules = append(matchedRules, matchedRule)
		}
		if levelRank > strictestRank {
			strictestLevel = level
			strictestRank = levelRank
			strictestReason = resultReason
			if strictestReason == "" {
				strictestReason = "send_file_path_guard_" + level
			}
		}
	}

	return SendFilePathGuardResult{
		Status:         SendFileGuardStatusEvaluated,
		Level:          strictestLevel,
		Reason:         strictestReason,
		CanonicalPaths: canonicalPaths,
		MatchedRules:   matchedRules,
		AskAccesses:    askAccesses,
	}
}

// permissionResultLevel 从权限结果取级别字符串；nil 结果返回空串。
//
// Python: _permission_result_level(result)
// Python 的 `str(value or "").strip().lower()` 对 None 结果返回 None（不是 ""），
// Go 用 result == nil 区分：调用方据此判断是否走 allow 兜底。
func permissionResultLevel(result *security.PermissionResult) string {
	if result == nil {
		return ""
	}
	switch result.Permission {
	case security.PermissionLevelAllow:
		return "allow"
	case security.PermissionLevelAsk:
		return "ask"
	case security.PermissionLevelDeny:
		return "deny"
	default:
		return ""
	}
}

// unevaluable 构造不可评估结果。
//
// Python: _unevaluable(reason, canonical_paths=())
func unevaluable(reason string, canonicalPaths []string) SendFilePathGuardResult {
	return SendFilePathGuardResult{
		Status:         SendFileGuardStatusUnevaluable,
		Level:          "ask",
		Reason:         reason,
		CanonicalPaths: canonicalPaths,
	}
}

// ──────────────────────────── 辅助 ────────────────────────────

// evaluateSendFilePathSafely 调 checker.Evaluate 并捕获 panic。
//
// Python 用 try/except 包住 checker.evaluate；Go 对应 defer/recover。
// 第二个返回值 failed 表示是否因异常失败（区别于「合法的 nil 结果」）。
func evaluateSendFilePathSafely(
	checker *fileguard.FileGuardChecker,
	toolName, argumentName, path string,
) (result *security.PermissionResult, failed bool) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(sendFileGuardLogComponent).
				Err(fmt.Errorf("%v", r)).
				Msg("SendFilePathGuard path evaluation failed")
			failed = true
			result = nil
		}
	}()
	return checker.Evaluate(toolName, map[string]any{argumentName: path}), false
}

// buildSendFileGuardChecker 构造 checker 并捕获 panic（Python try/except）。
func buildSendFileGuardChecker(
	permissions map[string]any,
	workspace string,
	trusted []string,
) (checker *fileguard.FileGuardChecker) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(sendFileGuardLogComponent).
				Err(fmt.Errorf("%v", r)).
				Msg("SendFilePathGuard checker construction failed")
			checker = nil
		}
	}()
	return fileguard.BuildFileGuardChecker(permissions, workspace, trusted)
}

// lookupSendFileSpecs 查询 send_file 的已注册 spec（异常时返回 nil）。
func lookupSendFileSpecs() []fileguard.FileToolSpec {
	return fileguard.LookupFileToolSpecs(SendFileToolName)
}

// resolveSendFilePathStrictFalse Path.expanduser().resolve(strict=False) 等价。
func resolveSendFilePathStrictFalse(value string) (string, error) {
	expanded := expandHomeString(strings.TrimSpace(value))
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Clean(abs)), nil
}
