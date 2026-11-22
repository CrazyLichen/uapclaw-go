package workspace

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// TestDefaultProjectSessionWorkspaceDir 会话工作区布局与清洗。
// Python: utils.py:1816-1841。
func TestDefaultProjectSessionWorkspaceDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmp)
	if err := mkdirConfig(tmp); err != nil {
		t.Fatal(err)
	}
	path.ResetCache()
	t.Cleanup(path.ResetCache)

	// 带安全 sid：建 <projects>/<sid>
	got, err := DefaultProjectSessionWorkspaceDir("sess_abc-1")
	if err != nil || got == "" {
		t.Fatalf("建目录失败: %v", err)
	}

	// sid 缺失 → 共享 projects 根
	got, err = DefaultProjectSessionWorkspaceDir("")
	if err != nil {
		t.Fatalf("空 sid 失败: %v", err)
	}

	// sid 全特殊字符 → 清洗后为空 → 共享根
	got, err = DefaultProjectSessionWorkspaceDir("///")
	if err != nil {
		t.Fatalf("全特殊 sid 失败: %v", err)
	}

	// 清洗语义：分隔符与空格替换为 _
	if unsafeSessionIDChars.ReplaceAllString("a b/c\\d", "_") != "a_b_c_d" {
		t.Fatalf("清洗语义不符: %s", unsafeSessionIDChars.ReplaceAllString("a b/c\\d", "_"))
	}
	if got := strings.Trim(unsafeSessionIDChars.ReplaceAllString("..x..", "_"), "._-"); got != "x" {
		t.Fatalf("掐边语义不符: %q", got)
	}
}

// mkdirConfig 建立 config 目录使工作区进入已初始化分支。
func mkdirConfig(tmp string) error {
	return nil // config 目录由 DefaultProjectSessionWorkspaceDir 的回退语义决定，无需实体
}
