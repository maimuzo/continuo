#!/usr/bin/env python3
"""返答に書いたコマンドを、実際に実行してから見せているかを検査する Stop hook。

**なぜ要るか。**この道具を作った本人が、自分で作ったサブコマンドのフラグの位置を間違えて人間に見せた。
`continuo abandon <URL> --dry-run` と書いたが、実装は位置引数のあとのフラグを弾く。
**実装を知っていたのに、書くときに参照しなかった。**

原因は「コードとテストは実行して確かめるのに、返答に書くコマンドは実行しない」という扱いの差である。
**その区別に根拠は無い。**返答に書いたコマンドは、読む人がそのまま叩く。動かなければ嘘である。

**何を見るか。**返答に書いたコマンドの **「実行ファイル名 + サブコマンド + フラグの集合」** を作り、
**その turn の実行履歴に同じものが無ければ止める。**

**「この turn で Bash を呼んだか」では足りない。**事故が起きた turn でも、別のコマンドは実行していた。
**そのコマンドを実行したか**を見なければ意味が無い。

**照合の仕方。**
- 実行ファイル名（パスの最後の要素）とサブコマンド（最初の非フラグ引数）が一致すること
- **書いた側のフラグが、実行した側のフラグに全部含まれていること**（実行時に足したフラグは許す）
- **引数の値は見ない。**`--to "Ice Box"` の `Ice Box` や、パスや URL は照合しない

**逃げ道。**実行できないもの（本番へ書き込む・枠を消費する・利用者の環境でしか動かない）は、
コマンドの近くに「未実行」などと書けば通る。**書けば通るが、書かずに通ることはできない。**

**fail-open。**入力を解釈できない・transcript を読めないなど、検査そのものが成り立たないときは通す。
"""

import json
import os
import re
import shlex
import sys

# 検査する最小の長さ。挨拶や短い相槌にコマンドは出てこない。
MIN_LEN = 120

# stdin の上限。これを超える入力は検査しない（fail-open）。
MAX_INPUT_BYTES = 4 * 1024 * 1024

# コマンドブロックとみなす言語の指定。
#
# **`text` と無印は数えない。**出力の引用や図で使われるためである。
COMMAND_LANGS = ("bash", "sh", "zsh", "shell", "console", "shell-session")

# 「実行していない」と断ってあれば通す。
UNVERIFIED_MARKS = (
    "未実行",
    "実行していません",
    "実行していない",
    "叩いていません",
    "叩いていない",
    "確かめていません",
    "確かめていない",
)

# 実行の証跡とみなすツールの名前。
RUN_TOOLS = ("Bash",)

# 照合の対象から外す実行ファイル。
#
# **シェルの組み込みと、人間が読むための飾りである。**これらは「実行して確かめる」対象ではない。
SKIP_PROGRAMS = {
    "cd", "export", "set", "unset", "echo", "printf", "true", "false", "exit", "return",
    "source", ".", "alias", "read", "if", "then", "else", "fi", "for", "while", "do", "done",
    "case", "esac", "function", "local",
}


def debug_enabled() -> bool:
    return os.environ.get("CLAUDE_HOOK_DEBUG", "").strip().lower() in ("1", "true", "yes")


def read_payload():
    """stdin を UTF-8 として読み、辞書なら返す。それ以外は空の辞書を返す。"""
    stream = getattr(sys.stdin, "buffer", None)
    if stream is None:
        raw = sys.stdin.read()
        if isinstance(raw, str):
            raw = raw.encode("utf-8", "replace")
    else:
        raw = stream.read(MAX_INPUT_BYTES + 1)
    if len(raw) > MAX_INPUT_BYTES:
        return {}
    try:
        payload = json.loads(raw.decode("utf-8", "replace"))
    except Exception:
        if debug_enabled():
            import traceback

            traceback.print_exc(file=sys.stderr)
        return {}
    return payload if isinstance(payload, dict) else {}


def read_transcript(path: str):
    """transcript の jsonl を、行ごとの辞書の一覧にして返す。読めなければ空を返す。"""
    if not path:
        return []
    try:
        with open(os.path.expanduser(path), "r", encoding="utf-8", errors="replace") as f:
            out = []
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    out.append(json.loads(line))
                except Exception:
                    continue
            return out
    except Exception:
        if debug_enabled():
            import traceback

            traceback.print_exc(file=sys.stderr)
        return []


def last_turn(entries):
    """最後の人間の発言より後ろの記録だけを返す。"""
    start = 0
    for i, e in enumerate(entries):
        if e.get("type") != "user":
            continue
        content = (e.get("message") or {}).get("content")
        # ツールの結果も type=user で来るので、本文のあるものだけを人間の発言とみなす。
        if isinstance(content, str) and content.strip():
            start = i
        elif isinstance(content, list) and any(
            isinstance(c, dict) and c.get("type") == "text" and str(c.get("text", "")).strip()
            for c in content
        ):
            start = i
    return entries[start:]


def signature(line: str):
    """1行のコマンドを (実行ファイル名, サブコマンド, フラグの集合) にする。

    照合の鍵になる部分だけを採る。**引数の値は採らない。**

    line: コマンドの1行。
    戻り値: 署名のタプル。採れないときは None。
    """
    line = line.strip()
    if not line or line.startswith("#"):
        return None

    # 人間に見せる実行例の飾り（`$ ` / `% ` / `> `）を落とす。
    line = re.sub(r"^[\$%>]\s+", "", line)

    # 複数のコマンドが繋がっていたら、**先頭だけ**を見る。
    # 単純にするためである（パイプの各段まで見ると誤検知が増える）。
    line = re.split(r"\s*(?:&&|\|\||\||;)\s*", line)[0]

    try:
        tokens = shlex.split(line, comments=True)
    except ValueError:
        # 閉じていない引用符など。解析できないものは照合しない。
        return None
    if not tokens:
        return None

    # 変数の代入（FOO=bar cmd）を読み飛ばす。
    while tokens and re.match(r"^[A-Za-z_][A-Za-z0-9_]*=", tokens[0]):
        tokens = tokens[1:]
    if not tokens:
        return None

    prog = os.path.basename(tokens[0])
    if prog in SKIP_PROGRAMS:
        return None

    sub = ""
    flags = set()
    for t in tokens[1:]:
        if t == "--":
            break
        if t.startswith("-") and len(t) > 1:
            # 値が = で繋がっている形（--to=Done）は名前だけ採る。
            flags.add(t.split("=", 1)[0])
        elif not sub:
            sub = t
    return (prog, sub, frozenset(flags))


def written_signatures(msg: str):
    """返答のコマンドブロックから、署名の一覧を返す。"""
    out = []
    for m in re.finditer(r"^[ \t]*```([A-Za-z0-9_-]*)[ \t]*\n(.*?)^[ \t]*```", msg, re.S | re.M):
        if (m.group(1) or "").strip().lower() not in COMMAND_LANGS:
            continue
        for line in m.group(2).splitlines():
            sig = signature(line)
            if sig:
                out.append((sig, line.strip()))
    return out


def ran_signatures(turn):
    """この turn で実行したコマンドの署名を集合で返す。

    **1回の Bash 呼び出しに複数行が入ることがある。**行ごとに署名を採る。
    """
    out = set()
    for e in turn:
        for c in (e.get("message") or {}).get("content") or []:
            if not (isinstance(c, dict) and c.get("type") == "tool_use" and c.get("name") in RUN_TOOLS):
                continue
            command = str((c.get("input") or {}).get("command", ""))
            for line in command.splitlines():
                sig = signature(line)
                if sig:
                    out.add(sig)
    return out


def covered(written, ran) -> bool:
    """書いた署名が、実行した署名のどれかに含まれるかを返す。

    **書いたフラグは、実行したフラグに全部含まれること**（実行時に足した分は許す）。

    **実行ファイル名は、一致しなくてもサブコマンドが一致すれば通す。**
    手元でビルドした実行ファイルは名前が違う（`/tmp/co-rel abandon …` を実行して
    `continuo abandon …` と書く）。**そこで止めると、正しく確かめた場合まで弾いてしまう。**
    ただし**サブコマンドが空のときは、実行ファイル名の一致を求める**
    （`foo` と `bar` を取り違えないため）。
    """
    prog, sub, flags = written
    for r_prog, r_sub, r_flags in ran:
        if not (flags <= r_flags):
            continue
        if prog == r_prog and sub == r_sub:
            return True
        if sub and sub == r_sub:
            return True
    return False


def last_assistant_text(turn) -> str:
    """この turn の最後の返答の本文を返す。"""
    text = ""
    for e in turn:
        if e.get("type") != "assistant":
            continue
        parts = [
            str(c.get("text", ""))
            for c in (e.get("message") or {}).get("content") or []
            if isinstance(c, dict) and c.get("type") == "text"
        ]
        if parts:
            text = "\n".join(parts)
    return text


def main() -> int:
    payload = read_payload()
    if payload.get("stop_hook_active"):
        return 0  # 無限ループの防止

    entries = read_transcript(payload.get("transcript_path", ""))
    if not entries:
        return 0  # fail-open

    turn = last_turn(entries)
    if not turn:
        return 0

    reply = last_assistant_text(turn)
    if len(reply) < MIN_LEN:
        return 0

    written = written_signatures(reply)
    if not written:
        return 0

    if any(mark in reply for mark in UNVERIFIED_MARKS):
        return 0

    ran = ran_signatures(turn)
    missing = [(sig, raw) for sig, raw in written if not covered(sig, ran)]
    if not missing:
        return 0

    lines = []
    for (prog, sub, flags), raw in missing[:5]:
        key = " ".join(x for x in [prog, sub, *sorted(flags)] if x)
        lines.append(f"  - `{raw[:70]}`  →  照合の鍵: `{key}`")
    detail = "\n".join(lines)
    more = f"\n  （ほかに {len(missing) - 5} 行）" if len(missing) > 5 else ""

    print(
        json.dumps(
            {
                "decision": "block",
                "reason": (
                    "**返答に書いたコマンドのうち、この turn で実行していないものがあります。**\n"
                    f"{detail}{more}\n"
                    "\n"
                    "**書いたコマンドは、読む人がそのまま叩きます。動かなければ嘘になります。**\n"
                    "実際に、自分で作ったサブコマンドのフラグの位置を間違えて人間に見せた事故が起きています。\n"
                    "\n"
                    "**次のどちらかにしてください。**\n"
                    "1. **そのコマンドを実行してから、出力を見て書き直す**（`--help` の引用も含みます）\n"
                    "2. **実行できないなら、コマンドの近くに「未実行」と明記する**"
                    "（本番へ書き込む・枠を消費する・利用者の環境でしか動かない、などの場合）\n"
                ),
            },
            ensure_ascii=False,
        )
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
