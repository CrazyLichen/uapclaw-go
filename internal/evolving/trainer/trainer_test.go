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
	if trainer.checkpointEveryNEpochs != defaultCheckpointEveryNEpochs {
		t.Errorf("期望 checkpointEveryNEpochs=%d, 实际=%d", defaultCheckpointEveryNEpochs, trainer.checkpointEveryNEpochs)
	}
	if trainer.checkpointOnImprove != defaultCheckpointOnImprove {
		t.Errorf("期望 checkpointOnImprove=%v, 实际=%v", defaultCheckpointOnImprove, trainer.checkpointOnImprove)
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
		WithCheckpointDir("/tmp/evolve"),
		WithCheckpointEveryNEpochs(3),
		WithCheckpointOnImprove(false),
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
	if trainer.checkpointDir != "/tmp/evolve" {
		t.Errorf("期望 checkpointDir=/tmp/evolve, 实际=%s", trainer.checkpointDir)
	}
	if trainer.checkpointEveryNEpochs != 3 {
		t.Errorf("期望 checkpointEveryNEpochs=3, 实际=%d", trainer.checkpointEveryNEpochs)
	}
	if trainer.checkpointOnImprove != false {
		t.Error("期望 checkpointOnImprove=false")
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
	if p.MaxEpoch != defaultNumIterations {
		t.Errorf("期望 MaxEpoch=%d, 实际=%d", defaultNumIterations, p.MaxEpoch)
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

// TestProgress_RunEpoch 测试 RunEpoch 迭代
func TestProgress_RunEpoch(t *testing.T) {
	p := NewProgressWithMaxEpoch(5)
	p.StartEpoch = 0

	var epochs []int
	for epoch := range p.RunEpoch() {
		epochs = append(epochs, epoch)
	}

	// 期望迭代 1,2,3,4,5
	if len(epochs) != 5 {
		t.Errorf("期望 5 个 epoch, 实际 %d", len(epochs))
	}
	for i, want := range []int{1, 2, 3, 4, 5} {
		if epochs[i] != want {
			t.Errorf("epochs[%d]=%d, 期望 %d", i, epochs[i], want)
		}
	}
	// 自然结束后 CurrentEpoch 应为 MaxEpoch
	if p.CurrentEpoch != 5 {
		t.Errorf("期望 CurrentEpoch=5, 实际=%d", p.CurrentEpoch)
	}
}

// TestProgress_RunEpoch_从断点续训 测试 RunEpoch 从 StartEpoch+1 开始
func TestProgress_RunEpoch_从断点续训(t *testing.T) {
	p := NewProgressWithMaxEpoch(5)
	p.StartEpoch = 2 // 从 epoch 3 开始

	var epochs []int
	for epoch := range p.RunEpoch() {
		epochs = append(epochs, epoch)
	}

	// 期望迭代 3,4,5
	if len(epochs) != 3 {
		t.Errorf("期望 3 个 epoch, 实际 %d", len(epochs))
	}
	for i, want := range []int{3, 4, 5} {
		if epochs[i] != want {
			t.Errorf("epochs[%d]=%d, 期望 %d", i, epochs[i], want)
		}
	}
}

// TestProgress_RunEpoch_中断时CurrentEpoch保持 测试 break 中断时 CurrentEpoch 保持
func TestProgress_RunEpoch_中断时CurrentEpoch保持(t *testing.T) {
	p := NewProgressWithMaxEpoch(10)

	for epoch := range p.RunEpoch() {
		if epoch == 3 {
			break
		}
	}

	// break 时 CurrentEpoch 应为 3
	if p.CurrentEpoch != 3 {
		t.Errorf("期望 CurrentEpoch=3, 实际=%d", p.CurrentEpoch)
	}
}

// TestProgress_RunBatch 测试 RunBatch 迭代
func TestProgress_RunBatch(t *testing.T) {
	p := NewProgress()
	p.MaxBatchIter = 4
	p.BestBatchScore = 0.5 // 应被重置为 0

	var batches []int
	for batch := range p.RunBatch() {
		batches = append(batches, batch)
	}

	// 期望迭代 0,1,2,3
	if len(batches) != 4 {
		t.Errorf("期望 4 个 batch, 实际 %d", len(batches))
	}
	for i, want := range []int{0, 1, 2, 3} {
		if batches[i] != want {
			t.Errorf("batches[%d]=%d, 期望 %d", i, batches[i], want)
		}
	}
	// BestBatchScore 应被重置为 0
	if p.BestBatchScore != 0 {
		t.Errorf("期望 BestBatchScore=0, 实际=%f", p.BestBatchScore)
	}
}

// TestProgress_RunBatch_中断时CurrentBatchIter保持 测试 break 中断时 CurrentBatchIter 保持
func TestProgress_RunBatch_中断时CurrentBatchIter保持(t *testing.T) {
	p := NewProgress()
	p.MaxBatchIter = 10

	for batch := range p.RunBatch() {
		if batch == 2 {
			break
		}
	}

	if p.CurrentBatchIter != 2 {
		t.Errorf("期望 CurrentBatchIter=2, 实际=%d", p.CurrentBatchIter)
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────
