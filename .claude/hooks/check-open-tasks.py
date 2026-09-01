#!/usr/bin/env python3
"""未完了の指示が残ったまま turn を終えるのを止める。

**AI は忘れる。**割り込みが入ると、前の指示が記憶から薄れる。
**だから毎回突きつける。**

**確かめ方が「済」を返しているものは、閉じ忘れである。**それも知らせる。

入出力
------
- **標準入力**: Claude Code が渡す JSON。使うのは `stop_hook_active` だけ。
  **真なら何もしない**（止めた直後の再実行。無限ループの防止）。
- **標準出力**: 知らせることがあるときだけ `{"decision": "block", "reason": "..."}` を1行。
- **終了コード**: 常に 0。

**なぜ `decision: block` で出すのか。**
**Stop hook が exit 0 で出した標準出力は、人間の画面にしか出ない。**AI の文脈には入らない。
この hook の文面は「閉じてください」「返答の中で状態を書いてください」と
**AI に向けて書いてある。**AI に届かない知らせは無いのと同じである。
`check-verified-commands.py` と同じ形（標準出力の JSON で伝える）に揃えた。

**止まるのは1回きりである。**差し戻された次の実行では `stop_hook_active` が真になり、素通しする。

何を走らせるか
--------------
**`verify` は LLM が書いた文字列である。**記録（`.claude/requests/tasks.jsonl`）は
`.gitignore` 済みのただのテキストで、**turn を終えるたびに、そこに書いてあるものが shell で走る。**

**通してよい形だけを通す**（[task_common.py](task_common.py) の `verify_rejection`）。
読むだけのコマンド（`grep` / `test` / `git` と `gh` の読み取りなど）を
`&&` `||` `|` `;` で繋いだものだけが走る。
**リダイレクト・`$(…)`・backtick・`sh` や `curl` のようなものは走らせない。**

**通らなかったものは、走らせずに「未完了」として並べ、断った理由もそのまま出す。**
黙って飛ばすと、**確かめていないものが確かめたように見える。**

時間
----
**確かめ方には通信を伴うものがある**（`gh release list` など）。
1件ずつ上限を置くだけだと、件数が増えたぶん全体が伸びる。
**全体の予算を決めて、そこで打ち切る**（`TOTAL_BUDGET`）。
打ち切ったものは「未完了」として並べる。**確かめていないものを「済」と言わないため。**
"""
import json, os, sys, time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import task_common as tc  # noqa: E402

# **1回のログインでこれ以上並べない。**全部出すと読まれない。
MAX_SHOW = 12
# **確かめ方の実行に使ってよい合計の秒数。**
# 設定側の `timeout` は他の Stop hook に揃えて10秒を想定している。読み書きと組み立ての分を残す。
TOTAL_BUDGET = 8.0
# **1件に使ってよい上限。**1件が予算を食い尽くすと、残りが1件も確かめられない。
PER_TASK_MAX = 5.0
SKIPPED = "（時間切れで確かめていません）"


def read_payload():
    try:
        raw = sys.stdin.read()
    except Exception:
        return {}
    try:
        payload = json.loads(raw)
    except Exception:
        return {}
    return payload if isinstance(payload, dict) else {}


def is_true(v):
    if isinstance(v, bool):
        return v
    if isinstance(v, str):
        return v.strip().lower() in ("1", "true", "yes")
    return bool(v)


def check(rows, now=time.monotonic):
    """確かめ方を予算の中で実行し、(閉じ忘れ, 未完了) を返す。

    予算を使い切ったら、残りは確かめずに「未完了」へ入れる。
    """
    ready, still = [], []
    deadline = now() + TOTAL_BUDGET
    for r in rows:
        cmd = r.get("verify")
        if not cmd:
            still.append((r, "（確かめ方なし）"))
            continue
        remaining = deadline - now()
        if remaining <= 0.5:
            still.append((r, SKIPPED))
            continue
        ok, out = tc.run_verify(cmd, timeout=min(PER_TASK_MAX, remaining))
        (ready if ok else still).append((r, out))
    return ready, still


def build_message(ready, still):
    lines = []
    if ready:
        lines.append(f"**確かめ方が通っているのに、閉じていないものが {len(ready)} 件あります。**")
        lines.append("**閉じてから終えてください。**")
        lines.append("")
        for r, out in ready[:MAX_SHOW]:
            lines.append(f"  {r.get('id')}  {r.get('what')}")
            lines.append(f"      {r.get('verify')} → {out}")
        if len(ready) > MAX_SHOW:
            lines.append(f"  （ほか {len(ready) - MAX_SHOW} 件）")
        lines.append("")
        lines.append("  閉じ方: python3 .claude/hooks/task-store.py close --id <id> --did \"<何をしたか>\"")
        lines.append("")

    if still:
        lines.append(f"**未完了が {len(still)} 件あります。**")
        lines.append("**この返答で触れていないものは、返答の中で状態を書いてください。**")
        lines.append("")
        for r, out in still[:MAX_SHOW]:
            lines.append(f"  {r.get('id')}  {r.get('what')}")
            lines.append(f"      {r.get('verify') or '確かめ方なし'} → {out}")
        if len(still) > MAX_SHOW:
            lines.append(f"  （ほか {len(still) - MAX_SHOW} 件。`task-store.py list` で全部見られます）")
    return "\n".join(lines)


def main():
    payload = read_payload()
    # 既に止めた結果の再実行なら、そのまま通す（無限ループを避ける）。
    if is_true(payload.get("stop_hook_active")):
        return 0

    rows = [r for r in tc.load()
            if r.get("status") == "open" and r.get("kind") != "standing"]
    if not rows:
        return 0

    ready, still = check(rows)
    message = build_message(ready, still)
    if not message:
        return 0

    print(json.dumps({"decision": "block", "reason": message}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
