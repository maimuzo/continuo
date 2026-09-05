# worktree の branch の食い違いを、起こさない／起きたら案内する

**この文書は [docs/plans/continuo_design.md](../continuo_design.md) の 3-66 と 3-69 を実装へ落としたものである。**
**食い違ったら詳細版（continuo_design.md）が正である。**実装のとき、この文書の決定を
3-22 / 3-66 / 3-69 / 5-3 へ書き戻す。

対象は maimuzo/continuo の #142（worktree が別の branch を出していると永久に飛ばされる）と
#144（worktree の branch は変えず push 先だけ分ける運用へ揃え、リンクされた branch を base に使う）である。

**この文書が正なのは #142 の分（0-5、16、17）だけである。**
**#144 の分（6-14）は [docs/plans/impl/issue144_branch_and_push.md](issue144_branch_and_push.md) に置き換わった。**
あちらは4つのユースケースを通す形に書き直してあり、base の決め方も push 先の決め方も違う。
**#144 を実装するときは、この文書の 6-14 ではなくあちらを見ること。**

**前提。**remote の名前は `origin` 1つとする。continuo は remote 名を設定で受け取る仕組みを持っていない
（`grep -rn 'remote' internal/config/types.go` が0件）。

---

## 0. いまの状態：#142 は出せる。#144 の base は人間の判断待ち

**言いたいこと。**設計のレビューを3回回した。**#142 の側は指摘が0になり、そのまま実装してよい。**
**#144 の「リンクされた branch を base にする」は、既定では1件も発火しないことが分かった。**
**捨てるか、`git fetch` を足して発火させるかは、人間が決める**（14 を見よ）。

| 塊 | いまの状態 | 触るもの |
| --- | --- | --- |
| **#142 の番兵と文面**（3-5） | **実装してよい。**3回目のレビューで指摘0 | 6ファイル |
| **雛形に「branch を変えるな」を足す**（6） | **実装してよい**（fetch の1行を足す形で） | 3ファイル |
| **リンクされた branch を base にする**（8-12） | **判断待ち。**既定では発火しない | 10ファイル以上 |

**なぜ発火しないか。****continuo は `git fetch` を1回も呼ばない**
（`grep -rn '"fetch"' --include='*.go' internal/` が0件）。
`gh issue develop` が作る branch は GitHub の側にできるだけで、**ghq の clone の `refs/heads/` には入らない。**
**実在することを唯一の安全弁にしている以上、既定では条件が満たされない。**
**しかも満たされなかったことは WARN のログ1行にしかならず、
[docs/plans/continuo_design.md](../continuo_design.md) 3-68 が「ログは pane を見ていない限り誰にも届かない」と名指しした手段そのものである。**

**この文書は 8-12 も最後まで書いてある。**判断が「出す」なら、そのまま実装できる形にしてある。

---

## 1. #142: 専用の番兵を作る

**言いたいこと。**いまは `registered == true` を確かめた直後の分岐が「登録されていません」と名乗る。
**読んだ人間は [docs/FAQ.md:413](../../FAQ.md#L413) の別の症状（ディレクトリだけが残っている）へ行き、
生きている worktree を消しにいく。**

| 何 | 実測 |
| --- | --- |
| 番兵の定義 | [internal/workspace/prepare.go:25](../../../internal/workspace/prepare.go#L25) の `errors.New` で日本語固定 |
| 包んでいる箇所 | 同 `:207-212`（`Prepare` の段2）と `:495-499`（`CheckWorktreeUsable`） |
| 残す箇所 | 同 `:226` と `:478`（登録の無い実体）。**`ErrUnregisteredWorktree` は消さない** |

```go
// ErrWorktreeBranchMismatch は worktree が期待と違う branch に載っていることを表す（issue #142）。
//
// **ErrUnregisteredWorktree では拾えない。**登録はされていて、載っている branch が違うだけである。
// **文面を分けないと、人間は「登録されていません」を読んで、生きている worktree を消しにいく。**
//
// **detached HEAD（ErrWorktreeDetached）とも分ける。**3-68 の通知が
// 「飛ばした理由の種類」を鍵に含めるので、2つを同じ番兵にすると数え直しが効かない。
var ErrWorktreeBranchMismatch = i18n.Sentinel(i18n.KeyWorkspaceErrWorktreeBranchMismatch)
```

**替えて落ちるテストは1件だけである。**`grep -rn 'ErrUnregisteredWorktree' --include='*.go' .` を叩くと
`errors.Is` は [test/internal/workspace/prepare_test.go](../../../test/internal/workspace/prepare_test.go) の
`:207` / `:340` / `:369` / `:467` の4箇所にある。**落ちるのは `:340`
（`TestPrepare_別のbranchへ切り替えられたworktreeは再利用しない`）だけ**で、
`:207` と `:467` は登録の無い実体、`:369` は「そうならない」ことの確認である。
**本番コードに `errors.Is(…, ErrUnregisteredWorktree)` は0件である。**

**判定の順番は変えない。**[internal/workspace/prepare.go:199](../../../internal/workspace/prepare.go#L199) は
`if head != loc.Branch.String() && m.isDetachedHead(...)` で、**branch 名の比較が先**、
真のときだけ `isDetachedHead` を見る。この形のままにする。

---

## 2. #142: 足す文言の実物

**言いたいこと。****引数の番号（`%[2]s`）を使い回してはならない。**
[test/internal/i18n/i18n_test.go:616-623](../../../test/internal/i18n/i18n_test.go#L616-L623) の `verbByArg` が
「引数の番号 %d が2回使われている」で落とす。**同じ値は、引数として何度も渡す。**

**触る i18n のキーは2つである**（**新設1つ・差し替え1つ**）。
[internal/i18n/keys.go](../../../internal/i18n/keys.go) の定数と、
末尾の `AllKeys` の並び（`:3031-3033` の近く）の**両方**へ足すこと。

| キー | 何 |
| --- | --- |
| `workspace.err.worktree_branch_mismatch` | 番兵そのものの文言（**新設**） |
| `workspace.prepare.branch_mismatch` | 既にある。**中身を差し替える** |

**[internal/i18n/messages/ja.json](../../../internal/i18n/messages/ja.json) に書く実物。**指定子は `%w` 1個・`%q` 2個・`%s` 6個の**計9個で、番号は使わない。**

```json
"workspace.err.worktree_branch_mismatch": "worktree が期待と違う branch に載っています",
"workspace.prepare.branch_mismatch": "%w: %s は %q をチェックアウトしています（期待は %q）\n【確かめ方】git -C %s status\n【よくある原因】issue の本文が別の branch を指していて、エージェントがそこへ切り替えた / 1つの issue で2本目の PR を出すために新しい branch を切った\n【対処】未コミットの変更と、push していない commit があるかを先に確かめてください。\n        いまの branch の作業が要るなら、期待の branch へ戻したあとでマージしてください。\n        期待の branch が残っているとき: git -C %s switch %s\n        期待の branch が消えているとき: git -C %s switch -c %s\n【注意】continuo abandon は使わないでください。worktree の中の作業が消えます。\n        --force を付けなくても、条件が揃うと Status を failure_state へ動かします。"
```

**呼び出し側は、同じ値を9回に分けて渡す**（`prepare.go:207-212` と `:495-499` の両方）。

```go
return nil, i18n.Errorf(
    i18n.KeyWorkspacePrepareBranchMismatch,
    ErrWorktreeBranchMismatch, loc.Path, head, loc.Branch.String(),
    loc.Path, loc.Path, loc.Branch.String(), loc.Path, loc.Branch.String())
```

**[internal/i18n/messages/en.json](../../../internal/i18n/messages/en.json) 側は `%[1]w` … `%[9]s` の明示の番号で書く**
（既にある `workspace.prepare.detached_head` の英語と同じ書き方。**番号は1回ずつしか使わない**）。
**`en.json` を直したら `_source_sha256`（[internal/i18n/messages/en.json:2](../../../internal/i18n/messages/en.json#L2)）も入れ直す。**
[test/internal/i18n/i18n_test.go:298-302](../../../test/internal/i18n/i18n_test.go#L298-L302) が
`shasum -a 256 internal/i18n/messages/ja.json` の値と突き合わせて落とす。

**`errors.New` に日本語を足してはならない。**
[test/internal/testdesign/no_japanese_messages_test.go:79](../../../test/internal/testdesign/no_japanese_messages_test.go#L79) の
`japaneseAllowance` は `"internal/workspace": 19,` と決めており、**この変更で19のまま動かない。**

---

## 3. #142: 直す doc コメントは4箇所

**言いたいこと。****この変更の目的は「名乗りと実物を合わせる」ことである。**
番兵を替えたのに doc コメントが `ErrUnregisteredWorktree` を名乗り続けると、新しい嘘が残る。

| 場所 | いま何と書いてあるか |
| --- | --- |
| [internal/workspace/prepare.go:55-56](../../../internal/workspace/prepare.go#L55-L56) | `ErrWorktreeDetached` の説明が**旧文面**（`… は "HEAD" をチェックアウトしています`）を引いている |
| 同 `:138-141` | `Prepare` の戻り値の説明が「別の branch を出している（ErrUnregisteredWorktree）」 |
| 同 `:426-429` | `CheckWorktreeUsable` の一覧が「別の branch を出している → ErrUnregisteredWorktree」 |
| 同 `:435-436` | 同じ関数の戻り値の説明が `ErrUnregisteredWorktree・ErrWorktreeDetached・ErrBranchInUseElsewhere` |

---

## 4. #142: FAQ と upgrading に足す

**言いたいこと。**FAQ の節の題は、**番兵の固定の文言をそのまま使う。**
隣の2つの節がそうなっており、人間は画面に出た文字列で検索するためである。

```markdown
### 着手が「worktree が期待と違う branch に載っています」で止まる
```

**置く場所は、detached HEAD の節の「後ろ」である。**
[docs/FAQ.md:431](../../FAQ.md#L431) の `**上の節（ディレクトリだけが残っている）とは違います。**` が
指す「上の節」がずれないようにするためである。**前に差し込むと、この案内が新しい節を指してしまう。**

**[docs/upgrading.md](../../upgrading.md) にも節を足す。**
detached HEAD のときは [docs/upgrading.md:77-108](../../upgrading.md#L77-L108) に
「detached HEAD の worktree で出るメッセージが変わりました」の節が立っている。**同じ形で1つ足す。**

**detached HEAD と1つにまとめない**（#142 の5-3 の案は採らない）。

| なぜ分けるか | 根拠 |
| --- | --- |
| **原因が違う** | detached は rebase / bisect / SHA の直接チェックアウト。食い違いはエージェントの切り替え |
| **3-68 が種類で数える** | 3-68 は鍵に「飛ばした理由の種類」を含める。同じ番兵にすると数え直しが効かない |

---

## 5. #142: 触るもの（この塊だけで完結する）

| ファイル | 何を |
| --- | --- |
| [internal/workspace/prepare.go](../../../internal/workspace/prepare.go) | 番兵の新設・`:207-212` と `:495-499` の差し替え・doc コメント4箇所（3 を見よ） |
| [internal/i18n/keys.go](../../../internal/i18n/keys.go) | キー1つ新設・`AllKeys` へ追記 |
| [internal/i18n/messages/ja.json](../../../internal/i18n/messages/ja.json) | 1つ新設・1つ差し替え |
| [internal/i18n/messages/en.json](../../../internal/i18n/messages/en.json) | 同上・**`_source_sha256` を入れ直す** |
| [docs/FAQ.md](../../FAQ.md) / [docs/upgrading.md](../../upgrading.md) | 節を1つずつ（4 を見よ） |
| [test/internal/workspace/prepare_test.go](../../../test/internal/workspace/prepare_test.go) | `:340` を `ErrWorktreeBranchMismatch` へ |

---

## 6. #144: 雛形に足す3段落（**ここから 14 までは置き換わった。**[docs/plans/impl/issue144_branch_and_push.md](issue144_branch_and_push.md) が正）

**言いたいこと。****足すのは「切り替えるな」だけである。**push の話は1文字も足さない（7 を見よ）。
**見出し（`##`）を新しく作らない。**貼り先は `## 終わったらやること`（
[internal/scaffold/template.go:327](../../../internal/scaffold/template.go#L327)）の中であり、
**`##` を差し込むと後ろの3段落が新しい見出しの下へ落ちる。**

**backtick を1つも使わない。**貼り先は Go の raw string literal で、
[internal/scaffold/template.go:335-343](../../../internal/scaffold/template.go#L335-L343) は
backtick を入れるために `"`" + `review` + "`" +` の形で文字列を毎回切っている。
**切り方を1文字でも間違えるとコンパイルが通らない。**コマンドは4字下げの行で書く。

**`fetch` を必ず1行入れる。**continuo は fetch を叩かず、いまの雛形にも fetch の指示が1行も無い。
**`git merge origin/<その branch>` だけを書くと、その ref が無くて必ず落ちる。**

**`**push 先は、この issue のために作られた branch です。**` で始まる段落の「直前」に、次を足す。**
**その段落自身と、後ろの段落は1文字も触らない。**

```markdown
**continuo が用意した worktree と branch のまま作業してください。**
別の branch へ checkout したり、新しい branch を作ったりしないでください。
**切り替えると、次の巡回から continuo がこの issue に着手できなくなります。**

**issue やコメントで「別の branch の続きをやれ」と言われた場合も、切り替えないでください。**
その branch の内容が要るなら、先に取ってきてから、この worktree へマージしてください。

    git fetch origin <その branch>
    git merge FETCH_HEAD

中身を読むだけなら、別の場所へ一時的に checkout して参照し、読み終わったら消してください。

    git fetch origin <その branch>
    git worktree add --detach /tmp/<任意の名前> FETCH_HEAD
    git worktree remove /tmp/<任意の名前>
```

**同じ本文を [docs/plans/continuo_design.md](../continuo_design.md) の 5-3 の markdown ブロックへも、
1行の違いも無く入れる。**通す検査は2つである。

- [test/internal/scaffold/design_template_test.go:111-129](../../../test/internal/scaffold/design_template_test.go#L111-L129) の `assertSameBody`
- `TestTemplate_雛形の本文が設計5_3の本文と一致する`（[docs/plans/continuo_design.md:9950](../continuo_design.md#L9950) が名指ししている。**いまの名前は `TestTemplate_組み込みのプロンプトが設計5_3と一致する` である**）

**[test/internal/prompt/blocked_push_test.go:14-18](../../../test/internal/prompt/blocked_push_test.go#L14-L18) が探す
`を出す前に、必ず commit して push してください。` と `git push -u origin HEAD` は、どちらも1文字も触らない。**

---

## 7. #144: push 先を分ける話は、この変更に入れない

**言いたいこと。****`git push origin HEAD:<別名>` は upstream を張らない**（張るのは `-u`）。
**片付けは upstream が無いと base との差分を見る**ので
（[internal/workspace/cleanup.go:774-781](../../../internal/workspace/cleanup.go#L774-L781)）、
**成果を別の branch へ押した worktree は必ず見送られ、永久に片付かない。**

**これは新しい判断であり、既にある決定ではない。**
[docs/plans/continuo_design.md](../continuo_design.md) 5-3b が凍結しているのは
「push できないときの行き先」「commit するものが無いとき」「PR を作らせるか」「`working` の毎 turn の push」の**4つだけ**で、
**「push 先を分けてよいか」はそこに入っていない。**
**入れるなら 5-3b の5つ目として足し、同時に 3-9 の片付けの前提（upstream で判定する）も直す。**14 の1つ目である。

---

## 8. #144: リンクを base に使うのは、新しく作るときだけ

**言いたいこと。****`resolveBase` をそのまま書き換えてはならない。**
あれは**再利用の経路からも呼ばれ、その戻り値が身元ファイルの `base` を上書きする。**
**リンクを後から付けても、リンクから作った直後の巡回でも、記録が黙って化ける。**

**両方向とも壊れる。**[internal/workspace/prepare.go:243-250](../../../internal/workspace/prepare.go#L243-L250) は
再利用でも `resolveBase` を呼び、その値が
[internal/orchestrator/dispatch.go:839](../../../internal/orchestrator/dispatch.go#L839) の `Base: prepared.Base.String()` から
[internal/workspace/identity.go:457-462](../../../internal/workspace/identity.go#L457-L462) へ流れる。

| 向き | 何が起きるか |
| --- | --- |
| **後からリンクを付けた** | `main` で作った worktree の記録が `feature/x` に化ける。**差分が消え、片付けが「失うものはありません」と答えて消す** |
| **リンクから作った** | 次の巡回で記録が `main` に戻る。**リンク元の commit が丸ごと差分に出て、永久に片付かない** |

**`identity.go:458-460` のコメントは既に事実と違う**（「再利用のときは base を作り直せない」と書いてあるが、
`prepare.go:247` が作り直している）。**このコメントに乗ったまま設計を足すと、上書きが見えない。**

**採る形は3つ。**

| 何を | どうする |
| --- | --- |
| **記録は作成時に確定させる** | [internal/workspace/identity.go:457](../../../internal/workspace/identity.go#L457) を `if existing.Base != "" { fresh.Base = existing.Base }` にする。**一度書いた base は、再利用で上書きしない** |
| **リンクを見るのは新規作成だけ** | `resolveBase(issue)` はそのまま残し、`resolveCreateBase(ctx, repoPath, loc, issue)` を新設して `prepare.go:229` だけが呼ぶ |
| **記録するのは実際に切った元だけ** | `prepare.go:233` の無条件の `result.Base = base` を**消す**。9 の `usedBase` が真のときだけ入れる |

```go
// resolveCreateBase は worktree を新しく作るときの base を決める（issue #144）。
//
// **リンクされた branch を見るのはここだけである。**再利用の経路（記録の補い）で見ると、
// あとからリンクを付けただけで身元ファイルの base が変わり、片付けが失う量を測り違える。
func (m *Manager) resolveCreateBase(
    ctx context.Context, repoPath string, loc *Location, issue IssueRef,
) (normalize.SafeName, error)
```

---

## 9. #144: 記録する base は「実際に切った元」だけ

**言いたいこと。**[internal/workspace/git.go:361-368](../../../internal/workspace/git.go#L361-L368) は
branch が既にあれば `git worktree add <path> <branch>` を叩き、**base を1バイトも使わない。**
**それでも `resolveCreateBase` の値を記録すると、記録と実物が食い違う。**

**採る形。**`gitWorktreeAddOnce`（[internal/workspace/git.go:355-360](../../../internal/workspace/git.go#L355-L360)）に
**`usedBase bool` の戻り値を足し、`gitWorktreeAdd`（同 `:305-311`）が
2回の呼び出し（同 `:312` と `:338`）のうち最後の結果をそのまま返す。**

- **「branch が既にある」経路を通ったら偽**
- **壊れた ref を消して packed-refs 側が生き返り、2回目で「ある」経路になったときも偽**
- **`Prepare` が呼ぶのは `gitWorktreeAdd` である**（`prepare.go:237`）

```go
usedBase, err := gitWorktreeAdd(ctx, repoPath, loc.Path, loc.Branch, base, m.brokenRefPolicy())
if err != nil { return nil, err }
if usedBase {
    result.Base = base
}
// 偽なら空のままにする。下の `if result.Base == ""` が resolveBase(issue) で補い、
// それも失敗したら空のまま（片付けは「判定できないので消さない」と扱う。いまと同じ）。
```

**base を解決する前に `gitBranchExists` を先読みする。**真なら `resolveCreateBase` を呼ばず、
`resolveBase(issue)` の結果（失敗しても無視）だけを記録に使う。
**使いもしない base を引けないという理由で issue を `failure_state` へ落とさない。**

**先読みが失敗したら（`gitBranchExists` の第2戻り値がエラー）、`resolveCreateBase` を呼ぶ側に倒す。**
**先読みが真でも、`gitWorktreeAddOnce` の中の再判定が偽になることがある**
（`:361-370`。その間に branch が消えた場合）。**そのとき空の base が `-b` に渡ると `git` が落ちる。**
**先読みが真でも base は必ず決めておき、`usedBase` で記録の可否だけを分ける。**

---

## 10. #144: リンクを使う4つの条件

**言いたいこと。**`resolveCreateBase` は `Issue.LinkedBranches` を **`herdr.worktree.base` より上**に置く。
**下に置くと、`base` を書いている環境では1件も変えられず、#144 の目的（issue ごとに base を変える）を達しない。**

| 順 | 何を読むか |
| --- | --- |
| **1** | `IssueRef.LinkedBranches`（下の4条件を全部満たすとき） |
| 2 | `m.cfg.Herdr.Worktree.Base`（空でないとき。`resolveBase` へ委ねる） |
| 3 | `IssueRef.NativeRef["default_branch"]`。無ければ `ErrBaseUnknown` |

| 条件 | どう確かめるか |
| --- | --- |
| **`linkedBranches` が3件返らなかった** | 3件返ったら「3本以上ある」ので使わない |
| **`loc.Branch` と一致するものを除いて、残りがちょうど1本** | 自分自身のリンクは base として意味を持たない（その branch が実在すれば base は使われない） |
| **`normalize.Normalize` が1文字も変えなかった** | 変わったら**別名の branch を引く**。`feature/fix+1` は `feature/fix_1` になり、その名前の別物が実在しうる |
| **手元に `refs/heads/<名前>` として実在する** | `git -C <clone> show-ref --verify --quiet refs/heads/<名前>`（[internal/workspace/git.go:272-273](../../../internal/workspace/git.go#L272-L273) の `gitBranchExists` と同じ綴り） |

**クエリはリポジトリ名も取る。**[internal/tracker/query.go:54](../../../internal/tracker/query.go#L54) を
`linkedBranches(first: 3) { nodes { ref { name repository { nameWithOwner } } } }` にし、
**issue のリポジトリと一致しないリンクは落とす。**
**いまの受け皿は名前しか持たない**（同 `:343-353` の `rawRef` は `Name string` の1つだけ）ので、
**別のリポジトリに作られたリンクでも、同じ名前の branch が手元にあれば、その別物から切ってしまう。**

**なぜ `first: 3` か。**自分自身のリンクは多くて1本なので、**2件までしか返らなければ全部が見えており、
そこから自分を除いた数は正確である。**3件返ったら「3本以上ある」と分かるので、そこで打ち切る。
`first: 2` では、自分を除いて1本残ったときに「3本目があるかもしれない」を排除できない。

**段1 を使ったときは INFO を1行出す。**設定に `base` を書いている人が、黙って上書きされたことに気づけない。

```
level=INFO msg="リンクされた branch を base に使います（herdr.worktree.base より優先）" identifier=acme-inc/sample-app#123 base=feature/existing-work configured_base=develop
```

---

## 11. #144: 条件に外れたときは、黙って落とさない

**言いたいこと。****リンクを使えないときは、段2 へ進んで着手は続ける。失敗させない。**
着手を止めると #142 と同じ「永久に飛ばされる」を新しく作る。

| 外れ方 | どうする |
| --- | --- |
| **手元に無い** | WARN を1行出して段2 へ |
| **2本以上ある / リポジトリが違う** | 同上 |
| **正規化で名前が変わった** | 同上（`normalize` が出す警告（3-7）と合わせて1行） |

**3件とも `logger.Warn` で出し、i18n のキーは使わない。**
ログは資源へ載せない決まりであり（[test/internal/testdesign/no_japanese_messages_test.go:196-202](../../../test/internal/testdesign/no_japanese_messages_test.go#L196-L202) が
`logger` のメソッドを逃がしている）、**片方だけ資源に載せると、英語を選んだとき同じ機能の WARN が2言語で混ざる。**

**属性のキーは ASCII で綴る。**既存はすべて ASCII である
（[internal/workspace/prepare.go:318-319](../../../internal/workspace/prepare.go#L318-L319) の `"workspace_id"` / `"label"` / `"error"`）。

```
level=WARN msg="リンクされた branch が手元の clone に無いので base には使いません" identifier=acme-inc/sample-app#123 linked=feature/existing-work base=main hint="git -C /home/octocat/ghq/github.com/acme-inc/sample-app fetch origin feature/existing-work:feature/existing-work"
```

**`fetch origin <名前>:<名前>` と綴る。**`git fetch origin <名前>` だけでは `FETCH_HEAD` にしか入らず、
**`refs/heads/<名前>` はできない。**段1 の条件は次の巡回でも満たされない。

**remote-tracking ref（`refs/remotes/origin/<名前>`）は base に使わない。**
使うと `git worktree add -b` が upstream を `origin/<名前>` へ張り、
片付けの段2b（`git rev-list --count @{u}..HEAD`。
[internal/workspace/cleanup.go:751-765](../../../internal/workspace/cleanup.go#L751-L765)）が
**エージェントが `origin/continuo/…` へ押しても0にならず、永久に「押していない」と答える。**

---

## 12. #144: 型と、触るもの

**言いたいこと。**`BranchName` は `SPEC.md` 4.1.1 の項目として残し、
**base の判断には新しい `LinkedBranches` を使う。**`NativeRef` には入れない。

**[internal/tracker/tracker.go](../../../internal/tracker/tracker.go) の `Issue` に1つ足す。**

```go
// LinkedBranches は GitHub の "Development" でリンクされた branch のうち、
// **この issue と同じリポジトリのもの**の名前である
// （多くて3件。internal/tracker/query.go の linkedBranches(first: 3)）。
// **3件あれば「3本以上ある」という意味である**（全部ではない）。
// **BranchName とは必ずしも一致しない。**あちらは絞り込む前の先頭1件である。
// **draft issue では nil である**（リポジトリを持たないため）。
LinkedBranches []string
```

**[internal/workspace/workspace.go](../../../internal/workspace/workspace.go) の `IssueRef` に1つ足す。**`NativeRef` には入れない。
[internal/workspace/workspace.go:251-255](../../../internal/workspace/workspace.go#L251-L255) の注記が
**「このパッケージが読むのは "default_branch" の1キーだけである … 唯一の例外がここである」**と書いており、
**2つ目の例外を作るとその注記が嘘になる。**

**触るもの（5 の6ファイルに、これだけ足す）。**

| ファイル | 何を |
| --- | --- |
| [internal/workspace/prepare.go](../../../internal/workspace/prepare.go) | `resolveCreateBase` の新設・`gitBranchExists` の先読み・**`:233` の `result.Base = base` を消す**・`:15-18` の doc コメント（「base を推測しない」が 3-22 段4 を名指ししている） |
| [internal/workspace/git.go](../../../internal/workspace/git.go) | リンクの実在の確認・`gitWorktreeAdd` と `gitWorktreeAddOnce` の両方に `usedBase` の戻り値 |
| [internal/workspace/identity.go](../../../internal/workspace/identity.go) | `:457` を `if existing.Base != "" { … }` へ。**コメントも直す**（いまの文は事実と違う） |
| [internal/workspace/workspace.go](../../../internal/workspace/workspace.go) | `IssueRef.LinkedBranches` |
| [internal/tracker/tracker.go](../../../internal/tracker/tracker.go) / [query.go](../../../internal/tracker/query.go) | `Issue.LinkedBranches`・`first: 3`・`repository { nameWithOwner }`・`rawRef` に1フィールド |
| [internal/orchestrator/dispatch.go](../../../internal/orchestrator/dispatch.go) | `toIssueRef` へ写す（`prompt.go` は触らない。13 を見よ） |
| [internal/scaffold/template.go](../../../internal/scaffold/template.go) | 6 の3段落 |
| [docs/plans/continuo_design.md](../continuo_design.md) | **3-22 の段4**（base の決め方と「引けなければ失敗させる」）・**`:1414`** の同じ定義・3-66・3-69・5-3 の本文 |

---

## 13. #144: リンクをプロンプトへは渡さない

**言いたいこと。**push の指示を足さない以上、エージェントがリンクを使ってやることが無い。
**使い道の無い変数を先に公開すると、あとで意味を変えられない。**

**14 の1つ目が「分けてよい」で決まったときに足す。**
**`data` に鍵を足しても既存の WORKFLOW.md は壊れない**
（`missingkey=error` は参照した鍵が無いときだけ落ちる。
[internal/orchestrator/prompt.go:30](../../../internal/orchestrator/prompt.go#L30)）ので、後から足せる。

**雛形に `gh issue develop --list <番号>` を書く案は採らない。**
**出力の形が未検証である。**#144 の投稿者自身が「1本以上あるときの出力の形は確かめられていない」と書いている。

---

## 14. 人間に決めてもらうこと

**言いたいこと。**3回のレビューで #144 の base 側が収まらなかった。
**指摘を1件ずつ潰すのをやめ、2つの案を並べる。**

### 案の比較

| 案 | 何を出すか | 触るファイル |
| --- | --- | --- |
| **縮める** | **#142 の全部（1-5）と、雛形の3段落（6）だけ。**リンクを base にする話（8-12）は出さず、**「Development のリンクは base に使わない」と [docs/FAQ.md](../../FAQ.md) に書いて閉じる** | **9** |
| **いまのまま** | 8-12 も出す。**ただし `git fetch` を足さないと既定では1件も発火しない**（0 を見よ） | **16** |

**推すのは「縮める」である。**#144 の使い道 B（既存 branch の続き）は、
**push 先を分けてよいか（下の1つ目）が決まらないと成立しない。**
**base だけ先に入れても、エージェントは continuo の branch へ押すので、既存 branch には1 commit も載らない。**
**順番が逆である。**

### 判断が要る3つ

| 何 | 案 | 推す |
| --- | --- | --- |
| **push 先を分けてよいと雛形に書くか**（5-3b の5つ目になる） | (a) 書かない / (b) 書く。ただし 3-9 の片付けの前提も同時に直す | **いまは (a)。**(b) だけを入れると、押し先を分けた worktree は upstream が無く、`cleanup.require_pushed` に必ず引っかかって永久に片付かない |
| **リンクされた branch を fetch するか** | (a) しない。11 の WARN で人間へ渡す / (b) `git fetch origin <名前>:<名前>` を1回だけ叩く | **(a) なら 8-12 は出さない**（発火しないため）。**8-12 を出すなら (b) が要る** |
| **`herdr.worktree.base` とリンクの優先順** | (a) リンクが上（10 のとおり） / (b) `base` が上 | **(a)。**(b) だと `base` を書いている環境で1件も変えられず、#144 の目的を達しない |

**#144 は人間が「指示だと思ってよい」と明言したものである。**
**だから「対応しない」ではなく、「#142 と雛形を先に出し、base とリンクは push の判断が付いてから出す」を推す。**

---

## 15. 採らなかった案

| 案 | なぜ採らないか |
| --- | --- |
| **切り替えを認め、HEAD の branch 名を着手の可否に使わない**（3-69 の案2） | **人間が手作業していた worktree の上で、continuo が黙ってエージェントを起こす。**#144 が「判定を緩めるのではなく運用を1本の規則に収める」と書いており、提案者の意図とも逆である |
| **身元ファイルの `branch` を HEAD で書き換えて追随する**（3-69 の案3） | **continuo が自分の作った branch の名前を失う。**片付けが `git branch -D` に渡す名前を、接頭辞（3-9 の段6b）で判定できなくなる |
| **`git switch` / `git checkout` を hook で拒む** | **エージェントは `git` を直に叩ける。**止まらない検査を足すと「止まる」と誤解される |
| **`resolveBase` を1つのまま書き換える** | **再利用の経路からも呼ばれ、身元ファイルの `base` を上書きする**（8 を見よ）。片付けが失う量を測り違える |
| **remote-tracking ref を base に使い、`--no-track` で逃げる** | 引数が2つの関数に増え、`-b` を使わない経路で意味を持たない。**手元に無いリンクは使わない、で足りる** |
| **飛ばしたことを issue へ1回だけ知らせる**（#142 の6） | **3-68 が既に設計済みである。**3-68 は「理由の種類を見分けるには 3-66 の番兵の新設が先に要る」と書いており、**この変更がその前提を作る** |

---

## 16. 実装の記録（#142 と雛形の3段落だけを出した）

**言いたいこと。**1-6 だけを実装した。**7 以降（#144 の base とリンク）には1バイトも手を付けていない。**
作業した branch は `fix/issue142-branch-mismatch`。

### 16-1. 段2（設計を issue へ貼る）

**貼った。**https://github.com/maimuzo/continuo/issues/142#issuecomment-5493412686

中身は3つ。**何を直すか**（番兵の差し替えと足す文面の実物）・
**#144 の base の話をこの PR に入れない理由**（既定で発火しない／push 先の判断が先）・
**設計文書の場所**。

### 16-2. 実装した中身

| ファイル | 何をしたか |
| --- | --- |
| [internal/i18n/keys.go](../../../internal/i18n/keys.go) | `KeyWorkspaceErrWorktreeBranchMismatch`（`workspace.err.worktree_branch_mismatch`）を新設し、`AllKeys` にも足した |
| [internal/i18n/messages/ja.json](../../../internal/i18n/messages/ja.json) | 番兵の文言を新設し、`workspace.prepare.branch_mismatch` を9個の指定子の文面へ差し替えた |
| [internal/i18n/messages/en.json](../../../internal/i18n/messages/en.json) | 同上を `%[1]w` … `%[9]s` で書き、`_source_sha256` を入れ直した。**値はここに写さない**（[internal/i18n/messages/en.json:2](../../../internal/i18n/messages/en.json#L2) が正。`ja.json` を直すたびに変わるので、写すと必ず古くなる） |
| [internal/workspace/prepare.go](../../../internal/workspace/prepare.go) | `ErrWorktreeBranchMismatch` を新設。`Prepare` の段2 と `CheckWorktreeUsable` の2箇所を差し替え。doc コメント4箇所を直した |
| [internal/scaffold/template.go](../../../internal/scaffold/template.go) | 「切り替えるな」の3段落を `**push 先は、…**` の直前へ入れた |
| [docs/plans/continuo_design.md](../continuo_design.md) | 5-3 の本文へ同じ3段落 |
| [test/internal/workspace/prepare_test.go](../../../test/internal/workspace/prepare_test.go) | 別の branch のテストを新しい番兵へ。detached の側にも「取り違えていない」を1本足した |

### 16-3. 設計から変えたところ

**無い。**2 の文面・6 の3段落は、設計に書いた実物をそのまま入れた。

**足したものが1つある。**detached HEAD のテストへ
`if errors.Is(err, workspace.ErrWorktreeBranchMismatch)` の確認を1本足した。
**2つの番兵が混ざっていないことを、両方向から押さえるためである**（3-68 が種類で数える）。

### 16-4. 段6（draft の PR）

**作った。**https://github.com/maimuzo/continuo/pull/146
（「別の branch を出している worktree に、消させない案内を返す」）

**触ったのは9ファイルで、設計の 5 と 12 が数えた9と同じである。**
[docs/plans/continuo_design.md](../continuo_design.md) は 3-66 の状態・3-68 の前提の一文・3-69・5-3 の本文の4箇所。

**3-68 の前提の一文も直した。**「いまは branch の食い違いも登録の欠落も同じ番兵で包まれており、
2つを区別できない」は、この変更で事実でなくなった。**4つの番兵を名指しする文へ置き換えた。**

### 16-5. 文書に書いた版

**`v0.1.12` とした。**[docs/releasing.md](../../releasing.md) の「4. 版を決める」が
「直しと、省略できる設定の追加だけ → 末尾を1つ上げる」と決めており、
**この変更は設定のキーを1つも増やしていない。**手元の最新の tag は `v0.1.11`。

**[docs/upgrading.md](../../upgrading.md) には「本文に足すもの」として3段落を丸ごと載せた。**
`continuo init --force` は使えないので、貼る文面をこちらが書いて渡すしかない。

### 16-6. 段3（テスト）

**手元も CI も全部通った。**

| 何 | 結果 |
| --- | --- |
| `sh scripts/test-like-ci.sh` | **EXIT=0。**落ちた package は0件（`test/internal/orchestrator` が241秒でいちばん長い） |
| GitHub Actions（PR #146） | **build 4つと test 2つが全部 pass** |

**手元の worktree では `mise trust` が要った。**`.claude/worktrees/` の下に作られた worktree の
`mise.toml` は信頼されておらず、[scripts/test-like-ci.sh](../../../scripts/test-like-ci.sh) が組み立てる clean PATH の `go` の shim が
「Config files are not trusted」で落ちる。**テストが1件も走らないまま EXIT=1 になる。**
`mise trust <worktree>/mise.toml` を1回叩けば通る。

### 16-7. 段7（`/code-review` の結果と対応）

**correctness の欠陥は0件、low が3件。**結果は PR #146 のコメントへ貼った
（先頭に `<!-- code-review-result -->`）。https://github.com/maimuzo/continuo/pull/146#issuecomment-5493858175

| 指摘 | どうしたか | 理由 |
| --- | --- | --- |
| **段0 側の番兵にテストが無い**（`CheckWorktreeUsable` の `ErrWorktreeBranchMismatch`） | **直した** | daemon が実際に通るのは段0 である。9個の引数を2箇所へ手で写しており、`i18n.Errorf` は個数を検査しない。片方だけ直すと本物の文面だけが `%!s(MISSING)` になる |
| **禁止の指示を置いた見出しが遅い**（`## 終わったらやること` の中） | **直していない。人間の判断待ち** | **置き場所は 6 が名指しで決めており、3回のレビューを通っている。**AI の判断で動かさない |
| **worktree の後始末をエージェントの善意に預けている**（`git worktree add --detach /tmp/…`） | **直していない。人間の判断待ち** | 同じく 6 が本文を1行の違いも無く決めている。**害は限定的**（detached なので branch を握らない） |

**足したテスト。**`TestCheckWorktreeUsable_別のbranchを段0で断る`。
段2 側の `TestPrepare_別のbranchへ切り替えられたworktreeは再利用しない` にも
`%!` / `(MISSING)` / `(EXTRA` を掴む検査を入れた。

### 16-8. 人間に決めてもらうこと（この PR について）

**2つとも、雛形の3段落をどこに置き、何を書くかの話である。**

| 何 | いまの形 | 代案 |
| --- | --- | --- |
| **3段落の置き場所** | `## 終わったらやること` の中（`**push 先は、…**` の直前） | `## この issue に着手してよいことは、もう決まっています` の直後。**見出しを増やさずに、読む順番より先に置ける** |
| **別の branch の中身を読む手順** | `git worktree add --detach /tmp/<任意の名前> FETCH_HEAD` と `git worktree remove` | worktree の置き場所の下（continuo が prune する場所）を使わせる。あるいは読むだけなら `git show <ref>:<パス>` |

**どちらも [internal/scaffold/template.go](../../../internal/scaffold/template.go) と 5-3 の本文を同時に直すことになる**
（`TestTemplate_雛形の本文が設計5_3の本文と一致する` が一致を検査している）。

**残っていること。**

- **draft は外していない。**人間が確かめてから外す
- **`git worktree` は片付けていない。**PR #146（worktree が別の branch を出していると永久に飛ばされるのを断る）がマージされるまで使う

---

## 17. #142: 残っていた low 2件を直した（人間の判断が出たあと）

**言いたいこと。**16-8 で人間の判断待ちにしていた2件は、**2件とも「直す」で決まった。**
**置き場所は `## この issue を読むこと` の直前へ移し、読むだけのときは worktree を作らせない。**
6 が決めていた「見出しを増やさない」「`## 終わったらやること` の中へ置く」は、**この節が置き換える。**

### 17-1. 「切り替えるな」を、issue を読ませる前へ移した

**採る形。**`## worktree と branch は切り替えないこと` という独立の見出しを1つ作り、
`## この issue に着手してよいことは、もう決まっています` の節の直後
（= `## この issue を読むこと` の直前）へ置く。

**なぜ移すか。**エージェントが branch を切り替えるのは、`## この issue を読むこと` と
`## この issue に紐づく PR も読むこと` で読んだ中身が原因である。
**#142（worktree が別の branch を出していると永久に飛ばされる）の報告者の実測では、着手の82秒後に切り替わっていた。**
`## 終わったらやること` の中にある指示に辿り着くのは、**切り替わったあとである。**

**なぜ見出しを1つ増やしたか。**`## この issue に着手してよいことは、もう決まっています` の中へ
地の文で足すこともできるが、**その見出しは「着手してよい」を名乗っており、
「どこで作業するか」は名乗っていない。**名乗りと中身が食い違う記述になる。
**6 の「見出しを新しく作らない」は、貼り先が `## 終わったらやること` の中で、
`##` を差し込むと後ろの3段落が新しい見出しの下へ落ちるからだった。**
節と節の境目へ置くいまの形では、その理由が成り立たない。

### 17-2. 読むだけのときは worktree を作らせない

**採る形。**

    git fetch origin <その branch>
    git show FETCH_HEAD:<見たいファイルのパス>

**なぜ変えるか。**旧い本文は `git worktree add --detach /tmp/<任意の名前> FETCH_HEAD` と
`git worktree remove /tmp/<任意の名前>` の2行を書かせていた。
**エージェントがこの2行の間で止まると、共有の clone に登録が残る。**
`Prepare` が叩く `gitWorktreePrune`（[internal/workspace/prepare.go:180](../../../internal/workspace/prepare.go#L180)、
実体は [internal/workspace/git.go:143](../../../internal/workspace/git.go#L143)）は
**実体が先に消えた登録しか落とさない**ので、`/tmp/<名前>` が在る限り残り続ける。
**`git show` なら登録が1つも増えない。**

**逃げ道（worktree でないと足りない場合）は置かなかった。**
置き場所を指せる変数が本文に無い（5-3 の変数の表にあるのは `.issue.*` と `.attempt` だけである）。
**continuo の worktree の置き場所を本文から名指しできないので、
「`/tmp` ではなくそちらへ」と書いても、エージェントは結局 `/tmp` を選ぶ。**同じ取り残しに戻る。

### 17-3. 触ったもの

| ファイル | 何をしたか |
| --- | --- |
| [internal/scaffold/template.go](../../../internal/scaffold/template.go) | 塊を移し、読むだけの手順を `git show` に替えた |
| [docs/plans/continuo_design.md](../continuo_design.md) 5-3 | 同じ本文へ1行の違いも無く反映した |
| [docs/plans/continuo_design.md](../continuo_design.md) 3-69 | 置き場所の決定と、`git show` にした根拠を書いた |
| [docs/upgrading.md](../../upgrading.md) | 貼り先を `## この issue を読むこと` の直前に直し、確かめ方に「順番」を足した |

**[docs/FAQ.md](../../FAQ.md) は直していない。**探させている文言
（「continuo が用意した worktree と branch のまま作業してください」）が1文字も変わっていないためである。

**`TestTemplate_雛形の本文が設計5_3の本文と一致する` が
[internal/scaffold/template.go](../../../internal/scaffold/template.go) と 5-3 の一致を検査している。**

### 17-4. 通したもの

| 何 | 結果 |
| --- | --- |
| `mise trust` | `No untrusted config files found.`（先に叩いた） |
| `sh scripts/test-like-ci.sh` | **EXIT=0。**`FAIL` は1件も無い。`test/internal/scaffold` も `ok` |
| commit | `8374929`（branch `fix/issue142-branch-mismatch`） |
| push | `8d1e49f..8374929` |
| PR #146（別の branch を出している worktree に、消させない案内を返す）のコメント | https://github.com/maimuzo/continuo/pull/146#issuecomment-5494163768 |
| draft | **外していない**（`isDraft: true`） |
