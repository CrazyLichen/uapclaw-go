package team

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/registry"
	runner "github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	upath "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeDeletePoolAccessor 模拟 registry.PoolAccessor 并记录 DeleteTeam 调用。
// 用于验证 DeleteSessionRuntime 团队路径（runner.DeleteAgentTeam 探测的 deleter 接口）。
type fakeDeletePoolAccessor struct {
	// deleteErr DeleteTeam 返回的错误（nil 表示成功）
	deleteErr error
	// deleteCalls 记录收到的 teamName 列表
	deleteCalls []string
}

// failingCheckpointer Release 返回错误的 Checkpointer 桩。
// 用于验证回退路径 Runner.release 失败时的错误契约。
type failingCheckpointer struct {
	// releaseErr Release 返回的错误
	releaseErr error
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDeleteSessionRuntime_团队路径成功 验证 metadata 解析 team_name 后走 DeleteAgentTeam。
// Python: team_name 可解析 → await Runner.delete_agent_team(team_name=, session_ids=[session_id], force=True)。
func TestDeleteSessionRuntime_团队路径成功(t *testing.T) {
	setupDeleteSessionRuntimeEnv(t, "sess_team_ok", map[string]any{
		"session_id": "sess_team_ok",
		"team_name":  "alpha-team",
	})
	fake := &fakeDeletePoolAccessor{}
	injectFakePoolAccessor(t, fake)

	mgr := NewTeamManager()
	deleted, err := mgr.DeleteSessionRuntime(context.Background(), "sess_team_ok", "user_delete")
	if err != nil {
		t.Fatalf("DeleteSessionRuntime 返回错误: %v", err)
	}
	if !deleted {
		t.Error("deleted 应为 true")
	}
	if len(fake.deleteCalls) != 1 || fake.deleteCalls[0] != "alpha-team" {
		t.Errorf("DeleteTeam 调用 = %v, 期望 [alpha-team]", fake.deleteCalls)
	}
}

// TestDeleteSessionRuntime_团队删除失败 验证 DeleteTeam 失败时错误契约 (false, err)。
// Python: except → logger.warning + return False（清理失败必须告知调用方，
// 否则用户收到"已解散"成功文案但团队 runtime 仍残留）。
func TestDeleteSessionRuntime_团队删除失败(t *testing.T) {
	setupDeleteSessionRuntimeEnv(t, "sess_team_fail", map[string]any{
		"session_id": "sess_team_fail",
		"team_name":  "beta-team",
	})
	fake := &fakeDeletePoolAccessor{deleteErr: errors.New("delete_agent_team failed")}
	injectFakePoolAccessor(t, fake)

	mgr := NewTeamManager()
	deleted, err := mgr.DeleteSessionRuntime(context.Background(), "sess_team_fail", "user_delete")
	if err == nil {
		t.Fatal("期望返回错误（DeleteTeam 失败）")
	}
	if deleted {
		t.Error("deleted 应为 false（错误契约：清理失败必须告知调用方）")
	}
	if len(fake.deleteCalls) != 1 || fake.deleteCalls[0] != "beta-team" {
		t.Errorf("DeleteTeam 调用 = %v, 期望 [beta-team]", fake.deleteCalls)
	}
}

// TestDeleteSessionRuntime_回退路径成功 验证无 team_name 时走 Runner.release 且不调 DeleteTeam。
// Python: team_name 为 None → logger.warning(fell back) + await Runner.release(session_id)。
func TestDeleteSessionRuntime_回退路径成功(t *testing.T) {
	setupDeleteSessionRuntimeEnv(t, "sess_fallback_ok", map[string]any{
		"session_id": "sess_fallback_ok",
	})
	fake := &fakeDeletePoolAccessor{}
	injectFakePoolAccessor(t, fake)
	// 显式注入 in-memory checkpointer，避免全局默认值被其他测试污染
	injectCheckpointer(t, checkpointer.NewInMemoryCheckpointer())

	mgr := NewTeamManager()
	deleted, err := mgr.DeleteSessionRuntime(context.Background(), "sess_fallback_ok", "user_delete")
	if err != nil {
		t.Fatalf("DeleteSessionRuntime 返回错误: %v", err)
	}
	if !deleted {
		t.Error("deleted 应为 true")
	}
	if len(fake.deleteCalls) != 0 {
		t.Errorf("回退路径不应调用 DeleteTeam, 实际 %v", fake.deleteCalls)
	}
}

// TestDeleteSessionRuntime_回退释放失败 验证 Runner.release 失败时错误契约 (false, err)。
// Python: await Runner.release(session_id) 抛异常 → except → return False。
func TestDeleteSessionRuntime_回退释放失败(t *testing.T) {
	setupDeleteSessionRuntimeEnv(t, "sess_fallback_fail", map[string]any{
		"session_id": "sess_fallback_fail",
	})
	fake := &fakeDeletePoolAccessor{}
	injectFakePoolAccessor(t, fake)
	injectCheckpointer(t, &failingCheckpointer{releaseErr: errors.New("release failed")})

	mgr := NewTeamManager()
	deleted, err := mgr.DeleteSessionRuntime(context.Background(), "sess_fallback_fail", "user_delete")
	if err == nil {
		t.Fatal("期望返回错误（Release 失败）")
	}
	if deleted {
		t.Error("deleted 应为 false（错误契约：清理失败必须告知调用方）")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// PoolReader 实现 registry.PoolAccessor（返回 nil 即可，本测试不触达池读取）。
func (f *fakeDeletePoolAccessor) PoolReader() registry.PoolReader { return nil }

// DeleteTeam 实现 runner.DeleteAgentTeam 探测的 deleter 接口，记录调用并返回注入的错误。
func (f *fakeDeletePoolAccessor) DeleteTeam(ctx context.Context, teamName string, sessionIDs []string, force bool) (bool, error) {
	f.deleteCalls = append(f.deleteCalls, teamName)
	if f.deleteErr != nil {
		return false, f.deleteErr
	}
	return true, nil
}

// PreWorkflowExecute 工作流执行前（桩，无操作）
func (f *failingCheckpointer) PreWorkflowExecute(ctx context.Context, s interfaces.InnerSession, inputs any) error {
	return nil
}

// PostWorkflowExecute 工作流执行后（桩，无操作）
func (f *failingCheckpointer) PostWorkflowExecute(ctx context.Context, s interfaces.InnerSession, result any, exception error) error {
	return nil
}

// PreAgentExecute Agent 执行前（桩，无操作）
func (f *failingCheckpointer) PreAgentExecute(ctx context.Context, s interfaces.InnerSession, inputs any) error {
	return nil
}

// PreAgentTeamExecute AgentTeam 执行前（桩，无操作）
func (f *failingCheckpointer) PreAgentTeamExecute(ctx context.Context, s interfaces.InnerSession, inputs any) error {
	return nil
}

// InterruptAgentExecute Agent 中断时（桩，无操作）
func (f *failingCheckpointer) InterruptAgentExecute(ctx context.Context, s interfaces.InnerSession) error {
	return nil
}

// PostAgentExecute Agent 执行后（桩，无操作）
func (f *failingCheckpointer) PostAgentExecute(ctx context.Context, s interfaces.InnerSession) error {
	return nil
}

// PostAgentTeamExecute AgentTeam 执行后（桩，无操作）
func (f *failingCheckpointer) PostAgentTeamExecute(ctx context.Context, s interfaces.InnerSession) error {
	return nil
}

// SessionExists 检查会话是否存在（桩，恒不存在）
func (f *failingCheckpointer) SessionExists(ctx context.Context, sessionID string) (bool, error) {
	return false, nil
}

// Release 释放会话资源（返回注入的错误）
func (f *failingCheckpointer) Release(ctx context.Context, sessionID string, agentID ...string) error {
	return f.releaseErr
}

// GraphStore 获取图状态存储（桩，返回 nil）
func (f *failingCheckpointer) GraphStore() any { return nil }

// setupDeleteSessionRuntimeEnv 隔离 sessions 目录与 metadata 缓存，写入指定 metadata。
// UAPCLAW_DATA_DIR 控制 workspace.AgentSessionsDir()，元数据缓存逐测试清空。
func setupDeleteSessionRuntimeEnv(t *testing.T, sessionID string, meta map[string]any) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	upath.ResetCache()
	session.ClearAllSessionMetadataCache()
	t.Cleanup(func() {
		upath.ResetCache()
		session.ClearAllSessionMetadataCache()
	})
	if meta != nil {
		sessionsDir := filepath.Join(tmpDir, "agent", "sessions")
		sessionDir := filepath.Join(sessionsDir, sessionID)
		if err := os.MkdirAll(sessionDir, 0o755); err != nil {
			t.Fatalf("创建会话目录失败: %v", err)
		}
		data, err := json.MarshalIndent(meta, "", "  ")
		if err != nil {
			t.Fatalf("序列化 metadata 失败: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sessionDir, "metadata.json"), data, 0o644); err != nil {
			t.Fatalf("写入 metadata.json 失败: %v", err)
		}
	}
}

// injectFakePoolAccessor 注入 fake PoolAccessor 到全局 Runner，测试结束恢复原值。
func injectFakePoolAccessor(t *testing.T, fake *fakeDeletePoolAccessor) {
	t.Helper()
	orig := runner.GetTeamRuntimeManager()
	runner.SetTeamRuntimeManager(fake)
	t.Cleanup(func() { runner.SetTeamRuntimeManager(orig) })
}

// injectCheckpointer 注入全局默认 checkpointer，测试结束恢复原值。
func injectCheckpointer(t *testing.T, cp interfaces.Checkpointer) {
	t.Helper()
	orig := checkpointer.GetCheckpointer()
	checkpointer.SetDefaultCheckpointer(cp)
	t.Cleanup(func() { checkpointer.SetDefaultCheckpointer(orig) })
}
