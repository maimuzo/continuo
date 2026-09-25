// Package setup は `continuo setup` の実体である。**既にあるカンバンの Status の選択肢を、
// continuo の5つの役割へ割り当てる対話**を行う（RUCM「既存のボードの Status を割り当てる」）。
//
// **対話するコマンドは continuo setup の1つだけである。**`continuo init` と `continuo trust` は
// 標準入力を1度も握らない。対話をここへ切り出してあるので、`init` は自動化から叩ける。
//
// **カンバンは1文字も書き換えない。**このパッケージが gh へ渡すのは
// `gh project field-list`（読み取り）だけである。**選択肢を足す API は呼ばない**
// （`updateProjectV2Field` は選択肢の指定を全件の置き換えとして扱うので、設定済みの
// Status が全部消える）。選択肢が足りないときは、GitHub の画面から足すよう案内して打ち切る。
//
// **WORKFLOW.md は書かない。**割り当てが決まったら Assignment.Statuses() を
// `scaffold.UpdateStatuses` へ渡すのは呼び出し側（cmd/continuo）である。
// 書き込みの実体を2箇所に持たないためである。
package setup

import "github.com/maimuzo/continuo/internal/i18n"

// Role は continuo がカンバンの Status に与える役割である。
//
// **値は尋ねる順序でもある**（issue が実際に通る順）。0 から始まる連番にしてあるので、
// Assignment の添字にそのまま使える。
type Role int

const (
	// RoleDispatch は着手待ちである。continuo はここから issue を取る。
	RoleDispatch Role = iota
	// RoleRunning は作業中である。continuo は issue を取ったときにここへ動かす。
	RoleRunning
	// RoleReview はレビュー待ちである。エージェントが終わったと表明したらここへ動かす。
	RoleReview
	// RoleBlocked は保留である。エージェントが判断を仰ぐとき・打ち切ったときにここへ動かす。
	RoleBlocked
	// RoleDone は完了である。人間がここへ動かすと continuo が worktree と branch を片付ける。
	RoleDone
	// RoleDirectChat は direct chat である。人間が pane で直接エージェントと話すあいだ、
	// continuo は指示を送らず、pane も worktree も閉じない（設計 3-82）。
	//
	// **この役割だけは飛ばせる**（`OptionalRoles`）。カンバンに選択肢が無くても
	// continuo は起動するので、割り当てを強いる理由が無い。
	RoleDirectChat
)

// RoleCount は尋ねる役割の数である。
const RoleCount = 6

// RequiredRoleCount は「選択肢がこれだけ無いと対話を始めない」数である。
//
// **RoleCount より1つ少ない。**`RoleDirectChat` は飛ばせるので、その選択肢が無くても
// 残り5つは割り当てきれる。**ここを RoleCount にすると、選択肢がちょうど5つの
// カンバンで `continuo setup` が1問も尋ねずに終わる**（設計 3-82）。
const RequiredRoleCount = 5

// IsOptional は、その役割を飛ばせるかどうかを返す。
//
// **飛ばせるのは `RoleDirectChat` だけである。**残り5つは、割り当てないと continuo が
// 動かない（着手待ちが無ければ issue を取れず、完了が無ければ片付けられない）。
//
// r: 役割。
// 戻り値: 飛ばせるなら true。
func (r Role) IsOptional() bool { return r == RoleDirectChat }

// roleOrder は尋ねる順序である。**issue が実際に通る順に並べてある。**
// カンバン上の並びと同じ順に尋ねると、利用者は一覧を上から順に消化できる。
var roleOrder = [RoleCount]Role{RoleDispatch, RoleRunning, RoleReview, RoleBlocked, RoleDone, RoleDirectChat}

// roleConfigKeys は、その役割が WORKFLOW.md のどのキーに書かれるかである。添字は Role の値。
//
// **画面には役割の呼び名ではなくこれを出す。**「着手待ち」と言われても、利用者は
// WORKFLOW.md のどの行が変わるのかを知らない。**キー名なら、答えたあとに自分で確かめられる。**
// **翻訳しない。**設定ファイルに書かれる文字列そのものなので、言語で変わってはならない。
var roleConfigKeys = [RoleCount]string{
	RoleDispatch: "dispatch_state",
	RoleRunning:  "running_state",
	RoleReview:   "status_signal_map.review",
	// **保留だけは2つのキーに同じ値が入る。**両方書かないと、答えたあとに
	// WORKFLOW.md を見た利用者が「もう1つはどこから来たのか」を追えない。
	RoleBlocked:    "status_signal_map.blocked / failure_state",
	RoleDone:       "terminal_states",
	RoleDirectChat: "direct_chat_state",
}

// roleDescKeys は役割の説明の文言のキーである。添字は Role の値。
//
// **説明は Status の名前ではなく continuo の振る舞いで書く。**初見の利用者は
// 「どの Status がどの役割か」を知らないので、Status 名を先に見せると、名前の似た
// 選択肢を役割の意味と無関係に選ぶ。
var roleDescKeys = [RoleCount]i18n.Key{
	RoleDispatch:   i18n.KeySetupRoleDispatchDesc,
	RoleRunning:    i18n.KeySetupRoleRunningDesc,
	RoleReview:     i18n.KeySetupRoleReviewDesc,
	RoleBlocked:    i18n.KeySetupRoleBlockedDesc,
	RoleDone:       i18n.KeySetupRoleDoneDesc,
	RoleDirectChat: i18n.KeySetupRoleDirectChatDesc,
}

// Roles は割り当てる役割を、尋ねる順に返す。
//
// 戻り値: dispatch_state・running_state・status_signal_map.review・
// status_signal_map.blocked / failure_state・terminal_states・direct_chat_state の
// 順に並んだ役割（呼び出し側が書き換えても内部には影響しない）。
func Roles() []Role {
	out := make([]Role, RoleCount)
	copy(out, roleOrder[:])
	return out
}

// ConfigKey は、その役割が WORKFLOW.md のどのキーに書かれるかを返す。
//
// **翻訳しない。**設定ファイルに書かれる文字列そのものである。
//
// 戻り値: `dispatch_state` などのキー名。範囲外の値なら空文字。
func (r Role) ConfigKey() string {
	if r < 0 || int(r) >= RoleCount {
		return ""
	}
	return roleConfigKeys[r]
}

// Description は「continuo がその Status で何をするか」を、いま選ばれている言語で返す。
//
// 戻り値: 「continuo はここから issue を取ります」などの1文。範囲外の値なら空文字。
func (r Role) Description() string {
	if r < 0 || int(r) >= RoleCount {
		return ""
	}
	return i18n.T(roleDescKeys[r])
}
