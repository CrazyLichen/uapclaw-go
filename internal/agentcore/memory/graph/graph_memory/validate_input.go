package graph_memory

import (
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// storeType 图记忆存储类型标识，用于异常信息
	//
	// Python: _STORE_TYPE = "graph mem store"
	storeType = "graph mem store"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateAddMemoryInput 校验添加记忆的输入参数
//
// 依次校验：
//  1. contentFmtKwargs 为非 nil 时必须是 dict[str,str] 且键值非空
//  2. srcType 必须是有效的 EpisodeType
//  3. userID 必须是非空字符串且长度不超过 userIDMaxLength
//
// Python: validate_add_memory_input(user_id_max_length, src_type, user_id, content_fmt_kwargs)
func ValidateAddMemoryInput(userIDMaxLength int, srcType config.EpisodeType, userID string, contentFmtKwargs map[string]string) error {
	// 校验 contentFmtKwargs
	if contentFmtKwargs != nil {
		if len(contentFmtKwargs) == 0 {
			return exception.BuildError(
				exception.StatusMemoryStoreValidationInvalid,
				exception.WithParam("store_type", storeType),
				exception.WithParam("error_msg", "When supplied, content_fmt_kwargs must be of type dict[str, str] and not empty"),
			)
		}
		for k, v := range contentFmtKwargs {
			if k == "" || v == "" {
				return exception.BuildError(
					exception.StatusMemoryStoreValidationInvalid,
					exception.WithParam("store_type", storeType),
					exception.WithParam("error_msg", "content_fmt_kwargs must have non-empty keys and values of string type"),
				)
			}
		}
	}

	// 校验 srcType
	if !isValidEpisodeType(srcType) {
		return exception.BuildError(
			exception.StatusMemoryStoreValidationInvalid,
			exception.WithParam("store_type", storeType),
			exception.WithParam("error_msg", "src_type must be one of [EpisodeType.CONVERSATION, EpisodeType.DOCUMENT, EpisodeType.JSON]"),
		)
	}

	// 校验 userID
	trimmed := strings.TrimSpace(userID)
	if len(trimmed) < 1 || len(trimmed) > userIDMaxLength {
		return exception.BuildError(
			exception.StatusMemoryStoreValidationInvalid,
			exception.WithParam("store_type", storeType),
			exception.WithParam("error_msg", fmt.Sprintf("user_id must be a string of length <= %d (preferably UUID4)", userIDMaxLength)),
		)
	}

	return nil
}

// ValidateSearchInput 校验搜索记忆的输入参数
//
// 依次校验：
//  1. query 必须是非空字符串
//  2. userID 展开为列表后，每个元素必须是非空字符串且长度 <= 32
//  3. settings 中每个元素必须为布尔值
//
// 返回展开后的 userID 列表。
//
// Python: validate_search_input(query, user_id, settings)
func ValidateSearchInput(query string, userID any, settings []bool) ([]string, error) {
	// 校验 query
	if strings.TrimSpace(query) == "" {
		return nil, exception.BuildError(
			exception.StatusMemoryStoreValidationInvalid,
			exception.WithParam("store_type", storeType),
			exception.WithParam("error_msg", "query must be a non-empty string value"),
		)
	}

	// 将 userID 规范化为 []string
	userIDs, err := normalizeUserIDs(userID)
	if err != nil {
		return nil, err
	}

	// 校验每个 userID 元素
	for _, uid := range userIDs {
		trimmed := strings.TrimSpace(uid)
		if trimmed == "" || len(trimmed) > 32 {
			return nil, exception.BuildError(
				exception.StatusMemoryStoreValidationInvalid,
				exception.WithParam("store_type", storeType),
				exception.WithParam("error_msg", "user_id must be a non-empty string of length <= 32 or a list of such strings"),
			)
		}
	}

	// 校验 settings 中每个元素为 bool（Go 类型已保证，但需检查长度）
	// Python: if not all(isinstance(s, bool) for s in settings)
	// Go 中 settings 参数类型已为 []bool，无需逐个检查类型

	return userIDs, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isValidEpisodeType 检查 EpisodeType 是否为有效值
//
// Python: isinstance(src_type, EpisodeType)
func isValidEpisodeType(t config.EpisodeType) bool {
	switch t {
	case config.EpisodeTypeConversation, config.EpisodeTypeDocument, config.EpisodeTypeJSON:
		return true
	default:
		return false
	}
}

// normalizeUserIDs 将 userID 规范化为 []string
//
// Python: if not isinstance(user_id, list): user_id = [user_id]
func normalizeUserIDs(userID any) ([]string, error) {
	switch uid := userID.(type) {
	case string:
		return []string{uid}, nil
	case []string:
		return uid, nil
	default:
		return nil, exception.BuildError(
			exception.StatusMemoryStoreValidationInvalid,
			exception.WithParam("store_type", storeType),
			exception.WithParam("error_msg", "user_id must be a string or a list of strings"),
		)
	}
}
