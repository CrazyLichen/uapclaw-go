package resources_manager

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestModelMgr_添加获取正常 测试 AddModel → GetModel 正常流程
func TestModelMgr_添加获取正常(t *testing.T) {
	mgr := NewModelMgr()

	mockClient := &stubBaseModelClient{}
	provider := func(_ context.Context, _ string) (model_clients.BaseModelClient, error) {
		return mockClient, nil
	}

	err := mgr.AddModel("model-1", provider)
	if err != nil {
		t.Fatalf("AddModel 失败: %v", err)
	}

	model, err := mgr.GetModel(context.Background(), "model-1", nil)
	if err != nil {
		t.Fatalf("GetModel 失败: %v", err)
	}
	if model == nil {
		t.Error("GetModel 返回 nil，期望非 nil")
	}
}

// TestModelMgr_Session为nil不装饰 测试 session=nil 时不进行追踪装饰
func TestModelMgr_Session为nil不装饰(t *testing.T) {
	mgr := NewModelMgr()

	mockClient := &stubBaseModelClient{}
	provider := func(_ context.Context, _ string) (model_clients.BaseModelClient, error) {
		return mockClient, nil
	}

	err := mgr.AddModel("model-1", provider)
	if err != nil {
		t.Fatalf("AddModel 失败: %v", err)
	}

	model, err := mgr.GetModel(context.Background(), "model-1", nil)
	if err != nil {
		t.Fatalf("GetModel 失败: %v", err)
	}
	// session=nil 时返回的应该是原始客户端，不是 TracedModelClient
	// 通过类型断言验证不是 TracedModelClient
	if _, ok := model.(*stubBaseModelClient); !ok {
		t.Error("session=nil 时应返回原始客户端，而非装饰后的客户端")
	}
}

// TestModelMgr_获取不存在返回错误 测试不存在的 modelID 返回错误
func TestModelMgr_获取不存在返回错误(t *testing.T) {
	mgr := NewModelMgr()

	_, err := mgr.GetModel(context.Background(), "not-exist", nil)
	if err == nil {
		t.Error("获取不存在的模型应返回错误")
	}
}

// TestModelMgr_重复注册报错 测试同 ID 二次注册报错
func TestModelMgr_重复注册报错(t *testing.T) {
	mgr := NewModelMgr()

	provider := func(_ context.Context, _ string) (model_clients.BaseModelClient, error) {
		return &stubBaseModelClient{}, nil
	}

	err := mgr.AddModel("model-1", provider)
	if err != nil {
		t.Fatalf("首次 AddModel 失败: %v", err)
	}

	err = mgr.AddModel("model-1", provider)
	if err == nil {
		t.Error("重复注册应返回错误")
	}
}

// TestModelMgr_Provider返回错误 测试 provider 执行时返回错误
func TestModelMgr_Provider返回错误(t *testing.T) {
	mgr := NewModelMgr()

	provider := func(_ context.Context, _ string) (model_clients.BaseModelClient, error) {
		return nil, errors.New("internal error")
	}

	err := mgr.AddModel("err-model", provider)
	if err != nil {
		t.Fatalf("AddModel 失败: %v", err)
	}

	_, err = mgr.GetModel(context.Background(), "err-model", nil)
	if err == nil {
		t.Error("provider 返回错误时 GetModel 应传播错误")
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────
