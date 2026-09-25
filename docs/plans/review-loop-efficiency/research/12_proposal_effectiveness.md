<!-- 目的: 計画（../../review-loop-efficiency.md）の0節の9つの決めごとと、原因ごとの手3つを、1つずつ敵対的に検証し、要る／要らない／形を変えれば要るを判定する -->

# 決めごとの効き目の検証（9つ＋3つ）

**この文書が引いている計画の文面は、そのあとの書き直しで消えたものを含む。**
**消えたものには行番号を付けていない**（2026-09-22 に数え直した）。

**言いたいこと。**12件のうち **要らないと判定したのは2件**（2周目以降を差分だけにする、確かめ役を立てる）で、この2件は互いに独立ではない。差分だけをやめると、それを測るための読み直しの周（決めごと7）も丸ごと消える。
**要ると判定したのは2件**（前提の横展開を見るレビュワー、規則を1本にまとめる）。残る8件は形を変えれば要る。
**ただし「規則を1本にまとめる」の判定は、計画の4節が取り下げた。**`.claude/` に2枚目の定義を置かず、正は組み込みの指示書の1箇所だけにする。
**いちばん重い発見は、10節の「既存」の書き方が、実測でいちばん多い原因（修正漏れ52件・24.0%）を「この PR を止めない」側へ落としてしまうことである。**

**この文書は判定であって決定ではない。**決めるのは人間である。最後の節に「人間に訊く必要があるもの」と「訊かなくてよいもの」を分けた。

---

## 0. 前置きの問いへの答え

### この作業は、どの規則に当てはまるか

| 規則 | どう当てたか |
| --- | --- |
| [.claude/rules/reporting.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/reporting.md) の「測る前に断定しない」 | 件数には出典の節を添えた。測っていないものは「測っていない」と書いた。8節に一覧を置いた |
| 同じファイルの「数字は、合否の線とセットでしか出さない」 | 判定の線を「03 の分類のどの群に、何件ぶん当たるか」に固定し、当たらないものは「当たらない」と書いた |
| 同じファイルの「名札は、単独で書かない」 | issue と PR の番号には題名を添えた。自分で記号を振らず、決めごとは内容そのもので呼んだ |
| [.claude/skills/worker-briefing/SKILL.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md) の 2-5（同じものが他に無いかを数える） | 「確かめ役に依存する箇所」「差分だけの実測が無いと書いてある箇所」を数え、件数と検索パターンと範囲を本文に書いた |
| 同じファイルの 2-7（合理的根拠を「誰が何を失うか」で書く） | 「要らない」と判定した2件は、直さなかったときではなく**採ったときに誰が何を失うか**で書いた |
| [CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) の「公開してよい情報かを常に判断する」 | このファイルは PUBLIC の `docs/plans/` に置かれる。個人の絶対パスを書かず `~/` から書いた |
| [CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md) の「`~/.claude/projects/` 配下を消さない」 | 書いたのはこのファイル1本だけ。git と gh は読み取りだけ（`git show` / `git grep` / `gh pr list`）。worker は立てていない |

### 飛ばしてよい段はあるか

**無い。**[.claude/rules/design-review.md:5-17](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L5-L17) の9段は issue の実装作業の順番である。この作業は設計も実装も PR の作成もしないので、そもそも9段に入らない。同じ文書の「飛ばしてよい場合」（文書だけの変更）を根拠にしたのではない。

### 公開してよくない情報を書きうる場面はあるか

**3つあった。**(1) worktree と scratchpad の絶対パス（利用者名を含む）→ `~/` から書いた。(2) メモリのディレクトリ名（利用者名を含む）→ ファイル名だけを書いた。(3) 開いている PR の題名と番号 → 公開情報なのでそのまま書いた。トークン・ホスト名は扱っていない。

### 作業に効く場所をどう探し、何を読んだか

| 何を | どうやって |
| --- | --- |
| 前置き | [.claude/skills/worker-briefing/SKILL.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md) 全文を Read |
| 計画と元資料 | [../../review-loop-efficiency.md](../../review-loop-efficiency.md)、[03_empirical_pr_rounds.md](03_empirical_pr_rounds.md)、[04_anthropic_official.md](04_anthropic_official.md)（755行を2回に分けて全部）、[05_industry_tools_blogs.md](05_industry_tools_blogs.md)（747行を2回に分けて全部）、[06_academic.md](06_academic.md)（540行を2回に分けて全部）、[07_sibling_fix_prevention.md](07_sibling_fix_prevention.md) 全文、[08_plan_review_round1.md](08_plan_review_round1.md) と [09_plan_review_round2.md](09_plan_review_round2.md) 全文 |
| 指示に名前が出ていないもの（2-4） | [01_inventory_repo.md](01_inventory_repo.md) の4節（写し）・5節（食い違い）・6節（総量）と、[02_inventory_memory_plugins.md](02_inventory_memory_plugins.md) の4節（メモリの食い違い）・5節（プラグインに在って規則に無い考え方）・6節（重複して走りうるレビュー）。**どちらも指示の「読むもの」の表に無い。**決めごと6の費用と、確かめ役の重複を判定するために自分で開いた |
| いまの規則 | [CLAUDE.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md)、[.claude/rules/design-review.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md)、[.claude/skills/pr-review-and-merge/SKILL.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md)。[.claude/rules/parallel-work.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/parallel-work.md) は worktree の版と `git show origin/main:` の版の両方 |
| 開いている PR の現状 | `gh pr list --state open --json number,title,isDraft`（読み取りだけ） |
| 数えたもの | `grep -c '確かめ役' review-loop-efficiency.md`、`git grep -c 'ここには写さない'`、`git grep -c '要点だけ'`、`wc -l`、`grep -l 'paths' .claude/rules/*.md` |

**読めなかったものは無い。**ただし **`/code-review` は1回も実行していない**（レートリミットを使うため）。差分の範囲を渡せるかは、この文書でも確かめていない。

---

## 1. 問題の定義

| 何を | 中身 |
| --- | --- |
| **何が起きているか** | レビューの周回が収まらない。実測で、PR 1本の実装レビューが最大36周、issue 1件の設計＋実装が最大62周（[03](03_empirical_pr_rounds.md) の 1-2 と 1-5）。その対策として計画が9つの決めごとを出したが、効くかどうかを1件ずつ測った文書が無い |
| **なぜ困るか** | 効かない決めごとを入れると、周は減らないまま、増えたエージェントと増えた文書の行数だけが残る。**しかも周を増やす向きの副作用を持つものが混ざっている**（下の 決めごと5 と 決めごと7） |
| **いま何を決めるのか** | 12件のうち、どれを計画から落とすか。**落とす候補として2件を挙げる。**決めるのは人間である |

**人間の見立て。**

> また、それぞれその提案が妥当なのか、本当に効果があるのか先にレビューしてから再度説明と質問して。意味のないものが含まれている気がしている。

**この見立ては当たっている、というのがこの文書の結論である。**

---

## 2. 判定の前提

### 2-1. 人間が新しく決めた重さの定義

> criticalは必ず修正が必要なもの。利用者にとって利用をやめる程度に悪影響があるもの
> highは修正しなくても動くが、利用者または運営者に直接害または不利益があるもの
> midは使い勝手が悪いが運用でカバーできる程度のもの
> lowは本当に些細なもの
> という定義にする。
> これはレビューする側の定義に関わらず、レビューを受けた側がこの基準で再評価して振り分けること。

**この定義は、計画の10節にあった重さの表を置き換える。**そのうえで、**計画の7節と正面から衝突する。**

| どちらが何を言っているか | 場所 |
| --- | --- |
| **人間**: 重さを振り分けるのは**レビューを受けた側**である | 上の原文 |
| **計画**: 「重さは、見つけた役でも**対応表を書く本人でもなく**、確かめ役が 10節の基準で付ける」 | [../../review-loop-efficiency.md](../../review-loop-efficiency.md) |

**「対応表を書く本人」がレビューを受けた側である。**計画は、人間がいま名指しで指定した相手から、その仕事を取り上げている。
**この衝突は、下の 決めごと4 の判定の土台になる。**

### 2-2. 判定に使う実測（[03](03_empirical_pr_rounds.md) の 2-2）

周回の多い上位5本の Critical と High 217件の分類である。**判定の「どの原因に何件ぶん効くか」は、すべてこの表を指す。**

| 群 | 件数 | 割合 | 内訳 |
| --- | --- | --- | --- |
| 同様の箇所を確かめていれば防げた | 74 | 34.1% | 修正漏れ 52 / 参照のずれ 18 / 写しの食い違い 4 |
| 直しが新しく生んだ | 49 | 22.6% | 直しが持ち込んだ 21 / 振り子 19 / 膨張 9 |
| 直さない判断のまま残り続けた | 35 | 16.1% | 再指摘 23 / レビュワーの誤り 5 / 既に決めた判断と衝突 3 / 持ち越し 2 / 範囲外の要求 2 |
| 前の周から在った | 17 | 7.8% | 見落とし 15 / 直しが浅い 2 |
| 起点（比べる前の周が無い） | 15 | 6.9% | 初回 9 / 作り直しで入った 6 |
| その他 | 27 | 12.4% | 分類できない 24 / 手続き 3 |

**この表の限界を、判定でどう扱ったか。**

- **diff を読んでいない**（[03](03_empirical_pr_rounds.md) の 0節「**PR の diff は読んでいない。**分類はコメントの本文だけにもとづく」）。だから「修正漏れ52件が、文字列で探せば見つかったか」は**言えない。**下の 手A の判定で、文字列だけでは足りない根拠を 03 の 2-3 から取り直した
- **上位5本だけ**である。全 PR の割合ではない。だから判定では「34.1%」を全体の見積もりとして使わず、「上位5本でいちばん多い群」としてだけ使った
- **分類できない24件**がある。除くと修正漏れは26.9%、同様の箇所の群は38.3%になる。**どちらの数え方でも順位は変わらない**ので、判定は順位だけに乗せた

---

## 3. 9つの決めごとの判定

### 3-1. 2周目以降は差分と前の周の表だけを見る（9節）→ **要らない**

| 観点 | 判定の中身 |
| --- | --- |
| **どの原因に効くか** | **再指摘23件（10.6%）だけ。**しかも [03](03_empirical_pr_rounds.md) の 2-6 が「**C/H が全部これだった周は2つで、どちらも10周目で止まった周である**」と測っている。**上位5本で、再指摘を消して減る周は最大2周である** |
| **効果の根拠** | **無い。**「差分だけにして周回が減った実測」は、**4箇所で探して見つかっていない**（[../../review-loop-efficiency.md](../../review-loop-efficiency.md)、[05:711](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/05_industry_tools_blogs.md#L711)、[06:510](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/06_academic.md#L510)、[04:728](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/04_anthropic_official.md#L728)）。採る根拠は「Bugbot が既定でそうしている」だけで、その出典は WebFetch の要約モデル経由である |
| **反証** | **2件ある。**GitHub Docs「**再レビューのとき、Copilot は、解決済みにしたり低評価を付けたりしたコメントでも、繰り返すことがある**」（[05:369-371](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/05_industry_tools_blogs.md#L369-L371)）。GitHub Community の Discussion #189767「500〜1000行の PR で5周、新しいコメントが10・6・4・2・2件出た。**前の diff に既に在ったのに指摘されなかったコードへの新しいコメントだった。**約24件のうち役に立ったのは約3件」（[05:373-378](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/05_industry_tools_blogs.md#L373-L378)）。**差分だけにしても、変えていない行からの新しい指摘は止まっていない** |
| **費用** | **決めごと7（測る10本の読み直し）を丸ごと生む。**PR 1本につき1周分×10本で、出た C/H は直して回し10回に数える。**つまり周を減らすための決めごとが、周を増やす決めごとを1つ連れてくる** |
| **無くても達成できるか** | **できる。**[.claude/rules/design-review.md:143-146](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L143-L146) が既に「次の周のレビュワーには、前の周の対応表を渡す」「`/code-review` では渡せない。**そのときは、受け取った側が前の周の対応表と突き合わせる**」と決めている。再指摘は、受けた側が同じ理由を書き直すだけで1行で終わる |
| **副作用** | **いちばん多い群（同様の箇所74件）を見つける経路が細る。**修正漏れは「触っていない文書に残った参照」なので、差分の外にある。9節の表は「差分の周のレビュワーに、差分の外を探させはしない」と書いており、[03](03_empirical_pr_rounds.md) の 2-3 が数えた5つの前提のうち4つは、差分の外の文書に出ている |
| **未検証の前提** | **`/code-review` に2つの SHA を渡せるかを、誰も試していない**（[../../review-loop-efficiency.md:509](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency.md#L510)）。このセッションの skill の一覧に出る `code-review` の説明文は「Review the current diff, or a **PR number/branch/path target**」で、**commit の範囲を受け取るとは書いていない。**計画でいちばん構造を変える決めごとが、いちばん確かめられていない前提の上に乗っている |

**判定。要らない。**採ると、上位5本で最大2周しか減らない見込みのために、未検証の呼び出し方と、周を増やす測定の決めごとと、最大の原因群を見つけにくくする副作用の3つを同時に背負う。
**採らないと続くこと。**毎周 PR 全体を読み直す費用は残る。**それを減らしたいなら、範囲を差分に限定するのではなく、差分と前の周の対応表を「材料として渡す」だけにすればよい**（範囲は絞らない）。これなら未検証の呼び出し方も、測定の周も要らない。

### 3-2. 重さの基準を置く。この PR が持ち込んでいない既存の欠陥は止めない（10節）→ **形を変えれば要る**

**基準そのものは、人間が 2-1 で決めた。**残っているのは「既存」の扱いである。

| 観点 | 判定の中身 |
| --- | --- |
| **どの原因に効くか** | 「既存」の行だけを見ると、当たるのは範囲外の要求2件（0.9%）である。**それ以上に、当たってはいけない群に当たる** |
| **副作用（いちばん重い）** | 10節の文言は「**この PR の diff に入らない行に前から在るもの**」を既存と定義する。**修正漏れ52件（単独最多、24.0%）が、この定義に文字どおり当たる。**例: [03](03_empirical_pr_rounds.md) の 2-3 の「消した設定 `pause_above_percent` を文書に残さない」は、PR #230（枠が残り少ないときに止まる理由を出し、1週間の枠を待つ上限を足す（#173 / #197 / #199））の3つの周に4件出ている。**その文書の行は、この PR が1文字も触っていない。**つまり「既存」として、この PR を止めない側へ落ちる |
| **反証** | Code Review 文書「**This works bidirectionally: if your PR changes code in a way that makes a `CLAUDE.md` statement outdated, Claude flags that the docs need updating too.**（PR の変更が CLAUDE.md の記述を古くしたら、文書の更新も要ると指摘する）」（[04:528](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/04_anthropic_official.md#L528)、WebFetch の全文）。OpenAI Codex の rubric も「既存のバグ」を「**その commit より前からコードベースに在ったもの**」と定義し、[05:126](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/05_industry_tools_blogs.md#L126) が「2-6 の『前の周に既に在ったもの』とは別物である」と注意している |
| **費用** | 文言に1文足すだけ |
| **衝突** | 既存の規則4つと衝突することは、計画が既に書いている（[CLAUDE.md:545](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L545)・[:551](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L551)、[design-review.md:134-136](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L134-L136)、maimuzo-dev-core の general-claude-md） |

**判定。形を変えれば要る。**「既存」の定義に、次の1文を必ず足す。

> **この PR の変更によって初めて誤りになった箇所は、その行が変わっていなくても「既存」ではない。**

**足さないと、実測でいちばん多い原因が、規則によって「止めなくてよいもの」に変わる。**

### 3-3. 同じ issue の1周目で、読むだけの役を並列に走らせてよい（7節）→ **形を変えれば要る**

| 観点 | 判定の中身 |
| --- | --- |
| **どの原因に効くか** | **当たらない。**これは 決めごと4 を実行できるようにするための手続きで、原因に直接は効かない |
| **効果の根拠** | 時間だけ。直列でも同じ結果が出る |
| **費用** | [.claude/rules/parallel-work.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/parallel-work.md) に1行 |
| **無くても達成できるか** | **できる。**2体を直列に走らせても結果は同じで、1周目が長くなるだけである |
| **いまの状態（実測）** | PR #256（同じ issue のレビューと修正を同時に走らせない規則を足す（要約の1行も読み違えを塞ぐ形へ））は、**2026-09-16 時点で draft のまま開いている**（`gh pr list --state open` で確認。開いているのは7本で全部 draft）。`git show origin/main:.claude/rules/parallel-work.md` は、まだ古い本文を返す |
| **例外がどこに要るか** | PR #256 の本文は「**同じ issue のレビューと、それに対する作業**」を禁じ、「それに対する作業」に**実装・修正・設計の書き直しの3つ**を挙げている。**読むだけの役どうしは、この禁止の対象に入っていない。**止めているのは「走らせる順番」の段1「**1つの issue からは1つだけ**」のほうである。**だから例外が要るのは段1だけで、禁止の表は触らなくてよい** |
| **人間の方針との衝突** | 2026-09-05 の「なんでこんなことをしたんだ。ルール違反だし、普通に考えてもレビューしながら修正するのはおかしいだろ。」は、**レビューと修正の同時実行**への指摘である。読むだけの2体には当たらない |

**判定。形を変えれば要る。**例外は「走らせる順番」の段1にだけ、**ファイルを変えない役どうしに限って**足す。禁止の表は1文字も変えない。
**決めごと4 の前半を採らないなら、この決めごとは要らない。**

### 3-4. 1周目に「前提の横展開を見るレビュワー」と「確かめ役」を足す（7節）→ **2つに分ける**

**計画は2つの役を1つの決めごとにまとめているが、効き目がまったく違う。分けて判定する。**

#### (a) 前提の横展開を見るレビュワー → **要る**

| 観点 | 判定の中身 |
| --- | --- |
| **どの原因に効くか** | **同様の箇所74件（34.1%、上位5本でいちばん多い群）。**これに当たる「見つける側」の手は、この計画でこれ1つだけである |
| **効果の根拠** | **実測がある。**Hatton 2008「Testing the value of checklists in code inspections」（[07:311](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/07_sibling_fix_prevention.md#L311)。07 の書き手が PDF を起こして読んだ）が、**308件の点検で「チェックリストは点検の成果を有意に変えなかった。1人で平均53%、2人組で平均76%の欠陥を見つけた」**と測っている。**これは「表に行を足す」を否定し、「人を足す」を支持する。**この調査で集まった材料のうち、この判定にいちばん直接当たる実測である |
| **補強** | [03](03_empirical_pr_rounds.md) の 2-3 が「同じ前提が、形を変えて何周にもわたって出た例」を5つ挙げ、**5つのうち3つは「指摘の本文に共通する語が無い」**（関数名・欄の名前・空白の種類が毎回違う）。**文字列の検索では届かないので、意味で探す役が要る。**これはこのリポジトリで測ったものである |
| **費用** | 1周目に Agent 1体。`/code-review` と並列に走らせるなら 決めごと3 の例外が要る |
| **無くても達成できるか** | **できない。**6節（出す前の記録）と 8節（直すときの記録）は書く側の手で、**書く側が気づかなかった前提には当たらない。**Hatton の実測が、まさにその差を測っている |
| **副作用** | トークンが増える。[04:602](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/04_anthropic_official.md#L602) は複数 agent が「約15倍のトークンを使う」と書くが、**これは調査タスクの社内評価であってコードレビューではない**（04 自身がそう注意している） |

**判定。要る。**

#### (b) 確かめ役 → **要らない**

| 観点 | 判定の中身 |
| --- | --- |
| **どの原因に効くか** | **レビュワーの誤り5件（2.3%）が最大である。**計画は「指摘は1件も落とさない」と決めている（[../../review-loop-efficiency.md](../../review-loop-efficiency.md)）ので、**対応表の行数は1行も減らない。**残る仕事は「重さを付ける」「根拠を補う」の2つになる |
| **人間の方針との衝突** | **正面から衝突する。**人間が 2-1 で「**レビューを受けた側がこの基準で再評価して振り分けること**」と決めた仕事を、計画は「対応表を書く本人でもなく、確かめ役が付ける」（[../../review-loop-efficiency.md](../../review-loop-efficiency.md)）と、名指しで取り上げている |
| **反証** | **2件ある。**Greptile「**LLM に自分のコメントの重大度を判定させる → ほぼでたらめだった**（`the LLMs judgment of its own output was nearly random`）」（[05:688](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/05_industry_tools_blogs.md#L688)、WebFetch の抽出）。Lu ほか（ICML 2025）「**Validator で誤警報は下がるが、key bug inclusion も 31.11%→20.00% に下がる**」（[06:310](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/06_academic.md#L310)、要約経由） |
| **費用** | **計画の23行が確かめ役に依存している**（`grep -c '確かめ役' ~/Sources/github/continuo/.claude/worktrees/review-loop-efficiency/docs/plans/review-loop-efficiency.md` が23）。**1周ごとに1体増えるので、周が増えるほど費用が比例して増える。**周を減らす計画の中で、周に比例する費用を持つ唯一の決めごとである |
| **無くても達成できるか** | **できる。**[.claude/rules/design-review.md:124](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L124) の「絶対条件：合理的根拠を否定できるなら、直さない」を受けた側が行う形は既に規則にあり、[.claude/skills/pr-review-and-merge/SKILL.md:113-117](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L113-L117) が「根拠が足りなければ**受け取る側が補って対応表に載せる**」と既に決めている。**確かめ役は、この2つを別のエージェントへ移すだけである** |
| **公式の注意** | Prompting Claude Opus 5「`do not use subagents to verify or double-check your own work`」（[04:167](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/04_anthropic_official.md#L167)、WebFetch の全文）。04 は「他の agent が挙げた指摘の検証とは別物だと読める。**ただしそう読んでよいかは文書に書かれておらず、確かめていない**」と書いている。**計画は、この未確認の読み方の上に役を1つ立てている** |

**判定。要らない。**残したい仕事は2つだけで、どちらも新しい役を立てずに置ける。

- **振り子の判定**（19件）→ 下の 3-5 の「同じ前提から2周続けて出たか」で拾う
- **仕組みを足す直しの判定**（9件）→ 下の 手C のとおり、対応表の「分類」列に値を足す

### 3-5. 3周目以降、差分レビューで C/H が減らなければ6段を通す（9節）→ **形を変えれば要る**

| 観点 | 判定の中身 |
| --- | --- |
| **どの原因に効くか** | **直接は当たらない。**周の向きを変える手である |
| **効果の根拠** | [03](03_empirical_pr_rounds.md) の 2-5 が「**High は、上位2本では減らなかった**」を測っている。**症状の存在は測れている** |
| **副作用（いちばん重い）** | **1回の比較では、騒音で発火する。**同じ 2-5 の実測で、PR #230（枠が残り少ないときに止まる理由を出し、1週間の枠を待つ上限を足す（#173 / #197 / #199））の系列4の High は **5・2・3・3・4・3・3・4・2・6**。「前の回より減っていない」は 2→3・3→3・3→4・3→4・2→6 の**5箇所で成り立つ。10周のうち5回、6段を通すことになる** |
| **費用** | 6段は目的の確認役・敵対的レビュワー・設計の敵対的レビュー・**作り直す worker** を伴う（[CLAUDE.md:640-659](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L640-L659)）。しかも [03](03_empirical_pr_rounds.md) の 2-2 は「**作り直しで入った 6件**」を数えている。**作り直しそのものが C/H を生む** |
| **出典との食い違い** | 計画が引いている出典は、どちらも「2周続けて」である。workflows 文書「`two rounds in a row make no progress`」（[04:631](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/04_anthropic_official.md#L631)、全文）、Best practices「`corrected Claude more than twice`」（[04:622](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/04_anthropic_official.md#L622)、全文）。**計画が1回にした理由は「発動が1周遅れる」だが、上の推移では1回だと騒音に反応する** |
| **無くても達成できるか** | **半分はできる。**3・6・9回目の6段は既にある（[CLAUDE.md:640](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L640)）。足すのは「前倒しで入る条件」だけである |

**判定。形を変えれば要る。**2つ直す。

1. **1回ではなく2周続けて**にする（出典に合わせる）
2. **件数ではなく「同じ前提からの指摘が2周続けて出たか」で判定する。**[03](03_empirical_pr_rounds.md) の 2-3 と 2-4 が数えているのは、件数の増減ではなく**同じ前提の再出現と往復**である。件数は上下するが、前提の再出現は数えられる

### 3-6. 規則を `.claude/rules/review-loop.md` 1本にまとめ、写しを消す。メモリ6件も直す（11節）→ **要る**（**この判定は、計画の4節が取り下げた。**`.claude/` に2枚目の定義を置かず、正は組み込みの指示書の1箇所だけにする）

| 観点 | 判定の中身 |
| --- | --- |
| **どの原因に効くか** | **写しの食い違い4件（1.8%）に直接。**間接には、規則が届かないことによる全体に効く（下の根拠） |
| **効果の根拠（このセッションで直接確かめた）** | **`.claude/rules/*.md` は自動で読まれる。**このセッションの system reminder に、`~/Sources/github/continuo/.claude/rules/` の worktree.md・parallel-work.md・reporting.md・issue.md・release.md・plugins.md・plan-file.md・design-review.md の**8本が「project instructions, checked into the codebase」として注入された。私はこの8本を Read していない。**`grep -l 'paths' .claude/rules/*.md` は0件である。一方 skills は名前と説明だけが一覧に出て、本文は注入されていない（worker-briefing は Read で開いた）。**つまり [CLAUDE.md:308](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L308) の「`.claude/rules/` と `.claude/skills/` の下のファイルは自動では読まれない」は、rules については事実と逆である**（skills については正しい） |
| **写しの実測** | `git grep -c 'ここには写さない' -- CLAUDE.md .claude/` が **12件**（design-review.md 4・worker-briefing 3・pr-review-and-merge 3・reporting.md 2）。`git grep -c '要点だけ'` が **5件**（design-review.md 4・pr-review-and-merge 1）。[01](01_inventory_repo.md) の4節は、書式の崩れ `****そのとき` が**3ファイルに同じ形で複製**されていること、[01](01_inventory_repo.md) の5節は pr-review-and-merge:190 が**実在しない見出し**「絶対条件：連続10回で止まる」を指していることを挙げている |
| **総量** | 実装レビューの1周で直す側が従う規則は **883行・約60KB**（[01](01_inventory_repo.md) の6節）。Best practices「**禁じる規則があるのに Claude が同じことを続けるなら、ファイルが長すぎて規則が埋もれている可能性が高い**」（[07:176](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/07_sibling_fix_prevention.md#L176)、全文を開いたもの） |
| **費用** | 文書の移動。**足す行より消す写しのほうが多くなる見込み**だが、行数は書いてから測る（計画の12節がそう書いている） |
| **無くても達成できるか** | **できない。**写しの食い違いは、写しがある限り出続ける |
| **副作用** | [CLAUDE.md:308](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L308) を直すのは、レビューループの外（全作業の読み方）に触る。08 の1周目でも Low として挙がり、「直す対象に残す」と判定されている |

**判定。要る。****ただし、この判定は計画の4節が取り下げた。**`.claude/` に2枚目の定義を置かず、正は組み込みの指示書の1箇所だけにする。

### 3-7. 測る10本だけ、収まったあとに PR 全体を1回読み直す（15節）→ **決めごと1に完全に従属する**

| 観点 | 判定の中身 |
| --- | --- |
| **どの原因に効くか** | **1件も当たらない。**これは測定の段である |
| **なぜ在るか** | 差分だけにすると「周が減った」と「見逃しが増えた」を区別できないためである。08 の1周目の High「効き目の線が見逃しを測らない」への対応として入った |
| **費用** | **PR 1本につき1周分×10本。**しかも「出た Critical と High は直して回し、その周も10回に数える」（[../../review-loop-efficiency.md](../../review-loop-efficiency.md)）。**2026-09-05 の「収まったら最大1周」を、測る10本のあいだだけ超えることを、計画自身が0節の決めごとにしている** |
| **無くても達成できるか** | **決めごと1（差分だけ）を採らなければ、測る対象そのものが存在しない** |

**判定。決めごと1を採るなら要る。採らないなら要らない。**単独では決められない。

### 3-8. 開いている PR 3本が片付いてから、その上に作る → **形を変えれば要る。人間に訊く必要は無い**

| 観点 | 判定の中身 |
| --- | --- |
| **どの原因に効くか** | 当たらない。衝突を避ける手続きである |
| **いまの状態（実測）** | `gh pr list --state open` は **7本**を返す（#230・#254・#255・#256・#267・#271・#272。全部 draft）。このうち同じ節を書き換えるのは3本（#256・#267・#272）である |
| **副作用** | PR #230（枠が残り少ないときに止まる理由を出し、1週間の枠を待つ上限を足す（#173 / #197 / #199））は**36周回っている**（[03](03_empirical_pr_rounds.md) の 1-2）。「開いている PR が片付いてから」を広く読むと、この計画が長く止まる |
| **無くても達成できるか** | **できる。**「待つ」ではなく「どちらが先に入るかを決め、あとから入るほうが揃える」で足りる |

**判定。形を変えれば要る**（衝突する3本だけを見る）。**これは AI が決めてよい。**

### 3-9. この変更のための issue を1件立て、文書だけの変更として PR を出す → **要る。人間の許可が要る**

| 観点 | 判定の中身 |
| --- | --- |
| **どの原因に効くか** | 当たらない。手続きである |
| **なぜ人間に訊くか** | [.claude/rules/issue.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/issue.md) の「絶対条件：issue を作ることと、着手することは別」と、メモリの feedback_issue_creation_needs_human_approval.md（AI が独断で issue を書くことを絶対禁止。人間の依頼か許可のみ） |
| **費用** | issue 1件と PR 1本 |

**判定。要る。**訊くのは「立ててよいか」であって、中身ではない。

---

## 4. 原因ごとの手3つの判定

### 4-1. 横展開の記録（6節・8節）→ **形を変えれば要る**

**Hatton 2008 の否定的な実測に照らして、効くと言えるか。**

**半分は言えない。半分は言える。**Hatton が測ったのは「**チェックリストの有無で点検の成果が変わるか**」である（[07:311](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/07_sibling_fix_prevention.md#L311)）。

| 6節・8節のどの部分か | Hatton の否定に当たるか |
| --- | --- |
| **[worker-briefing 2-5](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L211-L219) の表に4行足す**（消した・状態を足した・読む回数を変えた・前提を変えた） | **当たる。**これは点検項目を増やすことそのものである。**この4行だけでは効かないと見るべきである** |
| **前提を1文にし、探し方と当たった箇所を表に書いて残す** | **当たらない。**Hatton は記録も再検査も測っていない |
| **次の周に、その探し方を叩き直す** | **当たらない。**むしろ [07](07_sibling_fix_prevention.md) の仮説4（「数えた結果が、修正後に検査されていない」）と、Claude Code 公式の「**成功を主張させるのではなく、証拠を見せさせる**」（[07:179-180](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/07_sibling_fix_prevention.md#L179-L180)、全文）が支える |

**[07](07_sibling_fix_prevention.md) の 4-1 が、この区別を裏づける実測を持っている。**2-5 が入ってから「数えた記録」はコメントの大半に付くようになったのに（〜09-03 で7件 → 09-04〜05 で103件 → 09-06〜 で57件）、**取り残し系の Critical / High の行は0にならない**（7 → 17 → 14）。07 の読み取りは「**『数えていない』より『数え方が当たっていない』を疑うほうが合う**」である。

**叩き直す主体を、確かめ役にしてはならない**（3-4 (b) で役ごと落としたため）。**[07](07_sibling_fix_prevention.md) の5節が代案を持っている。**

> 修正 commit で、対応表のパターンを叩き直して残り件数を表に並べる（hook か検査スクリプトにできる）

**これは Best practices の「`Unlike CLAUDE.md instructions which are advisory, hooks are deterministic and guarantee the action happens.`（CLAUDE.md の指示は助言にすぎないが、hook は決定的で、その動作が必ず起きることを保証する）」（[07:173-174](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/07_sibling_fix_prevention.md#L173-L174)、全文）と合う。**
**同型のものが既にリポジトリに在る。**[.claude/rules/plan-file.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/plan-file.md) の「設計文書へ1行でも足したら、そこを指すリンクを全部検算する」の検査スクリプトである。

**判定。形を変えれば要る。**記録は残す。**叩き直しは人ではなくスクリプトへ移す。**表に4行足すだけで終わらせない。

### 4-2. 指摘に付いた直し方の文面を採らない → **形を変えれば要る**

| 観点 | 判定の中身 |
| --- | --- |
| **どの原因に効くか** | **このリポジトリでは測っていない。**[03](03_empirical_pr_rounds.md) の 2-1 の分類の表に「指摘の文面を採った」という分類は無い。近いのは「直しが持ち込んだ 21件」「膨張 9件」だが、**その21件が指摘の文面を採ったものかは、03 が diff を読んでいないので言えない** |
| **効果の根拠** | **外部の観測1件だけ。**bentleypark/aiwatch の issue #1298（9周。「`each round's findings landed on prose the previous round's fix had just written`」）。オーケストレーターが原文の先頭5,000字を開いている。**さらに、その対策の効果は「直し方を書かないレビュー役を出した結果、1回の実行で4件の指摘・書き換え案0件」という1回の観測である**（[05:471](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/05_industry_tools_blogs.md#L471)）。**1例の観測から1例の対策へ、という鎖である** |
| **費用** | ほぼ0。Agent で立てるレビュワーへの指示1行 |
| **制約** | **`/code-review` には渡せない**（[.claude/skills/pr-review-and-merge/SKILL.md:99](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L99)「自由なプロンプトを足せない」）。**必須の道具では守れない。**規則の本文に禁止として書くと、毎周守れない条項が1つ増える |
| **無くても達成できるか** | 一部できる。[04:400-402](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/04_anthropic_official.md#L400-L402) の Best practices「抜けを探せと言われたレビュワーは、作業が健全でも普通はいくつか報告する。**指摘を全部追いかけると過剰設計になる**」が、同じ向きを別の言い方で押さえている |

**判定。形を変えれば要る。**規則の禁止条項にせず、**Agent で立てるレビュワーへの指示に1行として入れる。`/code-review` には効かないと同じ場所に書く。**採る理由は「費用がほぼ0」だけで、根拠は弱い。**弱いと書いたうえで採る。**

### 4-3. 仕組みを足す直しは、その場で入れず判定に回す → **形を変えれば要る**

| 観点 | 判定の中身 |
| --- | --- |
| **どの原因に効くか** | **膨張9件（4.1%）。**件数は小さい |
| **効果の根拠** | **件数は小さいが、根拠はこの12件でいちばん強い部類である。**[.claude/rules/design-review.md:220-237](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md#L220-L237) が、このリポジトリで測った3件を持つ。ログ1行 → **17ファイル**（3回目の Critical 1）、文面1本 → **11ファイル**（Critical 2）、文書のみ → 文書＋検査の仕組み（**Critical 7**）。**レビューの回数と設計の大きさが比例した**、と書いている |
| **補強** | Best practices「`A reviewer prompted to find gaps will usually report some, even when the work is sound … Chasing every finding leads to over-engineering`」（[04:400](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/04_anthropic_official.md#L400)、全文）。Prompting Claude Opus 5「Opus 5 は頼まれていない段を足してタスクの範囲を広げることがある」（[04:414](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/docs/plans/review-loop-efficiency/research/04_anthropic_official.md#L414)、全文） |
| **費用** | 計画の形（確かめ役に判定させる）だと、3-4 (b) で落とした役が要る |
| **無くても達成できるか** | **判定の段だけは既にある。**3・6・9回目の6段の段2が「issue に無い内容が入っているなら、それが必要となる合理的理由をまとめる」（[CLAUDE.md:640-659](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L640-L659)）。**足りないのは「どの行が仕組みを足す直しか」の印だけである** |

**判定。形を変えれば要る。****新しい役を立てず、対応表の「分類」列に「仕組みを足す」の値を足す。**その値が付いた行だけ、3・6・9回目の6段で必ず見る。

---

## 5. 判定の一覧（12件）

| 何 | 判定 | 当たる原因（217件中） | 一言 |
| --- | --- | --- | --- |
| **2周目以降は差分と前の周の表だけを見る**（決めごと1・9節） | **要らない** | 再指摘23件。減る周は上位5本で最大2周 | 実測が1件も無く、反証が2件。未検証の呼び出し方と、周を増やす測定を連れてくる |
| **重さの基準を置く。既存の欠陥は止めない**（決めごと2・10節） | **形を変えれば要る** | 範囲外2件。**ただし修正漏れ52件を誤って外す** | 「この PR の変更で初めて誤りになった箇所は既存ではない」を必ず足す |
| **読むだけの役を並列に走らせてよい**（決めごと3・7節） | **形を変えれば要る** | 当たらない（手続き） | 例外は「走らせる順番」の段1だけ。禁止の表は触らない |
| **前提の横展開を見るレビュワーを足す**（決めごと4の前半・7節） | **要る** | **同様の箇所74件（最大の群）** | Hatton 2008 が「表に行を足す」でなく「人を足す」を支持している |
| **確かめ役を足す**（決めごと4の後半・7節） | **要らない** | レビュワーの誤り5件 | 人間が「受けた側が再評価」と決めた仕事を取り上げる。周に比例して費用が増える |
| **減らなければ6段を通す**（決めごと5・9節） | **形を変えれば要る** | 当たらない（周の向き） | 1回→2周続けて。件数ではなく「同じ前提の再出現」で判定する |
| **規則を1本にまとめ、写しとメモリを直す**（決めごと6・11節） | **要る**（**計画の4節が取り下げた**） | 写しの食い違い4件＋全体 | rules が自動で読まれることを、このセッションで直接確かめた。**ただし `.claude/` へ置くと正が2つになるので、組み込みの指示書の1箇所だけを正とする** |
| **測る10本で PR 全体を読み直す**（決めごと7・15節） | **決めごと1に従属** | 当たらない（測定） | 決めごと1を落とすなら、これも消える |
| **開いている PR が片付いてから作る**（決めごと8） | **形を変えれば要る** | 当たらない（手続き） | 衝突する3本だけ。36周の PR を待たない |
| **issue を1件立てて PR を出す**（決めごと9） | **要る** | 当たらない（手続き） | 人間の許可が要る |
| **横展開の記録**（手A・6節と8節） | **形を変えれば要る** | **同様の箇所74件** | 表に4行足す部分は Hatton の否定に当たる。叩き直しをスクリプトへ |
| **直し方の文面を採らない**（手B・8節） | **形を変えれば要る** | このリポジトリでは測っていない | 根拠は外部の1例。費用が0なので採る。規則の禁止にせず、Agent への指示に置く |
| **仕組みを足す直しを判定に回す**（手C・8節） | **形を変えれば要る** | 膨張9件 | 役を立てず、対応表の列に値を足す |

**要らないと判定した2件を落とすと、計画から消えるもの。**9節の差分レビューの手順、15節の読み直しの周、7節の確かめ役（計画の23行）、0節の決めごとのうち3つ（差分だけ・確かめ役・読み直し）。
**残るのは、1周目を厚くする（前提の横展開を見るレビュワー）・出す前と直すときに横展開を記録する・重さの基準・規則の1本化・止め方の直しの5つである。**
**そのうち「規則の1本化」は、計画の4節が取り下げた。**

---

## 6. 人間に訊く必要があるものと、訊かなくてよいもの

### 6-1. 人間に訊く必要があるもの（4件）

| 何を訊くか | なぜ人間か |
| --- | --- |
| **2周目以降を差分だけにするのをやめてよいか**（決めごと1） | **計画の構造がいちばん変わる。**落とすと決めごと7も消え、節約の主な源として計画が挙げていたものが無くなる。**代わりに何で費用を下げるかを決める必要がある** |
| **確かめ役を立てないでよいか**（決めごと4の後半） | **人間が 2-1 で「レビューを受けた側が再評価する」と決めた直後である。**計画の7節はそれと逆を書いている。どちらを正にするかは人間の方針である |
| **「既存の欠陥はこの PR を止めない」に、どこまで例外を切るか**（決めごと2） | 既存の規則4つと衝突する。**carve-out を入れても、「触っていない文書の行を直すのはこの PR の仕事か」の線引きは人間の判断である** |
| **この変更のための issue を1件立ててよいか**（決めごと9） | [.claude/rules/issue.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/issue.md) と、メモリの feedback_issue_creation_needs_human_approval.md が、AI の独断での起票を禁じている |

### 6-2. 既に人間が決めているもの（訊かない）

| 何 | いつ・どこで |
| --- | --- |
| **重さの4段の定義と、受けた側が再評価すること** | 2-1 の原文（この検証の前提として渡された） |
| **収まったら最大1周。最後の1回でも収まっていればもう直さない** | 2026-09-05。[CLAUDE.md:573-611](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L573-L611) に入っている |
| **10回で止まる。設計と実装は別に数える。方針変更で数え直す** | 2026-09-03。[CLAUDE.md:663](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L663) |
| **1回で全部挙げさせる。根拠を否定できるなら直さない** | 2026-09-04。[worker-briefing 2-6・2-7](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L256) |

### 6-3. AI が決めてよいもの（訊かない）

| 何 | なぜ AI でよいか |
| --- | --- |
| **開いている PR のどれを待つか、どちらを先に入れるか**（決めごと8） | 作業の順番であって方針ではない。衝突する3本は機械的に特定できる |
| **並列の例外を「走らせる順番」の段1にだけ足す文面**（決めごと3） | PR #256 の禁止の表は読むだけの役を対象にしていないので、方針を変えていない |
| **減らないときの判定を2周続けてに直すこと**（決めごと5） | 計画が引いた出典2つが、どちらも「2周続けて」と書いている。出典に戻すだけである |
| **横展開の叩き直しをスクリプトへ移すこと**（手A） | 同型の検査が既にリポジトリに在る（[.claude/rules/plan-file.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/plan-file.md) の行番号リンクの検算） |
| **仕組みを足す直しを、役ではなく対応表の列で拾うこと**（手C） | 判定の段（6段）は既にあり、足すのは印だけである |
| **写しを消し、メモリ6件の食い違いを直すこと**（決めごと6の大部分） | 食い違いは事実の誤りである。**ただし [CLAUDE.md:308](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L308) を含む CLAUDE.md の構成の変更は、人間へ見せてから入れる** |

---

## 7. 根拠の強さの区別

**同じ「実測」でも、誰がどこまで確かめたかが違う。判定で重く使ったものほど、上に置いた。**

| 強さ | 何 | この文書での使い方 |
| --- | --- | --- |
| **このセッションで直接確かめた** | `.claude/rules/*.md` が自動で注入されること（8本が system reminder に出た）、`grep -l 'paths'` が0件、開いている PR が7本で全部 draft、`git show origin/main:.claude/rules/parallel-work.md` が古い本文を返すこと、写しの件数（12と5）、計画の確かめ役への依存23行 | **決めごと6と決めごと3の判定の土台にした** |
| **このリポジトリで測ったもの（[03](03_empirical_pr_rounds.md)）** | 217件の分類、High の推移（5・2・3・3・4・3・3・4・2・6）、同じ前提が5例中3例で共通語を持たないこと、「直さない」だけで止まった周が2つ | **すべての判定の「どの原因に効くか」に使った。**ただし **diff を読んでいない・上位5本だけ**という限界を 2-2 に書いた |
| **原文を開いたもの（`gh api` か WebFetch の全文）** | Opus 5 文書、Code Review 文書、Best practices、workflows 文書、Codex の rubric、code-review プラグイン、bentleypark の issue #1298、Hatton 2008 の PDF、Yue ほか・LASE・ROSE の PDF | **判定の根拠として使ってよいものとして扱った** |
| **WebFetch の要約モデル経由** | Bugbot の 52%→70% と「既定で差分だけ」、Copilot の +47%、Greptile の「ほぼでたらめ」、cubic の 51%、Cloudflare、Uber、fullsend の issue #1294、BitsAI-CR、Snyk | **「反証」としては使ったが、「採る根拠」としては単独で使っていない。**とくに Bugbot の「既定で差分だけ」は、決めごと1 を支える唯一の出典でありながらこの段である |
| **arXiv の要旨だけ** | SWR-Bench、Shukla ほか | 数値を判定の分岐には使っていない |
| **検索結果の要約だけ** | CodeQL の CVE 400件超、Safe Coding、Card 2017 の 5 Whys 批判 | **1件も判定に使っていない** |

---

## 8. 測っていないこと・確かめていないこと

- **`/code-review` に2つの SHA の範囲を渡せるか。**実行していない（レートリミットを使うため）。このセッションの skill の一覧の説明文は「Review the current diff, or a PR number/branch/path target」で、**commit の範囲を受け取るとは書いていない**
- **`/code-review` が CLAUDE.md の重さの基準に従って出力を変えるか。**計画の16節も同じことを書いている
- **確かめ役を落としたときに、レビュワーの誤り5件がどう扱われるか。**受けた側が同じ判定をできるかは測っていない（規則の上ではできる、としか言えない）
- **前提の横展開を見るレビュワーが、実際に修正漏れを何件拾うか。**Hatton 2008 は人間の点検の数値であって、Agent で測ったものではない
- **手Bの「直し方の文面を採らない」が、このリポジトリで何件に当たるか。**[03](03_empirical_pr_rounds.md) がその分類を持っていない
- ~~**規則を1本にまとめたときに、行数がどれだけ減るか。**書いてから測る~~ **← 計画の4節が取り下げた**
- **12件を全部採ったとき／2件を落としたときの、1 PR あたりの費用の差。**どちらも測っていない
- **[01](01_inventory_repo.md) と [02](02_inventory_memory_plugins.md) は、4節・5節・6節だけを読んだ。**全文は読んでいない
