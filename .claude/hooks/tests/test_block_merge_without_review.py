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
import subprocess
import sys
import tempfile

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

# --- ここから、オーケストレーターが22通り検査して見つかった1件（2026-08-30）。
# バッククォートのコマンド置換がトークンとして割れず、`` `gh `` / ``94` `` のように
# 前後の語にくっついて `_is_gh()` が一致しなかった。直す前は [] を返していた。

case(
    "バッククォートのコマンド置換",
    "`%s %s 94`" % (GH, MERGE),
    False,
    True,
    "94",
)
case(
    "$(...) のコマンド置換（すでに正しく止まっていたことの固定）",
    "$(%s %s 94)" % (GH, MERGE),
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

# --- ここから、PR #109 の2回目のレビューで見つかった、何もしていないのに
# 止めてしまう2件（A群）。直す前は、どちらも誤って止まっていた。

case(
    "heredoc の中身（<<EOF）はコマンドとして読まない",
    "cat > memo.txt <<EOF\n%s %s 94\nEOF\n" % (GH, MERGE),
    False,
    False,
)
case(
    "heredoc の中身（<<'EOF'）はコマンドとして読まない",
    "cat > memo.txt <<'EOF'\n%s %s 94\nEOF\n" % (GH, MERGE),
    False,
    False,
)
case(
    "heredoc の中身（<<\"EOF\"）はコマンドとして読まない",
    'cat > memo.txt <<"EOF"\n%s %s 94\nEOF\n' % (GH, MERGE),
    False,
    False,
)
case(
    "heredoc の中身（<<-EOF、区切り語の前にタブ）はコマンドとして読まない",
    "cat > memo.txt <<-EOF\n\t%s %s 94\n\tEOF\n" % (GH, MERGE),
    False,
    False,
)
case(
    "heredoc の後ろに続く gh pr merge は、引き続き見る",
    "cat > memo.txt <<EOF\n%s %s 94\nEOF\n%s %s 95\n" % (GH, MERGE, GH, MERGE),
    False,
    True,
    "95",
)
case(
    "壊れた引用符の行は、空白区切りに落とさず見送る（誤爆しない）",
    "echo 'it will run %s %s 94 later" % (GH, MERGE),
    False,
    False,
)

# --- ここから、2回目のレビューで見つかった、素直な書き方なのに素通りする5件（B群）
# のうち、コマンド全体で確かめられる4件。直す前は、どれも [] を返していた。

case(
    "branch 名で指すと、番号で指し直すよう求めて止める",
    "%s %s my-branch" % (GH, READY),
    False,
    True,
    "番号で指し直してください",
)
case(
    "-R= の形でもこのリポジトリを指せば止める",
    "%s %s -R=%s 94" % (GH, MERGE, THIS_REPO),
    False,
    True,
    "94",
)
case(
    "-R= の形で別のリポジトリを指せば見ない",
    "%s %s -R=%s 94" % (GH, MERGE, OTHER_REPO),
    False,
    False,
)
case(
    "--repo が他所を指しても、URL がこのリポジトリなら見る（gh は URL を優先する）",
    "%s %s --repo %s https://github.com/%s/pull/94" % (GH, MERGE, OTHER_REPO, THIS_REPO),
    False,
    True,
    "94",
)
case(
    "--repo がこのリポジトリを指しても、URL が他所なら見ない（gh は URL を優先する）",
    "%s %s --repo %s https://github.com/%s/pull/94" % (GH, MERGE, THIS_REPO, OTHER_REPO),
    False,
    False,
)
case(
    "語の途中の # はコメント扱いにしない",
    "curl https://example.com/#x && %s %s 94" % (GH, MERGE),
    False,
    True,
    "94",
)

# --- ここから、実際に踏む3件のうち2件（`<<` の誤認 / `#` の中の branch 名）を確かめる。
# **直す前は、上4件がどれも素通りし、下2件がどちらも誤って止まっていた。**

case(
    "引用符の中の << は heredoc の始まりではない（あとのマージを見落とさない）",
    "echo 'a << b'\n%s %s 94" % (GH, MERGE),
    False,
    True,
    "94",
)
case(
    "<<< は here-string であって heredoc ではない（あとのマージを見落とさない）",
    "echo x <<< WORD\n%s %s 94" % (GH, MERGE),
    False,
    True,
    "94",
)
case(
    "改行が CRLF でも heredoc の区切り語は一致する（あとのマージを見落とさない）",
    "cat > memo.txt <<EOF\r\nhello\r\nEOF\r\n%s %s 94\r\n" % (GH, MERGE),
    False,
    True,
    "94",
)
case(
    "閉じていない heredoc は、残りの行を捨てない",
    "cat > memo.txt <<EOF\nhello\n%s %s 94\n" % (GH, MERGE),
    False,
    True,
    "94",
)
case(
    "行末のコメントの中の gh pr ready は通す",
    "echo hi   # %s %s main de modosu" % (GH, READY),
    False,
    False,
)
case(
    "行頭のコメントの中の gh pr merge は通す",
    "# %s %s 94\necho hi" % (GH, MERGE),
    False,
    False,
)
case(
    "コメントの中の << も heredoc の始まりにはしない",
    "echo hi   # a << b\n%s %s 94" % (GH, MERGE),
    False,
    True,
    "94",
)


def run_cases(mod):
    """(ng, total) を返す。"""
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
    return ng, len(cases)


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
    """(ng, total) を返す。"""
    cases_ = build_review_cases(mod.MARKER)
    ng = 0
    for name, comments, want_count in cases_:
        got = mod.count_trusted_reviews(comments)
        if got == want_count:
            print("ok  %s" % name)
        else:
            ng += 1
            print("NG  %s: %d 件（想定は %d 件）" % (name, got, want_count))
    return ng, len(cases_)


# --- B群: リポジトリ名の判定を、target_prs() で直接確かめる。
# `run_cases()` とは別に、`current_repo` の返す値を差し替えながら試す。


def run_repo_cases(mod):
    """(ng, total) を返す。"""
    ng = 0
    total = 0

    # 末尾一致（str.endswith）ではなく、`/` 区切りの末尾2つで比べることを確かめる。
    mod.current_repo = lambda: THIS_REPO
    imposter_repo = "my" + THIS_REPO  # 例: "myoctocat/hello-world"
    got = mod.target_prs("%s %s --repo %s 94" % (GH, MERGE, imposter_repo))
    total += 1
    if got == []:
        print("ok  リポジトリ名の境界（末尾一致で誤判定しない）")
    else:
        ng += 1
        print("NG  リポジトリ名の境界: %r（想定は []）" % (got,))

    # このリポジトリの名前が分からないとき、止める側へ倒れることを確かめる。
    # （分からないからと見送ると、`--repo` を付けるだけで検査ごと素通りできてしまう）
    mod.current_repo = lambda: None
    got = mod.target_prs("%s %s --repo %s 94" % (GH, MERGE, OTHER_REPO))
    total += 1
    if got == ["94"]:
        print("ok  リポジトリ名を取れないときは止める側へ倒れる")
    else:
        ng += 1
        print("NG  リポジトリ名を取れないとき: %r（想定は ['94']）" % (got,))

    mod.current_repo = lambda: THIS_REPO
    return ng, total


def run_dedupe_case(mod):
    """(ng, total) を返す。

    PR #109 の2回目のレビューで見つかった、同じ PR 番号を2度書くと `gh` へ2度
    問い合わせてしまう穴を確かめる。`target_prs()` が重複を除くことを見る。
    """
    got = mod.target_prs("%s %s 94 && %s %s 94" % (GH, MERGE, GH, MERGE))
    if got == ["94"]:
        print("ok  同じ PR 番号を2度書いても、1回だけ集める")
        return 0, 1
    print("NG  同じ PR 番号の重複除去: %r（想定は ['94']）" % (got,))
    return 1, 1


def run_parse_owner_repo_cases(mod):
    """(ng, total) を返す。

    `_parse_owner_repo()` を直接呼び、`current_repo()` が使う git remote の URL の
    書き方（https / https の `.git` 無し / 末尾スラッシュ / ssh の短縮形 / `ssh://`）を
    正しく読み取れることを確かめる。
    """
    samples = [
        ("https://github.com/%s.git" % THIS_REPO, THIS_REPO),
        ("https://github.com/%s" % THIS_REPO, THIS_REPO),
        ("https://github.com/%s/" % THIS_REPO, THIS_REPO),
        ("git@github.com:%s.git" % THIS_REPO, THIS_REPO),
        ("ssh://git@github.com/%s.git" % THIS_REPO, THIS_REPO),
    ]
    ng = 0
    for url, want in samples:
        got = mod._parse_owner_repo(url)
        if got == want:
            print("ok  _parse_owner_repo(%r) == %r" % (url, want))
        else:
            ng += 1
            print("NG  _parse_owner_repo(%r): %r（想定は %r）" % (url, got, want))
    return ng, len(samples)


def run_current_repo_case(mod, real_current_repo):
    """(ng, total) を返す。

    **PR #109 の2回目のレビューで指摘された「テストが本物を通らない」の1つ目
    （リポジトリ名の切り出し・origin の読み取り）を、実際の `git` で確かめる。**
    以前は `mod.current_repo` を丸ごと差し替えて試しており、`current_repo()` 自身の
    subprocess 呼び出しと `CLAUDE_PROJECT_DIR` の扱いは1度も動いていなかった。

    **`gh` は呼ばない。**`git` は禁止されていない（本番のボード・PR に触れるのは
    `gh` だけである）。実際に `git init` した使い捨てのディレクトリを
    `CLAUDE_PROJECT_DIR` に指定し、`real_current_repo`
    （`load_hook()` 直後に捕まえておいた、差し替え前の本物の `current_repo`）を
    そのまま呼ぶ。
    """
    ng = 0
    total = 0

    def with_project_dir(d):
        old = os.environ.get("CLAUDE_PROJECT_DIR")
        os.environ["CLAUDE_PROJECT_DIR"] = d
        real_current_repo.cache_clear()
        try:
            return real_current_repo()
        finally:
            if old is None:
                os.environ.pop("CLAUDE_PROJECT_DIR", None)
            else:
                os.environ["CLAUDE_PROJECT_DIR"] = old
            real_current_repo.cache_clear()

    with tempfile.TemporaryDirectory() as d:
        subprocess.run(["git", "init", "-q"], cwd=d, check=True)
        subprocess.run(
            ["git", "remote", "add", "origin", "https://github.com/%s.git" % THIS_REPO],
            cwd=d,
            check=True,
        )
        got = with_project_dir(d)
        total += 1
        if got == THIS_REPO:
            print("ok  current_repo() は CLAUDE_PROJECT_DIR のリポジトリの origin を読む")
        else:
            ng += 1
            print("NG  current_repo(): %r（想定は %r）" % (got, THIS_REPO))

    with tempfile.TemporaryDirectory() as d2:
        # git リポジトリではない場所。`git remote get-url origin` が失敗し、None になる。
        got = with_project_dir(d2)
        total += 1
        if got is None:
            print("ok  current_repo() は git リポジトリでない場所では None を返す")
        else:
            ng += 1
            print("NG  current_repo()（git でない場所）: %r（想定は None）" % (got,))

    return ng, total


def run_has_review_json_cases(mod, real_has_review):
    """(ng, total) を返す。

    **PR #109 の2回目のレビューで指摘された「テストが本物を通らない」の2つ目
    （コメントの JSON の読み取り）を確かめる。**以前は `has_review` 自体を
    `lambda pr: review` に差し替えて試しており、`has_review()` 本体の JSON の
    パース・件数の集計・エラー処理（終了コード非0・壊れた JSON・例外）は
    1度も動いていなかった。

    **`gh` は呼ばない。**`subprocess.run`（hook モジュールと同じ singleton）を
    一時的に差し替え、`gh pr view --json comments` が返すはずの文字列を模して渡す。
    `real_has_review`（`load_hook()` 直後に捕まえておいた、差し替え前の本物の
    `has_review`）自身のパース処理を、置き換えずに通す。
    """
    ng = 0
    total = 0
    original_run = subprocess.run

    def check(name, fake_run, want):
        nonlocal ng, total
        total += 1
        subprocess.run = fake_run
        try:
            got = real_has_review("94")
        finally:
            subprocess.run = original_run
        if got == want:
            print("ok  %s" % name)
        else:
            ng += 1
            print("NG  %s: %r（想定は %r）" % (name, got, want))

    def ok_run(stdout):
        def fake(args, **kwargs):
            return subprocess.CompletedProcess(args, 0, stdout=stdout, stderr="")

        return fake

    def fail_run(args, **kwargs):
        return subprocess.CompletedProcess(args, 1, stdout="", stderr="not found")

    def raising_run(args, **kwargs):
        raise OSError("gh が無い")

    check(
        "信頼できる投稿者の目印が有れば True",
        ok_run(json.dumps({"comments": [{"authorAssociation": "OWNER", "body": mod.MARKER}]})),
        True,
    )
    check(
        "目印が無ければ False",
        ok_run(json.dumps({"comments": [{"authorAssociation": "OWNER", "body": "レビューしました"}]})),
        False,
    )
    check("gh が失敗（終了コード非0）したら None", fail_run, None)
    check("壊れた JSON では None", ok_run("not json"), None)
    check("gh の呼び出しで例外が飛んでも None", raising_run, None)

    return ng, total


def run_has_review_target_cases(mod, real_has_review):
    """(ng, total) を返す。

    **`has_review()` が問い合わせる先が、リポジトリ名の判定（`current_repo()`）と
    同じリポジトリを指すことを確かめる。**直す前は、`gh pr view` をいまいる
    ディレクトリで引数なしに呼んでいた。worktree の中で作業していると、
    **`is_this_repo()` は `CLAUDE_PROJECT_DIR` のリポジトリで判定したのに、
    レビューの有無は別のリポジトリの同じ番号の PR を見る**、という食い違いが起きる。

    **`gh` は呼ばない。**`subprocess.run` を差し替えて、渡された引数と `cwd` を見る。
    """
    ng = 0
    total = 0
    original_run = subprocess.run
    seen = {}

    def fake(args, **kwargs):
        seen["args"] = list(args)
        seen["cwd"] = kwargs.get("cwd")
        return subprocess.CompletedProcess(args, 0, stdout=json.dumps({"comments": []}), stderr="")

    old = os.environ.get("CLAUDE_PROJECT_DIR")
    with tempfile.TemporaryDirectory() as d:
        os.environ["CLAUDE_PROJECT_DIR"] = d
        subprocess.run = fake
        try:
            real_has_review("94")
        finally:
            subprocess.run = original_run
            if old is None:
                os.environ.pop("CLAUDE_PROJECT_DIR", None)
            else:
                os.environ["CLAUDE_PROJECT_DIR"] = old

        args = seen.get("args") or []
        total += 1
        if "--repo" in args and args[args.index("--repo") + 1] == THIS_REPO:
            print("ok  has_review() は current_repo() のリポジトリを --repo で明示する")
        else:
            ng += 1
            print("NG  has_review() の引数に --repo %s が無い: %r" % (THIS_REPO, args))

        total += 1
        if seen.get("cwd") == d:
            print("ok  has_review() は CLAUDE_PROJECT_DIR のディレクトリで gh を呼ぶ")
        else:
            ng += 1
            print("NG  has_review() の cwd: %r（想定は %r）" % (seen.get("cwd"), d))

    return ng, total


# --- C群: 信頼する肩書きの一覧が、scripts/check-release-ready.sh と揃っていることを確かめる。
# Python の集合（TRUSTED_ASSOCIATIONS）と jq の配列に、別々の一覧を書いてしまうと、
# リリース前の検査とマージの検査が食い違う。


def run_associations_sync_case(mod):
    """(ng, total) を返す。"""
    path = os.path.join("scripts", "check-release-ready.sh")
    with open(path, encoding="utf-8") as f:
        text = f.read()
    # review_of() の中にある `["OWNER", "MEMBER", "COLLABORATOR"]` を素朴に取り出す。
    m = re.search(r'\[([^\]]*"OWNER"[^\]]*)\]', text)
    if not m:
        print("NG  scripts/check-release-ready.sh から肩書きの一覧を見つけられない")
        return 1, 1
    found = set(re.findall(r'"([A-Z]+)"', m.group(1)))
    if found == mod.TRUSTED_ASSOCIATIONS:
        print("ok  scripts/check-release-ready.sh の肩書きの一覧が hook 側と揃っている")
        return 0, 1
    print(
        "NG  肩書きの一覧が食い違う: sh=%r python=%r" % (found, mod.TRUSTED_ASSOCIATIONS)
    )
    return 1, 1


def run_marker_sync_case(mod):
    """(ng, total) を返す。

    PR #109 の2回目のレビューで指摘された「目印の二重定義」を確かめる。
    `scripts/check-release-ready.sh` の `review_of()` は、目印の文字列と
    「コメントの先頭にあること」の2つを、Python 側とは別に jq の正規表現で
    書いている。**この2つが実際に揃っているかまでは、肩書きの一覧の同期テスト
    （`run_associations_sync_case`）は見ていなかった。**
    """
    path = os.path.join("scripts", "check-release-ready.sh")
    with open(path, encoding="utf-8") as f:
        text = f.read()

    ng = 0
    total = 2

    if mod.MARKER in text:
        print("ok  scripts/check-release-ready.sh に MARKER と同じ文字列がある")
    else:
        ng += 1
        print("NG  scripts/check-release-ready.sh に MARKER の文字列が見つからない: %r" % (mod.MARKER,))

    # jq の `test("^\\s*<MARKER>")` に相当するリテラル（ファイル上は `\` が2つ）を探す。
    # Python 文字列としては `"^\\\\s*"`（`\\\\` が、ファイル中の2文字のバックスラッシュに対応する）。
    anchored = "^\\\\s*" + mod.MARKER
    if anchored in text:
        print("ok  scripts/check-release-ready.sh の判定が、MARKER を先頭に置く条件になっている")
    else:
        ng += 1
        print("NG  scripts/check-release-ready.sh の判定が、MARKER を先頭に置く条件になっていない")

    return ng, total


def main():
    mod = load_hook()
    # 差し替える前に、本物の関数を捕まえておく（`_本物_` 系のテストが使う）。
    real_current_repo = mod.current_repo
    real_has_review = mod.has_review
    # 「このリポジトリ」を、実行環境に依存しない架空の名前へ固定する。
    mod.current_repo = lambda: THIS_REPO

    results = [
        run_cases(mod),
        run_review_cases(mod),
        run_repo_cases(mod),
        run_dedupe_case(mod),
        run_associations_sync_case(mod),
        run_marker_sync_case(mod),
        run_parse_owner_repo_cases(mod),
        run_current_repo_case(mod, real_current_repo),
        run_has_review_json_cases(mod, real_has_review),
        run_has_review_target_cases(mod, real_has_review),
    ]
    ng = sum(n for n, _ in results)
    total = sum(t for _, t in results)

    print("\n%d 件中 %d 件が想定どおり" % (total, total - ng))
    return 1 if ng else 0


if __name__ == "__main__":
    sys.exit(main())
