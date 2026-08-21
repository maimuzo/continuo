// Package herdr は herdr の socket API へ接続するクライアントである
// （docs/plans/continuo_design.md 2-1）。
//
// 実測でわかった herdr の socket API の性質は次の3点である。
//   - Unix domain socket + 改行区切り JSON。JSON-RPC 2.0 ではない。
//     リクエストは {id, method, params} の3つとも必須で、id は文字列必須、
//     params は空でも {} が要る
//   - 1コネクション = 1リクエスト。応答を1行返した直後にサーバがコネクションを閉じる。
//     コネクションプールを作れない
//   - socket のパスは**設定ファイルの herdr.socket を使う。環境変数 HERDR_SOCKET_PATH は
//     読まない**（読むと設定で切り替える手段が無くなる。2-1）。未指定なら既定の
//     ~/.config/herdr/herdr.sock へ落ちる
//
// このパッケージは herdr サーバへ実際に接続せずにテストできるよう作られている。
// テストでは net.Listen("unix", ...) で偽のサーバを立て、決まった JSON を返させる。
package herdr

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
)

// DefaultSocketPath は herdr の socket API の既定の置き場所である（2-1）。
const DefaultSocketPath = "~/.config/herdr/herdr.sock"

// EnvSocketPath は herdr が pane 内のプロセスへ注入する環境変数の名前である（2-1）。
//
// **continuo はこの環境変数を自分で読まない。**読むと、設定ファイルに書いたパスが
// 黙って無視される（2-1 の理由を参照）。この定数を残しているのは、WORKFLOW.md の雛形が
// 「環境変数で切り替えたいなら ${HERDR_SOCKET_PATH} と書く」と案内しており、
// その名前を1箇所で持つためである。展開は 5-5 の規則で internal/config が行う。
const EnvSocketPath = "HERDR_SOCKET_PATH"

// 待ち時間の既定値である。設定ファイルの claude セクションの既定値
// （docs/plans/continuo_design.md 5-3 の設定例）に合わせてある。
const (
	// DefaultReadTimeout は herdr の socket API の応答を待つ上限である
	// （herdr.read_timeout_ms = 5000）。**対象は herdr の socket API の応答だけ**であり、
	// agent の起動待ちや turn の待機には使わない。
	DefaultReadTimeout = 5 * time.Second
	// DefaultStartupTimeout は herdr の agent 起動を待つ上限である
	// （herdr.startup_timeout_ms = 60000）。agent.start は実測で検知まで既定30秒かかるため、
	// read_timeout_ms では足りない。**worktree 系の3つも同じ上限を使う**
	// （worktree.go の冒頭コメントを参照）。
	DefaultStartupTimeout = 60 * time.Second
	// DefaultTurnTimeout は待ちを伴う1回の呼び出しの上限である
	// （claude.turn_timeout_ms = 3600000）。agent.prompt を待機ありで呼ぶときに使う。
	// **turn の総実行時間の上限ではない**（画面が変わり続けている限り、呼び出し側が待ち直す）。
	DefaultTurnTimeout = time.Hour
)

// Timeouts は呼び出しの種類ごとの待ち時間の上限である。
//
// 設計は3つの別々の上限を定めている（5-3 の設定例）。1つに束ねてはならない。
//   - Read    … herdr の socket API の応答（herdr.read_timeout_ms）
//   - Startup … herdr の agent 起動（herdr.startup_timeout_ms）
//   - Turn    … 1つの turn（claude.turn_timeout_ms）
//
// 0 以下のフィールドは New が既定値（DefaultReadTimeout 等）で埋める。
type Timeouts struct {
	// Read は herdr の socket API の応答を待つ上限である。**即答する呼び出しにだけ
	// 適用する。**待ちを伴う呼び出し（agent.start・待機ありの agent.prompt・
	// worktree.create / worktree.open / worktree.remove）には適用しない。
	Read time.Duration
	// Startup は待ちを伴う呼び出し（agent.start と worktree 系の3つ）の応答を待つ
	// 上限である。
	Startup time.Duration
	// Turn は待機あり（Wait が真）の agent.prompt の応答を待つ上限である。
	Turn time.Duration
}

// withDefaults は 0 以下のフィールドを既定値で埋めた写しを返す。
func (t Timeouts) withDefaults() Timeouts {
	if t.Read <= 0 {
		t.Read = DefaultReadTimeout
	}
	if t.Startup <= 0 {
		t.Startup = DefaultStartupTimeout
	}
	if t.Turn <= 0 {
		t.Turn = DefaultTurnTimeout
	}
	return t
}

// checkAbsSocketPath は herdr の socket のパスが絶対パスかどうかを検査する。
//
// internal/socketpath の checkAbs と同じ理由で必要である。相対パスだと continuo を
// 起動したディレクトリによって接続先が変わり、無人運用では「原因の分からない接続エラー」
// になる。設定を読んだ時点で、値の出どころを名指しして落とす。
//
// path: 検査対象のパス。
// source: エラーメッセージに出す、その値の出どころの説明
// （例: "環境変数 HERDR_SOCKET_PATH"、"設定ファイルの herdr.socket"）。
// 戻り値: 絶対パスでない場合にエラーを返す。
func checkAbsSocketPath(path, source string) error {
	if !filepath.IsAbs(path) {
		return i18n.Errorf(i18n.KeyHerdrSocketPathNotAbsolute, source, path)
	}
	return nil
}

// ResolveSocketPath は herdr の socket API に接続する Unix domain socket の絶対パスを、
// 次の優先順位で決める（2-1）。
//
//  1. configured（設定ファイルの herdr.socket の値。5-5 の展開を済ませた後の値を渡すこと）
//  2. 既定値 ~/.config/herdr/herdr.sock
//
// **環境変数 HERDR_SOCKET_PATH をここで読んではならない。**設定に書いたパスが黙って
// 無視されると、continuo は利用者が指定した先とは別の herdr で pane を作り worktree を消す。
// herdr の pane の中で動かす環境ではこの環境変数が常に入っているので、優先すると
// 設定ファイルで切り替える手段が一切なくなる（テストでも切り替えられない）。
// 環境変数で切り替えたい利用者は設定に ${HERDR_SOCKET_PATH} と書く。展開は 5-5 の規則で
// internal/config が行い、未定義ならそこで落ちる（既定値へは落ちない）。
//
// 決まった値が絶対パスでなければエラーにする（checkAbsSocketPath）。
//
// configured: 設定ファイルの herdr.socket の値。空文字なら未指定として扱う。
// 戻り値: 決定した socket の絶対パス。既定値へ落ちるときにホームディレクトリの取得に
// 失敗した場合はエラーを返す。
func ResolveSocketPath(configured string) (string, error) {
	if configured != "" {
		if err := checkAbsSocketPath(configured, i18n.T(i18n.KeyHerdrSocketPathSourceConfig)); err != nil {
			return "", err
		}
		return configured, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", i18n.Errorf(i18n.KeyHerdrSocketPathHomeDirFailed, DefaultSocketPath, err)
	}
	return filepath.Join(home, ".config", "herdr", "herdr.sock"), nil
}

// Client は herdr の socket API を呼び出すクライアントである。
//
// 1コネクション = 1リクエストという herdr の性質上、Client はコネクションを
// 保持しない。呼び出しのたびに新しく connect する（2-1）。
type Client struct {
	// socketPath は接続先の Unix domain socket の絶対パスである。
	socketPath string
	// timeouts は呼び出しの種類ごとの待ち時間の上限である。
	timeouts Timeouts
}

// New は herdr の socket API クライアントを作る。
//
// socketPath: 接続先の Unix domain socket の絶対パス（ResolveSocketPath で決めた値）。
// timeouts: 呼び出しの種類ごとの待ち時間の上限。0 以下のフィールドは既定値で埋める
// （DefaultReadTimeout / DefaultStartupTimeout / DefaultTurnTimeout）。
// 戻り値: 呼び出し可能な *Client。この時点では socket に接続しない
// （接続は各メソッド呼び出しのたびに行う）。
func New(socketPath string, timeouts Timeouts) *Client {
	return &Client{socketPath: socketPath, timeouts: timeouts.withDefaults()}
}

// ReadTimeout は herdr の socket API の応答を待つ上限を返す。
func (c *Client) ReadTimeout() time.Duration { return c.timeouts.Read }

// StartupTimeout は agent.start の応答を待つ上限を返す。
func (c *Client) StartupTimeout() time.Duration { return c.timeouts.Startup }

// TurnTimeout は待機ありの agent.prompt の応答を待つ上限を返す。
func (c *Client) TurnTimeout() time.Duration { return c.timeouts.Turn }

// wireRequest は herdr の socket API へ送るリクエストの wire format である（2-1）。
// id・method・params の3つとも必須で、id は文字列、params は空でも {} を送る。
type wireRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// wireError は herdr が返すエラー応答の中身である。
type wireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// wireResponse は herdr の socket API から返る応答の wire format である。
//
// **ID はリクエストとの対応づけには使えない。**エラー応答では空文字で返る（2-1 の実測）。
// 受け取った内容をログに出す用途のためだけに残してある。
type wireResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *wireError      `json:"error"`
}

// newRequestID はリクエストごとに一意な id 文字列を作る。
//
// 戻り値: 16バイトの乱数を16進数表記した32文字の文字列。crypto/rand を使うため、
// Client を複数の goroutine から同時に使っても衝突しない（共有のカウンタ状態を
// 持たないため排他制御も不要である）。
func newRequestID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", i18n.Errorf(i18n.KeyHerdrCallRequestIDFailed, err)
	}
	return hex.EncodeToString(buf), nil
}

// marshalParams は params を JSON へ変換する。
// nil、または JSON化した結果が "null" になる値（例: 型付き nil ポインタ）は、
// herdr が要求する「params は空でも {} が要る」（2-1）を満たすため "{}" に変換する。
func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return json.RawMessage("{}"), nil
	}
	b, err := json.Marshal(params)
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyHerdrCallMarshalParamsFailed, err)
	}
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		return json.RawMessage("{}"), nil
	}
	return b, nil
}

// canceledError は「呼び出し側が ctx を打ち切った」ことを表すエラーを作る。
//
// **herdr の障害と区別できる形にする。**打ち切りを herdr の障害として報告すると、
// 停止処理の途中で run が「stall した」として捨てられる。
//
// method: 打ち切られた呼び出しのメソッド名（例: "agent.prompt"）。
// cause: ctx.Err() の値（context.Canceled または context.DeadlineExceeded）。
// 戻り値: Code が ErrCodeCanceled の *Error。cause を包むので
// errors.Is(err, context.Canceled) で判定できる。
func canceledError(method string, cause error) error {
	return &Error{
		Code:    ErrCodeCanceled,
		Message: fmt.Sprintf("herdr の呼び出しが打ち切られました（method=%s）", method),
		Err:     cause,
	}
}

// call は herdr の socket API を1回呼び出す。すべてのメソッド呼び出し（pane.split・
// agent.start 等）はこの関数を経由する。
//
// 待ち時間は呼び出しの種類ごとに違うため、必ず引数で渡す（agent.start は Startup、
// 待機ありの agent.prompt は Turn、それ以外は Read）。read_timeout_ms 一本で
// すべてを打ち切ってはならない。
//
// ctx: 呼び出し全体（接続・送信・応答待ち）に適用するコンテキスト。**ctx に期限が
// あればその期限を使い、無ければ timeout を使う。**早いほうを採らないのは、呼び出し側が
// 既定より長い待ち時間を与えられるようにするためである。**ctx を cancel すると、応答待ちの
// 途中でも即座に打ち切る**（context.AfterFunc で socket を閉じる。conn.SetDeadline だけでは
// cancel が効かない）。
// method: 呼び出すメソッド名（例: "pane.split"）。
// params: リクエストの params に入れる値。nil を渡すと "{}" として送る
// （marshalParams を参照）。
// timeout: ctx に期限が無いときに使う待ち時間の上限。
// 戻り値: 成功時は応答の result フィールドの生 JSON。**失敗はすべて *Error で返る**
// （errors.As で判定できる。IsCode / IsTransient を使うとより簡潔に判定できる）。
//   - herdr のエラー応答（{"error":{"code":...,"message":...}}）… herdr が返した Code
//   - 接続・送信・応答読み取りの失敗 … ErrCodeTransport（Retryable が真）
//   - 待ち時間が尽きた … ErrCodeReadTimeout（Retryable が真）
//   - 呼び出し側が ctx を打ち切った … ErrCodeCanceled（Retryable は偽。
//     errors.Is(err, context.Canceled) で辿れる）
func (c *Client) call(
	ctx context.Context,
	method string,
	params any,
	timeout time.Duration,
) (json.RawMessage, error) {
	id, err := newRequestID()
	if err != nil {
		return nil, err
	}

	paramsJSON, err := marshalParams(params)
	if err != nil {
		return nil, err
	}

	reqBytes, err := json.Marshal(wireRequest{ID: id, Method: method, Params: paramsJSON})
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyHerdrCallMarshalRequestFailed, err)
	}
	// 改行区切り JSON なので、1行として送る（2-1）。
	reqBytes = append(reqBytes, '\n')

	// 1コネクション = 1リクエストなので、呼び出しのたびに connect し直す（2-1）。
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, canceledError(method, ctxErr)
		}
		return nil, &Error{
			Code:      ErrCodeTransport,
			Message:   fmt.Sprintf("herdr の socket に接続できません: %s", c.socketPath),
			Retryable: true,
			Err:       err,
		}
	}
	defer func() { _ = conn.Close() }()

	// **ctx が終わったら conn を閉じる。**conn.SetDeadline は「期限が来たら」しか効かず、
	// 呼び出し側の cancel では解けない。待機ありの agent.prompt は Turn（既定1時間）を
	// 使うので、これが無いと SIGINT を受けても停止処理が最大1時間ブロックする。
	// 閉じた結果の読み取りエラーは、下で ctx.Err() を先に見て「打ち切り」として返す。
	stopOnDone := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopOnDone()

	// ctx に期限があればそれを使う。無ければ呼び出しの種類ごとの timeout を使う。
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok {
		deadline = ctxDeadline
	}
	// エラーメッセージに出す「実際に待った上限」。
	budget := time.Until(deadline)
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, &Error{
			Code:      ErrCodeTransport,
			Message:   "herdr の socket にタイムアウトを設定できません",
			Retryable: true,
			Err:       err,
		}
	}

	if _, err := conn.Write(reqBytes); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, canceledError(method, ctxErr)
		}
		return nil, &Error{
			Code:      ErrCodeTransport,
			Message:   fmt.Sprintf("herdr へのリクエスト送信に失敗しました（method=%s）", method),
			Retryable: true,
			Err:       err,
		}
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		// **ctx の打ち切りを先に見る。**上の context.AfterFunc が conn を閉じるので、
		// 読み取りは「閉じた socket からの読み取り」という別の顔で失敗する。
		// 先に ctx を見ないと、呼び出し側の cancel が herdr の障害として報告される。
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, canceledError(method, ctxErr)
		}
		// 時間切れは、途中まで読めていてもタイムアウトとして返す。読めたバイトの有無で
		// 別種のエラー（JSON 解析の失敗）に化けると、リトライの可否を判断する層が
		// 時間切れと壊れた応答を区別できなくなる。
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, &Error{
				Code: ErrCodeReadTimeout,
				Message: fmt.Sprintf(
					"herdr からの応答がタイムアウトしました（method=%s, %s 待機, 受信済み %d バイト）",
					method, budget.Round(time.Millisecond), len(line),
				),
				Retryable: true,
				Err:       err,
			}
		}
		if len(line) == 0 {
			return nil, &Error{
				Code:      ErrCodeTransport,
				Message:   fmt.Sprintf("herdr からの応答読み取りに失敗しました（method=%s）", method),
				Retryable: true,
				Err:       err,
			}
		}
		// 改行の直前でサーバがコネクションを閉じる実装（EOF）でも、1行分の JSON が
		// 揃っていれば応答として扱う。揃っていなければ「途中で切れた応答」として、
		// 読み取りエラーを添えて返す。
		if !json.Valid(bytes.TrimSpace(line)) {
			return nil, &Error{
				Code: ErrCodeTransport,
				Message: fmt.Sprintf(
					"herdr の応答が途中で切れています（method=%s, body=%s）", method, string(line),
				),
				Retryable: true,
				Err:       err,
			}
		}
	}

	var resp wireResponse
	if err := json.Unmarshal(bytes.TrimSpace(line), &resp); err != nil {
		// **壊れた応答は一時的な失敗ではない。**同じものが返ってくるだけなので Retryable にしない。
		return nil, &Error{
			Code: ErrCodeTransport,
			Message: fmt.Sprintf(
				"herdr の応答を JSON として解析できません（method=%s, body=%s）", method, string(line),
			),
			Err: err,
		}
	}

	// **応答の id は当てにしない**（2-1。2026-08-18 の実測）。
	// 正常時は送った id がそのまま返るが、**エラー応答では id が空文字で返る。**
	//
	//	正常:   {"id": "probe", "result": {...}}
	//	エラー: {"id": "", "error": {"code": "invalid_request", "message": "..."}}
	//
	// したがって id で応答を対応づけることはできない。もっとも1コネクション = 1リクエストで、
	// 接続したその場で1行だけ読むので、対応づける必要がそもそも無い。
	// id は herdr が必須としているので送るだけである。
	if resp.Error != nil {
		return nil, &Error{Code: resp.Error.Code, Message: resp.Error.Message}
	}

	return resp.Result, nil
}
