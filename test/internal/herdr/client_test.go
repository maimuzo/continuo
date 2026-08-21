package herdr_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// 目的: herdr へ送るリクエストが改行区切りの1行で送られることを確認する（設計 2-1）。
// 与える情報: agent.list を呼ぶだけの単純な呼び出し。
// 成功条件: 偽サーバが受け取った1行が、ちょうど1個の改行（末尾のみ）で終わっており、
// 行の中に別の改行が混ざっていないこと。
func TestCall_改行区切りの1行で送られる(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.AgentListResult{})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second, Startup: time.Second, Turn: time.Second})
	if _, err := client.AgentList(context.Background()); err != nil {
		t.Fatalf("AgentList が失敗した: %v", err)
	}

	lines := fs.Lines()
	if len(lines) != 1 {
		t.Fatalf("偽サーバが受け取った行数が想定と違う: got %d, want 1", len(lines))
	}
	line := lines[0]

	if len(line) == 0 || line[len(line)-1] != '\n' {
		t.Fatalf("リクエストの末尾が改行で終わっていない: %q", line)
	}
	body := line[:len(line)-1]
	if strings.Contains(string(body), "\n") {
		t.Fatalf("リクエストの本体に改行が混ざっている（複数行になっている）: %q", line)
	}
}

// 目的: id が文字列型で送られ、params が指定しない場合でも空オブジェクト {} になる
// ことを確認する（設計 2-1: 「id は文字列必須、params は空でも {} が要る」）。
// 与える情報: params を渡さない AgentList 呼び出し。
// 成功条件: 偽サーバが受け取った JSON を汎用的な map として解析したとき、
// id が string 型であり、params が空の JSON オブジェクト（{}）であること。
func TestCall_idが文字列でparamsが空オブジェクトになる(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.AgentListResult{})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second, Startup: time.Second, Turn: time.Second})
	if _, err := client.AgentList(context.Background()); err != nil {
		t.Fatalf("AgentList が失敗した: %v", err)
	}

	lines := fs.Lines()
	if len(lines) != 1 {
		t.Fatalf("偽サーバが受け取った行数が想定と違う: got %d, want 1", len(lines))
	}

	var generic map[string]any
	if err := json.Unmarshal(lines[0], &generic); err != nil {
		t.Fatalf("リクエストを汎用 JSON として解析できない: %v", err)
	}

	idValue, ok := generic["id"]
	if !ok {
		t.Fatalf("リクエストに id フィールドが無い: %s", lines[0])
	}
	if _, isString := idValue.(string); !isString {
		t.Fatalf("id が文字列型ではない: %T (%v)", idValue, idValue)
	}
	if idValue.(string) == "" {
		t.Fatalf("id が空文字である")
	}

	paramsValue, ok := generic["params"]
	if !ok {
		t.Fatalf("リクエストに params フィールドが無い（params を渡さなくても {} を送る必要がある）: %s", lines[0])
	}
	paramsMap, isMap := paramsValue.(map[string]any)
	if !isMap {
		t.Fatalf("params がオブジェクトではない: %T (%v)", paramsValue, paramsValue)
	}
	if len(paramsMap) != 0 {
		t.Fatalf("params を渡さなかったのに空オブジェクトになっていない: %v", paramsMap)
	}
}

// 目的: 1リクエストごとに毎回 connect し直していることを確認する
// （設計 2-1: 「1コネクション = 1リクエスト。応答を1行返した直後にサーバが
// コネクションを閉じる。コネクションプールを作れない」）。
// 与える情報: 同じ Client で AgentList を3回呼ぶ。
// 成功条件: 偽サーバが数えた接続回数が3であること（呼び出し回数と一致すること）。
func TestCall_1リクエストごとに接続し直す(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		writeResult(t, conn, req.ID, herdr.AgentListResult{})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second, Startup: time.Second, Turn: time.Second})
	for i := 0; i < 3; i++ {
		if _, err := client.AgentList(context.Background()); err != nil {
			t.Fatalf("%d回目の AgentList が失敗した: %v", i+1, err)
		}
	}

	if got := fs.ConnCount(); got != 3 {
		t.Fatalf("接続回数が呼び出し回数と一致しない: got %d, want 3（コネクションを使い回している疑い）", got)
	}
}

// 目的: herdr からの応答が読み取りタイムアウト（herdr.read_timeout_ms 相当）を
// 超えたとき、Client がハングせずにエラーを返すことを確認する。
// 与える情報: リクエストを受け取っても一切応答を書かない偽サーバと、短い読み取り
// タイムアウト（50ミリ秒）を設定した Client。
// 成功条件: AgentList がタイムアウト相当の時間内にエラーを返すこと
// （テスト全体のタイムアウトより十分早く戻ることで、ハングしていないと判定する）。
func TestCall_応答タイムアウトが効く(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		// 意図的に応答を書かない。接続はしばらく開けたままにする。
		time.Sleep(2 * time.Second)
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: 50 * time.Millisecond})

	start := time.Now()
	_, err := client.AgentList(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("応答が来ないのにエラーが返らなかった")
	}
	if elapsed > time.Second {
		t.Fatalf("タイムアウトが効いていない（%s かかった。設定は50ミリ秒）", elapsed)
	}
}

// 目的: 環境変数 HERDR_SOCKET_PATH が設定されていても、設定ファイルの値が使われることを
// 確認する（設計 2-1）。
//
// **これは実際に起きた事故の再発防止である。**環境変数を優先していたため、設定ファイルに
// 別の socket を書いても無視され、herdr の pane の中で動かす環境（この環境変数が常に入る）
// では設定ファイルで切り替える手段が一切なくなっていた。continuo は herdr で pane を作り
// worktree を消すので、利用者が指定した先とは別の herdr を操作しうる。
//
// 環境変数で切り替えたい利用者は、設定に ${HERDR_SOCKET_PATH} と書く（展開は 5-5 の規則で
// internal/config が行う）。
//
// 与える情報: HERDR_SOCKET_PATH を明示的に設定し、configured にも別の値を渡す。
// 成功条件: ResolveSocketPath が configured の値を返すこと（環境変数の値ではない）。
func TestResolveSocketPath_環境変数が設定されていても設定値を使う(t *testing.T) {
	t.Setenv(herdr.EnvSocketPath, "/from/env/herdr.sock")

	got, err := herdr.ResolveSocketPath("/from/config/herdr.sock")
	if err != nil {
		t.Fatalf("ResolveSocketPath が失敗した: %v", err)
	}
	if got != "/from/config/herdr.sock" {
		t.Fatalf("環境変数が設定値を上書きしている: got %q（設定に書いたパスが無視されている）", got)
	}
}

// 目的: 環境変数 HERDR_SOCKET_PATH が設定されていても、設定値が空なら既定値へ落ちることを
// 確認する（環境変数を読まないので、環境変数の値は既定値の決定にも影響しない）。
// 与える情報: HERDR_SOCKET_PATH に絶対パスを設定し、configured は空文字。HOME を明示する。
// 成功条件: 既定値 "<HOME>/.config/herdr/herdr.sock" が返ること。
func TestResolveSocketPath_設定値が空なら環境変数を無視して既定値へ落ちる(t *testing.T) {
	t.Setenv(herdr.EnvSocketPath, "/from/env/herdr.sock")
	t.Setenv("HOME", "/home/tester")

	got, err := herdr.ResolveSocketPath("")
	if err != nil {
		t.Fatalf("ResolveSocketPath が失敗した: %v", err)
	}
	want := "/home/tester/.config/herdr/herdr.sock"
	if got != want {
		t.Fatalf("既定値へ落ちていない: got %q, want %q", got, want)
	}
}

// unsetSocketPathEnv は環境変数 HERDR_SOCKET_PATH を「未設定」にする。
//
// t.Setenv で一度設定してからすぐ os.Unsetenv で消すのは、t.Setenv がテスト終了時の
// 復元を登録してくれるためである（並列テストとの併用も t.Setenv 側が禁止してくれる）。
// 「設定されているが空」と「未設定」は設計 5-5 で扱いが違う（前者はエラー）ので、
// 空文字を入れて代用してはならない。
func unsetSocketPathEnv(t *testing.T) {
	t.Helper()
	t.Setenv(herdr.EnvSocketPath, "placeholder")
	if err := os.Unsetenv(herdr.EnvSocketPath); err != nil {
		t.Fatalf("環境変数 %s を未設定にできません: %v", herdr.EnvSocketPath, err)
	}
}

// 目的: 環境変数 HERDR_SOCKET_PATH が無い場合、設定ファイルの値が使われることを確認する。
// 与える情報: HERDR_SOCKET_PATH を未設定にし、configured に絶対パスを渡す。
// 成功条件: ResolveSocketPath が configured の値をそのまま返すこと。
func TestResolveSocketPath_環境変数が無ければ設定値を使う(t *testing.T) {
	unsetSocketPathEnv(t)

	got, err := herdr.ResolveSocketPath("/from/config/herdr.sock")
	if err != nil {
		t.Fatalf("ResolveSocketPath が失敗した: %v", err)
	}
	if got != "/from/config/herdr.sock" {
		t.Fatalf("設定値が使われていない: got %q", got)
	}
}

// 目的: 環境変数も設定値も無い場合、既定値 ~/.config/herdr/herdr.sock が使われることを
// 確認する（設計 2-1）。
// 与える情報: HERDR_SOCKET_PATH を未設定にし、configured も空文字で呼ぶ。HOME を明示的に
// 設定する。
// 成功条件: ResolveSocketPath が "<HOME>/.config/herdr/herdr.sock" を返すこと。
func TestResolveSocketPath_どちらも無ければ既定値を使う(t *testing.T) {
	unsetSocketPathEnv(t)
	t.Setenv("HOME", "/home/tester")

	got, err := herdr.ResolveSocketPath("")
	if err != nil {
		t.Fatalf("ResolveSocketPath が失敗した: %v", err)
	}
	want := "/home/tester/.config/herdr/herdr.sock"
	if got != want {
		t.Fatalf("既定値が想定と違う: got %q, want %q", got, want)
	}
}

// 目的: 設定ファイルの herdr.socket が相対パスのときもエラーになることを確認する。
// 与える情報: HERDR_SOCKET_PATH を未設定にし、configured に相対パスを渡す。
// 成功条件: エラーになり、メッセージに設定キー名（herdr.socket）が含まれること。
func TestResolveSocketPath_設定値が相対パスならエラーになる(t *testing.T) {
	unsetSocketPathEnv(t)

	_, err := herdr.ResolveSocketPath("relative/herdr.sock")
	if err == nil {
		t.Fatalf("相対パスなのにエラーが返らなかった")
	}
	if !strings.Contains(err.Error(), "herdr.socket") {
		t.Fatalf("エラーメッセージに設定キー名が含まれていない: %v", err)
	}
}

// 目的: herdr が応答を途中まで書いて止まった場合に、JSON 解析の失敗ではなく
// タイムアウトとして報告されることを確認する（リトライの可否を判断する層が、
// 時間切れと壊れた応答を区別できるようにするため）。
// 与える情報: 応答を `{"id":"x","result":{"pan` まで書いて改行を書かずに止まる偽サーバと、
// 読み取りタイムアウト 200 ミリ秒の Client。
// 成功条件: PaneList がエラーを返し、そのメッセージが「タイムアウト」と言っていること
// （「JSON として解析できません」になっていないこと）。
func TestCall_応答が途中で切れたときタイムアウトとして報告される(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		if _, err := conn.Write([]byte(`{"id":"x","result":{"pan`)); err != nil {
			t.Errorf("途中までの応答を書けませんでした: %v", err)
			return
		}
		// 改行を書かないまま、クライアントのタイムアウトより長く保持する。
		time.Sleep(2 * time.Second)
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: 200 * time.Millisecond})

	_, err := client.PaneList(context.Background(), herdr.PaneListParams{})
	if err == nil {
		t.Fatalf("応答が途中で切れたのにエラーが返らなかった")
	}
	if !strings.Contains(err.Error(), "タイムアウト") {
		t.Fatalf("タイムアウトとして報告されていない: %v", err)
	}
}

// 目的: 応答の id を対応づけに使っていないことを確認する。
//
// **2026-08-18 の実測で、herdr はエラー応答の id を空文字で返すことが分かった**（設計 2-1）。
// 正常時は送った id がそのまま返るが、エラー時は返らないので、id では応答を対応づけられない。
// 1コネクション = 1リクエストで、接続したその場で1行だけ読むため、対応づける必要もない。
// 以前のクライアントは id が食い違うとエラーにしていたので、その挙動が消えていることを
// ここで固定する。
//
// 与える情報: 送られてきた id を無視し、常に別の id で成功応答を返す偽サーバ。
// 成功条件: AgentList が成功すること（id の食い違いでエラーにならないこと）。
func TestCall_応答のidを対応づけに使わない(t *testing.T) {
	const wrongID = "not-the-request-id"

	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		writeResult(t, conn, wrongID, herdr.AgentListResult{})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})

	if _, err := client.AgentList(context.Background()); err != nil {
		t.Fatalf("応答の id が食い違うだけでエラーになった（id は当てにしてはならない）: %v", err)
	}
}

// 目的: **エラー応答の id が空文字でも、エラーとして正しく扱えること**を確認する
// （設計 2-1 の実測。エラー応答は {"id": "", "error": {...}} の形で返る）。
// 与える情報: 実測どおり id を空文字にした code="invalid_request" のエラー応答を返す偽サーバ。
// 成功条件: AgentList がエラーを返し、herdr.IsCode でコードを判定でき、message が
// エラーメッセージに含まれること（id が空でも握りつぶされないこと）。
func TestCall_エラー応答のidが空でもエラーとして扱える(t *testing.T) {
	const wantMessage = "unknown method"

	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		// 実測の原文と同じ形（id は空文字）で返す。
		writeErrorResponse(t, conn, "", "invalid_request", wantMessage)
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: time.Second})

	_, err := client.AgentList(context.Background())
	if err == nil {
		t.Fatalf("エラー応答なのに error が返らなかった（id が空なので握りつぶされた疑い）")
	}
	if !herdr.IsCode(err, "invalid_request") {
		t.Fatalf("code で判定できるエラーになっていない: %v", err)
	}
	if !strings.Contains(err.Error(), wantMessage) {
		t.Fatalf("エラーメッセージに herdr の message が含まれていない: %v", err)
	}
}

// 目的: ctx に期限があれば、herdr.read_timeout_ms より長くても呼び出し側の期限が使われる
// ことを確認する（早いほうを採ると呼び出し側から延長できない）。
// 与える情報: herdr.read_timeout_ms 相当を 100 ミリ秒にした Client と、400 ミリ秒待ってから
// 応答する偽サーバ。ctx には 5 秒の期限を与える。
// 成功条件: AgentList が成功すること（100 ミリ秒で打ち切られないこと）。
func TestCall_ctxの期限が既定より長ければ延長できる(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, n int32, line []byte, conn net.Conn) {
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("偽サーバがリクエストを解析できませんでした: %v", err)
			return
		}
		time.Sleep(400 * time.Millisecond)
		writeResult(t, conn, req.ID, herdr.AgentListResult{})
	})

	client := herdr.New(fs.SocketPath(), herdr.Timeouts{Read: 100 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.AgentList(ctx); err != nil {
		t.Fatalf("ctx の期限まで待つはずが打ち切られた: %v", err)
	}
}
