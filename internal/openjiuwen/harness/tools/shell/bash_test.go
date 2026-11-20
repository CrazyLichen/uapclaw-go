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
	v := 5000
	if resolveBashMaxOutputChars(&v) != 5000 {
		t.Errorf("5000 应保持 5000")
	}
}

// TestResolveBashMaxOutputChars_零值 测试显式 0 表示不限制
func TestResolveBashMaxOutputChars_零值(t *testing.T) {
	zero := 0
	if resolveBashMaxOutputChars(&zero) != 0 {
		t.Errorf("显式 0 表示无限制，应保持 0")
	}
}

// TestResolveBashMaxOutputChars_过小值 测试过小输出字符数
func TestResolveBashMaxOutputChars_过小值(t *testing.T) {
	v := 50
	if resolveBashMaxOutputChars(&v) != 200 {
		t.Errorf("过小值应钳制为 200")
	}
}

// TestResolveBashMaxOutputChars_超大值 测试超大输出字符数
func TestResolveBashMaxOutputChars_超大值(t *testing.T) {
	v := 50000
	if resolveBashMaxOutputChars(&v) != 20000 {
		t.Errorf("超过最大值应钳制为 20000，got %d", resolveBashMaxOutputChars(&v))
	}
}

// TestResolveBashMaxOutputChars_未传取默认 验证 nil（调用方未传）取默认 20000。
// 回归：字段曾是 int，零值无法区分"未传"与"显式 0"，导致默认路径恒为无限制，
// 单条命令 600K token 输出可撑爆上下文。
func TestResolveBashMaxOutputChars_未传取默认(t *testing.T) {
	if got := resolveBashMaxOutputChars(nil); got != 20000 {
		t.Errorf("未传应取默认 20000，got %d", got)
	}
}

// TestResolveBashMaxOutputChars_指针语义 验证 nil/0/正数三分支
func TestResolveBashMaxOutputChars_指针语义(t *testing.T) {
	zero := 0
	small := 100
	normal := 5000
	huge := 999999

	cases := []struct {
		name string
		raw  *int
		want int
	}{
		{"未传取默认 20000", nil, 20000},
		{"显式 0 表示不限制", &zero, 0},
		{"小于 200 钳到 200", &small, 200},
		{"正常值原样", &normal, 5000},
		{"超上限钳到默认上限", &huge, 20000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveBashMaxOutputChars(tc.raw); got != tc.want {
				t.Fatalf("resolveBashMaxOutputChars = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestResolveBashMaxOutputChars_环境变量上限 验证 BASH_TOOL_MAX_OUTPUT_CHARS 参与钳制
func TestResolveBashMaxOutputChars_环境变量上限(t *testing.T) {
	t.Setenv("BASH_TOOL_MAX_OUTPUT_CHARS", "3000")
	v := 9999
	if got := resolveBashMaxOutputChars(&v); got != 3000 {
		t.Fatalf("env cap not applied: got %d, want 3000", got)
	}
	// 环境变量同样钳制默认值
	if got := resolveBashMaxOutputChars(nil); got != 3000 {
		t.Fatalf("env cap not applied to default: got %d, want 3000", got)
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

// TestBashStreamInput_指针字段 验证流式输入结构体的 MaxOutputChars 可区分未传与显式 0
func TestBashStreamInput_指针字段(t *testing.T) {
	var absent BashStreamInput
	if err := json.Unmarshal([]byte(`{"command":"echo hi"}`), &absent); err != nil {
		t.Fatal(err)
	}
	if absent.MaxOutputChars != nil {
		t.Fatalf("未传时应为 nil，实际 %v", *absent.MaxOutputChars)
	}
	if got := resolveBashMaxOutputChars(absent.MaxOutputChars); got != 20000 {
		t.Fatalf("未传应取默认 20000，实际 %d", got)
	}

	var explicitZero BashStreamInput
	if err := json.Unmarshal([]byte(`{"command":"echo hi","max_output_chars":0}`), &explicitZero); err != nil {
		t.Fatal(err)
	}
	if explicitZero.MaxOutputChars == nil || *explicitZero.MaxOutputChars != 0 {
		t.Fatal("显式 0 应被解析为指向 0 的指针")
	}
	if got := resolveBashMaxOutputChars(explicitZero.MaxOutputChars); got != 0 {
		t.Fatalf("显式 0 应为不限制，实际 %d", got)
	}
}
