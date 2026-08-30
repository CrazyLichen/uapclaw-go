package skill

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// gitCloneTimeout git clone 默认超时（秒）
	gitCloneTimeout = 120
	// gitPullTimeout git pull 默认超时（秒）
	gitPullTimeout = 60
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// gitClone 克隆远程仓库到本地目录（对齐 Python: git clone --depth 1 <url> <dir>）。
//
// 使用浅克隆（--depth 1）加速，支持 context 取消。
func gitClone(ctx context.Context, url, dir string) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, gitCloneTimeout*time.Second)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, "git", "clone", "--depth", "1", url, dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone 失败 (%s): %w, 输出: %s", url, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// gitPull 拉取远程更新（对齐 Python: git -C <dir> pull --ff-only）。
func gitPull(ctx context.Context, dir string) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, gitPullTimeout*time.Second)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, "git", "-C", dir, "pull", "--ff-only")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git pull 失败 (%s): %w, 输出: %s", dir, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// gitGetCommit 获取当前 HEAD 的 commit hash（对齐 Python: git -C <dir> rev-parse HEAD）。
func gitGetCommit(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse 失败 (%s): %w", dir, err)
	}
	return strings.TrimSpace(string(output)), nil
}
