---
name: naming
status: decided
decided_name: continuo
target: herdr-symphony の Go 再実装版（ツール本体の名前）
updated: 2026-08-17
---

# 名前を決める

## 0. 決定（2026-08-17）

**名前は `continuo` に決定した。**

| 項目 | 内容 |
| --- | --- |
| 由来 | **通奏低音**（basso continuo）。バロック音楽で、曲の最初から最後まで途切れず鳴り続け、全体の和声を支える低音パート |
| なぜこれか | **常駐して裏で回り続け、全体の土台になる**という、このツールの性質そのものを一語で表す |
| 読み | コンティヌオ |
| GitHub の空き | 完全一致は `umd-mith/continuo`（5 star）の1件のみ |
| **未調査** | **商標・ドメイン・npm は未調査。**`/naming-research` と `/naming-domain-name` で確認する |

**残る弱点。**検索したとき `continuous-*` 系の語に埋もれる。実測でも `GoogleCloudPlatform/continuous-deployment-on-kubernetes` などが上位を占めた。**リポジトリの説明文で何をするツールかを明示して補う必要がある。**

---

## 1. 何に名前をつけるのか

**herdr の上に乗る Go 製の常駐プロセス。**herdr-symphony の単純な移植版ではないため、**元の名前を含む名前（`herdr-symphony-go` 等）にはしない。**この決定は 2026-08-13 に確定済みで、**先行調査の設計判断表にある**（記録は非公開のリポジトリにあるため、この文書からは参照できない）。

### このツールが何をするか（名前の材料）

**出典は先行調査の記録である。**非公開のリポジトリにあるため、この文書からは参照できない。**必要な結論は [docs/plans/continuo_design.md](plans/continuo_design.md) に転記してある。**

| 性質 | 内容 |
| --- | --- |
| 見張る対象 | GitHub Projects v2 のボード1枚。5リポジトリ分の issue が1枚に載る |
| 起動するもの | issue ごとに worktree を用意し、herdr のペインで Claude Code を対話モードで立てる |
| 続け方 | 同じセッションへ継続の指示を送り続ける turn ループ。上限は 20 turn |
| 完了の決め方 | ボードが唯一の真実の源。エージェント自身が Status を動かし、本体は読むだけ |
| 成果の残し方 | issue のコメント |
| 止まったときの扱い | 入力待ちが 60 秒続いたら止まったとみなしてボードを見に行く。stall 検知あり |
| 後片付け | 使い終わった worktree と branch を本体が消す |
| herdr との関係 | socket API を直叩き。CLI を spawn して stdout をパースしない |
| 実装の立場 | [openai/symphony](https://github.com/openai/symphony) の `SPEC.md`（Apache-2.0）に準拠。clean room |

**名前の材料として効くのは次の3点。**

1. **配って、待って、最後まで面倒を見る**のが役割。作業そのものはしない
2. **herdr の上に乗る**が、herdr-symphony の子孫ではない
3. **ボードが真実の源**で、本体は状態を持たない（再起動してもボードから復元する）

---

## 2. 方向性の決定（2026-08-17）

| 決定事項 | 内容 |
| --- | --- |
| **隠喩の方向** | **音楽の隠喩を続ける。**`SPEC.md` 準拠であることを名前で示す |
| **追加の軸** | **音楽家っぽい仮想の人の名前も可**（同日追加） |
| **公開の前提** | **OSS として公開するつもり。**既存 OSS との衝突回避を強く優先する |

**採らなかった方向。**herdr の牧畜の隠喩に寄せる案、隠喩を捨てて機能を名乗る案（boardrunner 等）、日本語の造語。

---

## 3. ラウンド1 — GitHub 上の衝突を実測（2026-08-17）

**調査方法。**`gh search repos --match name <語> --limit 40` で、**リポジトリ名がその語そのものになっているもの**を抽出した。star 数と description を確認している。**商標・ドメイン・npm は未調査**（`/naming-research` と `/naming-domain-name` の範囲）。

### 3-1. 同じ領域に取られていた語（除外）

**AI エージェントのオーケストレーション、または Claude Code 周辺で既に使われている。**

| 語 | 取っているもの | star |
| --- | --- | --- |
| **tutti** | `tutti-os/tutti`「Where people and agents build in tune.」 | 3282 |
| **tutti** | `nutthouse/tutti`「Multi-agent orchestration CLI — your agents, all together」 | 112 |
| **volta** | `VoltAgent/voltagent`「AI Agent Engineering Platform」 | 10367 |
| **volta** | `volta-cli/volta`「JS Toolchains as Code」 | 13053 |
| **rondo** | `roniel-rhack/rondo`「terminal productivity app that combines task management」 | 47 |
| **camerata** | `diegohce/camerata`「server orchestration set of tools」 | 11 |
| **attacca** | `adihebbalae/Attacca`「Context-engineering plugin marketplace for Claude Code」 | 3 |
| **attacca** | `attacca-ai/attacca`（org ごと押さえられている） | 0 |
| **sordino** | `sordino-sh/sordino`「PII mask/unmask reverse-proxy for Claude Code」 | 9 |
| **ripieno** | `ijmh2/ripieno`「shared workspace where several people and several AI agents」 | 1 |

### 3-2. 大きな既存プロジェクトに取られていた語（除外）

| 語 | 取っているもの | star |
| --- | --- | --- |
| fermata | `AndreyPavlenko/Fermata` | 1252 |
| libretto | `saffron-health/libretto`（AI toolkit） | 875 |
| segno | `heuer/segno`（Python QR エンコーダ） | 795 |
| ostinato | `pstavirs/ostinato`（パケットジェネレータ） | 773 |
| stretto | `benkaiser/stretto` / `al8n/stretto`（Rust キャッシュ） | 634 / 434 |
| tacto | `facebookresearch/tacto` | 470 |
| partitura | `CPJKU/partitura` | 370 |
| rubato | `HEnquist/rubato` / `andOrlando/rubato` | 355 / 266 |
| dacapo | `dacapobench/dacapobench` | 193 |

### 3-3. 空いていた語（候補として残る）

| 語 | 意味 | 完全一致の最大 star |
| --- | --- | --- |
| **continuo** | 通奏低音。曲の最初から最後まで途切れず鳴り、全体の和声を支える | 5（`umd-mith/continuo`） |
| **cadenzo** | cadenza（独奏者が自由に演奏し、指揮者は待つ部分）の男性形造語 | 0（`Shane-and-Lee/CADENZO`） |
| **ostino** | ostinato（執拗に繰り返す音型）の短縮造語 | **完全一致なし** |
| **soggetto** | フーガの主題。各声部が順に受け取って展開する | 完全一致なし |
| **kapelle** | ドイツ語で楽団。Kapellmeister（楽長）の語幹 | 0（4件） |
| **sostenuto** | 音を保ち続ける | 3 |
| **tenuto** | 音を保つ | 2 |
| **obbligato** | 省略できない必須の声部 | 5 |
| **anacrusis** | 弱起。本題の前に置く音 | 3 |
| **tacet** | その楽器は休み | 8 |
| **leggio** | イタリア語で譜面台 | 7（`maestro-os/leggio` が存在） |
| **ritornello** | 繰り返し戻ってくる部分 | 13 |
| **perpetuo** | moto perpetuo（無窮動）。止まらず動き続ける | 24（`njsmith/perpetuo` は **stall tracking** ツール） |
| **batuta** | スペイン語・イタリア語で指揮棒 | 25（同名リポジトリが18件。全て小規模） |
| **concertante** | 独奏群と合奏群が対話する形式 | 1 |

### 3-4. 人名として調べた語

| 語 | 完全一致の最大 star | 備考 |
| --- | --- | --- |
| tullio | 2 | 実在の指揮者 Tullio Serafin の名 |
| cosimo | 5 | |
| kaspar | 5 | |
| aldo | 8 | |
| silvio | 14 | |
| anselm | 25 | `trailofbits/anselm` |
| renzo | 5 | GitHub では小さいが Renzo Protocol（DeFi）が実在 |
| enzo | 82 | |

**人名そのものは音楽との結びつきが弱い。**音楽用語を人名化した造語（cadenzo・ostino）のほうが、音楽の隠喩と人名の両方を同時に満たす。

---

## 4. ラウンド2 — 音楽用語を人名化した造語（2026-08-17）

**狙い。**「音楽の隠喩」と「音楽家っぽい仮想の人の名前」を同時に満たす。音楽用語をイタリア系・ドイツ系の人名の形に変える。

### 4-1. 同じ領域に取られていた（除外）

| 語 | 取っているもの | star |
| --- | --- | --- |
| **concertino** | `matto00/concertino`「Harness-agnostic, evidence-gated **agent orchestra** for au…」 | 2 |

### 4-2. GitHub に完全一致が1件も無かった語

| 語 | 由来 | 何を表すか |
| --- | --- | --- |
| **ritorno** | ritornello（繰り返し戻ってくる部分）の語幹。イタリア語で「帰還」 | turn ごとに同じセッションへ戻る |
| **battuto** | battuta（拍を打つこと）の過去分詞。イタリア系の姓に見える | 拍を刻んで全体を進める |
| **kapelli** | Kapelle（ドイツ語で楽団）の人名化。Kapellmeister は楽長 | 楽団を率いる人 |
| **segnori** | segno（反復記号）＋ signore | 「ここへ戻れ」を指示する人 |
| **ostinelli** | ostinato ＋ イタリア姓の語尾。実在のイタリア姓でもある | 執拗に繰り返す |
| **batutti** | batuta（指揮棒）＋ 姓の語尾 | 指揮棒を持つ人 |
| **sostino** | sostenuto（音を保つ）の人名化 | セッションを保ち続ける |

### 4-3. 0 star の同名のみ（実質空き）

contino / tempori / maestrino / tessitore

---

## 5. 現時点の推薦

| 順位 | 候補 | 推す理由 | 弱点 |
| --- | --- | --- | --- |
| 1 | **continuo** | **意味の適合が最も高い。**曲の最初から最後まで途切れず鳴り続けて全体を支える低音パートは、常駐プロセスの説明そのもの | **検索で "continuous" に埋もれる。**8文字 |
| 2 | **cadenzo** | **設計思想と一致する。**カデンツァは独奏者が自由に弾き、指揮者は手を止めて待つ部分。エージェントが自律して本体は待つ構造と同じ。イタリア系の姓にも見える | 造語なので初見で意味が伝わらない |
| 3 | **ritorno** | **完全に空いている。**turn ごとに同じセッションへ戻る動きを表す。イタリア語の実在語で読みやすく、人名にも見える | 音楽用語としての知名度は ritornello より低い |
| 4 | **ostino** | **完全に空いている。**繰り返し続ける隠喩。人名にも見える | ostinato（773 star）と紛らわしい |

**次点。**kapelli（楽長）・battuto（拍を打つ）・segnori（戻れと指示する人）。いずれも GitHub に完全一致なし。

---

## 6. 次にやること

- [ ] 候補を1〜3個に絞る（人間の判断待ち）
- [ ] 絞り込み後に `/naming-research` で商標・競合を調査
- [ ] `/naming-domain-name` でドメインの空きを調査
