// Package herdr は herdr の socket API へ接続するクライアントである
// （docs/plans/continuo_design.md 2-1）。
//
// 実測でわかった herdr の socket API の性質は次の3点である。
//   - Unix domain socket + 改行区切り JSON。JSON-RPC 2.0 ではない。
//     リクエストは {id, method, params} の3つとも必須で、id は文字列必須、
//     params は空でも {} が要る
//   - 1コネクション = 1リクエスト。応答を1行返した直後にサーバがコネクションを閉じる。
//     コネクションプールを作れない
//   - socket のパスは環境変数 HERDR_SOCKET_PATH が最優先。無ければ設定の値、
//     それも無ければ既定の ~/.config/herdr/herdr.sock
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
	// （claude.read_timeout_ms = 5000）。**対象は herdr の socket API の応答だけ**であり、
	// agent の起動待ちや turn の待機には使わない。
	DefaultReadTimeout = 5 * time.Second
	// DefaultStartupTimeout は herdr の agent 起動を待つ上限である
	// （claude.startup_timeout_ms = 60000）。agent.start は実測で検知まで既定30秒かかるため、
	// read_timeout_ms では足りない。
	DefaultStartupTimeout = 60 * time.Second
	// DefaultTurnTimeout は1つの turn の上限である（claude.turn_timeout_ms = 3600000）。
	// agent.prompt を待機ありで呼ぶときに使う。
	DefaultTurnTimeout = time.Hour
)

// Timeouts は呼び出しの種類ごとの待ち時間の上限である。
//
// 設計は3つの別々の上限を定めている（5-3 の設定例）。1つに束ねてはならない。
//   - Read    … herdr の socket API の応答（claude.read_timeout_ms）
//   - Startup … herdr の agent 起動（claude.startup_timeout_ms）
//   - Turn    … 1つの turn（claude.turn_timeout_ms）
//
// 0 以下のフィールドは New が既定値（DefaultReadTimeout 等）で埋める。
type Timeouts struct {
	// Read は herdr の socket API の応答を待つ上限である。agent.start と、
	// 待機ありの agent.prompt を除くすべての呼び出しに適用する。
	Read time.Duration
	// Startup は agent.start の応答を待つ上限である。
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
		return fmt.Errorf(
			"%s の値 %q が絶対パスではありません（相対パスだと continuo を起動したディレクトリによって"+
				"herdr の socket の場所が変わる）",
			source, path,
		)
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
		if err := checkAbsSocketPath(configured, "設定ファイルの herdr.socket"); err != nil {
			return "", err
		}
		return configured, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf(
			"herdr の socket の場所を決められません（既定の %s のためホームディレクトリの"+
				"取得が必要ですが失敗しました）: %w",
			DefaultSocketPath, err,
		)
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
		return "", fmt.Errorf("リクエスト id の生成に失敗しました: %w", err)
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
		return nil, fmt.Errorf("params の JSON 変換に失敗しました: %w", err)
	}
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		return json.RawMessage("{}"), nil
	}
	return b, nil
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
// 既定より長い待ち時間を与えられるようにするためである。
// method: 呼び出すメソッド名（例: "pane.split"）。
// params: リクエストの params に入れる値。nil を渡すと "{}" として送る
// （marshalParams を参照）。
// timeout: ctx に期限が無いときに使う待ち時間の上限。
// 戻り値: 成功時は応答の result フィールドの生 JSON。herdr がエラー応答
// （{"error":{"code":...,"message":...}}）を返した場合は *Error を返す
// （errors.As で判定できる。IsCode を使うとより簡潔に判定できる）。
// 接続・送信・応答読み取りに失敗した場合もエラーを返す。
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
		return nil, fmt.Errorf("herdr へのリクエストの組み立てに失敗しました: %w", err)
	}
	// 改行区切り JSON なので、1行として送る（2-1）。
	reqBytes = append(reqBytes, '\n')

	// 1コネクション = 1リクエストなので、呼び出しのたびに connect し直す（2-1）。
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return nil, fmt.Errorf("herdr の socket に接続できません: %s: %w", c.socketPath, err)
	}
	defer func() { _ = conn.Close() }()

	// ctx に期限があればそれを使う。無ければ呼び出しの種類ごとの timeout を使う。
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok {
		deadline = ctxDeadline
	}
	// エラーメッセージに出す「実際に待った上限」。
	budget := time.Until(deadline)
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("herdr の socket にタイムアウトを設定できません: %w", err)
	}

	if _, err := conn.Write(reqBytes); err != nil {
		return nil, fmt.Errorf("herdr へのリクエスト送信に失敗しました（method=%s）: %w", method, err)
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		// 時間切れは、途中まで読めていてもタイムアウトとして返す。読めたバイトの有無で
		// 別種のエラー（JSON 解析の失敗）に化けると、リトライの可否を判断する層が
		// 時間切れと壊れた応答を区別できなくなる。
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, fmt.Errorf(
				"herdr からの応答がタイムアウトしました（method=%s, %s 待機, 受信済み %d バイト）: %w",
				method, budget.Round(time.Millisecond), len(line), err,
			)
		}
		if len(line) == 0 {
			return nil, fmt.Errorf("herdr からの応答読み取りに失敗しました（method=%s）: %w", method, err)
		}
		// 改行の直前でサーバがコネクションを閉じる実装（EOF）でも、1行分の JSON が
		// 揃っていれば応答として扱う。揃っていなければ「途中で切れた応答」として、
		// 読み取りエラーを添えて返す。
		if !json.Valid(bytes.TrimSpace(line)) {
			return nil, fmt.Errorf(
				"herdr の応答が途中で切れています（method=%s, body=%s）: %w",
				method, string(line), err,
			)
		}
	}

	var resp wireResponse
	if err := json.Unmarshal(bytes.TrimSpace(line), &resp); err != nil {
		return nil, fmt.Errorf(
			"herdr の応答を JSON として解析できません（method=%s, body=%s）: %w",
			method, string(line), err,
		)
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
