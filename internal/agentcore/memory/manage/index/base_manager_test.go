//go:build test

package index

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeMemoryIndex 用于测试的 BaseMemoryIndex 假实现
type fakeMemoryIndex struct {
	index.MemoryIndexBase
	memories map[string]*index.MemoryDoc // key = userID/scopeID/memID
}

func newFakeMemoryIndex() *fakeMemoryIndex {
	return &fakeMemoryIndex{
		memories: make(map[string]*index.MemoryDoc),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func (f *fakeMemoryIndex) key(userID, scopeID, memID string) string {
	return userID + "/" + scopeID + "/" + memID
}

func (f *fakeMemoryIndex) SetStorageCodec(_ index.StorageCodec) {}

func (f *fakeMemoryIndex) AddMemories(_ context.Context, userID string, scopeID string, memories []*index.MemoryDoc) error {
	for _, doc := range memories {
		f.memories[f.key(userID, scopeID, doc.ID)] = doc
	}
	return nil
}

func (f *fakeMemoryIndex) UpdateMemories(_ context.Context, userID string, scopeID string, memories []*index.MemoryDoc) error {
	for _, doc := range memories {
		f.memories[f.key(userID, scopeID, doc.ID)] = doc
	}
	return nil
}

func (f *fakeMemoryIndex) DeleteMemories(_ context.Context, userID string, scopeID string, ids []string) error {
	for _, id := range ids {
		delete(f.memories, f.key(userID, scopeID, id))
	}
	return nil
}

func (f *fakeMemoryIndex) DeleteByUser(_ context.Context, userID string) error {
	for k := range f.memories {
		if len(k) > len(userID) && k[:len(userID)] == userID {
			delete(f.memories, k)
		}
	}
	return nil
}

func (f *fakeMemoryIndex) DeleteByScope(_ context.Context, scopeID string) error {
	for k := range f.memories {
		if len(k) > len(scopeID) && k[len(k)-len(scopeID):] == scopeID {
			delete(f.memories, k)
		}
	}
	return nil
}

func (f *fakeMemoryIndex) DeleteByUserAndScope(_ context.Context, userID string, scopeID string) error {
	for k := range f.memories {
		prefix := userID + "/" + scopeID + "/"
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(f.memories, k)
		}
	}
	return nil
}

func (f *fakeMemoryIndex) Search(_ context.Context, userID string, scopeID string, query string, memTypes []string, topK int) ([]*index.MemorySearchResult, error) {
	var results []*index.MemorySearchResult
	prefix := userID + "/" + scopeID + "/"
	for k, doc := range f.memories {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			results = append(results, &index.MemorySearchResult{Doc: doc, Score: 0.8})
		}
	}
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

func (f *fakeMemoryIndex) GetByID(_ context.Context, userID string, scopeID string, memID string) (*index.MemoryDoc, error) {
	doc, ok := f.memories[f.key(userID, scopeID, memID)]
	if !ok {
		return nil, nil
	}
	return doc, nil
}

func (f *fakeMemoryIndex) ListMemories(_ context.Context, userID string, scopeID string, offset int, limit int, memTypes []string) ([]*index.MemoryDoc, error) {
	var results []*index.MemoryDoc
	prefix := userID + "/" + scopeID + "/"
	for k, doc := range f.memories {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			results = append(results, doc)
		}
	}
	if offset < len(results) {
		results = results[offset:]
	}
	if limit < len(results) {
		results = results[:limit]
	}
	return results, nil
}

func TestValidateParams_UserIDEmpty(t *testing.T) {
	base := &memoryManagerBase{
		memoryIndex: newFakeMemoryIndex(),
		memType:     "fragment",
	}
	err := base.validateParams("", "scope-1", exception.StatusMemoryAddMemoryExecutionError, "fragment")
	if err == nil {
		t.Fatal("期望返回 error，但得到 nil")
	}
	var baseErr *exception.BaseError
	if !errors.As(err, &baseErr) {
		t.Fatalf("期望 *BaseError，得到 %T", err)
	}
	if baseErr.Status() != exception.StatusMemoryAddMemoryExecutionError {
		t.Errorf("Status = %v, want %v", baseErr.Status(), exception.StatusMemoryAddMemoryExecutionError)
	}
}

func TestValidateParams_ScopeIDEmpty(t *testing.T) {
	base := &memoryManagerBase{
		memoryIndex: newFakeMemoryIndex(),
		memType:     "fragment",
	}
	err := base.validateParams("user-1", "", exception.StatusMemoryAddMemoryExecutionError, "fragment")
	if err == nil {
		t.Fatal("期望返回 error，但得到 nil")
	}
}

func TestValidateParams_MemoryIndexNil(t *testing.T) {
	base := &memoryManagerBase{
		memoryIndex: nil,
		memType:     "fragment",
	}
	err := base.validateParams("user-1", "scope-1", exception.StatusMemoryAddMemoryExecutionError, "fragment")
	if err == nil {
		t.Fatal("期望返回 error，但得到 nil")
	}
}

func TestValidateParams_Success(t *testing.T) {
	base := &memoryManagerBase{
		memoryIndex: newFakeMemoryIndex(),
		memType:     "fragment",
	}
	err := base.validateParams("user-1", "scope-1", exception.StatusMemoryAddMemoryExecutionError, "fragment")
	if err != nil {
		t.Fatalf("不期望返回 error，但得到 %v", err)
	}
}

func TestWrapException_BaseErrorPassthrough(t *testing.T) {
	base := &memoryManagerBase{memType: "fragment"}
	originalErr := exception.BuildError(exception.StatusMemoryAddMemoryExecutionError,
		exception.WithParam("memory_type", "fragment"),
		exception.WithMsg("original error"),
	)
	err := base.wrapException(originalErr, exception.StatusMemoryUpdateMemoryExecutionError, "fragment")
	var baseErr *exception.BaseError
	if !errors.As(err, &baseErr) {
		t.Fatalf("期望 *BaseError，得到 %T", err)
	}
	// BaseError 应原样透传，status 不变
	if baseErr.Status() != exception.StatusMemoryAddMemoryExecutionError {
		t.Errorf("Status = %v, want %v (应透传原始 BaseError)", baseErr.Status(), exception.StatusMemoryAddMemoryExecutionError)
	}
}

func TestWrapException_OtherErrorWrapped(t *testing.T) {
	base := &memoryManagerBase{memType: "fragment"}
	originalErr := exception.NewBaseError(exception.StatusMemoryAddMemoryExecutionError,
		exception.WithMsg("some error"),
	)
	err := base.wrapException(originalErr, exception.StatusMemoryUpdateMemoryExecutionError, "fragment")
	var baseErr *exception.BaseError
	if !errors.As(err, &baseErr) {
		t.Fatalf("期望 *BaseError，得到 %T", err)
	}
}

func TestEncryptDecryptMemoryIfNeeded(t *testing.T) {
	// 空 key → passthrough
	result := encryptMemoryIfNeeded(nil, "hello")
	if result != "hello" {
		t.Errorf("空 key 时应返回原文，得到 %q", result)
	}
	result = decryptMemoryIfNeeded(nil, "hello")
	if result != "hello" {
		t.Errorf("空 key 时应返回原文，得到 %q", result)
	}
	// 空字符串 → passthrough
	result = encryptMemoryIfNeeded([]byte{1, 2, 3}, "")
	if result != "" {
		t.Errorf("空字符串时应返回原文，得到 %q", result)
	}
}

func TestFragmentMemoryTypes(t *testing.T) {
	expected := []string{"user_profile", "semantic_memory", "episodic_memory"}
	for i, typ := range FragmentMemoryTypes {
		if typ != expected[i] {
			t.Errorf("FragmentMemoryTypes[%d] = %q, want %q", i, typ, expected[i])
		}
	}
	if mem_model.MemoryTypeUserProfile.String() != "user_profile" {
		t.Errorf("MemoryTypeUserProfile.String() = %q, want %q", mem_model.MemoryTypeUserProfile.String(), "user_profile")
	}
}
