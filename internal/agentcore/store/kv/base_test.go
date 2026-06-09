package kv

import (
	"context"
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
