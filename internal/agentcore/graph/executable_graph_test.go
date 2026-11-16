package graph

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// ──────────────────────────── 测试工具 ────────────────────────────

// fakeGraph 满足 ExecutableGraph 的最小 fake：仅实现 Invoke_ 与四钩子（继承基座）
type fakeGraph struct {
	BaseExecutable[map[string]any, map[string]any]
	invokeArgs map[string]any
	config     any
}

func (f *fakeGraph) Invoke_(inputs map[string]any, _ session.BaseSession, config any) (map[string]any, error) {
	f.invokeArgs = inputs
	f.config = config
	return map[string]any{"ok": true}, nil
}

// Interrupt 满足 ExecutableGraph 接口（本测试不消费，默认 pass 行为经 GraphInterrupt 承载）
func (f *fakeGraph) Interrupt(_ map[string]any) error { return nil }

// 编译期断言：fakeGraph 满足 ExecutableGraph 接口
var _ ExecutableGraph[map[string]any, map[string]any] = (*fakeGraph)(nil)

// ──────────────────────────── 信封常量 ────────────────────────────

// Test信封常量 断言键名对齐 Python INPUTS_KEY/CONFIG_KEY（constant.py:16-17）
func Test信封常量(t *testing.T) {
	if InputsKey != "inputs" {
		t.Errorf("InputsKey 应为 inputs, got %q", InputsKey)
	}
	if ConfigKey != "config" {
		t.Errorf("ConfigKey 应为 config, got %q", ConfigKey)
	}
}

// ──────────────────────────── Invoke 信封拆分 ────────────────────────────

// TestExecutableGraph_Invoke拆信封 断言 Invoke 拆出 inputs/config 后转发 Invoke_
// （Python base.py:13-14：inputs.get(INPUTS_KEY), inputs.get(CONFIG_KEY)）
func TestExecutableGraph_Invoke拆信封(t *testing.T) {
	g := &fakeGraph{}
	cfg := map[string]any{"recursion_limit": 25}
	got, err := GraphInvoke[map[string]any, map[string]any](g,
		map[string]any{InputsKey: map[string]any{"q": "hi"}, ConfigKey: cfg}, nil)
	if err != nil {
		t.Fatalf("Invoke 不应报错: %v", err)
	}
	if got["ok"] != true {
		t.Errorf("应转发 Invoke_ 返回值, got %v", got)
	}
	if g.invokeArgs["q"] != "hi" {
		t.Errorf("Invoke_ 应收到拆出的业务输入, got %v", g.invokeArgs)
	}
	if g.config == nil || g.config.(map[string]any)["recursion_limit"] != 25 {
		t.Errorf("Invoke_ 应收到拆出的 config, got %v", g.config)
	}
}

// TestExecutableGraph_Invoke_config缺失宽容 断言 config 缺失传 nil 不报错
// （Python .get(CONFIG_KEY) 无默认值 → None；业务输入缺失硬错是消费方语义，留 8.20）
func TestExecutableGraph_Invoke_config缺失宽容(t *testing.T) {
	g := &fakeGraph{}
	_, err := GraphInvoke[map[string]any, map[string]any](g,
		map[string]any{InputsKey: map[string]any{}}, nil)
	if err != nil {
		t.Fatalf("config 缺失应宽容: %v", err)
	}
	if g.config != nil {
		t.Errorf("config 缺失时应传 nil, got %v", g.config)
	}
}

// TestExecutableGraph_Invoke_inputs缺失宽容 断言 inputs 键缺失传 nil（.get 宽容语义）
func TestExecutableGraph_Invoke_inputs缺失宽容(t *testing.T) {
	g := &fakeGraph{}
	_, err := GraphInvoke[map[string]any, map[string]any](g, map[string]any{}, nil)
	if err != nil {
		t.Fatalf("inputs 键缺失应宽容: %v", err)
	}
	if g.invokeArgs != nil {
		t.Errorf("inputs 键缺失时应传 nil, got %v", g.invokeArgs)
	}
}

// TestExecutableGraph_Invoke_信封nil宽容 断言整个信封为 nil 时不 panic
func TestExecutableGraph_Invoke_信封nil宽容(t *testing.T) {
	g := &fakeGraph{}
	_, err := GraphInvoke[map[string]any, map[string]any](g, nil, nil)
	if err != nil {
		t.Fatalf("信封 nil 应宽容: %v", err)
	}
}

// ──────────────────────────── 默认 pass 语义（spec 偏差 D5） ────────────────────────────

// TestExecutableGraph_默认pass语义 断言 Stream/Collect/Transform/Interrupt 默认空实现
// （Python base.py:16-26 为 pass，与父类抛 NotImplementedError 不同；流式返回已关闭空 channel）
func TestExecutableGraph_默认pass语义(t *testing.T) {
	g := &fakeGraph{}

	ch, err := GraphStream[map[string]any, map[string]any](g, map[string]any{}, nil)
	if err != nil {
		t.Fatalf("Stream 默认不应报错: %v", err)
	}
	if _, ok := <-ch; ok {
		t.Error("Stream 默认应返回已关闭的空 channel")
	}

	if _, err := GraphCollect[map[string]any, map[string]any](g, nil, nil); err != nil {
		t.Errorf("Collect 默认不应报错: %v", err)
	}

	ch2, err := GraphTransform[map[string]any, map[string]any](g, nil, nil)
	if err != nil {
		t.Fatalf("Transform 默认不应报错: %v", err)
	}
	if _, ok := <-ch2; ok {
		t.Error("Transform 默认应返回已关闭的空 channel")
	}

	if err := GraphInterrupt[map[string]any, map[string]any](g, nil); err != nil {
		t.Errorf("Interrupt 默认不应报错: %v", err)
	}
	_ = context.Background
}
