#!/usr/bin/env python3
"""指示を覚えておく置き場である。

**AI は忘れる。**割り込みが入ると、前の指示が記憶から薄れる。
**だからファイルに持つ。**このスクリプトは、その読み書きだけを担う。

**識別子は「発言の時刻 + その発言の中の連番」にする。**
文面では特定しない。同じ文面のタスク（「原因を説明しろ」など）が
何度も出るためである。

**読み書きと「確かめ方が通ったか」の判定は
[task_common.py](task_common.py) に置いてある。**`check-open-tasks.py` と共有する。
2つに同じコードを持つと、片方だけを直したときに判定が食い違う。
"""
import argparse, os, sys
from datetime import datetime, timezone

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import task_common as tc  # noqa: E402

_load = tc.load
_save = tc.save
_run_verify = tc.run_verify


def _short(text, n):
    """原文を n 文字で切る。**切ったことが分かるように … を付ける。**"""
    t = (text or "").replace("\n", " ").strip()
    if not t:
        return "（原文なし）"
    return t if len(t) <= n else t[:n] + "…"


def _now():
    return datetime.now(timezone.utc).astimezone().isoformat(timespec="seconds")


def cmd_add(a):
    with tc.locked():
        rows = _load()
        same = [r for r in rows if str(r.get("id", "")).startswith(a.at + "-")]
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
    dup = [r for r in rows
           if r.get("what") == a.what and r.get("id") != tid and r.get("status") == "open"]
    print(f"登録: {tid}  {a.what}")
    if dup:
        print(f"  ⚠ 同じ文面が open で {len(dup)} 件あります。**原文を見比べてください。**")
        for d in dup + [row]:
            print(f"    {d.get('id')}  「{_short(d.get('quote'), 100)}」")
        print("    同じ話なら `merge`、別の話ならそのままにしてください。")
    return 0


def cmd_close(a):
    with tc.locked():
        rows = _load()
        hit = [r for r in rows if r.get("id") == a.id]
        if not hit:
            print(f"そんな id はありません: {a.id}", file=sys.stderr)
            return 1
        r = hit[0]
        if not a.did.strip():
            print("--did が空です。**何をしたかを書かないと閉じられません。**", file=sys.stderr)
            return 1
        if r.get("verify"):
            ok, out = _run_verify(r["verify"])
            if not ok:
                print("閉じられません。確かめ方が通りませんでした。", file=sys.stderr)
                print(f"  {r['verify']}", file=sys.stderr)
                print(f"  → {out}", file=sys.stderr)
                return 1
            print(f"確かめました: {r['verify']} → {out}")
        r["status"] = "done"
        r["closed_at"] = _now()
        r["did"] = a.did
        _save(rows)
    print(f"閉じました: {a.id}  {r.get('what')}")
    return 0


def cmd_merge(a):
    """同じ内容のタスクを1つにまとめる。

    **残す側が実在することを先に確かめる。**確かめないと、`--into TYPO-999` と打ったときに
    **まとめる側だけが `merged` になって一覧から消え、どこにも残らない。**
    """
    with tc.locked():
        rows = _load()
        keep = a.into
        by_id = {r.get("id"): r for r in rows}
        if keep not in by_id:
            print(f"--into に指定した id がありません: {keep}", file=sys.stderr)
            print("  `task-store.py list` で実在する id を確かめてください。", file=sys.stderr)
            return 1
        if by_id[keep].get("status") != "open":
            print(f"--into に指定した {keep} は open ではありません"
                  f"（status={by_id[keep].get('status')}）。", file=sys.stderr)
            print("  閉じたタスクへまとめると、まとめた側も追えなくなります。", file=sys.stderr)
            return 1
        ids = set(a.ids)
        missing = sorted(i for i in ids if i not in by_id)
        if missing:
            print(f"--ids に無い id があります: {' '.join(missing)}", file=sys.stderr)
            return 1
        now = _now()
        n = 0
        for r in rows:
            if r.get("id") in ids and r.get("id") != keep:
                r["status"] = "merged"
                r["closed_at"] = now
                r["did"] = f"{keep} にまとめた"
                n += 1
        _save(rows)
    print(f"{n} 件を {keep} にまとめました")
    return 0


def cmd_list(a):
    rows = [r for r in _load() if r.get("status") == "open"]
    if a.kind:
        rows = [r for r in rows if r.get("kind") == a.kind]
    if not rows:
        print("未完了はありません")
        return 0
    print(f"【未完了 {len(rows)} 件】")
    width = 400 if a.full else 140
    for r in rows:
        if r.get("verify"):
            ok, out = _run_verify(r["verify"])
            mark = "済" if ok else "未"
            note = f"確かめ方: {r['verify']} → {out}"
        else:
            mark = "－"
            note = "確かめ方: なし"
        print(f"  [{mark}] {r.get('id')}  {r.get('what')}")
        # **原文を必ず出す。**これが無いと、忘れたときに何のことか分からない。
        print(f"       原文: 「{_short(r.get('quote'), width)}」")
        if r.get("session"):
            print(f"       出典: {r['session']}")
        print(f"       {note}")
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
    m.add_argument("--into", required=True, help="残す id。**実在する open のものだけ**")
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
