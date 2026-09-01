# 着手の関門で止めた issue を、ダッシュボードと issue のコメントに出す

**対象。**#134（ダッシュボードに「着手できずに止まっているもの」を出す）が代表。
#140（人間が担当者で着手できないことを、issue のコメントとして1回だけ書く）と
#136（担当者が2人以上いる issue も、着手できないことを知らせる）を同じ worktree で直す。

**この文書が正である。**[docs/plans/continuo_design.md](../continuo_design.md) の 3-68 を、この3件の範囲で具体化したもの。
設計が固まったら 3-68 の節をこの内容へ置き換える。

---

## 1. 言いたいこと

**言いたいこと。**飛ばした事実は、いまどこにも残っていない。
**`handoffGate` が止めた issue だけを覚える map を1つ足し、ダッシュボードの表と issue のコメントの
両方をそこから作る。**記録する場所は1関数、消す場所は2つに固定する。

**この設計が扱う範囲。**

| 何 | 出す先 |
| --- | --- |
| **人間が付けた担当者で止めた**（`ActionSkipHumanAssigned`） | ダッシュボード＋issue へ1回だけコメント |
| **担当者が2人以上で止めた**（`len(logins) >= 2` の早い戻り） | ダッシュボード＋issue へ1回だけコメント |
| それ以外の関門（worktree・信頼・枠・ラベル・入札） | **出さない。**理由は 13 節 |

**呼び名。****「gate」＝着手の前の関門**である。いまは
[internal/orchestrator/handoff.go](../../../internal/orchestrator/handoff.go) の `handoffGate` だけを指す。

**`blocked` という語を使わない。**ボードの Status に `Blocked` があり
（[internal/orchestrator/dispatch.go:71](../../../internal/orchestrator/dispatch.go#L71) の `dispatchBlockedStates`）、
**同じ語が2つの別のものを指すことになる。**

---

## 2. 前の2版が落ちた穴と、この版で起きない理由

**言いたいこと。**v1 は「巡回の最後に掃除する」で落ち、v2 は「既にある記録を読む」で落ちた。
**この版は掃除をボードの候補一覧から行い、記録は関門の中で自分で作る。**

| 前の版の穴 | この版で起きない理由 |
| --- | --- |
| **空きスロットが尽きた巡回で全消し**（v1） | **掃除は `dispatchCandidates` のループを見ない。**`Tick` が取った候補一覧に居ない項目だけを消す（5 節） |
| **枠で止まった巡回で全消し**（v1） | 同上。`dispatchPaused` で早く戻っても候補一覧は取れている |
| **`o.failures` に着手前の issue が入らない**（v2） | **`o.failures` を読まない。**`handoffGate` の中で自分で記録する |
| **`failureNote` が識別子を持たない**（v2） | 記録に `Identifier` / `Title` / `URL` を持たせる（3 節） |
| **理由の種類を見分けられない**（v2） | **担当者の経路は 3-66（番兵エラーの新設）に依存しない。**`handoff.Action` が既に種類を持っている |

**`FetchIssuesByStates` は途中で切れない。**ページ数が上限を超えたら
`CategoryPagination` の `*Error` を返す（[internal/tracker/adapter.go:608-617](../../../internal/tracker/adapter.go#L608-L617)）。
**したがって「エラーなしで返った候補一覧」は必ず全件である。**掃除の土台に使ってよい。

---

## 3. 記録の置き場所

**言いたいこと。**メモリだけに持つ。**新しいファイルは1つも作らない。**
`o.mu` が守る map を1つ足し、鍵は project item の ID にする。
**判定には1度も使わない。**表示とコメントのためだけに持つ。

**新しいファイル。**[internal/orchestrator/gate.go](../../../internal/orchestrator/gate.go)（新規）に型と関数をまとめる。

```go
// GateReason は、着手の関門が止めた理由の種類である（issue #134 / #136 / #140）。
//
// **文字列にしてある。**issue のコメントの印に埋め込み、再起動をまたいで照合するため。
type GateReason string

const (
	// GateReasonHumanAssigned は、continuo が付けたのではない担当者が1人付いていることを表す。
	GateReasonHumanAssigned GateReason = "human_assigned"
	// GateReasonManyAssignees は、担当者が2人以上付いていることを表す。
	GateReasonManyAssignees GateReason = "many_assignees"
)

// gateNote は、着手の関門で止めた issue 1件の記録である（設計 3-68）。
//
// **永続化しない。**再起動すると消えるが、次の巡回で作り直される。
// **判定には1度も使わない。**ダッシュボードと「コメントを1回だけ」のためだけに持つ。
type gateNote struct {
	// Identifier は `<owner>/<repo>#<番号>` である。
	Identifier string
	// Title は issue の題名である。**外部から来る文字列である。**表示時にエスケープすること。
	Title string
	// URL は issue の URL である。draft issue は持たないので空文字になる。
	URL string
	// Reason は止めた理由の種類である。
	Reason GateReason
	// Assignees は、いま付いている担当者のログイン名である。**巡回のたびに上書きする。**
	// **コメントには書かない**（7 節）。ダッシュボードだけが出す。
	Assignees []string
	// FirstSeenAt は、この理由で最初に止めた時刻である。**同じ理由なら更新しない。**
	FirstSeenAt time.Time
	// LastSeenAt は、この理由で最後に止めた時刻である。
	LastSeenAt time.Time
	// NoticedAt は issue へコメントを書いた時刻である。ゼロ値ならまだ書いていない。
	NoticedAt time.Time
}
```

**置き場所。**[internal/orchestrator/orchestrator.go](../../../internal/orchestrator/orchestrator.go) の `Orchestrator` に1行、`New` の初期化に1行。

```go
	// gated は、着手の関門で止めた issue の記録である（issue #134 / #136 / #140）。
	// キーは project item の ID。**mu が守る。**
	gated map[string]*gateNote
```

**`o.runs` に相乗りしない。**あれは「印＝実行中」であり
（[internal/orchestrator/orchestrator.go:303-305](../../../internal/orchestrator/orchestrator.go#L303-L305)）、
入れると空きスロットの数え方（`freeSlotBlocker`）と重複判定（`lookupRunByID`）の両方が壊れる。

---

## 4. 記録する場所は2つだけ

**言いたいこと。**[internal/orchestrator/handoff.go](../../../internal/orchestrator/handoff.go) の `handoffGate` の中の2箇所で `noteGate` を呼ぶ。
**呼ぶのは「その巡回で止めたこと」を記録する1関数だけで、コメントを書くかどうかはその戻り値が決める。**

| どこ | いまの動き | 足すもの |
| --- | --- | --- |
| **担当者が2人以上**（[internal/orchestrator/handoff.go:81-86](../../../internal/orchestrator/handoff.go#L81-L86)） | WARN を1行出して `handoffDecision{}` | `noteGate(…, GateReasonManyAssignees, logins)` |
| **人間が付けた担当**（[internal/orchestrator/handoff.go:134-149](../../../internal/orchestrator/handoff.go#L134-L149)） | WARN を1行出して `handoffDecision{}` | `noteGate(…, GateReasonHumanAssigned, logins)` |

**`noteGate` の形。**

```go
// noteGate は、着手の関門で止めたことを記録し、issue へ案内を書くべきかを返す（設計 3-68）。
//
// **判定には1度も使わない。**ダッシュボードに出すためと、案内を1回だけにするために持つ。
// **理由が変わったら数え直す。**担当者が2人から1人へ減れば、別の理由として数え直す。
//
// issue: 止めた issue。
// reason: 止めた理由の種類。
// assignees: いま付いている担当者のログイン名。
// 戻り値: 案内をまだ書いておらず、書く条件（noticeMinCount / noticeMinAge）を満たしていれば true。
func (o *Orchestrator) noteGate(issue tracker.Issue, reason GateReason, assignees []string) bool
```

**いつ案内を書くか。**設計 3-68 の「同じ鍵で3回続けて止め、かつ最初に止めてから60秒以上たったとき」をそのまま採る。

```go
// noticeMinCount は、案内を書くまでに同じ理由で止めた回数である（設計 3-68）。
//
// **1回目では書かない。**人間が担当者を付け替えている最中の1巡回で書くと、
// 数秒で解消する状態に永久に残るコメントを1件足すことになる。
const noticeMinCount = 3

// noticeMinAge は、最初に止めてから案内を書くまでに空ける時間である（設計 3-68）。
//
// **回数だけでは足りない。**`polling.interval_ms` を短くした環境では
// 3回が数秒で埋まる。**時間だけでも足りない。**巡回が止まっていた間に経った時間は
// 「止まり続けている」の証拠にならない。
const noticeMinAge = 60 * time.Second
```

**担当者が2人以上の経路では、コメントを読まない。**
[internal/orchestrator/handoff.go:77-80](../../../internal/orchestrator/handoff.go#L77-L80) の「コメントを読まずに答えが出るものを先に処理する」を崩さない。
**読み取りの枠（`maxHandoffFetchesPerPoll` は10件）を1件も使わない。**
#136（担当者が2人以上いる issue も、着手できないことを知らせる）が挙げた2つの道
（その分岐でもコメントを読む／map だけで判定する）のうち、後者を**回数と時間の条件付きで**採る。

---

## 5. 記録を消す場所は2つだけ

**言いたいこと。**v1 は「その巡回で見なかったものを消す」で落ちた。
**この版は `dispatchCandidates` のループを一切見ない。**ボードの候補一覧に居ないものだけを消す。
**空きスロットが尽きても、枠で止まっても、読み取りの枠を使い切っても、記録は1件も減らない。**

| いつ消すか | どこで | なぜ |
| --- | --- | --- |
| **関門を通ったとき** | `handoffGate` の `ActionProceed` と入札に勝った直後 | 直ったのに残ると、直しても消えない行がダッシュボードに残る |
| **ボードの候補から消えたとき** | `Tick`（候補の取得が成功したときだけ） | issue を閉じた・Status を動かした・ボードから外した |

**`Tick` に足す2行。**[internal/orchestrator/orchestrator.go:560-564](../../../internal/orchestrator/orchestrator.go#L560-L564) の直後に置く。

```go
	candidates, err := o.tracker.FetchIssuesByStates(ctx, o.cfg.Tracker.ActiveStates)
	if err != nil {
		o.logger.Warn("候補の取得に失敗しました（この巡回の dispatch は行いません）", "error", err)
		dispatchAllowed = false
	} else {
		// **取れた候補一覧に居ないものだけを消す**（設計 3-68 の「通ったら印を消す」の補い）。
		// **`dispatchCandidates` のループは見ない。**空きスロットが尽きて途中で
		// 打ち切られた巡回でも、記録は1件も減らない。
		o.sweepGated(candidates)
	}
```

**`sweepGated` は「印を持っている run」も消す。**着手できた issue は `In Progress`（active_states）に
残るので候補一覧からは消えないが、`dispatchCandidates` の
[internal/orchestrator/dispatch.go:190-193](../../../internal/orchestrator/dispatch.go#L190-L193) が
`lookupRunByID` で先に飛ばすため、`handoffGate` へは二度と来ない。
**そこで `sweepGated` は、印を持つ issue の記録も外す。**

```go
// sweepGated は、いまのボードの候補に居ない issue の記録を外す（設計 3-68）。
//
// **候補の取得が成功した巡回でだけ呼ぶ。**失敗した巡回で呼ぶと全件が消える。
// **印を持っている issue の記録も外す**（着手できたのだから、止まっていない）。
//
// candidates: `FetchIssuesByStates` が返した候補（全件）。
func (o *Orchestrator) sweepGated(candidates []tracker.Issue)
```

**`FirstSeenAt` は巡回の打ち切りでリセットされない。**消すのは上の2つの場合だけであり、
**「この巡回で見なかったから」では1件も消さない。**

---

## 6. 案内を1回だけにする仕組み（2層）

**言いたいこと。**`noteUntrusted` を真似る。**印を持つ・ログを出す・1回だけコメントする、を1関数に収める。**
**人間が付けた担当の経路では、手元のコメントも見て、再起動をまたいでも1回にする。**
担当者が2人以上の経路にはコメントが無いので、メモリの印だけで抑える。

| 層 | 何を見るか | どちらの理由に効くか |
| --- | --- | --- |
| **メモリの印**（`gateNote.NoticedAt`） | `o.gated` の記録 | **両方** |
| **手元のコメント**（`gateNoticedIn`） | `FetchAllComments` が返した全件 | **人間が付けた担当だけ** |

**`FetchAllComments` は1件も落とさない。**`keep` に0を渡すので打ち切りが効かず
（[internal/tracker/adapter.go:1150-1158](../../../internal/tracker/adapter.go#L1150-L1158)）、
**continuo 自身が書いたコメントも、持ち回りの印が付いたコメントも、そのまま返る**
（[internal/tracker/adapter.go:1132-1136](../../../internal/tracker/adapter.go#L1132-L1136)）。
**だから、前の起動で書いた案内が手元に来る。**

```go
// gateNoticedIn は、この理由の案内が既に issue に書かれているかを返す（設計 3-68 / 3-65）。
//
// **印だけで「continuo が書いた」と決めない**（設計 3-65）。印は本文の先頭に置く
// ただの文字列であり、issue にコメントできる人なら誰でも同じものを書ける。
// **投稿者が gh の持ち主であるものだけを、自分が書いたものとして扱う。**
//
// comments: FetchAllComments が返した全件。
// selfLogin: continuo が使う gh の持ち主。**空文字なら常に false を返す**（照合できない）。
// reason: 探す理由の種類。
// 戻り値の1つ目: 見つかったコメントの時刻。
// 戻り値の2つ目: 見つかれば true。
func gateNoticedIn(comments []tracker.Comment, selfLogin string, reason GateReason) (time.Time, bool)
```

**探す印。**`<!-- continuo:gated:human_assigned -->` を本文の2行目に置く。

**1行目は `self_marker` である。**[internal/orchestrator/comment.go:373-375](../../../internal/orchestrator/comment.go#L373-L375) の `postComment` を使い、
`<!-- continuo:self -->` を先頭に付けさせる。**付けないと、この案内が次の turn の入力として
エージェントへ渡る**（[internal/tracker/adapter.go:1080-1083](../../../internal/tracker/adapter.go#L1080-L1083) が `self_marker` で始まるものだけを外す）。

**`postOwnMarkedComment` は使わない。**あれは持ち回りのコメント（入札・hold・released）専用で、
**本文の先頭が持ち回りの印そのものでなければならない**
（[internal/orchestrator/comment.go:388-391](../../../internal/orchestrator/comment.go#L388-L391)）。案内は持ち回りのコメントではない。

---

## 7. 決めた2つ

**言いたいこと。**担当者が変わっても案内は書き直さない。**そのために、担当者の名前を案内に書かない。**
通知は切れるようにする。**切っても、記録とダッシュボードと WARN は残る。**

### 7-1. 担当者が変わっても書き直さない

**採る形。****案内に担当者のログイン名を1文字も書かない。**

**なぜ。****書き直す手段が無い。**[internal/tracker](../../../internal/tracker) にコメントを書き換える経路も消す経路も無い
（`updateIssueComment` / `deleteIssueComment` / `minimizeComment` を
検索パターン `updateIssueComment|deleteIssueComment|minimizeComment|UpdateComment|DeleteComment`、
対象 `internal/` と `cmd/`、commit 73fb41ae654add121c72ed01464ef77f69684812 で0件）。
**書き直すとは「もう1件足す」ことでしかなく、担当者を付け替えるたびに古い案内が積まれる。**

**書かなくても困らない。****やることは担当者が誰であっても同じである**（GitHub の画面で担当者を外す）。
**誰が付いているかは issue の画面の右側に、いま現在の値が出ている。**
**担当者の名前は WARN の1行とダッシュボードに出す。**どちらも常に最新である。

### 7-2. 通知は切れる

**採る形。**設定を1つ足す。`trust.on_untrusted` と同じ形にする。

```yaml
tracker:
  provider:
    handoff:
      on_human_assignee: warn_and_comment   # 人間の担当者で着手できないときの扱い。warn_only にすると issue へ書かない
```

| 値 | WARN | ダッシュボード | issue へのコメント |
| --- | --- | --- | --- |
| **`warn_and_comment`**（既定） | 出す | 出す | **書く** |
| **`warn_only`** | 出す | 出す | **書かない** |

**この設定は、記録そのものには一切効かせない。**`noteGate` は値を見ずに必ず記録し、
**値を見るのはコメントを投稿する直前の1箇所だけである。**

**#140（人間が担当者で着手できないことを、issue のコメントとして1回だけ書く）が挙げた過去の失敗を、
この置き方で避ける。**

> **「投稿が成功したときだけ map に入れる」と「通知を切れる設定」を同時に入れたので、通知を切ると飢餓が戻ります。**

**印は投稿の成否に関わらず付ける**（`noteUntrusted` も
[internal/orchestrator/dispatch.go:644-649](../../../internal/orchestrator/dispatch.go#L644-L649) で先に印を付けてから投稿する）。
**読み取りの枠（`maxHandoffFetchesPerPoll`）の消費は、この設計で1件も増減しない。**

---

## 8. 案内の本文

**言いたいこと。**理由ごとに1本ずつ書く。**担当者の名前は書かない**（7-1）。
**担当者が2人以上の版だけ、再起動でもう一度書くことがある旨を添える。**

**置き場所。**[internal/orchestrator/prompt.go](../../../internal/orchestrator/prompt.go)（`buildUntrustedComment` の隣）に `buildGatedComment` を足す。
**`internal/i18n` は使わない。**`internal/orchestrator` の人間向けの文言は
「まとめて資源へ移す」と決まっており（[internal/orchestrator/dispatch.go](../../../internal/orchestrator/dispatch.go) の注記）、この issue だけ先に移すと揃わない。

**人間が付けた担当のとき。**

```
<!-- continuo:self -->
<!-- continuo:gated:human_assigned -->
この issue には担当者が付いているため、continuo は着手しません。
continuo が付けた担当ではないので、人間が作業中だと判断しています。

着手させるには、GitHub の画面でその担当者を外してください。

continuo が使うアカウントへの付け替えは案内しません。付け替えると、
同じアカウントで動いている別の機械も「自分の担当だ」と読むため、2台が同時に着手できてしまいます。

この案内は、この理由につき1回だけ書きます。
```

**担当者が2人以上のとき。**

```
<!-- continuo:self -->
<!-- continuo:gated:many_assignees -->
この issue には担当者が2人以上付いているため、continuo は着手しません。
人間が作業を分担していると判断しています。

着手させるには、GitHub の画面で担当者を1人も付いていない状態にしてください。

この案内は、この理由につき1回だけ書きます。
**continuo を再起動すると、もう一度書くことがあります。**
この経路は issue のコメントを読まないので、前の起動で書いたことを手元から確かめられません。
```

**本文は `postComment` を通る。**[internal/orchestrator/comment.go:402-412](../../../internal/orchestrator/comment.go#L402-L412) の `postCommentWithMarker` が
手元の絶対パスを `~` へ縮める唯一の場所である。**この案内はパスを1つも載せないが、経路は揃える。**

**`buildGatedComment` が返すのは2行目からである。**1行目の `<!-- continuo:self -->` は
[internal/tracker/adapter.go:1110-1113](../../../internal/tracker/adapter.go#L1110-L1113) が `selfMarker + "\n" + body` で付ける。

---

## 9. ダッシュボードに出す欄

**言いたいこと。**`Snapshot` にフィールドを1つ、`RunSource` にメソッドを1つ足し、表を1つ増やす。
**`orchestrator` の型をそのまま出さない。**表示用の型へ写す（既存の `Run` と同じ流儀）。

**`internal/orchestrator` 側に足す写しの型。**

```go
// GateView は、着手の関門で止めた issue の写しである（issue #134）。
//
// **`o.gated` の中身をそのまま渡さない。**ダッシュボードは印の集合にも記録にも触らない
// （設計 3-25）。呼ばれた時点の値を写して返す。
type GateView struct {
	// Identifier は issue の識別子（`<owner>/<repo>#<番号>`）である。
	Identifier string
	// Title は issue の題名である。**外部から来る文字列である。**
	Title string
	// URL は issue の URL である。draft issue は持たないので空文字になる。
	URL string
	// Reason は止めた理由の種類である。**文言に直すのは internal/server である。**
	Reason GateReason
	// Assignees は、いま付いている担当者のログイン名である。
	Assignees []string
	// Since は最初にこの理由で止めた時刻である。
	Since time.Time
	// Noticed は issue へ案内を書き終えているかである。
	Noticed bool
}

// GateViews は、着手の関門で止めた issue の写しを返す（順序は不定）。
func (o *Orchestrator) GateViews() []GateView
```

**`internal/server` 側。**

```go
type RunSource interface {
	RunViews() []orchestrator.RunView
	// GateViews は着手の関門で止めた issue の写しを返す（issue #134）。
	GateViews() []orchestrator.GateView
}
```

```go
	// Gated は着手の関門で止めた issue である（issue #134）。
	// **Since の古い順に並べてある**（`GateViews` の順序は不定なので、
	// 並べないと10秒ごとの再読み込みで行が入れ替わる）。
	Gated []Gated `json:"gated"`
```

---

## 10. ダッシュボードの表示用の型と表

**言いたいこと。**理由を文言へ直すのは [internal/server/view.go](../../../internal/server/view.go) である。
**`i18n.Key` をテンプレートへ渡さない。**テンプレートの `t` は `string` しか受け取らない
（[internal/server/template.go:150-152](../../../internal/server/template.go#L150-L152)）。

```go
// Gated は着手の関門で止めた issue 1件の表示用の写しである（issue #134）。
type Gated struct {
	// Identifier は issue の識別子である。
	Identifier string `json:"identifier"`
	// Title は issue の題名である。**HTML へ出すときは必ずエスケープすること。**
	Title string `json:"title"`
	// URL は issue の URL である。空なら画面はリンクにしない。
	URL string `json:"url"`
	// Reason は止めた理由の種類である（機械が読むための値。`"human_assigned"` など）。
	Reason string `json:"reason"`
	// ReasonText は理由の1行である（資源から引いた文言）。
	ReasonText string `json:"reason_text"`
	// Remedy は直し方の1行である（資源から引いた文言）。
	Remedy string `json:"remedy"`
	// Assignees は担当者のログイン名を `, ` で繋いだものである。
	Assignees string `json:"assignees"`
	// Since は最初にこの理由で止めた時刻である。
	Since time.Time `json:"since"`
	// SinceAgo は Since からの経過を人間向けに書いたものである（`humanizeSince`）。
	SinceAgo string `json:"since_ago"`
	// Noticed は issue へ案内を書き終えているかである。
	Noticed bool `json:"noticed"`
}
```

**表。**列は4つ。`{{ t "dashboard.caption_gated" }}` を見出しにして、実行中の run の表の**上**に置く。
**上に置く理由。**「実行中の run はありません」しか出ない画面を見に来た人が探しているのは、こちらである。

| 着手できずに止まっているもの | なぜ | いつから | 直し方 |
| --- | --- | --- | --- |
| `octocat/hello-world#42`（題名） | 担当者が付いています（`octocat`） | 40分前 | GitHub の画面で担当者を外してください |

**空のときは1行だけ出す。**「着手できずに止まっているものはありません」。

**`Noticed` は表に列を作らない。**JSON にだけ出す。
**列を1つ増やすより、案内を書いていない行に印を1つ添えるほうが読める**
（既存の `badge_waiting_quota` と同じ形。`{{- if not .Noticed }}<span class="badge">…</span>{{ end }}`）。

---

## 11. 足す文言（11件）

**言いたいこと。**`internal/server` は文言を全部資源から引く（設計 3-35）。
**理由と直し方も資源に置く。**`internal/orchestrator` 側の WARN と issue のコメントは、いまどおり日本語を直に書く。

| キー | 何に出るか |
| --- | --- |
| `dashboard.caption_gated` | 表の見出し「着手できずに止まっているもの」 |
| `dashboard.col_gated_reason` / `dashboard.col_gated_since` / `dashboard.col_gated_remedy` | 列の見出し（issue の列は既存の `dashboard.col_issue` を使い回す） |
| `dashboard.no_gated` | 1件も無いときの1行 |
| `dashboard.note_gated` | 表の下の注記（メモリだけに持つので再起動で消える、と書く） |
| `dashboard.gate_reason_human_assigned` / `dashboard.gate_reason_many_assignees` | 理由の1行（`%s` に担当者） |
| `dashboard.gate_remedy_human_assigned` / `dashboard.gate_remedy_many_assignees` | 直し方の1行 |
| `dashboard.badge_not_noticed` | 案内をまだ書いていない行に添える印 |

**`ja.json` の中身。**

```json
  "dashboard.caption_gated": "着手できずに止まっているもの",
  "dashboard.col_gated_reason": "なぜ止まっているか",
  "dashboard.col_gated_since": "いつから",
  "dashboard.col_gated_remedy": "直し方",
  "dashboard.no_gated": "着手できずに止まっているものはありません",
  "dashboard.note_gated": "この表はメモリだけに持っています。continuo を再起動すると消え、次の巡回で作り直されます。",
  "dashboard.gate_reason_human_assigned": "担当者が付いています（%s）",
  "dashboard.gate_reason_many_assignees": "担当者が2人以上います（%s）",
  "dashboard.gate_remedy_human_assigned": "GitHub の画面でその担当者を外してください",
  "dashboard.gate_remedy_many_assignees": "GitHub の画面で担当者を1人も付いていない状態にしてください",
  "dashboard.badge_not_noticed": "issue へは未通知"
```

**`en.json` にも同じ11件を足し、先頭の `_source_sha256`
（[internal/i18n/messages/en.json:2](../../../internal/i18n/messages/en.json#L2)）を入れ直す。**

**理由から文言のキーを引く表は、[internal/server/view.go](../../../internal/server/view.go) に置く。**
**`switch` の `default` で `i18n.KeyDashboardNone` に落とさない。**
知らない理由が来たら、その理由の文字列をそのまま出す。
**引けなかったキーは `(no message is registered for this key: …)` になって画面に出る**
（[internal/i18n/i18n.go:279-287](../../../internal/i18n/i18n.go#L279-L287)）ので、落ちはしないが黙って消えもしない。

---

## 12. 実装の順 — PR は1本にする

**言いたいこと。**3件を1本の PR にまとめる。**分けると同じ関数を3回書き換えることになる。**
段は5つに分け、**段ごとに `go build ./...` と `go test ./test/internal/orchestrator/` が通る形にする。**

| 段 | 何を作るか | どの issue が閉じるか |
| --- | --- | --- |
| **1** | [internal/orchestrator/gate.go](../../../internal/orchestrator/gate.go)（型・`noteGate` / `clearGate` / `sweepGated` / `GateViews`）と `Orchestrator` の1行 | まだ閉じない |
| **2** | `handoffGate` の2箇所で `noteGate` を呼び、`ActionProceed` と入札に勝った直後で `clearGate` を呼ぶ。`Tick` で `sweepGated` | まだ閉じない |
| **3** | ダッシュボード（`RunSource` / `Snapshot` / 表 / 文言11件） | **#134（ダッシュボードに「着手できずに止まっているもの」を出す）** |
| **4** | `buildGatedComment` と `gateNoticedIn`、`postComment` の呼び出し、設定 `on_human_assignee` | **#140（人間が担当者で着手できないことを、issue のコメントとして1回だけ書く）** |
| **5** | 担当者が2人以上の経路の案内と、[docs/FAQ.md](../../FAQ.md) / [docs/upgrading.md](../../upgrading.md) | **#136（担当者が2人以上いる issue も、着手できないことを知らせる）** |

**なぜ1本か。**3件とも `handoffGate` の中を触る。
**別々の PR にすると、2本目以降は必ず1本目の変更の上に rebase することになり、
`handoffGate` の同じ分岐で衝突する。**

**なぜこの順か。**

| 順 | 理由 |
| --- | --- |
| **記録が先** | ダッシュボードも案内も、記録が無いと1件も出せない |
| **ダッシュボードが次** | **1バイトも書かない。**実機で確かめるときに、issue を汚さずに動作を見られる |
| **案内が最後** | issue へ書き込む。**ダッシュボードで記録が正しいことを確かめてから足す** |

**依存する別の作業。**[docs/plans/impl/issue142_144_branch_mismatch.md](issue142_144_branch_mismatch.md) が
[internal/i18n/keys.go](../../../internal/i18n/keys.go) と `en.json` / `ja.json` を触る。
**同じファイルの同じ末尾へ足すので、後から main へ入るほうが衝突を解く。**
**先に入ったほうへ rebase してから段3を書くこと。**

---

## 13. 変えるファイル（15）

**言いたいこと。**新規は1本。**`internal/workspace` と `internal/tracker` は1バイトも触らない。**

| ファイル | 何をするか |
| --- | --- |
| [internal/orchestrator/gate.go](../../../internal/orchestrator/gate.go) | **新規。**`GateReason` / `gateNote` / `noteGate` / `clearGate` / `sweepGated` / `gateNoticedIn` / `GateView` / `GateViews` |
| [internal/orchestrator/orchestrator.go](../../../internal/orchestrator/orchestrator.go) | 構造体に `gated` を1行、`New` の初期化に1行、`Tick` に `sweepGated` |
| [internal/orchestrator/handoff.go](../../../internal/orchestrator/handoff.go) | 2箇所で `noteGate`、2箇所で `clearGate`、案内の投稿 |
| [internal/orchestrator/prompt.go](../../../internal/orchestrator/prompt.go) | `buildGatedComment` |
| [internal/config/types.go](../../../internal/config/types.go) | `OnHumanAssignee` を1行 |
| [internal/config/default.go](../../../internal/config/default.go) | 既定値 `"warn_and_comment"` を1行 |
| [internal/config/validate.go](../../../internal/config/validate.go) | 値の検査（`trust.on_untrusted` と同じ形。[internal/config/validate.go:348](../../../internal/config/validate.go#L348)） |
| [internal/scaffold/template.go](../../../internal/scaffold/template.go) | WORKFLOW.md の雛形に1行 |
| [internal/server/server.go](../../../internal/server/server.go) | `RunSource` に `GateViews()` |
| [internal/server/view.go](../../../internal/server/view.go) | `Snapshot.Gated` と `Gated` 型、理由→文言の表、`NewSnapshot` の詰め替え |
| [internal/server/template.go](../../../internal/server/template.go) | 表を1つ増やす |
| [internal/i18n/keys.go](../../../internal/i18n/keys.go) | キー11件と `allKeys` |
| [internal/i18n/messages/ja.json](../../../internal/i18n/messages/ja.json) / [en.json](../../../internal/i18n/messages/en.json) | 文言11件。**`_source_sha256` を入れ直す** |
| [docs/FAQ.md](../../FAQ.md) / [docs/upgrading.md](../../upgrading.md) | 「担当者が付いた issue が着手されない」に、ダッシュボードと案内のことを足す |
| [docs/plans/continuo_design.md](../continuo_design.md) | 3-68 をこの内容へ置き換える。5-2 の設定表に `on_human_assignee` を1行 |

**テスト。**

| ファイル | 何を確かめるか |
| --- | --- |
| [test/internal/orchestrator/](../../../test/internal/orchestrator) | 空きスロットが尽きた巡回で記録が減らない／枠で止まった巡回で減らない／候補から消えたら消える／案内は3回目かつ60秒後に1回だけ／`warn_only` では書かない |
| [test/internal/server/](../../../test/internal/server) | `fakeSource` に `GateViews` を足す。表の行と、1件も無いときの1行 |
| [test/internal/i18n/](../../../test/internal/i18n) | 既存の突き合わせが11件を拾う（新しいテストは要らない） |

**`test/internal/orchestrator/expected_warnings_test.go` に足す。**
担当者を2人付けたテストと、人間の担当を付けたテストは **WARN を出す**ので、
そのテスト名を登録しないと落ちる（[test/internal/orchestrator/expected_warnings_test.go:7](../../../test/internal/orchestrator/expected_warnings_test.go#L7)）。

---

## 14. やらないこと

**言いたいこと。****関門は24分岐あるが、この設計が出すのは2つだけである。**
v1 は全部を出そうとして落ちた。**足すのは呼び出し1行なので、あとから足せる。**

| 何 | なぜやらないか |
| --- | --- |
| **worktree の経路を出す**（置き場所・登録の欠落・branch の食い違い） | **3-66（番兵エラーの新設）が先に要る。**いまは branch の食い違いも登録の欠落も同じ番兵で包まれており、2つを区別できない |
| **未信頼のリポジトリを出す** | 既に WARN を出し、issue にもコメントを書いている（`noteUntrusted`）。**同じことを2つの仕組みで持たない** |
| **枠・空きスロット・必須ラベルを出す** | **止まっているのではなく、順番を待っている。**次の巡回で勝手に進む。出すと画面が常時埋まり、本当に止まっているものが埋もれる。**ログには既に出ている**（PR #141「issue が1件も着手されない3つの理由を、ログから探せるようにする」） |
| **入札に負けた・他の機械が担当中を出す** | **正常に譲っている。**止まっていない |
| **記録を永続化する** | 再起動すると消えるが、次の巡回で作り直される。**ファイルを1つ増やす価値がない。**そのことを表の下の注記に書く |
| **記録を判定に使う** | **表示と案内のためだけに持つ。**判定に使うと、表示の欠陥が着手を止める |
| **案内を書き直す** | コメントを書き換える手段が無い（7-1） |
| **担当者が2人以上の経路でコメントを読む** | 読み取りの枠（1巡回10件）を使い、#136（担当者が2人以上いる issue も、着手できないことを知らせる）が否定した道である |
| **読み取りの枠の飢餓を直す** | **この設計では枠の消費が1件も増減しない。**人間の担当が付いた issue がボードの上位を占めると11件目以降へ届かない問題は、いまも同じように起きている。**別の issue として立てる** |

---

## 15. 確かめたこと

**言いたいこと。**設計の前提にした事実を、コマンドと出力で残す。
**対象の commit は 73fb41ae654add121c72ed01464ef77f69684812 である。**

| 主張 | 根拠 |
| --- | --- |
| **`FetchAllComments` は1件も落とさない** | [internal/tracker/adapter.go:1155](../../../internal/tracker/adapter.go#L1155) が `fetchCommentNodes(ctx, issueNodeID, maxCommentsPerFetch, 0)` を呼ぶ。`keep` が0なら打ち切らない（[internal/tracker/adapter.go:1250-1253](../../../internal/tracker/adapter.go#L1250-L1253) の `if keep > 0 && unmarked >= keep`） |
| **コメントを書き換える経路も消す経路も無い** | 検索パターン `updateIssueComment\|deleteIssueComment\|minimizeComment\|UpdateComment\|DeleteComment`、対象 `internal/` と `cmd/`、commit 73fb41ae で `wc -l` が `0` |
| **`FetchIssuesByStates` は途中で切れない** | [internal/tracker/adapter.go:609-617](../../../internal/tracker/adapter.go#L609-L617) が上限超過で `CategoryPagination` の `*Error` を返す |
| **`PostComment` が `self_marker` を先頭に付ける** | [internal/tracker/adapter.go:1110-1113](../../../internal/tracker/adapter.go#L1110-L1113) の `full = selfMarker + "\n" + body` |
| **担当者が2人以上の経路にコメントは無い** | [internal/orchestrator/handoff.go:81-86](../../../internal/orchestrator/handoff.go#L81-L86) の `return` は、`FetchAllComments`（[internal/orchestrator/handoff.go:111](../../../internal/orchestrator/handoff.go#L111)）より25行前にある |
| **`o.failures` は着手できた run しか持たない** | `noteFailure` の呼び出しは [internal/orchestrator/lifecycle.go:551](../../../internal/orchestrator/lifecycle.go#L551) と [internal/orchestrator/lifecycle.go:625](../../../internal/orchestrator/lifecycle.go#L625) の2箇所だけで、どちらも `rs *runState` を持つ |
| **知らないキーでも落ちない** | [internal/i18n/i18n.go:279-287](../../../internal/i18n/i18n.go#L279-L287) が `(no message is registered for this key: %s)` を返す |
