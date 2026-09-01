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

# **これも取り逃していた本題である。**標準エラーを「出力」として数えていた。
# `grep -rc x dir/` が読めないディレクトリに当たると、
# **終了コード 0・標準出力は空・標準エラーに警告だけ**という結果になり、
# それを「出力がある」と読んで**0件を「済」にしていた。**
print("\n【標準エラーを判定に混ぜない】")
check("標準出力が空で標準エラーだけなら未完了",
      verdict(0, "", "grep: dir/x: Permission denied"), False)
check("標準出力が空なら、標準エラーが何行あっても未完了",
      verdict(0, "", "warning: a\nwarning: b\nwarning: c"), False)
check("標準出力が 0件なら、標準エラーがあっても未完了",
      verdict(0, "a/x.go:0", "grep: b: Permission denied"), False)
check("標準出力に件数があれば、標準エラーがあっても済",
      verdict(0, "a/x.go:3", "grep: b: Permission denied"), True)
_, shown = tc.verify_result(0, "", "grep: dir/x: Permission denied")
check("標準エラーの中身は見せる", "Permission denied" in shown, True)
_, shown = tc.verify_result(0, "a/x.go:3", "grep: b: Permission denied")
check("済のときも標準エラーを添える",
      "a/x.go:3" in shown and "Permission denied" in shown, True)


# ------------------------------------------------- 確かめ方を実行してよいか
#
# **`verify` は LLM が書いた文字列で、turn を終えるたびに shell で走る。**
# 読むだけの形しか通さない。

print("\n【確かめ方の形】")

PASS_CASES = [
    "grep -c 'ユースケース D' docs/plans/impl/issue142_144_branch_mismatch.md",
    "grep -rc 'WaitingCommentInterval' internal/config/types.go | grep -v '^0$'",
    "grep -rc -- '--resume' internal/orchestrator/ --include='*.go' | grep -v ':0'",
    "test -f .claude/hooks/task-store.py && echo 1",
    "test -f x || true",
    "gh pr view 112 --repo maimuzo/continuo --json state --jq '.state' | grep -c MERGED",
    "gh release list --repo maimuzo/continuo --limit 1 | grep -c 'v0.1.12'",
    "gh api repos/maimuzo/continuo/issues/149/comments --jq '.[].id' | wc -l",
    "git log --oneline -1 | grep -c fix",
    "~/.local/bin/continuo version | grep -c v0.1.11",
    "ls docs | wc -l",
    # 引用符の中の記号は、演算子ではない。**grep の柄として通す。**
    "grep -c '>' docs/FAQ.md",
    "grep -c '|' docs/FAQ.md",
    "grep -c ';' docs/FAQ.md",
]
for c in PASS_CASES:
    check("通す: %s" % c, tc.verify_rejection(c), None)

REJECT_CASES = [
    ("消すもの", "rm -rf /"),
    ("書き込むリダイレクト", "echo x > /tmp/a"),
    ("追記するリダイレクト", "echo x >> /tmp/a"),
    ("読み込むリダイレクト", "cat < /tmp/a"),
    ("バックグラウンド", "grep -c x f &"),
    ("繋いだ先が書き込む", "grep -c x f && rm -rf /"),
    ("`;` の先が書き込む", "grep -c x f ; rm -rf /"),
    ("パイプの先が書き込む", "grep -c x f | tee /tmp/a"),
    ("任意のコードを走らせる shell", "sh -c 'rm -rf /'"),
    ("任意のコードを走らせる python3", "python3 -c 'import os'"),
    ("通信する curl", "curl -s https://example.com"),
    ("コマンド置換", "echo $(whoami)"),
    ("二重引用符の中のコマンド置換", 'echo "$(whoami)"'),
    ("backtick", "echo `id`"),
    ("変数の展開", "echo ${HOME}"),
    ("書き込む git", "git commit -m x"),
    ("送る git", "git push"),
    ("書き込む gh", "gh issue close 1"),
    ("POST する gh api", "gh api repos/o/r -X POST"),
    ("フィールドを送る gh api", "gh api repos/o/r -f a=b"),
    ("消せる find", "find . -delete"),
    ("書き換える sed", "sed -i s/a/b/ f"),
    ("空", ""),
    # **並べて持たずに「繋ぐもの以外の記号」で落としている。**
    # 思いつかなかった綴りを取り逃がさないため。
    ("標準エラーを混ぜる", "grep -c x f 2>&1"),
    ("パイプに標準エラーを乗せる", "grep -c x f |& cat"),
    ("括弧で入れ子にする", "( grep -c x f )"),
    ("知らない綴りの記号", "grep -c x f ;; echo 1"),
]
for name, c in REJECT_CASES:
    check("通さない: %s" % name, tc.verify_rejection(c) is None, False)

# 断るときは、**理由を必ず添える。**「駄目です」だけでは書き直せない。
check("断る理由に、使えない記号を書く",
      "`>`" in (tc.verify_rejection("echo x > /tmp/a") or ""), True)
check("断る理由に、使えないコマンド名を書く",
      "`rm`" in (tc.verify_rejection("rm -rf /") or ""), True)

# 走らせずに断る。**実行してから断ったのでは遅い。**
ok, out = tc.run_verify("echo x > /tmp/continuo-should-not-exist")
check("通らないものは実行せず未完了にする", ok, False)
check("実行していないと書く", "実行していません" in out, True)
check("走らせていない（ファイルができていない）",
      os.path.exists("/tmp/continuo-should-not-exist"), False)


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


print("\n【登録のときに確かめ方の形を見る】")
root = new_root()
try:
    rc, so, se = cli(root, "add", "--at", "20260902T0900", "--what", "危ないもの",
                     "--verify", "echo x > /tmp/continuo-add-should-not-exist")
    check("通らない確かめ方は登録できない", rc, 1)
    check("1件も登録しない", len(rows_of(root)), 0)
    check("断る理由を書く", "`>` は使えません" in se, True)
    check("書き直し方を添える", "grep -c" in se, True)
    check("走らせてもいない",
          os.path.exists("/tmp/continuo-add-should-not-exist"), False)

    rc, so, se = cli(root, "add", "--at", "20260902T0900", "--what", "ふつうのもの",
                     "--verify", "grep -c x docs/FAQ.md")
    check("読むだけの確かめ方なら登録できる", rc, 0)
    check("確かめ方なしでも登録できる",
          cli(root, "add", "--at", "20260902T0901", "--what", "確かめ方なし")[0], 0)
finally:
    shutil.rmtree(root, ignore_errors=True)


print("\n【閉じるとき】")


def load_store():
    """task-store.py を module として読み込む。**`_run_verify` を差し替えるため。**"""
    spec = importlib.util.spec_from_file_location("task_store_cli", STORE_CLI)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


class Args:
    def __init__(self, **kw):
        self.__dict__.update(kw)


root = new_root()
try:
    cli(root, "add", "--at", "20260902T1000", "--what", "A", "--verify", "echo 1")
    tid = rows_of(root)[0]["id"]

    # **ロックを握ったまま確かめ方を走らせない。**
    # 握ったままだと、確かめ方が終わるまで、ほかのセッションはロックを取れない。
    # ロックの取得は10秒で諦めるのに確かめ方は30秒まで走るので、**必ず落ちる側が出る。**
    store = load_store()
    os.environ["CLAUDE_PROJECT_DIR"] = root
    seen = {}

    def spy_verify(cmd, timeout=30):
        try:
            with store.tc.locked(timeout=1.0):
                seen["取れた"] = True
        except TimeoutError:
            seen["取れた"] = False
        return True, "1"

    try:
        store._run_verify = spy_verify
        rc = store.cmd_close(Args(id=tid, did="やった"))
    finally:
        os.environ.pop("CLAUDE_PROJECT_DIR", None)
    check("確かめている間、ほかからロックを取れる", seen.get("取れた"), True)
    check("それでも閉じられる", rc, 0)
    check("閉じたあとは done", rows_of(root)[0]["status"], "done")

    # **閉じたものを上書きしない。**
    rc, so, se = cli(root, "close", "--id", tid, "--did", "べつのこと")
    check("done をもう一度閉じようとすると断る", rc, 1)
    r = rows_of(root)[0]
    check("did を上書きしない", r["did"], "やった")
    check("断る理由に「既に閉じています」と書く", "既に閉じています" in se, True)
finally:
    shutil.rmtree(root, ignore_errors=True)


root = new_root()
try:
    # **`merged` を上書きしない。**上書きすると「どこへまとめたか」が消える。
    cli(root, "add", "--at", "20260902T1100", "--what", "残す")
    cli(root, "add", "--at", "20260902T1100", "--what", "まとめる")
    ids = [r["id"] for r in rows_of(root)]
    cli(root, "merge", "--into", ids[0], "--ids", ids[1])
    check("まとめた側は merged", rows_of(root)[1]["status"], "merged")
    did_before = rows_of(root)[1]["did"]

    rc, so, se = cli(root, "close", "--id", ids[1], "--did", "閉じたつもり")
    check("merged は閉じられない", rc, 1)
    r = rows_of(root)[1]
    check("merged のままにする", r["status"], "merged")
    check("まとめ先を書いた did を消さない", r["did"], did_before)
    check("断る理由にまとめ先を書く", ids[0] in se, True)
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

    # **名前を決め打ちで探さない。**`.tmp` で始まるものだけを探していたので、
    # `tempfile.mkstemp` が付ける名前（`.tasks.jsonl.xxxxxx`）は1つも当たらず、
    # **実装が何を残しても必ず空になっていた。**
    # **残っていてよいものを並べて、それ以外が1つでもあれば落とす。**
    check("書き終えたディレクトリに、余計なファイルを残さない",
          sorted(os.listdir(d)), ["tasks.jsonl", "tasks.lock"])
    check("ロックファイルを同じディレクトリに置く",
          os.path.exists(os.path.join(d, "tasks.lock")), True)

    # 途中で落ちても、一時ファイルを残さず、元の中身も壊さない。
    # **JSON にできない値を1つ混ぜて、書いている最中に落とす。**
    os.environ["CLAUDE_PROJECT_DIR"] = root
    try:
        before_text = open(path, encoding="utf-8").read()
        raised = False
        try:
            tc.save([{"id": "X", "bad": object()}])
        except Exception:  # noqa: BLE001
            raised = True
        check("書けない値なら例外を投げる", raised, True)
        check("落ちても一時ファイルを残さない",
              sorted(os.listdir(d)), ["tasks.jsonl", "tasks.lock"])
        check("落ちても元の中身が残る", open(path, encoding="utf-8").read(), before_text)
    finally:
        os.environ.pop("CLAUDE_PROJECT_DIR", None)

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
