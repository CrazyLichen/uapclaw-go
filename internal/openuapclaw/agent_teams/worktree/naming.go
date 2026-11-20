package worktree

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/worktree"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// teamPartLength 名称中 team 段的最大长度
	// Python: naming.py _TEAM_PART_LENGTH (:14)
	teamPartLength = 14
	// memberPartLength 名称中 member 段的最大长度
	// Python: naming.py _MEMBER_PART_LENGTH (:15)
	memberPartLength = 16
)

// slugInvalidChars 非法字符清洗：保留 [a-z0-9._-]，其余换 "-"
// Python: naming.py _slug_part 的 re.sub(r"[^a-z0-9._-]+", "-", raw)
var slugInvalidChars = regexp.MustCompile(`[^a-z0-9._-]+`)

// ──────────────────────────── 非导出函数 ────────────────────────────

// slugPart 把任意字符串清洗为受长度约束的 slug 段，空值回退 fallback。
// Python: naming.py _slug_part(value, fallback, *, max_length) (:17-22)
func slugPart(value string, fallback string, maxLength int) string {
	raw := strings.ToLower(strings.TrimSpace(value))
	slug := slugInvalidChars.ReplaceAllString(raw, "-")
	slug = strings.Trim(slug, "._-")
	if slug == "" {
		slug = fallback
	}
	if len(slug) > maxLength {
		slug = slug[:maxLength]
	}
	slug = strings.Trim(slug, "._-")
	if slug == "" {
		return fallback
	}
	return slug
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildTeammateWorktreeName 为会话级团队所有者构建确定性 worktree slug。
// Python: naming.py build_teammate_worktree_name(*, team_name, member_name,
// session_id, project_hash) (:24-41)
//
// 形如 agent-{team}-{member}-{hash10}；同一 (team, member, session, project)
// 恒定，跨会话/跨项目互不冲突。slug 非法时返回错误（对齐 validate_slug 抛出）。
func BuildTeammateWorktreeName(teamName string, memberName string, sessionID string, projectHash string) (string, error) {
	team := slugPart(teamName, "team", teamPartLength)
	member := slugPart(memberName, "member", memberPartLength)
	seed := fmt.Sprintf("%s:%s:%s:%s", teamName, memberName, sessionID, projectHash)
	digest := sha256.Sum256([]byte(seed))
	slug := fmt.Sprintf("agent-%s-%s-%s", team, member, hex.EncodeToString(digest[:])[:10])
	if err := worktree.ValidateSlug(slug); err != nil {
		return "", err
	}
	return slug, nil
}
