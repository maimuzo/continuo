#!/usr/bin/env python3
"""レビュー結果の目印を数える条件が、3箇所で本当に同じかを確かめる。

    python3 .claude/hooks/tests/test_marker_pattern_parity.py

**リポジトリのルートから実行すること。**

## なぜこのテストが要るか

**3箇所が「同じ条件である」と互いのコメントで名乗っているが、実際は同じでなかった。**

実測（2026-09-02）。目印の前に全角空白 U+3000 を1文字だけ置いた本文で、

| どこ | 何を使っていたか | 判定 |
| --- | --- | --- |
| .github/workflows/review-gate.yml | jq `test("^\\s*<!-- code-review-result -->")` | 数える |
| scripts/check-release-ready.sh | 同じ jq | 数える |
| .claude/hooks/block-merge-without-review.py | `re.compile(r"\\A\\s*…", re.ASCII)` | **数えない** |

**Python の `re` と jq（Oniguruma）で `\\s` の当たる範囲が違う。**
`re.ASCII` を外すと今度は Python のほうが広くなる（`\\x1c` などにも当たる）ので、
**どちらの `\\s` に寄せても揃わない。**そこで3箇所とも `[ \\t\\r\\n]*` と並べて書き、
このテストが「並べたものが1文字ずつ同じか」と「実際に同じ答えを返すか」の両方を見る。

## 何を見るか

1. **書いてある文字列が同じか。**3つのファイルから目印の正規表現を取り出して突き合わせる
2. **実際に同じ答えを返すか。**Python の `MARKER_RE` と、jq へ渡す式に
   同じ本文の一覧を食わせて、1件ずつ答えを比べる（jq が無い環境では、この段は飛ばす）
"""

import importlib.util
import json
import os
import re
import shutil
import subprocess
import sys

HOOK = os.path.join(".claude", "hooks", "block-merge-without-review.py")
WORKFLOW = os.path.join(".github", "workflows", "review-gate.yml")
RELEASE = os.path.join("scripts", "check-release-ready.sh")

MARKER = "<!-- code-review-result -->"

# jq のソースに書いてある `test("…")` を取り出す。
# **jq の文字列の中なので、`\t` は `\\t` と2文字で書かれている。**
JQ_TEST_RE = re.compile(r'test\("(\^[^"]*' + re.escape(MARKER) + r')"\)')


def load_hook():
    """hook を module として読み込む。"""
    spec = importlib.util.spec_from_file_location("hook", HOOK)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def jq_pattern_of(path):
    """jq のソースから、目印を数える正規表現を取り出す。

    返すのは **jq が受け取る形**（`\\\\t` を1つの `\\t` に戻したもの）。
    見つからなければ None、2つ以上あって食い違っていれば例外。
    """
    with open(path, encoding="utf-8") as f:
        text = f.read()
    found = {m.group(1) for m in JQ_TEST_RE.finditer(text)}
    if not found:
        return None
    if len(found) > 1:
        raise AssertionError("%s に食い違う式が %d 個ある: %s" % (path, len(found), sorted(found)))
    # jq の文字列リテラルの escape を解く。`"\\t"` は `\t` の2文字になる。
    return json.loads('"' + found.pop() + '"')


def python_pattern_of(mod):
    """hook が使っている正規表現を、jq と同じ形（`^` 始まり）に直して返す。"""
    pat = mod.MARKER_RE.pattern
    if not pat.startswith(r"\A"):
        raise AssertionError("MARKER_RE が `\\A` で始まっていない: %r" % pat)
    # `re.escape` が目印へ入れた escape を外して、書いてある形に戻す。
    return "^" + pat[len(r"\A"):].replace(re.escape(MARKER), MARKER)


# 目印の前に置く文字。**全部を「同じ答えになるか」で見る。**
PREFIXES = [
    ("前に何も無い", ""),
    ("半角空白1つ", " "),
    ("半角空白3つ", "   "),
    ("タブ", "\t"),
    ("CR", "\r"),
    ("LF", "\n"),
    ("半角空白とタブの混在", " \t "),
    ("全角空白 U+3000", "\u3000"),
    ("NBSP U+00A0", "\u00a0"),
    ("EN SPACE U+2002", "\u2002"),
    ("THIN SPACE U+2009", "\u2009"),
    ("行区切り U+2028", "\u2028"),
    ("垂直タブ U+000B", "\x0b"),
    ("改ページ U+000C", "\x0c"),
    ("FILE SEPARATOR U+001C", "\x1c"),
    ("ふつうの文字", "a"),
    ("見出し", "## "),
]

# 目印そのものを含まない本文。**どちらも「数えない」であること。**
NON_MARKER_BODIES = [
    ("本文の途中に目印がある", "この PR は " + MARKER + " を貼りました"),
    ("目印が2行目にある", "はじめに\n" + MARKER),
    ("目印が無い", "レビューしました"),
    ("空の本文", ""),
]


def jq_matches(pattern, bodies):
    """jq に本文の一覧を食わせて、真偽の一覧を返す。"""
    program = '[.[] | test(%s)]' % json.dumps(pattern)
    out = subprocess.run(
        ["jq", "-c", program],
        input=json.dumps(bodies), capture_output=True, text=True, timeout=20,
    )
    if out.returncode != 0:
        raise AssertionError("jq が落ちた: %s" % out.stderr.strip())
    return json.loads(out.stdout)


def main():
    ng = 0
    ran = 0
    mod = load_hook()

    py_pat = python_pattern_of(mod)
    jq_ci = jq_pattern_of(WORKFLOW)
    jq_release = jq_pattern_of(RELEASE)

    # 段1. 書いてある文字列が同じか。
    for name, got in (
        (".github/workflows/review-gate.yml", jq_ci),
        ("scripts/check-release-ready.sh", jq_release),
    ):
        ran += 1
        if got is None:
            ng += 1
            print("NG  %s から目印の式を取り出せない" % name)
        elif got != py_pat:
            ng += 1
            print("NG  %s の式が hook と違う: %r（hook は %r）" % (name, got, py_pat))
        else:
            print("ok  %s の式が hook と同じ（%r）" % (name, got))

    # 段2. `\s` を使っていないか。**engine で当たる範囲が変わるので使ってはならない。**
    for name, got in (
        ("hook（block-merge-without-review.py）", py_pat),
        (".github/workflows/review-gate.yml", jq_ci),
        ("scripts/check-release-ready.sh", jq_release),
    ):
        ran += 1
        if got is not None and r"\s" in got:
            ng += 1
            print(r"NG  %s の式が `\s` を使っている: %r" % (name, got))
        else:
            print(r"ok  %s の式が `\s` を使っていない" % name)

    # 段3. 実際に同じ答えを返すか。
    bodies = [p + MARKER for _, p in PREFIXES] + [b for _, b in NON_MARKER_BODIES]
    names = ["目印の前が %s" % n for n, _ in PREFIXES] + [n for n, _ in NON_MARKER_BODIES]
    py_got = [bool(mod.MARKER_RE.match(b)) for b in bodies]

    if shutil.which("jq") is None:
        print("--  jq が無いので、実際に当ててみる段は飛ばした")
    elif jq_ci is None:
        print("--  CI の式を取り出せなかったので、実際に当ててみる段は飛ばした")
    else:
        jq_got = jq_matches(jq_ci, bodies)
        for name, want, got in zip(names, py_got, jq_got):
            ran += 1
            if want != got:
                ng += 1
                print("NG  %s: hook=%s / jq=%s" % (name, want, got))
            else:
                print("ok  %s: どちらも %s" % (name, want))

    # 段4. 想定そのものを書き下す。**「同じ」だけでは、両方まとめて緩んでも気づけない。**
    want_table = {
        "前に何も無い": True,
        "半角空白1つ": True,
        "半角空白3つ": True,
        "タブ": True,
        "CR": True,
        "LF": True,
        "半角空白とタブの混在": True,
        "全角空白 U+3000": False,
        "NBSP U+00A0": False,
        "EN SPACE U+2002": False,
        "THIN SPACE U+2009": False,
        "行区切り U+2028": False,
        "垂直タブ U+000B": False,
        "改ページ U+000C": False,
        "FILE SEPARATOR U+001C": False,
        "ふつうの文字": False,
        "見出し": False,
    }
    for (name, prefix), got in zip(PREFIXES, py_got):
        ran += 1
        want = want_table[name]
        if want != got:
            ng += 1
            print("NG  目印の前が %s: %s（想定は %s）" % (name, got, want))
        else:
            print("ok  目印の前が %s: %s" % (name, got))

    for (name, body), got in zip(NON_MARKER_BODIES, py_got[len(PREFIXES):]):
        ran += 1
        if got:
            ng += 1
            print("NG  %s を数えてしまう" % name)
        else:
            print("ok  %s は数えない" % name)

    print("\n%d 件中 %d 件が想定どおり" % (ran, ran - ng))
    return 1 if ng else 0


if __name__ == "__main__":
    sys.exit(main())
