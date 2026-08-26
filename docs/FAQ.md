# よくある質問

画面に出たメッセージから引ける一覧です。使い方は [README.ja.md](../README.ja.md) と
[trying_it_out.md](trying_it_out.md) にあります。

困ったら、まず `continuo doctor` を叩いてください。設定ファイル / claude / hook の置き場所 /
Claude の設定 / worktree の場所 / herdr / gh の認証 / ボード / clone / 信頼登録 / 資格情報の
11個を調べます。`✗` が1つでもあれば終了コードは 1、`!` だけなら 0 です。

```bash
cd ~/continuo-work && continuo doctor
```

---

## 起動できないとき

### 起動した瞬間に「hook を受ける socket のディレクトリの親を作成できません」と出る

**原因。**`XDG_RUNTIME_DIR` が設定されているのに、そのディレクトリが実在しません。
`/run/user/<uid>` を作るのは systemd であって、continuo が作ってよい場所ではありません。

**直し方。**まず版を上げてください。実在しない `XDG_RUNTIME_DIR` と `TMPDIR` は、いまは使いません。
それでも置き場所を選び直したいときは、書ける場所を明示します。

```bash
CONTINUO_RUNTIME_DIR="$HOME/.continuo/run" continuo doctor
```

`hook の置き場所` が `✓` なら起動できます。**doctor は実際にそこへ socket を作って閉じています。**

### 「hook を受ける socket のパスが長すぎます（… バイト。上限は 103 バイト）」で止まる

**原因。**macOS の Unix domain socket は、絶対パスが104バイト以上になると bind に失敗します。
`/var/folders/…` の下は既定でも長いので、ホームの下に深い置き場所を指定すると超えます。

**直し方。**短い場所を指定します。

```bash
CONTINUO_RUNTIME_DIR=/tmp/continuo-run continuo
```

### 「既にある hook を受ける socket のディレクトリ … の権限が 0755 です」で止まる

**原因。**continuo は、自分が作っていないディレクトリの権限を書き換えません。
symlink も受け付けません（辿った先へ socket とロックファイルが落ちるためです）。

**直し方。**

```bash
chmod 700 /tmp/continuo-run
CONTINUO_RUNTIME_DIR=/tmp/continuo-run continuo doctor
```

### 「二重起動を検出しました（ロックファイル …）」で起動できない

**原因。**別の continuo が動いています。または**ロックファイルの置き場所が食い違っています。**
置き場所は `CONTINUO_RUNTIME_DIR` / `XDG_RUNTIME_DIR` / `TMPDIR` で決まるので、
launchd から起動した continuo と、端末で叩いたコマンドが別の場所を見ることがあります。

**直し方。**動いている continuo を止めるか、同じ環境変数で叩き直します。

```bash
pgrep -fl continuo
CONTINUO_RUNTIME_DIR="$HOME/.continuo/run" continuo
```

### 「front matter が不正です: unknown field "…"」で止まる

**原因。**continuo を更新して設定のキーが増減しました。front matter は未知のキーを弾きます。

**直し方。**出たキーの行を `WORKFLOW.md` から消してください。
**`continuo init --force` は使わないこと。**`continuo setup` で決めた Status の割り当てが雛形で潰れます。

```bash
grep -n "消したいキー名" ~/continuo-work/WORKFLOW.md
```

**v0.x のうちはキーの改名・削除がありえます。**更新したら release notes を見てください。

### 「埋めていない設定が … 件あります（`__FILL_ME__` のままです）」と出る

**原因。**`continuo init` が gh からボードを引けなかったので、値がプレースホルダのまま残っています。
**ファイルは読めていて、中身が悪いだけです。**

**直し方。**`WORKFLOW.md` の front matter に値を直接書くか、値を渡して作り直します。
ボードの番号は `gh project list` の左端の数字です。

```bash
gh project list --owner <owner>
continuo init --owner <owner> --project <番号> ~/continuo-work
```

---

## doctor が通らないとき

### `✗ ボード  Status の選択肢名が設定と一致しません`

**原因。**GitHub の既定の Status は `Todo` / `In Progress` / `Done` の3つだけです。
continuo は5つの役割それぞれに別の選択肢を使います。
GraphQL はエラーを出さずに0件を返し続けるので、起動時の検査でここで止めています。

**直し方。****足りない選択肢は GitHub の画面から足します。**
ボードの `Settings` → 左の `Custom fields` の `Status` → `Options` の `Add option...`。名前は何でも構いません。
足したら役割との対応を付け直します。

```bash
gh project field-list <番号> --owner <owner>   # いまの選択肢を確かめる
continuo setup ~/continuo-work                      # 5つの役割に、どの選択肢を使うかを対話で決める
```

**API（`gh project field-create` / `updateProjectV2Field`）で足してはいけません。**
選択肢の指定は全件の置き換えとして扱われ、GitHub が ID を採番し直すので、**設定済みの Status の値が全部消えます。**

### `✗ gh の認証  gh の scope に "project" がありません`

**原因。**`gh auth login` のときに project の scope を付けていません。`read:project` では Status を書けません。

**直し方。**

```bash
gh auth refresh -h github.com -s project   # 既にログイン済みならこちら
gh auth login -s project                   # 未ログインならこちら
```

確かめるなら次を叩きます。`project` が単独で並んでいれば通ります。

```bash
gh auth status
```

### `✗ clone  ghq が PATH にありません`

**原因。**continuo は clone の場所の解決に `ghq`、worktree の作成に `git` を起動します。
**この2つが無いと、着手は必ず落ちます。**

**直し方。**両方入れて PATH を通してから、もう一度調べます。

```bash
command -v git ghq
cd ~/continuo-work && continuo doctor
```

### `✗ clone  対象 N件のうち M件が見つかりません`

**原因。**ボードに載っているだけのリポジトリを、continuo は無断で取ってきません。
着手も `巡回のループは勝手に clone しません` で止まります。

**直し方。**`WORKFLOW.md` の `trust.repositories` にそのリポジトリがあることを確かめてから
`continuo trust` を叩きます。**clone の取得と信頼の登録をまとめて行います。**

```bash
ghq list -p -e <owner>/<repo>       # 0行なら手元にありません
continuo trust ~/continuo-work
```

clone だけ取るなら `ghq get <owner>/<repo>` です。

### `✗ 信頼登録  対象 N件のうち M件が未承認です`

**原因。****信頼の門番は `~/.claude.json` です。**`WORKFLOW.md` の `trust.repositories` に書くだけでは足りません。
未承認のまま巡回すると、その issue は飛ばされ、worktree も pane も作られず、
そのリポジトリにつき1回だけ issue にコメントが投稿されます。

**直し方。**

```bash
continuo trust --dry-run ~/continuo-work   # 何を許すことになるかを先に見る（~/.claude.json は書き換えない）
continuo trust ~/continuo-work
```

**Claude Code のセッションが動いていると `~/.claude.json` を取り合って、書き換えが失われることがあります。**
気になるなら Claude Code を全部終了してから叩いてください。

### `continuo trust` が「~/.claude.json がありません」で終わる

**原因。**Claude Code をこの機械で1度も起動していません。continuo はこのファイルを作りません。

**直し方。**Claude Code を1度起動して初回の設定を済ませてから、もう一度叩きます。

```bash
claude --version && ls -la ~/.claude.json
```

### `✗ 資格情報  Keychain の項目 "Claude Code-credentials" を読めません`（macOS）

**原因。**Keychain の読み取りを1回も許可していません。
または claude でログインしていない / 別のユーザーのログイン Keychain に入っている /
ログイン Keychain がロックされている / ダイアログで「許可しない」を選んだ、のどれかです。

**直し方。**1回だけ許可します。**確認のダイアログでは「常に許可」を選んでください**（「許可」だけだと次にまた出ます）。

```bash
continuo allow-keychain-access
```

**読めないままでも continuo は起動します。**枠の判定ができないだけです。
やめるなら `WORKFLOW.md` の `rate_limit.token_source` を `env` にして `token_env` に環境変数名を書くか、
`rate_limit.source` を `none` にします。

項目そのものを確かめたいときは次を叩きます。**`-w` は付けないでください**（トークンが端末に出ます）。

```bash
security find-generic-password -s "Claude Code-credentials"
```

### `continuo allow-keychain-access` が返ってこない

**原因。**Keychain の確認のダイアログが出たまま、誰も答えていません。
**別のウィンドウの裏に隠れていることがあります。**

**直し方。**画面のダイアログを探して「常に許可」を選び、もう一度叩きます。

```bash
continuo allow-keychain-access
```

### `✗ herdr  herdr の protocol 版が設定と一致しません`

**原因。**continuo が想定している herdr の socket の protocol 版と、入っている herdr の版が食い違っています。

**直し方。**herdr を設定に合う版へ更新するか、`WORKFLOW.md` の `herdr.protocol` を herdr が返した版に合わせます。
**herdr 0.8.2 は protocol 20、0.8.0 は 19 です。**

```bash
herdr --version
grep -n "protocol:" ~/continuo-work/WORKFLOW.md
```

### `! clone` `! 信頼登録` と出るが、何が悪いのか分からない

**原因。****異常ではありません。**`!` は「足りない」ではなく「確かめられなかった」です。
`Ready` / `In Progress` に issue が1件も無いと、どのリポジトリを検査すべきかが決まりません。

**直し方。**ボードに issue を1件載せて `Ready` にしてから、もう一度叩けば判定が出ます。
**`!` だけなら終了コードは 0 のままです。**

```bash
cd ~/continuo-work && continuo doctor; echo "exit=$?"
```

---

## issue が動かないとき

### ボードに載せた item が1件も処理されない。エラーも出ない

**原因。**ボードの **draft item** はリポジトリを持たないので、作業場所を決められません。continuo は飛ばします。

**直し方。**draft ではなく、リポジトリの issue をボードに載せます。

```bash
gh issue create --repo <owner>/<repo> --title "…" --body "…"
gh project item-add <番号> --owner <owner> --url https://github.com/<owner>/<repo>/issues/42
```

### `continuo setup` が「使うボードの番号が決まりませんでした」で止まる

**原因。**ボードが organization にあるのに、以前の版は個人アカウントのログイン名しか見ていませんでした。
GitHub Enterprise で organization のボードを使っていると必ずこうなります。
`--project <番号>` を付けても `Could not resolve to a ProjectV2 with the number N. (user.projectV2)` になります。

**直し方。**いまは `gh api user/orgs` を引いて organization も探します。
それでも複数見つかるときは owner を明示します。

```bash
gh project list --owner <owner>
continuo setup --owner <owner> --project <番号> ~/continuo-work
```

### 着手が「1つの branch を出せる worktree は1つだけなので…」で止まる

**原因。**同じ branch を別の worktree が既にチェックアウトしています。
前の run の worktree が別の場所に残っているか、人間が手で同じ branch の worktree を作っています。

**直し方。**どこが出しているかを見てから片付けます。**消したくない作業が無いかを先に確かめてください。**

```bash
git -C ~/ghq/github.com/<owner>/<repo> worktree list
continuo abandon https://github.com/<owner>/<repo>/issues/42 ~/continuo-work
```

### 着手が「目的のパスに実体があるのに git の worktree として登録されていません」で止まる

**原因。**worktree の置き場所にディレクトリだけが残っています。
**continuo が作ったものとは限らないので、乗っ取らずに止めます。**

**直し方。**中身を確かめてから片付け、ボードでその issue を `Ready` へ戻します。

```bash
ls -la ~/worktrees/github.com/<owner>/<repo>/continuo-<owner>-<repo>-42
continuo abandon https://github.com/<owner>/<repo>/issues/42 ~/continuo-work
```

**この判定は着手の最初に行います。**落ちても Status は1バイトも書きません。

### 着手が「`herdr.worktree.base` が空で、ボードから引いた issue にも既定 branch の情報がありませんでした」で止まる

**原因。**base を書いていないときはボードが返す既定 branch を使いますが、それが取れませんでした。

**直し方。**`WORKFLOW.md` に branch 名を書きます。

```bash
grep -n -A2 "worktree:" ~/continuo-work/WORKFLOW.md   # herdr.worktree.base の行を探す
```

```yaml
herdr:
  worktree:
    base: "main"
```

### 「起動直後に確認の画面で止まりました」と出る

**原因。**そのフォルダが Claude Code に信頼登録されていないか、許可されていないコマンドを実行しようとしました。

**直し方。**まず画面を読みます。

```bash
herdr agent read continuo-hello-world-42 --source recent-unwrapped --lines 40
```

信頼登録が足りないなら `continuo trust ~/continuo-work`。
許可が要るなら `WORKFLOW.md` の `claude.permissions.allow` に足します。

### 「Claude Code が起動しませんでした（herdr が返した状態: "unknown"）」と出る

**原因。**`claude` が PATH に無い / 起動が途中で失敗した / そのフォルダが信頼登録されていない、のどれかです。

**直し方。**

```bash
command -v claude
herdr agent explain continuo-hello-world-42
herdr agent read continuo-hello-world-42 --source recent-unwrapped --lines 40
```

`continuo doctor` は `claude` という見出し語で PATH 上の実行ファイルを調べています。

### 「Claude Code の起動が N ミリ秒たっても落ち着きませんでした」と出る

**原因。**起動時の処理が重いか、ネットワークが遅いためです。

**直し方。**`WORKFLOW.md` の `herdr.startup_timeout_ms`（既定 60000）を増やします。

```bash
grep -n "startup_timeout_ms" ~/continuo-work/WORKFLOW.md
```

### issue が急に `Blocked` になった

**原因。**エージェントが判断を仰いだか、打ち切られました。
打ち切りは、**herdr が見ている画面の版が変わらないまま** `claude.turn_timeout_ms`（既定1時間）が過ぎたときです。

**直し方。****issue のコメントを開いてください。**何が起きたか・どう確かめるか・どう直すかが書いてあります。
対応方法をコメントに書いて `Ready` へ戻せば続きが動きます。

```bash
gh issue view https://github.com/<owner>/<repo>/issues/42 --comments
```

**`turn_timeout_ms` は turn の総実行時間の上限ではありません。**
画面が変わり続けている限り、1つの指示に何時間かかっても打ち切りません。

### issue が `In Review` にならない

**原因。**エージェントが `CONTINUO-STATUS: review` を出していません。
**continuo が信じるのはカンバンの Status だけです。**エージェントが「終わった」と言っても、Status が動いていなければ終わっていません。

**直し方。**pane で応答を読み、`WORKFLOW.md` の下半分（1回目のプロンプト）に依頼が入っているかを確かめます。

```bash
herdr agent read continuo-hello-world-42 --source recent-unwrapped --lines 40
grep -n "CONTINUO-STATUS" ~/continuo-work/WORKFLOW.md
```

**書き換えたら continuo を再起動してください。**動いている最中は読み直しません。

### Claude Code が `SessionStart:startup hook error / EROFS: read-only file system` で止まり続ける（WSL）

**原因。****continuo の欠陥ではありません。**WSL のファイルシステムが壊れて `EIO` と `EROFS` が同時に出ています。
Claude Code は SessionStart hook を走らせる前に `~/.claude/session-env/<session id>/` を作るので、
ホームが書けないと着手できません。**設定を作り直しても直りません。**

**直し方。**上から順に確かめます。

```bash
mount | grep ' / '        # ルートが ro で再マウントされていないか
dmesg | grep -i ext4      # I/O エラーが出ていないか
df -h ~
```

Windows 側の C ドライブの空き容量も見てください（仮想ディスクを伸ばせないと書き込みが落ちます）。
最後に PowerShell で `wsl --shutdown` を実行し、Windows を再起動してから開き直します。
**この症状は再起動で直った実例があります。**

`continuo doctor` の `Claude の設定` が `✓` なら、この症状は起きません
（使い捨ての名前で実際に作って消しています）。

---

## 片付けたいとき

### 間違えた issue を `Ready` に置いてしまった。ボードで戻せばよい？

**原因。****ボードの操作だけでは取り消せません。**`Ready` は作業中の Status の1つなので止まらず、
もう一度着手されることがあります。`Done` へ動かすと、continuo が片付ける前に
「この作業のコメントが issue にあるか」を確かめ、無ければセッションを復元して書かせようとするので、
**Claude Code が起動し直されます。**

**直し方。****`continuo abandon` を使います。**

```bash
continuo abandon --dry-run https://github.com/<owner>/<repo>/issues/42 ~/continuo-work
continuo abandon https://github.com/<owner>/<repo>/issues/42 ~/continuo-work
```

`--dry-run` はボードに1文字も書きません。詳しい振る舞いは [README.ja.md](../README.ja.md) の
「間違えて着手したとき」にあります。

### `continuo abandon` が「失うものがあるので何も消しません」で止まる

**原因。**コミットしていない変更か、push していない commit があります。

**直し方。**承知のうえで消すなら `--force` を付けます。

```bash
continuo abandon --dry-run https://github.com/<owner>/<repo>/issues/42 ~/continuo-work
continuo abandon --force   https://github.com/<owner>/<repo>/issues/42 ~/continuo-work
```

### `continuo abandon` が「`--park` に指定した … は tracker.active_states の値です」で止まる

**原因。**手を離させる先に**作業中の Status** を指定しました。
そこへ動かしても continuo は手を離さず、pane も閉じません。

**直し方。**`tracker.active_states` に入っていない値を渡します（省略すると `tracker.failure_state`、既定 `Blocked`）。

```bash
grep -n "active_states\|failure_state" ~/continuo-work/WORKFLOW.md
continuo abandon --park "Ice Box" https://github.com/<owner>/<repo>/issues/42 ~/continuo-work
```

**手を離させたあとで止まった場合、Status はその値のまま残ります。**
continuo は元へ戻しません（戻す先が作業中の Status なので、戻した瞬間に拾い直しかねないためです）。
戻すかどうかはボードで決めてください。

### `continuo abandon --dry-run` が「失われるものを調べられません: … invalid gitfile format」で何も出さずに終わる

**原因。**worktree の `.git` は1行だけのファイルで、空になったり書き換えられたりすると、
その中では git が1つも通らなくなります。**古い版は、そこで何もせずに終了していました。**

**直し方。**いまは止まりません。調べられなかったことを並べたうえで一覧を出し、
**「失うものはありません」とは決して言いません。**中身が分からないものを消すには `--force` が要ります。

```bash
continuo abandon --dry-run https://github.com/<owner>/<repo>/issues/42 ~/continuo-work
continuo abandon --force   https://github.com/<owner>/<repo>/issues/42 ~/continuo-work
```

`--force` を付けると、`.git` を読まずに worktree のディレクトリ・branch・herdr の workspace を消し切ります。

### worktree だけ消えて branch が残った

**原因。**片付けの途中で失敗すると、branch だけ残ることがあります。
**古い版は worktree を起点に対象を探していたので、この状態に手が出せませんでした。**

**直し方。**worktree が無くても片付けられます。**先頭の commit と、どの remote にも載っていない commit の件数を見てから消します。**

```bash
continuo abandon --dry-run https://github.com/<owner>/<repo>/issues/42 ~/continuo-work
continuo abandon --force   https://github.com/<owner>/<repo>/issues/42 ~/continuo-work
```

消したときは、戻すためのコマンド（`git -C <clone> branch <名前> <SHA>`）が画面に出ます。

### `continuo abandon` が、無いはずの branch を「残っています」と言う

**原因。**古い版は branch 名を規則から組み立てるだけで、リポジトリに実在するかを確かめていませんでした。
**「現物を引けない」と「存在しない」を区別していませんでした。**

**直し方。**版を上げてください。いまは消す前に実在を確かめ、無ければ
`branch … も残っていません。片付けるものはありません。` とだけ出て終わります。

```bash
git -C ~/ghq/github.com/<owner>/<repo> branch --list 'continuo/*'
```

### `error: cannot delete branch '…' used by worktree at '…'` と出る

**原因。**git が、その branch を出している worktree の**登録**を見て守っています。
**continuo は登録を勝手に外しません。**`git worktree prune` をリポジトリ全体へ撃つと、
**単に移動しただけの別の worktree まで壊れる**ためです。

**直し方。****prune を撃つかどうかは利用者が決めます。**
continuo は登録が指すパスを画面に出して止まるので、**そのディレクトリが本当に無いことを確かめてから**叩いてください。

```bash
git -C ~/ghq/github.com/<owner>/<repo> worktree list
git -C ~/ghq/github.com/<owner>/<repo> worktree prune
```

### `continuo abandon --force` のあとに「branch … は残っています（消してよい branch だと検算できませんでした）」と出る

**原因。**branch を消してよいかを worktree の現物から検算していますが、
`.git` が壊れていたり worktree が既に消えていたりすると引けません。**安全側に倒して残しています。**

**直し方。**検算できなかった理由はメッセージの括弧に出ます。手で消すなら次を叩きます。
**先に何が失われるかを確かめてください。**

```bash
git -C ~/ghq/github.com/<owner>/<repo> log --oneline -5 continuo/<owner>/<repo>/42
git -C ~/ghq/github.com/<owner>/<repo> branch -D continuo/<owner>/<repo>/42
```

そもそも `cleanup.delete_branch` が `false` なら、continuo は branch を消しません（そう出ます）。

### `fatal: cannot lock ref '…': reference broken` で着手できない。`git branch -D` も通らない

**原因。**壊れているのは `.git` 全体ではなく、
`<clone>/.git/refs/heads/continuo/<owner>/<repo>/<番号>` という**1つのファイル**です。
branch がどの commit を指すかを書いた40文字ほどのテキストが読めなくなっています。
git は消す前に中身を読もうとするので、**git のコマンドでは消せません。**

**直し方。**版を上げてください。continuo がその1ファイルだけを消してから、
worktree の作成をもう一度だけやり直します。**消してよい条件は7つあり、1つでも欠けたら1バイトも消しません。**
消したあとは、reflog から控えた commit と戻すコマンドを画面に出します。
`continuo abandon` も同じ扱いなので、壊れた ref のせいで片付けられなかった issue も片付けられます。

```bash
continuo abandon --dry-run https://github.com/<owner>/<repo>/issues/42 ~/continuo-work
```

---

## 気にしなくてよいもの

### ログに「hook の transcript_path を … 捨てました」が WARN で何度も出る

**原因。**まだ書かれていない transcript のファイルに対して、symlink の解決が必ず失敗していました。
**turn の終わりの検知には影響していません。**

**直し方。****対処は要りません。**実在するときだけ symlink を解決するようにし、
「ファイルが無い」場合は Debug へ落としました。版を上げれば WARN は出なくなります。

```bash
continuo version
```

### 「候補の取得に失敗しました … 絞り込みが効いていません」で巡回が止まる

**原因。**Status を書いた直後の、GitHub 側の反映待ちです。
**古い版は、頼んだ Status に無い item が1件混ざっただけで巡回全体をエラーにしていました。**

**直し方。****対処は要りません。**いまは、頼んだ Status に無い item を落として続けます。
エラーにするのは**外れた item が大半を占めるとき**だけです。
それでも出るなら、`tracker.provider.status_field` に書いた名前がボードのフィールド名と一致しているかを確かめます。

```bash
grep -n "status_field" ~/continuo-work/WORKFLOW.md
gh project field-list <番号> --owner <owner>
```

### issue を1件処理するたびに herdr の workspace が閉じ残る

**原因。**`worktree.open` に clone 本体のパスを渡すと、herdr は workspace を2つ開きます。
片付けは worktree 側しか閉じていませんでした。

**直し方。****対処は要りません。**親の workspace の ID を身元ファイル（`.continuo.json` の
`herdr_repo_workspace_id`）に控え、片付けで閉じます。
**人間が自分で開いた workspace は閉じません**（continuo が開かせたもので、かつ同じリポジトリの
worktree workspace が1つも残っていないときだけ閉じます）。確かめるなら、issue を1件通す前後で数えます。

```bash
herdr workspace list | tr ',' '\n' | grep -c '"workspace_id"'
```

### herdr の一覧で pane を見分けられない

**原因。**古い版は pane と workspace の label に issue の URL をそのまま入れていたので、
先頭が全部 `https://github.com/` になり、見分けたい部分が右へ押し出されていました。

**直し方。**いまの label は `<owner>/<repo>/issues/<番号>` の形です。

```bash
herdr pane list | tr ',' '\n' | grep '"label"'
```

**label は人間が見分けるための表示名で、continuo は読み戻しません。**
復元の照合は pane の cwd と worktree のパス1本なので、
古い形式の label が付いた pane が残っていても引き継ぎは壊れません。

---

## 使い方が分からないとき

### `init` / `setup` / `trust` / `doctor` / `abandon` のどれを使えばいい？

**原因。**サブコマンドが分かれていて、叩く順番と役割が一覧になっていません。

**直し方。**上から順に1回ずつ叩けば通ります。手元の一覧は `continuo --help` に出ます。

```bash
continuo --help
```

| コマンド | 何をするか |
| --- | --- |
| `continuo init [ディレクトリ]` | `WORKFLOW.md` の雛形を置く。`--force` は setup 済みなら使わない |
| `continuo setup [ディレクトリ]` | ボードの Status を5つの役割へ対応づける（対話） |
| `continuo trust [ディレクトリ]` | 対象リポジトリを Claude Code に信頼登録する。`--dry-run` で下見 |
| `continuo doctor [ディレクトリ]` | 前提が揃っているかを11の見出し語で調べる |
| `continuo abandon <URL> [ディレクトリ]` | 間違えて着手した issue を着手前へ戻す |
| `continuo allow-keychain-access` | macOS だけ。枠を読むために1回 |
| `continuo` | 常駐を始める。`--port` でダッシュボード、`--log-level` |

`continuo hook` は Claude Code の hook から呼ばれるもので、人間が直接叩くものではありません。

### フラグを位置引数の後ろに書いてもいい？

**原因。**Go の flag パッケージは、既定では位置引数より後ろのフラグを読みません。

**直し方。****continuo は前でも後ろでも受け付けます。**次の2つは同じ意味です。

```bash
continuo trust ~/continuo-work --dry-run
continuo trust --dry-run ~/continuo-work
```

`--` より後ろは、`-` で始まっていても位置引数として扱います。
知らないフラグは、どこに書いてもエラーのままです（終了コード 2）。

### `WORKFLOW.md` を書き換えたのに反映されない

**原因。**continuo は動いている最中に設定を読み直しません（実装しないと決めています）。

**直し方。****continuo を再起動してください。**再起動は安全に作ってあり、
`restart.orphan_running_action` が実行中の run を引き継ぐか着手待ちへ戻します。**worktree も pane も残ります。**

```bash
cd ~/continuo-work && continuo doctor && continuo
```

### 手元の変更が herdr との組み合わせで壊れていないか確かめたい（開発者向け）

**原因。**mock だけのテストは、herdr の本物と引数がずれていても緑になります。
**実際に、全パッケージのテストが緑のまま実機で全件落ちたことがあります。**

**直し方。**herdr だけは本物を叩くテストがあります。
**Claude Code は叩きません**（定額プランの枠を消費するため）。**GitHub の GraphQL も叩きません。**

```bash
git clone https://github.com/maimuzo/continuo.git /tmp/continuo-src
cd /tmp/continuo-src && go test -count=1 -v ./test/live/
```

**`-count=1` を外さないでください**（キャッシュされると本物を叩かなくなります）。
herdr が無ければ静かに飛びます。開発とテストの全体は [CONTRIBUTING.md](../CONTRIBUTING.md) にあります。
