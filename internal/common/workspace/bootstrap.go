package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateBootstrapEnv 为实例创建 bootstrap .env 文件。
//
// 生成的 .env 文件包含：
//   - UAPCLAW_DATA_DIR: 实例工作区路径
//   - UAPCLAW_INSTANCE: 实例名称
//   - 各端口变量：UAPCLAW_AGENT_SERVER_PORT, UAPCLAW_WEB_PORT, UAPCLAW_GATEWAY_PORT, UAPCLAW_FRONTEND_PORT
//
// Python: jiuwenswarm/instance_manager/bootstrap.py create_bootstrap_env(config)
func CreateBootstrapEnv(config *InstanceConfig) (string, error) {
	if config == nil {
		return "", fmt.Errorf("config 不能为 nil")
	}

	envPath := filepath.Join(config.Workspace, ".env")

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		return "", fmt.Errorf("创建 .env 目录失败: %w", err)
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("# Bootstrap .env for instance: %s", config.Name))
	lines = append(lines, fmt.Sprintf("UAPCLAW_DATA_DIR=%s", config.Workspace))
	lines = append(lines, fmt.Sprintf("UAPCLAW_INSTANCE=%s", config.Name))

	// 添加端口变量
	for _, pt := range PortTypes {
		envName, ok := portEnvName(pt)
		if !ok {
			continue
		}
		if port, ok := config.Ports[pt]; ok {
			lines = append(lines, fmt.Sprintf("%s=%d", envName, port))
		}
	}

	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("写入 .env 失败: %w", err)
	}

	logger.Info(logComponent).Str("instance", config.Name).Str("path", envPath).Msg("created instance bootstrap .env")
	return envPath, nil
}

// CreateBootstrapEnvForName 按名称创建 bootstrap .env 文件。
//
// 便捷方法：从 instances.yaml 加载配置后调用 CreateBootstrapEnv。
// Python: jiuwenswarm/instance_manager/bootstrap.py create_bootstrap_env_for_name(name, workspace)
func CreateBootstrapEnvForName(name string, workspace string) (string, error) {
	index, err := GetInstanceIndex(name)
	if err != nil {
		return "", fmt.Errorf("获取实例序号失败: %w", err)
	}

	ports := CalculateInstancePorts(index)
	config := &InstanceConfig{
		Name:      name,
		Workspace: workspace,
		Ports:     ports,
	}

	return CreateBootstrapEnv(config)
}

// portEnvName 返回端口类型对应的环境变量名。
// 变量名对齐 Python bootstrap.py 的 port_env_mapping，但统一加 UAPCLAW_ 前缀，
// 必须与各组件实际读取点一致（web → UAPCLAW_WEB_PORT 等），
// 否则 bootstrap 分配的端口不会被进程采纳，多实例仍会抢同一端口。
func portEnvName(portType string) (string, bool) {
	switch portType {
	case "agent_server":
		return "UAPCLAW_AGENT_SERVER_PORT", true
	case "web":
		return "UAPCLAW_WEB_PORT", true
	case "gateway":
		return "UAPCLAW_GATEWAY_PORT", true
	case "frontend":
		return "UAPCLAW_FRONTEND_PORT", true
	default:
		return "", false
	}
}
