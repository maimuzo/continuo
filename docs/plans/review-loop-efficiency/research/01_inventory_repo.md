# レビューループを定義している箇所（リポジトリ側の一覧）

**言いたいこと。**レビューループの定義は、開発者向けの5ファイル（約880行・約60KB）に、写しを含めて散らばっている。
**収束の判定（Critical と High が0件）を支える「重大度の決め方」は、利用者向けの [internal/prompt/builtin.md](../../../../internal/prompt/builtin.md) が4段の表として持っている**（`origin/main` の時点から在る。2026-09-22 に数え直した）。
**開発者向けの [CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) には無い。書く側が最初のレビューへ出す前の自己点検は、どちらにも無い。**
**必須の道具 `/code-review` には、1回で全部挙げさせる指示も、前の周の対応表も、effort level も渡っておらず、機械の関門は目印と投稿者しか見ていない。**

---

## 0. 前提

| 何 | 値 |
| --- | --- |
| **対象コミット** | `df36f9d7`（`git rev-parse HEAD` の出力は `df36f9d7ec971e82ee0b680053a29ac3f9d35eb7`。origin/main を切り出した worktree） |
| **数えた範囲** | `~/Sources/github/continuo/.claude/worktrees/review-loop-efficiency` の追跡ファイル全部（`git grep`）。**開いている PR のうち、CLAUDE.md / .claude/ / internal/prompt/builtin.md を触る6本の差分**（`gh pr list --state open` の files で絞り、`gh pr diff` で取った） |
| **見なかったもの** | メモリ・プラグイン・個人設定（別の worker の担当） |
| **書いたもの** | このファイルだけ。git と gh の書き込みはしていない |

### 読んだうえで答える問い

1. **この作業に当てはまる規則。**worker-briefing の 2-4（指示に名前が出ていないものも探す）、2-5（同じものが他に無いかを数え、件数・検索パターン・範囲を書く）、3 の公開情報の制約、[.claude/rules/reporting.md:554-562](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/reporting.md#L554-L562) の根拠の付け方、同じファイルの「測る前に断定しない」。2-6 と 2-7 はレビューを頼まれた worker 向けで、この作業はレビューではないので、指摘の形ではなく根拠つきの一覧として書く。
2. **飛ばしてよい段。**[.claude/rules/design-review.md:5-17](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L5-L17) の9段は issue の実装作業の手順で、調査だけのこの作業には当たらない。CLAUDE.md の「hook の挙動が変化する変更」の検討も、コードを1行も変えないので当たらない。
3. **公開してよくない情報を書きうる場面。**このファイルは公開リポジトリの worktree に置かれる。worktree と scratchpad の絶対パスを書きうるので、`~/` に直した。PR の差分に出てくる実在のリポジトリ名は、既に履歴に入った文の引用としてだけ扱い、書き足していない。
4. **探し方と読んだもの。**
   - 候補を出した検索: `git grep -n -c -E 'code-review|収まっ|Critical|連続10回|3・6・9|対応表|敵対的|design-review-result|code-review-result|周目|最後の1回|レビュワー'`（リポジトリ全体）
   - 全文を読んだもの: CLAUDE.md 380-774、design-review.md、pr-review-and-merge/SKILL.md、worker-briefing/SKILL.md、review-gate.yml、block-merge-without-review.py、check-release-ready.sh、settings.json
   - 部分を読んだもの: builtin.md 1-40・95-380・470-490・775-800、scaffold/template.go 225-289、FAQ.md 200-250、CONTRIBUTING.md 120-150、upgrading.md 1168-1192、continuo_design.md 10460-10470・10805-10860・11088-11105
   - PR の差分: PR #256・#272・#267 は規則ファイルの差分を全部読んだ。PR #255・#230・#254 は、レビューの語を含む変更行だけを grep で見た
   - 読めなかったもの: 無い。**`/code-review`（Claude Code に同梱の skill）の出力の形は、実行していないので見ていない**

---

## 1. いちばん重要な発見

**言いたいこと。**5つある。どれも「規則を足しても1〜2回で収まらない」理由の候補である。

| 発見 | 根拠の在りか |
| --- | --- |
| **収束の判定が、定義の無いラベルに乗っている。**「収まっている」は Critical と High が0件のこと（[CLAUDE.md:568](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L568)）だが、何を Critical / High にするかの基準が [CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) に無い。**利用者向けの [internal/prompt/builtin.md](../../../../internal/prompt/builtin.md) には4段の表として在る**（2026-09-22 に数え直した）。付けるのはレビュワーである | 3-2 |
| **書く側が最初のレビューへ出す前の自己点検の段が無い。**「同じものを数える」（worker-briefing 2-5）は、指摘を受けてから・指摘する前に効くもので、指摘が無ければ発火しない | 3-1 |
| **必須の道具 `/code-review` に、徹底度の指示も前の周の対応表も effort level も渡っていない。**規則自身が「渡さないと同じものが必ずまた挙がり、周だけが増える」と書いている | [.claude/rules/design-review.md:143-146](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L143-L146)、[.claude/skills/pr-review-and-merge/SKILL.md:99-103](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L99-L103)、3-3 |
| **機械の関門は、目印が先頭にあるかと投稿者しか見ない。**周回数・重大度の件数・「数えた件数」の行は、どの機械も検査しない | 2-5 の表と、その下の grep |
| **コードレビュー記録フロー（[CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md)）の中に、1回目のレビューの前に効く段は無い。**6段・4段・判定後の表・10回停止は、全部「収まらなかったあと」の手順である。設計側だけは、書く前の2節（[.claude/rules/design-review.md:57-79](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L57-L79)）を持つ | 2-4 |

---

## 2. 定義している箇所

### 2-1. レビューの起動条件（いつ・何を・誰が・どの道具で）

**言いたいこと。**実装レビューは `/code-review` が必須、設計レビューは architect、利用者向けは general-purpose が既定である。
**実装レビューの道具は、同じ規則群の中で2通りに書かれている**（5章）。

| 場所 | 何を定義しているか | 原文の引用 | 区分 |
| --- | --- | --- | --- |
| [CLAUDE.md:394-419](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L394-L419) | 実装レビューの起動。draft で作り、`/code-review` を通し、結果を貼り、収まるまで段2〜4を繰り返す | 「PR を出すときは、必ず `/code-review` でレビューする。」 | 開発者向け |
| [CLAUDE.md:509](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L509) | 受け取った道具が何でも、記録フローは同じ | 「`/code-review` / `code-reviewer` / `security-reviewer` のどれで受けたときも同じである。」 | 開発者向け |
| [.claude/rules/design-review.md:5-20](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L5-L20) | issue の作業の9段。段3で設計レビュー、段7で `/code-review` | 「設計をサブエージェントにレビューさせる」 | 開発者向け |
| [.claude/rules/design-review.md:83-99](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L83-L99) | 設計レビューの担当と渡し方（ファイルに落とす） | 「`maimuzo-from-ecc:architect` に渡す。」 | 開発者向け。**maimuzo の環境のプラグインを名指ししている** |
| [.claude/rules/design-review.md:115-120](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L115-L120) | 実装レビューは architect ではなく、Bash を持つエージェントに頼む | 「Bash を持つエージェント（`general-purpose` など）を立てること。」 | 開発者向け |
| [.claude/rules/design-review.md:239-246](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L239-L246) | 設計レビューを飛ばしてよい場合 | 「迷ったら飛ばさない。」 | 開発者向け |
| [.claude/skills/pr-review-and-merge/SKILL.md:93-131](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L93-L131) | `/code-review <PR 番号>` の叩き方。プロンプトを足せない。`ultra` は人間の指示があるときだけ | 「`/code-review` は自由なプロンプトを足せない。」 | 開発者向け |
| [internal/prompt/builtin.md:127-134](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L127-L134) | continuo が起動したエージェントの計画レビュー（3-2） | 「敵対的レビューの subagent に計画をレビューさせる」 | 利用者向け |
| [internal/prompt/builtin.md:334-343](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L334-L343) | PR のレビュー（3-6）。**観点は具体的に書かず、エージェントに書き換えさせる** | 「差分に当たる観点へ書き換えて渡してください。」 | 利用者向け |
| [internal/prompt/builtin.md:199](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L199) と [internal/prompt/builtin.md:1325](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L1325) | どの subagent に頼むかは WORKFLOW.md の本文。既定は general-purpose | 「どの subagent へレビューを頼むか（書いていなければ general-purpose）」 | 利用者向け |
| [internal/scaffold/template.go:281-287](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/scaffold/template.go#L281-L287) | 雛形の `### レビューを頼む subagent`。名前を書かせる案内だけ | 「このリポジトリで使う名前を書いてください。」 | 利用者向け |

### 2-2. レビュワーへの指示

**言いたいこと。**徹底度（2-6）と根拠（2-7）と数え方（2-5）は worker-briefing にある。
**ただし必須の `/code-review` にはどれも渡せず、受け取る側が補う形になっている。**

| 場所 | 何を定義しているか | 原文の引用 | 区分 |
| --- | --- | --- | --- |
| [.claude/rules/design-review.md:101-113](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L101-L113) | 設計レビューの4観点・根拠の添え方・読むだけ・worker-briefing を読ませる | 「実装しても目的を達しない記述（守りが1箇所だけ抜けている、など）」 | 開発者向け |
| [.claude/skills/worker-briefing/SKILL.md:256-275](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L256-L275)（2-6） | 1回で全部挙げる。前の周に在ったか、直しが持ち込んだかを分類する | 「まったく同じ結果になるくらい徹底的に洗い出すこと。」 | 開発者向け |
| [.claude/skills/worker-briefing/SKILL.md:277-298](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L277-L298)（2-7） | 指摘ごとに「直さなかったら誰が何を失うか」で根拠を書く | 「「規約に反する」だけでは根拠にならない。」 | 開発者向け |
| [.claude/skills/worker-briefing/SKILL.md:183-254](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L183-L254)（2-5） | 指摘する前に同じものを全部数え、件数・パターン・範囲を書き、1件にまとめる | 「指摘する前・直す前に、その主張が当てはまる場所を全部数える。」 | 開発者向け |
| [.claude/skills/worker-briefing/SKILL.md:349](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L349) | 2周目以降は、前の周の対応表を渡す | 「渡さないと、否定した指摘が毎周また挙がる」 | 開発者向け |
| [.claude/rules/design-review.md:143-149](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L143-L149) | 前の周の対応表を渡す。`/code-review` では渡せないので、受け取る側が突き合わせる | 「`/code-review` では渡せない。」 | 開発者向け |
| [.claude/skills/pr-review-and-merge/SKILL.md:99-120](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L99-L120) | 2-6 と 2-7 は `/code-review` へ渡せない。根拠・分類・数えた件数が無くても落とさず、受け取る側で補う | 「落とすのではなく、こちらで補って対応表に載せる。」 | 開発者向け |
| [.claude/rules/reporting.md:564-583](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/reporting.md#L564-L583) | worker の報告（レビューを含む）を落とす条件と、読み替える条件 | 「同じ誤りが他に無いかを数えていない指摘」 | 開発者向け |
| [internal/prompt/builtin.md:187-199](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L187-L199) | 計画レビューへ渡す文面（4観点・根拠・読むだけ）。**徹底度・数える・分類は、この pull request が 5-6「レビュワーへ何を求めるか」へ入れ、この行から指すようにした。**重大度の指示だけが無い（2026-09-22 に数え直した） | 「計画の穴を探してください。」 | 利用者向け |

### 2-3. 指摘を受けた側（直す側）への指示

**言いたいこと。**「表を書いてから直す」「数えた件数の全部を直す」「根拠を否定できるなら直さない」が開発者向けの柱である。
**利用者向けにも「同じものを数える」指示は在る**（2026-09-22 に数え直した）。
[internal/prompt/builtin.md](../../../../internal/prompt/builtin.md) の「直す前に書くこと」の段2 が `origin/main` の時点から持っており、
この pull request が 5-6 の「レビュワーへ何を求めるか」の段3 と、前提を1文にして探す段を足した。

| 場所 | 何を定義しているか | 原文の引用 | 区分 |
| --- | --- | --- | --- |
| [CLAUDE.md:520-532](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L520-L532) | 手順5段。表を書く前に数える、件数を添える、数えた全部を直す、人間へ報告 | 「段1で数えた件数の全部を直す（1箇所だけ直さない）」 | 開発者向け |
| [CLAUDE.md:534-543](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L534-L543) | 対応表の6列（短縮名・レベル・指摘内容・直す/直さない・合理的理由・分類） | 「Critical / High / Medium / Low / Info」 | 開発者向け |
| [CLAUDE.md:545-551](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L545-L551) | Critical と High は直す。それ以下は簡単なら直し、設計に触るなら follow-up の issue | 「「この pull request の範囲外である」は否定ではない。」 | 開発者向け |
| [.claude/rules/design-review.md:124-149](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L124-L149) | 合理的根拠を否定できるなら直さない | 「否定できるなら直さない。」 | 開発者向け |
| [.claude/skills/worker-briefing/SKILL.md:211-254](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L211-L254) | 直したあとに数えるもの（言い換え・戻り値の説明・移した節を指す文・版・後ろのリンク）。前提を1文にしてから探す | 「数えるのは「前提」である。」 | 開発者向け |
| [.claude/skills/worker-briefing/SKILL.md:105-121](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L105-L121)（2-1） | 3回で収まらないのは設計があやふやだから。1件ずつ潰すのをやめる | 「指摘を1件ずつ潰すのをやめて、設計を疑う。」 | 開発者向け |
| [.claude/skills/pr-review-and-merge/SKILL.md:168-188](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L168-L188)（段4） | CLAUDE.md の記録フローに従う（要点の写しを持つ） | 「ここには写さない。」 | 開発者向け |
| [.claude/rules/plan-file.md:93-135](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/plan-file.md#L93-L135) | 設計文書へ行を足したら、リンクを全部検算する（直しが持ち込む欠陥の予防） | 「8周のレビューのうち3周が、同じ原因である。」 | 開発者向け |
| [internal/prompt/builtin.md:131](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L131) | 指摘を全部直そうとせず、1件ずつ判断する | 「1件ずつ「直すのが妥当か」を判断する」 | 利用者向け |
| [internal/prompt/builtin.md:203-234](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L203-L234) | 判断票の7列と、その見本 | 「判断票の形。**1行目と2行目の並びを変えないでください。**」。**書き換える前の引用「Critical と High は原則すべて直します。」のほうは、builtin.md に0件である**（2026-09-22 に `Critical と High` と `原則` で数えて0件。origin/main でも0件）。**ただし同じことを言う文は在る**——「『直さない』と決めてよいのは、レビュワーの根拠を否定できたときだけです。…critical と high では使えません」と、「何周回すか」の表の「critical か high が1件以上 → 直して次の周を回す」の2つである | 利用者向け |
| [internal/prompt/builtin.md:519-524](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L519-L524)（5-2） | issue に無い実装は根拠をレビュワーへ。否定されたら実装を変える | 「**レビュワーに否定されたら、実装を変えてください。**判定のしかたは 5-6 にあります。」 | 利用者向け |

### 2-4. 収束の定義・回数・止まる条件

**言いたいこと。**正は [CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) で、ほかは写しか要点である。
**利用者向けの builtin.md にも、回数・収束・停止の定義が在る**（2026-09-22 に数え直した）。
[internal/prompt/builtin.md:740](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L740) が収束、[internal/prompt/builtin.md:939-950](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L939-L950) が何周回すか、
[internal/prompt/builtin.md:950](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L950) が連続10回で止まる、である。**下の根拠は、そこに書いてあるコミット（`df36f9d7`）の時点では正しい。**

| 場所 | 何を定義しているか | 原文の引用 | 区分 |
| --- | --- | --- | --- |
| [CLAUDE.md:566-571](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L566-L571) | 「収まっている」の定義（正） | 「Critical と High が0件であることをいう。」 | 開発者向け |
| [CLAUDE.md:573-611](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L573-L611) | 収まったあとは最大1周。Medium / Low を直したら最後に1回。最後の周の Medium / Low は直さない | 「そこから先は最大1周である。」 | 開発者向け |
| [CLAUDE.md:613-639](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L613-L639) | 収まらないときの数え方。最後の1回も10回に数える | 「10回を超えて回してはならない。」 | 開発者向け |
| [CLAUDE.md:640-661](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L640-L661) | 3・6・9回目の6段（目的の確認役 → 理由をまとめる → 説得 → 削除と記録 → 報告 → 設計レビューへ戻る） | 「実装を止めて設計内容を敵対的レビューし、実装し直してから次のレビューを回す。」 | 開発者向け |
| [CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) | 設計と実装を別に数える。人間の方針変更でリセット。止まったときに人間へ見せる4項目 | 「足して20回まで、という意味ではない。」 | 開発者向け |
| [CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) | 3回で通らなかったときの4段と、判定後の2通り | 「PR の diff を見せない。」 | 開発者向け |
| [CLAUDE.md:412-418](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L412-L418) | PR 手順の段4に、収束と最後の1周の要点 | 「ただし Medium か Low を1件でも直したなら、最後の1回は必ず回す。」 | 開発者向け |
| [CLAUDE.md:310-317](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L310-L317) | 作業の進め方の要約4点 | 「3回ごとに issue と実装を突き合わせ直す。」 | 開発者向け |
| [.claude/rules/design-review.md:153-202](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L153-L202) | 回す回数（要点の写しと、3・6・9 / 10回の絶対条件） | 「2回で止めてよいかを訊かない。」 | 開発者向け |
| [.claude/rules/design-review.md:206-237](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L206-L237) | 3回で収まらなかったとき。回数と設計の大きさが比例した実測 | 「効くのは「捨てられるものを挙げろ」と問うことである。」 | 開発者向け |
| [.claude/skills/worker-briefing/SKILL.md:117-120](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L117-L120) | 3・6・9回目に6段へ入る | 「入るのは3回目だけではない。」 | 開発者向け |
| [.claude/skills/pr-review-and-merge/SKILL.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md) | 最大1周と10回の要点の写し。3・6・9回目のあとの進み方 | 「6段を終えてから段5へ進む。」 | 開発者向け |

**`df36f9d7` の時点では builtin.md に回数の定義が無かった、という根拠。**パターン `収まっ|周目|何周|10回|3回|回数|最後の1回|止まる`、対象 [internal/prompt/builtin.md](../../../../internal/prompt/builtin.md)、コミット `df36f9d7` で **0行**だった。**`origin/main` では13行、いまは25行である**（2026-09-22 に数え直した）。

### 2-5. 記録と機械の関門

**言いたいこと。**機械は3箇所（hook・CI・リリース前の検査）で、どれも「先頭の目印」と「投稿者」の2条件だけを数える。
**この hook は 2026-09-21 に廃止したので、いまは2箇所である**（CI とリリース前の検査）。
**周回数・重大度・対応表の中身は、どの機械も見ていない。**

| 場所 | 何を定義しているか | 原文の引用 | 区分 |
| --- | --- | --- | --- |
| [CLAUDE.md:421-472](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L421-L472) | 貼ったことが唯一の証拠。3箇所で止める。数える条件2つ。逃がし口の環境変数（**いずれも 2026-09-21 に2箇所へ減り、逃がし口は無くなった**） | 「3 を飛ばしたものは、レビューを実施していないものとして扱う。」 | 開発者向け |
| [CLAUDE.md:553-563](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L553-L563) と [CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) | 対応表を PR のコメントへ残す理由。何周目かを書く。設計レビューの回数を PR 本文へ写す | 「回数はレビュー結果のコメントへ書く。」 | 開発者向け |
| `.claude/hooks/block-merge-without-review.py:212-223`（**2026-09-21 に廃止。**リンクを外した） | `gh pr merge/ready <番号>` の前に、先頭の目印と投稿者を見る | `if not isinstance(body, str) or not MARKER_RE.match(body):` | 開発者向け |
| `.claude/settings.json:86-95`（**2026-09-21 に hooks ごと廃止。**リンクを外した） | 上の hook を `PreToolUse` の Bash に張る | `"matcher": "Bash"` | 開発者向け |
| [.github/workflows/review-gate.yml:66-231](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.github/workflows/review-gate.yml#L66-L231) | `design-review-result`。断りの目印か、紐づく issue のどれか1件に目印 | `(<!-- continuo:agent -->[ \\t\\r\\n]*)?<!-- design-review-result -->` | 開発者向け |
| [.github/workflows/review-gate.yml:236-316](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.github/workflows/review-gate.yml#L236-L316) | `code-review-result`。PR のコメントに先頭の目印 | `test("^[ \\t\\r\\n]*<!-- code-review-result -->")` | 開発者向け |
| [scripts/check-release-ready.sh:115-142](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/scripts/check-release-ready.sh#L115-L142) | タグの前に、区間の PR に目印があるか | 同じ正規表現 | 開発者向け |
| [docs/releasing.md:52](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/releasing.md#L52) と [docs/releasing.md:212-246](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/releasing.md#L212-L246) | リリース手順の中の検査 | 「`/code-review` を回し直し、結果をその PR のコメントへ貼る。」 | 開発者向け |
| [.claude/skills/pr-review-and-merge/SKILL.md:133-166](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L133-L166)（段3） | 貼るコメントの形（目印・題名・周目・対応表）。`--body-file` で貼る | 「何周目かと対応表を同じコメントに入れる」 | 開発者向け |
| [CONTRIBUTING.md:133-150](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CONTRIBUTING.md#L133-L150) | 外部の貢献者向けの2つの検査と数える条件 | 「レビュー結果が貼られていない PR は、CI が落とします。」 | 開発者向け（外部の貢献者） |
| [internal/scaffold/ci_template.go:83](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/scaffold/ci_template.go#L83) と [internal/scaffold/ci_template.go:242](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/scaffold/ci_template.go#L242) | 利用者へ配る同じ2つの検査。本体と条件を揃える（[.github/workflows/review-gate.yml:11-14](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.github/workflows/review-gate.yml#L11-L14)） | 「判定の条件（正規表現と投稿者の絞り込み）を、雛形と1文字も違えないこと。」 | 利用者向け |
| [internal/prompt/builtin.md:203-223](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L203-L223) と [internal/prompt/builtin.md:347-365](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L347-L365) | 判断票の目印の置き方（計画は2行目、実装は1行目） | 「1行目と2行目の並びを変えないでください。」 | 利用者向け |
| [docs/FAQ.md:201-250](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/FAQ.md#L201-L250) | 利用者が自分の CLAUDE.md に書く決まりの例 | 「この2つの目印を、機械が数えます。」 | 利用者向け |
| [docs/plans/continuo_design.md:11474-11551](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/continuo_design.md#L11474-L11551)（5-3p / 5-3q） | 利用者向けの CI と印の設計。既定のレビュワー | 「既定のレビュワーは general-purpose である」 | 利用者向け（設計） |
| [docs/plans/continuo_design.md:11799-11800](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/continuo_design.md#L11799-L11800) | CLAUDE.md の「draft → `/code-review` → ready」は、このリポジトリの決まりで配らない | 「このリポジトリの決まりであって、配るものではない。」 | 開発者向けと利用者向けの境界 |

**機械が周回と重大度を見ていない根拠。**パターン `周目|回目|round|Critical|High|severity|対応表`、
対象は block-merge-without-review.py・review-gate.yml・check-release-ready.sh・ci_template.go・test_block_merge_without_review.py・test_marker_pattern_parity.py、コミット `df36f9d7` で **0行**だった。

---

## 3. 無いもの

### 3-1. 書く側の「レビューへ出す前」の自己点検

**言いたいこと。**無い。最初のレビューを頼む前に、書いた本人が diff を点検する段はどのファイルにも無い。
**近いものは4つあるが、どれも目的が違う。**

**根拠（検索パターン・対象パス・対象コミット）。**

| 検索パターン | 対象パス | 出たもの |
| --- | --- | --- |
| `自己点検\|セルフレビュー\|self-review\|self review\|レビューに出す前\|レビューへ出す前\|レビューの前に\|出す前に` | リポジトリ全体（docs/evidence を除く） | [CONTRIBUTING.md:123](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CONTRIBUTING.md#L123) の「出す前に」（gofmt / vet / test）と、builtin.md の「`review` を出す前に push / PR を作る」系だけ。**書いたものの点検は0件** |
| `レビュー(を\|に\|へ)(頼む\|出す\|回す\|依頼する\|渡す)前\|見直(す\|して\|し)\|読み直(す\|して\|し)\|セルフチェック\|self-check\|自分でレビュー\|自己レビュー\|提出前` | CLAUDE.md・.claude/・internal/prompt/builtin.md・internal/scaffold/template.go・CONTRIBUTING.md・docs/releasing.md | 3行。[.claude/rules/reporting.md:707](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/reporting.md#L707)（表の列を読み直す）と、template.go の101行と201行（ポーリング間隔）。**レビューとは関係が無い** |

対象コミットはどちらも `df36f9d7`。

| 近いもの | 何か | なぜ自己点検ではないか |
| --- | --- | --- |
| [CONTRIBUTING.md:123-129](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CONTRIBUTING.md#L123-L129) | gofmt / go vet / test-like-ci | 機械の検査で、内容の点検ではない |
| [.claude/skills/worker-briefing/SKILL.md:183-185](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L183-L185)（2-5） | 「指摘する前・直す前に」同じものを数える | **起点が指摘である。**指摘が来るまで発火しない |
| [.claude/rules/design-review.md:57-79](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L57-L79) | 設計を書く前に疑う・既存の決定を探す | 設計の前段。**実装側に同じものは無い** |
| [.claude/skills/worker-briefing/SKILL.md:161-181](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L161-L181)（2-4） | 着手前に関連ファイルを読む | 下読みであって、書いたものの点検ではない |

### 3-2. 重大度の判定基準

**言いたいこと。**開発者向けには無い。[CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) には Critical / High / Medium / Low / Info の名前だけがあり、何をどれにするかを決めていない。
**利用者向けの [internal/prompt/builtin.md](../../../../internal/prompt/builtin.md) には、4段の表として在る**（2026-09-22 に数え直した）。
**それでも「収まっている」は Critical と High の件数だけで決まり、設計レビューにも同じ判定が効く**（[CLAUDE.md:665](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L665)）。

| 検索パターン | 対象パス | 出たもの |
| --- | --- | --- |
| `Critical とは\|High とは\|重大度\|深刻さ\|severity\|Severity` | CLAUDE.md・.claude/・builtin.md・scaffold/template.go・docs/FAQ.md・CONTRIBUTING.md | **いまの HEAD で0件**（2026-09-22 に数え直した。`df36f9d7` では `深刻さ` が1件当たった）。重さの定義は、この語では引けないが、[internal/prompt/builtin.md:733-738](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L733-L738) の4段の表が、**この pull request の前から origin/main に在る**（2026-09-22 に数え直した） |
| `Critical(:\| =\|とする\|に当たる\|の基準)\|レベルの(決め方\|基準\|定義)\|レベルを決め\|重さの(基準\|定義)\|Info` | CLAUDE.md・.claude/・builtin.md | [CLAUDE.md:539](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L539) の対応表の列の定義1件だけ |

対象コミットはどちらも `df36f9d7`。

- **レビュワーへレベルを付けさせる指示は無い。**これは欠けではなく決めごとで、5-6 が「重さは、受け取った側が付け直してください。レビュワーが付けた重さは、そのまま使いません」と書いている（`origin/main` から在る）。設計レビューの4観点（[.claude/rules/design-review.md:101-106](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L101-L106)）にも、builtin.md の計画レビューの文面（[internal/prompt/builtin.md:187-199](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L187-L199)）にも、レベルの語が入っていない
- **[CLAUDE.md:539](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L539) は Info も挙げるが、収まったあとの表（[CLAUDE.md:577-583](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L577-L583)）は Info の扱いを書いていない**
- worker-briefing 2-5 の段4（[.claude/skills/worker-briefing/SKILL.md:199](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L199)）の「いちばん重いレベル」も、定義を指していない
- **`/code-review` の出力にどのレベル名が出るかは、測っていない**（実行していない）。pr-review-and-merge/SKILL.md に、出力のレベルを読み替える記述は無い（93-131行を読んだ）

### 3-3. `/code-review` の effort level

**言いたいこと。**指定している箇所は無い。規則は `/code-review <PR 番号>` とだけ書く。

| 検索パターン | 対象パス | 出たもの |
| --- | --- | --- |
| `/code-review +(low\|medium\|high\|max\|ultra\|xhigh)` | リポジトリ全体 | **0件** |
| `/code-review` | リポジトリ全体 | 19行。**どれも level を付けていない** |
| `effort`（大文字小文字を問わない） | CLAUDE.md・.claude/・docs/・internal/prompt/・scripts/・.github/ | 規則側は [.claude/skills/pr-review-and-merge/SKILL.md:99](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L99)（受け取る引数の列挙）と [.claude/skills/pr-review-and-merge/SKILL.md:130-131](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L130-L131)（`ultra` は人間が明示したときだけ）。残りは docs/evidence の実測ログと、設計文書の hook 入力の表 |

対象コミットは `df36f9d7`。

**このセッションの skill 一覧に出ている `code-review` の説明は、level を省いたときの挙動をこう書いている。**

> "with no level given, it reuses the level you typed last"
> （訳: **level を渡さなければ、最後に打った level を使い回す。**）
>
> "low/medium: fewer, high-confidence findings; high→max: broader coverage, may include uncertain findings"
> （訳: low と medium は件数を絞り、確度の高い指摘だけを出す。**high から max は広く拾い、不確かな指摘も含む。**）

**つまり、1回のレビューが拾う範囲は、規則ではなく直前に打った level で決まる。**
これは説明文を読んだだけで、**実行して確かめてはいない。**

---

## 4. 重複（同じ定義の写し）

**言いたいこと。**収束と回数の規則は、正の CLAUDE.md のほかに3ファイルへ写されている。
**「ここには写さない」と書いた直後に「要点だけ」で写している箇所が4つあり、写しは書式の崩れ（`****`）まで同じ形で複製している。**

| 定義 | 正 | 写し |
| --- | --- | --- |
| **最大1周・最後の1回・10回目の例外** | [CLAUDE.md:573-635](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L573-L635) | [CLAUDE.md:412-418](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L412-L418)、[.claude/rules/design-review.md:160-165](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L160-L165)、[.claude/skills/pr-review-and-merge/SKILL.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md) |
| **3・6・9回目で6段、10回で止まる** | [CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) | [CLAUDE.md:315](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L315)、[CLAUDE.md:507](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L507)、[.claude/rules/design-review.md:155-192](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L155-L192)、[.claude/rules/design-review.md:209-213](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L209-L213)、[.claude/skills/worker-briefing/SKILL.md:117-118](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L117-L118)、[.claude/skills/pr-review-and-merge/SKILL.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md)、[.claude/skills/pr-review-and-merge/SKILL.md:185-188](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L185-L188) |
| **3回で収まらないときの考え方** | [CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) | [.claude/rules/design-review.md:206-218](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L206-L218)、[.claude/skills/worker-briefing/SKILL.md:105-116](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L105-L116) |
| **前の周の対応表を渡す** | [.claude/rules/design-review.md:143-149](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L143-L149) | [.claude/skills/worker-briefing/SKILL.md:349](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L349) |
| **数える条件（先頭の目印と投稿者）** | 3箇所の実装（**2026-09-21 以降は2箇所**） | [CLAUDE.md:433-436](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L433-L436)、[.claude/rules/design-review.md:33-36](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L33-L36)、[CONTRIBUTING.md:145-150](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CONTRIBUTING.md#L145-L150)、[docs/releasing.md:233-237](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/releasing.md#L233-L237) |

**「写さない」と書いた直後に写している箇所**（検索パターン `ここには写さない|写すと食い違う|この規則へ写さない` と `要点だけ`、対象 CLAUDE.md と .claude/、コミット `df36f9d7`）。

| 写さない宣言 | 直後の写し |
| --- | --- |
| [.claude/rules/design-review.md:157-158](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L157-L158) | [.claude/rules/design-review.md:160-165](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L160-L165)（6行） |
| [.claude/rules/design-review.md:176-178](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L176-L178) | [.claude/rules/design-review.md:180-182](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L180-L182)（3行） |
| [.claude/rules/design-review.md:209](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L209) | [.claude/rules/design-review.md:211-213](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L211-L213)（3行） |
| [.claude/skills/pr-review-and-merge/SKILL.md:170-171](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L170-L171) | [.claude/skills/pr-review-and-merge/SKILL.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md)（8行） |

**書式の崩れの複製。**`git grep -n -F '****そのとき'` で、規則側に3件、同じ文面で出る。

```
.claude/rules/design-review.md:165:****そのとき、Medium と Low は直さない。**そのまま follow-up の issue へ切り出す。**
.claude/skills/pr-review-and-merge/SKILL.md:184:****そのとき、Medium と Low は直さない。**そのまま follow-up の issue へ切り出す。**
CLAUDE.md:613:****そのとき、Medium と Low は直さない。**そのまま follow-up の issue へ切り出す。**回さずに終えるので、
```

（ほかに docs/plans/continuo_design.md:12841 が当たるが、別の表の行で、この文面ではない。）

---

## 5. 食い違い

**言いたいこと。**いちばん重いのは、3・6・9回目のあとの進み先がスキルと CLAUDE.md で割れていることと、実装レビューの道具が2通りあることである。

| 何が食い違うか | 片方の原文 | もう片方の原文 |
| --- | --- | --- |
| **3・6・9回目のあと、次に何をするか**（読み方が2通りに割れる） | [.claude/skills/pr-review-and-merge/SKILL.md:185-186](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L185-L186)「3・6・9回目で収まらないときは、CLAUDE.md の「回数を数える」にある6段を終えてから段5へ進む。」（このスキルの段5は検査の回し直し、段6はマージ） | [CLAUDE.md:649](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L649)「実装し直してから次のレビューを回す。」と [CLAUDE.md:412](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L412)「収まるまで 2〜4 を繰り返す。」。**スキルを字面どおりに読むと、収まっていないまま検査とマージの段へ進む** |
| **「段5」が2つある** | [CLAUDE.md:418-419](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L418-L419)「段5 へ進まずに止まる」「5. `gh pr ready` で draft を外す」 | [.claude/skills/pr-review-and-merge/SKILL.md:190](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L190)「段5. 検査を回し直す」 |
| **存在しない見出しを指している** | [.claude/skills/pr-review-and-merge/SKILL.md:188](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L188)「「絶対条件：連続10回で止まる」」 | 実在する見出しは [CLAUDE.md:663](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L663)「絶対条件：3回ごとに issue と実装を突き合わせ直す。連続10回で完全に止まる」。`git grep -F '絶対条件：連続10回で止まる'` は SKILL.md:190 の1件だけ |
| **同じ段の名前が2つ** | [CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md)「3回で通らなかったとき」 | [.claude/rules/design-review.md:206](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L206)「3回で収まらなかったとき」 |
| **要点は1行まで、と写しの長さ** | [CLAUDE.md:571](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L571)「要点を1行で添えるのはよい」 | 4章の写しは3〜8行 |
| **実装レビューの道具が2通り** | [CLAUDE.md:396](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L396)「必ず `/code-review` でレビューする。」と [.claude/rules/design-review.md:15](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L15) の段7 | [.claude/rules/design-review.md:115-118](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L115-L118)「実装レビューでは `maimuzo-from-ecc:architect` を使わない。…Bash を持つエージェント（`general-purpose` など）を立てること。」。**どちらを毎周使うかを決めた箇所は無い**（[.claude/skills/pr-review-and-merge/SKILL.md:119-120](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L119-L120) は両方の場合を書き分けるだけ） |
| **前の周の対応表を渡す、と渡せない** | [.claude/rules/design-review.md:143-144](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L143-L144)「渡さないと同じものが必ずまた挙がり、周だけが増える。」 | [.claude/rules/design-review.md:146](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L146)「`/code-review` では渡せない。」。**必須の道具のほうで、規則自身が挙げた「周だけが増える」条件が毎周成り立つ** |
| **1回で全部挙げさせる、と渡せない** | [.claude/skills/worker-briefing/SKILL.md:260](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L260)「まったく同じ結果になるくらい徹底的に洗い出すこと。」 | [.claude/skills/pr-review-and-merge/SKILL.md:102-103](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L102-L103)「2-6（1回で全部挙げる）と 2-7（合理的根拠を書く）を、レビュワーへ直接は渡せない。」 |
| **範囲外で直さない（対象が違うので矛盾ではなく差）** | [internal/prompt/builtin.md:232](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L232) の見本「直さない \| この issue の範囲外」（Low） | [CLAUDE.md:545-546](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L545-L546)「設計に触るなら follow-up の issue へ切り出す。」。**利用者向けには、follow-up を切り出す指示が無い** |

---

## 6. 規則の総量

**言いたいこと。**実装レビューの1周で、直す側が従う開発者向けの規則は、下の範囲だけで883行・約60KBある。
**何行なら1回で読み切れるかの線は、この調査では決めていない。**依頼に沿って、目安として置く。

| 範囲 | 行数 | バイト数 |
| --- | --- | --- |
| [CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) | 376 | 28,322 |
| [.claude/rules/design-review.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md) 全体 | 248 | 14,561 |
| [.claude/skills/worker-briefing/SKILL.md:105-121](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L105-L121) と [.claude/skills/worker-briefing/SKILL.md:183-298](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L183-L298) | 17 + 116 = 133 | 10,910 |
| [.claude/skills/pr-review-and-merge/SKILL.md:93-188](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L93-L188) | 98 | 6,450 |
| [.claude/rules/reporting.md:564-591](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/reporting.md#L564-L591) | 28 | 測っていない |
| **計** | **883** | **60,243**（reporting.md を除く） |

測ったコマンドは `sed -n '<開始>,<終了>p' <ファイル> | wc -l` と `| wc -c`、全体は `wc -l` / `wc -c`。

- **レビューを頼まれた worker は、worker-briefing/SKILL.md を全部（490行）読む前提である**（[.claude/skills/worker-briefing/SKILL.md:26](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L26) が「書いてあることを全部守れ」と書かせる）
- **利用者向け**は [internal/prompt/builtin.md:104-257](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L104-L257)（154行。3-2 の全体）、[internal/prompt/builtin.md:334-425](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L334-L425)（90行。3-6 の全体）、[internal/prompt/builtin.md:726-968](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L726-L968)（240行。5-6）、[internal/prompt/builtin.md:970-1067](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L970-L1067)（98行。5-7）、[internal/prompt/builtin.md:519-524](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/internal/prompt/builtin.md#L519-L524)（6行）。**この5つの行数だけは、2026-09-22 に数え直した値である**（`df36f9d7` では 3-2 が 104-194 の91行だった）
- **PR #267（人間が pane で直接続けるあいだ continuo が手を出さない Status を足す）がマージされると増える行**（`gh pr diff 267` の hunk の見出しから）: design-review.md が +53（`@@ -80,6 +80,59 @@`）、worker-briefing/SKILL.md が +3・+83・+1（`@@ -195,8 +195,11 @@` / `@@ -253,6 +256,89 @@` / `@@ -345,6 +431,7 @@`）、builtin.md が +14・+11、CLAUDE.md が +1

---

## 7. 開いている PR が、レビューループの定義をどう変えようとしているか

**言いたいこと。**収束の定義（Critical と High が0件、最大1周、3・6・9、10回）を変える PR は無い。
**PR #267 は「前の周の否定を知らないまま直す / 判定する」経路を塞ぐ段を、書く側とレビュワーの両方に足す。PR #272 は人間の方向調整の原文を CLAUDE.md から外す。**

拾い方: `gh pr list --state open --limit 100 --json number,title,headRefName,isDraft,files` で、CLAUDE.md・.claude/・review-gate.yml・check-release-ready.sh・docs/releasing.md・builtin.md を触る PR を出した。6本だった。

| PR と対になる issue | 変えようとしていること | レビューループへの効き | 状態（`gh pr view --json mergeable,mergeStateStatus`） |
| --- | --- | --- | --- |
| **PR #256（同じ issue のレビューと修正を同時に走らせない規則を足す（要約の1行も読み違えを塞ぐ形へ））**。対になる issue は無い（`closingIssuesReferences` が空） | parallel-work.md に「同じ issue の中では、レビューと、それに対する作業を同時に走らせない」絶対条件と「1つの issue からは1つだけ」を足し、「よくある間違い」を「複数の issue の修正を1つずつ順番にやるな」に絞る。CLAUDE.md の要約2行も直す | 収束の定義は変えない。**レビュー中に同じ issue を直すと、レビュワーが見ている物が書き換わる経路を塞ぐ** | draft、MERGEABLE/CLEAN |
| **PR #272（設計と spec と rules から経緯を外し、報告の形を話題ごとの節に変える（文書とテストだけ））**。対になる issue は無い | CLAUDE.md のレビュー節から人間の指摘の原文6つ（2026-09-05 が1、09-04 が2、09-03 が2、09-02 が1）を消し、中立の文へ置き換える。design-review.md の 09-04 の原文と実測の日付、parallel-work.md と hook の docstring の実例、plan-file.md の「8周のレビューのうち3周が、同じ原因である。」も消す。reporting.md の返答の形を7段に変え、判断票の目印を先頭に置く旨を足す | **収束の定義・回数・止まる条件の文は変えない**（差分は削除と言い換え）。**CLAUDE.md と design-review.md から方向調整の原文が消える。**worker-briefing/SKILL.md は触らないので、2-1・2-5・2-7 の原文は残る | draft、MERGEABLE/BLOCKED |
| **PR #267（人間が pane で直接続けるあいだ continuo が手を出さない Status を足す）** → **issue #263（continuoを使って始めたタスクでも途中で人間がチャットで進めて切りが良くなったらcontinuoに制御を戻したい）** | design-review.md に「過去のコメントを全部、省略無しで読む」絶対条件。worker-briefing 2-5 に「出た行を1行も切らずに読む」段と、3つの小節（数えた出力を `cut` / `head` / `tail` で切らない・範囲を絞らない・既存の仕組みを自分で開く）と「機械で止める」の提案表。4章の表に1行。CLAUDE.md の設計レビューの要約に5点目。builtin.md の 3-1 と、3-2 のレビュー依頼文に「全部読む」と観点「過去の周で人間が否定した方向へ、また進んでいる記述」「切って読まない」 | **書く側とレビュワーの両方に、前の周の否定を知らないまま直す / 判定する経路を塞ぐ段を足す。**根拠は差分の本文にある issue #263 の10周の実測。**機能の PR に規則の変更が同梱されている** | draft、MERGEABLE/CLEAN |
| **PR #254（issue のコメントを GitHub App の attribution で見分けられるようにする（設計のみ））** → **issue #245（issue のコメントを人間が書いたのか AI が書いたのか、あとから見分けられない）** | builtin.md の判断票を「ファイルへ書いてから渡す」形にする（差分の変更行「判断票も、ファイルへ書いてから渡してください」） | 判断票の投稿のしかただけ。**収束の定義は触らない** | draft、MERGEABLE/CLEAN |
| **PR #255（担当している issue を判定役へ渡し、起票が一律で断られるのを直す）** → **issue #246（人間が「issue を作ってよい」と許しても、continuo が起動したエージェントは起票できない）** | CLAUDE.md は issue の節のリンクの行番号1か所だけ。rules/issue.md の変更行にレビューの語は0件 | 変えない | draft |
| **PR #230（枠が残り少ないときに止まる理由を出し、1週間の枠を待つ上限を足す（#173 / #197 / #199））** → **issue #173（枠が残り少ないときに issue が1件も着手されないのに、その理由がログに出ない）**、**issue #197（1週間のレートリミットを待つ上限を、WORKFLOW.md から分で指定できない）**、**issue #199（モデル別の週次レートリミットについて、設計文書が事実と逆の説明を4行持っている）** | CLAUDE.md は同じリンクの行番号1か所だけ。rules/issue.md と check-reply-clarity.py の変更行にレビューの語は0件 | 変えない | draft |

**PR 同士の重なり。**

- PR #256 と PR #272 は、parallel-work.md の同じ「よくある間違い」の節（origin/main の30-37行）を別々に書き換える（PR #256 は `@@ -8,43 +8,87 @@`、PR #272 は `@@ -30,11 +30,8 @@`）
- PR #256 と PR #267 は、CLAUDE.md の「作業の進め方」の隣り合う節を触る（PR #267 は `@@ -309,12 +309,13 @@`、PR #256 は `@@ -321,8 +321,9 @@`）
- **実際に衝突するかは、merge を試していないので測っていない**

---

## 8. 測っていないこと

| 何を | なぜ |
| --- | --- |
| `/code-review` の出力の形（レベル名・件数・拾う範囲） | 実行していない。3-3 は skill 一覧の説明文を読んだだけ |
| PR 同士の衝突 | merge を試していない（git の書き込みを禁じられている） |
| 人間の観測の「20回ほど」 | 別の worker の担当 |
| メモリ・プラグイン・個人設定の側の定義 | 別の worker の担当 |
| PR #255・#230・#254 の規則ファイル以外の差分 | レビューループの定義に当たらないので、変更行をレビューの語で grep しただけ |
