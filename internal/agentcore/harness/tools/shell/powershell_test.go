//go:build test

package shell

import (
	"encoding/json"
	"testing"
)

// TestPowerShellInput_未传Timeout 测试输入未带 timeout 时解码为 0 且回落默认 300 秒。
// 回归：缺失 key 解码为 0，曾被钳制成 1 秒。
func TestPowerShellInput_未传Timeout(t *testing.T) {
	var input PowerShellInput
	payload := `{"command":"curl -s -i http://127.0.0.1:19000/","description":"获取首页"}`
	if err := json.Unmarshal([]byte(payload), &input); err != nil {
		t.Fatalf("unmarshal 失败: %v", err)
	}
	if input.Timeout != 0 {
		t.Fatalf("未传 timeout 应解码为 0，got %d", input.Timeout)
	}
	if got := resolvePSTimeout(input.Timeout); got != psDefaultTimeout {
		t.Errorf("未传 timeout 应回落 %d 秒，got %d", psDefaultTimeout, got)
	}
}

// TestResolvePSTimeout_正常值 测试正常超时值
func TestResolvePSTimeout_正常值(t *testing.T) {
	if got := resolvePSTimeout(120); got != 120 {
		t.Errorf("120 应保持 120，got %d", got)
	}
}

// TestResolvePSTimeout_零值 测试零超时值回落默认 300 秒
func TestResolvePSTimeout_零值(t *testing.T) {
	if got := resolvePSTimeout(0); got != psDefaultTimeout {
		t.Errorf("0 应按默认 %d 处理，got %d", psDefaultTimeout, got)
	}
}

// TestResolvePSTimeout_负值 测试负超时值回落默认 300 秒
func TestResolvePSTimeout_负值(t *testing.T) {
	if got := resolvePSTimeout(-5); got != psDefaultTimeout {
		t.Errorf("负值应按默认 %d 处理，got %d", psDefaultTimeout, got)
	}
}

// TestResolvePSTimeout_超大值 测试超大超时值
func TestResolvePSTimeout_超大值(t *testing.T) {
	if got := resolvePSTimeout(10000); got != 3600 {
		t.Errorf("超过最大值应钳制为 3600，got %d", got)
	}
}

// TestResolvePSTimeout_环境变量上限 测试环境变量覆盖最大超时
func TestResolvePSTimeout_环境变量上限(t *testing.T) {
	t.Setenv("POWER_SHELL_TOOL_MAX_TIMEOUT_SECONDS", "60")
	if got := resolvePSTimeout(600); got != 60 {
		t.Errorf("环境变量上限 60 应生效，got %d", got)
	}
}

// TestResolvePSTimeout_环境变量非法 测试环境变量非法时回落 3600
func TestResolvePSTimeout_环境变量非法(t *testing.T) {
	t.Setenv("POWER_SHELL_TOOL_MAX_TIMEOUT_SECONDS", "abc")
	if got := resolvePSTimeout(10000); got != 3600 {
		t.Errorf("非法环境变量应回落 3600，got %d", got)
	}
}

// TestResolvePSMaxOutputChars_零值 测试零输出字符数表示无限制
func TestResolvePSMaxOutputChars_零值(t *testing.T) {
	if got := resolvePSMaxOutputChars(0); got != 0 {
		t.Errorf("0 表示无限制，应保持 0，got %d", got)
	}
}

// TestResolvePSMaxOutputChars_正常值 测试正常输出字符数
func TestResolvePSMaxOutputChars_正常值(t *testing.T) {
	if got := resolvePSMaxOutputChars(5000); got != 5000 {
		t.Errorf("5000 应保持 5000，got %d", got)
	}
}

// TestResolvePSMaxOutputChars_过小值 测试过小输出字符数钳制为 200
func TestResolvePSMaxOutputChars_过小值(t *testing.T) {
	if got := resolvePSMaxOutputChars(50); got != 200 {
		t.Errorf("过小值应钳制为 200，got %d", got)
	}
}

// TestResolvePSMaxOutputChars_超大值 测试超大输出字符数钳制为 20000
func TestResolvePSMaxOutputChars_超大值(t *testing.T) {
	if got := resolvePSMaxOutputChars(50000); got != 20000 {
		t.Errorf("超过最大值应钳制为 20000，got %d", got)
	}
}

// TestResolvePSMaxOutputChars_环境变量上限 测试环境变量覆盖最大输出字符数
func TestResolvePSMaxOutputChars_环境变量上限(t *testing.T) {
	t.Setenv("POWER_SHELL_TOOL_MAX_OUTPUT_CHARS", "500")
	if got := resolvePSMaxOutputChars(5000); got != 500 {
		t.Errorf("环境变量上限 500 应生效，got %d", got)
	}
}
