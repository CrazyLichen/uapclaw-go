package sdk

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
)

// TestCryptoUtilityStub_GetCrypto 测试返回 nil
func TestCryptoUtilityStub_GetCrypto(t *testing.T) {
	stub := &CryptoUtilityStub{}
	result := stub.GetCrypto()
	if result != nil {
		t.Errorf("GetCrypto() = %v, want nil", result)
	}
}

// TestCryptoUtilityStub_Initialize 测试空实现
func TestCryptoUtilityStub_Initialize(t *testing.T) {
	stub := &CryptoUtilityStub{}
	err := stub.Initialize(context.Background(), &extensions.ExtensionConfig{})
	if err != nil {
		t.Errorf("Initialize() error: %v", err)
	}
}

// TestCryptoUtilityStub_Shutdown 测试空实现
func TestCryptoUtilityStub_Shutdown(t *testing.T) {
	stub := &CryptoUtilityStub{}
	err := stub.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Shutdown() error: %v", err)
	}
}

// TestCryptoUtility_接口契约 测试 CryptoUtility 接口契约
func TestCryptoUtility_接口契约(t *testing.T) {
	var _ CryptoUtility = (*CryptoUtilityStub)(nil)
}
