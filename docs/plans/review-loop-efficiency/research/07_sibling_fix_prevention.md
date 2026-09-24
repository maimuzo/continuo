# 07 修正漏れの防止（指摘されて直したとき、同様の箇所を直し漏れ、次の周で指摘される）

調べた日: 2026-09-15。リポジトリ側の主張は、この worktree の HEAD `df36f9d7`（未追跡は `docs/plans/review-loop-efficiency/` だけ）で確かめた。

**言いたいこと。**
学術でも実務でも、修正漏れは「不注意」ではなく、**直した場所から見えない所に同じ前提が散っている構造**から生まれる、と繰り返し測られている。
効いている手法は3系統で、(1) 根本原因を1文にしてから元の1件に当たる検索を作り、1要素ずつ広げる（変種分析）、(2) 削除・改名・移動のたびに参照と「一緒に変わるもの」を棚卸しする、(3) 繰り返し出る種類は機械の検査へ移す、である。
2-5（worker-briefing の「1件だけ見て終わるな。同じものが他に無いかを数える」）は(1)の半分を持つが、**削除の行が無い・レビュワーに届かない・数えた結果を修正後に検査しない**の3点で効き切っていない、というのが仮説である。

---

## 0. worker-briefing の「読んだうえで答えさせる問い」への答え

| 問い | 答え |
| --- | --- |
| この作業はどの規則に当てはまるか | 2-4（指示に名前が出ていない関連ファイルも自分で探して読む）、2-5（自分が書く「無い」「件数」にも検索パターン・範囲・件数を添える）、2-2 と [.claude/rules/reporting.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/reporting.md)（名札に意味を貼る・測る前に断定しない・根拠の付け方）、3 の表（公開してよい情報か・英語の技術用語を直訳しない）。2-6 と 2-7 はレビューを頼まれた worker 向けで、この調査には直接は効かない。分析の対象として読んだ |
| 飛ばしてよい段はあるか | worker-briefing に「飛ばしてよい段」の記述は無い。[.claude/rules/design-review.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md) の9段は issue の作業（設計→実装→PR）の段で、調査メモの作成はそれに当たらないと判断した（どの文書も「調査は飛ばしてよい」とは書いていないので、これは私の判断である）。2-5 の段4（2件以上をまとめる）は指摘を出すときの段なので、この作業では使っていない |
| 公開してよくない情報を書きうる場面 | (1) WebFetch が PDF を保存した場所がホームの下の絶対パスだった。本文には書かない。(2) PR のコメントの URL にはリポジトリ名が入る。PR 番号だけで書いた。(3) トークンやホスト名は扱っていない |
| 作業に効く場所をどう探し、何を読んだか | 下の表。読めなかったものは 7・8 節 |

**読んだリポジトリ内のもの（HEAD `df36f9d7`）。**

| 何 | どう探したか |
| --- | --- |
| [.claude/skills/worker-briefing/SKILL.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md) 全文（2-5 は 183-254 行） | 指示のパス。Read |
| [CLAUDE.md:520-528](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L520-L528)（コードレビュー記録フローの手順） | 指示の行番号を `sed -n 495,520p` で開いた |
| 2-5 を指している箇所 | `git grep -n '2-5' -- '.claude/' 'CLAUDE.md'` → 8行（design-review.md:117、reporting.md:572・575、worker-briefing 4行、CLAUDE.md:506） |
| [.claude/skills/pr-review-and-merge/SKILL.md:93-131](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L93-L131)（段2. レビューを回す） | `grep -n -E '^#|数え|同じ誤り|2-5|2-6|前の周'` で見出しと言及を拾ってから開いた |
| [.claude/rules/design-review.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/design-review.md) と [.claude/rules/reporting.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/reporting.md) | 注入された版と worktree の版を読んだ |
| 2-5 が入った commit | `git log -S '1件だけ見て終わるな'` → `63fe82b8 2026-09-04 09:37:44 +0900`、`git log -S '数えるのは「前提」である'` → `d92501b1 2026-09-04 10:14:30 +0900` |
| 別の worker が集めたレビュー結果コメント [research/03_work/code_comments.json](03_work/code_comments.json) | ディレクトリを `ls` して見つけた。**別の worker が 2026-09-15 21:35 に取得したもので、取得範囲が網羅的かは確かめていない**。337件、2026-08-27〜2026-09-15 |

---

## 1. 問題の定義

| 何を | 中身 |
| --- | --- |
| 何が起きているか | レビューで「A が誤り」と指摘され、A を直して次の周へ出すと、**A と同じ前提に立つ別の箇所 B** が Critical / High で指摘される。例: PR #230（本文の題名は未取得）の周で、同じ PR が消した設定キー `rate_limit.pause_above_percent` を、設計文書と要約版の図とコードのコメントが「こうすると」と勧めたまま残っていた（下の 5 節の表） |
| なぜ困るか | 1件の見落としが1周を消費する。2-5 の実例（PR #190。v0.1.14 の案内の「破壊的変更はありません」を直した PR）では、[worker-briefing:221-232](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L221-L232) が「10周のうち5周が、この1つの失敗で潰れた」と書いている |
| いま何を決めるのか | この文書は材料である。決めるのはオーケストレーターと人間。**下の「当てはめ」は案であって決定ではない** |

---

## 2. 手法の一覧（詳細は 3 節）

| 手法 | 入れる側 | 効果の実測 |
| --- | --- | --- |
| 変種分析（根本原因→元の1件に当たる検索→1要素ずつ広げる→全件を判定） | 書く側（直す前）。レビュワーにも使える | CodeQL で CVE 400件超（件数の出典は GitHub の検索要約のみ）、Semgrep の例で1件から6件。**「漏れを何割減らしたか」の比較実測は見つけていない** |
| 2件以上の例から一般化してから探す | 書く側 | LASE: 精度99%・再現率89%。1例だけからだと偽陽性・偽陰性が多いと明記 |
| 同じファイル・同じ節・一緒に変わる履歴を見る（空間の局所性・co-change） | 書く側 | 繰り返しの修正の70-94%が同じファイル（Yue ら）。ROSE は上位3件に正解が入る率70% |
| 削除・改名のたびに、残った参照を探す | 書く側（最初の変更の時点） | Tan らが3,000超のプロジェクトで「ほとんどが一度は古い参照を持つ」と測定。修正漏れの減少率は測っていない |
| 繰り返す種類を検査・hook・テストへ移す（種類ごと塞ぐ） | 機械（書く側の出す前、CI） | Google の Tricorder で「後戻りを防ぐのに有効」、Safe Coding で XSS をほぼ根絶（検索要約。一次の数値は未確認） |
| 根本原因を1文にしてから直す | 書く側 | Project Zero: 2022年上半期の 0-day のうち少なくとも9件が過去に直した脆弱性の変種。5 Whys には効果の根拠が無いという批判がある |
| 写しを減らす（単一の正・リンクで指す） | 文書を書く側 | クローンの不整合の約半数が障害（Juergens ら。コードの話）。文書での実測は見つけていない |
| 修正後に同じ検索を再実行し、残り件数を証拠として出す | 書く側。レビュワーが再実行してもよい | Claude Code の公式が「成功を主張するより証拠を見せる」と勧める。減少率の実測は無い |

---

## 3. 手法ごとの詳細

**凡例。**「開いた」は自分で WebFetch か gh で本文を取った。「要約」は WebFetch の要約モデルが返した文で、原文との一字一句の一致は確かめていない。PDF は Read で画像にできず（`pdftoppm is not installed`）、Python の zlib で本文テキストを抜いた。抜いた本文は語の間の空白と合字（fi / fl）が落ちるので、引用では空白と合字を戻した。

### 3-1. 修正漏れの実証研究

**言いたいこと。**最初の修正の2〜3割が不完全で、漏れた箇所は「似たコード」だけでは見つからない。散らばった大きな修正ほど漏れる。

| 研究 | 数値と条件 | 開いたか |
| --- | --- | --- |
| Park, Kim, Ray, Bae「An empirical study of supplementary bug fixes」MSR 2012（[IEEE](https://ieeexplore.ieee.org/document/6224298)、発表スライド [SlideShare](https://www.slideshare.net/huni7595/msr2012-an-empirical-study-of-supplementary-bug-fixes)） | Eclipse JDT core・SWT・Mozilla で「22% ~ 33% bugs require supplementary bug fixes」。不完全なパッチは「larger in size, and more scattered」。「About 15% of supplementary change locations are beyond the scope」（直接の隣より遠い）。「Predicting a supplementary fix location using code clone analysis alone is insufficient」。原因の内訳（他の部品への移植漏れ 28%・条件文 23%・依存する要素の更新漏れ 15%）は要約 | スライドの本文は要約で取得。論文本体は開けていない（8 節） |
| Yue, Meng, Wang「A Characterization Study of Repeated Bug Fixes」ICSME 2017（[PDF](https://people.cs.vt.edu/nm8247/publications/ruru-icsme-2017-camera-ready.pdf)） | 3プロジェクト。「15-20% of bugs involved repeated fixes」「86%, 99%, and 91% of repeated-fix groups were purely applied to code within the same package, while 70%, 94%, and 86% of groups were applied to the same file」「repeated fixes are not confined to code clones」 | 開いた（PDF の本文を抽出） |
| Wang, Meng, Zhong「An Empirical Study of Multi-Entity Changes in Real Bug Fixes」ICSME 2018（[PDF](https://people.cs.vt.edu/nm8247/publications/ye-icsme2018-camera-ready.pdf)） | 4プロジェクト 2,854件。「52-58% of bug fixes involved multi-entity changes」、一緒に変わる組は「invoked the same methods, accessed the same fields, or contained similar content」 | 開いた（抽出） |
| Nguyen ら「Recurring bug fixes in object-oriented programs」ICSE 2010（FixWizard。[ACM](https://dl.acm.org/doi/10.1145/1806799.1806847)） | 繰り返しの修正は役割の似たクラス・メソッド（code peers）に多い。数値「17-45% bug fixes were recurring」は Yue ら 2017 の引用で確かめた | 本体は開いていない |
| Juergens ら「Do Code Clones Matter?」ICSE 2009（[arXiv](https://arxiv.org/abs/1701.05472)、発表資料 [PDF](https://www.cs.uoregon.edu/events/icse2009/images/postPosters/Do%20Code%20Clones%20Matter.pdf)） | 5システム（C# 3・COBOL・Java）。平均で、クローン群の52%が不整合、不整合の28%が意図しないもの、不整合の15%・意図しない不整合の50%が障害。障害は合計107件（表の和を自分で計算して一致を確かめた）。結論「Every second unintentional inconsistency constitutes a fault.」 | 開いた（発表資料の抽出と arXiv の要旨） |

**引用と訳。**

> Park ら（スライド）: "Predicting a supplementary fix location using code clone analysis alone is insufficient"
> 訳: **補足の修正が要る場所を、コードクローンの分析だけで予測するのは不十分である。**

> Yue ら: "When clone detection techniques are not sufficient to reveal all locations in need of similar edits, we may need new approaches that also leverage the spatial locality characteristics to suggest edit locations."
> 訳: クローン検出だけで似た修正が要る場所を全部出せないなら、**空間的な局所性（同じファイル・同じパッケージ）も使って場所を示す方法**が要るかもしれない。

> Juergens ら: "Every second unintentional inconsistency constitutes a fault."
> 訳: **意図しない不整合の2つに1つが障害である。**

- 入れる側: 書く側（直す前の探索範囲の決め方）。
- 制約: 当たらない（考え方だけ使う）。
- 当てはめ案（決定ではない）: 2-5 の段2 の「範囲」を、文字列一致の全体検索に加えて「**直した行と同じファイル・同じ節を頭から読む**」の2本立てにする。Yue らの局所性と Park らの「クローンだけでは不十分」が根拠。

### 3-2. LLM とエージェントでの複数箇所の修正

**言いたいこと。**LLM も人と同じく、修正箇所が散らばるほど失敗する。テストが通ることは完全さの証拠にならない。

| 研究 | 数値と条件 | 開いたか |
| --- | --- | --- |
| Nashid ら「Characterizing Multi-Hunk Patches: Divergence, Proximity, and LLM Repair Challenges」ASE 2025（[arXiv 2506.04418](https://arxiv.org/abs/2506.04418)、2025-06-04 投稿） | 372件の実バグ（Hunk4J）、6つの LLM。「model success rates decline with increased divergence and spatial dispersion」。最も散らばった群は、補助なしではどのモデルも1件も直せなかった | 要旨を要約で取得 |
| Nashid ら「Beyond Accuracy: Behavioral Dynamics of Agentic Multi-Hunk Repair」（[arXiv 2511.11012](https://arxiv.org/abs/2511.11012)、v2 2026-06-05） | 4エージェント・404件。修正の正答率 26.98%〜92.82%（最高は Claude Code）。失敗した修正は成功より 33%〜440% 多くトークンを使った | 要旨を要約で取得 |
| Liu ら「SiblingRepair」（[arXiv 2605.06209](https://arxiv.org/abs/2605.06209)、2026-05-07） | 関連する機能を実装する複数箇所（siblings）に同じ誤りがある前提で、トークンと埋め込みで sibling を探して一貫して直す | 要旨を要約で取得 |
| Wang, Pradel, Liu「Are "Solved Issues" in SWE-bench Really Solved Correctly?」（[arXiv 2503.15223](https://arxiv.org/abs/2503.15223)、v2 2025-09-09） | テストを通った修正の29.6%が、正解の修正と違う振る舞いをした | 要旨を開いた |

> SiblingRepair: "Developers often make similar mistakes across code locations implementing related functionalities. These locations, called siblings, share similar issues and require similar fixes."
> 訳: 開発者は、関連する機能を実装する複数の場所で似た誤りをしがちである。**そうした場所（siblings）は同じ問題を持ち、同じ直しを要する。**

- 入れる側: 書く側。
- 制約: SiblingRepair の埋め込み検索はツール導入に当たるので使わない。「同じ機能を実装している別の場所を先に列挙する」という考え方だけ借りる。
- 当てはめ案: 直す前に「この誤りと同じ役割を持つ箇所（同じ設定キーを読む場所・同じ規則を要約している場所）」を列挙させる段を置く。

### 3-3. 変種分析（1件の誤りから、同じ種類を全部探す）

**言いたいこと。**セキュリティの世界では「1件見つけたら同じ誤りは他にもある」が前提で、手順が確立している。要は、**根本原因を先に言葉にし、元の1件に確実に当たる検索から始めて、1要素ずつ広げ、ノイズが増えたら止める**ことである。

| 出典 | 何をするか | 開いたか |
| --- | --- | --- |
| Google Project Zero, Maddie Stone「2022 0-day In-the-Wild Exploitation…so far」2022-06-30（[URL](https://projectzero.google/2022/06/2022-0-day-in-wild-exploitationso-far.html)） | 「At least nine of the 0-days are variants of previously patched vulnerabilities.」PoC の経路だけを塞ぎ、根本原因を直していなかった例 | 要約で取得 |
| 同 Stone「Déjà vu-lnerability」2021-02-03（[URL](https://projectzero.google/2021/02/deja-vu-lnerability.html)） | 「25% of the 0-days detected in 2020 are closely related to previously publicly disclosed vulnerabilities.」 | 要約で取得 |
| GitHub Security Lab, Sylwia Budzynska「CodeQL zero to hero part 3」2024-04-29（2025-10-07 更新）（[URL](https://github.blog/security/vulnerability-research/codeql-zero-to-hero-part-3-security-research-with-codeql/)） | 既知の1件を基にして、そのパターンを検索として書き、偽陽性を減らしながら広げる | 要約で取得 |
| Semgrep, Eugene Lim「Finding More Zero Days Through Variant Analysis」2025-07-10（[URL](https://semgrep.dev/blog/2025/finding-more-zero-days-through-variant-analysis/)） | 修正の diff と勧告から原因を理解し、元の1行に完全一致する規則から「slowly generalize」。例では元の2件を含む6件が出た | 要約で取得 |
| Trail of Bits「variant-analysis」skill（Claude Code 用の公開 skill。[GitHub](https://github.com/trailofbits/skills/tree/main/plugins/variant-analysis/skills/variant-analysis)、2026-09-15 の main） | 5段: 根本原因 → 元の1件にだけ当たる検索（当たらなければ誤りを理解していない証拠）→ 1要素ずつ一般化 → 「Stop when more than half the matches are noise」→ 候補を1件ずつ判定。よくある失敗に「元のモジュールだけを探す」「具体的すぎる検索で仲間を逃す」 | SKILL.md を要約で取得。全文の一字一句は読んでいない |

> Project Zero 2022: "Performing a root cause analysis can help ensure that a fix is addressing the underlying vulnerability and not just breaking the proof-of-concept."
> 訳: 根本原因の分析をすると、**修正が PoC を壊しただけでなく、根にある脆弱性を直している**ことを確かめやすくなる。

> Semgrep: "If a developer made a mistake in their code that caused a vulnerability, they likely made that mistake elsewhere in the codebase, too."
> 訳: 開発者が脆弱性の原因になる誤りをしたなら、**同じ誤りをコードベースの他の場所でもしている可能性が高い。**

> Trail of Bits: "Stop when more than half the matches are noise"
> 訳: **当たったものの半分より多くがノイズになったら、広げるのをやめる。**

- 効果の実測: CodeQL の変種分析で「400件を超える CVE」（GitHub の検索要約。一次の数値は開いていない）。**修正漏れの減少率を、手順の有無で比べた実測は見つけていない。**
- 入れる側: 書く側（直す前）。レビュワーが指摘を出す前にも使える。
- 制約: CodeQL・Semgrep は導入しない。`git grep` と Grep ツールで同じ段取りを踏む。
- 当てはめ案: 2-5 の段1〜3 は「特徴的な文字列を1つ決めて全部検索し、件数を書く」である。これに **「元の1件に当たることを確かめる」「1要素ずつ広げた版を2〜3本叩き、それぞれの件数を並べる」「半分がノイズになったら止める」** を足す。いまの段1 は1本の文字列しか求めていない。

### 3-4. 体系的な編集の伝播

**言いたいこと。**「似ているが同一ではない変更を多くの場所へ当てる」研究は、**1例からの一般化は外れやすく、2例以上から共通部分を取ると当たる**こと、**履歴で一緒に変わったものが次も一緒に変わる**ことを示している。

| 出典 | 数値と条件 | 開いたか |
| --- | --- | --- |
| Meng, Kim, McKinley「LASE: Locating and Applying Systematic Edits by Learning from Examples」ICSE 2013（[PDF](https://web.cs.ucla.edu/~miryung/Publications/icse2013-lase-submitted.pdf)） | 補足の修正が要った Eclipse JDT・SWT の実例を正解にして、場所の特定が精度99%・再現率89%、変換の正確さ91%。開発者が見落とした場所を見つけたと開発者本人に確認 | 開いた（抽出） |
| Rolim ら「Learning Syntactic Program Transformations from Examples」（Refazer）ICSE 2017（[arXiv](https://arxiv.org/abs/1608.09000)） | 例の修正から変換を学び、720人の学生の課題で87%の学生の誤答を直した | 検索要約のみ |
| Zimmermann, Weißgerber, Diehl, Zeller「Mining Version Histories to Guide Software Changes」ICSE 2004（[PDF](https://thomas-zimmermann.com/publications/files/zimmermann-icse-2004.pdf)） | 8つの OSS・10,761 トランザクション。1ファイルの変更から、同じトランザクションで変わったファイルの26%を予測。70%のトランザクションで上位3件に正しい場所が入る。例に「設定を足したら HTML の文書も更新すべき」（信頼度 0.75）を挙げている | 開いた（抽出） |
| Hyrum Wright「Software Engineering at Google」22章 Large-Scale Changes（[URL](https://abseil.io/resources/swe-book/html/ch22.html)） | 意味の索引（Kythe）で「この関数の呼び出し元はどこか」を全部引く。古い書き方の新しい使用をレビュー時に Tricorder が指摘し、後戻りを防ぐ | 要約で取得 |

> LASE: "edit scripts created from only one example produce too many false positives, false negatives, or both."
> 訳: **1つの例だけから作った編集手順は、偽陽性か偽陰性、あるいはその両方が多すぎる。**

> ROSE: "Programmers who changed these functions also changed ...."
> 訳: この関数を変えたプログラマは、こちらも変えている……

> Wright: "We use the Tricorder framework...to flag at review time when an engineer introduces a new use of a deprecated object, and this has proven an effective method to prevent backsliding."
> 訳: 非推奨のものを新たに使ったら、Tricorder がレビュー時に知らせる。**これは後戻りを防ぐ有効な方法だと分かっている。**

- 入れる側: 書く側。
- 制約: LASE・Refazer・Kythe は導入しない。ROSE の考え方は `git log --name-only` だけで再現できる（道具を足さない）。
- 当てはめ案: (1) 指摘を受けたら「元の1件」と「直す過程で見つけたもう1件」の**2件の共通部分**から検索パターンを作る（LASE）。(2) 直すファイルについて `git log --format= --name-only -- <ファイル> | sort | uniq -c | sort -rn | head` で**過去に一緒に変わったファイル**を出し、そこも開く（ROSE。設計文書と要約版、コードと FAQ のような組が出るはず。**このコマンドはまだ叩いていない**）。

### 3-5. エージェントの指示書は、修正漏れをどう書いているか

**言いたいこと。**公開されている指示書の多くは「根本原因を直せ」「検証の手段を渡せ」までで、**「同じ誤りを全部探せ」を手順として書いたものは、読んだ範囲では Trail of Bits の skill だけ**だった（探した検索パターンと対象は 7 節）。Anthropic の公開文書は、指示を増やすより検査（hook・テスト）へ移すことを勧めている。

| 出典 | 修正漏れに関わる記述 | 開いたか |
| --- | --- | --- |
| Claude Code「Best practices for Claude Code」（[URL](https://code.claude.com/docs/en/best-practices)、2026-09-15 取得） | 「Address root causes, not symptoms」。検証の手段を渡す。hook は決定的で、CLAUDE.md の指示は助言にすぎない。長い CLAUDE.md は無視される | 全文を開いた |
| Claude「Prompting best practices」（[URL](https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/claude-prompting-best-practices)） | テストの入力だけでなく全ての正しい入力で動く一般解を書く。開いていないコードについて推測しない | 全文を開いた |
| Claude「Prompting Claude Opus 5」（[URL](https://platform.claude.com/docs/en/build-with-claude/prompt-engineering/prompting-claude-opus-5)） | 検証や再確認を指示すると過剰な検証になる。レビューの指示で「高い重大度だけ報告」と書くと少なく報告するので、全部出させて別の段で絞る | 全文を開いた |
| Claude Code「Code Review」の「Review a diff locally」（[URL](https://code.claude.com/docs/en/code-review)） | `/code-review` は CLAUDE.md に従うが REVIEW.md は読まない。`low` と `medium` は確信の高い指摘だけ。レベルを打たないと前回打ったレベルを使い回す | 全文を開いた |
| anthropics/claude-code の code-review プラグイン（[plugins/code-review/commands/code-review.md](https://github.com/anthropics/claude-code/blob/main/plugins/code-review/commands/code-review.md)、最終 commit 2026-03-12） | バグを探す役に「Focus only on the diff itself without reading extra context.」。偽陽性の一覧に「Pre-existing issues」。**Claude Code に同梱の `/code-review` とは別物で、同梱版の中身は確かめていない**（本体の解析は禁止） | gh で本文を開いた |
| openai/codex の既定の指示（[codex-rs/protocol/src/prompts/base_instructions/default.md](https://github.com/openai/codex/blob/main/codex-rs/protocol/src/prompts/base_instructions/default.md) 136-140 行、2026-09-15 の main） | 「Fix the problem at the root cause rather than applying surface-level patches, when possible.」「Do not attempt to fix unrelated bugs or broken tests.」 | gh で本文を開き grep した |
| SWE-agent（[config/default.yaml](https://github.com/SWE-agent/SWE-agent/blob/main/config/default.yaml)） | 「5. Think about edgecases and make sure your fix handles them as well」。提出時に再現スクリプトを再実行させる | gh で本文を開いた |

> Claude Code best practices: "Unlike CLAUDE.md instructions which are advisory, hooks are deterministic and guarantee the action happens."
> 訳: CLAUDE.md の指示は助言にすぎないが、**hook は決定的で、その動作が必ず起きることを保証する。**

> 同: "If Claude keeps doing something you don't want despite having a rule against it, the file is probably too long and the rule is getting lost."
> 訳: 禁じる規則があるのに Claude が同じことを続けるなら、**ファイルが長すぎて規則が埋もれている**可能性が高い。

> 同: "Have Claude show evidence rather than asserting success: the test output, the command it ran and what it returned"
> 訳: **成功を主張させるのではなく、証拠を見せさせる。**テストの出力、叩いたコマンドとその戻り値。

> Opus 5: "If your review prompt says "only report high-severity issues" or "be conservative," the model may follow that instruction literally and report less; ask it to report everything and filter in a separate pass instead."
> 訳: レビューの指示に「高い重大度だけ報告」「控えめに」と書くと、文字どおりに従って少なく報告することがある。**全部を報告させ、絞るのは別の段で行う。**

> Code Review 文書: "At `low` and `medium`, the review reports only the findings it's most confident in, so you see fewer false positives; `high` through `max` broaden coverage"
> 訳: **`low` と `medium` では、いちばん確信のある指摘だけを報告する。**`high` から `max` は範囲を広げる。

> Codex: "Fix the problem at the root cause rather than applying surface-level patches, when possible."
> 訳: 可能なら、表面的なパッチではなく**根本原因で問題を直す**。

- 入れる側: 書く側（指示書）とレビュー側（effort と報告の絞り方）。
- 制約: 当たらない。
- 当てはめ案: (1) `/code-review` を回すときは effort を毎回明示する（前回の値の使い回しを避ける）。(2) 規則の文章を足す前に、同じ内容を hook か検査スクリプトで止められないかを先に問う。

### 3-6. 種類ごと塞ぐ（1件直すたびに、同じ種類を機械で止める）

**言いたいこと。**大規模な組織は「個々のバグを探して直す」より「その種類が書けない・入れば検査で落ちる」形へ移している。

| 出典 | 中身 | 開いたか |
| --- | --- | --- |
| Christoph Kern「Safe Coding」ACM Queue 23(5), 2025（[Google Research](https://research.google/pubs/safe-coding-rigorous-modular-reasoning-about-software-safety/)） | 「systematically eliminating the direct use of risky operations—those with complex safety preconditions—in application code.」XSS をほぼ根絶（検索要約。Queue と CACM の本文は 403 で開けていない） | Google Research の要旨を要約で取得 |
| John Lunney, Sue Lueder「Postmortem Culture」Google SRE book（[URL](https://sre.google/sre-book/postmortem-culture/)） | 事後検証の目的は、原因の理解と「effective preventive actions are put in place to reduce the likelihood and/or impact of recurrence」 | 要約で取得 |
| Wright（3-4 の Tricorder） | レビュー時の検査で後戻りを防ぐ | 同上 |
| このリポジトリの既存の例 | [.claude/rules/plan-file.md](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/rules/plan-file.md) の「設計文書へ1行でも足したら、そこを指すリンクを全部検算する」の検査スクリプト。行番号リンクのずれという**種類**を機械で拾っている | リポジトリで読んだ |

> SRE book: "effective preventive actions are put in place to reduce the likelihood and/or impact of recurrence."
> 訳: **再発の可能性や影響を減らす、効果のある予防策を入れる**（ことが事後検証の目的である）。

- 効果の実測: Tricorder は「有効」とあるが数値は取っていない。Safe Coding の数値は一次で確かめていない。
- 入れる側: 機械（書く側の出す前と CI）。
- 制約: 当たらない（Go のテスト・Python の hook・`git grep` で書ける）。
- 当てはめ案: 5 節の観測で多かった「消した設定キー・関数への参照の残存」は、**消した名前を PR の説明か1つのファイルに書き、`git grep -n -F` で0件を確かめる検査**にできる。行番号リンクの検算と同じ形である。

### 3-7. 根本原因を1文にする

**言いたいこと。**「前提を1文にする」は根本原因分析の一種である。ただし定番の 5 Whys には効果の根拠が無く、症状で止まる・人によって答えが違うと批判されている。**1文にするだけでなく、その1文から検索を作り、結果を検査される形にしないと、頭の中で閉じる。**

| 出典 | 中身 | 開いたか |
| --- | --- | --- |
| Project Zero（3-3） | PoC の経路だけを塞いで根本原因を直さなかった修正が、変種として再利用された | 同上 |
| Alan J. Card「The problem with '5 whys'」BMJ Quality & Safety 26(8):671-677, 2017（DOI 10.1136/bmjqs-2016-005849） | 5 Whys の普及は効果の証拠によるものではない、と論じて放棄を勧めた | 書誌は Europe PMC で確かめた。要旨は取れず、主張は検索要約と Wikipedia による |
| Wikipedia「Five whys」の Criticism（[URL](https://en.wikipedia.org/wiki/Five_whys)） | トヨタの元幹部 Teruyuki Minoura:「too basic a tool to analyze root causes at the depth necessary to ensure an issue is fixed」。症状で止まる、調べる人の知識の外に出られない、人によって結論が違う、原因を1つに絞りがち | 要約で取得 |

> Minoura（Wikipedia 経由）: "too basic a tool to analyze root causes at the depth necessary to ensure an issue is fixed"
> 訳: 問題が確実に直るのに必要な深さで根本原因を分析するには、**単純すぎる道具である。**

- 入れる側: 書く側。
- 当てはめ案: 2-5 の「前提を1文にする」の結果（1文・そこから作った検索パターン・件数）を**対応表の列として残させる**。いまの [CLAUDE.md:525](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L525) が PR のコメントへ求めているのは「数えた件数・叩いた検索パターン・範囲」で、前提の1文は求めていない。

### 3-8. 文書での横展開（写しを減らす・参照の残りを機械で探す）

**言いたいこと。**文書の修正漏れは「同じ事実が2箇所以上に書いてある」ことと「消したものへの参照が残る」ことから生まれる。前者は写しを減らし、後者は機械で探す。

| 出典 | 中身 | 開いたか |
| --- | --- | --- |
| Tan, Wagner, Treude「Detecting outdated code element references in software repository documentation」Empirical Software Engineering 2024（[arXiv 2212.01479](https://arxiv.org/abs/2212.01479)） | ソースから全部消えたのに文書に残ったコード要素の参照を自動で検出。3,000超の GitHub プロジェクトで、ほとんどが履歴のどこかで少なくとも1件の古い参照を持っていた | 要旨を開いた |
| 単一の正（Single Source of Truth）と transclusion（[Wikipedia: Single source of truth](https://en.wikipedia.org/wiki/Single_source_of_truth)、[Transclusion](https://en.wikipedia.org/wiki/Transclusion)、文書ツールの企業ブログ） | 1つの権威ある版を持ち、他は参照か埋め込みで使う。「写した事実は食い違う」 | 検索要約のみ。企業ブログは宣伝を含むので、根拠としては弱い |
| Juergens ら（3-1） | コードの写しの不整合の約半数が障害。文書に当てはまるかは測られていない | 同上 |

> Tan ら: "we propose an approach that can automatically detect code element references that survive in the documentation after all source code instances have been deleted."
> 訳: **ソースコードから全部消えたあとも文書に生き残っている、コード要素への参照**を自動で検出する方法を提案する。

- 入れる側: 文書を書く側と機械。
- 当てはめ案: (1) 「ここには写さない」と書きつつ「要点だけ」を写している箇所を、リンクだけに縮める（数は 4 節の仮説6）。(2) 設定キー・関数名を消した PR では、その名前で `git grep` して0件を示す（3-6 と同じ）。

---

## 4. 2-5 がなぜ効き切っていないか（仮説）

**言いたいこと。**2-5 は「指摘を受けて直す前に、文字列と前提で数える」を持っている。それでも漏れるのは、**数える段が遅い・数える種類の表に削除が無い・レビュワーに届かない・数えた結果が修正後に検査されない・写しが多い**ためだと考える。**以下はすべて仮説で、確かめていない。**

### 4-1. 手元のデータで見たこと（仮説の材料）

**データ。**[research/03_work/code_comments.json](03_work/code_comments.json)（別の worker が取得。レビュー結果のコメント337件）。時期は UTC の `created` で分けた（2-5 が入ったのは 2026-09-04 09:37 JST ＝ 00:37 UTC）。すべて正規表現での粗い数え方で、言い換えは拾えない。

| 数えたもの | 〜09-03 | 09-04〜05 | 09-06〜 |
| --- | --- | --- | --- |
| コメント数 | 123 | 125 | 89 |
| 「数えた件数」系の記録を含むコメント（`数えた件数\|件数.{0,10}検索パターン\|検索パターン`） | 7 | 103 | 57 |
| 表の Critical / High の行 | 94 | 328 | 285 |
| そのうち取り残し系の語（`取り残\|直し漏れ\|修正漏れ\|同じ誤り\|もう1箇所\|他の箇所\|別の箇所\|残っていた\|残っている`）を含む行 | 7 | 17 | 14 |

**読み取り。**2-5 が入ってから、数えた記録はコメントの大半に付くようになった。それでも取り残し系の Critical / High の行は0にならない。**「数えていない」より「数え方が当たっていない」を疑うほうが合う。**ただし語で拾えない漏れ（前提だけを共有するもの）は、この表に出ていない。

**09-06 以降の14行を1行ずつ読んで、自分で分けた**（分け方は私の判断。PR 番号の題名は取得していない）。

| 形 | 行 | 件数 |
| --- | --- | --- |
| 消した設定・処理・門を、別の場所（設計文書・要約版・コメント・組み込みの指示書）が残して勧めている | PR #230 の3行、PR #254、PR #271 の1行、PR #230 の要約版の1行 | 6 |
| 写し（PR の本文・要約版の図）が HEAD と合っていない | PR #256 の本文の表（ほかに要約版の1行が上と重なる） | 1（重なりを除く） |
| 対応表で「直す」と名指しした箇所が未編集 | PR #256 | 1 |
| 見出しだけ変えて中身が残った | PR #266 | 1 |
| sibling の取り残しではないもの（コードの論理の穴） | PR #230 の2行、PR #267 の1行 | 3 |
| 指摘の時点で同じ誤りを数え済みのもの（「同じ誤りは1件だけだった」「同じ誤りが10箇所ある」と書いてある） | PR #267 の1行、PR #271 の1行 | 2 |

### 4-2. 仮説と、確かめるなら何を測るか

| 仮説 | 根拠 | 確かめるなら何を測るか |
| --- | --- | --- |
| **1. 数える種類の表に「消した・やめた・名前を変えた」の行が無い** | [worker-briefing:213-219](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L213-L219) の表は「言い回し・grep のパターン・節の移動・版の名乗り・行番号リンク」の5行。`git grep -n -E '消した\|削除した\|やめた' -- .claude/skills/worker-briefing/SKILL.md`（HEAD `df36f9d7`）は0件。4-1 の14行のうち6行が削除の取り残し。Tan らの「消したあとも文書に残る参照」、Park らの「依存する要素の更新漏れ」と同じ形 | 周をまたいだ Critical / High の取り残しを全 PR で「削除・改名・移動・言い回し・コード」に分け、表の5行で拾える割合を出す |
| **2. 数える段が「指摘を受けたあと」にしかなく、最初の変更を書く段に無い** | 2-5 は「指摘する前・直す前に」数えると書く（[worker-briefing:185](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L185)）。1周目より前、設定キーを消した最初の commit で参照を棚卸しする段は無い。Reason は、主な目的を達したあとの段が抜けやすいと書く（4つの特徴の一覧は本文が取れず、確かめていない） | 取り残しの行が、1周目の時点で既にあったか（最初の変更が残したか）を `git show <1周目の commit>:<ファイル>` で調べ、割合を出す |
| **3. `/code-review` のレビュワーには 2-5 が届かず、差分の外の sibling は見られにくい** | [pr-review-and-merge:99](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/pr-review-and-merge/SKILL.md#L99)「`/code-review` は自由なプロンプトを足せない」、[CLAUDE.md:308](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L308)「`.claude/rules/` と `.claude/skills/` の下のファイルは自動では読まれない」。公式文書は `/code-review` が CLAUDE.md に従うと書くが、CLAUDE.md の段1 は 2-5 へのリンクだけである。`low` / `medium` は確信の高い指摘だけで、レベルは前回の値を使い回す。プラグイン版は「差分だけを見よ」「既存の問題は偽陽性」と書く（同梱版は未確認）。差分の外にある sibling は、直しが近くを触って差分へ入った周に初めて指摘される、という流れが起きうる | 取り残しの行について、指摘された周の1つ前の周の差分（`git diff <base>...<その周の commit>`）にその行が入っていたかを数える。入っていなかった割合が高ければ支持。あわせて各周で使った effort を記録から拾う（記録が無ければ、それ自体が分かったこと） |
| **4. 数えた結果が、修正後に検査されていない** | 数えた記録はコメントの大半に付く（4-1）。しかし PR #256 では、対応表が「直す」と名指しした2箇所が2件とも未編集のまま次の周へ出た。CLAUDE.md の段3「段1で数えた件数の全部を直す」を、機械も次の周も確かめていない。Claude Code の公式は「成功を主張させず証拠を見せさせる」 | 対応表に書かれた検索パターンを、その周の修正 commit で叩き直し、残り件数が0でない割合を出す |
| **5. 「前提を1文にする」が本人の頭の中で閉じている** | 2-5 は前提の言い直しを求めるが（[worker-briefing:237-254](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/.claude/skills/worker-briefing/SKILL.md#L237-L254)）、PR のコメントへ残すのは件数・パターン・範囲だけ（[CLAUDE.md:525](https://github.com/maimuzo/continuo/blob/f86a4acdc006029c1a61b058ccd728c54ba9cb0d/CLAUDE.md#L525)）。前提の1文は検査されない。Huang らは、外からの手掛かり無しの自己修正はうまくいかないと測っている | コメントの表の行のうち、前提の1文が書かれた行の割合と、その行の指摘が次の周で取り残しとして再び出た割合を比べる |
| **6. 同じ前提が多くの場所に写されていること自体が、漏れの面を作っている** | `git grep -c` で「ここには写さない」12件（design-review.md 4・reporting.md 2・pr-review-and-merge 3・worker-briefing 3）、「要点だけ」5件（design-review.md 4・pr-review-and-merge 1）。4-1 で写し（要約版・PR の本文）の取り残しが出ている。Juergens らは「意図しない不整合の2つに1つが障害」と測る。別の worker が写しの数を正確に数えているので、この数は目安 | 取り残しの行が「正（元の文）」と「写し（要約・本文・要約版）」のどちらにあったかを数える |
| **7. 文字列1本の検索は、構造上ぜんぶには届かない** | 2-5 の段1 は「いちばん短い特徴的な文字列」を1つ決める。LASE は1例からの一般化は外れやすいと書き、Park らはクローン分析だけでは不十分、Yue らは繰り返しの修正がクローンに閉じないと測った | 取り残しの行について、その前の周の対応表に書いたパターンで、その行が当たったかを叩き直して数える。当たらなかった割合が高ければ支持 |
| **8. 周を重ねること自体が漏れを増やしている** | 09-06 以降の表の行で、分類が「直しが持ち込んだ」だけの行が211、「前の周に既に在った」だけの行が143（正規表現。Medium と Low の行も含み、「持ち込んだ」の証明の有無は見ていない）。CLAUDE.md は「8周目の Critical が7周目の直しから生まれた」例を持つ | 「直しが持ち込んだ」行について、前の周の commit でその行が違っていたかを `git blame` で確かめ、本当に持ち込まれた割合を出す |

---

## 5. 当てはめの候補（案であって決定ではない）

**言いたいこと。**文章の規則を足すより、**段の位置を早める・削除の行を足す・数えた結果を機械で再検査する・写しを減らす**の4つが、集めた知見と観測に合う。どれを採るかはオーケストレーターと人間が決める。

| 案 | 入れる側 | 根拠 | 採らないと続くこと |
| --- | --- | --- | --- |
| 2-5 の「何を直したか→何を数えるか」表に「設定キー・関数・節・処理を消した／やめた／名前を変えた → その名前を含む文とコード全部（設計文書・要約版・コメント・組み込みの指示書を含む）」の行を足す | 書く側 | Tan ら、Park らの依存の更新漏れ、4-1 の6行 | 消したものを勧める文が、別の文書で残り続ける |
| 削除・改名・移動をした commit を出す前（1周目より前）に、同じ棚卸しをする | 書く側 | Reason の省略エラー、仮説2 | 1周目のレビューが取り残しの発見に使われる |
| 検索パターンを1本ではなく、元の1件に当たる版から1要素ずつ広げた2〜3本にし、件数を並べる | 書く側 | Trail of Bits・Semgrep・CodeQL、LASE | 言い回しの違う sibling が、次の周まで残る |
| 直すファイルと同じファイル・同じ節を読み、`git log` で一緒に変わってきたファイルも開く | 書く側 | Yue らの局所性、ROSE | 文字列を共有しない要約版やコメントが残る |
| 修正 commit で、対応表のパターンを叩き直して残り件数を表に並べる（hook か検査スクリプトにできる） | 書く側と機械 | Claude Code の「証拠を見せる」「hook は決定的」、仮説4 | 「直す」と書いた箇所が未編集のまま次の周へ出る |
| `/code-review` の effort を毎回明示し、差分の外の sibling は別のレビュワー（Agent で立て、前の周のパターンを渡す）に探させる | レビュー側 | Code Review 文書の effort の説明、Opus 5 の「全部を報告させて別の段で絞る」、仮説3 | 差分に入るまで sibling が指摘されない |
| 「要点だけ」の写しをリンクに縮める | 文書を書く側 | 単一の正、Juergens ら、仮説6 | 正を直すたびに写しの取り残しが1周を使う |

---

## 6. 否定的な知見

| 知見 | 出典 | この件への含意 |
| --- | --- | --- |
| **チェックリストは点検の成果を有意に変えなかった。**308件の点検。1人で平均53%、2人組で平均76%の欠陥を見つけた | Les Hatton「Testing the value of checklists in code inspections」IEEE Software 25(4), 2008（[PDF](https://www.leshatton.org/Documents/checklists_in_inspections.pdf)。開いた・抽出） | 2-5 の表に行を足すだけでは効かない可能性がある。人を足す（別のレビュワー）ほうが数値の差が出ている |
| **外からの手掛かりが無い自己修正は、うまくいかず、悪化することもある** | Huang ら「Large Language Models Cannot Self-Correct Reasoning Yet」ICLR 2024（[arXiv 2310.01798](https://arxiv.org/abs/2310.01798)。要旨を開いた） | 「前提を1文にして自分で探せ」だけでは弱い。検索の件数やテストのような外の信号と組にする |
| **Opus 5 に検証や再確認を指示すると、品質を上げずにトークンだけ増える** | Prompting Claude Opus 5（開いた） | 「もう一度確かめろ」型の文を規則に足す案は、効果が無いうえに費用を増やしうる |
| **長い CLAUDE.md は規則が埋もれて無視される。レビュワーは頼めば何か指摘し、全部を追うと過剰な設計になる** | Claude Code best practices（開いた） | 2-5 を長くする方向は逆効果になりうる。指摘の全件対応も周を増やす |
| **5 Whys には効果の根拠が無く、症状で止まり、人によって結論が違う** | Card 2017、Minoura（Wikipedia 経由） | 「前提を1文にする」を 5 Whys 型の自問にすると、同じ弱点を持つ |
| **1例からの一般化は偽陽性・偽陰性が多い** | LASE（開いた） | 指摘された1件だけから検索パターンを作る 2-5 の段1 は、この弱点を持つ |
| **補足の修正の場所の約15%は直接の隣より遠く、クローン分析だけでは足りない。繰り返しの修正はクローンに閉じない** | Park ら（スライド）、Yue ら（開いた） | 文字列一致でも、似た文の検索でも、届かない場所が残る |
| **LLM の修正成功率は、修正箇所が散らばるほど下がる** | Nashid ら 2025（要約） | 散らばった変更（設計文書・要約版・コード・FAQ を跨ぐ）ほど、周が増える前提で段を組む |
| **テストを通った修正の29.6%が正解と違う振る舞いをした** | Wang, Pradel, Liu 2025（開いた） | 「テストが通った」「指摘が消えた」は完全さの証拠にならない |

---

## 7. 探したが見つからなかったもの

| 何を | 検索語・場所 | 結果 |
| --- | --- | --- |
| OpenHands のシステムプロンプト | `gh api repos/OpenHands/software-agent-sdk/git/trees/main?recursive=1`（1,999件、truncated=false）を `system_prompt` と `\.j2$` で grep。本体の `OpenHands/OpenHands` の木も `prompt` で grep | テストの2件と別用途の `.j2` 3件だけ。システムプロンプトは見つからない |
| Cline のシステムプロンプト | `gh api repos/cline/cline/contents/src/core/prompts` → 404。木（4,670件、truncated=false）を `system-prompt\|system_prompt\|prompts/.*rules\|components/rules` で grep | 0件 |
| Aider の指示に「全出現を探せ」系の文 | `Aider-AI/aider` main の `aider/coders/base_prompts.py`・`editblock_prompts.py`・`architect_prompts.py` を `all (the )?(occurrence\|place\|file)\|every\|root cause\|similar\|other (place\|file)\|consistent\|search` で grep | SEARCH/REPLACE の書式の行だけ。該当なし |
| Codex・SWE-agent・anthropics/claude-code の code-review プラグインに「全出現・他の場所・同じ誤り」を探させる文 | パターン `occurrence\|all places\|everywhere\|other places\|elsewhere\|similar (bug\|issue\|code\|pattern)\|same (bug\|issue\|mistake\|pattern)\|all (instances\|call ?sites\|usages)`（大文字小文字を区別しない）。対象は openai/codex main `7f01a84effcc` の `codex-rs/protocol/src/prompts/base_instructions/default.md`・`codex-rs/core/gpt_5_codex_prompt.md`・`codex-rs/core/gpt_5_2_prompt.md`、SWE-agent main `3ea751c087f3` の `config/default.yaml`、anthropics/claude-code main の `plugins/code-review/commands/code-review.md`（2026-09-15 取得） | 5ファイルとも0件 |
| Anthropic の公開文書に「同じ誤りを全部探せ」の手順 | 「Prompting best practices」の保存テキスト（1,113行）を `hard-cod\|general solution\|investigate before\|never speculate\|overengineer\|over-engineer\|root cause\|all occurrences\|every occurrence\|thorough\|verify\|self-check\|double-check\|subagent` で grep。「Best practices for Claude Code」は全文を読んだ | 「all occurrences」「every occurrence」は0件。Claude Code の文書にも該当する手順は無かった（読んだ結果で、grep ではない） |
| Project Zero の RCA テンプレートの「変種分析」「パッチ分析」の設問 | [0days-in-the-wild の RCA 一覧](https://googleprojectzero.github.io/0days-in-the-wild/rca.html) | 一覧にはテンプレートの本文が無かった。テンプレートは開いていない |
| Reason 2002 の「省略を起こしやすい段の4つの特徴」 | PMC の HTML・PDF・Europe PMC の全文 XML | 要旨しか取れなかった |
| Card 2017 の要旨 | Europe PMC の REST（`abstractText` が無い）、BMJ（403）、PubMed（cookie の画面） | 取れなかった |
| 「修正漏れを防ぐ指示や手順の有無で、漏れがどれだけ減ったか」を LLM エージェントで測った研究 | 専用の検索はしていない | 未調査 |
| poka-yoke をソフトウェアの修正漏れに当てた実務の記事、「同じバグを二度直すな」系の記事 | 専用の検索はしていない（Semgrep の規則化の記事で代えた） | 未調査 |

---

## 8. 開けなかった URL

| URL | 何が返ったか |
| --- | --- |
| https://link.springer.com/article/10.1007/s10664-016-9432-x （Park らの雑誌版） | 認証へのリダイレクト |
| https://www.semanticscholar.org/paper/An-empirical-study-of-supplementary-patches-in-open-Park-Kim/a459646b48915f33da3fa6332972abaa2fa84076 | 空の本文 |
| https://api.semanticscholar.org/graph/v1/paper/search?… （3回） | 429 |
| https://ieeexplore.ieee.org/document/6224298 | 空の本文 |
| https://www.academia.edu/11794092/An_empirical_study_of_supplementary_bug_fixes | 403 |
| https://pmc.ncbi.nlm.nih.gov/articles/PMC1743575/ と同じ記事の PDF | 本文が含まれない／ダウンロード待ちの画面 |
| https://www.ebi.ac.uk/europepmc/webservices/rest/PMC1743575/fullTextXML | 404 |
| https://pubmed.ncbi.nlm.nih.gov/27590189/ | cookie の画面 |
| https://qualitysafety.bmj.com/content/26/8/671 | 403 |
| https://queue.acm.org/detail.cfm?id=3773098 と https://cacm.acm.org/practice/safe-coding/ | 403 |
| https://raw.githubusercontent.com/openai/codex/main/codex-rs/core/prompt.md | 404（ファイルが移動していた。gh で現在のパスを探して読んだ） |
| 論文 PDF 全般（LASE・Wang ら・Yue ら・ROSE・Juergens・Hatton） | Read は `pdftoppm is not installed` で画像にできず。ツールは入れず、Python の zlib で本文を抜いた。Juergens らの論文本体は7,180文字しか抜けず、数値は発表資料の PDF から取った |
