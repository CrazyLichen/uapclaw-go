package pregel

import (
	"context"
	"testing"
)

// TestNodeFunc_接收NodeInput 节点函数按 NodeFunc 签名被调用时，ctx 与 NodeInput 原样透传，State 为 nil。
// Python: task.py:137-154（按形参名嗅探后拼 kwargs 调用 func，state 恒 None）
func TestNodeFunc_接收NodeInput(t *testing.T) {
	type ctxKey string
	key := ctxKey("trace")
	ctx := context.WithValue(context.Background(), key, "abc")

	var gotCtx context.Context
	var gotIn NodeInput
	fn := NodeFunc(func(c context.Context, in NodeInput) error {
		gotCtx = c
		gotIn = in
		return nil
	})

	in := NodeInput{Config: InnerPregelConfig{PregelConfig: PregelConfig{Ns: "wf", SessionID: "s1"}}}
	if err := fn(ctx, in); err != nil {
		t.Fatalf("节点函数返回错误：%v", err)
	}
	if gotCtx.Value(key) != "abc" {
		t.Errorf("ctx 未透传：期望 %q", "abc")
	}
	if gotIn.Config.Ns != "wf" || gotIn.Config.SessionID != "s1" {
		t.Errorf("Config 未透传：Ns=%q SessionID=%q", gotIn.Config.Ns, gotIn.Config.SessionID)
	}
	if gotIn.State != nil {
		t.Errorf("State 应为 nil，实际 %v", gotIn.State)
	}
}
