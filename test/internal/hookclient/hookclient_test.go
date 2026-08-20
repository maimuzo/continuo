// Package hookclient_test は internal/hookclient（`continuo hook` の実体）を検証する。
//
// Claude Code は起動しない。hook の JSON を標準入力として渡し、偽の受け口
// （net.Listen("unix", ...) で立てた socket）へ何が書かれるかを見る。
package hookclient_test

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/hookclient"
)

// forwardWaitTimeout は Forward が返るのを待つ上限である。
// `continuo hook` は応答を待たずに終わるので（設計 3-2）、正常系ではこれより遥かに短い。
const forwardWaitTimeout = 5 * time.Second

// fakeSink は hook の受け口の代わりに使う偽のサーバである。
// 受け取った行を記録するだけで、応答は一切返さない（設計 3-2 の「応答なし」を再現する）。
type fakeSink struct {
	socketPath string
	ln         net.Listener

	mu    sync.Mutex
	lines []string

	// received は受け取った行を1行ずつ流すチャネルである。
	// **待つ側がポーリングしないためにある。**一定時間眠って様子を見る書き方は、
	// 負荷の高い環境で偽陰性（届いているのに届いていないと判定する）になる。
	received chan string

	// hold は接続を掴んだまま離さないための合図である。閉じるまで接続を保持する。
	hold chan struct{}
}

// newFakeSink は偽の hook 受け口を1つ立てる。
//
// socket のパスは意図的に短く保つ（macOS の Unix domain socket のパス長上限は
// 103バイト。internal/socketpath の実測）。t.TempDir() はテスト名を含む長いパスに
// なりやすいので使わない。
//
// t: 呼び出し元のテスト。後始末を t.Cleanup に登録する。
// holdConnections: true なら、行を読んだあとも接続を掴んだまま離さない
// （応答を待たずに終わることの検査に使う）。
// 戻り値: 立てた fakeSink。
func newFakeSink(t *testing.T, holdConnections bool) *fakeSink {
	t.Helper()

	dir, err := os.MkdirTemp("", "hookcli")
	if err != nil {
		t.Fatalf("一時ディレクトリを作成できません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	socketPath := filepath.Join(dir, "hooks.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("偽の hook 受け口を bind できません（%s）: %v", socketPath, err)
	}

	fs := &fakeSink{
		socketPath: socketPath,
		ln:         ln,
		received:   make(chan string, 128),
		hold:       make(chan struct{}),
	}
	t.Cleanup(func() {
		close(fs.hold)
		_ = ln.Close()
	})

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_ = c.SetReadDeadline(time.Now().Add(forwardWaitTimeout))
				reader := bufio.NewReader(c)
				line, err := reader.ReadString('\n')
				if len(line) > 0 || err == nil {
					fs.mu.Lock()
					fs.lines = append(fs.lines, line)
					fs.mu.Unlock()
					fs.received <- line
				}
				if holdConnections {
					<-fs.hold
				}
			}(conn)
		}
	}()

	return fs
}

// Lines はこれまでに受け取った行（末尾の改行を含む）を受け取った順に返す。
func (fs *fakeSink) Lines() []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	out := make([]string, len(fs.lines))
	copy(out, fs.lines)
	return out
}

// waitForLines は n 行を受け取るまで待つ。
//
// t: 呼び出し元のテスト。
// n: 待つ行数。
// 戻り値: 受け取った行。forwardWaitTimeout を超えたら t.Fatalf で落とす。
func (fs *fakeSink) waitForLines(t *testing.T, n int) []string {
	t.Helper()
	got := make([]string, 0, n)
	deadline := time.After(forwardWaitTimeout)
	for len(got) < n {
		select {
		case line := <-fs.received:
			got = append(got, line)
		case <-deadline:
			t.Fatalf("受け口が %d 行を受け取りませんでした（受け取ったのは %d 行）", n, len(got))
		}
	}
	return got
}

// newPendingDir は逃がし先のディレクトリを1つ作る。
//
// t: 呼び出し元のテスト。後始末を t.Cleanup に登録する。
// 戻り値: 作ったディレクトリの絶対パス。
func newPendingDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hookpending")
	if err != nil {
		t.Fatalf("逃がし先の一時ディレクトリを作成できません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "issues", "maimuzo-koetsumugi-188", "pending")
}

// fixedNow は受信時刻を固定する Now を返す（逃がし先のファイル名を検査するため）。
//
// micros: UnixMicro で返したい値。
// 戻り値: 常に同じ時刻を返す関数。
func fixedNow(micros int64) func() time.Time {
	return func() time.Time { return time.UnixMicro(micros) }
}

// entryNames は dir の中のファイル名を返す（ディレクトリが無ければ空）。
//
// t: 呼び出し元のテスト。
// dir: 一覧を取るディレクトリ。
// 戻り値: ファイル名の一覧（os.ReadDir の順、つまり名前の昇順）。
func entryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("ディレクトリを読めません: %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// 目的: socket へ改行区切りの JSON を1行書いて閉じ、応答を待たずに終わることを確認する
// （受け入れの基準「socket へ改行区切りの JSON を1行書いて閉じる。応答を読まない」
// および「continuo hook は転送して即終了する」）。
// 与える情報: 整形された（改行入りの）hook の JSON と、行を読んでも応答を返さず
// 接続を掴んだまま離さない偽の受け口。
// 成功条件: Forward が待たされずに sent で返り、受け口が受け取った行が1行に詰められた
// JSON であること（末尾に改行が1つ付いていること）。
func TestForward_socketへ1行書いて応答を待たずに終わる(t *testing.T) {
	sink := newFakeSink(t, true)

	input := "{\n  \"hook_event_name\": \"Stop\",\n  \"session_id\": \"s1\",\n  \"background_tasks\": []\n}\n"

	done := make(chan hookclient.Result, 1)
	go func() {
		done <- hookclient.Forward(hookclient.Config{
			SocketPath: sink.socketPath,
			PendingDir: newPendingDir(t),
			Stdin:      strings.NewReader(input),
		})
	}()

	var result hookclient.Result
	select {
	case result = <-done:
	case <-time.After(forwardWaitTimeout):
		t.Fatalf("Forward が返りませんでした（応答を待ってしまっています）")
	}

	if result.Outcome != hookclient.OutcomeSent {
		t.Fatalf("転送できたはずが %s でした: %v", result.Outcome, result.Err)
	}
	if result.EventName != "Stop" {
		t.Fatalf("hook_event_name を読めていません: %q", result.EventName)
	}

	lines := sink.waitForLines(t, 1)
	want := `{"hook_event_name":"Stop","session_id":"s1","background_tasks":[]}` + "\n"
	if lines[0] != want {
		t.Fatalf("受け口が受け取った行が想定と違います:\n  受け取った: %q\n  想定: %q", lines[0], want)
	}
}

// 目的: socket へ繋がらないとき、逃がし先へ .json.tmp で書き切ってから rename すること、
// つまり最終的に .json だけが残り .tmp が残らないことを確認する（設計 3-19）。
// 与える情報: 誰も listen していない socket のパスと、逃がし先のディレクトリ。
// 成功条件: 終了の理由が spilled で、逃がし先に <受信時刻>-Stop.json が1つだけでき、
// 中身が入力の JSON と同じであること。.tmp が1つも残っていないこと。
func TestForward_socketへ繋がらなければ逃がし先へ書く(t *testing.T) {
	pendingDir := newPendingDir(t)
	input := `{"hook_event_name":"Stop","session_id":"s1","background_tasks":[]}`

	result := hookclient.Forward(hookclient.Config{
		SocketPath: filepath.Join(filepath.Dir(pendingDir), "no-such.sock"),
		PendingDir: pendingDir,
		Stdin:      strings.NewReader(input),
		Now:        fixedNow(1787057953362306),
	})

	if result.Outcome != hookclient.OutcomeSpilled {
		t.Fatalf("逃がし先へ書いたはずが %s でした: %v", result.Outcome, result.Err)
	}

	names := entryNames(t, pendingDir)
	if len(names) != 1 || names[0] != "1787057953362306-Stop.json" {
		t.Fatalf("逃がし先のファイル名が想定と違います: %v", names)
	}
	if result.PendingPath != filepath.Join(pendingDir, names[0]) {
		t.Fatalf("書いたファイルのパスが返り値と一致しません: %q と %q", result.PendingPath, names[0])
	}

	body, err := os.ReadFile(result.PendingPath)
	if err != nil {
		t.Fatalf("逃がし先のファイルを読めません: %v", err)
	}
	if string(body) != input {
		t.Fatalf("逃がし先の中身が入力と違います:\n  書かれた: %q\n  入力: %q", string(body), input)
	}
}

// 目的: 標準入力が JSON として解釈できなければ、socket へも逃がし先へも書かずに終わることを
// 確認する（受け入れの基準「標準入力が JSON として解釈できなければ、どこにも書かずに exit 0」）。
// 与える情報: JSON ではない標準入力と、listen 中の受け口、逃がし先のディレクトリ。
// 成功条件: 終了の理由が dropped で、受け口が1行も受け取らず、逃がし先にファイルが
// 1つもできないこと（ディレクトリ自体も作られないこと）。
func TestForward_JSONでなければどこにも書かない(t *testing.T) {
	sink := newFakeSink(t, false)
	pendingDir := newPendingDir(t)

	for _, input := range []string{"これは JSON ではない", "", `[1,2,3]`, "null"} {
		result := hookclient.Forward(hookclient.Config{
			SocketPath: sink.socketPath,
			PendingDir: pendingDir,
			Stdin:      strings.NewReader(input),
		})
		if result.Outcome != hookclient.OutcomeDropped {
			t.Fatalf("入力 %q は捨てられるはずが %s でした", input, result.Outcome)
		}
		if result.Err == nil {
			t.Fatalf("入力 %q を捨てた理由が返っていません", input)
		}
	}

	// **時間で様子を見るのではなく、同じ socket をもう一度通す。**
	// 正しい hook を1件送って受け口に届くのを待てば、その時点で「先に呼んだ4件が
	// 何か書いていたなら、それも既に届いている」と言い切れる（同じ接続の受け付け順である）。
	barrier := `{"hook_event_name":"Stop","session_id":"barrier"}`
	if r := hookclient.Forward(hookclient.Config{
		SocketPath: sink.socketPath,
		PendingDir: pendingDir,
		Stdin:      strings.NewReader(barrier),
	}); r.Outcome != hookclient.OutcomeSent {
		t.Fatalf("目印の hook を転送できませんでした: %s: %v", r.Outcome, r.Err)
	}
	sink.waitForLines(t, 1)

	lines := sink.Lines()
	if len(lines) != 1 || lines[0] != barrier+"\n" {
		t.Fatalf("JSON でない入力が socket へ書かれました: %v", lines)
	}
	if names := entryNames(t, pendingDir); len(names) != 0 {
		t.Fatalf("JSON でない入力が逃がし先へ書かれました: %v", names)
	}
}

// 目的: 同じ受信時刻・同じイベント名の hook が続けて届いても、逃がし先のファイルを
// 上書きせずに両方残すことを確認する（上書きすると Stop を1件失う）。
// 与える情報: 受信時刻を固定した Forward の2回の呼び出し。
// 成功条件: 逃がし先に2つのファイルができ、中身がそれぞれの入力と一致すること。
func TestForward_同じ受信時刻でも逃がし先を上書きしない(t *testing.T) {
	pendingDir := newPendingDir(t)
	socketPath := filepath.Join(filepath.Dir(pendingDir), "no-such.sock")

	first := `{"hook_event_name":"Stop","session_id":"s1"}`
	second := `{"hook_event_name":"Stop","session_id":"s2"}`
	for _, input := range []string{first, second} {
		result := hookclient.Forward(hookclient.Config{
			SocketPath: socketPath,
			PendingDir: pendingDir,
			Stdin:      strings.NewReader(input),
			Now:        fixedNow(1787057953362306),
		})
		if result.Outcome != hookclient.OutcomeSpilled {
			t.Fatalf("逃がし先へ書いたはずが %s でした: %v", result.Outcome, result.Err)
		}
	}

	names := entryNames(t, pendingDir)
	if len(names) != 2 {
		t.Fatalf("逃がし先のファイルが2つになっていません: %v", names)
	}
	for i, name := range names {
		body, err := os.ReadFile(filepath.Join(pendingDir, name))
		if err != nil {
			t.Fatalf("逃がし先のファイルを読めません: %v", err)
		}
		want := []string{first, second}[i]
		if string(body) != want {
			t.Fatalf("%s の中身が想定と違います:\n  書かれた: %q\n  想定: %q", name, string(body), want)
		}
	}
}

// 目的: hook_event_name にパス区切りや ".." が入っていても、逃がし先のディレクトリの外へ
// 書かないことを確認する。
// 与える情報: hook_event_name が "../../evil" の hook と、逃がし先のディレクトリ。
// 成功条件: 逃がし先の直下にファイルが1つでき、その名前にパス区切りが含まれないこと。
func TestForward_イベント名にパス区切りがあっても逃がし先の外へ書かない(t *testing.T) {
	pendingDir := newPendingDir(t)
	socketPath := filepath.Join(filepath.Dir(pendingDir), "no-such.sock")

	result := hookclient.Forward(hookclient.Config{
		SocketPath: socketPath,
		PendingDir: pendingDir,
		Stdin:      strings.NewReader(`{"hook_event_name":"../../evil","session_id":"s1"}`),
		Now:        fixedNow(1787057953362306),
	})
	if result.Outcome != hookclient.OutcomeSpilled {
		t.Fatalf("逃がし先へ書いたはずが %s でした: %v", result.Outcome, result.Err)
	}
	if filepath.Dir(result.PendingPath) != pendingDir {
		t.Fatalf("逃がし先の外へ書かれました: %q", result.PendingPath)
	}

	names := entryNames(t, pendingDir)
	if len(names) != 1 || strings.ContainsAny(names[0], "/\\") {
		t.Fatalf("ファイル名がパス区切りを含んでいます: %v", names)
	}
	if names[0] != "1787057953362306-______evil.json" {
		t.Fatalf("イベント名の置き換えが想定と違います: %v", names)
	}
}

// 目的: 逃がし先（--pending-dir）が指定されていないときに、socket へ繋がらなくても
// 落ちずに終わることを確認する。
// 与える情報: 誰も listen していない socket のパスと、空の PendingDir。
// 成功条件: 終了の理由が dropped になり、理由が返ること（呼び出し側はこれを標準エラーへ出す）。
func TestForward_逃がし先が無ければ捨てて終わる(t *testing.T) {
	result := hookclient.Forward(hookclient.Config{
		SocketPath: filepath.Join(os.TempDir(), "continuo-no-such-hook.sock"),
		Stdin:      strings.NewReader(`{"hook_event_name":"Stop","session_id":"s1"}`),
	})
	if result.Outcome != hookclient.OutcomeDropped {
		t.Fatalf("捨てられるはずが %s でした: %v", result.Outcome, result.Err)
	}
	if result.Err == nil {
		t.Fatalf("捨てた理由が返っていません")
	}
}

// 目的: 標準入力が上限を超えても捨てず、判定に要る項目だけを拾って socket へ転送すること、
// そして大きな項目（tool_response）は落として1行に収めることを確認する
// （04_hook.md の受け入れの基準「どのイベントも捨てずに HookSink.OnHook へ渡す」）。
// 与える情報: 上限を 100 バイトに縮めた Config と、tool_response が上限を大きく超える
// PostToolUse の JSON。
// 成功条件: 終了の理由が sent で Truncated が true になり、受け口が受け取った1行に
// session_id・cwd・hook_event_name・continuo_truncated が入っていて、
// tool_response が入っていないこと。
func TestForward_上限を超えた入力は項目を拾って転送する(t *testing.T) {
	sink := newFakeSink(t, false)

	const limit = 100
	input := `{"session_id":"s1","cwd":"/tmp/wt","hook_event_name":"PostToolUse","tool_response":"` +
		strings.Repeat("x", 8192) + `"}`

	result := hookclient.Forward(hookclient.Config{
		SocketPath:    sink.socketPath,
		PendingDir:    newPendingDir(t),
		Stdin:         strings.NewReader(input),
		MaxInputBytes: limit,
	})
	if result.Outcome != hookclient.OutcomeSent {
		t.Fatalf("上限を超えた入力も転送されるはずが %s でした: %v", result.Outcome, result.Err)
	}
	if !result.Truncated {
		t.Fatalf("上限を超えたことが Result に出ていません: %+v", result)
	}

	line := sink.waitForLines(t, 1)[0]
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSuffix(line, "\n")), &got); err != nil {
		t.Fatalf("受け口が受け取った行を JSON として読めません: %q: %v", line, err)
	}
	for key, want := range map[string]any{
		"session_id":                     "s1",
		"cwd":                            "/tmp/wt",
		"hook_event_name":                "PostToolUse",
		"continuo_truncated":             true,
		"continuo_truncated_limit_bytes": float64(limit),
	} {
		if got[key] != want {
			t.Fatalf("組み立て直した hook の %s が %v ではなく %v でした（全体: %v）", key, want, got[key], got)
		}
	}
	if _, ok := got["tool_response"]; ok {
		t.Fatalf("上限を超えた大きな項目が落ちていません: %v", got)
	}
}

// 目的: 上限を超えたうえに先頭からも項目を1つも拾えない入力は、どこにも書かずに捨てる
// ことを確認する（逃がし先のファイル名が決まらないため。設計 3-19）。
// 与える情報: 上限を 20 バイトに縮めた Config と、先頭の1つ目のキーの途中で切れる JSON。
// 成功条件: 終了の理由が dropped で、逃がし先にファイルが1つもできないこと。
func TestForward_上限を超えて項目を1つも拾えなければ捨てる(t *testing.T) {
	sink := newFakeSink(t, false)
	pendingDir := newPendingDir(t)

	input := `{"tool_response":"` + strings.Repeat("x", 8192) + `"}`

	result := hookclient.Forward(hookclient.Config{
		SocketPath:    sink.socketPath,
		PendingDir:    pendingDir,
		Stdin:         strings.NewReader(input),
		MaxInputBytes: 20,
	})
	if result.Outcome != hookclient.OutcomeDropped {
		t.Fatalf("捨てられるはずが %s でした: %v", result.Outcome, result.Err)
	}
	if result.Err == nil {
		t.Fatalf("捨てた理由が返っていません")
	}
	if names := entryNames(t, pendingDir); len(names) != 0 {
		t.Fatalf("拾えなかった入力が逃がし先へ書かれました: %v", names)
	}
}

// 目的: 標準入力が上限を超えた Stop でも、turn の終わりの判定に使う background_tasks /
// stop_hook_active / prompt が値の形のまま届くことを確認する。文字列の項目だけを拾い直すと、
// 上限を超えた Stop は background_tasks が欠けた形で届き、受け取る側は
// 「欠けている（判定不能）」として扱うので turn の終わりにならない（設計 3-2）。
// 与える情報: 上限を 120 バイトに縮めた Config と、background_tasks（空配列）と
// stop_hook_active が先頭側にあり、そのあとに上限を大きく超える項目が続く Stop の JSON。
// 成功条件: 受け口が受け取った1行に background_tasks が空配列として入っており、
// stop_hook_active も入っていること。
func TestForward_上限を超えてもbackground_tasksを落とさない(t *testing.T) {
	sink := newFakeSink(t, false)

	const limit = 120
	input := `{"hook_event_name":"Stop","session_id":"s1","background_tasks":[],"stop_hook_active":false,` +
		`"last_assistant_message":"` + strings.Repeat("x", 8192) + `"}`

	result := hookclient.Forward(hookclient.Config{
		SocketPath:    sink.socketPath,
		PendingDir:    newPendingDir(t),
		Stdin:         strings.NewReader(input),
		MaxInputBytes: limit,
	})
	if result.Outcome != hookclient.OutcomeSent {
		t.Fatalf("上限を超えた Stop も転送されるはずが %s でした: %v", result.Outcome, result.Err)
	}
	if !result.Truncated {
		t.Fatalf("上限を超えたことが Result に出ていません: %+v", result)
	}

	line := sink.waitForLines(t, 1)[0]
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSuffix(line, "\n")), &got); err != nil {
		t.Fatalf("受け口が受け取った行を JSON として読めません: %q: %v", line, err)
	}
	raw, ok := got["background_tasks"]
	if !ok {
		t.Fatalf("background_tasks が落ちています（受け取る側は turn の終わりと判定できません）: %s", line)
	}
	var tasks []any
	if err := json.Unmarshal(raw, &tasks); err != nil {
		t.Fatalf("background_tasks が配列として読めません: %s: %v", raw, err)
	}
	if len(tasks) != 0 {
		t.Fatalf("background_tasks の中身が元のまま（空配列）ではありません: %s", raw)
	}
	if _, ok := got["stop_hook_active"]; !ok {
		t.Fatalf("stop_hook_active が落ちています: %s", line)
	}
}

// 目的: 上限を超えた UserPromptSubmit でも prompt が値の形のまま届くことを確認する。
// prompt は <task-notification> の判定に使うので（設計 1-3）、落とすと判定できない。
// 与える情報: 上限を 160 バイトに縮めた Config と、prompt が先頭側にある UserPromptSubmit。
// 成功条件: 受け口が受け取った1行の prompt に <task-notification> が入っていること。
func TestForward_上限を超えてもpromptを落とさない(t *testing.T) {
	sink := newFakeSink(t, false)

	const limit = 160
	input := `{"hook_event_name":"UserPromptSubmit","session_id":"s1",` +
		`"prompt":"<task-notification><task-id>a1</task-id></task-notification>",` +
		`"cwd":"` + strings.Repeat("x", 8192) + `"}`

	result := hookclient.Forward(hookclient.Config{
		SocketPath:    sink.socketPath,
		PendingDir:    newPendingDir(t),
		Stdin:         strings.NewReader(input),
		MaxInputBytes: limit,
	})
	if result.Outcome != hookclient.OutcomeSent {
		t.Fatalf("上限を超えた UserPromptSubmit も転送されるはずが %s でした: %v", result.Outcome, result.Err)
	}

	line := sink.waitForLines(t, 1)[0]
	var got struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSuffix(line, "\n")), &got); err != nil {
		t.Fatalf("受け口が受け取った行を JSON として読めません: %q: %v", line, err)
	}
	if !strings.Contains(got.Prompt, "<task-notification>") {
		t.Fatalf("prompt が落ちています（<task-notification> の判定ができません）: %s", line)
	}
}

// 目的: hook_event_name が極端に長くても、逃がし先へ書けることを確認する。
// 名前をそのままファイル名にすると os.OpenFile が ENAMETOOLONG で失敗し、
// その hook は socket にも逃がし先にも残らずに消える。
// 与える情報: 誰も listen していない socket のパスと、hook_event_name が 4096 文字の JSON。
// 成功条件: 終了の理由が spilled になり、逃がし先にファイルが1件でき、
// ファイル名の長さが 255 バイトに収まっていること。
func TestForward_イベント名が長くても逃がし先へ書ける(t *testing.T) {
	pendingDir := newPendingDir(t)
	dir, err := os.MkdirTemp("", "hooknoone")
	if err != nil {
		t.Fatalf("一時ディレクトリを作成できません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	input := `{"hook_event_name":"` + strings.Repeat("A", 4096) + `","session_id":"s1"}`
	result := hookclient.Forward(hookclient.Config{
		SocketPath: filepath.Join(dir, "nobody.sock"),
		PendingDir: pendingDir,
		Stdin:      strings.NewReader(input),
		Now:        fixedNow(1787057953362306),
	})
	if result.Outcome != hookclient.OutcomeSpilled {
		t.Fatalf("逃がし先へ書けるはずが %s でした: %v", result.Outcome, result.Err)
	}

	names := entryNames(t, pendingDir)
	if len(names) != 1 {
		t.Fatalf("逃がし先のファイルが1件ではありません: %v", names)
	}
	if len(names[0]) > 255 {
		t.Fatalf("逃がし先のファイル名が長すぎます（%d バイト）: %s", len(names[0]), names[0])
	}
}

// 目的: 逃がし先が上限まで太ったとき、量の多い PostToolUse は書かなくなるが、
// turn の終わりの判定に要る Stop は書き続けることを確認する。
// Stop まで落とすと、その run は claude.turn_timeout_ms（既定1時間）まで誰も気づかない（設計 3-19）。
// 与える情報: 逃がし先の上限を 1 件に縮めた Config（既に1件置いてある逃がし先）と、
// PostToolUse 1件・Stop 1件。
// 成功条件: PostToolUse が dropped になって理由が Result.Err に入り、Stop は spilled になること。
func TestForward_逃がし先が上限でもStopは書き続ける(t *testing.T) {
	pendingDir := newPendingDir(t)
	if err := os.MkdirAll(pendingDir, 0o700); err != nil {
		t.Fatalf("逃がし先を作れません: %v", err)
	}
	existing := filepath.Join(pendingDir, "1787057953362300-Stop.json")
	if err := os.WriteFile(existing, []byte(`{"hook_event_name":"Stop"}`), 0o600); err != nil {
		t.Fatalf("逃がし先へ既存のファイルを置けません: %v", err)
	}

	dir, err := os.MkdirTemp("", "hooknoone")
	if err != nil {
		t.Fatalf("一時ディレクトリを作成できません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "nobody.sock")

	base := hookclient.Config{
		SocketPath:      socketPath,
		PendingDir:      pendingDir,
		MaxPendingFiles: 1,
		Now:             fixedNow(1787057953362306),
	}

	noisy := base
	noisy.Stdin = strings.NewReader(`{"hook_event_name":"PostToolUse","session_id":"s1"}`)
	if got := hookclient.Forward(noisy); got.Outcome != hookclient.OutcomeDropped {
		t.Fatalf("上限を超えた逃がし先へ PostToolUse が書かれました: %s", got.Outcome)
	} else if got.Err == nil || !strings.Contains(got.Err.Error(), "上限") {
		t.Fatalf("上限で書かなかった理由が Result.Err に入っていません: %v", got.Err)
	}

	stop := base
	stop.Stdin = strings.NewReader(`{"hook_event_name":"Stop","session_id":"s1","background_tasks":[]}`)
	if got := hookclient.Forward(stop); got.Outcome != hookclient.OutcomeSpilled {
		t.Fatalf("上限を超えても Stop は書くはずが %s でした: %v", got.Outcome, got.Err)
	}
	if names := entryNames(t, pendingDir); len(names) != 2 {
		t.Fatalf("逃がし先のファイルが2件（既存 + Stop）ではありません: %v", names)
	}
}

// 目的: 逃がし先（--pending-dir）が相対パスのとき、そこへは書かないことを確認する。
// hook の cwd は worktree なので（設計 1-5）、相対パスだと逃がし先が worktree の中に掘られ、
// continuo は <実行時ディレクトリ>/issues/*/pending しか走査しないので永久に読まれない。
// 与える情報: 誰も listen していない socket のパスと、相対パスの逃がし先。
// 成功条件: 終了の理由が dropped になり、絶対パスでないことが Result.Err に入り、
// 相対パスのディレクトリが作られていないこと。
func TestForward_逃がし先が相対パスなら書かない(t *testing.T) {
	dir, err := os.MkdirTemp("", "hookrel")
	if err != nil {
		t.Fatalf("一時ディレクトリを作成できません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	// 相対パスが解決される先（＝いまの実行時ディレクトリ）を汚さないよう、
	// このテストの間だけ一時ディレクトリへ移る。
	before, err := os.Getwd()
	if err != nil {
		t.Fatalf("いまのディレクトリを取れません: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("一時ディレクトリへ移れません: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(before) })

	result := hookclient.Forward(hookclient.Config{
		SocketPath: filepath.Join(dir, "nobody.sock"),
		PendingDir: filepath.Join("pending", "relative"),
		Stdin:      strings.NewReader(`{"hook_event_name":"Stop","session_id":"s1"}`),
	})
	if result.Outcome != hookclient.OutcomeDropped {
		t.Fatalf("相対パスの逃がし先へ書かれました: %s", result.Outcome)
	}
	if result.Err == nil || !strings.Contains(result.Err.Error(), "絶対パス") {
		t.Fatalf("絶対パスでないことが Result.Err に入っていません: %v", result.Err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "pending")); !os.IsNotExist(err) {
		t.Fatalf("相対パスの逃がし先が作られました: %v", err)
	}
}
