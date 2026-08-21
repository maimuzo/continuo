# continuo

[English](README.md) | 日本語

**GitHub Projects v2 のボードを見張り、issue ごとに git worktree を用意して、Claude Code を対話モードで起動し、完了まで面倒を見る常駐プロセスです。**

名前は通奏低音（basso continuo）に由来します。曲の最初から最後まで途切れず鳴り続け、全体の和声を支える低音パート — [名前の由来](docs/naming.md)。

> **状態: 実装済み。ただし issue を1件通すところまでは未実行です。**
> 全段がビルドとテストを通り、セットアップのコマンドは実在のボードに対して（読み取りのみで）叩いてあります。

---

## 何をするか

| | |
| --- | --- |
| **見張る** | GitHub Projects v2 のボード1枚。**複数のリポジトリの issue が1枚に載っていてよい** |
| **用意する** | issue ごとに git worktree を、その issue のリポジトリに作る |
| **動かす** | **herdr の pane で Claude Code を対話モードで起動する**（`claude -p` は使いません） |
| **続ける** | issue が作業中の状態にある間、同じセッションへ指示を送り続ける |
| **終わりを知る** | **Claude Code の `Stop` hook で turn の終わりを検知する** |
| **信じる** | **ボードの Status だけ。**エージェントの自己申告は信じない |
| **片付ける** | issue が完了状態になったら worktree と branch を消す |

## こんなときに使う

- **溜まった issue を、寝ている間に1件ずつ片付けたい**
- **1件ずつ手で Claude Code を起動して、終わったか見に行くのをやめたい**
- **複数のリポジトリの作業を1枚のボードで管理したい**
- **どこまで進んだかを、ボードの Status で把握したい**

**向いていないもの。** 1回の指示で終わる小さな作業（手で叩いたほうが速い）。人間の判断が何度も要る設計作業。

## 前提

| | |
| --- | --- |
| OS | **macOS / Linux。**Windows ネイティブは対応しません（WSL2 を使ってください） |
| [Go](https://go.dev/dl/) | 1.26 以上（ビルドに必要。`testing/synctest` を使っています） |
| [herdr](https://github.com/herdrdev/herdr) | 0.8.0 以上。**pane と worktree をまとめる常駐プロセス** |
| [Claude Code](https://claude.com/claude-code) | 2.1.233 以上 |
| [`gh`](https://cli.github.com/) | 2.97.0 以上。`gh auth login -s project` でログイン済みであること |
| [`git`](https://git-scm.com/) / [`ghq`](https://github.com/x-motemen/ghq) | continuo が worktree の作成と clone の解決に使います |

**揃っているかは手で確かめないでください。** `continuo doctor` が全部検査して、足りないものと直し方を出します。

## 入れる

```bash
git clone https://github.com/maimuzo/continuo.git
cd continuo
go build -o /usr/local/bin/continuo ./cmd/continuo
```

[mise](https://mise.jdx.dev/) で Go を入れている場合は、先に1回だけ `mise trust` を実行してください。

## 使う

**ボードは新しく作りません。** いま使っているボードにそのまま足して使います。

```bash
mkdir -p ~/continuo-work && cd ~/continuo-work

continuo init                     # WORKFLOW.md を置く（owner とボード番号は gh から引く）
continuo setup                    # ボードの Status を continuo の5つの役割へ割り当てる
continuo trust                    # 対象リポジトリを Claude Code に信頼登録する（clone も取ってくる）
continuo allow-keychain-access    # macOS だけ。枠の残りを読むために1回だけ実行する
continuo doctor                   # 前提が揃っているか検査する

continuo                          # 常駐を始める。Ctrl+C で止める
```

**あとはボードで issue を着手待ちの Status に置くだけです。** continuo が拾って worktree を作り、Claude Code を起動します。

**止めるのは `Ctrl+C`。** 巡回を止め、走行中の turn の終わりを待ってから抜けます。**pane は閉じません**（次の起動で引き継ぎます）。

### 設定

`WORKFLOW.md` の front matter に書きます。`continuo init` が雛形を置くので、必要な行だけ直してください。

```yaml
tracker:
  provider:
    owner: octocat                # あなたの GitHub アカウント名
    project_number: 3             # ボードの番号
  dispatch_state: "Ready"         # ここに置いた issue を continuo が取る
  running_state: "In Progress"    # 取ったときに書き込む Status
  terminal_states: ["Done"]       # ここへ移すと worktree と branch を片付ける
agent:
  max_concurrent_agents: 3        # 同時に動かす issue の数
```

## もっと詳しく

| 何 | どこ |
| --- | --- |
| **1件の issue を最初から最後まで通す手順** | [docs/trying_it_out.md](docs/trying_it_out.md) |
| 設計と、その判断の根拠 | [docs/plans/continuo_design.md](docs/plans/continuo_design.md) |
| ユースケース記述（RUCM） | [docs/spec/usecases/](docs/spec/usecases/) |
| 名前の由来 | [docs/naming.md](docs/naming.md) |

## 準拠する仕様

continuo は [openai/symphony](https://github.com/openai/symphony) のサービス仕様（Apache-2.0）を実装しています。仕様の写しは [docs/spec/symphony/SPEC.md](docs/spec/symphony/SPEC.md) にあります。

**仕様は Codex の app-server と stdio で構造化メッセージをやり取りする前提ですが、continuo は herdr の pane で動く対話モードの Claude Code に読み替えています。** 守れない `MUST` と、その代わりに何をするかは設計文書に書いてあります。

## ライセンス

**MIT** — [LICENSE](LICENSE) を見てください。

**例外が1つあります。** [docs/spec/symphony/SPEC.md](docs/spec/symphony/SPEC.md) は [openai/symphony](https://github.com/openai/symphony) の仕様の再配布であり、**Apache-2.0** です（[LICENSE-APACHE-2.0.txt](LICENSE-APACHE-2.0.txt) / [NOTICE](NOTICE)）。**それ以外のファイルはすべて MIT です。**
