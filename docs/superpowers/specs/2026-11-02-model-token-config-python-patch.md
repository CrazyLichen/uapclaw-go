# Python 侧同构改动补丁说明（model token 配置）

- 日期：2026-11-02
- 对应 Go 侧设计：`docs/superpowers/specs/2026-11-02-model-token-config-design.md`（改动点 P1-P6）
- 适用仓库：`jiuwenswarm`（`jiuwenswarm-develop`）与 `agent-core`（`openjiuwen`）
- 说明：本仓库（uap-claw-go）无法提交这两个仓库的改动，以下为可直接应用的逐条补丁。

目的：让同一份 `config.yaml` 在 Python 与 Go 两个实现下表现一致——`max_input_tokens` 决定上下文窗口分母与压缩阈值，`max_output_tokens` 决定请求 `max_tokens` 默认值。

---

## P1：写侧白名单（`app_web_handlers.py`）

**位置**：`jiuwenswarm/gateway/channel_manager/web/app_web_handlers.py:828-843`（`_build_models_defaults_from_frontend` 的 `parsed.append({...})`）

改前：

```python
            parsed.append({
                "model_name": model_name,
                "api_base": api_base,
                "api_key": api_key,
                "model_provider": model_provider,
                "temperature": temperature,
                "is_default": is_default,
                "timeout": timeout,
                "verify_ssl": verify_ssl,
                "alias": alias,
                "origin_index": origin_index,
            })
```

改后（新增两个字段与解析，非法值抛 `_ConfigBadRequest`）：

```python
            max_input_tokens = 0
            raw_input = item.get("max_input_tokens")
            if raw_input not in (None, ""):
                try:
                    max_input_tokens = int(raw_input)
                except (TypeError, ValueError):
                    raise _ConfigBadRequest(f"models[{idx}].max_input_tokens must be a non-negative integer")
                if max_input_tokens < 0:
                    raise _ConfigBadRequest(f"models[{idx}].max_input_tokens must be a non-negative integer")

            max_output_tokens = 0
            raw_output = item.get("max_output_tokens")
            if raw_output not in (None, ""):
                try:
                    max_output_tokens = int(raw_output)
                except (TypeError, ValueError):
                    raise _ConfigBadRequest(f"models[{idx}].max_output_tokens must be a non-negative integer")
                if max_output_tokens < 0:
                    raise _ConfigBadRequest(f"models[{idx}].max_output_tokens must be a non-negative integer")

            parsed.append({
                "model_name": model_name,
                "api_base": api_base,
                "api_key": api_key,
                "model_provider": model_provider,
                "temperature": temperature,
                "is_default": is_default,
                "timeout": timeout,
                "verify_ssl": verify_ssl,
                "alias": alias,
                "origin_index": origin_index,
                "max_input_tokens": max_input_tokens,
                "max_output_tokens": max_output_tokens,
            })
```

---

## P2：合并落盘（`_merge_models_for_replace_all`）

**位置**：同文件 `:82-145`。该函数是**逐字段显式合并**（与 Go 的通用 key 迭代不同），因此两个分支都要各加两行。

「已有条目」分支，在 `new_entry["is_default"] = item["is_default"]` 之前插入：

```python
            if not _values_match(item["max_input_tokens"], resolved_mcc.get("max_input_tokens")):
                new_mcc["max_input_tokens"] = item["max_input_tokens"]
            if not _values_match(item["max_output_tokens"], resolved_mcc.get("max_output_tokens")):
                new_mcc["max_output_tokens"] = item["max_output_tokens"]
```

「新条目」分支，在构造 `"model_client_config": {...}` 的字典里追加：

```python
                    "max_input_tokens": item["max_input_tokens"],
                    "max_output_tokens": item["max_output_tokens"],
```

---

## P3：读侧回填（`_models_list`）

**位置**：同文件 `:1023-1055`（`_models_list` 内扁平化每个条目处）

在已有 `client_provider → model_provider` 映射之后追加：

```python
            mcc = entry.get("model_client_config") or {}
            if isinstance(mcc.get("max_input_tokens"), int) and mcc["max_input_tokens"] > 0:
                flat["max_input_tokens"] = mcc["max_input_tokens"]
            if isinstance(mcc.get("max_output_tokens"), int) and mcc["max_output_tokens"] > 0:
                flat["max_output_tokens"] = mcc["max_output_tokens"]
```

（`0` 不回填，前端显示为空，避免界面上出现一排 `0`。）

---

## P4：窗口映射（`interface_deep.py` `_deep_agent_context_engine_config`）

**位置**：`jiuwenswarm/server/runtime/agent_adapter/interface_deep.py:309-320`

改前：

```python
def _deep_agent_context_engine_config(react_cfg: dict[str, Any] | None) -> ContextEngineConfig:
    react_cfg = react_cfg or {}
    cec = react_cfg.get("context_engine_config")
    enable_kv = bool(cec.get("enable_kv_cache_release", False)) if isinstance(cec, dict) else False
    return ReActAgentConfig().context_engine_config.model_copy(
        update={"enable_kv_cache_release": enable_kv}
    )
```

改后（保留 `model_copy` 语义，额外收集窗口与比例）：

```python
def _deep_agent_context_engine_config(
    react_cfg: dict[str, Any] | None,
    config_base: dict[str, Any] | None = None,
) -> ContextEngineConfig:
    """构建 DeepAgent 的 ContextEngineConfig。

    与 Go 侧一致：max_input_tokens 收集成 model_context_window_tokens，
    compression_*_ratio 透传；其余字段沿用 ReActAgentConfig 默认值
    （max_context_message_num=200 / default_window_round_num=10）。
    """
    react_cfg = react_cfg or {}
    cec = react_cfg.get("context_engine_config")
    cec = cec if isinstance(cec, dict) else {}
    update: dict[str, Any] = {
        "enable_kv_cache_release": bool(cec.get("enable_kv_cache_release", False)),
    }
    trigger_ratio = cec.get("compression_trigger_ratio")
    if isinstance(trigger_ratio, (int, float)) and 0 < float(trigger_ratio) <= 1:
        update["compression_trigger_ratio"] = float(trigger_ratio)
    target_ratio = cec.get("compression_target_ratio")
    if isinstance(target_ratio, (int, float)) and 0 < float(target_ratio) <= 1:
        update["compression_target_ratio"] = float(target_ratio)

    window_map: dict[str, int] = {}
    default_model_name = ""
    first_model_name = ""
    for entry in get_default_models(config_base):
        mcc = entry.get("model_client_config") or {}
        name = mcc.get("model_name") or ""
        if not name:
            continue
        first_model_name = first_model_name or name
        if entry.get("is_default") and not default_model_name:
            default_model_name = name
        tokens = mcc.get("max_input_tokens")
        if not isinstance(tokens, int) or tokens <= 0:
            continue
        prev = window_map.get(name)
        if prev is not None and prev != tokens:
            if entry.get("is_default"):
                window_map[name] = tokens
            logger.warning(
                "[JiuWenClawDeepAdapter] max_input_tokens conflict for %s: keep %s, ignore %s",
                name, window_map[name], tokens,
            )
            continue
        window_map[name] = tokens

    if window_map:
        update["model_context_window_tokens"] = window_map
        update["model_name"] = react_cfg.get("model_name") or default_model_name or first_model_name

    return ReActAgentConfig().context_engine_config.model_copy(update=update)
```

调用点（`interface_deep.py:2268` 与热重载路径）改为传 `config_base`：

```python
            context_engine_config=_deep_agent_context_engine_config(config, config_base),
```

> 注：`config` 是 react 段，`config_base` 是整份配置（`models` 段在其中）。若调用点只有整份配置，传 `config.get("react", {})` 与整份配置两次。

---

## P5：输出上限（`interface_deep.py` `_build_model_from_entry`）

**位置**：`interface_deep.py:1482-1490`

改前：

```python
        m_config = ModelRequestConfig(
            model=name,
            temperature=mco.get("temperature", 0.95),
        )
```

改后：

```python
        max_output_tokens = mcc.get("max_output_tokens")
        max_tokens = (
            int(max_output_tokens)
            if isinstance(max_output_tokens, int) and max_output_tokens > 0
            else None
        )
        m_config = ModelRequestConfig(
            model=name,
            temperature=mco.get("temperature", 0.95),
            max_tokens=max_tokens,
        )
```

注意 `mcc_fields` 会把 `max_input_tokens` / `max_output_tokens` 一并传给 `ModelClientConfig(**mcc_fields)`。`ModelClientConfig` 是 `extra: "allow"`，不会报错但会在 `__pydantic_extra__` 里留一份，且 `intelli_router` 等客户端会读 extra。建议在构造前剔除：

```python
        mcc_fields = {k: v for k, v in mcc.items()
                      if k not in ("model_name", "max_input_tokens", "max_output_tokens")}
```

---

## P6：压缩阈值按比例收口（`context_processor_rail.py`）

**位置**：`openjiuwen/core/context_engine/processor/context_processor_rail.py`

在处理器配置合并**之后**（即 Python 的 `_build_merged_cfg` / `merge_config` 结果之上）追加收口，保证 yaml 的绝对值先参与 `min`：

```python
    def _apply_compression_ratios(self, processors, window: int,
                                  trigger_ratio: float, target_ratio: float):
        """阈值 = min(绝对阈值, round(ratio × window))，只下调不放大。

        只处理与「整个上下文总量」同量纲的字段；per-message 与单次输出长度类字段不缩放。
        """
        if window <= 0 or (trigger_ratio <= 0 and target_ratio <= 0):
            return processors

        def _shrink(absolute: int, ratio: float) -> int:
            if ratio <= 0 or absolute <= 0:
                return absolute
            scaled = round(ratio * window)
            return min(absolute, scaled) if scaled > 0 else absolute

        for name, cfg in processors:
            if name == "DialogueCompressor":
                cfg.tokens_threshold = _shrink(cfg.tokens_threshold, trigger_ratio)
            elif name == "CurrentRoundCompressor":
                cfg.tokens_threshold = _shrink(cfg.tokens_threshold, trigger_ratio)
            elif name == "RoundLevelCompressor":
                cfg.trigger_total_tokens = _shrink(cfg.trigger_total_tokens, trigger_ratio)
                cfg.target_total_tokens = _shrink(cfg.target_total_tokens, target_ratio)
                if cfg.target_total_tokens >= cfg.trigger_total_tokens:
                    fallback = max(1, round(cfg.trigger_total_tokens * 0.7))
                    logger.warning(
                        "[ContextProcessorRail] target >= trigger (%s >= %s)，回退为 %s",
                        cfg.target_total_tokens, cfg.trigger_total_tokens, fallback,
                    )
                    cfg.target_total_tokens = fallback
            elif name == "FullCompactProcessor":
                cfg.trigger_total_tokens = _shrink(cfg.trigger_total_tokens, trigger_ratio)
        return processors
```

窗口值取法与 Go 一致：

```python
    from openjiuwen.core.context_engine.context.context_utils import ContextUtils
    ce_cfg = agent_config.context_engine_config
    window = ContextUtils.resolve_context_max(
        model_name=ce_cfg.model_name,
        fallback_context_window_tokens=ce_cfg.context_window_tokens,
        model_context_window_tokens=ce_cfg.model_context_window_tokens,
    )
    processors = self._apply_compression_ratios(
        processors, window,
        getattr(ce_cfg, "compression_trigger_ratio", 0.0) or 0.0,
        getattr(ce_cfg, "compression_target_ratio", 0.0) or 0.0,
    )
```

需同时给 `openjiuwen/core/context_engine/schema/config.py` 的 `ContextEngineConfig` 加两个字段：

```python
    compression_trigger_ratio: float = Field(default=0.0, ge=0, le=1)
    compression_target_ratio: float = Field(default=0.0, ge=0, le=1)
```

并在 `model_validator` 中校验 `target < trigger`（两者都 > 0 时）。

---

## P7：模板补齐（`jiuwenswarm/resources/config.yaml`）

**位置**：`:269-303`（`react.context_engine_config`）

在 `enable_kv_cache_release: false` 之后追加：

```yaml
    # 压缩水位比例（相对 max_input_tokens）：阈值 = min(下列绝对阈值, ratio × 输入上限)
    compression_trigger_ratio: 0.85
    compression_target_ratio: 0.55
```

在段末（`round_level_compressor_config` 之后）追加（Go 已支持，Python 待支持）：

```yaml
    full_compact_processor_config:
      trigger_total_tokens: 180000
```

`models.defaults` 条目补两个注释掉的示例键（不要写死数值，避免覆盖用户环境）：

```yaml
        # max_input_tokens: 32000   # 输入上限（token），窗口分母
        # max_output_tokens: 8192   # 输出上限（token），请求 max_tokens 默认值
```

---

## 验证清单（Python 侧）

1. `_models_list` 返回两个字段；`models.replace_all` 写盘后 YAML 保留 `${...}` 占位符
2. 加载 32K 配置后 `ContextUtils.resolve_context_max(model_name=...)` 返回 32000（不再 200000）
3. `ModelRequestConfig.max_tokens == 8192`（未配置时为 `None`）
4. 32K 窗口下 `RoundLevelCompressor.trigger_total_tokens == 27200`、`target_total_tokens == 17600`
5. `enabled: false` 时 rail 不注册，阈值全部休眠
6. `pytest tests/` 与 Go 侧同名的 4 条集成场景全部通过
