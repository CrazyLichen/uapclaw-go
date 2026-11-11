package processcli

import "testing"

// TestTranslateArgError_五条规则 逐条对齐 main.py:84-120。
func TestTranslateArgError_五条规则(t *testing.T) {
	cases := map[string]string{
		"argument --mode: invalid choice: x (choose from 'a', 'b')": `参数 --mode 的值无效：x（可选值：'a', 'b'）`,
		"the following arguments are required: prompt":              "缺少必需参数：prompt",
		"unrecognized arguments: --bogus":                           "无法识别的参数：--bogus",
		"argument --limit: expected one argument":                   "参数 --limit 需要一个值",
		"argument --timeout: invalid float value: abc":              "参数 --timeout 的值无效：abc",
		"some other error":                                          "some other error",
	}
	for in, want := range cases {
		if got := translateArgError(in); got != want {
			t.Errorf("translateArgError(%q)\n 得 %q\n 期望 %q", in, got, want)
		}
	}
}

// TestValidateNonInteractive 非交互命令的互斥校验（main.py:401-412）。
func TestValidateNonInteractive(t *testing.T) {
	if err := validateNonInteractive(&AppArgs{Prompt: "x", OutputExplicit: true}); err == nil {
		t.Error("非交互 + 显式 --output 应报错")
	}
	if err := validateNonInteractive(&AppArgs{Prompt: "x", Mode: "agent.work.plan"}); err == nil {
		t.Error("非交互 + plan 模式应报错")
	}
	if err := validateNonInteractive(&AppArgs{Prompt: "x", Mode: "team.work.normal"}); err == nil {
		t.Error("非交互 + team 模式应报错")
	}
	if err := validateNonInteractive(&AppArgs{Prompt: "x", Mode: "agent.code.normal"}); err != nil {
		t.Errorf("合法组合报错：%v", err)
	}
}

// TestValidateInteractive 交互模式的校验（main.py:416-419）。
func TestValidateInteractive(t *testing.T) {
	if err := validateInteractive(&AppArgs{Output: "json"}); err == nil {
		t.Error("交互模式 + 非 human 输出应报错")
	}
	if err := validateInteractive(&AppArgs{Output: "human", Debug: true}); err == nil {
		t.Error("交互模式 + debug 应报错")
	}
	if err := validateInteractive(&AppArgs{Output: "human"}); err != nil {
		t.Errorf("合法组合报错：%v", err)
	}
}

// TestHasLegacyArguments_机器协议独立判定。
func TestHasLegacyArguments(t *testing.T) {
	args := &AppArgs{Mode: defaultMode, Output: defaultOutput}
	if args.hasLegacyArguments() {
		t.Error("纯默认值不应算混用")
	}
	args.Prompt = "x"
	if !args.hasLegacyArguments() {
		t.Error("带 prompt 应算混用")
	}
}

// TestDispatchBranch_查询分支优先 会话查询参数存在时不进入 prompt 执行。
func TestDispatchBranch_查询分支优先(t *testing.T) {
	var called []string
	deps := &mainDeps{
		ExecuteSessionQuery: func(*AppArgs) int { called = append(called, "session"); return 0 },
		RunApp:              func(*AppArgs, any, any) int { called = append(called, "app"); return 0 },
		RunRepl:             func(*AppArgs) int { called = append(called, "repl"); return 0 },
	}
	args := &AppArgs{ListSessions: true, Prompt: "ignored"}
	_ = dispatchBranch(args, deps)
	if len(called) != 1 || called[0] != "session" {
		t.Errorf("调用链 = %v, 期望只有 session", called)
	}
}

// TestDispatchBranch_无prompt走REPL prompt 为空时进交互外壳。
func TestDispatchBranch_无prompt走REPL(t *testing.T) {
	var called []string
	deps := &mainDeps{
		RunApp:  func(*AppArgs, any, any) int { called = append(called, "app"); return 0 },
		RunRepl: func(*AppArgs) int { called = append(called, "repl"); return 0 },
	}
	args := &AppArgs{Mode: defaultMode, Output: defaultOutput}
	_ = dispatchBranch(args, deps)
	if len(called) != 1 || called[0] != "repl" {
		t.Errorf("调用链 = %v, 期望只有 repl", called)
	}
}

// TestDispatchBranch_有prompt走App prompt 非空时进单命令生命周期。
func TestDispatchBranch_有prompt走App(t *testing.T) {
	var called []string
	deps := &mainDeps{
		RunApp:  func(*AppArgs, any, any) int { called = append(called, "app"); return 0 },
		RunRepl: func(*AppArgs) int { called = append(called, "repl"); return 0 },
	}
	args := &AppArgs{Prompt: "hello", Mode: defaultMode, Output: defaultOutput}
	_ = dispatchBranch(args, deps)
	if len(called) != 1 || called[0] != "app" {
		t.Errorf("调用链 = %v, 期望只有 app", called)
	}
}

// TestExpandHome 展开 ~ 前缀。
func TestExpandHome(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	if got := expandHome("~/x"); got != "/home/tester/x" {
		t.Errorf("expandHome(~/x) = %q", got)
	}
	if got := expandHome("/abs"); got != "/abs" {
		t.Errorf("expandHome(/abs) = %q", got)
	}
	if got := expandHome("~"); got != "/home/tester" {
		t.Errorf("expandHome(~) = %q", got)
	}
}
