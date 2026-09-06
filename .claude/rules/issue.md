# issue の扱い

## 絶対条件：issue を作ることと、着手することは別

**issue を作ったら、そこで止まる。**
**いまある issue 全部の中でグループ化し、カンバンへ載せ、着手順序へ並べ替えてから、人間の指示を待つ。**
**（continuo が起動したエージェントは、カンバンを触らない。下の「手順」の冒頭）**

**2026-08-30、README の件で issue を2本作り、確認を待たずに着手した。**
ユーザー指摘: **「issueを作ることと、そのissueをすぐ進めることは違うからな。
issueは優先順位を計画して人間確認してから着手すること」**

## 手順

**ここでいう AI は、人間と直接やりとりしているエージェントである。**
**continuo が起動したエージェントは、この節が言う「カンバンの操作」をしない。**
issue をカンバンへ載せることも、Status を付けることも、並べ替えることもしない
（載せることと Status は [docs/plans/continuo_design.md:9430](../../docs/plans/continuo_design.md#L9430)、
並べ替えは [docs/plans/continuo_design.md:9617-9621](../../docs/plans/continuo_design.md#L9617-L9621) の 4-4 の表に
「continuo が起動したエージェント」の行が無いこと）。
**ただし「1バイトも触らない」ではない。**設計は、そのエージェントが自分で `gh` を叩いて
`In Progress` → `Blocked` を動かす経路を認めている
（[docs/plans/continuo_design.md:9436](../../docs/plans/continuo_design.md#L9436)）。
**組み込みの指示書は、それを勧めてはいない**
（[internal/prompt/builtin.md:218](../../internal/prompt/builtin.md#L218) は「あなたが `gh` を叩く必要はありません」）。
そちらは応答の最後に `CONTINUO-STATUS:` の1行を書くだけで、Status を動かすのは continuo である。

**カンバンの操作は AI が行う。**ただし 4-1 の遷移表で「誰が」の欄が「人間」だけの3つは人間である
（[docs/plans/continuo_design.md:9428-9440](../../docs/plans/continuo_design.md#L9428-L9440)）。

| 遷移 | いつ |
| --- | --- |
| **`Ice Box` → `Ready`** | 着手を決めたとき |
| **`Blocked` → `Ready`** | コメントで回答したとき |
| **`In Review` → `Done`** | レビューを終えたとき |

**この3つを AI が動かすと、人間が1度も見ていない pull request が `Done` になり、
人間が答えていない `Blocked` が実行の対象へ戻る。**
**`Ice Box` → `Ready` を人間に残す理由は、下の段7 の説明にある3つである。**
**残る2つを人間に残す理由は、すぐ上の段落に書いた。**だから段ごとに「誰がやるか」を書く。

| 順 | 誰がやるか | 何をするか |
| --- | --- | --- |
| **1** | **AI** | **issue を作る。**依頼は非同期で来るので、**まとめる部分は workflow で並列に進める** |
| **2** | **AI** | **いまある issue 全部の中でグループ化し、着手順序を出す** |
| **3** | **AI** | **カンバンへ載せ、`Ice Box` を付ける。**代表も代表以外も `Ice Box` である。載せないと continuo は永久に拾わない |
| **4** | **AI** | **`Ice Box` の item の並び順を、段2 で出した着手順序へ並べ替える。**`Ready` と `In Progress` の item は動かさない（下の「守ること4つ」） |
| **5** | **AI** | **止まる。**着手順序を人間へ示す |
| **6** | **人間** | **着手する issue を指示する** |
| **7** | **人間**（GitHub の画面） | **その issue を `Ice Box` から `Ready` へ上げる**（グループなら代表だけ） |
| **8** | **AI** | **`Ready` へ上がったことを確かめてから着手する。**例外は下の「確認を待たずに着手してよい場合」だけである |

**段3 を飛ばすと、issue は作られたのにパイプラインから消える。**
[docs/plans/continuo_design.md:34](../../docs/plans/continuo_design.md#L34) が
「**カンバンへ載せて `Ice Box` を付けるのは continuo の外で1回行う**」「continuo はカンバンに載っていない issue を見ない」
と書いているとおり、**continuo 自身はこの操作をしない。**
**やるのは AI である。**人間へ「載せてください」と渡さない。

**段7 を飛ばすと、continuo は何も dispatch しない。**`tracker.active_states` の既定は
`Ready` と `In Progress` の2つで（[internal/config/default.go:107](../../internal/config/default.go#L107)）、
**`Ice Box` は入っていない。**段3 で issue を `Ice Box` へ置く以上、上げる段が要る。
**上げるのは人間で、GitHub の画面から行う**
（[docs/plans/continuo_design.md:9431](../../docs/plans/continuo_design.md#L9431) の 4-1 の遷移表）。
**AI は、人間に名指しで頼まれない限り、ここを代行しない。**理由は3つある。

| 何が | なぜ |
| --- | --- |
| **利用者への約束である** | [SECURITY.md:50](../../SECURITY.md#L50) が「**`Ready` へ動かすのは人間です。知らない issue を動かさないでください**」を、公開された安全対策として書いている。**AI が上げられるようにすると、この約束が空文になる** |
| **段5 と段6 の裏付けである** | 段5 で AI が止まり、段6 で人間が指示する。**段7 はその指示を機械の読める形にしたものなので、指示が無いまま段7 が起きると、段5 と段6 が飛ばされたことになる** |
| **実機が動き出す** | 上がった瞬間に continuo が dispatch し、Claude Code が起動してレートリミットの枠を消費する。**取り違えると、人間が知らないうちに機械が動く** |

**人間が名指しで依頼したときだけ、AI が代行してよい。**「代わりに上げておいて」と言われた場合である。
**このとき決めているのは人間のままなので、上の約束は空文にならない。**
**言われていないのに上げてはならない。**

**段4 の並び順は、`updateProjectV2ItemPosition` で動かす**（[docs/plans/continuo_design.md:9474](../../docs/plans/continuo_design.md#L9474) の 4-2）。
着手順序の逆順に「先頭へ送る」（3つ目の引数 `afterId` を省く）を繰り返すと、最後に送ったものが1位になる。
**引数名は 2026-09-04 に読み取りだけの introspection で確かめた**（`UpdateProjectV2ItemPositionInput` の入力に `afterId` がある）。

**この書き込みは worker へ渡さない**（[CLAUDE.md:334](../../CLAUDE.md#L334) の「**不可逆な操作**…**は worker に渡さない**」）。**メインエージェントが自分で叩く。**

**守ること4つ。**

| 何を | なぜ |
| --- | --- |
| **書き込みの間は1秒空ける** | GitHub が変更を伴うリクエストに求めている（[docs/plans/continuo_design.md:4316](../../docs/plans/continuo_design.md#L4316)）。104件の全並べ替えで約2分かかる |
| **`updateProjectV2Field` は絶対に呼ばない** | [CLAUDE.md](../../CLAUDE.md) の「GitHub Projects v2 の project #3 は本番のカンバンである」。**Status の値が全部消える** |
| **段4 のあと、`Ice Box` の item はカンバン全体の先頭に並ぶ** | そのため段7 で `Ready` へ上げた item は、前から待っている `Ready` の item より先に dispatch される。**それが着手順序どおりなので、そのままでよい** |
| **動かすのは `Ice Box` の item だけにする** | **並び順は project 全体で1本しかない**（[docs/plans/continuo_design.md:9507](../../docs/plans/continuo_design.md#L9507)）。「先頭へ送る」はカンバン全体の先頭へ送る。**`Ready` や `In Progress` の item を動かすと、走っている continuo が次に dispatch する issue が変わる**（[internal/orchestrator/dispatch.go:171-175](../../internal/orchestrator/dispatch.go#L171-L175) が「返ってきた配列の順序をそのまま使う」と書いている。**同じ行のコメントは「並び順を決めるのは人間である」と続くが、それは 3-30 の旧い見出しのままで、正は [docs/plans/continuo_design.md:4257](../../docs/plans/continuo_design.md#L4257) の本文である**） |

**段2 の着手順序は、2箇所へ出す。**

| 何を | どこへ |
| --- | --- |
| **全 issue の着手順序** | **人間へチャットで示す** |
| **グループごとの計画** | **そのグループの代表の issue のコメントへ残す**（[docs/plans/continuo_design.md:3577](../../docs/plans/continuo_design.md#L3577) の 3-26） |

**チャットだけに出すと、セッションが終わった時点で消える。**次のセッションが組み直すことになる。

## 確認を待たずに着手してよい場合

**次の2つを両方満たすときだけである。**

| 条件 | 確かめ方 |
| --- | --- |
| **レートリミットに余裕がある** | `maimuzo-dev-core` プラグインの `detect-usage-from-webapi` スキルを叩き、返る `limits` の `percent` が**全部50未満**であること。**`session` / `weekly_all` / `weekly_scoped` の3つとも見る。**`weekly_scoped` を外すと、モデル別の週次枠が尽きていても門を通る |
| **下の3つのどれかである** | typo の修正 / 文書だけの変更 / **既に人間が着手を指示したものの続き** |

**この3つに無いものは、余裕があっても着手しない。**
**「判断が要らないと思った」で広げてはならない。**広げると、上の絶対条件が3行で無効になる。

**飛ばしてよいのは、手順の段2・段4・段5・段6・段7 と、段8 の「`Ready` へ上がったことを確かめる」だけである。**
**段1・段3 は飛ばさない。**issue は作り、カンバンへ載せる。

**段2（グループ化と着手順序）と段4（並び順）を飛ばしてよいのは、
typo1件のために104件のカンバンを並べ替えるのが、この節の目的から外れるためである。**

**このとき issue は `Ice Box` のままである。**`tracker.active_states` に `Ice Box` は入っていないので
（[internal/config/default.go:107](../../internal/config/default.go#L107)）、**continuo はこの issue を dispatch しない。**
**直すのは、いま動いている AI 自身である。**continuo に回したくなったら、人間が手順の段7 で `Ready` へ上げる。

## グループ化するときにやること

| 順 | 誰がやるか | 何をするか |
| --- | --- | --- |
| **1** | **AI** | 同一原因・同一ファイル・同一コンポーネントでまとめ、**代表を1つ決める** |
| **2** | **AI** | **計画を代表の issue のコメントに書く** |
| **3** | **AI** | **グループの代表以外のうち、`Ready` か `In Progress` に在るものを `Ice Box` へ落とす**（[docs/plans/continuo_design.md:9432](../../docs/plans/continuo_design.md#L9432) の 4-1 の遷移表）。`updateProjectV2ItemFieldValue` を叩く |
| **4** | **AI** | **代表以外を、代表の sub-issue にする。**`addSubIssue` を叩く（下の実例のとおり `GraphQL-Features: sub_issues` のヘッダを付ける。2026-09-04 時点では無くても schema に出るが、付けておく） |
| **5** | **AI** | **代表（とグループを持たない issue）を、リリース管理の issue の sub-issue にする。**無ければ1件立てる（下の「リリースに入れるものを、issue 1件で管理する」） |

**絶対条件：Status を外してはならない。**`clearProjectV2ItemFieldValue` を使わない。

**continuo は Status 未設定の item を、issue として組み立てる前に捨てる**
（[internal/tracker/query.go:1020-1030](../../internal/tracker/query.go#L1020-L1030) が `Gone` を返し、
[internal/tracker/by_identifier.go:102-104](../../internal/tracker/by_identifier.go#L102-L104) が
**識別子を照合する前に `continue` する**）。
**外すと、エージェントが `CONTINUO-STATUS: #45 review` と書いても continuo がその issue を見つけられない。**
「カンバンに無いので動かせなかった」というコメントを残して捨て、
**グループの代表以外は永久に `In Review` へ上がらない。**

**代表と代表以外の見分けは、sub-issue が付ける**（この節の段4）。GitHub の画面で親子として表示される。

**この節の段3 の対象は、`Ready` か `In Progress` に在る issue である。**
手順の段2 は「**いまある issue 全部**」を見るので、**前のセッションで `Ready` や `In Progress` へ上がったものがグループに入りうる。**

**この節の段3 を飛ばすと、continuo が代表とは別に dispatch する。**
[docs/plans/continuo_design.md:3600-3601](../../docs/plans/continuo_design.md#L3600-L3601) の 3-26 が
「落とさないと `active_states` に残るので、**continuo が代表とは別に dispatch してしまう。**
『自分が取った』印は代表にしか付かないため、印では防げない」と書いている。
**印で防げない以上、この規則が唯一の防波堤である。**
同じ修正を2つの worktree が並行して行い、片方の成果が黙って失われる。

## リリースに入れるものを、issue 1件で管理する

**次のリリースに何を入れるかは、issue を1件立てて管理する。**
**そこへ、グループの代表を sub-issue としてぶら下げる。**代表には、そのグループの代表以外をぶら下げる。
**3階層になる。**

```
リリース管理の issue（次のリリースに何を入れるかを決め、着手の進みを追う）
  ├─ グループの代表A
  │    └─ 代表A と一緒に直す issue
  ├─ グループの代表B
  │    ├─ 代表B と一緒に直す issue
  │    └─ 代表B と一緒に直す issue
  └─ 単独の issue（グループを持たないもの）
```

### 各階層が持つもの

| 階層 | 何 | 何を持つか | カンバンの Status |
| --- | --- | --- | --- |
| **1** | **リリース管理の issue** | **着手順序のリスト。**リストの並び順が、そのまま着手順序である | `Ice Box`。カンバンの先頭に置く |
| **2** | **グループの代表** | そのグループの計画（一緒に直す issue・共通の原因・着手の順番・[.claude/rules/design-review.md:9-17](design-review.md#L9-L17) の9段） | `Ice Box` |
| **2** | **グループを持たない issue** | **その issue 1件の計画** | `Ice Box` |
| **3** | **代表以外** | **何をすべきかのコンテキストだけ。**continuo は処理しない | `Ice Box`。**Status を外してはならない** |

**リリース管理の issue の本文には、着手順序をリストで書く。**
**リストの順番と、カンバンの `Ice Box` の中の並び順を揃える。**片方だけ直すと食い違う。
**`Ready` へ上がったものは動かさないので、そのぶんは揃わない**（上の「守ること4つ」で禁じている。動かす手段はある）。

### 関連付けのしかた

```bash
# 親の node id と、子の node id を取る
PARENT=$(gh issue view <親の番号> --json id --jq .id)
CHILD=$(gh issue view <子の番号> --json id --jq .id)

# ぶら下げる（GraphQL-Features のヘッダは、2026-09-04 時点では無くても schema に出る）
gh api graphql -H "GraphQL-Features: sub_issues" \
  -f query="mutation { addSubIssue(input: {issueId: \"$PARENT\", subIssueId: \"$CHILD\"}) { subIssue { number } } }"
```

**書き込みの間は1秒空ける**（[docs/plans/continuo_design.md:4316](../../docs/plans/continuo_design.md#L4316)）。

**確かめ方。**

```bash
gh api graphql -H "GraphQL-Features: sub_issues" \
  -f query='query { repository(owner:"<owner>", name:"<repo>"){ issue(number:<親の番号>){ subIssues(first:20){ nodes { number } } } } }' \
  --jq '[.data.repository.issue.subIssues.nodes[].number] | join(", ")'
```

### 代表以外が処理される流れ

**代表以外は、continuo が dispatch しない。**`Ice Box` は `tracker.active_states` の既定
（`Ready` と `In Progress`。[internal/config/default.go:107](../../internal/config/default.go#L107)）に入っていないためである。

| 順 | 何が起きるか |
| --- | --- |
| 1 | 人間が代表を `Ready` へ上げる |
| 2 | continuo が代表を dispatch する |
| 3 | **エージェントが、代表と代表以外を1つの worktree でまとめて直す** |
| 4 | pull request の本文の `Closes #NNN` で、マージ時にまとめてクローズされる |

**代表以外には、何が直ったかが1行も残らない。**
組み込みの指示書（[internal/prompt/builtin.md:361-372](../../internal/prompt/builtin.md#L361-L372) の 7-2）が、
表明の1行と `Closes #45` しか書かせていないためである。
**人間が代表の pull request を見て確かめること。**

**クローズ後、代表以外の Status がどうなるかは測っていない。**GitHub Projects v2 には、項目を閉じたときに
Status を動かす組み込みの自動化があり、有効かどうかはカンバンごとに違う。**GitHub の画面で確かめること。**
**`Ice Box` に残るなら、人間がカンバンの表示でクローズ済みを隠す。**
**`Ice Box` → `Done` は 4-1 の遷移表に無いので、規則としては求めない。**

---

## 着手順序を組むときの観点（6つ）

| 観点 | 中身 |
| --- | --- |
| **閉じられるものを先に外す** | **現行コードと突き合わせ、既に直っているものを一覧から外す。**issue の題名だけで判断しない |
| **古いものを先に** | **放置するとソースとの乖離が進む** |
| **構造の問題を先に** | **後に回すほど修正コストが上がる** |
| **簡単なものは並行して** | **issue を貯めない** |
| **利用者向けを優先** | 利用者が困っている問題、README など新しい利用者向けのもの |
| **着手中を仕上げてから次を足す** | **同時に進める issue は2か3**（下の節） |

## 同時に進める issue の数

**同時に進める issue は2か3までとする。**レートリミットによって決める。

**これは continuo の設定 `agent.max_concurrent_agents`（既定2）とは別物である。**
あちらは **continuo が同時に走らせる Claude Code の数の上限**であり
（[internal/config/types.go:312-313](../../internal/config/types.go#L312-L313)）、
こちらは**人間と AI が同時に抱える issue の数**である。
**片方を変えても、もう片方は変わらない。**
**この節を読んで `agent.max_concurrent_agents` に手を入れてはならない。**

| 何 | どうするか |
| --- | --- |
| **着手中のもの** | **仕上げてマージすることを優先する** |
| **1つ手放せたら** | 優先度の高いものを次に置く |
| **人間判断待ちになったら** | **その issue をいったん手放して、他の issue を進める** |
| **並列の数** | **無駄に増やさない。**衝突の危険が上がる |

## 閉じられるかの確かめ方

**issue の本文だけで「未修正」と判断してはならない。**
**issue は修正後もクローズされずに残ることがある。**

| 順 | 何をするか |
| --- | --- |
| 1 | **issue が指す原因箇所を、現行コードで開く** |
| 2 | **症状を起こすコードが、いまも在るかを確かめる** |
| 3 | **修正方針が列挙されているなら、各項目が実装済みかを1つずつ照合する** |
| 4 | **起票日以降に、その場所へ入った commit を確かめる** |
| 5 | **症状に対応するテストが足されていないかを確かめる** |

**「完全に修正済み」なら、優先度の一覧から外し、閉じる提案として別に出す。**

## issue の題名

**症状を書く。**内部の作りの名前を書かない。

| 書く | 書いてはいけない |
| --- | --- |
| **エージェントが書き間違えた issue 番号で、別のエージェントの作業が止まる** | エージェントの表明で、別の run が担当中の issue の Status を動かせる |

**この見本は #80（エージェントが書き間違えた issue 番号で、別のエージェントの作業が止まる）の題名である。**
**表には番号を入れない。**題名そのものの見本だからで、番号は GitHub が別に表示する。

**読む人が、題名だけで「何が起きるか」を思い浮かべられる形にする。**

**題名の編集履歴は消せない。**個人の絶対パス・tailnet のホスト名・トークンは題名に書かず、本文へ書く
（[CLAUDE.md](../../CLAUDE.md) の「5. 公開してよい情報かを常に判断する」）。

**書いてしまったときにどこまで消せるか。**

| 何 | 消せるか |
| --- | --- |
| 本文・コメントの中身 | **書き換えられる** |
| **その編集履歴の版** | **GitHub の画面から1つずつ消せる。**API は無い |
| **題名の変更履歴** | **消せない。**issue ごと削除するしかない |

**本文に書いてしまったら、書き換えるだけでは足りない。**
**GitHub の画面から、その編集履歴の版も1つずつ消すこと**（API では消せない）。
**題名の変更履歴は消せない。**issue ごと削除するしかない。
**トークンを書いてしまったときは、消す前に必ず無効化すること。**消せたかどうかに関わらず。
