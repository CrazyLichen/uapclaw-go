# 开发环境：Docker（RSI 评估 swebench 真链测试依赖）

swebench docker 集成测试（`tests/integration/evolving/rsi/evaluator_scoring_pipeline/swebench_docker_test.go`）需要本机 docker。当前开发机（Ubuntu 24.04）环境配置，供环境重建时复用：

## 安装与自启

```bash
apt-get install -y docker.io          # 29.x
systemctl enable --now docker.service containerd.service
```

## 镜像加速（必须）

本机直连 `registry-1.docker.io` 超时（外网受限），`/etc/docker/daemon.json` 配国内 mirror（alpine/swebench 镜像经此拉取）：

```json
{
  "registry-mirrors": [
    "https://docker.m.daocloud.io",
    "https://docker.1ms.run",
    "https://hub.rat.dev"
  ]
}
```

systemd 管理时改完 `systemctl restart docker` 生效；手动起的裸 dockerd 只在启动时读一次该文件。

## 运行测试

```bash
SWEBENCH_INTEGRATION=1 go test -tags=integration \
  ./tests/integration/evolving/rsi/evaluator_scoring_pipeline/ -run TestSwebenchDockerSuite -v
```

- 常规门禁：`SWEBENCH_INTEGRATION=1`（本镜像本地构建 alpine itest，无需 GB 级官方镜像）。
- 扩展门禁：`SWEBENCH_FULL=1`（官方 SWE-bench harness 收卷，需 GB 级镜像 + 外网，另议）。

## 运维注意

- `pkill -f dockerd` 会匹配到含 "dockerd" 字样的自身命令行导致自杀——用 `pkill -x dockerd` 或 `kill $(pgrep -x dockerd)`。
- busybox sleep 不支持 `infinity`；itest 镜像已装 coreutils（`swebench_docker_test.go` SetupTest 的 Dockerfile）。
