package herdr

import (
	"errors"
	"fmt"
)

// ErrCodeAgentPaneBusy は、pane を作った直後に agent.start を呼ぶと返ることがある
// エラーコードである（実測。設計 2-1）。「pane を作った直後は agent_pane_busy が
// 返ることがあるのでリトライする」の判定に使う。
const ErrCodeAgentPaneBusy = "agent_pane_busy"

// ErrCodeAgentNotFound は、その名前の agent が herdr に登録されていないことを表す
// エラーコードである（実測: 2026-08-21）。
//
// **`agent.start` が成功したのにこれが返ることがある。**pane のシェルが準備できて
// いないまま `agent.start` を受け付けると、Claude Code は1文字も起動せず、
// agent も登録されない。**起動できていない証拠として使う**（設計 3-16 の段10）。
const ErrCodeAgentNotFound = "agent_not_found"

// ErrCodeTimeout は、待ち受けつきの呼び出し（agent.prompt の wait / agent.wait）が
// 期限までに落ち着かなかったときに返るエラーコードである（実測。設計 3-2）。
//
// **turn の時間切れと枠待ちを分ける起点である。**このコードで返ったら、枠待ちかどうかを
// 判定し（設計 3-27 の2条件）、枠待ちなら agent.wait で待ち直す（agent.prompt は再送しない）。
//
// **これは herdr が返すコードである。**continuo 側の待ち時間が尽きた場合は
// ErrCodeReadTimeout になる（区別しないと、herdr へ届いてすらいない呼び出しを
// 「turn が時間切れした」と誤認する）。
const ErrCodeTimeout = "timeout"

// ErrCodeTransport は herdr の socket へ届かなかった・送れなかった・応答を読めなかった
// ことを表す、**continuo 側が付けるエラーコード**である（herdr は返さない）。
//
// **一時的な失敗である**（Retryable が真になる）。herdr が再起動しただけでもこれになるので、
// 呼び出し側はこれを受けたときに run を捨ててはならない（IsTransient を使う）。
const ErrCodeTransport = "transport"

// ErrCodeReadTimeout は continuo 側の待ち時間（Timeouts の Read / Startup / Turn、
// または ctx の期限）が尽きたことを表す、**continuo 側が付けるエラーコード**である。
//
// **一時的な失敗である**（Retryable が真になる）。herdr が返す ErrCodeTimeout
// （待ち受けが期限までに落ち着かなかった）とは別物なので、コードを分けてある。
const ErrCodeReadTimeout = "read_timeout"

// ErrCodeCanceled は呼び出し側が ctx を打ち切ったことを表す、**continuo 側が付ける
// エラーコード**である。
//
// **一時的な失敗ではない**（Retryable は偽）。停止処理の途中でこれを受けたら、
// リトライせずそのまま畳むこと。包んだ ctx のエラーは errors.Is(err, context.Canceled)
// で辿れる。
const ErrCodeCanceled = "canceled"

// Error は herdr の socket API が返すエラー応答（{"error":{"code":...,"message":...}}）と、
// そこへ届かなかった失敗（接続・送信・応答の読み取り）を表す Go の error である。
//
// **一時的な失敗と恒久的な失敗を区別できる形にしてある。**区別が無いと、herdr が一瞬落ちて
// 再起動しただけの呼び出し側が「待ち受けが返ったのに Stop が来なかった」と誤った理由で
// run を捨てる（設計 3-2 の stall 判定と混ざる）。判定には IsTransient を使う。
type Error struct {
	// Code は herdr が返すエラーコード（例: "agent_pane_busy"）、または continuo 側が
	// 付けるコード（ErrCodeTransport / ErrCodeReadTimeout / ErrCodeCanceled）である。
	Code string
	// Message は人間可読なエラーメッセージである。
	Message string
	// Retryable は同じ呼び出しをやり直してよいかどうかのヒントである。
	// **herdr パッケージ自身はリトライしない**（AgentStartWithRetry を除く。呼び出し側の責務）。
	Retryable bool
	// Err はラップした元のエラーである（無ければ nil）。errors.Is / errors.As で辿れる。
	Err error
}

// Error は error インターフェースを満たす。
func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("herdr エラー [%s]: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("herdr エラー [%s]: %s", e.Code, e.Message)
}

// Unwrap は errors.Is / errors.As がラップした元のエラーを辿れるようにする
// （internal/tracker の Error と同じ形）。
func (e *Error) Unwrap() error {
	return e.Err
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

// IsTransient は err が「やり直せば通るかもしれない一時的な失敗」かどうかを判定する。
//
// **呼び出し側はこれが真のとき run を捨ててはならない。**herdr の再起動・socket の一時的な
// 不通・応答の遅れがこれに当たる。次の巡回へ持ち越すこと。
//
// err: 判定対象のエラー。
// 戻り値: herdr の *Error であり Retryable が真なら true。それ以外（nil、herdr 以外の
// エラー、恒久的な失敗）は false。
func IsTransient(err error) bool {
	var herdrErr *Error
	if errors.As(err, &herdrErr) {
		return herdrErr.Retryable
	}
	return false
}
