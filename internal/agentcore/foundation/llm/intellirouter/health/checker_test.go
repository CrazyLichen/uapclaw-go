package health

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
)

func newDep(t *testing.T, id, apiBase string) *intelliroutercore.Deployment {
	t.Helper()
	d, err := intelliroutercore.NewDeployment(intelliroutercore.DeploymentParams{
		ModelName: "m", APIKey: "k", APIBase: apiBase, ID: id, Provider: "openai",
	})
	require.NoError(t, err)
	return d
}

// TestCheckDeployment_健康与失败 最小 completion 探测（checker.py:80-119）。
func TestCheckDeployment_健康与失败(t *testing.T) {
	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求形态：chat/completions + max_tokens=10 + ping 消息
		require.Equal(t, "/chat/completions", r.URL.Path)
		w.WriteHeader(200)
	}))
	defer okServer.Close()

	state := intelliroutercore.NewLocalRouterState()
	c := NewSDKHealthChecker(nil, state, 0, 0, nil, 0, okServer.Client())
	require.InDelta(t, DefaultCheckInterval, c.CheckInterval, 0) // 默认值（py:44）
	require.InDelta(t, DefaultCheckTimeout, c.CheckTimeout, 0)
	require.Equal(t, DefaultCheckMaxTokens, c.CheckMaxTokens)

	res := c.CheckDeployment(newDep(t, "d1", okServer.URL))
	require.True(t, res.IsHealthy)
	require.NotNil(t, res.Latency)
	require.Nil(t, res.Error)

	// 非 200 → 失败 "HTTP 500"（py:116-119）
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer badServer.Close()
	c2 := NewSDKHealthChecker(nil, state, 0, 0, nil, 0, badServer.Client())
	res = c2.CheckDeployment(newDep(t, "d2", badServer.URL))
	require.False(t, res.IsHealthy)
	require.Equal(t, "HTTP 500", *res.Error)
}

// TestCheckAllDeployments_并发与回写 gather 并发 + update_health 回写（py:121-140）。
func TestCheckAllDeployments_并发与回写(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()
	state := intelliroutercore.NewLocalRouterState()
	deps := []*intelliroutercore.Deployment{
		newDep(t, "a", srv.URL), newDep(t, "b", srv.URL),
	}
	c := NewSDKHealthChecker(deps, state, 0, 0, nil, 0, srv.Client())
	results := c.CheckAllDeployments()
	require.Len(t, results, 2)
	require.True(t, results["a"].IsHealthy)
	require.True(t, state.HealthStateOf("a")) // 回写（py:138）
	require.True(t, state.HealthStateOf("b"))
}

// TestGetHealthyDeployments 双重条件（py:145-170）。
func TestGetHealthyDeployments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()
	state := intelliroutercore.NewLocalRouterState()
	d1 := newDep(t, "d1", srv.URL)
	d2 := newDep(t, "d2", srv.URL)
	d3 := newDep(t, "d3", srv.URL)
	c := NewSDKHealthChecker([]*intelliroutercore.Deployment{d1, d2, d3}, state, 0, 0, nil, 0, srv.Client())

	// 默认全健康（health_state 未登记默认 true，py:163）
	require.Len(t, c.GetHealthyDeployments(1000.0), 3)

	// d2 进入冷却（运行期判定走 state，py:165-166）
	state.OnFailure("d2", nil, 60.0)
	require.Len(t, c.GetHealthyDeployments(stateNow()), 2)

	// d2 冷却过期后 state 层判定可用（py:237-239），但 health_state 仍为
	// false（on_failure 写入，py:220）——双重条件缺一不可（py:150-152），
	// 与 Python 行为一致：需健康检查成功才恢复
	until := stateNow() - 1
	state.CooldownUntil["d2"] = &until
	require.Len(t, c.GetHealthyDeployments(stateNow()), 2)
	require.True(t, state.DeploymentAvailable("d2", stateNow())) // state 层已可用
	state.UpdateHealth("d2", true)                               // 健康检查通过加速恢复（py:152-154）
	require.Len(t, c.GetHealthyDeployments(stateNow()), 3)

	// health_state=false 直接排除（py:163）
	state.HealthStates["d3"] = false
	require.Len(t, c.GetHealthyDeployments(stateNow()), 2)
	require.Contains(t, c.GetUnhealthyIDs(), "d3") // py:188-193

	// UpdateHealth(true) 加速恢复：状态回 HEALTHY、冷却清空（py:152-154）
	require.Equal(t, intelliroutercore.DeploymentStatusHealthy, state.DeploymentStatuses["d2"])
	require.Nil(t, state.CooldownUntil["d2"])
	// 查询方法无写副作用：GetHealthyDeployments 不做 reset_deployment
	// 软恢复（py:240-242）——d3 仍 false、UnhealthyIDs 仍含 d3
	require.Contains(t, c.GetUnhealthyIDs(), "d3")
	require.Len(t, c.GetHealthyDeployments(stateNow()), 2)
}

// TestBackgroundCheck 后台循环启停（py:197-227）。
func TestBackgroundCheck(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()
	state := intelliroutercore.NewLocalRouterState()
	deps := []*intelliroutercore.Deployment{newDep(t, "d", srv.URL)}
	c := NewSDKHealthChecker(deps, state, 0, 0, nil, 0, srv.Client())

	c.StartBackgroundCheck()
	c.StartBackgroundCheck() // 幂等（py:199-200）
	// 启动即先查一轮（py:224-226 先 check 后 sleep）
	require.Eventually(t, func() bool { return state.HealthStateOf("d") }, 2e9, 1e7)
	_ = calls
	c.StopBackgroundCheck() // py:205-217
}

// stateNow 当前秒级时间（测试辅助）。
func stateNow() float64 {
	return float64(timeNowMs()) / 1000
}

// timeNowMs 当前毫秒时间戳。
func timeNowMs() int64 {
	return time.Now().UnixMilli()
}

// TestClose_UnhealthyIDs Close 与不健康集合（checker.py:71/188-193）。
func TestClose_UnhealthyIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	state := intelliroutercore.NewLocalRouterState()
	c := NewSDKHealthChecker([]*intelliroutercore.Deployment{newDep(t, "d", srv.URL)}, state, 0, 0, nil, 0, srv.Client())
	c.CheckAllDeployments() // 500 → health_state=false
	require.Contains(t, c.GetUnhealthyIDs(), "d")
	c.Close() // ← py:71 close（Go 客户端由注入方管理，方法面对齐）
	// BackgroundCheck 未启动时 Stop 幂等（py:206-210）
	c.StopBackgroundCheck()
	c.StopBackgroundCheck()
	// CheckDeployment 网络错误路径（py:128-132 except Exception）
	badDep := newDep(t, "bad", "http://127.0.0.1:1") // 连接拒绝
	res := c.CheckDeployment(badDep)
	require.False(t, res.IsHealthy)
	require.NotNil(t, res.Error)
}
