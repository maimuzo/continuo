# continuo

**[English](README.md)**

GitHub Project v2をカンバンとして使い、複数リポジトリ上のissueをカンバン上のReady stateに配置することで、continuoがそれをherdr上で実行し、処理結果をカンバンにフィードバックするオーケストレーターです。OpenAIのSymphonyをベースにclaudeとgo言語とherdrを使って処理内容を確認しながら実行することができます。

---

## カンバンの上での挙動

**タスクをissueにまとめ `Ready` に置くだけです。**あとは continuo が処理します。Blockedに移ったらAIが困っているのでissueコメントで指示出ししてください。In Reviewに移ったら処理を終えているので内容を確認し、OKならDoneに移してください

![continuo が動かすカンバン](docs/images/board.png)

| Status | 誰が動かすか | そのとき何が起きているか |
| --- | --- | --- |
| `Ready` | **あなた** | continuo が上から順に herdr 上で処理します |
| `In Progress` | continuo | continuoが処理を開始したらこのstateに自動的に移ります。今処理中のissueです。featureブランチを作りgit worktree上で作業を開始します |
| `In Review` | continuo | AIがうまく処理できたら自動的に移ります。人間確認待ちなのでissueを開いて内容を確認してください。**OKなら `Done` へ動かすのはあなた** |
| `Blocked` | continuo | うまく処理できなかった場合、もしくは人間からの質問の回答待ちになると自動的に移ります。issueコメントに対応方法を書いて `Ready` に移してください |
| `Done` | **あなた** | issueが完了した状態です。ここに移るとcontinuo が worktree と branch を消します |

**Status の名前はボードに合わせられます。**`Ready` でも `Todo` でも、`continuo setup` で対応づけます。

## 何をしているか、見える

**Claude Code は herdr の pane で、対話モードのまま動きます。**バックグラウンドで、見えないまま動くのではありません。herdr の画面を開けば動いている様子が見えますし、別の端末から読むこともできます。

```bash
herdr agent read continuo-hello-world-188 --source recent-unwrapped --lines 40
```

**従量課金の API は使いません。**`claude -p` も Agent SDK も **API の直叩き**も使わず、定額プランのまま、対話セッションを維持して続きの指示を送ります。**同じセッションの中で文脈が積み上がります。**

**`continuo --port 8080` オプションで起動すると、進行中の issue の一覧を `http://localhost:8080` で見られます。**

## 複数のリポジトリを1枚で回せる

**カンバン1枚に、複数のリポジトリの issue を載せられます。**continuo は issue ごとに、そのリポジトリの worktree を作ります。

```
~/worktrees/github.com/octocat/hello-world/continuo-octocat-hello-world-188/
~/worktrees/github.com/octocat/sample-app/continuo-octocat-sample-app-42/
```

**同時に動かす issue の数は設定で決めます**（既定2件）。

## 始める前に知っておくこと

**エージェントは、あなたのリポジトリを実際に編集し、commit して push します。**continuo は Claude Code を「人間に確認を出さないモード」で起動し、`Bash` を引数の制限なしに許可します。**確認のダイアログは出ません。**

**最初は使い捨てにできるリポジトリで試してください。**本業のリポジトリをいきなり指定しないこと。

## 前提

| | |
| --- | --- |
| OS | macOS / Linux。**Windows ネイティブは非対応**（WSL2 を使う） |
| [herdr](https://github.com/herdrdev/herdr) | **pane と worktree を束ねる常駐プロセス。**continuo はこれ越しに Claude Code を動かす。**0.8.0 で動作を確認**（socket の protocol が一致しないと起動を止める） |
| [Claude Code](https://claude.com/claude-code) | **定額プランで使う。**2.1.233 で動作を確認 |
| [`gh`](https://cli.github.com/) | `gh auth login -s project` でログイン済みであること。2.97.0 で動作を確認 |
| [`git`](https://git-scm.com/) / [`ghq`](https://github.com/x-motemen/ghq) | worktree の作成と、clone の場所の解決に使う |
| [Go](https://go.dev/dl/) 1.26+ | ビルドにだけ必要 |

**ボードには Status の選択肢が5つ要ります。**GitHub の既定は `Todo` / `In Progress` / `Done` の3つなので、**足りない2つは GitHub の画面から足してください** — ボードの `Settings` を開き、左の `Custom fields` の `Status` を選び、`Options` の下の `Add option...` に名前を入れて `Add`。名前は何でも構いません。役割との対応は `continuo setup` で決めます。

**`continuo doctor` が8つの検査を通します** — 設定 / Claude Code / herdr / gh 認証 / ボード / clone / 信頼 / 資格情報（定額プランの枠を読むためのもの）。**OS と Go の版は調べないので、そこは自分で確認してください。**

## 入れる

```bash
curl -fsSL https://raw.githubusercontent.com/maimuzo/continuo/main/install.sh | sh
```

**やること。**OS と命令セットを見分け、[release](https://github.com/maimuzo/continuo/releases) から実行ファイルを取り、`~/.local/bin/continuo` へ置きます。**足りない道具があれば1つずつ尋ねます**（`git` / `gh` / `ghq`）。**`herdr` と `claude` は入れません** — どちらも独自の配布経路と認証があるため、案内するだけです。

| オプション | 何をするか |
| --- | --- |
| `--yes` | すべての確認に「はい」と答える |
| `--no-deps` | 道具を1つも入れず、足りないものを並べるだけ |
| `--dir DIR` | 置き先を変える（既定 `~/.local/bin`） |

**ソースから作ることもできます。**Go 1.26 が要ります。

```bash
git clone https://github.com/maimuzo/continuo.git
cd continuo
go build -o ~/.local/bin/continuo ./cmd/continuo
```

## 使う

**ボードは新しく作りません。**いま使っているカンバンにそのまま足します。

```bash
mkdir -p ~/continuo-work && cd ~/continuo-work

continuo init      # WORKFLOW.md を置く。owner とボード番号は gh から引く
```

**ここで一度 `WORKFLOW.md` を開いてください。**`trust.repositories` に、ボードに載っていたリポジトリが全部並んでいます。**要らない行を消さないと、無関係なリポジトリまで Claude Code に信頼登録されます。**

```bash
continuo setup                    # ボードの Status を continuo の5つの役割に対応づける（対話）
continuo trust --dry-run          # 何を信頼登録するかを、実行せずに表示する
continuo trust                    # 対象リポジトリを信頼登録する。clone が無ければ取ってくる
continuo allow-keychain-access    # macOS だけ。定額プランの枠を読むために1回
continuo doctor                   # 前提が揃っているか調べる

continuo                          # 常駐を始める
```

**あとは issue を `Ready` に置くだけです。**

**止めるときは `Ctrl+C`。**巡回を止め、turn ループを畳んで抜けます。**pane は閉じません。**Claude Code はそのまま動き続けるので、次に起動したとき、その pane を引き継いで続きから進めます。

### 設定

`continuo init` が `WORKFLOW.md` を置きます。**この1枚が設定ファイルであり、エージェントへ送る指示書でもあります。**

**上半分（front matter）が設定です。**よく触るのは次の4つ。

```yaml
tracker:
  provider:
    owner: octocat                # GitHub のアカウント名
    project_number: 3             # カンバンの番号
agent:
  max_concurrent_agents: 2        # 同時に動かす issue の数
claude:
  turn_timeout_ms: 3600000        # 画面が変わらないまま何ミリ秒たったら打ち切るか
```

**`turn_timeout_ms` は turn の総時間ではありません。**herdr が見ている画面の版が変わり続けている限り、1つの指示に何時間かかっても打ち切りません。

**下半分が、Claude Code へ送る1回目のプロンプトそのものです。**「終わったら `CONTINUO-STATUS: review` と書け」「その前に commit して push しろ」といった依頼が書いてあります。**プロジェクトの流儀に合わせて書き換えてください。**

**書き換えたら continuo を再起動してください。**動いている最中は読み直しません。

## 動く仕組み

**turn の終わりは、herdr が pane を見て判定します。**continuo は herdr の socket へ「指示を送って待て」と頼み、agent が `idle` / `done` / `blocked` になるまで返りません。**herdr のコマンドを起動するのではなく、socket を直接開いて JSON をやり取りします。**

1. continuo が issue の処理を開始し、Status を `In Progress` にする
2. worktree を作り、herdr の pane で Claude Code を起動して、指示を送る
3. **herdr が「agent は止まった」と返す**
4. **Claude Code の `Stop` hook で裏を取る。**`background_tasks` が空になり、そのあと数秒たっても新しい仕事が始まらなければ、turn が終わったと確定する
5. transcript を読み、`CONTINUO-STATUS:` の行があれば Status を動かす
6. issue がまだ作業中の状態なら、続きの指示を送る（`agent.max_dispatch_turns` 回まで。既定20回）

**信じるのはカンバンの Status だけです。**エージェントが「終わった」と言っても、Status が動いていなければ終わっていません。

## いまの状態

**実装とテストは通っていますが、実機で issue を1件通しきった実績はまだありません。**手順は [docs/trying_it_out.md](docs/trying_it_out.md) にあります。

## もっと詳しく

| | |
| --- | --- |
| 設計と判断の根拠 | [docs/plans/continuo_design.md](docs/plans/continuo_design.md) |
| ユースケース記述（RUCM） | [docs/spec/usecases/](docs/spec/usecases/) |
| 名前の由来 | [docs/naming.md](docs/naming.md) |

## 準拠する仕様

**symphony は、OpenAI が公開しているコーディングエージェントのオーケストレーターの仕様です**（Apache-2.0）。continuo はこれを実装しています。仕様の本文は [SPEC.md](https://github.com/openai/symphony/blob/main/SPEC.md) にあります（**このリポジトリには同梱していません**）。

**仕様は Codex の app-server と stdio でやり取りする前提です。**continuo はそこを、herdr の pane で動く Claude Code に読み替えました。守れない `MUST` と、代わりに何をするかは [docs/plans/continuo_design.md](docs/plans/continuo_design.md) の「8. symphony の仕様と異なるところ」にあります。

## ライセンス

**MIT** — [LICENSE](LICENSE)
