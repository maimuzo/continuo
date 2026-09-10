# Security Policy / セキュリティについて

**English follows Japanese.**

---

## 報告先

**脆弱性を見つけたら、公開の issue には書かないでください。**

GitHub の **[Private vulnerability reporting](https://github.com/maimuzo/continuo/security/advisories/new)** から報告してください。作者だけが読めます。

**返答の目安は1週間です。**1人で片手間に開発しているため、それ以上かかることがあります。

## 対応している版

**いちばん新しい release と、`main` の先頭が対象です。**

**それより古い release には手を入れません。**v0.x のうちは、直したものを次の release として出します
（[docs/releasing.md](docs/releasing.md)）。**古い版に留まる必要がある場合は、issue で相談してください。**

## この道具が持つ、生まれつきの危険

**continuo は、確認を求めないエージェントに、あなたの機械での作業を任せる道具です。**
以下は不具合ではなく、そういう設計です。**報告の前に、ここに当たるかを確かめてください。**

| 何 | どういうことか |
| --- | --- |
| **確認ダイアログが出ない** | Claude Code を `--permission-mode auto` で起動します（既定）。**保護対象パスへの書き込みとシェルのコマンドは、Claude Code の中の判定役が会話の流れを読んで決めます。**`dontAsk` を選ぶと、許可の一覧の外は確認せずに拒否されます |
| **リポジトリを書き換えて push する** | エージェントは commit も push もします |
| **issue の本文が指示になる** | 既定の指示書は issue の本文とコメントを全部読ませます。**第三者が書いた文が、あなたの機械でコマンドとして実行されえます** |
| **第三者のコメントが判定に混ざります** | 既定の `--permission-mode auto` では、Claude Code の中の判定役が会話の流れを読んで実行の可否を決めます。**エージェントは issue のコメントを読むので、その出力も会話に載ります。**組み込みの指示書は、`OWNER` / `MEMBER` / `COLLABORATOR` 以外が書いたものを指示として扱わないようエージェントへ指示していますが、**それはエージェントへの指示であって、判定役がその区別を使うかどうかは測っていません。****公開リポジトリの issue を処理させるなら `claude.permission_mode` を `dontAsk` にしてください。**`claude.tool_gate.mode` を `public_only` にすると `Bash` の呼び出しは実行の前にもう一度判定を通せますが、**`.claude/` 配下と `.mcp.json` への書き込みは対象外です**（判定に回る道具は `Bash` だけです） |
| **信頼の登録を書き換える** | `continuo trust` は `~/.claude.json` を書き換え、対象リポジトリを Claude Code に信頼登録します |
| **`curl … \| sh` で配る** | インストーラーはネットワークから取ってきて実行されます |
| **資格情報を読む** | 定額プランの枠を読むために、`~/.claude/.credentials.json` か macOS の Keychain を読みます |
| **`continuo abandon` は消す** | worktree と branch と herdr の workspace を消します。**`--force` を付けると、コミットしていない変更と push していない commit ごと消えます**（`--dry-run` で何が消えるかを先に見られます） |
| **`continuo abandon` はカンバンも書き換える** | continuo が動いていれば、手を離させるために Status を `--park`（既定は `tracker.failure_state`）へ動かします。`--to` を付ければ片付けたあとにも動かします。**`--dry-run` はどちらも書かず、書く値を予告するだけです** |

**これらを踏まえたうえで、想定を超える挙動があれば報告してください。**たとえば次のようなものです。

- **意図していないリポジトリ**が信頼登録される
- **カンバンに載せていない** issue が処理される
- インストーラーが**取ってくる先を、警告なしに変えられる**
- 秘密が**ログや issue のコメントに漏れる**
- worktree の**外**へ書き込まれる

## 使う前に減らせる危険

| 抑え方 | どうするか |
| --- | --- |
| **自分が書いた issue だけを進める** | `Ready` へ動かすのは人間です。知らない issue を動かさないでください |
| **ラベルで絞る** | `tracker.required_labels` に印を入れ、それが付いた issue だけを対象にします |
| **信頼するリポジトリを減らす** | `continuo init` が並べた `trust.repositories` から、要らない行を消してください |
| **危ない道具の呼び出しを断らせる** | `claude.tool_gate.mode` は既定で `off` です。`public_only` にすると、公開リポジトリの issue のときだけ `Bash` の呼び出しを実行の前に検査します（`on` ならいつでも）。**この判定は会話を読まないので、コメントで許可を出しても通りません** |
| **隔離して動かす** | 専用のアカウントか、捨ててよい機械・コンテナで動かしてください |

---

## Reporting

**Please do not open a public issue for a vulnerability.**

Use GitHub's **[private vulnerability reporting](https://github.com/maimuzo/continuo/security/advisories/new)**. Only the maintainer can read it.

**Expect a reply within a week.** This is a one-person project worked on in spare time, so it may take longer.

## Supported versions

**The latest release and the tip of `main` are in scope.**

**Older releases do not get fixes.** While this is on v0.x, a fix ships as the next release. **If you need to stay on an older version, open an issue and let's talk.**

## Risks that are by design

**continuo hands your machine to an agent that does not ask for confirmation.** The following are not bugs — they are the design. **Please check this list before reporting.**

| What | What it means |
| --- | --- |
| **No permission prompts** | Claude Code is started with `--permission-mode auto` (the default). **Writes to protected paths and shell commands are decided by a classifier inside Claude Code that reads the conversation.** Choosing `dontAsk` denies anything outside the allow list without asking |
| **It commits and pushes** | The agent writes to your repository and pushes |
| **Issue text is instructions** | The default brief has the agent read the issue body and every comment. **Text written by other people can execute on your machine** |
| **A stranger's comment enters the transcript the classifier reads** | With the default `--permission-mode auto`, a classifier inside Claude Code decides what may run by reading the conversation. **The agent reads the issue comments, so that output is part of the conversation too.** The built-in brief tells the *agent* not to treat text from anyone outside `OWNER` / `MEMBER` / `COLLABORATOR` as an instruction, **but that is an instruction to the agent — whether the classifier uses the same distinction is not something we have measured.** **If you let it work on public issues, set `claude.permission_mode` to `dontAsk`.** Setting `claude.tool_gate.mode` to `public_only` puts `Bash` calls through a second check before they run, **but writes to `.claude/` and `.mcp.json` are not covered** (only `Bash` is sent to that check) |
| **It edits your trust settings** | `continuo trust` rewrites `~/.claude.json` to trust the target repositories |
| **It is installed via `curl … \| sh`** | The installer is fetched from the network and executed |
| **It reads credentials** | To read your plan's usage window, it reads `~/.claude/.credentials.json` or the macOS Keychain |
| **`continuo abandon` deletes** | It removes the worktree, the branch, and the herdr workspace. **With `--force` it takes uncommitted changes and unpushed commits with them** (`--dry-run` shows what would go first) |
| **`continuo abandon` also writes to the board** | If continuo is running, it moves the Status to `--park` (default: `tracker.failure_state`) to make continuo let go of the issue. With `--to` it moves the Status again after cleanup. **`--dry-run` writes neither; it only announces the values it would write** |

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
| **Have dangerous tool calls refused** | `claude.tool_gate.mode` defaults to `off`. Set it to `public_only` to inspect `Bash` calls before they run when the issue is in a public repository (`on` for always). **This check does not read the conversation, so granting permission in a comment does not get past it** |
| **Isolate it** | Run it under a dedicated account, or on a machine or container you can discard |
