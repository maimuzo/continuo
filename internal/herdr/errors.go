package herdr

import (
	"errors"
	"fmt"
)

// ErrCodeAgentPaneBusy は、pane を作った直後に agent.start を呼ぶと返ることがある
// エラーコードである（実測。設計 2-1）。「pane を作った直後は agent_pane_busy が
// 返ることがあるのでリトライする」の判定に使う。
const ErrCodeAgentPaneBusy = "agent_pane_busy"

// ErrCodeTimeout は、待ち受けつきの呼び出し（agent.prompt の wait / agent.wait）が
// 期限までに落ち着かなかったときに返るエラーコードである（実測。設計 3-2）。
//
// **turn の時間切れと枠待ちを分ける起点である。**このコードで返ったら、枠待ちかどうかを
// 判定し（設計 3-27 の2条件）、枠待ちなら agent.wait で待ち直す（agent.prompt は再送しない）。
const ErrCodeTimeout = "timeout"

// Error は herdr の socket API が返すエラー応答（{"error":{"code":...,"message":...}}）
// を表す Go の error である。
type Error struct {
	// Code は herdr が返すエラーコード（例: "agent_pane_busy"）である。
	Code string
	// Message は herdr が返す人間可読なエラーメッセージである。
	Message string
}

// Error は error インターフェースを満たす。
func (e *Error) Error() string {
	return fmt.Sprintf("herdr エラー [%s]: %s", e.Code, e.Message)
}

// IsCode は err が herdr の *Error であり、かつその Code が code と一致するかどうかを
// 判定する。err がラップされたエラーであっても errors.As で辿って判定する。
//
// err: 判定対象のエラー。
// code: 期待するエラーコード（例: ErrCodeAgentPaneBusy）。
// 戻り値: 一致すれば true。err が herdr の *Error でない場合、または nil の場合は false。
func IsCode(err error, code string) bool {
	var herdrErr *Error
	if errors.As(err, &herdrErr) {
		return herdrErr.Code == code
	}
	return false
}
