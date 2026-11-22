package evaluator

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	sysop "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation"
)

// intPtr / strPtr 测试助手（Python 字面 None→nil 对齐）。
func intPtr(v int) *int { return &v }

// ──────────────────────────── 测试桩（对齐 Python monkeypatch subprocess.run） ────────────────────────────

// tbRunCall 单次命令调用记录（断言调用参数序列与超时/输入）。
type tbRunCall struct {
	Name       string
	Args       []string
	TimeoutSec float64
	Input      []byte
}

// fakeTBRun 可编程命令桩：按调用次数依序返回脚本输出。
type fakeTBRun struct {
	calls  []tbRunCall
	steps  []tbRunResultErr
	defRes tbRunResultErr
}

// tbRunResultErr 单步桩返回（Python fake subprocess.run 的 return_value）。
type tbRunResultErr struct {
	res tbRunResult
	err error
}

// handler 逐次弹出 steps；耗尽后回落 defRes。
func (f *fakeTBRun) run(name string, args []string, timeoutSec float64, input []byte) (tbRunResult, error) {
	f.calls = append(f.calls, tbRunCall{Name: name, Args: args, TimeoutSec: timeoutSec, Input: input})
	if len(f.steps) > 0 {
		step := f.steps[0]
		f.steps = f.steps[1:]
		return step.res, step.err
	}
	return f.defRes.res, f.defRes.err
}

// installTBRunFake 替换包级 terminalBenchRun 并返回恢复函数（Python monkeypatch 等价）。
func installTBRunFake(t *testing.T, fake *fakeTBRun) {
	t.Helper()
	orig := terminalBenchRun
	terminalBenchRun = fake.run
	t.Cleanup(func() { terminalBenchRun = orig })
}

// deadlineResult 造一个"宿主侧超时"桩返回（对齐 Python subprocess.TimeoutExpired：
// 带部分输出 + 超时错误）。
func deadlineResult() tbRunResultErr {
	return tbRunResultErr{res: tbRunResult{Stdout: "partial-out\n", Stderr: "partial-err\n"}, err: errTBDeadline}
}

// ──────────────────────────── 用例 ────────────────────────────

func TestTerminalBenchCommandRecorder_80条上限(t *testing.T) {
	rec := NewTerminalBenchCommandRecorder()
	for i := 0; i < 100; i++ {
		rec.Record("cmd", "/tmp", intPtr(0), "", "", nil, false)
	}
	if got := len(rec.Entries); got != 80 {
		t.Fatalf("entries = %d, want 80", got)
	}
}

func TestTerminalBenchCommandRecorder_2000字符截断(t *testing.T) {
	rec := NewTerminalBenchCommandRecorder()
	long := strings.Repeat("x", 3000)
	rec.Record("cmd", "/tmp", intPtr(0), long, long, nil, false)
	entry := rec.Entries[0]
	if got := len(entry.StdoutExcerpt); got != 2000 {
		t.Fatalf("stdout_excerpt len = %d, want 2000", got)
	}
	if !strings.HasSuffix(entry.StdoutExcerpt, "...") {
		t.Fatalf("excerpt must end with ...: %q", entry.StdoutExcerpt[1990:])
	}
}

func TestTerminalBenchCommandRecorder_ToList字段键对齐Python(t *testing.T) {
	rec := NewTerminalBenchCommandRecorder()
	rec.Record("ls -la", "/testbed", intPtr(2), "out", "err", intPtr(30), true)
	list := rec.ToList()
	if len(list) != 1 {
		t.Fatalf("len = %d, want 1", len(list))
	}
	entry := list[0]
	for _, key := range []string{"command", "cwd", "exit_code", "stdout_excerpt", "stderr_excerpt", "timeout_sec", "background"} {
		if _, ok := entry[key]; !ok {
			t.Fatalf("missing key %q (Python to_list 键逐字对齐)", key)
		}
	}
	if entry["command"] != "ls -la" || entry["background"] != true {
		t.Fatalf("values mismatch: %#v", entry)
	}
	if got := *(entry["exit_code"].(*int)); got != 2 {
		t.Fatalf("exit_code = %d, want 2", got)
	}
	if got := *(entry["timeout_sec"].(*int)); got != 30 {
		t.Fatalf("timeout_sec = %d, want 30", got)
	}
}

func TestTerminalBenchSolverContainerName_生成规则(t *testing.T) {
	name := TerminalBenchSolverContainerName("Case_01/中文!")
	if !strings.HasPrefix(name, "ach-tb-solver-case-01-") {
		t.Fatalf("unsafe chars must map to '-': %q", name)
	}
	suffix := strings.TrimPrefix(name, "ach-tb-solver-")
	if len(strings.SplitN(suffix, "-", 2)[0]) > 48 {
		t.Fatalf("safe segment must be truncated to 48: %q", suffix)
	}
	empty := TerminalBenchSolverContainerName("")
	if !strings.HasPrefix(empty, "ach-tb-solver-case-") {
		t.Fatalf("empty case_id must fall back to 'case': %q", empty)
	}
}

func TestRunDocker_check失败报错且含stdout与stderr(t *testing.T) {
	fake := &fakeTBRun{defRes: tbRunResultErr{res: tbRunResult{Stdout: "out-line", Stderr: "err-line", ExitCode: 3}}}
	installTBRunFake(t, fake)
	_, err := runDocker([]string{"docker", "failing"}, true, 0)
	if err == nil {
		t.Fatal("check=true 非零退出必须报错")
	}
	for _, want := range []string{"docker failing", "out-line", "err-line"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q: %v", want, err)
		}
	}
}

func TestRunDocker_checkFalse不报错(t *testing.T) {
	fake := &fakeTBRun{defRes: tbRunResultErr{res: tbRunResult{Stderr: "already", ExitCode: 1}}}
	installTBRunFake(t, fake)
	res, err := runDocker([]string{"docker", "rm", "-f", "x"}, false, 0)
	if err != nil {
		t.Fatalf("check=false must not error: %v", err)
	}
	if res.ExitCode != 1 {
		t.Fatalf("exit code = %d, want 1", res.ExitCode)
	}
}

func TestStartTerminalBenchSolverContainer_调用序(t *testing.T) {
	fake := &fakeTBRun{defRes: tbRunResultErr{res: tbRunResult{ExitCode: 0}}}
	installTBRunFake(t, fake)
	ws := t.TempDir()
	name, err := StartTerminalBenchSolverContainer(StartSolverContainerParams{
		DockerImage:           "img:1",
		CaseID:                "case-1",
		WorkspaceDir:          ws,
		TimeoutSec:            60,
		ContainerWorkspaceDir: "/testbed",
		MountWorkspace:        true, // Python 默认 mount_workspace=True
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "ach-tb-solver-case-1-") {
		t.Fatalf("unexpected container name %q", name)
	}
	if len(fake.calls) != 3 {
		t.Fatalf("calls = %d, want 3: %#v", len(fake.calls), fake.calls)
	}
	if fake.calls[0].Name != "docker" || strings.Join(fake.calls[0].Args, " ") != "rm -f "+name {
		t.Fatalf("call0 = %v %v, want rm -f", fake.calls[0].Name, fake.calls[0].Args)
	}
	create := strings.Join(fake.calls[1].Args, " ")
	for _, want := range []string{"create --name " + name, "--workdir /testbed",
		"--mount type=bind,source=" + ws + ",target=/testbed", "img:1", "sleep infinity"} {
		if !strings.Contains(create, want) {
			t.Fatalf("create missing %q: %s", want, create)
		}
	}
	if fake.calls[1].TimeoutSec != 60 || fake.calls[2].TimeoutSec != 60 {
		t.Fatalf("create/start timeouts must be timeout_sec: %#v", fake.calls[1:])
	}
	if strings.Join(fake.calls[2].Args, " ") != "start "+name {
		t.Fatalf("call2 = %v", fake.calls[2].Args)
	}
}

func TestStartTerminalBenchSolverContainer_mountWorkspaceFalse不挂载(t *testing.T) {
	fake := &fakeTBRun{defRes: tbRunResultErr{res: tbRunResult{ExitCode: 0}}}
	installTBRunFake(t, fake)
	ws := t.TempDir()
	if _, err := StartTerminalBenchSolverContainer(StartSolverContainerParams{
		DockerImage: "img:1", CaseID: "c", WorkspaceDir: ws, TimeoutSec: 60,
		ContainerWorkspaceDir: "/testbed", MountWorkspace: false,
	}); err != nil {
		t.Fatal(err)
	}
	create := strings.Join(fake.calls[1].Args, " ")
	if strings.Contains(create, "--mount") {
		t.Fatalf("mount_workspace=False must omit --mount: %s", create)
	}
}

func TestSyncContainerGitPatchToWorkspace_链路(t *testing.T) {
	dir := t.TempDir()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not available: %v", err)
	}
	diff := "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n"
	fake := &fakeTBRun{steps: []tbRunResultErr{
		// 第 1 步：容器内 git diff（run_docker timeout=120）。
		{res: tbRunResult{Stdout: diff, ExitCode: 0}},
		// 第 2 步：宿主 git apply（Python shutil.which("git") + subprocess.run timeout=120）。
		{res: tbRunResult{ExitCode: 0}},
	}}
	installTBRunFake(t, fake)
	got, err := SyncContainerGitPatchToWorkspace(SyncPatchParams{
		ContainerName: "c1", WorkspaceDir: dir, ContainerWorkspaceDir: "/testbed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != diff {
		t.Fatalf("returned diff mismatch: %q", got)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("calls = %d, want [docker diff, git apply]", len(fake.calls))
	}
	apply := fake.calls[1]
	if apply.Name != gitPath || !strings.Contains(strings.Join(apply.Args, " "), "apply --binary --whitespace=nowarn -") {
		t.Fatalf("git apply args mismatch: %v %v", apply.Name, apply.Args)
	}
	if string(apply.Input) != diff {
		t.Fatalf("patch must be piped to stdin: %q", string(apply.Input))
	}
}

func TestSyncContainerGitPatchToWorkspace_apply失败报RuntimeError语义(t *testing.T) {
	fake := &fakeTBRun{steps: []tbRunResultErr{
		{res: tbRunResult{Stdout: "diff\n", ExitCode: 0}},
		{res: tbRunResult{Stdout: "apply-out", Stderr: "apply-err", ExitCode: 128}},
	}}
	installTBRunFake(t, fake)
	_, err := SyncContainerGitPatchToWorkspace(SyncPatchParams{
		ContainerName: "c1", WorkspaceDir: t.TempDir(), ContainerWorkspaceDir: "/testbed",
	})
	if err == nil || !strings.Contains(err.Error(), "failed to sync solver patch") || !strings.Contains(err.Error(), "apply-err") {
		t.Fatalf("expected sync failure with stderr detail, got %v", err)
	}
}

func TestSyncContainerGitPatchToWorkspace_空diff直接返回(t *testing.T) {
	fake := &fakeTBRun{defRes: tbRunResultErr{res: tbRunResult{Stdout: "", ExitCode: 0}}}
	installTBRunFake(t, fake)
	diff, err := SyncContainerGitPatchToWorkspace(SyncPatchParams{
		ContainerName: "c1", WorkspaceDir: t.TempDir(), ContainerWorkspaceDir: "/testbed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if diff != "" {
		t.Fatalf("empty diff must return empty, got %q", diff)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("empty diff must skip git apply, calls = %d", len(fake.calls))
	}
}

func TestRemoveTerminalBenchContainer_checkFalse(t *testing.T) {
	fake := &fakeTBRun{defRes: tbRunResultErr{res: tbRunResult{Stderr: "no such", ExitCode: 1}}}
	installTBRunFake(t, fake)
	RemoveTerminalBenchContainer("c1") // 不应 panic/报错
	if strings.Join(fake.calls[0].Args, " ") != "rm -f c1" {
		t.Fatalf("rm args mismatch: %v", fake.calls[0].Args)
	}
}

func TestShellOperation_ContainerCwd映射(t *testing.T) {
	host := t.TempDir()
	op := NewTerminalBenchDockerShellOperation(ShellOpParams{
		ContainerName: "c1", HostWorkspaceDir: host, ContainerWorkspaceDir: "/testbed",
	})
	if got := op.containerCwd(""); got != "/testbed" {
		t.Fatalf("empty cwd → default, got %q", got)
	}
	if got := op.containerCwd(filepath.Join(t.TempDir(), "elsewhere")); got != "/testbed" {
		t.Fatalf("outside cwd → default, got %q", got)
	}
	if got := op.containerCwd(host); got != "/testbed" {
		t.Fatalf("host root → default, got %q", got)
	}
	if got := op.containerCwd(filepath.Join(host, "sub")); got != "/testbed/sub" {
		t.Fatalf("subdir mapping, got %q", got)
	}
}

func TestShellOperation_DockerExecCommand组装(t *testing.T) {
	op := NewTerminalBenchDockerShellOperation(ShellOpParams{
		ContainerName: "c1", HostWorkspaceDir: t.TempDir(),
		ContainerWorkspaceDir:  "/testbed",
		CleanShell:             true,
		RuntimeEnvironment:     map[string]string{"B": "2"},
		EnforceInContainerTime: true,
	})
	cmd := op.dockerExecCommand("pytest -q", dockerExecParams{
		Cwd: "/testbed", Environment: map[string]string{"A": "1"},
		Detached: false, Timeout: intPtr(30),
	})
	joined := strings.Join(cmd, "\x00")
	// 环境变量合并后按 key 排序：A=1, B=2（Python sorted(merged.items())）。
	if !strings.Contains(joined, "-e\x00A=1\x00-e\x00B=2") {
		t.Fatalf("env must merge and sort: %s", joined)
	}
	// clean_shell: bash --noprofile --norc -c
	if !strings.Contains(joined, "bash\x00--noprofile\x00--norc\x00-c\x00pytest -q") {
		t.Fatalf("clean shell form missing: %s", joined)
	}
	// enforce timeout: timeout --signal=TERM --kill-after=5s 30s 前缀。
	if !strings.Contains(joined, "timeout\x00--signal=TERM\x00--kill-after=5s\x0030s\x00bash") {
		t.Fatalf("in-container timeout prefix missing: %s", joined)
	}
	if !strings.Contains(joined, "-w\x00/testbed\x00c1") {
		t.Fatalf("-w + container name missing: %s", joined)
	}
}

func TestShellOperation_DockerExecCommand_非clean带lc(t *testing.T) {
	op := NewTerminalBenchDockerShellOperation(ShellOpParams{
		ContainerName: "c1", HostWorkspaceDir: t.TempDir(),
	})
	cmd := op.dockerExecCommand("echo hi", dockerExecParams{Cwd: "/app", Detached: true})
	joined := strings.Join(cmd, "\x00")
	if !strings.Contains(joined, "bash\x00-lc\x00echo hi") {
		t.Fatalf("non-clean shell form missing: %s", joined)
	}
	if !strings.Contains(joined, "-d") {
		t.Fatalf("detached flag missing: %s", joined)
	}
	if strings.Contains(joined, "timeout\x00") {
		t.Fatalf("timeout prefix must be absent without enforce: %s", joined)
	}
}

func TestShellOperation_ExecuteCmd成功路径(t *testing.T) {
	fake := &fakeTBRun{defRes: tbRunResultErr{res: tbRunResult{Stdout: "hello\n", ExitCode: 0}}}
	installTBRunFake(t, fake)
	op := NewTerminalBenchDockerShellOperation(ShellOpParams{
		ContainerName: "c1", HostWorkspaceDir: t.TempDir(),
		ContainerWorkspaceDir: "/testbed",
		Recorder:              NewTerminalBenchCommandRecorder(),
	})
	res, err := op.ExecuteCmd(context.Background(), "echo hello",
		sysop.WithShellCwd("/testbed"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 0 {
		t.Fatalf("code = %d, want 0", res.Code)
	}
	if res.Data.Stdout != "hello\n" || res.Data.ExitCode == nil || *res.Data.ExitCode != 0 {
		t.Fatalf("data mismatch: %#v", res.Data)
	}
	if res.Data.Cwd != "/testbed" {
		t.Fatalf("cwd mismatch: %q", res.Data.Cwd)
	}
	// 调用参数必须走 _docker_exec_command 组装（docker exec -w /testbed c1 bash -lc …）。
	call := fake.calls[0]
	joined := strings.Join(call.Args, "\x00")
	if !strings.Contains(joined, "exec\x00-w\x00/testbed\x00c1\x00bash\x00-lc\x00echo hello") {
		t.Fatalf("docker exec command mismatch: %s", joined)
	}
	if len(op.CommandLog()) != 1 {
		t.Fatal("recorder entry missing")
	}
}

func TestShellOperation_ExecuteCmd超时路径(t *testing.T) {
	fake := &fakeTBRun{defRes: deadlineResult()}
	installTBRunFake(t, fake)
	op := NewTerminalBenchDockerShellOperation(ShellOpParams{
		ContainerName: "c1", HostWorkspaceDir: t.TempDir(),
		ContainerWorkspaceDir: "/testbed",
		Recorder:              NewTerminalBenchCommandRecorder(),
	})
	res, err := op.ExecuteCmd(context.Background(), "sleep 999",
		sysop.WithShellCwd("/testbed"), sysop.WithShellTimeout(1))
	if err != nil {
		t.Fatal(err)
	}
	if res.Data.ExitCode != nil {
		t.Fatalf("timeout → exit_code None, got %d", *res.Data.ExitCode)
	}
	if !strings.Contains(res.Data.Stderr, "command timed out after 1s") {
		t.Fatalf("timeout note missing: %q", res.Data.Stderr)
	}
	entry := op.CommandLog()[0]
	if ec, ok := entry["exit_code"].(*int); !ok || ec != nil {
		t.Fatalf("recorded exit_code must be nil on timeout: %#v", entry)
	}
	if entry["stdout_excerpt"] != "partial-out\n" {
		t.Fatalf("partial stdout must be recorded: %#v", entry)
	}
}

func TestShellOperation_ExecuteCmdStream三块(t *testing.T) {
	fake := &fakeTBRun{defRes: tbRunResultErr{res: tbRunResult{Stdout: "OUT", Stderr: "ERR", ExitCode: 0}}}
	installTBRunFake(t, fake)
	op := NewTerminalBenchDockerShellOperation(ShellOpParams{
		ContainerName: "c1", HostWorkspaceDir: t.TempDir(),
	})
	ch, err := op.ExecuteCmdStream(context.Background(), "cmd")
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for chunk := range ch {
		texts = append(texts, chunk.Data.Text)
	}
	if len(texts) != 3 || texts[0] != "OUT" || texts[1] != "ERR" || texts[2] != "" {
		t.Fatalf("stream chunks = %#v, want [OUT ERR \"\"]", texts)
	}
}

func TestShellOperation_ExecuteCmdBackground(t *testing.T) {
	fake := &fakeTBRun{defRes: tbRunResultErr{res: tbRunResult{ExitCode: 0}}}
	installTBRunFake(t, fake)
	op := NewTerminalBenchDockerShellOperation(ShellOpParams{
		ContainerName: "c1", HostWorkspaceDir: t.TempDir(),
		Recorder: NewTerminalBenchCommandRecorder(),
	})
	res, err := op.ExecuteCmdBackground(context.Background(), "server &")
	if err != nil {
		t.Fatal(err)
	}
	if res.Data.Pid != nil {
		t.Fatalf("pid must be nil (Python pid=None): %#v", res.Data)
	}
	entry := op.CommandLog()[0]
	if entry["background"] != true {
		t.Fatalf("background flag missing: %#v", entry)
	}
	// detached=True 时 timeout 前缀与 -w 都要出现（Python L266-271 detached 组装）。
	joined := strings.Join(fake.calls[0].Args, "\x00")
	if !strings.Contains(joined, "exec\x00-d") {
		t.Fatalf("background must use -d: %s", joined)
	}
}

// tbRunProcess 真实子进程路径（echo + 退出码透传）。
func TestSubprocessRunWithCwd_真实子进程(t *testing.T) {
	res, err := subprocessRunWithCwd("echo", []string{"hello"}, 0, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Stdout != "hello\n" || res.ExitCode != 0 {
		t.Fatalf("result mismatch: %#v", res)
	}
	res, err = subprocessRunWithCwd("sh", []string{"-c", "echo e >&2; exit 3"}, 0, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 3 || res.Stderr != "e\n" {
		t.Fatalf("exit code passthrough mismatch: %#v", res)
	}
	// 超时：1 秒限时跑 sleep 3 → errTBDeadline + 部分输出（Go 侧无输出）。
	_, err = subprocessRunWithCwd("sleep", []string{"3"}, 1, nil, "")
	if !errors.Is(err, errTBDeadline) {
		t.Fatalf("expected deadline error, got %v", err)
	}
	// stdin 输入传递（cat 回显）。
	res, err = subprocessRunWithCwd("cat", nil, 5, []byte("piped"), "")
	if err != nil || res.Stdout != "piped" {
		t.Fatalf("stdin passthrough mismatch: %#v err=%v", res, err)
	}
}

func TestShellOperation_基类未实现转发(t *testing.T) {
	op := NewTerminalBenchDockerShellOperation(ShellOpParams{
		ContainerName: "c1", HostWorkspaceDir: t.TempDir(),
	})
	if _, err := op.WriteStdin(context.Background(), "s", "x"); err == nil {
		t.Fatal("WriteStdin must fall through to base unimplemented error")
	}
	if _, err := op.KillProcess(context.Background(), "s"); err == nil {
		t.Fatal("KillProcess must fall through to base unimplemented error")
	}
	if _, err := op.ListProcesses(context.Background()); err == nil {
		t.Fatal("ListProcesses must fall through to base unimplemented error")
	}
}

func TestBuildTerminalBenchSysOperation_Shell路由与CommandLog(t *testing.T) {
	op := BuildTerminalBenchSysOperation(SysOpBuildParams{
		SysOperationID: "sweb_single_x",
		ContainerName:  "c1",
		WorkspaceDir:   t.TempDir(),
		Recorder:       NewTerminalBenchCommandRecorder(),
	})
	if op.Card() == nil || op.Card().Mode != sysop.OperationModeLocal {
		t.Fatal("card mode must be LOCAL")
	}
	if op.Shell() == nil {
		t.Fatal("shell() must route to docker shell operation")
	}
	if op.CommandLog() == nil {
		t.Fatal("command_log must delegate to shell operation recorder")
	}
	if op.Card().ID != "sweb_single_x" {
		t.Fatalf("card ID = %q, want sweb_single_x", op.Card().ID)
	}
}
