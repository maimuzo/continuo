# 訳語集（日本語 → 英語）

**言いたいこと。**`internal/i18n/messages/en.json` の訳は、ここに書いた語だけを使う。
**同じものを2つの英単語で呼ばない。**迷ったら README.md を grep して、そこにある言い回しを採る。
**以後の訳は、必ずこの文書を参照して決める。**新しい語を決めたら、その場でここへ足す。

---

## 1. どこから来た語か

**言いたいこと。**訳語の出典には順位がある。上にあるものが勝つ。
**README.md の英語が正である。**そこに無い語だけを、この文書で決める。

| 順位 | 出典 | 扱い |
| --- | --- | --- |
| 1 | [README.md](../../README.md) | **正。**ここに出てくる言い回しをそのまま使う |
| 2 | 設定のキー・CLI のフラグ・コマンド名 | 訳さない。英語の名詞はキーの語に合わせる |
| 3 | [docs/plans/continuo_design.md](../plans/continuo_design.md) | README に無い語はここを見る |
| 4 | この文書 | 上の3つに無い語だけを、ここで決める |

**新しい語を作る前に README.md を grep すること。**
**作った語は、この文書へ足してから使うこと。**足していない語を en.json に書かない。

---

## 2. 訳を固定する語 — 中心の概念

| 日本語 | 英語 | なぜそう決めたか |
| --- | --- | --- |
| continuo / herdr | continuo / herdr | 文頭でも小文字。README がそう書いている。`Continuo` と書かない |
| 着手 / 着手する | the start / start | README の "Started the wrong issue?" に合わせる。`kickoff` `initiation` は使わない |
| 引き渡し / dispatch する | dispatch | 設定のキー `agent.max_dispatch_turns` が既に英語。`hand-off` を作らない |
| 巡回 | poll | README の "It stops polling"。キーは `polling.interval_ms`。`patrol` `sweep` は使わない |
| カンバン | kanban board | **英語は2語で `kanban board` に統一する。**単独の `board` は使わない（GitHub 公式文書が "You can create a kanban board by setting your column field to a Status field" と書いている）。**日本語は「カンバン」。**「ボード」と書かない |
| Status（カンバンのフィールド） | Status | 大文字の S。GitHub のフィールド名そのもの |
| 状態（herdr の agent の） | status | 小文字。カンバンの `Status` と区別するため。`idle` / `done` / `blocked` の値を指す |
| エージェント | the agent | README の "the agent needs something from you"。`worker` `the AI` は使わない |
| 常駐 | daemon | README の `# start the daemon` |
| 選択肢（Status の） | option | GitHub 自身が single-select の値をこう呼ぶ |
| 役割（5つの） / 割り当てる | role / map | README の "you map your own option names to these five roles" |
| 対応表（`tracker.automated_state_rewrite`） | rewrite table | README の "the rewrite table's keys"。`mapping table` は使わない |
| 作業中の状態（`tracker.active_states`） | working state | README が "if it names a working state" と書く。**キー名が `active_states` でも `active state` と書かない** |
| 終了状態（`tracker.terminal_states`） | terminal state | キー名 `terminal_states` に合わせる。working state と対になる |

---

## 3. 訳を固定する語 — 置き場所とファイル

| 日本語 | 英語 | なぜそう決めたか |
| --- | --- | --- |
| 身元ファイル | identity file | キーが `workspace.identity_file`。`marker file` と書くとキーと繋がらない |
| worktree の置き場所 | worktree root | README の "the worktree root"。設定されたディレクトリなので root |
| hook の置き場所 | hook socket location | README が doctor の検査をこの語で並べている。doctor のラベルは `hook socket` に縮める |
| 置き場所（一般） | location / where X lives | README の "resolving where a clone lives"。`storage location` は使わない |
| 逃がし先 / `--pending-dir` | pending directory | 利用者が見るフラグが `--pending-dir`。**Go の識別子の `spill` を画面に出さない** |
| 設定ファイル（WORKFLOW.md） | config | README の "That single file is both the config and the brief" |
| 設定ファイル（issue ごとの Claude Code の） | settings file | WORKFLOW.md と区別する。doctor の検査名は "the Claude settings directory" |
| 雛形（WORKFLOW.md の） | template | README の "writes WORKFLOW.md"。**package 名の `scaffold` を画面に出さない** |
| プロンプトの本文 / 指示文 | the brief | README の "the brief you send to the agent" |
| front matter | front matter | 既に英語。2語・小文字のまま |

---

## 4. 訳を固定する語 — 片付けと失敗

| 日本語 | 英語 | なぜそう決めたか |
| --- | --- | --- |
| 片付け / 片付ける | cleanup / clean up | キーが `cleanup.on_states`。名詞は1語、動詞は2語 |
| 待避する（Status を） / `--park` | park | フラグが `--park`。README の "it parks the Status at `tracker.failure_state`" |
| 手を離す（issue から） | let go of the issue | README の "It makes continuo let go of the issue first" をそのまま使う |
| 残った branch | leftover branch | README の "A leftover branch is cleaned up even when the worktree is gone." |
| 身元を確かめられない worktree | broken worktree | README の "A broken worktree is still cleanable."。この種類を1つの名前で呼ぶ |
| 先頭の commit | tip commit | README の "the tip commit"。`HEAD commit` `latest commit` は使わない |
| 失うものがある | something to lose | README の "If there is anything to lose, it deletes nothing and stops." |
| 落ち着く（起動が） | settle | README の "herdr reports that the agent has settled" |
| 拾う（issue を） / 上から順に | pick up / in the order they sit on the kanban board | README の "continuo picks these up in the order they sit on the kanban board"。**`board order` は使わない**（単独の `board` を含む） |
| 信頼登録する / 信頼済み / 未承認 | trust / trusted / not trusted | README の "trust those repositories"。**「未承認」は `not trusted`。**`unapproved` は使わない |
| 資格情報 | credentials | README が doctor の検査をこの語で並べている |
| 枠 / 枠の判定 | usage window | README の "used to read your plan's usage window"。`quota` `rate limit budget` を混ぜない |
| 検査 / 前提が揃っている | check / everything is in place | README の "runs fifteen checks" と "# check that everything is in place" |
| 表明（`CONTINUO-STATUS:` の行） | the `CONTINUO-STATUS:` line | README が行そのものを名指ししている。抽象名詞に訳さない |
| 機械 | machine | README の "a machine or container you can discard" |
| 実行ファイル / 版 | binary / version | README の Install が "the binary" と書く |
| 既定 / 既定値 | default | README の "(two by default)" |
| 着手できずに止まっているもの | what cannot be started | ダッシュボードの表の見出し（issue #134）。「着手」は上の `start` に揃える。`stuck` `blocked` は使わない（`blocked` はカンバンの Status の値と衝突する） |
| 案内（issue へ書く1件のコメント） | notice | issue #140 で issue へ1回だけ書くコメントを指す。`notification` は使わない（herdr の通知と紛れる） |
| 印（ダッシュボードの行に添えるもの） | badge | 既に HTML の class 名が `badge` である（[internal/server/template.go](../../internal/server/template.go)）。`tag` `label` は使わない（`label` は GitHub のラベルと衝突する） |

---

## 5. 訳さないもの

**言いたいこと。**識別子は訳さない。訳すと、貼り付けて動く手順が動かなくなる。
**大文字小文字も変えない。**

| 何 | 例 |
| --- | --- |
| **既に英語の技術用語** | worktree / branch / commit / clone / pane / hook / socket / issue / run / turn / transcript / session / agent / workspace / project item / backoff / stall |
| **ファイル名・設定のキー・フラグ・コマンド** | `WORKFLOW.md` / `tracker.active_states` / `--dry-run` / `continuo trust` / `herdr agent read` / `CONTINUO-STATUS:` |
| **環境変数名・JSON のフィールド名** | `LANG` / `GITHUB_TOKEN` / `hasTrustDialogAccepted` |
| **大文字を保つ固有名** | `Claude Code` / `GitHub` / `Projects v2` / `Keychain` / `Status`（カンバンのフィールド） |

**プレースホルダだけは訳す。**利用者が自分で埋める場所だからである。

| 日本語 | 英語 |
| --- | --- |
| `<名前>` | `<name>` |
| `<番号>` | `<number>` |
| `<ホスト>` | `<host>` |

---

## 6. 文体の決めごと

**言いたいこと。**言い回しを揃えることが、読みやすさより優先である。
**同じ状況の文は、同じ形にする。**

| 決めごと | 中身 |
| --- | --- |
| **書式の verb** | **同じ verb を、同じ個数だけ置く。順番は英語の語順に合わせてよい。**ただし**並べ替えるなら、引数の番号を明示する**（下の節） |
| **空白と改行** | **そのまま写す。**行頭の2つの空白と `\n` は、doctor の字下げと setup の表示が頼っている |
| **`→ ` の接頭辞** | 対処の行の先頭に付く `→ ` を落とさない |
| **`**太字**` の印** | 日本語にある場所にだけ置く。**無いところに足さない** |
| **全角の記号** | ASCII に直す。`（）`→`()`、`／`→`/`、`、`→`,`、`。`→`.`、`「」`→ 引用符か backtick。**`✓` と `✗` は記号ではなく doctor の印なので残す** |
| **backtick** | 日本語にあるものだけを写す。増やしも減らしもしない。差分が読めなくなる |
| **`%d件`** | 英語では数詞だけでは通じない。`%d checks` / `%d repositories` のように、周りのキーから名詞を選んで添える |
| **「〜してください」** | 命令形にする。**`please` は書かない。**README に1つも無い |
| **行の長さ** | 1行の文言はおおむね100文字まで。日本語は密なので、直訳すると倍になる。**2文に割る** |
| **説明の増減** | 日本語にある節を落とさない。日本語に無い説明を足さない |

### 大文字・小文字と句点

| 種類 | 書き出し | 末尾 |
| --- | --- | --- |
| **Go の `error` になるもの**（`%w` を含むもの、キーが `err_` / `_failed` / `unparsable` / `not_found`） | 小文字（Go の慣習） | 日本語に合わせる |
| **画面に出る散文**（doctor の詳細・dashboard・`cli.*.usage`） | 大文字 | 日本語に `。` があれば `.` |
| **対処の1行**（`→ ` が付くもの・`init` の案内） | 大文字 | **`.` を付ける。**日本語に `。` が無くても付ける |
| **埋めた根拠の1行**（`init` が括弧の中に出すもの） | 大文字 | **付けない。**括弧の中に入るため |

### 指定子には引数の番号を書く

**言いたいこと。**日本語と英語で語順が変わると、**同じ `%d` でも別の値が入る。**
実際に `対象 5件のうち 2件が見つかりません` が `5 of 2 targets are missing` になっていた。
**`fmt` は `%[2]d` と番号を書ける。**書けば語順を自由にしてよい。

```json
"doctor.clone.detail_missing": "対象 %d件のうち %d件が見つかりません"
"doctor.clone.detail_missing": "%[2]d of %[1]d targets are missing"
```

| 決めごと | 中身 |
| --- | --- |
| **番号を必ず書く場合** | **同じ verb が2つ以上あるとき**（`%d` が2つ、`%s` が3つ、など）。機械では入れ替わりを見分けられないため |
| **番号の書き方** | `%` のすぐ後ろに `[n]` を置く（`%[2]d`）。`n` は**日本語の側で何番目に出てくるか**である |
| **`%w` も同じ** | `%[1]w` と書ける。**エラーの連鎖は切れない**（`errors.Is` が通ることを確かめてある） |
| **日本語の側** | **番号を書かない。**原文であり、引数の順番はそこで決まる |

**`test/internal/i18n` が機械で確かめる。**番号ごとに verb を突き合わせるので、
`%s` と `%d` を取り違えた訳も、番号の書き忘れも落ちる。

### 時制を潰さない

| 日本語 | 英語 |
| --- | --- |
| 〜できません（いまの能力） | cannot X |
| 〜できませんでした（この試行） | could not X |
| 〜に失敗しました | failed to X |

---

## 7. 語句を変えてはならない文

**言いたいこと。**下の文は、出てくる全部の場所で1文字も違えない。
**言い回しを変えると、同じ書式に見えなくなる。**

| 日本語 | 英語 | どこに出るか |
| --- | --- | --- |
| 何も消していません。 | `Nothing was deleted.` | `continuo abandon` の10箇所 |
| 【なぜ】 | `Why:` | 担当が移った run を止めるときの理由 |
| 【確かめ方】 | `How to check:` | doctor の10箇所 |
| 【対処】 | `What to do:` | doctor の9箇所 |
| 【よくある原因】 | `Common causes:` |
| 【注意】 | `Note:` | doctor の8箇所。**1件でも複数形のまま** |
| `--force` を付ければ消します。 | ``Add `--force` if you want it gone anyway.`` | `continuo abandon` の2箇所 |

---

## 8. doctor のラベルは15桁まで

**言いたいこと。**[internal/doctor/report.go](../../internal/doctor/report.go) の `labelColumn` が 16 である。
**16桁に満たない語を並べて桁を揃えている。**15桁を超えるラベルを1つ置くと、全部の行の桁が崩れる。

**使ってよいラベルは、この15語だけである。**

`config` / `cleanup states` / `missing keys` / `claude` / `hook socket` /
`Claude settings` / `worktree root` / `herdr` / `gh auth` / `board` /
`Status names` / `rewrite keys` / `clones` / `trust` / `credentials`

**伸ばすときは、先に `labelColumn` を数え直すこと。**

---

## 9. まだ訳していないところ

**言いたいこと。**画面に出るもののうち、次の2つはまだ日本語のままである。
**利用者向けの文書（[docs/FAQ.md](../FAQ.md) と [docs/upgrading.md](../upgrading.md)）にも、そう書いてある。**

| どこ | 中身 |
| --- | --- |
| `continuo trust` の出力 | [internal/trust](../../internal/trust) が `fmt.Sprintf` で組み立てている |
| ログ | i18n は画面に出す文言だけを対象にしている（[internal/i18n/i18n.go](../../internal/i18n/i18n.go) の説明） |

**残っている場所の全部と件数は、機械が数えている。**
[test/internal/testdesign/no_japanese_messages_test.go](../../test/internal/testdesign/no_japanese_messages_test.go)
の `japaneseAllowance` がそれで、**そこが正である。**
**この節と食い違ったら、そちらを見ること。**移し終えたら件数を下げ、0 になったら行ごと消す。

**訳すときも、この文書の語を使うこと。**
