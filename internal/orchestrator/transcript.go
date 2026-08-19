package orchestrator

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// transcriptMaxLineBytes は transcript の1行として読み切る上限である。
// 1行が API 応答1件ぶんの JSON なので、大きめに取る。
const transcriptMaxLineBytes = 16 << 20

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

// ReadTranscript は transcript の JSONL を1回読み、表明とトークンの両方を取る
// （設計 3-15 / 3-25）。
//
// 表明の拾い方は設計 3-25 のとおりである。
//
//  1. `type == "user"` かつ `promptId` が一致する行を探す（promptID が空なら飛ばす）
//  2. そこから前へ遡り、`type == "user"` かつ `promptSource == "typed"` の最初の行を見つける
//  3. 頭から後ろへ、次の "typed" の手前までをこの turn の範囲とする
//  4. 範囲内の `type == "assistant"` かつ `isSidechain == false` の行から
//     `message.content[]` の `type == "text"` を集める
//  5. 集めた text を**行に割って** prefix を含む行を拾う（ブロックの一致では取れない）
//
// **`prompt_id` で区切ってはならない**（17件中3件で取り逃した）。1つの人間の指示が
// transcript の中で複数の `prompt_id` に割れる。**`promptSource == "typed"` を起点に
// すれば 17件中17件で取れた。**
//
// トークンは設計 3-15 のとおり、**ファイル全体**の `type == "assistant"` の行を
// `requestId` で重複排除してから足す。
//
// path: transcript の JSONL のパス。
// promptID: Stop hook が渡した prompt_id。空なら最後の "typed" 起点の範囲を使う。
// prefix: 表明の印（`tracker.status_signal_prefix`）。
// currentIdentifier: いま作業している issue の識別子（対象を書かない行の対象になる）。
// 戻り値の1つ目: 読み取った結果。
// 戻り値の2つ目: ファイルを開けない・読めない場合のエラー。
// **行の JSON が壊れていてもエラーにしない**（読める行だけを使う）。
func ReadTranscript(path, promptID, prefix, currentIdentifier string) (*TranscriptReadResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("transcript を開けません: %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), transcriptMaxLineBytes)

	var lines []transcriptLine
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}
		var line transcriptLine
		if err := json.Unmarshal(raw, &line); err != nil {
			// 壊れた行は飛ばす。1行の障害で残り全部を落とさない。
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("transcript を読めません: %s: %w", path, err)
	}

	start, end := turnRange(lines, promptID)
	texts := collectAssistantTexts(lines[start:end])
	signals := ParseSignals(texts, prefix, currentIdentifier)

	return &TranscriptReadResult{Signals: signals, Usage: sumUsage(lines)}, nil
}

// turnRange は転写の中から、この turn に対応する範囲を切り出す（設計 3-25 の段2〜4）。
//
// lines: transcript の全行。
// promptID: Stop hook が渡した prompt_id。空なら最後の "typed" 起点を使う。
// 戻り値の1つ目: 範囲の開始位置（含む）。
// 戻り値の2つ目: 範囲の終了位置（含まない）。
func turnRange(lines []transcriptLine, promptID string) (int, int) {
	anchor := -1
	if promptID != "" {
		for i := len(lines) - 1; i >= 0; i-- {
			if lines[i].Type == "user" && lines[i].PromptID == promptID {
				anchor = i
				break
			}
		}
	}
	if anchor < 0 {
		anchor = len(lines) - 1
	}
	if anchor < 0 {
		return 0, 0
	}

	// 段3: そこから前へ遡り、最初の "typed" の user 行を turn の頭とする。
	start := 0
	for i := anchor; i >= 0; i-- {
		if isTypedUser(lines[i]) {
			start = i
			break
		}
	}

	// 段4: 頭から後ろへ、次の "typed" の手前までをこの turn の範囲とする。
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if isTypedUser(lines[i]) {
			end = i
			break
		}
	}
	return start, end
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

// collectAssistantTexts は範囲内の assistant の text ブロックを順に集める（設計 3-25 の段5）。
//
// **`isSidechain == false` に絞る。**subagent の発言を印として拾わないためである。
//
// lines: turn の範囲の行。
// 戻り値: 集めた text の並び。
func collectAssistantTexts(lines []transcriptLine) []string {
	var texts []string
	for _, line := range lines {
		if line.Type != "assistant" || line.IsSidechain || line.Message == nil {
			continue
		}
		texts = append(texts, contentTexts(line.Message.Content)...)
	}
	return texts
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

// sumUsage は transcript 全体のトークンを集計する（設計 3-15）。
//
// **`requestId` で必ず重複排除する。**assistant の行が API 呼び出しと1対1である保証は
// 取れていない。重複排除しておけば、どちらでも正しい値になる。
//
// lines: transcript の全行。
// 戻り値: 集計したトークン。
func sumUsage(lines []transcriptLine) TokenUsage {
	var usage TokenUsage
	seen := map[string]bool{}
	for _, line := range lines {
		if line.Type != "assistant" || line.Message == nil || line.Message.Usage == nil {
			continue
		}
		if line.RequestID != "" {
			if seen[line.RequestID] {
				continue
			}
			seen[line.RequestID] = true
		}
		u := line.Message.Usage
		usage.APICalls++
		usage.Input += u.InputTokens
		usage.CacheCreation += u.CacheCreationInputTokens
		usage.CacheRead += u.CacheReadInputTokens
		usage.Output += u.OutputTokens
	}
	return usage
}
