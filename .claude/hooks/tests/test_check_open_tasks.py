#!/usr/bin/env python3
"""check-open-tasks.py が、狙った場合に止め、狙わない場合に通ることを確かめる。

    python3 .claude/hooks/tests/test_check_open_tasks.py

**リポジトリのルートから実行すること。**

**この hook は Stop で走る。**次の3つは、どれも走らせてみるまで分からない。

- **`stop_hook_active` を見ないと無限ループになる。**止めた直後の再実行で、また止める
- **exit 0 の標準出力は AI に届かない。**`{"decision": "block", ...}` で出す必要がある
- **確かめ方に時間のかかるものが混じると、件数ぶん伸びる。**全体の予算で打ち切る

**本物の記録（`.claude/requests/tasks.jsonl`）は触らない。**
`CLAUDE_PROJECT_DIR` に一時ディレクトリを渡して、その中だけで動かす。
"""

import json
import os
import shutil
import subprocess
import sys
import tempfile
import time

HOOK = os.path.join(".claude", "hooks", "check-open-tasks.py")

ng = 0


def check(name, got, want):
    global ng
    if got == want:
        print("ok  %s" % name)
    else:
        ng += 1
        print("NG  %s\n     期待=%r / 実際=%r" % (name, want, got))


def make_root(rows):
    root = tempfile.mkdtemp(prefix="check-open-tasks-")
    d = os.path.join(root, ".claude", "requests")
    os.makedirs(d)
    with open(os.path.join(d, "tasks.jsonl"), "w", encoding="utf-8") as f:
        for r in rows:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")
    return root


def run(rows, stop_hook_active=False):
    """hook を1回走らせて (終了コード, 止めたか, reason) を返す。"""
    root = make_root(rows)
    try:
        env = dict(os.environ)
        env["CLAUDE_PROJECT_DIR"] = root
        p = subprocess.run(
            [sys.executable, HOOK],
            input=json.dumps({"stop_hook_active": stop_hook_active}),
            capture_output=True, text=True, env=env)
    finally:
        shutil.rmtree(root, ignore_errors=True)
    out = p.stdout.strip()
    if not out:
        return (p.returncode, False, "")
    body = json.loads(out)
    return (p.returncode, body.get("decision") == "block", body.get("reason") or "")


def task(tid, what, verify="", status="open", kind="once"):
    return {"id": tid, "at": tid.split("-")[0], "what": what, "quote": "原文",
            "session": "", "verify": verify, "kind": kind,
            "status": status, "closed_at": "", "did": ""}


print("【止める / 通す】")
rc, blocked, reason = run([])
check("記録が空なら通す", (rc, blocked), (0, False))

rc, blocked, reason = run([task("T-1", "済んだはず", "echo 1")], stop_hook_active=True)
check("stop_hook_active が真なら素通しする（無限ループの防止）",
      (rc, blocked), (0, False))

rc, blocked, reason = run([task("T-1", "済んだはず", "echo 1")])
check("確かめ方が通っているのに open なら止める", blocked, True)
check("終了コードは 0（止める／通すは標準出力で伝える）", rc, 0)
check("止める理由に「閉じ忘れ」の案内が入る", "閉じてから終えてください" in reason, True)
check("止める理由に id が入る", "T-1" in reason, True)
check("閉じ方のコマンドを添える", "task-store.py close" in reason, True)

rc, blocked, reason = run([task("T-2", "まだ途中", "false")])
check("確かめ方が通らないものも AI に届ける", blocked, True)
check("未完了として並べる", "未完了が 1 件あります" in reason, True)

rc, blocked, reason = run([task("T-3", "ずっと効く規則", "echo 1", kind="standing")])
check("standing は数えない", (rc, blocked), (0, False))

rc, blocked, reason = run([task("T-4", "閉じ済み", "echo 1", status="done")])
check("done は数えない", (rc, blocked), (0, False))


print("\n【数え方】")
# **これが取り逃していた本題である。**パスの中の数字を拾って「済」にしていた。
rc, blocked, reason = run([task("T-5", "v2 の設定", "echo internal/v2/config.go:0")])
check("grep -rc の 0件（パスに数字がある）を「済」にしない",
      "閉じてから終えてください" in reason, False)
check("それは未完了として並べる", "未完了が 1 件あります" in reason, True)

rc, blocked, reason = run([task("T-6", "見つかる", "echo internal/v2/config.go:3")])
check("1件でもあれば閉じ忘れとして出す", "閉じてから終えてください" in reason, True)


print("\n【壊れた記録に耐える】")
rc, blocked, reason = run([{"id": "T-7", "status": "open"}])
check("キーが欠けた行があっても落ちない", rc, 0)
check("確かめ方なしとして並べる", "確かめ方なし" in reason, True)


print("\n【通してよい形かを見る】")
# **`verify` は LLM が書いた文字列である。**turn を終えるたびに shell で走るので、
# **通してよい形でないものは走らせない。**走らせないことを、書き込みで確かめる。
mark = os.path.join(tempfile.gettempdir(), "check-open-tasks-should-not-exist-%d" % os.getpid())
if os.path.exists(mark):
    os.unlink(mark)
try:
    rc, blocked, reason = run([task("T-9", "危ない確かめ方", "echo pwned > " + mark)])
    check("書き込むものは実行しない（ファイルができていない）", os.path.exists(mark), False)
    check("実行しなかったことを AI に見せる", "実行していません" in reason, True)
    check("断った理由も見せる", "`>` は使えません" in reason, True)
    check("それは未完了として並べる", "未完了が 1 件あります" in reason, True)
    check("「済」にはしない", "閉じてから終えてください" in reason, False)
finally:
    if os.path.exists(mark):
        os.unlink(mark)

rc, blocked, reason = run([task("T-10", "任意のコード", "python3 -c 'print(1)'")])
check("python3 は実行しない", "実行していません" in reason, True)

rc, blocked, reason = run([task("T-11", "読むだけの確かめ方",
                                "echo 0 | grep -c 0")])
check("読むだけのものは実行する（実行していませんと言わない）",
      "実行していません" in reason, False)


print("\n【時間】")
# 通信を伴う確かめ方を模して、1件 3 秒かかるものを6件並べる。
# 素直に回すと 18 秒。**全体の予算（TOTAL_BUDGET）で打ち切る。**
slow = [task("S-%d" % i, "遅いもの %d" % i, "sleep 3") for i in range(1, 7)]
t0 = time.monotonic()
rc, blocked, reason = run(slow)
elapsed = time.monotonic() - t0
check("全体の予算で打ち切る（12秒以内）  実測=%.1f 秒" % elapsed, elapsed < 12.0, True)
check("打ち切ったものは「済」にしない", "閉じてから終えてください" in reason, False)
check("打ち切ったことを書く", "時間切れで確かめていません" in reason, True)
check("6件とも並べる", "未完了が 6 件あります" in reason, True)


print("\n合わなかったもの: %d 件" % ng)
sys.exit(1 if ng else 0)
