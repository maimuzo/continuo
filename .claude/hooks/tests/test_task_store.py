#!/usr/bin/env python3
"""task-store.py と task_common.py が、狙ったとおりに数え、狙ったとおりに書くことを確かめる。

    python3 .claude/hooks/tests/test_task_store.py

**リポジトリのルートから実行すること。**

**なぜこのテストが要るか。**この2つは、次の2件を実際に取り逃していた。

- **出力の数字を全部つないで判定していた。**`internal/v2/config.go:0` は `20` になり、
  **0件が「済」になった**（`counts_from_output` のケース）
- **`r["id"]` のように直接引いていた。**1行でもキーが欠けると `KeyError` で全部使えなくなる
  （「キーが欠けた行があっても落ちない」のケース）

**本物の記録（`.claude/requests/tasks.jsonl`）は触らない。**
`CLAUDE_PROJECT_DIR` に一時ディレクトリを渡して、その中だけで動かす。
"""

import importlib.util
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time

HOOKS = os.path.join(".claude", "hooks")
STORE_CLI = os.path.join(HOOKS, "task-store.py")

ng = 0


def check(name, got, want):
    global ng
    if got == want:
        print("ok  %s" % name)
    else:
        ng += 1
        print("NG  %s\n     期待=%r / 実際=%r" % (name, want, got))


def load_common():
    spec = importlib.util.spec_from_file_location(
        "task_common", os.path.join(HOOKS, "task_common.py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


tc = load_common()


# ---------------------------------------------------------------- 数え方

def verdict(rc, out, err=""):
    ok, _ = tc.verify_result(rc, out, err)
    return ok


print("【確かめ方の判定】")
# **これが取り逃していた本題である。**パスの中の数字を拾って「済」にしていた。
check("grep -rc の 0件（パスに数字がある）は未完了",
      verdict(0, "internal/v2/config.go:0"), False)
check("grep -c の 0 は未完了", verdict(0, "0"), False)
check("終了コードが 1 なら未完了", verdict(1, "0"), False)
check("grep -rc の複数行が全部 0 なら未完了",
      verdict(0, "a/x.go:0\nb/y.go:0"), False)
check("grep -rc の複数行に 1件でもあれば済",
      verdict(0, "a/x.go:0\nb/y.go:3"), True)
check("test -f … && echo 1 は済", verdict(0, "1"), True)
check("出力が空なら未完了", verdict(0, ""), False)
check("件数として読めない出力は、出ていること自体を済とみなす",
      verdict(0, "v0.1.12  Latest"), True)
check("パスに : が混ざっていても末尾の数字を読む",
      verdict(0, "a:b:c:0"), False)
check("件数の行と、そうでない行が混ざったら件数として扱わない",
      verdict(0, "2\nそれらしい説明"), True)

check("counts_from_output は読めない行があれば None",
      tc.counts_from_output("a:0\nほげ"), None)
check("counts_from_output は空なら None", tc.counts_from_output(""), None)
check("counts_from_output は件数の並びを返す",
      tc.counts_from_output("a:0\nb:12"), [0, 12])


# 本物の grep で確かめる。**手で作った文字列だけだと、出力の形を思い違える。**
def real_grep_case():
    d = tempfile.mkdtemp(prefix="task-store-grep-")
    try:
        os.makedirs(os.path.join(d, "internal", "v2"))
        with open(os.path.join(d, "internal", "v2", "config.go"), "w") as f:
            f.write("package v2\n")
        p = subprocess.run("grep -rc WeeklyWaitLimit internal/ || true",
                           shell=True, cwd=d, capture_output=True, text=True)
        return p.stdout.strip(), tc.verify_result(0, p.stdout, p.stderr)[0]
    finally:
        shutil.rmtree(d, ignore_errors=True)


out, ok = real_grep_case()
check("本物の grep -rc（0件）が未完了になる  出力=%r" % out, ok, False)


# ---------------------------------------------------------------- CLI

def cli(root, *args):
    """task-store.py を一時ディレクトリの中で走らせる。"""
    env = dict(os.environ)
    env["CLAUDE_PROJECT_DIR"] = root
    p = subprocess.run([sys.executable, STORE_CLI] + list(args),
                       capture_output=True, text=True, env=env)
    return p.returncode, p.stdout, p.stderr


def rows_of(root):
    path = os.path.join(root, ".claude", "requests", "tasks.jsonl")
    if not os.path.exists(path):
        return []
    return [json.loads(ln) for ln in open(path, encoding="utf-8") if ln.strip()]


def new_root():
    return tempfile.mkdtemp(prefix="task-store-")


print("\n【登録と閉じ方】")
root = new_root()
try:
    rc, so, se = cli(root, "add", "--at", "20260901T0900", "--what", "ひとつめ",
                     "--quote", "原文1", "--verify", "echo 1")
    check("add が通る", rc, 0)
    check("add で1件になる", len(rows_of(root)), 1)
    check("id は 時刻-連番", rows_of(root)[0]["id"], "20260901T0900-1")

    cli(root, "add", "--at", "20260901T0900", "--what", "ふたつめ", "--quote", "原文2")
    check("同じ時刻なら連番が増える", rows_of(root)[1]["id"], "20260901T0900-2")

    rc, so, se = cli(root, "close", "--id", "20260901T0900-1", "--did", "やった")
    check("close が通る", rc, 0)
    r = rows_of(root)[0]
    check("close で done になる", r["status"], "done")
    check("close は closed_at を入れる", bool(r["closed_at"]), True)

    # 確かめ方が通らないものは閉じられない。
    cli(root, "add", "--at", "20260901T0901", "--what", "みっつめ",
        "--verify", "echo internal/v2/config.go:0")
    rc, so, se = cli(root, "close", "--id", "20260901T0901-1", "--did", "やった")
    check("確かめ方が 0件なら閉じられない", rc, 1)
    check("閉じられなかったものは open のまま",
          [x for x in rows_of(root) if x["id"] == "20260901T0901-1"][0]["status"], "open")
finally:
    shutil.rmtree(root, ignore_errors=True)


print("\n【まとめる】")
root = new_root()
try:
    for i, what in enumerate(["A", "B", "C"], start=1):
        cli(root, "add", "--at", "20260901T1000", "--what", what)
    ids = [r["id"] for r in rows_of(root)]

    # **これが取り逃していた本題である。**存在しない id へまとめると、
    # まとめた側だけが消えて、どこにも残らなかった。
    rc, so, se = cli(root, "merge", "--into", "TYPO-999", "--ids", ids[1], ids[2])
    check("存在しない --into は失敗する", rc, 1)
    check("失敗したときは1件も merged にしない",
          [r["status"] for r in rows_of(root)], ["open", "open", "open"])
    check("理由に id を書く", "TYPO-999" in se, True)

    rc, so, se = cli(root, "merge", "--into", ids[0], "--ids", ids[1], "TYPO-999")
    check("--ids に無い id があれば失敗する", rc, 1)
    check("そのときも1件も merged にしない",
          [r["status"] for r in rows_of(root)], ["open", "open", "open"])

    rc, so, se = cli(root, "merge", "--into", ids[0], "--ids", ids[1], ids[2])
    check("実在する --into ならまとめられる", rc, 0)
    after = {r["id"]: r for r in rows_of(root)}
    check("残す側は open のまま", after[ids[0]]["status"], "open")
    check("まとめた側は merged", after[ids[1]]["status"], "merged")
    check("まとめた側にも closed_at が入る", bool(after[ids[2]]["closed_at"]), True)

    # 閉じたものへまとめない（まとめた側も追えなくなる）。
    cli(root, "close", "--id", ids[0], "--did", "やった")
    cli(root, "add", "--at", "20260901T1100", "--what", "D")
    d_id = [r["id"] for r in rows_of(root) if r["what"] == "D"][0]
    rc, so, se = cli(root, "merge", "--into", ids[0], "--ids", d_id)
    check("done へはまとめられない", rc, 1)
    check("そのとき D は open のまま",
          [r for r in rows_of(root) if r["id"] == d_id][0]["status"], "open")
finally:
    shutil.rmtree(root, ignore_errors=True)


print("\n【壊れた記録に耐える】")
root = new_root()
try:
    d = os.path.join(root, ".claude", "requests")
    os.makedirs(d)
    path = os.path.join(d, "tasks.jsonl")
    with open(path, "w", encoding="utf-8") as f:
        # キーが欠けた行・JSON として壊れた行・まともな行を混ぜる。
        f.write(json.dumps({"id": "X-1", "status": "open"}, ensure_ascii=False) + "\n")
        f.write("{壊れている\n")
        f.write(json.dumps({"id": "X-2", "status": "open", "what": "ふつう",
                            "verify": "echo 1", "kind": "once", "quote": "原文"},
                           ensure_ascii=False) + "\n")
    rc, so, se = cli(root, "list")
    check("キーが欠けた行があっても落ちない", rc, 0)
    check("KeyError を出さない", "KeyError" in se, False)
    check("壊れた行を捨てて残りを並べる", "X-1" in so and "X-2" in so, True)
finally:
    shutil.rmtree(root, ignore_errors=True)


print("\n【書き方】")
root = new_root()
try:
    cli(root, "add", "--at", "20260901T1200", "--what", "A")
    d = os.path.join(root, ".claude", "requests")
    path = os.path.join(d, "tasks.jsonl")
    os.chmod(path, 0o640)
    cli(root, "add", "--at", "20260901T1200", "--what", "B")
    check("元の権限を引き継ぐ", oct(os.stat(path).st_mode & 0o777), oct(0o640))
    leftovers = [n for n in os.listdir(d) if n.startswith(".tmp") or n.endswith(".tmp")]
    check("決め打ちの .tmp を残さない", leftovers, [])
    check("ロックファイルを同じディレクトリに置く",
          os.path.exists(os.path.join(d, "tasks.lock")), True)

    # ロックは取って返せる（2回続けて取れる）。
    os.environ["CLAUDE_PROJECT_DIR"] = root
    try:
        with tc.locked(timeout=2):
            pass
        with tc.locked(timeout=2):
            pass
        check("ロックを取って返せる", True, True)
    except Exception as e:  # noqa: BLE001
        check("ロックを取って返せる", repr(e), True)
    finally:
        os.environ.pop("CLAUDE_PROJECT_DIR", None)

    # 別のプロセスが握っている間は待つ（待ちきれなければ TimeoutError）。
    holder_src = (
        "import sys, time\n"
        "sys.path.insert(0, %r)\n"
        "import task_common as tc\n"
        "with tc.locked(timeout=5):\n"
        "    print('held', flush=True)\n"
        "    time.sleep(3)\n" % os.path.abspath(HOOKS)
    )
    holder = subprocess.Popen(
        [sys.executable, "-c", holder_src],
        env=dict(os.environ, CLAUDE_PROJECT_DIR=root),
        stdout=subprocess.PIPE, text=True)
    holder.stdout.readline()
    time.sleep(0.3)
    os.environ["CLAUDE_PROJECT_DIR"] = root
    got = "取れた"
    try:
        with tc.locked(timeout=0.5):
            pass
    except TimeoutError:
        got = "待って諦めた"
    finally:
        os.environ.pop("CLAUDE_PROJECT_DIR", None)
        holder.wait(timeout=10)
    check("ほかが握っている間は取れない", got, "待って諦めた")
finally:
    shutil.rmtree(root, ignore_errors=True)


print("\n合わなかったもの: %d 件" % ng)
sys.exit(1 if ng else 0)
