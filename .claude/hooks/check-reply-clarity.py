#!/usr/bin/env python3
"""Claude Code の Stop hook。返答が「初見で正しく理解できる形」かを検査する。

これは何か
----------
Claude Code が1つの turn を終えようとするたびに呼ばれるプログラムである。
返答の文面を読み、**読む側が意図を掴むのに余分な労力を使う書き方**を見つけたら、
turn を終わらせずに書き直させる。

5段構成（引用 → 三行まとめ → 何が言いたいのか → 結果 → 詳細）そのものは
`maimuzo-chat-response` プラグインの Stop hook が見ている。**こちらはその上に載る検査である。**
両方が Stop で走り、どちらかが止めれば turn は終わらない。

なぜ要るか
----------
規則を文書に書いても守られず、同じ指摘を何度も繰り返すことになった。
実際に指摘された3つを、そのまま検査にしてある。

1. **issue と PR を番号だけで書く。**「#60 を最優先します」と書かれても、
   読む側は番号と内容の対応表を覚えていない。番号だけを書くのは「調べ直せ」と同じである。

2. **`## 何が言いたいのか` に、返答か質問かが書いていない。**
   読む側は「自分は何をすればよいのか」を毎回추측することになる。
   **報告（読むだけでよい）/ 質問（答えが要る）/ 確認（決定済みだが返事が要る）**の
   どれなのかを先に名乗らせる。

3. **話題が変わったのに、区切りも三行まとめも無い。**
   1つの返答で別の話題へ移るときは、`-----` の区切り線を入れ、
   **その先にも `## 三行まとめ` と `## 何が言いたいのか` を置く。**
   これが無いと、読む側は同じ話の続きなのか別の話なのかを判断できない。

どう繋がっているか
------------------
`.claude/settings.json` の `hooks.Stop` から呼ばれる。

    "hooks": {
      "Stop": [
        {"hooks": [{"type": "command",
                    "command": "python3 \"$CLAUDE_PROJECT_DIR/.claude/hooks/check-reply-clarity.py\"",
                    "timeout": 10}]}
      ]
    }

**外したいときは、この `Stop` の項目を消す。**このファイル自体は消さなくてよい。

入出力
------
- **標準入力**: Claude Code が渡す JSON。使うのは次の2つ。
    `last_assistant_message` … いま終えようとしている turn の返答の本文
    `stop_hook_active`       … 差し戻しの直後かどうか。真なら何もしない（無限ループの防止）
- **標準出力**: 止めるときだけ `{"decision": "block", "reason": "..."}` を1行。
  通すときは何も出さない。

壊れたときの振る舞い
--------------------
例外が出ても turn は止めない（fail-open）。検査が壊れて全部の turn が止まるのを避ける。
ただし黙って死なない。何が起きたかを `systemMessage` で見せる。
`REPLY_CLARITY_HOOK_DEBUG=1` のときは stderr に traceback も出す。

正規表現を使わない理由
----------------------
`#` や空白が10万文字並んだ行で破滅的なバックトラックが起きる。
行ごとの走査なら長さに比例した時間で終わる。
"""

import json
import os
import sys

# この文字数以上の返答を検査する。コードフェンスを除いた散文で数える。
# 短い確認や相槌に構成を求めると、やり取りが冗長になるだけである。
# プラグイン側の5段構成の検査と同じ値にしてある。
MIN_LEN_FOR_CHECK = 200

# stdin をこのバイト数まで読む。超えたら検査しない。
MAX_INPUT_BYTES = 1024 * 1024

# コードフェンスの開き・閉じの記号。
FENCE_MARKS = ("```", "~~~")
# フェンスを差し替える印。中身のある1行として数えられるが、見出しにも引用にも一致しない。
FENCE_MARK_LINE = "コードブロック"

SPACES = " \t　"

# 番号の参照に添える補足を囲む記号。全角と半角の両方を見る。
OPEN_PARENS = "（("
CLOSE_PARENS = "）)"

# `## 何が言いたいのか` の冒頭で名乗らせる3つのカテゴリ。
# 読む側が「自分は何をすればよいか」を最初の1語で掴めるようにする。
CATEGORY_WORDS = ("報告", "質問", "確認")

# 引用（`> `）の合計がこの文字数に満たなければ、対象が分からないと見なす。
#
# **1文だけ引いても、何の話への返答かは伝わらない。**
# 実例（2026-08-29）: 「これでなにか問題があるか検討し、問題なければ実装して良い」だけを引いたところ、
# **「引用がこれだけでは、何を指しているのかわからない。引用範囲をもっと広く取って」**と指摘された。
# その依頼は、その前に7行の仕様が並んでいて、**結びの1文だけでは仕様のほうが読み取れなかった。**
#
# **だから、結びの1文ではなく、判断の材料になった部分から引く。**
MIN_QUOTE_CHARS = 80

# 「設計 6-23b」のように、節を番号だけで指すのを止める。
# 直後に markdown link（`[path:12-34](path#L12-L34)`）が無ければ、どこを見ればよいか分からない。
SECTION_REF_WORDS = ("設計", "節")
SECTION_REF_NAME = "設計の節を番号だけで指している箇所（ファイルパスと行番号が無い）"

# ファイルを指すときは [path:12-34](path#L12-L34) の形に固定する。
# 行番号が無いと、読む側はファイルを開いてから探すことになる。
# backtick で囲むのも禁止（規則がそう定めている）。
FILE_EXTS = (
    ".go", ".md", ".py", ".json", ".yaml", ".yml", ".sh", ".ts", ".js",
    ".toml", ".txt", ".jsonl", ".sql", ".html", ".css", ".mod", ".sum",
)
FILE_REF_NAME = "ファイルの参照に行番号（#L12-L34）が無い箇所"
FILE_BACKTICK_NAME = "backtick で囲んだファイルパス"

# 話題の切れ目に入れる区切り線。行頭からこの文字だけが並ぶ行を区切りとみなす。
# 表の区切り（`| --- |`）は行頭が `|` なので当たらない。
DIVIDER_CHARS = "-"
DIVIDER_MIN = 3

# 区切り線の先に求める見出し。話題が変わったら、そこでも名乗り直す。
REQUIRED_AFTER_DIVIDER = (
    ("三行まとめ", ("三行まとめ", "3行まとめ", "３行まとめ")),
    ("何が言いたいのか", ("何が言いたいのか", "何が言いたいか")),
)

ISSUE_REF_NAME = "issue / PR の番号に内容が添えられていない箇所"
CATEGORY_NAME = "`## 何が言いたいのか` の冒頭に、報告 / 質問 / 確認のどれかを名乗っていない"
QUOTE_THIN_NAME = "引用が短く、何の話への返答かが読み取れない"
DIVIDER_NAME = "区切り線（`-----`）の先に、三行まとめと何が言いたいのかが無い"


def debug_enabled() -> bool:
    """REPLY_CLARITY_HOOK_DEBUG=1 のときだけ stderr に診断を出す。"""
    return os.environ.get("REPLY_CLARITY_HOOK_DEBUG", "") not in ("", "0", "false", "no")


def emit(obj) -> None:
    """hook の出力を stdout に書く。

    ensure_ascii=True にしてあるのは、stdio の符号化がロケール依存だからである。
    PYTHONIOENCODING=ascii の環境で日本語をそのまま書くと例外になり、検査が黙って無効化される。
    """
    sys.stdout.write(json.dumps(obj, ensure_ascii=True) + "\n")
    sys.stdout.flush()


def notify(text: str) -> None:
    """turn は止めずに、利用者へ一言見せる。"""
    emit({"systemMessage": "check-reply-clarity: " + text})


def sanitize(text, limit: int = 200) -> str:
    """systemMessage に載せる文字列を1行・短めに刈り込む。"""
    return " ".join(str(text).split())[:limit]


def is_true(value) -> bool:
    """真偽の判定。文字列の "false" / "0" / "no" を真として扱わない。"""
    if isinstance(value, str):
        return value.strip().lower() not in ("", "false", "0", "no", "off")
    return bool(value)


def heading_level(line: str) -> int:
    """行が見出しならその段（`#` の数）を返す。見出しでなければ 0 を返す。"""
    level = 0
    for ch in line:
        if ch != "#":
            break
        level += 1
    if level == 0 or level > 6:
        return 0
    rest = line[level:]
    if rest and rest[0] not in SPACES:
        return 0
    return level


def heading_text(line: str) -> str:
    """見出しの飾りを剥がした中身を返す。"""
    level = heading_level(line)
    if level == 0:
        return ""
    text = line[level:].strip(SPACES)
    text = text.strip("*").strip(SPACES)
    return text.rstrip(":：").strip(SPACES)


def is_quote_line(line: str) -> bool:
    """引用の行か。`> 原文` `>原文` `>　原文` のどれでもよい。`>` だけの行は数えない。"""
    stripped = line.lstrip(" \t")
    if not stripped.startswith(">"):
        return False
    return bool(stripped[1:].strip(SPACES))


def quote_body(line: str) -> str:
    """引用の行から `>` を剥がした中身を返す。"""
    stripped = line.lstrip(" \t")
    return stripped[1:].strip(SPACES) if stripped.startswith(">") else ""


def split_fences(msg: str):
    """(散文だけの文字列, フェンスを印に差し替えた文字列) を返す。

    散文のほうは長さの判定に使う。コードだけの返答に構成を求めないためである。
    印のほうは見出し・引用の検査に使う。フェンスの中に見出しを並べた回避を塞ぎつつ、
    「フェンス1つだけの節」を中身が空と誤判定しないよう、印を1行として残す。
    閉じ忘れたフェンスは、開いたところから末尾までをフェンス扱いにする。
    """
    prose = []
    masked = []
    fence = None
    for line in msg.split("\n"):
        head = line.lstrip(" \t")
        if fence is None:
            if head.startswith(FENCE_MARKS[0]) or head.startswith(FENCE_MARKS[1]):
                fence = head[:3]
                masked.append(FENCE_MARK_LINE)
            else:
                prose.append(line)
                masked.append(line)
        elif head.startswith(fence):
            fence = None
    return "\n".join(prose), "\n".join(masked)


def strip_inline_code(line: str) -> str:
    """インラインコード（`...`）の中身を空白に潰す。

    書き方の例として番号を見せることがあるので、説明を求める対象にしない。
    閉じ忘れたバッククォートは、開いたところから行末までをコード扱いにする。
    """
    out = []
    in_code = False
    for ch in line:
        if ch == "`":
            in_code = not in_code
            out.append(" ")
        else:
            out.append(" " if in_code else ch)
    return "".join(out)


def strip_urls(text: str) -> str:
    """URL を含むトークンを空白に潰す。

    GitHub のリンク（`.../issues/60`、`#issuecomment-…`）を番号の参照と数えないため。
    """
    out = []
    for token in text.split(" "):
        if "http://" in token or "https://" in token:
            out.append(" " * len(token))
        else:
            out.append(token)
    return " ".join(out)


def inside_parens(text: str, pos: int) -> bool:
    """pos が括弧の内側にあるか。

    「外部コメントからの実行経路（#60）」のように、内容を書いた文の末尾へ
    補足として置いた番号を通すためである。
    """
    depth = 0
    for ch in text[:pos]:
        if ch in OPEN_PARENS:
            depth += 1
        elif ch in CLOSE_PARENS and depth > 0:
            depth -= 1
    return depth > 0


def count_bare_refs(text: str) -> int:
    """1行の中の「内容を添えていない番号の参照」を数える。

    通すのは次の2つだけである。
        `#60（外部コメントからの実行経路）` — 直後に括弧で内容を添えたもの
        `…の経路（#60）`                     — 内容を書いた文の末尾に括弧で補足したもの
    """
    bare = 0
    i = 0
    n = len(text)
    while i < n:
        if text[i] != "#":
            i += 1
            continue
        j = i + 1
        while j < n and text[j].isdigit():
            j += 1
        if j == i + 1:  # `#` の後ろが数字でない（見出しや `#L12` など）
            i += 1
            continue
        k = j
        while k < n and text[k] in SPACES:
            k += 1
        if k < n and text[k] in OPEN_PARENS:  # 直後に内容を添えている
            i = j
            continue
        if inside_parens(text, i):  # 括弧の中の補足
            i = j
            continue
        bare += 1
        i = j
    return bare


def bare_issue_refs(masked: str) -> int:
    """内容を添えていない issue / PR の番号の参照を数える。

    見ないもの。
        コードフェンスの中（呼ぶ側が masked を渡す）、インラインコードの中、
        URL を含むトークン、そして引用行。
        引用は人間の原文をそのまま引くところなので、こちらでは直せない。
    """
    count = 0
    for line in masked.split("\n"):
        if is_quote_line(line):
            continue
        count += count_bare_refs(strip_urls(strip_inline_code(line)))
    return count


def quote_chars(masked: str) -> int:
    """引用の中身の合計文字数（空白を除く）を返す。"""
    total = 0
    for line in masked.split("\n"):
        if is_quote_line(line):
            total += len("".join(quote_body(line).split()))
    return total


def missing_category(masked: str):
    """`## 何が言いたいのか` の冒頭でカテゴリを名乗っていなければ True を返す。

    節が見つからなければ False（プラグイン側の5段構成の検査が別に止めるため、
    ここで二重に止めない）。
    """
    lines = masked.split("\n")
    start = None
    for i, line in enumerate(lines):
        if heading_level(line) and heading_text(line) in ("何が言いたいのか", "何が言いたいか"):
            start = i + 1
            break
    if start is None:
        return False

    for line in lines[start:]:
        if heading_level(line):
            break
        stripped = line.strip(SPACES + "*>-|　")
        if not stripped:
            continue
        # 最初の中身のある行に、3つのうちどれかが入っていればよい。
        return not any(word in stripped for word in CATEGORY_WORDS)
    return True


def bare_section_refs(masked: str) -> int:
    """節を番号だけで指している箇所を数える。

    「設計 6-23b」「節 3-64」のような書き方を拾う。
    **同じ行に markdown link（`](` を含む）があれば通す。**
    行をまたいで書くこともあるので、判定はその行だけで閉じる。

    見ないもの。
        コードフェンスの中（呼ぶ側が masked を渡す）、インラインコードの中、引用行。
    """
    count = 0
    for line in masked.split("\n"):
        if is_quote_line(line):
            continue
        text = strip_inline_code(line)
        if "](" in text:  # その行に markdown link があるなら通す
            continue
        for word in SECTION_REF_WORDS:
            i = 0
            while True:
                i = text.find(word, i)
                if i < 0:
                    break
                j = i + len(word)
                while j < len(text) and text[j] in SPACES:
                    j += 1
                # 数字 + ハイフン + 数字 の形が続くか
                k = j
                while k < len(text) and text[k].isdigit():
                    k += 1
                if k > j and k < len(text) and text[k] == "-":
                    m = k + 1
                    while m < len(text) and text[m].isdigit():
                        m += 1
                    if m > k + 1:
                        count += 1
                i = j
    return count


def looks_like_path(text: str) -> bool:
    """ファイルパスに見えるか。拡張子を持ち、空白を含まないものだけを拾う。"""
    if not text or " " in text or "\t" in text:
        return False
    lowered = text.lower()
    return any(lowered.endswith(e) or (e + "#") in lowered or (e + ":") in lowered for e in FILE_EXTS)


def file_refs_without_lines(masked: str):
    """行番号の無いファイル参照と、backtick で囲んだファイルパスを数える。

    通す形は1つだけである。
        [docs/plans/foo.md:12-34](docs/plans/foo.md#L12-L34)

    見ないもの。
        コードフェンスの中（呼ぶ側が masked を渡す）、引用行、http で始まるリンク先、
        ディレクトリ（拡張子が無いもの）。
    """
    no_lines = 0
    in_backtick = 0
    for line in masked.split("\n"):
        if is_quote_line(line):
            continue

        # backtick で囲んだファイルパス
        parts = line.split("`")
        for idx in range(1, len(parts), 2):  # 奇数番目が backtick の中身
            if looks_like_path(parts[idx]):
                in_backtick += 1

        # markdown link のリンク先
        text = strip_inline_code(line)
        i = 0
        while True:
            i = text.find("](", i)
            if i < 0:
                break
            j = text.find(")", i + 2)
            if j < 0:
                break
            target = text[i + 2:j]
            i = j + 1
            if target.startswith("http://") or target.startswith("https://"):
                continue
            if not looks_like_path(target):
                continue
            if "#l" not in target.lower():
                no_lines += 1
    return no_lines, in_backtick


def is_divider(line: str) -> bool:
    """話題の切れ目の区切り線か。行頭から `-` だけが3つ以上並ぶ行。

    表の区切り（`| --- |`）は行頭が `|` なので当たらない。
    """
    stripped = line.strip()
    if len(stripped) < DIVIDER_MIN:
        return False
    return all(ch in DIVIDER_CHARS for ch in stripped)


def blocks_missing_summary(masked: str):
    """区切り線の先で、三行まとめか何が言いたいのかが欠けているブロックの番号を返す。

    **話題が変わったら、そこでも名乗り直す。**見出しの階層は見ない。
    先頭のブロック（1つ目の話題）は、5段構成の検査（別の hook）が見ているので、ここでは見ない。
    """
    blocks = []
    current = []
    for line in masked.split("\n"):
        if is_divider(line):
            blocks.append(current)
            current = []
        else:
            current.append(line)
    blocks.append(current)

    if len(blocks) < 2:
        return []

    bad = []
    for i, block in enumerate(blocks[1:], start=2):
        body = "\n".join(block)
        # 中身がほとんど無いブロック（署名や1行の補足）は求めない。
        if len("".join(body.split())) < MIN_LEN_FOR_CHECK // 2:
            continue
        labels = set()
        for line in block:
            text = heading_text(line)
            if not text:
                continue
            for name, variants in REQUIRED_AFTER_DIVIDER:
                if text in variants:
                    labels.add(name)
        if len(labels) < len(REQUIRED_AFTER_DIVIDER):
            bad.append(i)
    return bad


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


def build_reason(bare_refs, no_category, thin_quote, late_blocks, section_refs=0, file_no_lines=0, file_backtick=0, qchars=0) -> str:
    """block したときに Claude へ返す指示文。

    入力由来の文字列を混ぜない。件数だけは int に通してから %d で埋める。
    ブロックの番号だけは、どこを直せばよいかが分からなくなるので載せる（数値なので安全）。
    """
    parts = ["返答が「初見で理解できる形」になっていません。**書き直してください。**\n"]

    if bare_refs:
        parts.append("\n%s: %d 件\n" % (ISSUE_REF_NAME, int(bare_refs)))
        parts.append(
            "\n**issue と PR を番号だけで書かないこと。**同じ文の中に「何の話か」を添える。\n"
            "  悪い: #60 を最優先します\n"
            "  良い: #60（外部コメントからの実行経路）を最優先します\n"
            "  良い: 外部コメントからの実行経路（#60）を最優先します\n"
            "**初出でも2度目以降でも添えること。**表の中でも同じです。\n"
        )

    if no_category:
        parts.append("\n%s\n" % CATEGORY_NAME)
        parts.append(
            "\n**`## 何が言いたいのか` は、まず次のどれかを名乗ってから1行でまとめること。**\n"
            "  **報告** … 読んで理解してもらえればよい。返事は要らない\n"
            "  **質問** … 答えをもらわないと先へ進めない\n"
            "  **確認** … こちらで決めたが、違っていたら言ってほしい\n"
            "書き方の例: `**報告。**workflow の報告に誤りがあったので、原文で確かめ直しました。`\n"
            "**これが無いと、読む側は自分が何をすればよいのかを毎回推し量ることになります。**\n"
        )

    if thin_quote:
        parts.append("\n引用が短く、何の話への返答かが読み取れない\n")
        parts.append(
            "\n**引用は、判断の材料になった部分から引くこと。**\n"
            "結びの1文だけを引いても、何の話への返答かは伝わりません。\n"
            "\n"
            "**引用の合計は %d 文字以上にすること**（空白は数えません）。\n"
            "**いまの引用は %d 文字です。**\n"
            "\n"
            "**箇条書きで指示が来たら、判断に効いた項目をそのまま引く。**\n"
            "**長すぎるときは途中を飛ばしてよいが、飛ばしたぶんは文字数に数えられません。**\n"
            "足りなければ、判断に効いた行をもう1つ引くこと。\n"
            "\n"
            "  悪い（結びだけ）:\n"
            "    > これでなにか問題があるか検討し、問題なければ実装して良い\n"
            "\n"
            "  良い（判断の材料から引く）:\n"
            "    > - 判定は5時間枠、1週間全体枠の2つ。\n"
            "    > - 5時間余裕値=5時間枠-5時間マージン、…\n"
            "    > - 判定スコア=5時間余裕値*2+1週間余裕値とし、…\n"
            "    > これでなにか問題があるか検討し、問題なければ実装して良い\n"
            % (MIN_QUOTE_CHARS, int(qchars))
        )

    if late_blocks:
        nums = " / ".join("%d つ目" % int(n) for n in late_blocks[:5])
        parts.append("\n%s: %s の話題\n" % (DIVIDER_NAME, nums))
        parts.append(
            "\n**1つの返答で別の話題へ移るときは、`-----` の区切り線を入れ、"
            "その先にも `## 三行まとめ` と `## 何が言いたいのか` を置くこと。**\n"
            "見出しの階層（`##` か `###` か）は関係ありません。**話題が変わったかどうかで判断してください。**\n"
            "**これが無いと、読む側は同じ話の続きなのか別の話なのかを判断できません。**\n"
        )

    if section_refs:
        parts.append("\n%s: %d 件\n" % (SECTION_REF_NAME, int(section_refs)))
        parts.append(
            "\n**設計の節を番号だけで指さないこと。**"
            "読む側は、どのファイルの何行目かを毎回訊き直すことになります。\n"
            "**markdown link 形式で、行番号を含めて書いてください。**\n"
            "  悪い: 設計 6-23b に書きました\n"
            "  良い: [docs/plans/continuo_design.md:8292-8324](docs/plans/continuo_design.md#L8292-L8324) に書きました\n"
            "同じ行に markdown link があれば通ります。\n"
        )

    if file_no_lines:
        parts.append("\n%s: %d 件\n" % (FILE_REF_NAME, int(file_no_lines)))
        parts.append(
            "\n**ファイルを指すときは、行番号まで書くこと。**\n"
            "  悪い: [docs/plans/continuo_design.md](docs/plans/continuo_design.md)\n"
            "  良い: [docs/plans/continuo_design.md:8292-8324](docs/plans/continuo_design.md#L8292-L8324)\n"
            "**行番号が無いと、読む側はファイルを開いてから探すことになります。**\n"
        )

    return "".join(parts)


def main() -> int:
    payload = read_payload()
    if not payload:
        return 0

    # 既に block した結果の再実行なら、そのまま通す（無限ループを避ける）。
    if is_true(payload.get("stop_hook_active")):
        return 0

    raw_msg = payload.get("last_assistant_message")
    if raw_msg is None:
        return 0
    if not isinstance(raw_msg, str):
        notify(
            "last_assistant_message が文字列ではありません（type=%s）。"
            "この turn の検査は飛ばしました。" % type(raw_msg).__name__
        )
        return 0

    msg = raw_msg.replace("\r\n", "\n").replace("\r", "\n")
    prose, masked = split_fences(msg)

    if len(prose) < MIN_LEN_FOR_CHECK:
        return 0

    bare_refs = bare_issue_refs(masked)
    no_category = missing_category(masked)
    # **引用が1文字も無い場合も止める。**
    # 0 を見逃すと、**引用を消すのがいちばん安い逃げ道になる。**
    # 実例（2026-08-29 のレビュー）: 閾値を 80 へ上げた結果、
    # 「40文字だけ正直に引く」は止まり、「1文字も引かない」は通る状態になっていた。
    # **罰する範囲だけを広げて、逃げ道を残してはならない。**
    qchars = quote_chars(masked)
    thin_quote = qchars < MIN_QUOTE_CHARS
    late_blocks = blocks_missing_summary(masked)
    section_refs = bare_section_refs(masked)
    file_no_lines, _unused_backtick = file_refs_without_lines(masked)

    if (not bare_refs and not no_category and not thin_quote and not late_blocks
            and not section_refs and not file_no_lines):
        return 0

    emit({
        "decision": "block",
        "reason": build_reason(bare_refs, no_category, thin_quote, late_blocks, section_refs,
                               file_no_lines, 0, qchars),
    })
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except SystemExit:
        raise
    except Exception as exc:  # 検査が壊れても turn は止めない
        if debug_enabled():
            import traceback

            traceback.print_exc(file=sys.stderr)
        try:
            notify(
                "検査が失敗しました（%s: %s）。この turn は素通しにしました。"
                % (type(exc).__name__, sanitize(exc))
            )
        except Exception:
            pass
        sys.exit(0)
