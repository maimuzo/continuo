#!/usr/bin/env python3
"""レビュー結果の目印を数える条件が、2箇所で本当に同じかを確かめる。

    python3 .claude/hooks/tests/test_marker_pattern_parity.py

**リポジトリのルートから実行すること。**

## なぜこのテストが要るか

**「同じ条件である」と互いのコメントで名乗っているが、実際は同じでなかった。**

実測（2026-09-02）。目印の前に全角空白 U+3000 を1文字だけ置いた本文で、

| どこ | 何を使っていたか | 判定 |
| --- | --- | --- |
| .github/workflows/review-gate.yml | jq `test("^\\s*<!-- code-review-result -->")` | 数える |
| scripts/check-release-ready.sh | 同じ jq | 数える |
| .claude/hooks/block-merge-without-review.py（廃止済み） | `re.compile(r"\\A\\s*…", re.ASCII)` | **数えなかった** |

**Python の `re` と jq（Oniguruma）で `\\s` の当たる範囲が違っていた。**
`re.ASCII` を外すと今度は Python のほうが広くなる（`\\x1c` などにも当たる）ので、
**どちらの `\\s` に寄せても揃わなかった。**そこで数える側は全部 `[ \\t\\r\\n]*` と並べて書き、
このテストが「並べたものが1文字ずつ同じか」と「実際に当てた答えが想定どおりか」の両方を見る。

**Python の実装は、もう1つも無い。**`block-merge-without-review.py` は 2026-09-21 に廃止した
（branch の保護設定で `enforce_admins` を有効にし、admin も赤い検査を素通りできなくしたため）。
**残る2つはどちらも jq なので、比べる相手は jq どうしである。**
**どちらが正本かを決めておく必要があるので、`scripts/check-release-ready.sh` を正本とする。**

## 何を見るか

1. **書いてある文字列が同じか。**2つのファイルから目印の正規表現を取り出して突き合わせる
2. **当てた答えが想定どおりか。**jq へ渡す式に本文の一覧を食わせ、
   **このテストが書いている想定の一覧と1件ずつ比べる**（jq が無い環境では、この段は飛ばす）
"""

import json
import os
import re
import shutil
import subprocess
import sys

WORKFLOW = os.path.join(".github", "workflows", "review-gate.yml")
RELEASE = os.path.join("scripts", "check-release-ready.sh")

MARKER = "<!-- code-review-result -->"

# jq のソースに書いてある `test("…")` を取り出す。
# **jq の文字列の中なので、`\t` は `\\t` と2文字で書かれている。**
JQ_TEST_RE = re.compile(r'test\("(\^[^"]*' + re.escape(MARKER) + r')"\)')


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
    # **正本は scripts/check-release-ready.sh とする。**
    # Python の実装（hook）は廃止したので、比べる相手は jq どうしである。
    jq_release = jq_pattern_of(RELEASE)
    jq_ci = jq_pattern_of(WORKFLOW)

    # 段1. 書いてある文字列が、正本と同じか。
    ran += 1
    if jq_release is None:
        ng += 1
        print("NG  scripts/check-release-ready.sh（正本）から目印の式を取り出せない")
    elif jq_ci is None:
        ng += 1
        print("NG  .github/workflows/review-gate.yml から目印の式を取り出せない")
    elif jq_ci != jq_release:
        ng += 1
        print("NG  review-gate.yml の式が正本と違う: %r（正本は %r）" % (jq_ci, jq_release))
    else:
        print("ok  review-gate.yml の式が正本と同じ（%r）" % jq_ci)

    # 段2. `\s` を使っていないか。**engine で当たる範囲が変わるので使ってはならない。**
    for name, got in (
        ("scripts/check-release-ready.sh（正本）", jq_release),
        (".github/workflows/review-gate.yml", jq_ci),
    ):
        ran += 1
        if got is not None and r"\s" in got:
            ng += 1
            print(r"NG  %s の式が `\s` を使っている: %r" % (name, got))
        else:
            print(r"ok  %s の式が `\s` を使っていない" % name)

    # 段3. 実際に当てた答えが、想定どおりか。
    # **「2つが同じ」だけでは、両方まとめて緩んでも気づけない。**想定そのものを書き下す。
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
    bodies = [prefix + MARKER for _, prefix in PREFIXES] + [b for _, b in NON_MARKER_BODIES]

    if shutil.which("jq") is None:
        # **飛ばして緑にしてはならない。**
        # 飛ばすと、2つの式を同時に緩めても段1（互いに同じか）と段2（`\s` を使っていないか）を
        # 通ってしまい、**手元では `3 件中 3 件が想定どおり` と出て「揃っている」と読める。**
        # このテストが生まれた原因（2026-09-02 に全角空白で2つの実装が割れた件）は、
        # **当てて初めて分かる。**jq はこのリポジトリの検査に必須（`gh --jq` も使う）なので、
        # 無い環境を緑にする理由が無い。
        ran += 1
        ng += 1
        print("NG  jq が無いので、当てて確かめられない（jq を入れること）")
    elif jq_release is None or jq_ci is None:
        print("--  式を取り出せなかったので、実際に当ててみる段は飛ばした")
    else:
        for path_name, pattern in (
            ("scripts/check-release-ready.sh（正本）", jq_release),
            (".github/workflows/review-gate.yml", jq_ci),
        ):
            got_all = jq_matches(pattern, bodies)
            for (name, _prefix), got in zip(PREFIXES, got_all):
                ran += 1
                want = want_table[name]
                if want != got:
                    ng += 1
                    print("NG  %s / 目印の前が %s: %s（想定は %s）" % (path_name, name, got, want))
                else:
                    print("ok  %s / 目印の前が %s: %s" % (path_name, name, got))

            for (name, _body), got in zip(NON_MARKER_BODIES, got_all[len(PREFIXES):]):
                ran += 1
                if got:
                    ng += 1
                    print("NG  %s / %s を数えてしまう" % (path_name, name))
                else:
                    print("ok  %s / %s は数えない" % (path_name, name))

    print("\n%d 件中 %d 件が想定どおり" % (ran, ran - ng))
    return 1 if ng else 0


if __name__ == "__main__":
    sys.exit(main())
