#!/usr/bin/env python3
"""block-merge-without-review.py が、狙った場合に止まり、狙わない場合に通ることを確かめる。

    python3 .claude/hooks/tests/test_block_merge_without_review.py

**リポジトリのルートから実行すること。**

**gh を呼ばずに試す。**`has_review` を差し替えて、レビューの有無を作る。
本物の `gh` を呼ぶと、その日の PR の状態でテストの結果が変わってしまう。
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


def run(mod, command, review):
    """hook を1回走らせて (止まったか, 理由) を返す。

    review が True ならレビュー結果が有る、False なら無い、None なら確かめられない。
    """
    mod.has_review = lambda pr: review
    payload = json.dumps({"tool_name": "Bash", "tool_input": {"command": command}})
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

cases = []


def case(name, command, review, want_block, want_in=None):
    cases.append((name, command, review, want_block, want_in))


case("レビュー結果が無ければ止まる", "%s %s 94 --merge" % (GH, MERGE), False, True, "94")
case("レビュー結果が有れば通る", "%s %s 94 --merge" % (GH, MERGE), True, False)
case("ready も止まる", "%s %s 94" % (GH, READY), False, True)
case("ready もレビューが有れば通る", "%s %s 94" % (GH, READY), True, False)
case("確かめられなければ通す", "%s %s 94 --merge" % (GH, MERGE), None, False)
case("番号が無ければ見ない", "%s %s --merge" % (GH, MERGE), False, False)
case("関係ないコマンドは通す", "git status", False, False)
case("似た語を含むだけなら通す", "echo 'merge した'", False, False)
case(
    "ほかのリポジトリは見ない",
    "%s %s --repo octocat/hello-world 188 --merge" % (GH, MERGE),
    False,
    False,
)
case(
    "オプションが挟まっても番号を拾う",
    "%s %s --auto 188" % (GH, MERGE),
    False,
    True,
    "188",
)
case(
    "draft へ戻すのは止めない",
    "%s %s 188 --undo" % (GH, READY),
    False,
    False,
)
case(
    "逃がし口が置かれていれば通す",
    "%s %s 94 --merge" % (GH, MERGE),
    False,
    False,
)


# コメント1件を「レビュー結果」と数えるかの判定。
# **CI（.github/workflows/review-gate.yml）と scripts/check-release-ready.sh と同じ条件である。**
# 3箇所のどれかが緩むと、緩いほうが実質の規則になるので、ここで条件を固定する。
MARKER = "<!-- code-review-result -->"

judge_cases = [
    ("先頭に目印があり OWNER なら数える",
     {"body": MARKER + "\n## code-review の結果", "authorAssociation": "OWNER"}, True),
    ("MEMBER も数える", {"body": MARKER, "authorAssociation": "MEMBER"}, True),
    ("COLLABORATOR も数える", {"body": MARKER, "authorAssociation": "COLLABORATOR"}, True),
    ("先頭の空白は許す",
     {"body": "\n  " + MARKER + "\n本文", "authorAssociation": "OWNER"}, True),
    ("途中に書いたものは数えない",
     {"body": "レビューの話をしました\n" + MARKER, "authorAssociation": "OWNER"}, False),
    ("投稿者が外部なら数えない", {"body": MARKER, "authorAssociation": "NONE"}, False),
    ("CONTRIBUTOR も数えない", {"body": MARKER, "authorAssociation": "CONTRIBUTOR"}, False),
    ("目印が無ければ数えない",
     {"body": "code-review を回しました", "authorAssociation": "OWNER"}, False),
    ("本文が無ければ数えない", {"authorAssociation": "OWNER"}, False),
    ("投稿者が無ければ数えない", {"body": MARKER}, False),
]


# **このリポジトリの `<owner>/<repo>`。**hook の `CONTINUO_HOOK_REPO` へ渡して固定する。
# **固定しないと、`targets_other_repo` が `gh repo view` を叩いて答えを決める。**
# gh に認証が無い CI では引けず、他所を指す `--repo` まで「このリポジトリ」とみなして止めるので、
# **手元では通り CI では落ちる**（2026-09-02 に実測）。
# **文字列はここで組み立てる。**そのまま書くと、この hook 自身がこのファイルを止める。
THIS_REPO = "maimuzo" + "/" + "continuo"


def main():
    mod = load_hook()
    ng = 0

    # **どの段でも `gh` を叩かせない。**pop は最後に1回だけ行う。
    os.environ["CONTINUO_HOOK_REPO"] = THIS_REPO

    for name, comment, want in judge_cases:
        got = mod.counts_as_review(comment)
        if got != want:
            ng += 1
            print("NG  %s: %s（想定は %s）"
                  % (name, "数えた" if got else "数えなかった", "数える" if want else "数えない"))
        else:
            print("ok  %s" % name)

    for i, (name, command, review, want_block, want_in) in enumerate(cases):
        # 最後の1件だけ、逃がし口の環境変数を置いて試す。
        escape = name.startswith("逃がし口")
        if escape:
            os.environ[mod.ESCAPE_ENV] = "1"
        else:
            os.environ.pop(mod.ESCAPE_ENV, None)

        blocked, reason = run(mod, command, review)
        ok = blocked == want_block
        if ok and want_in:
            ok = want_in in reason
        if not ok:
            ng += 1
            got = "止まった" if blocked else "通った"
            want = "止まる" if want_block else "通る"
            print("NG  %s: %s（想定は %s）" % (name, got, want))
        else:
            print("ok  %s" % name)

    os.environ.pop(mod.ESCAPE_ENV, None)

    # **`--repo <このリポジトリ>` は止める。**
    # **2026-09-01、ここが素通しになっていて、9本が hook を通らずにマージされた。**
    # **文字列はここで組み立てる。**そのまま書くと、この hook 自身がこのファイルを止める。
    _g, _r = "gh pr", "--repo"
    repo_cases = [
        ("このリポジトリを指す --repo は止める",
         f"{_g} merge 149 {_r} {THIS_REPO} --merge", ["149"]),
        ("このリポジトリを指す --repo つきの ready も止める",
         f"{_g} ready 149 {_r} {THIS_REPO}", ["149"]),
        ("他所を指す --repo は止めない",
         f"{_g} merge 5 {_r} octocat/hello-world --merge", []),
    ]
    for name, command, want in repo_cases:
        got = mod.target_prs(command)
        if got != want:
            ng += 1
            print("NG  %s: %s（想定は %s）" % (name, got, want))
        else:
            print("ok  %s" % name)

    os.environ.pop("CONTINUO_HOOK_REPO", None)

    # **リポジトリ名を引けないときの倒れ方を、ここで固定する。**
    # `CONTINUO_HOOK_REPO` を外し、`gh repo view` の代わりを差し込む。
    # **環境の gh には触らせない。**触らせると、この2件がまた環境で答えを変える。
    real_repo_of_cwd = mod._repo_of_cwd
    mod._repo_of_cwd = lambda: ""
    try:
        unknown_cases = [
            ("リポジトリ名を引けないときは、他所を指す --repo でも番号を拾う",
             f"{_g} merge 5 {_r} octocat/hello-world --merge", ["5"]),
            ("リポジトリ名を引けなくても、--repo が無ければ今までどおり拾う",
             f"{_g} merge 94 --merge", ["94"]),
        ]
        for name, command, want in unknown_cases:
            got = mod.target_prs(command)
            if got != want:
                ng += 1
                print("NG  %s: %s（想定は %s）" % (name, got, want))
            else:
                print("ok  %s" % name)
    finally:
        mod._repo_of_cwd = real_repo_of_cwd

    total = len(judge_cases) + len(cases) + len(repo_cases) + len(unknown_cases)
    print("\n%d 件中 %d 件が想定どおり" % (total, total - ng))
    return 1 if ng else 0


if __name__ == "__main__":
    sys.exit(main())
