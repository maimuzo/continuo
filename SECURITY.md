# Security Policy / セキュリティについて

**English follows Japanese.**

---

## 報告先

**脆弱性を見つけたら、公開の issue には書かないでください。**

GitHub の **[Private vulnerability reporting](https://github.com/maimuzo/continuo/security/advisories/new)** から報告してください。作者だけが読めます。

**返答の目安は1週間です。**1人で片手間に開発しているため、それ以上かかることがあります。

## 対応している版

**release はまだありません。**いまは `main` の先頭だけが対象です。

release を出したあとは、この節に対応している版を書きます。

## この道具が持つ、生まれつきの危険

**continuo は、確認を求めないエージェントに、あなたの機械での作業を任せる道具です。**
以下は不具合ではなく、そういう設計です。**報告の前に、ここに当たるかを確かめてください。**

| 何 | どういうことか |
| --- | --- |
| **確認ダイアログが出ない** | Claude Code を `--permission-mode dontAsk` で起動し、`Bash` を引数の制限なしに許可します |
| **リポジトリを書き換えて push する** | エージェントは commit も push もします |
| **issue の本文が指示になる** | 既定の指示書は issue の本文とコメントを全部読ませます。**第三者が書いた文が、あなたの機械でコマンドとして実行されえます** |
| **信頼の登録を書き換える** | `continuo trust` は `~/.claude.json` を書き換え、対象リポジトリを Claude Code に信頼登録します |
| **`curl … \| sh` で配る** | インストーラーはネットワークから取ってきて実行されます |
| **資格情報を読む** | 定額プランの枠を読むために、`~/.claude/.credentials.json` か macOS の Keychain を読みます |

**これらを踏まえたうえで、想定を超える挙動があれば報告してください。**たとえば次のようなものです。

- **意図していないリポジトリ**が信頼登録される
- **ボードに載せていない** issue が処理される
- インストーラーが**取ってくる先を、警告なしに変えられる**
- 秘密が**ログや issue のコメントに漏れる**
- worktree の**外**へ書き込まれる

## 使う前に減らせる危険

| 抑え方 | どうするか |
| --- | --- |
| **自分が書いた issue だけを進める** | `Ready` へ動かすのは人間です。知らない issue を動かさないでください |
| **ラベルで絞る** | `tracker.required_labels` に印を入れ、それが付いた issue だけを対象にします |
| **信頼するリポジトリを減らす** | `continuo init` が並べた `trust.repositories` から、要らない行を消してください |
| **隔離して動かす** | 専用のアカウントか、捨ててよい機械・コンテナで動かしてください |

---

## Reporting

**Please do not open a public issue for a vulnerability.**

Use GitHub's **[private vulnerability reporting](https://github.com/maimuzo/continuo/security/advisories/new)**. Only the maintainer can read it.

**Expect a reply within a week.** This is a one-person project worked on in spare time, so it may take longer.

## Supported versions

**There are no releases yet.** Only the tip of `main` is in scope.

## Risks that are by design

**continuo hands your machine to an agent that does not ask for confirmation.** The following are not bugs — they are the design. **Please check this list before reporting.**

| What | What it means |
| --- | --- |
| **No permission prompts** | Claude Code is started with `--permission-mode dontAsk` and `Bash` allowed without argument restrictions |
| **It commits and pushes** | The agent writes to your repository and pushes |
| **Issue text is instructions** | The default brief has the agent read the issue body and every comment. **Text written by other people can execute on your machine** |
| **It edits your trust settings** | `continuo trust` rewrites `~/.claude.json` to trust the target repositories |
| **It is installed via `curl … \| sh`** | The installer is fetched from the network and executed |
| **It reads credentials** | To read your plan's usage window, it reads `~/.claude/.credentials.json` or the macOS Keychain |

**With that understood, please report anything beyond it** — for example:

- A repository you did not list gets trusted
- An issue that is not on the board gets picked up
- The installer's download source can be changed without a warning
- Secrets leak into logs or issue comments
- Something is written outside the worktree

## Reducing the risk before you start

| Mitigation | What to do |
| --- | --- |
| **Only advance issues you wrote** | A human moves things into `Ready`. Do not move an issue you did not read |
| **Filter by label** | Set `tracker.required_labels` so only issues carrying your marker are eligible |
| **Trust fewer repositories** | Delete the lines you do not need from the `trust.repositories` list that `continuo init` writes |
| **Isolate it** | Run it under a dedicated account, or on a machine or container you can discard |
