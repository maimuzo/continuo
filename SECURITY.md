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
| **確認ダイアログを出さずに進む** | Claude Code を `--permission-mode auto` で起動します（既定）。**保護対象パスへの書き込みとシェルのコマンドは、Claude Code の中の判定役が実行の前に確かめます。**`dontAsk` を選ぶと、許可の一覧の外は確認せずに拒否されます。**判定役が遮断を続けたときに確認へ戻るかは検証していません** |
| **リポジトリを書き換えて push する** | エージェントは commit も push もします |
| **issue の本文が指示になる** | 既定の指示書は issue の本文とコメントを全部読ませます。**第三者が書いた文が、あなたの機械でコマンドとして実行されえます** |
| **第三者のコメントが、実行の可否の判定に混ざりえます** | 既定の `--permission-mode auto` では、Claude Code の中の判定役が実行の可否を決めます。**判定役への要求から、道具の結果は取り除かれます**（公式文書の permission modes のページ。2026-09-18 に取得）。**エージェントが `gh` で読んだ issue のコメントは道具の結果として届くので、判定役には渡りません。**許可を出す向きは、2026-09-18 の実測でも届きませんでした。**例外は、continuo が最初のメッセージに付けて渡す信頼できる立場のコメントだけで（次の行）、外部の人のコメントは付けません。****注入の向きは測っていません。**組み込みの指示書は、`OWNER` / `MEMBER` / `COLLABORATOR` 以外が書いたものを指示として扱わないよう**エージェントへ**指示していますが、**判定役がその区別を使うかどうかは測っていません。** 減らし方は2つです。**`claude.permission_mode` を `dontAsk` にする**（そのぶん `.claude/` 配下と `.mcp.json` へ書けなくなります。[docs/upgrading.md](docs/upgrading.md)）。または **`claude.tool_gate.mode` を `public_only` にする**（既定では `Bash` だけが判定に回ります。`.claude/` への書き込みも見せたいなら `claude.tool_gate.tools` に `Write` と `Edit` を足してください） |
| **信頼できる立場のコメントが、人間の許可として判定役に届きます** | continuo は、Claude Code を閉じるたびに issue へ `<!-- continuo:closed -->` で始まる「閉じた記録」を書きます。次に Claude Code を起動するとき、**その記録より後に `OWNER` / `MEMBER` / `COLLABORATOR` が AI の印を付けずに書いたコメントを、最初のメッセージに付けて渡します**（既定の `auto` で、`agent.relay_trusted_comments` が `true` のとき）。判定役はそれを人間の許可として読みます。**次のものも、同じく人間の許可として届きえます。**(1) `MEMBER` は、リポジトリを持つ組織のメンバー全員に付きます（リポジトリへの権限を問いません）。`COLLABORATOR` に読み取りだけの協力者が入るかは測っていません。(2) 同じ GitHub アカウントで動く AI（別の issue の Claude Code や、あなたが自分で起動した Claude Code）が、印を付け忘れて書いたコメント。(3) 信頼できる人が外部の人の文を引用したコメントの、引用の部分（引用だけを落とすと「ただし」の部分まで落ちうるので、落としません）。**減らし方は `agent.relay_trusted_comments: false` です**（コメントで出した許可は届かなくなります） |
| **同じアカウントの Claude Code が、閉じた記録を作る・消す・書き換える** | 閉じた記録は1行目の印で見分けるだけで、認証ではありません。**同じアカウントで動く Claude Code が閉じた記録を偽って書く・本物を消すと、どこから先のコメントを渡すかの境目が動きます。**本物を消すと境目が前へ戻り、前の回の途中の書き込みが人間の許可として渡ります。境目より後に作られた人間のコメントを書き換えると、書き換えた本文が渡ります。**閉じた記録は消さずに隠してください**（隠しても境目は動きません）。見分けるにはアカウントを分けるか手元に控えを置くしかなく、continuo はどちらも採っていません |
| **エージェントが、ほかの run と共有する手順を書き換えうる** | continuo は、エージェントへ渡す手順（plugin `continuo-agent-procedures` のスキルとスクリプト）を、実行時ディレクトリの `plugins/<12桁>/` に書き出し、同じ版で走るすべての run で共有します（12桁は plugin の中身のハッシュです）。ファイルは書き込みの権限を外して置きますが、同じユーザーで動くエージェントは権限を戻して書き換えられます。**continuo は Claude Code を起動する前に毎回、ファイルを1つずつ確かめて元へ戻します**（種類と権限も比べ、足されたファイルは消します）。**ただし、次にどれかの run を起動するまでのあいだは、走っているほかの run が、書き換えられた手順を読み、書き換えられたスクリプトを叩きえます。**issue の本文に従ったエージェントが書き換える経路もあります（上の「issue の本文が指示になる」）。issue ごとの設定ファイルを起動のたびに作り直しているのと同じ限界で、continuo は起動と起動のあいだの書き換えを見張っていません |
| **ログインしたままの Chrome を操作させる** | `continuo init` が置く `WORKFLOW.md` の本文は、Claude in Chrome（Chrome を操作する拡張機能）が使える機械では、作っているものを手元で立ち上げた画面を開いて確かめるようエージェントに言います。**その Chrome は、あなたがログインしたままのものです。**本文はページの中の指示に従わないよう言いますが、守られるかは測っていません。本文は最初のプロンプトに入るので、**判定役にはあなたの指示として届きます。**公式文書によれば、判定役が通したブラウザの呼び出しでは、`deny` でどれかのサイトを禁じていない限り、拡張機能のサイトごとの確認が省かれます。一方で判定役は、ページの中身・cookie・資格情報を別のオリジンへ送りうる操作を既定で止めます（どちらも公式文書の記述で、測っていません）。**本文の節を消しても道具は呼べるまま残ります。**減らし方は下の「使う前に減らせる危険」と [docs/FAQ.md](docs/FAQ.md) の「エージェントにブラウザで画面を確かめさせたい」にあります |
| **信頼の登録を書き換える** | `continuo trust` は `~/.claude.json` を書き換え、対象リポジトリを Claude Code に信頼登録します |
| **`curl … \| sh` で配る** | インストーラーはネットワークから取ってきて実行されます |
| **資格情報を読む** | 定額プランの枠を読むために、`~/.claude/.credentials.json` か macOS の Keychain（`rate_limit.token_source: env` なら環境変数）を読みます。**`rate_limit.source` が `statusline` か `none` なら読みません** |
| **枠を読むために、Claude Code を自分で起動する** | 枠の使用率を読む usage API が誤りのあいだは、Claude Code のステータスラインから受け取ります。**値が古いと、`trust.repositories` の信頼済みの clone の中で haiku の Claude Code を短く起動し、`hello` を1回送って閉じます**（既定で最大5分に1回）。道具を外し、`--restricted` と `--permission-mode dontAsk` で起動するので、利用者の設定ファイル・MCP サーバー・その clone の hook は読み込まれません。会話の記録は残しません。**API キーで動かしている機械では、この会話が従量で課金されます**（`rate_limit.source: none` で止まります） |
| **`continuo abandon` は消す** | worktree と branch と herdr の workspace を消します。**`--force` を付けると、コミットしていない変更と push していない commit ごと消えます**（`--dry-run` で何が消えるかを先に見られます） |
| **`continuo abandon` はカンバンも書き換える** | continuo が動いていれば、手を離させるために Status を `--park`（既定は `tracker.failure_state`）へ動かします。`--to` を付ければ片付けたあとにも動かします。**`--dry-run` はどちらも書かず、書く値を予告するだけです** |
| **信頼できる立場のコメントで頼まれると、エージェントが本番へ書き込み・マージ・release を行う** | 担当の issue のコメントで、宛先のリポジトリへ書き込み権限を持つ人がはっきり頼んだときだけ行います（頼まれなければ行いません）。**印を付け忘れた同じアカウントの AI のコメントや、印を変えた利用者では、エージェントの報告が人間のコメントに見えることがあります。**頼みの文や project の文書が本番と書いていない branch が実は本番だったときは止められません。記録のコメントの書き換えや削除も見分けられません |

**これらを踏まえたうえで、想定を超える挙動があれば報告してください。**たとえば次のようなものです。

- **意図していないリポジトリ**が信頼登録される
- **カンバンに載せていない** issue が処理される
- インストーラーが**取ってくる先を、警告なしに変えられる**
- 秘密が**ログや issue のコメントに漏れる**
- 頼まれていないのに、worktree の**外**へ書き込まれる

## 使う前に減らせる危険

| 抑え方 | どうするか |
| --- | --- |
| **自分が書いた issue だけを進める** | `Ready` へ動かすのは人間です。知らない issue を動かさないでください |
| **ラベルで絞る** | `tracker.required_labels` に印を入れ、それが付いた issue だけを対象にします |
| **信頼するリポジトリを減らす** | `continuo init` が並べた `trust.repositories` から、要らない行を消してください |
| **本番への書き込み・マージ・release を頼めなくする** | WORKFLOW.md の本文に「本番の環境への書き込み・マージ・release の作成は行わない」と書くか、それらのコマンドを `claude.permissions.deny` に足します（既定の deny の `AskUserQuestion` は残します）。どちらも変えたら continuo を再起動します。`WORKFLOW.local.md` に書くと、その機械ではその欄が置き換わります |
| **危ない道具の呼び出しを断らせる** | `claude.tool_gate.mode` は既定で `off` です。`public_only` にすると、公開リポジトリの issue のときだけ `Bash` の呼び出しを実行の前に検査します（`on` ならいつでも）。**コメントで許可を出しても通りません**（この検査は hook の入力だけを見るので、continuo が最初のメッセージに付けて渡したコメントも読みません） |
| **コメントの許可を判定役へ渡さない** | `agent.relay_trusted_comments: false` を書きます。閉じた記録も書かなくなります。許可を出す先は `claude.permissions.allow` だけになります |
| **ブラウザを触らせない** | `claude.permissions.deny` に `"mcp__claude-in-chrome__*"` を足して continuo を再起動します（deny は `auto` でも効きます）。または、Claude Code の `/chrome` で「Enabled by default」を外します。**`WORKFLOW.md` の本文の `### 画面で動作を確かめる道具` を消すだけでは止まりません** |
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
| **Runs without permission prompts** | Claude Code is started with `--permission-mode auto` (the default). **Writes to protected paths and shell commands are checked by a classifier inside Claude Code before they run.** Choosing `dontAsk` denies anything outside the allow list without asking. **Whether it falls back to a prompt after repeated classifier blocks has not been verified** |
| **It commits and pushes** | The agent writes to your repository and pushes |
| **Issue text is instructions** | The default brief has the agent read the issue body and every comment. **Text written by other people can execute on your machine** |
| **A stranger's comment can enter the decision about what may run** | With the default `--permission-mode auto`, a classifier inside Claude Code decides what may run before it runs. **Tool results are stripped from the classifier's requests** (official permission modes page, retrieved 2026-09-18). **Issue comments reach the agent as `gh` output, that is, as a tool result, so they do not reach the classifier.** We measured the grant direction on 2026-09-18 and it did not get through. **The one exception is the trusted-role comments that continuo itself attaches to the first message (next row); it never attaches comments from outsiders.** **We have not measured the injection direction.** The built-in brief tells the *agent* not to treat text from anyone outside `OWNER` / `MEMBER` / `COLLABORATOR` as an instruction, **but whether the classifier uses the same distinction is not something we have measured.** There are two ways to reduce this. **Set `claude.permission_mode` to `dontAsk`** (writes to `.claude/` and `.mcp.json` then stop working — see [docs/upgrading.md](docs/upgrading.md)). Or **set `claude.tool_gate.mode` to `public_only`** (by default only `Bash` is sent to that check; add `Write` and `Edit` to `claude.tool_gate.tools` to cover writes as well) |
| **Trusted-role comments reach the classifier as a human's permission** | Each time continuo closes Claude Code, it posts a "closed record" comment whose first line is `<!-- continuo:closed -->`. The next time it starts Claude Code, **it attaches to the first message every comment written after that record by an `OWNER` / `MEMBER` / `COLLABORATOR` without an AI marker** (with the default `auto` and `agent.relay_trusted_comments: true`). The classifier reads those as a human's permission. **The following can reach it the same way.** (1) `MEMBER` is given to every member of the organization that owns the repository, whatever their access to it. Whether a read-only collaborator counts as `COLLABORATOR` has not been measured. (2) A comment from an AI running under the same GitHub account (Claude Code on another issue, or one you started yourself) that forgot its marker. (3) The quoted part of a trusted person's comment that quotes an outsider (quotes are not stripped, because stripping them can drop the "but not this" part). **To reduce this, set `agent.relay_trusted_comments: false`** (permission given in comments then stops reaching the classifier) |
| **Claude Code under the same account can forge, delete or edit closed records** | A closed record is recognized by its first-line marker only; it is not authentication. **If Claude Code running under the same account forges a closed record or deletes a real one, the boundary that decides which comments are passed moves.** Deleting a real one moves the boundary back, so writes made in the middle of the earlier run get passed as a human's permission. Editing a human comment created after the boundary passes the edited text. **Hide closed records instead of deleting them** (hiding does not move the boundary). Telling these apart would take a separate account or a local record, and continuo uses neither |
| **An agent can rewrite the procedures it shares with other runs** | continuo writes the procedures it hands to the agent (the skills and scripts of the plugin `continuo-agent-procedures`) to `plugins/<12 hex digits>/` under its runtime directory, and every run on the same version shares them (the 12 digits are a hash of the plugin's contents). The files are written without write permission, but an agent running as the same user can restore the permission and rewrite them. **Before it starts Claude Code, every time, continuo checks the files one by one and puts them back** (it also compares file type and permissions, and deletes files that were added). **Until the next run starts, though, other runs that are already going can read the rewritten procedures and execute the rewritten scripts.** An agent following an issue's text is one way this can happen (see "Issue text is instructions" above). This is the same limit as the per-issue settings file, which is also rebuilt only when Claude Code starts; continuo does not watch for changes between starts |
| **It has the agent drive your signed-in Chrome** | The body of the `WORKFLOW.md` that `continuo init` writes tells the agent to open the screen of what it is building, started locally, and check it, on machines where Claude in Chrome (the extension that drives Chrome) is available. **That Chrome is the one you are signed in to.** The body tells the agent not to follow instructions written inside the pages it opens, but whether that holds has not been measured. The body goes into the first prompt, so **it reaches the classifier as your instruction.** According to the official docs, for a browser call the classifier approves, the extension skips its own per-site check unless your `deny` rules block some site for Claude in Chrome. On the other hand, the classifier blocks, by default, actions that could send page content, cookies or credentials off-origin (both are from the official docs and have not been measured). **Deleting that section of the body leaves the tools callable.** See "Reducing the risk before you start" below and "エージェントにブラウザで画面を確かめさせたい" in [docs/FAQ.md](docs/FAQ.md) |
| **It edits your trust settings** | `continuo trust` rewrites `~/.claude.json` to trust the target repositories |
| **It is installed via `curl … \| sh`** | The installer is fetched from the network and executed |
| **It reads credentials** | To read your plan's usage window, it reads `~/.claude/.credentials.json` or the macOS Keychain (or an environment variable with `rate_limit.token_source: env`). **It reads none of them when `rate_limit.source` is `statusline` or `none`** |
| **It starts Claude Code on its own to read the usage window** | While the usage API that reports the usage window returns errors, the usage window comes from Claude Code's status line. **When the last value is stale, continuo starts a short haiku session of Claude Code inside a trusted clone from `trust.repositories`, sends `hello` once, and closes it** (at most once every five minutes by default). It starts with no tools, `--restricted` and `--permission-mode dontAsk`, so your settings files, MCP servers and that clone's hooks are not loaded, and no transcript is kept. **On a machine that runs on an API key, that session is billed per use** (`rate_limit.source: none` stops it) |
| **`continuo abandon` deletes** | It removes the worktree, the branch, and the herdr workspace. **With `--force` it takes uncommitted changes and unpushed commits with them** (`--dry-run` shows what would go first) |
| **`continuo abandon` also writes to the board** | If continuo is running, it moves the Status to `--park` (default: `tracker.failure_state`) to make continuo let go of the issue. With `--to` it moves the Status again after cleanup. **`--dry-run` writes neither; it only announces the values it would write** |
| **Asked in a comment from a trusted role, the agent writes to production, merges, and creates releases** | It does so only when someone with write access to the target repository asks clearly in a comment on the issue it is working on (it does not do so unasked). **If an AI comment from the same account lacks the marker, or you changed the markers, the agent's own report can look like a human comment.** It cannot stop a push to a branch that turns out to be production when neither the request nor the project's documents say so. It cannot tell when a record comment was edited or deleted |

**With that understood, please report anything beyond it** — for example:

- A repository you did not list gets trusted
- An issue that is not on the board gets picked up
- The installer's download source can be changed without a warning
- Secrets leak into logs or issue comments
- Something is written outside the worktree without being asked for

## Reducing the risk before you start

| Mitigation | What to do |
| --- | --- |
| **Only advance issues you wrote** | A human moves things into `Ready`. Do not move an issue you did not read |
| **Filter by label** | Set `tracker.required_labels` so only issues carrying your marker are eligible |
| **Trust fewer repositories** | Delete the lines you do not need from the `trust.repositories` list that `continuo init` writes |
| **Stop the agent from being asked to write to production, merge, or release** | Write "do not write to production, merge pull requests, or create releases" in the body of WORKFLOW.md, or add those commands to `claude.permissions.deny` (keep the default `AskUserQuestion` in deny). Restart continuo after changing either. Writing them in `WORKFLOW.local.md` replaces that field on that machine |
| **Have dangerous tool calls refused** | `claude.tool_gate.mode` defaults to `off`. Set it to `public_only` to inspect `Bash` calls before they run when the issue is in a public repository (`on` for always). **Granting permission in a comment does not get past it** (this check only sees the hook input, so it does not read the comments continuo attaches to the first message either) |
| **Keep comment permissions away from the classifier** | Set `agent.relay_trusted_comments: false`. continuo then stops posting closed records, and `claude.permissions.allow` is the only place to grant permission |
| **Keep the agent off your browser** | Add `"mcp__claude-in-chrome__*"` to `claude.permissions.deny` and restart continuo (`deny` works under `auto` too). Or turn off "Enabled by default" in Claude Code's `/chrome`. **Deleting the `### 画面で動作を確かめる道具` section from the body of `WORKFLOW.md` does not stop it** |
| **Isolate it** | Run it under a dedicated account, or on a machine or container you can discard |
