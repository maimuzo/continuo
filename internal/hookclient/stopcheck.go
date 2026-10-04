package hookclient

import (
	"encoding/json"

	"github.com/maimuzo/continuo/internal/statussignal"
)

// stopEventName は、表明を調べる hook のイベント名である。
//
// **`SubagentStop` は調べない。**subagent の応答は表明として読まない
// （本体も `isSidechain == false` の text だけを集める。設計 3-25）。
const stopEventName = "Stop"

// stopInput は `Stop` hook の入力のうち、表明を調べるのに要る項目である。
//
// **`stop_hook_active` と `last_assistant_message` は、「欄が在るか」を区別して読む**（ポインタで受ける）。
// 欄が無い入力を偽や空文字と同じに読むと、Claude Code の版が変わって欄が消えたときに、
// 調べる側へ倒れる。`hook_event_name` は区別しない（欄が無ければ空文字で、`Stop` と一致しない）。
// **型が違う入力**（真偽値でない `stop_hook_active` など）**は、JSON の読み取りが失敗して調べない。**
type stopInput struct {
	HookEventName string `json:"hook_event_name"`
	// StopHookActive は、差し戻されて書き直したあとの `Stop` で真になる。
	// **どの `Stop` hook が差し戻したかに関わらず真になる**（利用者が入れた別の hook でも）。
	StopHookActive *bool `json:"stop_hook_active"`
	// LastAssistantMessage は、その turn の最後の assistant のテキストブロック1つである。
	// **表明のあとに道具を呼ぶと、ここには表明が入らない**（2026-10-04、Claude Code 2.1.289 で実測）。
	LastAssistantMessage *string `json:"last_assistant_message"`
}

// StopDecision は、`Stop` hook の入力を調べた結果である（issue #274）。
type StopDecision struct {
	// Block は、差し戻すなら true である。
	Block bool
	// Reason は、差し戻すときに Claude Code へ渡す本文である。Block が偽なら空。
	Reason string
}

// CheckStop は `Stop` hook の入力を調べ、表明の値が決まり以外なら差し戻すと決める
// （issue #274 の経路1。設計 3-25）。
//
// **調べる材料が1つでも欠けていたら、差し戻さない。**差し戻さなかった分は、本体が
// turn の終わりに transcript から読み、次の turn の指示で返す（経路2）。
// 次のどれかに当たれば、差し戻さない。
//
//	`Stop` 以外の hook である（`SubagentStop` を含む）
//	入力を JSON として読めない
//	`stop_hook_active` の欄が無い、または真である
//	`last_assistant_message` の欄が無い、または文字列でない
//	取り得る値のファイルが無い・読めない・壊れている・材料が欠けている
//	表明が1行も無い、または全部が取り得る値である
//
// **`stop_hook_active` が真の `Stop` を調べないのは、差し戻しを1つの turn で1回だけに
// するためである。**書き直しても決まり以外だったときに、差し戻しが繰り返されるのを防ぐ。
//
// **この関数は goroutine を立てない。**呼ぶ側（`continuo hook`）は `recover` で包んでおり、
// 別の goroutine の panic はそこでは受けられない。Go の panic は終了コード 2 になり、
// `Stop` hook の 2 は「止まるな」と読まれる。
//
// line: 本体へ転送した1行（hook の入力の JSON）。
// pendingDir: `--pending-dir` に渡された絶対パス。取り得る値のファイルは、その親に在る。
// 戻り値: 差し戻すかどうかと、その本文。
func CheckStop(line []byte, pendingDir string) StopDecision {
	var in stopInput
	if err := json.Unmarshal(line, &in); err != nil {
		return StopDecision{}
	}
	if in.HookEventName != stopEventName {
		return StopDecision{}
	}
	if in.StopHookActive == nil || *in.StopHookActive {
		return StopDecision{}
	}
	if in.LastAssistantMessage == nil || *in.LastAssistantMessage == "" {
		return StopDecision{}
	}

	file, err := statussignal.Read(statussignal.PathFromPendingDir(pendingDir))
	if err != nil || !file.Usable() {
		return StopDecision{}
	}

	signals := statussignal.Parse([]string{*in.LastAssistantMessage}, file.Prefix, file.Identifier)
	invalid := statussignal.FindInvalid(signals, file.Values)
	if len(invalid) == 0 {
		return StopDecision{}
	}
	return StopDecision{
		Block:  true,
		Reason: statussignal.BlockReason(file.Prefix, file.Identifier, invalid, file.Values),
	}
}
