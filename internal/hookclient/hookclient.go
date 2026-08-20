// Package hookclient は `continuo hook` の実体である（docs/plans/continuo_design.md 3-2 / 3-19）。
//
// やることは1つだけである。標準入力の JSON を1行にして hook 受け口の socket へ書き、
// 接続を閉じて終わる。応答は待たない（差し戻しを採らないので待つ必要が無い。設計 3-25）。
//
// 例外の扱いも設計で決まっている。
//
//	socket へ繋がらない        … 逃がし先へ書いて終わる（continuo が落ちていても
//	                             エージェントを止めない。設計 3-19）
//	標準入力が JSON でない     … どこにも書かずに終わる。逃がし先のファイル名に
//	                             hook_event_name が要るので、名前が決まらないためである
//
// **どの経路でも終了コードは 0 にする。**hook が失敗を返すと Claude Code の動きを
// 止めてしまうためである。呼び出し側（cmd/continuo）は Result を見てログや標準エラーに
// 出すだけで、終了コードを変えてはならない。
package hookclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 既定値である。呼び出し側は Config で上書きできる。
const (
	// DefaultDialTimeout は socket への接続を待つ上限である。
	// 相手（continuo）が居ないときは即座に失敗するので、これは「居るのに応答しない」
	// 場合の保険である。長く待つとエージェントの動きを止めるので短くする。
	DefaultDialTimeout = 2 * time.Second

	// DefaultWriteTimeout は socket へ1行を書き切るまでの上限である。
	DefaultWriteTimeout = 2 * time.Second

	// DefaultMaxInputBytes は標準入力から読み取る上限である。
	// PostToolUse の tool_response には大きな出力が入りうるので余裕を取る。
	//
	// メモリを守るための安全弁であって、**超えた hook を捨てるための線引きではない。**
	// 超えた場合は先頭からこのバイト数だけを読み、判定に要る項目を拾って組み立て直した
	// 1件として転送する（truncatedLine を参照）。受け口の上限
	// （hookserver.DefaultMaxMessageBytes）はこれより大きいので、必ず受け取れる。
	DefaultMaxInputBytes = 8 << 20

	// pendingFileExt は逃がし先の最終的な拡張子である。continuo（読む側）は
	// これに一致するものだけを走査する（internal/hookserver.PendingFileExt と同じ値）。
	pendingFileExt = ".json"
	// pendingTmpSuffix は書き込み中のファイルに付ける接尾辞である。
	// 書き切ってから os.Rename でこの接尾辞を外す（設計 3-19）。
	pendingTmpSuffix = ".tmp"
	// brokenDirName は読む側が壊れた JSON を隔離するディレクトリの名前である
	// （internal/hookserver.BrokenDirName と同じ値）。書く側はここへ書かないが、
	// 逃がし先の太り具合を数えるときに合算する。
	brokenDirName = "broken"

	// unknownEventName は hook_event_name が空だったときにファイル名へ使う語である。
	// JSON として解釈できている以上は捨てずに逃がすが、名前が無いと保存できないため。
	unknownEventName = "unknown"

	// maxSpillNameAttempts は逃がし先のファイル名がぶつかったときに、受信時刻を
	// 1マイクロ秒ずつずらして試す回数の上限である。同じマイクロ秒に同じイベントが
	// 複数届いても、上書きで失わないようにする。
	maxSpillNameAttempts = 1000

	// maxEventNameLen は逃がし先のファイル名に使う hook_event_name の最大バイト数である。
	//
	// hook_event_name は外から来る文字列で、標準入力の上限まで長くなりうる。長いまま
	// ファイル名にすると os.OpenFile が ENAMETOOLONG で失敗し、その hook は socket にも
	// 逃がし先にも残らずに消える。実測で出ている名前は最長でも SubagentStop の12バイトなので、
	// 64 バイトは実在するイベント名を1つも切らない。
	maxEventNameLen = 64

	// DefaultMaxPendingBytes は逃がし先に溜めてよい合計バイト数の目安である。
	//
	// PreToolUse / PostToolUse は matcher が "*" なのでツールを叩くたびに発火する。
	// continuo が落ちている間もエージェントは走り続けるので、上限が無いと逃がし先は
	// いくらでも太る。ディスクが埋まれば worktree の git 操作も continuo の再起動も巻き添えで失敗する。
	DefaultMaxPendingBytes int64 = 256 << 20

	// DefaultMaxPendingFiles は逃がし先に溜めてよいファイル数の目安である。
	DefaultMaxPendingFiles = 10000

	// pendingHardLimitFactor は「turn の終わりに要るイベントも書かなくなる」までの倍率である。
	//
	// 上限に達しても、いきなり全部を捨てない。turn の終わりの判定に要るイベント
	// （spillEssentialEvents）だけは書き続け、量の多い PreToolUse / PostToolUse から止める。
	// Stop は turn に1回しか出ないので、これで太り方は桁で落ちる。
	// それでもこの倍率まで太ったら、ディスクを守るためにすべて書かない。
	pendingHardLimitFactor = 4
)

// spillEssentialEvents は逃がし先が上限に達しても書き続けるイベント名である。
//
// **turn の終わりの通知を落とさないことを最優先にする。**落とすと、その run は
// claude.turn_timeout_ms（既定1時間）まで誰も気づかない（設計 3-19）。
// UserPromptSubmit を入れるのは、<task-notification> の判定に prompt が要るためである（設計 1-3）。
var spillEssentialEvents = map[string]bool{
	"Stop":             true,
	"SubagentStop":     true,
	"Notification":     true,
	"UserPromptSubmit": true,
}

// Outcome は `continuo hook` の1回の実行がどの経路で終わったかを表す。
type Outcome int

const (
	// OutcomeSent は socket へ転送できたことを表す。
	OutcomeSent Outcome = iota
	// OutcomeSpilled は socket へ繋がらず、逃がし先へ書いたことを表す。
	OutcomeSpilled
	// OutcomeDropped はどこにも書かずに捨てたことを表す。
	// 標準入力が JSON として解釈できなかった場合と、逃がし先へも書けなかった場合である。
	OutcomeDropped
)

// String は Outcome をログに出せる短い語にする。
func (o Outcome) String() string {
	switch o {
	case OutcomeSent:
		return "sent"
	case OutcomeSpilled:
		return "spilled"
	case OutcomeDropped:
		return "dropped"
	default:
		return fmt.Sprintf("unknown(%d)", int(o))
	}
}

// Config は `continuo hook` の1回の実行に必要な入力である。
type Config struct {
	// SocketPath は hook を受ける socket の絶対パスである（--socket）。
	SocketPath string
	// PendingDir は socket へ繋がらなかったときの逃がし先である（--pending-dir）。
	// <実行時ディレクトリ>/issues/<issue のスラグ>/pending を絶対パスで渡す。
	// 空文字なら逃がし先を持たない（socket へ繋がらなければ捨てる）。
	PendingDir string
	// Stdin は hook の JSON の入力元である。nil なら空の入力として扱う。
	Stdin io.Reader
	// Now は逃がし先のファイル名に使う受信時刻を返す。nil なら time.Now。
	Now func() time.Time
	// DialTimeout / WriteTimeout は 0 以下なら既定値を使う。
	DialTimeout  time.Duration
	WriteTimeout time.Duration
	// MaxInputBytes は標準入力から読む上限である。0 以下なら DefaultMaxInputBytes。
	MaxInputBytes int64
	// MaxPendingBytes は逃がし先に溜めてよい合計バイト数である。
	// 0 以下なら DefaultMaxPendingBytes。
	MaxPendingBytes int64
	// MaxPendingFiles は逃がし先に溜めてよいファイル数である。
	// 0 以下なら DefaultMaxPendingFiles。
	MaxPendingFiles int
}

// Result は Forward の結果である。呼び出し側はこれをログや標準エラーに出すだけで、
// 終了コードは常に 0 にする。
type Result struct {
	// Outcome はどの経路で終わったかである。
	Outcome Outcome
	// EventName は解釈できた hook_event_name である（解釈できなかった場合は空）。
	EventName string
	// PendingPath は逃がし先へ書いた場合の、書いたファイルの絶対パスである。
	PendingPath string
	// Truncated は標準入力が上限を超えたため、共通の項目だけを拾って組み立て直したことを表す。
	// **転送そのものは行っている**（捨てない）。呼び出し側は標準エラーへ1行出すこと。
	Truncated bool
	// Err は起きた不具合である。Outcome が OutcomeSent でも、
	// 「逃がし先へ書けなかった」以外の軽微な失敗が入ることがある。
	Err error
}

// Forward は標準入力の hook の JSON を socket へ転送する。転送できなければ逃がし先へ書く。
//
// cfg: 上記 Config。SocketPath は必須（空だと接続できず、逃がし先へ回る）。
// 戻り値: どの経路で終わったかと、起きた不具合。**エラーを返り値の error にしないのは、
// 呼び出し側が終了コードを変えてはならないためである**（設計 3-2。continuo が落ちていても
// エージェントを止めない）。
func Forward(cfg Config) Result {
	cfg = cfg.withDefaults()

	data, truncated, err := readInput(cfg)
	if err != nil {
		return Result{Outcome: OutcomeDropped, Err: err}
	}

	var (
		line      []byte
		eventName string
	)
	if truncated {
		// 上限を超えた入力でも捨てない。判定に要る項目だけを拾って組み立て直す
		// （04_hook.md の受け入れの基準「どのイベントも捨てずに HookSink.OnHook へ渡す」）。
		line, eventName, err = truncatedLine(data, cfg.MaxInputBytes)
	} else {
		line, eventName, err = compactLine(data)
	}
	if err != nil {
		// 逃がし先のファイル名には hook_event_name が要る。解釈できなければ
		// 名前が決まらないので、socket へも逃がし先へも書かずに捨てる（設計 3-19）。
		return Result{Outcome: OutcomeDropped, Err: err}
	}

	sendErr := sendToSocket(cfg, line)
	if sendErr == nil {
		return Result{Outcome: OutcomeSent, EventName: eventName, Truncated: truncated}
	}

	if cfg.PendingDir == "" {
		return Result{
			Outcome:   OutcomeDropped,
			EventName: eventName,
			Truncated: truncated,
			Err: fmt.Errorf(
				"socket へ転送できず、逃がし先（--pending-dir）も指定されていないので捨てました: %w",
				sendErr,
			),
		}
	}

	path, spillErr := spill(cfg, eventName, line)
	if spillErr != nil {
		return Result{
			Outcome:   OutcomeDropped,
			EventName: eventName,
			Truncated: truncated,
			Err:       fmt.Errorf("socket へ転送できず（%v）、逃がし先へも書けませんでした: %w", sendErr, spillErr),
		}
	}
	return Result{
		Outcome:     OutcomeSpilled,
		EventName:   eventName,
		PendingPath: path,
		Truncated:   truncated,
		Err:         sendErr,
	}
}

// withDefaults は 0 値のフィールドを既定値で埋めた写しを返す。
func (c Config) withDefaults() Config {
	if c.Stdin == nil {
		c.Stdin = strings.NewReader("")
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.DialTimeout <= 0 {
		c.DialTimeout = DefaultDialTimeout
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = DefaultWriteTimeout
	}
	if c.MaxInputBytes <= 0 {
		c.MaxInputBytes = DefaultMaxInputBytes
	}
	if c.MaxPendingBytes <= 0 {
		c.MaxPendingBytes = DefaultMaxPendingBytes
	}
	if c.MaxPendingFiles <= 0 {
		c.MaxPendingFiles = DefaultMaxPendingFiles
	}
	return c
}

// readInput は標準入力を上限つきで読み切る。
//
// 上限は「1回の hook でメモリを使い切らない」ための安全弁である。**超えても捨てない。**
// 超えた場合は先頭 MaxInputBytes バイトを返し、truncated を true にする。
// 呼び出し側は truncatedLine で判定に要る項目を拾い直す。
//
// cfg: 既定値を埋めた Config。
// 戻り値:
//   - 第1: 読み取ったバイト列（上限を超えた場合は先頭 MaxInputBytes バイト）
//   - 第2: 上限を超えたかどうか
//   - 第3: 標準入力の読み取りそのものに失敗した場合のエラー
func readInput(cfg Config) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(cfg.Stdin, cfg.MaxInputBytes+1))
	if err != nil {
		return nil, false, fmt.Errorf("標準入力を読めません: %w", err)
	}
	if int64(len(data)) > cfg.MaxInputBytes {
		return data[:cfg.MaxInputBytes], true, nil
	}
	return data, false, nil
}

// truncatedStringFields は上限を超えた入力から拾い直す、値が文字列の項目である。
//
// **どのイベントでも JSON の先頭側に並ぶ。**
// docs/evidence/hooks_probe_20260817.jsonl の実測では、7種すべてで
// session_id / transcript_path / cwd / prompt_id / hook_event_name が
// tool_input・tool_response より前にある。大きくなるのは後者だけなので、
// 先頭 MaxInputBytes バイトの中にこれらは収まる。
var truncatedStringFields = map[string]bool{
	"hook_event_name":   true,
	"session_id":        true,
	"transcript_path":   true,
	"cwd":               true,
	"prompt_id":         true,
	"notification_type": true,
	"agent_id":          true,
	"agent_type":        true,
}

// truncatedRawFields は上限を超えた入力から、**値の形を変えずに**拾い直す項目である。
//
// **turn の終わりの判定に要るのはこちらである。**文字列の項目だけを拾い直すと、
// 上限を超えた Stop は background_tasks が欠けた形で届く。受け取る側の判定は
// 「欠けている（判定不能）」と「空配列（settle_ms 待って turn の終わりとする）」を
// 別扱いにしているので（設計 3-2。hookserver.HookEvent.BackgroundTasks の GoDoc）、
// 欠けたまま届いた Stop は turn の終わりとして扱われない。
//
// encoding/json の Decoder は「値が完全に読めたとき」だけ成功するので、
// 拾えた時点でその値は元のままである。そのまま詰め直せばよい。
var truncatedRawFields = map[string]bool{
	"background_tasks": true,
	"stop_hook_active": true,
	"prompt":           true,
}

// truncatedLine は上限を超えた hook の入力から、判定に要る項目だけを拾って1行の JSON を作る。
//
// 上限を超えた入力はそのままでは JSON として閉じていないので解釈できない。だが巨大になるのは
// PostToolUse の tool_response のような後ろ側の項目であり、turn の終わりの判定に使う項目
// （truncatedStringFields と truncatedRawFields）は先頭側にある。encoding/json の Decoder で
// トップレベルのオブジェクトを先頭から読み進め、拾えたものだけを詰め直す。**捨てないためである**
// （04_hook.md の受け入れの基準「どのイベントも捨てずに HookSink.OnHook へ渡す」）。
//
// 組み立てた JSON には continuo_truncated と continuo_truncated_limit_bytes を入れ、
// 中身が欠けていることを orchestrator へ伝える（hookserver.HookEvent が読む）。
//
// data: 先頭 limit バイトぶんの入力。
// limit: 超えた上限（バイト）。
// 戻り値:
//   - 第1: 末尾に改行を付けた1行分のバイト列
//   - 第2: hook_event_name（拾えなければ空文字）
//   - 第3: トップレベルが JSON のオブジェクトでない場合と、1つも項目を拾えなかった場合のエラー
func truncatedLine(data []byte, limit int64) ([]byte, string, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, "", fmt.Errorf(
			"標準入力が上限（%d バイト）を超えており、先頭も hook の JSON として読めませんでした: %w", limit, err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, "", fmt.Errorf(
			"標準入力が上限（%d バイト）を超えており、しかも JSON のオブジェクトで始まっていません", limit)
	}

	picked := map[string]any{}
	eventName := ""
	// 値が途中で切れているところで Decode が失敗する。そこまでに拾えた分を使う。
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			break
		}
		key, ok := keyTok.(string)
		if !ok {
			break
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			break
		}
		switch {
		case truncatedStringFields[key]:
			var v string
			if err := json.Unmarshal(raw, &v); err != nil {
				continue
			}
			picked[key] = v
			if key == "hook_event_name" {
				eventName = v
			}
		case truncatedRawFields[key]:
			// json.RawMessage は Marshal で元のバイト列がそのまま出る。
			// 中身を作り直さないので、background_tasks の要素の項目も欠けない。
			picked[key] = raw
		}
	}

	if len(picked) == 0 {
		return nil, "", fmt.Errorf(
			"標準入力が上限（%d バイト）を超えており、先頭からは hook の項目を1つも拾えませんでした", limit)
	}

	out := make(map[string]any, len(picked)+2)
	for k, v := range picked {
		out[k] = v
	}
	out["continuo_truncated"] = true
	out["continuo_truncated_limit_bytes"] = limit

	// encoding/json はキーを名前の昇順に並べるので、出来上がる1行は毎回同じ形になる。
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, "", fmt.Errorf("上限を超えた hook の項目を1行の JSON に組み立てられません: %w", err)
	}
	return append(encoded, '\n'), eventName, nil
}

// compactLine は hook の JSON を1行へ詰め、hook_event_name を取り出す。
//
// data: 標準入力から読んだバイト列。
// 戻り値:
//   - 第1: 末尾に改行を付けた1行分のバイト列（socket へ書く形。逃がし先へ書くときは改行を外す）
//   - 第2: hook_event_name（空文字のこともある）
//   - 第3: JSON のオブジェクトとして解釈できなかった場合のエラー
func compactLine(data []byte) ([]byte, string, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, "", fmt.Errorf("標準入力を hook の JSON として解釈できません: %w", err)
	}
	if probe == nil {
		return nil, "", errors.New("標準入力が JSON のオブジェクトではありません（null でした）")
	}

	var named struct {
		HookEventName string `json:"hook_event_name"`
	}
	// probe が通っている以上ここは失敗しないが、hook_event_name が文字列でない場合に
	// 落ちないよう、失敗しても空の名前として扱う。
	_ = json.Unmarshal(data, &named)

	// 改行区切りの約束（設計 3-2）を守るため、整形された JSON が来ても1行に詰める。
	compact, err := jsonCompact(data)
	if err != nil {
		return nil, "", fmt.Errorf("標準入力の JSON を1行に詰められません: %w", err)
	}
	return append(compact, '\n'), named.HookEventName, nil
}

// jsonCompact は JSON から改行と余分な空白を取り除く。
//
// encoding/json の Compact を使うので、キーの並び順も値の表記も元のまま残る
// （中身は hook の JSON をそのまま入れる、という設計 3-19 の約束を守るため）。
//
// data: 解釈済みの JSON のバイト列。
// 戻り値: 1行に詰めたバイト列と、詰められなかった場合のエラー。
func jsonCompact(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// sendToSocket は1行を socket へ書いて閉じる。応答は読まない（設計 3-2）。
//
// cfg: 既定値を埋めた Config。
// line: 末尾に改行を含む1行分のバイト列。
// 戻り値: 接続または書き込みに失敗した場合のエラー。
func sendToSocket(cfg Config, line []byte) error {
	if cfg.SocketPath == "" {
		return errors.New("hook を受ける socket のパス（--socket）が指定されていません")
	}
	conn, err := net.DialTimeout("unix", cfg.SocketPath, cfg.DialTimeout)
	if err != nil {
		return fmt.Errorf("hook を受ける socket へ接続できません: %s: %w", cfg.SocketPath, err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetWriteDeadline(time.Now().Add(cfg.WriteTimeout)); err != nil {
		return fmt.Errorf("hook を受ける socket に書き込みの期限を設定できません: %w", err)
	}
	if _, err := conn.Write(line); err != nil {
		return fmt.Errorf("hook を受ける socket へ書き込めません: %s: %w", cfg.SocketPath, err)
	}
	return nil
}

// spill は socket へ転送できなかった hook を逃がし先へ書く（設計 3-19）。
//
// 書き方は「同じディレクトリに .json.tmp を付けた名前で書き切り、os.Rename で
// 最終の名前に変える」である。os.Rename は同じファイルシステム内で不可分なので、
// .json という名前で見えた時点で中身は必ず完全である。読む側が書き込みの途中を
// 「壊れた JSON」と判定して隔離し、Stop を1件失うのを防ぐ。
//
// cfg: 既定値を埋めた Config。
// eventName: hook_event_name（ファイル名に使う。空なら unknown）。
// line: 末尾に改行を含む1行分のバイト列。ファイルには改行を外して書く
// （中身は hook の JSON をそのまま入れる。封筒を付けない）。
// 戻り値: 書いたファイルの絶対パスと、書けなかった場合のエラー。
func spill(cfg Config, eventName string, line []byte) (string, error) {
	// **相対パスでは書かない。**hook の cwd は worktree なので（設計 1-5）、相対パスだと
	// 逃がし先が worktree の中に掘られる。continuo は <実行時ディレクトリ>/issues/*/pending しか
	// 走査しないので、そこへ書いたものは永久に読まれない（worktree も汚れる）。
	// 受け口の側（hookserver.New）も同じ理由で絶対パスを要求している。
	if !filepath.IsAbs(cfg.PendingDir) {
		return "", fmt.Errorf(
			"hook の逃がし先（--pending-dir）%q が絶対パスではありません"+
				"（hook の cwd は worktree なので、相対パスでは worktree の中に書いてしまい、continuo は読めません）",
			cfg.PendingDir,
		)
	}
	if err := os.MkdirAll(cfg.PendingDir, 0o700); err != nil {
		return "", fmt.Errorf("hook の逃がし先を作れません: %s: %w", cfg.PendingDir, err)
	}
	if err := checkPendingCapacity(cfg, eventName); err != nil {
		return "", err
	}

	payload := []byte(strings.TrimSuffix(string(line), "\n"))
	safeName := sanitizeEventName(eventName)
	micros := cfg.Now().UnixMicro()

	for i := 0; i < maxSpillNameAttempts; i++ {
		base := fmt.Sprintf("%d-%s", micros+int64(i), safeName)
		finalPath := filepath.Join(cfg.PendingDir, base+pendingFileExt)
		tmpPath := finalPath + pendingTmpSuffix

		// 同じマイクロ秒に同じイベントが複数届いても上書きしない。
		if _, err := os.Lstat(finalPath); err == nil {
			continue
		}
		f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("hook の逃がし先のファイルを作れません: %s: %w", tmpPath, err)
		}

		if err := writeAndSync(f, payload); err != nil {
			_ = os.Remove(tmpPath)
			return "", fmt.Errorf("hook の逃がし先のファイルへ書けません: %s: %w", tmpPath, err)
		}
		if err := os.Rename(tmpPath, finalPath); err != nil {
			_ = os.Remove(tmpPath)
			return "", fmt.Errorf("hook の逃がし先のファイル名を確定できません: %s: %w", tmpPath, err)
		}
		return finalPath, nil
	}
	return "", fmt.Errorf(
		"hook の逃がし先のファイル名が %d 回続けてぶつかりました: %s",
		maxSpillNameAttempts, cfg.PendingDir,
	)
}

// checkPendingCapacity は逃がし先が太りすぎていないかを確かめる。
//
// 数えるのは逃がし先の直下と、その下の隔離先（pending/broken/）である。隔離先を数に入れるのは、
// 設計 3-19 が「壊れた JSON は消さずに残す」と決めていて自動では減らないためである
// （消す代わりに、溜まったら書き込みを止めて人間に気づかせる）。
//
// 上限に達したときも、いきなり全部は止めない。turn の終わりの判定に要るイベント
// （spillEssentialEvents）は書き続け、量の多い PreToolUse / PostToolUse から止める。
// pendingHardLimitFactor 倍まで太ったら、ディスクを守るためにすべて止める。
//
// cfg: 既定値を埋めた Config。
// eventName: これから書こうとしている hook のイベント名。
// 戻り値: 書いてはいけない場合のエラー（呼び出し側はこれを Result.Err に入れる）。
// 数えられなかった場合は nil を返す（数えられないことを理由に hook を落とさない）。
func checkPendingCapacity(cfg Config, eventName string) error {
	total, count, ok := measurePendingDir(cfg.PendingDir)
	if !ok {
		return nil
	}
	if total < cfg.MaxPendingBytes && count < cfg.MaxPendingFiles {
		return nil
	}

	hardBytes := cfg.MaxPendingBytes * pendingHardLimitFactor
	hardFiles := cfg.MaxPendingFiles * pendingHardLimitFactor
	if total < hardBytes && count < hardFiles && spillEssentialEvents[eventName] {
		return nil
	}
	return fmt.Errorf(
		"hook の逃がし先が上限に達しています（%d バイト / %d 件。上限 %d バイト / %d 件）: %s",
		total, count, cfg.MaxPendingBytes, cfg.MaxPendingFiles, cfg.PendingDir,
	)
}

// measurePendingDir は逃がし先に溜まっている合計バイト数とファイル数を数える。
//
// dir: 逃がし先のディレクトリ。
// 戻り値: 合計バイト数、ファイル数、数えられたかどうか。
func measurePendingDir(dir string) (int64, int, bool) {
	var (
		total int64
		count int
	)
	for _, d := range []string{dir, filepath.Join(dir, brokenDirName)} {
		entries, err := os.ReadDir(d)
		if err != nil {
			// 逃がし先そのものを読めないときだけ「数えられなかった」とする。
			// 隔離先はまだ無いのが普通なので、無いことを失敗にしない。
			if d == dir {
				return 0, 0, false
			}
			continue
		}
		for _, e := range entries {
			if !e.Type().IsRegular() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			total += info.Size()
			count++
		}
	}
	return total, count, true
}

// writeAndSync は中身を書き切り、ディスクへ流し込んでから閉じる。
//
// f: 書き込み先の（作成済みの）ファイル。この関数が閉じる。
// payload: 書く中身。
// 戻り値: 書き込み・同期・クローズのいずれかに失敗した場合のエラー。
func writeAndSync(f *os.File, payload []byte) error {
	if _, err := f.Write(payload); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// sanitizeEventName は hook_event_name をファイル名に使える形にする。
//
// hook_event_name は Claude Code が入れる値だが、そのままファイル名にすると
// スラッシュや ".." が入ったときに逃がし先の外へ書けてしまう。英数字・アンダースコア・
// ハイフン以外はすべて "_" に置き換える。
//
// **長さも maxEventNameLen で切る。**hook_event_name は標準入力の上限まで長くなりうるので、
// 切らないと os.OpenFile が ENAMETOOLONG で失敗し、その hook が socket にも逃がし先にも
// 残らずに消える。置き換え先はすべて1バイトの文字なので、途中で切っても壊れない。
//
// name: hook_event_name の値。
// 戻り値: ファイル名に使える文字列。空になる場合は "unknown" を返す。
func sanitizeEventName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if b.Len() >= maxEventNameLen {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return unknownEventName
	}
	return b.String()
}
