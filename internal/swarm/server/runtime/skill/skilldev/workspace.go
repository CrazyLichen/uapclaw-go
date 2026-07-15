package skilldev

import (
	"os"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorkspaceProvider SkillDev 工作区管理（本地文件系统实现）。
//
// 职责：提供每个 task_id 的隔离工作区目录，并维护标准目录结构。
//
// 目录结构（单机本地模式）：
//
//	~/.jiuwenswarm/agent/workspace/skilldev/{task_id}/
//	├── state.json          ← StateStore checkpoint
//	├── resources/          ← 上传的资源文件（解压后）
//	├── skill/              ← 生成的 skill 目录
//	│   ├── SKILL.md
//	│   └── ...
//	├── evals/
//	│   ├── evals.json      ← 测试用例定义
//	│   └── iteration-{N}/  ← 每轮测试结果
//	└── output/
//	    └── {skill_name}.skill  ← 最终打包产物
//
// base_dir 由调用方传入，约定为 getWorkspaceDir() / "skilldev"，
// 与整个 jiuwenswarm 的目录体系保持一致，不另起顶级目录。
//
// 扩展点：替换为支持远程对象存储的实现（接口不变），
// sync_to_remote 届时将文件同步到 S3/OBS。
type WorkspaceProvider struct {
	// baseDir SkillDev 工作区根目录
	baseDir string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// workspaceSubDirs 工作区标准子目录
var workspaceSubDirs = []string{"resources", "skill", "evals", "output"}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkspaceProvider 创建新的 WorkspaceProvider 实例。
//
// baseDir 约定为 getWorkspaceDir() / "skilldev"，即 ~/.jiuwenswarm/agent/workspace/skilldev/
func NewWorkspaceProvider(baseDir string) *WorkspaceProvider {
	return &WorkspaceProvider{baseDir: baseDir}
}

// GetLocalPath 返回指定任务的本地工作区路径（不保证已创建）。
func (w *WorkspaceProvider) GetLocalPath(taskID string) string {
	return filepath.Join(w.baseDir, taskID)
}

// EnsureLocal 确保工作区目录及其标准子目录存在，返回工作区根路径。
func (w *WorkspaceProvider) EnsureLocal(taskID string) (string, error) {
	workspace := w.GetLocalPath(taskID)
	for _, sub := range workspaceSubDirs {
		dir := filepath.Join(workspace, sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			logger.Error(logComponent).
				Str("task_id", taskID).
				Str("dir", dir).
				Err(err).
				Msg("[WorkspaceProvider] 创建子目录失败")
			return "", err
		}
	}
	logger.Debug(logComponent).Str("workspace", workspace).Msg("[WorkspaceProvider] workspace ready")
	return workspace, nil
}

// SyncToRemote 将本地工作区同步到远程存储（本地实现为空操作）。
//
// 扩展点：多实例部署时，此处将文件同步到共享存储（S3/OBS/NFS），
// 以支持不同实例间的工作区共享。当前单机部署无需实现。
func (w *WorkspaceProvider) SyncToRemote(_ string) error {
	// 待实现: 生产环境实现远程同步（S3 / OBS / NFS）
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
