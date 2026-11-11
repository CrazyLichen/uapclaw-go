//go:build integration && llm

package web_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestGoalCommand_真实LLM冒烟 真模型接入后协议面不崩的冒烟。
//
// 双守卫：文件级 integration && llm 标签 + LLM_API_KEY 缺失时 Skip。
// 只硬断言确定性契约（受理、帧形状、枚举、顺序不互斥），不断言模型是否
// 选择 submit_goal_report / 是否跑到 COMPLETED / attempt 轮数 —— 同一提示词
// 两轮可完全不走目标流程，模型行为不可断言。
//
// 运行方式：
//
//	LLM_API_KEY=xxx go test -tags "sqlite_fts5 test integration llm" \
//	  ./tests/integration/swarm/web/ -run TestGoalCommand_真实LLM冒烟 -count=1 -v
func TestGoalCommand_真实LLM冒烟(t *testing.T) {
	if os.Getenv("LLM_API_KEY") == "" {
		t.Skip("LLM_API_KEY 未配置，跳过真 LLM 冒烟")
	}
	t.Setenv(pathutil.EnvDataDir, t.TempDir())
	pathutil.ResetCache()
	sessionpkg.ClearAllSessionMetadataCache()
	t.Cleanup(func() {
		sessionpkg.FlushMetadataQueue(5.0)
		sessionpkg.FlushHistoryQueue(5.0)
		sessionpkg.ClearAllSessionMetadataCache()
		pathutil.ResetCache()
	})

	cfg, err := config.New("")
	require.NoError(t, err)
	env := testhelpers.NewServerEnv(t, cfg, nil)

	// ServerEnv.AwaitStreamEnd 自带 30s 硬上界（server_env.go 的
	// serverEnvWaitTimeout），在主 goroutine 调用，避免 Fatalf 落到子协程
	// 导致测试提前终止；真 LLM 未在 30s 内收敛时由该上界软性收口。

	rid := env.Send(string(schema.ReqMethodCommandGoal), map[string]any{
		"action": "set", "objective": "列出当前目录文件", "mode": "agent",
		"overwrite_confirmed": true,
	}, "web-goal-realllm", true)

	frames := goalFrames(env.AwaitStreamEnd(rid))
	require.NotEmpty(t, frames, "必须至少产出一帧")

	snap := indexOfEvent(frames, eventGoalSnapshot)
	accepted := indexOfEvent(frames, eventRuntimeAccepted)
	require.True(t, snap >= 0 || accepted >= 0,
		"set 必须被受理（goal.snapshot 或 runtime.accepted 之一到达）")
	if snap >= 0 && accepted >= 0 {
		require.Less(t, snap, accepted, "O1：snapshot 应早于 accepted")
	}
	for _, c := range frames {
		if eventTypeOf(c) != eventGoalUpdated {
			continue
		}
		if c.Payload["goal"] != nil {
			requireGoalRecord(t, c.Payload["goal"])
		}
	}
	// 429 / 慢模型 / 模型没走目标流程都属可接受：本用例不因此 FAIL，
	// 只要求协议面（至少一帧 + 受理信号 + 事件形状合法）成立。
	require.True(t, frames[len(frames)-1].IsComplete,
		"流最终必须关闭（AwaitStreamEnd 已保证收到终结帧）")
}
