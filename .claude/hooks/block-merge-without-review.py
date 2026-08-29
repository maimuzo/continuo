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
  **`--repo` / `-R` / URL に書かれたリポジトリ名を、実際にこのリポジトリ
  （`CLAUDE_PROJECT_DIR` のディレクトリで `git remote get-url origin` を実行して取れる名前。
  無ければ、いま実行しているディレクトリで実行する）と比べる。**
  **`--repo` / `-R` と URL が両方あるときは、URL の値を優先する**（`gh` 自身がそうする挙動を確認済み。
  でたらめな `--repo` を前に置いて URL の判定を逃れることはできない）。
  比べるのは `/` で区切った最後の2つ（`HOST/owner/repo` の形を許すため）。
  一致しなければ見ない。一致すれば、番号は他所の値でも意味が無いので、**このリポジトリの番号として扱って止める。**
  **このリポジトリの名前が分からないときも同じく、このリポジトリの番号として扱って止める**
  （分からないからと見送ると、`--repo` を付けるだけで検査ごと素通りできてしまう）。
- `gh pr ready <番号> --undo`。**draft へ戻す操作なので止めない**（`gh pr ready` の呼び出しに限る。
  `gh pr merge` に `--undo` という flag は無い — `gh pr merge -h` で確かめた）
- `gh` が使えない・応答しないとき。**検査そのものが落ちたら通す**（作業を止めない）
- **heredoc の中身**（`<<EOF` … `EOF` の間の行）。**実行される文ではなく、ファイルへ書く
  文章そのものである。**中に `gh pr merge 94` という例が出てきても、それは実行されない
  （2026-08-30、レビューで指摘。以前は中身も素朴に読んでいて、文書を書き足すだけで
  誤って止まっていた）。**heredoc とみなすのは、引用符の外にある `<<` で、区切り語が
  ちゃんと閉じている場合だけである**（`<<<` は here-string であって heredoc ではない。
  閉じていないものは heredoc として扱わない）
- **`#` から行末まで**（コメント）。**ただし `#` が語の先頭に来たときだけである。**
  `https://example.com/#x` のような語の途中の `#` はコメントではない

## 番号を特定できない書き方は、念のため止める

**`gh pr ready my-branch` のように、番号でも URL でもない語（branch 名など）を書いた形は、
対象の PR が分からずレビューの有無を確かめられない。**「番号を書かない形」（前項）とは違う
——**何かは書いてあるのに、それを PR として読み取れない**という状態である。
**読み取れないまま通すと検査が無意味になるため、念のため止め、「番号で指し直してください」と案内する。**

## この hook で防げないもの

**この hook は「規則を忘れて、レビューを通さずに素直に `gh pr merge` / `gh pr ready` を
叩いてしまうこと」を止めるためのものである。**「完全に防げる」ものではない。

**文字列の中に隠す形は止められない。**この hook は `Bash` に渡す `command` 文字列を
字句解析するだけで、実際にシェルへ渡して実行するわけではない。次のような形は、
字句解析の段階では中身がただの文字列にしか見えず、`gh pr merge` の呼び出しだと
気づけない。

```
eval "gh pr merge 94"
bash -c "gh pr merge 94"
CMD="gh pr merge"; $CMD 94
```

**そこまで防ぐには、コマンドを実際に実行する側（シェルそのもの、あるいはシェルの
実行結果）を見るしかない。**この hook は `Bash` ツールに渡す文字列を実行の前に見る
`PreToolUse` の仕組みでしかなく、実行結果を見る仕組みではない。

**だから、この hook は意図して迂回しようとする経路を塞ぐものではない。**
規則を忘れた・読み替えた場合の素の呼び出しを止めるのが目的である。

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
# `&&` / `;` / `|` / `&` / 改行 / 括弧 / バッククォートなどで複数のコマンドをつないだときに、
# 次のコマンドの引数を前の gh 呼び出しの引数として誤読しないためである。
#
# **`"\n"` は、ここに書いただけでは働かない。**`shlex.split("a\nb")` は `['a', 'b']` を
# 返し、改行そのものは独立したトークンとして出てこない（2026-08-30 に確認）。
# `_tokenize()` が改行を行の区切りとして扱い、行と行の間に `"\n"` という独立した
# トークンを自分で挟むことで、はじめてここに書いた `"\n"` が意味を持つ。
#
# **バッククォートのコマンド置換（`` `gh pr merge 94` ``）を見つけるための区切りでもある。**
# `$(...)` は `(` `)` が独立トークンになるので元から拾えていたが、バッククォートは
# `punctuation_chars` に含めるまでは1つ前後の語にくっつき（`` `gh `` / ``94` ``）、
# `_is_gh()` が `gh` と一致せず素通りしていた（2026-08-30、オーケストレーターの検査で発覚）。
SEPARATORS = {";", "&&", "||", "|", "&", "\n", "(", ")", "`"}

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

# 「branch 名らしい語」だけを許す形。英数字・`.` `_` `/` `-` のみで、先頭は英数字。
# **これに合わないものは、branch 名として扱わず、素通り（見送る）させる。**
# `<` `>` をトークン化で独立させていないため（`_tokenize()` 参照）、
# `gh pr merge --help 2>&1` のようなコマンドで `2>` という残骸トークンが残ることがある。
# これを branch 名と誤認して止めると、無関係なコマンドまで誤爆する
# （2026-08-30、このテストを書く作業中に実際に踏んだ）。
_BRANCH_LIKE_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._/-]*$")

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

BRANCH_REASON = """**マージ・ready の対象を PR 番号として読み取れませんでした。**

`{token}` は番号でも PR の URL でもありません。**この検査は対象の PR が分からないと、
レビューの有無を確かめられません。**分からないまま通すと検査が無意味になるため、
念のため止めます。

**番号で指し直してください。**

    gh pr merge <PR番号>
    gh pr ready <PR番号>

対象の番号が分からないときは `gh pr list` で確認してください。

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


# heredoc の区切り語を読む正規表現。`<<EOF` `<<'EOF'` `<<"EOF"` `<<-EOF` の4形を扱う。
# 区切り語は `\w+`（英数字とアンダースコア）に限る。実務上の heredoc の区切り語は
# ほぼ全てこの範囲に収まる。
# **`<<` と区切り語の間に置けるのは空白とタブだけにする**（`\s` だと改行まで含んでしまう）。
# **この正規表現は行全体に対して `search` しない。**`_scan_line()` が、引用符の外に
# 見つけた `<<` の位置でだけ `match` する。
_HEREDOC_DELIM_RE = re.compile(r"<<-?[ \t]*(['\"]?)(\w+)\1")

# `#` がコメントの始まりになるのは、語の先頭に来たときだけである。
# 行頭・空白の直後に加えて、これらの文字の直後も語の先頭とみなす。
_WORD_START_AFTER = ";&|()"


def _scan_line(line):
    """1行を先頭から読み、(コメントを落とした行, heredoc の区切り語のリスト) を返す。

    **引用符（`'` `"`）の中と、`\\` で逃がした直後の1文字は、シェルの構文ではなく
    ただの文字として扱う。**この2つを見ないと、次の2つの誤爆が起きる。

    - **`echo 'a << b'` の `<<` を heredoc の始まりだと読む。**区切り語 `b` は
      当然どこにも現れず、その先の行を全部 heredoc の中身として捨ててしまう
      （＝以降の `gh pr merge` が素通りする）
    - **`echo hi   # gh pr ready main` のコメントの中の語を、実行される呼び出しだと読む**

    **`<<<`（here-string）は heredoc ではない。**`echo x <<< WORD` の `WORD` は
    区切り語ではなく、その場で渡される文字列である。**`<<` として読むと、やはり
    区切り語 `WORD` が閉じないまま、その先の行を全部捨ててしまう。**

    区切り語のリストの各要素は `(区切り語, `<<-` か)` である。1行に複数の heredoc を
    書いた形（`cat <<A <<B`）に備えて list で返す。
    """
    out = []
    delims = []
    quote = None  # いま開いている引用符（`'` / `"`）。開いていなければ None
    prev = ""  # 直前に読んだ文字。`#` が語の先頭かどうかの判定に使う
    i, n = 0, len(line)
    while i < n:
        ch = line[i]
        if quote is not None:
            out.append(ch)
            # `"` の中では `\` が次の1文字を逃がす。`'` の中では逃がさない。
            if ch == "\\" and quote == '"' and i + 1 < n:
                out.append(line[i + 1])
                prev = line[i + 1]
                i += 2
                continue
            if ch == quote:
                quote = None
            prev = ch
            i += 1
            continue
        if ch == "\\":
            out.append(ch)
            if i + 1 < n:
                out.append(line[i + 1])
                prev = line[i + 1]
                i += 2
            else:
                i += 1
            continue
        if ch in "'\"":
            quote = ch
            out.append(ch)
            prev = ch
            i += 1
            continue
        if ch == "#" and (prev == "" or prev.isspace() or prev in _WORD_START_AFTER):
            # ここから行末はコメント。**語の途中の `#`（`https://example.com/#x`）は
            # コメントではないので、ここには来ない。**
            break
        if line.startswith("<<<", i):
            # here-string。heredoc ではない。
            out.append("<<<")
            prev = "<"
            i += 3
            continue
        if line.startswith("<<", i):
            m = _HEREDOC_DELIM_RE.match(line, i)
            if m:
                delims.append((m.group(2), line.startswith("<<-", i)))
                out.append(m.group(0))
                prev = m.group(0)[-1]
                i = m.end()
                continue
        out.append(ch)
        prev = ch
        i += 1
    return "".join(out), delims


def _code_lines(lines):
    """行のリストを、実行される部分だけの行のリストにする。

    **各行からコメントを落とし、heredoc の中身の行を取り除く。**

    **heredoc の中身は「実行される文」ではなく「ファイルへ書く文章」である。**
    その中に `gh pr merge 94` という例が出てきても、実行はされない。中身をそのまま
    トークン化すると、文書を書き足しただけで誤って止まる。

    区切り語の行を探すときは、**行末の `\\r` を先に取り除く。**改行が CRLF の
    コマンドでは、行が `EOF\\r` の形になって区切り語と一致せず、heredoc が
    閉じていないものとして扱われてしまう。
    `<<-` のときは、区切り語の行の先頭のタブも取り除いてから比べる
    （bash の `<<-` はタブだけを取り除く。スペースは取り除かない）。

    **区切り語が最後まで見つからなければ（heredoc が閉じていない）、飛ばした行を
    元に戻し、ふつうの行として読み直す。**残りの行を全部捨てると、`<<` を
    heredoc の始まりだと読み違えたときに、その先の `gh pr merge` がまるごと
    素通りしてしまう。**読み違えたときの実害が大きすぎるので、閉じていない
    heredoc は「heredoc ではなかった」として扱う。**
    """
    out = []
    i, n = 0, len(lines)
    while i < n:
        code, delims = _scan_line(lines[i])
        out.append(code)
        i += 1
        if not delims:
            continue
        start = i
        closed = True
        for delim, strip_tabs in delims:
            closed = False
            while i < n:
                body = lines[i].rstrip("\r")
                if strip_tabs:
                    body = body.lstrip("\t")
                i += 1
                if body == delim:
                    closed = True
                    break
            if not closed:
                break
        if not closed:
            # 閉じていない。飛ばした行を捨てず、ふつうの行として読み直す。
            i = start
    return out


def _tokenize(command):
    """command を、shell の区切り演算子も独立した語として持つトークン列にする。

    `shlex.shlex(..., punctuation_chars="();|&\`")` を使い、`;` `&&` `||` `|` `&` `(` `)`
    `` ` `` を独立したトークンとして切り出す。**`<` `>` は含めない。**含めると `2>&1` のような
    リダイレクトが `2` という独立トークンに割れ、PR 番号と誤認する
    （2026-08-30 に `punctuation_chars=True`〈既定は `();<>|&`〉で試して実際に踏んだ）。

    **`commenters` を空にする。**既定の `#` はコメント開始とみなされ、
    `https://example.com/#x` のような語の途中の `#` から後ろが消えてしまう
    （2026-08-30、レビューで指摘。bash は語の途中の `#` をコメントにしない）。
    **語の先頭に来た `#` から行末までは、その前に `_code_lines()` が落としている。**

    改行は `shlex` が空白として扱い、独立したトークンにならない。**先に行ごとへ分けて
    から行ごとにトークン化し、行と行の間に `"\\n"` という区切りトークンを自分で挟む。**
    行末が `\\` で終わる行継続は、トークン化の前に1行へつなげる。**コメントと heredoc の
    中身は、トークン化の前に `_code_lines()` で取り除く。**

    **崩れた引用符で解析に失敗したら、その行は何も拾わない。**空白で素朴に切り直す
    経路はやめた（2026-08-30、レビューで指摘。以前は空白区切りへ落ちており、
    `echo 'it will …` のような閉じていない引用符の行が `gh pr merge 94` という
    語を含んでいるだけで誤って止まっていた）。**誤って見逃すことはあっても、
    誤って壊れた解析結果で誤爆はしない**——この hook の役目は「実害が大きい誤爆を
    避けること」を「見逃しを減らすこと」より優先するためである。
    """
    joined = re.sub(r"\\\n", " ", command)
    lines = _code_lines(joined.split("\n"))
    tokens = []
    for i, line in enumerate(lines):
        if i > 0:
            tokens.append("\n")
        try:
            lex = shlex.shlex(line, posix=True, punctuation_chars="();|&`")
            lex.commenters = ""
            lex.whitespace_split = True
            tokens.extend(lex)
        except ValueError:
            # 崩れた引用符。この行からは何も拾わない（見逃す側へ倒す）。
            pass
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
    """1回の gh 呼び出し分のトークンから
    (undo か, リポジトリ名 or None, PR番号 or None, 番号でもURLでもない語 or None) を返す。

    **リポジトリ名は、URL から読めた値を `--repo` / `-R` から読めた値より優先する。**
    `gh` 自身が、`--repo` / `-R` と PR の URL が両方指定されたとき URL 側を使う
    （2026-08-30、レビューで確かめた挙動: `gh pr merge --repo <他所> <このリポジトリのURL>`
    は URL 側で処理される）。**旧コードは「最初に見つかった値」を採っていたため、
    でたらめな `--repo` を前に置くだけで URL の判定を逃れられた。**
    `--repo` / `-R` どうし、URL どうしでは、それぞれ最初に見つかった値を採る。

    **PR 番号は「最初に見つかった値」を採る**（`number is None` のガード）。

    **番号でも URL でもない語（branch 名など）を見つけたら、最初の1つを覚えておく。**
    呼び出し側（`_pending_refs()`）が、番号が1つも取れなかったときにこれを使う。
    """
    undo = False
    repo_flag = None
    repo_url = None
    number = None
    branch_like = None
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
                repo_flag = repo_flag or value
            k += 1
            continue
        if tok.startswith("-R") and tok != "-R":
            # `-Rowner/repo` / `-R=owner/repo` のようにくっついた形。gh はどちらの形も
            # 受け付ける。**`=` は値に含めない。**（2026-08-30、レビューで指摘。以前は
            # `-R=owner/repo` の `=` が値に残り、どのリポジトリとも一致しなかった）
            value = tok[2:]
            if value.startswith("="):
                value = value[1:]
            if value:
                repo_flag = repo_flag or value
            k += 1
            continue
        if tok in VALUE_FLAGS:
            value = seg[k + 1] if k + 1 < n else None
            if tok in REPO_FLAGS and value:
                repo_flag = repo_flag or value
            k += 2
            continue
        if tok.startswith("-"):
            # そのほかの真偽値オプション。値を消費しない。
            k += 1
            continue
        m = PR_URL_RE.match(tok)
        if m:
            if number is None:
                repo_url = repo_url or m.group(1)
                number = m.group(2)
            k += 1
            continue
        if re.fullmatch(r"\d+", tok):
            if number is None:
                number = tok
            k += 1
            continue
        # 番号でも URL でもない引数。**branch 名らしい語（`_BRANCH_LIKE_RE`）だけを
        # 覚える。**シェルのリダイレクトの残骸（`2>` など）まで拾うと、無関係な
        # コマンドを誤って止めてしまう。それ以外の語は、そのまま見送る。
        if branch_like is None and _BRANCH_LIKE_RE.match(tok):
            branch_like = tok
        k += 1
    repo = repo_url if repo_url is not None else repo_flag
    return undo, repo, number, branch_like


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


def _project_dir():
    """`git` と `gh` を実行するディレクトリを返す。`CLAUDE_PROJECT_DIR` が無ければ None。

    **`current_repo()`（リポジトリ名の判定）と `has_review()`（レビューの問い合わせ）が、
    必ず同じ場所を見るようにするための1箇所である。**None は「いま実行している
    ディレクトリのまま」を意味する（`subprocess.run(cwd=None)`）。
    """
    return os.environ.get("CLAUDE_PROJECT_DIR") or None


@functools.lru_cache(maxsize=1)
def current_repo():
    """このリポジトリの `owner/repo` を返す。分からなければ None。

    **`git remote get-url origin` を、`CLAUDE_PROJECT_DIR`（環境変数）のディレクトリで
    実行する。**それが設定されていなければ、いま実行しているディレクトリで実行する。

    **実行時のディレクトリをそのまま使うと、fork や worktree で外れる。**fork では
    `origin` が自分の複製を指し、上流のリポジトリ名と一致しない。continuo は
    他所のリポジトリの worktree の中で動くので、そこでも実行時のディレクトリが
    対象のリポジトリを指すとは限らない（2026-08-30、レビューで指摘）。
    `CLAUDE_PROJECT_DIR` は、この hook を登録している `.claude/settings.json` 自身が
    `$CLAUDE_PROJECT_DIR/.claude/hooks/block-merge-without-review.py` という形で
    使っている環境変数であり、Claude Code がセッションのプロジェクトルートに設定する。

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
            cwd=_project_dir(),
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


def _pending_refs(command):
    """コマンドから (対象PR番号のリスト, 番号を特定できなかった語) を返す。

    PR 番号のリストは重複を除き、出現順に並べる。**同じ PR 番号を2度書いても、
    `has_review()`（`gh` への問い合わせ）は1回しか呼ばれない**（2026-08-30、
    レビューで指摘。以前は呼び出しのたびに `gh` へ問い合わせていた）。

    2つ目の戻り値（番号を特定できなかった語）は、**PR 番号が1件も取れなかったときだけ**
    呼び出し側（`main()`）が使う。branch 名などで指されたために番号を読み取れなかった
    呼び出しがあれば、その最初の語を返す。
    """
    if "gh" not in command:
        return [], None
    tokens = _tokenize(command)
    numbers = []
    branch_like = None
    for subcommand, seg in _invocations(tokens):
        undo, repo, number, seg_branch_like = _extract(seg)
        # **`--undo` は `gh pr ready` にしかない flag である**（`gh pr merge -h` で確認、
        # `--undo` は出てこない）。`pr merge` の呼び出しに紛れ込んでも見送らない。
        if undo and subcommand == "ready":
            continue
        if repo is not None and not is_this_repo(repo):
            continue
        if number is not None:
            if number not in numbers:
                numbers.append(number)
        elif seg_branch_like is not None and branch_like is None:
            branch_like = seg_branch_like
    return numbers, branch_like


def target_prs(command):
    """コマンドから、マージ・ready の対象になる PR 番号を集める（重複を除く）。"""
    numbers, _ = _pending_refs(command)
    return numbers


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
    """その PR に、信頼できる投稿者によるレビュー結果の目印があるかを返す。確かめられなければ None。

    **どのリポジトリの PR かを、リポジトリ名の判定（`current_repo()`）と必ず揃える。**
    `gh` は、リポジトリを指定しないと「いま実行しているディレクトリ」から判断する。
    continuo は他所のリポジトリの worktree の中で動くので、そのままだと
    **`is_this_repo()` は `CLAUDE_PROJECT_DIR` のリポジトリで判定したのに、
    レビューの有無は別のリポジトリの同じ番号の PR を見る**、という食い違いが起きる。

    揃え方は2つで、両方を行う。

    - `--repo`（`current_repo()` が読めたとき）。**`gh` に明示するのがいちばん強い**
    - `cwd=_project_dir()`。`current_repo()` が読めなかったときの拠り所であり、
      `git remote get-url origin` を実行したのと同じ場所である
    """
    args = ["gh", "pr", "view", pr, "--json", "comments"]
    repo = current_repo()
    if repo:
        args += ["--repo", repo]
    try:
        out = subprocess.run(
            args,
            capture_output=True, text=True, timeout=GH_TIMEOUT_SEC,
            cwd=_project_dir(),
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
    # `_pending_refs()` は `--repo` の判定で `current_repo()`（`git remote get-url origin`）
    # を呼ぶことがある。逃がし口を先に見ることで、通す場合に無駄な呼び出しをしない。
    if os.environ.get(ESCAPE_ENV) == "1":
        return 0

    numbers, branch_like = _pending_refs(command)
    if not numbers:
        # **番号が1件も無いが、branch 名などそれらしい語はあった。**分からないまま
        # 通すと検査が無意味になるため、念のため止める。
        if branch_like is not None:
            emit("deny", BRANCH_REASON.format(token=branch_like, env=ESCAPE_ENV))
        return 0

    for pr in numbers:
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
