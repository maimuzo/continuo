# 学術研究: 反復的な自己修正と LLM レビューの性質

**言いたいこと。**同じ全体を1人のレビュワーに直列で何度も見せても、1回のパスの取りこぼしは埋まりにくく、直すたびに別の欠陥が入る余地が増える。
実証研究が効くと示しているのは「同じ周の中で独立したレビューを並列に複数回し、和集合を取る」「挙がった指摘を別の文脈で検証して偽陽性を落とす」「指摘に位置・再現・テストの失敗のような外部の裏付けを持たせる」の3つである。
「同じ依頼を別のレビュワーに出したら同じ結果になる」は、1人のレビュワーへの指示では実現されていない（同じモデルを5回回すと、見つけたものの重なりは小さい）。

- 調べた日: 2026-09-15
- 担当: 論文・プレプリント・査読付き会議・大規模な実証研究。企業ブログと Anthropic 公式は別の worker、修正漏れ（同様の箇所の直し漏れ）の手順も別の worker が担当する
- この文書は調査結果であって、規則の変更を決めたものではない。**「当てはめうる場所」はすべて案である**

---

## 0. 読んだうえで答えた問い（worker-briefing の4問）

**1. この作業は、どの規則に当てはまるか。**

| 規則 | この調査でどう守ったか |
| --- | --- |
| [worker-briefing 2-4（指示に書かれたことだけで判断しない）](../../../../.claude/skills/worker-briefing/SKILL.md#L161) | 指示に名前が出ていない規則・スキルも読み、当てはめる場所を行番号で示した（下の「読んだもの」） |
| [worker-briefing 2-5（同じものが他に無いかを数える）](../../../../.claude/skills/worker-briefing/SKILL.md#L183) | 1本の論文で結論を出さず、同じ結論を支える論文と否定する論文を両方探した。「無い」と書くものには検索語と場所を書いた（5節） |
| [.claude/rules/reporting.md](../../../../.claude/rules/reporting.md) の「測る前に断定しない」「根拠の付け方」 | 主張ごとに出典と読んだ範囲を書き、要約モデル経由の数値はそう明記した（1節） |
| [CLAUDE.md](../../../../CLAUDE.md) の「`~/.claude/projects/` 配下を消さない」「公開してよい情報かを常に判断する」 | 書いたのはこのファイルだけ。PDF はメモリ上で読み、ファイルを作っていない |

[worker-briefing 2-6（1回で全部挙げる）](../../../../.claude/skills/worker-briefing/SKILL.md#L256) と [2-7（合理的根拠）](../../../../.claude/skills/worker-briefing/SKILL.md#L277) はレビュワー向けで、この調査には直接は当てはまらない。
ただし論文ごとに「持ち込める限界」を書いたのは、2-7 の「否定できる形で書く」と同じ趣旨である。

**2. 飛ばしてよい段はあるか。**
[.claude/rules/design-review.md:3](../../../../.claude/rules/design-review.md#L3) の9段は issue の実装作業の順番で、実装の無いこの調査には当てはまらない。
[.claude/rules/reporting.md](../../../../.claude/rules/reporting.md) の5段構成は人間への返答の形であり、このファイルは返答ではないので、[.claude/rules/plan-file.md](../../../../.claude/rules/plan-file.md) の「言いたいことを先に置く」を採った。

**3. 公開してよくない情報を書きうる場面はあるか。**
ある。このファイルは公開リポジトリの `docs/` の下にある。
(a) WebFetch が PDF を保存した先は個人の絶対パスを含むので書かない。
(b) 論文の長い転載は著作権の扱いが増えるので、引用は1〜2文に留めた。

**4. 作業に効く場所をどう探し、何を読んだか。**

- 検索: WebSearch（検索語は5節に列挙）
- 論文: arXiv の abs ページ、arXiv の HTML 版、ar5iv を WebFetch で開いた。WebFetch で文字化けした PDF は、WebFetch が保存した PDF を Python の pypdf で標準出力へ起こして読んだ（ファイルは書いていない）
- リポジトリで読んだもの: [CLAUDE.md:486](../../../../CLAUDE.md#L486) の「コードレビュー記録フロー」から「敵対的レビューが判定したあと」まで、[.claude/rules/design-review.md](../../../../.claude/rules/design-review.md) 全体、[.claude/skills/pr-review-and-merge/SKILL.md:93-190](../../../../.claude/skills/pr-review-and-merge/SKILL.md#L93-L190) の段2〜段4、[.claude/skills/worker-briefing/SKILL.md](../../../../.claude/skills/worker-briefing/SKILL.md) 全体
- 読めなかったもの: ACM DL・IEEE・Springer・ResearchGate・Wiley の論文ページ（403 か認証への転送）、Semantic Scholar の API（429）。一覧は6節

---

## 1. 読んだ範囲の表記（引用の確かさ）

**言いたいこと。**数値と引用の確かさは、読み方で4段に分かれる。「要約経由」のものは、原文と一字一句は照合していない。

| 表記 | 何をしたか | 引用と数値の確かさ |
| --- | --- | --- |
| **本文（PDF を起こして確認）** | PDF を pypdf で文字に起こし、該当箇所を目で読んだ | 原文どおり |
| **HTML 本文（要約経由）** | arXiv の HTML 版か ar5iv を WebFetch で開いた。WebFetch は小さいモデルで要約して返す | 要約モデルが返した数値・引用。原文との一字一句の照合はしていない |
| **abstract** | abs ページを WebFetch で開いた | abstract に書かれた範囲だけ |
| **検索結果のみ** | WebSearch の結果の要約だけで、本文を開けていない | いちばん弱い |

---

## 2. 問いごとの答えのまとめ

### 問い1: 1回のレビューのパスは在る欠陥の何割を拾うか。残りは直列と並列のどちらで拾うのが効率的か

**言いたいこと。**実在の PR を正解にした測定では、1回のパスの再現率（recall）は2〜4割程度である。
直列と並列を直接比べた研究は見つからなかったが、「同じ周で独立に複数回回して集約すると recall が上がる」実測と、「独立な試行の数を増やすほうが、同じ回数の直列の手直しより効く」実測はある。

| 条件 | 1回のパスの値 | 出典 |
| --- | --- | --- |
| 実在の GitHub PR 1,000件、プロジェクト全体の文脈つき | 最良で recall 23.18%・precision 16.65%。機能の変更に限ると recall 40.72% | SWR-Bench（2025/2026） |
| 業務の C++ コードで損失を出した実バグ | key bug inclusion（重要なバグを含められた割合）最大 31.11%、誤警報率 75〜94% | Lu ほか（ICML 2025） |
| LLM の応答に入った誤りの検出 | GPT-4 の recall 11.9〜59.5%、Claude 3 Opus 26.4〜38.6%。人間は F1 で約90〜95 | ReaLMistake（COLM 2024） |
| 推論の途中の誤りの位置の特定 | GPT-4 の正答率 52.87% | Tyen ほか（ACL Findings 2024） |
| JavaScript のセキュリティ診断（Snyk Code の検出を正解とする） | Claude Opus 4.6 Medium の F1 75.4% | Snyk VulnBench JS 1.0（2026） |

- **並列に独立して回し集約すると recall が上がる。**SWR-Bench の Self-Agg（同じモデルで10本のレビューを作り、LLM が1本に統合）で recall 13.91%→30.44%、F1 15.29%→21.91%。precision は少し下がり、5本を超えると伸びが鈍る（要約経由）。Lu ほかでは reviewer を1人→3人にすると key bug inclusion 26.67%→31.11%、ただし誤警報も増える（要約経由）
- **直列より独立な試行を増やすほうが効く、という間接証拠。**Olausson ほか（ICLR 2024）では、初めに10本生成して各1回修正すると pass@20 の 1.05倍、初めに2本だけ生成して各10回修正すると pass@22 の 0.97倍（要約経由）。Huang ほか（ICLR 2024）は、応答の数を揃えると multi-agent debate が self-consistency に勝たないと書く（本文で確認）。Choi ほか（NeurIPS 2025）は debate の利得の大半が多数決から来ると示した
- **同じモデルを繰り返しても同じ結果にならない。**SWR-Bench では同じ LLM を5回独立に回して「見つけた変更の重なり」が27件だけ（和集合の大きさは取得できていない）。Snyk VulnBench では、正解と一致した158件のうち134件は5回すべてに出たが、余分な指摘161件のうち80件は5回中1回だけに出た。Klishevich ほか（2025）は温度0でも出力が揺れると報告した
- **2026-09-04 の人間の方針「同じ内容を別のレビュワーに依頼したら、まったく同じ内容になるぐらい徹底的に」について。**1人のレビュワーへの指示でこれを達成したという実証は見つからなかった。実証のある手段で近づけるなら、(1) 同じ周の中で独立したレビューを複数回し、その和集合を「その周の全部」とする、(2) 回どうしの重なりで見つかっていない残りを見積もる（ソフトウェアインスペクションの capture-recapture。Petersson ほか 2004、Briand ほか 2000）、(3) 観点やモデルの系統を分けて独立性を上げる（Basili ほか の perspective-based reading、Verga ほか 2024 の PoLL）の組み合わせになる。**これは案である**

### 問い2: 自己修正・反復修正の周回を重ねたとき、品質と新しい欠陥はどう推移するか

**言いたいこと。**外部の裏付けが無い自己修正は、横ばいか悪化する。裏付けがあっても利得は1〜2周目に集中し、その先は頭打ちになる。
曖昧な「改善して」を人間抜きで回し続けると、欠陥が周回とともに増えた実測がある。

- **外部の裏付けなしだと悪化しうる。**Huang ほか（ICLR 2024）: GPT-4 の GSM8K 正答率 95.5%→91.5%（1周目）→89.0%（2周目）。GPT-3.5 の CommonSenseQA は 75.8%→38.1%→41.8%（本文の表3で確認）。Stechly ほか（ICLR 2025）: GPT-4 の自己批評で性能が大きく崩れ、正しい外部検証器では大きく上がった（abstract）。Kamoi ほか（TACL 2024）のサーベイ: プロンプトだけの自己修正が成功した信頼できる証拠は、自己修正に特に向いた課題を除いて無い
- **裏付けがあっても逓減する。**Self-Refine（NeurIPS 2023）のコード最適化 22.0→27.0→27.9→28.8（要約経由）。CRITIC（ICLR 2024）は1〜2周に利得が集中（要約経由）。Arimbur（2026）はテストの例外メッセージを返す自己修復で「2周で達成可能な利得の76〜95%」（要約経由）。Yang ほか（EMNLP 2025）は t 周目の正答率を `Acc_t = Upp − α^t (Upp − Acc_0)` と置き、上限へ等比で近づくと理論化した（abstract）
- **新しい欠陥が入る。**Shukla ほか（IEEE-ISTAS 2025）: GPT-4o に「改善して」系の指示を人間抜きで10周させ、サンプルあたりの脆弱性が1〜3周で平均2.1、4〜7周で4.7、8〜10周で6.2。複雑さの増加と相関 r=0.64（要約経由）。Cihan ほか（2025）: 問題の説明を渡さないと、正しいコードの最大24.80%に誤った修正提案が付いた（要約経由）。Zhong ほか（2026）: 採用された AI の提案は、人間の提案より複雑さと規模を大きく増やした（abstract）
- **反論されると正しい答えを捨てる。**FlipFlop（Laban ほか 2023）: 「本当に?」と聞き返すと平均46%で答えを変え、正答率が平均17%下がる（abstract）。Who Flips（Nikeghbal ほか 2026）: 反論を与えたときの答えの反転率はモデルにより17.5%〜97.3%（要約経由）
- **反対の知見。**推論型のモデルは、自分の誤りを自分で直す余地が既に小さい（Chen ほか 2026 の付録: gpt-oss-20B は77%、DeepSeek-R1 は100%を既に直していた。要約経由）。訓練で自己修正を身につけさせると改善する（SCoRe、abstract）。つまり 2023〜2024 年のモデルでの「自己修正は効かない」を、最新の Claude へそのまま持ち込むのは言い過ぎになりうる

### 問い3: 指摘（フィードバック）の質を上げる手段で、実証されたものは何か

**言いたいこと。**効くと示されたのは、検証できる外部の信号（テスト・実行結果・検証器）、誤りの位置の特定、具体的で短くコード片つきの指摘、問題の説明と選んだ文脈、元の回答を見せない別の文脈での検証である。

| 手段 | 実測 | 出典 |
| --- | --- | --- |
| テストの失敗・実行結果を返す | ユニットテストがある課題で最大12%の改善 | Self-Debugging（ICLR 2024、abstract） |
| 正しい外部検証器 | 「sound verifier で再プロンプトするだけで、複雑な仕組みの利得の大半が残る」 | Stechly ほか（ICLR 2025、abstract） |
| 誤りの位置を渡す | 位置さえ分かれば直せる。位置の特定の精度が60〜70%でも後戻りは効く | Tyen ほか（ACL Findings 2024、本文で確認） |
| より良い説明 | GPT-4 自身の説明で修復成功33.3%、人間の説明で52.6%（1.58倍） | Olausson ほか（ICLR 2024、本文で確認） |
| 具体的な指摘 | 汎用の指摘より課題に即した指摘が上（例: 43.2 対 31.2） | Self-Refine（NeurIPS 2023、要約経由） |
| 短く・コード片つき・hunk 単位 | そういうコメントほどコードの変更につながる | Sun ほか（2025、abstract） |
| インラインのコード提案 | 解決される見込みが1.62倍（オッズ比）。長いコメントは0.93倍 | Cynthia ほか（2026、要約経由） |
| 問題の説明を渡す | 有無で最大22.87ポイントの差 | Cihan ほか（2025、要約経由） |
| 文脈を選んで足す | 1件に絞らせるより一覧で挙げさせるほうが +4.68ポイント。文脈は全部足すより選ぶほうが上 | Sun ほか（2026、Go のレビュー、要約経由） |
| 元の回答を見せずに検証させる | 検証の質問を別の文脈で答えさせると幻覚が減る（伝記の FactScore 55.9→63.7、修正込みで71.4） | Chain-of-Verification（2023、要約経由） |
| 自分の思考として置かれた誤りを、外部の役の文として置き直す | 誤りを明示的に直す率が +23〜93ポイント（推論型モデルでは上がる余地が無い） | Chen ほか（2026、要約経由） |

**自分で作ったテストは偏る。**「post-execution self-debugging struggles with the test bias introduced by self-generated tests」（Chen ほか ACL 2025、abstract）。
（訳: 実行後に自己デバッグする方式は、自分で生成したテストが持ち込む偏りに苦しむ。）

### 問い4: レビュワーの偽陽性を減らす手段で、実証されたものは何か

**言いたいこと。**検証の段（フィルタ）と、実行やテストで裏を取る段は効く。ただし recall も一緒に削る。
多数決や全員一致は、正しさの保証にならない。

- **検証の段。**BitsAI-CR（ByteDance、2025）: 検出の段だけで precision 57.03%、検証の段（ReviewFilter）を足して65.59%、本番で75.0%。検証の出力を「結論を先・理由を後」にすると77.09%、「理由を先」だと65.80%（要約経由）。Lu ほか（ICML 2025）: Validator で誤警報は下がるが、key bug inclusion も31.11%→20.00%に下がる（要約経由）。Du ほか（2026、Tencent）: 静的解析と LLM で偽陽性の94〜98%を除いた（abstract）
- **実行で裏を取る。**Refute-or-Promote（2026、1人の運用者による事例研究）: 候補171件の約79%を公開前に捨てた。80以上のエージェントが一致して認めた存在しない脆弱性を、テスト1本が否定した。別系統のモデルの批評役が、同系統で通った修正19件中3件に問題を見つけた（要約経由）
- **繰り返しの安定性を目印にする。**Snyk VulnBench（2026）: 本物（正解と一致したもの）は5回すべてに出やすく、余分な指摘は1回だけに出やすい（要約経由）
- **長さと件数を抑える。**CriticGPT（OpenAI、2024）: 長い批評ほど見逃しは減るが、でっち上げと些細な指摘が増える（本文で確認）。MSR 2026 の Fatima ほか: レビューボットのコメントが多いほど解決が遅れ、質が下がる（abstract）
- **効かなかったもの。**ReaLMistake（COLM 2024）: self-consistency と多数決は誤り検出を改善しなかった（abstract）。同じモデルは自分の出力をひいきする（Panickssery ほか NeurIPS 2024、Xu ほか ACL 2024）。複数系統の審査員を並べると、単一の大きな審査員より偏りが小さい（PoLL、2024、abstract）

### 問い5: 人間のレビューで、周回を増やす主な原因は何か

**言いたいこと。**周回の数そのものの原因を直接数えた研究は開けなかった。近いものとして、変更の大きさ、設計に関する指摘、変更の理由が書かれていないことが挙がっている。

- Beller ほか（MSR 2014）: レビューで直されたものの75%が保守性、25%が機能の問題。レビューコメントの7〜35%は捨てられ、変更の10〜22%はコメントなしに入る。変更ファイルが多くコードの変更量が多い課題ほど、レビューによる変更が多い（本文の abstract で確認）
- El Zanaty ほか（ESEM 2018）: 設計に関する指摘が付いた変更は、放棄される率が統計的に有意に高い（abstract）
- Ebert ほか（EMSE 2021）: コードレビューでの混乱の理由を30種、影響を14種、対処を13種に整理した（abstract）。「理由（rationale）が書かれていない」が最多という順位は、検索結果の要約にしか無く、本文で確かめていない
- Zhong ほか（2026）: AI が書いたコードのレビューでは、人間が書いたコードより往復が11.8%多い（abstract）
- Cihan ほか（ICSE SEIP 2025）: LLM のレビューボットを入れたあと、PR のクローズまでの平均が5時間52分→8時間20分に延びた（要約経由）

### このリポジトリへ当てはめうる場所（すべて案であって決定ではない）

| 実証が効くと示した手段 | 当てはめうる場所 | 主な出典 |
| --- | --- | --- |
| 同じ周の中で独立したレビューを複数並列に回し、和集合を取って重複を除く | [worker-briefing 2-6](../../../../.claude/skills/worker-briefing/SKILL.md#L256)（1人に全部を求める形の補い）、[pr-review-and-merge 段2](../../../../.claude/skills/pr-review-and-merge/SKILL.md#L93)（`/code-review` を1本ずつ直列に回す形） | SWR-Bench、Lu ほか、Snyk、Basili ほか |
| Critical と High を、指摘を出していない別の文脈で検証してから数える | [CLAUDE.md:548](../../../../CLAUDE.md#L548) の「収まっている」とは何か | BitsAI-CR、Lu ほか、Refute-or-Promote、CoVe |
| 指摘に位置・再現手順・失敗するテストを持たせる | [worker-briefing 2-7](../../../../.claude/skills/worker-briefing/SKILL.md#L277) | Tyen ほか、Olausson ほか、Stechly ほか |
| 実装者の反論をレビュワー本人と往復させず、別の文脈で判定する | [.claude/rules/design-review.md:124](../../../../.claude/rules/design-review.md#L124) | FlipFlop、Who Flips、Choi ほか |
| 独立したレビュー同士の重なりで残りを見積もり、次の周の要否を決める | [CLAUDE.md:543](../../../../CLAUDE.md#L543) の「回数を数える」 | Petersson ほか、Briand ほか |
| 再レビューで、直しが持ち込んだ退行を明示的に探させる | [CLAUDE.md:516](../../../../CLAUDE.md#L516) の対応表の「分類」列 | Shukla ほか、Cihan ほか 2025、Zhong ほか |
| 利得が1〜2周で頭打ちになる前提で周回の設計を見直す | [CLAUDE.md:645](../../../../CLAUDE.md#L645) の連続10回 | Arimbur、Self-Refine、CRITIC、Yang ほか |
| 実装の前に受け入れ条件と例（テスト）を固める | [.claude/rules/design-review.md:3](../../../../.claude/rules/design-review.md#L3) の段1〜4 | TiCoder、ClarifyGPT、NaPiRE |
| 指摘は短く、コード片つきにし、件数を絞る | 対応表とレビュー出力の形 | Sun ほか 2025、Cynthia ほか、BitsAI-CR |

---

## 3. 論文・知見ごとの節

**共通の限界。**このリポジトリのレビューは、(a) 実装とは別の文脈のレビュワーが指摘し、(b) 実装側が直し、(c) Go のコードと日本語の長い規則文書の両方が対象である。
下の研究の多くは、同じモデルが自分の出力を直す「内在的な自己修正」か、Python の関数単位の課題か、英語の課題である。**このリポジトリで使っているモデルの版は測っていない。**

### 3-A. 自己修正の限界

#### 自己修復は銀の弾丸ではない

- **書誌**: Olausson, Inala, Wang, Gao, Solar-Lezama「Is Self-Repair a Silver Bullet for Code Generation?」2024、ICLR 2024、[arXiv:2306.09896](https://arxiv.org/abs/2306.09896)。**本文（PDF を起こして確認）**＋ ar5iv（要約経由）
- **主張と実測**: Code Llama・GPT-3.5・GPT-4 に HumanEval と APPS で自分のコードを直させた。修復の費用を含めると利得は小さく、ばらつき、無いこともある。人間の説明に置き換えると修復の成功率が 33.30%→52.60%（難易度別: 入門 42.64%→62.21%、面接 19.33%→45.67%、競技 3.67%→14.67%。表1で確認）。初めに多く生成して修正を少なくする配分が、初めを少なくして修正を多くする配分より良い（1.05倍 対 0.97倍。要約経由）
- **原文（訳）**: 「self-repair is bottlenecked by the model’s ability to provide feedback on its own code; using a stronger model to artificially boost the quality of the feedback, we observe substantially larger performance gains」（自己修復は、自分のコードに指摘を出す能力で律速されている。より強いモデルで指摘の質を人工的に上げると、はるかに大きな利得が出る）
- **答える問い**: 問い1（直列より独立な試行）、問い3（指摘の質が律速）
- **持ち込める限界**: GPT-4 世代、単体の関数、テストの合否が正解。レビュワーが別文脈で指摘する形とは違う
- **当てはめうる場所（案）**: [worker-briefing 2-7](../../../../.claude/skills/worker-briefing/SKILL.md#L277) の根拠の書き方を「なぜ間違いか・どこか」の具体性で強める

#### 大規模言語モデルはまだ推論を自己修正できない

- **書誌**: Huang, Chen, Mishra, Zheng, Yu, Song, Zhou「Large Language Models Cannot Self-Correct Reasoning Yet」2023、ICLR 2024、[arXiv:2310.01798](https://arxiv.org/abs/2310.01798)。**本文（PDF を起こして確認）**
- **主張と実測**: 外部の信号なしの自己修正（表3）。GPT-4: GSM8K 95.5→91.5→89.0、CommonSenseQA 82.0→79.5→80.0、HotpotQA 49.0→49.0→43.0。GPT-3.5: CommonSenseQA 75.8→38.1→41.8。正解のラベルを使うと上がる（GPT-4 の GSM8K 97.5、表2）ので、先行研究の改善はラベルの漏れによるものがあった。応答の数を揃えると multi-agent debate は self-consistency に勝たない
- **原文（訳）**: 「LLMs struggle to self-correct their responses without external feedback, and at times, their performance even degrades after self-correction」（LLM は外部の指摘なしには応答を自己修正しにくく、自己修正のあとに性能が落ちることさえある）。「its efficacy is no better than self-consistency (Wang et al., 2022) when considering an equivalent number of responses」（応答の数を揃えると、その効果は self-consistency より良くない）
- **答える問い**: 問い1、問い2
- **持ち込める限界**: 数学・常識推論で、GPT-3.5/4。レビューの指摘は「外部の指摘」に当たるので、このリポジトリの修正側はこの論文の否定の対象そのものではない。当てはまるのは「レビュワーが自分の前の指摘を見直す」場面
- **当てはめうる場所（案）**: 周回で同じレビュワーに前の結論を見直させる形を避ける

#### LLM は自分の誤りをいつ直せるのか（サーベイ）

- **書誌**: Kamoi, Zhang, Zhang, Han, Zhang「When Can LLMs Actually Correct Their Own Mistakes? A Critical Survey of Self-Correction of LLMs」2024、TACL、[arXiv:2406.01297](https://arxiv.org/abs/2406.01297)。**HTML 本文（要約経由）**
- **主張と実測**: 自己修正が効くのは (1) 信頼できる外部の指摘がある課題、(2) 大規模な微調整、(3) 検証が易しい小問に分解できる課題。律速は指摘の生成。先行研究には正解の漏れ、弱い初回プロンプト、強いベースラインとの比較の欠落がある
- **原文（訳）**: 「no prior work demonstrates successful self-correction with feedback from prompted LLMs, except for studies in tasks that are exceptionally suited for self-correction」（自己修正に特に向いた課題を除き、プロンプトした LLM の指摘で自己修正に成功したことを示した先行研究は無い）
- **答える問い**: 問い2、問い3
- **持ち込める限界**: 2024 年までの研究の整理。推論型モデル以後は含まない
- **当てはめうる場所（案）**: 比較の相手を「同じ費用で独立に回したもの」にする考え方を、周回の設計の評価に使う

#### 誤りは見つけられないが、位置を渡せば直せる

- **書誌**: Tyen, Mansoor, Cărbune, Chen, Mak「LLMs cannot find reasoning errors, but can correct them given the error location」2023、ACL Findings 2024、[arXiv:2311.08516](https://arxiv.org/abs/2311.08516)。**本文（PDF を起こして確認）**＋ ar5iv（要約経由）
- **主張と実測**: BIG-Bench Mistake で誤りの位置の特定の正答率は GPT-4 で52.87%（表4で確認）、GPT-3.5-Turbo 14.78%。位置を渡すと直せる（例: 物の入れ替えの追跡で誤った推論の正答率が +43.92、正しい推論は −6.67。要約経由）。位置の特定が60〜70%の精度でも後戻りは効く（本文で確認）
- **原文（訳）**: 「backtracking is still effective even without gold standard mistake location labels」（正解の位置ラベルが無くても、後戻りは効く）
- **答える問い**: 問い3（指摘に位置を持たせる）
- **持ち込める限界**: 推論の一本道の手順。コードの欠陥は複数行・複数ファイルに散る
- **当てはめうる場所（案）**: [worker-briefing 2-7](../../../../.claude/skills/worker-briefing/SKILL.md#L277) に「ファイルと行」を必須にする（既に reporting.md の根拠の付け方にある）

#### 推論と計画での自己検証の限界

- **書誌**: Stechly, Valmeekam, Kambhampati「On the Self-Verification Limitations of Large Language Models on Reasoning and Planning Tasks」2024、ICLR 2025、[arXiv:2402.08115](https://arxiv.org/abs/2402.08115)。**abstract**
- **主張と実測**: GPT-4 の Game of 24・グラフ彩色・STRIPS 計画
- **原文（訳）**: 「We observe significant performance collapse with self-critique and significant performance gains with sound external verification. We also note that merely re-prompting with a sound verifier maintains most of the benefits of more involved setups.」（自己批評では性能が大きく崩れ、正しい外部検証では大きく上がった。正しい検証器で再プロンプトするだけで、込み入った仕組みの利得の大半が残る）
- **答える問い**: 問い2、問い3
- **持ち込める限界**: 正解が機械で判定できる課題。規則文書のレビューには機械的な検証器が無いものが多い
- **当てはめうる場所（案）**: Go 側は build・test・既存の検査スクリプトを「検証器」として指摘の真偽判定に使う

#### 自己改善と自己ひいき

- **Self-Refine**: Madaan ほか 2023、NeurIPS 2023、[arXiv:2303.17651](https://arxiv.org/abs/2303.17651)。**abstract ＋ ar5iv（要約経由）**。平均で約20ポイント改善と主張（abstract）。コード最適化 22.0→27.0→27.9→28.8、制約つき生成 29.0→40.3→46.7→49.7 で逓減。数学では指摘の94%が「everything looks good」で改善がほぼ無い（要約経由）。**Huang ほかは、初回プロンプトを適切にすると制約つき生成で自己修正が 81.8→75.1 に下がったと反論している（要約経由）**。答える問い: 問い2・問い3
- **Pride and Prejudice**: Xu, Zhu, Zhao, Pan, Li, Wang「LLM Amplifies Self-Bias in Self-Refinement」2024、ACL 2024、[aclanthology 2024.acl-long.826](https://aclanthology.org/2024.acl-long.826/)。**abstract**。「while the self-refine pipeline improves the fluency and understandability of model outputs, it further amplifies self-bias」（自己改善は流暢さと分かりやすさを上げるが、自己ひいきをさらに強める）。大きいモデルと正確な外部の指摘で偏りが減る。答える問い: 問い2・問い4
- **自分の出力の見分けとひいき**: Panickssery, Bowman, Feng 2024、NeurIPS 2024、[proceedings](https://proceedings.neurips.cc/paper_files/paper/2024/hash/7f1f0218e45f5414c79c0679633e47bc-Abstract-Conference.html)。**abstract**。自分の出力を見分ける力と自己ひいきの強さに線形の相関。答える問い: 問い4
- **生成と検証の差**: Song ほか「Mind the Gap」2024、ICLR 2025、[arXiv:2412.02674](https://arxiv.org/abs/2412.02674)。**abstract**。自己改善を「生成と検証の差」で定式化し、その差が事前学習の計算量とともに単調に大きくなる。答える問い: 問い2（新しいモデルほど自己検証の余地が広がる可能性）
- **持ち込める限界（まとめ）**: どれも同じモデルが自分を評価する形。このリポジトリのレビュワーは別文脈だが、同じモデル系統なら自己ひいきに近い偏りが残りうる（測っていない）
- **当てはめうる場所（案）**: 検証の段で、指摘を出したのと別の文脈を使う

#### 自己修正の死角と「錯覚」（新しいモデル）

- **Self-Correction Bench**: Tsui 2025（arXiv の版は 2026-08-02 付の v3）、[arXiv:2507.02778](https://arxiv.org/abs/2507.02778)。**HTML 本文（要約経由）**。同じ誤りでも、利用者の誤りなら直すが自分の誤りだと直さない「死角」が、推論型でない14モデルで平均64.5%。「Wait」を足すと89.3%減る
- **The Self-Correction Illusion**: Chen, Su, Chiang 2026、[arXiv:2606.05976](https://arxiv.org/abs/2606.05976)。**HTML 本文（要約経由）**。Claude Sonnet 4・GPT-4o・Gemini 2.5 Flash などで、同一の誤った主張を `<thought>` の中に置くか、外部の役（利用者・ツール・メモリ）として置くかだけを変えた。外部の役に置き直すと、誤りを明示的に直す率が +23〜93ポイント（1セル30問）。推論型の gpt-oss-20B（77%）と DeepSeek-R1（100%）は置き直す前から直しており、上がる余地が無い
- **原文（訳、要約経由）**: 「The failure to self-correct is not a cognitive deficit; it is a chat-template artifact.」（自己修正の失敗は認知の欠陥ではなく、チャットテンプレートの産物である）
- **答える問い**: 問い3（誤りを「他人の書いたもの」として渡すと直しやすい）
- **持ち込める限界**: 数学と論理の短い課題、1セル30問。最新の推論型では差が消える
- **当てはめうる場所（案）**: レビュワーには「実装者の成果物」として別文脈で渡す（いまも別文脈で渡しているので、変えるなら検証の段にも同じ形を使う）

#### 訓練で自己修正を身につけさせる

- **書誌**: Kumar ほか「Training Language Models to Self-Correct via Reinforcement Learning」（SCoRe）2024、[arXiv:2409.12917](https://arxiv.org/abs/2409.12917)。**abstract**
- **原文（訳）**: 「Self-correction is a highly desirable capability of large language models (LLMs), yet it has consistently been found to be largely ineffective in modern LLMs.」（自己修正は望ましい能力だが、現代の LLM ではほとんど効かないと一貫して報告されてきた）。Gemini 1.0 Pro と 1.5 Flash で MATH +15.6%、HumanEval +9.1%
- **答える問い**: 問い2（反対の知見）
- **持ち込める限界**: 訓練が要る。規則とスキルの変更では持ち込めない

### 3-B. 周回と劣化

#### 反復的な AI コード生成でのセキュリティの劣化

- **書誌**: Shukla, Joshi, Syed「Security Degradation in Iterative AI Code Generation — A Systematic Analysis of the Paradox」2025、IEEE-ISTAS 2025、[arXiv:2506.11022](https://arxiv.org/abs/2506.11022)。**HTML 本文（要約経由）**
- **主張と実測**: GPT-4o（温度0.7）。基準のコード10本 × 指示4種 × 10周 = 400サンプル。静的解析（Clang Static Analyzer・CodeQL・SpotBugs）と手動のセキュリティレビューで数えた。サンプルあたりの脆弱性は1〜3周で平均2.1、4〜7周で4.7、8〜10周で6.2。指示別の総数は、性能重視124・機能追加158・セキュリティ重視38・曖昧な「改善して」67。複雑さの増加と r=0.64。abstract は「5周で重大な脆弱性が37.6%増えた」と書く
- **答える問い**: 問い2（周回で新しい欠陥が増える）
- **持ち込める限界**: 周回ごとに人間もツールも挟まない設計で、指示は具体的な指摘ではなく「改善して」。著者自身が「人間の入力が挟まる実務より脆弱性の混入に寄った設定」と限界に書いている（要約経由）。基準のコードは10本と少ない
- **当てはめうる場所（案）**: 再レビューで「直しが持ち込んだもの」を明示的に探させる（[CLAUDE.md:516](../../../../CLAUDE.md#L516) の分類の列を、事後の分類でなく探す観点にする）

#### 反復の自己修復は何回で効くか

- **書誌**: Arimbur「How Many Tries Does It Take? Iterative Self-Repair in LLM Code Generation Across Model Scales and Benchmarks」2026、[arXiv:2604.10508](https://arxiv.org/abs/2604.10508)。**HTML 本文（要約経由）**。著者は独立研究者
- **主張と実測**: Llama 3.1 8B〜Gemini 2.5 Pro の7モデル、HumanEval と MBPP。例外のトレースバックを返す。1周目の伸びが最大で、「two repair rounds capture the majority (76–95%) of achievable gains」（2周で達成可能な利得の76〜95%を得る）。assertion の失敗は約45%しか直らない。直しが通っていたものを壊したかは測っていない
- **答える問い**: 問い2
- **持ち込める限界**: テストの合否という明確な外部信号がある。レビュー指摘の真偽は不確かなので、同じ速さで収束する保証は無い
- **当てはめうる場所（案）**: 周回の上限の根拠として「2周で大半」を参照する

#### 自己修正の周回の確率モデル

- **書誌**: Yang ほか「A Probabilistic Inference Scaling Theory for LLM Self-Correction」2025、EMNLP 2025、[arXiv:2508.16456](https://arxiv.org/abs/2508.16456)。**abstract**
- **主張**: t 周目の正答率 `Acc_t = Upp − α^t (Upp − Acc_0)`。上限（Upp）と収束の速さ（α）は1周の結果から推定でき、多周の曲線を予測できる
- **答える問い**: 問い2（等比で上限へ近づく。周回を増やしても上限は超えない）
- **持ち込める限界**: 正答率が1つの数で測れる課題。レビュー指摘の件数に当てはまるかは測っていない
- **当てはめうる場所（案）**: 1周目と2周目の件数の減り方から、続ける価値を見積もる考え方

#### 反論されると答えを変える

- **FlipFlop**: Laban, Murakhovs'ka, Xiong, Wu 2023、[arXiv:2311.08596](https://arxiv.org/abs/2311.08596)。**abstract**。「models flip their answers on average 46% of the time and that all models see a deterioration of accuracy between their first and final prediction, with an average drop of 17%」（平均46%で答えを変え、全モデルで正答率が落ち、平均で17%下がった）
- **Who Flips**: Nikeghbal, Kargaran, Kolli, Diesner 2026、[arXiv:2606.16011](https://arxiv.org/abs/2606.16011)。**HTML 本文（要約経由）**。MMLU 2,052問・7モデル。誤答を支持する反論で、反転率は17.5%〜97.3%。自分が書いた反論だと示すと反転が平均 +7.1ポイント。ばらつきの76.7%は「どのモデルか」で説明された
- **答える問い**: 問い2、問い4
- **持ち込める限界**: 多肢選択の分類。コードの指摘の撤回とは形が違う
- **当てはめうる場所（案）**: [.claude/rules/design-review.md:124](../../../../.claude/rules/design-review.md#L124)「合理的根拠を否定できるなら、直さない」で、実装側の反論を元のレビュワーとの往復で決めず、別文脈の判定に回す

#### AI の提案が複雑さを増やす・往復が増える

- **書誌**: Zhong, Noei, Zou, Adams「Human-AI Synergy in Agentic Code Review」2026、[arXiv:2603.15911](https://arxiv.org/abs/2603.15911)。**abstract**
- **主張と実測**: 300の OSS の 278,790 件のレビュー会話。AI が書いたコードのレビューでは往復が11.8%多い。採用されなかった AI の提案の半分超は誤りか、別の直し方で片付けられた。採用された AI の提案は、人間の提案より複雑さと規模を大きく増やした
- **答える問い**: 問い2、問い5
- **持ち込める限界**: 人間とエージェントが混ざる OSS。ここは AI どうしのループ

#### コードレビューでの LLM の退行

- **書誌**: Cihan, İçöz, Haratian, Tüzün「Evaluating Large Language Models for Code Review」2025、[arXiv:2505.20206](https://arxiv.org/abs/2505.20206)。**HTML 本文（要約経由）**
- **主張と実測**: GPT-4o と Gemini 2.0 Flash。問題の説明を渡すと正誤判定が GPT-4o で68.50%。説明が無いと「up to 24.80% of correct code blocks received incorrect code suggestions」（正しいコードの最大24.80%に誤った修正提案が付いた）。説明の有無で最大22.87ポイントの差
- **答える問い**: 問い2、問い3
- **持ち込める限界**: HumanEval 由来の単体コード
- **当てはめうる場所（案）**: レビュワーに issue の要求（受け入れ条件）を必ず渡す。`/code-review` は自由なプロンプトを受け取れない（[pr-review-and-merge 段2](../../../../.claude/skills/pr-review-and-merge/SKILL.md#L93)）ので、PR の本文に受け入れ条件を書く形になる

### 3-C. 1回のパスの再現率とばらつき

#### SWR-Bench: 実在の PR でのレビューの性能と、複数レビューの集約

- **書誌**: Zeng, Shi, Han, Li, Sun, Wang, Yu, Xie, Ye, Zhang「SWR-Bench: Assessing LLM Performance in Real-World Code Review Comment Generation」2025（v2 は 2026-06-05）、FSE 2026 の研究論文の一覧に載っている（[conf.researchr.org](https://conf.researchr.org/details/fse-2026/fse-2026-research-papers/78/SWR-Bench-Assessing-LLM-Performance-in-Real-World-Code-Review-Comment-Generation)）、[arXiv:2509.01494](https://arxiv.org/abs/2509.01494)。**HTML 本文（要約経由）**
- **主張と実測**: 人手で確かめた GitHub の PR 1,000件、リポジトリ全体の文脈つき。正解の問題がレビューに含まれたかを LLM で判定し、人間との一致は約90%。最良は PR-Review ＋ Gemini-2.5-Pro で precision 16.65%・recall 23.18%・F1 19.38%。機能の変更に限ると recall 40.72%。偽陽性の原因は「文脈の理解不足」48%、「変更への過敏」17%、「曖昧で実行できない指摘」16%、「領域知識の不足」13%。**集約**: n 本のレポートを連結し、LLM に1本の優れたレビューへ統合させる。Gemini-2.5-Flash で n=10 にすると recall 13.91%→30.44%、F1 15.29%→21.91%。precision は少し下がり、n=5 を超えると伸びが鈍る。**ばらつき**: 「for the same LLM over five independent runs, only 27 successfully identified change-actions overlapped」（同じ LLM の独立な5回で、見つけた変更の重なりは27件だけ）。和集合の大きさは取れていない
- **答える問い**: 問い1（再現率、並列の集約、ばらつき）、問い4（偽陽性の原因）
- **持ち込める限界**: 2025 年の Gemini・DeepSeek・Qwen などで、最新の Claude は測られていない。正解は「実際の PR で直された問題」なので、正解に無い本物の欠陥も偽陽性に数えられうる。日本語の文書は対象外
- **当てはめうる場所（案）**: [pr-review-and-merge 段2](../../../../.claude/skills/pr-review-and-merge/SKILL.md#L93) で、同じ周に独立のレビューを複数立てて統合する

#### Snyk VulnBench JS 1.0: LLM は同じバグを2度見つけるか

- **書誌**: Tal, Kloos, Rudich, Thoemmes, Nair「Snyk VulnBench JS 1.0: Can LLMs Find the Same Bugs Twice?」2026、[arXiv:2606.15762](https://arxiv.org/abs/2606.15762)。**HTML 本文（要約経由）**。著者はセキュリティ企業の所属
- **主張と実測**: Claude Code のハーネス（TypeScript Agent SDK）で Claude Opus 4.6（Medium・High）・Opus 4.7（Max）・Sonnet 4.6（Medium・High）を、同じ監査プロンプトで JavaScript の10プロジェクトに5回ずつ、計300回。正解（Snyk Code の検出）と一致した158件のうち134件（84.8%）は5回すべてに出た。余分な指摘161件のうち80件（49.7%）は5回中1回だけ。1回だけの割合は Sonnet 4.6 Medium 61.7%、Sonnet 4.6 High 46.3%、Opus 4.7 Max 47.2%、Opus 4.6 High 16.7%、Opus 4.6 Medium 0.0%。最良の F1 は Opus 4.6 Medium の75.4%。Opus 4.7 Max は5.7倍の費用で点が低かった
- **原文（訳、要約経由）**: 「true positives were usually stable, while extra non-reference reports were much noisier」（本物はたいてい安定し、正解に無い余分な報告はずっと揺れた）
- **答える問い**: 問い1（ばらつき）、問い4（安定性を偽陽性の目印にする）
- **持ち込める限界**: セキュリティの監査で、PR のレビューではない。正解に無い指摘がすべて偽陽性とは限らない。effort level を上げても安定するとは限らない（Opus 4.6 は Medium が High より安定、Sonnet 4.6 は High が Medium より安定）
- **当てはめうる場所（案）**: 同じ周の複数回で1回しか出なかった Critical・High を、検証の段へ優先して回す

#### 温度0でもレビューの出力は揺れる

- **書誌**: Klishevich, Denisov-Blanch, Obstbaum, Ciobanu, Kosinski「Measuring Determinism in Large Language Models for Software Code Review」2025、[arXiv:2502.20747](https://arxiv.org/abs/2502.20747)。**HTML 本文（要約経由）**
- **主張と実測**: Java の70 commit を、GPT-4o mini・GPT-4o・Claude 3.5 Sonnet・LLaMA 3.2 90B に温度0で5回ずつ評価させた（難しさ・工数・保守性などの数値の質問）。回どうしのピアソン相関は Claude 3.5 が0.85〜0.93、GPT-4o が0.69〜0.85、GPT-4o mini が0.53〜0.84
- **答える問い**: 問い1（「同じ依頼で同じ結果」は温度0でも成り立たない）
- **持ち込める限界**: 指摘の一覧ではなく数値の評価。モデルは 2024 年のもの

#### LLM の応答の誤り検出（ReaLMistake）

- **書誌**: Kamoi ほか「Evaluating LLMs at Detecting Errors in LLM Responses」2024、COLM 2024、[arXiv:2404.03602](https://arxiv.org/abs/2404.03602)。**abstract ＋ HTML 本文（要約経由）**
- **主張と実測**: GPT-4 と Llama 2 70B の応答に入った客観的な誤りを専門家が注釈。検出の recall は GPT-4 で MathGen 59.5%・FgFactV 11.9%・AnsCls 12.6%、Claude 3 Opus で35.9%・38.6%・26.4%。人間は F1 で約90〜95（要約経由）
- **原文（訳）**: 「Popular approaches to improving LLMs, including self-consistency and majority vote, do not improve the error detection performance.」（self-consistency や多数決のような定番の改善手法は、誤り検出の性能を上げなかった）
- **答える問い**: 問い1、問い4（多数決は効かない）
- **持ち込める限界**: 「誤りがあるか無いか」の判定。多数決は少数しか気づかない誤りを消す方向に働くので、和集合で recall を上げる SWR-Bench の集約とは向きが違う

#### 業務の C++ での欠陥に焦点を当てた自動レビュー

- **書誌**: Lu, Jiang, Li, Fang, Zhang, Yang, Zuo「Towards Practical Defect-Focused Automated Code Review」2025、ICML 2025（Spotlight）、[arXiv:2505.17928](https://arxiv.org/abs/2505.17928)。**HTML 本文（要約経由）**
- **主張と実測**: 日次利用者約4億人の企業の推薦サービス（C++）で、実際に損失を出したバグの merge request を使った。役割は Reviewer・Meta-Reviewer（複数の Reviewer の集約）・Validator・Translator。コードスライスで文脈を取ると key bug inclusion が23.70%（差分だけ）→37.04%（Left Flow）。Reviewer を1人→3人で26.67%→31.11%、誤警報率も83.26%→87.81%に上がる。Validator は誤警報を下げるが key bug inclusion も下げる（Full Flow・3人で 31.11%→20.00%）。検証の質問は「些細な指摘か」「偽の問題か」「どれほど重大か」
- **答える問い**: 問い1、問い3、問い4
- **持ち込める限界**: LLaMA 3.1 405B などのオープンモデル。誤警報率が75%を超えており、そのまま運用できる水準ではない
- **当てはめうる場所（案）**: 集約と検証の2段の間の釣り合い（recall と誤警報）を、Critical・High の数え方に入れる

### 3-D. 並列・集約・合議

#### 討論か投票か

- **書誌**: Choi, Zhu, Li「Debate or Vote: Which Yields Better Decisions in Multi-Agent Large Language Models?」2025、NeurIPS 2025、[arXiv:2508.17536](https://arxiv.org/abs/2508.17536)。**HTML 本文（要約経由）**
- **主張と実測**: Qwen2.5-7B と Llama3.1-8B、7ベンチマーク。平均正答率は多数決 0.7691 対 分散型の討論（2周）0.7377（Qwen）。討論は信念の軌跡に martingale を作り、期待される正しさを上げない。討論が投票を上回るのは、正しい答えを保持させるなど更新に正しさへの偏りを入れたときだけ
- **原文（訳、要約経由）**: 「Majority Voting alone accounts for most of the performance gains typically attributed to MAD」（討論の利得とされるものの大半は、多数決だけで説明がつく）
- **答える問い**: 問い1、問い4
- **持ち込める限界**: 小さいオープンモデル、正解が1つの課題
- **当てはめうる場所（案）**: 3・6・9回目の「敵対的レビュワーを説得する」段（[CLAUDE.md:731](../../../../CLAUDE.md#L731)）で、往復の討論に頼らず独立の判定を並べる

#### 繰り返しのサンプリングで被覆が伸びる

- **書誌**: Brown, Juravsky, Ehrlich, Clark, Le, Ré, Mirhoseini「Large Language Monkeys: Scaling Inference Compute with Repeated Sampling」2024、[arXiv:2407.21787](https://arxiv.org/abs/2407.21787)。**abstract**
- **原文（訳）**: 「coverage -- the fraction of problems that are solved by any generated sample -- scales with the number of samples over four orders of magnitude」（どれか1本で解けた問題の割合は、サンプル数とともに4桁にわたって伸びる）。SWE-bench Lite で1本15.9%→250本56%。一方「majority voting and reward models plateau beyond several hundred samples」（自動の検証器が無い領域では、多数決と報酬モデルによる選択は数百本で頭打ち）
- **答える問い**: 問い1（独立な試行の和集合は伸びるが、選別の段が律速になる）
- **持ち込める限界**: 解の生成の話で、レビューの指摘の和集合ではない

#### 自由形式の出力の self-consistency

- **書誌**: Chen ほか「Universal Self-Consistency for Large Language Model Generation」2023、[arXiv:2311.17311](https://arxiv.org/abs/2311.17311)。**abstract**
- **主張**: 答えの形が揃わない自由形式の候補から、LLM 自身に最も一貫したものを選ばせる。コード生成で実行による投票に並ぶ
- **答える問い**: 問い1（文章の指摘の一覧でも、LLM による集約の段が作れる根拠）

#### 小さいモデルの審査員を並べる

- **書誌**: Verga ほか「Replacing Judges with Juries: Evaluating LLM Generations with a Panel of Diverse Models」2024、[arXiv:2404.18796](https://arxiv.org/abs/2404.18796)。**abstract**
- **原文（訳）**: 「using a PoLL composed of a larger number of smaller models outperforms a single large judge, exhibits less intra-model bias due to its composition of disjoint model families, and does so while being over seven times less expensive」（系統の違う小さいモデルを多数並べると、単一の大きな審査員を上回り、同じモデル内の偏りが小さく、費用は7分の1以下）
- **答える問い**: 問い4
- **持ち込める限界**: このリポジトリで使える系統は限られる（Claude Code の中では Claude のみ。Codex のプラグインはある）。費用の制約（`claude -p` と API は使えない）とも合わせて考える必要がある

### 3-E. 偽陽性を減らす検証

#### BitsAI-CR: 検出と検証の2段

- **書誌**: Sun ほか（Tao Sun ほか12名）「BitsAI-CR: Automated Code Review via LLM in Practice」2025、第33回 FSE の予稿集（ACM DOI 10.1145/3696630.3728552。どのトラックかは確かめていない）、[arXiv:2501.15134](https://arxiv.org/abs/2501.15134)。**HTML 本文（要約経由）**
- **主張と実測**: 5言語で219のレビュー規則。検出（RuleChecker）57.03% → 検証（ReviewFilter）を足して65.59%、18週で本番の precision 75.0%。検証の出力形式は「結論だけ」63.27%（1.7秒）、「理由を先」65.80%（31.0秒）、「結論を先・理由を後」77.09%（1.7秒）。似たコメントは埋め込みの類似度でまとめて1件だけ残す。recall より precision を優先し、その理由を「alert fatigue に似て、コメントが多いと開発者は全部を無視する」と書く。オフラインの recall は約39.77%
- **答える問い**: 問い4
- **持ち込める限界**: 社内の規則分類と大量のデータで回した微調整モデル。Go の変更で「Outdated Rate」（指摘された行が1週間以内に書き換えられた割合）26.7%
- **当てはめうる場所（案）**: Critical・High を数える前に、「結論を先に書かせる」形式の検証の段を置く

#### LLM 批評役は LLM のバグを捕まえる（CriticGPT）

- **書誌**: McAleese, Pokorny, Cerón Uribe, Nitishinskaya, Trębacz, Leike「LLM Critics Help Catch LLM Bugs」2024、OpenAI、[arXiv:2407.00215](https://arxiv.org/abs/2407.00215)。**本文（PDF を起こして確認）**
- **主張と実測**: 人間が意図的に入れたバグを、RLHF で訓練した批評役が、コードレビューで報酬を払った人間より多く見つけた。自然に起きた誤りでは、モデルの批評が63%で人間の批評より好まれた
- **原文（訳）**: 「models which hallucinate bugs more often are also more likely to catch human inserted and previously detected bugs. We see this as analogous to precision and recall」（でっち上げのバグを多く出すモデルほど、入れたバグも見つけやすい。これは precision と recall の関係に似ている）。「longer critiques are, however, also more likely to include hallucinations and nitpicks」（長い批評ほど、でっち上げと些細な指摘を含みやすい）。「the rate of nitpicks and hallucinated bugs is much higher for models than for humans」（些細な指摘とでっち上げの率は、人間よりモデルのほうがはるかに高い）
- **答える問い**: 問い1、問い4（網羅と偽陽性の釣り合い。長さの調整＝FSBS で釣り合いを取る）
- **持ち込める限界**: 訓練した専用の批評役。単一ファイル・短い課題で、複数ファイルやリポジトリの移動を含まないと著者が限界に書いている
- **当てはめうる場所（案）**: `/code-review` の effort level が高いほど「uncertain findings」を含むという説明と同じ向きの証拠として、Critical・High の真偽を別に確かめる

#### 敵対的な段で候補を落とす（Refute-or-Promote）

- **書誌**: Agarwal「Refute-or-Promote: An Adversarial Stage-Gated Multi-Agent Review Methodology for High-Precision LLM-Assisted Defect Discovery」2026、[arXiv:2604.19049](https://arxiv.org/abs/2604.19049)。**HTML 本文（要約経由）**。**1人の運用者による31日間の事例研究で、独立の追試は無い**
- **主張と実測**: 批評役に「改善・評価」ではなく「否定する」役を与え、討論で合意に向かわせない。候補約171件の約79%を公開前に落とした（前向きの30件では83%）。OpenSSL で80以上のエージェントが一致して認めた存在しない脆弱性を、コンパイルしたテスト1本が否定した。原因は全員が同じ誤った前提を共有していたこと。別系統のモデルの批評役が、同系統で通った修正19件中3件に問題を見つけた
- **原文（訳、要約経由）**: 「One test killed what 80+ agents' reasoning could not」（80以上のエージェントの推論が落とせなかったものを、テスト1本が落とした）
- **答える問い**: 問い4（全員一致は正しさの保証にならない。実行による検証と別系統の批評役）
- **持ち込める限界**: 証拠が弱い（事例研究）。脆弱性の発見でコードレビューではない
- **当てはめうる場所（案）**: 「敵対的レビュワー」（[CLAUDE.md:731](../../../../CLAUDE.md#L731)）の判定を、推論の合意でなく、再現・テストで裏を取れる形に寄せる

#### 静的解析の偽陽性を LLM で落とす

- **書誌**: Du ほか「Reducing False Positives in Static Bug Detection with LLMs: An Empirical Study in Industry」2026、[arXiv:2601.18844](https://arxiv.org/abs/2601.18844)。**abstract**
- **主張と実測**: Tencent の警告433件（偽陽性328・真陽性105）。LLM と静的解析の組み合わせで偽陽性の94〜98%を除き、recall も高い。手作業の確認は1件10〜20分
- **答える問い**: 問い4（検出と検証を分けると偽陽性は大きく落ちる）
- **持ち込める限界**: 静的解析の警告という構造化された入力

#### 誤解を誘うコメントは最新のレビュワーをだませない

- **書誌**: Thornton「LLM Code Reviewers Are Harder to Fool Than You Think」2026、[arXiv:2602.16741](https://arxiv.org/abs/2602.16741)。**HTML 本文（要約経由）**
- **主張と実測**: 「既にセキュリティレビュー済み」のようなコメントをコードに入れても、Claude Opus 4.6・GPT-5.2 など8モデルで見逃し率の変化は −5%〜+4%（有意差なし）。商用モデルの検出率は89〜96%。SAST との照合で96.9%
- **答える問い**: 問い4（コードの中の説明文そのものは検出を大きく曲げない）。FlipFlop・Who Flips（会話の中で反論する形）とは条件が違う
- **持ち込める限界**: 脆弱性のパターン。論理や時間の絡む欠陥は苦手と著者が書く

### 3-F. フィードバックの質・文脈

#### 道具で裏付けた批評（CRITIC）

- **書誌**: Gou, Shao, Gong, Shen, Yang, Duan, Chen「CRITIC: Large Language Models Can Self-Correct with Tool-Interactive Critiquing」2023、ICLR 2024、[arXiv:2305.11738](https://arxiv.org/abs/2305.11738)。**ar5iv（要約経由）**
- **主張と実測**: ChatGPT で、道具あり 対 道具なしの批評。AmbigNQ F1 74.9 対 67.3、HotpotQA 52.9 対 46.1、GSM8K 78.2 対 77.0。利得は1〜2周に集中
- **原文（訳、要約経由）**: 「exclusive reliance on self-correction without external feedback may yield modest improvements or even deteriorate performance」（外部の指摘なしの自己修正だけに頼ると、改善は小さいか、悪化さえする）
- **答える問い**: 問い2、問い3

#### 自己デバッグ（実行結果を返す）

- **Self-Debugging**: Chen, Lin, Schärli, Zhou「Teaching Large Language Models to Self-Debug」2023、ICLR 2024、[arXiv:2304.05128](https://arxiv.org/abs/2304.05128)。**abstract と検索結果**。ユニットテストがある課題で最大12%改善、テストの無い Spider では2〜3%。10倍以上の候補を作るベースラインに並ぶ
- **自己生成テストの偏り**: Chen ほか「Revisit Self-Debugging with Self-Generated Tests for Code Generation」2025、ACL 2025、[aclanthology 2025.acl-long.881](https://aclanthology.org/2025.acl-long.881/)。**abstract**。実行後の自己デバッグは自分で作ったテストの偏りに苦しむ
- **Reflexion**: Shinn ほか 2023、NeurIPS 2023、[arXiv:2303.11366](https://arxiv.org/abs/2303.11366)。**abstract**。HumanEval で pass@1 91%。MBPP で自己生成テストの偽陽性により GPT-4 を下回ったという点は、検索結果の要約にしか無く、本文で確かめていない
- **答える問い**: 問い3（外部の信号は効く。ただし自分で作った信号は偏る）
- **当てはめうる場所（案）**: Go の変更では、指摘の真偽をテストで裏付ける。テストは実装者と別の文脈で書かせるか、既存のテストを使う

#### AI レビューのコメントが変更につながる条件

- **GitHub Actions の AI レビュー**: Sun ほか「Does AI Code Review Lead to Code Changes? A Case Study of GitHub Actions」2025（v2 は 2026）、[arXiv:2508.18771](https://arxiv.org/abs/2508.18771)。**abstract**。16のアクション、178リポジトリ、22,000件超のコメント。「Comments that are concise, contain code snippets, and are manually triggered, particularly those from hunk-level review tools, are more likely to result in code changes.」（短く、コード片を含み、手動で起動され、とくに hunk 単位のツールのコメントほど、コードの変更につながる）
- **エージェントのレビューコメントへの開発者の反応**: Cynthia, Widyasari, Roy, Zhang, Lo 2026、[arXiv:2607.21997](https://arxiv.org/abs/2607.21997)。**HTML 本文（要約経由）**。342リポジトリの54,791件。解決率は Copilot 72.9%・Cursor 67.2%・Codex 54.8%。インラインのコード提案でオッズ比1.62、長いコメントは0.93。未解決470件の分類で最多は「意図した設計」112件、次が「誤った提案」67件
- **Go のレビューで一覧を挙げさせる**: Sun ほか「Improving LLM-Based Go Code Review through Issue-List Generation and Context Augmentation」2026、[arXiv:2606.01859](https://arxiv.org/abs/2606.01859)。**HTML 本文（要約経由）**。CodeReviewer 由来の Go の1,438件、DeepSeek-V3（温度0）。「最も重要な1件」より「一覧」で +4.68ポイント（RefineEM 17.15%→21.83%）。文脈は隣接と類似の変更の2つが最良（25.59%）で、3つ全部（24.87%）より上
- **答える問い**: 問い3、問い4
- **持ち込める限界**: 開発者が人間の OSS。Go の研究は20行以下の hunk
- **当てはめうる場所（案）**: 対応表とレビュー出力の形（1件ごとにコード片と位置、短く）

#### 業務でのレビューボットの導入

- **ICSE SEIP 2025**: Cihan, Haratian, İçöz, Gül, Devran, Bayendur, Uçar, Tüzün「Automated Code Review In Practice」、[arXiv:2412.18531](https://arxiv.org/abs/2412.18531)。**HTML 本文（要約経由）**。Qodo PR-Agent（GPT-4-32K）。コメントの73.8%が解決、PR のクローズまでの平均が5時間52分→8時間20分。欠点として「faulty reviews, unnecessary corrections, and irrelevant comments」（誤ったレビュー、不要な修正、関係の無いコメント）
- **WirelessCar の事例**: Dalsteinsson ほか「Rethinking Code Review Workflows with LLM Assistance: An Empirical Study」2025、[arXiv:2505.16339](https://arxiv.org/abs/2505.16339)。**HTML 本文（要約経由）**。参加者の声として「if they're not good enough, you stop reading them」（質が足りないと読まなくなる）
- **レビューエージェントの実態**: Chowdhury ほか「From Industry Claims to Empirical Reality: An Empirical Study of Code Review Agents in Pull Requests」2026、[arXiv:2604.03196](https://arxiv.org/abs/2604.03196)。**abstract**。エージェントだけがレビューした PR のマージ率45.20%、人間だけ68.37%。13のエージェントのうち12が有効な指摘の比率60%未満
- **レビューボットの足跡**: Fatima ほか「On the Footprints of Reviewer Bots Feedback on Agentic Pull Requests in OSS GitHub Repositories」2026、MSR 2026、[arXiv:2604.24450](https://arxiv.org/abs/2604.24450)。**abstract**。「reviewer bots should prioritize targeted high-relevance feedback over generating large numbers of comments」（大量のコメントより、的を絞った関連の高い指摘を優先すべき）
- **答える問い**: 問い4、問い5

### 3-G. 人間のコードレビューとインスペクション

#### 観点を分けた読み方（perspective-based reading）

- **書誌**: Basili, Green, Laitenberger, Lanubile, Shull, Sørumgård, Zelkowitz「The Empirical Investigation of Perspective-Based Reading」、Empirical Software Engineering 誌（[DOI 10.1007/BF00368702](https://link.springer.com/article/10.1007/BF00368702)）。**本文（UMD の PDF を起こして abstract を確認）**。発行年はページで確かめていない
- **主張と実測**: NASA の開発者で要求文書のインスペクション実験。テスター・開発者・利用者の観点に分けて読ませた
- **原文（訳）**: 「Our assumption is that the combination of different perspectives provides better coverage of the document, i.e., uncovers a wider range of defects, than the same number of readers using their usual technique.」（異なる観点の組み合わせは、同じ人数がいつもの読み方で読むより、文書の被覆がよく、より広い範囲の欠陥を見つけるという仮説である）。「Teams applying PBR are shown to achieve significantly better coverage of documents than teams that do not apply PBR.」（PBR を使うチームは、使わないチームより有意に高い被覆を示した）
- **反対の知見**: 追試で、3つの観点の間で検出率と被覆に有意差が無かったという報告がある（「Are the perspectives really different?」ほか。**検索結果の要約のみ**）
- **答える問い**: 問い1（並列のレビュワーの観点を分けると、和集合が広がる）
- **持ち込める限界**: 人間の要求文書のインスペクション。LLM で観点を分けると重なりが減るかは測られていない（見つからなかった）
- **当てはめうる場所（案）**: 並列に立てるレビュワーに、コードの正しさ・規則文書の整合・退行、のように観点を割り当てる

#### 独立したレビュワーの重なりで残りの欠陥を見積もる（capture-recapture）

- **書誌**: Petersson, Thelin, Runeson, Wohlin「Capture-recapture in Software Inspections after 10 Years Research – Theory, Evaluation and Application」2004、Journal of Systems and Software 72(2)、[著者の PDF](https://wohlin.eu/jss04-1.pdf)。**本文（PDF を起こして該当節を確認）**。Briand, El-Emam, Freimut, Laitenberger「A Comprehensive Evaluation of Capture-Recapture Models for Estimating Software Defect Content」2000、IEEE TSE（[IEEE Xplore 852741](https://ieeexplore.ieee.org/document/852741/)）は**検索結果の要約のみ**
- **原文（訳）**: 「The method uses the overlap between the sets of faults found by different reviewers to estimate the fault content. It is assumed that the reviewers work independently of each other and therefore the fault searching has to be performed before, and not during, an inspection meeting.」（異なるレビュワーが見つけた欠陥の集合の重なりから、欠陥の総数を見積もる。レビュワーは互いに独立して作業すると仮定するので、欠陥探しはインスペクション会議の前に行い、会議中には行わない）。推定量の1つ（Mh-JK）は「accurate when the number of reviewers is 4 or more」（レビュワーが4人以上なら正確）で、2人では過小に見積もる
- **答える問い**: 問い1（「もう1周要るか」を、独立したレビューの重なりで決める考え方）
- **持ち込める限界**: 独立性の仮定。同じモデルの複数回は前提（学習データ）を共有するので独立ではない（Refute-or-Promote の全員一致の誤りがその例）。LLM のレビューに当てはめた研究は見つからなかった
- **当てはめうる場所（案）**: [CLAUDE.md:543](../../../../CLAUDE.md#L543) の「回数を数える」に、「同じ周の独立レビューの重なりが大きければ次の周を回さない」という止め方の候補として

#### インスペクション会議は要るか

- **書誌**: Votta「Does every inspection need a meeting?」1993、ACM SIGSOFT Software Engineering Notes（[DOI 10.1145/167049.167070](https://dl.acm.org/doi/10.1145/167049.167070)）。**検索結果の要約のみ（ACM は 403）**
- **主張（検索結果の要約）**: 会議で得る欠陥より、会議で失われる欠陥（個人だけが見つけていたもの）が多く、会議は費用が大きい
- **答える問い**: 問い1（合議より独立の和集合）。数値は使わない

#### レビューで何が直るか

- **書誌**: Beller, Bacchelli, Zaidman, Juergens「Modern Code Reviews in Open-Source Projects: Which Problems Do They Fix?」2014、MSR 2014、[著者の PDF](http://sback.it/publications/msr2014.pdf)。**本文（PDF を起こして abstract と該当節を確認）**
- **原文（訳）**: 「featuring the similar 75:25 ratio of maintainability-related to functional problems. We also reveal that 7–35% of review comments are discarded and that 10–22% of the changes are not triggered by an explicit review comment.」（保守性と機能の問題の比が75:25で先行研究と同じ。レビューコメントの7〜35%は捨てられ、変更の10〜22%は明示的なコメントなしに起きる）。「bug-fixing tasks lead to fewer changes and tasks with more altered files and a higher code churn have more …」（バグ修正の課題は変更が少なく、変更ファイルが多くコードの変更量が多い課題は多い）
- **答える問い**: 問い5
- **持ち込める限界**: 2つの OSS（Java）、人間のレビュー

#### 設計の議論と混乱

- **設計の議論**: El Zanaty, Hirao, McIntosh, Ihara, Matsumoto「An Empirical Study of Design Discussions in Code Review」2018、ESEM 2018、[SWAG Lab のページ](https://swag.uwaterloo.ca/publications/an-empirical-study-of-design-discussions-in-code-review.html)。**abstract**。OpenStack の220レビュー・2,817コメント。設計のコメントは9〜14%で、73%は提案を伴う。「code changes that have design-related feedback have a statistically significantly increased rate of abandonment」（設計に関する指摘が付いた変更は、放棄される率が統計的に有意に高い）
- **混乱**: Ebert, Castor, Novielli, Serebrenik「An Exploratory Study on Confusion in Code Reviews」2021、Empirical Software Engineering、[著者のページ](https://felipeebert.github.io/publication/ebert-emse-2021/)。**abstract（要約経由）**。理由30種・影響14種・対処13種
- **答える問い**: 問い5（設計の未決と変更の理由の欠落が往復を増やす）
- **当てはめうる場所（案）**: [.claude/rules/design-review.md:3](../../../../.claude/rules/design-review.md#L3) の段2（設計を issue のコメントに書く）に、変更の理由を必ず含める

### 3-H. 仕様・受け入れ条件を先に固める

- **TiCoder**: Fakhoury, Naik, Sakkas, Chakraborty, Lahiri「LLM-Based Test-Driven Interactive Code Generation: User Study and Empirical Evaluation」2024、IEEE TSE 50(9)、[arXiv:2404.10100](https://arxiv.org/abs/2404.10100)。**abstract**。テストで意図を部分的に形式化する対話。15人の利用者実験で、AI のコードを正しく評価できる割合が有意に上がり、認知負荷が下がった。4つの LLM で、5回以内のやりとりで pass@1 が平均で絶対値45.97%改善（abstract の数値。検索結果の要約は45.73%と書いており、版で違う可能性がある）
- **ClarifyGPT**: Mu ほか「ClarifyGPT: Empowering LLM-based Code Generation with Intention Clarification」2023、FSE 2024、[arXiv:2310.10996](https://arxiv.org/abs/2310.10996)。**abstract**。曖昧な要求を検出して質問し、要求を直してから生成。GPT-4 の MBPP-sanitized で pass@1 70.96%→80.80%
- **NaPiRE**: Méndez Fernández ほか「Naming the Pain in Requirements Engineering: Contemporary Problems, Causes, and Effects in Practice」2016、Empirical Software Engineering、[arXiv:1611.10288](https://arxiv.org/abs/1611.10288)。**abstract**。10か国228社の調査で、21の問題を重要度で分析。「不完全・隠れた要求が最多」は**検索結果の要約のみ**
- **TDD のメタ分析**: Rafique, Mišić「The Effects of Test-Driven Development on External Quality and Productivity: A Meta-Analysis」2013、IEEE TSE 39(6)。**検索結果の要約のみ（ResearchGate は 403）**。品質に小さな正の効果、生産性は結論が出ない
- **答える問い**: 問い3、問い5（受け入れ条件を実装前に固めると、後の指摘の往復が減る方向の証拠）。**実装前に仕様を固めることが、レビューの周回数そのものを減らすと直接測った研究は見つからなかった**
- **持ち込める限界**: 生成の正確さの研究で、レビューの周回ではない
- **当てはめうる場所（案）**: [.claude/rules/design-review.md:3](../../../../.claude/rules/design-review.md#L3) の段1〜4 で、受け入れ条件を「確かめ方（テスト・コマンド・行）」の形で書く

### 3-I. 修正漏れの担当と重なるもの（題名だけ）

修正漏れは別の worker の担当なので、重なりうるものの題名だけを置く。この worker は「同様の箇所の直し漏れ」の文献（systematic edits・recurring fixes など）を検索していない。

- LLMs cannot find reasoning errors, but can correct them given the error location（Tyen ほか 2024）
- Structure Enables Effective Self-Localization of Errors in LLMs（Samanta ほか 2026、[arXiv:2602.02416](https://arxiv.org/abs/2602.02416)。abstract: 推論を段に分けると誤りの位置を自分で特定しやすく、oracle 付きで20〜40%の自己修正の上積み）
- Evaluating Large Language Models for Code Review（Cihan ほか 2025）
- Human-AI Synergy in Agentic Code Review（Zhong ほか 2026）
- Modern Code Reviews in Open-Source Projects: Which Problems Do They Fix?（Beller ほか 2014）

---

## 4. 否定的・相反する知見

**言いたいこと。**「並列にして集約すれば良い」も「周回は無駄」も、条件によって崩れる。持ち込むときは下の反例を当てること。

| 知見 | 反する知見 | どちらに寄せるかの材料 |
| --- | --- | --- |
| SWR-Bench: 複数レビューの LLM による統合で recall が上がる | ReaLMistake: self-consistency と多数決は誤り検出を改善しない | 統合（和集合）と多数決（共通部分に近い）は向きが逆。recall を上げたいなら和集合、precision を上げたいなら検証の段 |
| Lu ほか: レビュワーを増やすと key bug inclusion が上がる | 同じ論文で誤警報も上がる。Validator で誤警報を下げると key bug inclusion も下がる | 集約と検証は釣り合いで、ただで両方は上がらない |
| Huang ほか・Stechly ほか: 外部の信号なしの自己修正は悪化する | Chen ほか 2026 の付録: 推論型モデルは既に自分で直す。SCoRe: 訓練で直せる。Song ほか: 生成と検証の差はモデルが大きいほど広がる | 2023〜2024 年のモデルの否定的な結果を、最新の Claude へそのまま当てはめるのは言い過ぎになりうる |
| Olausson ほか: 自己修復の利得は小さい | Arimbur 2026: 例外メッセージを返すと全モデル・全ベンチマークで改善、2周で大半 | 外部の信号が明確なら修復の周回は効く。レビュー指摘の真偽が不確かなら効き方は落ちる |
| Self-Refine: 反復で約20ポイント改善 | Huang ほか: 初回プロンプトを適切にすると改善が消える | 比較の相手が弱いと周回の効果は大きく見える |
| Basili ほか: 観点を分けると被覆が上がる | 追試で観点の間に差が無かった（検索結果のみ） | LLM で観点を分けたときの効果は測られていない |
| Thornton 2026: コードの中の誤解を誘うコメントでは最新モデルはだまされない | FlipFlop・Who Flips: 会話の中で反論されると答えを変える | 実装側の説明を「成果物の中の文」として渡すのと「会話の反論」として渡すのでは結果が違いうる |
| Snyk 2026: 本物は繰り返しで安定する | 同じ論文: effort level や費用を上げても安定・性能が上がるとは限らない | 「高い effort で1回」より「独立に複数回」の根拠になるが、モデルと effort の組み合わせで大きく違う |
| Refute-or-Promote: 敵対的な段で偽陽性の大半を落とせる | 1人の事例研究で追試が無い。全員一致の誤りも起きた | 推論の合意ではなく、テストや再現のような外部の裏付けが決め手 |
| Cihan ほか 2024: レビューボットで指摘の73.8%が解決 | 同じ論文: PR のクローズまでの時間が延びた。Chowdhury ほか 2026: エージェントだけのレビューはマージ率が低い | 指摘が多いこと自体が周回と時間を増やす |

---

## 5. 探したが見つからなかったもの

**言いたいこと。**このリポジトリの問いにいちばん近い「直列の再レビューと並列の集約で、収束までの周回数を比べた研究」は見つからなかった。

| 探したもの | 検索語（WebSearch） | 場所 | 結果 |
| --- | --- | --- | --- |
| LLM コードレビューで、直列の再レビューと並列の集約を収束までの周回数で比べた研究 | `AI code review bot follow-up commits new comments after fixes review rounds empirical study pull requests CodeRabbit Copilot 2026 arXiv`、`iterative self-refinement code generation introduces new bugs regressions number of iterations study 2025 arXiv "refinement" degrade` | Web 全般（arXiv を含む） | 見つからなかった。近いのは SWR-Bench（同じ周での集約）と Zhong ほか（往復の数） |
| 差分だけの再レビューと全体の再レビューの比較 | 上の2つの検索語 | Web 全般 | 見つからなかった |
| 日本語の長い規則文書を LLM でレビューしたときの再現率 | 専用の検索はしていない（上の検索と SWR-Bench・ReaLMistake の対象を確認した範囲で無かった） | — | 見つからなかった（探し方が弱いことを明記する） |
| 最新の Claude で PR 単位のレビューの recall を測った研究 | `SWR-Bench LLM code review benchmark arXiv 2025 multi-review aggregation`、`LLM code review non-determinism consistency repeated runs same pull request different comments study arXiv` | Web 全般 | PR 単位では見つからなかった。Snyk VulnBench（セキュリティ監査）と Thornton（脆弱性のパターン）が Claude Opus 4.6 を含む |
| チェックリストで LLM レビューの recall が上がるかの実証 | `checklist prompting LLM code review effectiveness empirical study recall improvement 2025` | Web 全般 | 見つからなかった。近いのは「一覧で挙げさせる」Go の研究 |
| LLM のレビュワーで観点を分けると指摘の重なりが減るか | `perspective-based reading software inspection experiment defect detection team coverage Basili` | Web 全般 | 人間の研究だけ。LLM では見つからなかった |
| LLM のレビューに capture-recapture を当てた研究 | `capture-recapture software inspection estimate remaining defects independent inspectors overlap` | Web 全般 | 人間のインスペクションの研究だけ |
| 人間のレビューで周回（patch set）の数の原因を直接分析した研究 | `empirical study code review iterations revisions patch sets Gerrit why patches need multiple revisions`、`code review rounds rework reasons empirical "review rounds" OR "review iterations" causes misunderstanding design discussion OpenStack Qt study` | Web 全般 | 「Reviewing rounds prediction for code patches」（EMSE 2021）が見つかったが開けなかった（6節）。ほかは近い代理指標だけ |
| 実装前に仕様を固めるとレビューの周回が減ることを直接測った研究 | `ClarifyGPT clarifying questions ambiguous requirements code generation FSE 2024 results`、`TiCoder test-driven user intent formalization interactive code generation evaluation TSE`、`NaPiRE incomplete underspecified requirements causes of project failure rework empirical Méndez Fernández` | Web 全般 | 生成の正確さの研究だけ |
| LLM のレビューの重大度の判定が過大になるかの研究 | `LLM code review severity classification accuracy overestimate severity critical false positives study arXiv` | Web 全般 | 重大度の較正を直接測った研究は見つからなかった |

そのほかに叩いた検索語（出発点の論文の存在確認と周辺）:
`"Is Self-Repair a Silver Bullet for Code Generation" arXiv`、`"Large Language Models Cannot Self-Correct Reasoning Yet" Huang arXiv`、`"When Can LLMs Actually Correct Their Own Mistakes" critical survey self-correction TACL`、`"Security Degradation in Iterative AI Code Generation" arXiv 2025`、`"LLM Critics Help Catch LLM Bugs" CriticGPT arXiv comprehensiveness hallucination`、`"LLMs cannot find reasoning errors, but can correct them" Tyen arXiv`、`"On the Self-Verification Limitations of Large Language Models on Reasoning and Planning Tasks" Stechly`、`"Debate or Vote" multi-agent debate majority voting arXiv 2025`、`BitsAI-CR automated code review industrial ByteDance arXiv precision ReviewFilter`、`"Automated Code Review In Practice" ICSE SEIP 2025 LLM industry study`、`"Self-Refine: Iterative Refinement with Self-Feedback" Madaan arXiv iterations diminishing`、`"Chain-of-Verification Reduces Hallucination" factored verification arXiv`、`"Pride and Prejudice: LLM Amplifies Self-Bias in Self-Refinement" arXiv`、`"Towards Practical Defect-Focused Automated Code Review" arXiv key bug inclusion false alarm`、`"AI-Assisted Assessment of Coding Practices in Modern Code Review" AutoCommenter Google`、`"Replacing Judges with Juries" panel of LLM evaluators PoLL arXiv`、`"Large Language Monkeys" repeated sampling coverage verification arXiv`、`"Modern code reviews in open-source projects: which problems do they fix" Beller evolvability functional defects`、`"Evaluating LLMs at Detecting Errors in LLM Responses" ReaLMistake recall`、`"Teaching Large Language Models to Self-Debug" execution feedback results arXiv`、`"CRITIC: Large Language Models Can Self-Correct with Tool-Interactive Critiquing" results without tools`、`LLM sycophancy "Are you sure?" flip answers when challenged FlipFlop experiment arXiv`、`"Universal Self-Consistency" free-form generation arXiv Chen 2023`、`code review effectiveness change size number of defects found empirical study large patches reviewers miss`、`"Does AI Code Review Lead to Code Changes" GitHub Actions empirical study 2025`、`"Mind the Gap" self-improvement generation-verification gap LLM arXiv 2024`、`reasoning models intrinsic self-correction effective 2025 study o1 DeepSeek-R1 self-verification improves arXiv`、`"LLM Evaluators Recognize and Favor Their Own Generations" self-preference`、`"A comprehensive evaluation of capture-recapture models for estimating software defect content" Briand abstract`、`"test-driven development" meta-analysis external quality productivity Rafique Misic TSE`、`self-correction frontier LLMs 2026 study intrinsic self-correction still fails OR succeeds Claude GPT-5 reasoning models arXiv`、`"An exploratory study on confusion in code reviews" arXiv Ebert Castor Novielli Serebrenik reasons missing rationale`、`"Reflexion: Language Agents with Verbal Reinforcement Learning" HumanEval self-generated tests false positive`、`Votta "Does every inspection need a meeting" meeting gains defects found individual preparation`

**出発点に挙がっていた題名は、すべて存在を確かめた**（Self-Refine・Reflexion・Is Self-Repair a Silver Bullet・Large Language Models Cannot Self-Correct Reasoning Yet・When Can LLMs Actually Correct Their Own Mistakes・CRITIC・反復でセキュリティが劣化する研究・SWR-Bench）。「CodeReviewer」は Sun ほか 2026 の Go の研究のデータの出どころとして出てきたが、CodeReviewer そのものの論文は開いていない。Google の AutoCommenter（AIware 2024、[arXiv:2405.13565](https://arxiv.org/abs/2405.13565)）は存在だけ確かめ、本文を読んでいない。

---

## 6. 開けなかった URL

| URL | 何が起きたか | 代わりにしたこと |
| --- | --- | --- |
| https://arxiv.org/pdf/2306.09896v5 ほか PDF の直リンク（2306.09896・2310.01798・2311.08516・CriticGPT・Basili ほか・Beller ほか・Petersson ほか） | WebFetch が PDF を読めず、文字化けとして返した | WebFetch が保存した PDF を pypdf で起こして読んだ（開けた扱い） |
| https://dl.acm.org/doi/10.1145/167049.167070 | 403 | 検索結果の要約だけを使い、数値は使わなかった |
| https://www.semanticscholar.org/paper/Does-every-inspection-need-a-meeting-Votta/6e7f154e4984b5147b3401af2591b94f0fd867c7 | 中身が空で返った | 同上 |
| https://api.semanticscholar.org/graph/v1/paper/search（Votta と Rafique・Mišić の2件） | 429 | 同上 |
| https://www.researchgate.net/publication/260649027_The_Effects_of_Test-Driven_Development_on_External_Quality_and_Productivity_A_Meta-Analysis | 403 | 検索結果の要約だけを使った |
| https://ietresearch.onlinelibrary.wiley.com/doi/full/10.1049/iet-sen.2020.0134 | 403 | 「従来のコードレビューは平均で約60%の欠陥を見つける」という値は、出典を確かめられなかったので使わなかった |
| https://link.springer.com/article/10.1007/s10664-020-09909-5 | Springer の認証へ転送 | 著者のページで abstract を読んだ |
| https://link.springer.com/article/10.1007/s10664-021-10035-z（Reviewing rounds prediction for code patches） | Springer の認証へ転送 | 代わりが無く、使っていない |
| https://arxiv.org/html/2507.23640 | 404 | abs ページは試していない。使っていない |
