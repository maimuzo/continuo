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

	// unknownEventName は hook_event_name が空だったときにファイル名へ使う語である。
	// JSON として解釈できている以上は捨てずに逃がすが、名前が無いと保存できないため。
	unknownEventName = "unknown"

	// maxSpillNameAttempts は逃がし先のファイル名がぶつかったときに、受信時刻を
	// 1マイクロ秒ずつずらして試す回数の上限である。同じマイクロ秒に同じイベントが
	// 複数届いても、上書きで失わないようにする。
	maxSpillNameAttempts = 1000
)

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

// truncatedFields は上限を超えた入力から拾い直す項目である。
//
// **どれも値が文字列であり、どのイベントでも JSON の先頭側に並ぶ。**
// docs/evidence/hooks_probe_20260817.jsonl の実測では、7種すべてで
// session_id / transcript_path / cwd / prompt_id / hook_event_name が
// tool_input・tool_response より前にある。大きくなるのは後者だけなので、
// 先頭 MaxInputBytes バイトの中にこれらは収まる。
var truncatedFields = map[string]bool{
	"hook_event_name":   true,
	"session_id":        true,
	"transcript_path":   true,
	"cwd":               true,
	"prompt_id":         true,
	"notification_type": true,
	"agent_id":          true,
	"agent_type":        true,
}

// truncatedLine は上限を超えた hook の入力から、判定に要る項目だけを拾って1行の JSON を作る。
//
// 上限を超えた入力はそのままでは JSON として閉じていないので解釈できない。だが巨大になるのは
// PostToolUse の tool_response のような後ろ側の項目であり、turn の終わりの判定に使う項目
// （truncatedFields）は先頭側にある。encoding/json の Decoder でトップレベルのオブジェクトを
// 先頭から読み進め、拾えたものだけを詰め直す。**捨てないためである**
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

	picked := map[string]string{}
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
		if !truncatedFields[key] {
			continue
		}
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			continue
		}
		picked[key] = v
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
	return append(encoded, '\n'), picked["hook_event_name"], nil
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
	if err := os.MkdirAll(cfg.PendingDir, 0o700); err != nil {
		return "", fmt.Errorf("hook の逃がし先を作れません: %s: %w", cfg.PendingDir, err)
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
// name: hook_event_name の値。
// 戻り値: ファイル名に使える文字列。空になる場合は "unknown" を返す。
func sanitizeEventName(name string) string {
	var b strings.Builder
	for _, r := range name {
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
