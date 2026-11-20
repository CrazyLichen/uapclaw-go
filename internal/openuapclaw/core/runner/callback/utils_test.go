package callback

import (
	"context"
	"testing"
)

func TestGetCallbackFramework_非nil(t *testing.T) {
	fw := GetCallbackFramework()
	if fw == nil {
		t.Errorf("GetCallbackFramework() = nil, want non-nil")
	}
}

func TestGetCallbackFramework_单例(t *testing.T) {
	fw1 := GetCallbackFramework()
	fw2 := GetCallbackFramework()
	if fw1 != fw2 {
		t.Errorf("GetCallbackFramework() 返回不同实例")
	}
}

func TestTrigger_便捷函数(t *testing.T) {
	var called bool
	fw := GetCallbackFramework()
	fw.OnCustom("test_trigger_util", func(ctx context.Context, data map[string]any) any {
		called = true
		return nil
	})
	Trigger(context.Background(), "test_trigger_util", nil)
	if !called {
		t.Errorf("Trigger 未触发回调")
	}
	fw.OffAllCustom("test_trigger_util")
}
