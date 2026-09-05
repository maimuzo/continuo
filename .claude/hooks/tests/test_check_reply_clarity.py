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
QUOTE = (
    "> 依頼の原文をここに引く。結びの1文だけでなく、判断の材料になった部分から引く。\n"
    "> そうすると読む側は、何の話への返答なのかを本文より先に知ることができる。\n"
    "> 短く切り詰めると、依頼のうちどこに答えているのかが読み取れなくなる。\n\n"
)


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
    "> #60 を最優先してくれ。他にも進めたいものはあるが、まずはそこからやってほしい。\n"
    "> 対策が入るまでは動かさないことにしたので、そこが片付かないと先へ進めない。\n"
    "> 終わったら、次に何をやるかを相談させてほしい。\n\n"
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
case(
    "結びの1文だけの引用も止まる",
    "> これでなにか問題があるか検討し、問題なければ実装して良い\n\n"
    + base()[base().index("## 三行まとめ"):],
    True,
    "引用",
)
case("引用が十分な長さなら通る", base(), False)
case(
    "引用が1文字も無ければ止まる",
    base()[base().index("## 三行まとめ"):],
    True,
    "引用",
)
# **閾値そのものを固定する。**この検査が無いと、閾値を戻しても全部通ってしまう。
# 実例（2026-08-29 のレビュー）: 閾値を 30 に戻しても 42 件が全部通った。
_JUST_UNDER = "> " + ("あ" * 79) + "\n\n"
_JUST_OVER = "> " + ("あ" * 80) + "\n\n"
case(
    "閾値の1文字下は止まる",
    _JUST_UNDER + base()[base().index("## 三行まとめ"):],
    True,
    "引用",
)
case(
    "閾値ちょうどは通る",
    _JUST_OVER + base()[base().index("## 三行まとめ"):],
    False,
)

# ---- 話題の切れ目（区切り線） ---------------------------------------------

# 区切り線のあとに、三行まとめと何が言いたいのかを置いた2つ目の話題。
SECOND_TOPIC_OK = (
    "\n-----\n\n"
    "## 三行まとめ\n" + FILLER + "\n\n"
    "## 何が言いたいのか\n**報告。**別の話題です。" + FILLER + "\n\n"
    "## 結果\n" + FILLER + "\n"
)

# 区切り線のあとに何も名乗らない2つ目の話題。
SECOND_TOPIC_NG = "\n-----\n\n## 別の話\n" + FILLER + "\n" + FILLER + "\n"

case("区切り線の先に名乗りが無いと止まる", base() + SECOND_TOPIC_NG, True, "別の話題へ移るとき")
case("区切り線の先に三行まとめと何が言いたいのかがあれば通る", base() + SECOND_TOPIC_OK, False)
case(
    "三行まとめだけでは止まる",
    base() + "\n-----\n\n## 三行まとめ\n" + FILLER + "\n" + FILLER + "\n",
    True,
)
case("区切り線が無ければ何も求めない", base() + "\n## 別の話\n" + FILLER + "\n", False)
case(
    "区切り線の先が短ければ求めない",
    base() + "\n-----\n\nこれは署名のような短い補足です。\n",
    False,
)
case(
    "表の区切りは区切り線と数えない",
    base("| 何 | 状態 |\n| --- | --- |\n| A（説明つき） | B |"),
    False,
)
case("コードフェンスの中の区切り線は数えない", base() + "\n```\n-----\n```\n", False)

# ---- 節を番号だけで指す -----------------------------------------------------

case(
    "設計の節を番号だけで指すと止まる",
    base("**設計 6-23b に書きました。**"),
    True,
    "番号だけで指さないこと",
)
case(
    "markdown link があれば通る",
    base("**[docs/plans/continuo_design.md:8354-8418](docs/plans/continuo_design.md#L8354-L8418) に書きました。**"),
    False,
)
case(
    "同じ行に link があれば節番号を書いても通る",
    base("設計 6-23b（[docs/plans/foo.md:1-2](docs/plans/foo.md#L1-L2)）に書きました。"),
    False,
)
case("節と書いても止まる", base("節 3-64 を見てください。"), True)
case("インラインコードの中は数えない", base("`設計 6-23b` という書き方の例です。"), False)
case("数字が続かなければ止まらない", base("設計の記録をまとめました。"), False)
case("ハイフンの後ろが数字でなければ止まらない", base("設計 3-abc を見てください。"), False)

# ---- ファイル参照の形式 -----------------------------------------------------

case(
    "行番号の無い markdown link は止まる",
    base("[docs/plans/continuo_design.md](docs/plans/continuo_design.md) を見てください。"),
    True,
    "行番号まで書くこと",
)
case(
    "行番号があれば通る",
    base("[docs/plans/continuo_design.md:12-34](docs/plans/continuo_design.md#L12-L34) を見てください。"),
    False,
)
case(
    "backtick で囲んだファイルパスは通す（機械的に止めない）",
    base("`internal/orchestrator/dispatch.go` を見てください。"),
    False,
)
case(
    "ディレクトリは求めない",
    base("[docs/plans/](docs/plans/) にあります。"),
    False,
)
case(
    "URL は求めない",
    base("[公式文書](https://code.claude.com/docs/en/hooks.md) を見てください。"),
    False,
)
case(
    "コードフェンスの中のパスは数えない",
    base() + "\n```bash\ncat internal/config/types.go\n```\n",
    False,
)
case(
    "空白を含む backtick はコマンドとみなして通す",
    base("`go test ./test/internal/scaffold/...` を実行しました。"),
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
