package sharing

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SharingBackend 经验共享后端接口。
//
// Hub 按 skill_id 分区存储，每个技能保持一个不可变的技能包，
// 经验 bundle 随时间追加到同一 skill_id 下。
//
// backend.LocalFileBackend 隐式实现此接口。
//
// Python: openjiuwen/agent_evolving/sharing/backends/base.py SharingBackend
type SharingBackend interface {
	// UploadBundle 上传经验 bundle，返回上传结果。
	UploadBundle(ctx context.Context, bundle SharedSkillBundle) UploadResult
	// DownloadBundles 按 skill_id 和关键词检索，返回最多 topK 个 bundle。
	DownloadBundles(ctx context.Context, skillID string, query QueryKeywords, topK int) []SharedSkillBundle
	// HasSkillPackage Hub 是否已有该技能包。
	HasSkillPackage(ctx context.Context, skillID string) bool
	// UploadSkillPackage 上传初始技能包（不可变，重复上传为 no-op）。
	UploadSkillPackage(ctx context.Context, skillID string, packageBytes []byte, meta SkillPackageMeta) error
	// DownloadSkillPackage 下载技能包字节。
	DownloadSkillPackage(ctx context.Context, skillID string) ([]byte, error)
	// GetSkillPackageMeta 获取技能包元数据。
	GetSkillPackageMeta(ctx context.Context, skillID string) (*SkillPackageMeta, error)
	// SearchSkills 全局关键词搜索技能。
	SearchSkills(ctx context.Context, query QueryKeywords, topK int) []SkillSearchResult
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件常量，sharing 包和 backend 子包共用。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
