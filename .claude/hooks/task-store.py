#!/usr/bin/env python3
"""指示を覚えておく置き場である。

**AI は忘れる。**割り込みが入ると、前の指示が記憶から薄れる。
**だからファイルに持つ。**このスクリプトは、その読み書きだけを担う。

**識別子は「発言の時刻 + その発言の中の連番」にする。**
文面では特定しない。同じ文面のタスク（「原因を説明しろ」など）が
何度も出るためである。
"""
import argparse, json, os, subprocess, sys
from datetime import datetime, timezone

ROOT = os.environ.get("CLAUDE_PROJECT_DIR") or os.getcwd()
DIR = os.path.join(ROOT, ".claude", "requests")
STORE = os.path.join(DIR, "tasks.jsonl")


def _load():
    if not os.path.exists(STORE):
        return []
    out = []
    with open(STORE, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if line:
                try:
                    out.append(json.loads(line))
                except json.JSONDecodeError:
                    pass
    return out


def _save(rows):
    """**その場で空にしてから書かない。**途中で落ちると全部失う。"""
    os.makedirs(DIR, exist_ok=True)
    tmp = STORE + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        for r in rows:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")
    os.replace(tmp, STORE)


def _short(text, n):
    """原文を n 文字で切る。**切ったことが分かるように … を付ける。**"""
    t = (text or "").replace("\n", " ").strip()
    if not t:
        return "（原文なし）"
    return t if len(t) <= n else t[:n] + "…"


def cmd_add(a):
    rows = _load()
    same = [r for r in rows if r["id"].startswith(a.at + "-")]
    tid = f"{a.at}-{len(same) + 1}"
    row = {
        "id": tid,
        "at": a.at,
        "what": a.what,
        # **原文を持つ。**「忘れた原因を説明する」だけでは、何を説明するのか復元できない。
        # **文面は要約であり、要約は文脈を落とす。**落ちた文脈はここにしか無い。
        "quote": a.quote or "",
        "session": a.session or "",
        "verify": a.verify or "",
        "kind": a.kind,
        "status": "open",
        "closed_at": "",
        "did": "",
    }
    rows.append(row)
    _save(rows)
    # **同じ文面が既にあれば知らせる。**まとめるかどうかは人間と AI が決める。
    dup = [r for r in rows if r["what"] == a.what and r["id"] != tid and r["status"] == "open"]
    print(f"登録: {tid}  {a.what}")
    if dup:
        print(f"  ⚠ 同じ文面が open で {len(dup)} 件あります。**原文を見比べてください。**")
        for d in dup + [row]:
            print(f"    {d['id']}  「{_short(d.get('quote'), 100)}」")
        print("    同じ話なら `merge`、別の話ならそのままにしてください。")
    return 0


def _run_verify(cmd):
    """確かめ方のコマンドを実行し、(通ったか, 出力) を返す。"""
    try:
        p = subprocess.run(cmd, shell=True, cwd=ROOT, capture_output=True,
                           text=True, timeout=30)
    except subprocess.TimeoutExpired:
        return False, "（30秒で打ち切りました）"
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


def cmd_close(a):
    rows = _load()
    hit = [r for r in rows if r["id"] == a.id]
    if not hit:
        print(f"そんな id はありません: {a.id}", file=sys.stderr)
        return 1
    r = hit[0]
    if not a.did.strip():
        print("--did が空です。**何をしたかを書かないと閉じられません。**", file=sys.stderr)
        return 1
    if r["verify"]:
        ok, out = _run_verify(r["verify"])
        if not ok:
            print(f"閉じられません。確かめ方が通りませんでした。", file=sys.stderr)
            print(f"  {r['verify']}", file=sys.stderr)
            print(f"  → {out}", file=sys.stderr)
            return 1
        print(f"確かめました: {r['verify']} → {out}")
    r["status"] = "done"
    r["closed_at"] = datetime.now(timezone.utc).astimezone().isoformat(timespec="seconds")
    r["did"] = a.did
    _save(rows)
    print(f"閉じました: {a.id}  {r['what']}")
    return 0


def cmd_merge(a):
    rows = _load()
    keep = a.into
    ids = set(a.ids)
    n = 0
    for r in rows:
        if r["id"] in ids and r["id"] != keep:
            r["status"] = "merged"
            r["did"] = f"{keep} にまとめた"
            n += 1
    _save(rows)
    print(f"{n} 件を {keep} にまとめました")
    return 0


def cmd_list(a):
    rows = [r for r in _load() if r["status"] == "open"]
    if a.kind:
        rows = [r for r in rows if r["kind"] == a.kind]
    if not rows:
        print("未完了はありません")
        return 0
    print(f"【未完了 {len(rows)} 件】")
    width = 400 if a.full else 140
    for r in rows:
        if r["verify"]:
            ok, out = _run_verify(r["verify"])
            mark = "済" if ok else "未"
            note = f"\n       確かめ方: {r['verify']} → {out}"
        else:
            mark = "－"
            note = "\n       確かめ方: なし"
        print(f"  [{mark}] {r['id']}  {r['what']}")
        # **原文を必ず出す。**これが無いと、忘れたときに何のことか分からない。
        print(f"       原文: 「{_short(r.get('quote'), width)}」")
        if r.get("session"):
            print(f"       出典: {r['session']}")
        print(note.lstrip("\n"))
    return 0


def main():
    p = argparse.ArgumentParser(description="指示を覚えておく置き場")
    sub = p.add_subparsers(dest="cmd", required=True)

    a = sub.add_parser("add", help="タスクを登録する")
    a.add_argument("--at", required=True, help="元の発言の時刻（例: 20260901T0839）")
    a.add_argument("--what", required=True, help="追跡すべきタスク")
    a.add_argument("--quote", default="",
                   help="**その指示が出た発言の原文。**要約せずそのまま入れる。"
                        "同じ文面のタスクを見分ける唯一の手がかりになる")
    a.add_argument("--session", default="",
                   help="発言が入っているセッションの jsonl のパス。原文を辿り直すため")
    a.add_argument("--verify", default="", help="完了を確かめるコマンド。無ければ省略")
    a.add_argument("--kind", default="once", choices=["once", "standing"],
                   help="once=1回で終わる / standing=ずっと効く規則")
    a.set_defaults(func=cmd_add)

    c = sub.add_parser("close", help="タスクを閉じる。確かめ方が通らなければ閉じない")
    c.add_argument("--id", required=True)
    c.add_argument("--did", required=True, help="何をしたか。空なら閉じられない")
    c.set_defaults(func=cmd_close)

    m = sub.add_parser("merge", help="同じ内容のタスクを1つにまとめる")
    m.add_argument("--into", required=True, help="残す id")
    m.add_argument("--ids", nargs="+", required=True, help="まとめる id の並び")
    m.set_defaults(func=cmd_merge)

    l = sub.add_parser("list", help="未完了を並べる。確かめ方も実行する")
    l.add_argument("--kind", choices=["once", "standing"])
    l.add_argument("--full", action="store_true", help="原文を切らずに長く出す")
    l.set_defaults(func=cmd_list)

    args = p.parse_args()
    sys.exit(args.func(args))


if __name__ == "__main__":
    main()
