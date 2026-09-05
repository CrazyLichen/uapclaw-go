package mem_model

import (
	"context"
	"encoding/json"
	"fmt"

	kv "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// UserMemStore 基于 KV 存储的用户记忆 CRUD。
// 对齐 Python: openjiuwen/core/memory/manage/mem_model/user_mem_store.py (UserMemStore)
//
// 键格式:
//
//	UMD/{user_id}/{scope_id}/{mem_id}       — 记忆数据
//	UMD/{user_id}/{scope_id}/ids             — 用户全部 ID 列表
//	UMD/{user_id}/{scope_id}/{mem_type}/ids  — 按类型 ID 列表
//	UMD/{user_id}/{scope_id}/UPT/ids         — 用户画像主题 ID 列表
type UserMemStore struct {
	// kvStore KV 存储后端
	kvStore kv.BaseKVStore
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// byteNumPerID 每个 ID 占用 24 字节（DataIdManager 生成）
	byteNumPerID = 24
	// idsStr ID 列表键后缀
	idsStr = "ids"
	// userProfileTopicStr 用户画像主题键
	userProfileTopicStr = "UPT"
	// keyPrefixStr KV 键前缀
	keyPrefixStr = "UMD"
	// separator 键分隔符
	separator = "/"
	// memTypeFieldKey 记忆类型字段键
	memTypeFieldKey = "mem_type"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// fragmentMemoryTypes 片段记忆类型列表
	// 对齐 Python: FRAGMENT_MEMORY_TYPE = [MemoryType.USER_PROFILE.value, ...]
	fragmentMemoryTypes = []string{
		MemoryTypeUserProfile.String(),
		MemoryTypeSemanticMemory.String(),
		MemoryTypeEpisodicMemory.String(),
	}
	// legacyPrefixes 旧版键前缀列表（对齐 Python LEGACY_PREFIXES）
	legacyPrefixes = []string{}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewUserMemStore 创建用户记忆 KV 存储。
// 对齐 Python: UserMemStore.__init__
func NewUserMemStore(kvStore kv.BaseKVStore) (*UserMemStore, error) {
	if kvStore == nil {
		return nil, exception.BuildError(
			exception.StatusMemoryStoreInitFailed,
			exception.WithParam("store_type", "user mem store"),
			exception.WithParam("error_msg", "kv store instance is None in UserMemStore"),
		)
	}
	// 注册 KV 前缀
	_ = common.KVPrefixRegistry.RegisterCurrent(keyPrefixStr)
	for _, legacyPrefix := range legacyPrefixes {
		_ = common.KVPrefixRegistry.RegisterLegacy(legacyPrefix)
	}
	return &UserMemStore{kvStore: kvStore}, nil
}

// Write 写入记忆数据。若 mem_id 已存在返回 false。
// 对齐 Python: UserMemStore.write
func (s *UserMemStore) Write(ctx context.Context, userID, scopeID, memID string, data map[string]any) (bool, error) {
	if len(data) == 0 {
		logger.Error(logComponent).
			Str("memory_id", memID).
			Str("event_type", "MEMORY_STORE").
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Msg("Write failed, because data is empty")
		return false, nil
	}

	userMemKey := s.getUserMemKey(userID, scopeID, memID)
	exists, err := s.kvStore.Exists(ctx, userMemKey)
	if err != nil {
		return false, err
	}
	if exists {
		logger.Error(logComponent).
			Str("memory_id", memID).
			Str("event_type", "MEMORY_STORE").
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Msg("Write failed, user memory already exists")
		return false, nil
	}

	// 存入记忆数据
	jsonData, err := json.Marshal(data)
	if err != nil {
		return false, err
	}
	if err := s.kvStore.Set(ctx, userMemKey, jsonData); err != nil {
		return false, err
	}

	// 更新 mem_type ids 和 user profile topic ids
	if memType, ok := data[memTypeFieldKey]; ok {
		// 对齐 Python: mem_type 始终是字符串，非 string 视为异常
		memTypeStr, ok := memType.(string)
		if !ok {
			logger.Error(logComponent).Str("memory_id", memID).
				Str("event_type", "MEMORY_STORE").
				Str("field", memTypeFieldKey).
				Msg("mem_type 字段不是字符串类型")
			return false, fmt.Errorf("mem_type 字段不是字符串类型，实际类型: %T", memType)
		}
		// mem_type ids
		userMemIDsKey := s.getUserIDsKey(userID, scopeID, memTypeStr)
		userMemIDsValue, _ := s.kvStore.Get(ctx, userMemIDsKey)
		idsValueStr := ""
		if userMemIDsValue != nil {
			idsValueStr = string(userMemIDsValue)
		}
		if err := s.kvStore.Set(ctx, userMemIDsKey, []byte(writeID(idsValueStr, memID))); err != nil {
			return false, err
		}

		// user profile topic ids（仅片段记忆类型）
		if isFragmentMemoryType(memTypeStr) {
			userMemTopicKey := s.getConcatenationKey([]string{userID, scopeID, userProfileTopicStr, idsStr})
			userMemTopicValue, _ := s.kvStore.Get(ctx, userMemTopicKey)
			topicValueStr := ""
			if userMemTopicValue != nil {
				topicValueStr = string(userMemTopicValue)
			}
			if err := s.kvStore.Set(ctx, userMemTopicKey, []byte(writeID(topicValueStr, memID))); err != nil {
				return false, err
			}
		}
	}

	// 更新 user ids
	userIDsKey := s.getUserIDsKey(userID, scopeID)
	userIDsValue, _ := s.kvStore.Get(ctx, userIDsKey)
	userIDsStr := ""
	if userIDsValue != nil {
		userIDsStr = string(userIDsValue)
	}
	if err := s.kvStore.Set(ctx, userIDsKey, []byte(writeID(userIDsStr, memID))); err != nil {
		return false, err
	}

	return true, nil
}

// Update 更新记忆数据（合并字段）。若 mem_id 不存在返回 false。
// 对齐 Python: UserMemStore.update
func (s *UserMemStore) Update(ctx context.Context, userID, scopeID, memID string, data map[string]any) (bool, error) {
	userMemKey := s.getUserMemKey(userID, scopeID, memID)
	exists, err := s.kvStore.Exists(ctx, userMemKey)
	if err != nil {
		return false, err
	}
	if !exists {
		logger.Error(logComponent).
			Str("memory_id", memID).
			Str("event_type", "MEMORY_UPDATE").
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Msg("Update failed, user memory does not exists")
		return false, nil
	}

	oldData, _ := s.kvStore.Get(ctx, userMemKey)
	if len(oldData) == 0 {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return false, err
		}
		if err := s.kvStore.Set(ctx, userMemKey, jsonData); err != nil {
			return false, err
		}
		return true, nil
	}

	var dictValue map[string]any
	if err := json.Unmarshal(oldData, &dictValue); err != nil {
		return false, err
	}
	for newKey, newValue := range data {
		dictValue[newKey] = newValue
	}
	jsonData, err := json.Marshal(dictValue)
	if err != nil {
		return false, err
	}
	if err := s.kvStore.Set(ctx, userMemKey, jsonData); err != nil {
		return false, err
	}
	return true, nil
}

// Delete 删除指定记忆。
// 对齐 Python: UserMemStore.delete
func (s *UserMemStore) Delete(ctx context.Context, userID, scopeID, memID string) error {
	return s.innerDelete(ctx, userID, scopeID, memID)
}

// BatchDelete 批量删除记忆。
// 对齐 Python: UserMemStore.batch_delete
func (s *UserMemStore) BatchDelete(ctx context.Context, userID, scopeID string, memIDs []string) error {
	for _, memID := range memIDs {
		if err := s.innerDelete(ctx, userID, scopeID, memID); err != nil {
			return err
		}
	}
	return nil
}

// Get 获取指定记忆数据。
// 对齐 Python: UserMemStore.get
func (s *UserMemStore) Get(ctx context.Context, userID, scopeID, memID string) (map[string]any, error) {
	userMemKey := s.getUserMemKey(userID, scopeID, memID)
	return s.get(ctx, userMemKey)
}

// BatchGet 批量获取记忆数据。
// 对齐 Python: UserMemStore.batch_get
func (s *UserMemStore) BatchGet(ctx context.Context, userID, scopeID string, memIDs []string) ([]map[string]any, error) {
	keysList := make([]string, len(memIDs))
	for i, memID := range memIDs {
		keysList[i] = s.getUserMemKey(userID, scopeID, memID)
	}
	valueList, err := s.kvStore.MGet(ctx, keysList)
	if err != nil {
		return nil, err
	}
	if len(valueList) == 0 {
		return []map[string]any{}, nil
	}
	result := make([]map[string]any, 0, len(valueList))
	for _, value := range valueList {
		if value == nil {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(value, &m); err != nil {
			continue
		}
		result = append(result, m)
	}
	return result, nil
}

// GetAll 获取用户指定类型（或全部）记忆。memType 为空时获取全部类型。
// 对齐 Python: UserMemStore.get_all
func (s *UserMemStore) GetAll(ctx context.Context, userID, scopeID, memType string) ([]map[string]any, error) {
	userIDsKey := s.getUserIDsKey(userID, scopeID, memType)
	exists, err := s.kvStore.Exists(ctx, userIDsKey)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	userIDsValue, _ := s.kvStore.Get(ctx, userIDsKey)
	if len(userIDsValue) == 0 {
		return nil, nil
	}
	allIDs := getAllIDs(string(userIDsValue))
	memIDs := make([]string, len(allIDs))
	copy(memIDs, allIDs)
	return s.BatchGet(ctx, userID, scopeID, memIDs)
}

// GetByTopic 按主题获取记忆。
// 对齐 Python: UserMemStore.get_by_topic
func (s *UserMemStore) GetByTopic(ctx context.Context, userID, scopeID, topic string) ([]map[string]any, error) {
	userMemTopicKey := s.getConcatenationKey([]string{userID, scopeID, userProfileTopicStr, topic, idsStr})
	exists, err := s.kvStore.Exists(ctx, userMemTopicKey)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	userMemTopicValue, _ := s.kvStore.Get(ctx, userMemTopicKey)
	if len(userMemTopicValue) == 0 {
		return nil, nil
	}
	allIDs := getAllIDs(string(userMemTopicValue))
	memIDs := make([]string, len(allIDs))
	copy(memIDs, allIDs)
	return s.BatchGet(ctx, userID, scopeID, memIDs)
}

// GetInRange 按范围获取记忆（分页）。
// 对齐 Python: UserMemStore.get_in_range
func (s *UserMemStore) GetInRange(ctx context.Context, userID, scopeID string, startIdx, endIdx int, memType string) ([]map[string]any, error) {
	userIDsKey := s.getUserIDsKey(userID, scopeID, memType)
	exists, err := s.kvStore.Exists(ctx, userIDsKey)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	userIDsValue, _ := s.kvStore.Get(ctx, userIDsKey)
	if len(userIDsValue) == 0 {
		return nil, nil
	}
	memIDs := getIDsInRange(string(userIDsValue), startIdx, endIdx)
	return s.BatchGet(ctx, userID, scopeID, memIDs)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getUserIDsKey 获取用户 ID 列表键。
// 对齐 Python: UserMemStore.__get_user_ids_key
func (s *UserMemStore) getUserIDsKey(userID, scopeID string, memType ...string) string {
	if len(memType) > 0 && memType[0] != "" {
		return s.getConcatenationKey([]string{userID, scopeID, memType[0], idsStr})
	}
	return s.getConcatenationKey([]string{userID, scopeID, idsStr})
}

// getUserMemKey 获取用户记忆数据键。
// 对齐 Python: UserMemStore.__get_user_mem_key
func (s *UserMemStore) getUserMemKey(userID, scopeID, memID string) string {
	return s.getConcatenationKey([]string{userID, scopeID, memID})
}

// getConcatenationKey 拼接 KV 键。
// 对齐 Python: UserMemStore.__get_concatenation_key
func (s *UserMemStore) getConcatenationKey(fields []string) string {
	keyStr := keyPrefixStr
	for _, field := range fields {
		keyStr += fmt.Sprintf("%s%s", separator, field)
	}
	return keyStr
}

// innerDelete 内部删除方法。
// 对齐 Python: UserMemStore.__inner_delete
func (s *UserMemStore) innerDelete(ctx context.Context, userID, scopeID, memID string) error {
	userMemKey := s.getUserMemKey(userID, scopeID, memID)
	exists, err := s.kvStore.Exists(ctx, userMemKey)
	if err != nil {
		return err
	}
	if !exists {
		logger.Warn(logComponent).
			Str("memory_id", memID).
			Str("event_type", "MEMORY_STORE").
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Msg("Delete failed, user memory does not exists")
		return nil
	}

	data, _ := s.kvStore.Get(ctx, userMemKey)
	if len(data) > 0 {
		var dictValue map[string]any
		if err := json.Unmarshal(data, &dictValue); err == nil {
			if memType, ok := dictValue[memTypeFieldKey]; ok {
				// 对齐 Python: mem_type 始终是字符串，非 string 视为异常
				memTypeStr, ok := memType.(string)
				if !ok {
					logger.Error(logComponent).Str("memory_id", memID).
						Str("event_type", "MEMORY_STORE").
						Str("field", memTypeFieldKey).
						Msg("mem_type 字段不是字符串类型")
					return fmt.Errorf("mem_type 字段不是字符串类型，实际类型: %T", memType)
				}
				// 删除 mem_type ids
				userMemIDsKey := s.getUserIDsKey(userID, scopeID, memTypeStr)
				if err := s.deleteMemID(ctx, userMemIDsKey, memID); err != nil {
					return err
				}

				// 删除 user profile topic ids（仅片段记忆类型）
				if isFragmentMemoryType(memTypeStr) {
					userMemTopicKey := s.getConcatenationKey([]string{userID, scopeID, userProfileTopicStr, idsStr})
					if err := s.deleteMemID(ctx, userMemTopicKey, memID); err != nil {
						return err
					}
				}
			}
		}
	}

	// 删除 user ids
	userIDsKey := s.getUserIDsKey(userID, scopeID)
	if err := s.deleteMemID(ctx, userIDsKey, memID); err != nil {
		return err
	}

	// 删除用户记忆数据
	return s.kvStore.Delete(ctx, userMemKey)
}

// deleteMemID 从 ID 列表中删除指定 ID。
// 对齐 Python: UserMemStore.__delete_mem_id
func (s *UserMemStore) deleteMemID(ctx context.Context, idsKey, memID string) error {
	exists, err := s.kvStore.Exists(ctx, idsKey)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	idsValue, _ := s.kvStore.Get(ctx, idsKey)
	if idsValue == nil {
		return nil
	}
	newIDsValue := deleteIDByValue(string(idsValue), memID)
	if newIDsValue != "" {
		return s.kvStore.Set(ctx, idsKey, []byte(newIDsValue))
	}
	return s.kvStore.Delete(ctx, idsKey)
}

// get 内部获取方法。
// 对齐 Python: UserMemStore.__get
func (s *UserMemStore) get(ctx context.Context, memKey string) (map[string]any, error) {
	memValue, err := s.kvStore.Get(ctx, memKey)
	if err != nil {
		return nil, err
	}
	if len(memValue) == 0 {
		return nil, nil
	}
	var result map[string]any
	if err := json.Unmarshal(memValue, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// writeID 将 ID 追加到 ID 列表。
// 对齐 Python: UserMemStore.__write_id
func writeID(dataList, id string) string {
	return dataList + id
}

// deleteIDByValue 从 ID 列表中删除指定 ID。
// 对齐 Python: UserMemStore.__delete_id_by_value
func deleteIDByValue(dataList, idStr string) string {
	total := len(dataList) / byteNumPerID
	for i := 0; i < total; i++ {
		chunk := dataList[i*byteNumPerID : (i+1)*byteNumPerID]
		if chunk == idStr {
			return dataList[:i*byteNumPerID] + dataList[(i+1)*byteNumPerID:]
		}
	}
	return dataList
}

// getAllIDs 返回 ID 列表中的所有 ID。
// 对齐 Python: UserMemStore.__get_all_ids
func getAllIDs(dataList string) []string {
	total := len(dataList) / byteNumPerID
	result := make([]string, total)
	for i := 0; i < total; i++ {
		result[i] = dataList[i*byteNumPerID : (i+1)*byteNumPerID]
	}
	return result
}

// getIDsInRange 返回指定范围内的 ID 列表。
// 对齐 Python: UserMemStore.__get_ids_in_range
func getIDsInRange(dataList string, startIdx, endIdx int) []string {
	total := len(dataList) / byteNumPerID
	startIdx = max(startIdx, 0)
	endIdx = min(endIdx, total)
	if startIdx >= endIdx {
		return []string{}
	}
	result := make([]string, endIdx-startIdx)
	for i := startIdx; i < endIdx; i++ {
		result[i-startIdx] = dataList[i*byteNumPerID : (i+1)*byteNumPerID]
	}
	return result
}

// isFragmentMemoryType 判断是否为片段记忆类型
func isFragmentMemoryType(memType string) bool {
	for _, ft := range fragmentMemoryTypes {
		if ft == memType {
			return true
		}
	}
	return false
}
