package permissions

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/fileguard"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolDecisionFacts 一次已归一化根工具调用的宿主事实。
//
// 只承载**权威事实**；参数值本身仍不可信（UntrustedArgs）。
//
// Python: @dataclass(frozen=True, slots=True) ToolDecisionFacts
type ToolDecisionFacts struct {
	// Capability 工具能力分类
	Capability ToolCapability
	// UntrustedArgs 不可信参数副本
	UntrustedArgs map[string]any
	// ArgumentsValidObject 原始参数是否为合法对象
	ArgumentsValidObject bool
	// AccessesKnown 路径访问是否已知（false 表示无法判定，需按保守处理）
	AccessesKnown bool
	// ReadPaths 读路径（去重保序）
	ReadPaths []string
	// WritePaths 写路径（含 exec，去重保序）
	WritePaths []string
	// ExternalPaths 外部路径（去重保序）
	ExternalPaths []string
	// Command shell 命令文本（仅 shell 类非空）
	Command string
	// WorkspaceRoot 工作空间根（posix 形式）
	WorkspaceRoot string
	// PlatformTrustedRoot 平台受信根（posix 形式）
	PlatformTrustedRoot string
}

// DecisionRoute 一次紧凑的宿主路由结果。
//
// Python: @dataclass(frozen=True, slots=True) DecisionRoute
type DecisionRoute struct {
	// Level 判定级别字符串
	Level string
	// Reason 原因
	Reason string
	// Source 来源（hard_guard / manual_only / semantic_reviewer / ...）
	Source string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sourceHardGuard 硬拦截来源
	sourceHardGuard = "hard_guard"
	// sourceManualOnly 仅人工来源
	sourceManualOnly = "manual_only"
	// sourceSemanticReviewer 语义审查来源
	sourceSemanticReviewer = "semantic_reviewer"
	// sourceRecentSearchResult 近期搜索结果确定性放行来源
	sourceRecentSearchResult = "recent_search_result"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ToolName 返回能力分类里的工具名。
//
// Python: ToolDecisionFacts.tool_name（property）
func (f ToolDecisionFacts) ToolName() string { return f.Capability.ToolName }

// ToolCategory 返回能力分类的 category。
//
// Python: ToolDecisionFacts.tool_category（property）
func (f ToolDecisionFacts) ToolCategory() string { return f.Capability.Category }

// Paths 返回读路径与写路径的去重并集（保持首次出现顺序）。
//
// Python: ToolDecisionFacts.paths（property）—— tuple(dict.fromkeys((*reads, *writes)))
func (f ToolDecisionFacts) Paths() []string {
	seen := make(map[string]struct{}, len(f.ReadPaths)+len(f.WritePaths))
	out := make([]string, 0, len(f.ReadPaths)+len(f.WritePaths))
	for _, p := range f.ReadPaths {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	for _, p := range f.WritePaths {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// IsHardBlock 是否为硬拦截。
//
// Python: DecisionRoute.is_hard_block（property）
func (r DecisionRoute) IsHardBlock() bool { return r.Source == sourceHardGuard }

// RequiresManual 是否需要人工确认。
//
// Python: DecisionRoute.requires_manual（property）
func (r DecisionRoute) RequiresManual() bool { return r.Source == sourceManualOnly }

// RequiresReviewer 是否需要语义审查。
//
// Python: DecisionRoute.requires_reviewer（property）
func (r DecisionRoute) RequiresReviewer() bool { return r.Source == sourceSemanticReviewer }

// IsDeterministicAllow 是否为确定性放行。
//
// Python: DecisionRoute.is_deterministic_allow（property）
func (r DecisionRoute) IsDeterministicAllow() bool { return r.Source == sourceRecentSearchResult }

// Accepted 是否可接受（非硬拦截）。
//
// Python: DecisionRoute.accepted（property）
func (r DecisionRoute) Accepted() bool { return !r.IsHardBlock() }

// AllowedOutcomes 本路由允许的结果集。
//
// 三种情况（顺序对齐 Python allowed_outcomes）：
//  1. 需审查 或 确定性放行 → (allow_once, manual, deny)
//  2. 需人工 → (manual, deny)
//  3. 其他 → 空（由调用方按基线决定）
//
// Python: DecisionRoute.allowed_outcomes（property）
func (r DecisionRoute) AllowedOutcomes() []string {
	if r.RequiresReviewer() || r.IsDeterministicAllow() {
		return []string{"allow_once", "manual", "deny"}
	}
	if r.RequiresManual() {
		return []string{"manual", "deny"}
	}
	return nil
}

// NoAutoAllowReason 不可自动放行的原因；仅 manual_only 时给出。
//
// Python: DecisionRoute.no_auto_allow_reason（property）
func (r DecisionRoute) NoAutoAllowReason() string {
	if r.RequiresManual() {
		return r.Reason
	}
	return ""
}

// BuildToolDecisionFacts 投影适配器、Core 访问与 Engine 的既有事实，不重新解析。
//
// Python: build_tool_decision_facts(tool_name, tool_args, *, workspace_root,
//
//	platform_trusted_root=None, original_args_were_valid_object,
//	external_paths=(), send_paths=())
func BuildToolDecisionFacts(
	toolName string,
	toolArgs map[string]any,
	workspaceRoot string,
	platformTrustedRoot string,
	originalArgsWereValidObject bool,
	externalPaths []string,
	sendPaths []string,
) ToolDecisionFacts {
	capability := ClassifyTool(toolName)

	// Python: args = {str(key): value for key, value in tool_args.items()}
	args := make(map[string]any, len(toolArgs))
	for k, v := range toolArgs {
		args[k] = v
	}

	root := workspaceRootOf(workspaceRoot)
	platformRoot := workspaceRootOf(platformTrustedRoot)

	readPaths, writePaths, accessesKnown := coreAccesses(capability, args, root, sendPaths)

	// command 只在 shell 类取值（Python 用 args.get("command") or args.get("cmd")）
	command := ""
	if capability.Category == "shell" {
		if v, ok := args["command"].(string); ok && v != "" {
			command = strings.TrimSpace(v)
		} else if v, ok := args["cmd"].(string); ok {
			command = strings.TrimSpace(v)
		}
	}

	return ToolDecisionFacts{
		Capability:           capability,
		UntrustedArgs:        args,
		ArgumentsValidObject: originalArgsWereValidObject,
		AccessesKnown:        accessesKnown,
		ReadPaths:            readPaths,
		WritePaths:           writePaths,
		ExternalPaths:        dedupNonEmptyStrings(externalPaths),
		Command:              command,
		WorkspaceRoot:        root,
		PlatformTrustedRoot:  platformRoot,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// workspaceRootOf 把路径规整为绝对 posix 形式，失败或空返回空串。
//
// Python: _workspace_root(value) —— Path(...).expanduser().resolve(strict=False)
func workspaceRootOf(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	expanded := value
	if strings.HasPrefix(expanded, "~") {
		home := userHomeDir()
		if home != "" {
			rest := strings.TrimLeft(expanded[1:], "/\\")
			if rest == "" {
				expanded = home
			} else {
				expanded = filepath.Join(home, rest)
			}
		}
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(abs))
}

// coreAccesses 抽取核心访问。
//
// 五个分支（顺序对齐 Python _core_accesses，不可调换）：
//  1. native path 投影（Smart 场景）—— 命中即返回
//  2. send_file_to_user —— 用 send_paths
//  3. 非 path/shell 类 —— 空且 known=true（确定无路径访问）
//  4. 无 workspace root —— 空且 known=false（无法判定）
//  5. 其余 —— 调 extract_accesses_native；异常则空且 known=false
//
// known 的最终判定：shell 类恒 true；其余看是否抽到路径。
//
// Python: _core_accesses(capability, args, root, *, send_paths)
func coreAccesses(capability ToolCapability, args map[string]any, root string, sendPaths []string) ([]string, []string, bool) {
	// 1. native path 投影
	if native := CurrentNativePathAccess(capability.ToolName, args); native != nil {
		if native.Action == "read" {
			return []string{native.Path}, nil, true
		}
		return nil, []string{native.Path}, true
	}
	// 2. send_file_to_user
	if capability.ToolName == "send_file_to_user" {
		paths := dedupNonEmptyStrings(sendPaths)
		return paths, nil, len(paths) > 0
	}
	// 3. 非 path/shell 类
	if capability.Category != "path" && capability.Category != "shell" {
		return nil, nil, true
	}
	// 4. 无 root
	if root == "" {
		return nil, nil, false
	}
	// 5. extract_accesses_native
	accessArgs := args
	// lsp + workspaceSymbol 把 root 当 file_path（Python 的 workaround）
	if capability.ToolName == "lsp" {
		if op, ok := args["operation"].(string); ok && op == "workspaceSymbol" {
			accessArgs = make(map[string]any, len(args)+1)
			for k, v := range args {
				accessArgs[k] = v
			}
			accessArgs["file_path"] = root
		}
	}
	accesses := safeExtractAccesses(capability.ToolName, accessArgs, root)
	if accesses == nil {
		return nil, nil, false
	}

	// 按 action 分流，写与 exec 都进 WritePaths，两边各自去重
	reads := make([]string, 0, len(accesses))
	writes := make([]string, 0, len(accesses))
	seenRead := make(map[string]struct{}, len(accesses))
	seenWrite := make(map[string]struct{}, len(accesses))
	for _, acc := range accesses {
		normalized := filepath.ToSlash(acc.Path)
		if acc.Action == fileguard.FileActionWrite || acc.Action == fileguard.FileActionExec {
			if _, dup := seenWrite[normalized]; dup {
				continue
			}
			seenWrite[normalized] = struct{}{}
			writes = append(writes, normalized)
			continue
		}
		if _, dup := seenRead[normalized]; dup {
			continue
		}
		seenRead[normalized] = struct{}{}
		reads = append(reads, normalized)
	}

	known := capability.Category == "shell" || len(reads) > 0 || len(writes) > 0
	return reads, writes, known
}

// safeExtractAccesses 调 extract_accesses_native 并吞掉 panic；nil 表示抽取失败。
//
// Python 侧包的是 except (OSError, RuntimeError, TypeError, ValueError)。
func safeExtractAccesses(toolName string, args map[string]any, root string) (out []fileguard.FileAccess) {
	defer func() {
		if rec := recover(); rec != nil {
			out = nil
		}
	}()
	return fileguard.ExtractAccessesNative(toolName, args, root, nil)
}

// dedupNonEmptyStrings 去掉空串并保序去重。
//
// Python: tuple(dict.fromkeys(str(path) for path in paths if str(path)))
func dedupNonEmptyStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, dup := seen[item]; dup {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

// userHomeDir 返回当前用户 home 目录，失败返回空串。
func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}
