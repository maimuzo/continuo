package workspace

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// cappedBuffer は先頭の一定バイト数だけを覚える io.Writer である。
//
// **外部コマンドの出力を上限なしで溜めない。**エージェントは worktree の中で自由に
// ファイルを作れるので、`git status --porcelain` も workspace_hooks の出力も、
// いくらでも長くなりうる。無人の常駐プロセスが、面倒を見ている対象の作り出した状態で
// 落ちる経路を残さない。
//
// **上限を超えた分は捨てる。**捨てたことは truncated で分かる。
type cappedBuffer struct {
	limit     int
	buf       []byte
	truncated bool
}

// newCappedBuffer は上限つきの Writer を作る。
//
// limit: 覚えておく上限（バイト）。**0 以下なら何も覚えない。**
// 戻り値: 組み立てた Writer。
func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{limit: limit}
}

// Write は io.Writer を満たす。**上限を超えた分は捨てるが、エラーにはしない**
// （エラーにすると外部コマンド側が壊れた出力を書いたように見える）。
//
// p: 書き込まれた内容。
// 戻り値の1つ目: 受け取ったバイト数（常に len(p)）。
// 戻り値の2つ目: 常に nil。
func (c *cappedBuffer) Write(p []byte) (int, error) {
	remaining := c.limit - len(c.buf)
	if remaining <= 0 {
		if len(p) > 0 {
			c.truncated = true
		}
		return len(p), nil
	}
	if len(p) > remaining {
		c.buf = append(c.buf, p[:remaining]...)
		c.truncated = true
		return len(p), nil
	}
	c.buf = append(c.buf, p...)
	return len(p), nil
}

// String は覚えている内容をそのまま返す（前後の空白は落とさない）。
//
// 戻り値: 覚えている内容。
func (c *cappedBuffer) String() string { return string(c.buf) }

// Truncated は上限を超えて捨てた分があるかを返す。
//
// 戻り値: 捨てた分があれば true。
func (c *cappedBuffer) Truncated() bool { return c.truncated }

// text は人間に見せる形（前後の空白を落とし、切り詰めたなら断り書きを付ける）で返す。
//
// 戻り値: エラー文やログに載せる文字列。
func (c *cappedBuffer) text() string {
	trimmed := strings.TrimSpace(c.String())
	if !c.truncated {
		return trimmed
	}
	return fmt.Sprintf("%s …（出力が %d バイトを超えたので切り詰めました）", trimmed, c.limit)
}

// setProcessGroup は、外部コマンドを新しいプロセスグループの長として起動させる。
//
// **これが無いと、時間切れでシェルを殺しても、シェルが起こした子・孫が生き残る。**
// 生き残ったプロセスが出力の pipe を握っていると Cmd.Wait が返らない。
//
// **unix 系の OS を前提にする。**continuo は herdr の Unix domain socket と `sh` に
// 依存しているので、この前提はパッケージ全体で共通である。
//
// cmd: 起動前の外部コマンド。
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup は setProcessGroup で起動した外部コマンドを、プロセスグループごと落とす。
//
// **exec.Cmd.Cancel に渡して使う**（コンテキストが切れたときに呼ばれる）。
//
// cmd: 起動済みの外部コマンド。
// 戻り値: シグナルを送れなかった場合のエラー。
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// 負の PID はプロセスグループ全体を指す。
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		// グループを落とせないときは、せめて本人を落とす。
		return cmd.Process.Kill()
	}
	return nil
}

// readCappedFile はファイルの先頭を上限まで読み、人間に見せる形で返す。
//
// **外部コマンドの出力をファイルで受けたときに使う**（hooks.go を見よ）。
// 上限を超えていたら断り書きを付ける。読めない場合はその旨を返す（エラーにはしない。
// 出力はあくまで補助の情報であり、これで処理を止めない）。
//
// path: 読むファイルのパス。
// limit: 読む上限（バイト）。
// 戻り値: エラー文やログに載せる文字列。
func readCappedFile(path string, limit int) string {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Sprintf("（出力を読めませんでした: %v）", err)
	}
	defer func() { _ = file.Close() }()

	buf := newCappedBuffer(limit)
	if _, err := io.Copy(buf, file); err != nil {
		return fmt.Sprintf("%s（出力の読み取りに失敗しました: %v）", buf.text(), err)
	}
	return buf.text()
}
