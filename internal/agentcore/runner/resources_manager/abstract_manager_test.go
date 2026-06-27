package resources_manager

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestAbstractManager_注册获取 测试 register → get 返回正确实例
func TestAbstractManager_注册获取(t *testing.T) {
	mgr := NewAbstractManager[int]()
	err := mgr.registerProvider("res1", func(_ context.Context) (int, error) {
		return 42, nil
	})
	if err != nil {
		t.Fatalf("registerProvider 失败: %v", err)
	}

	val, err := mgr.getResource(context.Background(), "res1")
	if err != nil {
		t.Fatalf("getResource 失败: %v", err)
	}
	if val != 42 {
		t.Errorf("getResource = %d, want 42", val)
	}
}

// TestAbstractManager_重复注册返回错误 测试同 ID 二次注册报错
func TestAbstractManager_重复注册返回错误(t *testing.T) {
	mgr := NewAbstractManager[int]()
	_ = mgr.registerProvider("res1", func(_ context.Context) (int, error) { return 1, nil })

	err := mgr.registerProvider("res1", func(_ context.Context) (int, error) { return 2, nil })
	if err == nil {
		t.Error("重复注册应返回错误")
	}
}

// TestAbstractManager_获取不存在返回错误 测试未注册 ID 报错
func TestAbstractManager_获取不存在返回错误(t *testing.T) {
	mgr := NewAbstractManager[int]()
	_, err := mgr.getResource(context.Background(), "notexist")
	if err == nil {
		t.Error("获取不存在的资源应返回错误")
	}
}

// TestAbstractManager_注销 测试 unregister 后 get 报错
func TestAbstractManager_注销(t *testing.T) {
	mgr := NewAbstractManager[int]()
	_ = mgr.registerProvider("res1", func(_ context.Context) (int, error) { return 42, nil })

	provider, err := mgr.unregisterProvider("res1")
	if err != nil {
		t.Fatalf("unregisterProvider 失败: %v", err)
	}
	if provider == nil {
		t.Error("unregisterProvider 应返回被注销的 provider")
	}

	_, err = mgr.getResource(context.Background(), "res1")
	if err == nil {
		t.Error("注销后获取应返回错误")
	}
}

// TestAbstractManager_注销不存在 测试不存在 ID 注销报错
func TestAbstractManager_注销不存在(t *testing.T) {
	mgr := NewAbstractManager[int]()
	_, err := mgr.unregisterProvider("notexist")
	if err == nil {
		t.Error("注销不存在的资源应返回错误")
	}
}

// TestAbstractManager_Provider返回错误 测试 provider 函数返回 error 的情况
func TestAbstractManager_Provider返回错误(t *testing.T) {
	mgr := NewAbstractManager[int]()
	_ = mgr.registerProvider("err-res", func(_ context.Context) (int, error) {
		return 0, fmt.Errorf("internal error")
	})

	_, err := mgr.getResource(context.Background(), "err-res")
	if err == nil {
		t.Error("provider 返回错误时 getResource 应传播错误")
	}
}

// TestAbstractManager_并发注册获取 测试多 goroutine 并发操作
func TestAbstractManager_并发注册获取(t *testing.T) {
	mgr := NewAbstractManager[int]()
	var wg sync.WaitGroup
	const n = 50

	// 并发注册
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = mgr.registerProvider(fmt.Sprintf("res-%d", idx), func(_ context.Context) (int, error) {
				return idx, nil
			})
		}(i)
	}
	wg.Wait()

	// 并发获取
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			val, err := mgr.getResource(context.Background(), fmt.Sprintf("res-%d", idx))
			if err != nil {
				t.Errorf("getResource(res-%d) 失败: %v", idx, err)
			}
			if val != idx {
				t.Errorf("getResource(res-%d) = %d, want %d", idx, val, idx)
			}
		}(i)
	}
	wg.Wait()
}
