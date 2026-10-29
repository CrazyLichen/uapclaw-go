package main

// 11.25 Task 10：acp 子命令测试（URL 四级回落 + 旗标注册）。
// Python 基线：acp_connect.py main() L1773-1807（tests/unit_tests/acp 无此层用例，
// 映射表例 3 test_main_passes_explicit_agent_server_url 归属此处）。

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// mapEnv map → getenv 函数适配。
func mapEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// TestResolveAcpGatewayURL 四级回落链（Python main L1800-1805）。
// Python: test_main_passes_explicit_agent_server_url。
func TestResolveAcpGatewayURL(t *testing.T) {
	// 1. 显式 --gateway-url 最高优先
	assert.Equal(t, "ws://explicit:1/acp",
		resolveAcpGatewayURL("ws://explicit:1/acp", "", nil))

	// 2. --agent-server-url 兼容旗标（Python argparse.SUPPRESS 隐藏旗标）
	assert.Equal(t, "ws://legacy:2/acp",
		resolveAcpGatewayURL("", "ws://legacy:2/acp", nil))

	// 3. env GATEWAY_URL
	assert.Equal(t, "ws://envurl:3/acp",
		resolveAcpGatewayURL("", "", mapEnv(map[string]string{"GATEWAY_URL": "ws://envurl:3/acp"})))

	// 4. host/port 组装默认
	assert.Equal(t, "ws://127.0.0.1:19001/acp",
		resolveAcpGatewayURL("", "", nil))
	assert.Equal(t, "ws://10.0.0.1:20001/acp",
		resolveAcpGatewayURL("", "", mapEnv(map[string]string{"GATEWAY_HOST": "10.0.0.1", "GATEWAY_PORT": "20001"})))

	// 显式旗标压过 env
	assert.Equal(t, "ws://explicit:1/acp",
		resolveAcpGatewayURL("ws://explicit:1/acp", "ws://legacy:2/acp",
			mapEnv(map[string]string{"GATEWAY_URL": "ws://envurl:3/acp"})))
	// 兼容旗标压过 env
	assert.Equal(t, "ws://legacy:2/acp",
		resolveAcpGatewayURL("", "ws://legacy:2/acp",
			mapEnv(map[string]string{"GATEWAY_URL": "ws://envurl:3/acp"})))
	// env URL 压过 host/port
	assert.Equal(t, "ws://envurl:3/acp",
		resolveAcpGatewayURL("", "",
			mapEnv(map[string]string{"GATEWAY_URL": "ws://envurl:3/acp", "GATEWAY_HOST": "10.0.0.1"})))
}

// TestNewAcpCmd_旗标注册 旗标与 RunE 接线。
func TestNewAcpCmd_旗标注册(t *testing.T) {
	cmd := newAcpCmd()
	assert.Equal(t, "acp", cmd.Use)
	assert.NotNil(t, cmd.RunE)

	gw, err := cmd.Flags().GetString("gateway-url")
	assert.NoError(t, err)
	assert.Empty(t, gw) // 默认空 → 模式一自举

	as, err := cmd.Flags().GetString("agent-server-url")
	assert.NoError(t, err)
	assert.Empty(t, as)

	// 旗标可设值
	assert.NoError(t, cmd.Flags().Set("gateway-url", "ws://x:1/acp"))
	gw, _ = cmd.Flags().GetString("gateway-url")
	assert.Equal(t, "ws://x:1/acp", gw)
}
