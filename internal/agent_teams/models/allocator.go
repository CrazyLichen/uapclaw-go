package models

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Allocation 模型分配结果。
// 对齐 Python: Allocation (openjiuwen/agent_teams/models/allocator.py)
//
// 携带被选中的池条目以及持久化 DB 引用所需的位置信息。
type Allocation struct {
	// Entry 选中的池条目
	Entry ModelPoolEntry
	// GroupIndex 条目在同名组内的位置索引
	GroupIndex int
}

// ──────────────────────────── 接口 ────────────────────────────

// ModelAllocator 模型分配器接口。
// 对齐 Python: ModelAllocator Protocol
//
// 实现封装从池中选取下一个条目的策略（轮询/按名/路由）。
// 返回 nil 表示"无可用条目"——调用者回退到每 Agent 的模型配置。
type ModelAllocator interface {
	// Allocate 返回下一次分配，或 nil 当不可用时。
	Allocate(modelName string) *Allocation
	// StateDict 返回分配器计数器的 JSON 友好快照。
	StateDict() map[string]any
	// LoadStateDict 从先前的 StateDict 恢复计数器。
	LoadStateDict(state map[string]any)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ToTeamModelConfig 物化为 TeamModelConfig。
// 对齐 Python: Allocation.to_team_model_config()
func (a Allocation) ToTeamModelConfig() TeamModelConfig {
	return a.Entry.ToTeamModelConfig()
}

// ToDBRef 产生轻量级的 {model_name, model_index} 引用用于 DB 持久化。
// 对齐 Python: Allocation.to_db_ref()
func (a Allocation) ToDBRef() map[string]any {
	return map[string]any{
		"model_name":  a.Entry.ModelName,
		"model_index": a.GroupIndex,
	}
}

// BuildModelAllocatorForPool 根据模型池和策略构建分配器。
// 对齐 Python: build_model_allocator(spec, team_spec)
// ⤵️ 回填: 9.64 — allocator 运行时逻辑（RoundRobin/ByModelName/Router 分发）
//
// 此函数接受基本类型参数以避免 import 循环。
// 调用者应从 TeamAgentSpec/TeamSpec 中提取 pool 和 strategy 传入。
func BuildModelAllocatorForPool(pool []ModelPoolEntry, strategy string, teamName string) ModelAllocator {
	// ⤵️ 回填: 9.64 — 根据 strategy 分发到具体 allocator
	if len(pool) == 0 {
		return nil
	}
	logger.Info(logger.ComponentCommon).Str("team_name", teamName).
		Str("strategy", strategy).
		Int("pool_size", len(pool)).
		Msg("BuildModelAllocatorForPool 留桩：分配器尚未实现")
	return nil
}

// ResolveMemberModelFromPool 从池中按引用解析成员模型。
// 对齐 Python: resolve_member_model(team_spec, model_name, model_index)
// ⤵️ 回填: 9.64 — member model 解析逻辑
//
// 此函数接受基本类型参数以避免 import 循环。
func ResolveMemberModelFromPool(pool []ModelPoolEntry, modelName string, modelIndex int) *TeamModelConfig {
	// ⤵️ 回填: 9.64 — 从 pool 按 modelName + modelIndex 解析
	if len(pool) == 0 || modelName == "" {
		return nil
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// poolDigest 计算池结构形状的稳定摘要。
// 对齐 Python: _pool_digest(pool)
//
// 捕获每条目的 (model_name, api_base_url) 顺序。
// 凭证或元数据变更不改变摘要；重排或增删条目会改变。
func poolDigest(pool []ModelPoolEntry) string {
	h := sha1.New()
	for _, entry := range pool {
		h.Write([]byte(entry.ModelName))
		h.Write([]byte{0x00})
		h.Write([]byte(entry.APIBaseURL))
		h.Write([]byte{0x1f})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// groupIndexOf 返回 entry 在 group 中的引用位置。
// 对齐 Python: _group_index_of(entry, group)
func groupIndexOf(entry ModelPoolEntry, group []ModelPoolEntry) int {
	for i, candidate := range group {
		if entrySignature(entry) == entrySignature(candidate) && entry.ModelID == candidate.ModelID {
			return i
		}
	}
	return 0
}

// marshalForSignature 将值序列化为 JSON 字符串，用于签名计算。
func marshalForSignature(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}
