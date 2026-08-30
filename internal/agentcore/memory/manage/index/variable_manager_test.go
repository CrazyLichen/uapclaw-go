//go:build test

package index

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
)

func TestNewVariableManager(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, err := NewVariableManager(store, nil)
	if err != nil {
		t.Fatalf("NewVariableManager 返回 error: %v", err)
	}
	if mgr == nil {
		t.Fatal("NewVariableManager 返回 nil")
	}
	if mgr.memType != "variable" {
		t.Errorf("memType = %q, want %q", mgr.memType, "variable")
	}
}

func TestNewVariableManager_NilKVStore(t *testing.T) {
	mgr, err := NewVariableManager(nil, nil)
	if err != nil {
		t.Fatalf("NewVariableManager(nil kvStore) 返回 error: %v", err)
	}
	if mgr == nil {
		t.Fatal("NewVariableManager(nil kvStore) 返回 nil")
	}
}

func TestVariableManager_AddMemories(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)

	memories := map[string][]mem_model.MemoryUnit{
		"variable": {
			&mem_model.VariableUnit{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{
					MemType: mem_model.MemoryTypeVariable,
					MemID:   "var-001",
				},
				VariableName: "age",
				VariableMem:  "25",
			},
		},
	}
	result, err := mgr.AddMemories(context.Background(), "user-1", "scope-1", memories)
	if err != nil {
		t.Fatalf("AddMemories 返回 error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("期望返回 1 个结果，得到 %d", len(result))
	}
	// 验证写入到 InMemoryKVStore
	key := "user_var/user-1/scope-1/age"
	raw, _ := store.Get(context.Background(), key)
	if raw == nil {
		t.Fatal("期望变量已写入，但 Get 返回 nil")
	}
	if string(raw) != "25" {
		t.Errorf("value = %q, want %q", string(raw), "25")
	}
}

func TestVariableManager_AddMemories_NonVariableTypeIgnored(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)

	memories := map[string][]mem_model.MemoryUnit{
		"user_profile": {
			&mem_model.FragmentMemoryUnit{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeUserProfile, MemID: "prof-001"},
				Content: "用户画像",
			},
		},
	}
	result, err := mgr.AddMemories(context.Background(), "user-1", "scope-1", memories)
	if err != nil {
		t.Fatalf("AddMemories 返回 error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("期望返回 0 个结果（非 variable 类型被忽略），得到 %d", len(result))
	}
}

func TestVariableManager_AddMemories_KVStoreNil(t *testing.T) {
	mgr, _ := NewVariableManager(nil, nil)

	memories := map[string][]mem_model.MemoryUnit{
		"variable": {
			&mem_model.VariableUnit{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeVariable, MemID: "var-001"},
				VariableName: "age", VariableMem: "25",
			},
		},
	}
	result, err := mgr.AddMemories(context.Background(), "user-1", "scope-1", memories)
	if err != nil {
		t.Fatalf("AddMemories 返回 error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("kvStore 为 nil 时应返回空，得到 %d", len(result))
	}
}

func TestVariableManager_Update_NotImplemented(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)
	ok, err := mgr.Update(context.Background(), "user-1", "scope-1", "var-001", "new_value")
	if err != nil {
		t.Fatalf("Update 返回 error: %v", err)
	}
	if ok {
		t.Error("Not implemented 方法应返回 false")
	}
}

func TestVariableManager_Delete_NotImplemented(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)
	ok, err := mgr.Delete(context.Background(), "user-1", "scope-1", "var-001")
	if err != nil {
		t.Fatalf("Delete 返回 error: %v", err)
	}
	if ok {
		t.Error("Not implemented 方法应返回 false")
	}
}

func TestVariableManager_Get_NotImplemented(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)
	doc, err := mgr.Get(context.Background(), "user-1", "scope-1", "var-001")
	if err != nil {
		t.Fatalf("Get 返回 error: %v", err)
	}
	if doc != nil {
		t.Error("Not implemented 方法应返回 nil")
	}
}

func TestVariableManager_Search_NotImplemented(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)
	result, err := mgr.Search(context.Background(), "user-1", "scope-1", "query", 5, nil)
	if err != nil {
		t.Fatalf("Search 返回 error: %v", err)
	}
	if result != nil {
		t.Error("Not implemented 方法应返回 nil")
	}
}

func TestVariableManager_DeleteByUserID(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)

	// 预先写入变量
	_ = store.Set(context.Background(), "user_var/user-1/scope-1/age", []byte("25"))
	_ = store.Set(context.Background(), "user_var/user-1/scope-1/name", []byte("Alice"))
	_ = store.Set(context.Background(), "session_var/user-1/scope-1/session-1/token", []byte("abc"))

	ok, err := mgr.DeleteByUserID(context.Background(), "user-1", "scope-1")
	if err != nil {
		t.Fatalf("DeleteByUserID 返回 error: %v", err)
	}
	if !ok {
		t.Error("期望返回 true")
	}

	// 验证变量已删除
	raw, _ := store.Get(context.Background(), "user_var/user-1/scope-1/age")
	if raw != nil {
		t.Error("期望用户变量已删除")
	}
	raw, _ = store.Get(context.Background(), "session_var/user-1/scope-1/session-1/token")
	if raw != nil {
		t.Error("期望会话变量已删除")
	}
}

func TestVariableManager_DeleteByUserID_KVStoreNil(t *testing.T) {
	mgr, _ := NewVariableManager(nil, nil)
	ok, err := mgr.DeleteByUserID(context.Background(), "user-1", "scope-1")
	if err != nil {
		t.Fatalf("DeleteByUserID 返回 error: %v", err)
	}
	if ok {
		t.Error("kvStore 为 nil 时应返回 false")
	}
}

func TestVariableManager_UpdateUserVariable(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)

	// 先添加变量
	_ = store.Set(context.Background(), "user_var/user-1/scope-1/age", []byte("25"))

	err := mgr.UpdateUserVariable(context.Background(), "user-1", "scope-1", "age", "30")
	if err != nil {
		t.Fatalf("UpdateUserVariable 返回 error: %v", err)
	}

	// 验证更新后的值
	raw, _ := store.Get(context.Background(), "user_var/user-1/scope-1/age")
	if raw == nil || string(raw) != "30" {
		t.Errorf("更新后 value = %q, want %q", string(raw), "30")
	}
}

func TestVariableManager_UpdateUserVariable_NotExist(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)

	// 变量不存在时不更新
	err := mgr.UpdateUserVariable(context.Background(), "user-1", "scope-1", "nonexistent", "value")
	if err != nil {
		t.Fatalf("UpdateUserVariable 返回 error: %v", err)
	}

	// 验证未写入
	raw, _ := store.Get(context.Background(), "user_var/user-1/scope-1/nonexistent")
	if raw != nil {
		t.Error("变量不存在时不应写入")
	}
}

func TestVariableManager_DeleteUserVariable(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)

	_ = store.Set(context.Background(), "user_var/user-1/scope-1/age", []byte("25"))

	err := mgr.DeleteUserVariable(context.Background(), "user-1", "scope-1", "age")
	if err != nil {
		t.Fatalf("DeleteUserVariable 返回 error: %v", err)
	}

	raw, _ := store.Get(context.Background(), "user_var/user-1/scope-1/age")
	if raw != nil {
		t.Error("期望变量已删除")
	}
}

func TestVariableManager_QueryVariable_ByName(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)

	_ = store.Set(context.Background(), "user_var/user-1/scope-1/age", []byte("25"))

	result, err := mgr.QueryVariable(context.Background(), "user-1", "scope-1", "age", "")
	if err != nil {
		t.Fatalf("QueryVariable 返回 error: %v", err)
	}
	if result["age"] != "25" {
		t.Errorf("result[age] = %q, want %q", result["age"], "25")
	}
}

func TestVariableManager_QueryVariable_All(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)

	_ = store.Set(context.Background(), "user_var/user-1/scope-1/age", []byte("25"))
	_ = store.Set(context.Background(), "user_var/user-1/scope-1/name", []byte("Alice"))

	// name 为空时查全部
	result, err := mgr.QueryVariable(context.Background(), "user-1", "scope-1", "", "")
	if err != nil {
		t.Fatalf("QueryVariable 返回 error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("期望返回 2 个变量，得到 %d", len(result))
	}
	if result["age"] != "25" {
		t.Errorf("result[age] = %q, want %q", result["age"], "25")
	}
	if result["name"] != "Alice" {
		t.Errorf("result[name] = %q, want %q", result["name"], "Alice")
	}
}

func TestVariableManager_QueryVariable_WithSessionID(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)

	_ = store.Set(context.Background(), "session_var/user-1/scope-1/session-1/token", []byte("abc123"))

	result, err := mgr.QueryVariable(context.Background(), "user-1", "scope-1", "token", "session-1")
	if err != nil {
		t.Fatalf("QueryVariable 返回 error: %v", err)
	}
	if result["token"] != "abc123" {
		t.Errorf("result[token] = %q, want %q", result["token"], "abc123")
	}
}

func TestVariableManager_QueryVariable_CheckUserAndScopeID(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)

	// 空 userID 时仍执行查询（checkUserAndScopeID 只记日志不返回 error）
	_, err := mgr.QueryVariable(context.Background(), "", "scope-1", "age", "")
	if err != nil {
		t.Fatalf("QueryVariable 空 userID 不应返回 error: %v", err)
	}
}

func TestVariableManager_MakeVariablePairs_UserVar(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)

	key, value := mgr.makeVariablePairs("user-1", false, "scope-1", "age", "", "25", "")
	expectedKey := "user_var/user-1/scope-1/age"
	if key != expectedKey {
		t.Errorf("key = %q, want %q", key, expectedKey)
	}
	if string(value) != "25" {
		t.Errorf("value = %q, want %q", string(value), "25")
	}
}

func TestVariableManager_MakeVariablePairs_SessionVar(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)

	key, value := mgr.makeVariablePairs("user-1", false, "scope-1", "token", "sess-1", "", "abc")
	expectedKey := "session_var/user-1/scope-1/sess-1/token"
	if key != expectedKey {
		t.Errorf("key = %q, want %q", key, expectedKey)
	}
	if string(value) != "abc" {
		t.Errorf("value = %q, want %q", string(value), "abc")
	}
}

func TestVariableManager_MakeVariablePairs_ForDeletion(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)

	key, value := mgr.makeVariablePairs("user-1", true, "scope-1", "age", "", "25", "")
	if key != "user_var/user-1/scope-1/age" {
		t.Errorf("key = %q, want %q", key, "user_var/user-1/scope-1/age")
	}
	if value != nil {
		t.Errorf("forDeletion=true 时 value 应为 nil，得到 %q", string(value))
	}
}

func TestCheckExist(t *testing.T) {
	// 空字典
	if checkExist(nil, "name") {
		t.Error("空字典应返回 false")
	}
	if checkExist(map[string]string{}, "name") {
		t.Error("空字典应返回 false")
	}
	// 不存在的键
	if checkExist(map[string]string{"age": "25"}, "name") {
		t.Error("不存在的键应返回 false")
	}
	// 值为空
	if checkExist(map[string]string{"name": ""}, "name") {
		t.Error("值为空应返回 false")
	}
	// 正常存在
	if !checkExist(map[string]string{"name": "Alice"}, "name") {
		t.Error("存在的键且有值应返回 true")
	}
}

func TestVariableManager_AddMemories_WithCryptoKey(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	mgr, err := NewVariableManager(store, key)
	if err != nil {
		t.Fatalf("NewVariableManager 返回 error: %v", err)
	}

	memories := map[string][]mem_model.MemoryUnit{
		"variable": {
			&mem_model.VariableUnit{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeVariable, MemID: "var-001"},
				VariableName: "age", VariableMem: "25",
			},
		},
	}
	result, err := mgr.AddMemories(context.Background(), "user-1", "scope-1", memories)
	if err != nil {
		t.Fatalf("AddMemories 返回 error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("期望返回 1 个结果，得到 %d", len(result))
	}

	// 验证加密存储
	raw, _ := store.Get(context.Background(), "user_var/user-1/scope-1/age")
	if raw == nil {
		t.Fatal("期望变量已写入")
	}
	// 加密后的值应不等于原文
	if string(raw) == "25" {
		t.Error("加密后值不应等于原文")
	}

	// 通过 QueryVariable 解密后应得到原文
	queryResult, _ := mgr.QueryVariable(context.Background(), "user-1", "scope-1", "age", "")
	if queryResult["age"] != "25" {
		t.Errorf("解密后 value = %q, want %q", queryResult["age"], "25")
	}
}

func TestVariableManager_AddMemories_NonVariableUnitTypeIgnored(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)

	memories := map[string][]mem_model.MemoryUnit{
		"variable": {
			&mem_model.FragmentMemoryUnit{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeVariable, MemID: "var-001"},
				Content: "不是变量",
			},
		},
	}
	result, err := mgr.AddMemories(context.Background(), "user-1", "scope-1", memories)
	if err != nil {
		t.Fatalf("AddMemories 返回 error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("非 VariableUnit 类型应被忽略，得到 %d", len(result))
	}
}

func TestVariableManager_AddMemories_Multiple(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	mgr, _ := NewVariableManager(store, nil)

	memories := map[string][]mem_model.MemoryUnit{
		"variable": {
			&mem_model.VariableUnit{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeVariable, MemID: "var-001"},
				VariableName: "age", VariableMem: "25",
			},
			&mem_model.VariableUnit{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeVariable, MemID: "var-002"},
				VariableName: "name", VariableMem: "Alice",
			},
		},
	}
	result, err := mgr.AddMemories(context.Background(), "user-1", "scope-1", memories)
	if err != nil {
		t.Fatalf("AddMemories 返回 error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("期望返回 2 个结果，得到 %d", len(result))
	}
}
