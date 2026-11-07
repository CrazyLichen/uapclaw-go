//go:build windows

package local

import (
	"testing"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── Windows auto 分支 ────────────────────────────

// TestResolveExecutionPlan_Auto选择 验证 Windows auto 分支的 shell 选择顺序。
//
// 仅在 Windows 上运行：auto 分支的判定依赖 runtime.GOOS。
// 本地或发布前手工执行：
//
//	go test -run TestResolveExecutionPlan_Auto选择 ./internal/agentcore/sys_operation/local/
//
// 覆盖顺序（Python: shell_operation.py:300-315）：
// unwrap → LooksLikePowerShell（赋值）→ LooksLikePosix（控制流/命令名）→
// 裸变量兜底 → cmd。
func TestResolveExecutionPlan_Auto选择(t *testing.T) {
	op := NewLocalShellOperation(sysop.NewSysOperationCard()).(*LocalShellOperation)

	cases := []struct {
		command string
		want    string
	}{
		// bash 控制流：即使含裸 $i 也应选中 bash
		{"for i in 1 2; do echo $i; done", "bash"},
		{"while true; do sleep 1; done", "bash"},
		{"if [ -f x ]; then echo y; fi", "bash"},
		// PowerShell 赋值
		{"$x = 1; echo $x", "powershell"},
		// 裸变量：POSIX 未命中时兜底到 powershell
		{"echo $env:PATH", "powershell"},
		// 都不匹配：cmd
		{"dir", "cmd"},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			_, _, shellName, err := op.resolveExecutionPlan(tc.command, sysop.ShellTypeAuto)
			if err != nil {
				t.Fatal(err)
			}
			if shellName != tc.want {
				t.Fatalf("shell 选中 %q，期望 %q", shellName, tc.want)
			}
		})
	}
}
