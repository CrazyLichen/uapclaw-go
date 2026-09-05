package security

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/anmitsu/go-shlex"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_bash "github.com/tree-sitter/tree-sitter-bash/bindings/go"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ShellStructureFlags Shell 结构标志。
// 跟踪命令中出现的各种 shell 结构特征。
//
// 对齐 Python: ShellStructureFlags (shell_ast.py L34-61)
type ShellStructureFlags struct {
	// CompoundOperators 复合操作符（&& || ; &）
	CompoundOperators bool
	// Pipeline 管道（| |&）
	Pipeline bool
	// Subshell 子 shell（(command)）
	Subshell bool
	// CommandGroup 命令组（{ commands; }）
	CommandGroup bool
	// CommandSubstitution 命令替换（$(cmd) 或 `cmd`）
	CommandSubstitution bool
	// ProcessSubstitution 进程替换（<(cmd) 或 >(cmd)）
	ProcessSubstitution bool
	// ParameterExpansion 参数展开（${var}）
	ParameterExpansion bool
	// Heredoc Here 文档（<< EOF）
	Heredoc bool
	// InputRedirection 输入重定向（<）
	InputRedirection bool
	// OutputRedirection 输出重定向（> >>）
	OutputRedirection bool
	// ActualOperatorNodes 是否有真实的操作符节点（tree-sitter 特有）
	ActualOperatorNodes bool
	// Operators 检测到的操作符列表
	Operators []string
}

// HasRiskyStructure 判断是否为风险结构。
// 风险结构包括：复合操作符、管道、子 shell、命令组、命令替换、进程替换、参数展开、heredoc、输入/输出重定向。
//
// 对齐 Python: ShellStructureFlags.has_risky_structure() (shell_ast.py L49-61)
func (f *ShellStructureFlags) HasRiskyStructure() bool {
	return f.CompoundOperators ||
		f.Pipeline ||
		f.Subshell ||
		f.CommandGroup ||
		f.CommandSubstitution ||
		f.ProcessSubstitution ||
		f.ParameterExpansion ||
		f.Heredoc ||
		f.InputRedirection ||
		f.OutputRedirection
}

// ShellSubcommand Shell 子命令。
//
// 对齐 Python: ShellSubcommand (shell_ast.py L64-70)
type ShellSubcommand struct {
	// Text 命令文本
	Text string
	// Argv 命令参数列表
	Argv []string
	// Redirects 重定向列表
	Redirects []string
	// SourceSpan 源码字节范围 [start, end)
	SourceSpan [2]int
	// ParentOperators 父级操作符
	ParentOperators []string
}

// ShellAstParseResult Shell AST 解析结果。
//
// 对齐 Python: ShellAstParseResult (shell_ast.py L73-79)
type ShellAstParseResult struct {
	// Kind 解析结果类型
	Kind ShellAstKind
	// Subcommands 提取的子命令列表
	Subcommands []ShellSubcommand
	// Flags Shell 结构标志
	Flags ShellStructureFlags
	// Reason 原因描述
	Reason string
	// Backend 使用的后端（"tree-sitter" / "fallback"）
	Backend string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ShellAstKind Shell AST 解析结果类型
//
// 对齐 Python: ShellAstParseResult.kind (shell_ast.py L74-79)
type ShellAstKind int

const (
	// ShellAstKindSimple 可信任，子命令可评估
	ShellAstKindSimple ShellAstKind = iota
	// ShellAstKindTooComplex 结构风险，不可信任
	ShellAstKindTooComplex
	// ShellAstKindParseUnavailable 解析器不可用
	ShellAstKindParseUnavailable
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 保守扫描用的正则表达式
// 对齐 Python: shell_ast.py L28-31
var (
	commandSubstitutionRe = regexp.MustCompile("`|\\$\\(")
	processSubstitutionRe = regexp.MustCompile(`[<>]\(`)
	heredocRe             = regexp.MustCompile("<<<?")
	paramExpansionRe      = regexp.MustCompile(`\$\{`)
)

// 保守扫描检测的操作符标记
// 对齐 Python: _collect_operator_markers (shell_ast.py L181-186)
var operatorMarkers = []string{"&&", "||", ";", "|", ">>", ">", "<", "$(", "`", "<(", ">(", "<<", "<<<"}

// shellAstLogComponent ShellAST 日志组件
var shellAstLogComponent = logger.ComponentAgentCore

// treeSitterReady tree-sitter 后端可用状态（nil=未检测, true=可用, false=不可用）
var treeSitterReady *bool

// treeSitterParser 全局缓存的 tree-sitter Parser 实例
var treeSitterParser *tree_sitter.Parser

// treeSitterOnce 确保 Parser 只初始化一次
var treeSitterOnce sync.Once

// treeSitterMu 保护 Parser 并发访问（tree-sitter Parser 不是并发安全的）
var treeSitterMu sync.Mutex

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseShellForPermission 解析 Shell 命令用于权限评估。
//
// 对齐 Python: parse_shell_for_permission(command) (shell_ast.py L82-103)
// 返回 ShellAstParseResult，kind 为 Simple/TooComplex/ParseUnavailable 之一。
func ParseShellForPermission(command string) *ShellAstParseResult {
	text := strings.TrimSpace(command)
	if text == "" {
		return &ShellAstParseResult{
			Kind:    ShellAstKindSimple,
			Backend: "fallback",
		}
	}

	// 尝试 tree-sitter 后端
	parser := getTreeSitterBashParser()
	if parser != nil {
		treeSitterMu.Lock()
		result, err := parseWithTreeSitter(text, parser)
		treeSitterMu.Unlock()
		if err == nil {
			return result
		}
		// tree-sitter 解析失败，记录日志后降级
		logger.Warn(shellAstLogComponent).
			Str("backend", "tree-sitter").
			Err(err).
			Msg("tree-sitter 解析失败，降级到保守扫描")
	}

	// 降级到保守扫描
	return parseWithConservativeFallback(text)
}

// String 返回 ShellAstKind 的字符串表示
func (k ShellAstKind) String() string {
	switch k {
	case ShellAstKindSimple:
		return "simple"
	case ShellAstKindTooComplex:
		return "too_complex"
	case ShellAstKindParseUnavailable:
		return "parse_unavailable"
	default:
		return fmt.Sprintf("unknown(%d)", k)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getTreeSitterBashParser 获取或初始化 tree-sitter bash 解析器。
// 使用 sync.Once 缓存全局 Parser 实例，避免每次调用重新创建。
//
// 对齐 Python: _get_tree_sitter_bash_parser() (shell_ast.py L106-128)
func getTreeSitterBashParser() *tree_sitter.Parser {
	treeSitterOnce.Do(func() {
		parser := tree_sitter.NewParser()
		lang := tree_sitter.NewLanguage(tree_sitter_bash.Language())
		if err := parser.SetLanguage(lang); err != nil {
			parser.Close()
			falseVal := false
			treeSitterReady = &falseVal
			logger.Info(shellAstLogComponent).Msg("tree-sitter bash 后端不可用，使用保守扫描")
			return
		}
		trueVal := true
		treeSitterReady = &trueVal
		treeSitterParser = parser
	})

	if treeSitterReady != nil && !*treeSitterReady {
		return nil
	}
	return treeSitterParser
}

// parseWithTreeSitter 使用 tree-sitter 精确解析。
//
// 对齐 Python: _parse_with_tree_sitter(command, parser) (shell_ast.py L189-268)
func parseWithTreeSitter(command string, parser *tree_sitter.Parser) (*ShellAstParseResult, error) {
	source := []byte(command)
	tree := parser.Parse(source, nil)
	if tree == nil {
		return &ShellAstParseResult{
			Kind:    ShellAstKindParseUnavailable,
			Reason:  "tree-sitter 返回空语法树",
			Backend: "tree-sitter",
		}, nil
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil {
		return &ShellAstParseResult{
			Kind:    ShellAstKindParseUnavailable,
			Reason:  "tree-sitter 返回空根节点",
			Backend: "tree-sitter",
		}, nil
	}

	// 对齐 Python: root.has_error → too_complex
	if root.HasError() {
		return &ShellAstParseResult{
			Kind:    ShellAstKindTooComplex,
			Reason:  "tree-sitter 报告解析错误",
			Backend: "tree-sitter",
		}, nil
	}

	// 收集结构标志
	flags := collectTreeSitterFlags(root)

	// 对齐 Python: 风险结构检测 → too_complex
	// (shell_ast.py L207-220)
	if flags.CommandSubstitution ||
		flags.ProcessSubstitution ||
		flags.ParameterExpansion ||
		flags.Heredoc ||
		flags.Subshell ||
		flags.CommandGroup {
		return &ShellAstParseResult{
			Kind:    ShellAstKindTooComplex,
			Flags:   flags,
			Reason:  "tree-sitter 检测到不支持的复杂 shell 结构",
			Backend: "tree-sitter",
		}, nil
	}

	// 收集命令节点
	commandNodes := collectCommandNodes(root)
	if len(commandNodes) == 0 {
		return &ShellAstParseResult{
			Kind:    ShellAstKindTooComplex,
			Flags:   flags,
			Reason:  "tree-sitter 未提取到可执行命令节点",
			Backend: "tree-sitter",
		}, nil
	}

	// 提取子命令
	var subcommands []ShellSubcommand
	for _, node := range commandNodes {
		startByte := int(node.StartByte())
		endByte := int(node.EndByte())
		text := strings.TrimSpace(string(source[startByte:endByte]))
		if text == "" {
			continue
		}

		// 对齐 Python: shlex.split(text) → Go shlex 分词
		argv, _ := shlex.Split(text, true)

		// 对齐 Python: 收集重定向
		var redirects []string
		for i := uint(0); i < node.ChildCount(); i++ {
			child := node.Child(i)
			childType := child.Kind()
			if strings.Contains(childType, "redirect") {
				redirects = append(redirects, strings.TrimSpace(string(source[child.StartByte():child.EndByte()])))
			}
		}

		subcommands = append(subcommands, ShellSubcommand{
			Text:            text,
			Argv:            argv,
			Redirects:       redirects,
			SourceSpan:      [2]int{startByte, endByte},
			ParentOperators: flags.Operators,
		})
	}

	if len(subcommands) == 0 {
		return &ShellAstParseResult{
			Kind:    ShellAstKindTooComplex,
			Flags:   flags,
			Reason:  "tree-sitter 仅提取到空命令节点",
			Backend: "tree-sitter",
		}, nil
	}

	return &ShellAstParseResult{
		Kind:        ShellAstKindSimple,
		Subcommands: subcommands,
		Flags:       flags,
		Backend:     "tree-sitter",
	}, nil
}

// collectTreeSitterFlags 从 tree-sitter AST 收集结构标志。
//
// 对齐 Python: _collect_tree_sitter_flags(root) (shell_ast.py L271-328)
func collectTreeSitterFlags(root *tree_sitter.Node) ShellStructureFlags {
	var flags ShellStructureFlags
	var operators []string
	operatorSet := make(map[string]bool)

	stack := []*tree_sitter.Node{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		nodeType := node.Kind()

		switch nodeType {
		case "pipeline":
			flags.Pipeline = true
		case "list", "list_item":
			flags.CompoundOperators = true
		case "subshell", "subshell_expression":
			flags.Subshell = true
		case "compound_statement", "brace_group":
			flags.CommandGroup = true
		case "command_substitution":
			flags.CommandSubstitution = true
		case "process_substitution":
			flags.ProcessSubstitution = true
		case "expansion", "simple_expansion":
			flags.ParameterExpansion = true
		default:
			if strings.Contains(nodeType, "heredoc") {
				flags.Heredoc = true
			}
		}

		// 重定向
		if nodeType == "redirected_statement" || nodeType == "file_redirect" || nodeType == "heredoc_redirect" {
			flags.InputRedirection = true
			flags.OutputRedirection = true
		}

		// 操作符节点
		switch nodeType {
		case "<", ">", ">>":
			flags.ActualOperatorNodes = true
			if nodeType == "<" {
				flags.InputRedirection = true
			} else {
				flags.OutputRedirection = true
			}
			if !operatorSet[nodeType] {
				operatorSet[nodeType] = true
				operators = append(operators, nodeType)
			}
		case ";", "&&", "||", "|", "|&", "&":
			flags.ActualOperatorNodes = true
			if nodeType != "|" && nodeType != "|&" {
				flags.CompoundOperators = true
			}
			if nodeType == "|" || nodeType == "|&" {
				flags.Pipeline = true
			}
			if !operatorSet[nodeType] {
				operatorSet[nodeType] = true
				operators = append(operators, nodeType)
			}
		}

		// 子节点入栈（逆序以保持顺序）
		childCount := node.ChildCount()
		for i := int(childCount) - 1; i >= 0; i-- {
			child := node.Child(uint(i))
			if child != nil {
				stack = append(stack, child)
			}
		}
	}

	flags.Operators = operators
	return flags
}

// collectCommandNodes 从 tree-sitter AST 提取所有 command 节点。
//
// 对齐 Python: _collect_command_nodes(root) (shell_ast.py L331-342)
func collectCommandNodes(root *tree_sitter.Node) []*tree_sitter.Node {
	var commandNodes []*tree_sitter.Node
	stack := []*tree_sitter.Node{root}

	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if node.Kind() == "command" {
			commandNodes = append(commandNodes, node)
			continue // 不深入 command 子节点
		}

		// 子节点入栈（逆序以保持顺序）
		childCount := node.ChildCount()
		for i := int(childCount) - 1; i >= 0; i-- {
			child := node.Child(uint(i))
			if child != nil {
				stack = append(stack, child)
			}
		}
	}

	return commandNodes
}

// parseWithConservativeFallback 使用保守正则扫描 fallback。
//
// 对齐 Python: _parse_with_conservative_fallback(command) (shell_ast.py L131-155)
func parseWithConservativeFallback(command string) *ShellAstParseResult {
	flags := scanShellStructure(command)

	if flags.HasRiskyStructure() {
		return &ShellAstParseResult{
			Kind:    ShellAstKindParseUnavailable,
			Flags:   flags,
			Reason:  "tree-sitter 后端不可用且保守扫描检测到 shell 结构",
			Backend: "fallback",
		}
	}

	// 对齐 Python: shlex.split(command)
	argv, _ := shlex.Split(command, true)

	subcommand := ShellSubcommand{
		Text:       command,
		Argv:       argv,
		SourceSpan: [2]int{0, len(command)},
	}

	return &ShellAstParseResult{
		Kind:        ShellAstKindSimple,
		Subcommands: []ShellSubcommand{subcommand},
		Flags:       flags,
		Backend:     "fallback",
	}
}

// scanShellStructure 正则扫描 Shell 结构特征。
//
// 对齐 Python: _scan_shell_structure(command) (shell_ast.py L158-178)
func scanShellStructure(command string) ShellStructureFlags {
	var flags ShellStructureFlags
	var operators []string
	operatorSet := make(map[string]bool)

	flags.Pipeline = strings.Contains(command, "|")
	flags.CompoundOperators = strings.Contains(command, "&&") ||
		strings.Contains(command, "||") ||
		strings.Contains(command, ";") ||
		strings.Contains(command, "\n") ||
		strings.Contains(command, "\r")
	flags.InputRedirection = strings.Contains(command, "<")
	flags.OutputRedirection = strings.Contains(command, ">")
	flags.CommandSubstitution = commandSubstitutionRe.MatchString(command)
	flags.ProcessSubstitution = processSubstitutionRe.MatchString(command)
	flags.ParameterExpansion = paramExpansionRe.MatchString(command)
	flags.Heredoc = heredocRe.MatchString(command)

	// 对齐 Python: _collect_operator_markers
	for _, token := range operatorMarkers {
		if strings.Contains(command, token) && !operatorSet[token] {
			operatorSet[token] = true
			operators = append(operators, token)
		}
	}
	flags.Operators = operators

	return flags
}

