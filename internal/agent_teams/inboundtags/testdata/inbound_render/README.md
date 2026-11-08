# inbound_render golden 基准

**这些文件由上游 Python 生成，请勿手改。**

- 生成脚本：`gen_golden.py`（同目录）
- 上游基线：`agent-core` 提交 `5a35c39f`（0.1.20）
- 重跑命令：

```bash
cd /home/opensource/uap-claw-go
python3 internal/agent_teams/testdata/inbound_render/gen_golden.py \
    --upstream /home/opensource/agent-core \
    --out internal/agent_teams/testdata/inbound_render
```

上游 `inbound_render.py` / `prompts/messages.py` / `timefmt.py` /
`external/format.py` / `i18n.py` 任一有变更时，重跑脚本并提交改动。

`*.golden` 是 UTF-8 原文（未做 `repr()` 包装），可直接 `cat` 核对。
上游返回 `None` 的用例写成空文件，Go 侧断言「返回 nil」。

**注意**：golden 文件**不带尾换行**（与上游返回值逐字节一致）。
`cat` 时最后一行的提示符会紧跟内容，这是正常的。
