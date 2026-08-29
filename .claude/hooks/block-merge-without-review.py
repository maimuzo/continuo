#!/usr/bin/env python3
"""レビュー結果が貼られていない PR の merge と ready を、実行の前に止める。

**なぜ機械で止めるのか。**[CLAUDE.md](../../CLAUDE.md) に
「PR を出すときは、必ず `/code-review` でレビューする」と書いてあるが、**守られなかった。**

実例（2026-08-29）。人間から「今実装しているものは特別に PR 作ったらそのままマージして良い」
と許可を得た AI が、**それを「ほかの規則も免除された」と読み替え、12本をレビューせずにマージした。**
リリース前の検査が13件を落とし、**マージ後にレビューを回し直すことになった。**
ユーザー指摘: **「以前にもPRをマージしてから別途code-reviewして、無駄に時間とコストをかけていた。根本的な対策を行え」**

**規則は読み替えられる。hook は読み替えられない。**

## 何を見るか

`Bash` の `command` から `gh pr merge` / `gh pr ready` の呼び出しを字句解析で切り出し、
対象の PR 番号を取り出す。**番号は次のどの書き方でも拾う。**

- 素の番号（`gh pr merge 94`）
- 引用符で囲んだ番号（`gh pr merge "94"` / `gh pr merge '94'`）
- 値を取るオプションが前に来た形（`gh pr merge --body "x" 94`）
- PR の URL（`gh pr merge https://github.com/<owner>/<repo>/pull/94`）

拾った番号について、**その PR のコメントの先頭に `<!-- code-review-result -->` があり、
かつ、その投稿者がこのリポジトリの OWNER / MEMBER / COLLABORATOR であるかを `gh` で確かめる。**

**無ければ止める。**あれば通す。

## 止めないもの

- **番号を書かない形**（`gh pr merge` だけ）。現在の branch から引く形は、番号を取れないので見送る
- `gh pr merge --repo <他所>` / `-R <他所>` / 他所を指す PR の URL。
  **`--repo` / `-R` / URL に書かれたリポジトリ名を、実際にこのリポジトリ（`git remote get-url origin` で取れる名前）と比べる。**
  比べるのは `/` で区切った最後の2つ（`HOST/owner/repo` の形を許すため）。
  一致しなければ見ない。一致すれば、番号は他所の値でも意味が無いので、**このリポジトリの番号として扱って止める。**
  **このリポジトリの名前が分からないときも同じく、このリポジトリの番号として扱って止める**
  （分からないからと見送ると、`--repo` を付けるだけで検査ごと素通りできてしまう）。
- `gh pr ready <番号> --undo`。**draft へ戻す操作なので止めない**（`gh pr ready` の呼び出しに限る。
  `gh pr merge` に `--undo` という flag は無い — `gh pr merge -h` で確かめた）
- `gh` が使えない・応答しないとき。**検査そのものが落ちたら通す**（作業を止めない）

## 手で通したいとき

**環境変数 `CONTINUO_ALLOW_UNREVIEWED_MERGE=1` を置く。**
人間が明示的に許した場合だけに使う。**AI が自分で置いてはならない。**
**置けるのは `claude` を起動する前のシェルだけである。**hook は `claude` プロセス自身の環境を読むので、
セッションの中で Bash ツールから `export` しても、別プロセスの環境を変えるだけで届かない
（[CLAUDE.md](../../CLAUDE.md) 参照）。
"""

import functools
import json
import os
import re
import shlex
import subprocess
import sys

# この目印が PR のコメントに1つでもあれば、レビューを通したものとみなす。
# **リリース前の検査（scripts/check-release-ready.sh）が数えるのと同じ目印である。**
# **片方だけ直すと、リリース前の検査とマージの検査が食い違う。両方直すこと。**
MARKER = "<!-- code-review-result -->"

# 目印が付いていても、この投稿者区分でなければ数えない。
# **このリポジトリは PUBLIC である。**通りがかりの人が目印を貼れば恒久的に開いてしまうので、
# オーナー・メンバー・コラボレーターだけを信頼する。
# **scripts/check-release-ready.sh の review_of() にも同じ一覧がある。片方だけ直さないこと。**
TRUSTED_ASSOCIATIONS = {"OWNER", "MEMBER", "COLLABORATOR"}

# 逃がし口。人間が明示的に許したときだけ置く。
ESCAPE_ENV = "CONTINUO_ALLOW_UNREVIEWED_MERGE"

# 外部コマンド（`gh` / `git`）の応答を待つ秒数。**超えたら通す**（検査が落ちて作業を止めない）。
GH_TIMEOUT_SEC = 20

# shlex でトークン化したとき、これらのトークンで「1回の gh 呼び出し」の区切りとみなす。
# `&&` / `;` / `|` / `&` / 改行 / 括弧などで複数のコマンドをつないだときに、
# 次のコマンドの引数を前の gh 呼び出しの引数として誤読しないためである。
#
# **`"\n"` は、ここに書いただけでは働かない。**`shlex.split("a\nb")` は `['a', 'b']` を
# 返し、改行そのものは独立したトークンとして出てこない（2026-08-30 に確認）。
# `_tokenize()` が改行を行の区切りとして扱い、行と行の間に `"\n"` という独立した
# トークンを自分で挟むことで、はじめてここに書いた `"\n"` が意味を持つ。
SEPARATORS = {";", "&&", "||", "|", "&", "\n", "(", ")"}

# gh pr merge / gh pr ready で、次のトークンを値として消費するオプション。
# 値のトークンを PR 番号やリポジトリ名として誤読しないために要る。
# 出典: `gh pr merge --help` / `gh pr ready --help`（2026-08-30 に実行して確認、下記コメントに実物）。
#
#   -A, --author-email text       Email text for merge commit author
#   -b, --body text               Body text for the merge commit
#   -F, --body-file file          Read body text from file
#       --match-head-commit SHA   Commit SHA that the pull request head must match
#   -t, --subject text            Subject text for the merge commit
#   -R, --repo [HOST/]OWNER/REPO  Select another repository
VALUE_FLAGS = {
    "-A", "--author-email",
    "-b", "--body",
    "-F", "--body-file",
    "--match-head-commit",
    "-t", "--subject",
    "-R", "--repo",
}

# このうちリポジトリ名を運ぶもの。
REPO_FLAGS = {"-R", "--repo"}

# PR の URL からリポジトリ名と番号を拾う。
PR_URL_RE = re.compile(r"^https?://github\.com/([^/\s]+/[^/\s]+?)/pull/(\d+)(?:[/?#].*)?$")

REASON = """**レビュー結果が貼られていない PR は、マージも ready もできません。**

PR #{pr} のコメントに、信頼できる投稿者（OWNER / MEMBER / COLLABORATOR）が貼った
`{marker}` が1件もありません。

**先にレビューを通してください。**

    /code-review {pr}

**結果を PR のコメントへ貼ってください。**先頭にこの目印を置くこと。

    {marker}

**なぜ止めているか。**[CLAUDE.md](CLAUDE.md) が「レビュー結果を貼ってあることが、実施したことの唯一の証拠である」と定めています。
**貼っていないものは、リリース前の検査（scripts/check-release-ready.sh）が落とします。**
マージしてからレビューを回し直すと、手戻りが大きくなります。

**人間が明示的に許した場合だけ、環境変数 `{env}=1` を置いて通せます**（`claude` を起動する前のシェルで置くこと）。
"""


def read_payload():
    """標準入力の JSON を読む。読めなければ None。"""
    try:
        raw = sys.stdin.read()
    except Exception:
        return None
    if not raw.strip():
        return None
    try:
        return json.loads(raw)
    except Exception:
        return None


def emit(decision, reason):
    """hook の応答を書き出す。"""
    body = {"hookSpecificOutput": {"hookEventName": "PreToolUse", "permissionDecision": decision}}
    if reason:
        body["hookSpecificOutput"]["permissionDecisionReason"] = reason
    sys.stdout.write(json.dumps(body, ensure_ascii=False))


def _is_gh(token):
    """トークンが gh コマンドそのものかを返す（`/usr/local/bin/gh` のような形も見る）。"""
    return token == "gh" or token.rsplit("/", 1)[-1] == "gh"


def _tokenize(command):
    """command を、shell の区切り演算子も独立した語として持つトークン列にする。

    `shlex.shlex(..., punctuation_chars="();|&")` を使い、`;` `&&` `||` `|` `&` `(` `)`
    を独立したトークンとして切り出す。**`<` `>` は含めない。**含めると `2>&1` のような
    リダイレクトが `2` という独立トークンに割れ、PR 番号と誤認する
    （2026-08-30 に `punctuation_chars=True`〈既定は `();<>|&`〉で試して実際に踏んだ）。

    改行は `shlex` が空白として扱い、独立したトークンにならない。**先に行ごとへ分けて
    から行ごとにトークン化し、行と行の間に `"\\n"` という区切りトークンを自分で挟む。**
    行末が `\\` で終わる行継続は、トークン化の前に1行へつなげる。

    **崩れた引用符で解析に失敗したら、その行だけ素朴な空白区切りへ落ちる。**
    誤って見逃すことはあっても、誤って壊れた解析結果で誤爆はしない。
    """
    joined = re.sub(r"\\\n", " ", command)
    tokens = []
    for i, line in enumerate(joined.split("\n")):
        if i > 0:
            tokens.append("\n")
        try:
            lex = shlex.shlex(line, posix=True, punctuation_chars="();|&")
            lex.whitespace_split = True
            tokens.extend(lex)
        except ValueError:
            tokens.extend(line.split())
    return tokens


def _invocations(tokens):
    """トークン列から `gh pr merge` / `gh pr ready` の呼び出し単位を切り出す。

    戻り値は (サブコマンド, その呼び出しに属する残りのトークン) の list。
    残りのトークンは、次の区切り（`;` `&&` `||` `|` など）の手前までである。
    """
    result = []
    i, n = 0, len(tokens)
    while i < n:
        if (
            i + 2 < n
            and _is_gh(tokens[i])
            and tokens[i + 1] == "pr"
            and tokens[i + 2] in ("merge", "ready")
        ):
            j = i + 3
            seg = []
            while j < n and tokens[j] not in SEPARATORS:
                seg.append(tokens[j])
                j += 1
            result.append((tokens[i + 2], seg))
            i = j
        else:
            i += 1
    return result


def _extract(seg):
    """1回の gh 呼び出し分のトークンから (undo か, リポジトリ名 or None, PR番号 or None) を返す。

    **リポジトリ名は「最初に見つかった値」を採る。**`--repo` / `-R` / URL のどれから来ても、
    2つ目以降に見つかった値では上書きしない。**PR 番号も同じ方針**（`number is None` の
    ガード）。この2つを揃えていないと、`--repo` と URL のどちらが勝つかが書く順番で
    変わってしまう。
    """
    undo = False
    repo = None
    number = None
    k, n = 0, len(seg)
    while k < n:
        tok = seg[k]
        if tok == "--undo":
            undo = True
            k += 1
            continue
        if tok.startswith("--") and "=" in tok:
            name, _, value = tok.partition("=")
            if name in REPO_FLAGS and value:
                repo = repo or value
            k += 1
            continue
        if tok.startswith("-R") and tok != "-R" and not tok.startswith("--"):
            # `-Rowner/repo` のようにくっついた形。gh はこの形を受け付ける。
            value = tok[2:]
            if value:
                repo = repo or value
            k += 1
            continue
        if tok in VALUE_FLAGS:
            value = seg[k + 1] if k + 1 < n else None
            if tok in REPO_FLAGS and value:
                repo = repo or value
            k += 2
            continue
        if tok.startswith("-"):
            # そのほかの真偽値オプション。値を消費しない。
            k += 1
            continue
        m = PR_URL_RE.match(tok)
        if m:
            if number is None:
                repo = repo or m.group(1)
                number = m.group(2)
            k += 1
            continue
        if re.fullmatch(r"\d+", tok):
            if number is None:
                number = tok
            k += 1
            continue
        # 番号でも URL でもない引数（branch 名など）。番号は取れない。
        k += 1
    return undo, repo, number


# git remote の URL から owner/repo を取り出す。対応する形:
#   https://github.com/OWNER/REPO.git / https://github.com/OWNER/REPO
#   git@github.com:OWNER/REPO.git
#   ssh://git@github.com/OWNER/REPO.git
_REMOTE_URL_RE = re.compile(r"[:/](?P<owner>[^/:]+)/(?P<repo>[^/]+?)(?:\.git)?/?$")


def _parse_owner_repo(url):
    """git remote の URL から `owner/repo` を取り出す。分からなければ None。"""
    m = _REMOTE_URL_RE.search(url.strip())
    if not m:
        return None
    return "%s/%s" % (m.group("owner"), m.group("repo"))


@functools.lru_cache(maxsize=1)
def current_repo():
    """このリポジトリの `owner/repo` を返す。分からなければ None。

    **`gh repo view` は使わない。**通信を伴い、実測 0.44 秒かかる
    （2026-08-30 に `time gh repo view --json nameWithOwner --jq .nameWithOwner` で計測）。
    逃がし口（`CONTINUO_ALLOW_UNREVIEWED_MERGE=1`）より前でこれを呼ぶと、通す場合でも
    毎回待たされる。`git remote get-url origin` はローカルの設定を読むだけで、
    通信しない（同じ日に計測して 0.02 秒）。
    """
    try:
        out = subprocess.run(
            ["git", "remote", "get-url", "origin"],
            capture_output=True, text=True, timeout=GH_TIMEOUT_SEC,
        )
    except Exception:
        return None
    if out.returncode != 0:
        return None
    url = out.stdout.strip()
    if not url:
        return None
    return _parse_owner_repo(url)


def _last_two_segments(value):
    """`OWNER/REPO` や `HOST/OWNER/REPO` から、末尾2つの `owner/repo` を返す。分からなければ None。"""
    parts = [p for p in value.strip("/").split("/") if p]
    if len(parts) < 2:
        return None
    return "/".join(parts[-2:]).lower()


def is_this_repo(repo):
    """repo（`--repo` / `-R` / URL から拾った値）がこのリポジトリを指すかを返す。

    **分からなければ「このリポジトリかもしれない」とみなす（True を返す）。**
    `current_repo()` が失敗しただけで `--repo` 付きのマージが検査対象から丸ごと
    外れると、`--repo <なんでもいい値>` を付けるだけで通ってしまう。

    比較は `/` で区切った最後の2つで行う（`HOST/OWNER/REPO` の形を許すため）。
    **末尾一致（`str.endswith`）は使わない。**`myowner/repo` が `owner/repo` に
    文字列として末尾一致してしまい、別のリポジトリを自分だと誤認する。
    """
    current = current_repo()
    if current is None:
        return True
    given = _last_two_segments(repo)
    if given is None:
        return True
    return given == _last_two_segments(current)


def target_prs(command):
    """コマンドから、マージ・ready の対象になる PR 番号を集める。"""
    if "gh" not in command:
        return []
    tokens = _tokenize(command)
    prs = []
    for subcommand, seg in _invocations(tokens):
        undo, repo, number = _extract(seg)
        # **`--undo` は `gh pr ready` にしかない flag である**（`gh pr merge -h` で確認、
        # `--undo` は出てこない）。`pr merge` の呼び出しに紛れ込んでも見送らない。
        if undo and subcommand == "ready":
            continue
        if number is None:
            continue
        if repo is not None and not is_this_repo(repo):
            continue
        prs.append(number)
    return prs


def count_trusted_reviews(comments):
    """コメントの一覧から、信頼できる投稿者が貼った目印の件数を返す。

    comments は `gh pr view --json comments` の `.comments` と同じ形（dict の list）。
    投稿者は `authorAssociation` で判定する。`TRUSTED_ASSOCIATIONS` だけを数える。

    **目印はコメントの先頭にあるものだけを数える。**[CLAUDE.md](../../CLAUDE.md) が
    「コメントの先頭に `<!-- code-review-result -->` を置く」と定めている。本文の途中で
    目印を引用しただけの説明コメント（例:「〇〇のコメントに目印が無い」という指摘そのもの）
    まで数えると、その PR は恒久的にレビュー済み扱いになってしまう。
    """
    count = 0
    for c in comments or []:
        if not isinstance(c, dict):
            continue
        if c.get("authorAssociation") not in TRUSTED_ASSOCIATIONS:
            continue
        body = c.get("body") or ""
        if body.lstrip().startswith(MARKER):
            count += 1
    return count


def has_review(pr):
    """その PR に、信頼できる投稿者によるレビュー結果の目印があるかを返す。確かめられなければ None。"""
    try:
        out = subprocess.run(
            ["gh", "pr", "view", pr, "--json", "comments"],
            capture_output=True, text=True, timeout=GH_TIMEOUT_SEC,
        )
    except Exception:
        return None
    if out.returncode != 0:
        return None
    try:
        comments = json.loads(out.stdout).get("comments") or []
    except Exception:
        return None
    return count_trusted_reviews(comments) > 0


def main():
    payload = read_payload()
    if not payload:
        return 0
    if payload.get("tool_name") != "Bash":
        return 0
    command = (payload.get("tool_input") or {}).get("command") or ""
    if not isinstance(command, str):
        return 0

    # **人間が明示的に許したときは、ここで抜ける。**
    # `target_prs()` は `--repo` の判定で `current_repo()`（`git remote get-url origin`）
    # を呼ぶことがある。逃がし口を先に見ることで、通す場合に無駄な呼び出しをしない。
    if os.environ.get(ESCAPE_ENV) == "1":
        return 0

    prs = target_prs(command)
    if not prs:
        return 0

    for pr in prs:
        ok = has_review(pr)
        # **確かめられなかったら通す。**検査が落ちて作業を止めない。
        if ok is None:
            continue
        if not ok:
            emit("deny", REASON.format(pr=pr, marker=MARKER, env=ESCAPE_ENV))
            return 0

    return 0


if __name__ == "__main__":
    sys.exit(main())
