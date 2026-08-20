#!/usr/bin/env python3
"""返答が定められた5段構成になっているかを機械的に検査する Stop hook。

なぜ要るか。
    .claude/rules/reporting.md と CLAUDE.md に構成の規則を書いても守られず、
    2026-08 に同じ指摘を5回以上受けた。記憶や意志に頼る対策は効果が無かったので、
    turn を終える直前に機械で検査し、違反していれば書き直させる。

何を検査するか。
    「引用で対象を宣言 → 三行まとめ → 何が言いたいのか → 結果 → 詳細」の5段。
    ただし短い応答（確認・相槌・コマンドの結果報告だけ）には求めない。

壊れたときの振る舞い。
    例外が出たら黙って通す（fail-open）。hook が壊れて全部の turn が止まるのを避ける。
"""

import json
import re
import sys

# この文字数を超える応答に5段構成を求める。
# 短い確認や「わかりました」に構成を強いると、やり取りが冗長になるだけである。
# 当初の 400 は根拠の無い値だったため、利用者の指定で 200 に変更した。
MIN_LEN_FOR_STRUCTURE = 200

# 見出しの表記ゆれを許す（「## 三行まとめ」「## 3行まとめ」など）。
SECTIONS = [
    ("三行まとめ", re.compile(r"^#{1,4}\s*(三行|3行|３行)まとめ", re.M)),
    ("何が言いたいのか", re.compile(r"^#{1,4}\s*何が言いたい", re.M)),
    ("結果", re.compile(r"^#{1,4}\s*結果", re.M)),
    ("詳細", re.compile(r"^#{1,4}\s*詳細", re.M)),
]

# 引用による対象の宣言。行頭の "> " を1行以上求める。
QUOTE = re.compile(r"^>\s+\S", re.M)


def main() -> int:
    try:
        payload = json.load(sys.stdin)
    except Exception:
        return 0

    # 既に block した結果の再実行なら、そのまま通す（無限ループを避ける）。
    if payload.get("stop_hook_active"):
        return 0

    msg = payload.get("last_assistant_message") or ""
    if len(msg) < MIN_LEN_FOR_STRUCTURE:
        return 0

    missing = [name for name, pat in SECTIONS if not pat.search(msg)]
    if not QUOTE.search(msg):
        missing.insert(0, "引用による対象の宣言（行頭の `> `）")

    if not missing:
        return 0

    reason = (
        "返答の構成が規則を満たしていません。**書き直してください。**\n\n"
        "足りないもの: " + " / ".join(missing) + "\n\n"
        "**すべてのセクションを次の5段で書くこと。順番を変えない。1段も落とさない。**\n"
        "1. 引用で対象を宣言する（リクエストの原文を `> ` で引く）\n"
        "2. `## 三行まとめ`（3行以内で結論）\n"
        "3. `## 何が言いたいのか`（読む側が次に何をすればよいか）\n"
        "4. `## 結果`（決まったこと・分かったことを表や箇条書きで）\n"
        "5. `## 詳細`（根拠・仕組み・データ）\n\n"
        "規則は .claude/rules/reporting.md にあります。"
    )
    print(json.dumps({"decision": "block", "reason": reason}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:
        # 検査そのものが壊れても turn を止めない。
        sys.exit(0)
