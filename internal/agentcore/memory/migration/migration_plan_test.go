package migration

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
)

// Test全局Registry实例 验证 5 个全局 Registry 实例已初始化
func Test全局Registry实例(t *testing.T) {
	if SQLRegistry == nil {
		t.Error("SQLRegistry 不应为 nil")
	}
	if VectorRegistry == nil {
		t.Error("VectorRegistry 不应为 nil")
	}
	if KVRegistry == nil {
		t.Error("KVRegistry 不应为 nil")
	}
	if MessageRegistry == nil {
		t.Error("MessageRegistry 不应为 nil")
	}
	if IndexRegistry == nil {
		t.Error("IndexRegistry 不应为 nil")
	}
}

// Test全局Registry_独立实例 验证各 Registry 是独立实例
func Test全局Registry_独立实例(t *testing.T) {
	registries := []*operation.OperationRegistry{SQLRegistry, VectorRegistry, KVRegistry, MessageRegistry, IndexRegistry}
	for i := 0; i < len(registries); i++ {
		for j := i + 1; j < len(registries); j++ {
			if registries[i] == registries[j] {
				t.Errorf("Registry[%d] 和 Registry[%d] 是同一实例", i, j)
			}
		}
	}
}
