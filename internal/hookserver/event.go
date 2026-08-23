// Package hookserver は Claude Code の hook を Unix domain socket で受け取り、
// orchestrator へ渡す受け口である（docs/plans/continuo_design.md 3-2 / 3-4 / 3-19 / 3-23）。
//
// このパッケージは判断をしない。socket の listen・届いた JSON の解釈・逃がし先の
// 読み戻しだけを持ち、受け取った HookEvent はすべて HookSink.OnHook へ渡す。
// turn の終わりの判定・stall の時計・session_id から run を引く索引は、すべて
// orchestrator の側にある（設計 3-4）。
//
// 呼ぶ順番は復元の手順（設計 3-4）に対応している。
//
//	Start          … 段5d。listen を始める。届いた hook は内部のキューに溜める
//	ReplayPending  … 段5e の1回目。逃がし先に溜まった hook を読み戻す（キューへはまだ積まない）
//	StartDelivery  … 段5e の2回目 + 段6b。もう一度逃がし先を走査してキューの先頭へ積み、
//	                 HookSink.OnHook への配送を始める
//	Close          … listen と accept 済みの接続を閉じ、配送の goroutine を終わらせる
//
// このほかに QueueLen を公開している。**手順には出てこない。**配送待ちの件数を返すだけで、
// 監視と検査（「まだ配送していない」ことを時間ではなく件数で確かめる）に使う。
package hookserver

// HookEvent は Claude Code の hook から届く JSON である（設計 1-4）。
//
// イベントによって入る項目が違うので、共通のものだけを必須にする。
// 未知のキーは encoding/json の既定どおり無視する。Claude Code 側で項目が増えても
// このパッケージは落ちない。
type HookEvent struct {
	HookEventName  string `json:"hook_event_name"`
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	PromptID       string `json:"prompt_id"`

	// BackgroundTasks は Stop / SubagentStop にだけ入る。
	// 「項目が欠けている」（nil）と「空配列」（長さ0の非 nil）を区別するためポインタにする。
	// 設計 3-2 の判定は、欠けているときを「判定不能」、空配列のときを
	// 「settle_ms 待って turn の終わりとする」と別扱いにしているため、
	// この区別を潰してはならない。
	BackgroundTasks *[]BackgroundTask `json:"background_tasks"`
	StopHookActive  bool              `json:"stop_hook_active"`

	// Prompt は UserPromptSubmit にだけ入る。<task-notification> の判定に使う（設計 1-3）。
	Prompt string `json:"prompt"`

	// NotificationType は Notification にだけ入る。permission_prompt を拾う（設計 3-11）。
	NotificationType string `json:"notification_type"`

	// AgentID / AgentType は SubagentStart / SubagentStop にだけ入る。
	// AgentType が空文字の SubagentStop は捨てる判断を orchestrator が行う（設計 1-3）。
	// hookserver はここでは捨てない。
	AgentID   string `json:"agent_id"`
	AgentType string `json:"agent_type"`

	// Truncated は Claude Code ではなく continuo hook が付ける印である。
	// 標準入力が上限（hookclient.DefaultMaxInputBytes）を超えたため、共通の項目だけを
	// 拾って組み立て直した hook であることを表す。**捨てずに渡すためにこの形にしている**
	// （04_hook.md の受け入れの基準「どのイベントも捨てずに HookSink.OnHook へ渡す」）。
	// tool_input / tool_response のような大きな項目は落ちている。
	Truncated bool `json:"continuo_truncated"`
	// TruncatedLimitBytes は Truncated が true のときの上限（バイト）である。
	// 何がどれだけ失われたかを orchestrator と人間が知るために入れる。
	TruncatedLimitBytes int64 `json:"continuo_truncated_limit_bytes"`
}

// BackgroundTask は Stop / SubagentStop の background_tasks の要素である。
//
// 項目は docs/evidence/hooks_probe_20260817.jsonl の実測に合わせてある。
// 実測で出た形は2種類である。
//
//	{"id":"a1f9f743842d397e1","type":"subagent","status":"running","description":"ディレクトリ調査","agent_type":"Explore"}
//	{"id":"bmr1ksf9i","type":"shell","status":"running","description":"45秒スリープをバックグラウンド実行","command":"sleep 45"}
//
// 判断に使うのは「空かどうか」だけである（設計 1-3）。
// 項目を全部持つのは、記録とダッシュボード表示のためである。
type BackgroundTask struct {
	ID          string `json:"id"`
	Type        string `json:"type"`   // "subagent" / "shell"
	Status      string `json:"status"` // "running" など
	Description string `json:"description"`
	AgentType   string `json:"agent_type"` // type == "subagent" のときだけ入る
	Command     string `json:"command"`    // type == "shell" のときだけ入る
}

// HookSink は hookserver が hook を届ける先である。orchestrator が実装する。
type HookSink interface {
	// OnHook は hook を1件受け取る。
	//
	// ev: 届いた hook のイベント1件。イベントの種類で選り分けずに、届いたものを全部渡す。
	// 戻り値: 知っている session_id なら true。知らない session_id なら false を返す
	// （hookserver は警告をログに出して捨てる。落ちない。設計 3-4 の段6b）。
	OnHook(ev HookEvent) (accepted bool)
}
