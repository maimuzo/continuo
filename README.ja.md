# continuo

GitHub Project v2 をカンバンとして使い、複数リポジトリ上の issue をカンバン上の Ready state に配置することで、continuo がそれを herdr 上で実行し、処理結果をカンバンにフィードバックするオーケストレーターです。OpenAI の Symphony をベースに claude と go 言語と herdr を使って処理内容を確認しながら実行することができます。

---

## カンバンの上で何が起きるか

**issue を `Ready` に置くだけです。**あとは continuo と Claude Code が Status を動かします。

| Status | 誰が動かすか | そのとき何が起きているか |
| --- | --- | --- |
| `Ready` | **あなた** | continuo が30秒ごとに見ている。見つけたら取る |
| `In Progress` | continuo | worktree を作り、herdr の pane で Claude Code を起動した |
| `In Review` | continuo | エージェントが `CONTINUO-STATUS: review` を出した。**作業は終わっている** |
| `Blocked` | continuo | エージェントが判断を仰いだ、または失敗した。**issue のコメントに理由が書かれる** |
| `Done` | **あなた** | 成果を確認して移す。continuo が worktree と branch を消す |

**Status の名前はボードに合わせられます。**`Ready` でも `Todo` でも、`continuo setup` で対応づけます。

## 処理内容を目で見られる

**Claude Code は herdr の pane で対話モードのまま動きます。**バックグラウンドで見えないところで動くのではありません。

- **herdr の画面を開けば、いま何をしているかがそのまま見えます**
- 途中で人間が同じ pane に割り込んで指示を足せます
- **`claude -p` は使いません。**対話セッションを維持したまま、続きの指示を送ります

**issue が `Blocked` に落ちたときは、コメントに次の3つが書かれます。**

```
【確かめ方】そのままコピーして叩けるコマンド
【よくある原因】/ で区切った候補
【対処】直し方。設定キーなら現在値も出る
```

## 複数リポジトリを1枚で回せる

**カンバン1枚に、複数のリポジトリの issue を載せられます。**continuo は issue ごとに、そのリポジトリの worktree を作ります。

```
~/worktrees/github.com/octocat/hello-world/continuo-octocat-hello-world-188/
~/worktrees/github.com/octocat/sample-app/continuo-octocat-sample-app-42/
```

**同時に動かす数は設定で決めます**（既定2件）。

## 前提

| | |
| --- | --- |
| OS | macOS / Linux。**Windows ネイティブは非対応**（WSL2 を使う） |
| [herdr](https://github.com/herdrdev/herdr) 0.8.0+ | **pane と worktree を束ねる常駐プロセス。**continuo はこれ越しに Claude Code を動かす |
| [Claude Code](https://claude.com/claude-code) 2.1.233+ | **定額プランで使う。**従量課金の API は使わない |
| [`gh`](https://cli.github.com/) 2.97.0+ | `gh auth login -s project` でログイン済みであること |
| [`git`](https://git-scm.com/) / [`ghq`](https://github.com/x-motemen/ghq) | worktree の作成と clone の場所の解決に使う |
| [Go](https://go.dev/dl/) 1.26+ | ビルドにだけ必要 |

**揃っているかは `continuo doctor` が全部調べます。**足りないものと直し方を出すので、手で確かめる必要はありません。

## 入れる

```bash
git clone https://github.com/maimuzo/continuo.git
cd continuo
go build -o /usr/local/bin/continuo ./cmd/continuo
```

## 使う

**ボードは新しく作りません。**いま使っているカンバンにそのまま足します。

```bash
mkdir -p ~/continuo-work && cd ~/continuo-work

continuo init                     # WORKFLOW.md を置く。owner とボード番号は gh から引く
continuo setup                    # カンバンの Status を continuo の5つの役割に対応づける（対話）
continuo trust                    # 対象リポジトリを Claude Code に信頼登録する。clone も取ってくる
continuo allow-keychain-access    # macOS だけ。枠の残りを読むために1回
continuo doctor                   # 前提が揃っているか調べる

continuo                          # 常駐を始める
```

**あとは issue を `Ready` に置くだけです。**

**止めるときは `Ctrl+C`。**巡回を止め、走行中の turn が終わるのを待ってから抜けます。**pane は閉じません。**次に起動したとき、その pane を引き継いで続きから進めます。

### 設定

`continuo init` が `WORKFLOW.md` を置きます。よく触るのは次の4つです。

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

## 動く仕組み

**turn の終わりは Claude Code の `Stop` hook で検知します。**画面の文字を読んで判定するのではありません。

1. continuo が issue を取り、worktree を作る
2. herdr の pane で Claude Code を起動し、指示を送る
3. **`Stop` hook が continuo へ届く。**`background_tasks` が空なら turn が終わっている
4. transcript を読み、`CONTINUO-STATUS:` の行があれば Status を動かす
5. issue がまだ作業中の状態なら、続きの指示を送る（上限あり）

**信じるのはカンバンの Status だけです。**エージェントが「終わった」と言っても、Status が動いていなければ終わっていません。

## もっと詳しく

| | |
| --- | --- |
| **issue を1件通す手順**（段1〜9） | [docs/trying_it_out.md](docs/trying_it_out.md) |
| 設計と判断の根拠 | [docs/plans/continuo_design.md](docs/plans/continuo_design.md) |
| ユースケース記述（RUCM） | [docs/spec/usecases/](docs/spec/usecases/) |
| 名前の由来 | [docs/naming.md](docs/naming.md) |

## 準拠する仕様

[openai/symphony](https://github.com/openai/symphony) のサービス仕様（Apache-2.0）を実装しています。仕様の本文は [SPEC.md](https://github.com/openai/symphony/blob/main/SPEC.md) にあります（**このリポジトリには同梱していません**）。

**仕様は Codex の app-server と stdio でやり取りする前提です。**continuo はそこを herdr の pane で動く Claude Code に読み替えました。守れない `MUST` と代わりに何をするかは、設計文書の適合の表にあります。

## ライセンス

**MIT** — [LICENSE](LICENSE)
