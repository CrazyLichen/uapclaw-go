#!/usr/bin/env python3
# coding: utf-8
"""生成 inbound_render / messages / timefmt / external.format 的 golden 文件。

用法:
    cd /home/opensource/uap-claw-go
    python3 internal/agent_teams/testdata/inbound_render/gen_golden.py \
        --upstream /home/opensource/agent-core \
        --out internal/agent_teams/testdata/inbound_render

前置:
    * 上游 agent-core 源码在 --upstream 指向的目录
    * 时区固定 TZ=Asia/Shanghai（脚本内部会设置并 tzset）
    * 无需安装 openjiuwen 的依赖：脚本用 importlib 直接加载单文件模块，
      绕过 openjiuwen.agent_teams.__init__ 的重依赖导入链

上游基线: agent-core 5a35c39f (0.1.20)
重跑时机: 上游 inbound_render.py / prompts/messages.py / timefmt.py /
          external/format.py / i18n.py 有变更时
产物:     *.golden（UTF-8 原文，不做 repr 包装，方便 cat 核对）
          * 请勿手改 golden —— 改上游再重跑本脚本
"""

from __future__ import annotations

import argparse
import enum
import importlib.util
import os
import sys
import types

TS = 1779949805000  # 2026-05-28 14:30:05 +08:00 的毫秒 epoch


def _bootstrap(upstream: str) -> dict:
    """伪造 openjiuwen 包层级，直接按文件路径加载目标模块（绕过 __init__）。"""
    sys.path.insert(0, upstream)
    os.environ["TZ"] = "Asia/Shanghai"
    import time as _t

    _t.tzset()

    for name in (
        "openjiuwen",
        "openjiuwen.agent_teams",
        "openjiuwen.agent_teams.schema",
        "openjiuwen.agent_teams.prompts",
        "openjiuwen.agent_teams.external",
    ):
        m = types.ModuleType(name)
        m.__path__ = []
        sys.modules[name] = m

    def load(modname: str, relpath: str):
        spec = importlib.util.spec_from_file_location(
            modname, os.path.join(upstream, relpath)
        )
        mod = importlib.util.module_from_spec(spec)
        sys.modules[modname] = mod
        spec.loader.exec_module(mod)
        return mod

    load("openjiuwen.agent_teams.constants", "openjiuwen/agent_teams/constants.py")
    i18n = load("openjiuwen.agent_teams.i18n", "openjiuwen/agent_teams/i18n.py")

    # TeamRole 单独造 stub：schema/team.py 依赖链过重，且 messages.py 只用到枚举值
    team_mod = types.ModuleType("openjiuwen.agent_teams.schema.team")

    class TeamRole(str, enum.Enum):
        LEADER = "leader"
        TEAMMATE = "teammate"
        HUMAN_AGENT = "human_agent"
        PASSIVE_HUMAN = "passive_human"
        BRIDGE_AGENT = "bridge_agent"
        WORKER = "worker"
        EXTERNAL_CLI = "external_cli"

    team_mod.TeamRole = TeamRole
    sys.modules["openjiuwen.agent_teams.schema.team"] = team_mod

    tf = load("openjiuwen.agent_teams.timefmt", "openjiuwen/agent_teams/timefmt.py")
    ir = load(
        "openjiuwen.agent_teams.inbound_render",
        "openjiuwen/agent_teams/inbound_render.py",
    )
    msgs = load(
        "openjiuwen.agent_teams.prompts.messages",
        "openjiuwen/agent_teams/prompts/messages.py",
    )
    fmt = load(
        "openjiuwen.agent_teams.external.format",
        "openjiuwen/agent_teams/external/format.py",
    )
    return {"i18n": i18n, "ir": ir, "tf": tf, "msgs": msgs, "fmt": fmt}


def _write(out_dir: str, name: str, content: str | None) -> None:
    """写一个 golden。None 表示上游返回 None，用特殊文件名 *__none.golden 表示。"""
    path = os.path.join(out_dir, name + ".golden")
    if content is None:
        # 上游返回 None 的用例：Go 侧应返回 nil。写空文件 + 用用例名区分。
        open(path, "w", encoding="utf-8").close()
        print(f"  {name}.golden  (None → 空文件)")
        return
    with open(path, "w", encoding="utf-8") as fh:
        fh.write(content)
    print(f"  {name}.golden  ({len(content)} 字节)")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--upstream", required=True)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    os.makedirs(args.out, exist_ok=True)
    mods = _bootstrap(args.upstream)
    i18n, ir, tf, msgs, fmt = (
        mods["i18n"], mods["ir"], mods["tf"], mods["msgs"], mods["fmt"],
    )
    i18n.set_language("cn")

    print("[inbound_render]")
    _write(args.out, "render_inbound_direct", ir.render_inbound(
        content="你帮我看看这个接口", sender="dev-1", message_id="m1",
        msg_type=ir.INBOUND_TYPE_DIRECT, time_info="2026-05-28 14:30:05 +08:00",
    ))
    _write(args.out, "render_inbound_with_reply_hint", ir.render_inbound(
        content="你帮我看看这个接口", sender="dev-1", message_id="m1",
        msg_type=ir.INBOUND_TYPE_DIRECT, time_info="2026-05-28 14:30:05 +08:00",
        note_kind="reply-hint", note_text=i18n.t("dispatcher.reply_hint", sender="dev-1"),
    ))
    _write(args.out, "render_inbound_hitt", ir.render_inbound(
        content="请审批", sender="dev-2", message_id="m2",
        msg_type=ir.INBOUND_TYPE_BROADCAST,
        time_info="2026-05-28 14:31:00 +08:00", for_controller=True,
        note_kind="hitt-silence", note_text=i18n.t("hitt.silence_note"),
    ))
    _write(args.out, "render_controller_input", ir.render_controller_input(
        content="转告 dev-1 已完成",
    ))
    _write(args.out, "render_clock", ir.render_clock(now="2026-05-28 14:35:00 +08:00"))
    _write(args.out, "render_event_plain", ir.render_event(
        kind="all-done", body="任务看板上已无未完成任务",
    ))
    _write(args.out, "render_event_with_task_id", ir.render_event(
        kind="task-assigned", body="请处理任务 t1", task_id="t1",
    ))
    _write(args.out, "render_team_context", ir.render_team_context(
        body="# 成员身份\n\n你的 member_name: dev-1",
    ))
    _write(args.out, "render_team_context_with_identity", ir.render_team_context_with_identity(
        identity_body="# 成员身份\n\n你的 member_name: dev-1",
        identity_conversion=None,
        info_body="# 团队信息\n\n- team_name: t1",
    ))
    _write(args.out, "render_team_context_with_conversion", ir.render_team_context_with_identity(
        identity_body="# 成员身份\n\n你的 member_name: dev-1",
        identity_conversion=msgs.build_identity_conversion(
            source="dev-0", member_name="dev-1", language="cn",
        ),
        info_body=None,
    ))
    _write(args.out, "render_team_policy", ir.render_team_policy(
        body="# 团队准则\n\n用 & 表示同时满足", tools="team_tools",
        note="工具名不带命名空间前缀",
    ))
    _write(args.out, "render_team_policy_inline", ir.render_team_policy_inline(
        body="# 团队准则\n\n- 先看板",
    ))
    _write(args.out, "drop_superseded_snapshots", "\n".join(ir.drop_superseded_snapshots([
        '<team-event kind="roster-change">\nA\n</team-event>',
        '<team-event kind="task-board">\nB1\n</team-event>',
        '<team-clock now="x"/>',
        '<team-event kind="task-board">\nB2\n</team-event>',
    ])))

    print("[timefmt]")
    _write(args.out, "format_absolute_time", tf.format_absolute_time(TS))
    _write(args.out, "format_time_context_just_now",
           tf.format_time_context(TS, TS + 3000))
    _write(args.out, "format_time_context_seconds",
           tf.format_time_context(TS, TS + 30000))
    _write(args.out, "format_time_context_minutes",
           tf.format_time_context(TS, TS + 180000))
    _write(args.out, "format_time_context_hours",
           tf.format_time_context(TS, TS + 7200000))
    _write(args.out, "format_time_context_days",
           tf.format_time_context(TS, TS + 172800000))
    _write(args.out, "format_time_context_unknown", tf.format_time_context(None, 0))

    print("[prompts.messages]")
    _write(args.out, "identity_text_full", msgs.build_identity_text(
        member_name="dev-1", display_name="开发一",
        member_workspace_path="/ws/dev-1", member_prompt="只改 internal/",
        language="cn",
    ))
    _write(args.out, "identity_text_minimal", msgs.build_identity_text(
        member_name="dev-1", language="cn",
    ))
    _write(args.out, "identity_text_fork_capable", msgs.build_identity_text(
        member_name="dev-1", display_name="开发一", language="cn", fork_capable=True,
    ))
    _write(args.out, "identity_text_en_full", msgs.build_identity_text(
        member_name="dev-1", display_name="Dev One",
        member_workspace_path="/ws/dev-1", member_prompt="internal only",
        language="en",
    ))
    _write(args.out, "identity_text_all_empty", msgs.build_identity_text(
        member_name=None, language="cn",
    ))
    _write(args.out, "identity_prompt_delta", msgs.build_identity_prompt_delta(
        member_prompt="只改 internal/", language="cn",
    ))
    _write(args.out, "identity_prompt_delta_empty", msgs.build_identity_prompt_delta(
        member_prompt="   ", language="cn",
    ))
    _write(args.out, "identity_conversion", msgs.build_identity_conversion(
        source="dev-0", member_name="dev-1", language="cn",
    ))
    _write(args.out, "team_info_text_full", msgs.build_team_info_text(
        team_info={"team_name": "t1", "display_name": "团队一", "desc": "做后端"},
        team_workspace_path="/ws/team", team_outputs_dir="/ws/team/out",
        language="cn",
    ))
    _write(args.out, "team_info_text_all_empty", msgs.build_team_info_text(
        team_info=None, language="cn",
    ))
    _write(args.out, "roster_snapshot", msgs.build_roster_snapshot_text(
        members=[{"member_name": "dev-1", "display_name": "开发一",
                  "desc": "后端", "role": "teammate"}],
        mark_humans=False, language="cn",
    ))
    _write(args.out, "roster_snapshot_empty", msgs.build_roster_snapshot_text(
        members=[], language="cn",
    ))
    _delta = msgs.diff_roster(
        [{"member_name": "a", "display_name": "A", "role": "teammate"}],
        [{"member_name": "a", "display_name": "A2", "role": "teammate"},
         {"member_name": "b", "display_name": "B", "role": "teammate"}],
    )
    _write(args.out, "roster_delta", msgs.build_roster_delta_text(
        delta=_delta, mark_humans=False, language="cn",
    ))
    _write(args.out, "roster_delta_en", msgs.build_roster_delta_text(
        delta=msgs.diff_roster(
            [{"member_name": "a", "display_name": "A", "role": "teammate"}],
            [{"member_name": "b", "display_name": "B", "role": "teammate"}],
        ),
        mark_humans=False, language="en",
    ))
    _write(args.out, "member_line_full", msgs.format_member_line(
        {"member_name": "dev-1", "display_name": "开发一", "desc": "后端",
         "role": "teammate", "model_group": "g1", "models_used": "a,b"},
    ))
    _write(args.out, "member_line_human", msgs.format_member_line(
        {"member_name": "h1", "display_name": "张三", "role": "passive_human"},
        mark_humans=True,
    ))
    _write(args.out, "member_line_prefix", msgs.format_member_line(
        {"member_name": "dev-1", "display_name": "开发一", "role": "teammate"},
        prefix="加入",
    ))

    print("[external.format]")
    class _Msg:
        message_id = "m1"
        from_member_name = "dev-1"
        content = "你帮我看看"
        broadcast = False
        timestamp = TS

    class _MsgUser(_Msg):
        message_id = "m3"
        from_member_name = "user"

    _write(args.out, "render_message_direct", fmt.render_message(_Msg()))
    _write(args.out, "render_message_human", fmt.render_message(_Msg(), is_human_agent=True))
    _write(args.out, "render_message_with_body",
           fmt.render_message(_Msg(), body="[框架模板] 请提交计划"))
    _write(args.out, "render_message_user", fmt.render_message(_MsgUser()))
    _write(args.out, "render_messages_batch", fmt.render_messages([_Msg(), _MsgUser()]))

    print("done.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
