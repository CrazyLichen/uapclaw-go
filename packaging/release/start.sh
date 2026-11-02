#!/usr/bin/env bash
# UapClaw 快捷启动：等价于 ./uapclaw app，额外参数原样透传（如 ./start.sh --port 19001）
set -euo pipefail
cd "$(dirname "$0")"
exec ./uapclaw app "$@"
