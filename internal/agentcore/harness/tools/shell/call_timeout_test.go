package shell

import "testing"

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestBashCallTimeout_前台加宽限 验证前台命令取得"执行预算 + 30s"。
//
// 外层调用预算必须比内层执行预算多留一段：内层超时要来得及
// kill 进程树并排空管道；两层同时到点会把还在收尾的调用砍掉。
func TestBashCallTimeout_前台加宽限(t *testing.T) {
	got := bashCallTimeout(&BashInput{Timeout: 300, RunInBackground: false}, 300)
	if got != 330 {
		t.Fatalf("前台应为 300+30=330，实际 %v", got)
	}
}

// TestBashCallTimeout_后台不宽限 验证后台模式原样返回默认预算
func TestBashCallTimeout_后台不宽限(t *testing.T) {
	got := bashCallTimeout(&BashInput{Timeout: 300, RunInBackground: true}, 300)
	if got != 300 {
		t.Fatalf("后台应原样返回 300，实际 %v", got)
	}
}

// TestBashCallTimeout_超时钳制后再加宽限 验证宽限基于钳制后的值
func TestBashCallTimeout_超时钳制后再加宽限(t *testing.T) {
	got := bashCallTimeout(&BashInput{Timeout: 99999}, 300)
	if got != 3600+30 {
		t.Fatalf("超时先钳到 3600 再加 30，实际 %v", got)
	}
}

// TestPSCallTimeout_前台加宽限 验证 PowerShell 同构
func TestPSCallTimeout_前台加宽限(t *testing.T) {
	got := psCallTimeout(&PowerShellInput{Timeout: 120, Background: false}, 300)
	if got != 150 {
		t.Fatalf("前台应为 120+30=150，实际 %v", got)
	}
}

// TestPSCallTimeout_后台不宽限 验证后台模式原样返回
func TestPSCallTimeout_后台不宽限(t *testing.T) {
	got := psCallTimeout(&PowerShellInput{Timeout: 120, Background: true}, 300)
	if got != 300 {
		t.Fatalf("后台应原样返回 300，实际 %v", got)
	}
}

// TestCallTimeout_未传timeout 验证未传时按默认 300 再加宽限
func TestCallTimeout_未传timeout(t *testing.T) {
	got := bashCallTimeout(&BashInput{}, 300)
	if got != 330 {
		t.Fatalf("未传 timeout 应按默认 300 再加 30，实际 %v", got)
	}
}
