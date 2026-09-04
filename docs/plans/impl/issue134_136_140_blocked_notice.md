# 着手の関門で止めた issue を、ダッシュボードと issue のコメントに出す

**対象。**#134（ダッシュボードに「着手できずに止まっているもの」を出す）が代表。
#140（人間が担当者で着手できないことを、issue のコメントとして1回だけ書く）と
#136（担当者が2人以上いる issue も、着手できないことを知らせる）を同じ worktree で直す。

**この文書が正である。**[docs/plans/continuo_design.md](../continuo_design.md) の 3-68 を、この3件の範囲で具体化したもの。

**3-68 は残す。置き換えない。**あちらは着手の検査の4つの経路（worktree・信頼・枠・担当者）をまとめて扱い、
**「重複を抑える鍵は、飛ばす原因の広がりより細かくしない。worktree の経路だけが issue 単位で、残る3つはリポジトリ単位である」**
という決めごとを持っている（[docs/plans/continuo_design.md:7039-7041](../continuo_design.md#L7039-L7041)）。
**置き換えると、この文書が扱わない3つの経路の決定が、文書のどこにも残らなくなる。**
**3-68 へ足すのは1行だけである。**「担当者の経路は [docs/plans/impl/issue134_136_140_blocked_notice.md](issue134_136_140_blocked_notice.md) が正」（14 節）。

---

## 1. 言いたいこと

**言いたいこと。**飛ばした事実は、いまどこにも残っていない。
**`handoffGate` が止めた issue だけを覚える map を1つ足し、ダッシュボードの表と issue のコメントの
両方をそこから作る。**記録する場所は1関数、消す規則は 6 節の2枚の表に固定する。

**この設計が扱う範囲。**

| 何 | 出す先 |
| --- | --- |
| **人間が付けた担当者で止めた**（`ActionSkipHumanAssigned`） | ダッシュボード＋issue へ1回だけコメント |
| **担当者が2人以上で、gh の持ち主が混じっていない** | ダッシュボード＋issue へ1回だけコメント |
| **担当者が2人以上で、gh の持ち主が混じっている** | **ダッシュボードだけ。**issue へは書かない（8-3） |
| それ以外の関門（worktree・信頼・枠・ラベル・入札） | **出さない。**理由は 15 節 |

**3行目を落とさない。**ここがいちばん切り分けの難しい状態である。
**「人間が2人」なのか「人間1人＋別の機械が hold を持っている」なのかを、この分岐は区別できない**（8-3）。
**issue へ書けないからこそ、ダッシュボードには出す。**出さないと、人間から見て
「なぜ着手されないのか」を知る手がかりが WARN の1行だけになる。

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
| **空きスロットが尽きた巡回で全消し**（v1） | **「その巡回で見なかったもの」では1件も消さない。**消すのは、目の前で飛ばした1件と、ボードの候補一覧から消えたものだけである（6 節） |
| **枠で止まった巡回で全消し**（v1） | 同上。`dispatchPaused` で早く戻っても候補一覧は取れている |
| **`o.failures` に着手前の issue が入らない**（v2） | **`o.failures` を読まない。**`handoffGate` の中で自分で記録する |
| **`failureNote` が識別子を持たない**（v2） | 記録に `Identifier` / `Title` / `URL` を持たせる（4 節） |
| **理由の種類を見分けられない**（v2） | **担当者の経路は 3-66（番兵エラーの新設）に依存しない。**`handoff.Action` が既に種類を持っている |

**`FetchIssuesByStates` は途中で切れない。**ページ数が上限を超えたら
`CategoryPagination` の `*Error` を返す（[internal/tracker/adapter.go:608-617](../../../internal/tracker/adapter.go#L608-L617)）。
**したがって「エラーなしで返った候補一覧」は必ず全件である。**掃除の土台に使ってよい。

---

## 3. 止めた理由の種類

**言いたいこと。**理由に短い名前を付ける。**番号を振らない。**
名前は issue のコメントの印にそのまま埋め込み、**再起動をまたいだ照合に使う**（7 節）。

**新しいファイル。**[internal/orchestrator/gate.go](../../../internal/orchestrator/gate.go)（新規）に、この節と次の節の型と関数をまとめる。

```go
// GateReason は、着手の関門が止めた理由の種類である（issue #134 / #136 / #140）。
//
// **文字列にしてある。**issue のコメントの印に埋め込み、再起動をまたいで照合するため。
type GateReason string

const (
	// GateReasonHumanAssigned は、continuo が付けたのではない担当者が1人付いていることを表す。
	GateReasonHumanAssigned GateReason = "human_assigned"
	// GateReasonManyAssignees は、担当者が2人以上付いていて、
	// そこに gh の持ち主が混じっていないことを表す。
	GateReasonManyAssignees GateReason = "many_assignees"
	// GateReasonManyAssigneesWithSelf は、担当者が2人以上付いていて、
	// そこに gh の持ち主が混じっていることを表す（8-3）。
	//
	// **この理由では issue へ1バイトも書かない。**「人間が2人」と
	// 「人間1人＋別の機械が hold を持っている」を、この分岐は区別できないためである。
	// **だから案内の印も持たない。**
	GateReasonManyAssigneesWithSelf GateReason = "many_assignees_with_self"
)
```

| 名前 | いつ付くか | 案内の印 |
| --- | --- | --- |
| **`human_assigned`** | continuo が付けたのではない担当者が1人 | `<!-- continuo:gated:human_assigned -->` |
| **`many_assignees`** | 担当者が2人以上で、gh の持ち主が混じっていない | `<!-- continuo:gated:many_assignees -->` |
| **`many_assignees_with_self`** | 担当者が2人以上で、gh の持ち主が混じっている | **持たない**（issue へ書かない） |

---

## 4. 記録の型と置き場所

**言いたいこと。**メモリだけに持つ。**新しいファイルは1つも作らない。**
`o.mu` が守る map を1つ足し、鍵は project item の ID にする。
**判定には1度も使わない。**表示と案内のためだけに持つ。

```go
// gateNote は、着手の関門で止めた issue 1件の記録である（設計 3-68）。
//
// **永続化しない。**再起動すると消えるが、次の巡回で作り直される。
// **判定には1度も使わない。**ダッシュボードと「案内を1回だけ」のためだけに持つ。
type gateNote struct {
	// Identifier は `<owner>/<repo>#<番号>` である。
	Identifier string
	// Title は issue の題名である。**外部から来る文字列である。**
	Title string
	// URL は issue の URL である。**draft issue はここへ来ない**
	// （`handoffGate` の入口 internal/orchestrator/handoff.go:70-73 が
	// ノード ID の無い issue を `proceed: true` で返して抜ける）。
	// **空文字になるのは URL を取れなかったときだけで、そのときは画面をリンクにしない。**
	URL string
	// Reason は止めた理由の種類である。
	Reason GateReason
	// Assignees は担当者のログイン名である。**巡回のたびに上書きする。**
	// **コメントには書かない**（8-1）。ダッシュボードだけが出す。
	Assignees []string
	// Count は、**いまの Reason で**止めた回数である（案内を書く条件に使う。設計 3-68）。
	// **理由が変わったら1へ戻す**（6-5）。
	Count int
	// FirstSeenAt は、**いまの Reason で**最初に止めた時刻である。**同じ理由なら更新しない。**
	// **理由が変わったら、その巡回の時刻で置き換える**（6-5）。
	FirstSeenAt time.Time
	// LastSeenAt は、理由を問わず最後に止めた時刻である。
	LastSeenAt time.Time
	// notices は、**理由ごとの**案内の状態である（6-5）。
	//
	// **理由が変わっても消さない。**案内は `<!-- continuo:gated:<理由> -->` の印を持つので、
	// 「1回だけ」は理由ごとに数える。
	// **理由が変わるたびに消すと、担当者を2人と1人で往復させただけで同じ案内が積まれる。**
	// **逆に issue ごとに1つしか持たないと、先に書いたほうの理由が、もう一方の案内を永久に塞ぐ。**
	notices map[GateReason]gateNotice
}

// gateNotice は、1つの理由についての案内の状態である（issue #134 / #140）。
//
// **NoticedAt と Skip が同時に入ることは無い。**どちらかが入っていれば `noteGate` は偽を返す。
type gateNotice struct {
	// NoticedAt は issue へ案内を書いた時刻である。ゼロ値ならまだ書いていない。
	// **`warn_only` では立てない**（7 節）。立てると「書いた」と読める。
	NoticedAt time.Time
	// Skip は、案内を書かないと決めた理由である。空なら「まだ書いていない」。
	Skip GateNoticeSkip
	// Failed は、案内を投稿しようとして失敗したことを表す。
	//
	// **NoticedAt は立てたままにする**（8-2）。**そのうえで、画面には別の印を出す。**
	// 「書いた」と「書こうとして失敗した」が見分けられないと、
	// **issue に1件も無いのに「案内済み」と読める行がダッシュボードに残る。**
	Failed bool
}

// GateNoticeSkip は、案内を書かないと決めた理由である（issue #134 / #140）。
type GateNoticeSkip string

const (
	// GateNoticeOffByConfig は `on_assignee_gate: warn_only` で切ってあることを表す。
	GateNoticeOffByConfig GateNoticeSkip = "off_by_config"
	// GateNoticeTooManyComments は、手元のコメントが取得の上限で切れていて、
	// 前の起動で書いたかどうかを確かめられなかったことを表す（7 節）。
	GateNoticeTooManyComments GateNoticeSkip = "too_many_comments"
	// GateNoticeUnclearOwner は、担当者に gh の持ち主が混じっていて、
	// 「人間が2人」と「人間1人＋別の機械が hold を持っている」を
	// 切り分けられなかったことを表す（8-3）。
	GateNoticeUnclearOwner GateNoticeSkip = "unclear_owner"
	// GateNoticeNoBody は、その理由に issue へ書く本文が用意されていないことを表す。
	//
	// **いまの3つの理由では立たない。**理由を1つ足して本文（9 節）を書き忘れたときにだけ立つ。
	// **黙って中身の無い案内を投稿しない。**消す手段が無いので、画面に出すほうがよい。
	GateNoticeNoBody GateNoticeSkip = "no_body"
)
```

**`Orchestrator` に1行、`New` の初期化に1行。**

```go
	// gated は、着手の関門で止めた issue の記録である（issue #134 / #136 / #140）。
	// キーは project item の ID。**mu が守る。**
	gated map[string]*gateNote
```

**`o.runs` に相乗りしない。**あれは「印＝実行中」であり
（[internal/orchestrator/orchestrator.go:303-305](../../../internal/orchestrator/orchestrator.go#L303-L305)）、
入れると空きスロットの数え方（`freeSlotBlocker`）と重複判定（`lookupRunByID`）の両方が壊れる。

---

## 5. 記録する場所は2つだけ

**言いたいこと。**[internal/orchestrator/handoff.go](../../../internal/orchestrator/handoff.go) の `handoffGate` の中の2箇所で `noteGate` を呼ぶ。
**呼ぶのは「その巡回で止めたこと」を記録する1関数だけで、コメントを書くかどうかはその戻り値が決める。**

| どこ | いまの動き | 足すもの |
| --- | --- | --- |
| **担当者が2人以上**（[internal/orchestrator/handoff.go:81-86](../../../internal/orchestrator/handoff.go#L81-L86)） | WARN を1行出して `handoffDecision{}` | **gh の持ち主が担当者に混じっていないときだけ** `noteGate(…, GateReasonManyAssignees, logins)`（8-3） |
| **人間が付けた担当**（[internal/orchestrator/handoff.go:134-147](../../../internal/orchestrator/handoff.go#L134-L147)） | WARN を1行出して `handoffDecision{}` | `noteGate(…, GateReasonHumanAssigned, logins)` |

**`noteGate` の形。**

```go
// noteGate は、着手の関門で止めたことを記録し、issue へ案内を書くべきかを返す（設計 3-68）。
//
// **判定には1度も使わない。**ダッシュボードに出すためと、案内を1回だけにするために持つ。
// **理由が変わったら Count と FirstSeenAt だけを数え直す。**理由ごとの案内の状態
// （notices）は持ち越す。**何が消えて何が残るかは 6-5 の表が正である。**
//
// issue: 止めた issue。
// reason: 止めた理由の種類。
// assignees: いま付いている担当者のログイン名。
// 戻り値: 案内をまだ書いておらず、書く条件（noticeMinCount / noticeMinAge）を満たしていれば true。
func (o *Orchestrator) noteGate(issue tracker.Issue, reason GateReason, assignees []string) bool
```

**いつ案内を書くか。****同じ鍵で3回以上止め、かつ最初に止めてから60秒以上たったとき。**
**判定の式はこれである。**`n.Count >= noticeMinCount && !now.Before(n.FirstSeenAt.Add(noticeMinAge))`

**`Count == noticeMinCount` と書かない。**`polling.interval_ms` の既定は30000ミリ秒
（[internal/config/default.go:87](../../../internal/config/default.go#L87)）なので、
**3回目の巡回で最初から経つのはちょうど60秒であり、`noticeMinAge` と同じ値である。**
`==` で書くと、揺らぎで3回目の経過が59.9秒になった瞬間に条件が二度と揃わず、
**案内が永久に書かれない。**

**「続けて」ではない。**設計 3-68 は「3回続けて飛ばし」と書いているが、`Count` は
**この理由で止めた回数の累計**であって、連続の回数ではない。**この文書は累計を採る。**
**連続にできないからである。**`handoffGate` へ届かないまま終わる巡回がある。
読み取りの枠を使い切った巡回（[internal/orchestrator/handoff.go:106-110](../../../internal/orchestrator/handoff.go#L106-L110) の
`takeHandoffFetch` が偽を返して `stop: true` で戻る）と、`dispatchAllowed` が偽で
`dispatchCandidates` を呼ばなかった巡回である。**そこを「連続が切れた」と数えると、
止まり続けている issue の案内が永久に書かれない。**

**数えなかった巡回は、数え直しの対象にもしない。**`Count` は減らない（6 節の規則で消えるときだけ、記録ごと落ちる）。
**3-68 の文面も「3回以上」に直す**（14 節）。

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

## 6. 記録を消す規則

**言いたいこと。**v1 は「その巡回で見なかったものを消す」で落ちた。
**この版は「見なかった」では1件も消さない。**消すのは
**「関門で止めた以外の結末に届いた」か「ボードの候補一覧から消えた」ときだけである。**

| いつ消すか | どこで | なぜ |
| --- | --- | --- |
| **関門より前で飛ばしたとき** | `dispatchCandidates` の6つの `continue`（下の表） | 止めているのは関門ではない。**古い理由と誤った直し方を出し続けない** |
| **関門が2つの理由以外で戻ったとき** | `handoffGate`（通した・譲った・入札の待ちと敗北） | 直ったのに残ると、直しても消えない行がダッシュボードに残る |
| **ボードの候補から消えたとき** | `Tick`（候補の取得が成功したときだけ） | issue を閉じた・Status を動かした・ボードから外した |

**消さないのは「判定できなかった」ときだけである。**

| 消さない場面 | なぜ |
| --- | --- |
| **空きスロットが尽きて `break` した**（[internal/orchestrator/dispatch.go:246](../../../internal/orchestrator/dispatch.go#L246)） | v1 の穴。順番を待っているだけで、状態は変わっていない |
| **枠で止まって `dispatchCandidates` を呼ばなかった** | 同上 |
| **読み取りの枠を使い切った**（`takeHandoffFetch` が偽） | 関門の判定まで届いていない |
| **コメントを読めない・gh の持ち主を取れない** | 材料が無い。**判定していない** |

### 6-1. `dispatchCandidates` で消す6箇所

**言いたいこと。**どれも `handoffGate`（[internal/orchestrator/dispatch.go:273](../../../internal/orchestrator/dispatch.go#L273)）より前にある `continue` である。
**ここを塞がないと、ボードに残ったまま関門へ到達しなくなった issue の行が、
古い理由と誤った直し方を付けて永久に残る。**

**`clearGate` は案内を書いた事実を残す**（6-5）ので、**「消すと数え直しになる」は案内には効かない。**
数え直すのは `Count` と `FirstSeenAt` だけであり、**同じ理由の案内が2件目として書かれることはない。**

| どこ | 何が起きたか |
| --- | --- |
| [internal/orchestrator/dispatch.go:191](../../../internal/orchestrator/dispatch.go#L191) の `lookupRunByID` | 既に着手している |
| [internal/orchestrator/dispatch.go:198](../../../internal/orchestrator/dispatch.go#L198) の `!containsFold(active_states, State)` | 人間が Status を動かした。**索引の反映が追いつくまでここへ落ち続ける** |
| [internal/orchestrator/dispatch.go:205](../../../internal/orchestrator/dispatch.go#L205) の `skipByFailure` | 同じ理由で失敗し続けている |
| [internal/orchestrator/dispatch.go:208](../../../internal/orchestrator/dispatch.go#L208) の `!issue.Dispatchable` | 未信頼のリポジトリである |
| [internal/orchestrator/dispatch.go:219](../../../internal/orchestrator/dispatch.go#L219) の `missingRequiredLabels` | 必須のラベルが足りない |
| [internal/orchestrator/dispatch.go:266](../../../internal/orchestrator/dispatch.go#L266) の `preflight` が偽 | 段0 で落ちた |

**それぞれの `continue` の直前に `o.clearGate(issue.ID)` を1行足す。**
**`break`（空きスロット切れ）と `ctx.Err()` には足さない。**足すと v1 の穴が開く。

**巡回の番号を持たない。**「この巡回で見た候補の集合」を作って差を取る形にすると、
**`break` した巡回でその集合が欠け、v1 と同じ全消しへ戻る。**
**消すのは、いま目の前で飛ばした1件だけにする。**

### 6-2. `handoffGate` は戻る直前に自分で片付ける

**言いたいこと。**戻り口が9つある。**1つずつ `clearGate` を書かない。**
**「判定できたか」と「関門の2つの理由で止めたか」の2つの真偽値で決める。**

```go
func (o *Orchestrator) handoffGate(ctx context.Context, issue tracker.Issue) handoffDecision {
	nodeID := issueNodeID(issue)
	if nodeID == "" { /* いまのまま */ }

	// judged は「この巡回でこの issue を判定できたか」である。**既定は真。**
	// **判定できなかった経路だけが偽を入れる**（gh の持ち主を取れない・
	// 読み取りの枠を使い切った・コメントを読めない）。
	// noted は「関門の2つの理由のどちらかで止めたか」である。
	judged := true
	noted := false
	defer func() {
		if judged && !noted {
			o.clearGate(issue.ID)
		}
	}()
	…
}
```

| 戻り口 | `judged` | `noted` | 記録 |
| --- | --- | --- | --- |
| `ActionProceed` / 入札に勝った（`bidForIssue`） | 真 | 偽 | **消す** |
| `ActionSkipHeld` / `ActionSkipOtherMachine` / `ActionSkipSelfUnknown` | 真 | 偽 | **消す** |
| 入札の待ちと敗北・[internal/orchestrator/handoff.go:91-96](../../../internal/orchestrator/handoff.go#L91-L96) の早い戻り | 真 | 偽 | **消す** |
| **`ActionSkipHumanAssigned`** / **担当者が2人以上**（gh の持ち主が混じっていてもいなくても。8-3） | 真 | **真** | **`noteGate` が書き直す** |
| gh の持ち主を取れない・読み取りの枠切れ・コメントを読めない | **偽** | 偽 | **そのまま** |

**入札に勝った直後に `clearGate` を書き足さない。**`bidForIssue` を返す戻り口も
`judged` が真・`noted` が偽で通るので、この `defer` が消す。

**この表が塞ぐ穴。**人間が担当者を外したあと、この機械が入札に負けて別の機械が担当になると、
以後は `ActionSkipHeld` / `ActionSkipOtherMachine` へ落ちる。
**そこで消さないと「担当者が付いています（外れた人の名前）」が永久に出続ける。**

### 6-3. `Tick` に `else` の節を1つ足す

[internal/orchestrator/orchestrator.go:560-564](../../../internal/orchestrator/orchestrator.go#L560-L564) を、次の形へ**置き換える**。
**足すのは `else { … }` だけで、`FetchIssuesByStates` の呼び出しは1回のままである**
（直後に同じブロックを並べると、同じ巡回で2回走って GraphQL が1本増える）。

```go
	candidates, err := o.tracker.FetchIssuesByStates(ctx, o.cfg.Tracker.ActiveStates)
	if err != nil {
		o.logger.Warn("候補の取得に失敗しました（この巡回の dispatch は行いません）", "error", err)
		dispatchAllowed = false
	} else {
		// **取れた候補一覧に居ないものだけを消す**（設計 3-68 の「通ったら印を消す」の補い）。
		// **`dispatchCandidates` のループは見ない。**空きスロットが尽きて途中で
		// 打ち切られた巡回でも、記録は1件も減らない。
		o.forgetGatedNotOnBoard(candidates)
	}
```

```go
// forgetGatedNotOnBoard は、いまのボードの候補に居ない issue の記録を外す（設計 3-68）。
//
// **候補の取得が成功した巡回でだけ呼ぶ。**失敗した巡回で呼ぶと全件が消える。
//
// candidates: `FetchIssuesByStates` が返した候補（全件）。
func (o *Orchestrator) forgetGatedNotOnBoard(candidates []tracker.Issue)
```

**名前に `sweep` を使わない。**この package の `sweep` は既に
「起動時に worktree を掃除する」を指している（[internal/orchestrator/sweep.go:25](../../../internal/orchestrator/sweep.go#L25) の
`SweepOnStartup` と [internal/orchestrator/sweep.go:63](../../../internal/orchestrator/sweep.go#L63) の `sweepFinishedWorktrees`）。
**同じ動詞が別の対象を指すと、読む人が探す場所を間違える。**

**落とす条件を「閉じた・ボードから外した」に絞らない。**候補一覧は
`active_states` で絞った結果でしかなく、**「閉じた」と「Status を動かした」を区別する材料がここに無い。**
区別するには issue を1件ずつ引き直すことになり、**巡回1回のリクエストが候補の数だけ増える。**

### 6-4. 記録がゼロから数え直しになる3つの場面

**言いたいこと。**どれも `forgetGatedNotOnBoard` が「ボードの候補一覧に居ない」だけを見ることから来る。
**`Count` も `FirstSeenAt` も `notices` も消えるので、3巡回と60秒でもう一度案内を書く条件が揃う。**

| 場面 | 何が起きるか |
| --- | --- |
| **continuo を再起動した** | 記録はメモリだけにある（4 節）ので全部消える |
| **人間が Status を出し入れした** | そのあいだ候補一覧から外れる |
| **ボードの索引の反映が遅れた** | **1巡回だけ候補一覧から外れる**（設計 3-34） |

| 理由 | 2度目が書かれるか |
| --- | --- |
| **人間が付けた担当** | **書かれない。**手元のコメントを見る（`gateNoticedIn`。7 節）ので、印が消えても前の案内が見つかる |
| **担当者が2人以上**（gh の持ち主が混じっていない） | **書かれることがある。**この経路はコメントを読まない。**3つの場面すべてを案内の本文に書く**（9 節） |
| **担当者が2人以上**（gh の持ち主が混じっている） | **そもそも1度も書かない**（8-3）。ダッシュボードにだけ出る |

### 6-5. 理由が変わったときに、何を数え直して何を残すか

**言いたいこと。**理由が変わったら **`Count` と `FirstSeenAt` だけ**を数え直す。
**理由ごとの案内の状態（`notices`）は持ち越す。**
**どちらに倒しても壊れるので、数え直す対象をフィールド単位で固定する。**

**全部を数え直すと、案内が積まれる。**担当者を2人と1人で往復させると、
**そのたびに `notices` が消えて、同じ理由の案内をもう一度書ける状態に戻る。**
**書いた案内を消す手段は無い**（8-1）。

**何も数え直さないと、片方の案内が永久に書かれない。**`human_assigned` で1回書いたあとに
`many_assignees` へ変わっても、案内済みの印が issue に1つしか無いと `noteGate` が偽を返し続ける。
**別の理由の案内が1度も出ない。**

**採る形。**

| フィールド | 理由が変わったとき | `clearGate`（関門で止めていない） | `forgetGatedNotOnBoard`（ボードから消えた） |
| --- | --- | --- | --- |
| `Reason` | **新しい理由で置き換える** | **空にする**（画面から消える印） | 消える |
| `Count` | **1へ戻す** | 0 へ戻す | 消える |
| `FirstSeenAt` | **その巡回の時刻で置き換える** | ゼロ値へ戻す | 消える |
| `Identifier` / `Title` / `URL` / `Assignees` | 上書きする | `Assignees` だけ落とす | 消える |
| **`notices`（理由ごと）** | **持ち越す** | **持ち越す** | **消える** |

**`clearGate` が `notices` を残す理由。**issue に書いたコメントは、この機械が
関門より前で1巡回飛ばしたくらいでは消えない。**消すと、`preflight` が1巡回落ちただけで
記録が作り直され、同じ本文の案内の2件目が書かれる。**書いたコメントを消す手段は無い（8-1）。

**`Reason` が空の記録は `GateViews` が1件も返さない。**「いまは関門で止めていない」ためである。
**案内を1件も書いていない記録は、`clearGate` がそのまま落とす**（残すものが無い）。

**`Count` と `FirstSeenAt` を数え直す理由。**新しい理由も、それ自体で3巡回と60秒
（`noticeMinCount` / `noticeMinAge`。5 節）持ちこたえてから案内する。
**持ち越すと、人間が担当者を付け替えている最中の1巡回で、別の理由の案内が出る。**

**`notices` を持ち越す理由。**案内の本文が持つ印は `<!-- continuo:gated:<理由> -->` であり（3 節）、
**「1回だけ」は理由ごとの約束である**（9 節の本文も「この理由につき1回だけ書きます」と書いている）。
**担当者が1人のときだけ、アカウントごとの約束でもある**（`gateNoticedIn` がコメントを読んで照合するのは、その経路だけである）。
**理由は3つしか無いので、この map は1件につき最大3要素である。**

**記録ごと落ちたら `notices` も落ちる。**そのことは 6-4 が扱っており、
`many_assignees` の案内の本文にも書いてある（9 節）。

---

## 7. 案内を1回だけにする仕組み（2層）

**言いたいこと。**`noteUntrusted` を真似る。**印を持つ・ログを出す・1回だけコメントする、を1関数に収める。**
**人間が付けた担当の経路では、手元のコメントも見て、再起動をまたいでも1回にする。**
担当者が2人以上の経路にはコメントが無いので、メモリの印だけで抑える。

| 層 | 何を見るか | どちらの理由に効くか |
| --- | --- | --- |
| **メモリの印**（`gateNote.NoticedAt`） | `o.gated` の記録 | **両方** |
| **手元のコメント**（`gateNoticedIn`） | `FetchAllComments` が返した全件 | **人間が付けた担当だけ** |

**呼び出しの形。**担当者が2人以上の経路（[internal/orchestrator/handoff.go:81-86](../../../internal/orchestrator/handoff.go#L81-L86)）。
**`viewerIdentity` を先に引いて、gh の持ち主が混じっていないことを確かめる**（8-3）。

```go
	if len(logins) >= 2 {
		o.logger.Warn( /* いまのまま */ )
		viewer, ok := o.viewerIdentity(ctx)
		switch {
		case !ok:
			// **gh の持ち主が分からない。**別の機械の担当かどうかを切り分けられないので、
			// 記録も案内も作らない。**記録は消さない**（判定していない。6-2）。
			judged = false
		case containsFold(logins, viewer.Login):
			// **continuo のアカウントが混じっている。**別の機械が担当している見込みなので、
			// 人間に外させない（8-3）。**issue へは1バイトも書かない。**
			// **記録は作る。**いちばん切り分けの難しい状態を、ダッシュボードから消さない。
			noted = true
			if o.noteGate(issue, GateReasonManyAssigneesWithSelf, logins) {
				o.markGateNoticeSkipped(issue.ID, GateReasonManyAssigneesWithSelf, GateNoticeUnclearOwner)
			}
		default:
			noted = true
			if o.noteGate(issue, GateReasonManyAssignees, logins) {
				o.postGateNotice(ctx, issue, nodeID, GateReasonManyAssignees)
			}
		}
		return handoffDecision{}
	}
```

**`containsFold` は既にある**（[internal/orchestrator/lifecycle.go:929](../../../internal/orchestrator/lifecycle.go#L929)。大文字小文字を無視して比べる）。

**人間が付けた担当の経路**（[internal/orchestrator/handoff.go:134-147](../../../internal/orchestrator/handoff.go#L134-L147)）。
**`comments` も `truncated` も `viewer` も手元にある**（7-1 で `FetchAllComments` の戻り値に `truncated` を足す）。

```go
	case handoff.ActionSkipHumanAssigned:
		o.logger.Warn( /* いまのまま */ )
		noted = true
		if o.noteGate(issue, GateReasonHumanAssigned, logins) {
			at, found := gateNoticedIn(comments, viewer.Login, GateReasonHumanAssigned)
			switch {
			case found:
				// **前の起動で書いてある。**印だけ立てて、二度と書かない。
				o.markGateNoticed(issue.ID, GateReasonHumanAssigned, at)
			case truncated:
				// **古い側を読み切れていない。**前に書いたかどうかを確かめられないので書かない。
				o.logger.Warn("コメントが多すぎて、前に案内を書いたかどうかを確かめられないので書きません",
					"identifier", issue.Identifier)
				o.markGateNoticeSkipped(issue.ID, GateReasonHumanAssigned, GateNoticeTooManyComments)
			default:
				o.postGateNotice(ctx, issue, nodeID, GateReasonHumanAssigned)
			}
		}
		return handoffDecision{}
```

**`found` を先に見る。**新しい側に案内が残っているなら、古い側が切れていても答えは出ている。
**逆にすると、コメントの多い issue で「前に書いてある」ことが分かっているのに
`too_many_comments` の印が立ち、ダッシュボードが嘘をつく。**

**投稿するかどうかを決める道は6つあり、決める場所は2つに分かれる。**
**どの道も、書き込む先は `notices[その理由]` の1件である**（6-5）。

| 道 | 決める場所 | `NoticedAt` | `Skip` |
| --- | --- | --- | --- |
| **前の起動で書いてある** | **呼び出し側**の `markGateNoticed` | **立てる**（そのコメントの時刻） | 空 |
| **手元のコメントが上限で切れていた** | **呼び出し側**の `markGateNoticeSkipped` | 立てない | `"too_many_comments"` |
| **gh の持ち主が担当者に混じっていた**（8-3） | **呼び出し側**の `markGateNoticeSkipped` | 立てない | `"unclear_owner"` |
| **`warn_and_comment`**（既定） | `postGateNotice` | **立てる**（投稿の前に） | 空 |
| **`warn_only`** | `postGateNotice` | 立てない | `"off_by_config"` |
| **その理由の本文が無い**（9 節に書き忘れたとき） | `postGateNotice` | 立てない | `"no_body"` |

**切れの検査を `postGateNotice` の中に置かない。**この関数は `comments` を受け取らないので、
**切れていたかどうかを知る手段が1つも無い。**
**`postGateNotice` が見るのは `on_assignee_gate` だけである。**

**6つとも、その理由については次の巡回で `noteGate` が偽を返す。**
`notices[その理由]` の `NoticedAt` と `Skip` のどちらかが入っているからである。
**別の理由へ変わったら、その理由の `notices` は空なので、また真を返しうる**（6-5）。
**毎巡回 true を返させない。**返させると、`warn_only` のあいだ `postGateNotice` が
毎巡回呼ばれ、同じ WARN が巡回のたびに1行ずつ積まれる。
**設定を変えたら、再起動で見直される**（記録はメモリだけに持つ。4 節）。

```go
// postGateNotice は、案内を issue へ1回だけ書く（設計 3-68）。
//
// **設定を先に見る。**`warn_only` のときは NoticedAt を立てない。
// 立てると「issue へ書いた」と読める印が付き、ダッシュボードから
// 「書いていない」ことが読めなくなる。代わりに NoticeSkip へ "off_by_config" を入れる。
// **投稿に失敗しても NoticedAt は残す**（8-2）。
func (o *Orchestrator) postGateNotice(ctx context.Context, issue tracker.Issue, nodeID string, reason GateReason)

// markGateNoticed は、issue に既に案内があったことを記録する。**投稿はしない。**
//
// reason: どの理由の案内か。**理由ごとに1つずつ持つ**（6-5）。
// at: 見つかった案内のコメントの時刻。
func (o *Orchestrator) markGateNoticed(issueID string, reason GateReason, at time.Time)

// markGateNoticeSkipped は、案内を書かないと決めたことを記録する。**投稿はしない。**
//
// reason: どの理由の案内か。**理由ごとに1つずつ持つ**（6-5）。
// skip: 書かないと決めた理由。
func (o *Orchestrator) markGateNoticeSkipped(issueID string, reason GateReason, skip GateNoticeSkip)
```

**`gateNoticedIn` は、印だけで「continuo が書いた」と決めない**（設計 3-65）。
**投稿者が gh の持ち主であるものだけを数える。**持ち主が空文字なら常に偽を返す。

### 7-1. 切れたことは、件数ではなくアダプタから受け取る

**言いたいこと。**`FetchAllComments` の戻り値に真偽値を1つ足す。
**件数で当てない。**両方向に外れる。

**`FetchAllComments` は2000件までしか読まない。**
1ページ100件（[internal/tracker/query.go:318](../../../internal/tracker/query.go#L318) の `maxCommentsPerFetch`）で
20ページ（[internal/tracker/query.go:266](../../../internal/tracker/query.go#L266) の `maxCommentPages`）が上限である。
**取り方は新しい順（`orderBy: { field: UPDATED_AT, direction: DESC }`）なので、
上限に達すると落ちるのは古い側である**（[internal/tracker/adapter.go:1232-1264](../../../internal/tracker/adapter.go#L1232-L1264)）。
**前の起動で書いた案内は古い側にあるので、いちばん落ちやすい。**
**書けないことより、同じ案内を2件書くことのほうが困る。**消す手段が無いからである（8-1）。

**`len(comments) >= 2000` では当てられない。**

| 外れ方 | いつ起きるか | 何が起きるか |
| --- | --- | --- |
| **切れているのに気づけない** | 100件に満たないページが混じったまま20ページを使い切ったとき | **案内が2件書かれる**（消せない） |
| **切れていないのに切れたことにする** | ちょうど2000件で `hasNextPage` が偽のとき | 案内が永久に書かれず、ダッシュボードに誤った印が出る |

**そこで、打ち切ったかどうかをアダプタが返す。**
**いま WARN を出している条件と同じものを、真偽値にして返すだけである**
（[internal/tracker/adapter.go:1261-1264](../../../internal/tracker/adapter.go#L1261-L1264)。
続きの cursor がありながら `maxCommentPages` を使い切ったとき）。

```go
// FetchAllComments は issue に付いたコメントを1件残らず取る（設計 3-77a）。
//
// 戻り値の1つ目: 正規化したコメントの一覧（**古い順**）。
// 戻り値の2つ目: **ページ数の上限で古い側を読み切れなかったら true**（issue #140）。
// 戻り値の3つ目: エラー。
func (a *Adapter) FetchAllComments(
	ctx context.Context,
	issueNodeID string,
	_ config.TrackerProviderCommentsConfig,
) ([]Comment, bool, error)
```

**`tracker.MaxCommentsFetched` は公開しない。**件数で当てるのをやめるので要らない。

**触る場所は5つである。**

| どこ | どうするか |
| --- | --- |
| [internal/tracker/adapter.go:1222](../../../internal/tracker/adapter.go#L1222) の `fetchCommentNodes` | 戻り値に `truncated bool` を足す。**`keep` で抜けたときは偽**（狙って止めたので、切れていない） |
| [internal/tracker/adapter.go:1061](../../../internal/tracker/adapter.go#L1061) の `FetchComments` | `_` で捨てる（`keep` で止める経路である） |
| [internal/tracker/adapter.go:1150](../../../internal/tracker/adapter.go#L1150) の `FetchAllComments` | そのまま返す |
| [internal/orchestrator/orchestrator.go:129](../../../internal/orchestrator/orchestrator.go#L129) の `Tracker` interface | 署名を揃える |
| [internal/orchestrator/handoff.go:111](../../../internal/orchestrator/handoff.go#L111) と [:716](../../../internal/orchestrator/handoff.go#L716) | 111 は `truncated` を使う。716（担当を確かめ直す経路）は `_` で捨てる |

**continuo 自身が書いたコメントも、切れていなければそのまま返る**（`keep` に0を渡すので
[internal/tracker/adapter.go:1150-1158](../../../internal/tracker/adapter.go#L1150-L1158) は途中で打ち切らない）。

---

## 8. 決めた3つ

**言いたいこと。**担当者が変わっても案内は書き直さない。**そのために、担当者の名前を案内に書かない。**
通知は切れるようにする。**切っても、記録とダッシュボードと WARN は残る。**
**gh の持ち主が担当者に混じっているときは、案内も記録も作らない。**

### 8-1. 担当者が変わっても書き直さない

**採る形。****案内に担当者のログイン名を1文字も書かない。**

**なぜ。****書き直す手段が無い。**[internal/tracker](../../../internal/tracker) にコメントを書き換える経路も消す経路も無い
（`updateIssueComment` / `deleteIssueComment` / `minimizeComment` を
検索パターン `updateIssueComment|deleteIssueComment|minimizeComment|UpdateComment|DeleteComment`、
対象 `internal/` と `cmd/`、commit 73fb41ae654add121c72ed01464ef77f69684812 で0件）。
**書き直すとは「もう1件足す」ことでしかなく、担当者を付け替えるたびに古い案内が積まれる。**

**書かなくても困らない。****やることは担当者が誰であっても同じである**（GitHub の画面で担当者を外す）。
**誰が付いているかは issue の画面の右側に、いま現在の値が出ている。**
**担当者の名前は WARN の1行とダッシュボードに出す。**どちらも常に最新である。

### 8-2. 通知は切れる

**採る形。**設定を1つ足す。`trust.on_untrusted` と同じ「決められた値だけを通す」形にする。
**ただし検査を書く場所は違う。**`validateHandoff` に足す（14 節）。

```yaml
tracker:
  provider:
    handoff:
      on_assignee_gate: warn_and_comment   # 担当者が付いていて着手できないとき（1人でも2人以上でも）の扱い。warn_only にすると issue へ書かない
```

**名前に `human` を入れない。**この設定は `postGateNotice` の中の1箇所で見るので、
**人間が付けた担当（1人）と、担当者が2人以上の両方に効く。**
`on_human_assignee` という名前だと、#136（担当者が2人以上いる issue も、着手できないことを知らせる）の
案内も止まることが、名前からも雛形の説明文からも読めない。

| 値 | WARN | ダッシュボード | issue へのコメント |
| --- | --- | --- | --- |
| **`warn_and_comment`**（既定） | 出す | 出す | **書く**（`human_assigned` と `many_assignees`） |
| **`warn_only`** | 出す | 出す | **書かない**（どちらの理由でも） |

**`many_assignees_with_self` は、この設定に関わらず1バイトも書かない**（8-3）。
**ダッシュボードには、どちらの値でも出る。**

**この設定は、記録そのものには一切効かせない。**`noteGate` は値を見ずに必ず記録し、
**値を見るのはコメントを投稿する直前の1箇所だけである。**
**`warn_only` のときは `NoticedAt` を立てず、`notices[その理由].Skip` に `"off_by_config"` を入れる**（7 節）。
ダッシュボードは、その行に「issue へは書かない設定です」の印を出す（11 節）。

**#140（人間が担当者で着手できないことを、issue のコメントとして1回だけ書く）が挙げた過去の失敗を、
この置き方で避ける。**

> **「投稿が成功したときだけ map に入れる」と「通知を切れる設定」を同時に入れたので、通知を切ると飢餓が戻ります。**

**印は投稿の成否に関わらず付ける**（`noteUntrusted` も
[internal/orchestrator/dispatch.go:644-649](../../../internal/orchestrator/dispatch.go#L644-L649) で先に印を付けてから投稿する）。
**読み取りの枠（`maxHandoffFetchesPerPoll`）の消費は、この設計で1件も増減しない。**

### 8-3. gh の持ち主が担当者に混じっているときは、案内を書かない。**記録は作る**

**採る形。**担当者が2人以上の分岐で `viewerIdentity` を引き、
**担当者の中に gh の持ち主のログイン名があるときは、`postGateNotice` を呼ばない。**
**`noteGate` は呼ぶ。**理由の名前は `many_assignees_with_self` で、
案内の状態には `unclear_owner` を入れる（7 節のコード例）。

**ダッシュボードには出す。**8-2 が「この設定は記録そのものには一切効かせない」と決めており、
**issue へ書くかどうかと、画面に出すかどうかは別の判断である。**
**ここを出さないと、いちばん切り分けの難しい状態が画面から消える。**
人間から見て「なぜ着手されないのか」の手がかりが、WARN の1行だけになる。
**その1行は、次の巡回のログに流されて埋まる。**

**なぜ。**この分岐は [internal/orchestrator/handoff.go:81-86](../../../internal/orchestrator/handoff.go#L81-L86) にあり、
`FetchAllComments`（[internal/orchestrator/handoff.go:111](../../../internal/orchestrator/handoff.go#L111)）より25行前である。
**hold のコメントを1行も読まないので、「人間が2人」と「人間1人＋別の機械が hold を持っている」を区別できない。**

**後者で「担当者をすべて外してください」と案内すると、人間は走っている別の機械の担当を外すことになる。**
その機械は止まらない（[internal/orchestrator/handoff.go:699-711](../../../internal/orchestrator/handoff.go#L699-L711) が
「担当者が1人もいないだけでは止めない」と決めている）一方で、担当者が0人になった issue は
**次の巡回で入札の対象になる。同じ issue に2台が乗る。**

**この切り分けに追加のリクエストは要らない。**`viewerIdentity` は一度取れたら覚える
（[internal/orchestrator/handoff.go:473-501](../../../internal/orchestrator/handoff.go#L473-L501)）ので、
定常状態では0本である。**読み取りの枠（`maxHandoffFetchesPerPoll`）はコメントの取得だけを数えるので、1件も使わない。**

**持ち主を取れなかったときは案内しない。**切り分けられないまま書くほうが害が大きい。
**記録も触らない**（判定していない。6-2 の `judged = false`）。

**「持ち主を取れなかった」と「持ち主が混じっていた」を、同じ扱いにしない。**
前者は**判定できていない**ので、古い記録をそのまま残して次の巡回に賭ける。
後者は**判定できている**（担当者が2人以上で、そこに gh の持ち主が居る、と分かっている）ので、
その事実を記録に書き、ダッシュボードに出す。

**文面は「担当者をすべて外してください」のままでよい。**この分岐で案内を書くのは
**continuo のアカウントが1人も混じっていないとき**だけなので、外す相手は全員が人間である。
**「continuo 以外の担当者を外してください」とは書かない。**continuo が付いていない issue で
その文を読むと、外さなくてよい人が居るように読める。

---

## 9. 案内の本文

**言いたいこと。**理由ごとに1本ずつ書く。**担当者の名前は書かない**（8-1）。
**担当者が2人以上の版だけ、着手待ちの一覧から外れるともう一度書くことがある旨を添える**（6-4）。

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

担当者を、この issue を処理させたい PC の gh の持ち主にしておく道もあります。その PC が着手します。
ただし、そのアカウントを使う continuo が1台だけのときに限ります。
同じアカウントで2台以上動かしていると、2台とも「自分の担当だ」と読み、同時に着手します。

どちらの道も、いま付いている担当者が作業中でないことを確かめてから行ってください。

この案内は、この理由につき、そのアカウントにつき1回だけ書きます。
```

**担当者が2人以上のとき。**

```
<!-- continuo:self -->
<!-- continuo:gated:many_assignees -->
この issue には担当者が2人以上付いているため、continuo は着手しません。
人間が作業を分担していると判断しています。

着手させるには、GitHub の画面で担当者を1人も付いていない状態にしてください。

この案内は、この理由につき1回だけ書きます。
ただし、この issue が着手待ちの一覧から一度外れて戻ると、もう一度書くことがあります。
外れるのは、continuo を再起動したとき、Status を着手待ちから一度外して戻したとき、
GitHub の検索の反映が遅れて1巡回だけ一覧に出なかったときです。
この経路は issue のコメントを読まないので、前に書いたことを手元から確かめられません。
```

**`many_assignees_with_self` の本文は作らない。**この理由では issue へ1バイトも書かない（8-3）。
**`buildGatedComment` は知らない理由に空文字を返し、`postGateNotice` はそれを投稿せずに
`no_body` の印を立てる。**理由を足して本文を書き忘れても、中身の無い案内が issue へ残らない。

**本文は `postComment` を通る。**[internal/orchestrator/comment.go:402-412](../../../internal/orchestrator/comment.go#L402-L412) の `postCommentWithMarker` が
手元の絶対パスを `~` へ縮める唯一の場所である。**この案内はパスを1つも載せないが、経路は揃える。**

**`buildGatedComment` が返すのは2行目からである。**1行目の `<!-- continuo:self -->` は
[internal/tracker/adapter.go:1110-1113](../../../internal/tracker/adapter.go#L1110-L1113) が `selfMarker + "\n" + body` で付ける。

---

## 10. ダッシュボードに出す欄

**言いたいこと。**`Snapshot` にフィールドを1つ、`RunSource` にメソッドを1つ足し、表を1つ増やす。
**`orchestrator` の型をそのまま出さない。**表示用の型へ写す（既存の `Run` と同じ流儀）。

**`internal/orchestrator` 側に足す写しの型。**

```go
// GateView は、着手の関門で止めた issue の写しである（issue #134）。
//
// **`o.gated` の中身をそのまま渡さない**（設計 3-25）。呼ばれた時点の値を写して返す。
type GateView struct {
	// Identifier は issue の識別子（`<owner>/<repo>#<番号>`）である。
	Identifier string
	// Title は issue の題名である。**外部から来る文字列である。**
	Title string
	// URL は issue の URL である。**空文字なら画面はリンクにしない**（取れなかったときだけ空になる）。
	URL string
	// Reason は止めた理由の種類である。**文言に直すのは internal/server である。**
	Reason GateReason
	// Assignees は担当者のログイン名である。**新しいスライスへ写したものである。**
	Assignees []string
	// Since は最初にこの理由で止めた時刻である。
	Since time.Time
	// Noticed は、**いまの Reason について** issue へ案内を書き終えているかである
	// （`notices[Reason].NoticedAt` が入っているか。6-5）。
	Noticed bool
	// NoticeSkip は、**いまの Reason について**案内を書かないと決めた理由である
	// （空なら「まだ書いていない」。`notices[Reason].Skip` の写し）。
	NoticeSkip GateNoticeSkip
}

// GateViews は、着手の関門で止めた issue の写しを返す（順序は不定）。
func (o *Orchestrator) GateViews() []GateView
```

**`Assignees` は新しいスライスへ写す。****構造体の代入ではスライスのヘッダしか写らない。**

```go
		out = append(out, GateView{
			// … 値型のフィールドはそのまま写す …
			Assignees: append([]string(nil), n.Assignees...),
		})
```

**先例の `RunView`（[internal/orchestrator/orchestrator.go:1158-1192](../../../internal/orchestrator/orchestrator.go#L1158-L1192)）は
フィールドが全部値型で、スライスを1つも持っていない。**だから
[internal/orchestrator/orchestrator.go:1197-1220](../../../internal/orchestrator/orchestrator.go#L1197-L1220) の `RunViews` は
そのまま代入していて安全に成立している。**ここで初めてスライスが入る。**

**`noteGate` の側も、受け取った `assignees` を写して持つ。**
**`o.gated` に入れたスライスの中身を、あとから書き換えない。**
巡回のたびに `Assignees` を差し替える（`n.Assignees = append([]string(nil), assignees...)`）。
**`o.mu` の外に出たスライスへ書き込むと、ダッシュボードが読んでいる最中の値が変わる。**

**`internal/server` 側。**

```go
type RunSource interface {
	RunViews() []orchestrator.RunView
	// GateViews は着手の関門で止めた issue の写しを返す（issue #134）。
	GateViews() []orchestrator.GateView
}

// Snapshot に足すのは1行である。
	// Gated は着手の関門で止めた issue である（issue #134）。
	// **Since の古い順、同じなら Identifier の昇順に並べてある。**
	// GateViews の順序は不定で、sort.Slice は安定ではない。
	Gated []Gated `json:"gated"`
```

**鍵を `Since` 1本にしない。**`Since` は `FirstSeenAt` の写しであり、
**同じ巡回で2件以上が同時に止まると `o.now()` の値が同じになりうる。**
`sort.Slice` は安定ではないので、同値のときは10秒ごとの再読み込みで行が入れ替わる。
**この節が並べる理由として挙げたことが、そのまま起きる。**

```go
	sort.Slice(gated, func(i, j int) bool {
		if !gated[i].Since.Equal(gated[j].Since) {
			return gated[i].Since.Before(gated[j].Since)
		}
		return gated[i].Identifier < gated[j].Identifier
	})
```

**既存の `Runs` は `Identifier` 1本で並べていて一意になっている**
（[internal/server/view.go:142](../../../internal/server/view.go#L142)）。
**`Identifier` は `<owner>/<repo>#<番号>` なので、`o.gated` の鍵（project item の ID）が
1件につき1つである以上、重複しない。**

---

## 11. ダッシュボードの表示用の型と表

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
	// NoticeSkip は案内を書かないと決めた理由である（機械が読むための値。`"off_by_config"` など）。
	NoticeSkip string `json:"notice_skip"`
	// NoticeBadge は行に添える印の文言である（資源から引いた文言）。**空なら印を出さない。**
	// **テンプレートで3分岐させない。**どの印を出すかは view.go が1箇所で決める。
	NoticeBadge string `json:"notice_badge"`
}
```

**`NoticeBadge` に何が入るか。**

| いまの状態 | 引くキー | 出る文言 |
| --- | --- | --- |
| **投稿に失敗した**（`Noticed` も真） | `dashboard.badge_notice_failed` | issue へ書けませんでした |
| **書き終えている**（`Noticed`） | 引かない | 空（印を出さない） |
| **まだ書いていない** | `dashboard.badge_not_noticed` | issue へは未通知 |
| **`warn_only` で切ってある** | `dashboard.badge_notice_off` | issue へは書かない設定です |
| **コメントが上限で切れていた** | `dashboard.badge_notice_capped` | コメントが多すぎて確かめられません |
| **gh の持ち主が担当者に混じっていた**（8-3） | `dashboard.badge_notice_unclear_owner` | 別の機械の担当かどうかを切り分けられません |
| **その理由の本文が無い** | `dashboard.badge_notice_no_body` | この理由に書く本文が用意されていません |
| **投稿に失敗した**（`Noticed` も真） | `dashboard.badge_notice_failed` | issue へ書けませんでした |

**表。**列は4つ。`{{ t "dashboard.caption_gated" }}` を見出しにして、実行中の run の表の**上**に置く。
**上に置く理由。**「実行中の run はありません」しか出ない画面を見に来た人が探しているのは、こちらである。

**列の見出しは、12 節で決めたキーがそのまま出る。**1列目は既存の `dashboard.col_issue`（中身は `"issue"`。
[internal/i18n/messages/ja.json:264](../../../internal/i18n/messages/ja.json#L264)）を使い回すので、**1列目は「issue」である。**

| issue | なぜ止まっているか | いつから | 直し方 |
| --- | --- | --- | --- |
| `octocat/hello-world#42`（題名） | 担当者が付いています（`octocat`） | 40分前 | GitHub の画面でその担当者を外してください |

**空のときは1行だけ出す。**「着手できずに止まっているものはありません」。

**`Noticed` は表に列を作らない。**JSON にだけ出す。
**列を1つ増やすより、行に印を1つ添えるほうが読める**
（既存の `badge_waiting_quota` と同じ形。`{{- if .NoticeBadge }}<span class="badge">{{ .NoticeBadge }}</span>{{ end }}`）。

---

## 12. 足す文言（19件）

**言いたいこと。**`internal/server` は文言を全部資源から引く（設計 3-35）。
**理由と直し方も資源に置く。**`internal/orchestrator` 側の WARN と issue のコメントは、いまどおり日本語を直に書く。
**19件のうち18件はダッシュボード、1件は設定の検査である**（`on_assignee_gate`。14 節）。

| キー | 何に出るか |
| --- | --- |
| `dashboard.caption_gated` | 表の見出し「着手できずに止まっているもの」 |
| `dashboard.col_gated_reason` / `dashboard.col_gated_since` / `dashboard.col_gated_remedy` | 列の見出し（issue の列は既存の `dashboard.col_issue` を使い回す） |
| `dashboard.no_gated` | 1件も無いときの1行 |
| `dashboard.note_gated` | 表の下の注記（メモリだけに持つので再起動で消える、と書く） |
| `dashboard.gate_reason_human_assigned` / `dashboard.gate_reason_many_assignees` / `dashboard.gate_reason_many_assignees_with_self` | 理由の1行（`%s` に担当者） |
| `dashboard.gate_remedy_human_assigned` / `dashboard.gate_remedy_many_assignees` / `dashboard.gate_remedy_many_assignees_with_self` | 直し方の1行 |
| `dashboard.badge_not_noticed` | 案内をまだ書いていない行に添える印 |
| `dashboard.badge_notice_off` | `warn_only` で切ってある行に添える印 |
| `dashboard.badge_notice_capped` | コメントが上限で切れていて確かめられなかった行に添える印 |
| `dashboard.badge_notice_unclear_owner` | gh の持ち主が担当者に混じっていて切り分けられなかった行に添える印（8-3） |
| `dashboard.badge_notice_no_body` | その理由に issue へ書く本文が用意されていない行に添える印 |
| `dashboard.badge_notice_failed` | 案内の投稿に失敗した行に添える印 |
| `config.validate.handoff_on_assignee_gate` | `on_assignee_gate` に知らない値が入っていたときのエラー |

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
  "dashboard.gate_reason_many_assignees_with_self": "担当者が2人以上いて、continuo が使うアカウントも混じっています（%s）",
  "dashboard.gate_remedy_human_assigned": "GitHub の画面でその担当者を外してください",
  "dashboard.gate_remedy_many_assignees": "GitHub の画面で担当者を1人も付いていない状態にしてください",
  "dashboard.gate_remedy_many_assignees_with_self": "別の機械が担当していないかを先に確かめてください。担当していなければ、GitHub の画面で担当者を1人も付いていない状態にしてください",
  "dashboard.badge_not_noticed": "issue へは未通知",
  "dashboard.badge_notice_off": "issue へは書かない設定です",
  "dashboard.badge_notice_capped": "コメントが多すぎて確かめられません",
  "dashboard.badge_notice_unclear_owner": "別の機械の担当かどうかを切り分けられません",
  "dashboard.badge_notice_no_body": "この理由に書く本文が用意されていません",
  "dashboard.badge_notice_failed": "issue へ書けませんでした"
```

**`en.json` の中身。**[docs/spec/translation-glossary.md](../../spec/translation-glossary.md) に従う
（[CONTRIBUTING.md:97-99](../../../CONTRIBUTING.md#L97-L99) が「英語を書くときはこれに従ってください」と決めている）。
**訳語は実装者が勝手に決めない。**

```json
  "dashboard.caption_gated": "What cannot be started",
  "dashboard.col_gated_reason": "why it cannot be started",
  "dashboard.col_gated_since": "since",
  "dashboard.col_gated_remedy": "how to make it start",
  "dashboard.no_gated": "Nothing is waiting to be started.",
  "dashboard.note_gated": "This table is kept in memory only. It disappears when continuo restarts, and is rebuilt on the next poll.",
  "dashboard.gate_reason_human_assigned": "An assignee is set (%s)",
  "dashboard.gate_reason_many_assignees": "Two or more assignees are set (%s)",
  "dashboard.gate_reason_many_assignees_with_self": "Two or more assignees are set, and the account continuo uses is one of them (%s)",
  "dashboard.gate_remedy_human_assigned": "Remove that assignee on GitHub.",
  "dashboard.gate_remedy_many_assignees": "Remove every assignee on GitHub.",
  "dashboard.gate_remedy_many_assignees_with_self": "Check first whether another machine is working on it. If none is, remove every assignee on GitHub.",
  "dashboard.badge_not_noticed": "not written to the issue",
  "dashboard.badge_notice_off": "writing to the issue is turned off",
  "dashboard.badge_notice_capped": "too many comments to check",
  "dashboard.badge_notice_unclear_owner": "cannot tell whether another machine owns it",
  "dashboard.badge_notice_no_body": "no notice text is defined for this reason",
  "dashboard.badge_notice_failed": "could not be written to the issue"
```

**この訳が従った決めごと。**

| 決めごと | どこ | ダッシュボードの18件でどう効いたか |
| --- | --- | --- |
| **画面に出る散文は大文字で始め、日本語に `。` があれば `.` を付ける** | 訳語集の「大文字・小文字と句点」 | `caption_gated` と `no_gated` と `note_gated` |
| **対処の1行は `.` を付ける**（日本語に `。` が無くても） | 同上 | `gate_remedy_*` の3件 |
| **`please` は書かない。命令形にする** | 訳語集の「〜してください」 | `gate_remedy_*` の3件 |
| **全角の記号は ASCII に直す** | 同上 | `（%s）` → `(%s)` |
| **assignee** | [internal/i18n/messages/en.json:445](../../../internal/i18n/messages/en.json#L445) が既に `assignee` を使っている | 理由と直し方の6件 |

**訳語集に無い語を使ったので、同じ PR で訳語集へ足す。**「着手できずに止まっているもの」＝
`what cannot be started`、「案内」＝ `notice`、「印（ダッシュボードの badge）」＝ `badge` の3語である
（[CONTRIBUTING.md:99](../../../CONTRIBUTING.md#L99) が「そこに無い語を使ったときは、その語を訳語集へ足してください」と決めている）。

**設定の検査の1件は、ダッシュボードの18件とは別の場所へ足す。**
既存の `config.validate.handoff_*` の隣である
（[internal/i18n/messages/ja.json:223-226](../../../internal/i18n/messages/ja.json#L223-L226)、
[internal/i18n/keys.go:1185-1199](../../../internal/i18n/keys.go#L1185-L1199) の `KeyConfigValidateHandoff*`）。

```json
  "config.validate.handoff_on_assignee_gate": "\"warn_and_comment\" か \"warn_only\" にすること"
```

```json
  "config.validate.handoff_on_assignee_gate": "Use \"warn_and_comment\" or \"warn_only\""
```

**`ja.json` を直したので、`en.json` の先頭の `_source_sha256`
（[internal/i18n/messages/en.json:2](../../../internal/i18n/messages/en.json#L2)）を入れ直す。**

**理由から文言のキーを引くのは、[internal/server/view.go](../../../internal/server/view.go) に置く表である。**

```go
// gateReasonKeys は、止めた理由をダッシュボードの文言のキーへ写す表である（issue #134）。
var gateReasonKeys = map[orchestrator.GateReason]struct{ Reason, Remedy i18n.Key }{ /* … */ }
```

**表に無い理由が来たら、i18n を1度も引かない。**`Reason` の文字列をそのまま `ReasonText` へ入れ、
`Remedy` は空にする。**`i18n.KeyDashboardNone` にも落とさない。**落とすと `—` になり、
**知らない理由が来たことが画面から読めなくなる。**

**`"dashboard.gate_reason_" + string(reason)` のようにキーを組み立てない。**
組み立てると、[internal/i18n/keys.go](../../../internal/i18n/keys.go) の `allKeys` に載らないキーが生まれ、
**日英の突き合わせの検査を素通りする。**表なら、キーは全部その場に書いてある。

---

## 13. 実装の順 — PR は1本にする

**言いたいこと。**3件を1本の PR にまとめる。**分けると同じ関数を3回書き換えることになる。**
段は5つに分け、**段ごとに `go build ./...` と `go test ./test/internal/orchestrator/` が通る形にする。**

| 段 | 何を作るか | どの issue が閉じるか |
| --- | --- | --- |
| **1** | [internal/orchestrator/gate.go](../../../internal/orchestrator/gate.go)（型・`noteGate` / `clearGate` / `markGateNoticed` / `markGateNoticeSkipped` / `forgetGatedNotOnBoard` / `GateViews`）と `Orchestrator` の1行 | まだ閉じない |
| **2** | `handoffGate` の `judged` / `noted` と `defer`、2箇所の `noteGate`、`dispatchCandidates` の6箇所の `clearGate`（6-1）、`Tick` の `forgetGatedNotOnBoard` | まだ閉じない |
| **3** | ダッシュボード（`RunSource` / `NewSnapshot` の引数 / 表 / 並べ替え / 文言18件） | **#134（ダッシュボードに「着手できずに止まっているもの」を出す）** |
| **4** | `gate.go` の `postGateNotice`、`gateNoticedIn`、[internal/orchestrator/prompt.go](../../../internal/orchestrator/prompt.go) の `buildGatedComment`、`FetchAllComments` の `truncated`（7-1）、設定 `on_assignee_gate`（文言1件） | **#140（人間が担当者で着手できないことを、issue のコメントとして1回だけ書く）** |
| **5** | 担当者が2人以上の経路の `viewerIdentity` の切り分け（8-3）と案内、[docs/FAQ.md](../../FAQ.md) / [docs/upgrading.md](../../upgrading.md) | **#136（担当者が2人以上いる issue も、着手できないことを知らせる）** |

**段2では `noteGate` の戻り値を捨てる。**投稿する相手（`postGateNotice`）が段4までできないからである。
**`if o.noteGate(…) { }` と空の分岐を書かない。**`o.noteGate(…)` を文として呼ぶだけにする。

**段5では、設定のコードを1行も書かない。**案内は段4で作った `postGateNotice` を通るので、
**`on_assignee_gate` は担当者が2人以上の経路にも自動で効く。**
**段5で足すのは本文（`buildGatedComment` の2本目）と、`viewerIdentity` の切り分けを含む呼び出しだけである。**

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
**キーは前置き（`dashboard.` / `workspace.` / `config.validate.`）ごとに固まっている場所へ足すので、触る行が近い。**
**どれもファイルの末尾ではなく、中ほどである**
（`dashboard.*` は [internal/i18n/messages/ja.json:252-278](../../../internal/i18n/messages/ja.json#L252-L278)、
`KeyDashboard*` は [internal/i18n/keys.go:1023-1076](../../../internal/i18n/keys.go#L1023-L1076)、
`allKeys` の該当箇所は [internal/i18n/keys.go:2674](../../../internal/i18n/keys.go#L2674) 付近）。
**後から main へ入るほうが衝突を解く。先に入ったほうへ rebase してから段3を書くこと。**

---

## 14. 変えるファイル（20。テストを除く）

**言いたいこと。**新規は1本。**`internal/workspace` は1バイトも触らない。**
**`internal/tracker` は `FetchAllComments` の戻り値に真偽値を1つ足すだけである**（7-1）。

**数えているのはファイルであって表の行ではない。**`ja.json` / `en.json` で1行、
`docs/FAQ.md` / `docs/upgrading.md` で1行にまとめてあるので、**18行で20ファイルである。**
**テストは下の別表にあり、この20には入れていない。**

| ファイル | 何をするか |
| --- | --- |
| [internal/tracker/adapter.go](../../../internal/tracker/adapter.go) | `fetchCommentNodes` / `FetchAllComments` / `FetchComments` の戻り値に `truncated bool` を足す（7-1） |
| [internal/orchestrator/gate.go](../../../internal/orchestrator/gate.go) | **新規。**`GateReason` / `GateNoticeSkip` / `gateNote` / `noteGate` / `clearGate` / `markGateNoticed` / `markGateNoticeSkipped` / `postGateNotice` / `forgetGatedNotOnBoard` / `gateNoticedIn` / `GateView` / `GateViews` |
| [internal/orchestrator/orchestrator.go](../../../internal/orchestrator/orchestrator.go) | 構造体に `gated` を1行、`New` の初期化に1行、`Tick` に `forgetGatedNotOnBoard`、`Tracker` interface の `FetchAllComments` の署名 |
| [internal/orchestrator/handoff.go](../../../internal/orchestrator/handoff.go) | `judged` / `noted` と `defer` での後始末（6-2）、2箇所で `noteGate`、`postGateNotice` の**呼び出し**、`viewerIdentity` の切り分け（8-3）、`FetchAllComments` の戻り値を2箇所で受ける |
| [internal/orchestrator/dispatch.go](../../../internal/orchestrator/dispatch.go) | `handoffGate` より前の6つの `continue` で `clearGate`（6-1） |
| [internal/orchestrator/prompt.go](../../../internal/orchestrator/prompt.go) | `buildGatedComment` |
| [internal/config/types.go](../../../internal/config/types.go) | `OnAssigneeGate` を1行 |
| [internal/config/default.go](../../../internal/config/default.go) | 既定値 `"warn_and_comment"` を1行 |
| [internal/config/validate.go](../../../internal/config/validate.go) | `validateHandoff`（[internal/config/validate.go:664-686](../../../internal/config/validate.go#L664-L686)）に `on_assignee_gate` の switch を1つ足す。`"warn_and_comment"` と `"warn_only"` だけを通す |
| [internal/scaffold/template.go](../../../internal/scaffold/template.go) | WORKFLOW.md の雛形に1行 |
| [internal/server/server.go](../../../internal/server/server.go) | `RunSource` に `GateViews()`。**`snapshot()`（[internal/server/server.go:374](../../../internal/server/server.go#L374)）が `NewSnapshot` へ第2引数を渡す** |
| [internal/server/view.go](../../../internal/server/view.go) | `Snapshot.Gated` と `Gated` 型、理由→文言の表、`NewSnapshot` の引数と詰め替え、**並べ替え（`Since` の古い順、同じなら `Identifier` の昇順。10 節）** |
| [internal/server/template.go](../../../internal/server/template.go) | 表を1つ増やす |
| [internal/i18n/keys.go](../../../internal/i18n/keys.go) | キー19件（ダッシュボード18件と `KeyConfigValidateHandoffOnAssigneeGate`）と `allKeys` |
| [internal/i18n/messages/ja.json](../../../internal/i18n/messages/ja.json) / [en.json](../../../internal/i18n/messages/en.json) | 文言19件。**`_source_sha256` を入れ直す** |
| [docs/spec/translation-glossary.md](../../spec/translation-glossary.md) | 新しく使った3語を足す（12 節） |
| [docs/FAQ.md](../../FAQ.md) / [docs/upgrading.md](../../upgrading.md) | 「担当者が付いた issue が着手されない」に、ダッシュボードと案内のことを足す |
| [docs/plans/continuo_design.md](../continuo_design.md) | 3-68 に「担当者の経路はこの文書が正」を1行足し、「3回続けて」を「3回以上」に直す（置き換えない）。**5-2 の yaml ブロック（設定の見本）の `handoff:` の下に `on_assignee_gate` を1行** |

**`trust.on_untrusted` の隣（[internal/config/validate.go:345-349](../../../internal/config/validate.go#L345-L349)）へは書かない。**
`on_assignee_gate` が入るのは `tracker.provider.handoff` であり、
**その検査は `validateHandoff` が全部持っている。**別の場所へ書くと handoff の設定の検査が2箇所に散る。
**エラーの文面も `validateHandoff` に揃える。**あそこの5件はすべて i18n のキー
（`KeyConfigValidateHandoff*`）を引いており、`trust.on_untrusted` のように日本語を直に書いていない。
**そのために文言を1件足す**（12 節）。
**既定値は `DefaultConfig` が `"warn_and_comment"` を入れる**ので、空文字が検査に掛かることは無い。

**`NewSnapshot` の署名を変える。**いまは2引数で、手元に4箇所ある。

```go
func NewSnapshot(views []orchestrator.RunView, gates []orchestrator.GateView, now time.Time) Snapshot
```

| 呼んでいる場所 | どうするか |
| --- | --- |
| [internal/server/server.go:374](../../../internal/server/server.go#L374) の `snapshot()` | `s.source.GateViews()` を渡す |
| [test/internal/server/view_test.go:18](../../../test/internal/server/view_test.go#L18) / :73 / :86 | 3箇所とも `nil` か、確かめたい `GateView` を渡す |

**署名を変えずに `snapshot()` が後から `snap.Gated` を詰める形にはしない。**
`Gated` の並べ替え（`Since` の古い順、同じなら `Identifier` の昇順。10 節）と文言の詰め替えが `NewSnapshot` の外へ出て、
**テストから `Gated` を作れなくなる。**

**5-2 と雛形は同じ PR で直す。**
[test/internal/scaffold/design_template_test.go:35](../../../test/internal/scaffold/design_template_test.go#L35) の
`TestTemplate_雛形のキー構成が設計5_2の設定例と一致する` が、
**雛形の front matter のキーの集合と 5-2 の yaml ブロック（設定の見本）のキーの集合が完全に一致すること**を強制している。
**片方だけに足すと必ず落ちる。**

**テスト。**

| ファイル | 何を確かめるか |
| --- | --- |
| [test/internal/orchestrator/](../../../test/internal/orchestrator) | 空きスロットが尽きた巡回で記録が減らない／枠で止まった巡回で減らない／候補から消えたら消える／**ラベル不足で飛ばした巡回で消える**（6-1）／**入札に負けた巡回で消える**（6-2）／案内は3回目かつ60秒後に1回だけ／**3回目で60秒に届かなければ4回目に書く**／`warn_only` では書かず `NoticedAt` も立たない／**担当者に gh の持ち主が混じっていたら記録は作るが案内は作らない**（8-3）／**理由が変わったら Count と FirstSeenAt は数え直し、理由ごとの案内の状態は残る**（6-5）／**理由が往復しても、同じ理由の案内は1回しか書かれない**（6-5）／**`truncated` が真なら書かず `too_many_comments` が立つ**／`GateViews` が返したスライスへ書いても `o.gated` が変わらない。**`fakeTracker.FetchAllComments` の戻り値を3つにする** |
| [test/internal/tracker/](../../../test/internal/tracker) | `FetchAllComments` が `truncated` を返す（20ページを使い切って続きがあるとき真、`hasNextPage` が偽なら偽）。**既存の2件の呼び出しに戻り値を足す** |
| [test/internal/server/](../../../test/internal/server) | `fakeSource` に `GateViews` を足す。**`view_test.go` の `NewSnapshot` の呼び出し3箇所に引数を足す。**表の行と、1件も無いときの1行、**`Since` が同じ2件が `Identifier` の昇順に並ぶ** |
| [test/internal/i18n/](../../../test/internal/i18n) | 既存の突き合わせが19件を拾う（新しいテストは要らない） |
| [test/internal/scaffold/](../../../test/internal/scaffold) | 既存の `TestTemplate_雛形のキー構成が設計5_2の設定例と一致する` が通る（新しいテストは要らない） |

**[test/internal/orchestrator/expected_warnings_test.go](../../../test/internal/orchestrator/expected_warnings_test.go) に足す。**
担当者を2人付けたテストと、人間の担当を付けたテストは **WARN を出す**ので、
そのテスト名を登録しないと落ちる（[test/internal/orchestrator/expected_warnings_test.go:7](../../../test/internal/orchestrator/expected_warnings_test.go#L7)）。

---

## 15. やらないこと

**言いたいこと。****関門は24分岐あるが、この設計が出すのは2つだけである。**
v1 は全部を出そうとして落ちた。**足すのは呼び出し1行なので、あとから足せる。**

| 何 | なぜやらないか |
| --- | --- |
| **worktree の経路を出す**（置き場所・登録の欠落・branch の食い違い） | **3-66（番兵エラーの新設）が先に要る。**いまは branch の食い違いも登録の欠落も同じ番兵で包まれており、2つを区別できない |
| **未信頼のリポジトリを出す** | 既に WARN を出し、issue にもコメントを書いている（`noteUntrusted`）。**同じことを2つの仕組みで持たない** |
| **枠・空きスロット・必須ラベルを出す** | **止まっているのではなく、順番を待っている。**次の巡回で勝手に進む。出すと画面が常時埋まり、本当に止まっているものが埋もれる。**ログには既に出ている**（PR #141（issue が1件も着手されない3つの理由を、ログから探せるようにする）） |
| **入札に負けた・他の機械が担当中を出す** | **正常に譲っている。**止まっていない |
| **記録を永続化する** | 再起動すると消えるが、次の巡回で作り直される。**ファイルを1つ増やす価値がない。**そのことを表の下の注記に書く |
| **記録を判定に使う** | **表示と案内のためだけに持つ。**判定に使うと、表示の欠陥が着手を止める |
| **案内を書き直す** | コメントを書き換える手段が無い（8-1） |
| **担当者が2人以上の経路でコメントを読む** | 読み取りの枠（1巡回10件）を使い、#136（担当者が2人以上いる issue も、着手できないことを知らせる）が否定した道である |
| **読み取りの枠の飢餓を直す** | **この設計では枠の消費が1件も増減しない。**人間の担当が付いた issue がボードの上位を占めると11件目以降へ届かない問題は、いまも同じように起きている。**別の issue として立てる** |

---

## 16. 確かめたこと

**言いたいこと。**設計の前提にした事実を、コマンドと出力で残す。
**対象の commit は 73fb41ae654add121c72ed01464ef77f69684812 である。**

| 主張 | 根拠 |
| --- | --- |
| **`FetchAllComments` は2000件までは落とさない** | [internal/tracker/adapter.go:1155](../../../internal/tracker/adapter.go#L1155) が `fetchCommentNodes(ctx, issueNodeID, maxCommentsPerFetch, 0)` を呼ぶ。`keep` が0なら `keep` では打ち切らない（[internal/tracker/adapter.go:1250](../../../internal/tracker/adapter.go#L1250) の `if keep > 0 && unmarked >= keep`）。**ページ数では打ち切る**（[internal/tracker/adapter.go:1232](../../../internal/tracker/adapter.go#L1232) の `for page := 0; page < maxCommentPages; page++`。`maxCommentPages` は20、`maxCommentsPerFetch` は100） |
| **上限で落ちるのは古い側である** | [internal/tracker/query.go:253](../../../internal/tracker/query.go#L253) が `orderBy: { field: UPDATED_AT, direction: DESC }` で取り、[internal/tracker/adapter.go:1267-1270](../../../internal/tracker/adapter.go#L1267-L1270) が最後に反転して古い順へ戻す。**打ち切りは新しい側を読み終えた時点で起きる** |
| **上限に達したことはログに出るが、戻り値からは分からない** | [internal/tracker/adapter.go:1261-1264](../../../internal/tracker/adapter.go#L1261-L1264) が `Warn("コメントが多すぎるので途中まででやめました（古いコメントは読めていません）", …)` を出すだけで、`FetchAllComments` の戻り値は `([]Comment, error)` のままである（[internal/tracker/adapter.go:1150-1158](../../../internal/tracker/adapter.go#L1150-L1158)）。**だから戻り値に真偽値を1つ足す**（7-1） |
| **件数では切れを当てられない** | 打ち切りは [internal/tracker/adapter.go:1232](../../../internal/tracker/adapter.go#L1232) の `for page := 0; page < maxCommentPages; page++` を、続きの cursor を持ったまま抜けたかどうかで決まる。**`len(nodes)` は1ページの件数が100に満たなくても増えないので、2000未満のまま切れることがある** |
| **`FetchAllComments` の呼び出し元は2つだけである** | `grep -rn "FetchAllComments" --include="*.go" .`（`.claude/worktrees/` を除く）で、実装以外は [internal/orchestrator/handoff.go:111](../../../internal/orchestrator/handoff.go#L111) と [internal/orchestrator/handoff.go:716](../../../internal/orchestrator/handoff.go#L716)、interface が [internal/orchestrator/orchestrator.go:129](../../../internal/orchestrator/orchestrator.go#L129)、fake が [test/internal/orchestrator/helpers_test.go:1328](../../../test/internal/orchestrator/helpers_test.go#L1328) |
| **担当者が2人以上の分岐は `viewerIdentity` より前にある** | [internal/orchestrator/handoff.go:81](../../../internal/orchestrator/handoff.go#L81) の `if len(logins) >= 2` に対し、[internal/orchestrator/handoff.go:98](../../../internal/orchestrator/handoff.go#L98) が `viewer, ok := o.viewerIdentity(ctx)` である。**だから 8-3 はこの分岐の中で自分で引く** |
| **担当者が0人になっても、走っている run は止まらない** | [internal/orchestrator/handoff.go:699-711](../../../internal/orchestrator/handoff.go#L699-L711) が `if len(logins) == 0 { … return false, "" }` で「担当者が1人もいないだけでは止めない」と決めている |
| **`handoffGate` へ届かない `continue` が5つある** | [internal/orchestrator/dispatch.go:273](../../../internal/orchestrator/dispatch.go#L273) の `decision := o.handoffGate(ctx, issue)` より前に、[:191](../../../internal/orchestrator/dispatch.go#L191)（`lookupRunByID`）・[:205](../../../internal/orchestrator/dispatch.go#L205)（`skipByFailure`）・[:208](../../../internal/orchestrator/dispatch.go#L208)（`!issue.Dispatchable`）・[:219](../../../internal/orchestrator/dispatch.go#L219)（`missingRequiredLabels`）・[:266](../../../internal/orchestrator/dispatch.go#L266)（`preflight`）がある |
| **handoff の設定の検査は `validateHandoff` が持っている** | [internal/config/validate.go:664-686](../../../internal/config/validate.go#L664-L686) に5件あり、すべて `i18n.T(i18n.KeyConfigValidateHandoff*)` を引く。`trust.on_untrusted` の検査は [internal/config/validate.go:345-349](../../../internal/config/validate.go#L345-L349) にあり、**日本語を直に書いている**（形が違う） |
| **`sort.Slice` は安定ではない** | [internal/server/view.go:142](../../../internal/server/view.go#L142) の `sort.Slice(runs, func(i, j int) bool { return runs[i].Identifier < runs[j].Identifier })` は鍵が一意なので成立している。**`Since` は一意ではない** |
| **`polling.interval_ms` の既定は30000ミリ秒** | [internal/config/default.go:87](../../../internal/config/default.go#L87) の `IntervalMs: 30000`。**3回目の巡回はちょうど60秒後になり、`noticeMinAge` と同値である** |
| **`dashboard.*` のキーはファイルの末尾に無い** | [internal/i18n/messages/ja.json:252-278](../../../internal/i18n/messages/ja.json#L252-L278)（ファイルは843行）、[internal/i18n/keys.go:1023-1076](../../../internal/i18n/keys.go#L1023-L1076) の `KeyDashboard*`、`allKeys` の該当は [internal/i18n/keys.go:2674](../../../internal/i18n/keys.go#L2674) 付近 |
| **`containsFold` は既にある** | [internal/orchestrator/lifecycle.go:929](../../../internal/orchestrator/lifecycle.go#L929) の `func containsFold(states []string, target string) bool` |
| **`RunView` はスライスを1つも持たない** | [internal/orchestrator/orchestrator.go:1158-1192](../../../internal/orchestrator/orchestrator.go#L1158-L1192) のフィールドは `string` / `int` / `bool` / `time.Time` / `TokenUsage` だけである。**だから [internal/orchestrator/orchestrator.go:1197-1220](../../../internal/orchestrator/orchestrator.go#L1197-L1220) の代入だけで写しが成立している** |
| **draft issue は関門へ来ない** | [internal/orchestrator/handoff.go:70-73](../../../internal/orchestrator/handoff.go#L70-L73) が `nodeID == ""` のとき `return handoffDecision{proceed: true}` で抜ける |
| **`NewSnapshot` は手元で4箇所から呼ばれている** | `grep -rn "NewSnapshot(" --include="*.go" .`（リポジトリの直下で） の出力から `.claude/worktrees/` を除くと、[internal/server/view.go:108](../../../internal/server/view.go#L108) の定義のほか [internal/server/server.go:374](../../../internal/server/server.go#L374) と [test/internal/server/view_test.go:18](../../../test/internal/server/view_test.go#L18) / :73 / :86 の4件 |
| **コメントを書き換える経路も消す経路も無い** | 検索パターン `updateIssueComment` `deleteIssueComment` `minimizeComment` `UpdateComment` `DeleteComment` の5本を `grep -rniE` で束ね、対象 `internal/` と `cmd/`、commit 73fb41ae で `wc -l` が `0` |
| **`FetchIssuesByStates` は途中で切れない** | [internal/tracker/adapter.go:609-617](../../../internal/tracker/adapter.go#L609-L617) が上限超過で `CategoryPagination` の `*Error` を返す |
| **`PostComment` が `self_marker` を先頭に付ける** | [internal/tracker/adapter.go:1110-1113](../../../internal/tracker/adapter.go#L1110-L1113) の `full = selfMarker + "\n" + body` |
| **担当者が2人以上の経路にコメントは無い** | [internal/orchestrator/handoff.go:81-86](../../../internal/orchestrator/handoff.go#L81-L86) の `return` は、`FetchAllComments`（[internal/orchestrator/handoff.go:111](../../../internal/orchestrator/handoff.go#L111)）より25行前にある |
| **`o.failures` は着手できた run しか持たない** | `noteFailure` の呼び出しは [internal/orchestrator/lifecycle.go:551](../../../internal/orchestrator/lifecycle.go#L551) と [internal/orchestrator/lifecycle.go:625](../../../internal/orchestrator/lifecycle.go#L625) の2箇所だけで、どちらも `rs *runState` を持つ |
| **`_source_sha256` の入れ直しは規則である** | [CONTRIBUTING.md:100](../../../CONTRIBUTING.md#L100) が「`ja.json` の文言を直したときは、`en.json` の先頭の `_source_sha256` を入れ直してください」と決めている |
