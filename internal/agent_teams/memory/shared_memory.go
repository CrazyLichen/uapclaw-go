package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SharedMemoryManager 管理 {team_home}/team-memory/ 目录下的团队摘要文件。
// 对齐 Python SharedMemoryManager (shared_memory.py)
//
// 所有成员只读访问 ReadTeamSummary；
// 提取 agent（leader extract_after_round）通过工具或 WriteTeamSummary 写入。
type SharedMemoryManager struct {
	// dir 团队记忆目录路径
	dir string
	// sysOperation 系统操作接口（可选，nil 时用本地文件系统）。⤴️ 9.64 具体类型回填
	sysOperation sysop.SysOperation
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// teamMemoryFilename 团队记忆文件名
	teamMemoryFilename = "TEAM_MEMORY.md"
	// teamMemoryMaxReadLines 读取最大行数
	teamMemoryMaxReadLines = 200
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// sharedLogComponent 日志组件
	sharedLogComponent = logger.ComponentCommon
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSharedMemoryManager 创建共享记忆管理器
func NewSharedMemoryManager(teamMemoryDir string, sysOperation sysop.SysOperation) *SharedMemoryManager {
	return &SharedMemoryManager{dir: teamMemoryDir, sysOperation: sysOperation}
}

// EnsureDir 确保团队记忆目录存在。对齐 Python ensure_dir
func (m *SharedMemoryManager) EnsureDir() error {
	return os.MkdirAll(m.dir, 0o755)
}

// ReadTeamSummary 读取团队记忆摘要文件。
// 对齐 Python read_team_summary — 真实实现
// 最多前 teamMemoryMaxReadLines 行，不存在或错误时返回空字符串
func (m *SharedMemoryManager) ReadTeamSummary(_ context.Context) string {
	filePath := filepath.Join(m.dir, teamMemoryFilename)

	// ⤵️ 回填: 7.2 — sysOperation 分支，当前仅本地文件系统
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > teamMemoryMaxReadLines {
		lines = lines[:teamMemoryMaxReadLines]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// WriteTeamSummary 写入团队记忆摘要（覆盖）。
// 对齐 Python write_team_summary — 真实实现（本地原子写入）
// ⤵️ 回填: 7.2 — sysOperation 分支优先，当前仅本地原子写入
func (m *SharedMemoryManager) WriteTeamSummary(_ context.Context, content string) error {
	if err := m.EnsureDir(); err != nil {
		return err
	}
	target := filepath.Join(m.dir, teamMemoryFilename)

	// 原子写入：先写临时文件，再 os.Rename 替换
	tmpFile, err := os.CreateTemp(m.dir, "team_memory_*.tmp")
	if err != nil {
		logger.Error(sharedLogComponent).Err(err).Str("path", target).
			Msg("WriteTeamSummary 创建临时文件失败")
		return err
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return err
	}
	tmpFile.Close()
	if err := os.Rename(tmpPath, target); err != nil {
		logger.Error(sharedLogComponent).Err(err).Str("tmp", tmpPath).Str("target", target).
			Msg("WriteTeamSummary 原子替换失败")
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// AppendEntry 追加一条团队记忆。
// 对齐 Python append_entry — 真实实现
// 读取现有内容 + 分隔线 + 新条目 → 覆盖写（非原子，适合低频/单 writer）
func (m *SharedMemoryManager) AppendEntry(ctx context.Context, entry string) error {
	existing := m.ReadTeamSummary(ctx)
	var newContent string
	if existing != "" {
		newContent = existing + "\n\n---\n\n" + entry
	} else {
		newContent = entry
	}
	return m.WriteTeamSummary(ctx, newContent)
}
