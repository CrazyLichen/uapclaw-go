package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// 端口类型到环境变量名的映射。
// 对应 Python: jiuwenswarm/instance_manager/bootstrap.py port_env_mapping
var portEnvMapping = map[string]string{
	"agent_server": "AGENT_SERVER_PORT",
	"web":          "WEB_PORT",
	"gateway":      "GATEWAY_PORT",
	"frontend":     "FRONTEND_PORT",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateBootstrapEnv 为实例创建 bootstrap .env 文件。
//
// 生成的 .env 文件包含：
//   - UAPCLAW_DATA_DIR: 实例工作区路径
//   - UAPCLAW_INSTANCE: 实例名称
//   - 各端口变量：AGENT_SERVER_PORT, WEB_PORT, GATEWAY_PORT, FRONTEND_PORT
//
// 对应 Python: jiuwenswarm/instance_manager/bootstrap.py create_bootstrap_env(config)
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
		envName, ok := portEnvMapping[pt]
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

	log.Info().Str("instance", config.Name).Str("path", envPath).Msg("已创建实例 bootstrap .env")
	return envPath, nil
}

// CreateBootstrapEnvForName 按名称创建 bootstrap .env 文件。
//
// 便捷方法：从 instances.yaml 加载配置后调用 CreateBootstrapEnv。
// 对应 Python: jiuwenswarm/instance_manager/bootstrap.py create_bootstrap_env_for_name(name, workspace)
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
