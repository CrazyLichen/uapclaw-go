package utils

import "testing"

// TestEndFrame_字段 对齐 Python session/utils.py:391 @dataclass EndFrame{source: str}
func TestEndFrame_字段(t *testing.T) {
	f := &EndFrame{Source: "nodeA"}
	if f.Source != "nodeA" {
		t.Fatal("EndFrame.Source 应保留 producer id")
	}
}

// TestIsEndFrame_判定 PathQueue 中区分普通帧与结束帧
func TestIsEndFrame_判定(t *testing.T) {
	if IsEndFrame("normal") {
		t.Fatal("普通值不应判定为 EndFrame")
	}
	if !IsEndFrame(&EndFrame{Source: "a"}) {
		t.Fatal("*EndFrame 应判定为结束帧")
	}
}

// TestMessageTransformer_签名 D4：修正 Python 注解错误，真实语义为消息变换
func TestMessageTransformer_签名(t *testing.T) {
	var tr MessageTransformer = func(m map[string]any) map[string]any {
		return map[string]any{"k": m["v"]}
	}
	got := tr(map[string]any{"v": 1})
	if got["k"] != 1 {
		t.Fatal("MessageTransformer 应可调用变换消息")
	}
}
