# 業界のツール・技術記事・実務家のブログにみる、レビューの周回を少なく収める手法

調べた日: 2026-09-15。
担当範囲: AI コードレビューを提供する企業・ツールの公式文書と技術記事、実務家のブログ、公開の議論。
**Anthropic の公式情報と学術論文は別の worker の担当なので、題名だけを末尾に挙げる。**

**各節の「このリポジトリへの当てはめ」は案であって、決定ではない。**

---

## 読んだうえで答える4つの問い

### 1. この作業は、どの規則に当てはまるか

| 規則 | この作業でどう当てたか |
| --- | --- |
| [.claude/skills/worker-briefing/SKILL.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md) の 2-4（指示に書かれたことだけで判断しない） | 当てはめ先の規則を読んだ。既存の規則に同じ手法が書かれていないかを語で数えた（下の問い4） |
| 同じファイルの 2-5（同じものが他に無いかを数える）（[L183](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L183)） | 直す指摘を出す作業ではない。**「見つからない」「書かれていない」と書く箇所に、検索語と探した場所を添える形で当てた** |
| 同じファイルの 2-2 と [.claude/rules/reporting.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/reporting.md) | 製品の紹介を書かない。記号を振らず、手法には内容を表す名前を付ける。英語の技術用語を直訳しない |
| reporting.md の「worker への指示に必ず入れること（根拠の付け方）」 | 主張ごとに URL と引用を添えた。引用の確度を下の「引用の確度」で3段に分けた |
| [CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) の「公開してよい情報かを常に判断する」 | 下の問い3 |
| 呼ぶ側の禁止事項 | 書いたのはこのファイルだけ。git と gh は読み取りだけ（`gh api` の GET）。worker を立てていない。`claude -p` を叩いていない |

**2-6（1回で全部挙げる）と 2-7（合理的根拠を書く）は、レビューを頼まれた worker への規則なので、この作業には当てていない。**

### 2. 飛ばしてよい段はあるか

**飛ばした段は無い。**
[.claude/rules/design-review.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md) の9段（設計→設計レビュー→実装→PR→`/code-review`）は issue の作業の段である。
**この作業は設計も実装も PR の作成もしないので、そもそも9段に入らない。**
同じ文書の「飛ばしてよい場合」（文書だけの変更）を根拠にしたのではない。

### 3. 公開してよくない情報を書きうる場面

**このファイルは PUBLIC リポジトリの `docs/plans/` の下に置かれる。**

| 書きうる場面 | どうしたか |
| --- | --- |
| 呼ぶ側が渡した成果物のパスに、個人のホームディレクトリの絶対パスが入っている | **リポジトリ内の相対リンクだけで書いた** |
| 同じ `research/` の下に、別の worker の作業用ディレクトリがあった | **中身を開いていない。このファイルにも書いていない** |
| 第三者の GitHub や Hacker News の投稿者名 | 公開情報だが本題に要らないので、出典の URL だけにした |
| 外部の記事の引用 | 短く引き、必ず出典を添えた |

### 4. 作業に効く場所をどう探し、何を読んだか

**当てはめ先の規則を探した。**working tree（HEAD `df36f9d7`）の `.claude/` と `CLAUDE.md` を `git grep -n -F` で数えた。

| 検索語 | 件数 | 何を確かめたか |
| --- | --- | --- |
| `前の周の対応表` | 4 | 否定した指摘を次の周へ渡す仕組みは既にある（[design-review.md:143](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L143)） |
| `確信度` / `多数決` / `差分だけ` / `P0` | 0 / 0 / 0 / 0 | 下の手法のうち、確信度・多数決・差分レビュー・優先度の印は、既存の規則に書かれていない |
| `effort` / `ultra` / `--fix` | 2 / 1 / 1 | すべて [pr-review-and-merge/SKILL.md:99-131](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L99-L131) |
| `最大1周` / `1回で全部挙げる` | 5 / 4 | 2026-09-05 と 2026-09-04 の方向調整が規則に入っていることを確かめた |

**読んだ規則。**[CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) の「コードレビュー記録フロー」（[L504](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L504)）から「3回で通らなかったとき」（[L731](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md)）まで。
[design-review.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md) 全体。worker-briefing 全体。
[pr-review-and-merge/SKILL.md:80-159](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L80-L159)。

**外部。**WebSearch で各社・各実務家の名前と「false positives」「nits」「resolution rate」「incremental review」「learnings」「non-converging review loop」などを組み合わせて探し、見つけたページを WebFetch で開いた。
OpenAI Codex のリポジトリは `gh api` で読み取りだけ行った。**開けなかったものは末尾の「開けなかった URL」にある。**

---

## 引用の確度

**同じ「引用」でも、取り方で確度が違う。各引用にどれかを添えた。**

| 印 | 取り方 | 一字一句の一致 |
| --- | --- | --- |
| **原文（gh api）** | `gh api repos/<owner>/<repo>/contents/<path>` の本文を復号した | **一致する** |
| **抽出（WebFetch）** | ページを開いたが、引用はツール内の要約モデルが抜き出した | **保証されない。**語句の細部・日付がずれうる。数値は抽出結果のまま |
| **未確認（検索要約のみ）** | 検索結果の要約だけで、ページを開いていない | **確かめていない** |

**一次情報**はその会社・その人が自分で出した文書、**二次情報**は他人による紹介・まとめとして区別した。
**ベンダー自身が自社製品について出した数値は、一次情報だが第三者の検証を経ていない。**そう書いた。

---

## 手法ごとの節

### 「作者が知ったら直すもの」だけを出し、該当するものは1件で止めずに出し切る判定基準（OpenAI Codex のレビュー用ルーブリック）

**何をするか。**「出すかどうか」の8条件と、「全部出し切れ」「該当が無ければ何も出すな」を同じ指示に置く。
優先度の印（P0〜P3）と、パッチ全体が正しいかの判定を必ず出させる。

**出典（一次・原文（gh api））。**
[openai/codex の codex-rs/prompts/templates/review/rubric.md](https://github.com/openai/codex/blob/main/codex-rs/prompts/templates/review/rubric.md)。
最終 commit は 2026-07-21（`81de4f251`）。
**第三者の gist が出典に書いていた旧いパス（`codex-rs/core/review_prompt.md`）は、main には無い**（`gh api repos/openai/codex/git/trees/main?recursive=1` を `review_prompt|review.*\.md$` で絞った。`truncated` は `false`）。

> 4. The bug was introduced in the commit (pre-existing bugs should not be flagged).
>
> （訳: その commit で持ち込まれたバグであること（既存のバグは指摘しない）。）

> 7. It is not enough to speculate that a change may disrupt another part of the codebase, to be considered a bug, one must identify the other parts of the code that are provably affected.
>
> （訳: 変更がコードベースの他の部分を壊すかもしれないと推測するだけでは足りない。バグとみなすには、**影響を受けることが証明できる他の箇所を特定しなければならない。**）

> Output all findings that the original author would fix if they knew about it. If there is no finding that a person would definitely love to see and fix, prefer outputting no findings. Do not stop at the first qualifying finding. Continue until you've listed every qualifying finding.
>
> （訳: 作者が知っていれば直すであろう指摘を全部出すこと。人が見て必ず直したいと思う指摘が無ければ、**何も出さないほうを選ぶ。****該当する最初の指摘で止まらず、該当するものを全部挙げるまで続ける。**）

> Correct implies that existing code and tests will not break, and the patch is free of bugs and other blocking issues. Ignore non-blocking issues such as style, formatting, typos, documentation, and other nits.
>
> （訳: 「正しい」とは、既存のコードとテストが壊れず、バグやその他の blocking な問題が無いことを指す。**スタイル・書式・typo・文書・その他の nit のような blocking でない問題は、この判定では無視する。**）

ほかに、指摘ごとの `confidence_score`（0.0〜1.0）と、「変更箇所と、欠陥または直し方で重複を除く」（`deduplicate findings by changed location and defect/remedy`）がある。

**GitHub 上のレビューでは P0 と P1 だけを出す**（一次・抽出（WebFetch）。[Codex の GitHub 連携の文書](https://learn.chatgpt.com/docs/third-party/github)。`developers.openai.com/codex/integrations/github` からの転送先。日付の表示は無い）。

> In GitHub, Codex flags only P0 and P1 issues so review comments stay focused on high-priority risks.
>
> （訳: GitHub では、Codex は P0 と P1 の問題だけを指摘する。レビューのコメントを優先度の高いリスクに絞るためである。）

**効果の実測。**示されていない。

**効く観点。**レビュワー側（出し切りと足切りを同じ指示に置く）。ループの制御（パッチ全体の判定が、止めてよいかの材料になる）。

**制約に当たるか。**当たらない。**テキストの指示なので、Agent で立てるレビュワーのプロンプトへそのまま再現できる。**
`/code-review` には渡せない（[pr-review-and-merge/SKILL.md:99](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L99)）。
**受け取る側が対応表で「直さない」を判定するときの基準としてなら、`/code-review` の出力にも当てられる。**

**このリポジトリへの当てはめ（案）。**
- [worker-briefing の 2-6](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L256) に、条件4・6・7・8（持ち込まれたものだけ・推測に頼らない・影響箇所を特定する・意図した変更は出さない）を足す
- [CLAUDE.md の「収まっている」](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L566) の定義に、「blocking な問題が無い」の判定をレベル名と並べて置く
- **注意。**条件4の「既存のバグ」は「その commit より前からコードベースに在ったもの」である。2-6 の「前の周に既に在ったもの」（前のレビューの周の時点で在ったもの）とは別物である

---

### 観点ごとにサブエージェントを1つずつ立て、各観点に「1件見つけても止めるな」と書く（OpenAI Codex リポジトリの code-review スキル群）

**何をするか。**観点（破壊的変更・変更の大きさ・テスト・文脈の上限）ごとに短いスキルを1つ置き、まとめ役がスキル1つにつきサブエージェント1つを立てる。
**全サブエージェントの指摘を1件も捨てずに返させる。**

**出典（一次・原文（gh api））。**OpenAI が自分のリポジトリで使っているスキルで、製品の文書ではない。

| ファイル | 最終 commit |
| --- | --- |
| [.codex/skills/code-review/SKILL.md](https://github.com/openai/codex/blob/main/.codex/skills/code-review/SKILL.md) | 2026-06-26（`ac85409b7`） |
| [.codex/skills/code-review-breaking-changes/SKILL.md](https://github.com/openai/codex/blob/main/.codex/skills/code-review-breaking-changes/SKILL.md) | 2026-04-20（`513dc2871`） |
| [codex-rs/skills/src/assets/samples/review-agent/SKILL.md](https://github.com/openai/codex/blob/main/codex-rs/skills/src/assets/samples/review-agent/SKILL.md) | 2026-07-14（`83a418783`） |

> Use subagents to review code using all code-review-* skills other than this orchestrator. One subagent per skill. Pass full skill path to subagents. Use xhigh reasoning.
>
> You must return every single issue from every subagent. You can return an unlimited number of findings.
>
> （訳: このまとめ役以外の code-review-* スキルを全部使い、サブエージェントでコードをレビューする。**スキル1つにつきサブエージェント1つ。**スキルのフルパスをサブエージェントへ渡す。推論は xhigh を使う。
> **全サブエージェントの指摘を1件残らず返すこと。**件数に上限は無い。）

> Do not stop after finding one issue; analyze all possible ways breaking changes can happen.
>
> （訳: **1件見つけたところで止めない。**破壊的変更が起きうる経路を全部分析する。）

> 3. Identify concrete regressions introduced by the change. Continue through the whole diff after finding the first issue.
> 4. Check the relevant tests and call sites to confirm that each finding is real and actionable.
>
> （訳: 3. 変更が持ち込んだ具体的な退行を特定する。**最初の問題を見つけたあとも、diff の最後まで読み続ける。**
> 4. **関係するテストと呼び出し元を確かめ、各指摘が実在し、手を打てるものであることを確認する。**）

> If there are no qualifying findings, say `No findings.` Do not invent a finding to fill the result.
>
> （訳: 該当する指摘が無ければ `No findings.` と書く。**結果を埋めるために指摘をでっち上げない。**）

**効果の実測。**示されていない。

**効く観点。**レビュワー側（1回で漏れなく挙げる）。

**制約に当たるか。**当たらない。**Agent ツールで、観点ごとのレビュワーを並列に立てれば再現できる。**
レートリミットの消費は観点の数だけ増えるはずだが、**測っていない。**

**このリポジトリへの当てはめ（案）。**
- [design-review.md の「渡す4つの観点」](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L101) を、観点ごとに別のレビュワーへ分けて渡す
- このリポジトリで繰り返し出る不変条件（[CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) の hook の挙動の4つの定義、設計文書の行番号リンクのずれ）を、観点ごとの短いファイルにする

---

### 専門を分けたレビュワーそれぞれに「指摘しないもの」を明示する

**何をするか。**1つの万能レビュワーをやめて観点ごとに分け、**各々に「何を見るか」と同じ重さで「何を見ないか」を書く。**
まとめ役が重複を除き、重大度を付け直す。

**出典。**

| 出典 | 印 | 要点 |
| --- | --- | --- |
| [Orchestrating AI Code Review at scale（Cloudflare, Ryan Skidmore, 2026-04-20）](https://blog.cloudflare.com/ai-code-review/) | 一次・抽出（WebFetch） | 最大7つの専門レビュワーとまとめ役。セキュリティ担当は `Theoretical risks that require unlikely preconditions`（起こりにくい前提が要る理論上のリスク）を無視すると明記 |
| [Learnings from building AI agents（cubic, Paul Sanglé-Ferrière, 2025-06-19）](https://www.cubic.dev/blog/learnings-from-building-ai-agents) | 一次（ベンダー）・抽出（WebFetch） | 1つの大きなプロンプトを Planner / Security / Duplication / Editorial の小さなエージェントへ分けた。判定の前に理由を書かせた。道具を減らした |
| [uReview（Uber, 2025-08-12）](https://www.uber.com/us/en/blog/ureview/) | 一次・抽出（WebFetch） | 開発者の評価が低かった分類を、分類器で丸ごと出さない |
| [Automated Code Review（Factory の文書）](https://docs.factory.ai/guides/droid-exec/code-review) | 一次・抽出（WebFetch） | `skips stylistic concerns, minor optimizations, and architectural opinions`（スタイル・細かな最適化・設計上の意見は扱わない）。カスタマイズ例に `Submit at most 5 comments total, prioritizing the most critical issues`（コメントは合計5件まで。最も重大なものを優先） |
| [How we built a high-quality AI code review agent（Augment Code, Akshay Utture, 2026-03-10）](https://www.augmentcode.com/blog/how-we-built-high-quality-ai-code-review-agent) | 一次（ベンダー）・抽出（WebFetch） | システムプロンプトで避ける分類（スタイル・nit）を指定する |

> telling an LLM what **not** to do is where the actual prompt engineering value resides
>
> （訳: **LLM に「何をしないか」を伝えるところに、プロンプトを作り込む本当の価値がある。**）— Cloudflare

> Readability nits, minor logging tweaks, low-impact performance optimizations, and stylistic issues consistently received poor ratings.
>
> （訳: 読みやすさの nit、ログの細かな手直し、影響の小さい性能の最適化、スタイルの問題は、**一貫して低い評価を受けた。**）— Uber

**効果の実測。**

| 出典 | 数値 | 条件 |
| --- | --- | --- |
| cubic | **偽陽性が51%減、PR あたりのコメント数の中央値が半分** | 6週間。ベンダーの自己報告で、測り方の詳細は書かれていない |
| Cloudflare | 30日で131,246回のレビュー、1回あたり約1.2件の指摘、人間の「break glass」上書きは288件（0.6%） | **量の数値であって、指摘が直されたかの数値ではない** |
| Uber | 下の「候補を出してから1件ずつ検証する段を置く」にまとめた | — |

**否定的な知見（同じ節の中で読む）。**cubic は `longer prompts, adjusting the model's temperature, experimenting with sampling`（プロンプトを長くする・temperature を変える・サンプリングを試す）では**ほとんど改善しなかった**と書いている。
Greptile は、1つのレビュワーへ「nit を減らせ」と指示しても、重要な指摘まで減ったと書いている（下の「否定的な知見」）。
**観点を分けたうえで「見ないもの」を書く形と、1つのレビュワーへ「細かく言うな」と書く形を、同じ条件で比べた実測は見つからなかった。**

**効く観点。**レビュワー側（ノイズを減らす）。

**制約に当たるか。**当たらない。Agent ツールで再現できる。

**このリポジトリへの当てはめ（案）。**
- 規則文書のレビューで Medium と Low に落ちる型（言い回し・強調・表の列の数）を、観点レビュワーへ「見ないもの」として渡す
- **ただし人間は「一回で全部漏らさず指摘する」を求めている**（2026-09-04）。**「見ないもの」は、Critical と High に当たりえない型だけに限る必要がある**

---

### 並列の複数パスを束ね、多数決と検証モデルで偽陽性を落とす（Cursor Bugbot）

**何をするか。**同じ diff を、**順序を変えて**8本並列に読ませる。似た指摘を1つの束にまとめ、**1本のパスでしか出なかった指摘を多数決で落とす。**
検証モデルに通し、要らない分類（コンパイラの警告・文書の誤り）を落とし、前回の実行で出した指摘との重複を除く。
**のちに、決まった段を踏む作りをやめ、エージェントが自分で深掘りする場所を決める作りへ移った。いちばん効いたのはこの移行だった。**

**出典（一次（ベンダー）・抽出（WebFetch））。**[Building a better Bugbot（Cursor, Jon Kaplan, 2026-01-15）](https://cursor.com/blog/building-bugbot)。

> running multiple bug-finding passes in parallel and combining their results with majority voting
>
> （訳: バグを探すパスを複数並列に走らせ、**その結果を多数決で束ねる**。）— 最も効いた品質改善の1つとして挙げている

> shifted to aggressive prompts that encouraged the agent to investigate every suspicious pattern
>
> （訳: **疑わしいパターンを全部調べるよう促す、攻めたプロンプトへ切り替えた。**）— エージェント型へ移ったあと

**効果の実測。**

| 数値 | 条件 |
| --- | --- |
| **解決率（resolution rate）52% → 70% 超** | 2025年7月の版1から2026年1月の版11まで。主要な実験40回 |
| 1回の実行あたりの指摘 0.4 → 0.7 件 | 同上 |
| PR あたりの解決された指摘 約0.2 → 約0.5 件 | 同上 |

**解決率の定義**は下の「作者が実際に直した割合を測って、打ち手を選ぶ」にある。

**同じ系統の実測。**

| 出典 | 印 | 数値 |
| --- | --- | --- |
| [Auto-resolution and analysis updates in Copilot code review（GitHub Changelog, 2026-09-11）](https://github.blog/changelog/2026-09-11-auto-resolution-and-analysis-updates-in-copilot-code-review/) | 一次・抽出（WebFetch） | Lite の effort level を複数エージェントの合議に変えた。**1レビューあたりの対応されたコメントが High で47%、Medium で31%、Low で11%増え、コストは約8%減った** |
| [Greptile v4 + New Pricing（2026-03-05）](https://www.greptile.com/blog/greptile-v4) | 一次（ベンダー）・抽出（WebFetch） | PR あたりの対応されたコメント 0.92 → 1.60、対応された割合 30% → 43%（数十万件の PR）。**作りの変更は書かれていない**（下の「探したが見つからなかったもの」） |
| [Introducing Qodo 2.0（2026-02-04）](https://www.qodo.ai/blog/introducing-qodo-2-0-agentic-code-review/) | 一次（ベンダー）・抽出（WebFetch） | 専門エージェントの結果を judge エージェントが「衝突を解き、重複を除き、信号の弱いものを落とす」。自社ベンチマークで F1 60.1% |

**効く観点。**レビュワー側（網羅と精度の両立）。

**制約に当たるか。**当たらない。**Agent ツールで同じレビュワーを N 体並列に立て、指摘を束ねれば再現できる。**
`/code-review` 自体は1回の実行である。`ultra` の effort level はクラウドで多数のエージェントを走らせるが、
**このリポジトリは人間が明示したときだけ使うと決めている**（[pr-review-and-merge/SKILL.md:130-131](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L130-L131)）。

**このリポジトリへの当てはめ（案）と、注意。**
- 設計レビューを、同じ入力で `maimuzo-from-ecc:architect` 2〜3体へ並列に投げ、読む順序を変える。指摘を束ねてから対応表を書く
- **多数決は「1本でしか出なかった指摘」を捨てるので、網羅を下げうる。**人間の「1回で全部」に合わせるなら、**和集合を取ってから、下の検証の段で落とすほうが向きが合う。これは推論で、実測は無い**

---

### 候補を出してから、1件ずつ別の役に検証させる段を置く

**何をするか。**生成役は候補を出すだけにする。**候補1件ごとに検証役を立て、根拠を取りに行かせてから出すかを決める。**確信度に閾値を置く。

**出典。**

| 出典 | 印 | 要点 |
| --- | --- | --- |
| [Building a code review system that uses prod data to predict bugs（Sentry, 2025-12-18）](https://blog.sentry.io/building-a-code-review-system-that-uses-prod-data-to-predict-bugs) | 一次（ベンダー）・抽出（WebFetch） | 下書き役は `at most 3 bug hypotheses`（仮説は3つまで）。仮説1つに検証役1つ。重大度0〜1で絞り、過去に低評価された指摘との類似度でも落とす |
| [uReview（Uber, 2025-08-12）](https://www.uber.com/us/en/blog/ureview/) | 一次・抽出（WebFetch） | 別のプロンプトが各コメントを採点し確信度を付ける。閾値を「アシスタント・言語・コメントの分類ごと」に置く |
| Codex の review-agent スキル（上の節） | 一次・原文（gh api） | 関係するテストと呼び出し元で、指摘が実在するかを確かめる |
| Qodo PR-Agent の self-reflection | **未確認（検索要約のみ）** | 提案を0〜10で自己採点し、0を落とす。閾値を8より上にしないよう勧めている、という要約。文書の現行ページには記述が無かった（下の「開けなかった URL」） |

> By focusing the verifying agents into a single hypothesis they can deep dive and more correctly assert if that is a valid bug or not.
>
> （訳: **検証役を仮説1つに集中させることで、深く掘り、それが本当にバグかどうかをより正しく判定できる。**）— Sentry

**効果の実測。**

| 出典 | 数値 | 条件 |
| --- | --- | --- |
| Uber | **有用と評価されたコメント75%、同じ変更の中で対応されたコメント65%（人間が書いたコメントは51%）** | 週およそ65,000件の diff の9割以上。「対応された」は、再実行5回で似たコメントが再現しなければ対応済みとみなす |
| Sentry | **精度の数値は出していない** | 評価の実行ごとに precision・recall を測っている、とだけ書いている |

**検証の段だけを外したときとの比較は、どの出典にも無い。**

**否定的な知見。**Greptile は、LLM に自分の出したコメントの重大度を判定させる方法を試し、`the LLMs judgment of its own output was nearly random`（**自分の出力への LLM の判定は、ほぼでたらめだった**）と書いている（下の「否定的な知見」）。
Uber は生成役と採点役に別のモデルを使った、と検索結果の要約にあるが、**本文では確かめていない。**

**効く観点。**レビュワー側（偽陽性を落とす）。書く側（直さなくてよいものに手を取られない）。

**制約に当たるか。**当たらない。Agent ツールで、指摘1件と根拠を検証役へ渡せば再現できる。

**このリポジトリへの当てはめ（案）。**
- 対応表を書く前に、Critical と High の指摘1件ごとに「その経路・その食い違いを、コードか文書から示せるか」を別コンテキストの検証役に確かめさせる
- [design-review.md の「合理的根拠を否定できるなら、直さない」](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L124) の判定を、直す本人ではなく検証役へ移す
- Greptile の否定的な知見に合わせるなら、**検証役に問うのは「重大度はいくつか」より「その経路は実在するか」のほうが材料に合う。これは推論である**

---

### 「止めてよい状態」を blocking の中身の列挙で固定し、周回が続いたら新しい軽い指摘を抑える

**何をするか。**「何が起きたら blocking か」を具体的に列挙し、**blocking が0件なら通す。**nit は1回だけ出し、nit のためだけに直しの周を回さない。
**周回が続いたら、新しく出た軽い指摘を抑え、「収まっていない」こと自体を指摘として出す。**

**出典（いずれも一次の書き込み。1件ずつの観測）。**

| 出典 | 印 | 要点 |
| --- | --- | --- |
| [meridianlabs-ai/agents の PR #58（Review loop passes on no blocking findings; nits no longer spend fix rounds, 2026-09-04）](https://github.com/meridianlabs-ai/agents/pull/58) | 抽出（WebFetch） | blocking の列挙と「blocking が0件なら通す」。周の上限10は据え置き |
| [fullsend-ai/agents の issue #1294（Review agent should detect non-converging review loops and suppress low-severity new findings, 2026-09-15）](https://github.com/fullsend-ai/agents/issues/1294) | 抽出（WebFetch） | 11周連続で変更要求。18件（Low 14・Medium 4）の分類が周ごとに移り、**人間が PR を閉じた原因の設計の問題は1度も指摘されなかった** |
| [When AI Reviewers Cannot Agree（Contrast Security, Shane Schisler, 2026-07-20）](https://www.contrastsecurity.com/security-influencers/when-ai-reviewers-cannot-agree) | 抽出（WebFetch） | 4つのレビュワーで3周・約30分。**指摘は減らず増え、37件（blocking 7件）が未解決で終わった** |
| [Review-Then-Implement Loop（agentpatterns.ai, 2026-06-13 見直し）](https://agentpatterns.ai/code-review/review-then-implement-loop/) | **二次**・抽出（WebFetch） | 自動の直しは1回まで。直しで新しい問題が出たらループせず人間へ回す |
| [Bugbot の文書（Cursor）](https://cursor.com/docs/bugbot) | 一次・抽出（WebFetch） | 自動修正は `max 3 attempts per PR to prevent loops`（ループを防ぐため PR あたり3回まで） |
| [The Standard of Code Review（Google eng-practices）](https://google.github.io/eng-practices/review/reviewer/standard.html) | 一次・抽出（WebFetch）。日付の表示は無い | 完璧でなくても、コードの健全性を確実に上げるなら承認する |

> A finding blocks only if it is a regression on a mainline path, a weakened security boundary, data loss or corruption, or a claim in the PR description the code does not deliver.
>
> （訳: **指摘が blocking になるのは、主要な経路の退行・弱まったセキュリティの境界・データの消失や破損・PR の説明に書いたのにコードが果たしていない主張、のどれかの場合だけである。**）— PR #58

> (2) suppress new low-severity findings that weren't present in prior rounds (only surface medium+ findings)
>
> （訳: **前の周に無かった新しい Low の指摘を抑える**（Medium 以上だけを出す）。）— issue #1294 の提案（4周を超え、新しい指摘がほぼ Low のとき）

> Require human escalation on non-convergence
>
> （訳: **収まらないときは人間へ上げることを必須にする。**）— Contrast Security。blocking の件数が厳密に減らないことを、収まらない兆候としている

> In general, reviewers should favor approving a CL once it is in a state where it definitely improves the overall code health of the system being worked on, even if the CL isn't perfect.
>
> （訳: 一般に、レビュワーは、**変更が完璧でなくても、システム全体のコードの健全性を確実に上げる状態になったら承認する側に傾くべきである。**）— Google

**二次情報。**[Multi-Model AI Code Review: Convergence Loops（Zylos Research, 2026-03-01）](https://zylos.ai/research/2026-03-01-multi-model-ai-code-review-convergence/)（抽出（WebFetch））が、対話での利用は3〜5周、CI では1〜2周を上限に、収まらなければ人間へ上げる、とまとめている。
**レビュー役は指摘だけ、直す権限は書く側だけに置くと振り子を防げる**とも書く。**引用している論文は開いていない。**

**効果の実測。****抑えたあとの効果の実測は無い。**issue #1294 は提案で、実装されたかは書かれていない。Contrast と issue #1294 は、収まらなかった観測である。

**効く観点。**ループの制御。

**制約に当たるか。**当たらない。規則の文面だけで再現できる。

**このリポジトリへの当てはめ（案）。**
- [CLAUDE.md の「対応表の列」](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L534) はレベル名（Critical / High / Medium / Low / Info）を並べている。**何を Critical や High とするかの基準を、この表の中には見つけていない。他の場所に在るかは、この調査では探していない。**PR #58 のような「blocking の中身の列挙」を置く
- 3・6・9回目の段（[CLAUDE.md:561](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L561) の「回数を数える」）に、issue #1294 の「前の周に無かった新しい Low を抑える」と、Contrast の「blocking の件数が厳密に減っていなければ収まらない兆候とみなす」を並べる

---

### 再レビューの範囲を「前回レビューした時点からの差分」に限り、前回の指摘が解決したかを確かめる

**何をするか。**2周目以降は、前回レビューした commit から後の変更だけを読む。前回の指摘は、直した commit が入ったら解決済みにする。

**出典。**

| 出典 | 印 | 要点 |
| --- | --- | --- |
| [Bugbot の文書（Cursor）](https://cursor.com/docs/bugbot) | 一次・抽出（WebFetch） | `By default, Bugbot reviews only the changes since the previous Bugbot review.`（既定では、前回の Bugbot のレビュー以降の変更だけをレビューする）。PR ごとに1回だけ走らせる設定もある |
| [auto-review の設定（CodeRabbit の文書）](https://docs.coderabbit.ai/configuration/auto-review) | 一次・抽出（WebFetch） | `auto_incremental_review` は既定で有効。前回のレビュー以降に足された commit に絞る。`@coderabbitai full review` で最初から読み直す |
| [Copilot code review の文書（GitHub Docs）](https://docs.github.com/en/copilot/how-tos/copilot-on-github/use-copilot-agents/copilot-code-review) | 一次・抽出（WebFetch） | 既定では push で再レビューしない。下の引用 |
| GitHub Changelog 2026-09-11（上の並列パスの節） | 一次・抽出（WebFetch） | `When you push a commit that addresses a Copilot code review comment, Copilot now resolves that comment during its rereview.`（コメントに対応した commit を push すると、再レビューの中でそのコメントを解決済みにする） |
| Cloudflare（上の専門レビュワーの節） | 一次・抽出（WebFetch） | 更新された PR では前回のレビューコメントの全文を入力に渡し、利用者が「won't fix」と返した指摘を解決済みとして扱う |

> When re-reviewing, Copilot may repeat previous comments, even if you resolved or downvoted them.
>
> （訳: **再レビューのとき、Copilot は、解決済みにしたり低評価を付けたりしたコメントでも、繰り返すことがある。**）— GitHub Docs

**否定的な知見。**[GitHub Community の Discussion #189767（Copilot Code Review generates new comments on every push, creating an endless fix-push-review loop, 2026-03-16）](https://github.com/orgs/community/discussions/189767)（一次の書き込み・抽出（WebFetch））。
500〜1000行の PR で5周、新しいコメントが10・6・4・2・2件出た。**前の diff に既に在ったのに指摘されなかったコードへの新しいコメントだった。**約24件のうち役に立ったのは約3件だった。

> If Copilot can find an issue on round 3, it should find it on round 1.
>
> （訳: **3周目で見つけられる問題なら、1周目で見つけられるはずだ。**）

**効果の実測。****差分レビューで周回が減ったという実測は見つからなかった。**

**効く観点。**ループの制御。

**制約に当たるか。**一部当たる。`/code-review` は PR 番号・branch・path を受け取る（[pr-review-and-merge/SKILL.md:99](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L99)）。
**commit の範囲を渡せるかは確かめていない。**Agent で立てるレビュワーなら、`git diff <前回レビューした commit>..HEAD` と前の周の対応表を渡せる。

**このリポジトリへの当てはめ（案）と、判断が要る点。**
- 1周目だけ全体を読ませ、2周目以降は「直しの commit の差分」と「前の周に直すと決めた指摘が解決したか」に限る
- **この形にすると、2-6 が数えている「前の周に既に在ったのに、いま初めて出た誤り」は、2周目以降に出なくなる。**1周目の網羅に全部を賭けることになる。**受け入れるかは人間の判断が要る**

---

### 否定した指摘を記録し、次のレビューの入力にして再び出させない

**何をするか。**「この指摘は当てはまらない。理由は〜」を記録し、**次のレビューのたびに読み込ませる。**PR をまたいで貯めるものと、1つの PR の周をまたいで渡すものがある。

**出典。**

| 出典 | 印 | 要点 |
| --- | --- | --- |
| [CodeRabbit learnings（CodeRabbit の文書）](https://docs.coderabbit.ai/knowledge-base/learnings) | 一次・抽出（WebFetch） | コメントへ「なぜ当てはまらないか」を返すと learning として保存され、次からコメントを出す前に読み込まれる。`explain the why, not just the what`（何かだけでなく、なぜかを説明する） |
| [How to Make LLMs Shut Up（Greptile, Daksh Gupta, 2024-12-18）](https://www.greptile.com/blog/make-llms-shut-up) | 一次（ベンダー）・抽出（WebFetch） | チームごとに、低評価されたコメントの埋め込みを貯める。**低評価された3件以上と似ていたら出さない** |
| Sentry（上の検証の節） | 一次（ベンダー）・抽出（WebFetch） | 過去に低評価された提案との類似度で落とす |
| [daniel-ospina/agent-infra の issue #665（review-loop hard cap is per-reviewer but every cycle uses a fresh reviewer — the loop never caps, 2026-09-10）](https://github.com/daniel-ospina/agent-infra/issues/665) | 一次の書き込み・抽出（WebFetch） | 毎周まっさらなレビュワーを立てたので、8〜10周目に受け入れ済みの境界を毎回掘り直した。受け入れ済みの一覧を毎回レビュワーの入力に入れる提案。**`not planned` で閉じられた** |
| [GitHub Community の Discussion #190754（Re-reviews ignore prior conversation and repeat the same incorrect suggestions, 2026-03-27）](https://github.com/orgs/community/discussions/190754) | 一次の書き込み・抽出（WebFetch） | 作者が理由を返したのに、同じ誤った提案が4周続いた |
| [Custom Code Review rules for Codex（OpenAI Developers）](https://developers.openai.com/blog/custom-code-review-rules-for-codex) | 一次・抽出（WebFetch）。**日付と著者は表示されていない** | リポジトリの規則ファイルに、レビュワーが毎回説明している不変条件を書く |
| [Feedback flywheel（Thoughtworks Technology Radar Vol.34, 2026-04, Assess）](https://www.thoughtworks.com/radar/techniques/feedback-flywheel) | 一次・抽出（WebFetch） | セッションの成功と失敗を振り返り、共有の指示と検査へ戻す |

> Every time CodeRabbit prepares to add a comment to a pull request or issue, it loads the learnings that apply based on your configured scope.
>
> （訳: **CodeRabbit は、pull request や issue にコメントを足そうとするたびに、設定した範囲に当てはまる learning を読み込む。**）

> In the primary suite, rule-guided variants recovered 98% of the required custom findings, compared with 58.3% in the baseline control.
>
> （訳: 主な評価セットでは、**規則を与えた版は、出すべき独自の指摘の98%を拾った。規則を与えない対照は58.3%だった。**）— OpenAI。規則の数・PR の数・「出すべき独自の指摘」の定義は書かれていない

> Keep formatting and other mechanical checks in CI. Save repository rules for the questions a reviewer would otherwise have to ask again.
>
> （訳: **書式などの機械的な検査は CI に置く。リポジトリの規則は、書いておかないとレビュワーがまた訊くことになる問いのために取っておく。**）— OpenAI

**効果の実測。**

| 出典 | 数値 | 条件 |
| --- | --- | --- |
| Greptile | **対応された割合 19% → 55%超** | 機能を出してから2週間、既存の利用者 |
| OpenAI | 規則ありで98%、規則なしで58.3% | 上の引用のとおり。条件の詳細は書かれていない |

**効く観点。**ループの制御（同じ指摘が戻ってこない）。レビュワー側（リポジトリ固有の不変条件を拾う）。

**制約に当たるか。**一部当たる。`/code-review` へは渡せない。
**`/code-review` が CLAUDE.md などリポジトリの規則ファイルを読むかは、確かめていない。**
Agent で立てるレビュワーには渡せる。**1つの PR の中では、[design-review.md:143](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L143) が既に「次の周のレビュワーには、前の周の対応表を渡す」と決めている。**

**このリポジトリへの当てはめ（案）。**
- **PR をまたいで**繰り返し否定した指摘の型を、リポジトリ内の1ファイルへ貯め、Agent で立てるレビュワーの入力にする（いまは1つの PR の中だけで渡している）
- issue #665 の失敗は、このリポジトリの「毎周まっさらな Agent を立てる」形にも起きうる。**前の周の対応表を渡す決まりが、それを防ぐ役を担っている**

---

### 同じ種類の問題は1件にまとめて全出現箇所を並べ、修正案の文面をレビュワーに書かせない（修正漏れと、直しが次の指摘を生む連鎖への対策）

**何をするか。**2つある。

1. **同じ型の問題が複数箇所にあるなら、1件の指摘にまとめて全箇所を並べる。**直す側は、その一覧を全部直す
2. **レビュワーに「こう書き換えよ」という文面を書かせない。**書き換え案をそのまま採ると、次の周の指摘がその文面に落ちるためである

**出典。**

| 出典 | 印 | 要点 |
| --- | --- | --- |
| [Copilot code review: Comment experience improvements（GitHub Changelog, 2026-05-12）](https://github.blog/changelog/2026-05-12-copilot-code-review-comment-experience-improvements/) | 一次・抽出（WebFetch） | 似たコメントを1つにまとめる。下の引用 |
| [60 million Copilot code reviews and counting（GitHub Blog, 2026-03-05）](https://github.blog/ai-and-ml/github-copilot/60-million-copilot-code-reviews-and-counting/) | 一次・抽出（WebFetch） | 繰り返し出るパターンの誤りを `clusters them into a single, cohesive unit`（1つのまとまりに束ねる）。**29%のレビューでは何もコメントしない。**1レビューあたり平均5.1件 |
| [Easily apply Copilot code review feedback with Copilot cloud agent（GitHub Changelog, 2026-05-19）](https://github.blog/changelog/2026-05-19-easily-apply-copilot-code-review-feedback-with-copilot-cloud-agent/) | 一次・抽出（WebFetch） | 「Fix batch with Copilot」で複数のコメントを1回の実行で直す |
| uReview（上の検証の節） | 一次・抽出（WebFetch） | 意味の近い提案を類似度でまとめる |
| Codex のルーブリック（上の節） | 一次・原文（gh api） | `Use one comment per distinct issue`（別々の問題ごとにコメント1つ）。**場所ごとの形で、Copilot のまとめる形とは向きが違う** |
| [bentleypark/aiwatch の issue #1298（Review loop keeps adopting agents' suggested rewrites — remove the artifact instead of policing the judgement, 2026-08-31）](https://github.com/bentleypark/aiwatch/issues/1298) | 一次の書き込み・抽出（WebFetch） | 9周収まらなかった。7周目の直し（docstring 3つ・ログの文言・テストの時計）に8周目の指摘が全部落ち、8周目の直しに9周目の指摘が全部落ちた |

> if Copilot has a suggestion for a better variable name for all of its occurrences in the pull request, Copilot will only point this out once.
>
> （訳: **pull request の中の全出現箇所に対して、よりよい変数名の提案があるなら、Copilot はそれを1回だけ指摘する。**）

> each round's findings landed on prose the previous round's fix had just written
>
> （訳: **各周の指摘は、前の周の直しがちょうど書いたばかりの文面に落ちた。**）— issue #1298

> If the reports carry no remedies, there is nothing to adopt. That is an input change, not an attempt to judge the operator's reasoning
>
> （訳: **レビューの報告に直し方が載っていなければ、採り入れるものが無い。**これは入力を変えることであって、作業者の判断を取り締まろうとすることではない。）— issue #1298

issue #1298 は、禁止を機械で止める作りを4通り試して採らなかった（守った作業まで止めるか、言い回ししか測れず中身を区別できなかった）。
**直し方を書かない「指摘だけ」のレビュー役を出した結果、1回の実行で4件の指摘・書き換え案0件だった。1回の観測である。**

**二次情報。**[The Never-Ending AI Code Review: Why One Pass Isn't Enough（DEV Community）](https://dev.to/brightgir/the-never-ending-ai-code-review-why-one-pass-isnt-enough-3k05)（抽出（WebFetch））が、モデルが1つ目のパスで null 安全の問題に引っかかると同種ばかり探し、競合状態のような別種を見落とす、というアンカリングを書いている。**出典として挙げている Semgrep の元の記事は開いていない。**

**効果の実測。**まとめる形の効果の数値は無い。issue #1298 は1回の観測。

**効く観点。**修正漏れ。ループの制御（直しが次の指摘を生む連鎖を断つ）。

**制約に当たるか。**一部当たる。`/code-review` の出力の形は変えられない。
Agent で立てるレビュワーには、まとめ方と「直し方を書くな」を指示できる。

**既にある規則との関係。**[worker-briefing 2-5 の段4](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L199) が既に「2件以上なら、全部を1つの指摘としてまとめる」を持っている。
`/code-review` の出力については、[pr-review-and-merge/SKILL.md:113-117](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L113-L117) が「受け取る側が数える」と決めている。

**このリポジトリへの当てはめ（案）。**
- 対応表の1行を「型」の単位にし、出現箇所の一覧を列として持たせる
- **Agent で立てるレビュワーには「何が誤りか・どこか・直さないと誰が何を失うか」だけを返させ、書き換えの文面を書かせない。**このリポジトリの日本語の長い規則文書では、直しの文面が次の周の指摘先になる形が、issue #1298 と同じ条件で起きうる

---

### 書く側: 決定的な検査（sensors）をレビューに出す前に通し、レビューを判断の要るものだけにする

**何をするか。**コンパイラ・linter・構造のテスト・テストスイートを、**エージェントが作業中に叩ける場所に置き、通ってからレビューへ出す。**繰り返し出る指摘の型は、専用の linter やテストにして機械へ移す。

**出典。**

| 出典 | 印 | 要点 |
| --- | --- | --- |
| [Feedback sensors for coding agents（Thoughtworks Technology Radar Vol.34, 2026-04, Trial）](https://www.thoughtworks.com/radar/techniques/feedback-sensors-for-coding-agents) | 一次・抽出（WebFetch） | 下の引用 |
| [Maintainability sensors for coding agents（martinfowler.com, Birgitta Böckeler, 2026-05-27）](https://martinfowler.com/articles/sensors-for-coding-agents.html) | 一次・抽出（WebFetch） | 独自の lint のメッセージに直し方の案内を書くと効いた。静的解析が「品質の錯覚」を生む危うさも書く |
| [Stop Sending IDE-Catchable AI Code Errors to Review（JetBrains Blog, Colette Des Georges, 2026-05）](https://blog.jetbrains.com/ai/2026/05/stop-sending-ide-catchable-ai-code-errors-to-review/) | 一次（ベンダー）・抽出（WebFetch） | `your reviewers' judgment is a finite resource`（レビュワーの判断力は有限の資源である）。AI のコードの幻覚の約20〜25%は静的解析で見つかる、という研究を引いている（**その研究は開いていない**） |
| [Augmented Coding: Beyond the Vibes（Kent Beck, 2025-06-25）](https://newsletter.kentbeck.com/p/augmented-coding-beyond-the-vibes) | 一次・抽出（WebFetch） | TDD を守らせるシステムプロンプト。軌道を外れた兆候として「ループ」「頼んでいない機能」「テストを無効化・削除する」を挙げる |
| [How far can we push AI autonomy in code generation?（martinfowler.com, Birgitta Böckeler, 2025-08-05）](https://martinfowler.com/articles/pushing-ai-autonomy.html) | 一次・抽出（WebFetch） | 下の引用 |

> run during the coding session and report clean results before a commit is made, rather than relying on post-commit checks
>
> （訳: **commit のあとの検査に頼るのではなく、コーディングの最中に走らせ、commit する前に通った結果を報告させる。**）— Thoughtworks

> claimed the build and tests were successful and moved on to the next step, even though they were not
>
> （訳: **ビルドとテストが通っていないのに、通ったと主張して次の段へ進んだ。**）— Böckeler（2025-08-05）

**効果の実測。****レビューの周回数への効果の実測は見つからなかった。**
Böckeler（2026-05-27）は、閾値を超えた循環的複雑度の出現が、直し方の案内を lint のメッセージへ足したあと減った、と観察として書いている。数値は無い。

**効く観点。**書く側。

**制約に当たるか。**当たらない。スクリプト・hooks・テストで再現できる。

**このリポジトリへの当てはめ（案）。**
- 過去の周で繰り返し出た指摘のうち、機械で判定できる型を、レビューに出す前の検査へ移す
- 既に同じ向きのものがある。[.claude/rules/plan-file.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/plan-file.md) の行番号リンクの検算、`.claude/hooks/` の3本の hook。[CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) の「マージの条件は、なるべく機械で判定する」も同じ向きである
- **過去の指摘のうち何割が機械で判定できる型かは、この調査では測っていない**

---

### 書く側: 意図・却下した案・証拠を、レビューに出す前に書いておく

**何をするか。**PR に「何をしようとしたか」「何を検討して採らなかったか」「動く証拠（テストの出力）」を先に書く。
**レビュワーは意図を再構成しなくて済み、意図した変更を誤りと読む偽陽性が減る、という主張である。**

**出典。**

| 出典 | 印 | 要点 |
| --- | --- | --- |
| [Agentic Code Review（Addy Osmani, 2026-06-15）](https://addyosmani.com/blog/agentic-code-review/) | 一次・抽出（WebFetch） | 下の引用 |
| [Code Review in the Age of AI（Addy Osmani, 2026-01-05）](https://addyo.substack.com/p/code-review-in-the-age-of-ai) | 一次・抽出（WebFetch） | 「PR Contract」: 意図を1〜2文・動く証拠・リスクの段階・人間に見てほしい1〜2箇所 |
| Codex のルーブリック（上の節）の条件6と8 | 一次・原文（gh api） | 作者の意図について明示されていない前提に頼る指摘、作者が意図した変更への指摘は出さない |
| Custom Code Review rules for Codex（上の節） | 一次・抽出（WebFetch） | `State the invariant and the safe path.`（不変条件と、安全な道筋を書く） |

> have the agent state what it was trying to do and what it ruled out, capture that as a decision log on the PR
>
> （訳: **エージェントに、何をしようとしたかと何を除外したかを述べさせ、それを PR の判断の記録として残す。**）

**効果の実測。**示されていない。

**効く観点。**書く側。レビュワー側（意図の誤読による偽陽性）。

**制約に当たるか。**一部当たる。**`/code-review` が PR の本文を読むかは確かめていない。**Agent で立てるレビュワーには渡せる。

**このリポジトリへの当てはめ（案）。**
- 設計のコメントと PR の本文に「採らなかった案とその理由」「守る不変条件」を必ず書き、レビュワーの入力に入れる
- [design-review.md の「合理的根拠を否定できるなら、直さない」](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L124) の判定材料にもなる

---

### 書く側: 変更を小さく保ち、段階に分けて出す

**何をするか。**1回に出す変更の量に上限を置き、超えるなら「最初に入れる最小のまとまり」を切り出す。

**出典。**

| 出典 | 印 | 要点 |
| --- | --- | --- |
| [openai/codex の .codex/skills/code-review-change-size/SKILL.md](https://github.com/openai/codex/blob/main/.codex/skills/code-review-change-size/SKILL.md) | 一次・原文（gh api） | 下の引用 |
| How far can we push AI autonomy in code generation?（上の節） | 一次・抽出（WebFetch） | エンティティ3〜5個なら動くアプリができた。10個にすると `a game of whac-a-mole`（もぐら叩き）になった |
| Augmented Coding: Beyond the Vibes（上の節） | 一次・抽出（WebFetch） | `plan.md` の次のテストを1つだけ実装させる |
| [My LLM codegen workflow atm（Simon Willison による紹介, 2025-02-21）](https://simonwillison.net/2025/Feb/21/my-llm-codegen-workflow-atm/) | **二次**・抽出（WebFetch） | Harper Reed が仕様を `Ask me one question at a time…`（1つずつ質問して）で固め、`spec.md` と `prompt_plan.md` と `todo.md` を作る。**元の記事は開けなかった。**「手順を小さく、強いテストで」という部分は検索要約にだけあり、**未確認** |

> Unless the change is mechanical the total number of changed lines should not exceed 800 lines.
> For complex logic changes the size should be under 500 lines.
>
> If the change is larger, explain whether it can be split into reviewable stages and identify the smallest coherent stage to land first.
>
> （訳: 機械的な変更でない限り、変更行数の合計は800行を超えないこと。複雑なロジックの変更なら500行未満にする。
> **それより大きいなら、レビューできる段階に分けられるかを説明し、最初に入れる最小のまとまりを特定する。**）

**効果の実測。**Böckeler の観察（作ったアプリの規模ごとの成否）だけで、周回数の数値は無い。

**効く観点。**書く側。

**制約に当たるか。**当たらない。

**このリポジトリへの当てはめ（案）。**
- 1つの PR の差分の行数に上限を置く
- **規則文書の PR では、行数より「触る規則ファイルの数」や「節の数」のほうが指摘の量に効くかもしれない。検討していない**

---

### 書く側: 仕様と受け入れ条件を先に固める（spec-driven）— 否定的な実測がある

**何をするか。**要件（受け入れ条件）→設計→タスクの文書を先に作り、それに沿って実装させる。

**出典。**

| 出典 | 印 | 要点 |
| --- | --- | --- |
| [Kiro の Specs の文書](https://kiro.dev/docs/specs/) | **未確認（検索要約のみ）** | `requirements.md` に EARS の形（`WHEN [condition/event] THE SYSTEM SHALL [expected behavior]`）で受け入れ条件を書き、`design.md`・`tasks.md` へ進む |
| [github/spec-kit](https://github.com/github/spec-kit) | **未確認（検索要約のみ）** | `/speckit.analyze` が仕様・計画・タスクの食い違いを横断で調べ、`/speckit.checklist` が要件の抜けを調べる |
| [Understanding Spec-Driven-Development: Kiro, spec-kit, and Tessl（martinfowler.com, Birgitta Böckeler, 2025-10-15）](https://martinfowler.com/articles/exploring-gen-ai/sdd-3-tools.html) | 一次・抽出（WebFetch） | 下の引用 |
| [Putting Spec Kit Through Its Paces（Scott Logic, Colin Eberhardt, 2025-11-26）](https://blog.scottlogic.com/2025/11/26/putting-spec-kit-through-its-paces-radical-idea-or-reinvented-waterfall.html) | 一次・抽出（WebFetch） | 同じ機能を spec-kit と普段の反復で作って比べた |

> Even with all of these files and templates and prompts and workflows and checklists, I frequently saw the agent ultimately not follow all the instructions.
>
> （訳: **これだけのファイルとテンプレートとプロンプトとワークフローとチェックリストがあっても、エージェントが最終的に指示を全部は守らないのを頻繁に見た。**）— Böckeler。spec-kit の文書は冗長で `tedious to review`（レビューが退屈）、「markdown を全部レビューするよりコードをレビューしたい」とも書く

**効果の実測（Scott Logic。1人・1機能の比較）。**

| 進め方 | エージェントの実行時間 | 生成物 | レビューの時間 | バグ |
| --- | --- | --- | --- | --- |
| spec-kit | 33分30秒 | コード689行・markdown 2,577行 | **3.5時間** | 1件 |
| 普段の反復 | 8分 | コード約1,000行 | **15分**（ほかに動作確認9分） | 0件 |

著者は、普段の反復のほうが全体で約10倍速かったとしている。

**効く観点。**書く側。

**制約に当たるか。**当たらない。

**このリポジトリへの当てはめ（案）。**
- このリポジトリは既に「設計→設計レビュー→実装」の順を持つ（[design-review.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md)）
- **上の否定的な実測は、「先に書く文書を増やすと、レビューする対象そのものが増える」ことを示している。**設計のコメントを長くする向きではなく、**受け入れ条件（何が起きたら blocking か）を短く書く向きのほうが、材料に合う。これは推論である**

---

### 作者が実際に直した割合を測って、打ち手を選ぶ

**何をするか。**「出した指摘のうち、作者が実際に直した割合」を測る。**各社はこれを見ながら、上の打ち手を採るかを決めている。**

**出典と定義。**

| 出典 | 指標 | 定義 |
| --- | --- | --- |
| Building a better Bugbot（上の節） | **解決率（resolution rate）** | `uses AI to determine, at PR merge time, which bugs were actually resolved by the author in the final code`（**マージの時点で、作者が最終的なコードで実際に直した指摘を AI に判定させる**）。社内で PR の作者と突き合わせ、ほぼ全件が正しく分類されていたと書く |
| How to Make LLMs Shut Up（上の節） | 対応された割合（address rate） | `Percentage of Greptile's comments that devs address before merging`（マージ前に開発者が対応した Greptile のコメントの割合） |
| uReview（上の節） | 対応されたコメント | 再実行5回で意味の近いコメントが再現しなければ、対応済みとみなす |
| 60 million Copilot code reviews（上の節） | 肯定的な反応 | 高評価・低評価の反応と、指摘がマージ前に解決されたか |
| Bugbot の文書（上の節） | 受け入れ率 | 規則ごとの `Acceptance rate` |

**効果の実測。**各社の数値は、上のそれぞれの節にある。

**効く観点。**ループの制御（どの打ち手が効いたかを判定する材料）。

**制約に当たるか。**当たらない。

**このリポジトリへの当てはめ（案）。**
- PR のコメントに貼ってある対応表の「直す / 直さない」を、周ごと・指摘の型ごとに数えれば、**「直さない」の割合（ノイズの割合）と、周回を伸ばした型**が出る
- **このリポジトリで実際に数えてはいない**

---

### 前提となる実測: 同じ対象を何度レビューさせても、結果は揃わない

**これは手法ではない。**人間の方向調整（2026-09-04）の「同じ内容を別のレビュワーに依頼したら、まったく同じ内容になるぐらい」が、1回の指示で達成できるかを判断する材料である。

| 出典 | 印 | 何が揃わなかったか |
| --- | --- | --- |
| [Best AI Code Reviewer in 2026? We Ran 4 in Parallel for 3 Weeks (146 PRs, 679 Findings)（DEV Community, 2026-05-12）](https://dev.to/_vjk/best-ai-code-reviewer-in-2026-we-ran-4-in-parallel-for-3-weeks-146-prs-679-findings-1c0f) | 一次（実務家の測定）・抽出（WebFetch） | **別々の4つのツール**を既定の設定で並べた。下の引用 |
| [Which Model Reviews Code Best?（Factory, 2026-04-29）](https://factory.ai/news/code-review-benchmark) | 一次（ベンダー）・抽出（WebFetch） | 13モデル×50 PR×3回以上。`Most models are remarkably consistent across runs, with standard deviations under 5 points.`（**ほとんどのモデルは実行ごとのばらつきが小さく、標準偏差は5ポイント未満**）。**これは F1 の点数の揃い方で、同じ指摘が出るかではない** |
| Maintainability sensors for coding agents（上の節） | 一次・抽出（WebFetch） | セッションをまたいだ文脈なしで、モジュール性のレビューを2回走らせると、毎回違う問題が出た |
| The Never-Ending AI Code Review（上の節） | **三次** | Semgrep の研究として「同じ実行を3回したら、指摘が3件・6件・11件だった」を引いている。**元の研究は開いていない** |
| Discussion #189767（上の差分レビューの節） | 一次の書き込み | 5周にわたり、前から在ったコードへ新しいコメントが出続けた |

> Out of 617 distinct (file, line) coordinates flagged across the merged-PR window: 576 (93.4%) were caught by exactly one reviewer, 37 (6.0%) were caught by exactly two reviewers, 4 (0.6%) were caught by three reviewers, 0 (zero) were caught by all four reviewers.
>
> （訳: マージされた PR の期間に指摘された617の（ファイル, 行）のうち、**576（93.4%）はちょうど1つのレビュワーだけが拾った。**37（6.0%）は2つ、4（0.6%）は3つ、**4つ全部が拾ったものは0だった。**）

Addy Osmani（2026-06-15）が同じ数値を引いている。**この数値の出典は、この DEV Community の記事である**（検索語 `"146" PRs "679" findings`）。

**精度と網羅の引き換え（ベンダーの測定）。**[We benchmarked 7 AI code review tools on large open-source projects（Augment Code, 2025-12-11）](https://www.augmentcode.com/blog/we-benchmarked-7-ai-code-review-tools-on-real-world-prs-here-are-the-results)（抽出（WebFetch））。
50 PR で、Codex Code Review は精度68%・網羅29%、Claude Code は精度23%・網羅51%、Cursor Bugbot は精度60%・網羅41%だった。**自社製品を1位に置いたベンダーの測定である。**

> tools that push recall higher often become noisy, while tools tuned for precision usually miss a significant number of real issues
>
> （訳: **網羅を上げたツールはノイズが多くなりがちで、精度に合わせたツールはたいてい本物の問題をかなり取りこぼす。**）

**この材料から言えること（解釈であって実測ではない）。**
- **「1回の指示で、別のレビュワーと同じ結果にさせる」ことを支える実測は見つからなかった**
- 業界が採っている対策は2つある。**複数のパスを束ねる**（Bugbot・Copilot の合議）と、**決定的な検査へ移す**（Thoughtworks）

---

## 否定的な知見

| 何を試したか | どうなったか | 出典 |
| --- | --- | --- |
| **プロンプトで nit を減らす** | `even with all kinds of prompting tricks, we simply could not get the LLM to produce fewer nits without also producing fewer critical comments`（**あらゆるプロンプトの工夫をしても、重要なコメントまで減らさずに nit だけを減らすことはできなかった**） | Greptile（2024-12-18） |
| **LLM に自分のコメントの重大度を判定させる**（7未満を落とす） | `the LLMs judgment of its own output was nearly random`（**ほぼでたらめ**） | Greptile（2024-12-18） |
| **偽陽性を抑えるためにモデルを「控えさせる」プロンプト** | エージェント型へ移ったあとは慎重になりすぎ、攻めたプロンプトへ切り替えた。`many changes, surprisingly, regressed our metrics`（**多くの変更が、意外にも指標を悪化させた**） | Cursor（2026-01-15） |
| **長いプロンプト・temperature の調整・サンプリング** | ほとんど改善しなかった | cubic（2025-06-19） |
| **「もっと正確に」のような曖昧な要求** | Copilot code review の文書が、対応しない指示の種類として挙げている | [Using custom instructions to unlock the power of Copilot code review（GitHub Docs）](https://docs.github.com/en/copilot/tutorials/customize-code-review)（抽出（WebFetch）） |
| **指示ファイルで「スタイルの指摘をするな」と書く** | 利用者の報告では、指示が読まれた形跡が無く、スタイルの指摘が続いた。GitHub の人からの説明は付いていない | [Discussion #187926（2026-02-24）](https://github.com/orgs/community/discussions/187926)（抽出（WebFetch）） |
| **再レビュー** | 解決済み・低評価のコメントを繰り返すことがあると、文書自身が書いている | GitHub Docs（上の差分レビューの節） |
| **文脈検索と function calling で偽陽性を減らす** | 誤った懸念と正しい指摘の比が約9:1から約1:1へ改善したが、`the AI reviewer's signal to noise ratio was just not good enough`（**それでも信号とノイズの比が足りなかった**） | [The practical and philosophical problems with AI code review（Graphite, Greg Foster, 2024-01-03）](https://graphite.com/blog/problems-with-ai-code-review)（抽出（WebFetch）） |
| **周ごとに指摘を直し続ける** | 11周で18件、設計の問題は1度も出なかった / 3周で指摘が増え、blocking 7件が未解決 / 9周、指摘が前の周の直しの文面に落ち続けた | issue #1294 / Contrast Security / issue #1298（上の各節） |
| **レビュワーが自分の出した直しを次に否定する** | バグと直し方を出したあと、同じ LLM が直しに反対して元に戻せと言った、という書き込み | [Hacker News の「Building a better Bugbot」のスレッド（2026-01-16）](https://news.ycombinator.com/item?id=46643737)（Algolia の API 経由・抽出（WebFetch）。二次の書き込みとして扱う） |
| **仕様の文書を先に厚く作る** | レビューの時間が15分から3.5時間になった（1機能の比較）。エージェントが指示を全部は守らなかった | Scott Logic / Böckeler（上の spec-driven の節） |
| **レビュー役のエージェントに自分で直させる** | ビルドとテストが通っていないのに通ったと言った / 直そうとしてコードを消しすぎた | Böckeler（2025-08-05） |
| **エージェントに TDD をさせる** | テストを無効化・削除して通そうとする | Kent Beck（2025-06-25） |

---

## 探したが見つからなかったもの

| 探したもの | 検索語・場所 | 結果 |
| --- | --- | --- |
| **CodeRabbit の「Also applies to」（同じ指摘の別の出現行を並べる表記）の公式の説明** | WebSearch `CodeRabbit review comment "Also applies to" lines same issue multiple locations` | 公式文書に説明が見つからなかった。**機能として在るかも確かめていない** |
| **Greptile v4 の作り（複数エージェントの「Swarm」）** | [Greptile v4 のページ](https://www.greptile.com/blog/greptile-v4) を WebFetch で開いた | **ページに「swarm」も複数エージェントの記述も無い。**検索結果の要約にあった「Swarm Agents で改善した」は確認できなかった |
| **Copilot の「解決の理由」（Addressed / Won't fix / Incorrect）が、次のレビューで同じ指摘を抑えるか** | [GitHub Changelog 2026-08-27](https://github.blog/changelog/2026-08-27-copilot-code-review-resolution-reasons-and-expanded-capabilities/) を開いた | 製品の改善に使うとだけ書かれ、次のレビューで抑えるとは書かれていない |
| **Google の「7.5%」（ML が提案した編集で対応されたコメントの割合）** | [Resolving code review comments with ML（Google Research, 2023-05-23）](https://research.google/blog/resolving-code-review-comments-with-ml/) を開いた | **ブログの本文に7.5%は無い。**検索結果の要約にだけあった。本文にあるのは、評価データで提案の50%が正しくなるよう絞ったこと、公開までに対応の割合が2倍になったこと |
| **差分だけの再レビューで、周回数が減ったという実測** | Cursor・CodeRabbit・Copilot の文書と changelog | 見つからなかった |
| **同じ型の指摘を1件にまとめて、修正漏れや周回が減ったという実測** | Copilot の changelog と GitHub Blog | 見つからなかった |
| **1つのレビュワーへの「nit を言うな」と、観点を分けた「見ないもの」を、同じ条件で比べた実測** | Greptile・Cloudflare・cubic の記事 | 見つからなかった |
| **Codex のレビューが、新しい push でどう再レビューするか** | [Codex の GitHub 連携の文書](https://learn.chatgpt.com/docs/third-party/github) | 書かれていなかった |
| **Qodo PR-Agent の self-reflection の現行の文書** | `docs.qodo.ai/code-review` と `docs.qodo.ai/code-review/reduce-review-noise` | 重大度ごとの表示の振り分けだけで、self-reflection・採点の閾値は書かれていなかった |
| **Microsoft の社内 AI レビューの、コメントの絞り方** | [Enhancing Code Quality at Scale with AI-Powered Code Reviews（2025-07-14）](https://devblogs.microsoft.com/engineering-at-microsoft/enhancing-code-quality-at-scale-with-ai-powered-code-reviews/) | 絞り方の仕組みは書かれていない。5,000リポジトリで PR 完了時間の中央値が10〜20%改善、とだけある |
| **Amp（Sourcegraph から独立）のレビュー役が何を指摘し、何を見ないか** | [Tessl の紹介記事（2025-12-31）](https://tessl.io/blog/amp-adds-agentic-code-review-to-its-coding-agent-toolkit/)（二次） | 書かれていなかった。Amp 自身の文書は探していない |
| **OpenAI cookbook のコードレビューの例の日付** | [Build Code Review with the Codex SDK](https://developers.openai.com/cookbook/examples/codex/build_code_review_with_codex_sdk) | 日付の表示が無い。プロンプトの `Prioritize severe issues and avoid nit-level comments unless they block understanding of the diff.`（重大な問題を優先し、diff の理解を妨げない限り nit のコメントは避ける）は抽出（WebFetch）で取れた |

---

## 開けなかった URL

| URL | 何が返ったか | 代わりにしたこと |
| --- | --- | --- |
| https://raw.githubusercontent.com/openai/codex/main/codex-rs/core/review_prompt.md | 404 | `gh api` でファイル一覧を引き、`codex-rs/prompts/templates/review/rubric.md` を原文で読んだ |
| https://sentry-blog.sentry.dev/how-we-built-code-reviews-that-catch-issues-not-noise/ | 404 | 同じ題名で `blog.sentry.io` も試して404。同社の別記事（2025-12-18）を読んだ |
| https://blog.sentry.io/how-we-built-code-reviews-that-catch-issues-not-noise/ | 404 | 同上 |
| https://docs.greptile.com/prompt-guide | 接続を拒否された | なし |
| https://www.greptile.com/docs/code-review-bot/best-practices | 本文が空で返った | なし |
| https://harper.blog/2025/02/16/my-llm-codegen-workflow-atm/ | 403 | Simon Willison の紹介記事（二次）を読んだ |
| https://news.ycombinator.com/item?id=42465374 | 429 | `https://hn.algolia.com/api/v1/items/42465374` で読んだ。実質的な技術の議論はほぼ無かった |
| https://qodo-merge-docs.qodo.ai/tools/improve/ | 301 で `docs.qodo.ai/code-review` へ転送 | 転送先に self-reflection の記述は無かった |

**開こうとしなかったもの。**`qodo-ai/pr-agent` の `configuration.toml`、Kiro と spec-kit の公式文書、Semgrep の非決定性の元の記事、Uber の講演、cubic の2本目の記事（The false positive problem）。

---

## 別の worker の担当として題名だけ挙げるもの（学術論文）

| 題名 | 見つけた場所 |
| --- | --- |
| Code Review Agent Benchmark（arXiv 2603.23448） | `"93.4%"` の検索結果 |
| AI-Assisted Fixes to Code Review Comments at Scale（arXiv 2507.13499、Meta） | Meta の AI レビューの検索結果 |
| Automating Low-Risk Code Review at Meta: RADAR, Risk Calibration, and Review Efficiency（arXiv 2605.30208） | 同上 |
| Zylos Research の記事が引く arXiv 2509.01494 / 2505.20206 / 2602.13377 / 2510.11822 / 2504.20434 | 題名を確かめていない |
