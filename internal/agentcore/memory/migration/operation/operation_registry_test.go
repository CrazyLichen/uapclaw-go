package operation

import (
	"testing"
)

// TestNewOperationRegistry 测试创建空注册表
func TestNewOperationRegistry(t *testing.T) {
	r := NewOperationRegistry()
	if r == nil {
		t.Fatal("NewOperationRegistry() 返回 nil")
	}
	if len(r.GetAllEntities()) != 0 {
		t.Error("新注册表应为空")
	}
}

// TestOperationRegistry_Register 测试注册操作
func TestOperationRegistry_Register(t *testing.T) {
	r := NewOperationRegistry()
	op := &BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}}
	err := r.Register("user_messages", op)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if r.GetCurrentVersion("user_messages") != 1 {
		t.Errorf("GetCurrentVersion() = %d, want 1", r.GetCurrentVersion("user_messages"))
	}
}

// TestOperationRegistry_Register_版本单调递增 测试版本必须单调递增
func TestOperationRegistry_Register_版本单调递增(t *testing.T) {
	r := NewOperationRegistry()
	_ = r.Register("test", &BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}})
	_ = r.Register("test", &BaseOperation{Metadata: OperationMetadata{SchemaVersion: 2}})
	err := r.Register("test", &BaseOperation{Metadata: OperationMetadata{SchemaVersion: 2}})
	if err == nil {
		t.Error("重复版本号应返回错误")
	}
	err = r.Register("test", &BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}})
	if err == nil {
		t.Error("低版本号应返回错误")
	}
}

// TestOperationRegistry_GetOperations 测试按范围获取操作
func TestOperationRegistry_GetOperations(t *testing.T) {
	r := NewOperationRegistry()
	_ = r.Register("test", &BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}})
	_ = r.Register("test", &BaseOperation{Metadata: OperationMetadata{SchemaVersion: 2}})
	_ = r.Register("test", &BaseOperation{Metadata: OperationMetadata{SchemaVersion: 3}})
	ops := r.GetOperations("test", 2, 3)
	if len(ops) != 2 {
		t.Errorf("GetOperations(2,3) 返回 %d 项, want 2", len(ops))
	}
}

// TestOperationRegistry_GetOperations_from大于to 测试范围无效返回空
func TestOperationRegistry_GetOperations_from大于to(t *testing.T) {
	r := NewOperationRegistry()
	ops := r.GetOperations("test", 5, 1)
	if len(ops) != 0 {
		t.Errorf("from > to 应返回空列表, got %d 项", len(ops))
	}
}

// TestOperationRegistry_GetCurrentVersion 测试获取当前版本
func TestOperationRegistry_GetCurrentVersion(t *testing.T) {
	r := NewOperationRegistry()
	if r.GetCurrentVersion("nonexist") != 0 {
		t.Error("未注册实体应返回 0")
	}
	_ = r.Register("test", &BaseOperation{Metadata: OperationMetadata{SchemaVersion: 5}})
	if r.GetCurrentVersion("test") != 5 {
		t.Errorf("GetCurrentVersion() = %d, want 5", r.GetCurrentVersion("test"))
	}
}

// TestOperationRegistry_GetAllEntities 测试获取所有实体键
func TestOperationRegistry_GetAllEntities(t *testing.T) {
	r := NewOperationRegistry()
	_ = r.Register("a", &BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}})
	_ = r.Register("b", &BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}})
	entities := r.GetAllEntities()
	if len(entities) != 2 {
		t.Errorf("GetAllEntities() 返回 %d 项, want 2", len(entities))
	}
}

// TestOperationRegistry_Clear 测试清空
func TestOperationRegistry_Clear(t *testing.T) {
	r := NewOperationRegistry()
	_ = r.Register("test", &BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}})
	r.Clear()
	if r.GetCurrentVersion("test") != 0 {
		t.Error("Clear() 后应返回 0")
	}
}

// TestOperationRegistry_SetOperations 测试设置操作映射
func TestOperationRegistry_SetOperations(t *testing.T) {
	r := NewOperationRegistry()
	r.SetOperations(map[string][]Operation{
		"test": {&BaseOperation{Metadata: OperationMetadata{SchemaVersion: 3}}},
	})
	if r.GetCurrentVersion("test") != 3 {
		t.Errorf("SetOperations 后 GetCurrentVersion() = %d, want 3", r.GetCurrentVersion("test"))
	}
}

// TestOperationRegistry_GetAllOperations 测试获取浅拷贝
func TestOperationRegistry_GetAllOperations(t *testing.T) {
	r := NewOperationRegistry()
	_ = r.Register("test", &BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}})
	all := r.GetAllOperations()
	if len(all) != 1 {
		t.Errorf("GetAllOperations() 返回 %d 项, want 1", len(all))
	}
	// 修改副本不影响原注册表
	all["test"] = nil
	if r.GetCurrentVersion("test") != 1 {
		t.Error("修改 GetAllOperations 副本不应影响原注册表")
	}
}
