// send_file 的精确参数辅助。
//
// 对应 Python 代码：_auto_permission/artifact_declaration.py（15 行 / 1 函数）
//
// 单独成文件是为了让「路径归一」这一件事只有一处实现——
// artifact_authorization 的授权签发与校验都从这里取。

package auto_permission

import (
	"path/filepath"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// requestedFilePaths 把读路径归一成绝对路径。
//
// Python: _requested_file_paths(facts)
// Path(path).expanduser().resolve(strict=False)
func requestedFilePaths(facts permissions.ToolDecisionFacts) []string {
	out := make([]string, 0, len(facts.ReadPaths))
	for _, rawPath := range facts.ReadPaths {
		expanded := pathutil.ExpandHome(rawPath)
		if resolved, err := permissions.ResolvePathStrictFalse(expanded); err == nil {
			out = append(out, resolved)
			continue
		}
		out = append(out, filepath.Clean(expanded))
	}
	return out
}
