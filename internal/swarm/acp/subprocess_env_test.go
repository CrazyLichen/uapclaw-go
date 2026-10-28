package acp

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestBuildAcpSubprocessEnv_继承系统变量(t *testing.T) {
	t.Setenv("ACP_TEST_NORMAL", "sysvalue")
	env := BuildAcpSubprocessEnv(nil)
	if env["ACP_TEST_NORMAL"] != "sysvalue" {
		t.Errorf("普通系统变量应继承，实际 %v", env["ACP_TEST_NORMAL"])
	}
	if _, exists := env["PATH"]; !exists {
		t.Error("PATH 应继承")
	}
}

func TestBuildAcpSubprocessEnv_剥离敏感键(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "leaked")
	t.Setenv("HTTPS_PROXY", "http://leak")
	env := BuildAcpSubprocessEnv(nil)
	if _, exists := env["OPENAI_API_KEY"]; exists {
		t.Error("OPENAI_API_KEY 不应从父进程继承")
	}
	if _, exists := env["HTTPS_PROXY"]; exists {
		t.Error("HTTPS_PROXY 不应从父进程继承")
	}
}

func TestBuildAcpSubprocessEnv_profile覆盖(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "leaked")
	env := BuildAcpSubprocessEnv(map[string]any{
		"OPENAI_API_KEY": "profile-key",
		"EMPTY_VAL":      "  ",
	})
	if env["OPENAI_API_KEY"] != "profile-key" {
		t.Errorf("profile env 应覆盖，实际 %v", env["OPENAI_API_KEY"])
	}
	if _, exists := env["EMPTY_VAL"]; exists {
		t.Error("空白值不应写入")
	}
}

func TestBuildAcpSubprocessEnv_环境变量占位符(t *testing.T) {
	t.Setenv("ACP_TEST_REF", "resolved")
	env := BuildAcpSubprocessEnv(map[string]any{
		"DERIVED": "${ACP_TEST_REF}",
	})
	if env["DERIVED"] != "resolved" {
		t.Errorf("${VAR} 应被解析，实际 %v", env["DERIVED"])
	}
}
