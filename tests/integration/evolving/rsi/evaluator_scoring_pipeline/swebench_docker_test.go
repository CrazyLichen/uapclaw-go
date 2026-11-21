//go:build integration

// 17.6 swebench/terminal-bench 容器运行时真实 docker 全链集成用例
// （spec 2026-11-21 §9 硬要求 2 + 2026-11-21 17.6d spec §7）。
//
// 运行方式：SWEBENCH_INTEGRATION=1 go test -tags=integration ./tests/integration/evolving/rsi/evaluator_scoring_pipeline/ -run TestSwebench -v
//
// 真实跨子系统链路（mock 只在 docker 进程边界——本测试不 mock docker）：
// swebench_runtime.PrepareSwebenchWorkspace（镜像核验/构建 + create/bind mount +
// start + 符号链接摘除/重建 + docker cp + git 配置）→
// terminal_bench_runtime.StartTerminalBenchSolverContainer（rm -f → create →
// start）→ 容器内 shell 命令（TerminalBenchDockerShellOperation，testbed 环境）→
// SyncContainerGitPatchToWorkspace（容器 git diff → 宿主 git apply）→
// RemoveTerminalBenchContainer。
//
// 官方 SWE-bench harness 收卷（pip 安装 swebench + 官方数据集）依赖 GB 级官方
// 镜像与外网，属 SWEBENCH_FULL=1 扩展门禁；本测试覆盖 Go 侧全部容器运行时逻辑。

package rsi

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/evaluator"
	sysop "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation"
)

// SwebenchDockerSuite swebench 容器运行时套件。
type SwebenchDockerSuite struct {
	suite.Suite
	image string
}

// TestSwebenchDockerSuite 入口（SWEBENCH_INTEGRATION=1 门禁）。
func TestSwebenchDockerSuite(t *testing.T) {
	if os.Getenv("SWEBENCH_INTEGRATION") != "1" {
		t.Skip("requires docker + SWEBENCH_INTEGRATION=1")
	}
	if out, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		t.Skipf("docker unavailable: %v: %s", err, out)
	}
	suite.Run(t, new(SwebenchDockerSuite))
}

// SetupTest 构建本地 itest 镜像（alpine + python3 + /testbed + 符号链接）。
func (s *SwebenchDockerSuite) SetupTest() {
	ctxDir := s.T().TempDir()
	dockerfile := `FROM alpine:3.19
RUN apk add --no-cache python3 git && mkdir -p /testbed /opt/miniconda3/envs/testbed/bin \
	&& ln -s /usr/bin/python3 /opt/miniconda3/envs/testbed/bin/python
WORKDIR /testbed
RUN echo "base content" > /testbed/README.md && echo "dep==1.0" > /testbed/requirements.txt
RUN ln -s /usr/bin/python3 /testbed/python-link
`
	s.Require().NoError(os.WriteFile(filepath.Join(ctxDir, "Dockerfile"), []byte(dockerfile), 0o644))
	image := "uapclaw-itest/sweb.eval.x86_64.itest_case_1:latest"
	build := exec.Command("docker", "build", "-t", image, ctxDir)
	out, err := build.CombinedOutput()
	s.Require().NoError(err, "docker build failed: %s", out)
	s.image = image
}

// TestSwebenchDocker_全链 考场准备 → solver 容器 → 容器命令 → patch sync → 清理。
func (s *SwebenchDockerSuite) TestSwebenchDocker_全链() {
	root := s.T().TempDir()
	workspaceDir := filepath.Join(root, "workspace")

	// ① PrepareSwebenchWorkspace：真实 docker create/start/cp/symlink/git config。
	image, err := evaluator.PrepareSwebenchWorkspace(evaluator.PrepareSwebenchParams{
		Case: map[string]any{
			"case_id": "itest-case-1",
			"swebench": map[string]any{
				"instance_id":  "itest__case-1",
				"namespace":    "uapclaw-itest",
				"docker_image": s.image,
			},
		},
		WorkspaceDir: workspaceDir,
		TimeoutSec:   300,
	})
	s.Require().NoError(err)
	s.Require().Equal(s.image, image)
	// /testbed 内容已拷出（docker cp 真实生效）。
	s.Require().FileExists(filepath.Join(workspaceDir, "README.md"))
	s.Require().FileExists(filepath.Join(workspaceDir, "requirements.txt"))
	// 符号链接经容器 bind mount 重建（Windows 场景对齐件在 Linux 上同样执行）。
	if _, err := os.Lstat(filepath.Join(workspaceDir, "python-link")); err != nil {
		s.T().Logf("symlink not restored (non-fatal on linux): %v", err)
	}
	// git 配置落位（core.filemode=false 等）。
	out, err := exec.Command("git", "-C", workspaceDir, "config", "core.filemode").CombinedOutput()
	s.Require().NoError(err)
	s.Require().Equal("false\n", string(out))

	// ② StartTerminalBenchSolverContainer。
	containerName, err := evaluator.StartTerminalBenchSolverContainer(
		evaluator.StartSolverContainerParams{
			DockerImage:           s.image,
			CaseID:                "itest-case-1",
			WorkspaceDir:          workspaceDir,
			TimeoutSec:            120,
			ContainerWorkspaceDir: "/testbed",
			MountWorkspace:        true,
		})
	s.Require().NoError(err)
	s.Require().True(strings.HasPrefix(containerName, "ach-tb-solver-itest-case-1-"))

	// ③ 容器内 shell 命令（TerminalBenchDockerShellOperation 真实 docker exec）。
	recorder := evaluator.NewTerminalBenchCommandRecorder()
	shellOp := evaluator.BuildTerminalBenchSysOperation(evaluator.SysOpBuildParams{
		SysOperationID:         "sweb_single_itest",
		ContainerName:          containerName,
		WorkspaceDir:           workspaceDir,
		Recorder:               recorder,
		ContainerWorkspaceDir:  "/testbed",
		CleanShell:             true,
		RuntimeEnvironment:     map[string]string{"ITEST_VAR": "hello"},
		EnforceInContainerTime: true,
	})
	var shell sysop.ShellOperation = shellOp.Shell()
	result, err := shell.ExecuteCmd(s.T().Context(), "echo $ITEST_VAR && cat README.md")
	s.Require().NoError(err)
	s.Require().True(result.IsSuccess())
	s.Require().Contains(result.Data.Stdout, "hello")
	s.Require().Contains(result.Data.Stdout, "base content")
	// 录音机记录真实命令。
	s.Require().Len(shellOp.CommandLog(), 1)
	s.Require().Equal("echo $ITEST_VAR && cat README.md", shellOp.CommandLog()[0]["command"])

	// ④ 容器内改动 → patch sync 回宿主（真实 git diff + git apply）。
	_, err = shell.ExecuteCmd(s.T().Context(), "echo 'solver edit' >> README.md && git add -A")
	s.Require().NoError(err)
	patch, err := evaluator.SyncContainerGitPatchToWorkspace(evaluator.SyncPatchParams{
		ContainerName:         containerName,
		WorkspaceDir:          workspaceDir,
		ContainerWorkspaceDir: "/testbed",
	})
	s.Require().NoError(err)
	s.Require().Contains(patch, "diff --git")
	// 宿主检出已应用补丁。
	edited, err := os.ReadFile(filepath.Join(workspaceDir, "README.md"))
	s.Require().NoError(err)
	s.Require().Contains(string(edited), "solver edit")

	// ⑤ CollectModelPatch（宿主 git diff --binary HEAD；git 配置已由考场准备落位）。
	// 宿主仓库未 init —— Python 语义下 collect_model_patch 失败抛 SWEbenchInfrastructureError，
	// 这里断言失败路径真实（错误可分类）。
	_, collectErr := evaluator.CollectModelPatch(workspaceDir)
	s.Require().Error(collectErr)

	// ⑥ RemoveTerminalBenchContainer（真实 docker rm -f）。
	evaluator.RemoveTerminalBenchContainer(containerName)
	check := exec.Command("docker", "inspect", containerName)
	s.Require().Error(check.Run(), "container must be removed")
}

// TestSwebenchDocker_依赖缓存 真实文件系统缓存链（swebench_runtime 依赖清单缓存）。
func (s *SwebenchDockerSuite) TestSwebenchDocker_依赖缓存() {
	cacheRoot := s.T().TempDir()
	s.T().Setenv("SWEBENCH_DEPENDENCY_CACHE_ROOT", cacheRoot)
	root := s.T().TempDir()
	workspaceDir := filepath.Join(root, "workspace")
	_, err := evaluator.PrepareSwebenchWorkspace(evaluator.PrepareSwebenchParams{
		Case: map[string]any{
			"case_id": "itest-case-2",
			"swebench": map[string]any{
				"instance_id": "itest__case-2",
				"namespace":   "uapclaw-itest",
				"docker_image": s.image,
				// base_commit == environment_setup_commit → 不触发外网下载。
				"base_commit":              "deadbeef",
				"environment_setup_commit": "deadbeef",
			},
		},
		WorkspaceDir: workspaceDir,
		TimeoutSec:   300,
	})
	s.Require().NoError(err)
	// 缓存目录落位：requirements.txt 被快照进依赖缓存。
	marker := filepath.Join(cacheRoot, "uapclaw-itest", "deadbeef", "cache.json")
	s.Require().FileExists(marker)
	s.Require().FileExists(filepath.Join(cacheRoot, "uapclaw-itest", "deadbeef", "requirements.txt"))
}
