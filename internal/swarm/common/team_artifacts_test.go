package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGetTeamArtifactWorkspace_SharedChatDir 同 session 全员共享一个 chat-<n>
// （对齐 team_artifacts.py:71-107）。
func TestGetTeamArtifactWorkspace_SharedChatDir(t *testing.T) {
	root := t.TempDir()
	ws1, err := GetTeamArtifactWorkspace(root, "sess-1", "")
	if err != nil {
		t.Fatal(err)
	}
	ws2, err := GetTeamArtifactWorkspace(root, "sess-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if ws1.RootDir != ws2.RootDir {
		t.Fatalf("同 session 应复用同 root: %q vs %q", ws1.RootDir, ws2.RootDir)
	}
	if filepath.Base(ws1.OutputsDir) != outputsSubdir {
		t.Fatalf("outputs 目录形状: %q", ws1.OutputsDir)
	}
	if !strings.Contains(ws1.RootDir, artifactsSubdir) {
		t.Fatalf("root 应在 artifacts/ 下: %q", ws1.RootDir)
	}
	// 注册收敛到 <artifacts>/.team_artifacts（重定位自包含）
	if !fileExists(filepath.Join(GetTeamArtifactsDir(root), teamArtifactsRegistrySubdir)) {
		t.Fatal("注册目录应收敛到 artifacts/.team_artifacts")
	}
}

// TestGetTeamArtifactsDir 形状 `<root>/artifacts`。
func TestGetTeamArtifactsDir(t *testing.T) {
	root := t.TempDir()
	got := GetTeamArtifactsDir(root)
	if filepath.Base(got) != artifactsSubdir {
		t.Fatalf("got %q", got)
	}
}

// TestResolveMemberWorkDir_Slug 隔离目录形状 + 二次调用复用
// （对齐 team_artifacts.py:110-129）。
func TestResolveMemberWorkDir_Slug(t *testing.T) {
	root := t.TempDir()
	dir, err := ResolveMemberWorkDir(root, "Alice Dev")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dir, filepath.Join(workSubdir, "Alice-Dev")) {
		t.Fatalf("work 目录形状: %q", dir)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("目录应已创建: %v", err)
	}
	again, err := ResolveMemberWorkDir(root, "Alice Dev")
	if err != nil {
		t.Fatal(err)
	}
	if again != dir {
		t.Fatal("二次调用应复用")
	}
}

// TestResolveMemberWorkDir_EmptyName 空名回落 default。
func TestResolveMemberWorkDir_EmptyName(t *testing.T) {
	root := t.TempDir()
	dir, err := ResolveMemberWorkDir(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "default" {
		t.Fatalf("空名应回落 default: %q", dir)
	}
}
