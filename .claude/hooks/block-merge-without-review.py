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

`Bash` の `command` に次のどちらかが含まれていたら、対象の PR 番号を取り出し、
**その PR のコメントに `<!-- code-review-result -->` があるかを `gh` で確かめる。**

- `gh pr merge <番号>`
- `gh pr ready <番号>`

**無ければ止める。**あれば通す。

## 止めないもの

- **番号を書かない形**（`gh pr merge` だけ）。現在の branch から引く形は、番号を取れないので見送る
- `gh pr merge --repo <他所>`。**このリポジトリの PR でなければ見ない**
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
# **`--repo <他所>` も止めない。**このリポジトリの PR ではないので、番号を当てても意味が無い。
# **番号の前に来てよいのはオプションだけにする。**`--repo other/repo 123` の 123 を拾わないため。
MERGE_RE = re.compile(
    r"\bgh\s+pr\s+(?:merge|ready)\s+"
    r"(?!(?:[-\w=./:]+\s+)*?--undo\b)"
    r"(?!(?:[-\w=./:]+\s+)*?--repo\b)"
    r"(?:--?[-\w]+(?:=[-\w./:]+)?\s+)*?"
    r"(\d+)\b"
)

# この目印が PR のコメントに1つでもあれば、レビューを通したものとみなす。
# **リリース前の検査（scripts/check-release-ready.sh）が数えるのと同じ目印である。**
MARKER = "<!-- code-review-result -->"

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

**なぜ止めているか。**[CLAUDE.md](CLAUDE.md) が「レビュー結果を貼ってあることが、実施したことの唯一の証拠である」と定めています。
**貼っていないものは、リリース前の検査（scripts/check-release-ready.sh）が落とします。**
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
    """コマンドから、マージ・ready の対象になる PR 番号を集める。"""
    if "gh" not in command:
        return []
    return [m.group(1) for m in MERGE_RE.finditer(command)]


def has_review(pr):
    """その PR のコメントに目印があるかを返す。確かめられなければ None。"""
    try:
        out = subprocess.run(
            ["gh", "pr", "view", pr, "--json", "comments",
             # **`contains` を使わない。**本文の途中に目印があるコメント
             # （手順を説明した進捗の報告など）を1件と数えてしまう。
             # **2026-09-01、それで2本がレビュー未実施のままマージされた。**
             # `.github/workflows/review-gate.yml` と同じく、先頭にあることを見る。
             "--jq",
             # **`\s` は使えない。**jq の文字列は JSON 文字列なので、
             # バックスラッシュ1つの `\s` は「不正なエスケープ」で構文エラーになる。
             # **落ちると has_review が None を返し、hook は全部通してしまう。**
             '[.comments[] | select(.body | test("^[[:space:]]*%s"))] | length' % MARKER],
            capture_output=True, text=True, timeout=GH_TIMEOUT_SEC,
        )
    except Exception:
        return None
    if out.returncode != 0:
        return None
    try:
        return int(out.stdout.strip()) > 0
    except Exception:
        return None


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
            emit("deny", REASON.format(pr=pr, marker=MARKER, env=ESCAPE_ENV))
            return 0

    return 0


if __name__ == "__main__":
    sys.exit(main())
