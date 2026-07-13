package trainer

import (
	"context"
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewTrainer 测试 Trainer 构造函数默认值
func TestNewTrainer(t *testing.T) {
	trainer := NewTrainer()
	if trainer.numParallel != defaultNumParallel {
		t.Errorf("期望 numParallel=%d, 实际=%d", defaultNumParallel, trainer.numParallel)
	}
	if trainer.earlyStopScore != defaultEarlyStopScore {
		t.Errorf("期望 earlyStopScore=%f, 实际=%f", defaultEarlyStopScore, trainer.earlyStopScore)
	}
	if trainer.callbacks == nil {
		t.Error("期望 callbacks 非 nil")
	}
}

// TestNewTrainer_使用选项 测试 Trainer 构造函数带选项
func TestNewTrainer_使用选项(t *testing.T) {
	cb := &Callbacks{}
	trainer := NewTrainer(
		WithNumParallel(5),
		WithEarlyStopScore(0.8),
		WithCallbacks(cb),
		WithResumeFrom("/tmp/ckpt"),
	)
	if trainer.numParallel != 5 {
		t.Errorf("期望 numParallel=5, 实际=%d", trainer.numParallel)
	}
	if trainer.earlyStopScore != 0.8 {
		t.Errorf("期望 earlyStopScore=0.8, 实际=%f", trainer.earlyStopScore)
	}
	if trainer.callbacks != cb {
		t.Error("期望 callbacks 为传入实例")
	}
	if trainer.resumeFrom != "/tmp/ckpt" {
		t.Errorf("期望 resumeFrom=/tmp/ckpt, 实际=%s", trainer.resumeFrom)
	}
}

// TestNewTrainer_桩方法返回未实现错误 测试桩方法返回 not implemented 错误
func TestNewTrainer_桩方法返回未实现错误(t *testing.T) {
	trainer := NewTrainer()
	ctx := context.Background()

	_, err := trainer.Train(ctx, nil, nil, nil, 0, nil)
	if err == nil {
		t.Error("期望 Train 返回 not implemented 错误")
	}

	_, _, _, _, err = trainer.Forward(ctx, nil, nil)
	if err == nil {
		t.Error("期望 Forward 返回 not implemented 错误")
	}

	_, _, err = trainer.Evaluate(ctx, nil, nil)
	if err == nil {
		t.Error("期望 Evaluate 返回 not implemented 错误")
	}

	_, err = trainer.PredictOnly(ctx, nil, nil)
	if err == nil {
		t.Error("期望 PredictOnly 返回 not implemented 错误")
	}

	_, _, err = trainer.Predict(ctx, nil, nil)
	if err == nil {
		t.Error("期望 Predict 返回 not implemented 错误")
	}
}

// TestNewProgress 测试 Progress 构造默认值
func TestNewProgress(t *testing.T) {
	p := NewProgress()
	if p.MaxEpoch != 3 {
		t.Errorf("期望 MaxEpoch=3, 实际=%d", p.MaxEpoch)
	}
	if p.MaxBatchIter != 1 {
		t.Errorf("期望 MaxBatchIter=1, 实际=%d", p.MaxBatchIter)
	}
	if p.StartEpoch != 0 {
		t.Errorf("期望 StartEpoch=0, 实际=%d", p.StartEpoch)
	}
}

// TestNewProgressWithMaxEpoch 测试指定最大 epoch 的 Progress 构造
func TestNewProgressWithMaxEpoch(t *testing.T) {
	p := NewProgressWithMaxEpoch(10)
	if p.MaxEpoch != 10 {
		t.Errorf("期望 MaxEpoch=10, 实际=%d", p.MaxEpoch)
	}
}

// TestSetCallbacks 测试设置回调
func TestSetCallbacks(t *testing.T) {
	trainer := NewTrainer()
	cb := &Callbacks{}
	trainer.SetCallbacks(cb)
	if trainer.callbacks != cb {
		t.Error("期望 callbacks 为设置后的实例")
	}
}

// TestNewCallbacks 测试 Callbacks 默认构造
func TestNewCallbacks(t *testing.T) {
	cb := NewCallbacks()
	if cb == nil {
		t.Error("期望 NewCallbacks 返回非 nil")
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────
