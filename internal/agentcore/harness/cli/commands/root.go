// Package commands CLI 的 cobra 命令树。
//
// Python: openjiuwen/harness/cli/cli.py（1117 行）
//
// ⏵ 实施范围（2026-11-27）：根命令（全局 flags + 无子命令时的 TTY 分派）
// + chat（REPL）+ run（非交互）+ auto-harness 组骨架。auto-harness 的
// 四个子命令（run / experience search / experience list / gap-analyze /
// verify-ext）依赖 auto-harness 优化链路（_run_auto_harness 约 300 行），
// ⤵️ 15.6+ 回填。
package commands

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/agent"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/storage"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/ui/repl"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/ui/runner"
)

// ──────────────────────────── 结构体 ────────────────────────────

// cliOptions 全局 flags 的载体。
// Python: cli.py:176-199 CLIOptions
type cliOptions struct {
	provider  string
	model     string
	apiKey    string
	apiBase   string
	remote    string
	verbose   bool
	workspace string
}

// loadConfig 三层合并构造 CLIConfig。
// Python: cli.py:203-228 _run_chat / :230-243 _run_once 的公共前置
func loadConfig(opts cliOptions) (agent.CLIConfig, error) {
	return agent.LoadConfig(agent.LoadConfigOptions{
		Provider:  opts.provider,
		Model:     opts.model,
		APIKey:    opts.apiKey,
		APIBase:   opts.apiBase,
		ServerURL: opts.remote,
		Workspace: opts.workspace,
		Verbose:   opts.verbose,
	})
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRootCommand 构造根命令。
// Python: cli.py:274-297 cli（group, invoke_without_command）
//
// 无子命令时：TTY stdin → chat；非 TTY stdin → run（管道）。
func NewRootCommand(version string) *cobra.Command {
	var opts cliOptions

	root := &cobra.Command{
		Use:     "openjiuwen",
		Short:   "OpenJiuWen — 终端交互式 AI 编程助手",
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Python: :293-297 无子命令时按 stdin 类型分派
			if isTerminalFn() {
				return runChat(opts, os.Stdout)
			}
			// 非 TTY stdin → 当作管道喂给 run
			prompt := readStdin()
			if prompt == "" {
				return fmt.Errorf("A prompt argument is required, or pipe via stdin.")
			}
			code := runOnce(opts, prompt, runner.OutputFormatText)
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
	f := root.Flags()
	f.StringVarP(&opts.model, "model", "m", "", "Model name.")
	f.StringVar(&opts.provider, "provider", "", "LLM provider.")
	f.StringVar(&opts.apiKey, "api-key", "", "API key.")
	f.StringVar(&opts.apiBase, "api-base", "", "API base URL.")
	f.StringVar(&opts.remote, "remote", "", "Remote agent-server URL.")
	f.BoolVarP(&opts.verbose, "verbose", "v", false, "Verbose logging.")
	f.StringVarP(&opts.workspace, "workspace", "w", "", "Agent workspace directory (default: ~/.openjiuwen/workspace).")

	root.AddCommand(newChatCommand(&opts), newRunCommand(&opts), newAutoHarnessCommand(&opts))
	return root
}

// ──────────────────────────── 子命令 ────────────────────────────

// newChatCommand 交互 REPL 模式（默认）。
// Python: cli.py:299-331 chat
func newChatCommand(opts *cliOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Interactive REPL mode (default).",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChat(*opts, os.Stdout)
		},
	}
}

// newRunCommand 非交互运行模式。
// Python: cli.py:333-362 run
//
// 支持管道：openjiuwen run -（或无参数且 stdin 非 TTY）读 stdin。
func newRunCommand(opts *cliOptions) *cobra.Command {
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "run [prompt]",
		Short: "Non-interactive run mode.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt := ""
			if len(args) > 0 {
				prompt = args[0]
			}
			// Python: :347-350 stdin 管道
			if prompt == "-" || (prompt == "" && !isTerminalStdin()) {
				prompt = readStdin()
			}
			if prompt == "" {
				return fmt.Errorf("A prompt argument is required, or pipe via stdin.")
			}
			code := runOnce(*opts, prompt, outputFormat)
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&outputFormat, "output-format", "f",
		runner.OutputFormatText, "Output format (text|json|stream-json).")
	return cmd
}

// newAutoHarnessCommand auto-harness 优化命令组（骨架）。
// Python: cli.py:768-902 auto_harness 组
//
// ⏵ run / experience search / experience list / gap-analyze / verify-ext
// 依赖优化链路，⤵️ 15.6+ 回填。
func newAutoHarnessCommand(opts *cliOptions) *cobra.Command {
	group := &cobra.Command{
		Use:   "auto-harness",
		Short: "Run auto-harness optimization.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("auto-harness subcommands are not yet available (⏵ 15.6+)")
		},
	}
	group.AddCommand(&cobra.Command{
		Use: "run", Short: "Run the optimization loop.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("auto-harness run is not yet available (⏵ 15.6+)")
		},
	})
	return group
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isTerminalFn TTY 判定（测试可替换；go test 的 stdin 是 /dev/null——
// 它也是字符设备，会误判为 TTY，见 pty-isatty-testing-traps）。
var isTerminalFn = isTerminalStdin

// runChat 启动 REPL 会话。
// Python: cli.py:203-228 _run_chat
//
// 装配后端 + 会话存储，跑 REPL；API key 缺失时由 LoadConfig 报错。
// ⏵ REPL 主循环 TTY 部分（⏵ repl 包）。
func runChat(opts cliOptions, _ *os.File) error {
	cfg, err := loadConfig(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	backend, err := agent.CreateBackend(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	if err := backend.Start(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	defer func() { _ = backend.Stop(context.Background()) }()

	store, err := storage.NewSessionStore("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	store.NewSession(backend.SessionID(), cfg.Model)
	// ⏵ run_repl 主循环依赖 TTY（⏵ repl 包）；骨架下打印可用命令后返回
	_ = repl.SortedSkillNames()
	return nil
}

// runOnce 非交互执行。
// Python: cli.py:230-243 _run_once
func runOnce(opts cliOptions, prompt, outputFormat string) int {
	cfg, err := loadConfig(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		return 1
	}
	return runner.RunOnce(cfg, prompt, outputFormat)
}

// isTerminalStdin stdin 是否是终端。
// Python: sys.stdin.isatty()（stat 模式 & ModeCharDevice）
func isTerminalStdin() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// readStdin 读全部标准输入并去首尾空白。
func readStdin() string {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	var builder strings.Builder
	for scanner.Scan() {
		builder.WriteString(scanner.Text())
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}
