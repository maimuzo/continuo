// Package setup は `continuo setup` の実体である。**既にあるボードの Status の選択肢を、
// continuo の5つの役割へ割り当てる対話**を行う（RUCM「既存のボードの Status を割り当てる」）。
//
// **対話するコマンドは continuo setup の1つだけである。**`continuo init` と `continuo trust` は
// 標準入力を1度も握らない。対話をここへ切り出してあるので、`init` は自動化から叩ける。
//
// **ボードは1文字も書き換えない。**このパッケージが gh へ渡すのは
// `gh project field-list`（読み取り）だけである。**選択肢を足す API は呼ばない**
// （`updateProjectV2Field` は選択肢の指定を全件の置き換えとして扱うので、設定済みの
// Status が全部消える）。選択肢が足りないときは、GitHub の画面から足すよう案内して打ち切る。
//
// **WORKFLOW.md は書かない。**割り当てが決まったら Assignment.Statuses() を
// `scaffold.WriteTemplateWithValues` へ渡すのは呼び出し側（cmd/continuo）である。
// 書き出しの実体を2箇所に持たないためである。
package setup

import "github.com/maimuzo/continuo/internal/i18n"

// Role は continuo がボードの Status に与える役割である。
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
)

// RoleCount は割り当てる役割の数である。
//
// **選択肢はこの数だけ要る。**1つの選択肢を2つの役割へ割り当てないので、
// 選択肢がこれより少ないボードでは対話が必ず途中で行き止まる。
const RoleCount = 5

// roleOrder は尋ねる順序である。**issue が実際に通る順に並べてある。**
// ボード上の並びと同じ順に尋ねると、利用者は一覧を上から順に消化できる。
var roleOrder = [RoleCount]Role{RoleDispatch, RoleRunning, RoleReview, RoleBlocked, RoleDone}

// roleNameKeys は役割の名前の文言のキーである。添字は Role の値。
var roleNameKeys = [RoleCount]i18n.Key{
	RoleDispatch: i18n.KeySetupRoleDispatchName,
	RoleRunning:  i18n.KeySetupRoleRunningName,
	RoleReview:   i18n.KeySetupRoleReviewName,
	RoleBlocked:  i18n.KeySetupRoleBlockedName,
	RoleDone:     i18n.KeySetupRoleDoneName,
}

// roleDescKeys は役割の説明の文言のキーである。添字は Role の値。
//
// **説明は Status の名前ではなく continuo の振る舞いで書く。**初見の利用者は
// 「どの Status がどの役割か」を知らないので、Status 名を先に見せると、名前の似た
// 選択肢を役割の意味と無関係に選ぶ。
var roleDescKeys = [RoleCount]i18n.Key{
	RoleDispatch: i18n.KeySetupRoleDispatchDesc,
	RoleRunning:  i18n.KeySetupRoleRunningDesc,
	RoleReview:   i18n.KeySetupRoleReviewDesc,
	RoleBlocked:  i18n.KeySetupRoleBlockedDesc,
	RoleDone:     i18n.KeySetupRoleDoneDesc,
}

// Roles は割り当てる役割を、尋ねる順に返す。
//
// 戻り値: 着手待ち・作業中・レビュー待ち・保留・完了の順に並んだ役割
// （呼び出し側が書き換えても内部には影響しない）。
func Roles() []Role {
	out := make([]Role, RoleCount)
	copy(out, roleOrder[:])
	return out
}

// Name は役割の名前を、いま選ばれている言語で返す。
//
// 戻り値: 「着手待ち」などの短い名前。範囲外の値なら空文字。
func (r Role) Name() string {
	if r < 0 || int(r) >= RoleCount {
		return ""
	}
	return i18n.T(roleNameKeys[r])
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
