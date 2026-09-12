package backend

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/evolving/sharing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// SharingBackend 经验共享后端接口。
//
// Hub 按 skill_id 分区存储，每个技能保持一个不可变的技能包，
// 经验 bundle 随时间追加到同一 skill_id 下。
//
// Python: openjiuwen/agent_evolving/sharing/backends/base.py SharingBackend
type SharingBackend interface {
	// UploadBundle 上传经验 bundle，返回上传结果。
	UploadBundle(ctx context.Context, bundle sharing.SharedSkillBundle) sharing.UploadResult

	// DownloadBundles 按 skill_id 和关键词检索，返回最多 topK 个 bundle。
	DownloadBundles(ctx context.Context, skillID string, query sharing.QueryKeywords, topK int) []sharing.SharedSkillBundle

	// HasSkillPackage Hub 是否已有该技能包。
	HasSkillPackage(ctx context.Context, skillID string) bool

	// UploadSkillPackage 上传初始技能包（不可变，重复上传为 no-op）。
	UploadSkillPackage(ctx context.Context, skillID string, packageBytes []byte, meta sharing.SkillPackageMeta) error

	// DownloadSkillPackage 下载技能包字节。
	DownloadSkillPackage(ctx context.Context, skillID string) ([]byte, error)

	// GetSkillPackageMeta 获取技能包元数据。
	GetSkillPackageMeta(ctx context.Context, skillID string) (*sharing.SkillPackageMeta, error)

	// SearchSkills 全局关键词搜索技能。
	SearchSkills(ctx context.Context, query sharing.QueryKeywords, topK int) []sharing.SkillSearchResult
}
