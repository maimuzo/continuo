package orchestrator

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

// transcriptMaxLineBytes は transcript の1行として読み切る上限である。
// 1行が API 応答1件ぶんの JSON なので、大きめに取る。
//
// **超えた行は読み捨てて、残りの行の処理を続ける。**大きなファイルを読ませた turn の
// tool 結果が1行に入ると超えうるが、その1行のために turn 全体の表明を落としてはならない。
const transcriptMaxLineBytes = 16 << 20

// transcriptMaxRequestIDs はトークンの重複排除に覚えておく `requestId` の件数の上限である。
//
// **上限を置く理由。**transcript のファイルサイズには上限が無い（Claude Code が書く）。
// 件数ぶんの文字列を無条件に覚えると、長く走ったセッションや細工されたファイルで
// メモリが際限なく増える。**上限に達したら以後は重複排除をやめる**（件数が多少多く
// 出ることはあっても、ダッシュボードに出すだけの値であり判断には使わない。設計 3-15）。
const transcriptMaxRequestIDs = 200000

// transcriptLine は transcript の JSONL の1行である（設計 3-15 / 3-25）。
//
// **未知のキーは無視する。**Claude Code 側で項目が増えても落ちない。
type transcriptLine struct {
	// Type は行の種別である（"user" / "assistant" ほか）。
	Type string `json:"type"`
	// IsSidechain は subagent の会話であることを表す。
	// **表明は `false` に絞って拾う**（subagent の発言を印として拾わないため。設計 3-25）。
	IsSidechain bool `json:"isSidechain"`
	// PromptSource は user 行の出どころである。
	// **`"typed"` が turn の頭である**（設計 3-25）。`prompt_id` では区切らない。
	PromptSource string `json:"promptSource"`
	// PromptID は user 行の prompt_id である。Stop hook の prompt_id との照合に使う。
	PromptID string `json:"promptId"`
	// RequestID は API 応答の識別子である。**トークンの重複排除に使う**（設計 3-15）。
	RequestID string `json:"requestId"`
	// Message は assistant / user のメッセージ本体である。
	Message *transcriptMessage `json:"message"`
}

// transcriptMessage は transcript の行のメッセージ本体である。
type transcriptMessage struct {
	// Content はメッセージのブロックの並びである。
	// **文字列で入ることもある**ので json.RawMessage で受けて後から解く。
	Content json.RawMessage `json:"content"`
	// Usage は API 応答ごとのトークンの計上である（設計 3-15）。
	Usage *transcriptUsage `json:"usage"`
}

// transcriptContentBlock はメッセージのブロック1件である。
type transcriptContentBlock struct {
	// Type はブロックの種別である。表明を拾うのは "text" だけである。
	Type string `json:"type"`
	// Text は本文である。
	Text string `json:"text"`
}

// transcriptUsage は1回の API 応答のトークンの計上である（設計 3-15）。
type transcriptUsage struct {
	// InputTokens は入力のトークン数である。
	InputTokens int `json:"input_tokens"`
	// CacheCreationInputTokens はキャッシュ作成のトークン数である。
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	// CacheReadInputTokens はキャッシュ読み出しのトークン数である。
	CacheReadInputTokens int `json:"cache_read_input_tokens"`
	// OutputTokens は出力のトークン数である。
	OutputTokens int `json:"output_tokens"`
}

// TokenUsage は transcript から集計したトークンの合計である（設計 3-15）。
//
// **1回の集計が見ているのは transcript 1ファイルである。**セッションをまたいだ累計は
// `runState` が `Add` で足して持つ（transcript のファイル名はセッション UUID なので、
// 再 dispatch で UUID を採り直すと別のファイルになる）。
type TokenUsage struct {
	// APICalls は数えた API 応答の件数（`requestId` で重複排除したあと）である。
	APICalls int
	// Input は入力のトークンの合計である。
	Input int
	// CacheCreation はキャッシュ作成のトークンの合計である。
	CacheCreation int
	// CacheRead はキャッシュ読み出しのトークンの合計である。
	CacheRead int
	// Output は出力のトークンの合計である。
	Output int
}

// Add は2つの集計を足した新しい値を返す（レシーバは書き換えない）。
//
// **セッションをまたいだ累計を作るためのものである。**同じ transcript を2回足しては
// ならない（`requestId` の重複排除はファイル単位でしか効かない）。
//
// other: 足す集計。
// 戻り値: 項目ごとに足し合わせた集計。
func (u TokenUsage) Add(other TokenUsage) TokenUsage {
	return TokenUsage{
		APICalls:      u.APICalls + other.APICalls,
		Input:         u.Input + other.Input,
		CacheCreation: u.CacheCreation + other.CacheCreation,
		CacheRead:     u.CacheRead + other.CacheRead,
		Output:        u.Output + other.Output,
	}
}

// TranscriptReadResult は transcript を1回読んだ結果である。
//
// **表明とトークンの両方を、同じ1回の読み取りで取る**（設計 3-15。2回開かない）。
type TranscriptReadResult struct {
	// Signals は拾った表明である。キーは対象の issue の識別子
	// （対象を書かない行は空文字のキーになる）、値は `review` / `blocked` / `working` など。
	// **issue ごとに、最後に現れた1行を採る**（設計 3-25）。
	Signals map[string]string
	// Usage はトークンの合計である。
	Usage TokenUsage
}

// ReadTranscript は transcript の JSONL を読み、表明とトークンの両方を取る
// （設計 3-15 / 3-25）。
//
// 表明の拾い方は設計 3-25 のとおりである。
//
//  1. `type == "user"` かつ `promptId` が一致する行を探す（promptID が空なら飛ばす）
//  2. そこから前へ遡り、`type == "user"` かつ `promptSource == "typed"` の最初の行を見つける
//  3. 頭から後ろへ、次の "typed" の手前までをこの turn の範囲とする
//  4. 範囲内の `type == "assistant"` かつ `isSidechain == false` の行から
//     `message.content[]` の `type == "text"` を集める
//  5. 集めた text を**行に割って** prefix の行を拾う（ブロックの一致では取れない）
//
// **`prompt_id` で区切ってはならない**（17件中3件で取り逃した）。1つの人間の指示が
// transcript の中で複数の `prompt_id` に割れる。**`promptSource == "typed"` を起点に
// すれば 17件中17件で取れた。**
//
// トークンは設計 3-15 のとおり、**ファイル全体**の `type == "assistant"` の行を
// `requestId` で重複排除してから足す。
//
// **全行をメモリに載せない。**2回走査する形にしてある。
//
//	1回目  … 行の位置だけを覚えながら turn の範囲（バイト位置）を決め、トークンを足す
//	2回目  … 範囲の中の assistant 行だけを解いて text を集める
//
// 覚えるのは "typed" の user 行のバイト位置（turn ごとに1件）だけなので、**ファイルが
// どれだけ大きくてもメモリはほぼ増えない。**turn の終わりごとに最大6回読み直す経路
// （`readSignals`）があるため、ここでファイル全体を抱えると同時実行数ぶん掛け算になる。
//
// **開くのは通常のファイルだけである。**FIFO を渡されると `os.Open` が書き手の現れるまで
// 永久に返らず、turn ループの goroutine ごと固まる（設計 3-2 の hook の値は外部入力である）。
//
// path: transcript の JSONL のパス。
// promptID: Stop hook が渡した prompt_id。空なら最後の "typed" 起点の範囲を使う。
// prefix: 表明の印（`tracker.status_signal_prefix`）。
// currentIdentifier: いま作業している issue の識別子（対象を書かない行の対象になる）。
// 戻り値の1つ目: 読み取った結果。
// 戻り値の2つ目: ファイルを開けない・通常のファイルでない・読めない場合のエラー。
// **行の JSON が壊れていてもエラーにしない**（読める行だけを使う）。
// **長すぎる行も読み捨てて続ける。**
func ReadTranscript(path, promptID, prefix, currentIdentifier string) (*TranscriptReadResult, error) {
	f, err := openRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	scan, err := scanTranscript(f, promptID)
	if err != nil {
		return nil, fmt.Errorf("transcript を読めません: %s: %w", path, err)
	}

	texts, err := collectTurnTexts(f, scan.start, scan.end)
	if err != nil {
		return nil, fmt.Errorf("transcript を読めません: %s: %w", path, err)
	}

	return &TranscriptReadResult{
		Signals: ParseSignals(texts, prefix, currentIdentifier),
		Usage:   scan.usage,
	}, nil
}

// openRegularFile は通常のファイルだけを開く。
//
// **`O_NONBLOCK` を付けて開き、開いたあとに種別を確かめる。**先に `os.Lstat` で見るだけ
// では、見てから開くまでの間に差し替えられる。FIFO は `O_NONBLOCK` があれば即座に返り、
// そのあとの検査で弾ける。**通常のファイルには `O_NONBLOCK` は影響しない。**
//
// path: 開くパス。
// 戻り値の1つ目: 開いたファイル。
// 戻り値の2つ目: 開けない・通常のファイルでない場合のエラー。
func openRegularFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("transcript を開けません: %s: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("transcript の種別を読めません: %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("transcript が通常のファイルではありません: %s: mode=%s", path, info.Mode())
	}
	return f, nil
}

// transcriptScan は1回目の走査の結果である。
type transcriptScan struct {
	// start は turn の範囲の開始位置（バイト。含む）である。
	start int64
	// end は turn の範囲の終了位置（バイト。含まない）である。
	end int64
	// usage はファイル全体のトークンの集計である。
	usage TokenUsage
}

// scanTranscript は1回目の走査を行い、turn の範囲とトークンの集計を求める。
//
// **覚えるのは "typed" の user 行のバイト位置だけである**（turn ごとに1件）。
//
// f: 読むファイル（先頭から読み直す）。
// promptID: Stop hook が渡した prompt_id。空なら最後の行を起点にする。
// 戻り値の1つ目: 範囲とトークン。
// 戻り値の2つ目: 読めない場合のエラー。
func scanTranscript(f *os.File, promptID string) (transcriptScan, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return transcriptScan{}, err
	}
	r := bufio.NewReaderSize(f, 64*1024)

	var (
		out          transcriptScan
		typedOffsets []int64
		anchor       int64 = -1
		lastOffset   int64 = -1
		offset       int64
		seen         = map[string]bool{}
	)

	for {
		raw, consumed, truncated, err := readTranscriptLine(r)
		lineStart := offset
		offset += consumed
		if len(raw) > 0 && !truncated {
			var line transcriptLine
			// 壊れた行は飛ばす。1行の障害で残り全部を落とさない。
			if json.Unmarshal(raw, &line) == nil {
				lastOffset = lineStart
				if isTypedUser(line) {
					typedOffsets = append(typedOffsets, lineStart)
				}
				if promptID != "" && line.Type == "user" && line.PromptID == promptID {
					anchor = lineStart
				}
				addUsage(&out.usage, line, seen)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return transcriptScan{}, err
		}
	}

	out.end = offset
	if anchor < 0 {
		anchor = lastOffset
	}
	if anchor < 0 {
		// 読める行が1つも無い。
		return transcriptScan{start: 0, end: 0, usage: out.usage}, nil
	}

	// 段3: anchor から前へ遡り、最初の "typed" の user 行を turn の頭とする。
	out.start = 0
	for i := len(typedOffsets) - 1; i >= 0; i-- {
		if typedOffsets[i] <= anchor {
			out.start = typedOffsets[i]
			break
		}
	}
	// 段4: 頭から後ろへ、次の "typed" の手前までをこの turn の範囲とする。
	for _, off := range typedOffsets {
		if off > out.start {
			out.end = off
			break
		}
	}
	return out, nil
}

// addUsage は1行ぶんのトークンを集計へ足す（設計 3-15）。
//
// **`requestId` で必ず重複排除する。**assistant の行が API 呼び出しと1対1である保証は
// 取れていない。重複排除しておけば、どちらでも正しい値になる。
//
// usage: 足し込む先。
// line: 1行。
// seen: これまでに数えた `requestId`（`transcriptMaxRequestIDs` を上限に覚える）。
func addUsage(usage *TokenUsage, line transcriptLine, seen map[string]bool) {
	if line.Type != "assistant" || line.Message == nil || line.Message.Usage == nil {
		return
	}
	if line.RequestID != "" {
		if seen[line.RequestID] {
			return
		}
		if len(seen) < transcriptMaxRequestIDs {
			seen[line.RequestID] = true
		}
	}
	u := line.Message.Usage
	usage.APICalls++
	usage.Input += u.InputTokens
	usage.CacheCreation += u.CacheCreationInputTokens
	usage.CacheRead += u.CacheReadInputTokens
	usage.Output += u.OutputTokens
}

// collectTurnTexts は2回目の走査で、turn の範囲の assistant の text を集める
// （設計 3-25 の段5）。
//
// **`isSidechain == false` に絞る。**subagent の発言を印として拾わないためである。
//
// f: 読むファイル。
// start: 範囲の開始位置（バイト。含む）。
// end: 範囲の終了位置（バイト。含まない）。
// 戻り値の1つ目: 集めた text の並び。
// 戻り値の2つ目: 読めない場合のエラー。
func collectTurnTexts(f *os.File, start, end int64) ([]string, error) {
	if end <= start {
		return nil, nil
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	r := bufio.NewReaderSize(f, 64*1024)

	var texts []string
	offset := start
	for offset < end {
		raw, consumed, truncated, err := readTranscriptLine(r)
		offset += consumed
		if len(raw) > 0 && !truncated {
			var line transcriptLine
			if json.Unmarshal(raw, &line) == nil &&
				line.Type == "assistant" && !line.IsSidechain && line.Message != nil {
				texts = append(texts, contentTexts(line.Message.Content)...)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
	}
	return texts, nil
}

// readTranscriptLine は JSONL の1行を読む。
//
// **`bufio.Scanner` を使わない。**上限を超えた行で `bufio.ErrTooLong` を返して以降を
// 読めなくなるため、**長すぎる行はその1行だけを読み捨てて続ける。**
//
// r: 読み元。
// 戻り値の1つ目: 行の中身（改行を含まない）。読み捨てた場合は nil。
// 戻り値の2つ目: 消費したバイト数（改行を含む）。**呼び出し側の位置の計算に使う。**
// 戻り値の3つ目: 上限を超えて読み捨てたなら true。
// 戻り値の4つ目: ファイルの終わりなら io.EOF。それ以外は読み取りの失敗。
func readTranscriptLine(r *bufio.Reader) ([]byte, int64, bool, error) {
	var (
		line      []byte
		consumed  int64
		truncated bool
	)
	for {
		chunk, err := r.ReadSlice('\n')
		consumed += int64(len(chunk))
		if !truncated {
			if len(line)+len(chunk) > transcriptMaxLineBytes {
				truncated = true
				line = nil
			} else {
				line = append(line, chunk...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			return trimLineEnd(line), consumed, truncated, err
		}
		return trimLineEnd(line), consumed, truncated, nil
	}
}

// trimLineEnd は行末の改行を落とす。
//
// line: 1行。
// 戻り値: 前後の空白と改行を落とした行。空白だけの行は nil を返す。
func trimLineEnd(line []byte) []byte {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil
	}
	return trimmed
}

// isTypedUser は「turn の頭」の行かを判定する（設計 3-25）。
//
// **`promptSource` は `"typed"` に一致するかだけを見る。**観測できた値は
// `typed` / `system` / 項目なし の3通りだが、他の値が出ないことは確認できていない。
// **一致しない値はすべて「turn の頭ではない」として扱う。**
//
// line: 判定する行。
// 戻り値: turn の頭なら true。
func isTypedUser(line transcriptLine) bool {
	return line.Type == "user" && line.PromptSource == "typed"
}

// contentTexts は message.content から text ブロックの本文を取り出す。
//
// content は配列のことも文字列のこともあるので、両方を受ける。
//
// content: message.content の生の JSON。
// 戻り値: 取り出した本文の並び。
func contentTexts(content json.RawMessage) []string {
	if len(content) == 0 {
		return nil
	}
	var blocks []transcriptContentBlock
	if err := json.Unmarshal(content, &blocks); err == nil {
		var out []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				out = append(out, b.Text)
			}
		}
		return out
	}
	var s string
	if err := json.Unmarshal(content, &s); err == nil && s != "" {
		return []string{s}
	}
	return nil
}
