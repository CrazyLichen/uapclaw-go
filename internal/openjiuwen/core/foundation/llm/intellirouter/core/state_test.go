package core

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// clock 推进式测试时钟。
type clock struct{ t float64 }

func (c *clock) now() float64      { return c.t }
func (c *clock) advance(d float64) { c.t += d }

func newTestState(t *testing.T, cooldown float64) (*LocalRouterState, *clock) {
	c := &clock{t: 1000.0}
	prev := SetNowSource(c.now)
	t.Cleanup(func() { SetNowSource(prev) })
	_ = cooldown // cooldown_time 在 OnFailure 调用点传入（py 默认 60.0）
	return NewLocalRouterState(), c
}

// TestLocalRouterState_CooldownBackoff 冷却退避序列（py:204-221）。
func TestLocalRouterState_CooldownBackoff(t *testing.T) {
	s, c := newTestState(t, 60.0)
	// 第 1 次失败：consecutive_failures=1，cooldown = 60*1（py:220）
	s.OnFailure("d1", errors.New("x"), 60.0)
	require.Equal(t, 1, s.ConsecutiveFailures["d1"])
	require.Equal(t, DeploymentStatusCooldown, s.DeploymentStatuses["d1"])
	require.InDelta(t, 1060.0, *s.CooldownUntil["d1"], 1e-9)
	require.False(t, s.HealthStates["d1"])

	// 第 2 次失败：退避 60*2=120s
	s.OnFailure("d1", errors.New("x"), 60.0)
	require.Equal(t, 2, s.ConsecutiveFailures["d1"])
	require.InDelta(t, 1120.0, *s.CooldownUntil["d1"], 1e-9)

	// 冷却中不可用（py:262 until <= now 判定）
	require.Empty(t, s.GetAvailableDeployments(c.now()))

	// 到期 → 软恢复并计入可用（py:262-264 reset_deployment）
	avail := s.GetAvailableDeployments(1121.0)
	require.Equal(t, []string{"d1"}, avail)
	require.Equal(t, DeploymentStatusHealthy, s.DeploymentStatuses["d1"])
	require.Nil(t, s.CooldownUntil["d1"])
	// 软恢复保留失败计数（py:137 consecutive_failures 保留）
	require.Equal(t, 2, s.ConsecutiveFailures["d1"])
	require.True(t, s.HealthStates["d1"])
}

// TestLocalRouterState_OnSuccess 成功清零与延迟记录（py:158-200）。
func TestLocalRouterState_OnSuccess(t *testing.T) {
	s, _ := newTestState(t, 60.0)
	s.OnFailure("d1", errors.New("x"), 60.0)
	s.OnSuccess("d1", 2.0, 400)
	require.Equal(t, 0, s.ConsecutiveFailures["d1"]) // py:167 清零
	require.Equal(t, DeploymentStatusHealthy, s.DeploymentStatuses["d1"])
	require.Nil(t, s.CooldownUntil["d1"])      // py:171
	require.True(t, s.HealthStates["d1"])      // py:190
	require.Equal(t, 400, s.TotalTokens["d1"]) // py:187
	require.Equal(t, 1, s.TotalRequests["d1"]) // py:188
	recs := s.Latencies["d1"]
	require.Len(t, recs, 1)
	require.InDelta(t, 2.0/400.0, recs[0].Normalized, 1e-12) // py:177 normalized=latency/max(tokens,1)
}

// TestLocalRouterState_LatencyHistory 留最近 10 条（py:183-184）。
func TestLocalRouterState_LatencyHistory(t *testing.T) {
	s, _ := newTestState(t, 60.0)
	for i := 0; i < 12; i++ {
		s.OnSuccess("d1", float64(i), 100)
	}
	require.Len(t, s.Latencies["d1"], MaxLatencyHistory)
	require.Equal(t, 2.0, s.Latencies["d1"][0].Latency) // 最早的 2 条被挤出
}

// TestLocalRouterState_GetAverageLatency 归一化口径（py:223-240）。
func TestLocalRouterState_GetAverageLatency(t *testing.T) {
	s, _ := newTestState(t, 60.0)
	require.Equal(t, math.Inf(1), s.GetAverageLatency("none")) // py:235 无记录 → inf
	// tokens<=0 的记录不参与归一化统计（py:230-233）
	s.OnSuccess("d1", 5.0, 0)
	s.OnSuccess("d1", 2.0, 100)
	require.Equal(t, 2.0/100.0, s.GetAverageLatency("d1"))
	// raw 口径：全部记录参与，无记录返回 nil（py:243-251）
	raw := s.GetAverageLatencyRaw("d1")
	require.NotNil(t, raw)
	require.InDelta(t, (5.0+2.0)/2, *raw, 1e-12)
	require.Nil(t, s.GetAverageLatencyRaw("none"))
}

// TestLocalRouterState_UpdateHealth 健康更新不降级（py:146-155）。
func TestLocalRouterState_UpdateHealth(t *testing.T) {
	s, _ := newTestState(t, 60.0)
	s.OnFailure("d1", errors.New("x"), 60.0)
	// 不健康时不降级（py:151 注释）——已是 COOLDOWN 保持不变
	s.UpdateHealth("d1", false)
	require.Equal(t, DeploymentStatusCooldown, s.DeploymentStatuses["d1"])
	require.NotNil(t, s.CooldownUntil["d1"])
	// 健康时加速恢复（py:152-154）
	s.UpdateHealth("d1", true)
	require.Equal(t, DeploymentStatusHealthy, s.DeploymentStatuses["d1"])
	require.Nil(t, s.CooldownUntil["d1"])
}

// TestLocalRouterState_QuotaRemaining 未配置配额返回 inf（py:192-200 注释语义）。
func TestLocalRouterState_QuotaRemaining(t *testing.T) {
	s, _ := newTestState(t, 60.0)
	// 未注册配额的部署服务请求后仍返回 inf（py:198-200）
	s.OnSuccess("d1", 1.0, 100)
	require.Equal(t, math.Inf(1), s.GetTokenRemaining("d1"))
	require.Equal(t, math.Inf(1), s.GetRPMRemaining("d1"))
	require.Equal(t, 0.0, s.GetTokenUtilization("d1"))
	require.Equal(t, 0.0, s.GetRPMUtilization("d1"))

	// 注册配额后（由 router 的 quota wiring 创建，此处模拟）
	s.TokenUsage["d1"] = &TokenUsage{Used: 300, Limit: 1000, Timestamp: 0}
	s.RPMTrackers["d1"] = &RPMTracker{RPMLimit: 10}
	s.OnSuccess("d1", 1.0, 100) // py:196-199 已注册才累计
	require.Equal(t, 400, s.TokenUsage["d1"].Used)
	require.Equal(t, 1, s.RPMTrackers["d1"].CurrentRPM())
	require.Equal(t, float64(600), s.GetTokenRemaining("d1"))
	require.Equal(t, float64(9), s.GetRPMRemaining("d1"))
	require.InDelta(t, 0.4, s.GetTokenUtilization("d1"), 1e-12)
	require.InDelta(t, 0.1, s.GetRPMUtilization("d1"), 1e-12)
}

// TestTokenUsageLimit0 未设限语义（py:53-55 limit==0 → 0.0 使用率）。
func TestTokenUsageLimit0(t *testing.T) {
	u := TokenUsage{Used: 500, Limit: 0}
	require.Equal(t, 0, u.Remaining())
	require.Equal(t, 0.0, u.UtilizationRatio())
	u2 := TokenUsage{Used: 1200, Limit: 1000}
	require.Equal(t, 0, u2.Remaining()) // py:48 max(0, ...)
}

// TestRPMTracker 滑窗与使用率（py:61-86）。
func TestRPMTracker(t *testing.T) {
	c := &clock{t: 100.0}
	prev := SetNowSource(c.now)
	t.Cleanup(func() { SetNowSource(prev) })
	tr := &RPMTracker{RPMLimit: 5}
	tr.AddRequest()
	tr.AddRequest()
	require.Equal(t, 2, tr.CurrentRPM())
	// 1 分钟前记录过期（py:68-69）
	c.advance(61)
	tr.AddRequest()
	require.Equal(t, 1, tr.CurrentRPM())
	require.Equal(t, 4, tr.Remaining())
	require.InDelta(t, 0.2, tr.UtilizationRatio(), 1e-12)
	zero := &RPMTracker{}
	require.Equal(t, 0.0, zero.UtilizationRatio())
}

// TestRemoveDeployment 单部署精确清理（py:277-306）。
func TestRemoveDeployment(t *testing.T) {
	s, _ := newTestState(t, 60.0)
	s.OnFailure("d1", errors.New("x"), 60.0)
	s.OnSuccess("d2", 1.0, 10)
	s.SessionDeploymentMap["sess-1"] = "d1"
	s.SessionTimestamps["sess-1"] = 1.0

	require.True(t, s.RemoveDeployment("d1"))
	require.False(t, s.RemoveDeployment("d1")) // 再删返回 false（py:296 existed）
	require.NotContains(t, s.ConsecutiveFailures, "d1")
	require.NotContains(t, s.CooldownUntil, "d1")
	require.NotContains(t, s.SessionDeploymentMap, "sess-1") // py:303-307
	// 不影响其他部署（py:287-289 注释）
	require.Contains(t, s.TotalTokens, "d2")
}

// TestCleanupDeployments 反向清理与去重保序（py:309-342）。
func TestCleanupDeployments(t *testing.T) {
	s, _ := newTestState(t, 60.0)
	s.OnFailure("d1", errors.New("x"), 60.0)
	s.OnSuccess("d2", 1.0, 10)
	s.OnSuccess("d3", 1.0, 10)
	// 同一部署出现在多个注册表 → 去重（py:342）。
	// removed 顺序对齐 Python set 迭代（无稳定顺序），以集合相等断言。
	removed := s.CleanupDeployments([]string{"d2"})
	require.ElementsMatch(t, []string{"d1", "d3"}, removed)
	require.Contains(t, s.TotalTokens, "d2")
	require.NotContains(t, s.ConsecutiveFailures, "d1")
	require.NotContains(t, s.TotalTokens, "d3")
}

// TestState_导出方法面 补齐 ResetDeployment/HealthStateOf/DeploymentAvailable 直测。
func TestState_导出方法面(t *testing.T) {
	s, c := newTestState(t, 60.0)
	// ResetDeployment 空部署也安全（未登记 → 状态写入 HEALTHY）
	s.ResetDeployment("none")
	require.Equal(t, DeploymentStatusHealthy, s.DeploymentStatuses["none"])
	// DeploymentAvailable 默认 HEALTHY（py:236 get(id, HEALTHY)）
	require.True(t, s.DeploymentAvailable("unregistered", c.now()))
	// 冷却未过期不可用、过期可用（py:237-239）
	s.OnFailure("d1", nil, 60.0)
	require.False(t, s.DeploymentAvailable("d1", c.now()))
	require.False(t, s.HealthStateOf("d1")) // py:220 health_state=false
	// HealthStateOf 未登记默认 true（py:163 get(id, True)）
	require.True(t, s.HealthStateOf("unregistered"))
	// health 通过加速恢复（py:152-154）
	s.UpdateHealth("d1", true)
	require.True(t, s.DeploymentAvailable("d1", c.now()))
	require.Equal(t, DeploymentStatusHealthy, s.DeploymentStatuses["d1"])
}

// TestDeploymentFromDict_双形态 补 dict 解析分支（内存 ToDict 与 JSON 两形态）。
func TestDeploymentFromDict_双形态(t *testing.T) {
	// JSON 形态（float64 / []any / map[string]any）
	jsonShape := map[string]any{
		"id": "x1", "model_name": "m", "api_key": "k", "api_base": "u",
		"provider": "openai", "status": "cooldown",
		"cooldown_until": 99.0, "tags": []any{"t1", "t2"},
		"tpm": float64(10), "rpm": float64(5), "timeout": float64(2.5),
		"endpoint_profile": "dashscope", "fallback_tag": "q",
		"model_description": "d", "request_defaults": map[string]any{"a": 1.0},
		"custom_headers": map[string]any{"H": "v"}, "consecutive_failures": float64(2),
	}
	d, err := DeploymentFromDict(jsonShape)
	require.NoError(t, err)
	require.Equal(t, DeploymentStatusCooldown, d.Status)
	require.Equal(t, 99.0, *d.CooldownUntil)
	require.Equal(t, []string{"t1", "t2"}, d.Tags)
	require.Equal(t, 10, *d.TPM)
	require.Equal(t, 5, *d.RPM)
	require.Equal(t, 2.5, *d.Timeout)
	require.Equal(t, "q", *d.FallbackTag)
	require.Equal(t, 2, d.ConsecutiveFailures)
	require.Equal(t, "v", d.CustomHeaders["H"])
	// 非法 status 报错（DeploymentStatusFrom）
	bad := map[string]any{"model_name": "m", "api_key": "k", "api_base": "u", "provider": "p", "status": "bogus"}
	_, err = DeploymentFromDict(bad)
	require.Error(t, err)
	// verify_ssl 缺省 true（py 默认 true）
	require.True(t, d.VerifySSL)
}

// TestIntPtrFrom_分支 补 dict 双形态助手分支。
func TestIntPtrFrom_分支(t *testing.T) {
	i := 3
	require.Equal(t, 3, *intPtrFrom(&i))
	require.Equal(t, 4, *intPtrFrom(4))
	require.Equal(t, 5, *intPtrFrom(float64(5)))
	require.Nil(t, intPtrFrom("x"))
	require.Nil(t, intPtrFrom(nil))
	f := 1.5
	require.Equal(t, 1.5, *floatPtrFrom(&f))
	require.Equal(t, 2.0, *floatPtrFrom(2))
	require.Equal(t, 3.5, *floatPtrFrom(float64(3.5)))
	require.Nil(t, floatPtrFrom("x"))
	require.Equal(t, "s", *strPtrFrom("s"))
	sp := "sp"
	require.Equal(t, "sp", *strPtrFrom(&sp))
	require.Nil(t, strPtrFrom(1))
	require.Equal(t, []string{"a"}, strSliceFrom([]string{"a"}))
	require.Equal(t, []string{"b"}, strSliceFrom([]any{"b"}))
	require.Nil(t, strSliceFrom(1))
	require.True(t, boolFrom("x", true))
	require.False(t, boolFrom(false, true))
}
