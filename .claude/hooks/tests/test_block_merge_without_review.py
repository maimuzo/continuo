#!/usr/bin/env python3
"""block-merge-without-review.py が、狙った場合に止まり、狙わない場合に通ることを確かめる。

    python3 .claude/hooks/tests/test_block_merge_without_review.py

**リポジトリのルートから実行すること。**

**gh を呼ばずに試す。**`has_review` を差し替えて、レビューの有無を作る。
`current_repo`（このリポジトリの `owner/repo` を返す関数）も差し替えて、
「このリポジトリ」を固定の架空の名前にする。
本物の `gh` を呼ぶと、その日の PR やリポジトリの状態でテストの結果が変わってしまう。
"""

import importlib.util
import io
import json
import os
import re
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
VIEW = "pr view"

# テストの中で「このリポジトリ」とみなす架空の名前と、「別のリポジトリ」の架空の名前。
# 実在のリポジトリ名は使わない（CLAUDE.md「公開してよい情報かを常に判断する」）。
THIS_REPO = "octocat/hello-world"
OTHER_REPO = "example/other-repo"

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
    "ほかのリポジトリ（--repo）は見ない",
    "%s %s --repo %s 188 --merge" % (GH, MERGE, OTHER_REPO),
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

# --- ここから、レビューで見つかった「番号を取り出せない書き方」の穴を塞いだことを確かめる。
# 直す前は、target_prs() がどれも [] を返し、素通りしていた。

case(
    "引用符付きの番号（ダブルクォート）も拾う",
    '%s %s "94"' % (GH, MERGE),
    False,
    True,
    "94",
)
case(
    "引用符付きの番号（シングルクォート）も拾う",
    "%s %s '94'" % (GH, MERGE),
    False,
    True,
    "94",
)
case(
    "このリポジトリを指す --repo は止める",
    "%s %s --repo %s 94" % (GH, MERGE, THIS_REPO),
    False,
    True,
    "94",
)
case(
    "このリポジトリを指す -R も止める",
    "%s %s -R %s 94" % (GH, MERGE, THIS_REPO),
    False,
    True,
    "94",
)
case(
    "このリポジトリを指す -R は、別のリポジトリなら見ない",
    "%s %s -R %s 188" % (GH, MERGE, OTHER_REPO),
    False,
    False,
)
case(
    "PR の URL からも番号を拾う",
    "%s %s https://github.com/%s/pull/94" % (GH, MERGE, THIS_REPO),
    False,
    True,
    "94",
)
case(
    "別のリポジトリを指す URL は見ない",
    "%s %s https://github.com/%s/pull/188" % (GH, MERGE, OTHER_REPO),
    False,
    False,
)
case(
    "値を取るオプションが前に来ても番号を拾う（--body）",
    '%s %s --body "x" 94' % (GH, MERGE),
    False,
    True,
    "94",
)
case(
    "くっついた -R も読む（-Rowner/repo）",
    "%s %s -R%s 94" % (GH, MERGE, THIS_REPO),
    False,
    True,
    "94",
)
case(
    "くっついた -R でも別のリポジトリなら見ない",
    "%s %s -R%s 188" % (GH, MERGE, OTHER_REPO),
    False,
    False,
)

# --- ここから、PR #109 のレビューで見つかった、コマンドの区切りを見ていなかった
# 5件（A群）。直す前は target_prs() が [] か、別の PR の番号を返していた。

case(
    "別の PR を見る（; の手前で区切る）",
    "%s %s 94; %s %s 105" % (GH, MERGE, GH, VIEW),
    False,
    True,
    "94",
)
case(
    "改行の区切り",
    "%s %s 94\n%s %s 95 --undo" % (GH, MERGE, GH, READY),
    False,
    True,
    "94",
)
case(
    "2つ目の呼び出し（別のリポジトリのあとに、自分のリポジトリ）",
    "%s %s --repo %s 1\n%s %s 94" % (GH, MERGE, OTHER_REPO, GH, MERGE),
    False,
    True,
    "94",
)
case(
    "& の区切り",
    "%s %s 94 & %s %s 95 --undo" % (GH, MERGE, GH, READY),
    False,
    True,
    "94",
)
case(
    "括弧付きの gh",
    "(%s %s 94)" % (GH, MERGE),
    False,
    True,
    "94",
)

# --- おまけ: トークン化に変えたことで、シェルのリダイレクト（`2>&1`）の数字を
# PR 番号と誤認しなくなったことも確かめる。
# 直す前の正規表現は、`--help 2>&1` の "2" を PR 番号として拾い、
# 無関係な `gh pr merge --help` まで誤って止めていた（このテストを書く作業中に実際に踏んだ）。
case(
    "--help のあとのリダイレクトは番号ではない",
    "%s %s --help" % (GH, MERGE) + " 2>&1",
    False,
    False,
)


def run_cases(mod):
    ng = 0
    for name, command, review, want_block, want_in in cases:
        # 「逃がし口」のケースだけ、環境変数を置いて試す。
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
    return ng


# --- レビューで見つかった「目印を誰が貼っても通る」穴を塞いだことを確かめる。
# count_trusted_reviews() を直接試す。gh は呼ばない。


def build_review_cases(marker):
    return [
        (
            "OWNER の目印は数える",
            [{"authorAssociation": "OWNER", "body": marker + "\n本文"}],
            1,
        ),
        (
            "MEMBER の目印も数える",
            [{"authorAssociation": "MEMBER", "body": marker}],
            1,
        ),
        (
            "COLLABORATOR の目印も数える",
            [{"authorAssociation": "COLLABORATOR", "body": marker}],
            1,
        ),
        (
            "通りがかりの投稿者（NONE）の目印は数えない",
            [{"authorAssociation": "NONE", "body": marker}],
            0,
        ),
        (
            "CONTRIBUTOR の目印も数えない",
            [{"authorAssociation": "CONTRIBUTOR", "body": marker}],
            0,
        ),
        (
            "目印が無いコメントは数えない",
            [{"authorAssociation": "OWNER", "body": "レビューしました"}],
            0,
        ),
        (
            "信頼できる投稿者と通りがかりが混ざっていれば、信頼できる分だけ数える",
            [
                {"authorAssociation": "NONE", "body": marker},
                {"authorAssociation": "OWNER", "body": marker},
            ],
            1,
        ),
        (
            "authorAssociation が無いコメントは数えない",
            [{"body": marker}],
            0,
        ),
        (
            "目印が本文の途中にあるコメントは数えない",
            [{"authorAssociation": "OWNER", "body": "レビューしました\n" + marker}],
            0,
        ),
        (
            "目印の前に空白・改行があっても、先頭とみなして数える",
            [{"authorAssociation": "OWNER", "body": "  \n" + marker + "\n本文"}],
            1,
        ),
    ]


def run_review_cases(mod):
    ng = 0
    for name, comments, want_count in build_review_cases(mod.MARKER):
        got = mod.count_trusted_reviews(comments)
        if got == want_count:
            print("ok  %s" % name)
        else:
            ng += 1
            print("NG  %s: %d 件（想定は %d 件）" % (name, got, want_count))
    return ng


# --- B群: リポジトリ名の判定を、target_prs() で直接確かめる。
# `run_cases()` とは別に、`current_repo` の返す値を差し替えながら試す。


def run_repo_cases(mod):
    ng = 0

    # 末尾一致（str.endswith）ではなく、`/` 区切りの末尾2つで比べることを確かめる。
    mod.current_repo = lambda: THIS_REPO
    imposter_repo = "my" + THIS_REPO  # 例: "myoctocat/hello-world"
    got = mod.target_prs("%s %s --repo %s 94" % (GH, MERGE, imposter_repo))
    if got == []:
        print("ok  リポジトリ名の境界（末尾一致で誤判定しない）")
    else:
        ng += 1
        print("NG  リポジトリ名の境界: %r（想定は []）" % (got,))

    # このリポジトリの名前が分からないとき、止める側へ倒れることを確かめる。
    # （分からないからと見送ると、`--repo` を付けるだけで検査ごと素通りできてしまう）
    mod.current_repo = lambda: None
    got = mod.target_prs("%s %s --repo %s 94" % (GH, MERGE, OTHER_REPO))
    if got == ["94"]:
        print("ok  リポジトリ名を取れないときは止める側へ倒れる")
    else:
        ng += 1
        print("NG  リポジトリ名を取れないとき: %r（想定は ['94']）" % (got,))

    mod.current_repo = lambda: THIS_REPO
    return ng


# --- C群: 信頼する肩書きの一覧が、scripts/check-release-ready.sh と揃っていることを確かめる。
# Python の集合（TRUSTED_ASSOCIATIONS）と jq の配列に、別々の一覧を書いてしまうと、
# リリース前の検査とマージの検査が食い違う。


def run_associations_sync_case(mod):
    path = os.path.join("scripts", "check-release-ready.sh")
    with open(path, encoding="utf-8") as f:
        text = f.read()
    # review_of() の中にある `["OWNER", "MEMBER", "COLLABORATOR"]` を素朴に取り出す。
    m = re.search(r'\[([^\]]*"OWNER"[^\]]*)\]', text)
    if not m:
        print("NG  scripts/check-release-ready.sh から肩書きの一覧を見つけられない")
        return 1
    found = set(re.findall(r'"([A-Z]+)"', m.group(1)))
    if found == mod.TRUSTED_ASSOCIATIONS:
        print("ok  scripts/check-release-ready.sh の肩書きの一覧が hook 側と揃っている")
        return 0
    print(
        "NG  肩書きの一覧が食い違う: sh=%r python=%r" % (found, mod.TRUSTED_ASSOCIATIONS)
    )
    return 1


def main():
    mod = load_hook()
    # 「このリポジトリ」を、実行環境に依存しない架空の名前へ固定する。
    mod.current_repo = lambda: THIS_REPO

    ng = run_cases(mod)
    ng += run_review_cases(mod)
    ng += run_repo_cases(mod)
    ng += run_associations_sync_case(mod)

    total = len(cases) + len(build_review_cases(mod.MARKER)) + 3
    print("\n%d 件中 %d 件が想定どおり" % (total, total - ng))
    return 1 if ng else 0


if __name__ == "__main__":
    sys.exit(main())
