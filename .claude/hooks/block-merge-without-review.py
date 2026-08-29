#!/usr/bin/env python3
"""PR のマージと ready を、実行の前に一律で止める。

**なぜ機械で止めるのか。**[CLAUDE.md](../../CLAUDE.md) に
「PR を出すときは、必ず `/code-review` でレビューする」と書いてあるが、**守られなかった。**

実例（2026-08-29）。人間から「今実装しているものは特別に PR 作ったらそのままマージして良い」
と許可を得た AI が、**それを「ほかの規則も免除された」と読み替え、12本をレビューせずにマージした。**
リリース前の検査が13件を落とし、**マージ後にレビューを回し直すことになった。**
ユーザー指摘: **「以前にもPRをマージしてから別途code-reviewして、無駄に時間とコストをかけていた。根本的な対策を行え」**

**規則は読み替えられる。hook は読み替えられない。**

## 何を見るか

`Bash` の `command` を語に切り、**`gh` `pr` `merge`（または `ready`）がこの順で語として
並んでいたら止める。それだけである。**

- **PR 番号は取り出さない。**引用符・URL・`--repo`・branch 名のどれで指されていても、
  区別せずに止める
- **その PR にレビュー結果が貼ってあるかは見ない。**`gh` を呼ばない
- **このリポジトリの PR かどうかも見ない**

**なぜ番号を取り出さないのか。**番号を取り出す形は、字句解析の穴を塞ぎ切れなかった。
引用符の中の `<<`・コメントに書いた例・`--repo` を2回書いた形・番号と branch 名が
混ざった形・リポジトリ名の判定と問い合わせ先の食い違いなど、**3回のレビューで45件の
指摘が出て、収束しなかった**（PR #109）。

**止めるだけなら、番号は要らない。**レビュー結果が貼ってあるかどうかは、
**人間が自分の目で確かめ、逃がし口（下記）で通す。**

## 止めないもの

- **`--undo` が同じ断片にあるもの。**draft へ戻す手順そのものだから通す
- **`--help` / `-h` が同じ断片にあるもの。**使い方を表示するだけで、PR は動かない
- **heredoc の中身**（`<<EOF` `<<'EOF'` `<<"EOF"` `<<-EOF` の4形）。**実行される文ではなく、
  ファイルへ書く文章である。**文書にマージの手順を書くだけで止まっては困る
- **語に切れなかった行**（引用符が閉じていないなど）。**解析に失敗したら通す。**
  見逃すことはあっても、壊れた解析結果で誤爆はしない。**この hook は、誤爆の実害を
  見逃しより重く見る**

## この hook で防げないもの

**この hook は「規則を忘れて、レビューを通さずに素直に叩いてしまうこと」を止めるための
ものである。**「完全に防げる」ものではない。

**文字列の中に隠す形は止められない。**この hook は `Bash` に渡す `command` 文字列を
語に切るだけで、実際にシェルへ渡して実行するわけではない。`eval "…"` や
`bash -c "…"`、変数越しの呼び出しは、語に切った段階ではただの文字列にしか見えない。

**そこまで防ぐには、コマンドを実際に実行する側を見るしかない。**この hook は
`PreToolUse` の仕組みでしかない。**だから、意図して迂回しようとする経路は塞がない。**

## 手で通したいとき

**環境変数 `CONTINUO_ALLOW_UNREVIEWED_MERGE=1` を置く。**
人間が明示的に許した場合だけに使う。**AI が自分で置いてはならない。**
**置けるのは `claude` を起動する前のシェルだけである。**hook は `claude` プロセス自身の環境を読むので、
セッションの中で Bash ツールから `export` しても、別プロセスの環境を変えるだけで届かない
（[CLAUDE.md](../../CLAUDE.md) 参照）。
"""

import json
import os
import re
import shlex
import sys

# レビュー結果の目印。**この hook は目印を探さない。**止めるときの文言に載せるだけである。
# 目印を実際に数えるのは、リリース前の検査（scripts/check-release-ready.sh）だけになった。
MARKER = "<!-- code-review-result -->"

# 逃がし口。人間が明示的に許したときだけ置く。
ESCAPE_ENV = "CONTINUO_ALLOW_UNREVIEWED_MERGE"

# 「1回の呼び出し」の区切りとみなすトークン。
# `&&` / `;` / `|` / `&` / 改行 / 括弧 / バッククォートで複数のコマンドをつないだときに、
# 次のコマンドの語を前の呼び出しの語として誤読しないためである。
#
# **`"\n"` は、ここに書いただけでは働かない。**`shlex.split("a\nb")` は `['a', 'b']` を
# 返し、改行そのものは独立したトークンとして出てこない。`_tokenize()` が行ごとに
# 切ってから、行と行の間に `"\n"` という独立したトークンを自分で挟んでいる。
SEPARATORS = {";", "&&", "||", "|", "&", "\n", "(", ")", "`"}

# この語が同じ断片にあれば通す。
# `--undo` は draft へ戻す操作そのもの（`gh pr ready <番号> --undo`）。
# `--help` / `-h` は使い方を表示するだけで、PR を動かさない。
PASS_FLAGS = {"--undo", "--help", "-h"}

# heredoc の開始。`<<EOF` `<<'EOF'` `<<"EOF"` `<<-EOF` の4形を扱う。
# 区切り語は `\w+`（英数字とアンダースコア）に限る。実務上の heredoc の区切り語は
# ほぼ全てこの範囲に収まる。
_HEREDOC_START_RE = re.compile(r"<<-?\s*(['\"]?)(\w+)\1")

REASON = """**PR のマージと ready は、この hook が一律で止めます。**

**PR 番号もレビューの有無も見ていません。**この hook は、レビューを通したかどうかを
判定しません。**確かめるのは人間です。**

**まだレビューを通していないなら、先に通してください。**

    /code-review <PR番号>

**結果は PR のコメントへ貼ってください。**コメントの先頭にこの目印を置くこと。

    {marker}

**すでに通してあるなら、次の順で進めてください。**

1. **その PR に、先頭が `{marker}` のコメントが貼ってあるかを、人間が自分の目で確かめる**
2. **貼ってあったら、`claude` を起動する前のシェルで `{env}=1` を置いてから実行する**
   （`{env}=1 claude` のように起動コマンドの前に置いてもよい）

**AI が自分でこの環境変数を置いてはなりません。**人間が明示的に許したときだけ置くものです。
セッションの中で `export {env}=1` を実行しても、Bash ツールが動かす別プロセスの環境が
変わるだけで、`claude` 本体には届きません（意図した性質です）。

**なぜ止めているか。**[CLAUDE.md](CLAUDE.md) が「レビュー結果を貼ってあることが、実施したことの唯一の証拠である」と定めています。
**貼っていないものは、リリース前の検査（scripts/check-release-ready.sh）が落とします。**
マージしてからレビューを回し直すと、手戻りが大きくなります。
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


def _strip_heredocs(lines):
    """行のリストから、heredoc の中身の行を取り除く。

    **heredoc の中身は「実行される文」ではなく「ファイルへ書く文章」である。**
    その中にマージの例が出てきても、実行はされない。中身をそのまま語に切ると、
    文書を書き足しただけで誤って止まる。

    `<<EOF` `<<'EOF'` `<<"EOF"` `<<-EOF` のいずれかを見つけたら、次の行から
    区切り語が単独で現れる行までを丸ごと飛ばす（区切り語の行自身も飛ばす）。
    `<<-` のときは、区切り語の行の先頭のタブを取り除いてから比べる
    （bash の `<<-` はタブだけを取り除く。スペースは取り除かない）。

    **区切り語が最後まで見つからなければ（heredoc が閉じていない）、残りの行を
    すべて飛ばす。**bash では、閉じていない heredoc はそこから先の入力を
    全部中身として読み続け、実際のコマンドとしては実行されない。
    """
    out = []
    i, n = 0, len(lines)
    while i < n:
        line = lines[i]
        out.append(line)
        m = _HEREDOC_START_RE.search(line)
        if not m:
            i += 1
            continue
        strip_tabs = line[m.start() : m.start() + 3].startswith("<<-")
        delim = m.group(2)
        i += 1
        closed = False
        while i < n:
            body_line = lines[i]
            check = body_line.lstrip("\t") if strip_tabs else body_line
            i += 1
            if check == delim:
                closed = True
                break
        if not closed:
            break
    return out


def _tokenize(command):
    """command を、shell の区切り演算子も独立した語として持つトークン列にする。

    `shlex.shlex(..., punctuation_chars="();|&\\`")` を使い、`;` `&&` `||` `|` `&` `(` `)`
    `` ` `` を独立したトークンとして切り出す。**`<` `>` は含めない。**含めると `2>&1` のような
    リダイレクトが割れて余計なトークンが生まれる。

    **`commenters` を空にする。**既定の `#` はコメント開始とみなされ、
    `https://example.com/#x` のような語の途中の `#` から後ろが消えてしまう
    （bash は語の途中の `#` をコメントにしない）。

    改行は `shlex` が空白として扱い、独立したトークンにならない。**先に行ごとへ分けて
    から行ごとに語へ切り、行と行の間に `"\\n"` という区切りトークンを自分で挟む。**
    行末が `\\` で終わる行継続は、語に切る前に1行へつなげる。**heredoc の中身は、
    語に切る前に `_strip_heredocs()` で取り除く。**

    **崩れた引用符で解析に失敗したら、その行は何も拾わない。**空白で素朴に切り直すことは
    しない。`echo 'it will …` のような閉じていない引用符の行が、マージを指す語を含んで
    いるだけで誤って止まるのを避けるためである。**誤って見逃すことはあっても、
    誤って壊れた解析結果で誤爆はしない。**
    """
    joined = re.sub(r"\\\n", " ", command)
    lines = _strip_heredocs(joined.split("\n"))
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


def _segments(tokens):
    """トークン列から、マージ・ready の呼び出しに属する語の並びを切り出す。

    戻り値は list。要素は、`gh` `pr` `merge`（または `ready`）の直後から、
    次の区切り（`;` `&&` `||` `|` `&` 改行 括弧 バッククォート）の手前までの語である。
    **1つでも見つかれば止める**ので、呼び出し側は中身を `PASS_FLAGS` と照らすだけでよい。
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
            result.append(seg)
            i = j
        else:
            i += 1
    return result


def should_block(command):
    """このコマンドを止めるかを返す。

    **`gh` `pr` `merge`（または `ready`）がこの順で語として並んでいれば止める。**
    ただし、その呼び出しの断片に `PASS_FLAGS`（`--undo` / `--help` / `-h`）があれば止めない。
    """
    if "gh" not in command:
        return False
    for seg in _segments(_tokenize(command)):
        if any(tok in PASS_FLAGS for tok in seg):
            continue
        return True
    return False


def main():
    payload = read_payload()
    if not payload:
        return 0
    if payload.get("tool_name") != "Bash":
        return 0
    command = (payload.get("tool_input") or {}).get("command") or ""
    if not isinstance(command, str):
        return 0

    # **人間が明示的に許したときは、判定より先にここで抜ける。**
    if os.environ.get(ESCAPE_ENV) == "1":
        return 0

    if should_block(command):
        emit("deny", REASON.format(marker=MARKER, env=ESCAPE_ENV))
    return 0


if __name__ == "__main__":
    sys.exit(main())
