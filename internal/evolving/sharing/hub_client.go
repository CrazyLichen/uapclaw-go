package sharing

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExperienceHubClient 搜索 Hub 技能并安装到本地。
//
// 封装 ExperienceSharer 的搜索/下载能力和 EvolutionStore 的安装能力，
// 提供从搜索到安装的一站式接口。
//
// Python: openjiuwen/agent_evolving/sharing/hub_client.py ExperienceHubClient
type ExperienceHubClient struct {
	// sharer 经验共享门面
	sharer *ExperienceSharer
	// store 演进存储，用于安装技能包
	store *checkpointing.EvolutionStore
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExperienceHubClient 创建 ExperienceHubClient 实例。
//
// 内部创建 ExperienceSharer（localCacheDir=nil，默认重试和退避参数）。
// bk 参数接受 sharing.SharingBackend 接口，backend.LocalFileBackend 隐式实现此接口。
//
// Python: ExperienceHubClient.__init__(backend, evolution_store)
func NewExperienceHubClient(bk SharingBackend, store *checkpointing.EvolutionStore) *ExperienceHubClient {
	sharer := NewExperienceSharer(bk, "", defaultUploadRetries, defaultBackoffSecs, nil)
	return &ExperienceHubClient{
		sharer: sharer,
		store:  store,
	}
}

// Sharer 返回底层 ExperienceSharer。
//
// Python: ExperienceHubClient.sharer (property)
func (c *ExperienceHubClient) Sharer() *ExperienceSharer {
	return c.sharer
}

// SearchSkills 全局搜索技能。
//
// 委托 sharer.SearchSkills。
//
// Python: ExperienceHubClient.search_skills()
func (c *ExperienceHubClient) SearchSkills(ctx context.Context, query QueryKeywords, topK int) []SkillSearchResult {
	return c.sharer.SearchSkills(ctx, query, topK)
}

// InstallSkill 下载并安装 Hub 技能包到本地技能目录。
//
// 流程：
//  1. 下载技能包字节（sharer.DownloadSkillPackage）
//  2. 若包为空，返回错误
//  3. 获取技能包元数据（sharer.GetSkillPackageMeta）
//  4. 确定 targetName：skillName 参数 > meta.SkillName > ""
//  5. 安装技能包（store.InstallSkillPackage）
//  6. 返回安装路径
//
// Python: ExperienceHubClient.install_skill()
func (c *ExperienceHubClient) InstallSkill(ctx context.Context, skillID string, skillName string) (string, error) {
	resolvedID := strings.TrimSpace(skillID)
	if resolvedID == "" {
		return "", fmt.Errorf("skill_id 不能为空")
	}

	packageBytes := c.sharer.DownloadSkillPackage(ctx, resolvedID)
	if len(packageBytes) == 0 {
		logger.Warn(logComponent).
			Str("skill_id", resolvedID).
			Msg("[ExperienceHubClient] no package found for skill_id")
		return "", fmt.Errorf("未找到技能包: skill_id=%s", resolvedID)
	}

	meta := c.sharer.GetSkillPackageMeta(ctx, resolvedID)
	targetName := strings.TrimSpace(skillName)
	if targetName == "" && meta != nil {
		targetName = strings.TrimSpace(meta.SkillName)
	}

	installed, err := c.store.InstallSkillPackage(ctx, packageBytes, targetName)
	if err != nil {
		return "", err
	}
	if installed == "" {
		return "", fmt.Errorf("安装技能包失败: skill_id=%s", resolvedID)
	}

	logger.Info(logComponent).
		Str("skill_id", resolvedID).
		Str("installed", installed).
		Msg("[ExperienceHubClient] installed skill")
	return installed, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
