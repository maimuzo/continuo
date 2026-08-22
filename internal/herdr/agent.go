package herdr

import (
	"context"
	"encoding/json"
	"regexp"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/normalize"
)

// メソッド名・params の引数名・result の形は 2026-08-18 に `herdr api schema --json` で
// 確認済みである（docs/plans/continuo_design.md 2-1 の
// 「socket API の実在するメソッドと引数」。protocol=19 / herdr 0.8.0）。
// result に出てくる値の形は types.go を参照すること。
const (
	// MethodAgentStart は agent を起動するメソッド名である。
	MethodAgentStart = "agent.start"
	// MethodAgentPrompt はプロンプトを送るメソッド名である。
	MethodAgentPrompt = "agent.prompt"
	// MethodAgentRead は画面を読むメソッド名である。
	MethodAgentRead = "agent.read"
	// MethodAgentGet は agent の状態を読むメソッド名である。
	// **`agent.status` というメソッドは存在しない**（2-1）。
	MethodAgentGet = "agent.get"
	// MethodAgentList は agent の一覧を読むメソッド名である。
	MethodAgentList = "agent.list"
	// MethodAgentWait は agent の状態が変わるまで待つメソッド名である。
	MethodAgentWait = "agent.wait"
	// MethodAgentRename は agent の名前を変えるメソッド名である。
	MethodAgentRename = "agent.rename"
	// MethodAgentSendKeys は agent にキーを送るメソッド名である。
	// 権限の確認で止まった agent を取り消すのに使う（設計 3-11）。
	MethodAgentSendKeys = "agent.send_keys"
)

// agentNamePattern は herdr が受け付ける agent 名のパターンである
// （設計 3-3。実測: `^[a-z][a-z0-9_-]{0,31}$`）。
var agentNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// ValidateAgentName は name が herdr の agent 名の許容パターン
// （`^[a-z][a-z0-9_-]{0,31}$`。3-3）に収まっているかを検査する。
//
// agent 名など herdr へ渡す識別子は internal/normalize の SafeName 型を経由させる
// （3-7）。ただし SafeName が許す文字集合（英数字・アンダースコア・ハイフン・ドット・
// スラッシュ、大文字含む）は herdr の agent 名のパターンより広いため、agent 名として
// 使う直前にこの関数で追加の検査を行う。
//
// name: 検査対象。
// 戻り値: パターンに収まっていれば nil。収まっていなければ、人間が読めるエラーを返す。
func ValidateAgentName(name normalize.SafeName) error {
	s := name.String()
	if !agentNamePattern.MatchString(s) {
		return i18n.Errorf(i18n.KeyHerdrAgentInvalidName, s)
	}
	return nil
}

// AgentStartParams は agent.start の params である
// （`schemas.request.$defs.AgentStartParams`。引数は name / kind / pane_id / args /
// timeout_ms。**必須は name / kind / pane_id の3つで、args は必須ではない**）。
//
// **環境変数はここでは渡せない。**`agent.start` に env という引数は無く、
// 環境変数を渡す経路は `pane.split` の env である（PaneSplitParams.Env）。
type AgentStartParams struct {
	// Name は起動する agent の名前である。`^[a-z][a-z0-9_-]{0,31}$` に収まる必要がある
	// （3-3）。AgentStart はこのパターンを満たさない場合、herdr へ接続する前にエラーを返す。
	// JSON 上は文字列として送られる（型が normalize.SafeName なのは実装側の制約。3-7）。
	Name normalize.SafeName `json:"name"`
	// Kind は herdr に渡す agent の種別である（設定の claude.kind。例: "claude"）。
	Kind string `json:"kind"`
	// PaneID は agent を起動する pane の ID である（pane.split の結果から得る）。
	PaneID string `json:"pane_id"`
	// Args は Claude Code へ渡す起動フラグである
	// （例: ["--settings", "<設定ファイル>", "--session-id", "<UUID>",
	// "--permission-mode", "dontAsk"]。3-9 の段9）。
	// **これが Claude Code への起動フラグを渡す経路である**（2-1）。
	Args []string `json:"args,omitempty"`
	// TimeoutMs は herdr 側で agent の検知を待つ上限（ミリ秒）である。
	// 0 なら送らず、herdr の既定（実測で30秒）に任せる。
	// **実スキーマの説明は「3000 より大きく 300000 以下」と定めている**ので、
	// 送るならその範囲に収めること。
	TimeoutMs int `json:"timeout_ms,omitempty"`
}

// AgentStartResult は agent.start の result である。
//
// 【メソッドと応答の対応づけは推定である】
// 応答のスキーマ自体は実在するが、**スキーマは「どのメソッドがどの変種を返すか」を
// 書いていない。**変種 `agent_started`
// （`{"type":"agent_started","agent":AgentInfo,"argv":[…]}`）を返すものとして解釈する。
// 推定が外れていれば Type で気づける。
//
// **以前この型が持っていた name / pane_id / interactive_ready を result の直下に置く形は
// 誤りだった。**それらは agent（AgentInfo）の中のフィールドである。
type AgentStartResult struct {
	// Type は応答の変種を表す判別子である（推定: "agent_started"）。
	Type string `json:"type"`
	// Agent は起動した agent である。名前は Agent.Name、pane は Agent.PaneID で取る。
	Agent Agent `json:"agent"`
	// Argv は herdr が実際に実行したコマンド行である。
	// 起動フラグ（AgentStartParams.Args）が意図どおり渡ったかの確認に使える。
	Argv []string `json:"argv,omitempty"`
}

// AgentStart は agent.start を1回だけ呼ぶ（リトライしない）。
// pane を作った直後の agent_pane_busy に対処したい場合は AgentStartWithRetry を使うこと。
//
// 待ち時間は Startup（herdr.startup_timeout_ms。既定60秒）を使う。agent の起動は
// 実測で検知まで既定30秒かかるため、herdr の socket API の応答用の
// Read（herdr.read_timeout_ms。既定5秒）では必ず足りない。
// ctx に期限があればそちらを使う。
//
// ctx: 呼び出しに適用するコンテキスト。
// params: 起動する agent の名前・種別・pane・起動フラグ。
// 戻り値: params.Name が herdr の agent 名パターンに収まらない場合はエラーを返す
// （herdr へは接続しない）。herdr のエラー応答は *Error として返る（IsCode で判定できる）。
func (c *Client) AgentStart(ctx context.Context, params AgentStartParams) (*AgentStartResult, error) {
	if err := ValidateAgentName(params.Name); err != nil {
		return nil, err
	}

	raw, err := c.call(ctx, MethodAgentStart, params, c.timeouts.Startup)
	if err != nil {
		return nil, err
	}
	var result AgentStartResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, i18n.Errorf(i18n.KeyHerdrCallUnmarshalFailed, MethodAgentStart, err)
	}
	return &result, nil
}

// AgentStartWithRetry は agent.start を呼び、herdr が ErrCodeAgentPaneBusy を返した場合に
// リトライする（設計 2-1: 「herdr pane split の直後に herdr agent start を呼ぶと
// agent_pane_busy が返ることがある（実測で1回発生）。リトライを入れる」）。
//
// **`agent_pane_busy` は「pane がまだシェルのプロンプトに来ていない」という意味である。**
// `worktree.open` が pane を作った直後は、シェルの起動（プロファイルの読み込みなど）が
// 終わっておらず、herdr は `agent target pane <id> is not an available shell` を返す
// （2026-08-21 に E2E で再現）。**待てば必ず使えるようになるので、時間で粘る。**
//
// ctx: 呼び出しに適用するコンテキスト。リトライの待機中も尊重する
// （ctx が終わればその時点で打ち切る）。
// params: AgentStart と同じ。
// budget: `agent_pane_busy` を受けたときに粘る時間の上限。**0 以下なら1回だけ呼ぶ。**
// delay: 各リトライの前に待つ時間。
// 戻り値: いずれかの試行が成功すればその結果を返す。agent_pane_busy 以外のエラーを
// 受けた場合は即座にそのエラーを返す（リトライしない）。budget を使い切っても
// なお agent_pane_busy の場合は、最後に受け取ったエラーを返す。
func (c *Client) AgentStartWithRetry(
	ctx context.Context,
	params AgentStartParams,
	budget time.Duration,
	delay time.Duration,
) (*AgentStartResult, error) {
	deadline := time.Now().Add(budget)
	var lastErr error
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		result, err := c.AgentStart(ctx, params)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !IsCode(err, ErrCodeAgentPaneBusy) {
			return nil, err
		}
		// **時間で打ち切る。**回数で打ち切ると、シェルの起動が遅いマシンで足りなくなる。
		if !time.Now().Before(deadline) {
			return nil, lastErr
		}
	}
}

// AgentWaitOptions は「agent の状態が落ち着くまで待つ」ときの条件である
// （`schemas.request.$defs.AgentPromptWaitOptions`。項目は timeout_ms と until で、
// **どちらも必須ではない**）。
//
// agent.prompt の wait に入れる値である。ゼロ値（`&AgentWaitOptions{}`）を渡すと
// `{}` を送ることになり、herdr の既定（idle / done / blocked のいずれかに落ち着くまで待つ）
// に従う。
type AgentWaitOptions struct {
	// TimeoutMs は herdr 側で待つ上限（ミリ秒）である。0 なら送らず、herdr の既定に任せる。
	TimeoutMs int `json:"timeout_ms,omitempty"`
	// Until は待機の終了条件である。**状態名の配列である。**
	// 空なら送らず、herdr の既定（idle / done / blocked）に任せる。
	Until []AgentStatus `json:"until,omitempty"`
}

// AgentPromptParams は agent.prompt の params である
// （`schemas.request.$defs.AgentPromptParams`。引数は target / text / wait。
// **必須は target と text の2つである**）。
type AgentPromptParams struct {
	// Target は送信先の agent である。**continuo は常に agent 名で指す。**
	//
	// herdr 本体の skill 文書は「Agent commands accept either a unique live agent name or
	// the pane ID currently hosting that agent」（原文。訳: agent 系のコマンドは、
	// 一意な稼働中の agent 名か、その agent が居る pane の ID のどちらかを受け付ける）と
	// 書いているが、continuo は自分で付けた agent 名でしか指さないので、AgentPrompt は
	// 送信前に ValidateAgentName で agent 名のパターンを検査する。
	Target normalize.SafeName `json:"target"`
	// Text は送信するプロンプト本文である。
	Text string `json:"text"`
	// Wait は turn の完了を待つ条件である。**真偽値ではなくオブジェクトである。**
	// nil なら送らず、待たずに返る。待つ場合は `&AgentWaitOptions{}` を渡すと
	// herdr の既定（idle / done / blocked のいずれかに落ち着くまで待つ）に従う。
	//
	// **以前この項目を bool で宣言していたのは誤りだった。**実スキーマの型は
	// `AgentPromptWaitOptions | null` であり、`true` を送ると型が合わない。
	Wait *AgentWaitOptions `json:"wait,omitempty"`
}

// AgentPromptResult は agent.prompt の result である。
//
// 【メソッドと応答の対応づけは推定である】
// 変種 `agent_prompted`（`{"type":"agent_prompted","agent":AgentInfo}`）を返すものとして
// 解釈する。待機後の状態は Agent.AgentStatus で取る。
//
// **以前この型が持っていた status を result の直下に置く形は誤りだった。**
// 実スキーマに `status` という名前のフィールドは無く、状態は AgentInfo の
// `agent_status` である。
type AgentPromptResult struct {
	// Type は応答の変種を表す判別子である（推定: "agent_prompted"）。
	Type string `json:"type"`
	// Agent はプロンプトを送ったあとの agent である。
	Agent Agent `json:"agent"`
}

// AgentPrompt は agent.prompt を呼び、agent へプロンプトを送る。
//
// 待ち時間は params.Wait で変わる。Wait が nil でないときは turn の完了まで待つので
// Turn（claude.turn_timeout_ms。既定1時間）を使い、nil のときは herdr の socket API の
// 応答を待つだけなので Read（herdr.read_timeout_ms。既定5秒）を使う。
// ctx に期限があればそちらを使う。
//
// ctx: 呼び出しに適用するコンテキスト。
// params: 送信先・本文・待機の条件。
// 戻り値: params.Target が herdr の agent 名パターンに収まらない場合はエラーを返す。
// herdr のエラー応答は *Error として返る。
func (c *Client) AgentPrompt(ctx context.Context, params AgentPromptParams) (*AgentPromptResult, error) {
	if err := ValidateAgentName(params.Target); err != nil {
		return nil, err
	}

	timeout := c.timeouts.Read
	if params.Wait != nil {
		timeout = c.timeouts.Turn
	}

	raw, err := c.call(ctx, MethodAgentPrompt, params, timeout)
	if err != nil {
		return nil, err
	}
	var result AgentPromptResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, i18n.Errorf(i18n.KeyHerdrCallUnmarshalFailed, MethodAgentPrompt, err)
	}
	return &result, nil
}

// AgentReadParams は agent.read の params である
// （`schemas.request.$defs.AgentReadParams`。引数は target / source / format / lines /
// strip_ansi。**必須は target と source の2つである**）。
type AgentReadParams struct {
	// Target は読み取る agent である（AgentPromptParams.Target と同じ扱い）。
	Target normalize.SafeName `json:"target"`
	// Source は読み取る対象である。**必須の引数である。**
	// 値は ReadSource の4つ（visible / recent / recent_unwrapped / detection）に限られる。
	//
	// **CLI の綴りをそのまま渡してはならない。**CLI は
	// `herdr agent read --source recent-unwrapped` と**ハイフン**で書くが、
	// socket API の enum は `recent_unwrapped` と**アンダースコア**である。
	Source ReadSource `json:"source"`
	// Format は読み取った内容の書式である。空なら送らず、herdr の既定（text）に従う。
	// 値は ReadFormat の2つ（text / ansi）に限られる。
	Format ReadFormat `json:"format,omitempty"`
	// Lines は読み取る行数である。0 なら送らず、herdr の既定に任せる。
	Lines int `json:"lines,omitempty"`
	// StripANSI は ANSI エスケープ列を落とすかどうかである。
	// 送らない（nil）と herdr の既定（true）に従う。**明示的に偽を送りたい場合があるので
	// ポインタで持つ**（bool + omitempty だと偽を送れない）。
	StripANSI *bool `json:"strip_ansi,omitempty"`
}

// AgentReadResult は agent.read の result である。
//
// 【メソッドと応答の対応づけは推定である】
// 画面の読み取り結果を返す変種は `pane_read`
// （`{"type":"pane_read","read":PaneReadResult}`）だけなので、これを返すものとして解釈する。
//
// **以前この型が持っていた text を result の直下に置く形は誤りだった。**
// 本文は read（PaneRead）の中の Text である。
type AgentReadResult struct {
	// Type は応答の変種を表す判別子である（推定: "pane_read"）。
	Type string `json:"type"`
	// Read は読み取った結果である。画面の本文は Read.Text である。
	Read PaneRead `json:"read"`
}

// AgentRead は agent.read を呼び、agent の画面を読む。
//
// ctx: 呼び出しに適用するコンテキスト。
// params: 読み取る agent・範囲。
// 戻り値: params.Target が herdr の agent 名パターンに収まらない場合はエラーを返す。
// herdr のエラー応答は *Error として返る。
func (c *Client) AgentRead(ctx context.Context, params AgentReadParams) (*AgentReadResult, error) {
	if err := ValidateAgentName(params.Target); err != nil {
		return nil, err
	}

	raw, err := c.call(ctx, MethodAgentRead, params, c.timeouts.Read)
	if err != nil {
		return nil, err
	}
	var result AgentReadResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, i18n.Errorf(i18n.KeyHerdrCallUnmarshalFailed, MethodAgentRead, err)
	}
	return &result, nil
}

// AgentGetParams は agent.get の params である
// （`schemas.request.$defs.AgentTarget`。**引数は target だけで、必須である**）。
type AgentGetParams struct {
	// Target は状態を読む agent である（AgentPromptParams.Target と同じ扱い）。
	Target normalize.SafeName `json:"target"`
}

// AgentGetResult は agent.get の result である。
//
// 【メソッドと応答の対応づけは推定である】
// 変種 `agent_info`（`{"type":"agent_info","agent":AgentInfo}`）を返すものとして解釈する。
//
// **以前この型が持っていた name / status を result の直下に置く形は誤りだった。**
// 名前は Agent.Name、状態は Agent.AgentStatus である（`status` という名前ではない）。
type AgentGetResult struct {
	// Type は応答の変種を表す判別子である（推定: "agent_info"）。
	Type string `json:"type"`
	// Agent は agent の情報である。状態は Agent.AgentStatus で取る。
	Agent Agent `json:"agent"`
}

// AgentGet は agent.get を呼び、agent の状態を読む。
//
// ctx: 呼び出しに適用するコンテキスト。
// params: 状態を読む agent。
// 戻り値: params.Target が herdr の agent 名パターンに収まらない場合はエラーを返す。
// herdr のエラー応答は *Error として返る。
func (c *Client) AgentGet(ctx context.Context, params AgentGetParams) (*AgentGetResult, error) {
	if err := ValidateAgentName(params.Target); err != nil {
		return nil, err
	}

	raw, err := c.call(ctx, MethodAgentGet, params, c.timeouts.Read)
	if err != nil {
		return nil, err
	}
	var result AgentGetResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, i18n.Errorf(i18n.KeyHerdrCallUnmarshalFailed, MethodAgentGet, err)
	}
	return &result, nil
}

// AgentListResult は agent.list の result である。
//
// **形は実測で確認済みである。**`herdr agent list`（読み取りのみ）の応答は
// `{"id":"cli:agent:list","result":{"agents":[…],"type":"agent_list"}}` であり、
// 各項目は agent / agent_session / agent_status / cwd / focused / pane_id / revision /
// tab_id / terminal_id / workspace_id 等を持っていた（types.go の Agent）。
type AgentListResult struct {
	// Type は応答の変種を表す判別子である（"agent_list"）。
	Type string `json:"type"`
	// Agents は稼働中の agent の一覧である。
	Agents []Agent `json:"agents"`
}

// AgentList は agent.list を呼び、agent の一覧を読む
// （`schemas.request.$defs.EmptyParams`。引数は無い）。
//
// ctx: 呼び出しに適用するコンテキスト。
// 戻り値: 稼働中の agent の一覧。herdr のエラー応答は *Error として返る。
func (c *Client) AgentList(ctx context.Context) (*AgentListResult, error) {
	raw, err := c.call(ctx, MethodAgentList, nil, c.timeouts.Read)
	if err != nil {
		return nil, err
	}
	var result AgentListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, i18n.Errorf(i18n.KeyHerdrCallUnmarshalFailed, MethodAgentList, err)
	}
	return &result, nil
}

// AgentWaitParams は agent.wait の params である
// （`schemas.request.$defs.AgentWaitParams`。引数は target / timeout_ms / until。
// **必須は target だけである**）。
type AgentWaitParams struct {
	// Target は待つ agent である（AgentPromptParams.Target と同じ扱い）。
	Target normalize.SafeName `json:"target"`
	// TimeoutMs は herdr 側で待つ上限（ミリ秒）である。0 なら送らず、herdr の既定に任せる。
	TimeoutMs int `json:"timeout_ms,omitempty"`
	// Until は待機の終了条件である。**状態名の配列である。**
	// 要素は AgentStatus の5つ（idle / working / blocked / done / unknown）に限られる。
	// 空なら送らず、herdr の既定（idle / done / blocked のいずれかに落ち着くまで待つ）に任せる。
	//
	// **以前この項目を string で宣言していたのは誤りだった。**実スキーマの型は
	// `{"items":{"$ref":"…/AgentStatus"},"type":"array"}` であり、
	// `"until":"idle"` と文字列で送ると型が合わない。`"until":["idle"]` と送る。
	Until []AgentStatus `json:"until,omitempty"`
}

// AgentWaitResult は agent.wait の result である。
//
// 【メソッドと応答の対応づけは推定である】
// 変種 `agent_info`（`{"type":"agent_info","agent":AgentInfo}`）を返すものとして解釈する。
// 待機が終わった時点の状態は Agent.AgentStatus で取る。
type AgentWaitResult struct {
	// Type は応答の変種を表す判別子である（推定: "agent_info"）。
	Type string `json:"type"`
	// Agent は待機が終わった時点の agent である。
	Agent Agent `json:"agent"`
}

// AgentWait は agent.wait を呼び、agent の状態が変わるまで待つ。
//
// 待ち時間は Turn（claude.turn_timeout_ms。既定1時間）を使う。状態変化を待つ呼び出しは
// herdr の socket API の応答用の Read（既定5秒）では足りないためである。
// ctx に期限があればそちらを使う。
//
// ctx: 呼び出しに適用するコンテキスト。
// params: 待つ agent・終了条件（状態名の配列）・herdr 側の上限。
// 戻り値: params.Target が herdr の agent 名パターンに収まらない場合はエラーを返す。
// herdr のエラー応答は *Error として返る。
func (c *Client) AgentWait(ctx context.Context, params AgentWaitParams) (*AgentWaitResult, error) {
	if err := ValidateAgentName(params.Target); err != nil {
		return nil, err
	}

	raw, err := c.call(ctx, MethodAgentWait, params, c.timeouts.Turn)
	if err != nil {
		return nil, err
	}
	var result AgentWaitResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, i18n.Errorf(i18n.KeyHerdrCallUnmarshalFailed, MethodAgentWait, err)
	}
	return &result, nil
}

// AgentRenameParams は agent.rename の params である
// （`schemas.request.$defs.AgentRenameParams`。引数は target と name。
// **必須は target だけである**）。
type AgentRenameParams struct {
	// Target は名前を変える agent である（AgentPromptParams.Target と同じ扱い）。
	Target normalize.SafeName `json:"target"`
	// Name は新しい agent 名である。`^[a-z][a-z0-9_-]{0,31}$` に収まる必要がある（3-3）。
	// 空なら送らない（実スキーマ上は省略可能）。
	Name normalize.SafeName `json:"name,omitempty"`
}

// AgentRenameResult は agent.rename の result である。
//
// 【メソッドと応答の対応づけは推定である】
// agent を対象にした更新なので `agent_info`
// （`{"type":"agent_info","agent":AgentInfo}`）を返すものとして解釈する。
type AgentRenameResult struct {
	// Type は応答の変種を表す判別子である（推定: "agent_info"）。
	Type string `json:"type"`
	// Agent は名前を変えたあとの agent である。
	Agent Agent `json:"agent"`
}

// AgentRename は agent.rename を呼び、agent の名前を変える。
//
// ctx: 呼び出しに適用するコンテキスト。
// params: 対象の agent（必須）と新しい名前。
// 戻り値: params.Target が herdr の agent 名パターンに収まらない場合はエラーを返す。
// **params.Name も、空でなければ同じパターンで検査する**（新しい名前が herdr に
// 拒否される形だと、呼び出しは通ったのに名前が変わらないという分かりにくい失敗になるため）。
// herdr のエラー応答は *Error として返る。
func (c *Client) AgentRename(ctx context.Context, params AgentRenameParams) (*AgentRenameResult, error) {
	if err := ValidateAgentName(params.Target); err != nil {
		return nil, err
	}
	if params.Name != "" {
		if err := ValidateAgentName(params.Name); err != nil {
			return nil, err
		}
	}

	raw, err := c.call(ctx, MethodAgentRename, params, c.timeouts.Read)
	if err != nil {
		return nil, err
	}
	var result AgentRenameResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, i18n.Errorf(i18n.KeyHerdrCallUnmarshalFailed, MethodAgentRename, err)
	}
	return &result, nil
}

// AgentSendKeysParams は agent.send_keys に渡す引数である。
// target: agent 名。keys: 送るキーの並び。Escape は "esc"（"escape" も受理される）。
type AgentSendKeysParams struct {
	Target normalize.SafeName `json:"target"`
	Keys   []string           `json:"keys"`
}

// AgentSendKeys は agent にキーを送る。
//
// 使いどころは、権限の確認で止まった agent を取り消すことである（設計 3-11）。
// blocked のまま次のプロンプトを投げると、保留中の権限要求が承認されて実行されるため、
// 次を投げる前に必ず ["esc"] を送る。
//
// ctx: 呼び出しに適用するコンテキスト。
// params: 宛先の agent 名と、送るキーの並び。
// 戻り値: 送出に失敗したときのエラー。agent 名が規則に合わなければ呼び出す前に落とす。
func (c *Client) AgentSendKeys(ctx context.Context, params AgentSendKeysParams) error {
	if err := ValidateAgentName(params.Target); err != nil {
		return err
	}
	if len(params.Keys) == 0 {
		return i18n.Errorf(i18n.KeyHerdrAgentSendKeysEmpty, MethodAgentSendKeys)
	}
	if _, err := c.call(ctx, MethodAgentSendKeys, params, c.timeouts.Read); err != nil {
		return err
	}
	return nil
}
