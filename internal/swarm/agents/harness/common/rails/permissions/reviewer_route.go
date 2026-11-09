// 无状态语义审查器的紧凑 Host 路由。
//
// 对应 Python 代码：permissions/reviewer_route.py（330 行 / 4 导出 + 8 私有）
//
// 职责：对一次已完成策略评估的调用，给出唯一的 Host 路由——
//   - 硬拒绝（策略 deny / 守卫 deny / 策略既非 ask 也非 allow）
//   - 人工（结构性问题、领域策略、执行提供方未验证、文件投递受限、搜索查询缺失）
//   - 语义审查（其余需要 LLM 判断的场景）
//   - 直接放行（仅当 URL 证据来自近期检索结果这一条确定性路径）
//
// DecisionRoute 的 is_hard_block / requires_manual 在 Go 侧用 Source 判定
// （Python 是 property，分别等价于 source == "hard_guard" / "manual_only"）。

package permissions

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SemanticReviewSource 语义审查来源
	SemanticReviewSource = "semantic_reviewer"
	// ManualReviewSource 仅人工来源
	ManualReviewSource = "manual_only"
	// HardBlockSource 硬拦截来源
	HardBlockSource = "hard_guard"
	// RecentFetchSource 近期检索结果来源
	RecentFetchSource = "recent_search_result"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// AllowableReviewerOutcomes 审查器允许的输出结局。
	//
	// Python: ALLOWABLE_REVIEWER_OUTCOMES
	AllowableReviewerOutcomes = []string{"allow_once", "manual", "deny"}

	// workspaceWriteTools 工作区写入工具集合。
	//
	// Python: WORKSPACE_WRITE_TOOLS
	workspaceWriteTools = map[string]struct{}{
		"apply_patch":     {},
		"write_file":      {},
		"edit_file":       {},
		"write_text_file": {},
		"write":           {},
		"search_replace":  {},
	}

	// userPathTools 用户路径工具集合。
	//
	// Python: USER_PATH_TOOLS
	userPathTools = map[string]struct{}{
		"glob":             {},
		"glob_file_search": {},
		"grep":             {},
		"list_dir":         {},
		"list_files":       {},
		"read":             {},
		"read_file":        {},
		"read_text_file":   {},
		"write_file":       {},
		"write_text_file":  {},
	}

	// DefaultDeliveryExcludedPaths 文件投递默认排除路径。
	//
	// Python: DEFAULT_DELIVERY_EXCLUDED_PATHS
	DefaultDeliveryExcludedPaths = []string{
		".git", ".github", ".gitlab-ci.yml", ".circleci", ".buildkite",
		".envrc", ".env", ".env.local", ".ssh", ".bashrc", ".bash_profile",
		".zshrc", ".zprofile", ".profile", ".config/fish/config.fish",
		".netrc", ".npmrc", ".pypirc", "AGENTS.md", "CLAUDE.md", "Dockerfile",
		"Jenkinsfile", "Cargo.lock", "Cargo.toml", "Gemfile", "Gemfile.lock",
		"Pipfile", "Pipfile.lock", "bun.lock", "bun.lockb", "composer.json",
		"composer.lock", "go.mod", "go.sum", "package-lock.json", "package.json",
		"pnpm-lock.yaml", "poetry.lock", "pyproject.toml", "requirements",
		"requirements-dev.txt", "requirements.txt", "setup.cfg", "setup.py",
		"uv.lock", "yarn.lock", "approval_overrides", "config/config.yaml",
		"config/permissions.tools", "config/permissions.yaml", "deploy",
		"deployment", "docker-compose.yaml", "docker-compose.yml", "helm", "k8s",
		"permissions.tools", "permissions.yaml",
	}

	// searchQuerySecretKeyPattern 搜索查询里的密钥键名。
	searchQuerySecretKeyPattern = regexp.MustCompile(
		`['"]?\b(?:api[_\s-]*key|apikey|authorization|bearer|password|passwd|secret|token)\b['"]?\s*[:=]`)

	// searchQuerySecretAssignPattern 密钥键后跟值。
	searchQuerySecretAssignPattern = regexp.MustCompile(
		`\b(?:api[_\s-]*key|apikey|authorization|bearer|password|passwd|secret|token)\b\s+(?:is\s+)?['"]?` +
			`(?:sk-(?:proj-)?[a-z0-9_-]{12,}|ghp_[a-z0-9_]{20,}|` +
			`github_pat_[a-z0-9_]{20,}|akia[a-z0-9]{16}|` +
			`[a-z0-9][a-z0-9_.-]{19,})`)

	// searchQuerySKTokenPattern sk- 前缀令牌
	searchQuerySKTokenPattern = regexp.MustCompile(`\bsk-(?:proj-)?[a-z0-9_-]{12,}\b`)
	// searchQueryGHTokenPattern GitHub 令牌
	searchQueryGHTokenPattern = regexp.MustCompile(`\b(?:ghp|github_pat)_[a-z0-9_]{20,}\b`)
	// searchQueryAKIATokenPattern AWS AKIA 令牌
	searchQueryAKIATokenPattern = regexp.MustCompile(`\bakia[a-z0-9]{16}\b`)
	// whitespaceRunPattern 连续空白
	whitespaceRunPattern = regexp.MustCompile(`\s+`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ReviewerRoute 返回一次已完成策略评估调用的唯一 Host 路由。
//
// Python: reviewer_route(facts, *, policy_level, guard_result, workspace_root,
// delivery_max_files=3, delivery_excluded_paths=(), original_user_intent=None,
// domain_route=None, recent_url_sources=())
func ReviewerRoute(
	facts ToolDecisionFacts,
	policyLevel string,
	guardResult string,
	workspaceRoot string,
	deliveryMaxFiles int,
	deliveryExcludedPaths []string,
	originalUserIntent *OriginalUserIntentEvidence,
	domainRoute *DecisionRoute,
	recentURLSources []RecentURLSource,
) DecisionRoute {
	policy := strings.ToLower(strings.TrimSpace(policyLevel))
	guard := strings.ToLower(strings.TrimSpace(guardResult))

	if policy == AutoDecisionDenyLevel {
		return reviewerDeny("policy_deny")
	}
	if guard == AutoDecisionDenyLevel {
		return reviewerDeny("guard_deny")
	}
	if policy != AutoDecisionAskLevel && policy != AutoDecisionAllowLevel && guard != "scoped_candidate" {
		return reviewerDeny("policy_not_ask")
	}
	if guard == "terminal_manual" {
		return reviewerManual("terminal_manual")
	}

	if reason := structuralManualReason(facts); reason != "" {
		return reviewerManual(reason)
	}

	if domainRoute != nil {
		if domainRoute.Source == HardBlockSource {
			if domainRoute.Reason != "" {
				return reviewerDeny(domainRoute.Reason)
			}
			return reviewerDeny("domain_policy_deny")
		}
		if domainRoute.Source == ManualReviewSource {
			if domainRoute.Reason != "" {
				return reviewerManual(domainRoute.Reason)
			}
			return reviewerManual("domain_policy_manual")
		}
	}

	if RequiresManualExecutionProviderReview(facts.Capability.ToolName, facts.Capability.Category) {
		return reviewerManual(ExecutionProviderContractUnverified)
	}

	if facts.Capability.ToolName == "send_file_to_user" {
		if reason := FileDeliveryManualReason(
			facts, workspaceRoot, deliveryMaxFiles, deliveryExcludedPaths); reason != "" {
			return reviewerManual(reason)
		}
	}

	if isWorkspaceWrite(facts) {
		if workspaceRoot == "" {
			return reviewerManual("bounded_write_workspace_missing")
		}
		return reviewerSemantic()
	}
	if _, isUserPathTool := userPathTools[facts.Capability.ToolName]; isUserPathTool && len(facts.ExternalPaths) > 0 {
		return reviewerSemantic()
	}

	if isPublicSearch(facts) {
		query := strings.TrimSpace(anyToStringValue(facts.UntrustedArgs["query"]))
		if query == "" {
			return reviewerManual("search_query_missing")
		}
		if searchQueryIsSecretLike(query) {
			return reviewerDeny("search_query_secret_like")
		}
	}

	if isPublicFetch(facts) {
		urlDecision := EvaluateReviewableURLScope(facts, originalUserIntent, recentURLSources)
		if urlDecision.HardBlock {
			return reviewerDeny(urlDecision.Reason)
		}
		if !urlDecision.Accepted {
			if urlDecision.Reason != "" {
				return reviewerManual(urlDecision.Reason)
			}
			return reviewerManual("network_url_scope_rejected")
		}
		sourceKind := anyToStringValue(urlDecision.EvidenceSummary["source_kind"])
		if sourceKind == RecentFetchSource {
			return DecisionRoute{
				Level:  AutoDecisionAllowLevel,
				Reason: "deterministic_readonly_public_web",
				Source: RecentFetchSource,
			}
		}
	}

	return reviewerSemantic()
}

// FileDeliveryManualReason 返回文件投递需要人工的原因，空串表示无需人工。
//
// Python: file_delivery_manual_reason(facts, *, workspace_root, max_files, excluded_paths)
func FileDeliveryManualReason(
	facts ToolDecisionFacts,
	workspaceRoot string,
	maxFiles int,
	excludedPaths []string,
) string {
	if workspaceRoot == "" {
		return "bounded_user_file_delivery_workspace_missing"
	}
	readPaths := facts.ReadPaths
	if len(readPaths) == 0 {
		return "bounded_user_file_delivery_missing_path"
	}
	if len(facts.ExternalPaths) > 0 {
		return "bounded_user_file_delivery_external_path"
	}
	if len(readPaths) > maxFiles {
		return "bounded_user_file_delivery_too_many_files"
	}
	root, err := resolvePathStrictFalseWithHome(workspaceRoot)
	if err != nil {
		root = workspaceRoot
	}
	exclusions := append(append([]string{}, DefaultDeliveryExcludedPaths...), excludedPaths...)
	for _, rawPath := range readPaths {
		candidate, err := resolvePathStrictFalseWithHome(rawPath)
		if err != nil {
			candidate = rawPath
		}
		for _, excluded := range exclusions {
			if pathMatchesExclusion(candidate, excluded, root) {
				return "bounded_user_file_delivery_excluded_path"
			}
		}
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// structuralManualReason 返回结构性人工原因，空串表示无。
//
// Python: _structural_manual_reason(facts)
func structuralManualReason(facts ToolDecisionFacts) string {
	if !facts.ArgumentsValidObject {
		return "arguments_not_object"
	}
	category := facts.Capability.Category
	if (category == "path" || category == "shell") && !facts.AccessesKnown {
		return "core_accesses_unknown"
	}
	if facts.Capability.AliasConflict {
		return "alias_conflict"
	}
	if facts.Capability.FactsSource != "host_static" {
		return "facts_source_unverified"
	}
	if facts.Capability.OperationFamily == "unknown" || category == "unknown" {
		return "unknown_operation"
	}
	return ""
}

// isPublicSearch 是否公开搜索。
//
// Python: _is_public_search(facts)
func isPublicSearch(facts ToolDecisionFacts) bool {
	return facts.Capability.OperationFamily == "public_search" &&
		facts.Capability.Category == "network"
}

// isWorkspaceWrite 是否工作区写入（中风险 + 有写路径）。
//
// Python: _is_workspace_write(facts)
func isWorkspaceWrite(facts ToolDecisionFacts) bool {
	if _, ok := workspaceWriteTools[facts.Capability.ToolName]; !ok {
		return false
	}
	return facts.Capability.Category == "path" &&
		facts.Capability.RiskTier == "medium" &&
		len(facts.WritePaths) > 0
}

// pathMatchesExclusion 候选路径是否命中排除项。
//
// Python: _path_matches_exclusion(candidate, *, excluded_path, workspace_root)
// 三级判定：绝对路径前缀 → 相对工作区路径 → 大小写无关的分段子序列匹配。
func pathMatchesExclusion(candidate, excludedPath, workspaceRoot string) bool {
	text := strings.TrimSpace(excludedPath)
	if text == "" {
		return false
	}
	excluded := expandHomeString(text)
	if filepath.IsAbs(excluded) {
		resolved, err := resolvePathStrictFalseWithHome(excluded)
		if err != nil {
			resolved = excluded
		}
		return isPathWithin(candidate, resolved)
	}
	joined, err := resolvePathStrictFalseWithHome(filepath.Join(workspaceRoot, excluded))
	if err != nil {
		joined = filepath.Join(workspaceRoot, excluded)
	}
	if isPathWithin(candidate, joined) {
		return true
	}
	candidateParts := splitPathCasefold(candidate)
	normalizedExcluded := strings.ReplaceAll(text, `\`, "/")
	normalizedExcluded = strings.Trim(normalizedExcluded, "/")
	excludedParts := splitPathCasefold(normalizedExcluded)
	if len(excludedParts) == 1 {
		return containsString(candidateParts, excludedParts[0])
	}
	width := len(excludedParts)
	if width == 0 {
		return false
	}
	for index := 0; index+width <= len(candidateParts); index++ {
		if equalStringSlices(candidateParts[index:index+width], excludedParts) {
			return true
		}
	}
	return false
}

// isPublicFetch 是否公开网页读取。
//
// Python: _is_public_fetch(facts)
func isPublicFetch(facts ToolDecisionFacts) bool {
	return facts.Capability.OperationFamily == "public_web_read" &&
		facts.Capability.Category == "network"
}

// searchQueryIsSecretLike 搜索查询是否像密钥。
//
// Python: _search_query_is_secret_like(query)
// NFKC 归一 → 两轮 percent-decode → 小写合并空白 → 五条正则。
func searchQueryIsSecretLike(query string) bool {
	text := norm.NFKC.String(query)
	for i := 0; i < 2; i++ {
		decoded, err := percentDecode(text)
		if err != nil || decoded == text {
			break
		}
		text = decoded
	}
	normalized := strings.TrimSpace(whitespaceRunPattern.ReplaceAllString(strings.ToLower(text), " "))
	return searchQuerySecretKeyPattern.MatchString(normalized) ||
		searchQuerySecretAssignPattern.MatchString(normalized) ||
		searchQuerySKTokenPattern.MatchString(normalized) ||
		searchQueryGHTokenPattern.MatchString(normalized) ||
		searchQueryAKIATokenPattern.MatchString(normalized)
}

// reviewerManual 构造人工路由。
//
// Python: _manual(reason)
func reviewerManual(reason string) DecisionRoute {
	return DecisionRoute{Level: AutoDecisionAskLevel, Reason: reason, Source: ManualReviewSource}
}

// reviewerDeny 构造硬拒绝路由。
//
// Python: _deny(reason)
func reviewerDeny(reason string) DecisionRoute {
	return DecisionRoute{Level: AutoDecisionDenyLevel, Reason: reason, Source: HardBlockSource}
}

// reviewerSemantic 构造语义审查路由。
//
// Python: _semantic()
func reviewerSemantic() DecisionRoute {
	return DecisionRoute{
		Level:  AutoDecisionAskLevel,
		Reason: "semantic_review_required",
		Source: SemanticReviewSource,
	}
}

// percentDecode percent-decode 字符串（Python urllib.parse.unquote 等价）。
//
// 与 QueryUnescape 的区别：不把 "+" 转成空格（Python unquote 同样不转）。
func percentDecode(text string) (string, error) {
	return url.PathUnescape(text)
}

// resolvePathStrictFalseWithHome 展开 ~ 并取绝对路径（Path.expanduser().resolve(strict=False)）。
func resolvePathStrictFalseWithHome(raw string) (string, error) {
	return resolveUserPathStrictFalse(raw)
}

// ResolvePathStrictFalse 展开 ~ 并取绝对路径（供子包复用）。
//
// Python: Path(raw).expanduser().resolve(strict=False)
func ResolvePathStrictFalse(raw string) (string, error) {
	return resolveUserPathStrictFalse(raw)
}

// isPathWithin 判断 candidate 是否在 root 之下。
//
// Python: Path.is_relative_to
func isPathWithin(candidate, root string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// splitPathCasefold 按分隔符切分并转小写。
func splitPathCasefold(path string) []string {
	normalized := strings.ReplaceAll(path, `\`, "/")
	parts := strings.Split(normalized, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, strings.ToLower(part))
	}
	return out
}

// equalStringSlices 两个字符串切片是否逐项相等。
func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
