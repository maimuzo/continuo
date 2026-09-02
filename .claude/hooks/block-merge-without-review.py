#!/usr/bin/env python3
r"""レビュー結果が貼られていない PR の merge と ready を、実行の前に止める。

**なぜ機械で止めるのか。**[CLAUDE.md](../../CLAUDE.md) に
「PR を出すときは、必ず `/code-review` でレビューする」と書いてあるが、**守られなかった。**

実例（2026-08-29）。人間から「今実装しているものは特別に PR 作ったらそのままマージして良い」
と許可を得た AI が、**それを「ほかの規則も免除された」と読み替え、12本をレビューせずにマージした。**
リリース前の検査が13件を落とし、**マージ後にレビューを回し直すことになった。**
ユーザー指摘: **「以前にもPRをマージしてから別途code-reviewして、無駄に時間とコストをかけていた。根本的な対策を行え」**

**規則は読み替えられる。hook は読み替えられない。**

## 何を見るか

`Bash` の `command` に次のどちらかが含まれていたら、対象の PR 番号を取り出し、
**その PR にレビュー結果のコメントがあるかを `gh` で確かめる。**

- `gh pr merge <番号>`
- `gh pr ready <番号>`

**無ければ止める。**あれば通す。

## 何を「レビュー結果」と数えるか

**次の2つを両方満たすコメントだけを数える。**

1. **本文の先頭に `<!-- code-review-result -->` がある**（前に空白文字があってもよい）。
   **途中に書いたものは数えない。**「レビューの話をしただけ」のコメントで通ってしまう
2. **投稿者が `OWNER` / `MEMBER` / `COLLABORATOR` のいずれかである。**
   **誰でもコメントできるので、外部の人が目印を貼れば通る状態にしない**

**この条件は3箇所で同じにしてある。**片方だけ緩いと、緩いほうが実質の規則になる。

| どこ | 何を止めるか |
| --- | --- |
| このファイル | AI の手元の `gh pr merge` / `gh pr ready` |
| [.github/workflows/review-gate.yml](../../.github/workflows/review-gate.yml) | PR のマージ（branch protection の必須の検査） |
| [scripts/check-release-ready.sh](../../scripts/check-release-ready.sh) | タグを打つこと |

**「前の空白文字」に何を含めるかも、3箇所で同じにしてある。**
半角空白・タブ・CR・LF の4つだけで、**全角空白 U+3000 や NBSP U+00A0 は含めない。**
`\s` は Python の `re` と jq（Oniguruma）で当たる範囲が違うので使わない（下の `MARKER_SPACE_CLASS`）。
**3箇所が同じであることは [.claude/hooks/tests/test_marker_pattern_parity.py](tests/test_marker_pattern_parity.py) が押さえる。**

## 止めないもの

- **番号を書かない形**（`gh pr merge` だけ）。現在の branch から引く形は、番号を取れないので見送る
- `gh pr merge --repo <他所>`。**このリポジトリの PR でなければ見ない**
  （**このリポジトリを指す `--repo` は止める。**2026-09-01、`--repo <このリポジトリ>` を
  付けたマージが9本とも素通しになっていた）
- `gh` が使えない・応答しないとき。**検査そのものが落ちたら通す**（作業を止めない）

## 手で通したいとき

**環境変数 `CONTINUO_ALLOW_UNREVIEWED_MERGE=1` を置く。**
人間が明示的に許した場合だけに使う。**AI が自分で置いてはならない。**
"""

import json
import os
import re
import subprocess
import sys

# 実行の前に見る Bash のコマンド。番号を取り出す。
# **`--undo` は止めない。**CLAUDE.md が「draft へ戻してからレビューする」と定めた手順そのものだからである。
# **`--repo <他所>` は止めない。**このリポジトリの PR ではないので、番号を当てても意味が無い。
#
# **ただし `--repo <このリポジトリ>` は止める。**
# **2026-09-01、`--repo` が付いていれば無条件に素通ししていたため、
# `gh pr merge <番号> --repo <このリポジトリ>` で9本が hook を通らずにマージされた。**
# **`--repo` を付けるのは、むしろこのリポジトリを明示する普通の書き方である。**
#
# **番号の前に来てよいのはオプションだけにする。**`--repo other/repo 123` の 123 を拾わないため。
MERGE_RE = re.compile(
    r"\bgh\s+pr\s+(?:merge|ready)\s+"
    r"(?!(?:[-\w=./:]+\s+)*?--undo\b)"
    r"(?:--?[-\w]+(?:[= ][-\w./:]+)?\s+)*?"
    r"(\d+)\b"
)

# **`--repo` が指しているのが他所かどうかを見る。**
# 他所なら止めない。このリポジトリなら止める。**指定が無ければこのリポジトリである。**
REPO_RE = re.compile(r"--repo[= ]+([-\w.]+/[-\w.]+)")


def targets_other_repo(cmd):
    """そのコマンドが他所のリポジトリを指しているかを返す。"""
    m = REPO_RE.search(cmd)
    if not m:
        return False
    named = m.group(1)
    here = os.environ.get("CONTINUO_HOOK_REPO") or _repo_of_cwd()
    if not here:
        # **確かめられないなら止める側へ倒す。**素通しは事故につながる。
        return False
    return named.lower() != here.lower()


def _repo_of_cwd():
    """いまのディレクトリの `<owner>/<repo>` を返す。引けなければ空文字。"""
    try:
        out = subprocess.run(
            ["gh", "repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner"],
            capture_output=True, text=True, timeout=5,
        )
    except Exception:
        return ""
    return out.stdout.strip() if out.returncode == 0 else ""

# この目印がコメントの先頭にあれば、レビュー結果とみなす。
# **リリース前の検査（scripts/check-release-ready.sh）と CI（.github/workflows/review-gate.yml）が
# 数えるのと同じ目印である。**
MARKER = "<!-- code-review-result -->"

# **先頭にあることを求める。**前に空白文字があってもよい。
#
# **`\s` を使わない。**Python の `re` と jq（Oniguruma）で当たる範囲が違う。
# 実測（2026-09-02）。全角空白 U+3000 を1文字だけ前に置いた本文で、
#   jq 1.7.1 `test("^\\s*<!-- code-review-result -->")`          → true
#   Python `re.compile(r"\A\s*…", re.ASCII)`                     → False
#   Python `re.compile(r"\A\s*…")`（フラグなし）                  → True
# **どちらの `\s` に寄せても、もう一方とはずれる。**
# そこで**当たる文字を並べて書く**（半角空白・タブ・CR・LF の4つだけ）。
# この4文字は Python の文字クラスでも Oniguruma の文字クラスでも同じ意味になる。
#
# **並びは `MARKER_SPACE_CLASS` を正とする。**
# .github/workflows/review-gate.yml と scripts/check-release-ready.sh が
# jq へ渡す `[ \t\r\n]*` と、1文字ずつ同じであること。
# **揃っていることは .claude/hooks/tests/test_marker_pattern_parity.py が押さえる。**
MARKER_SPACE_CLASS = r"[ \t\r\n]*"
MARKER_RE = re.compile(r"\A" + MARKER_SPACE_CLASS + re.escape(MARKER))

# レビュー結果として数える投稿者。**それ以外のコメントは数えない。**
# 誰でもコメントできるので、外部の人が目印を貼れば通る状態にしない。
ALLOWED_ASSOCIATIONS = ("OWNER", "MEMBER", "COLLABORATOR")

# 逃がし口。人間が明示的に許したときだけ置く。
ESCAPE_ENV = "CONTINUO_ALLOW_UNREVIEWED_MERGE"

# gh の応答を待つ秒数。**超えたら通す**（検査が落ちて作業を止めない）。
GH_TIMEOUT_SEC = 20

REASON = """**レビュー結果が貼られていない PR は、マージも ready もできません。**

PR #{pr} のコメントに `{marker}` が1件もありません。

**先にレビューを通してください。**

    /code-review {pr}

**結果を PR のコメントへ貼ってください。**先頭にこの目印を置くこと。

    {marker}

**数える条件は2つです。**

- **目印が本文の先頭にあること。**途中に書いたものは数えません
- **投稿者が {assoc} のいずれかであること。**それ以外の人のコメントは数えません

**貼ったあと、CI の検査（review-gate）を回し直してください。**

- **PR が draft なら** `gh pr ready {pr}` を打つ。`ready_for_review` が飛んで回り直します
- **PR が既に draft でないなら、`gh pr ready` は効きません。**`ready_for_review` は
  draft を ready にしたときにしか起きないので、**`gh run rerun` で回し直してください**
  （手順は .claude/skills/pr-review-and-merge/SKILL.md の段5）

**なぜ止めているか。**[CLAUDE.md](CLAUDE.md) が「レビュー結果を貼ってあることが、実施したことの唯一の証拠である」と定めています。
**貼っていないものは、CI（.github/workflows/review-gate.yml）とリリース前の検査（scripts/check-release-ready.sh）も落とします。**
マージしてからレビューを回し直すと、手戻りが大きくなります。

**人間が明示的に許した場合だけ、環境変数 `{env}=1` を置いて通せます。**
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


def target_prs(command):
    """コマンドから、マージ・ready の対象になる PR 番号を集める。

    **`--repo` が他所を指しているときだけ空を返す。**
    このリポジトリを指す `--repo` は、むしろ普通の書き方なので止める。
    """
    if "gh" not in command:
        return []
    if targets_other_repo(command):
        return []
    return [m.group(1) for m in MERGE_RE.finditer(command)]


def counts_as_review(comment):
    """コメント1件を、レビュー結果として数えるかを返す。

    **絞り込みを Python 側で行うのは、テストから直接呼べるようにするためである。**
    jq の式に押し込むと、`gh` を叩かない限り条件を確かめられない。
    """
    if not isinstance(comment, dict):
        return False
    body = comment.get("body") or ""
    if not isinstance(body, str) or not MARKER_RE.match(body):
        return False
    return (comment.get("authorAssociation") or "") in ALLOWED_ASSOCIATIONS


def has_review(pr):
    """その PR にレビュー結果のコメントがあるかを返す。確かめられなければ None。"""
    try:
        out = subprocess.run(
            ["gh", "pr", "view", pr, "--json", "comments", "--jq", ".comments"],
            capture_output=True, text=True, timeout=GH_TIMEOUT_SEC,
        )
    except Exception:
        return None
    if out.returncode != 0:
        return None
    try:
        comments = json.loads(out.stdout)
    except Exception:
        return None
    if not isinstance(comments, list):
        return None
    return any(counts_as_review(c) for c in comments)


def main():
    payload = read_payload()
    if not payload:
        return 0
    if payload.get("tool_name") != "Bash":
        return 0
    command = (payload.get("tool_input") or {}).get("command") or ""
    if not isinstance(command, str):
        return 0

    prs = target_prs(command)
    if not prs:
        return 0

    # **人間が明示的に許したときだけ通す。**
    if os.environ.get(ESCAPE_ENV) == "1":
        return 0

    for pr in prs:
        ok = has_review(pr)
        # **確かめられなかったら通す。**検査が落ちて作業を止めない。
        if ok is None:
            continue
        if not ok:
            emit("deny", REASON.format(
                pr=pr, marker=MARKER, env=ESCAPE_ENV,
                assoc=" / ".join(ALLOWED_ASSOCIATIONS),
            ))
            return 0

    return 0


if __name__ == "__main__":
    sys.exit(main())
