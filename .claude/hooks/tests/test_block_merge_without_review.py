#!/usr/bin/env python3
"""block-merge-without-review.py が、狙った場合に止まり、狙わない場合に通ることを確かめる。

    python3 .claude/hooks/tests/test_block_merge_without_review.py

**リポジトリのルートから実行すること。**

**外部コマンドを呼ばない。**この hook は `gh` も `git` も呼ばなくなった。
判定に使うのは `Bash` へ渡す `command` 文字列と、逃がし口の環境変数だけである。

**コマンド文字列は組み立てて作る**（`GH` と `MERGE` を `%s` でつなぐ）。
**1つの文字列にそのまま書くと、この hook 自身がこのファイルの編集を止める。**
"""

import importlib.util
import io
import json
import os
import sys

HOOK = os.path.join(".claude", "hooks", "block-merge-without-review.py")


def load_hook():
    """hook を module として読み込む。"""
    spec = importlib.util.spec_from_file_location("hook", HOOK)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def run(mod, command):
    """hook を1回走らせて (止まったか, 理由) を返す。"""
    payload = json.dumps({"tool_name": "Bash", "tool_input": {"command": command}})
    return _run_payload(mod, payload)


def _run_payload(mod, payload):
    old_stdin, old_stdout = sys.stdin, sys.stdout
    sys.stdin = io.StringIO(payload)
    sys.stdout = io.StringIO()
    try:
        mod.main()
        out = sys.stdout.getvalue()
    finally:
        sys.stdin, sys.stdout = old_stdin, old_stdout
    if not out.strip():
        return (False, "")
    body = json.loads(out)
    h = body.get("hookSpecificOutput") or {}
    return (h.get("permissionDecision") == "deny", h.get("permissionDecisionReason") or "")


# コマンドの断片を組み立てる。**1つの文字列にすると、この hook 自身が止める。**
GH = "gh"
MERGE = "pr merge"
READY = "pr ready"
LIST = "pr list"
VIEW = "pr view"
CREATE = "pr create"

# 架空のリポジトリ名。実在のリポジトリ名は使わない（CLAUDE.md「公開してよい情報かを常に判断する」）。
THIS_REPO = "octocat/hello-world"
OTHER_REPO = "example/other-repo"

cases = []


def case(name, command, want_block):
    cases.append((name, command, want_block))


# --- 止まる場合 -------------------------------------------------------------
# **番号を取り出さないので、どの指し方でも同じように止まる。**

case("番号つきの merge は止まる", "%s %s 94" % (GH, MERGE), True)
case("番号つきの ready は止まる", "%s %s 94" % (GH, READY), True)
case("引数を書かない merge も止まる", "%s %s" % (GH, MERGE), True)
case("引数を書かない ready も止まる", "%s %s" % (GH, READY), True)
case("flag だけの merge も止まる", "%s %s --squash --admin" % (GH, MERGE), True)
case('二重引用符で囲んだ番号でも止まる', '%s %s "94"' % (GH, MERGE), True)
case("単一引用符で囲んだ番号でも止まる", "%s %s '94'" % (GH, MERGE), True)
case(
    "このリポジトリの URL でも止まる",
    "%s %s https://github.com/%s/pull/94" % (GH, MERGE, THIS_REPO),
    True,
)
case(
    "他所のリポジトリの URL でも止まる",
    "%s %s https://github.com/%s/pull/94" % (GH, MERGE, OTHER_REPO),
    True,
)
case("--repo を付けても止まる", "%s %s --repo %s 94" % (GH, MERGE, OTHER_REPO), True)
case("-R をくっつけた形でも止まる", "%s %s -R%s 94" % (GH, MERGE, OTHER_REPO), True)
case(
    "--repo を2回書いても止まる",
    "%s %s --repo %s --repo %s 94" % (GH, MERGE, OTHER_REPO, THIS_REPO),
    True,
)
case("branch 名で指しても止まる", "%s %s my-branch" % (GH, READY), True)
case("番号と branch 名が混ざっても止まる", "%s %s 94 my-branch" % (GH, MERGE), True)
case("フルパスの gh でも止まる", "/usr/local/bin/%s %s 94" % (GH, MERGE), True)
case("環境変数を前に置いても止まる", "FOO=1 %s %s 94" % (GH, READY), True)

# 区切りの先にあっても止まる。
case("; の先でも止まる", "echo hi; %s %s 94" % (GH, MERGE), True)
case("&& の先でも止まる", "git status && %s %s 94" % (GH, MERGE), True)
case("& の先でも止まる", "sleep 1 & %s %s 94" % (GH, MERGE), True)
case("| の先でも止まる", "echo 94 | xargs %s %s" % (GH, MERGE), True)
case("改行の先でも止まる", "echo hi\n%s %s 94" % (GH, MERGE), True)
case("括弧の中でも止まる", "(%s %s 94)" % (GH, MERGE), True)
case("$( ) の中でも止まる", "echo $(%s %s 94)" % (GH, MERGE), True)
case("バッククォートの中でも止まる", "echo `%s %s 94`" % (GH, MERGE), True)
case(
    "--undo が別の断片にあるものは止まる",
    "%s %s 94 --undo && %s %s 95" % (GH, READY, GH, MERGE),
    True,
)
case(
    "heredoc が閉じたあとの行は止まる",
    "cat <<EOF > x.md\n%s %s 94\nEOF\n%s %s 95" % (GH, MERGE, GH, MERGE),
    True,
)

# --- 通る場合 ---------------------------------------------------------------

case("--undo は通る", "%s %s 94 --undo" % (GH, READY), False)
case("--undo が前にあっても通る", "%s %s --undo 94" % (GH, READY), False)
case("--help は通る", "%s %s --help 2>&1" % (GH, MERGE), False)
case("-h は通る", "%s %s -h" % (GH, MERGE), False)
case(
    "heredoc の中の例は通る",
    "cat <<EOF > docs/x.md\n%s %s 94 を叩く\nEOF" % (GH, MERGE),
    False,
)
case(
    "heredoc（単一引用符の区切り語）の中の例は通る",
    "cat <<'EOF' > docs/x.md\n%s %s 94 を叩く\nEOF" % (GH, MERGE),
    False,
)
case(
    "heredoc（二重引用符の区切り語）の中の例は通る",
    'cat <<"EOF" > docs/x.md\n%s %s 94 を叩く\nEOF' % (GH, MERGE),
    False,
)
case(
    "heredoc（<<- でタブを落とす形）の中の例は通る",
    "cat <<-EOF > docs/x.md\n\t%s %s 94 を叩く\n\tEOF" % (GH, MERGE),
    False,
)
case(
    "閉じていない heredoc の中の例は通る",
    "cat <<EOF > docs/x.md\n%s %s 94 を叩く" % (GH, MERGE),
    False,
)
case(
    "引用符が閉じていない行は通る",
    "echo 'このあと %s %s 94 と書く" % (GH, MERGE),
    False,
)
case("pr list は通る", "%s %s --state open" % (GH, LIST), False)
case("pr view は通る", "%s %s 94 --json comments" % (GH, VIEW), False)
case("pr create は通る", "%s %s --draft" % (GH, CREATE), False)
case("git commit は通る", 'git commit -m "何かを直した"', False)
case("gh を含まないコマンドは通る", "git status", False)
case("語の並びが違えば通る", "%s merge pr 94" % (GH,), False)
case("pr だけなら通る", "%s pr" % (GH,), False)
case(
    "引用符でひとつの語にまとめた文字列は通る（防げない範囲）",
    'echo "%s %s 94"' % (GH, MERGE),
    False,
)


def run_cases(mod):
    """(ng, total) を返す。"""
    ng = 0
    for name, command, want_block in cases:
        blocked, _reason = run(mod, command)
        if blocked == want_block:
            print("ok  %s" % name)
        else:
            ng += 1
            print(
                "NG  %s: %s（想定は %s）"
                % (name, "止まった" if blocked else "通った", "止まる" if want_block else "通る")
            )
    return ng, len(cases)


def run_escape_case(mod):
    """逃がし口の環境変数が置いてあれば通ることを確かめる。(ng, total) を返す。"""
    ng = 0
    total = 0
    command = "%s %s 94" % (GH, MERGE)
    old = os.environ.get(mod.ESCAPE_ENV)
    try:
        os.environ[mod.ESCAPE_ENV] = "1"
        blocked, _ = run(mod, command)
        total += 1
        if blocked:
            ng += 1
            print("NG  逃がし口が置いてあるのに止まった")
        else:
            print("ok  逃がし口が置いてあれば通る")

        os.environ[mod.ESCAPE_ENV] = "0"
        blocked, _ = run(mod, command)
        total += 1
        if blocked:
            print("ok  逃がし口の値が 1 以外なら止まる")
        else:
            ng += 1
            print("NG  逃がし口の値が 1 以外なのに通った")
    finally:
        if old is None:
            os.environ.pop(mod.ESCAPE_ENV, None)
        else:
            os.environ[mod.ESCAPE_ENV] = old
    return ng, total


def run_payload_cases(mod):
    """Bash 以外の tool や壊れた入力で止めないことを確かめる。(ng, total) を返す。"""
    ng = 0
    total = 0
    command = "%s %s 94" % (GH, MERGE)

    checks = [
        ("Bash 以外の tool は見ない", json.dumps({"tool_name": "Edit", "tool_input": {"command": command}})),
        ("command が無ければ通る", json.dumps({"tool_name": "Bash", "tool_input": {}})),
        ("command が文字列でなければ通る", json.dumps({"tool_name": "Bash", "tool_input": {"command": 94}})),
        ("壊れた JSON は通る", "{"),
        ("空の入力は通る", ""),
    ]
    for name, payload in checks:
        total += 1
        blocked, _ = _run_payload(mod, payload)
        if blocked:
            ng += 1
            print("NG  %s: 止まった" % name)
        else:
            print("ok  %s" % name)
    return ng, total


def run_reason_cases(mod):
    """止めるときの文言に、人間が次にやることが全部入っているかを確かめる。(ng, total) を返す。"""
    _blocked, reason = run(mod, "%s %s 94" % (GH, MERGE))
    wants = [
        (mod.MARKER, "レビュー結果の目印"),
        (mod.ESCAPE_ENV, "逃がし口の環境変数の名前"),
        ("人間が自分の目で確かめる", "人間が自分の目で確かめること"),
        ("AI が自分でこの環境変数を置いてはなりません", "AI が自分で置いてはならないこと"),
        ("claude` を起動する前のシェル", "置ける場所"),
    ]
    ng = 0
    for needle, label in wants:
        if needle in reason:
            print("ok  止める文言に %s が入っている" % label)
        else:
            ng += 1
            print("NG  止める文言に %s が入っていない" % label)
    return ng, len(wants)


def run_removed_api_cases(mod):
    """番号を取り出す仕組みが本当に消えていることを確かめる。(ng, total) を返す。

    **「消した」と書いておいて残っていると、消したつもりのコードが動き続ける。**
    hook の module に、以下の名前が1つも無いことを見る。
    """
    removed = [
        "current_repo",
        "is_this_repo",
        "_parse_owner_repo",
        "has_review",
        "count_trusted_reviews",
        "target_prs",
        "_pending_refs",
        "_extract",
        "PR_URL_RE",
        "REPO_FLAGS",
        "VALUE_FLAGS",
        "TRUSTED_ASSOCIATIONS",
    ]
    ng = 0
    for name in removed:
        if hasattr(mod, name):
            ng += 1
            print("NG  %s がまだ残っている" % name)
        else:
            print("ok  %s は消えている" % name)
    return ng, len(removed)


def main():
    mod = load_hook()
    results = [
        run_cases(mod),
        run_escape_case(mod),
        run_payload_cases(mod),
        run_reason_cases(mod),
        run_removed_api_cases(mod),
    ]
    ng = sum(n for n, _ in results)
    total = sum(t for _, t in results)

    print("\n%d 件中 %d 件が想定どおり" % (total, total - ng))
    return 1 if ng else 0


if __name__ == "__main__":
    sys.exit(main())
