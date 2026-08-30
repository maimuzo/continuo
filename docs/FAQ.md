# よくある質問

画面に出たメッセージから引ける一覧です。使い方は [README.ja.md](../README.ja.md) と
[trying_it_out.md](trying_it_out.md) にあります。
**新しい版に上げたあと何を足せばよいかは [upgrading.md](upgrading.md) にあります。**

困ったら、まず `continuo doctor` を叩いてください。設定ファイル / 片付けの状態 /
未記入の項目 / claude / hook の置き場所 / Claude の設定 / worktree の場所 / herdr /
gh の認証 / ボード / Status の名前 / 対応表のキー / clone / 信頼登録 / 資格情報の15個を調べます。
`✗` が1つでもあれば終了コードは 1、`!` だけなら 0 です。

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

**原因。**別の continuo が既に動いています。
**ロックは `~/.continuo/continuo.lock` の1本に固定されています。**
環境変数（`CONTINUO_RUNTIME_DIR` / `XDG_RUNTIME_DIR` / `TMPDIR`）では動きません。
**1台で continuo は1本だけ、と覚えてください。**

**直し方。**動いているものを止めるか、**`--id <名前>` を付けて別の1本として動かします。**

```bash
pgrep -fl continuo
continuo --id e2e ~/continuo-work    # 別の1本として動かす
```

**`--id` は、分けるべきものを5つまとめて分けます。**

| 分ける対象 | `--id e2e` を付けたとき |
| --- | --- |
| **ロック** | `~/.continuo/id/e2e/continuo.lock` |
| **socket と実行時ディレクトリ** | `~/.continuo/id/e2e/run/` |
| **worktree の置き場所** | `<workspace.root>/e2e` |
| **branch 名** | `e2e/` を先頭に付けたもの |
| **herdr の agent 名** | `continuo-e2e-<repo>-<番号>` |

**`--id` を付けると `CONTINUO_RUNTIME_DIR` と `claude.hook_bridge.listen` は使いません。**
指定してあれば、そのことを起動時のログに1行出します。

**名前は小文字の英数字とハイフンだけです。**先頭は英数字、32文字まで。
**`continuo abandon` にも同じ名前を渡してください。**渡さないと既定の1本を見に行き、
`--id` で作った worktree も branch も見つけられません。
**渡し忘れても、ボードのロック（下の節）で止まります。**

**`continuo doctor` にも同じ名前を渡してください。**

```bash
continuo doctor --id e2e ~/continuo-work
```

**渡さないと既定の場所だけを見ます。**`--id` を付けた起動は socket もロックも
`~/.continuo/id/e2e/` を使うので、**全項目 `✓` が出たのに起動だけが落ちることがあります。**

### 「同じボード（… の project #…）を見ている continuo が既に動いています」で起動できない

**原因。****同じボードを2つの continuo が見ると、同じ issue を2つが拾います。**
だから**ボード1枚につきロック1本**を取り、取れなければ起動を止めます
（`~/.continuo/board/<owner>-<番号>.lock`）。

**`--id` を付けても、これは回避できません。**ボードだけは名前から分けられないからです。

**直し方。**誰が握っているかは、ロックの隣の覚え書きに書いてあります。

```bash
cat ~/.continuo/board/<owner>-<番号>.json
```

```json
{
  "owner": "octocat",
  "project_number": 10,
  "instance_id": "e2e",
  "pid": 12345,
  "config_path": "~/continuo-e2e-work/WORKFLOW.md",
  "lock_file": "~/.continuo/id/e2e/continuo.lock",
  "started_at": "2026-08-30T12:00:00+09:00"
}
```

**この覚え書きは、人間が読むためだけのものです。**排他の判定には使いません
（判定は `flock` 1本だけです）。**握っていたプロセスが死ねば、OS がロックを解放します。**
**残骸を消す必要はありません。**

**覚え書きは、continuo が終わるときに消します。**
**ただし、覚え書きだけが残ることがあります**（電源が落ちた・`kill -9` された・消せなかった）。
**残っていても、そのプロセスが生きているとは限りません。**確かめ方は次のとおりです。

```bash
ps -p "$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["pid"])' ~/.continuo/board/<owner>-<番号>.json)"
```

**`ps` が何も出さなければ、その覚え書きは古いものです。**消して構いません
（**ロックのほうは消さないでください。**OS が解放するので、消す必要がありません）。

**2本目を動かしたいなら、別のボードを見せてください。**

### 「--id に渡した名前が使えません」で起動できない

**原因。**`--id` に書けるのは、**小文字の英数字で始まり、以降が小文字の英数字とハイフンだけ**の
名前です。**32文字まで。****大文字・空白・`.`・`/` は弾きます。**

**この文字列はパスにも branch 名にも socket のパスにも入ります。**
絞らないと `--id ../../etc` が `~/.continuo` の外を指し、空白や `..` は git の branch 名として不正になります。

**直し方。**名前を書き直してください。

```bash
continuo --id e2e ~/continuo-work        # ○
continuo --id issue-87 ~/continuo-work   # ○
continuo --id E2E ~/continuo-work        # ×（大文字）
continuo --id "my id" ~/continuo-work    # ×（空白）
```

**「hook を受ける socket のパスが長すぎます」と一緒に出たときは、名前が長すぎます。**
socket は `~/.continuo/id/<名前>/run/hooks.sock` になるので、
**ホームディレクトリのパスが長いと、それだけで上限（103バイト）に近づきます。**名前を短くしてください。

### `runtime.lock_file` にパスを書いたのに、そこにロックができない

**起動のときに、こう出ているはずです。**

```
level=WARN msg="runtime.lock_file はもう効きません（この設定は無視して、機械で決めた場所のロックを使います）。
                1台で2本以上動かしたいなら --id <名前> を使ってください
                （ロック・実行時ディレクトリ・worktree の置き場所・branch 名が、その名前ごとに分かれます）"
                runtime.lock_file=/tmp/continuo.lock
```

**原因。****`runtime.lock_file` は読まなくなりました。**ロックは `~/.continuo/continuo.lock` です。
設定でロックの場所を変えられると、`continuo abandon` が別の場所を見て「動いていない」と判定し、
**走っている worktree を消しに行くからです。**

**キーは受け取ります。****書いてあっても起動は止まりません。**
`lock_file: null` は `continuo init` の雛形が置いていった行なので、
**キーごと弾くと、これまでに `continuo init` した人が全員、次の起動で落ちます。**

**直し方。****ロックを分けたいのなら、`--id <名前>` を使ってください**
（上の「二重起動を検出しました」を見てください）。**警告を止めたいだけなら、この2行を消します。**

```bash
grep -n -A1 "^runtime:" ~/continuo-work/WORKFLOW.md
```

```yaml
runtime:
  lock_file: null                           # ← 消してよい。残しても起動する
```

**消しても `continuo doctor` は「未記入の項目」として挙げません。**雛形から外してあります。

### 「front matter が不正です: unknown field "…"」で止まる

**原因。**continuo を更新して設定のキーが増減しました。front matter は未知のキーを弾きます。

**直し方。**出たキーの行を `WORKFLOW.md` から消してください。
**`continuo init --force` は使わないこと。**`continuo setup` で決めた Status の割り当てが雛形で潰れます。

```bash
grep -n "消したいキー名" ~/continuo-work/WORKFLOW.md
```

**v0.x のうちはキーの改名・削除がありえます。**更新したら release notes を見てください。

### インストーラーが「破壊的変更があります」と出した

**原因。**上げた版に、設定を直さないと動かない変更が入っています。
**実行ファイルの入れ替えは終わっています。**インストーラーはここで止めません。

**直し方。**出てきた行のとおりに `WORKFLOW.md` を直してから起動してください。
直さずに起動すると、1つ上の「front matter が不正です」で落ちます。

```bash
# 直したあとで確かめる
continuo doctor ~/continuo-work
```

**出なくても変更が無いとは限りません。**インストーラーは、
**いま入っているものが版を名乗るときにだけ**比べます。
ソースから作ったもの（`continuo version` が `dev` と答えるもの）から上げたときと、
初めて入れたときは何も言いません。**その2つの場合は release notes を読んでください。**

### 「埋めていない設定が … 件あります（`__FILL_ME__` のままです）」と出る

**原因。**`continuo init` が gh からボードを引けなかったので、値がプレースホルダのまま残っています。
**ファイルは読めていて、中身が悪いだけです。**

**直し方。**`WORKFLOW.md` の front matter に値を直接書くか、値を渡して作り直します。
ボードの番号は `gh project list` の左端の数字です。

```bash
gh project list --owner <owner>
continuo init --owner <owner> --project <番号> ~/continuo-work
```

### `continuo init --force` が「WORKFLOW.md を書き換える一時ファイルを作れません」で止まる

**原因。**既にある `WORKFLOW.md` を書き換えるとき、continuo は
**同じディレクトリに一時ファイルを書いてから差し替えます。**
途中で落ちても、手で直した `WORKFLOW.md` が失われないようにするためです。
**そのため、`WORKFLOW.md` を置いてあるディレクトリ自身への書き込み権限が要ります。**
ファイルだけ書ければ足りた頃とは、要る権限が違います。

**直し方。**ディレクトリの権限を見てください。`continuo setup` の書き換えも同じ経路です。

```bash
ls -ld ~/continuo-work
chmod u+w ~/continuo-work
```

**ファイルが読み取り専用（`0444` など）でも止まりません。**
差し替えに要るのはディレクトリの権限だけなので、**読み取り専用の `WORKFLOW.md` も `--force` で置き換わります。**
消えては困る内容があるなら、権限ではなく控えで守ってください。

```bash
cp ~/continuo-work/WORKFLOW.md ~/continuo-work/WORKFLOW.md.bak
```

**`WORKFLOW.md` が symlink のときは、`--force` でも辿らずに止まります**
（リンク先を雛形で潰さないためです）。実体を置き直してください。

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

### `! 片付けの状態  cleanup.on_states に、tracker.terminal_states の外の Status があります`

**原因。**片付けを始める Status（`cleanup.on_states`）が、
終わったとみなす Status（`tracker.terminal_states`）に入っていません。
**この状態だと、continuo は同じ issue を「終わっていない」と判定した直後に worktree を片付けます。**

**よくある形。**ボードの組み込みの自動化が PR のマージで `Done` を書くのに、
`tracker.terminal_states` には `AI Done` しか書いていない、という組み合わせです。

**直し方。**どちらかにそろえます。**同じ内容の警告が、起動したときにもログへ1行出ます。**

```yaml
tracker:
  terminal_states: ["AI Done", "Done"]   # 片付ける Status を、こちらにも並べる
cleanup:
  on_states: ["AI Done", "Done"]         # あるいは、こちらから外の値を外す
```

**起動は止まりません。**`!` なので終了コードも 0 のままです。

### `! 未記入の項目  WORKFLOW.md に書かれていない設定項目があります`

**原因。**`continuo init` が置く雛形にある設定項目が、あなたの `WORKFLOW.md` に書かれていません。
**版を上げて設定項目が増えたときに出ます。**書いていないあいだは continuo が持つ既定値が使われます。

**放っておいても壊れません。**ただし、**その設定項目があること自体に気づく手段が、この行しかありません。**
リリースノートは1回きりで、あとから読み返す場所がないからです。

**直し方。**この行の下に、書かれていない項目の名前が並びます（10件を超えたぶんは
「ほか N 件」にまとめます）。**足す差分は次のコマンドで出します。**

```bash
cd ~/continuo-work && continuo doctor --missing-keys-patch WORKFLOW.md
```

**差分を `continuo doctor` の中に出さないのは、長いからです。**版を1つ上げて増えた3項目でも
30行あり、検査結果の並びが画面の外へ押し出されます。

**そのまま当てるなら、`patch` へ渡します。**当てる前に、上のコマンドで差分を読んでください。

```bash
cd ~/continuo-work && continuo doctor --missing-keys-patch WORKFLOW.md | patch -p0 WORKFLOW.md
```

**最後の `WORKFLOW.md` を落とさないでください。**Linux の `patch`（GNU patch）は、
差分の `---` の行が絶対パスだと**「いまいるディレクトリの外を書き換えようとしている」と見なして
差分を捨てます**（`Ignoring potentially dangerous file name` が出て、1行も変わりません）。
**当てる相手をこうして名指しすれば、Linux でも macOS でも当たります。**

**当てても、あなたが書いた行は1つも消えません。**足すだけの差分です。
**並び順を変えていても当たります。**差分は雛形ではなく、あなたの `WORKFLOW.md` から組み立てています。

**要らない項目でも出続けます。**「要らないから書いていない」と「知らないから書いていない」は
機械には見分けられないので、黙らせる手段はありません。**書けば出なくなります。**

**起動は止まりません。**`!` なので終了コードも 0 のままです。

### `! 対応表のキー  tracker.automated_state_rewrite のキーに、ボードの Status の選択肢に無いものがあります`

**原因。**書き戻しの対応表（`tracker.automated_state_rewrite`）のキーに書いた Status が、
ボードの選択肢にありません。**キーはボードの自動化が書く Status 名なので、
ボードにその選択肢が無ければ、その行は一度も引かれません。**

**よくある形は2つです。**

| 形 | どうするか |
| --- | --- |
| **綴りを打ち間違えた**（`Todo` を `To Do` と書いた） | キーの綴りを、ボードの選択肢名に合わせる |
| **その Status をボードで使わなくなった** | 対応表からその行を消す |

```yaml
tracker:
  automated_state_rewrite:
    "Todo": "In Progress"   # 左がボードの選択肢名と1文字ずつ合っているか
```

**大文字小文字と前後の空白は無視して照合します。**`todo` と書いても `!` にはなりません。

**起動は止まりません。**`!` なので終了コードも 0 のままです。
**ボードの自動化をやめて選択肢を消した人が、起動できなくなってはならないからです**
（この検査で起動を止めると、抜け出す道が無くなります）。
**同じ内容の警告が、起動したときにもログへ1行出ます。**

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

### 「作業の途中で確認の画面に止まりました」と出る

**まず見る場所。**issue に付いた引き渡しの通知の【調べるところ】に、記録のパスが並んでいます。
**親のセッションの記録と、サブエージェントの記録の両方を開いてください。**
**親の記録の末尾には何も残っていないことがあります。**作業がサブエージェントの側で進んでいた場合、
そこで何が起きたかは親の記録には書かれません。

```bash
tail -n 20 ~/.claude/projects/<符号化した worktree>/<セッション UUID>.jsonl
ls -lt ~/.claude/projects/<符号化した worktree>/<セッション UUID>/subagents/
```

**「走行中のサブエージェントを止めました」と書いてあったら、worktree を先に見てください。**
continuo は esc を送る前に、走っているサブエージェントが終わるのを少し待ちます
（`claude.poll_wait_ms` のあいだ）。**それでも終わらなければ、走ったまま止めます。**
**そのときは、書きかけの変更が worktree に残っている可能性があります。**

```bash
git -C <worktree のパス> status
git -C <worktree のパス> diff
```

**待っても確認の画面は消えません。**待つのは「別のサブエージェントが書き終えるのを待つ」ためであって、
**待てば作業が再開する、という意味ではありません。**この issue は人間が見ないと進みません。

**`claude.permissions.allow` に足しても直らない止まり方があります。**
continuo は Claude Code を `--permission-mode dontAsk` で起動します。
**このモードでは、許可の一覧に無いツールは確認の画面を出さずに、その場で拒否されます。**
拒否は静かに起きるので、**確認の画面が出て止まったのなら、それは許可の一覧の不足とは別の原因です。**

**直し方。**記録を読んで、何をしようとして止まったのかを確かめます。
**許してよい操作だと分かったときだけ** `WORKFLOW.md` の `claude.permissions.allow` に足し、
Status を着手待ちへ戻してください。

### エージェントが叩いたコマンドが「危ない」と断られる

**原因。**v0.1.10 から、**公開リポジトリの issue では、`Bash` の呼び出しを実行の前に検査します**
（`claude.tool_gate`。**書いていなければ既定で有効です**）。
公開の issue とコメントは誰でも書けるので、**外部の人が書いた文がそのままコマンドになる経路**を狭めるためのものです。
**非公開リポジトリの issue には、既定では掛かりません。**

**まず知っておくこと。****断られても turn は止まりません。**
断った理由がコマンドのエラーとしてエージェントへ返り、エージェントは別のやり方を試します。
**1回断られただけなら、何もしなくて構いません。**

**判定が載っているかを見る。**場所は、その issue の worktree の身元ファイル（`.continuo.json`）が持っています。

```bash
cd <その issue の worktree>
grep -c '"type": "prompt"' "$(jq -r .settings_path .continuo.json)"
```

`1` なら載っています。`0` なら、この節の話ではありません。

**同じコマンドが繰り返し断られて進まないとき。**

| どうするか | 書く行 | 引き換えに失うもの |
| --- | --- | --- |
| **そのままにする** | なし | ありません（断ってほしい操作なら、守れています） |
| **判定を止める** | `claude.tool_gate.mode: off` | **外部の人が書いた指示を止める仕掛けが1つ減ります** |

```yaml
claude:
  tool_gate:
    mode: off
```

**書き換えたら continuo を再起動してください。**動いている最中は設定を読み直しません。

**設定の意味と、判定に回す道具の増やし方は [upgrading.md](upgrading.md) にあります。**

### エージェントが、外部の人の書いたコメントを命令として実行してしまう

**原因。**`WORKFLOW.md` の本文が v0.1.9 のままだと、**エージェントは「誰が書いたか」を読めません。**

**v0.1.9 の本文には、書いた人の立場を読ませる指示が1文字も入っていません。**
「## 書いた人によって扱いを変えること」の節そのものがありません。
**この節は v0.1.10 で入りました。**足りないのはキーの名前ではなく、節そのものです。

**確かめ方。**節があるかと、`--jq` が `author_association` を出す本数を数えます。

```bash
grep -c '^## 書いた人によって扱いを変えること' ~/continuo-work/WORKFLOW.md
grep -c 'author_association: \.author_association' ~/continuo-work/WORKFLOW.md
```

| 1本目 | 2本目 | どう読むか |
| --- | --- | --- |
| `1` | `4` | **この節の話ではありません。**下の「他に見るところ」を読んでください |
| `0` | `0` | **本文が v0.1.9 のままです。**[upgrading.md](upgrading.md) の「差し替え方（書いた人の立場）」のとおりに、32行を消して110行を貼ってください |
| 上のどちらでもない | | **貼り方が途中で切れています。**同じく「差し替え方（書いた人の立場）」の消す範囲から取り直してください |

**1本目が `0` なら、キーの名前を直しても届きません。**節そのものが無いので、
**「`author_association` を見て扱いを変えろ」という指示がどこにも書かれていません。**

**直したら continuo を再起動してください。**動いている最中は `WORKFLOW.md` を読み直しません。
**すでに動いている issue には届きません。**その issue は Status を着手待ちへ戻してやり直させてください。

**他に見るところ。**立場の確認は仕掛けの1つでしかありません。

- **`gh issue view --comments` の表示を読ませていないか。**この表示は、外部の人が本文に
  `author:` `association:` の行を書き足せます。**本文は `--json comments` を使わせています**
- **`claude.tool_gate` を `off` にしていないか。**上の「エージェントが叩いたコマンドが「危ない」と断られる」を読んでください

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

### 人間は何も触っていないのに Status が変わり、issue が止まった

**原因。****ボードの組み込みの自動化です。**GitHub Projects で新しく作ったボードは、
`Item added to project` / `Pull request linked to issue` / `Pull request merged` が有効な状態で作られます。
**エージェントが PR を作って issue に紐づけた瞬間に、ボードが Status を書き換えます。**
continuo は自分の知らない Status になった issue を、猶予（`tracker.unknown_state_grace_ms`、既定10分）のあと止めます。

**見分け方。**issue のコメントに **【この Status を書いたのは人間ではありません】** の行があれば、これです。

```bash
gh issue view https://github.com/<owner>/<repo>/issues/42 --comments
```

**直し方は2つ。どちらか一方でかまいません。**

| どうするか | 何をするか |
| --- | --- |
| **書き戻させる** | `WORKFLOW.md` に対応表を書く。自動化が書いた Status を、本来の Status へ戻させる |
| **自動化を止める** | ボードの `Workflows` から、その自動化を無効にする |

**対応表の書き方は [upgrading.md](upgrading.md) の「足す場所と中身」が正です。**
そのまま貼れる yaml・左と右の決め方・書き戻しの上限・確かめ方が、そこに1箇所だけあります。
**この文書には写しを置きません**（2箇所にあると、片方だけ直したときに食い違います）。

**左に何を書けばよいか分からないときは、書かなくて構いません。**
次に自動化が Status を動かしたとき、continuo が issue のコメントに
**「この2行を足してください」とそのまま貼れる形で書きます。**

**書いたら continuo を再起動してください。**動いている最中は設定を読み直しません。
**キーの綴りは `continuo doctor` の `対応表のキー` が照合します。**

**仕組み**（誰が Status を動かしたかの見分け方と、戻す先を決める道筋）**は
[agent_life_cycle.md](agent_life_cycle.md) の「自動化に Status を横取りされたとき」にあります。**

### 片付ける Status へ動かしたら issue が止まり、案内どおりに直したら起動しなくなった

**原因。**止まった Status の名前が `cleanup.on_states`（worktree の片付けを始める Status）にあり、
**`tracker.terminal_states` には無い**という組み合わせです。
continuo が知っているのは `tracker` に書いた Status だけなので、その名前は「知らない Status」になります。

**`tracker.active_states` へ書き足してはいけません。**次のエラーで起動しなくなります。

```text
設定キー cleanup.on_states の値 Archived が不正です:
  tracker.active_states と同じ値を含めないこと（作業中の worktree を片付けてしまう。3-9）
```

**直し方。**その Status に持たせたい意味で選びます。**どちらもそのまま書いて起動します。**

| その Status の意味 | どう直すか |
| --- | --- |
| **終わったとみなす**（片付けてよい） | `tracker.terminal_states` にその名前を書き足す |
| **まだ作業を続けさせたい** | **先に `cleanup.on_states` からその行を消してから**、`tracker.active_states` に書き足す |

```yaml
tracker:
  terminal_states: ["Done", "Archived"]   # 終わったとみなす Status に並べる
cleanup:
  on_states: ["Done", "Archived"]         # 片付けを始める Status は、上の一覧の中から選ぶ
```

**その名前が `tracker.automated_state_rewrite` のキーにもある場合は、消す先が2つあります。**
**まず対応表のその行を消してください。**残したまま `tracker` の他のキーへ書き足すと、
「キーは設定の他のどこにも名前が出てこない Status にすること」で落ちます。
そのうえで、上の表のどちらかへ進みます（作業を続けさせたい場合は、`cleanup.on_states` からも消します）。

**その worktree が残るかどうかは `cleanup.enabled` で決まります。**
**止めた理由のコメントに、その設定でどうなるかが書いてあります。**

| `cleanup.enabled` | 止めたあとの worktree |
| --- | --- |
| **`true`**（既定） | **残りません。**continuo が worktree と branch を片付けます |
| **`false`** | **残ります。**片付けそのものを行いません |

**片付ける設定でも、次のものが残っていれば片付けを見送ります。**
どちらも既定は `true` で、`false` にすると見なくなります（見ないので、残っていても片付きます）。

| 設定キー | 残っていれば片付けない |
| --- | --- |
| `cleanup.require_clean_worktree` | コミットしていない変更（未追跡のファイルを含む） |
| `cleanup.require_pushed` | push していない commit |

**成果を残したいなら、片付く前に Status を戻すか、`cleanup.on_states` からその行を消してください。**

**`continuo doctor` の `片付けの状態` が `!` なら、この形になっています。**
**書き換えたら continuo を再起動してください。**動いている最中は設定を読み直しません。

### issue が `In Review` にならない

**原因。**エージェントが `CONTINUO-STATUS: review` を出していません。
**continuo が信じるのはカンバンの Status だけです。**エージェントが「終わった」と言っても、Status が動いていなければ終わっていません。

**直し方。**pane で応答を読み、`WORKFLOW.md` の下半分（1回目のプロンプト）に依頼が入っているかを確かめます。

```bash
herdr agent read continuo-hello-world-42 --source recent-unwrapped --lines 40
grep -n "CONTINUO-STATUS" ~/continuo-work/WORKFLOW.md
```

**書き換えたら continuo を再起動してください。**動いている最中は読み直しません。

### issue にコメントが残っているのに「この run のコメントが無い」と言われる

**原因。**continuo は**印だけでは「エージェントが書いた」と認めません。**
`<!-- continuo:agent -->` は本文の先頭に置くただの文字列で、issue にコメントできる人なら誰でも書けます。
そこで**投稿者が「continuo が使う `gh` の持ち主」と一致するか**も見ています。
一致しないコメントを見つけたときは、こう出ます。

```text
level=WARN msg="コメントに印は付いていますが、投稿者が gh の持ち主と違います（エージェントが書いたものとして数えません）" 投稿者=outsider gh の持ち主=octocat url=…
```

**直し方。**まず、いま continuo が誰として GitHub を触っているかを確かめます。

```bash
gh api user --jq .login
```

**この名前とコメントの投稿者が同じになるようにします。**別のアカウントへ切り替わっていたなら
`gh auth switch` で戻し、**continuo を再起動してください**（一度取れた持ち主は取り直さないので、切り替えても古い名前のままです）。
**投稿者が身に覚えのない名前なら、その issue に第三者が印を騙って書いています。**そのコメントを消してください。

**投稿者が continuo 自身の名前なら、`gh` とボードのトークンが別のアカウントです。**
`WORKFLOW.md` の `tracker.provider.token_source` が `env` で、`token_env` に指定した環境変数のトークンが
`gh` のログインとは別のアカウントのときに起きます。continuo が書いた通知そのものが「第三者の投稿」と読まれ、
次の turn の入力にも混ざります。**`token_source: gh_auth`（既定）に戻すか、`gh` と同じアカウントのトークンを渡してください。**

**持ち主そのものを取れないときは、こう出ます。**そのあいだは印だけで判定するので、
上の騙りを見分けられません。`gh` が使える状態に戻してください。

```text
level=WARN msg="gh の持ち主を取れません（コメントの印だけで判定します。…）" 連続して失敗した回数=3
```

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

### 複数の機械で見張っているのに、いつも同じ機械しか issue を取らない

**原因。**同じボードを複数の機械で見張る「持ち回り」は、担当者のいない issue に対して
**枠にいちばん余裕がある1台だけが担当者になる**仕組みです（勝者総取り）。
同じ機械が勝ち続けるのは、その機械の判定スコア（`5時間余裕値 × 2 + 1週間余裕値`）が
ほかの機械よりずっと大きい状態が続いているからです。**あまり動かしていない機械ほど勝ちやすくなります。**

**確かめ方。**issue のコメントに、勝敗を決める入札が並びます。

```
<!-- continuo:bid -->
{"host": "mac-studio", "five_hour": 87, "weekly": 16, "score": 190, "at": "2026-08-29T16:45:00+09:00"}
```

**`host` でどちらの機械の入札か、`score` でどちらが勝つ見込みかが分かります。**
このコメントはエージェントへは渡さず、issue を GitHub の画面で開いたときだけ見えます。

**直し方。**次の2つを確認してください。

| 確認すること | 何が起きているか |
| --- | --- |
| **一方の機械だけ `five_hour_margin_percent` / `weekly_margin_percent` が大きい** | マージンが大きい機械ほど余裕値が下がり、判定スコアで負け続けます。両方の機械で値を揃えてください |
| **`bid_window_ms` が機械ごとに違う** | 締め切りの計算がずれ、機械ごとに勝者の判定が食い違うことがあります。両方の機械の `WORKFLOW.md` で同じ値にしてください |

**枠の使用率そのものが偏っているだけなら、これは仕様どおりの動きです。**
暇な機械に処理を集めることが、この仕組みの目的です。

### 複数の機械で見張っているのに、どの機械も issue を取らない

**原因。**入札は次の3つのどれかに当たると、**コメントを1件も書かずに黙って見送ります。**

| 理由 | 何が起きているか |
| --- | --- |
| **枠を読めない** | 直前の枠の読み取りに失敗しています。`rate_limit.source` の資格情報が読めていません |
| **枠の使い過ぎ** | 5時間・1週間のどちらかの枠が `rate_limit.pause_above_percent`（既定95）を超えています |
| **余裕値がマイナス** | `100 − 使用率 − マージン` がマイナスです。`five_hour_margin_percent` / `weekly_margin_percent`（既定10）が大きすぎるか、実際の使用率が高すぎます |

**黙って見送るだけなので、issue にも画面にも何も出ません。**Ready のまま何も起きていないように見えます。

**確かめ方。**まず、continuo のログで「枠の読み取りに失敗しました」を探してください。
**これは既定のログレベル（`info`）でも出ます。**

出ていなければ、`--log-level debug` で起動し直し、「入札しません」の行を探してください。**理由がそこに出ます。**

```bash
cd ~/continuo-work && continuo --log-level debug
```

**直し方。**

| 理由 | 直し方 |
| --- | --- |
| **枠を読めない** | `continuo doctor` の `資格情報` の行を確認してください。`✗` ならその行の案内に従ってください |
| **枠の使い過ぎ** | 待つか、`rate_limit.pause_above_percent` を上げてください。**上げると、枠を使い切る手前まで dispatch も続けます**（この閾値は持ち回りの入札と dispatch の一時停止の両方に効きます） |
| **余裕値がマイナス** | `WORKFLOW.md` の `five_hour_margin_percent` / `weekly_margin_percent` を下げてください |

**`rate_limit.source: none` にしている場合はここに当たりません。**その設定は「枠で判定しない」という
運用者の決定として扱われ、使用率0（＝いちばん暇）として常に入札します。

### 担当が付いたまま、issue がいつまでも進まない

**原因。**担当している機械が落ちています（PC の電源を落とした・枠を使い切った・セッションが切れた）。
**他の機械は、担当している機械の最後のコメントから `idle_timeout_ms`（既定18時間）が経つまで、
その issue に触りません。**hold のコメントがあることが「担当は機械である」の証拠なので、
この間は入札もされません。

**確かめ方。**issue のコメントの `<!-- continuo:hold -->` を見てください。
**`host` にどの機械の担当かが、`at` にいつ書かれた hold かが入っています。**

```
<!-- continuo:hold -->
{"host":"mac-studio","assignee":"octocat-bot-a","branch":"continuo/octocat/hello-world/188","at":"2026-08-29T18:45:00+09:00"}
```

**直し方。**

| 状況 | どうするか |
| --- | --- |
| **他の機械が動いている** | **18時間待てば自動で引き継がれます。**担当が外れると `<!-- continuo:released -->` のコメントが付き、入札をやり直します |
| **早く引き継ぎたい** | GitHub の画面から、その issue の assignee を外してください。**次の巡回で「担当者が無い」と判定され、入札からやり直します** |
| **動いている機械がこれ1台しかない** | **自動では引き継がれません。**その機械を再起動してください。同じ worktree の続きから再開します |
| **待つ時間そのものを変えたい** | `WORKFLOW.md` の `tracker.provider.handoff.idle_timeout_ms` を短くしてください（単位はミリ秒） |

**push していない変更は、担当が移った時点で失われます。**生きている機械は、進捗のコメントと一緒に push してください。

**外れたはずの機械が動き続けているように見えるとき。**担当を外された機械自身は、
**`recheck_interval_ms`（既定1時間）ごとに担当を確かめ直しており**、他の機械が入札に勝って
担当者を書き換えた時点で、そのターンの終わりに気づいて止まります。**Status もコメントも書かず、
`workspace_hooks.after_run` も走らせません。**それより早く止めたい場合は、この値を短くしてください。

### 持ち回りを使わずに、1台だけで動かしたい

**原因。**持ち回りの仕組みを丸ごと止める設定はありません。**1台だけで動かしていても、この仕組みは常に効きます。**
担当者のいない issue には必ず入札のコメントを1件書き、締め切り（既定3分）を待ってから自分を担当者にします。

**1台構成でも issue に何が付くか。**

| コメント | いつ付くか |
| --- | --- |
| `<!-- continuo:bid -->` | 担当者のいない issue を見つけるたび |
| `<!-- continuo:hold -->` | 締め切りを過ぎて、自分を担当者にしたとき |

**どちらもエージェントへは渡りません。**issue を GitHub の画面で開いたときだけ見えます。**動作に支障はありません。**

**待ち時間だけ無くしたい場合。**`bid_window_ms` を `0` にしてください。
**締め切りを待たずに、同じ巡回で自分を担当者にします**（コメント自体は書かれたままです）。

```yaml
tracker:
  provider:
    handoff:
      bid_window_ms: 0
```

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

### `continuo abandon` が、動いている continuo の worktree を消そうとする

**原因。****`--id` の付け忘れです。**`--id e2e` で動かした continuo の worktree を、
`--id` なしの `continuo abandon` で片付けようとすると、**abandon は既定の1本を見ます。**
そこにロックは無いので「continuo は動いていない」と判定し、
**`--id e2e` の continuo がいま使っている worktree を、止まっている残骸として消しに行きます。**

**直し方。****起動に渡した名前を、そのまま `abandon` にも渡してください。**

```bash
continuo abandon --id e2e https://github.com/octocat/hello-world/issues/42 ~/continuo-e2e-work --dry-run
continuo abandon --id e2e https://github.com/octocat/hello-world/issues/42 ~/continuo-e2e-work
```

**`--dry-run` を先に叩いてください。**消えるものが一覧で出ます。
**`--id` を間違えていると、branch 名の先頭（`e2e/`）と worktree の置き場所が食い違うので、
そこで気づけます。**

**`runtime.lock_file` で分けようとしないでください。**この設定はもう読みません
（「起動できないとき」の「`runtime.lock_file` にパスを書いたのに…」を見てください）。
**この事故が起きうるから読まなくしました。**

### `continuo abandon` が返ってこない（「pane が閉じるのを待っています」のまま止まって見える）

**原因。****待っているだけです。**continuo が動いていて、その issue が作業中の Status なら、
`abandon` はまず**手を離させてから** pane が閉じるのを待ちます。
**上限は `herdr.read_timeout_ms` の10倍**です（既定は 5000 ミリ秒なので50秒）。

**herdr が答えないときも、その場では止まりません。**
「herdr へ pane の一覧を問い合わせられませんでした（…）。上限までは待ち直します。」を1行だけ出し、
**上限まで待ってから**「pane ごと消してよいなら `--force` を付けてください」と言って止まります。

**叩き直したときにまた待つかどうかは、そのときの Status で決まります。**
**待つのは、手を離させる書き込みを実際に行ったときだけです。**

| 叩き直したときの Status | 2回目の待ち時間 |
| --- | --- |
| **`tracker.active_states` の外**（1回目が動かした `Blocked` のまま） | **待ちません**（手を離させる段を通らないので0秒） |
| **`tracker.active_states` の中**（そのあいだに誰かがボードで戻した） | **もう一度上限まで待ちます**（合わせて上限の2回ぶん） |

**ふつうは上の行になります。**1回目が Status を `tracker.failure_state`（既定は `Blocked`）へ
動かしているので、2回目はこう言ってすぐ先へ進みます。

```text
Status は "Blocked" で、tracker.active_states に入っていないので動かしません（continuo はもうこの issue を持っていません）。
```

**直し方。**herdr が動くなら、直してから叩くのがいちばん速いです。

```bash
herdr pane list
continuo abandon https://github.com/<owner>/<repo>/issues/42 ~/continuo-work
```

**herdr が戻らないまま消し切ってよいなら `--force` を付けます。**
pane の生死を確かめずに、worktree・branch・herdr の workspace を消します。

**`--dry-run` は待ちません。**手を離させる段を通らないので、その場で調べて終わります。

**待ち終えても pane が残っていた場合は、何も消さずに止まります。**
`--force` を付けない限り、勝手に pane ごと消すことはありません。

### `Blocked` になった issue の worktree に、push していない作業が残っている

**原因。**v0.1.9 までの `WORKFLOW.md` は、`review` の前にしか push を求めていませんでした。
**`blocked` を出したエージェントは、push せずに手を離します。**

**そうなるとどうなるか。****その issue は片付きません。**
`cleanup.require_clean_worktree` と `cleanup.require_pushed` が既定で `true` なので、
continuo も `continuo abandon` も「失うものがあるので何も消しません」で止まります。
**勝手に消されることはありませんが、あなたが手で始末するまで worktree が残り続けます。**

**直し方は2つあり、両方やってください。**

**1. 残っている作業を救い出す。**worktree に入って push します。

```bash
grep -n -A2 '^workspace:' ~/continuo-work/WORKFLOW.md   # root: が worktree の置き場所
cd <その issue の worktree>
git status
git add -A && git commit -m "<何をしたか>"
git push -u origin HEAD
```

**`git push -u origin HEAD` で足ります。**worktree はその issue のために作られた branch に
乗っているので、branch 名を自分で調べる必要はありません。

**2. 同じことが起きないように、`WORKFLOW.md` の本文を当てる。**
差し替える文面は [upgrading.md](upgrading.md) の「v0.1.9 から v0.1.10 へ」にあります。
**当てたら continuo を再起動してください。**動いている最中は `WORKFLOW.md` を読み直しません。

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
| `continuo doctor [ディレクトリ]` | 前提が揃っているかを見出し語ごとに調べる。`--id` で名前ごとの場所を見る |
| `continuo abandon <URL> [ディレクトリ]` | 間違えて着手した issue を着手前へ戻す |
| `continuo allow-keychain-access` | macOS だけ。枠を読むために1回 |
| `continuo` | 常駐を始める。`--port` でダッシュボード、`--log-level` |

`continuo hook` は Claude Code の hook から呼ばれるもので、人間が直接叩くものではありません。

### 1台で continuo を2つ動かしたい（本番を止めずに、検証用をもう1本立てたい）

**やること。****2本目に `--id <名前>` を付けます。**設定は1行も書き換えません。

```bash
continuo ~/continuo-work                   # 1本目（いま動いているもの）。そのままでよい
continuo --id e2e ~/continuo-e2e-work      # 2本目
```

**`--id` は、分けるべきものを5つまとめて分けます。**

| 分ける対象 | `--id e2e` を付けたとき |
| --- | --- |
| **ロック** | `~/.continuo/id/e2e/continuo.lock` |
| **socket と実行時ディレクトリ** | `~/.continuo/id/e2e/run/` |
| **worktree の置き場所** | `<workspace.root>/e2e` |
| **branch 名** | `e2e/` を先頭に付けたもの |
| **herdr の agent 名** | `continuo-e2e-<repo>-<番号>` |

**設定や環境変数では分かれません。**`runtime.lock_file` は読みません。
`CONTINUO_RUNTIME_DIR` / `XDG_RUNTIME_DIR` / `TMPDIR` を変えても、ロックは1本のままです。
**分ける手段は `--id` だけです。**

**2本目には別のボードを見せてください。**同じボードを2つの continuo が見ると同じ issue を
2つが拾うので、**2つ目はボードのロックで起動を止められます**（上の「同じボード…」を見てください）。

**`continuo doctor` と `continuo abandon` にも同じ名前を渡してください。**
渡さないと既定の1本を見に行き、`--id` で作った worktree も branch も見つけられません。
**`abandon` は、渡し忘れてもボードのロックで止まります。**

**孤児 branch の掃除は、`--id` を付けただけでは始まりません。**
`herdr.worktree.branch_template` が `{{` で始まっている設定では、掃除は元から止まっています
（接頭辞を決められないためです）。**`--id e2e` を足しても `e2e/` を接頭辞として使いません。**
使うと、あなたが自分で切った `e2e/…` の branch を消してしまうからです。

### フラグを位置引数の後ろに書いてもいい？

**原因。**Go の flag パッケージは、既定では位置引数より後ろのフラグを読みません。

**直し方。****continuo は前でも後ろでも受け付けます。**次の2つは同じ意味です。

```bash
continuo trust ~/continuo-work --dry-run
continuo trust --dry-run ~/continuo-work
```

`--` より後ろは、`-` で始まっていても位置引数として扱います。
知らないフラグは、どこに書いてもエラーのままです（終了コード 2）。

### 画面に出る文言の言葉を変えたい（英語にしたい・日本語で固定したい）

**書く場所。**`WORKFLOW.md` の `language:` です。**設定は `LANG` より強いので、書けば必ずその言語が選ばれます。**

```yaml
language: ja
```

| `language:` の値 | 画面に出る文言 |
| --- | --- |
| `ja` | 日本語 |
| `en` | 英語 |
| `auto`（雛形の値）または未記入 | 環境変数 `LANG` から決める。決まらなければ**英語** |

**`ja` と書いておく意味。**書かずにいると、`LANG` を持たない環境（CI・コンテナ・`env -i`）では
英語になります。**日本語で使い続けたいなら書いてください。**

**`continuo init` は、英語を選べば全部英語で出ます。**
**`continuo doctor` も、下の表の後ろ2行の場合を除いて英語で出ます。**

**まだ日本語のまま出るところが5つあります。**

| どこ | いつ出るか |
| --- | --- |
| `continuo trust` の出力 | いつでも |
| ログ | いつでも |
| `continuo init` が書き出す `WORKFLOW.md` の中の説明 | いつでも。**画面ではなくファイルの中です。**設定のキーと値そのものは英語のままなので、読めなくても起動します |
| `continuo doctor` の `config` の行 | 設定に不正な値を書いたとき。**理由の部分だけが日本語です**（例: `0より大きい整数にすること`）。**値を埋め忘れただけのときは英語で出ます** |
| `continuo doctor` の `board` の行 | ボードを読めなかったとき。**GitHub と話す層が返すエラーの本文だけが日本語です**（例: `tracker エラー [tracker_response]: …`） |

**どれも訳の対象からまだ外れています。**
**日本語のまま出るところを見つけたときの直し方は
[CONTRIBUTING.md](../CONTRIBUTING.md) にあります。**

**`LANG` を見るのは `auto` と未記入のときだけです。**読むのは `LANG` だけで、
`LC_ALL` と `LC_MESSAGES` は読みません（どれが効いたのかを説明できなくなるためです）。

**資源の無い言語（`fr` など）を書くと、起動せずに止まります。**黙って別の言語で動くと、
書いたつもりの設定が効いていないことに、無人で動かしているあいだ誰も気づけないからです。

**書き換えたら continuo を再起動してください。**動いている最中は設定を読み直しません。

**選ばれている言語は `continuo doctor` の出力で分かります。**見出し語が
`設定ファイル` なら日本語、`config` なら英語です。

```bash
cd ~/continuo-work && continuo doctor
```

### continuo を新しい版に入れ替えた。`WORKFLOW.md` は作り直したほうがいい？

**原因。**v0.x のうちは設定のキーが増減しうるので、作り直しが要るのかどうかが判断しづらい。

**直し方（v0.1.10 に上げた場合）。****作り直しは要りません。v0.1.9 の `WORKFLOW.md` がそのまま通ります。**
**ただし、当てるものが3つあります。**

| 何 | v0.1.10 では |
| --- | --- |
| **消えたキー** | **ありません** |
| **名前が変わったキー** | **front matter にはありません**（変わったのは本文です） |
| **増えたキー** | `claude.tool_gate` の1つだけ。**省略できます** |
| **本文（プロンプト）その1** | **`blocked` を出す前にも push させる指示が入りました。**当てないと、エージェントは人間へ渡す前に push しません |
| **本文（プロンプト）その2** | **「## 書いた人によって扱いを変えること」の節が入りました。**v0.1.9 にはこの節がありません。当てないと、エージェントは立場を読めず、**外部の人が書いたコメントを命令として扱いえます** |

**設定と本文は、当てなかったときに起きることが違います。**

| どこ | 当てないとどうなるか |
| --- | --- |
| **front matter**（先頭の `---` に挟まれた YAML） | **壊れません。**ただし `claude.tool_gate` は**省略すると既定が効きます。**公開リポジトリの issue で、エージェントが `Bash` を叩くたびに、その中身が危なくないかの検査が1回入ります。**元に戻す1行は [upgrading.md](upgrading.md) にあります** |
| **本文**（front matter より下） | **エージェントの動きが古いままです。**continuo は本文を読み替えないので、書いていない指示は届きません |

**`continuo init --force` で作り直さないでください。**`continuo setup` で決めた Status の割り当てが雛形で潰れます。
**増えた設定も、変わった本文も、その部分だけを手で当てます。**

**足す場所と中身、当てないと何が起きるか、当てたあとの確かめ方は
[upgrading.md](upgrading.md) にあります。**版ごとにそこへ積み上げます。

**上げたあとは `continuo doctor` を1回叩いてください。**
**増えた設定項目は `未記入の項目` の行に、名前で出ます。**
**足す差分は `continuo doctor --missing-keys-patch WORKFLOW.md` で出します。**
`片付けの状態` と `対応表のキー` と `未記入の項目` は `!` を出すことがありますが、
**`!` だけなら起動します**（終了コードも 0 です）。

```bash
cd ~/continuo-work && continuo doctor; echo "exit=$?"
```

**`continuo doctor` は本文を検査しません。**本文が当たっているかは `grep` で見ます。**3本あります。**

```bash
grep -c 'blocked` を出す前に' ~/continuo-work/WORKFLOW.md
grep -c '^## 書いた人によって扱いを変えること' ~/continuo-work/WORKFLOW.md
grep -c 'author_association: \.author_association' ~/continuo-work/WORKFLOW.md
```

**上から `1` `1` `4` なら、3つとも当たっています。**
そうでなければ [upgrading.md](upgrading.md) の「v0.1.9 から v0.1.10 へ」を見てください。
**既に `blocked` で止まっている issue があるなら、**「片付けたいとき」の
**`Blocked` になった issue の worktree に、push していない作業が残っている** も見てください。

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
