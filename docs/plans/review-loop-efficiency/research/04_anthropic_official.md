# Anthropic 公式が紹介している、レビューを少ない周回で収める方法（調査）

**調べた日。**2026-09-15。
**書いた者。**レビューループの効率化の材料集めのために立てられた調査 worker（担当は Anthropic 公式の情報）。
**位置づけ。**オーケストレーターがプランファイルを書くための材料である。**各節の「当てはめうる場所」は、すべて案であって決定ではない。**

## 引用の取り方（読む前に知っておくこと）

| 取り方 | どの出典 | 原文と一致するか |
| --- | --- | --- |
| GitHub の定義ファイルを `gh api` で commit を固定して取得 | anthropics/claude-code（`f96c3b49c4c8721685206aaab23609b2d399df4e`、2026-09-15）、anthropics/claude-code-security-review（`0c6a49f1fa56a1d472575da86a94dbc1edb78eda`、2026-02-11）、anthropics/skills（`34040c9c568585f6929bedeaad110ad08f079624`、2026-09-10）、anthropics/claude-code-action（`bf38e86e`。先頭8文字だけ控えた、2026-09-15） | **原文そのもの** |
| WebFetch がページ全体を Markdown で返したもの | code.claude.com と platform.claude.com の文書 | **原文そのもの**（ツールの変換だけ受けている） |
| WebFetch の要約モデルが、こちらの指示で抜き出した引用 | anthropic.com の記事、claude.com/blog、academy.claude.com | **1文字ずつは照合していない。**言い換えが混ざりうる。出典の表に「要約モデル経由」と書いた |

**文書のページ（code.claude.com / platform.claude.com）には日付の表示が無い。**「2026-09-15 に取得」とだけ書く。

---

## 0. 前置きの問いへの答え

### 0-1. この作業は、どの規則に当てはまるか

| 規則 | どう当てはまるか |
| --- | --- |
| [.claude/skills/worker-briefing/SKILL.md:161](../../../../.claude/skills/worker-briefing/SKILL.md#L161) の 2-4（指示に書かれたことだけで判断しない） | 当てはめ先を書くために、レビューループを定義している規則を自分で開いた（0-4） |
| 同じファイルの [:183](../../../../.claude/skills/worker-briefing/SKILL.md#L183) 2-5（同じものが他に無いかを数える） | 「見つからなかった」と書くものに、検索語・範囲・commit を添えた（5章） |
| 同じファイルの「3. 言葉づかいと制約」と [.claude/rules/reporting.md](../../../../.claude/rules/reporting.md) の根拠の付け方 | 英文の引用に訳を併記した。worktree / hook / subagent などは訳していない |
| [CLAUDE.md](../../../../CLAUDE.md) の「公開してよい情報かを常に判断する」 | 0-3 |
| [CLAUDE.md](../../../../CLAUDE.md) の「`claude -p` は使用禁止」 | 公式文書が `claude -p` の例を載せていても、試していない。当てはめ案にも入れていない |

### 0-2. 飛ばしてよい段はあるか

| 飛ばした段 | どの記述が飛ばしてよいと言っているか |
| --- | --- |
| worker-briefing 2-6（1回で全部挙げる）と 2-7（指摘ごとの合理的根拠） | [SKILL.md:258](../../../../.claude/skills/worker-briefing/SKILL.md#L258) が「この節は、コードレビューや設計レビューを頼まれた worker に効く」と書いている。これは調査であってレビューではない |
| worker-briefing 2-3（下の worker へ方向調整を渡す） | 依頼の「禁止」が「さらに worker（subagent）を立てない」と決めている。渡す相手がいない |
| [.claude/rules/design-review.md:3](../../../../.claude/rules/design-review.md#L3) の9段 | 実装も PR も作らない。成果物は調査結果の1ファイルだけ |

### 0-3. 公開してよくない情報を書きうる場面

- **ツールが結果を保存した場所の絶対パス**（スクラッチパッドと `~/.claude/projects/` の下）。利用者名を含むので書いていない。
- **anthropics/* のリポジトリ名は書いた。**公開の一次情報の出典だからである。[CLAUDE.md](../../../../CLAUDE.md) が禁じているのは「自分の実在のリポジトリ名」である。
- このリポジトリの PR 番号や過去の事件の中身は書いていない。規則の行へのリンクだけにした。

### 0-4. どう探し、何を読んだか

**公式の側。**

- WebSearch を6回叩いた。Building effective agents、Claude Code の best practices、harness design、Code Review の偽陽性、sycophancy、同種箇所の修正。ドメインは anthropic.com / claude.com / code.claude.com / platform.claude.com / docs.claude.com / arxiv.org に絞った。
- anthropic.com/engineering の一覧（25本）の題名と日付を取った。
- WebFetch で開いたページは31（転送だけが返った2回と、同じページを2回開いた分を除く）。一覧は各節の出典の表と6章にある。
- `gh api` で取った定義ファイルは次のとおり。
  - code-review プラグインのコマンドと README
  - pr-review-toolkit のコマンド・README と、agent 5本
  - feature-dev のコマンドと code-reviewer
  - ralph-wiggum の README とコマンド
  - security-review のコマンド・`findings_filter.py`・`prompts.py`・フィルタの文書・README
  - skill-creator の SKILL.md・grader・comparator・analyzer
  - claude-code-action の review-pr・code-quality-reviewer・例1本
- 各ファイルの変更履歴も取った。

**このリポジトリの側（当てはめ先を書くため）。**

- `git grep -n -l -E 'code-review|収まっている|レビューを頼まれたら|同じものが他に無いか' -- .claude/ CLAUDE.md docs/` を叩いた（HEAD `df36f9d7`）。16ファイルが返った。
- そのうち開いたのは次の5本である。
  - [CLAUDE.md](../../../../CLAUDE.md) の486行目以降
  - [.claude/rules/design-review.md](../../../../.claude/rules/design-review.md) の全体
  - [.claude/skills/worker-briefing/SKILL.md](../../../../.claude/skills/worker-briefing/SKILL.md) の全体
  - [.claude/skills/pr-review-and-merge/SKILL.md](../../../../.claude/skills/pr-review-and-merge/SKILL.md) の段2と、見出しの一覧
  - [.claude/rules/reporting.md](../../../../.claude/rules/reporting.md)
- **開いていないもの。**`.claude/hooks/` の3本、`docs/` の8本。同じディレクトリの `03_work/` は別の worker の作業場所なので読んでいない。

---

## 1. この調査が答える問い

| 何を | 中身 |
| --- | --- |
| **何が起きているか** | `/code-review` と設計レビューで Critical と High が収まらない。人間の観測では20周ほど回る。直すときに同じ種類の箇所を確かめないので、次の周で修正漏れとして挙がる |
| **なぜ困るか** | 1周ごとにレートリミットと時間を使う。直しが新しい欠陥を持ち込む機会も増える（[CLAUDE.md:611](../../../../CLAUDE.md#L611) の実例: 8周目の Critical が7周目の直しから生まれた） |
| **この文書で何を決めるか** | 決めない。公式が紹介している方法を、4つの観点（レビュワー側・書く側・ループの制御・修正漏れ）に分けて並べる |

**読むときの軸。**これまでの規則は「徹底させる」「数える」「上限を置く」という同じ向きの手を積み増してきた。
**公式の材料には、逆向きの手（見つけたあと検証で捨てる・範囲を絞る・規則を削る）が多い。**各節に「いまの規則と同じ向きか」を1行書いた。

---

## 2. 一覧

**並べ順の根拠。**(1) 人間が挙げた3つの症状（周が長い・周ごとに新しい指摘が出る・修正漏れ）に直接当たるか。(2) 公式に効果の実測があるか。(3) 制約に当たらずに再現できるか。
**このリポジトリで効果を測ってはいない。**

| 手法 | 効く観点 | 制約 | 公式の実測 |
| --- | --- | --- | --- |
| 3-1. 見つける役と確かめる役を分け、確かめを通らなかった指摘を捨てる | レビュワー / ループ | 管理サービスと ultra は採れない。考え方は Agent ツールで再現できる | あり（誤りと印を付けられた指摘は1%未満、ほか） |
| 3-8. 指摘を全部追いかけない | ループ / レビュワー | 無い | 無い |
| 3-3. `/code-review` の effort level を毎回明示して揃える | レビュワー / ループ | 無い | 無い |
| 3-2. 見つける段では絞らず、絞り込みは別の段で行う | レビュワー | `/code-review` には指示を足せない | 無い |
| 3-6. 2周目からは軽微な指摘を出させない | ループ | REVIEW.md は `/code-review` に届かない | 無い（「7周目」は例示） |
| 3-7. 範囲を「この変更が持ち込んだもの」に絞る | ループ | 無い | 無い |
| 3-9. 書く前に「完了の条件」と「範囲外」を合意する | 書く側 / ループ | 無い | 費用と時間の数値だけ |
| 3-5. 重大度の意味と証拠の水準を決める | レビュワー / ループ | REVIEW.md は `/code-review` に届かない | 無い |
| 3-4. 偽陽性の型を一覧にしてレビュワーへ渡す | レビュワー / ループ | 同上 | 無い |
| 3-14. 2周続けて進まなければ止め、新しい文脈で書き直す | ループ | 無い | 無い |
| 3-10. 書く側に機械で合否が出る検査を持たせる | 書く側 / 修正漏れ | 無い（agent 型 hook は実験的） | 無い |
| 3-11. 同じ誤りが他に残っていないかを確かめる | 修正漏れ | workflows はトークンを使う | 無い（公式の記述が薄い） |
| 3-13. 同じ観点を独立に複数見させる | レビュワー | トークンが増える | 調査タスクの数値だけ |
| 3-12. 評価役を、人間の判断と食い違った事例で較正する | レビュワー | 無い | 無い |
| 3-15. 規則を削り、機械で守らせるものは hook へ移す | 書く側 / レビュワー | 無い | 無い |

---

## 3. 手法ごとの詳細

### 3-1. 見つける役と、見つけた指摘を確かめる役を分け、確かめを通らなかった指摘を捨てる

**何をするか。**レビュワーが挙げた候補を、別の agent が1件ずつ「本当に起きるか」確かめる。確かめられなかったものは報告から外す。
**いまの規則と同じ向きか。**逆向き。いまは受け取った指摘を全部対応表に載せ、受け取る側が根拠を否定できるかを考える（[.claude/rules/design-review.md:124](../../../../.claude/rules/design-review.md#L124)）。公式は、否定の作業を別の agent に先にやらせる。

| 出典 | 発行元・日付 | 取り方 | 一次 / 二次 |
| --- | --- | --- | --- |
| [plugins/code-review/commands/code-review.md](https://github.com/anthropics/claude-code/blob/f96c3b49c4c8721685206aaab23609b2d399df4e/plugins/code-review/commands/code-review.md) | anthropics/claude-code。ファイルの最終変更は 2026-03-12 | 原文 | 一次 |
| [Code Review](https://code.claude.com/docs/en/code-review) | Claude Code 文書。2026-09-15 取得 | 全文 | 一次 |
| [Code Review for Claude Code](https://claude.com/blog/code-review) | claude.com/blog、2026-03-09 | 要約モデル経由 | 一次（製品の発表） |
| [.claude/commands/security-review.md](https://github.com/anthropics/claude-code-security-review/blob/0c6a49f1fa56a1d472575da86a94dbc1edb78eda/.claude/commands/security-review.md) | anthropics/claude-code-security-review | 原文 | 一次 |
| [Find bugs with ultrareview](https://code.claude.com/docs/en/ultrareview) | Claude Code 文書 | 全文 | 一次 |
| [Best practices for Claude Code](https://code.claude.com/docs/en/best-practices) | Claude Code 文書 | 全文 | 一次 |
| [Making frontier cybersecurity capabilities available to defenders](https://www.anthropic.com/news/claude-code-security) | anthropic.com/news、2026-02-20 | 要約モデル経由 | 一次 |
| [Finding bugs with Claude and property-based testing](https://www.anthropic.com/research/property-based-testing) | anthropic.com/research、2026-01-14、Muhammad Maaz ほか | 要約モデル経由 | 一次（研究記事） |

> 5. For each issue found in the previous step by agents 3 and 4, launch parallel subagents to validate the issue. … The agent's job is to review the issue to validate that the stated issue is truly an issue with high confidence.
> 6. Filter out any issues that were not validated in step 5.

（訳: 前の段で agent 3 と 4 が見つけた指摘ごとに、並列に subagent を立てて検証させよ。その agent の仕事は、書かれた指摘が本当に問題であることを高い確信で確かめることである。／段5で検証されなかった指摘は捨てよ。code-review.md:55-57）

> When a review runs, multiple agents analyze the diff and surrounding code in parallel on Anthropic infrastructure. Each agent looks for a different class of issue, then a verification step checks candidates against actual code behavior to filter out false positives.

（訳: 複数の agent が差分と周辺のコードを並列に解析する。agent ごとに別の種類の問題を探し、そのあと検証の段が候補を実際のコードの挙動と突き合わせて偽陽性を除く。Code Review 文書）

> 2. Then for each vulnerability identified by the above sub-task, create a new sub-task to filter out false-positives. Launch these sub-tasks as parallel sub-tasks. … 3. Filter out any vulnerabilities where the sub-task reported a confidence less than 8.

（訳: 見つけた脆弱性ごとに、偽陽性を除く sub-task を新しく並列に立てよ。sub-task が確信度8未満と報告したものは捨てよ。security-review.md:188-189）

> By a second opinion: a verification subagent or a dynamic workflow that checks its own findings has a fresh model try to refute the result, so the agent doing the work isn't the one grading it.

（訳: 検証用の subagent や、自分の指摘を確かめる dynamic workflow は、新しいモデルに結果の反証を試みさせる。作業した agent が自分を採点しないで済む。Best practices）

ultrareview 文書は "every reported finding is independently reproduced and verified"（報告される指摘はすべて独立に再現・検証される）と書く。
Claude Code Security の記事は "Claude re-examines each result, attempting to prove or disprove its own findings and filter out false positives."（Claude が各結果を見直し、自分の指摘の証明か反証を試みて偽陽性を除く。要約モデル経由）と書く。

**効果の実測。**

| 出典 | 数値 | 条件 |
| --- | --- | --- |
| Code Review の blog（要約モデル経由） | "less than 1% of findings are marked incorrect"（誤りと印を付けられた指摘は1%未満）。実質的なレビューコメントが付く PR が "16%" から "54%" へ。1,000行以上の PR の "84%" に指摘、平均 "7.5" 件。50行未満は "31%"、平均 "0.5" 件。1回 "around 20 minutes"、"$15–25" | Anthropic 社内。何周目かの区別は書かれていない |
| property-based testing（要約モデル経由） | 984件の報告から選んだ50件のうち有効 "56%"、報告に値する "32%"。採点基準（15点満点）で上位のものは有効 "86%"、有効かつ報告に値する "81%" | 検証の段ではなく、採点基準での順位付けの効果。レビューの周回ではない |

**効く観点。**レビュワー側（ノイズを減らす）。ループの制御にも効くと考えるが、これは推測である（偽陽性を直す手間と、その直しが持ち込む欠陥が減る、という筋書き）。

**制約。**

- **Claude Code Review（管理サービス）は採れない。**GitHub App で動き、usage credits で課金される。Team と Enterprise だけで使える。
- **ultrareview（`/code-review ultra`）は従量課金に当たる。**Pro と Max は1回きりの無料3回、そのあと1回 "$5 to $25" の usage credits。[.claude/skills/pr-review-and-merge/SKILL.md:130](../../../../.claude/skills/pr-review-and-merge/SKILL.md#L130) が「人間が明示的に指示したときだけ」と決めている。
- **code-review プラグインは導入しない。**対話の slash command で、`claude -p` も API も使わない。ただし入れることは OSS の導入に当たるので、定義の考え方だけを使う。
- **security-review の GitHub Action は採れない。**`claudecode/claude_api_client.py` で API を叩く。同じリポジトリの `.claude/commands/security-review.md` は対話セッションで動く。
- **`/code-review` にプロンプトを足せない制約には当たらない。**検証の段は `/code-review` の外に足せる。結果を受け取ったあと、オーケストレーターが Agent ツールで指摘1件ごとに検証役を立てればよい。
- **注意。**[Prompting Claude Opus 5](https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/prompting-claude-opus-5) は "do not use subagents to verify or double-check your own work"（自分の作業の検証や再確認に subagent を使うな）と書く。これは作業した本人の成果物の話で、他の agent が挙げた指摘の検証とは別物だと読める。**ただしそう読んでよいかは文書に書かれておらず、確かめていない。**

**当てはめうる場所（案であって決定ではない）。**

- [CLAUDE.md:520](../../../../CLAUDE.md#L520) の「手順」の段1（対応表を書く前）に段を足す。Critical と High の指摘1件ごとに検証役を立て、指摘の文と PR の意図だけを渡す。返させるのは「再現できる file:line と、壊れる筋書き」である。返せなかった指摘は「直さない」とし、理由欄に「検証役が再現できなかった」と書く。
- [.claude/rules/design-review.md:124](../../../../.claude/rules/design-review.md#L124) の「否定できるなら直さない」を、受け取る側の頭の中ではなく、この検証役にやらせる。

### 3-2. 見つける段では重大度で絞らず全部挙げさせ、絞り込みは別の段で行う

**何をするか。**見つける段の指示に「重大なものだけ」「控えめに」と書かない。全部挙げさせ、絞るのは後段（3-1 の検証か、受け取る側）に任せる。
**いまの規則と同じ向きか。**見つける段は [worker-briefing 2-6](../../../../.claude/skills/worker-briefing/SKILL.md#L256) と同じ向き。ただしいまの規則には絞り込みの段が無い。全部を対応表に載せて1件ずつ判断している。

| 出典 | 発行元・日付 | 取り方 | 一次 / 二次 |
| --- | --- | --- | --- |
| [Prompting Claude Opus 5](https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/prompting-claude-opus-5) | Claude Platform 文書 | 全文 | 一次 |
| [plugins/pr-review-toolkit/agents/silent-failure-hunter.md](https://github.com/anthropics/claude-code/blob/f96c3b49c4c8721685206aaab23609b2d399df4e/plugins/pr-review-toolkit/agents/silent-failure-hunter.md) | anthropics/claude-code | 原文 | 一次 |

> Code review and bug-finding: Claude Opus 5 reviews code with high precision and recall: it finds real bugs at a high rate per pass, and its additional findings are mostly real issues rather than false positives. Accuracy holds at lower effort settings, which supports a fast pass at review time and a more thorough pass later. If your review prompt says "only report high-severity issues" or "be conservative," the model may follow that instruction literally and report less; ask it to report everything and filter in a separate pass instead.

（訳: Claude Opus 5 は高い適合率と再現率でコードをレビューする。1回の走査で本物のバグを高い割合で見つけ、増えた指摘もほとんどが偽陽性ではなく本物の問題である。低い effort でも精度が保たれるので、レビュー時に速く走査し、あとでより徹底して走査する形が取れる。レビューの指示に「重大度の高い問題だけ報告せよ」「控えめに」とあると、モデルはそれを字義どおりに受け取り、報告を減らすことがある。代わりに全部を報告させ、別の段で絞り込むこと。）

silent-failure-hunter.md:114 は "Call out every instance of inadequate error handling, no matter how minor"（どれほど小さくても、不十分なエラー処理はすべて指摘せよ）と書く。

**逆向きの定義も公式にある。**code-review.md:36 は "Flag only significant bugs; ignore nitpicks and likely false positives."（重大なバグだけを挙げ、細かい指摘と偽陽性らしいものは無視せよ）。:51 は "If you are not certain an issue is real, do not flag it."（本物だと確信できないなら挙げるな）。
見つける段にも絞り込みをさせる書き方である。ただし同じ定義は段5で別の検証も置いている。

**効果の実測。**示されていない。
**効く観点。**レビュワー側（再現率）。
**制約。**`/code-review` には指示を足せない。見つける範囲は effort level でしか変えられない（3-3）。Agent で立てるレビュワー（設計レビューの architect など）には書ける。

**当てはめうる場所（案）。**

- [worker-briefing 2-6](../../../../.claude/skills/worker-briefing/SKILL.md#L256) に一文を足す。「見つける段では重大度で絞らない。重大度を付けて全部返し、捨てるのは受け取る側か 3-1 の検証役」。
- [.claude/rules/design-review.md:101](../../../../.claude/rules/design-review.md#L101) の「渡す4つの観点」も同じ書き方にそろえる。

### 3-3. `/code-review` の effort level を毎回明示し、周ごとに揃える

**何をするか。**`/code-review` を叩くたびに effort level を打つ。打たないと前回打った level が使われるので、周ごとに見つける範囲が変わりうる。
**いまの規則と同じ向きか。**いまの規則に無い観点。[.claude/skills/pr-review-and-merge/SKILL.md:93](../../../../.claude/skills/pr-review-and-merge/SKILL.md#L93) の段2は `/code-review <PR 番号>` とだけ書く。
`git grep -n -i 'effort' -- CLAUDE.md .claude/rules .claude/skills`（HEAD `df36f9d7`）が返したのは同じファイルの :99 と :130 の2行だけで、level を決めた行は無い。

| 出典 | 発行元・日付 | 取り方 | 一次 / 二次 |
| --- | --- | --- | --- |
| [Code Review](https://code.claude.com/docs/en/code-review) の「Tune effort and arguments」 | Claude Code 文書 | 全文 | 一次 |
| [Claude Code 101: Code review](https://academy.claude.com/courses/claude-code-101/code-review) | Claude Academy。日付の表示なし | 要約モデル経由 | 一次（公式の講座） |
| [Find bugs with ultrareview](https://code.claude.com/docs/en/ultrareview) の比較表 | Claude Code 文書 | 全文 | 一次 |

> At `low` and `medium`, the review reports only the findings it's most confident in, so you see fewer false positives; `high` through `max` broaden coverage and may include findings the review is less sure about.

（訳: low と medium では最も確信のある指摘だけを報告するので、偽陽性は減る。high から max は範囲を広げ、確信の低い指摘も含めうる。）

> When you don't type a level, the review reuses the last level from `low` through `max` you typed, even in an earlier session, and Claude Code shows a notice such as `Reusing high effort, the level you typed last time`. … If you've never typed a level, the review uses the session's current effort.

（訳: level を打たないと、以前のセッションも含め、最後に打った low〜max の level を使う。そのとき「Reusing high effort, …」のような通知を出す。一度も打っていなければ、セッションの現在の effort を使う。）

ultrareview 文書の比較表は "Use `/code-review` for fast feedback as you work … Use `/code-review ultra` before merging a substantial change"（作業中の速いフィードバックには /code-review、大きな変更をマージする前には ultra）と書く。

**効果の実測。**示されていない。**このリポジトリで過去の周がどの level で走ったかも測っていない。**
**効く観点。**レビュワー側（周ごとの一貫性）、ループの制御。

**推測（確かめていない）。**1周目が low か medium で走り、あとの周が high で走ると、1周目に出なかった指摘が後の周で「新しく」出る。
[worker-briefing 2-6](../../../../.claude/skills/worker-briefing/SKILL.md#L256) が「前の周のレビューが足りなかった」と扱っている現象の一部を、これが説明しうる。
PR コメントに level の通知が残っていれば、別の worker が集めている周ごとのデータで確かめられる。

**制約。**ultra は従量課金（3-1）。low〜max は通常の使用量に数える（比較表 "counts toward normal usage"）。

**当てはめうる場所（案）。**

- [pr-review-and-merge 段2](../../../../.claude/skills/pr-review-and-merge/SKILL.md#L93) を `/code-review <level> <PR 番号>` にし、level を固定する（文書は level を先、対象をそのあとに読むと書いている）。
- 段3の結果コメントに、使った level を書く。

### 3-4. 偽陽性になりやすい型を「報告しないもの」と「先例」として書き出し、レビュワーへ渡す

**何をするか。**過去に誤りだった指摘の型を一覧にし、見つける段と検証の段の両方へ渡す。
**いまの規則と同じ向きか。**近い規則がある。[.claude/rules/design-review.md:124](../../../../.claude/rules/design-review.md#L124) の節は「次の周のレビュワーには、前の周の対応表を渡す」と決めている。
**ただしそれは1つの PR の中で周をまたぐだけで、PR をまたいで貯める一覧は無い。**

| 出典 | 発行元・日付 | 取り方 | 一次 / 二次 |
| --- | --- | --- | --- |
| code-review.md:79-86 | anthropics/claude-code | 原文 | 一次 |
| security-review.md:134-181 と [docs/custom-filtering-instructions.md](https://github.com/anthropics/claude-code-security-review/blob/0c6a49f1fa56a1d472575da86a94dbc1edb78eda/docs/custom-filtering-instructions.md) | anthropics/claude-code-security-review | 原文 | 一次 |
| [Code Review](https://code.claude.com/docs/en/code-review) の REVIEW.md の節 | Claude Code 文書 | 全文 | 一次 |

> Use this list when evaluating issues in Steps 4 and 5 (these are false positives, do NOT flag):
> - Pre-existing issues
> - Something that appears to be a bug but is actually correct
> - Pedantic nitpicks that a senior engineer would not flag
> - Issues that a linter will catch (do not run the linter to verify)
> - General code quality concerns (e.g., lack of test coverage, general security issues) unless explicitly required in CLAUDE.md
> - Issues mentioned in CLAUDE.md but explicitly silenced in the code (e.g., via a lint ignore comment)

（訳: 段4と段5の評価にこの一覧を使え。これらは偽陽性であり、挙げない。
- 既存の問題
- バグに見えるが実は正しいもの
- 上級エンジニアなら挙げない細かすぎる指摘
- linter が捕まえる問題（確かめるために linter を走らせない）
- 一般的な品質の懸念（テストの不足、一般的なセキュリティ）で、CLAUDE.md が明示的に求めていないもの
- CLAUDE.md にあるが、コードで明示的に黙らせているもの）

security-review.md は「HARD EXCLUSIONS」18項目（番号は17までだが16が2回ある）、「PRECEDENTS」12項目、「SIGNAL QUALITY CRITERIA」4問を持つ。例は次のとおり。

> 7. A lack of hardening measures. Code is not expected to implement all security best practices, only flag concrete vulnerabilities.

（訳: 堅牢化の不足。コードがすべてのベストプラクティスを実装していることは求めない。具体的な脆弱性だけを挙げよ。）

フィルタの文書（:43-45）は "**Start with defaults**: Begin with the default instructions and modify based on false positives you encounter" と "**Document assumptions**: Explain why certain patterns are excluded" と書く。
（訳: 既定から始め、遭遇した偽陽性に基づいて直す。／なぜその型を除外するのかを書く。）

**効果の実測。**示されていない。
**効く観点。**レビュワー側。ループの制御（否定済みの指摘が再び挙がるのを防ぐ）。
**制約。**`/code-review` は REVIEW.md を読まない（Code Review 文書: "The review follows your `CLAUDE.md` like any Claude Code session, but it doesn't read `REVIEW.md`."）。
このリポジトリに REVIEW.md は無い（`git ls-files | grep -i -E '(^|/)review\.md$'` が0件、HEAD `df36f9d7`）。Agent で立てるレビュワーと 3-1 の検証役には渡せる。

**当てはめうる場所（案）。**

- 「否定された指摘の型」を PR をまたいで貯める一覧を1枚置き、[.claude/rules/design-review.md:83](../../../../.claude/rules/design-review.md#L83) の「誰にレビューさせるか」で渡すものに加える。
- 型の元になるのは、別の worker が集めている PR コメントのうち「直さない」の理由である。

### 3-5. 重大度の意味をリポジトリに合わせて具体的に決め、指摘に求める証拠の水準を決める

**何をするか。**「直してからマージ」に入る指摘を種類で列挙する。指摘には file:line の引用と、どう壊れるかの筋書きを必須にする。
**いまの規則と同じ向きか。**証拠の要求は同じ向き（[worker-briefing 2-7](../../../../.claude/skills/worker-briefing/SKILL.md#L277)）。**重大度の中身の定義は、いまの規則に無い。**
`git grep -n 'Critical' -- CLAUDE.md .claude/rules .claude/skills`（HEAD `df36f9d7`）で23行が返った。定義らしい行は [CLAUDE.md:539](../../../../CLAUDE.md#L539) の「Critical / High / Medium / Low / Info」だけで、各レベルに何が入るかは書かれていない。

| 出典 | 発行元・日付 | 取り方 | 一次 / 二次 |
| --- | --- | --- | --- |
| [Code Review](https://code.claude.com/docs/en/code-review) の「Severity levels」と REVIEW.md の節 | Claude Code 文書 | 全文 | 一次 |
| security-review.md:107-123 | anthropics/claude-code-security-review | 原文 | 一次 |
| [Demystifying evals for AI agents](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents) | anthropic.com/engineering、2026-01-09、Mikaela Grace ほか | 要約モデル経由 | 一次 |

Code Review の重大度は3つである。
- Important: "A bug that should be fixed before merging"（マージ前に直すべきバグ）
- Nit: "A minor issue, worth fixing but not blocking"（直す価値はあるが止めない小さな問題）
- Pre-existing: "A bug that exists in the codebase but was not introduced by this PR"（この PR が持ち込んでいない既存のバグ）

> **Severity**: redefine what 🔴 Important means for your repo. The default calibration targets production code; a docs repo, a config repo, or a prototype might want a much narrower definition. State explicitly which classes of finding are Important and which are Nit at most.

（訳: 重大度。Important の意味をリポジトリに合わせて定義し直せ。既定の較正は本番のコードを想定している。文書のリポジトリ、設定のリポジトリ、試作ははるかに狭い定義が要るかもしれない。どの種類の指摘が Important で、どれが最大でも Nit かを明示せよ。）

> **Verification bar**: require evidence before a class of finding is posted. For example, "behavior claims need a `file:line` citation in the source, not an inference from naming" cuts false positives that would otherwise cost the author a round trip.

（訳: 検証の水準。ある種類の指摘を出す前に証拠を求めよ。例えば「挙動についての主張には、名前からの推測ではなく file:line の引用が要る」とすれば、書き手に1往復を払わせる偽陽性が減る。）

security-review.md:109 は、指摘ごとに "exploit scenario"（悪用の筋書き）を必須にしている。
evals の記事は "A good task is one where two domain experts would independently reach the same pass/fail verdict."（良い課題とは、2人の専門家が独立に同じ合否に達するものである）と書く。
人間が 2026-09-04 に求めた「同じ内容を別のレビュワーに依頼したらまったく同じ内容になる」は、公式の書き方ではレビュワーの努力ではなく、**判定の基準が曖昧でないこと**で担保されている。

**効果の実測。**示されていない（"cuts false positives" は主張であって数値ではない）。
**効く観点。**レビュワー側。ループの制御にも効く。[CLAUDE.md:566](../../../../CLAUDE.md#L566) の「収まっている」は Critical と High の件数だけで決まるので、何を High と呼ぶかがぶれると判定もぶれる。
**制約。**REVIEW.md は `/code-review` に届かない（3-4）。Agent のレビュワーには渡せる。

**当てはめうる場所（案）。**

- [CLAUDE.md:534](../../../../CLAUDE.md#L534) の「対応表の列」の「レベル」に、日本語の規則文書と Go のコードそれぞれで Critical と High に入る種類を列挙した定義を足す。
  文書の例: 「書かれたとおりに従うと、利用者か AI が壊れる操作をする」だけを High 以上にする。この例は worker が作ったもので、公式の記述ではない。
- 「この PR より前から在る」を別の印にし、[CLAUDE.md:566](../../../../CLAUDE.md#L566) の件数から外すかを検討する（3-7）。

### 3-6. 2回目以降のレビューでは、新しい軽微な指摘を出させず、件数に上限を置く

**何をするか。**1回目のあとは Important（直してからマージ）だけを出させる。Nit の件数には上限を置く。
**いまの規則と同じ向きか。**同じ向き。2026-09-05 の人間の指示（[CLAUDE.md:573](../../../../CLAUDE.md#L573)）が、Medium と Low について近いことを決めている。
公式はさらに「最初のレビューのあと」から、軽微な指摘を出すこと自体を止める。

| 出典 | 発行元・日付 | 取り方 | 一次 / 二次 |
| --- | --- | --- | --- |
| [Code Review](https://code.claude.com/docs/en/code-review) の「What you can tune」 | Claude Code 文書 | 全文 | 一次 |
| code-review.md:14-20 | anthropics/claude-code | 原文 | 一次 |

> **Nit volume**: cap how many 🟡 Nit comments a single review posts. Prose and config files can be polished forever. A cap like "report at most five nits, mention the rest as a count in the summary" keeps reviews actionable.

（訳: Nit の量。1回のレビューで出す Nit の件数に上限を置け。文章と設定ファイルは永遠に磨ける。「Nit は最大5件、残りは要約に件数だけ書く」のような上限が、レビューを行動に移せるものに保つ。）

> **Re-review convergence**: tell Claude how to behave when a PR has already been reviewed. A rule like "after the first review, suppress new nits and post Important findings only" stops a one-line fix from reaching round seven on style alone.

（訳: 再レビューの収束。PR が既にレビュー済みのときの振る舞いを指示せよ。「最初のレビューのあとは新しい Nit を出さず、Important だけを出す」という規則が、1行の修正が文体だけで7周目に達するのを止める。）

> **Summary shape**: ask for the review body to open with a one-line tally such as `2 factual, 4 style`, and to lead with "no factual issues" when that's the case.

（訳: 要約の形。レビューの本文を `2 factual, 4 style` のような1行の集計で始めさせ、事実の問題が無いときは「no factual issues」を先頭に置かせよ。）

code-review.md:18 は、Claude が既にコメントした PR ならレビューを始めない（"Claude has already commented on this PR" なら止まる）。

**効果の実測。**示されていない（「7周目」は例示であって測定値ではない）。
**効く観点。**ループの制御。日本語の長い規則文書には "Prose … can be polished forever" がそのまま当たる。
**制約。**REVIEW.md は `/code-review` に届かない。`/code-review` は CLAUDE.md を読むので、CLAUDE.md に書けば届く可能性はある。**ただしレビューの振る舞いを変える指示として効くかは測っていない。**CLAUDE.md が長いと指示が埋もれる（3-15）。

**当てはめうる場所（案）。**

- [CLAUDE.md:573](../../../../CLAUDE.md#L573) の節を「2周目からは Critical と High だけを出させる」へ広げる。
- `/code-review` の出力に2周目以降も Medium と Low が混ざったら、受け取る側は対応表に載せず、件数だけ書く。

### 3-7. レビューの範囲を「この変更が持ち込んだもの」に絞り、既存の欠陥は別枠にする

**何をするか。**見つける対象を変更したコードに限る。変更が持ち込んでいない既存の欠陥は、止める理由にしない。
**いまの規則と同じ向きか。**逆向きの面がある。[worker-briefing 2-6](../../../../.claude/skills/worker-briefing/SKILL.md#L256) は「前の周に既に在った誤りが、いま初めて出てきた」を前の周のレビューの落ち度として扱い、見つけることを求めている。

| 出典 | 発行元・日付 | 取り方 | 一次 / 二次 |
| --- | --- | --- | --- |
| code-review.md:35-39、:81 | anthropics/claude-code | 原文 | 一次 |
| security-review.md:36 | anthropics/claude-code-security-review | 原文 | 一次 |
| [Code Review](https://code.claude.com/docs/en/code-review) の Pre-existing | Claude Code 文書 | 全文 | 一次 |

> Agent 3: Opus bug agent … Scan for obvious bugs. Focus only on the diff itself without reading extra context. … Do not flag issues that you cannot validate without looking at context outside of the git diff.
> Agent 4: Opus bug agent … Look for problems that exist in the introduced code. … Only look for issues that fall within the changed code.

（訳: agent 3 は明らかなバグを探す。余計な文脈を読まず差分だけに集中する。差分の外の文脈を見ないと確かめられない問題は挙げない。／agent 4 は持ち込まれたコードにある問題を探す。変更されたコードの中の問題だけを探す。）

security-review.md:36 は "focus ONLY on security implications newly added by this PR. Do not comment on existing security concerns."（この PR が新しく持ち込んだ影響だけに集中し、既存の懸念には触れるな）と書く。

**管理サービスとの違い。**Code Review の管理サービスは "in the context of your full codebase"（コードベース全体の文脈で）見る。そのうえで既存のバグは Pre-existing として別の重大度で出し、マージを止めない。

**効果の実測。**示されていない。
**効く観点。**ループの制御。変更と関係の無い既存の箇所から、周ごとに「新しい」指摘が出るのを止める。
**見つからなかったこと。**「2周目以降は前の周からの差分だけを見せる」という公式の記述は見つからなかった。公式が言っているのは「この PR の変更」までである。
**制約。**`/code-review` は対象として "a ref range such as `main...my-feature`" を受け取る。**前の周の commit から HEAD までを渡したときの挙動は試していない。**

**当てはめうる場所（案）。**

- [CLAUDE.md:566](../../../../CLAUDE.md#L566) の「収まっている」の件数から「この PR より前から在る」指摘を外し、CLAUDE.md が既に決めている follow-up の扱いに回す。
- 2周目以降は、前の周の直しの commit 範囲だけを見せる（未検証）。

### 3-8. 指摘を全部追いかけない。正しさと要件に効くものだけを直し、残りは任意とする

**何をするか。**レビュワーには、正しさか明示された要件に効く抜けだけを挙げさせる。それ以外は任意として扱う。
**いまの規則と同じ向きか。**現象の認識は同じ。[.claude/rules/design-review.md:220](../../../../.claude/rules/design-review.md#L220) の「なぜこの段が要るか」は、1回目の案がログ1行だったのに3回目で17ファイルになった実測を持つ。
**違うのは止め方である。**いまの規則は3・6・9回目に入る段で止める。公式は毎回のレビュワーへの指示で、最初から止める。

| 出典 | 発行元・日付 | 取り方 | 一次 / 二次 |
| --- | --- | --- | --- |
| [Best practices for Claude Code](https://code.claude.com/docs/en/best-practices) の「Add an adversarial review step」 | Claude Code 文書 | 全文 | 一次 |
| [Claude Code 101: Code review](https://academy.claude.com/courses/claude-code-101/code-review) | Claude Academy | 要約モデル経由 | 一次 |
| [Prompting best practices](https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/claude-prompting-best-practices) の「Overeagerness」 | Claude Platform 文書 | 全文 | 一次 |

> A reviewer prompted to find gaps will usually report some, even when the work is sound, because that is what it was asked to do. Chasing every finding leads to over-engineering: extra abstraction layers, defensive code, and tests for cases that can't happen. Tell the reviewer to flag only gaps that affect correctness or the stated requirements, and treat the rest as optional.

（訳: 抜けを探せと言われたレビュワーは、作業が健全でも普通はいくつか報告する。そう頼まれたからである。指摘を全部追いかけると過剰設計になる。余計な抽象化の層、防御的なコード、起こりえない場合のテスト。レビュワーには正しさか明示された要件に効く抜けだけを挙げさせ、残りは任意として扱え。）

同じ節のプロンプト例。

> Check that every requirement is implemented, the listed edge cases have tests, and nothing outside the task's scope changed. Report gaps, not style preferences.

（訳: 要件がすべて実装され、挙げたエッジケースにテストがあり、タスクの範囲の外が変わっていないかを確かめよ。好みの文体ではなく抜けを報告せよ。）

Academy（要約モデル経由）は、指摘を3つの山に分けさせる。"Fix now"（直す）、"Ask why"（確かめきれない・おかしいと思う指摘）、"Leave it"（本物だが小さい）。
そのうえで "If a fix eventually grows into a large change of its own, run the review again."（修正がそれ自体で大きな変更に育ったら、もう一度レビューを回す）と書く。

Prompting best practices の過剰設計を抑える例は "Defensive coding: Don't add error handling, fallbacks, or validation for scenarios that can't happen."（起こりえない場合のエラー処理・代替・検証を足すな）を含む。
Prompting Claude Opus 5 は "Claude Opus 5 can also expand the scope of a task, adding steps that weren't requested"（Opus 5 は頼まれていない段を足してタスクの範囲を広げることがある）と書く。

**効果の実測。**示されていない。
**効く観点。**ループの制御、レビュワー側。
**制約。**`/code-review` には指示を足せない。Agent のレビュワーには書ける。

**当てはめうる場所（案）。**

- [.claude/rules/design-review.md:101](../../../../.claude/rules/design-review.md#L101) の4つの観点に「正しさか、issue に書かれた要件に効かない抜けは挙げない」を足す。
- Academy の「修正が大きな変更に育ったら再レビュー」を、[CLAUDE.md:573](../../../../CLAUDE.md#L573) の周回の判断に取り込めるか検討する。いまは Critical か High を直したら、必ず次の周を回す。

### 3-9. 書く前に「完了の条件」と「範囲外」を合意し、レビュワーはそれに照らして判定する

**何をするか。**書き始める前に、何をもって完了とするかと、何が範囲外かを文章にする。採点役もその条件で判定する。
**いまの規則と同じ向きか。**同じ向き。[.claude/rules/design-review.md:3](../../../../.claude/rules/design-review.md#L3) の段1〜2（設計を固め、issue に書く）がある。
**違うのは、公式は完了の条件を採点役と先に合意し、採点をその条件に縛る点である。**

| 出典 | 発行元・日付 | 取り方 | 一次 / 二次 |
| --- | --- | --- | --- |
| [Harness design for long-running application development](https://www.anthropic.com/engineering/harness-design-long-running-apps) | anthropic.com/engineering、2026-03-24、Prithvi Rajasekaran | 要約モデル経由 | 一次 |
| [Best practices for Claude Code](https://code.claude.com/docs/en/best-practices) の「Let Claude interview you」 | Claude Code 文書 | 全文 | 一次 |
| [Building effective agents](https://www.anthropic.com/engineering/building-effective-agents) | anthropic.com/engineering、2024-12-19 | 要約モデル経由 | 一次 |
| [Skill authoring best practices](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices) | Claude Platform 文書 | 全文 | 一次 |
| [Effective harnesses for long-running agents](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents) | anthropic.com/engineering、2025-11-26、Justin Young | 要約モデル経由 | 一次 |

> Before each sprint, the generator and evaluator negotiated a sprint contract: agreeing on what 'done' looked like for that chunk of work before any code was written.

（訳: 各スプリントの前に、生成役と評価役がスプリントの契約を交わした。コードを書く前に、その作業の「完了」がどういう状態かを合意した。harness design）

> Each criterion had a hard threshold, and if any one fell below it, the sprint failed.

（訳: 基準ごとに厳格な閾値があり、1つでも下回ればスプリントは失敗とした。同上）

> The most useful specs are self-contained: they name the files and interfaces involved, state what is out of scope, and end with an end-to-end verification step that proves the feature works. Time spent making the spec precise pays off more than time spent watching the implementation.

（訳: 最も役に立つ仕様は自己完結している。関わるファイルとインターフェースを名指しし、範囲外を明記し、機能が動くことを証明する端から端までの検証の段で終わる。仕様を正確にする時間は、実装を見張る時間より報われる。Best practices）

Building effective agents は、評価と最適化を繰り返す型が合う条件として "This workflow is particularly effective when we have clear evaluation criteria, and when iterative refinement provides measurable value."（評価の基準が明確で、繰り返しの改善が測れる価値を生むときに特に効く）と書く。
Skill authoring best practices は "**Create evaluations BEFORE writing extensive documentation.**"（大量の文書を書く前に評価を作れ）と書く。
Effective harnesses は "Only mark features as 'passing' after careful testing."（注意深く試してから「通過」と印を付けよ）と書く。

**効果の実測。**harness design に、単独で "20 min"・"$9"、道具立て全体で "6 hr"・"$200" の比較がある。**品質の差は数値で取り出していない。周回数への効果の数値も示されていない。**
**効く観点。**書く側（指摘の元を減らす）。ループの制御（範囲外の指摘を「範囲外」と判定できる）。
**制約。**無い。

**当てはめうる場所（案）。**

- [.claude/rules/design-review.md:3](../../../../.claude/rules/design-review.md#L3) の段2で書く設計コメントに、「完了の条件（1行ずつ機械か目で確かめられる形）」と「範囲外」を必須にする。実装レビューのレビュワーにもそれを渡す。
- 範囲外の指摘は Critical と High に数えない。これは [CLAUDE.md:566](../../../../CLAUDE.md#L566) の定義を変えることになる。

### 3-10. 書く側に、機械で合否が出る検査を持たせ、レビューに出す前に通す

**何をするか。**繰り返し出る指摘の型を、書く側が自分で走らせる検査（テスト・スクリプト・hook）に移す。レビューに出す前にそれを通す。
**いまの規則と同じ向きか。**同じ向き。[.claude/rules/plan-file.md:93](../../../../.claude/rules/plan-file.md#L93) のリンクの検算スクリプトや、返答を検査する hook がある。
公式が加えるのは、「繰り返し出る型を検査へ移す」ことを手順にする考え方である。

| 出典 | 発行元・日付 | 取り方 | 一次 / 二次 |
| --- | --- | --- | --- |
| [Best practices for Claude Code](https://code.claude.com/docs/en/best-practices) の「Give Claude a way to verify its work」「Set up hooks」 | Claude Code 文書 | 全文 | 一次 |
| [Building agents with the Claude Agent SDK](https://claude.com/blog/building-agents-with-the-claude-agent-sdk) | claude.com/blog、2025-09-29、Thariq Shihipar | 要約モデル経由 | 一次 |
| [Building a C compiler with a team of parallel Claudes](https://www.anthropic.com/engineering/building-c-compiler) | anthropic.com/engineering、2026-02-05、Nicholas Carlini | 要約モデル経由 | 一次 |
| [Automate actions with hooks](https://code.claude.com/docs/en/hooks-guide) | Claude Code 文書 | 全文 | 一次 |
| [Skill authoring best practices](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices) | Claude Platform 文書 | 全文 | 一次 |

> Claude stops when the work looks done. Without a check it can run, "looks done" is the only signal available, and you become the verification loop: every mistake waits for you to notice it.

（訳: Claude は作業が済んだように見えたら止まる。走らせられる検査が無いと「済んだように見える」が唯一の合図になり、人が検証のループになる。すべての誤りが、人が気づくのを待つ。Best practices）

> Unlike CLAUDE.md instructions which are advisory, hooks are deterministic and guarantee the action happens.

（訳: 助言にすぎない CLAUDE.md の指示と違い、hook は決定的で、その動作が起きることを保証する。同上）

同じ文書の失敗例の節は "If Claude already does something correctly without the instruction, delete it or convert it to a hook."（指示が無くても正しくやれていることは、消すか hook に変えよ）と書く。
Agent SDK の記事は "The best form of feedback is providing clearly defined rules for an output, then explaining which rules failed and why."（最良のフィードバックは、出力の規則を明確に定め、どの規則がなぜ落ちたかを説明することである）と書く。
C compiler の記事は "So it's important that the task verifier is nearly perfect, otherwise Claude will solve the wrong problem."（検証役がほぼ完璧であることが重要で、そうでないと Claude は違う問題を解く）と書く。
Skill authoring best practices は "Common pattern: Run validator → fix errors → repeat. This pattern greatly improves output quality."（検証を走らせ、誤りを直し、繰り返す。この型は出力の品質を大きく上げる）と書く。

hooks ガイドには、Stop hook で検査する例がある（`"type": "agent"` で "Verify that all unit tests pass. Run the test suite and check the results. $ARGUMENTS"）。
ただし "Claude Code overrides a Stop hook after it blocks eight times in a row without progress."（進みが無いまま8回続けて止めると、Claude Code は Stop hook を無視する）。

**効果の実測。**示されていない（"greatly improves" は数値ではない）。C compiler の "nearly 2,000 Claude Code sessions"・"just under $20,000"・"100,000-line compiler" は規模であって、検査の効果ではない。
**効く観点。**書く側、修正漏れ。

**制約。**

- 対話セッションの中で動く。
- agent 型の hook は "Agent hooks are experimental." と書かれている。
- Stop hook の設定を変えるのは設定の変更であり、この worker の範囲外である。
- `/goal` の評価役は "Evaluation tokens are billed on the small fast model configured for your provider and are typically negligible" と書かれている。**plan の使用量に数えるのか usage credits なのかは確かめていない。**

**当てはめうる場所（案）。**

- 過去のレビューで繰り返し出た型をスクリプトにする。例は、設計文書へ行を足したときのリンクの行ずれ（[.claude/rules/plan-file.md:93](../../../../.claude/rules/plan-file.md#L93) の節が「8周のレビューのうち3周が、同じ原因」と記録している）。型の一覧は別の worker の集計を待つ。
- [pr-review-and-merge 段2](../../../../.claude/skills/pr-review-and-merge/SKILL.md#L93) の前に、書く側がそれを走らせる段を置く。

### 3-11. 同じ誤りが他に残っていないかを確かめる（修正漏れ）

**言いたいこと。**公式に「直すときは同種の箇所を全部探して直せ」を手順として書いた記述は見つからなかった（5章に検索語）。いちばん近いものを4つ挙げる。
**いまの規則と同じ向きか。**[worker-briefing 2-5](../../../../.claude/skills/worker-briefing/SKILL.md#L183) の「文字列で数える」「直した文の前提を1文にして探す」は、公式のどの記述よりも具体的である。

| 近いもの | 出典 | 取り方 |
| --- | --- | --- |
| 同じ問題をファイルごとに agent を1つ当てて探し、見つかった指摘を検証する | [Orchestrate subagents at scale with dynamic workflows](https://code.claude.com/docs/en/workflows) の例 | 全文 |
| 変更によって CLAUDE.md の記述が古くなったら、それも指摘する | [Code Review](https://code.claude.com/docs/en/code-review) の「CLAUDE.md」 | 全文 |
| 症状ではなく原因を直す | [Best practices for Claude Code](https://code.claude.com/docs/en/best-practices) | 全文 |
| 何度も落ちる型は、道具の側に「見つけて直す」規則として入れる | [Building agents with the Claude Agent SDK](https://claude.com/blog/building-agents-with-the-claude-agent-sdk) | 要約モデル経由 |

> Audit many files for the same issue — Fan out one agent per file, then collect and verify the findings.
> `use a workflow to audit every route handler under src/routes/ for missing authentication checks, and adversarially verify each finding before reporting it`

（訳: 多数のファイルを同じ問題について調べる。ファイルごとに agent を1つ立て、指摘を集めて検証する。／例: src/routes/ の下のすべてのルートハンドラを認証の抜けについて調べ、報告する前に各指摘を敵対的に検証せよ。workflows 文書）

同じ文書は止め方も例示している。"Find issues until the list stops growing — Keep searching in rounds and stop when new rounds turn up nothing new."（一覧が増えなくなるまで探す。周ごとに探し、新しい周で何も増えなくなったら止める）

> This works bidirectionally: if your PR changes code in a way that makes a `CLAUDE.md` statement outdated, Claude flags that the docs need updating too.

（訳: これは両方向に効く。PR の変更が CLAUDE.md の記述を古くしたら、文書の更新も要ると指摘する。Code Review 文書）

Best practices の表の例は "address the root cause, don't suppress the error"（原因を直せ。誤りを黙らせるな）。
Agent SDK の記事は "If your agent fails at a task repeatedly, can you add a formal rule in your tool calls to identify and fix the failure?"（agent が同じ課題で何度も落ちるなら、道具の呼び出しに、その失敗を見つけて直す明文の規則を足せないか）と書く。
code-review.md:74 は "changes spanning multiple locations"（複数箇所にまたがる修正）を、提案ブロックを付けずに説明だけする扱いにしている。

**効果の実測。**無い。
**効く観点。**修正漏れ。
**制約。**workflows は有料プランで使え、"Runs count toward your plan's usage and rate limits."（実行は plan の使用量とレートリミットに数える）。規模の指針の既定は `medium`（10 agent 未満）。

**当てはめうる場所（案）。**

- 2-5 の段2（検索）では文字列が当たらない「前提」の確認を、変更したファイルの節ごとに agent を1つ当てる形で補えるか検討する。
- 止め方は「新しい周で候補が増えなくなるまで」にする。2-5 の段3（件数・検索パターン・範囲の報告）は残す。

### 3-12. 評価役を、人間の判断と食い違った事例から較正する

**何をするか。**評価役の出力を読み、人間の判断と食い違った事例を見つけて、評価役の指示を直す。これを繰り返す。
**いまの規則と同じ向きか。**いまの規則には、レビュワーの指示を過去の誤りから直す手順が無い。規則は人間の指摘を受けて足されている。

| 出典 | 発行元・日付 | 取り方 | 一次 / 二次 |
| --- | --- | --- | --- |
| [Harness design for long-running application development](https://www.anthropic.com/engineering/harness-design-long-running-apps) | 2026-03-24 | 要約モデル経由 | 一次 |
| [Demystifying evals for AI agents](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents) | 2026-01-09 | 要約モデル経由 | 一次 |
| [How we built our multi-agent research system](https://www.anthropic.com/engineering/multi-agent-research-system) | 2025-06-13、Jeremy Hadfield ほか | 要約モデル経由 | 一次 |
| [skills/skill-creator/agents/grader.md](https://github.com/anthropics/skills/blob/34040c9c568585f6929bedeaad110ad08f079624/skills/skill-creator/agents/grader.md) | anthropics/skills | 原文 | 一次 |

> The tuning loop was to read the evaluator's logs, find examples where its judgment diverged from mine, and update the QAs prompt to solve for those issues.

（訳: 調整のループは、評価役のログを読み、その判断が自分と食い違った事例を見つけ、それを解くように QA のプロンプトを直すことだった。harness design）

> I calibrated the evaluator using few-shot examples with detailed score breakdowns.

（訳: 採点の内訳を詳しく書いた少数の例で、評価役を較正した。同上）

evals の記事は、判定役について次のように書く。
- "LLM-as-judge graders should be closely calibrated with human experts to gain confidence."（人間の専門家に近く較正せよ）
- "grade each dimension with an isolated LLM-as-judge rather than using one to grade all dimensions."（1体に全部の観点を採点させず、観点ごとに独立した判定役で採点せよ）
- "Give the LLM a way out, like providing an instruction to return 'Unknown' when it doesn't have enough information."（情報が足りないときは「Unknown」と返せる逃げ道を与えよ）

multi-agent research の記事は "single LLM call with a single prompt outputting scores from 0.0-1.0 and a pass-fail grade was the most consistent"（1回の呼び出しで 0.0〜1.0 の点数と合否を出させる形が最も一貫していた）と書く。
grader.md:9 は "A passing grade on a weak assertion is worse than useless — it creates false confidence."（弱い条件での合格は無益より悪い。偽の安心を生む）と書く。

**効果の実測。**示されていない（harness design は "several rounds" とだけ書く）。
**効く観点。**レビュワー側。
**制約。**無い（過去の PR コメントを読むだけ）。

**当てはめうる場所（案）。**

- 別の worker が集めている周ごとのデータから、「レビュワーは Critical か High としたが、対応表で根拠を否定して直さなかった」事例を拾う。それを 3-4 の一覧と、[.claude/rules/design-review.md:101](../../../../.claude/rules/design-review.md#L101) の観点の例に足す。
- [worker-briefing 2-7](../../../../.claude/skills/worker-briefing/SKILL.md#L277) に「確かめきれない指摘は重大度を付けず『未確認』で返してよい」という逃げ道を足す。

### 3-13. 同じ観点を独立に複数見させ、まとめてから確かめる

**何をするか。**観点を分けた複数のレビュワーを並列に走らせ、重複を除き、3-1 の検証に回す。
**いまの規則と同じ向きか。**違う。人間が 2026-09-04 に求めた「同じ内容を別のレビュワーに依頼したらまったく同じ内容になるぐらい」は、1人のレビュワーの徹底で結果を揃えることを求めている。
**公式の設計は、モデルの判定役が非決定的だという前提に立つ。**複数の agent の和集合と検証で揃えている。1つの agent への指示だけには頼っていない。

| 出典 | 発行元・日付 | 取り方 | 一次 / 二次 |
| --- | --- | --- | --- |
| code-review.md:30-39 と [README](https://github.com/anthropics/claude-code/blob/f96c3b49c4c8721685206aaab23609b2d399df4e/plugins/code-review/README.md) | anthropics/claude-code | 原文 | 一次 |
| [plugins/feature-dev/commands/feature-dev.md](https://github.com/anthropics/claude-code/blob/f96c3b49c4c8721685206aaab23609b2d399df4e/plugins/feature-dev/commands/feature-dev.md):106 | anthropics/claude-code | 原文 | 一次 |
| [Demystifying evals for AI agents](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents) | 2026-01-09 | 要約モデル経由 | 一次 |
| [How we built our multi-agent research system](https://www.anthropic.com/engineering/multi-agent-research-system) | 2025-06-13 | 要約モデル経由 | 一次 |

code-review.md:30-32 は4体を並列に走らせ、そのうち2体に同じ観点（CLAUDE.md への適合）を持たせる。README:234 はこれを "**2x CLAUDE.md compliance agents**: Redundancy for guideline checks"（ガイドラインの検査の冗長化）と説明する。
feature-dev.md:106 は "Launch 3 code-reviewer agents in parallel with different focuses: simplicity/DRY/elegance, bugs/functional correctness, project conventions/abstractions"（観点を変えて3体を並列に立てよ）と書く。
evals の記事は、モデルによる採点の弱みに "Non-deterministic"（非決定的）を挙げる。

**効果の実測。**multi-agent research（要約モデル経由）が次の数値を出している。**調査タスクの社内評価であって、コードレビューではない。**
- "outperformed single-agent Claude Opus 4 by 90.2%"（単独の agent より 90.2% 上回った）
- "token usage by itself explains 80% of the variance"（トークン使用量だけで差の80%を説明する）
- "multi-agent systems use about 15× more tokens"（約15倍のトークンを使う）

**効く観点。**レビュワー側（再現率）。
**制約。**トークンが増える。**ローカルの `/code-review` が中で何体の agent を使うかは文書に書かれていない。**ultrareview 文書が "a larger fleet of reviewer agents"（より大きな reviewer agent の群れ）と比べているだけである。
**当てはめうる場所（案）。**1周目だけ、`/code-review` と、観点を分けた Agent のレビュワーを並列に走らせる。観点の例は「issue の要件への適合」と「同じ前提が残っている箇所」。重複を除いて 3-1 の検証に回す。2周目以降は 3-6 と 3-7 で範囲を絞る。

### 3-14. 周回に「進まなかったら止める」条件を置き、失敗が2回重なったら文脈を捨てて書き直す

**何をするか。**回数の上限に加えて、「2回続けて進まなかったら止める」条件を置く。同じ問題の修正が2回失敗したら、同じ文脈で直し続けない。学んだことを入れた新しい指示で書き直す。
**いまの規則と同じ向きか。**上限（連続10回）は同じ向き。**違うのは2点である。**公式の止め方は回数ではなく進み具合で決まる。失敗が2回重なった時点で文脈を捨てる。

| 出典 | 発行元・日付 | 取り方 | 一次 / 二次 |
| --- | --- | --- | --- |
| [Best practices for Claude Code](https://code.claude.com/docs/en/best-practices) の「Course-correct early and often」 | Claude Code 文書 | 全文 | 一次 |
| [Building effective agents](https://www.anthropic.com/engineering/building-effective-agents) | 2024-12-19 | 要約モデル経由 | 一次 |
| [dynamic workflows](https://code.claude.com/docs/en/workflows) の例 | Claude Code 文書 | 全文 | 一次 |
| [Keep Claude working toward a goal](https://code.claude.com/docs/en/goal) | Claude Code 文書 | 全文 | 一次 |
| [plugins/ralph-wiggum/README.md](https://github.com/anthropics/claude-code/blob/f96c3b49c4c8721685206aaab23609b2d399df4e/plugins/ralph-wiggum/README.md):121 | anthropics/claude-code | 原文 | 一次 |
| [Claude Code 101: Code review](https://academy.claude.com/courses/claude-code-101/code-review) | Claude Academy | 要約モデル経由 | 一次 |

> If you've corrected Claude more than twice on the same issue in one session, the context is cluttered with failed approaches. Run `/clear` and start fresh with a more specific prompt that incorporates what you learned. A clean session with a better prompt almost always outperforms a long session with accumulated corrections.

（訳: 1つのセッションで同じ問題を2回より多く直させたなら、文脈は失敗した方法で散らかっている。/clear して、学んだことを入れたより具体的な指示で始め直せ。良い指示の新しいセッションは、修正を重ねた長いセッションにほぼ必ず勝つ。Best practices）

同じ文書の失敗例は "**Correcting over and over.** … **Fix**: After two failed corrections, `/clear` and write a better initial prompt incorporating what you learned."（直しても直しても違う。2回失敗したら /clear し、学んだことを入れた最初の指示を書き直せ）と書く。

Building effective agents は "it's also common to include stopping conditions (such as a maximum number of iterations) to maintain control."（制御を保つために、繰り返しの上限などの停止条件を置くのが普通である）と書く。

workflows 文書の例は、進み具合での止め方を2つ示す。
- "keep fixing the reported errors until the type check passes or two rounds in a row make no progress"（型検査が通るか、2周続けて進まなくなるまで直し続けよ）
- "stop once two rounds in a row find nothing new"（2周続けて新しいものが見つからなければ止めよ）

`/goal` 文書は "include a turn or time clause in the condition, such as `or stop after 20 turns`" と書く。進みの無い turn が続くと止まることも書いている。
ralph-wiggum README:121 は "Always use `--max-iterations` as a safety net to prevent infinite loops on impossible tasks" と書く。
Academy は "A long session carries everything it has read and decided. … it's exactly the history you don't want in a reviewer."（長いセッションは読んで決めたことを全部抱えている。それこそレビュワーに持たせたくない履歴である）と書く。

**効果の実測。**示されていない（"almost always outperforms" は数値ではない）。
**効く観点。**ループの制御。
**制約。**無い。
**当てはめうる場所（案）。**[CLAUDE.md:613](../../../../CLAUDE.md#L613) と [:663](../../../../CLAUDE.md#L663) の「3・6・9回目」の段に、次の条件を足す。

> Critical と High の件数が2周続けて減らなかったら、その時点で修正を止め、対応表から学んだことを入れた設計で、新しい文脈の書き手に書き直させる。

いまの6段（[CLAUDE.md](../../../../CLAUDE.md) 以下）には「実装を止める → 設計を敵対的レビューする → 実装し直す」が既にある。**入る時点を早めるだけで足りるかを検討する。**

### 3-15. 規則を足し続けず、削る。機械で守らせられるものは hook へ移し、強い言い回しを減らす

**何をするか。**「消したら AI が間違えるか」で1行ずつ見直し、要らないものを消す。機械で判定できるものは hook やスクリプトに移す。全部を強調する書き方をやめる。
**いまの規則と同じ向きか。**逆向き。いまの規則は、人間の指摘を受けて節と強調を足してきた。

いまの規模を測った（HEAD `df36f9d7`、`wc -l` と `grep -c '\*\*'`）。

| ファイル | 行数 | `**` を含む行 |
| --- | --- | --- |
| [CLAUDE.md](../../../../CLAUDE.md) | 774 | 350 |
| [.claude/rules/design-review.md](../../../../.claude/rules/design-review.md) | 248 | 125 |
| [.claude/skills/worker-briefing/SKILL.md](../../../../.claude/skills/worker-briefing/SKILL.md) | 490 | 247 |
| [.claude/skills/pr-review-and-merge/SKILL.md](../../../../.claude/skills/pr-review-and-merge/SKILL.md) | 310 | 112 |

**これらの数値は多いか少ないかの線と一緒には出せない。公式に行数の線は無い**（Skill authoring best practices の "Keep SKILL.md body under 500 lines" は skill の本文の目安）。

| 出典 | 発行元・日付 | 取り方 | 一次 / 二次 |
| --- | --- | --- | --- |
| [Best practices for Claude Code](https://code.claude.com/docs/en/best-practices) の「Write an effective CLAUDE.md」 | Claude Code 文書 | 全文 | 一次 |
| [Prompting best practices](https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/claude-prompting-best-practices) | Claude Platform 文書 | 全文 | 一次 |
| [skills/skill-creator/SKILL.md](https://github.com/anthropics/skills/blob/34040c9c568585f6929bedeaad110ad08f079624/skills/skill-creator/SKILL.md):298-302 | anthropics/skills | 原文 | 一次 |
| [Harnessing Claude's intelligence](https://claude.com/blog/harnessing-claudes-intelligence) | claude.com/blog、2026-04-02、Lance Martin | 要約モデル経由 | 一次 |
| [Effective context engineering for AI agents](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents) | anthropic.com/engineering、2025-09-29 | 要約モデル経由 | 一次 |

> Keep it concise. For each line, ask: *"Would removing this cause Claude to make mistakes?"* If not, cut it. Bloated CLAUDE.md files cause Claude to ignore your actual instructions!

（訳: 簡潔に保て。各行について「これを消したら Claude は間違えるか」と問い、間違えないなら消せ。膨らんだ CLAUDE.md は、本当の指示を Claude に無視させる。）

> If Claude keeps skipping one instruction, add emphasis such as "IMPORTANT" to that line alone. If you emphasize many lines, none of them stands out.

（訳: Claude が1つの指示を飛ばし続けるなら、その1行だけに「IMPORTANT」のような強調を足せ。多くの行を強調すると、どれも目立たない。）

> Claude Opus 4.5 and Claude Opus 4.6 are also more responsive to the system prompt than previous models. … The fix is to dial back any aggressive language. Where you might have said "CRITICAL: You MUST use this tool when...", you can use more normal prompting like "Use this tool when...".

（訳: Opus 4.5 と 4.6 は、以前のモデルよりシステムプロンプトによく反応する。直し方は強い言い回しを弱めることである。「CRITICAL: You MUST …」ではなく「Use this tool when …」のような普通の書き方でよい。）

skill-creator SKILL.md は次のように書く。
- :298 "Rather than put in fiddly overfitty changes, or oppressively constrictive MUSTs, if there's some stubborn issue, you might try branching out and using different metaphors …"（しつこい問題があるときは、細かく過剰適合した変更や、息苦しい MUST を入れる代わりに、別のたとえや別の作業の型を試せ）
- :302 "If you find yourself writing ALWAYS or NEVER in all caps, or using super rigid structures, that's a yellow flag"（大文字の ALWAYS や NEVER を書いていたり、硬すぎる構造を使っていたりしたら、それは黄信号である）

Harnessing Claude's intelligence は "Agent harnesses encode assumptions about what Claude can't do on its own, but those assumptions grow stale as Claude gets more capable."（道具立ては「Claude が単独ではできないこと」の仮定を埋め込んでいるが、Claude が賢くなるにつれてその仮定は古くなる）と書く。
Code Review 文書は REVIEW.md について "Length has a cost: a long `REVIEW.md` dilutes the rules that matter most."（長さには代償がある。長い REVIEW.md は最も大事な規則を薄める）と書く。

**効果の実測。**示されていない。
**効く観点。**書く側とレビュワー側（指示が届く）。
**制約。**無い。

**当てはめうる場所（案）。**

- レビューループに関わる規則を「消したら AI が間違えるか」で1行ずつ見直す。対象は [CLAUDE.md:504](../../../../CLAUDE.md#L504) 以下、[.claude/rules/design-review.md](../../../../.claude/rules/design-review.md)、[worker-briefing 2-5〜2-7](../../../../.claude/skills/worker-briefing/SKILL.md#L183)、[pr-review-and-merge 段2〜4](../../../../.claude/skills/pr-review-and-merge/SKILL.md#L93)。
- 機械で判定できる部分は、既にある hook やスクリプトと重ねない。

---

## 4. 否定的な知見

| 知見 | 出典（取り方） | このリポジトリにとっての意味（推測を含む） |
| --- | --- | --- |
| **自分の成果物を自分で評価させると甘くなる。**"agents tend to respond by confidently praising the work—even when, to a human observer, the quality is obviously mediocre."（人から見て明らかに凡庸でも、自信を持って褒める） | [Harness design](https://www.anthropic.com/engineering/harness-design-long-running-apps)（要約モデル経由） | 書き手の自己点検でレビューを代替できない |
| **Opus 5 では、検証や再確認の指示が逆効果になる。**"instructions like these cause over-verification on Claude Opus 5, and removing them reduces wasted tokens with no loss in quality."（こうした指示は過剰な検証を起こし、消しても品質は落ちずに無駄なトークンが減る）。"Avoid instructing re-checks it already performs … these compound with the model's own behavior and add cost without improving results." | [Prompting Claude Opus 5](https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/prompting-claude-opus-5)（全文）、[What's new in Claude Opus 5](https://platform.claude.com/docs/en/models/opus-5/whats-new-opus-5)（全文） | 規則の「念のため再確認せよ」は、Opus 5 では周の品質を上げずに費用を増やしうる。**他のモデルでは逆で、Prompting best practices は "Before you finish, verify your answer against [test criteria]." が "catches errors reliably" と書く。**この worker は Opus 5 で走っている（システムの表示）。オーケストレーターと `/code-review` のモデルは確かめていない |
| **抜けを探せと頼まれたレビュワーは、作業が健全でも何か挙げる。** | [Best practices](https://code.claude.com/docs/en/best-practices)（全文。3-8 に引用） | 「Critical と High が0件になるまで回す」は、レビュワーが何かを High と呼び続ける限り収束しない形になりうる |
| **LLM を判定役にするのは頑健でない。**"This is generally not a very robust method, and can have heavy latency tradeoffs"（一般に頑健な方法ではなく、遅延の代償も大きい） | [Building agents with the Claude Agent SDK](https://claude.com/blog/building-agents-with-the-claude-agent-sdk)（要約モデル経由） | 判定を機械の検査に寄せる理由になる（3-10） |
| **モデルの判定役は非決定的である。** | [Demystifying evals](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents)（要約モデル経由） | 1体のレビュワーへの指示だけで「別のレビュワーでも同じ結果」は保証されない。記事は判定役一般について書いており、コードレビューの測定ではない |
| **修正を重ねた長い会話は、書き直した新しい会話に負ける。** | [Best practices](https://code.claude.com/docs/en/best-practices)（全文。3-14 に引用） | 同じ文脈で10周まで直し続ける形は、公式の推奨と逆である |
| **利用者の考えに合わせる傾向（sycophancy）。**"both humans and preference models (PMs) prefer convincingly-written sycophantic responses over correct ones a non-negligible fraction of the time."（人も選好モデルも、無視できない割合で、正しい応答より上手に書かれた迎合的な応答を好む） | [Towards understanding sycophancy in language models](https://www.anthropic.com/research/towards-understanding-sycophancy-in-language-models)、2023-10-23（要約モデル経由） | **2023年のモデルでの結果で、いまのモデルに当てはまるかは書かれていない。**指摘を受けた書き手が根拠を確かめずに受け入れる方向に働きうる。[.claude/rules/design-review.md:124](../../../../.claude/rules/design-review.md#L124) の「否定できるなら直さない」はこれに対抗する規則として読める |
| **評価役に価値があるのは、単独では確実に解けない課題だけである。**"It is worth the cost when the task sits beyond what the current model does reliably solo."。モデルが良くなってスプリントの仕組みを外した（"I started by removing the sprint construct entirely"） | [Harness design](https://www.anthropic.com/engineering/harness-design-long-running-apps)（要約モデル経由） | 小さな文書の変更に毎回同じ重さのレビューを回すことの見直しの材料になる |
| **評価と最適化の繰り返しが合うのは、評価の基準が明確なときである。** | [Building effective agents](https://www.anthropic.com/engineering/building-effective-agents)（要約モデル経由。3-9 に引用） | 基準が曖昧な日本語の規則文書のレビューは、収束しにくい形になりうる（推測） |
| **強い言い回しには過剰に反応する。**大文字の MUST は黄信号である。 | [Prompting best practices](https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/claude-prompting-best-practices)（全文）、skill-creator SKILL.md:302（原文） | 3-15 |
| **Opus 5 はタスクの範囲を広げることがある。** | [Prompting Claude Opus 5](https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/prompting-claude-opus-5)（全文） | 指摘への対応で変更が膨らむ要因になりうる（推測。3-8） |

---

## 5. 探したが見つからなかったもの

| 探したもの | 検索語・叩いたもの | 場所 | 結果 |
| --- | --- | --- | --- |
| **直すときに同種の箇所を全部直させる公式の手順** | WebSearch「Claude Code fix all similar occurrences after review finding same bug elsewhere codebase」（ドメインは anthropic.com / claude.com / code.claude.com / platform.claude.com / docs.claude.com）と、`grep -i -E 'similar\|other occurrence\|same pattern\|elsewhere\|all instances\|every instance\|spanning multiple\|root cause\|duplicate'` | 取得した公式の定義ファイル。code-review のコマンドと README、pr-review-toolkit の agent 5本とコマンド、feature-dev の2本、ralph-wiggum の README、security-review のコマンド、skill-creator の SKILL.md と grader.md、claude-code-action の review-pr（commit は冒頭の表） | **手順としての記述は0件。**当たったのは code-review.md:74（複数箇所にまたがる修正）と silent-failure-hunter.md:114（すべての箇所を指摘せよ）だけ。**WebSearch の要約は「コードベース全体から似たパターンを探して全部直すよう頼める」と書いた。**しかし候補の2ページ（[claude.com/blog/fix-software-bugs-faster-with-claude](https://claude.com/blog/fix-software-bugs-faster-with-claude)、Academy の Code Review の回）を開くと、その文は無かった。**出所は確かめられていない** |
| レビューの周回数と品質の関係を測った公式の数値 | engineering の一覧25本の題名と、開いた記事 | anthropic.com/engineering、claude.com/blog | 見つからない。harness design の "5 to 15 iterations per generation" は設計の生成の反復で、コードレビューの周ではない |
| Claude Code Review の再レビュー（push ごと）で、新しい指摘がどれだけ出るかの数値 | Code Review 文書、blog | code.claude.com、claude.com/blog | 見つからない |
| 日本語の長い規則文書のレビューについての公式の手法 | Code Review 文書 | code.claude.com | 専用の手法は無い。REVIEW.md の節に "a docs repo … might want a much narrower definition" と "Prose and config files can be polished forever" があるだけ |
| ローカルの `/code-review` が中で何体の agent を使い、どのモデルで走るか | Code Review 文書、ultrareview 文書、skills 文書 | code.claude.com | 書かれていない。skills 文書は `/code-review` を「forked subagent として走る skill」と書く。同じ文書は `context: fork` の skill について "The subagent doesn't see your conversation history"（会話の履歴は見えない）と書き、CLAUDE.md は agent の型に応じて読むとしている |
| 前の周からの差分だけを再レビューさせる公式の手順 | Code Review 文書 | code.claude.com | 見つからない。管理サービスは push ごとにレビューし、直った指摘のスレッドを自動で閉じる、とだけある |
| 書き手が「直さない」と判断する基準 | Best practices、Academy | code.claude.com、academy.claude.com | Academy の3つの山と、Best practices の「残りは任意」だけ（3-8） |
| このリポジトリで Critical と High の中身を定義した行 | `git grep -n 'Critical' -- CLAUDE.md .claude/rules .claude/skills` | HEAD `df36f9d7` | 23行が返り、定義の行は無い（3-5） |

---

## 6. 開けなかった URL・一部しか読めなかったもの・開いていないもの

### 転送されたもの・一部しか読めなかったもの

| URL | 何が起きたか |
| --- | --- |
| https://www.anthropic.com/engineering/claude-code-best-practices | 308 で https://code.claude.com/docs/en/best-practices へ転送された。転送先は全文を読んだ。**2025-04-18 の元の記事の本文は見ていない** |
| https://www.anthropic.com/engineering/building-agents-with-the-claude-agent-sdk | 308 で https://claude.com/blog/building-agents-with-the-claude-agent-sdk へ転送された。転送先は要約モデル経由で読んだ |
| https://code.claude.com/docs/en/sub-agents | 「Fork the current conversation」の節が、ツールの出力で途中までだった |
| https://www.anthropic.com/research/towards-understanding-sycophancy-in-language-models | 著者と数値が、要約の出力に出てこなかった。論文本体は開いていない |

### 要約モデル経由でしか読んでいない記事（原文と1文字ずつ照合していない）

Building effective agents、Harness design for long-running application development、Effective harnesses for long-running agents、Demystifying evals for AI agents、How we built our multi-agent research system、Effective context engineering for AI agents、Building a C compiler with a team of parallel Claudes、Code Review for Claude Code（blog）、How Anthropic secures its AI-native software development lifecycle（blog、2026-07-21）、Harnessing Claude's intelligence（blog）、Building agents with the Claude Agent SDK（blog）、Making frontier cybersecurity capabilities available to defenders、Finding bugs with Claude and property-based testing、Towards understanding sycophancy in language models、Fix software bugs faster with Claude（blog、2025-10-28）、Academy の2回分（Claude Code 101 の Code review、Claude Code in Action の GitHub Actions and Code Review）。

### 題名だけ見て開いていないもの

- anthropic.com/engineering の how-we-contain-claude、april-23-postmortem、managed-agents、claude-code-auto-mode、eval-awareness-browsecomp、infrastructure-noise、AI-resistant-technical-evaluations、advanced-tool-use、code-execution-with-mcp、claude-code-sandboxing、equipping-agents-for-the-real-world-with-agent-skills、a-postmortem-of-three-recent-issues、writing-tools-for-agents、desktop-extensions、claude-think-tool、swe-bench-sonnet、contextual-retrieval。
- resources.anthropic.com の PDF 3本（Building Effective AI Agents: Architecture Patterns and Implementation Frameworks / Scaling Agentic Coding Across Your Organization / 2026 Agentic Coding Trends Report）。
- support.claude.com の「Set up Code Review for Claude Code」。
- 取得したが中身を読んでいない定義ファイル。pr-review-toolkit の comment-analyzer / pr-test-analyzer / type-design-analyzer、skill-creator の comparator と analyzer（grep だけ）。claude-code-action の他の reviewer 定義4本は取得していない。
