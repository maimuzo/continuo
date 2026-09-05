# よくある質問

画面に出たメッセージから引ける一覧です。使い方は [README.ja.md](../README.ja.md) と
[trying_it_out.md](trying_it_out.md) にあります。
**新しい版に上げたあと何を足せばよいかは [upgrading.md](upgrading.md) にあります。**

困ったら、まず `continuo doctor` を叩いてください。設定ファイル / 片付けの状態 /
未記入の項目 / プロンプトの変数 / claude / hook の置き場所 / Claude の設定 /
worktree の場所 / herdr / gh の認証 / カンバン / Status の名前 / 対応表のキー / clone /
信頼登録 / 資格情報の16個を調べます。
`✗` が1つでもあれば終了コードは 1、`!` だけなら 0 です。

```bash
cd ~/continuo-work && continuo doctor
```

---

## 起動できないとき

### 版を上げたら「progress_interval_ms の値 3600000 が不正です」で起動しなくなった

**原因。**v0.1.14 で
`tracker.provider.handoff.progress_interval_ms`（エージェントに進捗報告を書かせる間隔。ミリ秒。既定 3600000 = 1時間）が増えました。
**この値は `tracker.provider.handoff.idle_timeout_ms`（担当者の最後の進捗報告からこれだけ経つと担当を外して入札をやり直す間隔。ミリ秒。既定 64800000 = 18時間）より短くなければなりません。**

**`idle_timeout_ms` を1時間以下へ下げている人だけが当たります。**
**そのうち 1 から 60000 までを書いている人は、`progress_interval_ms` を足しても起動しません**（下の直し方）。
**新しいキーを1文字も書いていなくても、既定値のほうがあなたの値より長くなります。**

```
front matter が不正です: 設定キー tracker.provider.handoff.progress_interval_ms の値 3600000 が不正です: tracker.provider.handoff.idle_timeout_ms（1800000 ミリ秒）より短くすること。長いと、エージェントが指示どおりに書いていても書く前に担当が外れる
```

**なぜ求めるか。**間隔のほうが長いと、**エージェントが指示どおりに書いていても、書く前に担当が外れます。**
そのとき別の機械が入札をやり直し、**push していない変更が失われます。**

**直し方。**`progress_interval_ms` を、`idle_timeout_ms` より短い値にします。
**60000（1分）以上にしてください。**59999 までは、また起動が止まります。
**送る文面へは分に直して埋めるので、59999 までは全部「0分以上黙らない」になるためです。**

**`idle_timeout_ms` に 1 から 60000 までを書いている人は、書ける値がありません。**
`idle_timeout_ms` のほうを 60000 より大きくしてください。
**`0` はこれに当たりません。**0 は「既定の18時間を使う」という意味なので、そのまま起動します。

```yaml
tracker:
  provider:
    handoff:
      idle_timeout_ms: 1800000        # 30分（あなたが書いていた値）
      progress_interval_ms: 600000    # 10分（これを足す）
```

**`continuo doctor` の `設定ファイル` が `✓` になれば直っています**（画面の言語が英語なら `config`）。

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

**原因。**別の continuo が動いています。
**ロックは `~/.continuo/continuo.lock` の1本に固定されています。**
環境変数でも設定でも動かないので、**launchd から起動した continuo と端末で叩いたコマンドが
別の場所を見ることはありません。**

**直し方。**動いている continuo を止めます。

```bash
pgrep -fl continuo
```

**わざと2本動かしたいときは、`--id <名前>` を付けます。**
開発中に、本番を止めずにテスト版を動かすためのものです。

```bash
continuo --id e2e ~/continuo-e2e-work    # ロックは ~/.continuo/id/e2e/continuo.lock
```

**`--id` が分けるのはロック1本だけです。**worktree の置き場所（`workspace.root`）と
socket（`claude.hook_bridge.listen`）は分かれないので、**テスト用の `WORKFLOW.md` を
別のディレクトリに置いて、そちらで書き換えてください。**分けないと、2本が同じ worktree に
Claude Code を二重に立て、**片方の成果が黙って消えます。**

**`continuo abandon` にも同じ名前を渡してください。**渡さないと、空いている既定のロックを見て
「continuo は動いていません」と判定し、**生きている worktree を消しにいきます。**

```bash
continuo abandon --id e2e <issue の URL> ~/continuo-e2e-work
```

### 「front matter が不正です: unknown field "runtime"」で起動できなくなった

**原因。**`runtime` の節（`runtime.lock_file`）を**消しました。**
ロックは `~/.continuo/continuo.lock` に固定してあり、設定では動きません。
**設定で変えられると、`continuo abandon` が別の場所を見て「動いていない」と判定し、
走っている worktree を消しにいくためです。**

**読まない値を受け取り続ける形は採りませんでした。**
書いてあるのに効かない項目を残すと、**次に読む人が「効いている」と思って設定します。**

**直し方。`WORKFLOW.md` から `runtime:` の2行を消してください。**それだけです。

```bash
grep -n -A1 '^runtime:' ~/continuo-work/WORKFLOW.md   # 消す行を確かめる
```

```yaml
runtime:
  lock_file: null    # ← この2行を消す
```

**1台で2本以上動かしたいなら `--id <名前>` を使ってください。**

### 「front matter が不正です: unknown field "…"」で止まる

**原因。**continuo を更新して設定のキーが増減しました。front matter は未知のキーを弾きます。

**直し方。**出たキーの行を `WORKFLOW.md` から消してください。
**`continuo init --force` は使わないこと。**`continuo setup` で決めた Status の割り当てが雛形で潰れ、
**手で書いた本文も雛形に戻ります**（`--force` は front matter も本文も上書きします）。

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

### `✗ カンバン  Status の選択肢名が設定と一致しません`

**原因。**GitHub の既定の Status は `Todo` / `In Progress` / `Done` の3つだけです。
continuo は5つの役割それぞれに別の選択肢を使います。
GraphQL はエラーを出さずに0件を返し続けるので、起動時の検査でここで止めています。

**直し方。****足りない選択肢は GitHub の画面から足します。**
カンバンの `Settings` → 左の `Custom fields` の `Status` → `Options` の `Add option...`。名前は何でも構いません。
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

### `! 対応表のキー  tracker.automated_state_rewrite のキーに、カンバンの Status の選択肢に無いものがあります`

**原因。**書き戻しの対応表（`tracker.automated_state_rewrite`）のキーに書いた Status が、
カンバンの選択肢にありません。**キーはカンバンの自動化が書く Status 名なので、
カンバンにその選択肢が無ければ、その行は一度も引かれません。**

**よくある形は2つです。**

| 形 | どうするか |
| --- | --- |
| **綴りを打ち間違えた**（`Todo` を `To Do` と書いた） | キーの綴りを、カンバンの選択肢名に合わせる |
| **その Status をカンバンで使わなくなった** | 対応表からその行を消す |

```yaml
tracker:
  automated_state_rewrite:
    "Todo": "In Progress"   # 左がカンバンの選択肢名と1文字ずつ合っているか
```

**大文字小文字と前後の空白は無視して照合します。**`todo` と書いても `!` にはなりません。

**起動は止まりません。**`!` なので終了コードも 0 のままです。
**カンバンの自動化をやめて選択肢を消した人が、起動できなくなってはならないからです**
（この検査で起動を止めると、抜け出す道が無くなります）。
**同じ内容の警告が、起動したときにもログへ1行出ます。**

**そもそも何を書けばよいのかから知りたいときは、「使い方が分からないとき」の
「エージェントが PR を作った直後に止まる（automated_state_rewrite）」を見てください。**

### `✗ プロンプトの変数  送るプロンプトに誤りがあります`

**原因。**エージェントへ送る指示書に、一覧に無い変数（`{{.issue.nope}}` など）が書かれているか、
`{{if}}` の閉じ忘れなど、テンプレートとして解釈できない書き方があります。

**この誤りがあると、issue は1件も着手できません。**書けなかった変数を空欄で埋めて送る、
ということはしません（**書いたつもりの指示が黙って消えるほうが危ないためです**）。

**どこが悪いのかは、内訳に出るファイルの名前と行番号で分かります。**

| 内訳に出る名前 | 何のことか |
| --- | --- |
| `WORKFLOW.md#body` | **あなたが書いた本文**（front matter の閉じの `---` より下）。**行番号は、その `---` の次の行を1行目として数えたものです** |
| `builtin.md#head` / `builtin.md#tail` | **continuo の組み込みのプロンプト。**ここが出たら continuo 側の不具合です |

**送る文面の全文は、次のコマンドで読めます。**

```bash
cd ~/continuo-work && continuo prompt --show
```

**使える変数は11個です。**

| 変数 | 中身 |
| --- | --- |
| `{{.issue.identifier}}` | `<owner>/<repo>#<番号>` |
| `{{.issue.owner}}` | リポジトリの所有者名 |
| `{{.issue.repo}}` | リポジトリ名 |
| `{{.issue.number}}` | issue の番号 |
| `{{.issue.url}}` | issue の URL |
| `{{.issue.title}}` | issue の題名 |
| `{{.issue.state}}` | **カンバンの Status の値**（`Ready` など）。GitHub の open / closed ではありません |
| `{{.issue.labels}}` | ラベルの並び |
| `{{.attempt}}` | 試行回数。**1回目は空**なので `{{if .attempt}}` で囲ってください |
| `{{.push_branch}}` | issue にリンクされた branch の名前。**リンクが1本でないときは空** |
| `{{.progress_interval_minutes}}` | 進捗報告を書かせる間隔（分）。`tracker.provider.handoff.progress_interval_ms` から決まる |

**`{{index .issue "title"}}` の形は使えません。**`{{.issue.title}}` と書いてください。
**この形を許すと、綴りを間違えた名前が誤りにならずに素通りします。**

**検査は完全ではありません。**continuo は作り物の issue で2回試すだけなので、
`{{if eq .issue.state "Done"}}` のように**値そのもので分かれる枝の中**までは届きません。

### `WORKFLOW.md` の本文に書いた指示が、組み込みの指示と2回届く

**原因。**v0.1.13 で、`WORKFLOW.md` の本文の意味が変わりました。

| 版 | 本文に中身があるとき、何が送られるか |
| --- | --- |
| **v0.1.12 まで** | **本文だけ。**組み込みのプロンプトは1文字も送られません |
| **v0.1.13 から** | **組み込みの前半 + 本文 + 組み込みの後半。**本文は真ん中へ挟まります |

**v0.1.12 までの本文には、組み込みと同じ指示を書き写している人がいます。**
表明の1行（`CONTINUO-STATUS:`）の書き方や、worktree を切り替えるなという指示です。
**そのまま v0.1.13 へ上げると、同じ指示が2回届きます。**

**直し方。**組み込みに同じことが書いてある部分を、本文から消してください。
**組み込みの全文は、次のコマンドで読めます。**

```bash
cd ~/continuo-work && continuo prompt --show --builtin
```

**送られる文面の全文は、次のコマンドで読めます。**

```bash
cd ~/continuo-work && continuo prompt --show
```

**手順は [upgrading.md](upgrading.md) の「v0.1.12 から v0.1.13 へ」にあります。**

### 進捗報告の間隔を変えたい

**`tracker.provider.handoff.progress_interval_ms`（エージェントに進捗報告を書かせる間隔。ミリ秒。既定 3600000 = 1時間）を変えてください。**

```yaml
tracker:
  provider:
    handoff:
      progress_interval_ms: 1800000   # 30分
```

**送る文面へ、分に直して埋まります。**上の例なら「30分以上コメントを書かないまま作業を続けないでください」になります。

**2つの決まりがあります。破ると起動しません。**

| 決まり | なぜ |
| --- | --- |
| **60000（1分）以上にすること** | **送る文面へは分に直して埋めるので、59999 までは全部「0分以上黙らない」になる** |
| **`idle_timeout_ms`（既定18時間）より短くすること** | 長いと、エージェントが指示どおりに書いていても、書く前に担当が外れる。**`idle_timeout_ms: 0` と書いた場合も、実行時に効く18時間と比べます** |

**continuo はこの値を測りません。**送る文面へ埋めるだけです。**測っているのは `idle_timeout_ms` のほうだけです。**
**つまり、この値を短くしても、担当が外れるまでの18時間は変わりません。**

### `WORKFLOW.md` に書いた決まりが、エージェントに届いていない

**HTML のコメントで書いていませんか。**

**v0.1.14 から、行頭の `<!-- … -->` は送る文面から取り除かれます。**
`continuo init` が置く雛形は、書き方の案内をコメントで書きます。**それはエージェントへ送る情報ではありません。**
**v0.1.13 まではそれも送っていたので、コメントで書いた決まりも届いていました。**

**確かめ方。**

```bash
continuo prompt --show <WORKFLOW.md のあるディレクトリ> | grep '<あなたが書いた決まりの一部>'
```

**何も返らなければ、届いていません。**

**直し方は2つです。**

| どうするか | いつ |
| --- | --- |
| **コメントを外して、ふつうの文として書く** | **エージェントに読ませたい決まりのとき。**こちらを勧めます |
| **バッククォート3つのコードブロックで囲む** | 見た目をコメントのまま残したいとき。**囲んだ中は取り除かれません** |

**取り除かれないものが2つあります。**

    字下げした <!-- … -->        … 4桁の字下げで書いたコード片
    バッククォートで囲んだ中     … ``` で囲んだ中身

**`-->` を打ち忘れた `<!--` は、取り除かれるとは限りません。**
**その断片の残りに `-->` が1つも無いときだけ、コメントとみなさずに残します。**
`continuo init` が置く雛形の本文には案内のコメントが何個も並ぶので、
**1つ打ち忘れると、その `<!--` は次に見つかった `-->` までを見出しごと飲み込みます。**
**画面にはエラーが1つも出ません。**

**節ごと消えることもあります。**見出しの下がコメントだけだった場合、
**コメントを取り除いた時点でその節は見出しだけになるので、見出しごと落とします。**
`continuo init` の直後の `WORKFLOW.md` では、7つの節のうち3つがこの形です
（テストの走らせ方 / pull request の決まり / レビューを頼む subagent）。
**あなたが書き込めば出ます。**

### `continuo prompt --show` の内訳に「WORKFLOW.md に本文はありません」と出る

**原因。**`WORKFLOW.md` の front matter の閉じの `---` より下が、空か空白だけです。

**壊れてはいません。**固有の指示が要らない project では、これが正しい状態です。
**組み込みのプロンプトだけが送られます。**

**書いたつもりなのにこう出るときは、書いた場所を確かめてください。**
**front matter の中（`---` に挟まれた YAML の側）に書いても、本文にはなりません。**

**本文だけを出すコマンドです。**2本目の `---` より下を出します。

```bash
cd ~/continuo-work && awk 'c>=2{print} /^---$/{c++}' WORKFLOW.md
```

**本文の雛形が要るなら、`continuo init` を別のディレクトリで叩いて写してください。**
**いまいるディレクトリで `--force` を打つと、front matter も雛形に戻ります。**

```bash
continuo init /tmp/continuo-template
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

### `continuo setup` が「使うカンバンの番号が決まりませんでした」で止まる

**原因。**カンバンが organization にあるのに、以前の版は個人アカウントのログイン名しか見ていませんでした。
GitHub Enterprise で organization のカンバンを使っていると必ずこうなります。
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

### 着手が「worktree がどの branch にも載っていません（detached HEAD）」で止まる

**原因。**worktree が detached HEAD になっています。**どの branch にも載っていない状態です。**

**上の節（ディレクトリだけが残っている）とは違います。**worktree は git に登録されていて、
**branch に載っていないだけです。**中の作業は残っています。

**よくある原因。**

| 何 | 例 |
| --- | --- |
| **commit を直接チェックアウトした** | `git checkout <SHA>` / `git switch --detach` |
| **rebase の途中** | `git rebase` が止まっている |
| **bisect の途中** | `git bisect` を始めたまま |

**直し方。**

**1. 未コミットの変更があるかを、先に確かめてください。**

```bash
git -C ~/worktrees/github.com/<owner>/<repo>/continuo-<owner>-<repo>-42 status
```

**`rebase in progress` と出たら、先に終わらせるか中止してください。**
**その状態では `git switch` が git に拒まれます。**

```bash
git -C ~/worktrees/github.com/<owner>/<repo>/continuo-<owner>-<repo>-42 rebase --abort
```

**`bisecting` と出たら、`git bisect reset` で元に戻してください。**
**bisect 中の `git switch` は拒まれませんが、警告が出るだけで bisect の途中状態が残ります**
（`git status` が `You are currently bisecting` を出し続けます）。

```bash
git -C ~/worktrees/github.com/<owner>/<repo>/continuo-<owner>-<repo>-42 bisect reset
```

**2. 期待の branch へ戻します。**

```bash
# 期待の branch が残っているとき
git -C ~/worktrees/github.com/<owner>/<repo>/continuo-<owner>-<repo>-42 switch continuo/<owner>/<repo>/42

# 期待の branch が消えているとき
git -C ~/worktrees/github.com/<owner>/<repo>/continuo-<owner>-<repo>-42 switch -c continuo/<owner>/<repo>/42
```

**3. カンバンでその issue を `Ready` へ戻します。**

**この判定も着手の最初に行います。**落ちても Status は1バイトも書きません。

**`continuo abandon` は使わないでください。**worktree の中の作業が消えます。
**`--force` を付けなくても、条件が揃うと Status を `failure_state` へ動かします。**

### 着手が「worktree が期待と違う branch に載っています」で止まる

**原因。**worktree が、continuo の作った branch とは別の branch をチェックアウトしています。

**上の2つの節とは違います。**worktree は git に登録されていて、どこかの branch には載っています。
**載っている branch が違うだけです。**中の作業は残っています。

**よくある原因。**

| 何 | 例 |
| --- | --- |
| **issue の本文が別の branch を指していた** | 「作業は既に `feature/x` にあり、draft PR も出ている」と書いてあり、エージェントがそこへ切り替えた |
| **1つの issue で2本目の PR を出した** | エージェントが新しい branch を切った |
| **人間が手で切り替えた** | 同じ worktree で別の作業をした |

**直し方。**

**1. 未コミットの変更と、push していない commit があるかを、先に確かめてください。**

```bash
git -C ~/worktrees/github.com/<owner>/<repo>/continuo-<owner>-<repo>-42 status
git -C ~/worktrees/github.com/<owner>/<repo>/continuo-<owner>-<repo>-42 log --oneline @{u}..HEAD
```

**2つ目のコマンドが `no upstream configured` で落ちたら、その branch は1度も push されていません。**
**commit は全部この機械の中にしかありません。**

**いまの branch の作業が要るなら、期待の branch へ戻したあとでマージしてください。**
**先に戻してしまうと、いまの branch を自分で覚えておかない限り引けなくなります。**

**2. 期待の branch へ戻します。**

```bash
# 期待の branch が残っているとき
git -C ~/worktrees/github.com/<owner>/<repo>/continuo-<owner>-<repo>-42 switch continuo/<owner>/<repo>/42

# 期待の branch が消えているとき
git -C ~/worktrees/github.com/<owner>/<repo>/continuo-<owner>-<repo>-42 switch -c continuo/<owner>/<repo>/42
```

**3. カンバンでその issue を `Ready` へ戻します。**

**この判定も着手の最初に行います。**落ちても Status は1バイトも書きません。

**`continuo abandon` は使わないでください。**worktree の中の作業が消えます。
**`--force` を付けなくても、条件が揃うと Status を `failure_state` へ動かします。**

**エージェントに切り替えさせないための指示は、`WORKFLOW.md` の本文にあります。**
v0.1.12 の雛形から「continuo が用意した worktree と branch のまま作業してください」が入りました。
**古い版から上げた `WORKFLOW.md` には入っていません。**
[upgrading.md](upgrading.md) の「v0.1.11 から v0.1.12 へ」を見て、手で足してください。

### 着手が「`herdr.worktree.base` が空で、カンバンから引いた issue にも既定 branch の情報がありませんでした」で止まる

**原因。**base を書いていないときはカンバンが返す既定 branch を使いますが、それが取れませんでした。

**直し方。**`WORKFLOW.md` に branch 名を書きます。

```bash
grep -n -A2 "worktree:" ~/continuo-work/WORKFLOW.md   # herdr.worktree.base の行を探す
```

```yaml
herdr:
  worktree:
    base: "main"
```

### issue に branch をリンクしたのに、worktree が既定 branch から始まる

**原因。**continuo は issue にリンクされた branch を worktree の起点にしますが、
**次のどれかに当たると、そのリンクを使わずに既定 branch へ倒します。**

| どういうとき | なぜ使えないか |
| --- | --- |
| **リンクが2本以上ある** | どれを起点にするか決められません。1本目を勝手に選ぶと、後から足したリンクで起点が変わります |
| **別のリポジトリの branch をリンクした**（fork の branch など） | issue のリポジトリの clone に `origin/<その名前>` が無いので、起点に据えると worktree を作れません |
| **branch 名に英数字・`_`・`-`・`.`・`/` 以外が入っている**（`作業/issue-42`・`feature/a+b` など） | continuo が名前を安全な形へ均すときに文字が変わり、取ってきた ref と別の名前になります |

**どれに当たったかはログに WARN で1行出ます。**リンクを捨てても着手は止めないので、
**気づく手掛かりはこの1行だけです。**

```bash
grep 'リンクされた branch' <ログの出力先>
```

**「取ってこられなかった」ときは、既定 branch へ倒しません。**上の表は「リンクを使わないと
決めた」場合です。**リンクは使えるのに `git fetch` が失敗した**（回線が切れた・権限が無い・
その branch が remote から消えた）ときは、**別の起点で作業を始めてしまわないように、
着手そのものをやり直します。**`agent.max_retries`（既定3回）まで待って試し直し、
それでも取ってこられなければ `failure_state` へ落として issue に理由を書きます。

**直し方。**リンクを**1本だけ**にして、**issue と同じリポジトリの**、
**英数字・`_`・`-`・`.`・`/` だけでできた名前の** branch を張り直します。

**張り直しただけでは、既にある worktree の起点は変わりません。**
起点は worktree を作るときに1度だけ決まります。
**作り直したいときは、その issue の worktree を片付けてから `Ready` へ戻してください。**

**起点をリンクで決めたくない場合は、`WORKFLOW.md` に固定の base を書きます。**
書いてあるときは、リンクより設定のほうが必ず勝ちます。

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

**それでも、何が確認の画面を出したのか分からないときは、次の節を読んでください。**
**agent teams が有効だと、`dontAsk` で起動していても確認の画面が出ます。**

### 「作業の途中で確認の画面に止まりました」と出る（agent teams が有効な場合）

**continuo は agent teams に対応していません。**有効になっていると、この症状が起きます。

**agent teams は Claude Code の実験的な機能で、既定では無効です。**
**`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS` を `1` にした環境でだけ有効になります。**

**何が起きるか。**エージェントが `Agent` ツールに名前を付けて呼ぶと、
**サブエージェントではなく teammate として起動します。**

**teammate は、独立した Claude Code のセッションです。**
**それを起こした側を「リード」と呼びます。**continuo が起動したのがリードです。

**teammate が許可を求めると、確認の画面はリードの pane に出ます。**
continuo はそれを「人間の入力を待っている」と読み、esc を送って pane を閉じ、issue を失敗にします。

**公式文書。**

> Teammate permission prompts appear in the lead session, so approve them there yourself.

**訳。**teammate の許可の確認はリードのセッションに出るので、そこで自分で承認すること。

出典: [Orchestrate teams of Claude Code sessions](https://code.claude.com/docs/en/agent-teams)（2026-09-01 取得）

**上の節の「`dontAsk` では確認の画面が出ない」と食い違って見えますが、両方とも起きます。**
continuo は `--permission-mode dontAsk` で起動するので、**リード自身は確認の画面を出しません。**
**ところが teammate はそれを継がず、`default` で走ることが観測されています**
（2026-08-27、外部の利用者の実測。報告された `meta.json` が3件とも `permissionMode: "default"` でした）。
**公式は「teammate はリードの許可設定を継ぐ」と書いており、この観測と食い違っています。**
**理由は分かっていません。**

**確かめ方。**

**continuo が起動した Claude Code に、その環境変数が届いているかを見ます。**

**1. continuo の設定を見る。**continuo はここに書いたものを `--settings` で渡します。

```bash
grep -n 'AGENT_TEAMS' ~/continuo-work/WORKFLOW.md
```

**2. 対象リポジトリの clone を見る。**チームで有効にしていることがあります。

```bash
ghq list --full-path | xargs -I{} grep -ln 'AGENT_TEAMS' {}/.claude/settings.json {}/.claude/settings.local.json 2>/dev/null
```

**3. 自分の設定を見る。**

```bash
grep -n 'AGENT_TEAMS' ~/.claude/settings.json ~/.claude/settings.local.json 2>/dev/null
```

**4. continuo と herdr を起動したシェルの環境変数を見る。**

**continuo は Claude Code を直接起動しません。**herdr が作った pane の中で起動します。
**だから、この2つは別のプロセスの環境になりえます。**両方見てください。

```bash
# いま叩いているシェル
echo "${CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS:-（設定されていません）}"

# herdr の常駐プロセスの環境（macOS / Linux）
ps eww "$(pgrep -n herdr)" | tr ' ' '\n' | grep '^CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS='
```

**`1` が入っていたら、まずこれを疑ってください。**
**ただし、これで原因が確定するわけではありません。**continuo は、何が確認の画面を出したかを持っていません。
`1` が入っていることは「agent teams が使える状態だった」という意味であって、
**この停止が teammate のせいだという証明ではありません。**
記録に teammate が出ていたかどうかは、上の節の【調べるところ】から辿れます。

**5. 4つとも空振りなのに症状が続くなら、組織の managed settings を疑ってください。**
**managed settings は、他のどの設定よりも後に当たります。**個人の設定では上書きできないので、
管理者に相談してください。

**直し方。**

**`WORKFLOW.md` の `claude.env` に1行足してください。**

**足す先は `claude:` の下の `env:` の塊です。**雛形には既にあります。**塊ごと貼り替えないでください。**
**場所は、既にある行から辿れます。**

```bash
grep -n 'RETRY_WATCHDOG' ~/continuo-work/WORKFLOW.md
```

```yaml
claude:
  # …（ほかの設定）
  env:                                        # Claude Code に渡す環境変数
    CLAUDE_CODE_RETRY_WATCHDOG: "1"           # 既にある行
    CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS: "0" # ← これを足す
```

**直したら continuo を再起動してください。**動いている最中は `WORKFLOW.md` を読み直しません。
**すでに動いている issue には届きません。**その issue は Status を着手待ちへ戻してやり直させてください。

**上の「確かめ方」の4番（シェルの環境変数）で見つかった場合も、同じ直し方で構いません。**
**公式の2つの文から、そう言えます。**（1つの文でそう書いてあるわけではありません。）

> Setting the variable to `0` in your user `settings.json` overrides a shell export.

**訳。**user の `settings.json` でこの変数を `0` にすると、シェルの export を上書きする。

**continuo が渡すのは `--settings` で、user の設定よりさらに後に当たります。**

> **Higher-precedence settings files**: project settings, local settings, and a `--settings` payload apply after user settings, so an `env` entry that sets the variable to `1` in any of them wins.

**訳。**優先順位の高い設定ファイル: プロジェクトの設定・ローカルの設定・`--settings` で渡すものは、
user の設定より後に当たる。だからそのどれかに、この変数を `1` にする `env` の項目があれば、そちらが勝つ。

**シェルで `unset` するだけでは足りないことがあります。**
**continuo は Claude Code を直接起動せず、herdr が作った pane の中で起動します。**
**pane が herdr の環境をどこまで継ぐかは、こちらでは確かめられていません。**

**`WORKFLOW.md` に書くのが確実です。**そちらは `--settings` で渡るので、
**シェルの環境変数より後に当たります。**

**なぜ `WORKFLOW.md` に書くのか。**

**continuo は Claude Code を `--settings` 付きで起動します。**
**その設定は、対象リポジトリの `.claude/settings.json` より優先順位が上です。**

| 順位 | どこ |
| --- | --- |
| 1 | 組織の managed settings |
| **2** | **`claude --settings`**（continuo が渡すもの） |
| 3 | `.claude/settings.local.json` |
| 4 | `.claude/settings.json` |
| 5 | `~/.claude/settings.json` |

**リポジトリ側が `1` にしていても、こちらが勝ちます。**
**勝てないのは1番の managed settings だけです。**その場合は管理者に相談してください。

**人が自分の手で叩く `claude` には影響しません。**continuo が起動したセッションにだけ効きます。

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

### ボードに issue があるのに、1件も着手されない

**Status も動かず、issue にも何も書かれないので、止まっているように見えます。**
**既定の水準（info）で出るのは、次の3つです。**まずこれを探してください。

```bash
grep -E '枠が閾値を超えている|空きスロットが尽きた|必須のラベルが揃っていない' <ログの出力先>
```

| 出ている行 | 何が起きているか | どうするか |
| --- | --- | --- |
| **枠が閾値を超えているので新規の dispatch を止めます** | 定額プランの枠が `rate_limit.pause_above_percent`（既定95%）を超えました。**走っている turn は止めません** | **待てば自分で再開します。**すぐ動かしたいときは `pause_above_percent` を上げてください |
| **空きスロットが尽きたので、この巡回ではこれ以上 dispatch しません** | 同時に動かせる数を使い切りました。**上限は2つあります**（全体と、Status ごと） | **走っているものが終われば順に着手します。**増やすには、**ログの `上限に達した設定` に出ている名前**を上げてください |
| **必須のラベルが揃っていないので飛ばします** | `tracker.required_labels` に書いたラベルが、その issue に付いていません。**足りないラベルの名前が全部、同じ行に出ます** | **ラベルを付けるか、`required_labels` を見直してください**。この行は**足りないラベルの組み合わせごとに1回だけ**出ます（1つ付けて、まだ足りなければもう1回出ます） |

**3つとも出ていないときは、まず入札を疑ってください。**

**枠の余裕が足りないとき、continuo は入札しません。**その判定は `pause_above_percent` より手前で働きます。

**見るのは5時間の枠だけではありません。**1週間の枠も、それぞれ独立に同じ判定を受けます。
**どれか1つでも下の帯に入れば、そこで止まります。**

| いちばん使っている枠の使用率 | dispatch を止めるか | 入札するか | 既定の水準で見えるか |
| --- | --- | --- | --- |
| 〜90% | 止めない | する | 正常に着手します |
| **91〜95%** | **止めない** | **しない** | **1行も出ません** |
| 96%〜 | 止める | しない | 上の表の1行目が出ます |

**境界の出どころ。**91% は `five_hour_margin_percent` と `weekly_margin_percent`（どちらも既定10）から、
96% は `rate_limit.pause_above_percent`（既定95。**`95` ちょうどでは止まりません**）から決まります。

**5時間の枠が30%でも、1週間の枠が92%なら、この帯に入ります。**

**この帯に入っているかは、Debug でしか分かりません。**

```bash
continuo --log-level debug   # 起動し直して「入札しません」を探す
```

**判定の中身は、下の「複数の機械で見張っているのに、どの機械も issue を取らない」にあります。**
**1台で動かしていても当たります。**

**それでも出ないときは、担当者を見てください。**
**人間が担当者になっていると、continuo は触りません**（下の
「人間が担当者になっている issue が、いつまでも着手されない」を読んでください）。

**v0.1.10 までは、「必須のラベルが揃っていない」だけ1行も出ていませんでした。**
**v0.1.11 から出ます。**残る2つは前から INFO で出ています。

**3つとも WARN ではなく INFO です。**どれも異常ではなく、
**待てば自分で進むか、設定を変えれば済むもの**だからです。

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

**何をどう書くかは、「使い方が分からないとき」の
「エージェントが PR を作った直後に止まる（automated_state_rewrite）」にあります。**
そのまま貼れる yaml と、書けない5つの形がそこにあります。
**足す場所と、当てたあとの確かめ方は [upgrading.md](upgrading.md) の「足す場所と中身」です。**

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

**対応表そのものの決め方は、「使い方が分からないとき」の
「エージェントが PR を作った直後に止まる（automated_state_rewrite）」にあります。**

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

### エージェントが push で終わり、PR が作られない

**まず知っておくこと。**v0.1.14 から、エージェントは `CONTINUO-STATUS: review` を出す前に
**自分で PR を作ります**（組み込みのプロンプトの `## 3-5. pull request を出す`）。
**組み込みに、それを打ち消す口はありません。**

**原因は2つあります。順に確かめてください。**

**1つ目。`WORKFLOW.md` の本文に、v0.1.13 の雛形の1行が残っています。**

```bash
cd ~/continuo-work && continuo prompt --show | grep -n 'PR は作らないでください'
```

**行番号が出たら、これです。**`WORKFLOW.md` を開いて、その行を消してください
（`## PR を作るか` の節ごと消してかまいません）。
**組み込みが「作れ」と言い、あなたの本文が「作るな」と言っている状態です。**
**組み込みは打ち消しを受け付けないので、どちらに従うかはエージェント次第になります。**
**確実に作らせたいなら、その行を消してください。**

**2つ目。組み込みの側に節が無い。**版が古いままです。

```bash
continuo prompt --show --builtin | grep -c '^## 3-5\. pull request を出す'
```

**`0` なら v0.1.13 以前です。**版を上げてください。
**`1` で、それでも作られないときは、pane の応答を読んでください。**
push できていない（`gh` の認証・branch protection）と、`gh pr create` は
**対話で push 先を聞いてきて、そこで止まります。**

```bash
herdr agent read continuo-hello-world-42 --source recent-unwrapped --lines 40
```

**書き換えたら continuo を再起動してください。**動いている最中は `WORKFLOW.md` を読み直しません。

### PR にレビューを書いたのに、エージェントが読まずに終わる

**原因。**その PR の本文に `Closes #<issue の番号>` がありません。

**仕組み。**エージェントは、次の turn で「この issue に紐づく PR」を2つのコマンドで探します。
**1つ目は `closingIssuesReferences` を見ます。**これは
**PR の本文に `Closes #42` / `Fixes #42` と書けば確実に埋まります**
（GitHub の画面の `Development` から手で紐づける道もありますが、本文に書くのが確実です）。
**2つ目は issue の timeline の相互参照を見ます。**こちらは本文以外からも張られるので、
**何が返るかを当てにできません。**返らないこともあれば、**この issue とは関係の無い PR が返ることもあります。**
**どちらにも出てこない PR は、エージェントから見えません。**

**確かめ方。**

```bash
gh pr view <PR番号> --repo <owner>/<repo> --json closingIssuesReferences --jq '.closingIssuesReferences'
```

**`[]` が返ったら、結びついていません。**

**直し方。**PR の本文へ1行足します。エージェントが次に起動されたときから見えるようになります。

```bash
gh pr edit <PR番号> --repo <owner>/<repo> --body "$(gh pr view <PR番号> --repo <owner>/<repo> --json body --jq .body)

Closes #<issue の番号>"
```

**組み込みのプロンプトは、この1行を入れるようエージェントに指示しています。**
**それでも落ちていたときの直し方が、これです。**

### issue が `In Review` にならない

**原因。**エージェントが `CONTINUO-STATUS: review` を出していません。
**continuo が信じるのはカンバンの Status だけです。**エージェントが「終わった」と言っても、Status が動いていなければ終わっていません。

**直し方。**pane で応答を読み、送られた文面に表明のしかたが入っているかを確かめます。

```bash
herdr agent read continuo-hello-world-42 --source recent-unwrapped --lines 40
cd ~/continuo-work && continuo prompt --show | grep -n "CONTINUO-STATUS"
```

**表明のしかたは組み込みのプロンプトにあります。**`WORKFLOW.md` を grep すると
front matter の `status_signal_prefix`（continuo が読む側の設定）が引っかかりますが、
**それはエージェントへの指示ではありません。**送られる文面のほうを見てください。

**書き換えたら continuo を再起動してください。**動いている最中は読み直しません。

### 応答を書き直している最中のエージェントに、continuo が次の指示を送ってしまう

**まず用語です。**Claude Code の `Stop` hook は、`{"decision":"block","reason":"…"}` を返すと
**エージェントを止めずに、`reason` を指示として渡して応答を書き直させます。**これを以下**差し戻し**と呼びます。
応答の書式を検査する hook や、コミット前の検査を強制する hook がこれを使います。

**何が起きていたか。**差し戻しは hook の**戻り値**であって、hook そのものではありません。
**continuo には届きません。**continuo から見えるのは「エージェントが止まってよいか尋ねた」という
`Stop` だけで、それを「turn が終わった」と読んでいました。**書き直しの中央値は 21 秒、最大 83 秒です。**
continuo は 2 秒で締め切っていたので、次の3つが起きます。

| 何が起きるか | 見え方 |
| --- | --- |
| **差し戻された側の応答で Status が動く** | 書き直している最中に pane が閉じられ、**書きかけの編集が消えます** |
| **書き直した応答が読まれない** | 正しく書き直した `CONTINUO-STATUS:` が拾われません |
| **遅れて届いた `Stop` が、次の turn の終わりとして数えられる** | 指示を1文字も読んでいないのに turn が終わったことになり、`max_dispatch_turns`（既定20）を空回りで使い切って `Blocked` へ落ちます。**issue に残る理由は「作業が終わったという表明を出しませんでした」**で、実際とは別の話になります |

**v0.1.13 で直りました。**continuo は締め切りが来た瞬間に herdr へ
「このエージェントはまだ動いているか」を1回聞き、**動いていれば turn の終わりとせずに待ち直します。**
**設定は要りません。**`WORKFLOW.md` に足すものもありません。

**この症状が出るのは、差し戻す `Stop` hook を入れている場合だけです。**
continuo が渡す設定は**追加**であって置き換えではないので、
`~/.claude/settings.json` と worktree の `.claude/settings.json` に書いた `Stop` hook も一緒に走ります。

**直っているかの確かめ方。**continuo のログに次の行が出ていれば、待ち直しています。

```text
空の Stop のあともエージェントが動いているので、turn の終わりとせずに待ち直します
```

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

**mac-studio がこの issue の担当に立候補しています。**上の JSON は、その機械にレートリミットの枠がどれだけ残っているかです。
**担当は約3分後に自動で決まります。**締め切りまでに届いた入札のうち、枠の余裕がいちばん大きい機械が担当になります。
```

**`host` でどちらの機械の入札か、`score` でどちらが勝つ見込みかが分かります。**
**JSON の下の2行は、同じことを日本語で書いたものです**（`language: en` なら英語で出ます）。
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
**他の機械は、担当している機械の最後の進捗報告から `idle_timeout_ms`（既定18時間）が経つまで、
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

### 作業が続いているのに担当が外れ、別の機械が同じ issue を最初からやり直した

**複数の機械で同じカンバンを見張っているときだけ起きます。**1台で動かしているなら起きません。

**症状。**1台が長い作業を続けているあいだに、その issue の担当（assignee）が外れ、
`<!-- continuo:released -->` のコメントが付き、**別の機械が入札からやり直しました。**
新しい機械は worktree を作り直すので、**前の機械が push した分からしか続けられません。**

**原因。**担当を外す期限は「担当者が最後に進捗報告を書いてから」で数えます
（`tracker.provider.handoff.idle_timeout_ms`。既定 `64800000` ミリ秒 = 18時間）。
**進捗報告とは、本文に `<!-- continuo:progress -->` が入っているコメントのことです**
（v0.1.14 から。下の「進捗のコメントが1件にまとまり、増えなくなりました」）。
**エージェントが黙って作業を続けているあいだ、時計は1秒も進みません。**

**進捗報告が1件も無いあいだは、担当を取った時刻（`<!-- continuo:hold -->` のコメント）から数えます。**
**これがいちばん多い場面です。**機械が落ちて、進捗報告を1件も書けないまま止まったときがそれです。
**そうしないと時計が始まらず、落ちた機械の担当が永久に外れません。**

**v0.1.13 で変わったこと。**continuo が送る組み込みのプロンプトに
`## 長くかかるときは、途中でも状況を書くこと` の節が入りました。
**v0.1.14 で、この節は `## 5-3. {{.progress_interval_minutes}}分以上黙らない` へ名前が変わりました。**
**一定の時間コメントを書かないまま作業を続けないようエージェントへ促し、
push できる状態なら push させます。**
**その時間は v0.1.13 では1時間で固定、v0.1.14 からは `tracker.provider.handoff.progress_interval_ms`（エージェントに進捗報告を書かせる間隔。ミリ秒。既定 3600000 = 1時間）で決まります。**
**担当が外れるのが早すぎるなら、この値を短くしてください。**
**`idle_timeout_ms` を1時間以下にしている人は、書かないと起動しません**
（上の「版を上げたら『progress_interval_ms の値 3600000 が不正です』で起動しなくなった」）。

**節が届いているかの確かめ方。****このコマンドは `WORKFLOW.md` を読まない**ので、どこで叩いても構いません。

```bash
continuo prompt --show --builtin | grep -c '^## 5-3\. '
```

**`1` なら入っています。**`0` なら v0.1.13 以前です。版を上げてください。
**引いているのは v0.1.14 の名前なので、v0.1.13 でも `0` が返ります。**

**見出しに変数が入っているので、`^## 5-3\. ` で引きます。**
`continuo prompt --show --builtin` は変数を展開しないため、
**`## 5-3. {{.progress_interval_minutes}}分以上黙らない` のまま出ます。**

**エージェントが書いているかの確かめ方。**issue に `<!-- continuo:agent -->` で始まるコメントが1件あり、
**その中に、`progress_interval_ms` で決めた間隔ごとの行が増えていれば効いています**（既定は1時間）。

**v0.1.14 から、コメントは1件のまま増えません。**
**エージェントは、いちばん下にある自分の進捗報告に書き足します**（下の
「進捗のコメントが1件にまとまり、増えなくなりました」）。
**GitHub の画面では、そのコメントに `edited`（編集済み）が付きます。**

**保証ではありません。**プロンプトは指示であって強制ではないので、
**エージェントが書かないまま18時間が過ぎれば、いままでどおり担当は外れます。**

**それでも外れるときの直し方。**

| どうしたいか | 何をするか |
| --- | --- |
| **待つ時間を延ばす** | `WORKFLOW.md` の `tracker.provider.handoff.idle_timeout_ms` を大きくする（単位はミリ秒）。**延ばすほど、本当に落ちた機械が抱えた issue が拾われるまで長くかかります** |
| **持ち回りをやめる** | 下の「持ち回りを使わずに、1台だけで動かしたい」 |
| **取り残された作業を拾う** | 前の機械の worktree で `git status --short` と `git log --oneline HEAD --not --remotes` を叩き、残っているものを push する |

**push していない変更は、担当が移った時点で他の機械から見えなくなります。**

### 進捗のコメントが1件にまとまり、増えなくなりました

**複数の機械で同じカンバンを見張っているときだけ関わる話です。**1台で動かしているなら、
表示が変わるだけで、直すものはありません。

**v0.1.13 まで。**エージェントは1時間ごとに**新しいコメントを1件投稿していました。**
**18時間の作業で18件並び、issue を開いても本題が読めませんでした。**

**v0.1.14 から。****いちばん下のコメントが自分の進捗報告なら、その1件に行を書き足します。**

```
<!-- continuo:agent -->
<!-- continuo:progress -->
まだ作業中です。

- 2026-09-03T05:40:34Z いま テストを直しています
- 2026-09-03T06:41:02Z いま PR の指摘に答えています
```

**間に別のコメントが入ったときは、書き足しません。**新しく1件投稿します
（**あなたが書いたコメントを、エージェントが書き潰さないためです**）。
**前の進捗報告はそのまま残ります。**

**設定に足すものはありません。****`WORKFLOW.md` は1文字も変えません。**

**持ち回りの期限は、書き足しでも進みます。**
**GitHub はコメントを編集しても作成時刻を動かしません**が、更新時刻は進みます。
**continuo は v0.1.14 から、その更新時刻も見ます。**

**`<!-- continuo:progress -->` は何か。**進捗の報告だけに付く2行目の印です。
**最後の成果報告には付きません。**付いていないので、成果報告に書き足されることはありません。
**設定のキーはありません。**`WORKFLOW.md` へ書くところはどこにもなく、値も変えられません。

**v0.1.14 から、continuo はこの印を見ます。**
**18時間の時計を進めるのは、この印が付いたコメントだけです。**

**なぜそうしたか。**エージェントも continuo もあなたも、同じ GitHub アカウントで投稿します。
**v0.1.13 まではコメントの投稿者だけで数えていたので、
あなたが無関係なコメントを1件書くたびに、18時間の時計が振り出しに戻っていました。**
**落ちて黙り込んだ機械の担当が、それだけで18時間延びます。**別の機械が拾い直せません。

| 何をしたとき | v0.1.13 まで | v0.1.14 から |
| --- | --- | --- |
| **あなたが issue へ1件書く** | **落ちた機械の担当が18時間延びた** | **延びません** |
| **エージェントが進捗報告に書き足す** | **時計は1秒も進まなかった** | **進みます** |

**あなたがこの印を書いても構いません。**そのコメントのぶん、
**落ちた機械の担当が18時間延びるだけです。**わざわざ書く理由はありません。

**副作用が1つあります。**担当者が古い進捗報告を1文字直しただけでも、18時間の時計は振り出しに戻ります。
**進捗報告を新しく1件書いたときも同じことが起きます**ので、同じ種類の遅れです。

### 人間が担当者になっている issue が、いつまでも着手されない

**原因。**その issue に、continuo が使うアカウント以外の担当者が付いています。
**continuo は、人間が付けた担当には触りません。**

**この判定は、Status が `Ready` でも `In Progress` でも働きます。**
**ボードの上では、着手待ちのまま止まって見えます。**

**確かめ方。**continuo のログに、この1行が出ています。

```
WARN 担当者が付いているので着手しません（continuo が付けたものではありません）。
     着手させるには、GitHub の画面でその担当者を外してください
     identifier=octocat/hello-world#42 担当者=octocat-human
```

```bash
grep '担当者が付いているので着手しません' <ログの出力先>
```

**この行は巡回のたびに出続けます**（既定30秒ごと）。**1回だけではありません。**

**ダッシュボードにも出ます。**`http://127.0.0.1:<server.port>/` を開くと、
いちばん上の「着手できずに止まっているもの」の表に、その issue と理由と直し方が並びます。
**この表はメモリだけに持っています。**continuo を再起動すると消え、次の巡回で作り直されます。

**issue にも書かれます。**同じ理由で3巡回以上（かつ最初に止めてから60秒以上）
止まり続けたときに、continuo が「なぜ着手しないか」と「どうすれば動くか」をコメントで1件書きます。
**書くのは、そのアカウントにつき1回です。**別々のアカウントの PC が5台あれば、5件付きます。
**担当者の名前は書きません。**やることは担当者が誰であっても同じだからです。

**書かせたくないときは、`WORKFLOW.md` の `on_assignee_gate` の値を書き換えてください。**

**まず、いまあるかどうかを確かめてください。**

```bash
grep -n 'on_assignee_gate' ~/continuo-work/WORKFLOW.md
```

**行が出たら、その値を書き換えます。**1行も出なければ、次で足してください
（v0.1.11 以前の `continuo init` が置いた `WORKFLOW.md` には、このキーがありません）。

```bash
cd ~/continuo-work && continuo doctor --missing-keys-patch WORKFLOW.md | patch -p0 WORKFLOW.md
```

**足りないキーだけを、正しい位置へ入れます。**既にあるキーには触らないので、
front matter が重複キーになることはありません。

```yaml
tracker:
  provider:
    handoff:
      # …（ほかの設定）
      on_assignee_gate: warn_only   # 既定は warn_and_comment。この値を書き換える
```

| 値 | ログの WARN | ダッシュボード | issue へのコメント |
| --- | --- | --- | --- |
| `warn_and_comment`（既定） | 出ます | 出ます | **そのアカウントにつき1回書きます** |
| `warn_only` | 出ます | 出ます | **書きません** |

**ダッシュボードとログは、この設定では止まりません。**止まるのは issue への書き込みだけです。

**直し方。**次のどれかを行ってください。**Status を動かさずに直す道もあります。**
**3つ目の道は、下の表のすぐあとに書いてあります。**

| どうしたいか | 何をするか |
| --- | --- |
| **continuo に任せる** | **GitHub の画面で、その担当者を外す。**次の巡回で「担当者が無い」と判定され、入札からやり直します |
| **人間が自分でやる** | **`tracker.active_states` に入っていない Status へ動かす**（既定は `Ready` と `In Progress` の2つなので、そのどちらでもない Status にする）。**ボードから外しても構いません** |

**`In Progress` へ動かしても止まりません。**既定ではそれも `active_states` に入っているので、
**候補として返り続け、巡回のたびにコメントを1本読んで WARN を出します。**

**「continuo が使うアカウントへ付け替える」は、条件を満たすときだけ効きます。**
効く条件は「そのアカウントを使う continuo が1台だけであること」です。
**continuo の総台数は関係ありません。**アカウント1つにつき1台であれば、何台動かしていても効きます。
同じアカウントを2台以上の continuo が使っていると、2台とも「自分の担当だ」と読み、同時に着手します。

メンバーごとに別の GitHub アカウントを使うチームでは、この条件を満たします。
**その使い方は下の「チームで、issue ごとに担当の PC を決めたい」にあります。**

**担当者が2人以上いる場合も同じです。**そのときの文面は
「担当者が2人以上いるので触りません（人間が触っています）」になります。
**ただし、この場合だけは「もう書いた」の記録がメモリにしかありません。**
continuo を再起動すると、同じ issue へもう一度書くことがあります。
**ダッシュボードにも同じように出ます。**

**ただし、その2人以上の中に continuo が使うアカウントが混じっているときは、issue へは書きません。**
**「人間が2人」なのか「人間1人 ＋ 別の機械が作業中」なのかを、continuo が区別できないためです。**
後者で「担当者を全部外してください」と書くと、走っている別の機械の担当まで外させることになり、
**同じ issue に2台が乗ります。**
このときダッシュボードの行には「別の機械の担当かどうかを切り分けられません」の印が付きます。
**別の機械が担当していないかを先に確かめてから、担当者を外してください。**

**なぜ触らないのか。**担当者は「いま誰がこの issue を持っているか」の印です。
**人間が付けた印を continuo が上書きすると、人間の作業を横取りすることになります。**

### チームで、issue ごとに担当の PC を決めたい

**メンバー1人につき continuo を1台、それぞれ別の GitHub アカウントで動かしている場合の使い方です。**

やりたいことは、たいてい2つに分かれます。

| どうしたいか | 何をするか |
| --- | --- |
| 誰の PC がやってもいい | 担当者を空のままにする。`Ready` へ動かせば、入札で決まります |
| 自分の PC にやらせたい | 自分を担当者にしてから `Ready` へ動かす |

2つ目が効く理由は1つです。
continuo は、担当者のアカウントを「その PC の `gh` の持ち主」と比べます。
一致すれば自分の担当と読み、着手します。

#### 成り立つ条件

**その PC の `gh` が、あなた自身のアカウントでログインしていること。**

```bash
gh auth status
```

`Logged in to github.com account <名前>` の名前が、担当者に付けた名前と同じであることを確かめてください。
continuo も、起動のたびにログへ出します。

```
INFO gh の持ち主を確認しました（コメントの印と併せて見ます） login=<その人のアカウント名>
```

ここが別人のアカウント（チームで共有しているボットなど）だと、この使い方は成り立ちません。
そのときは下の「同じ GitHub アカウントで、複数の PC を動かしたい」を読んでください。

#### 他のメンバーの PC は何をするか

触りません。ただし、issue へ「担当者を外してください」という案内を書きます。
あなたはわざと担当者を付けているので、この案内は当てはまりません。

**書くのは、そのアカウントにつき1回です。**「もう書いた」の記録は、その PC の `gh` の持ち主が
書いたコメントを探して作るので（[internal/orchestrator/gate.go:325-339](../internal/orchestrator/gate.go#L325-L339)）、
**他のメンバーが書いた案内は互いに見えません。**5人のチームなら、予約した issue 1件につき4件付きます。

**書かせたくないときは、全部の PC の `WORKFLOW.md` で `on_assignee_gate` の値を書き換えてください。**

**まず、いまあるかどうかを確かめてください。**

```bash
grep -n 'on_assignee_gate' ~/continuo-work/WORKFLOW.md
```

**行が出たら、その値を書き換えます。**1行も出なければ、次で足してください
（v0.1.11 以前の `continuo init` が置いた `WORKFLOW.md` には、このキーがありません）。

```bash
cd ~/continuo-work && continuo doctor --missing-keys-patch WORKFLOW.md | patch -p0 WORKFLOW.md
```

**足りないキーだけを、正しい位置へ入れます。**既にあるキーには触らないので、
front matter が重複キーになることはありません。

```yaml
tracker:
  provider:
    handoff:
      # …（ほかの設定）
      on_assignee_gate: warn_only   # 既定は warn_and_comment。この値を書き換える
```

ログとダッシュボードには出続けます。止まるのは issue への書き込みだけです。
担当している PC の動きは、この設定では何も変わりません。

#### 担当している PC が壊れたとき

**GitHub の画面で担当者を外してください。**外れた瞬間から入札の対象に戻り、
余裕のある機械が拾います。

### 同じ GitHub アカウントで、複数の PC を動かしたい

**サポートしていません。**機械ごとに別の GitHub アカウントを用意してください。

**なぜか。**担当者（assignee）だけでは、2台を見分けられないためです。
**同じアカウントなら、どちらが担当しても担当者欄は同じ値になります。**

**見分けるには `hold` のコメントの `host`（機械の名前）に頼るしかありませんが、これは重複します。**

| 何 | なぜ重複するか |
| --- | --- |
| **WSL のディストリを2つ動かしている** | **既定が Windows のホスト名です。**`/etc/wsl.conf` に書かないかぎり、2つとも同じ名前になります |
| **社内で命名規則に沿って配られた PC** | 規則が同じなら衝突します |

**重複すると、2台とも「自分が勝った」と読み、2台とも同じ issue に着手し、同じ branch を押し合います。**

#### 別のアカウントを用意する手順

**1. 2台目用の GitHub アカウントを作る。**組織で使うなら、bot 用のアカウントを1つ用意してください。

**2. そのアカウントを、対象のリポジトリとカンバンに招く。**

| 何 | 要る権限 |
| --- | --- |
| リポジトリ | **write**（branch を push し、issue にコメントするため） |
| カンバン（Projects v2） | **write**（Status を書き換えるため） |

**3. 2台目で、そのアカウントでログインする。**

```bash
gh auth login -s project
```

**4. `WORKFLOW.md` の `tracker.provider.owner` は、カンバンの持ち主のままにする。**
**ログインするアカウントとは別です。**`owner` はカンバンがぶら下がっている GitHub のユーザーか組織の名前です。

**5. 残りの手順は、1台目と同じです。**

**確かめ方。**issue のコメントを見てください。**`hold` の `assignee` が、機械ごとに違う名前になっていれば正しく分かれています。**

```
<!-- continuo:hold -->
{"host":"mac-studio","assignee":"octocat-bot-a","branch":"continuo/octocat/hello-world/188","at":"2026-08-29T18:45:00+09:00"}
```

#### 同じ PC で continuo を複数動かしたい

**これもサポートしていません。**検証のためだけの使い方です。

**同じ PC なら機械の名前も同じになるので、上と同じことが起きます。**

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

### push したのに「push されていない commit が N 件残っている」「base との差分が残っている」と言われ、worktree が片付かない

**原因。****`-u` を付けずに、別の名前へ push しました。**

```bash
git push origin HEAD:pr-2nd      # これだと upstream が張り替わらない
```

**`git push origin HEAD:<別の名前>` は upstream を張り替えません。**
upstream は worktree を作ったときのままです。**出るメッセージは、その upstream があるかどうかで違います。**

| その worktree の upstream | 出るメッセージ |
| --- | --- |
| **1本目の push で張られている** | `push されていない commit が N 件残っている` |
| **一度も張られていない** | `upstream が無いまま base <base> との差分が残っている（push されていない成果がある）` |

**どちらも同じ原因です。**片付けが upstream しか見ていなかったので、
別の名前へ出した成果を見つけられませんでした。

**v0.1.12 から、この形でも片付きます。**判定の最初の段が
「HEAD が `refs/remotes/` のどれかに載っているか」を見るようになったためです。
**載っていれば、upstream が古くても消してよいと判定します。**

**それでも `-u` は付けてください。**理由は2つあります。

| 何 | どうなるか |
| --- | --- |
| **見送りの理由の文面** | `-u` が無いと、古い upstream を基準に数えた件数が出ます。**人間が読む数が実態と合いません** |
| **次に `git push` とだけ叩いたとき** | upstream が古いままなので、**さっき出した branch ではないほうへ行きます** |

**直し方。**worktree に入って、`-u` を付けて叩き直します。
**`Everything up-to-date` と出ても upstream は張り替わります**（新しく push する必要はありません）。

```bash
grep -n -A2 '^workspace:' ~/continuo-work/WORKFLOW.md   # root: が worktree の置き場所
cd <その issue の worktree>
git push -u origin HEAD:pr-2nd
git rev-parse --abbrev-ref --symbolic-full-name '@{u}'  # origin/pr-2nd と出れば張り替わっている
```

**エージェントにも同じことをさせるため、`WORKFLOW.md` の本文を当ててください。**
差し替える文面は [upgrading.md](upgrading.md) の「v0.1.11 から v0.1.12 へ」にあります。
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
戻すかどうかはカンバンで決めてください。

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

### `git worktree list` に、continuo が作った覚えの無い worktree が並ぶ

**原因。**エージェントが自分で `git worktree add` を叩いています。
**continuo が片付けるのは、その issue のために自分で用意した worktree 1つだけです。**
片付ける相手はそのパス1つに固定されていて、**一覧から対象を増やすことはありません。**

**残ると何が起きるかは、置かれた場所で3通りに分かれます。**

| エージェントが置いた場所 | どうなるか |
| --- | --- |
| **continuo の worktree の中** | **その issue の片付けが止まります**（未追跡のファイルとして数えられるため） |
| **同じく中だが、そのパスが `.gitignore` に入っている**（`.claude/worktrees/<名前>` など） | **止まりません。**continuo は強制の指定を付けて消すので、**中の worktree ごと、コミットしていない変更も消えます** |
| **continuo の worktree の外** | **その issue は片付きます。**clone 側に登録と branch だけが黙って残ります |

**消す経路は設定で変わりますが、どれも強制です。**既定（`herdr.worktree.create_via_herdr` が `true`）なら
herdr の `worktree.remove` を `force` で呼び、`false` なら `git worktree remove --force` を叩きます。
**それでも実体が残っていたら、最後はディレクトリごと消します。**

**止まったときは、issue に「worktree を片付けずに残しました」というコメントが1回付きます。**
`cleanup.require_clean_worktree`（既定 `true`）が
「コミットされていない変更が残っている（未追跡のファイルを含む）」を理由に見送るためです。

**止まらない側は `--ignored` を付けないと見えません。**

```bash
git -C <その issue の worktree> status --porcelain -uall --ignored
```

**`!!` で始まる行は `git status` には出ません。**そこに worktree があっても、片付けは止まりません。

**`continuo doctor` は数えません。**`worktree の場所` の検査が見るのは、
`workspace.root` の直下4階層（`<root>/<host>/<owner>/<repo>/<スラグ>`）にあるものだけです。
エージェントが足した worktree はそこに当てはまらないので、`✓` のままになります。

**気づく手立ては `git worktree list` です。**

```bash
git -C ~/ghq/github.com/<owner>/<repo> worktree list
```

**一覧には、continuo が別の issue のためにいま使っている worktree も並びます。**
`workspace.root` の下（`<root>/<host>/<owner>/<repo>/<スラグ>`）にあるものが continuo のものです。
**そちらを消すと、動いているエージェントの作業場所が、確認も警告も無く消えます。**

**登録が残っているあいだは、その branch を `git branch -D` で消せません**
（下の「`error: cannot delete branch '…' used by worktree at '…'` と出る」）。

**実体が先に消えた登録は、次の着手のときに落ちます。**continuo は worktree を用意するたびに、
その clone へ `git worktree prune` を1回撃つためです（登録が残ったままだと worktree を作れません）。
**このとき、利用者がディレクトリごと移しただけの worktree の登録も一緒に落ちます。**
落ちると git はその branch を守らなくなるので、**移して残すのではなく、消す前に push を済ませてください。**

**直し方。**消す前に、失うものが無いことを確かめます。

```bash
git -C <消したい worktree> status --short
git -C <消したい worktree> log --oneline HEAD --not --remotes
git -C ~/ghq/github.com/<owner>/<repo> worktree remove <消したい worktree>
```

**1つ目が出たら commit してから、2つ目が出たら push してから消してください。**
**`--force` は付けないでください。**コミットしていない変更が、確認も警告も無く消えます。
**`git worktree remove` が断ったのは、消してはいけないものが残っているからです。**
**lock されているときも断ります**（`cannot remove a locked working tree`）。
中に何も残っていなくても断るので、そのときは `git worktree unlock <消したい worktree>` を先に叩いてください。

**同じことが起きないように、`WORKFLOW.md` の本文を当ててください。**
エージェントに自分で片付けさせる文面は [upgrading.md](upgrading.md) の
「v0.1.12 から v0.1.13 へ」にあります。
**当てたら continuo を再起動してください。**動いている最中は `WORKFLOW.md` を読み直しません。

### `error: cannot delete branch '…' used by worktree at '…'` と出る

**原因。**git が、その branch を出している worktree の**登録**を見て守っています。
**branch を消すときは、continuo は登録を外しません。**`git worktree prune` は
リポジトリ全体に効くので、**単に移動しただけの別の worktree まで巻き込む**ためです。
**片付けでも、実体の無い登録がほかに1つでもあれば撃ちません**（自分が消した1件だけが対象のときは撃ちます）。
**ただし、次にその clone で worktree を用意するときは、条件なしで1回撃ちます**
（登録が残ったままだと worktree を作れません）。**移した worktree の登録も、そのとき落ちます。**

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

### グループでまとめて直したら、代表以外の issue にもコメントが増えた

**原因。**版を上げて、エージェントへ送る指示書が
「**表明で `review` か `blocked` を出した issue には、その issue へ何をしたかを書く**」と
求めるようになりました。**古い版では、代表以外に残るのは continuo が書く
「Status を **… → In Review** へ動かしました」の1行だけで、何が直ったのかは1文字もありませんでした。**

**直し方。****対処は要りません。**そのコメントは `<!-- continuo:group -->` の行で始まります。
**代表の側には、書いた先の URL が並びます。**どちらもエージェントが書いたもので、continuo は代筆しません。

**`WORKFLOW.md` に足す設定はありません。**指示書は continuo の実行ファイルの中にあるので、
版を上げれば届きます。送られる全文は `continuo prompt --show` で読めます。

```bash
continuo prompt --show
```

**このコメントを止める設定はありません。**`WORKFLOW.md` の本文から
`### まとめて直してよい範囲` の節を消しても、**組み込みの 7-2 はそのまま届きます。**
**グループの計画は代表の issue のコメント側にもあるので**（`gh issue view --json comments` で読ませています）、
**節を消しても、まとめて直す経路そのものは残ります。**まとめて直したときは、このコメントが付きます。

**版を上げたときの案内は [upgrading.md](upgrading.md) にもあります。**

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
| `continuo setup [ディレクトリ]` | カンバンの Status を5つの役割へ対応づける（対話） |
| `continuo trust [ディレクトリ]` | 対象リポジトリを Claude Code に信頼登録する。`--dry-run` で下見 |
| `continuo doctor [ディレクトリ]` | 前提が揃っているかを15の見出し語で調べる |
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

**`continuo init --force` で作り直さないでください。**`continuo setup` で決めた Status の割り当てが雛形で潰れ、
**手で書いた本文も雛形に戻ります**（`--force` は front matter も本文も上書きします）。
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

### エージェントが PR を作った直後に止まる（automated_state_rewrite）

**まず、この設定が自分に要るのかを確かめてください。**
カンバンの `Settings` → `Workflows` を開きます。**Status を書く自動化**
（`Item added to project` / `Pull request merged` / `Code changes requested` など）が
1つでも**有効**になっていますか。

| `Workflows` の状態 | どうするか |
| --- | --- |
| **1つも有効になっていない** | **この設定は要りません。**`automated_state_rewrite: {}` のままにしてください |
| **1つでも有効になっている** | **下を読んでください** |

**何が起きるか。**エージェントが PR を作ると、その自動化がカンバンの Status を書き換えます。
書き換わった先は `tracker.active_states` に無い Status なので、
**continuo は「人間が引き取った」と読みます。**`tracker.unknown_state_grace_ms`（既定10分）の
猶予を置いてから、**動いているエージェントを turn の途中で止めます。**
利用者の環境では、**PR を作った3秒後に自動化が Status を書き、その29秒後の巡回で止まりました。**

**どう解決するか。****Status を書いたのが誰かを見ます。**

| Status を動かしたのが | continuo はどうするか |
| --- | --- |
| **カンバンの組み込みの自動化** | **止めません。**対応表にある Status へ書き戻して、作業を続けさせます |
| **人間** | **いままでどおりです。**猶予を置いてからエージェントを止めます |

**人間が動かしたものは戻しません。**
**「人間が Status を動かしてエージェントを止める」操作は、そのまま効きます。**

**書かなかったらどうなるか。**空（`{}`）のままでも壊れません。
自動化が Status を動かしたとき、`tracker.unknown_state_grace_ms` の猶予を置いてからエージェントを止めます。
**つまり「PR を作ってから CI の直しを続ける」流れでは、途中で止まります。**

**何を書くか。**`tracker:` の下に、`automated_state_rewrite` の対応表を足します。
**左が、自動化が書き込む Status 名です。右が、戻したい Status 名です。**

```yaml
tracker:
  active_states: ["AI Ready", "AI In Progress"]
  automated_state_rewrite:
    "In Progress": "AI In Progress"
    # 左：自動化が書き込む Status 名（カンバンの選択肢と1文字ずつ合わせる）
    # 右：戻したい Status 名（必ず active_states の中から選ぶ）
```

**この例は、カンバンの Status を `AI Ready` / `AI In Progress` のように先に改名してある人のものです。**
`continuo init` が置いた雛形の `active_states: ["Ready", "In Progress"]` のままで、
**`automated_state_rewrite` の行だけを写しても起動しません。**
左の `In Progress` が `tracker` の他のキーに出てくる Status だからです（下の表の2行目）。

**書けない形は5つあります。**どれも `continuo doctor` の `設定ファイル` の行が `✗` になり、起動しません。
**弾く条件の正は [internal/config/validate.go](../internal/config/validate.go) の
`validateAutomatedStateRewrite` の1箇所です。下の表は、その写しです。**

| 書けない形 | なぜ |
| --- | --- |
| **左と右が同じ** | 同じ値の書き込みは省かれるので、巡回のたびに書きに行き続けます |
| **左が、`tracker` の他のキーに出てくる Status** | その行は一度も引かれません。引くのは continuo が知らない Status になったときだけです |
| **右が `tracker.active_states` の外** | 書き戻した直後に、continuo 自身がその run を終わらせます |
| **大文字小文字だけが違う左が2つ** | どちらに当たるかが、実行のたびに変わります |
| **左が空、または右が空** | Status 名として存在しません |

**「`tracker` の他のキー」は6つです。**`active_states` / `terminal_states` / `running_state` /
`dispatch_state` / `failure_state` / `status_signal_map` の遷移先。
**`tracker` の外（`cleanup` など）は見ません。**

**足す場所と、当てたあとの確かめ方は [upgrading.md](upgrading.md) の「足す場所と中身」にあります。**

**書き戻しても自動化が書き直す押し合いになると、continuo は途中で書き戻しをやめます。**
そこから先はいままでどおり、猶予を置いてエージェントを止め、
issue のコメントで `Workflows` を切る手を案内します。
**何回でやめるかは、[upgrading.md](upgrading.md) の
「`tracker.automated_state_rewrite` — 自動化に動かされた Status を戻す」にあります。**

**左に何を書けばよいか分からないときは、書かなくて構いません。**
次に自動化が Status を動かしたとき、continuo が issue のコメントに
**「この2行を足してください」とそのまま貼れる形で書きます。**

**書き換えたら continuo を再起動してください。**動いている最中は設定を読み直しません。

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
