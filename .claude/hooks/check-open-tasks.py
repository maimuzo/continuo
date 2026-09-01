#!/usr/bin/env python3
"""未完了の指示が残ったまま turn を終えるのを止める。

**AI は忘れる。**割り込みが入ると、前の指示が記憶から薄れる。
**だから毎回突きつける。**

**確かめ方が「済」を返しているものは、閉じ忘れである。**それも知らせる。
"""
import json, os, subprocess, sys

ROOT = os.environ.get("CLAUDE_PROJECT_DIR") or os.getcwd()
STORE = os.path.join(ROOT, ".claude", "requests", "tasks.jsonl")
# **1回のログインでこれ以上並べない。**全部出すと読まれない。
MAX_SHOW = 12


def load():
    if not os.path.exists(STORE):
        return []
    rows = []
    with open(STORE, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if line:
                try:
                    rows.append(json.loads(line))
                except json.JSONDecodeError:
                    pass
    return rows


def run_verify(cmd):
    try:
        p = subprocess.run(cmd, shell=True, cwd=ROOT, capture_output=True,
                           text=True, timeout=15)
    except subprocess.TimeoutExpired:
        return False, "（打ち切り）"
    out = (p.stdout or "").strip() or (p.stderr or "").strip()
    if p.returncode != 0:
        return False, out or f"（終了コード {p.returncode}）"
    digits = "".join(ch for ch in out if ch.isdigit())
    if digits:
        return int(digits) > 0, out
    # **出力が空なら「済」にしない。**grep が何も出さないのは「無い」ということである。
    if not out:
        return False, "（出力が空）"
    return True, out


def main():
    try:
        json.load(sys.stdin)
    except Exception:
        pass

    rows = [r for r in load() if r.get("status") == "open" and r.get("kind") != "standing"]
    if not rows:
        sys.exit(0)

    ready, still = [], []
    for r in rows:
        if r.get("verify"):
            ok, out = run_verify(r["verify"])
            (ready if ok else still).append((r, out))
        else:
            still.append((r, "（確かめ方なし）"))

    lines = []
    if ready:
        lines.append(f"**確かめ方が通っているのに、閉じていないものが {len(ready)} 件あります。**")
        lines.append("**閉じてから終えてください。**")
        lines.append("")
        for r, out in ready[:MAX_SHOW]:
            lines.append(f"  {r['id']}  {r['what']}")
            lines.append(f"      {r['verify']} → {out}")
        lines.append("")
        lines.append("  閉じ方: python3 .claude/hooks/task-store.py close --id <id> --did \"<何をしたか>\"")
        lines.append("")

    if still:
        lines.append(f"**未完了が {len(still)} 件あります。**")
        lines.append("**この返答で触れていないものは、返答の中で状態を書いてください。**")
        lines.append("")
        for r, out in still[:MAX_SHOW]:
            lines.append(f"  {r['id']}  {r['what']}")
            lines.append(f"      {r.get('verify') or '確かめ方なし'} → {out}")
        if len(still) > MAX_SHOW:
            lines.append(f"  （ほか {len(still) - MAX_SHOW} 件。`task-store.py list` で全部見られます）")

    # **閉じ忘れがあるときだけ止める。**未完了そのものは、止める理由にしない
    # （まだ着手していない指示が並んでいるだけのことがある）。
    if ready:
        print("\n".join(lines), file=sys.stderr)
        sys.exit(2)

    print("\n".join(lines))
    sys.exit(0)


if __name__ == "__main__":
    main()
