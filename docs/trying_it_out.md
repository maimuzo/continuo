<!-- 目的: 人間が continuo を実際に動かして確かめるための手順 -->

# 試してみる / Trying it out

**English:** This walks through running `continuo` end to end: build it, prepare a throwaway
board, check the prerequisites with `continuo doctor`, put one issue into `Ready`, and watch
`continuo` open a worktree and drive Claude Code until the board says `Done`. Steps 1–5 are
verified; steps 6–8 spend real Claude Code quota, so read them before running.

**言いたいこと。**1件の issue が `Ready` から `Done` まで通るのを、実際に目で見るための手順である。
**段1〜5 は実際に叩いて確かめてある。段6 から先は実際に Claude Code が動き、枠を消費する。**

---

## 先に知っておくこと

| 何を | なぜ |
| --- | --- |
| **本番のボードで試さない** | continuo は Status を書き換える。**使い捨てのボードを作る**（段2） |
| **段7 から枠を消費する** | 実際に Claude Code が起動し、issue を実装しようとする |
| **止めるのは `Ctrl+C`** | 巡回を止め、hook の受け口を閉じ、turn の終わりを待ってから抜ける。**pane は閉じない**（次の起動で引き継ぐ） |

---

## 段1. ビルドする

```bash
cd <continuo のリポジトリ>
go build -o /tmp/continuo ./cmd/continuo
```

**Go 1.26 以上が要る**（`testing/synctest` を使っている）。

---

## 段2. 使い捨てのボードを作る

```bash
gh project create --owner <あなたのアカウント> --title "continuo-試用"
```

返ってきた URL の末尾の数字が **project の番号**である。段4 で使う。

### Status の選択肢を揃える

**GitHub が最初に作るのは `Todo` / `In Progress` / `Done` の3つだけである。**
continuo は次の5つを使うので、**足りないものを画面から追加する。**

| 選択肢 | 何に使うか |
| --- | --- |
| `Ready` | **ここに置いた issue を continuo が取る** |
| `In Progress` | 取ったときに continuo が書き込む |
| `In Review` | エージェントが `CONTINUO-STATUS: review` を出すと入る。**人間のレビュー待ち** |
| `Blocked` | エージェントが判断を仰ぐとき、または打ち切ったときに入る |
| `Done` | **人間がここへ動かすと、continuo が worktree と branch を片付ける** |

> **API で選択肢を足してはならない。**`updateProjectV2Field` は選択肢の指定を全件置き換えとして
> 扱うので、**設定済みの Status の値が全部消える。**必ず GitHub の画面から追加する。

---

## 段3. 対象リポジトリを用意する

```bash
ghq get <owner>/<repo>          # continuo は ghq で clone の場所を引く
cd $(ghq list -p -e <owner>/<repo>)
claude                          # 1度だけ起動して、フォルダの信頼を承認する
```

**信頼を承認していないと hook が1つも動かない。**turn の終わりを検知できないので、continuo は
その issue を飛ばす。

---

## 段4. 設定を置く

```bash
mkdir -p ~/continuo-try && cd ~/continuo-try
/tmp/continuo init
```

`WORKFLOW.md` ができるので、front matter の2箇所を埋める。

```yaml
tracker:
  provider:
    owner: <あなたのアカウント>        # __FILL_ME__ を置き換える
    project_number: <段2 の番号>       # 0 を置き換える
```

**`workspace.root` も見ておく。**worktree を置く場所である（既定は `~/worktrees`）。

---

## 段5. 前提が揃っているかを検査する

```bash
/tmp/continuo doctor
```

7項目を検査して、足りないものと直し方を出す。**`✗` が1つでもあれば終了コードは 1。**

```text
✓ 設定ファイル    ~/continuo-try/WORKFLOW.md を読めました（front matter の検証も通りました）
✓ herdr           protocol 19（設定と一致）／herdr 0.8.0／socket ~/.config/herdr/herdr.sock
✓ gh の認証       scope に project が含まれる（github.com の有効なアカウント）
✓ ボード          <あなた> の project #<番号> を読めました（Status の選択肢は設定と一致。…）
! clone           active_states の issue が0件なので、検査する対象がありません
! 信頼登録        active_states の issue が0件なので、検査する対象がありません
! 資格情報        ~/.claude/.credentials.json がありません（macOS では Keychain に入っています）

3件を確かめられませんでした（✗ 0件 / ! 3件）。足りないものはありません
```

**`!` は「確かめられなかった」であって、足りないという意味ではない。**
`資格情報` は macOS では常に `!` になる（**Keychain を読むと確認の画面が出て無人運用が止まる**ので読まない）。

### ここで詰まりやすいところ

| 症状 | 直し方 |
| --- | --- |
| `hook を受ける socket のパスが長すぎます（… バイト。上限は 103 バイト）` | **macOS の Unix domain socket は絶対パス103バイトまで。**`CONTINUO_RUNTIME_DIR=/tmp/continuo-run` のように短い場所を指定する |
| `実行時ディレクトリ … の権限が 755 です` | continuo は**自分が作っていないディレクトリの権限を書き換えない。**`chmod 700` してから起動する |
| `ボードの Status の選択肢名が設定と一致しません` | 段2 の選択肢を足し忘れている。**これを放置すると、巡回が無言で「対象0件」を返し続ける** |

---

## 段6. issue を1件用意する

対象リポジトリに issue を作り、段2 のボードに載せて **Status を `Ready` にする。**

**本文には「何をしてほしいか」を書く。**エージェントは起動後に
`gh issue view <URL> --comments` で自分で読む（プロンプトには本文を入れない）。

---

## 段7. 動かす

```bash
cd ~/continuo-try
/tmp/continuo
```

**ここから枠を消費する。**起きることは次のとおり。

| 順 | 何が起きるか | どこで見えるか |
| --- | --- | --- |
| 1 | 設定を読み、`flock` を取り、前提を検査する | 標準エラーのログ |
| 2 | 置き場所を走査して、引き継ぐ run を探す（初回は0件） | 同上 |
| 3 | `Ready` の issue を取り、**Status を `In Progress` へ書く** | **ボードで見える** |
| 4 | worktree を作り、herdr の pane で Claude Code を起動する | **herdr の画面で見える** |
| 5 | エージェントが作業し、`CONTINUO-STATUS:` の行を出す | herdr の pane |
| 6 | continuo がその行を読み、**Status を `In Review` へ動かす** | **ボードで見える** |
| 7 | 人間が確認して **`Done` へ動かす** | 人間の操作 |
| 8 | continuo が worktree と branch を片付ける | 置き場所から消える |

**巡回の間隔は既定30秒である。**すぐには動かない。

### 見ておくとよいもの

`WORKFLOW.md` の `server.port` に番号を書いておくと、**実行中の run の一覧を HTTP で見られる。**

```yaml
server:
  port: 8787
```

```bash
open http://127.0.0.1:8787/          # issue / Status / turn 数 / 最後に hook を受けた時刻 / トークン
curl -s http://127.0.0.1:8787/api/v1/state | jq .
```

**127.0.0.1 からしか繋がらない**（読むだけの窓であり、外へ晒すものではない）。

---

## 段8. 止める・片付ける

```
Ctrl+C
```

**巡回を止め、hook の受け口を閉じ、走行中の turn の終わりを待ってから抜ける。**
**pane は閉じない。**次に起動したとき、その pane を引き継ぐ。

### 試したあとの片付け

| 何を | どうするか |
| --- | --- |
| worktree と branch | **Status を `Done` にすれば continuo が片付ける。**残っていれば `git worktree remove` と `git branch -D` |
| 使い捨てのボード | GitHub の画面から消すか、`gh project delete` を使う（構文は `gh project delete --help` で確認すること。**番号を間違えると別のボードが消える**） |
| 実行時ディレクトリ | `CONTINUO_RUNTIME_DIR` に指定した場所を消す |

---

## うまく動かないとき

| 症状 | 見るところ |
| --- | --- |
| **issue を取ってくれない** | Status が `Ready` か。`doctor` の `ボード` が `✓` か。**選択肢名が1文字でも違うと0件になる** |
| **pane は立つが進まない** | 信頼の承認が済んでいるか（段3）。**信頼していないと hook が1つも動かない** |
| **`In Review` にならない** | エージェントが `CONTINUO-STATUS: review` を出しているか。herdr の pane で応答を見る |
| **片付かない** | **未コミットの変更が残っている**か、**push していない commit がある**と消さない（成果を失わないため）。ログに理由が出る |
| 枠を使い切った | continuo は待って再開する。`agent_status` を見て二重投入は避ける |

---

## いま無いもの

**設定の読み直しが実装されていない**（`SPEC.md` 6.2）。
`WORKFLOW.md` を書き換えても、**continuo を再起動するまで反映されない。**
詳しくは [plans/impl/tasks.md](plans/impl/tasks.md) の「未実装として残っているもの」を見ること。
