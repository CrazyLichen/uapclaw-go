package common

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HitInfo 记忆检索命中结果。
// 对齐 Python: openjiuwen/core/memory/common/base.py hits: list[Tuple[str, float]] 中的单个元素
type HitInfo struct {
	// ID 记忆标识
	ID string
	// Score 相似度分数
	Score float64
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GenerateIdxName 生成向量索引名称。
// 对齐 Python: openjiuwen/core/memory/common/base.py generate_idx_name
// 格式: uid_{usr_id}_gid_{scope_id}_mtype_{mem_type}
func GenerateIdxName(usrID, scopeID, memType string) string {
	return fmt.Sprintf("uid_%s_gid_%s_mtype_%s", usrID, scopeID, memType)
}

// ParseMemtypeFromIdxName 从向量索引名称解析记忆类型。
// 对齐 Python: openjiuwen/core/memory/common/base.py parse_memtype_from_idx_name
// 取最后一个 _ 之后的部分
func ParseMemtypeFromIdxName(idxName string) string {
	parts := strings.Split(idxName, "_")
	return parts[len(parts)-1]
}

// ParseMemoryHitInfos 解析记忆命中结果，分离 ID 列表和分数字典。
// 对齐 Python: openjiuwen/core/memory/common/base.py parse_memory_hit_infos
func ParseMemoryHitInfos(hits []HitInfo) (ids []string, scores map[string]float64, err error) {
	if len(hits) == 0 {
		return []string{}, map[string]float64{}, nil
	}
	ids = make([]string, 0, len(hits))
	scores = make(map[string]float64, len(hits))
	for _, hit := range hits {
		ids = append(ids, hit.ID)
		scores[hit.ID] = hit.Score
	}
	return ids, scores, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
