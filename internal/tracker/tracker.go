// Package tracker は GitHub Projects v2 のボードをトラッカーとして読み書きするアダプタである
// （docs/plans/continuo_design.md 3-13、SPEC.md 第11節）。
//
// 実測で確定した性質は次の3点である（設計 2-2）。
//   - project #3 は104件のうち100件に Status が入っており、4件は未設定である
//   - Status の選択肢名を間違えると、GraphQL はエラーを出さずに0件を返す。
//     **これが最大の落とし穴なので、起動時に選択肢名を照合する**（Bootstrap）
//   - `gh project` サブコマンドは1回 102 point かかるため使わない。
//     すべて GraphQL を net/http で直接叩く
//
// このパッケージは本番のボード（project #3）へ絶対に書き込まない設計にはなっていない
// （UpdateStatus / PostComment は書き込みそのものである）。**書き込みを検証するテストは
// 必ず httptest.Server で立てたテスト用GraphQL mockに対して行い、本番のボードへは
// 接続しないこと。**
package tracker

import "time"

// RepoTrustFunc は、あるリポジトリが Claude Code に信頼登録されているかを判定する関数である
// （設計 3-6 / 3-13 / 4-3）。
//
// **信頼していないフォルダでは Claude Code の hook が1つも動かず、turn 終了の検知が
// 全滅する。**そのため「信頼済みか」は dispatch してよいかの条件の1つであり、
// Issue.Dispatchable に集約する（設計 3-13）。
//
// 判定の実体は `~/.claude.json` の projects を**読み取る**もので、書き換えは行わない
// （設計 4-3 で決着済み）。tracker パッケージはその読み取り方法を知らないので、
// 関数として外から受け取る。
//
// owner: リポジトリの所有者名（例 "octocat"）。
// repo: リポジトリ名（例 "hello-world"）。
// 戻り値: 信頼登録されていれば true。
type RepoTrustFunc func(owner, repo string) bool

// BlockerRef は issue をブロックしている別の issue のベストエフォートな参照である
// （SPEC.md 4.1.1 の blocked_by）。**アダプタが独自のブロック関係の意味づけを作ってはならない**
// （SPEC.md 11.3: "adapters MUST NOT invent blocker semantics they cannot represent reliably")。
// GitHub 側にそのまま存在する参照をなぞるだけである。
type BlockerRef struct {
	// ID はブロックしている issue の GitHub 上のノード ID である。取得できなければ空文字。
	ID string
	// Identifier はブロックしている issue の人間可読な名前（<owner>/<repo>#<番号>）である。
	// 取得できなければ空文字。
	Identifier string
	// State はブロックしている issue の GitHub 上の状態（OPEN / CLOSED）である。
	// 取得できなければ空文字。
	State string
}

// Comment は issue に付いた1件のコメントを正規化した形である。
//
// エージェントが `gh issue comment` で書いたコメントは author が人間のアカウントになり、
// 人間が手で書いたものと区別できない（設計 2-2）。そのため本文の先頭に固定の印を
// 書かせて判別する。IsAgent / IsSelf はその判別結果である。
type Comment struct {
	// ID はコメントの GitHub 上のノード ID である。
	ID string
	// URL はコメントの URL である。
	URL string
	// Author はコメントを書いた GitHub アカウントのログイン名である。
	// 取得できない場合（削除済みアカウント等）は空文字。
	Author string
	// Body はコメント本文である。マーカーを剥がさず、そのまま保持する。
	Body string
	// CreatedAt はコメントが作成された時刻である。
	CreatedAt time.Time
	// IsAgent は、本文の先頭が `tracker.provider.comments.marker` の印で始まっている
	// （＝エージェントが書いたと判別できる）ことを示す。
	IsAgent bool
	// IsSelf は、本文の先頭が `tracker.provider.comments.self_marker` の印で始まっている
	// （＝continuo 自身が代筆した）ことを示す。IsSelf なコメントは
	// FetchComments が次の turn の入力から自動的に除外する。
	IsSelf bool
}

// Issue は SPEC.md 4.1.1 が定める15項目すべてを持つ、正規化済みの issue である。
// フィールド名・意味は SPEC.md 4.1.1 に対応する（コメントに原文と訳を添えてある）。
//
// **Priority は仕様上の項目として残すが、continuo は値を読まない**（設計 4-2）。
// このアダプタが返す Issue の Priority は常に nil である。
//
// このほかに、continuo 独自に4項目を足す（設計 3-13「アダプタが足す項目」）。
// Owner / Repo / Number / CommentCount がそれで、いずれもプロンプトのテンプレートや
// branch 名の組み立てに使う軽量な値である。**コメント本文そのもの（[]Comment）は
// ここには載せない。**取得コストが高いので、作業開始時に FetchComments で
// 別途まとめて取る（設計「その7」）。
type Issue struct {
	// ID は dispatch に使う安定した識別子である（SPEC.md 4.1.1 の id）。
	// REQUIRED。**project item の ID を使う**（設計 3-13）。orchestrator にとっては
	// 不透明な値であり、GitHub 本来のチケット ID ではないことがある
	// （SPEC.md: "It MAY be a project-item or board-entry ID instead of the provider's
	// underlying ticket ID." 訳: プロバイダ本来のチケット ID ではなく、project item や
	// board entry の ID であってもよい）。
	ID string
	// NativeRef は provider 固有の値の入れ物である（SPEC.md 4.1.1 の native_ref）。
	// OPTIONAL。orchestrator は中身を解釈しない（設計 3-13）。JSON-safe な値だけを持つ。
	// 少なくとも次のキーを持つ。
	//   - "issue_node_id": 下敷きの GitHub issue / draft issue のノード ID
	//     （addComment 等 provider-native tool の subjectId に使う）
	//   - "content_type": "ISSUE" か "DRAFT_ISSUE"
	//   - "github_issue_state": 下敷きの GitHub issue の OPEN / CLOSED（draft issue には無い）
	NativeRef map[string]any
	// Identifier は人間可読な一意の名前である（SPEC.md 4.1.1 の identifier）。REQUIRED。
	// **`<owner>/<repo>#<番号>` の形にする**（設計 3-13）。1枚のボードに複数リポジトリが
	// 載るため `#188` だけでは一意にならない。draft issue はリポジトリを持たないため
	// `draft:<project item の ID>` にする。
	Identifier string
	// Title はタイトルである（SPEC.md 4.1.1 の title）。
	Title string
	// Description は本文である（SPEC.md 4.1.1 の description）。OPTIONAL。
	// 本文が空文字の場合は nil にする（「説明が無い」ことを表す）。
	Description *string
	// Priority は常に nil である（設計 4-2。「Priority を読まない」）。
	Priority *int
	// State は現在のトラッカー上の状態名である（SPEC.md 4.1.1 の state）。REQUIRED。
	// **project の Status フィールドの値**であり、GitHub issue の OPEN/CLOSED ではない
	// （その値は NativeRef["github_issue_state"] に入る）。GitHub の綴りをそのまま保つ
	// （比較のときだけ大文字小文字を無視する。SPEC.md 11.3）。
	State string
	// StatusChangedByAutomation は、いまの State を書いたのがボードの組み込みの自動化
	// （`Pull request linked to issue` など）かどうかである（設計 2-6 / 3-54）。
	//
	// **ID 指定の取り直し（FetchIssuesByIDs）でだけ埋まる。**候補の取得
	// （FetchIssuesByStates）と識別子での照合（FetchIssueByIdentifier）では常に false である。
	// そちらは100件単位でボードを読むので、1件ずつにしか意味の無い timeline を要求しない。
	//
	// **判定は「`actor.__typename` が `Bot`、または `wasAutomated` が真」である。**
	// `wasAutomated` は組み込みの自動化でも `false` を返すため、単独では使えない（設計 2-6）。
	// **continuo 自身の書き込みは `User` になる**ので、自分の書き込みは自動化と判定されない。
	//
	// **イベントを1件も引けなかった場合は false である。**分からないなら
	// 「自動化ではない」に倒す（人間が動かしたときの扱いを既定にする）。
	StatusChangedByAutomation bool
	// StatusChangedBy は、いまの State を書いた主体のログイン名である
	// （組み込みの自動化なら `github-project-automation`）。
	// **ログと issue のコメントに出すためだけに持つ。**判定には使わない。取れなければ空文字。
	StatusChangedBy string
	// BranchName はトラッカーが返す branch のメタデータである（SPEC.md 4.1.1 の branch_name）。
	// OPTIONAL。GitHub の "Development" 機能でリンクされた branch があれば、その1本目の名前。
	// 無ければ nil。
	BranchName *string
	// URL は issue の URL である（SPEC.md 4.1.1 の url）。draft issue は URL を持たないため nil。
	URL *string
	// AssigneeID は担当者の ID である（SPEC.md 4.1.1 の assignee_id）。
	// GitHub ユーザーのノード ID。担当者がいなければ nil。
	AssigneeID *string
	// Labels はラベル名の一覧である（SPEC.md 4.1.1 の labels）。
	// 前後の空白を落として小文字にし、空のラベルは捨て、重複は取り除いてある（3-13）。
	Labels []string
	// BlockedBy はこの issue をブロックしている issue のベストエフォートな一覧である
	// （SPEC.md 4.1.1 の blocked_by）。draft issue には無いので空。
	BlockedBy []BlockerRef
	// Dispatchable は dispatch してよいかの判定を集約した値である
	// （SPEC.md 4.1.1 の dispatchable）。REQUIRED。設計 3-13 が定めるとおり、
	// **draft issue でない・Status が設定済み・リポジトリが信頼済み、の3つをすべてここで
	// 判定する**（受け皿をここに置かないと、条件が増えるたびに GitHub 固有の分岐が
	// orchestrator へ積み上がる）。
	//   - draft issue は false（リポジトリを持たず作業ディレクトリを決められない）
	//   - Status 未設定の item はそもそも Issue にならない（一覧では省き、ID 指定では失敗する）
	//   - 信頼登録されていないリポジトリの issue は false（NewAdapter に渡す RepoTrustFunc が
	//     判定する。渡さなければ全て信頼済みとして扱う）
	//
	// orchestrator は required_labels・claim・retry・concurrency の判定を別途重ねて適用する
	// （アダプタはここまでしか判定しない）。
	Dispatchable bool
	// CreatedAt は作成時刻である（SPEC.md 4.1.1 の created_at）。取得できなければ nil。
	CreatedAt *time.Time
	// UpdatedAt は更新時刻である（SPEC.md 4.1.1 の updated_at）。取得できなければ nil。
	UpdatedAt *time.Time

	// ===== ここから下は SPEC.md 4.1.1 に無い、continuo 独自の追加項目（設計 3-13）=====

	// Owner はリポジトリの所有者（organization / user）名である。draft issue では空文字。
	Owner string
	// Repo はリポジトリ名である。draft issue では空文字。
	Repo string
	// Number は GitHub issue 番号である。draft issue では 0。
	Number int
	// CommentCount は現在付いているコメントの件数である。コメント本文そのものは
	// 別途 FetchComments で取る（このフィールドは軽量なので候補の取得と同時に取れる）。
	CommentCount int
}
