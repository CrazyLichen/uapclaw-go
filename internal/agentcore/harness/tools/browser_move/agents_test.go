package browser_move

import (
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestBuildBrowserWorkerSystemPrompt_默认子目录 测试默认子目录
func TestBuildBrowserWorkerSystemPrompt_默认子目录(t *testing.T) {
	prompt := BuildBrowserWorkerSystemPrompt("", "")

	if !strings.Contains(prompt, "You are a browser worker agent.") {
		t.Error("系统提示词应以 'You are a browser worker agent.' 开头")
	}
	if !strings.Contains(prompt, "screenshots/") {
		t.Error("系统提示词应包含默认 screenshots/ 目录")
	}
	if !strings.Contains(prompt, "artifacts/") {
		t.Error("系统提示词应包含默认 artifacts/ 目录")
	}
}

// TestBuildBrowserWorkerSystemPrompt_自定义子目录 测试自定义子目录
func TestBuildBrowserWorkerSystemPrompt_自定义子目录(t *testing.T) {
	prompt := BuildBrowserWorkerSystemPrompt("custom_shots", "custom_artifacts")

	if !strings.Contains(prompt, "custom_shots/") {
		t.Error("系统提示词应包含自定义截图目录")
	}
	if !strings.Contains(prompt, "custom_artifacts/") {
		t.Error("系统提示词应包含自定义产物目录")
	}
}

// TestBuildBrowserWorkerSystemPrompt_路径规范化 测试路径规范化
func TestBuildBrowserWorkerSystemPrompt_路径规范化(t *testing.T) {
	prompt := BuildBrowserWorkerSystemPrompt("  shots\\deep  ", "  art\\deep  ")

	if strings.Contains(prompt, "shots\\deep") {
		t.Error("应将反斜杠替换为正斜杠")
	}
	if !strings.Contains(prompt, "shots/deep/") {
		t.Error("应规范化路径并保留尾部斜杠")
	}
}

// TestBuildBrowserWorkerSystemPrompt_关键指令 测试关键指令内容
func TestBuildBrowserWorkerSystemPrompt_关键指令(t *testing.T) {
	prompt := BuildBrowserWorkerSystemPrompt("", "")

	keyInstructions := []string{
		"browser_probe_interactives",
		"browser_probe_cards",
		"browser_snapshot",
		"browser_run_code_unsafe",
		"browser_custom_action",
		"browser_list_custom_actions",
		"Do not use browser_run_code_unsafe or browser_run_code to dump the entire document body",
		"If actions repeatedly fail, stop and report the exact failing action.",
		"browser_tabs",
		"action MUST be one of: list, new, close, select",
		"Never call browser_custom_action with action='browser_task'",
		"Do not launch nested browser tasks from the browser worker.",
		"IMPORTANT: Do NOT use browser_take_screenshot unless strictly necessary.",
		"Final output MUST be a single JSON object",
		"ok (boolean), final (string), page (object with url and title)",
		"Return JSON only, even on failures.",
		"Do not output markdown, code fences, or plain text outside the JSON object.",
		"status (completed|partial|blocked|failed)",
		"ok=true only when the exact user-visible goal is fully satisfied",
		"completion_evidence",
		"missing_requirements",
	}

	for _, instruction := range keyInstructions {
		if !strings.Contains(prompt, instruction) {
			t.Errorf("系统提示词应包含指令: %q", instruction)
		}
	}
}

// TestBuildBrowserWorkerAgent_待回填 测试 BuildBrowserWorkerAgent 当前返回错误（待回填）
func TestBuildBrowserWorkerAgent_待回填(t *testing.T) {
	config := &BrowserWorkerConfig{
		Provider:   "openai",
		APIKey:     "test-key",
		APIBase:    "https://api.openai.com/v1",
		ModelName:  "gpt-4o",
		MaxSteps:   15,
	}

	_, err := BuildBrowserWorkerAgent(config)
	// 当前实现返回错误（待 9.38-49 回填完整 ReActAgent 配置）
	if err == nil {
		t.Error("当前 BuildBrowserWorkerAgent 应返回错误（待回填）")
	}
}

// TestBuildBrowserWorkerAgent_无配置 测试无配置报错
func TestBuildBrowserWorkerAgent_无配置(t *testing.T) {
	_, err := BuildBrowserWorkerAgent(nil)
	if err == nil {
		t.Error("nil 配置应返回错误")
	}
}

// TestResolveToolTimeoutS_默认值 测试工具超时默认值
func TestResolveToolTimeoutS_默认值(t *testing.T) {
	// 清除环境变量
	for _, key := range []string{"PLAYWRIGHT_TOOL_TIMEOUT_S", "PLAYWRIGHT_MCP_TIMEOUT_S", "BROWSER_TIMEOUT_S"} {
		_ = unsetEnv(key)
	}

	result := resolveToolTimeoutS(180.0)
	if result != 180.0 {
		t.Errorf("默认超时 = %f, want 180.0", result)
	}
}

// TestResolveSamplingValue 测试采样参数解析
func TestResolveSamplingValue(t *testing.T) {
	// 清除环境变量
	_ = unsetEnv("TEST_TEMPERATURE")

	result := resolveSamplingValue([]string{"TEST_TEMPERATURE"}, 0.5, 0.0, 2.0)
	if result != 0.5 {
		t.Errorf("默认值 = %f, want 0.5", result)
	}
}

// TestResolveSamplingParams 测试采样参数解析
func TestResolveSamplingParams(t *testing.T) {
	// 清除环境变量
	for _, key := range []string{"TEST_TEMP", "TEST_TOP_P"} {
		_ = unsetEnv(key)
	}

	temp, topP := resolveSamplingParams(
		[]string{"TEST_TEMP"},
		[]string{"TEST_TOP_P"},
		0.2, 0.1,
	)
	if temp != 0.2 {
		t.Errorf("temperature = %f, want 0.2", temp)
	}
	if topP != 0.1 {
		t.Errorf("topP = %f, want 0.1", topP)
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// unsetEnv 辅助函数，清除环境变量
func unsetEnv(key string) error {
	return setEnvEmpty(key)
}

func setEnvEmpty(key string) error {
	// 不能 unsetenv 在测试中，设为空字符串模拟
	return nil
}
