package herdr_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
)

// agentStartSchemaKeys は agent.start の実スキーマ（AgentStartParams）に定義されている
// 引数の全集合である。
var agentStartSchemaKeys = []string{"name", "kind", "pane_id", "args", "timeout_ms"}

// agentPromptSchemaKeys は agent.prompt の実スキーマ（AgentPromptParams）に定義されている
// 引数の全集合である。
var agentPromptSchemaKeys = []string{"target", "text", "wait"}

// agentReadSchemaKeys は agent.read の実スキーマ（AgentReadParams）に定義されている
// 引数の全集合である。
var agentReadSchemaKeys = []string{"target", "source", "format", "lines", "strip_ansi"}

// agentWaitSchemaKeys は agent.wait の実スキーマ（AgentWaitParams）に定義されている
// 引数の全集合である。
var agentWaitSchemaKeys = []string{"target", "timeout_ms", "until"}

// agentRenameSchemaKeys は agent.rename の実スキーマ（AgentRenameParams）に定義されている
// 引数の全集合である。
var agentRenameSchemaKeys = []string{"target", "name"}

// 目的: herdr の agent 名パターン（`^[a-z][a-z0-9_-]{0,31}$`。設計 3-3）に収まらない
// 名前が ValidateAgentName で弾かれることを確認する。境界値（32文字ちょうどは許容、
// 33文字は拒否）も含める。
// 与える情報: 大文字始まり・数字始まり・許容外の文字（コロン）を含む・33文字・
// 空文字の各ケースと、境界の32文字ちょうどのケース。
// 成功条件: 不正なケースはすべてエラーになり、32文字ちょうどのケースはエラーにならないこと。
func TestValidateAgentName_許容パターンに収まらない場合に弾かれる(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "小文字始まりの正常な名前", input: "continuo-yukikaki-87", wantErr: false},
		{name: "大文字始まりは拒否される", input: "Continuo", wantErr: true},
		{name: "数字始まりは拒否される", input: "1continuo", wantErr: true},
		{name: "コロンを含む名前は拒否される", input: "continuo:1", wantErr: true},
		{name: "空文字は拒否される", input: "", wantErr: true},
		{name: "32文字ちょうどは許容される", input: "a" + repeat("b", 31), wantErr: false},
		{name: "33文字は拒否される", input: "a" + repeat("b", 32), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := herdr.ValidateAgentName(normalize.SafeName(tc.input))
			if tc.wantErr && err == nil {
				t.Fatalf("エラーになるはずが nil だった: input=%q", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("エラーにならないはずが失敗した: input=%q, err=%v", tc.input, err)
			}
		})
	}
}

// repeat は s を n 回繰り返した文字列を返す（strings.Repeat を使わない理由は無いが、
// このテストファイル内で完結させるための小さなヘルパー）。
func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

// 目的: agent 名が herdr のパターンに収まらない場合、AgentStart が herdr へ接続すら
// せずにエラーを返すことを確認する（クライアント側の検証が先に働くこと）。
// 与える情報: 大文字を含む不正な agent 名。偽サーバは接続を受けたら即座に失敗させる
// ように仕込み、接続が来たら検知できるようにする。
// 成功条件: AgentStart がエラーを返し、かつ偽サーバへの接続回数が0であること。
func TestAgentStart_agent名が不正なら接続せずに弾かれる(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		t.Errorf("agent 名が不正なのに herdr へ接続してしまった（%d 回目）", n)
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second, Startup: time.Second, Turn: time.Second})
	_, err := client.AgentStart(context.Background(), herdr.AgentStartParams{
		Name:   normalize.SafeName("Invalid-Name"),
		Kind:   "claude",
		PaneID: "w1:p1",
	})
	if err == nil {
		t.Fatalf("不正な agent 名なのにエラーが返らなかった")
	}
	if got := fs.ConnCount(); got != 0 {
		t.Fatalf("接続していないはずなのに接続回数が %d だった", got)
	}
}

// 目的: agent.start が agent_pane_busy を返してもリトライし、最終的に成功へ転じることを
// 確認する（設計 2-1: 「herdr pane split の直後に herdr agent start を呼ぶと
// agent_pane_busy が返ることがある。リトライを入れる」）。
// 与える情報: 偽サーバは1・2回目の接続で agent_pane_busy エラーを返し、3回目で成功する。
// 成功条件: AgentStartWithRetry(maxRetries=3) が最終的に成功結果を返し、
// 偽サーバへの接続回数がちょうど3であること（無駄なリトライをしていないこと）。
func TestAgentStartWithRetry_agent_pane_busyから成功に転じる(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		if n < 3 {
			// エラー応答では id が空文字で返る（2-1 の実測）。実機と同じ形で返す。
			writeErrorResponse(t, conn, "", herdr.ErrCodeAgentPaneBusy, "pane がまだ使える状態ではありません")
			return
		}
		writeResult(t, conn, req.ID, herdr.AgentStartResult{
			Type: "agent_started",
			Agent: herdr.Agent{
				Name:             "continuo-test",
				PaneID:           "w1:p2",
				InteractiveReady: true,
			},
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second, Startup: time.Second, Turn: time.Second})
	result, err := client.AgentStartWithRetry(context.Background(), herdr.AgentStartParams{
		Name:   normalize.SafeName("continuo-test"),
		Kind:   "claude",
		PaneID: "w1:p2",
	}, 3, 10*time.Millisecond)

	if err != nil {
		t.Fatalf("リトライの末に成功するはずが失敗した: %v", err)
	}
	if result.Agent.Name != "continuo-test" {
		t.Fatalf("結果の agent.name が想定と違う: got %q", result.Agent.Name)
	}
	if got := fs.ConnCount(); got != 3 {
		t.Fatalf("接続回数が想定と違う: got %d, want 3", got)
	}
}

// 目的: agent_pane_busy 以外のエラーではリトライしないことを確認する
// （タスク要件: pane_busy 以外まで無駄にリトライしないことの裏取り）。
// 与える情報: 偽サーバは常に別のエラーコード（"pane_not_found"）を返す。
// 成功条件: AgentStartWithRetry がリトライせず即座にエラーを返し、接続回数が1であること。
func TestAgentStartWithRetry_pane_busy以外はリトライしない(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeErrorResponse(t, conn, req.ID, "pane_not_found", "指定された pane は存在しません")
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second, Startup: time.Second, Turn: time.Second})
	_, err := client.AgentStartWithRetry(context.Background(), herdr.AgentStartParams{
		Name:   normalize.SafeName("continuo-test"),
		Kind:   "claude",
		PaneID: "w1:p2",
	}, 3, 10*time.Millisecond)

	if err == nil {
		t.Fatalf("エラーになるはずが成功した")
	}
	if !herdr.IsCode(err, "pane_not_found") {
		t.Fatalf("想定と異なるエラーになった: %v", err)
	}
	if got := fs.ConnCount(); got != 1 {
		t.Fatalf("pane_busy 以外なのにリトライしてしまった: 接続回数 got %d, want 1", got)
	}
}

// 目的: agent.start の待ち時間が read_timeout_ms ではなく startup_timeout_ms で
// 決まることを確認する（設計 5-3 の設定例。agent の起動は実測で検知まで既定30秒かかるので、
// read_timeout_ms（既定5秒）で打ち切ってはならない）。
// 与える情報: Read を 100 ミリ秒、Startup を 5 秒にした Client と、400 ミリ秒待ってから
// 応答する偽サーバ。
// 成功条件: AgentStart が成功すること（Read の 100 ミリ秒で打ち切られないこと）。
func TestAgentStart_起動の待ち時間はstartup_timeout_msで決まる(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		time.Sleep(400 * time.Millisecond)
		writeResult(t, conn, req.ID, herdr.AgentStartResult{
			Type:  "agent_started",
			Agent: herdr.Agent{Name: "continuo-test", PaneID: "w1:p2"},
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{
		Read:    100 * time.Millisecond,
		Startup: 5 * time.Second,
		Turn:    5 * time.Second,
	})

	result, err := client.AgentStart(context.Background(), herdr.AgentStartParams{
		Name:   normalize.SafeName("continuo-test"),
		Kind:   "claude",
		PaneID: "w1:p2",
	})
	if err != nil {
		t.Fatalf("startup_timeout_ms まで待つはずが read_timeout_ms で打ち切られた: %v", err)
	}
	if result.Agent.Name != "continuo-test" {
		t.Fatalf("結果の agent.name が想定と違う: got %q", result.Agent.Name)
	}
}

// 目的: 待機ありの agent.prompt が turn_timeout_ms まで待つことを確認する
// （設計 5-3: turn_timeout_ms は1つの turn の上限。read_timeout_ms で打ち切ってはならない）。
// 与える情報: Read を 100 ミリ秒、Turn を 5 秒にした Client と、400 ミリ秒待ってから
// 応答する偽サーバ。Wait に待機条件のオブジェクトを設定した AgentPromptParams。
// 成功条件: AgentPrompt が成功し、応答の agent_status を読み取れること。
func TestAgentPrompt_待機ありのときはturn_timeout_msで決まる(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		time.Sleep(400 * time.Millisecond)
		writeResult(t, conn, req.ID, herdr.AgentPromptResult{
			Type:  "agent_prompted",
			Agent: herdr.Agent{AgentStatus: herdr.AgentStatusDone},
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{
		Read:    100 * time.Millisecond,
		Startup: 5 * time.Second,
		Turn:    5 * time.Second,
	})

	result, err := client.AgentPrompt(context.Background(), herdr.AgentPromptParams{
		Target: normalize.SafeName("continuo-test"),
		Text:   "続きをどうぞ",
		Wait:   &herdr.AgentWaitOptions{},
	})
	if err != nil {
		t.Fatalf("turn_timeout_ms まで待つはずが read_timeout_ms で打ち切られた: %v", err)
	}
	if result.Agent.AgentStatus != herdr.AgentStatusDone {
		t.Fatalf("結果の agent_status が想定と違う: got %q", result.Agent.AgentStatus)
	}
}

// 目的: agent.prompt が実スキーマどおりの引数名（target / text / wait）で送られることを
// 確認する（設計 2-1。name / prompt という引数は存在しない）。
// 与える情報: Target と Text だけを設定した AgentPromptParams（待機しない）。
// 成功条件: 偽サーバが受け取った params が target / text だけを持ち、
// 実スキーマに無いキーを持たないこと。待機しないときは wait を送らないこと。
func TestAgentPrompt_引数はtargetとtextで送られる(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.AgentPromptResult{
			Type:  "agent_prompted",
			Agent: herdr.Agent{AgentStatus: herdr.AgentStatusWorking},
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	if _, err := client.AgentPrompt(context.Background(), herdr.AgentPromptParams{
		Target: normalize.SafeName("continuo-test"),
		Text:   "続きをどうぞ",
	}); err != nil {
		t.Fatalf("AgentPrompt が失敗した: %v", err)
	}

	params := sentParams(t, fs, herdr.MethodAgentPrompt)
	if got := params["target"]; got != "continuo-test" {
		t.Fatalf("target が想定と違う: got %v", got)
	}
	if got := params["text"]; got != "続きをどうぞ" {
		t.Fatalf("text が想定と違う: got %v", got)
	}
	if _, ok := params["wait"]; ok {
		t.Fatalf("待機しないのに wait を送っている: %v", params)
	}
	assertSchemaKeys(t, params, herdr.MethodAgentPrompt, agentPromptSchemaKeys)
}

// 目的: agent.prompt の wait が**真偽値ではなくオブジェクト**として送られることを確認する
// （実スキーマの型は AgentPromptWaitOptions か null であり、`"wait": true` は型が合わない）。
// 与える情報: Wait に待機条件（until と timeout_ms）を設定した AgentPromptParams。
// 成功条件: wait が JSON オブジェクトとして届き、その中の until が**配列**であること。
func TestAgentPrompt_waitはオブジェクトで送られる(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.AgentPromptResult{
			Type:  "agent_prompted",
			Agent: herdr.Agent{AgentStatus: herdr.AgentStatusIdle},
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second, Turn: 5 * time.Second})
	if _, err := client.AgentPrompt(context.Background(), herdr.AgentPromptParams{
		Target: normalize.SafeName("continuo-test"),
		Text:   "続きをどうぞ",
		Wait: &herdr.AgentWaitOptions{
			TimeoutMs: 120000,
			Until:     []herdr.AgentStatus{herdr.AgentStatusIdle, herdr.AgentStatusBlocked},
		},
	}); err != nil {
		t.Fatalf("AgentPrompt が失敗した: %v", err)
	}

	params := sentParams(t, fs, herdr.MethodAgentPrompt)
	wait, ok := params["wait"].(map[string]any)
	if !ok {
		t.Fatalf("wait がオブジェクトとして送られていない（真偽値で送っていないか）: %#v", params["wait"])
	}
	until, ok := wait["until"].([]any)
	if !ok {
		t.Fatalf("wait.until が配列として送られていない: %#v", wait["until"])
	}
	if len(until) != 2 || until[0] != "idle" || until[1] != "blocked" {
		t.Fatalf("wait.until の中身が想定と違う: got %v", until)
	}
	if got, ok := wait["timeout_ms"].(float64); !ok || int(got) != 120000 {
		t.Fatalf("wait.timeout_ms が想定と違う: got %v", wait["timeout_ms"])
	}
	for key := range wait {
		if key != "until" && key != "timeout_ms" {
			t.Fatalf("wait に実スキーマに無いキー %q を送っている: %v", key, wait)
		}
	}
}

// 目的: agent の状態を読むメソッドが agent.get であり、引数として target だけを送ることを
// 確認する（設計 2-1: 「agent.status というメソッドは存在しない」「引数は target（必須）
// だけ」）。応答の状態が **agent_status** という名前で、agent の中に入っていることも確かめる。
// 与える情報: Target だけを設定した AgentGetParams。
// 成功条件: 送られた method が "agent.get" で、params が target 1個だけであること。
// 応答から Agent.AgentStatus を読み取れること。
func TestAgentGet_targetだけを送る(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		// 実機と同じ形（agent_info の変種。状態のキーは agent_status）で返す。
		raw := json.RawMessage(`{"type":"agent_info","agent":{
			"name":"continuo-test","agent":"claude","agent_status":"done",
			"pane_id":"w1:p2","tab_id":"w1:t1","workspace_id":"w1",
			"terminal_id":"term_1","focused":false,"revision":12}}`)
		writeResponse(t, conn, rpcResponse{ID: req.ID, Result: raw})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	result, err := client.AgentGet(context.Background(), herdr.AgentGetParams{
		Target: normalize.SafeName("continuo-test"),
	})
	if err != nil {
		t.Fatalf("AgentGet が失敗した: %v", err)
	}
	if result.Agent.AgentStatus != herdr.AgentStatusDone {
		t.Fatalf("結果の agent_status が想定と違う: got %q", result.Agent.AgentStatus)
	}
	if result.Agent.Name != "continuo-test" {
		t.Fatalf("結果の name が想定と違う: got %q", result.Agent.Name)
	}

	params := sentParams(t, fs, herdr.MethodAgentGet)
	if len(params) != 1 {
		t.Fatalf("agent.get の引数が target だけになっていない: %v", params)
	}
	if got := params["target"]; got != "continuo-test" {
		t.Fatalf("target が想定と違う: got %v", got)
	}
}

// 目的: agent.wait の until が**状態名の配列**として送られることを確認する
// （実スキーマの型は `{"items":{"$ref":"…/AgentStatus"},"type":"array"}` であり、
// `"until":"idle"` と文字列で送ると型が合わない）。
// 与える情報: Target・Until（2つの状態）・TimeoutMs を設定した AgentWaitParams。
// 成功条件: 送られた method が "agent.wait" で、until が JSON 配列として届き、
// 中身が渡した順に並んでいること。実スキーマに無いキーを送っていないこと。
func TestAgentWait_untilは配列で送られる(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.AgentWaitResult{
			Type:  "agent_info",
			Agent: herdr.Agent{AgentStatus: herdr.AgentStatusIdle},
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second, Turn: 5 * time.Second})
	result, err := client.AgentWait(context.Background(), herdr.AgentWaitParams{
		Target:    normalize.SafeName("continuo-test"),
		Until:     []herdr.AgentStatus{herdr.AgentStatusIdle, herdr.AgentStatusDone},
		TimeoutMs: 120000,
	})
	if err != nil {
		t.Fatalf("AgentWait が失敗した: %v", err)
	}
	if result.Agent.AgentStatus != herdr.AgentStatusIdle {
		t.Fatalf("結果の agent_status が想定と違う: got %q", result.Agent.AgentStatus)
	}

	params := sentParams(t, fs, herdr.MethodAgentWait)
	if got := params["target"]; got != "continuo-test" {
		t.Fatalf("target が想定と違う: got %v", got)
	}
	until, ok := params["until"].([]any)
	if !ok {
		t.Fatalf("until が配列として送られていない（文字列で送っていないか）: %#v", params["until"])
	}
	want := []string{"idle", "done"}
	if len(until) != len(want) {
		t.Fatalf("until の要素数が想定と違う: got %v, want %v", until, want)
	}
	for i := range want {
		if until[i] != want[i] {
			t.Fatalf("until の %d 番目が想定と違う: got %v, want %q", i, until[i], want[i])
		}
	}
	if got, ok := params["timeout_ms"].(float64); !ok || int(got) != 120000 {
		t.Fatalf("timeout_ms が想定と違う: got %v", params["timeout_ms"])
	}
	assertSchemaKeys(t, params, herdr.MethodAgentWait, agentWaitSchemaKeys)
}

// 目的: agent.wait の until を指定しない場合、until というキーを送らないことを確認する
// （実スキーマ上 until は必須ではなく、省略すると herdr の既定
// （idle / done / blocked のいずれかに落ち着くまで待つ）に従うため。
// 空配列を送ると「どの状態でも終わらない」と解釈されかねない）。
// 与える情報: Target だけを設定した AgentWaitParams。
// 成功条件: params が target 1個だけであること。
func TestAgentWait_untilを指定しなければ送らない(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.AgentWaitResult{
			Type:  "agent_info",
			Agent: herdr.Agent{AgentStatus: herdr.AgentStatusIdle},
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second, Turn: 5 * time.Second})
	if _, err := client.AgentWait(context.Background(), herdr.AgentWaitParams{
		Target: normalize.SafeName("continuo-test"),
	}); err != nil {
		t.Fatalf("AgentWait が失敗した: %v", err)
	}

	params := sentParams(t, fs, herdr.MethodAgentWait)
	if len(params) != 1 {
		t.Fatalf("until も timeout_ms も指定していないのに余計な引数を送っている: %v", params)
	}
	if _, ok := params["until"]; ok {
		t.Fatalf("until を指定していないのに送っている: %v", params)
	}
}

// 目的: agent.read の source が socket API の綴り（アンダースコア）で送られることを
// 確認する。**CLI は `--source recent-unwrapped` とハイフンで書くが、socket API の
// enum は `recent_unwrapped` である。**CLI の綴りをそのまま送ると拒否される。
// 与える情報: Source に ReadSourceRecentUnwrapped、Lines を設定した AgentReadParams。
// 成功条件: source が "recent_unwrapped" として届くこと。実スキーマに無いキーを
// 送っていないこと。応答の本文を Read.Text から読み取れること。
func TestAgentRead_sourceはアンダースコアの綴りで送られる(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.AgentReadResult{
			Type: "pane_read",
			Read: herdr.PaneRead{
				PaneID: "w1:p2",
				Source: herdr.ReadSourceRecentUnwrapped,
				Format: herdr.ReadFormatText,
				Text:   "作業を終えました",
			},
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	result, err := client.AgentRead(context.Background(), herdr.AgentReadParams{
		Target: normalize.SafeName("continuo-test"),
		Source: herdr.ReadSourceRecentUnwrapped,
		Lines:  50,
	})
	if err != nil {
		t.Fatalf("AgentRead が失敗した: %v", err)
	}
	if result.Read.Text != "作業を終えました" {
		t.Fatalf("読み取った本文が想定と違う: got %q", result.Read.Text)
	}

	params := sentParams(t, fs, herdr.MethodAgentRead)
	if got := params["source"]; got != "recent_unwrapped" {
		t.Fatalf("source の綴りが想定と違う（CLI のハイフン表記を送っていないか）: got %v", got)
	}
	assertSchemaKeys(t, params, herdr.MethodAgentRead, agentReadSchemaKeys)
}

// 目的: agent.start に Claude Code の起動フラグを args として載せて送れることを確認する
// （設計 2-1: 「args が Claude Code への起動フラグを渡す経路である」。3-9 の段9で
// --settings / --session-id / --permission-mode dontAsk を渡す）。
// 与える情報: Args に3組のフラグを入れた AgentStartParams。
// 成功条件: 偽サーバが受け取った params の args がそのままの並びで届き、pane の指定が
// pane_id という名前で届くこと。**env という引数を送っていないこと**
// （agent.start に env は無く、環境変数は pane.split で渡す）。
func TestAgentStart_起動フラグをargsに載せて送れる(t *testing.T) {
	wantArgs := []string{
		"--settings", "/Users/tester/.continuo/settings/87.json",
		"--session-id", "0f7a9d1c-3b2e-4a55-9c11-6d8e0b7f2a34",
		"--permission-mode", "dontAsk",
	}

	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.AgentStartResult{
			Type:  "agent_started",
			Agent: herdr.Agent{Name: "continuo-test"},
			Argv:  append([]string{"claude"}, wantArgs...),
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second, Startup: time.Second})

	result, err := client.AgentStart(context.Background(), herdr.AgentStartParams{
		Name:   normalize.SafeName("continuo-test"),
		Kind:   "claude",
		PaneID: "w1:p2",
		Args:   wantArgs,
	})
	if err != nil {
		t.Fatalf("AgentStart が失敗した: %v", err)
	}
	if len(result.Argv) != len(wantArgs)+1 {
		t.Fatalf("応答の argv を読み取れていない: got %v", result.Argv)
	}

	params := sentParams(t, fs, herdr.MethodAgentStart)
	if got := params["pane_id"]; got != "w1:p2" {
		t.Fatalf("pane_id が想定と違う: got %v（pane という名前で送っていないか）", got)
	}
	if _, ok := params["env"]; ok {
		t.Fatalf("agent.start に env を送っている（環境変数は pane.split で渡す）: %v", params)
	}
	assertSchemaKeys(t, params, herdr.MethodAgentStart, agentStartSchemaKeys)

	gotArgs, ok := params["args"].([]any)
	if !ok {
		t.Fatalf("args が配列として送られていない: %v", params["args"])
	}
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("args の要素数が想定と違う: got %v, want %v", gotArgs, wantArgs)
	}
	for i := range wantArgs {
		if gotArgs[i] != wantArgs[i] {
			t.Fatalf("args の %d 番目が想定と違う: got %v, want %q", i, gotArgs[i], wantArgs[i])
		}
	}
}

// 目的: agent.rename が target と name を送ることを確認する（設計 2-1 の表に載っている
// メソッド。**必須は target だけである**）。
// 与える情報: Target と Name を設定した AgentRenameParams。
// 成功条件: 送られた method が "agent.rename" で、target と name がそのまま届くこと。
// 実スキーマに無いキーを送っていないこと。
func TestAgentRename_targetとnameを送る(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.AgentRenameResult{
			Type:  "agent_info",
			Agent: herdr.Agent{Name: "continuo-koetsumugi-188"},
		})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	result, err := client.AgentRename(context.Background(), herdr.AgentRenameParams{
		Target: normalize.SafeName("continuo-test"),
		Name:   normalize.SafeName("continuo-koetsumugi-188"),
	})
	if err != nil {
		t.Fatalf("AgentRename が失敗した: %v", err)
	}
	if result.Agent.Name != "continuo-koetsumugi-188" {
		t.Fatalf("応答の name を読み取れていない: got %q", result.Agent.Name)
	}

	params := sentParams(t, fs, herdr.MethodAgentRename)
	if got := params["target"]; got != "continuo-test" {
		t.Fatalf("target が想定と違う: got %v", got)
	}
	if got := params["name"]; got != "continuo-koetsumugi-188" {
		t.Fatalf("name が想定と違う: got %v", got)
	}
	assertSchemaKeys(t, params, herdr.MethodAgentRename, agentRenameSchemaKeys)
}

// 目的: agent.rename に渡す新しい名前が herdr の agent 名パターンに収まらない場合、
// herdr へ接続する前に弾かれることを確認する（呼び出しは通ったのに名前が変わらない、
// という分かりにくい失敗を避けるため）。
// 与える情報: Target は正しく、Name にコロンを含む不正な名前を設定した AgentRenameParams。
// 成功条件: AgentRename がエラーを返し、偽サーバへの接続回数が0であること。
func TestAgentRename_新しい名前が不正なら接続せずに弾かれる(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		t.Errorf("新しい名前が不正なのに herdr へ接続してしまった（%d 回目）", n)
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})
	_, err := client.AgentRename(context.Background(), herdr.AgentRenameParams{
		Target: normalize.SafeName("continuo-test"),
		Name:   normalize.SafeName("Continuo:188"),
	})
	if err == nil {
		t.Fatalf("不正な名前なのにエラーが返らなかった")
	}
	if got := fs.ConnCount(); got != 0 {
		t.Fatalf("接続していないはずなのに接続回数が %d だった", got)
	}
}
