// Package claudeversion は、continuo が起動する Claude Code の版を確かめる（issue #288）。
//
// **v2.1.304 より古い Claude Code では、continuo は着手できない。**continuo は Claude Code を
// `--append-system-prompt-file`・`--system-prompt-snapshot off`・`--plugin-dir` を付けて起動する。
//
//	v2.1.257 より前  `--system-prompt-snapshot` を知らず、`error: unknown option` で終了コード 1 になる
//	                 （2.1.250 で実測）。再着手では新しいセッションへ倒れ、前の会話を捨てる
//	v2.1.257〜303    起動はできるが、compaction のあとにスキルの一覧を作り直さない
//	                 （公式文書 context-window「Skill descriptions: Rebuilt … Requires Claude Code v2.1.304 or later」）。
//	                 **起動のエラーを出さないので、版を見ないと気づけない**
//
// **v2.1.304 から v2.1.309 は測っていない**（測ったのは 2.1.310）。下限は公式文書の版で決めた。
//
// **確かめるのは continuo の PATH の `claude.kind` である。**Claude Code を起動するのは herdr の pane の
// PATH なので、2つは違いうる。**だから読めないときは止めない**（警告だけにする）。
package claudeversion

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
)

// Minimum は continuo が要る Claude Code の版の下限である。
const Minimum = "2.1.304"

// Timeout は `--version` を待つ上限である。
const Timeout = 10 * time.Second

// Status は確かめた結果の種類である。
type Status int

const (
	// StatusOK は、版を読めて、下限以上だったことを表す。
	StatusOK Status = iota
	// StatusTooOld は、版を読めて、下限より古かったことを表す。**起動しない。**
	StatusTooOld
	// StatusUnknown は、版を読めなかったことを表す。**警告だけ出して起動する。**
	StatusUnknown
)

// Result は確かめた結果である。
type Result struct {
	// Status は結果の種類である。
	Status Status
	// Path は確かめた実行ファイルの絶対パスである。PATH に無ければ空文字。
	Path string
	// Version は読めた版である（`2.1.310` の形）。読めなければ空文字。
	Version string
	// Err は読めなかった理由である（StatusUnknown のときだけ入る）。
	Err error
}

// Runner は `<実行ファイル> --version` を叩いて標準出力を返す関数である。**テストが差し替える口である。**
type Runner func(ctx context.Context, path string) (string, error)

// LookPath は実行ファイルを PATH から探す関数である。**テストが差し替える口である。**
type LookPath func(file string) (string, error)

// DefaultRunner は本物の `--version` を叩く。
//
// ctx: 叩く期限。
// path: 実行ファイルのパス。
// 戻り値: 標準出力と、叩けなかった理由。
func DefaultRunner(ctx context.Context, path string) (string, error) {
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	return string(out), err
}

// Check は `kind` の実行ファイルの版を確かめる。
//
// ctx: 叩く期限の親。**Timeout をさらに掛ける。**
// kind: 設定の `claude.kind`（実行ファイルの名前でもある）。
// look: PATH から探す関数。nil なら exec.LookPath を使う。
// run: `--version` を叩く関数。nil なら DefaultRunner を使う。
// 戻り値: 確かめた結果。
func Check(ctx context.Context, kind string, look LookPath, run Runner) Result {
	if look == nil {
		look = exec.LookPath
	}
	if run == nil {
		run = DefaultRunner
	}
	path, err := look(kind)
	if err != nil {
		return Result{Status: StatusUnknown, Err: err}
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	out, err := run(ctx, path)
	if err != nil {
		return Result{Status: StatusUnknown, Path: path, Err: err}
	}
	v, ok := Parse(out)
	if !ok {
		return Result{Status: StatusUnknown, Path: path, Err: i18n.Errorf(i18n.KeyClaudeVersionNoVersion, strings.TrimSpace(out))}
	}
	if Less(v, Minimum) {
		return Result{Status: StatusTooOld, Path: path, Version: v}
	}
	return Result{Status: StatusOK, Path: path, Version: v}
}

// versionPattern は `2.1.310 (Claude Code)` の先頭の版に当たる。
var versionPattern = regexp.MustCompile(`\b(\d+)\.(\d+)\.(\d+)\b`)

// Parse は `--version` の出力から版を取り出す。
//
// out: `--version` の標準出力。
// 戻り値の1つ目: `メジャー.マイナー.パッチ` の形の版。
// 戻り値の2つ目: 取り出せたら真。
func Parse(out string) (string, bool) {
	m := versionPattern.FindStringSubmatch(out)
	if m == nil {
		return "", false
	}
	return m[1] + "." + m[2] + "." + m[3], true
}

// Less は、版 a が版 b より古いかを返す（`メジャー.マイナー.パッチ` を数として比べる）。
//
// a / b: `メジャー.マイナー.パッチ` の形の版。読めない部分は 0 とみなす。
// 戻り値: a が b より古ければ真。
func Less(a, b string) bool {
	pa, pb := parts(a), parts(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

// parts は版を3つの数に分ける。
//
// v: 版。
// 戻り値: メジャー・マイナー・パッチ。
func parts(v string) [3]int {
	var out [3]int
	for i, s := range strings.SplitN(v, ".", 3) {
		n, _ := strconv.Atoi(s)
		out[i] = n
	}
	return out
}
