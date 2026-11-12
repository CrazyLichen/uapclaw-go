package processcli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	swschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// 本文件是 channels/process_cli/main.py 的 Go 对应物。
//
// Python: channels/process_cli/main.py（478 行）
//
// 职责：参数层。构建 21 个可见参数 + 7 个隐藏内部参数，并按上游五分支
// 的优先级分派：会话查询 → query-json → run-jsonl → run-json → prompt/REPL。
// 前四支都不进入交互路径逻辑（不解析 work_mode、不 chdir、不校验模型）。

// ──────────────────────────── 结构体 ────────────────────────────

// AppArgs 一次 process-cli 调用的完整参数（可见 + 隐藏）。
// Python: main.py:154-302 build_parser 产出的 Namespace
type AppArgs struct {
	// Prompt 位置参数：要执行的指令；空则进交互式 CLI
	Prompt string
	// Session 恢复完整会话 ID
	Session string
	// ListSessions 列出本地 Process CLI 会话
	ListSessions bool
	// LatestSession 仅输出最近消息会话的完整 ID
	LatestSession bool
	// Search 查询关键词
	Search string
	// Limit 会话列表每页条数
	Limit int
	// Offset 会话列表跳过的条数
	Offset int
	// LimitSet 是否显式指定 --limit
	LimitSet bool
	// OffsetSet 是否显式指定 --offset
	OffsetSet bool
	// SearchSet 是否显式指定 --search
	SearchSet bool
	// Model 本次任务的模型名称/别名/name#索引
	Model string
	// APIBase 本次任务的 HTTP(S) 模型 API 基础地址
	APIBase string
	// APIKeyEnv 保存本次模型密钥的环境变量名称
	APIKeyEnv string
	// ModelProvider API 协议提供方
	ModelProvider string
	// QueryJSON 执行一次只读 Runtime 查询
	QueryJSON string
	// RunJSON 执行一份机器 JSON 请求
	RunJSON string
	// RunJSONL 按行接收 run/answer/cancel
	RunJSONL bool
	// Cwd 工作目录
	Cwd string
	// ProjectDir 稳定的项目目录
	ProjectDir string
	// TrustedDirs 可信目录
	TrustedDirs []string
	// Mode Runtime 运行模式
	Mode string
	// WorkMode 内部 work profile（work / code）
	WorkMode string
	// Output 输出格式（human/json/jsonl）
	Output string
	// OutputExplicit 是否显式指定 --output
	OutputExplicit bool
	// Timeout 总执行超时（秒）
	Timeout float64
	// TimeoutSet 是否显式指定 --timeout
	TimeoutSet bool
	// ShowReasoning 显示思考过程
	ShowReasoning bool
	// ShowTools 显示工具调用和结果
	ShowTools bool
	// Debug 显示详细诊断日志
	Debug bool
	// InteractiveWorker 内部：作为 REPL worker 运行
	InteractiveWorker bool
	// ForwardedLiveInput 内部：父进程已转发实时输入
	ForwardedLiveInput bool
	// SessionResultFile 内部：会话 ID 回执文件
	SessionResultFile string
	// WorkerResultFile 内部：worker 结果回执文件
	WorkerResultFile string
	// ModelSelection 内部：模型选择键
	ModelSelection string
	// PromptFile 内部：prompt 文件路径
	PromptFile string
	// Operation 内部：操作类型
	Operation string
	// lastWorkerResult 内部：上一次 worker 回执（REPL 用）
	lastWorkerResult map[string]any
}

// mainDeps 分派依赖，测试注入用。
type mainDeps struct {
	// ExecuteQuerySource --query-json 分支
	ExecuteQuerySource func(QueryEntryOptions) int
	// ExecuteSource --run-json / --run-jsonl 分支
	ExecuteSource func(ExecuteOptions) int
	// ExecuteSessionQuery 会话查询分支
	ExecuteSessionQuery func(*AppArgs) int
	// RunApp 单命令生命周期
	RunApp func(*AppArgs, any, any) int
	// RunRepl 交互外壳
	RunRepl func(*AppArgs) int
	// Stdout 用户可见输出
	Stdout any
	// Stderr 用户可见错误
	Stderr any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultMode 默认运行模式
	// Python: main.py:244 default="code.normal"
	defaultMode = "code.normal"
	// defaultOutput 默认输出格式
	// Python: main.py:253 default="human"
	defaultOutput = "human"
	// operationChat 默认操作
	// Python: main.py:299 default="chat"
	operationChat = "chat"
	// exitArgError 参数错误退出码
	// Python: main.py:151 self.exit(2, ...)
	exitArgError = 2
)

// errHelpRequested --help 触发的正常退出标记（非错误）。
var errHelpRequested = errors.New("help requested")

// ──────────────────────────── 全局变量 ────────────────────────────

// argparse 五条错误消息的等价正则。
// Python: main.py:84-120 _translate_argparse_error
var (
	argErrInvalidChoice = regexp.MustCompile(
		`^(?:argument|参数) (\S+): invalid choice: (.+) \(choose from (.+)\)$`)
	argErrRequired     = regexp.MustCompile(`^the following arguments are required: (.+)$`)
	argErrUnrecognized = regexp.MustCompile(`^unrecognized arguments: (.+)$`)
	argErrExpectedOne  = regexp.MustCompile(`^(?:argument|参数) (\S+): expected one argument$`)
	argErrInvalidValue = regexp.MustCompile(`^(?:argument|参数) (\S+): invalid (\S+) value: (.+)$`)
	// cobra 独有：未知 flag 用 "unknown flag: --x"，等价 argparse 的 unrecognized arguments
	argErrUnknownFlag = regexp.MustCompile(`^unknown flag: (.+)$`)
)

// usageTemplate 中文 usage 模板（对齐 ChineseArgumentParser.format_help 的四项替换）。
// 偏差 E-1：cobra 模板与 argparse 不同，此处只做标签中文化。
const usageTemplate = `用法：
  {{.UseLine}}{{if .HasAvailableSubCommands}}{{end}}{{if gt (len .Aliases) 0}}

别名：
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

示例：
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

位置参数：{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

选项：
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

全局选项：
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

附加帮助：{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

使用 "{{.CommandPath}} [command] --help" 查看命令的更多信息。{{end}}
`

// ──────────────────────────── 导出函数 ────────────────────────────

// RunMain 构建参数后按五分支分派，返回进程退出码。
// Python: main.py:340-474 main
func RunMain(argv []string) int {
	args, err := parseArgs(argv)
	if errors.Is(err, errHelpRequested) {
		// 帮助已由 cobra 打印；按上游 argparse 的 action="help" 语义退出 0
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "uapclaw-process: 错误：%s\n", translateArgError(err.Error()))
		return exitArgError
	}
	deps := &mainDeps{
		ExecuteQuerySource:  ExecuteQuerySource,
		ExecuteSource:       ExecuteSource,
		ExecuteSessionQuery: ExecuteSessionQuery,
		Stdout:              os.Stdout,
		Stderr:              os.Stderr,
	}
	return dispatchBranch(args, deps)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// translateArgError 把参数解析错误消息翻译成中文。
// Python: main.py:84-120 _translate_argparse_error
//
// 五条规则逐条对应；未命中时原样返回。
func translateArgError(message string) string {
	if m := argErrInvalidChoice.FindStringSubmatch(message); m != nil {
		choices := strings.Split(m[3], ",")
		rendered := make([]string, 0, len(choices))
		for _, choice := range choices {
			trimmed := strings.Trim(strings.TrimSpace(choice), "'")
			rendered = append(rendered, "'"+trimmed+"'")
		}
		return fmt.Sprintf("参数 %s 的值无效：%s（可选值：%s）",
			m[1], m[2], strings.Join(rendered, ", "))
	}
	if m := argErrRequired.FindStringSubmatch(message); m != nil {
		return "缺少必需参数：" + m[1]
	}
	if m := argErrUnrecognized.FindStringSubmatch(message); m != nil {
		return "无法识别的参数：" + m[1]
	}
	if m := argErrUnknownFlag.FindStringSubmatch(message); m != nil {
		return "无法识别的参数：" + m[1]
	}
	if m := argErrExpectedOne.FindStringSubmatch(message); m != nil {
		return fmt.Sprintf("参数 %s 需要一个值", m[1])
	}
	if m := argErrInvalidValue.FindStringSubmatch(message); m != nil {
		return fmt.Sprintf("参数 %s 的值无效：%s", m[1], m[3])
	}
	return message
}

// dispatchBranch 按上游五分支的优先级分派。
// Python: main.py:343-428
//
// 优先级：会话查询 → query-json → run-jsonl → run-json → prompt/REPL。
func dispatchBranch(args *AppArgs, deps *mainDeps) int {
	// 分支 1：会话查询（main.py:343-350）
	if args.ListSessions || args.LatestSession || args.SearchSet || args.LimitSet || args.OffsetSet {
		return deps.ExecuteSessionQuery(args)
	}

	// 分支 2：--query-json（main.py:352-366）
	if args.QueryJSON != "" {
		return deps.ExecuteQuerySource(QueryEntryOptions{
			Source:               args.QueryJSON,
			ConflictingArguments: args.hasLegacyArguments(),
		})
	}

	// 分支 3：--run-jsonl（main.py:367-377）
	if args.RunJSONL {
		return deps.ExecuteSource(ExecuteOptions{
			Source:               "-",
			JSONLines:            true,
			ConflictingArguments: args.hasLegacyArguments(),
		})
	}

	// 分支 4：--run-json（main.py:378-388）
	if args.RunJSON != "" {
		return deps.ExecuteSource(ExecuteOptions{
			Source:               args.RunJSON,
			ConflictingArguments: args.hasLegacyArguments(),
		})
	}

	// 分支 5：普通命令（main.py:389-428）
	args.WorkMode = ResolveCliWorkMode(args.Mode, args.WorkMode)

	// Python: :396-400 内部 prompt 文件与位置参数互斥
	if args.PromptFile != "" {
		if args.Prompt != "" {
			fmt.Fprintf(os.Stderr, "uapclaw-process: 错误：内部 prompt 文件不能与位置参数同时使用\n")
			return exitArgError
		}
		raw, err := os.ReadFile(args.PromptFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "uapclaw-process: 错误：读取 prompt 文件失败：%v\n", err)
			return exitArgError
		}
		args.Prompt = string(raw)
	}

	// Python: :401-412 非交互命令的互斥校验
	if args.Prompt != "" && !args.InteractiveWorker {
		if err := validateNonInteractive(args); err != nil {
			fmt.Fprintf(os.Stderr, "uapclaw-process: 错误：%v\n", err)
			return exitArgError
		}
	}

	// Python: :413 --cwd 在任何 Runtime 加载之前生效
	if err := activateRequestedCwd(args); err != nil {
		fmt.Fprintf(os.Stderr, "uapclaw-process: 错误：%v\n", err)
		return exitArgError
	}

	// Python: :414-415 --timeout 必须为正
	if args.TimeoutSet && args.Timeout <= 0 {
		fmt.Fprintf(os.Stderr, "uapclaw-process: 错误：--timeout 必须大于零\n")
		return exitArgError
	}

	// Python: :416-419 交互模式的校验
	if args.Prompt == "" {
		if err := validateInteractive(args); err != nil {
			fmt.Fprintf(os.Stderr, "uapclaw-process: 错误：%v\n", err)
			return exitArgError
		}
	}

	// Python: :420-423 模型连接参数校验
	if _, err := ValidateModelOptions(args); err != nil {
		fmt.Fprintf(os.Stderr, "uapclaw-process: 错误：%v\n", err)
		return exitArgError
	}

	// Python: :424-428 分派
	if args.Prompt == "" {
		return deps.RunRepl(args)
	}
	return deps.RunApp(args, deps.Stdout, deps.Stderr)
}

// validateNonInteractive 非交互命令的互斥校验。
// Python: main.py:401-412
func validateNonInteractive(args *AppArgs) error {
	if args.OutputExplicit {
		return fmt.Errorf("非交互式命令不支持 --output")
	}
	mode := ResolveDisplayMode(args.Mode, args.WorkMode)
	if swschema.IsPlanMode(mode) || swschema.IsTeamMode(mode) {
		return fmt.Errorf("非交互式命令仅支持单 Agent 普通模式，不能通过 --mode 进入 Plan 或 Team 模式；" +
			"请使用 agent.code.normal 或 agent.work.normal，或进入交互式 CLI")
	}
	return nil
}

// validateInteractive 交互模式的校验。
// Python: main.py:416-419
func validateInteractive(args *AppArgs) error {
	if args.Output != defaultOutput {
		return fmt.Errorf("交互模式仅支持 --output human")
	}
	if args.Debug {
		return fmt.Errorf("--debug 仅支持非交互式运行")
	}
	return nil
}

// activateRequestedCwd 在任何 Runtime 加载之前切换工作目录。
// Python: main.py:305-320 _activate_requested_cwd
//
// 关键语义：先 resolve 再 chdir，否则相对路径会基于新 cwd 解析。
func activateRequestedCwd(args *AppArgs) error {
	requested := strings.TrimSpace(args.Cwd)
	if requested == "" {
		return nil
	}
	expanded := expandHome(requested)
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return fmt.Errorf("--cwd 无法访问：%v", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return fmt.Errorf("--cwd 无法访问：%v", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("--cwd 无法访问：%s 不是目录", resolved)
	}
	if err := os.Chdir(resolved); err != nil {
		return fmt.Errorf("--cwd 无法访问：%v", err)
	}
	args.Cwd = resolved
	return nil
}

// hasLegacyArguments 是否携带了机器协议之外的显式参数。
// Python: main.py:353-356 / :379-382 standalone 判定的反向
func (a *AppArgs) hasLegacyArguments() bool {
	return a.Prompt != "" || a.Session != "" || a.ListSessions || a.LatestSession ||
		a.Model != "" || a.APIBase != "" || a.APIKeyEnv != "" || a.ModelProvider != "" ||
		a.Cwd != "" || a.ProjectDir != "" || len(a.TrustedDirs) > 0 ||
		a.Mode != defaultMode || a.OutputExplicit || a.TimeoutSet ||
		a.ShowReasoning || a.ShowTools || a.Debug
}

// parseArgs 解析命令行到 AppArgs。
// Python: main.py:154-302 build_parser
func parseArgs(argv []string) (*AppArgs, error) {
	args := &AppArgs{
		Mode:      defaultMode,
		WorkMode:  "code",
		Output:    defaultOutput,
		Operation: operationChat,
	}
	// ran 标记 RunE 是否真的执行过；--help / -h 时 cobra 打印帮助后不调 RunE。
	ran := false
	root := &cobra.Command{
		Use:           "uapclaw-process [prompt]",
		Short:         "在独立的本地 Runtime 进程中运行 UAPClaw 指令",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, positional []string) error {
			ran = true
			switch len(positional) {
			case 0:
			case 1:
				args.Prompt = positional[0]
			default:
				return fmt.Errorf("unrecognized arguments: %s", strings.Join(positional[1:], " "))
			}
			return nil
		},
	}
	root.SetUsageTemplate(usageTemplate)
	bindQueryFlags(root, args)
	bindModelFlags(root, args)
	bindMachineFlags(root, args)
	bindWorkspaceFlags(root, args)
	bindOutputFlags(root, args)
	bindHiddenFlags(root, args)

	root.SetArgs(argv)
	if err := root.Execute(); err != nil {
		// --help / -h 由 cobra 以 flag.ErrHelp 返回，属于正常退出而非参数错误
		if errors.Is(err, flag.ErrHelp) {
			return nil, errHelpRequested
		}
		return nil, err
	}

	if !ran {
		// cobra 已处理（--help / -h）：按上游 action="help" 语义退出 0
		return nil, errHelpRequested
	}

	// Python: :126 namespace._output_explicit = True（仅真实指定时置位）
	args.OutputExplicit = root.Flags().Changed("output")
	args.LimitSet = root.Flags().Changed("limit")
	args.OffsetSet = root.Flags().Changed("offset")
	args.SearchSet = root.Flags().Changed("search")
	args.TimeoutSet = root.Flags().Changed("timeout")
	return args, nil
}

// bindQueryFlags 会话查询参数。
// Python: main.py:171-202
func bindQueryFlags(cmd *cobra.Command, args *AppArgs) {
	cmd.Flags().StringVar(&args.Session, "session", "", "恢复完整会话 ID；可先用 --list-sessions 查询")
	cmd.Flags().BoolVar(&args.ListSessions, "list-sessions", false,
		"列出本地 Process CLI 的 Agent/Team 会话，显示完整 ID、时间、模式、标题和目录")
	cmd.Flags().BoolVar(&args.LatestSession, "latest-session", false,
		"仅输出最近消息会话的完整 ID；无匹配会话时退出码为 1")
	cmd.Flags().StringVar(&args.Search, "search", "", "查询关键词，匹配 ID、标题、项目及模式；最多 200 个字符")
	cmd.Flags().IntVar(&args.Limit, "limit", 20, "会话列表每页条数，1 到 200；默认 20")
	cmd.Flags().IntVar(&args.Offset, "offset", 0, "会话列表跳过的条数，非负整数；默认 0")
}

// bindModelFlags 模型连接参数。
// Python: main.py:203-218
func bindModelFlags(cmd *cobra.Command, args *AppArgs) {
	cmd.Flags().StringVarP(&args.Model, "model", "m", "",
		"本次任务的模型名称、唯一别名或 name#索引；省略时使用已有默认配置")
	cmd.Flags().StringVar(&args.APIBase, "api-base", "",
		"本次任务的 HTTP(S) 模型 API 基础地址；需同时指定 --model 和 --api-key-env")
	cmd.Flags().StringVar(&args.APIKeyEnv, "api-key-env", "",
		"保存本次模型密钥的环境变量名称（不是密钥值）；需同时指定 --model")
	cmd.Flags().StringVar(&args.ModelProvider, "model-provider", "",
		"API 协议提供方（OpenAI|Anthropic）；需同时指定 --api-base")
}

// bindMachineFlags 机器协议参数。
// Python: main.py:219-233
func bindMachineFlags(cmd *cobra.Command, args *AppArgs) {
	cmd.Flags().StringVar(&args.QueryJSON, "query-json", "",
		"执行一次只读 Runtime 查询，输出 query_result 后关闭 Runtime 并退出")
	cmd.Flags().StringVar(&args.RunJSON, "run-json", "",
		"执行一份机器 JSON 请求并输出版本化 JSONL；- 从标准输入读取至 EOF")
	cmd.Flags().BoolVar(&args.RunJSONL, "run-jsonl", false,
		"一次进程内按行接收 run、answer、cancel；任务结束即退出")
}

// bindWorkspaceFlags 工作区与模式参数。
// Python: main.py:234-248
func bindWorkspaceFlags(cmd *cobra.Command, args *AppArgs) {
	cmd.Flags().StringVar(&args.Cwd, "cwd", "", "工作目录；默认为当前目录")
	cmd.Flags().StringVar(&args.ProjectDir, "project-dir", "", "稳定的项目目录；默认与工作目录相同")
	cmd.Flags().StringArrayVar(&args.TrustedDirs, "trusted-dir", nil, "可信目录；可重复指定多个目录")
	cmd.Flags().StringVar(&args.Mode, "mode", defaultMode,
		"Runtime 运行模式；非交互命令仅支持单 Agent 普通模式，不支持 Plan 或 Team")
}

// bindOutputFlags 输出与显示参数。
// Python: main.py:249-263
func bindOutputFlags(cmd *cobra.Command, args *AppArgs) {
	cmd.Flags().StringVar(&args.Output, "output", defaultOutput, "输出格式（human|json|jsonl）")
	_ = cmd.Flags().MarkHidden("output")
	cmd.Flags().Float64Var(&args.Timeout, "timeout", 0, "总执行超时时间，单位为秒")
	cmd.Flags().BoolVar(&args.ShowReasoning, "show-reasoning", false, "显示思考过程")
	cmd.Flags().BoolVar(&args.ShowTools, "show-tools", false, "显示工具调用和结果")
	cmd.Flags().BoolVar(&args.Debug, "debug", false, "单次命令在终端显示详细诊断日志")
}

// bindHiddenFlags 内部协议参数（REPL 与 worker 之间）。
// Python: main.py:264-301
func bindHiddenFlags(cmd *cobra.Command, args *AppArgs) {
	cmd.Flags().BoolVar(&args.InteractiveWorker, "_interactive-worker", false, "")
	cmd.Flags().BoolVar(&args.ForwardedLiveInput, "_forwarded-live-input", false, "")
	cmd.Flags().StringVar(&args.SessionResultFile, "_session-result-file", "", "")
	cmd.Flags().StringVar(&args.WorkerResultFile, "_worker-result-file", "", "")
	cmd.Flags().StringVar(&args.ModelSelection, "_model-selection", "", "")
	cmd.Flags().StringVar(&args.PromptFile, "_prompt-file", "", "")
	cmd.Flags().StringVar(&args.Operation, "_operation", operationChat, "")
	for _, name := range []string{
		"_interactive-worker", "_forwarded-live-input", "_session-result-file",
		"_worker-result-file", "_model-selection", "_prompt-file", "_operation",
	} {
		_ = cmd.Flags().MarkHidden(name)
	}
}
