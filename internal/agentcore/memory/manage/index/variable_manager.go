package index

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/codec"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VariableManager 变量记忆管理器，管理变量型记忆的全生命周期。
//
// 独立实现 BaseMemoryManager 接口，不嵌入 memoryManagerBase（因为依赖 BaseKVStore
// 而非 BaseMemoryIndex，validateParams 中 memoryIndex==nil 检查会误报）。
// 通过 BaseKVStore 存取变量记忆，用 AesStorageCodec 加密。
// 变量记忆用于存储用户偏好、会话变量等键值对。
//
// 对应 Python: openjiuwen/core/memory/manage/index/variable_manager.py (VariableManager)
type VariableManager struct {
	// kvStore KV 存储后端
	kvStore kv.BaseKVStore
	// cryptoKey 加密密钥
	cryptoKey []byte
	// aesCodec AES 编解码器
	aesCodec *codec.AesStorageCodec
	// memType 类型标识
	memType string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// separator KV key 分隔符
	// 对齐 Python: VariableManager.SEPARATOR = "/"
	separator = "/"
	// userVarPrefix 用户级变量前缀
	// 对齐 Python: VariableManager.USER_VAR_PREFIX = "user_var"
	userVarPrefix = "user_var"
	// sessionVarPrefix 会话级变量前缀
	// 对齐 Python: VariableManager.SESSION_VAR_PREFIX = "session_var"
	sessionVarPrefix = "session_var"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVariableManager 创建变量记忆管理器。
//
// 注册 KV 前缀到全局注册表，创建 AES 编解码器。
//
// 对齐 Python: VariableManager.__init__(kv_store, crypto_key)
func NewVariableManager(kvStore kv.BaseKVStore, cryptoKey []byte) (*VariableManager, error) {
	// 对齐 Python: self._codec = AesStorageCodec(crypto_key)
	aesCodec, err := codec.NewAesStorageCodec(cryptoKey)
	if err != nil {
		return nil, fmt.Errorf("创建 AesStorageCodec 失败: %w", err)
	}

	// 对齐 Python: kv_prefix_registry.register_current(self.USER_VAR_PREFIX)
	_ = common.KVPrefixRegistry.RegisterCurrent(userVarPrefix)
	// 对齐 Python: kv_prefix_registry.register_current(self.SESSION_VAR_PREFIX)
	_ = common.KVPrefixRegistry.RegisterCurrent(sessionVarPrefix)

	return &VariableManager{
		kvStore:   kvStore,
		cryptoKey: cryptoKey,
		aesCodec:  aesCodec,
		memType:   mem_model.MemoryTypeVariable.String(),
	}, nil
}

// AddMemories 批量添加变量记忆。
//
// 遍历 VariableUnit，编码后写入 KV 存储。
//
// 对齐 Python: VariableManager.add_memories
func (m *VariableManager) AddMemories(ctx context.Context, userID string, scopeID string,
	memories map[string][]mem_model.MemoryUnit, _ ...*llm.Model) ([]mem_model.MemoryUnit, error) {

	// 对齐 Python: for mem_type, memory in memories.items():
	//               if mem_type != self.mem_type: 跳过
	for memType, units := range memories {
		if memType != m.memType {
			continue
		}
		for _, unit := range units {
			varUnit, ok := unit.(*mem_model.VariableUnit)
			if !ok {
				// 对齐 Python: memory_logger.warning("mem_unit is not a VariableUnit", ...)
				logger.Warn(logComponent).
					Str("event_type", "MEMORY_STORE").
					Str("memory_type", m.memType).
					Str("user_id", userID).
					Str("scope_id", scopeID).
					Msg("mem_unit 不是 VariableUnit 类型，跳过")
				continue
			}
			if m.kvStore == nil {
				// 对齐 Python: memory_logger.error("kv_store cannot be None", ...); return []
				logger.Error(logComponent).
					Str("event_type", "MEMORY_STORE").
					Str("memory_type", m.memType).
					Str("user_id", userID).
					Str("scope_id", scopeID).
					Msg("kv_store 不能为 nil")
				return nil, nil
			}
			// 对齐 Python: key, value = self._make_variable_pairs(user_id, False, scope_id, unit.variable_name, None, unit.variable_mem, None)
			key, value := m.makeVariablePairs(userID, false, scopeID, varUnit.VariableName, "", varUnit.VariableMem, "")
			if err := m.kvStore.Set(ctx, key, value); err != nil {
				logger.Error(logComponent).
					Err(err).
					Str("event_type", "MEMORY_STORE").
					Str("memory_type", m.memType).
					Str("user_id", userID).
					Str("scope_id", scopeID).
					Msg("写入变量失败")
				return nil, err
			}
		}
	}

	// 对齐 Python: return memories.get(self.mem_type, [])
	var result []mem_model.MemoryUnit
	if units, ok := memories[m.memType]; ok {
		for _, u := range units {
			if _, ok := u.(*mem_model.VariableUnit); ok {
				result = append(result, u)
			}
		}
	}
	return result, nil
}

// Update 按 ID 更新变量记忆。
//
// ⚠️ 未实现 — 对齐 Python: memory_logger.warning("Not implemented method update"); pass
func (m *VariableManager) Update(_ context.Context, userID string, scopeID string, memID string, _ string) (bool, error) {
	logger.Warn(logComponent).
		Str("event_type", "MEMORY_STORE").
		Str("memory_type", m.memType).
		Strs("memory_id", []string{memID}).
		Str("user_id", userID).
		Str("scope_id", scopeID).
		Msg("未实现方法 update")
	return false, nil
}

// Search 语义搜索变量记忆。
//
// ⚠️ 未实现 — 对齐 Python: memory_logger.warning("Not implemented method search"); pass
func (m *VariableManager) Search(_ context.Context, userID string, scopeID string, query string, _ int, _ []string) ([]*index.MemorySearchResult, error) {
	logger.Warn(logComponent).
		Str("event_type", "MEMORY_STORE").
		Str("memory_type", m.memType).
		Str("query", query).
		Str("user_id", userID).
		Str("scope_id", scopeID).
		Msg("未实现方法 search")
	return nil, nil
}

// Get 按 ID 获取变量记忆。
//
// ⚠️ 未实现 — 对齐 Python: memory_logger.warning("Not implemented method get"); pass
func (m *VariableManager) Get(_ context.Context, userID string, scopeID string, memID string) (*index.MemoryDoc, error) {
	logger.Warn(logComponent).
		Strs("memory_id", []string{memID}).
		Str("memory_type", m.memType).
		Str("user_id", userID).
		Str("scope_id", scopeID).
		Msg("未实现方法 get")
	return nil, nil
}

// Delete 按 ID 删除变量记忆。
//
// ⚠️ 未实现 — 对齐 Python: memory_logger.error("Not implemented method delete"); pass
func (m *VariableManager) Delete(_ context.Context, userID string, scopeID string, memID string) (bool, error) {
	logger.Error(logComponent).
		Str("event_type", "MEMORY_STORE").
		Strs("memory_id", []string{memID}).
		Str("memory_type", m.memType).
		Str("user_id", userID).
		Str("scope_id", scopeID).
		Msg("未实现方法 delete")
	return false, nil
}

// DeleteByUserID 删除用户+scope 下所有变量记忆。
//
// 按 user_var/session_var 前缀批量删除。
//
// 对齐 Python: VariableManager.delete_by_user_id
func (m *VariableManager) DeleteByUserID(ctx context.Context, userID string, scopeID string) (bool, error) {
	if m.kvStore == nil {
		// 对齐 Python: memory_logger.error("kv_store cannot be None", ...); return
		logger.Error(logComponent).
			Str("event_type", "MEMORY_STORE").
			Str("memory_type", m.memType).
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Msg("kv_store 不能为 nil")
		return false, nil
	}

	// 对齐 Python:
	//   用户前缀 = f"{self.USER_VAR_PREFIX}{self.SEPARATOR}{user_id}{self.SEPARATOR}{scope_id}{self.SEPARATOR}"
	//   会话前缀 = f"{self.SESSION_VAR_PREFIX}{self.SEPARATOR}{user_id}{self.SEPARATOR}{scope_id}{self.SEPARATOR}"
	userPrefix := fmt.Sprintf("%s%s%s%s%s%s", userVarPrefix, separator, userID, separator, scopeID, separator)
	sessionPrefix := fmt.Sprintf("%s%s%s%s%s%s", sessionVarPrefix, separator, userID, separator, scopeID, separator)

	// 对齐 Python: await self.kv_store.delete_by_prefix(user_prefix)
	if err := m.kvStore.DeleteByPrefix(ctx, userPrefix, 0); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", "MEMORY_STORE").
			Str("memory_type", m.memType).
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Msg("按前缀删除用户变量失败")
		return false, err
	}
	// 对齐 Python: await self.kv_store.delete_by_prefix(session_prefix)
	if err := m.kvStore.DeleteByPrefix(ctx, sessionPrefix, 0); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", "MEMORY_STORE").
			Str("memory_type", m.memType).
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Msg("按前缀删除会话变量失败")
		return false, err
	}
	return true, nil
}

// UpdateUserVariable 更新用户变量。
//
// 先查询变量是否存在，存在时更新。
//
// 对齐 Python: VariableManager.update_user_variable
func (m *VariableManager) UpdateUserVariable(ctx context.Context, userID string, scopeID string, varName string, varMem string) error {
	if m.kvStore == nil {
		// 对齐 Python: memory_logger.error("KV_store cannot be None", ...); return
		logger.Error(logComponent).
			Str("event_type", "MEMORY_STORE").
			Str("memory_type", m.memType).
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Msg("KV_store 不能为 nil")
		return nil
	}

	// 对齐 Python: existing_variable = await self.query_variable(user_id=user_id, scope_id=scope_id, name=var_name)
	existing, err := m.QueryVariable(ctx, userID, scopeID, varName, "")
	if err != nil {
		return err
	}
	// 对齐 Python: if not VariableManager._check_exist(existing_variable, var_name): return
	if !checkExist(existing, varName) {
		return nil
	}

	// 对齐 Python: key, value = self._make_variable_pairs(usr_id=user_id, for_deletion=False, scope_id=scope_id, var_name=var_name, user_var_value=var_mem)
	key, value := m.makeVariablePairs(userID, false, scopeID, varName, "", varMem, "")
	return m.kvStore.Set(ctx, key, value)
}

// DeleteUserVariable 按变量名删除用户变量。
//
// 对齐 Python: VariableManager.delete_user_variable
func (m *VariableManager) DeleteUserVariable(ctx context.Context, userID string, scopeID string, varName string) error {
	if m.kvStore == nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_STORE").
			Str("memory_type", m.memType).
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Msg("kv_store 不能为 nil")
		return nil
	}

	// 对齐 Python: key, _ = self._make_variable_pairs(usr_id=user_id, for_deletion=False, scope_id=scope_id, var_name=var_name)
	key, _ := m.makeVariablePairs(userID, false, scopeID, varName, "", "", "")
	return m.kvStore.Delete(ctx, key)
}

// QueryVariable 查询变量。
//
// name 为空时按前缀查全部，否则按 name 查单值。
// sessionID 非空时查会话级变量，否则查用户级变量。
//
// 对齐 Python: VariableManager.query_variable
func (m *VariableManager) QueryVariable(ctx context.Context, userID string, scopeID string, name string, sessionID string) (map[string]string, error) {
	// 对齐 Python: self._check_user_and_scope_id(user_id, scope_id, "Search")
	m.checkUserAndScopeID(userID, scopeID, "Search")

	// 对齐 Python: if not name or not name.strip():
	if name == "" {
		// 对齐 Python: prefix_str = f"{self.USER_VAR_PREFIX}{self.SEPARATOR}{user_id}{self.SEPARATOR}{scope_id}{self.SEPARATOR}"
		prefixStr := fmt.Sprintf("%s%s%s%s%s%s", userVarPrefix, separator, userID, separator, scopeID, separator)
		// 对齐 Python: kv_ret = await self.kv_store.get_by_prefix(prefix_str)
		kvRet, err := m.kvStore.GetByPrefix(ctx, prefixStr)
		if err != nil {
			return nil, err
		}
		// 对齐 Python: result = {}; for k, v in kv_ret.items(): v = self._codec.decode(v); result[k.split(f"{self.SEPARATOR}")[-1]] = v
		result := make(map[string]string)
		for k, v := range kvRet {
			decoded := m.aesCodec.Decode(string(v))
			// 对齐 Python: k.split(f"{self.SEPARATOR}")[-1]
			parts := strings.Split(k, separator)
			result[parts[len(parts)-1]] = decoded
		}
		return result, nil
	}

	// 对齐 Python: if session_id:
	//   会话变量键 = f"{self.SESSION_VAR_PREFIX}{self.SEPARATOR}{user_id}...{session_id}{self.SEPARATOR}{name}"
	var key string
	if sessionID != "" {
		key = fmt.Sprintf("%s%s%s%s%s%s%s%s%s", sessionVarPrefix, separator, userID, separator, scopeID, separator, sessionID, separator, name)
	} else {
		// 对齐 Python: key = f"{self.USER_VAR_PREFIX}{self.SEPARATOR}{user_id}{self.SEPARATOR}{scope_id}{self.SEPARATOR}{name}"
		key = fmt.Sprintf("%s%s%s%s%s%s%s", userVarPrefix, separator, userID, separator, scopeID, separator, name)
	}

	// 对齐 Python: kv_ret = await self.kv_store.get(key); kv_ret = self._codec.decode(kv_ret); return {name: kv_ret}
	raw, err := m.kvStore.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	var decoded string
	if raw != nil {
		decoded = m.aesCodec.Decode(string(raw))
	}
	return map[string]string{name: decoded}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// makeVariablePairs 构造 KV 键值对。
//
// 对齐 Python: VariableManager._make_variable_pairs
func (m *VariableManager) makeVariablePairs(usrID string, forDeletion bool, scopeID string, varName string, sessionID string, userVarValue string, sessionVarValue string) (string, []byte) {
	var key string
	var value []byte

	// 对齐 Python: user_var_value = self._codec.encode(user_var_value)
	encodedUserValue := m.aesCodec.Encode(userVarValue)
	// 对齐 Python: session_var_value = self._codec.encode(session_var_value)
	encodedSessionValue := m.aesCodec.Encode(sessionVarValue)

	if varName != "" {
		if sessionID == "" {
			// 对齐 Python: 1) 用户变量
			//   用户变量键 = f"{self.USER_VAR_PREFIX}{VariableManager.SEPARATOR}{usr_id}...{var_name}"
			key = fmt.Sprintf("%s%s%s%s%s%s%s", userVarPrefix, separator, usrID, separator, scopeID, separator, varName)
			if !forDeletion {
				value = []byte(encodedUserValue)
			}
		} else {
			// 对齐 Python: 2) 会话变量
			//   会话变量键 = f"{self.SESSION_VAR_PREFIX}{VariableManager.SEPARATOR}{usr_id}...{var_name}"
			key = fmt.Sprintf("%s%s%s%s%s%s%s%s%s", sessionVarPrefix, separator, usrID, separator, scopeID, separator, sessionID, separator, varName)
			if !forDeletion {
				value = []byte(encodedSessionValue)
			}
		}
	}
	return key, value
}

// checkUserAndScopeID 校验用户 ID 和 scope ID。
//
// 对齐 Python: VariableManager._check_user_and_scope_id
func (m *VariableManager) checkUserAndScopeID(userID string, scopeID string, context string) {
	if userID == "" || strings.TrimSpace(userID) == "" {
		// 对齐 Python: memory_logger.error("Check user and scope id operation failed, user ID is empty", ...)
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("memory_type", "variable").
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Str("context", context).
			Msg("校验用户和 scope 失败，user ID 为空")
	}
	if scopeID == "" || strings.TrimSpace(scopeID) == "" {
		// 对齐 Python: memory_logger.error("Check user and scope id operation failed, scope ID is empty", ...)
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("memory_type", "variable").
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Str("context", context).
			Msg("校验用户和 scope 失败，scope ID 为空")
	}
}

// checkExist 检查变量字典中是否存在指定变量且值非空。
//
// 对齐 Python: VariableManager._check_exist
func checkExist(variableDict map[string]string, variableName string) bool {
	if len(variableDict) == 0 {
		return false
	}
	val, ok := variableDict[variableName]
	if !ok {
		return false
	}
	return val != ""
}
