package executable

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session"
)

// ──────────────────────────── 测试工具 ────────────────────────────

// fakeSession 测试用空会话
type fakeSession struct {
	session.BaseSession
}

// ──────────────────────────── Executable 接口 ────────────────────────────

// TestBaseExecutable_四钩子默认值 断言四个策略钩子的默认值
// （重点：PostCommit 默认 true，与其他钩子不对称，spec §3.3）
func TestBaseExecutable_四钩子默认值(t *testing.T) {
	var base BaseExecutable[map[string]any, map[string]any]
	if base.SkipTrace() != false {
		t.Error("SkipTrace 默认应为 false")
	}
	if base.GraphInvoker() != false {
		t.Error("GraphInvoker 默认应为 false")
	}
	if base.PostCommit() != true {
		t.Error("PostCommit 默认应为 true（对齐 executable.py:56-57 默认值不对称）")
	}
	if base.ComponentType() != "" {
		t.Error("ComponentType 默认应为空串")
	}
}

// TestBaseExecutable_OnInvoke默认抛错 断言四个 On* 默认返回 NotImplementedError 复刻
func TestBaseExecutable_OnInvoke默认抛错(t *testing.T) {
	var base BaseExecutable[map[string]any, map[string]any]
	sess := fakeSession{}
	ctx := context.Background()

	_, err := base.OnInvoke(ctx, map[string]any{}, sess, nil)
	if err == nil || !strings.Contains(err.Error(), "OnInvoke") {
		t.Errorf("OnInvoke 默认应返回未实现错误且含方法名, got %v", err)
	}

	_, err = base.OnStream(ctx, map[string]any{}, sess, nil)
	if err == nil || !strings.Contains(err.Error(), "OnStream") {
		t.Errorf("OnStream 默认应返回未实现错误且含方法名, got %v", err)
	}

	_, err = base.OnCollect(ctx, nil, sess, nil)
	if err == nil || !strings.Contains(err.Error(), "OnCollect") {
		t.Errorf("OnCollect 默认应返回未实现错误且含方法名, got %v", err)
	}

	_, err = base.OnTransform(ctx, nil, sess, nil)
	if err == nil || !strings.Contains(err.Error(), "OnTransform") {
		t.Errorf("OnTransform 默认应返回未实现错误且含方法名, got %v", err)
	}
}

// TestBaseExecutable_OnInvoke错误文案 断言错误文案复刻 executable.py:16-21 提示语义
func TestBaseExecutable_OnInvoke错误文案(t *testing.T) {
	var base BaseExecutable[map[string]any, map[string]any]
	_, err := base.OnInvoke(context.Background(), map[string]any{}, fakeSession{}, nil)
	if err == nil {
		t.Fatal("err 不应为 nil")
	}
	// 对齐 Python 文案语义：未实现 + 请在子类覆写 + 提供推理逻辑
	for _, want := range []string{"未实现", "覆写"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误文案应含 %q, got: %s", want, err.Error())
		}
	}
	// 偏差 D2：Go 侧文案不带实现类名（Python 为 type(self).__name__）
}

// generalExecutorComp 编译期断言 GeneralExecutor 别名可实例化（D6 死别名纯复刻）
type generalExecutorComp struct {
	BaseExecutable[map[string]any, map[string]any]
}

func (generalExecutorComp) OnInvoke(_ context.Context, inputs map[string]any, _ session.BaseSession, _ map[string]any) (map[string]any, error) {
	return inputs, nil
}

// TestGeneralExecutor_别名实例化 断言 map 载荷往返（spec §5 用例 5）
func TestGeneralExecutor_别名实例化(t *testing.T) {
	var exec GeneralExecutor = generalExecutorComp{}
	got, err := exec.OnInvoke(context.Background(), map[string]any{"k": "v"}, fakeSession{}, nil)
	if err != nil {
		t.Fatalf("OnInvoke 不应报错: %v", err)
	}
	if got["k"] != "v" {
		t.Errorf("map 载荷应原样返回, got %v", got)
	}
}

// ──────────────────────────── StreamChunk 契约 ────────────────────────────

// streamComp 测试用流式组件：产两块正常数据后送达错误并关闭
type streamComp struct {
	BaseExecutable[map[string]any, map[string]any]
	streamErr error
}

func (c streamComp) OnStream(_ context.Context, _ map[string]any, _ session.BaseSession, _ map[string]any) (<-chan StreamChunk[map[string]any], error) {
	ch := make(chan StreamChunk[map[string]any], 3)
	ch <- StreamChunk[map[string]any]{Val: map[string]any{"i": 1}}
	ch <- StreamChunk[map[string]any]{Val: map[string]any{"i": 2}}
	ch <- StreamChunk[map[string]any]{Err: c.streamErr}
	close(ch)
	return ch, nil
}

// TestStreamChunk_错误末元素后关闭 断言流中错误作为末元素送达、随后 channel 关闭（spec §5 用例 4）
func TestStreamChunk_错误末元素后关闭(t *testing.T) {
	wantErr := errors.New("mid-stream failure")
	comp := streamComp{streamErr: wantErr}
	ch, err := comp.OnStream(context.Background(), map[string]any{}, fakeSession{}, nil)
	if err != nil {
		t.Fatalf("启动错误不应发生: %v", err)
	}

	var vals []int
	var gotErr error
	for chunk := range ch {
		if chunk.Err != nil {
			gotErr = chunk.Err
			break
		}
		vals = append(vals, chunk.Val["i"].(int))
	}
	if len(vals) != 2 {
		t.Errorf("错误前应收到 2 个正常块, got %d", len(vals))
	}
	if !errors.Is(gotErr, wantErr) {
		t.Errorf("末元素错误应与预期一致, got %v", gotErr)
	}
	// 错误后 channel 已关闭：再次 range 立即结束
	if _, ok := <-ch; ok {
		t.Error("错误送达后 channel 应已关闭")
	}
}

// TestStreamChunk_正常流关闭不带Err 断言正常结束的流不含 Err 元素
func TestStreamChunk_正常流关闭不带Err(t *testing.T) {
	ch := make(chan StreamChunk[map[string]any], 1)
	ch <- StreamChunk[map[string]any]{Val: map[string]any{"done": true}}
	close(ch)
	for chunk := range ch {
		if chunk.Err != nil {
			t.Errorf("正常流不应携带 Err, got %v", chunk.Err)
		}
	}
}

// 编译期断言：嵌入基座的组件满足 Executable 接口
var _ Executable[map[string]any, map[string]any] = (*streamComp)(nil)
