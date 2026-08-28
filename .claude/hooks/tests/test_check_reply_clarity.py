#!/usr/bin/env python3
"""check-reply-clarity.py が、狙った場合に止まり、狙わない場合に通ることを確かめる。

    python3 .claude/hooks/tests/test_check_reply_clarity.py

**リポジトリのルートから実行すること。**

**この検査は全部の turn で走る。**誤検知は通常の作業を止め、検知漏れは規則の強制を無意味にする。
だから「実際に指摘された書き方」をそのまま収録してある。
"""

import json
import subprocess
import sys

HOOK = ".claude/hooks/check-reply-clarity.py"

FILLER = "この節は中身がある文章である。" * 5
QUOTE = "> 依頼の原文をここに引く。何の話への返答かが分かる長さまで引いている\n\n"


def run(msg):
    """hook を走らせて (止まったか, reason) を返す。"""
    out = subprocess.run(
        ["python3", HOOK],
        input=json.dumps({"last_assistant_message": msg}, ensure_ascii=False),
        capture_output=True,
        text=True,
    ).stdout.strip()
    if not out:
        return (False, "")
    body = json.loads(out)
    return (body.get("decision") == "block", body.get("reason", ""))


def base(body="", category="**報告。**この turn で分かったことをまとめる。"):
    """5段構成を満たした返答を組み立てる。body は結果の節へ差し込む。"""
    return (
        QUOTE
        + "## 三行まとめ\n" + FILLER + "\n\n"
        + "## 何が言いたいのか\n" + category + FILLER + "\n\n"
        + "## 結果\n" + (body + "\n" if body else "") + FILLER + "\n\n"
        + "## 詳細\n" + FILLER + "\n"
    )


cases = []


def case(name, msg, want_block, want_in=None):
    cases.append((name, msg, want_block, want_in))


# ---- 番号だけの参照 -------------------------------------------------------

case("番号だけの参照は止まる", base("#60 を最優先します。"), True, "番号だけで書かないこと")
case("直後に括弧で内容を添えれば通る", base("#60（外部コメントからの実行経路）を最優先します。"), False)
case("半角の括弧でも通る", base("#60 (external comment path) first."), False)
case("文末に括弧で補足しても通る", base("外部コメントからの実行経路（#60）を最優先します。"), False)
case("2つ並べて片方が裸なら止まる", base("#60（外部コメントからの実行経路）と #87 を直します。"), True, "1 件")
case("インラインコードの中は数えない", base("`#60` という書き方の例です。"), False)
case(
    "URL の中は数えない",
    base("https://github.com/octocat/hello-world/issues/60 を見てください。"),
    False,
)
case(
    "行番号つきのパス参照は数えない",
    base("[docs/plans/foo.md:12-34](docs/plans/foo.md#L12-L34) を見てください。"),
    False,
)
case("コードフェンスの中は数えない", base() + "\n```\ngh issue view 60\n#60 を直す\n```\n", False)
case(
    "引用の中は数えない",
    "> #60 を最優先してくれ。他にも進めたいものはあるが、まずはそこからやってほしい\n\n"
    + base()[base().index("## 三行まとめ"):],
    False,
)
case("表の中の番号も数える", base("| 何 | 状態 |\n| --- | --- |\n| #87 | 未着手 |"), True)

# ---- 報告 / 質問 / 確認 のカテゴリ ---------------------------------------

case(
    "カテゴリを名乗らないと止まる",
    base(category="いろいろ調べた結果をまとめます。"),
    True,
    "報告 / 質問 / 確認",
)
case("報告と名乗れば通る", base(category="**報告。**調べた結果です。"), False)
case("質問と名乗れば通る", base(category="**質問。**どちらにしますか。"), False)
case("確認と名乗れば通る", base(category="**確認。**こう決めましたが、違えば言ってください。"), False)
case("太字でなくても語があれば通る", base(category="報告です。調べた結果をまとめます。"), False)

# ---- 引用の薄さ -----------------------------------------------------------

case(
    "引用が短すぎると止まる",
    "> 検討して\n\n" + base()[base().index("## 三行まとめ"):],
    True,
    "引用",
)
case("引用が十分な長さなら通る", base(), False)

# ---- サブセクションの冒頭 -------------------------------------------------

case(
    "表からいきなり始まるサブセクションは止まる",
    base() + "\n### 調べた結果\n\n| 何 | 状態 |\n| --- | --- |\n| A | B |\n",
    True,
    "冒頭に「一言で言うと何か」",
)
case(
    "太字の要約があれば通る",
    base() + "\n### 調べた結果\n\n**報告。**3件見つかりました。\n\n| 何 | 状態 |\n| --- | --- |\n| A | B |\n",
    False,
)
case(
    "カテゴリ語で始まっても通る",
    base() + "\n### 調べた結果\n\n報告。3件見つかりました。\n",
    False,
)
case(
    "見出しが2段（##）なら求めない",
    base() + "\n## 別の節\n\n| 何 | 状態 |\n| --- | --- |\n| A | B |\n",
    False,
)

# ---- 検査しない場合 -------------------------------------------------------

case("200文字未満は検査しない", "#60 を直しました。", False)
case(
    "コードだけの返答は検査しない",
    "こう書けます。\n\n```go\n" + ("// #60 を直す\n" * 40) + "```\n\n動きます。\n",
    False,
)


def main():
    ng = 0
    for name, msg, want_block, want_in in cases:
        blocked, reason = run(msg)
        ok = blocked == want_block
        if ok and want_in:
            ok = want_in in reason
        if not ok:
            ng += 1
            got = "止まった" if blocked else "通った"
            want = "止まる" if want_block else "通る"
            print("NG  %s: %s（想定は %s）" % (name, got, want))
            if want_in and blocked:
                print("    reason に %r が入っていない" % want_in)
        else:
            print("ok  %s" % name)
    print("\n%d 件中 %d 件が想定どおり" % (len(cases), len(cases) - ng))
    return 1 if ng else 0


if __name__ == "__main__":
    sys.exit(main())
