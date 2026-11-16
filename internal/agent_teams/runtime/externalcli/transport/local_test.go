package transport

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLocalTransport_run_起子进程读写 对齐 local.py run + base.py 契约：
// 假 CLI 脚本从 stdin 读一行并回显到 stdout。
func TestLocalTransport_run_起子进程读写(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "echo_cli.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nread line\necho \"got:$line\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	tr := &LocalTransport{}
	proc, err := tr.Run([]string{"/bin/sh", script}, nil, dir)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	defer func() { _, _ = proc.Wait() }()
	if proc.ReturnCode() != -1 {
		t.Fatalf("运行中退出码应为 -1（Python None），got %d", proc.ReturnCode())
	}
	if _, err := proc.Stdin().Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	if err := proc.Stdin().Flush(); err != nil {
		t.Fatal(err)
	}
	if err := proc.Stdin().WriteEOF(); err != nil {
		t.Fatal(err)
	}
	line, err := proc.Stdout().ReadLine()
	if err != nil {
		t.Fatalf("readline: %v", err)
	}
	if got := strings.TrimSpace(string(line)); got != "got:hello" {
		t.Fatalf("got %q", got)
	}
	if !proc.Stdin().CanWriteEOF() {
		t.Fatal("管道写端必须支持 EOF")
	}
}

// TestLocalTransport_run_退出码与Terminate 对齐 ProcessLike 的
// returncode/terminate/wait 面。
func TestLocalTransport_run_退出码与Terminate(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "sleep_cli.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	tr := &LocalTransport{}
	proc, err := tr.Run([]string{"/bin/sh", script}, nil, dir)
	if err != nil {
		t.Fatal(err)
	}
	proc.Terminate()
	code, err := proc.Wait()
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if code == 0 {
		t.Fatal("SIGTERM 终止的进程退出码不应为 0")
	}
	if proc.ReturnCode() != code {
		t.Fatalf("wait 后 returncode 应与 wait 一致: %d vs %d", proc.ReturnCode(), code)
	}
}

// TestLocalTransport_run_空argv报错 对齐 run 的管道缺失报错路径形状
// （Go 侧空 argv 先行拒绝，等价 Python 进程对象无管道时 AGENT_TEAM_EXECUTION_ERROR）。
func TestLocalTransport_run_空argv报错(t *testing.T) {
	_, err := (&LocalTransport{}).Run(nil, nil, "")
	if err == nil || !strings.Contains(err.Error(), "did not expose stdin/stdout pipes") {
		t.Fatalf("got %v", err)
	}
}

// TestLocalTransport_acloseNoop 对齐 aclose 注释：本地 fork 无连接可释放。
func TestLocalTransport_acloseNoop(t *testing.T) {
	if err := (&LocalTransport{}).Aclose(); err != nil {
		t.Fatalf("aclose 应为 no-op: %v", err)
	}
}

// TestLocalTransport_env与cwd 对齐 run 的 env/cwd 应用。
func TestLocalTransport_env与cwd(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "env_cli.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' \"$PROBE_VAR\"\necho \" cwd:$PWD\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	tr := &LocalTransport{}
	proc, err := tr.Run([]string{"/bin/sh", script}, map[string]string{"PROBE_VAR": "xyz"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = proc.Wait() }()
	all, err := io.ReadAll(proc.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(all), "xyz") || !strings.Contains(string(all), dir) {
		t.Fatalf("env/cwd 未生效: %q", string(all))
	}
}

// 编译期契约检查：localProcess 满足 ProcessLike，LocalTransport 满足 ProcessTransport。
var (
	_ ProcessLike      = (*localProcess)(nil)
	_ ProcessTransport = (*LocalTransport)(nil)
)
