package kv

import (
	"context"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeKVStore 用于验证 BaseKVStore 接口可被实现。
type fakeKVStore struct{}

func (f *fakeKVStore) Set(_ context.Context, _ string, _ []byte) error {
	return nil
}

func (f *fakeKVStore) ExclusiveSet(_ context.Context, _ string, _ []byte, _ int) (bool, error) {
	return false, nil
}

func (f *fakeKVStore) Get(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

func (f *fakeKVStore) Exists(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (f *fakeKVStore) Delete(_ context.Context, _ string) error {
	return nil
}

func (f *fakeKVStore) GetByPrefix(_ context.Context, _ string) (map[string][]byte, error) {
	return nil, nil
}

func (f *fakeKVStore) DeleteByPrefix(_ context.Context, _ string, _ int) error {
	return nil
}

// CompareAndDelete 桩实现，始终返回未删除。
func (f *fakeKVStore) CompareAndDelete(_ context.Context, _ string, _ []byte) (bool, error) {
	return false, nil
}

func (f *fakeKVStore) MGet(_ context.Context, _ []string) ([][]byte, error) {
	return nil, nil
}

func (f *fakeKVStore) BatchDelete(_ context.Context, _ []string, _ int) (int, error) {
	return 0, nil
}

func (f *fakeKVStore) Pipeline(_ context.Context) KVPipeline {
	return &fakeKVPipeline{}
}

// fakeKVPipeline 用于验证 KVPipeline 接口可被实现。
type fakeKVPipeline struct{}

func (f *fakeKVPipeline) Set(_ context.Context, _ string, _ []byte, _ int) error {
	return nil
}

func (f *fakeKVPipeline) Get(_ context.Context, _ string) error {
	return nil
}

func (f *fakeKVPipeline) Exists(_ context.Context, _ string) error {
	return nil
}

func (f *fakeKVPipeline) Execute(_ context.Context) ([]PipelineResult, error) {
	return nil, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──── 接口编译验证测试 ────

// TestBaseKVStore_接口满足 验证 fakeKVStore 满足 BaseKVStore 接口。
func TestBaseKVStore_接口满足(t *testing.T) {
	var _ BaseKVStore = (*fakeKVStore)(nil)
}

// TestKVPipeline_接口满足 验证 fakeKVPipeline 满足 KVPipeline 接口。
func TestKVPipeline_接口满足(t *testing.T) {
	var _ KVPipeline = (*fakeKVPipeline)(nil)
}

// TestPipelineResult_字段 验证 PipelineResult 结构体字段可赋值。
func TestPipelineResult_字段(t *testing.T) {
	result := PipelineResult{
		Op:     "get",
		Key:    "test_key",
		Value:  []byte("test_value"),
		Exists: false,
		Err:    nil,
	}
	if result.Op != "get" {
		t.Errorf("Op = %q, 期望 %q", result.Op, "get")
	}
	if result.Key != "test_key" {
		t.Errorf("Key = %q, 期望 %q", result.Key, "test_key")
	}
	if string(result.Value) != "test_value" {
		t.Errorf("Value = %q, 期望 %q", string(result.Value), "test_value")
	}
	if result.Exists != false {
		t.Errorf("Exists = %v, 期望 false", result.Exists)
	}
	if result.Err != nil {
		t.Errorf("Err = %v, 期望 nil", result.Err)
	}
}

// TestPipelineResult_Set操作 验证 Set 操作的结果结构。
func TestPipelineResult_Set操作(t *testing.T) {
	result := PipelineResult{
		Op:  "set",
		Key: "mykey",
		Err: nil,
	}
	if result.Op != "set" {
		t.Errorf("Op = %q, 期望 %q", result.Op, "set")
	}
}

// TestPipelineResult_Exists操作 验证 Exists 操作的结果结构。
func TestPipelineResult_Exists操作(t *testing.T) {
	result := PipelineResult{
		Op:     "exists",
		Key:    "mykey",
		Exists: true,
		Err:    nil,
	}
	if !result.Exists {
		t.Error("Exists 应为 true")
	}
}

// TestPipelineResult_错误结果 验证带错误的 PipelineResult。
func TestPipelineResult_错误结果(t *testing.T) {
	result := PipelineResult{
		Op:  "get",
		Key: "missing_key",
		Err: context.Canceled,
	}
	if result.Err == nil {
		t.Error("Err 不应为 nil")
	}
}

// TestBaseKVStore_CompareAndDelete_契约 验证各后端的原子比较删除语义一致。
// 覆盖：值匹配删除 / 值不匹配保留 / key 不存在返回 false。
func TestBaseKVStore_CompareAndDelete_契约(t *testing.T) {
	factories := map[string]func(t *testing.T) BaseKVStore{
		"in_memory": func(_ *testing.T) BaseKVStore { return NewInMemoryKVStore() },
		"file": func(t *testing.T) BaseKVStore {
			s, err := NewFileKVStore(filepath.Join(t.TempDir(), "kv.db"))
			if err != nil {
				t.Fatalf("创建 FileKVStore 失败: %v", err)
			}
			t.Cleanup(func() { _ = s.Close() })
			return s
		},
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := factory(t)
			if err := s.Set(ctx, "k", []byte("v1")); err != nil {
				t.Fatalf("Set 失败: %v", err)
			}

			// 值不匹配：不删除
			ok, err := s.CompareAndDelete(ctx, "k", []byte("wrong"))
			if err != nil {
				t.Fatalf("CompareAndDelete 失败: %v", err)
			}
			if ok {
				t.Fatal("值不匹配时应返回 false")
			}
			got, err := s.Get(ctx, "k")
			if err != nil {
				t.Fatalf("Get 失败: %v", err)
			}
			if string(got) != "v1" {
				t.Fatalf("值不匹配时不应删除，got %q", string(got))
			}

			// 值匹配：删除
			ok, err = s.CompareAndDelete(ctx, "k", []byte("v1"))
			if err != nil {
				t.Fatalf("CompareAndDelete 失败: %v", err)
			}
			if !ok {
				t.Fatal("值匹配时应返回 true")
			}
			got, err = s.Get(ctx, "k")
			if err != nil {
				t.Fatalf("Get 失败: %v", err)
			}
			if got != nil {
				t.Fatalf("删除后应取不到值，got %q", string(got))
			}

			// key 不存在：返回 false 且不报错
			ok, err = s.CompareAndDelete(ctx, "missing", []byte("v1"))
			if err != nil {
				t.Fatalf("CompareAndDelete 失败: %v", err)
			}
			if ok {
				t.Fatal("key 不存在时应返回 false")
			}
		})
	}
}
