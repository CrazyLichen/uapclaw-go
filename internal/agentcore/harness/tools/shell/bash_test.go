//go:build test

package shell

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestBashInput_未传Timeout 测试输入未带 timeout 时解码为 0 且回落默认 300 秒。
// 回归：缺失 key 解码为 0，曾被钳制成 1 秒，导致所有未传 timeout 的命令
// 1 秒即超时（Python inputs.get("timeout", 300) 取 300）。
func TestBashInput_未传Timeout(t *testing.T) {
	var input BashInput
	payload := `{"command":"curl -s -i http://127.0.0.1:19000/","description":"获取首页"}`
	if err := json.Unmarshal([]byte(payload), &input); err != nil {
		t.Fatalf("unmarshal 失败: %v", err)
	}
	if input.Timeout != 0 {
		t.Fatalf("未传 timeout 应解码为 0，got %d", input.Timeout)
	}
	if got := resolveBashTimeout(input.Timeout); got != bashDefaultTimeout {
		t.Errorf("未传 timeout 应回落 %d 秒，got %d", bashDefaultTimeout, got)
	}
}

// TestMakeSudoNoninteractive 测试注入 sudo -n
func TestMakeSudoNoninteractive(t *testing.T) {
	result := makeSudoNoninteractive("sudo apt-get update")
	if result != "sudo -n apt-get update" {
		t.Errorf("got %q, want %q", result, "sudo -n apt-get update")
	}
}

// TestMakeSudoNoninteractive_无sudo 测试无 sudo 命令
func TestMakeSudoNoninteractive_无sudo(t *testing.T) {
	result := makeSudoNoninteractive("ls -la")
	if result != "ls -la" {
		t.Errorf("无 sudo 命令应不变，got %q", result)
	}
}

// TestResolveBashTimeout_正常值 测试正常超时值
func TestResolveBashTimeout_正常值(t *testing.T) {
	if resolveBashTimeout(120) != 120 {
		t.Errorf("120 应保持 120")
	}
}

// TestResolveBashTimeout_零值 测试零超时值回落默认 300 秒
func TestResolveBashTimeout_零值(t *testing.T) {
	if got := resolveBashTimeout(0); got != bashDefaultTimeout {
		t.Errorf("0 应按默认 %d 处理，got %d", bashDefaultTimeout, got)
	}
}

// TestResolveBashTimeout_负值 测试负超时值回落默认 300 秒
func TestResolveBashTimeout_负值(t *testing.T) {
	if got := resolveBashTimeout(-5); got != bashDefaultTimeout {
		t.Errorf("负值应按默认 %d 处理，got %d", bashDefaultTimeout, got)
	}
}

// TestResolveBashTimeout_超大值 测试超大超时值
func TestResolveBashTimeout_超大值(t *testing.T) {
	if resolveBashTimeout(10000) != 3600 {
		t.Errorf("超过最大值应钳制为 3600，got %d", resolveBashTimeout(10000))
	}
}

// TestResolveBashMaxOutputChars_正常值 测试正常输出字符数
func TestResolveBashMaxOutputChars_正常值(t *testing.T) {
	if resolveBashMaxOutputChars(5000) != 5000 {
		t.Errorf("5000 应保持 5000")
	}
}

// TestResolveBashMaxOutputChars_零值 测试零输出字符数
func TestResolveBashMaxOutputChars_零值(t *testing.T) {
	if resolveBashMaxOutputChars(0) != 0 {
		t.Errorf("0 表示无限制，应保持 0")
	}
}

// TestResolveBashMaxOutputChars_过小值 测试过小输出字符数
func TestResolveBashMaxOutputChars_过小值(t *testing.T) {
	if resolveBashMaxOutputChars(50) != 200 {
		t.Errorf("过小值应钳制为 200")
	}
}

// TestResolveBashMaxOutputChars_超大值 测试超大输出字符数
func TestResolveBashMaxOutputChars_超大值(t *testing.T) {
	if resolveBashMaxOutputChars(50000) != 20000 {
		t.Errorf("超过最大值应钳制为 20000，got %d", resolveBashMaxOutputChars(50000))
	}
}

// TestRoundElapsed 测试秒数保留两位小数
func TestRoundElapsed(t *testing.T) {
	tests := []struct {
		in   float64
		want float64
	}{
		{0.0, 0.0},
		{1.0, 1.0},
		{1.234, 1.23},
		{1.235, 1.24},
		{1.999, 2.0},
		{123.456, 123.46},
	}
	for _, tt := range tests {
		got := roundElapsed(tt.in)
		if got != tt.want {
			t.Errorf("roundElapsed(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
