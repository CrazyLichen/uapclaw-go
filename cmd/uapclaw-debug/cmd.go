package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/agent"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/ui/runner"
)

// Run 执行调试 CLI 并按退出码返回。
//
// 规则 4：main.go 加 //go:build !test 只放 main()，可测逻辑全部在本文件。
//
// 参数极简：--query / --output。有 --query 走单命令（runner.RunOnce），
// 否则走完整命令树（含 chat REPL）。
//
// 不经 harness/cli/cli.py 的参数解析层（spec D2：该文件不交付）。
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	parsed, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	// 有 --query：单命令模式
	if parsed.query != "" {
		cfg, err := agent.LoadConfig(agent.LoadConfigOptions{})
		if err != nil {
			fmt.Fprintf(stderr, "load config: %v\n", err)
			return 1
		}
		return runner.RunOnce(cfg, parsed.query, parsed.output)
	}
	// 无 --query：REPL 装配骨架（⏵ 主循环 TTY 部分，见 repl 包）。
	// 不走命令树的 stdin 分派——go test 与管道下 stdin 是 /dev/null
	// （字符设备），会误判 TTY 进 chat 的 os.Exit 路径。
	cfg, err := agent.LoadConfig(agent.LoadConfigOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	backend, err := agent.CreateBackend(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "create backend: %v\n", err)
		return 1
	}
	if err := backend.Start(context.Background()); err != nil {
		fmt.Fprintf(stderr, "start: %v\n", err)
		return 1
	}
	defer func() { _ = backend.Stop(context.Background()) }()
	fmt.Fprintln(stdout, "REPL ready (interactive loop ⏵ TTY). Use --query for one-shot mode.")
	return 0
}

// debugVersion 调试 CLI 的版本串（与主网关版本解耦，调试用）。
const debugVersion = "debug-0.1.0"

// parsedArgs 解析结果。
type parsedArgs struct {
	// query 单命令查询文本
	query string
	// output 输出格式（text/json/stream-json）
	output string
}

// parseArgs 极简参数解析。
// Python: 无对应（cli.py 的 click 层不交付，spec D2）
func parseArgs(args []string) (parsedArgs, error) {
	parsed := parsedArgs{output: runner.OutputFormatText}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--query":
			if i+1 >= len(args) {
				return parsedArgs{}, fmt.Errorf("--query requires a value")
			}
			i++
			parsed.query = args[i]
		case "--output":
			if i+1 >= len(args) {
				return parsedArgs{}, fmt.Errorf("--output requires a value")
			}
			i++
			if !isSupportedOutput(args[i]) {
				return parsedArgs{}, fmt.Errorf("unknown output format: %s", args[i])
			}
			parsed.output = args[i]
		default:
			if strings.HasPrefix(args[i], "--") {
				return parsedArgs{}, fmt.Errorf("unknown flag: %s", args[i])
			}
		}
	}
	return parsed, nil
}

// isSupportedOutput 输出格式白名单。
func isSupportedOutput(format string) bool {
	switch format {
	case runner.OutputFormatText, runner.OutputFormatJSON, runner.OutputFormatStreamJSON:
		return true
	}
	return false
}
