// Package logging は continuo の構造化ログを組み立てる。
// 標準の log/slog（TextHandler）だけを使い、key=value 形式で出力する
// （docs/plans/continuo_design.md 2-5。SPEC.md 13.1 が求める形式をそのまま満たせる）。
package logging

import (
	"io"
	"log/slog"
	"strings"
)

// New は構造化ログ用の *slog.Logger を作る。
//
// w: ログの出力先（本番は os.Stderr、テストでは bytes.Buffer など）。
// level: 出力する最低レベル（例: slog.LevelInfo）。
// 戻り値: log/slog の TextHandler を使った *slog.Logger。呼び出し側は
// logger.Info("メッセージ", "key1", val1, "key2", val2) のように呼ぶことで
// `time=... level=INFO msg="メッセージ" key1=val1 key2=val2` 形式の1行を出力できる。
func New(w io.Writer, level slog.Level) *slog.Logger {
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(handler)
}

// ParseLevel は CLI のフラグなどで受け取ったログレベルの文字列を slog.Level へ変換する。
//
// s: "debug" / "info" / "warn"（"warning" も可）/ "error" のいずれか。
// 大文字小文字は区別せず、前後の空白も無視する。
// 戻り値:
//   - 第1返り値: 対応する slog.Level。認識できない文字列のときは slog.LevelInfo を返す
//     （ログレベルの指定ミスで起動全体を止めるほどの重大さは無いため）。
//   - 第2返り値: 認識できたかどうか。false のときは第1返り値が既定値の LevelInfo であり、
//     指定が効いていないことを意味する。無人運用では指定ミスに誰も気づけないので、
//     呼び出し側はこれを見て必ず警告を出すこと。
func ParseLevel(s string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return slog.LevelInfo, false
	}
}
