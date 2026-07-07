//go:build integration

package main

import (
	"strings"
	"testing"
)

// TestAppCmd_Execute 验证 app 子命令执行输出
// 运行方式: go test -tags=integration ./cmd/uapclaw/...
// app 子命令会启动真实 GatewayServer，需隔离避免单元测试超时
func TestAppCmd_Execute(t *testing.T) {
	buf := captureStdout(t, func() {
		rootCmd := newRootCmd()
		rootCmd.SetArgs([]string{"app"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("执行 app 失败: %v", err)
		}
	})

	if !strings.Contains(buf, "尚未实现") {
		t.Errorf("app 输出未包含 '尚未实现', 实际输出: %s", buf)
	}
}
