package rb

import (
	"context"
	"fmt"
	"testing"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeLLMService 用于测试的模拟 LLM 服务
type fakeLLMService struct {
	// response 模拟返回的响应
	response string
	// err 模拟返回的错误
	err error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// Generate 实现 LLMService 接口
func (f *fakeLLMService) Generate(_ context.Context, _ string, _ ...cecontext.GenerateOption) (string, error) {
	return f.response, f.err
}

// TestLabelDeterminator_判断成功 验证 LLM 返回 Status: success 时判定为成功
func TestLabelDeterminator_判断成功(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{
		response: "Thoughts: The agent completed the task.\nStatus: success",
	})

	d := NewLabelDeterminator(sc)
	got, err := d.DetermineLabel(context.Background(), "test query", "test trajectory")
	if err != nil {
		t.Fatalf("DetermineLabel 返回错误: %v", err)
	}
	if !got {
		t.Error("期望 true，实际 false")
	}
}

// TestLabelDeterminator_判断失败 验证 LLM 返回 Status: failure 时判定为失败
func TestLabelDeterminator_判断失败(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{
		response: "Thoughts: The agent failed.\nStatus: failure",
	})

	d := NewLabelDeterminator(sc)
	got, err := d.DetermineLabel(context.Background(), "test query", "test trajectory")
	if err != nil {
		t.Fatalf("DetermineLabel 返回错误: %v", err)
	}
	if got {
		t.Error("期望 false，实际 true")
	}
}

// TestLabelDeterminator_正则不匹配回退 验证正则不匹配时回退检查 success 关键字
func TestLabelDeterminator_正则不匹配回退(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{
		response: "The task was a great success overall",
	})

	d := NewLabelDeterminator(sc)
	got, err := d.DetermineLabel(context.Background(), "test query", "test trajectory")
	if err != nil {
		t.Fatalf("DetermineLabel 返回错误: %v", err)
	}
	if !got {
		t.Error("回退匹配应返回 true")
	}
}

// TestLabelDeterminator_LLM调用失败 验证 LLM 返回错误时传播错误
func TestLabelDeterminator_LLM调用失败(t *testing.T) {
	sc := cecontext.NewServiceContext()
	sc.RegisterService("llm", &fakeLLMService{
		err: fmt.Errorf("LLM 不可用"),
	})

	d := NewLabelDeterminator(sc)
	_, err := d.DetermineLabel(context.Background(), "test query", "test trajectory")
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestLabelDeterminator_LLM未注册 验证 LLM 服务未注册时返回错误
func TestLabelDeterminator_LLM未注册(t *testing.T) {
	sc := cecontext.NewServiceContext()
	d := NewLabelDeterminator(sc)
	_, err := d.DetermineLabel(context.Background(), "test query", "test trajectory")
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}
