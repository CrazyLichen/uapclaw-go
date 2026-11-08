package permissions

import "testing"

// TestRequiresNoHostFallback 测试「自动执行必须留在 JiuwenBox」的判定
//
// 两个条件**同时**满足才为真（Python 用 and 连接）：
//   - tool_category 属于 {shell, code}
//   - tool_name 属于 JIUWENBOX_SANDBOX_EXECUTION_TOOLS
func TestRequiresNoHostFallback(t *testing.T) {
	cases := []struct {
		name     string
		toolName string
		category string
		want     bool
	}{
		{"shell 类 + bash", "bash", "shell", true},
		{"shell 类 + mcp_exec_command", "mcp_exec_command", "shell", true},
		{"code 类 + code", "code", "code", true},
		{"shell 类 + cmd（不在盒子集合）", "cmd", "shell", false},
		{"shell 类 + create_terminal（ACP IDE）", "create_terminal", "shell", false},
		{"非 shell/code 类 + bash", "bash", "path", false},
		{"非 shell/code 类 + code", "code", "network", false},
		{"空类别", "bash", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RequiresNoHostFallback(c.toolName, c.category)
			if got != c.want {
				t.Errorf("RequiresNoHostFallback(%q, %q) = %v, want %v",
					c.toolName, c.category, got, c.want)
			}
		})
	}
}

// TestRequiresManualExecutionProviderReview 测试「宿主无法验证执行契约」的判定
//
// 条件：category ∈ {shell, code} 且 tool_name **不**在 JIUWENBOX 集合里。
// 与 RequiresNoHostFallback 互补（同一类别下二者恰有一个为真）。
func TestRequiresManualExecutionProviderReview(t *testing.T) {
	cases := []struct {
		name     string
		toolName string
		category string
		want     bool
	}{
		{"shell 类 + cmd", "cmd", "shell", true},
		{"shell 类 + powershell", "powershell", "shell", true},
		{"shell 类 + create_terminal", "create_terminal", "shell", true},
		{"shell 类 + bash", "bash", "shell", false},
		{"code 类 + code", "code", "code", false},
		{"非 shell/code 类 + cmd", "cmd", "path", false},
		{"非 shell/code 类 + read_file", "read_file", "path", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RequiresManualExecutionProviderReview(c.toolName, c.category)
			if got != c.want {
				t.Errorf("RequiresManualExecutionProviderReview(%q, %q) = %v, want %v",
					c.toolName, c.category, got, c.want)
			}
		})
	}
}

// TestExecutionProviderContract_互补性 测试两个判定在同一类别下互斥且完备
func TestExecutionProviderContract_互补性(t *testing.T) {
	names := []string{"bash", "mcp_exec_command", "code", "cmd", "powershell", "create_terminal"}
	for _, category := range []string{"shell", "code"} {
		for _, name := range names {
			noFallback := RequiresNoHostFallback(name, category)
			manual := RequiresManualExecutionProviderReview(name, category)
			if noFallback == manual {
				t.Errorf("category=%q tool=%q: noFallback=%v manual=%v，二者应恰有一个为真",
					category, name, noFallback, manual)
			}
		}
	}
}

// TestExecutionProviderContractUnverified 测试未验证标记常量
func TestExecutionProviderContractUnverified(t *testing.T) {
	if ExecutionProviderContractUnverified != "execution_provider_contract_unverified" {
		t.Errorf("常量值 = %q，与 Python 不一致", ExecutionProviderContractUnverified)
	}
}
