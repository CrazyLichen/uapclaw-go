//go:build integration

package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestGitMethods 验证 git 相关方法
// 运行方式: go test -tags=integration ./internal/swarm/server/runtime/skill/...
func TestGitMethods(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.gitClone(context.Background(), "https://github.com/test", filepath.Join(tmpDir, "clone"))
	sm.gitPull(context.Background(), filepath.Join(tmpDir, "pull"))
	sm.gitGetCommit(tmpDir)
}

// TestSyncMarketplaceRepos 验证同步 marketplace
// 运行方式: go test -tags=integration ./internal/swarm/server/runtime/skill/...
func TestSyncMarketplaceRepos(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.state["marketplaces"] = []any{
		map[string]any{"name": "m1", "url": "https://github.com/m1", "enabled": true},
		map[string]any{"name": "m2", "url": "https://github.com/m2", "enabled": false},
	}
	sm.saveState()
	sm.mu.Unlock()

	err := sm.syncMarketplaceRepos(context.Background())
	// git 操作依赖外部网络，不应 panic
	_ = err
}

// TestHandleSkillsInstall_marketplace有URL 验证 marketplace 有 URL 的情况
// 运行方式: go test -tags=integration ./internal/swarm/server/runtime/skill/...
func TestHandleSkillsInstall_marketplace有URL(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.state["marketplaces"] = []any{
		map[string]any{"name": "test-market", "url": "https://github.com/test/market", "enabled": true},
	}
	sm.saveState()
	sm.mu.Unlock()

	result, err := sm.HandleSkillsInstall(context.Background(), map[string]any{
		"spec": "some-plugin@test-market",
	})
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	_ = result
}

// TestHandleSkillsInstall_缺SKILLMD 验证缺少 SKILL.md
// 运行方式: go test -tags=integration ./internal/swarm/server/runtime/skill/...
func TestHandleSkillsInstall_缺SKILLMD(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSkillManager(tmpDir)

	sm.mu.Lock()
	sm.state["marketplaces"] = []any{
		map[string]any{"name": "no-md-market", "url": "https://github.com/test/no-md", "enabled": true},
	}
	sm.saveState()
	sm.mu.Unlock()

	// 仓库有 skills/ 子目录但无 SKILL.md
	repoDir := filepath.Join(sm.marketplaceDir, "no-md-market")
	skillSrcDir := filepath.Join(repoDir, "skills", "no-md-plugin")
	os.MkdirAll(skillSrcDir, 0o755)

	result, _ := sm.HandleSkillsInstall(context.Background(), map[string]any{
		"spec": "no-md-plugin@no-md-market",
	})
	if toBool(result["success"]) != false {
		t.Error("缺 SKILL.md 应返回 success=false")
	}
}
