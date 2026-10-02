package orchestrator_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// usage API（usage API）を主に読み、エラーのときは statusline取得へ切り替える（issue #284 の3本目）を確かめる。
//
// **本物の usage API は叩かない。**Reader の HTTP クライアントに台本の RoundTripper を渡し、
// 通信を1本も出さない（testing/synctest の中でも使える）。**本物の Keychain も読まない。**

// apiTokenEnv は台本の Reader がトークンを読む環境変数の名前である。
const apiTokenEnv = "CONTINUO_TEST_USAGE_API_TOKEN"

// apiScript は usage API の台本である（http.RoundTripper）。
type apiScript struct {
	mu sync.Mutex
	// calls は受けたリクエストの数である。
	calls int
	// respond は n 回目（1始まり）のリクエストへの応答を決める。
	respond func(n int, r *http.Request) (*http.Response, error)
}

// RoundTrip は台本どおりに応答する。
func (s *apiScript) RoundTrip(r *http.Request) (*http.Response, error) {
	s.mu.Lock()
	s.calls++
	n := s.calls
	f := s.respond
	s.mu.Unlock()
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	return f(n, r)
}

// set は応答を差し替える。
func (s *apiScript) set(f func(n int, r *http.Request) (*http.Response, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.respond = f
}

// Calls は受けたリクエストの数を返す。
func (s *apiScript) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// apiResponse は応答を1つ作る。
//
// status: HTTP の状態コード。
// body: 本文。
// header: 足すヘッダ（nil でよい）。
func apiResponse(status int, body string, header map[string]string) *http.Response {
	h := http.Header{"Content-Type": []string{"application/json"}}
	for k, v := range header {
		h.Set(k, v)
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(bytes.NewBufferString(body))}
}

// apiLimit は usage API の枠1件である（resetsAt が nil なら resets_at は null）。
func apiLimit(kind string, percent int, resetsAt *time.Time) map[string]any {
	m := map[string]any{"kind": kind, "percent": percent, "severity": "normal", "resets_at": nil}
	if resetsAt != nil {
		m["resets_at"] = resetsAt.UTC().Format(time.RFC3339Nano)
	}
	return m
}

// apiOK は 200 と枠の一覧を返す応答の関数を作る。
func apiOK(limits ...map[string]any) func(int, *http.Request) (*http.Response, error) {
	return func(int, *http.Request) (*http.Response, error) {
		raw, err := json.Marshal(map[string]any{"limits": limits})
		if err != nil {
			return nil, err
		}
		return apiResponse(http.StatusOK, string(raw), nil), nil
	}
}

// apiStatus は状態コードだけを返す応答の関数を作る。
func apiStatus(status int, header map[string]string) func(int, *http.Request) (*http.Response, error) {
	return func(int, *http.Request) (*http.Response, error) {
		return apiResponse(status, `{"error":"x"}`, header), nil
	}
}

// ptr は時刻のポインタを返す。
func ptr(t time.Time) *time.Time { return &t }

// newScriptedReader は台本の usage API を向いた Reader を作る。
//
// **環境変数を触るので、testing/synctest の bubble の外で呼ぶこと。**
//
// t: 呼び出し元のテスト。
// script: usage API の台本。
// token: トークン（空なら、環境変数が空で「恒久的な失敗」になる Reader）。
// 戻り値: 組み立てた Reader。
func newScriptedReader(t *testing.T, script *apiScript, token string) *ratelimit.Reader {
	t.Helper()
	t.Setenv(apiTokenEnv, token)
	reader, err := ratelimit.NewReader(ratelimit.Options{
		Config: config.RateLimitConfig{
			Source:      ratelimit.SourceOAuthUsageAPI,
			TokenSource: ratelimit.TokenSourceEnv,
			TokenEnv:    apiTokenEnv,
		},
		Endpoint:   "http://usage.invalid/api/oauth/usage",
		HTTPClient: &http.Client{Transport: script},
	})
	if err != nil {
		t.Fatalf("ratelimit.NewReader に失敗した: %v", err)
	}
	return reader
}

// newKeychainReader は、Keychain を読む Reader を、偽の `security` と短い期限で作る。
//
// **本物の Keychain を読まない。**PATH の先頭に偽の `security` を置く（bubble の外で呼ぶこと）。
//
// t: 呼び出し元のテスト。
// script: 偽の `security` の本体（sh）。
// timeout: `security` を待つ上限。
// 戻り値: 組み立てた Reader。
func newKeychainReader(t *testing.T, script *apiScript, securityBody string, timeout time.Duration) *ratelimit.Reader {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "security"), []byte("#!/bin/sh\n"+securityBody+"\n"), 0o700); err != nil {
		t.Fatalf("偽の security を書けません: %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	reader, err := ratelimit.NewReader(ratelimit.Options{
		Config: config.RateLimitConfig{
			Source:      ratelimit.SourceOAuthUsageAPI,
			TokenSource: ratelimit.TokenSourceKeychain,
		},
		Endpoint:        "http://usage.invalid/api/oauth/usage",
		HTTPClient:      &http.Client{Transport: script},
		KeychainTimeout: timeout,
	})
	if err != nil {
		t.Fatalf("ratelimit.NewReader に失敗した: %v", err)
	}
	return reader
}

// apiConfig は source: oauth_usage_api の既定に近い設定へ書き換える。
//
// **巡回の間隔は30秒・usage API を読む間隔と値の古さの上限は5分**（既定）にする。
func apiConfig(cfg *config.Config) {
	cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
	// **token_source を OS の既定に任せない。**既定は macOS が keychain、ほかが claude_credentials で、
	// Keychain の案内を WARN に足すかがこれで決まる（macOS でだけ通るテストを作らないため）。
	// Keychain の案内を確かめるテストは、mutate で keychain にする。
	cfg.RateLimit.TokenSource = ratelimit.TokenSourceClaudeCredentials
	cfg.RateLimit.PollIntervalMs = 300000
	cfg.RateLimit.RefreshIntervalMs = 300000
	cfg.Polling.IntervalMs = 30000
}

// newAPIFixture は、usage API の台本を持つ fixture を作る（テスト用herdr mock と手で進める時計）。
//
// **trust.repositories は空である**（statusline取得を開くと「使える clone が無い」で終わる。
// 開いたかどうかは、その WARN の数で見る）。
func newAPIFixture(t *testing.T, clock *testClock, reader *ratelimit.Reader, mutate func(cfg *config.Config)) *fixture {
	t.Helper()
	fx := newFixture(t, fixtureOptions{
		Now:       clock.Now,
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			apiConfig(cfg)
			if mutate != nil {
				mutate(cfg)
			}
		},
	})
	fx.AllowLog("usage API から statusline取得へ切り替えます", "使える clone が無い")
	return fx
}

// tickAndSettle は巡回を1回回し、statusline取得が走っていれば終わるまで待つ。
func tickAndSettle(t *testing.T, fx *fixture) {
	t.Helper()
	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "statusline取得が終わる", func() bool {
		return !fx.Orc.StatuslineFetchRunningForTest()
	})
}

// limitOf は写しから種別の枠を返す。
func limitOf(snap *ratelimit.Snapshot, kind string) (ratelimit.Limit, bool) {
	if snap == nil {
		return ratelimit.Limit{}, false
	}
	for _, l := range snap.Limits {
		if l.Kind == kind {
			return l, true
		}
	}
	return ratelimit.Limit{}, false
}

// 目的: usage API の resets_at が区切りの前後1秒以内で揺れても、ステータスラインの区切りちょうどの
// resets_at と同じ期間として扱い、使用率が下がらないことを確かめる（5時間と週の両方）。
//
// **実測（2026-09-28 23:30 JST）。**同じ期間を usage API は `18:59:59.662456Z` と `19:00:00.362779Z`、
// ステータスラインは `19:00:00Z` で返した。揺れたまま入れると「resets_at が違えば置き換え」に当たる。
//
// 与える情報: ステータスラインの新しい応答の行（5時間 8%・週 90%、区切りちょうど）のあと、
// usage API の値（5時間 9%・週 91%、区切りの0.34秒前と0.36秒後）。さらに usage API の古い値（5時間 7%）と、
// 止まったセッションの低い値（5時間 5%）の行。
// 成功条件: 保管値は 5時間 9%・週 91% で、resets_at はどちらも区切りちょうどのまま。
// 古い値でも低い値でも下がらないこと。
func TestQuotaAPI_resets_atの揺れを同じ期間として扱い値が下がらない(t *testing.T) {
	fx := newStubFixture(t, stubFixtureOptions{Mutate: apiConfig})
	t.Cleanup(fx.Close)
	boundary := time.Now().Add(3 * time.Hour).Truncate(time.Hour)
	week := boundary.Add(72 * time.Hour)

	fx.Orc.OnStatusline(slLine("pane-a", 100, slWin(8, boundary), slWin(90, week)))
	fx.Orc.OnStatusline(slLine("pane-a", 200, slWin(8, boundary), slWin(90, week)))
	fx.Orc.OnAPISnapshot(&ratelimit.Snapshot{Limits: []ratelimit.Limit{
		{Kind: handoff.LimitKindSession, Percent: 9, ResetsAt: ptr(boundary.Add(-338 * time.Millisecond))},
		{Kind: handoff.LimitKindWeeklyAll, Percent: 91, ResetsAt: ptr(week.Add(362 * time.Millisecond))},
	}})
	fx.Orc.OnAPISnapshot(&ratelimit.Snapshot{Limits: []ratelimit.Limit{
		{Kind: handoff.LimitKindSession, Percent: 7, ResetsAt: ptr(boundary.Add(362 * time.Millisecond))},
		{Kind: handoff.LimitKindWeeklyAll, Percent: 91, ResetsAt: ptr(week.Add(-338 * time.Millisecond))},
	}})
	fx.Orc.OnStatusline(slLine("pane-stopped", 50, slWin(5, boundary), slWin(80, week)))

	snap := fx.Orc.QuotaSnapshotForTest()
	for _, want := range []struct {
		kind    string
		percent int
		resets  time.Time
	}{
		{handoff.LimitKindSession, 9, boundary},
		{handoff.LimitKindWeeklyAll, 91, week},
	} {
		got, ok := limitOf(snap, want.kind)
		if !ok {
			t.Fatalf("%s が保管値に無い: %+v", want.kind, snap)
		}
		if got.Percent != want.percent || !got.ResetsAt.Equal(want.resets) {
			t.Errorf("%s が %d%% @ %s（want %d%% @ %s）", want.kind, got.Percent, got.ResetsAt, want.percent, want.resets)
		}
	}
}

// 目的: usage API の応答に無い期間は触らないこと、resets_at が null の期間の扱いを確かめる。
//
//   - ステータスラインから入った 100% の5時間は、5時間を持たない usage API の応答では消えない
//   - resets_at が null で 100% 未満の期間は「次に試してよい時刻 + polling.interval_ms」まで見え、
//     過ぎると見えなくなり、新しさが偽になる
//   - resets_at が null で 100% の期間は入らない（枠待ちの明ける時刻にされないため）
//
// 与える情報: 手で進める時計。ステータスラインの5時間 100% の行のあと、週だけの usage API の応答。
// 次に、週が null で 40%・5時間が null で 100% の usage API の応答。
// 成功条件: 5時間 100% が残り、null の週 40% は読める。5時間は null の 100% に置き換わらない。
// 時計を poll_interval_ms + polling.interval_ms より進めると、週が見えなくなり、入札の写しが nil になる。
func TestQuotaAPI_応答に無い期間は触らずnullの期間は次の読み取りまで見える(t *testing.T) {
	clock := newTestClock()
	fx := newAPIFixture(t, clock, nil, nil)
	now := clock.Now()
	boundary := now.Add(2 * time.Hour).Truncate(time.Minute)

	fx.Orc.OnStatusline(slLine("pane-a", 100, slWin(100, boundary), nil))
	fx.Orc.OnStatusline(slLine("pane-a", 200, slWin(100, boundary), nil))
	fx.Orc.OnAPISnapshot(&ratelimit.Snapshot{Limits: []ratelimit.Limit{
		{Kind: handoff.LimitKindWeeklyAll, Percent: 30, ResetsAt: ptr(now.Add(72 * time.Hour))},
	}})
	if got, ok := limitOf(fx.Orc.QuotaSnapshotForTest(), handoff.LimitKindSession); !ok || got.Percent != 100 {
		t.Fatalf("応答に無い5時間の 100%% が消えた: %+v", fx.Orc.QuotaSnapshotForTest())
	}

	fx.Orc.OnAPISnapshot(&ratelimit.Snapshot{Limits: []ratelimit.Limit{
		{Kind: handoff.LimitKindSession, Percent: 100, ResetsAt: nil},
		{Kind: handoff.LimitKindWeeklyAll, Percent: 40, ResetsAt: nil},
	}})
	snap := fx.Orc.QuotaSnapshotForTest()
	if got, ok := limitOf(snap, handoff.LimitKindSession); !ok || got.Percent != 100 || !got.ResetsAt.Equal(boundary) {
		t.Errorf("null の 100%% で5時間が置き換わった: %+v", got)
	}
	if got, ok := limitOf(snap, handoff.LimitKindWeeklyAll); !ok || got.Percent != 40 {
		t.Errorf("null の週 40%% が見えない: %+v", snap)
	}
	if fx.Orc.QuotaForBidForTest() == nil {
		t.Fatal("読めた直後なのに入札の写しが nil である")
	}

	clock.Advance(5*time.Minute + 30*time.Second + time.Second)
	if _, ok := limitOf(fx.Orc.QuotaSnapshotForTest(), handoff.LimitKindWeeklyAll); ok {
		t.Errorf("次の読み取りの時刻を過ぎても null の週が見えている: %+v", fx.Orc.QuotaSnapshotForTest())
	}
	if snap := fx.Orc.QuotaForBidForTest(); snap != nil {
		t.Errorf("null の期間の期限が過ぎたのに新しいままである: %+v", snap)
	}
}

// 目的: usage API が resets_at: null で返した期間の仮の期限が切れても、ほかの期間の値が新しければ
// 入札を止めないことを確かめる（issue #284。PR #294 の実装レビュー3周目）。
// ステータスラインは weekly_scoped を運ばないので、usage API から statusline取得へ切り替えると
// 仮の期限の weekly_scoped は置き換わらない。その切れを新しさの判定に数えると、新しい値が
// 届いていても入札を見送ってしまう。
// 与える情報: 手で進める時計。5時間・週が本物の resets_at、weekly_scoped が null の usage API の応答。
// 5分20秒後にステータスラインの新しい応答の行（5時間と週）。
// 成功条件: 仮の期限（5分30秒後）を過ぎても入札の写しが nil にならず、weekly_scoped は写しに入らない
// （入札は使用率0と読む）。
func TestQuotaAPI_nullの期間の仮の期限が切れても新しい値があれば入札を止めない(t *testing.T) {
	clock := newTestClock()
	fx := newAPIFixture(t, clock, nil, nil)
	now := clock.Now()
	five := now.Add(2 * time.Hour).Truncate(time.Minute)
	week := now.Add(72 * time.Hour).Truncate(time.Minute)

	fx.Orc.OnAPISnapshot(&ratelimit.Snapshot{Limits: []ratelimit.Limit{
		{Kind: handoff.LimitKindSession, Percent: 20, ResetsAt: ptr(five)},
		{Kind: handoff.LimitKindWeeklyAll, Percent: 30, ResetsAt: ptr(week)},
		{Kind: handoff.LimitKindWeeklyScoped, Percent: 0, ResetsAt: nil},
	}})
	clock.Advance(5*time.Minute + 20*time.Second)
	fx.Orc.OnStatusline(slLine("pane-a", 100, slWin(21, five), slWin(31, week)))
	fx.Orc.OnStatusline(slLine("pane-a", 200, slWin(21, five), slWin(31, week)))

	clock.Advance(20 * time.Second)
	snap := fx.Orc.QuotaForBidForTest()
	if snap == nil {
		t.Fatal("仮の期限が切れただけで、新しい値があるのに入札の写しが nil になった")
	}
	if _, ok := limitOf(snap, handoff.LimitKindWeeklyScoped); ok {
		t.Errorf("仮の期限が切れた weekly_scoped が入札の写しに残っている: %+v", snap)
	}
	if got, ok := limitOf(snap, handoff.LimitKindSession); !ok || got.Percent != 21 {
		t.Errorf("ステータスラインの5時間が写しに無い: %+v", snap)
	}
}

// apiFailure は、usage API の誤りを作る場合1つである。
type apiFailure struct {
	// name は場合の名前である。
	name string
	// reader は Reader を作る（bubble の外で呼ぶ）。2つ目の戻り値は、トークンを読めるように直す
	// 関数である（トークンの一時的な失敗のときだけ。それ以外は nil）。
	reader func(t *testing.T, script *apiScript) (*ratelimit.Reader, func())
	// respond は usage API の応答である（トークンの失敗では呼ばれない）。
	respond func(n int, r *http.Request) (*http.Response, error)
	// wait は、次に試してよい時刻までの長さである（恒久的な失敗なら 0 = 試さない）。
	wait time.Duration
	// permanent は恒久的な失敗か。
	permanent bool
}

// apiFailures は、切り替えになる usage API の誤りの一覧である（計画の「足すテスト」の3行目）。
func apiFailures(now time.Time) []apiFailure {
	token := func(t *testing.T, s *apiScript) (*ratelimit.Reader, func()) {
		return newScriptedReader(t, s, "test-token"), nil
	}
	date := now.Add(2 * time.Hour).Truncate(time.Second)
	return []apiFailure{
		{name: "sessionもweekly_allも無い200", reader: token,
			respond: apiOK(apiLimit(handoff.LimitKindWeeklyScoped, 3, ptr(now.Add(72*time.Hour)))), wait: 5 * time.Minute},
		{name: "429のRetry-Afterが秒", reader: token,
			respond: apiStatus(http.StatusTooManyRequests, map[string]string{"Retry-After": "3600"}), wait: time.Hour},
		{name: "429のRetry-AfterがHTTPの日付", reader: token,
			respond: apiStatus(http.StatusTooManyRequests, map[string]string{"Retry-After": date.UTC().Format(http.TimeFormat)}), wait: date.Sub(now)},
		{name: "429のRetry-Afterが25時間なら24時間", reader: token,
			respond: apiStatus(http.StatusTooManyRequests, map[string]string{"Retry-After": "90000"}), wait: 24 * time.Hour},
		{name: "401", reader: token, respond: apiStatus(http.StatusUnauthorized, nil), wait: 5 * time.Minute},
		{name: "403", reader: token, respond: apiStatus(http.StatusForbidden, nil), wait: 5 * time.Minute},
		{name: "5xx", reader: token, respond: apiStatus(http.StatusServiceUnavailable, nil), wait: 5 * time.Minute},
		{name: "通信の失敗", reader: token,
			respond: func(int, *http.Request) (*http.Response, error) { return nil, errors.New("connection refused") },
			wait:    5 * time.Minute},
		{name: "トークンの一時的な失敗", reader: func(t *testing.T, s *apiScript) (*ratelimit.Reader, func()) {
			// **目印のファイルが在ればトークンを返し、無ければ返ってこない `security`。**
			marker := filepath.Join(t.TempDir(), "ready")
			reader := newKeychainReader(t, s, "if [ ! -f "+marker+" ]; then exec sleep 30; fi\n"+
				`printf '%s' '{"claudeAiOauth":{"accessToken":"test-token"}}'`, 2*time.Second)
			// **期限は2秒にする。**読み直したあと偽の `security`（シェルの起動）が期限内に返ることを
			// 前提にするので、負荷の高い CI で 200ms を越えると一時的な失敗がもう1回起きて落ちる。
			// 失敗させる段は `sleep 30` なので、延びるのは期限の分だけである。
			return reader, func() {
				if err := os.WriteFile(marker, []byte("ready\n"), 0o600); err != nil {
					t.Fatalf("目印のファイルを置けません: %v", err)
				}
			}
		}, wait: 5 * time.Minute},
		{name: "トークンの恒久的な失敗", reader: func(t *testing.T, s *apiScript) (*ratelimit.Reader, func()) {
			return newScriptedReader(t, s, ""), nil
		}, permanent: true},
	}
}

// 目的: usage API がどの誤りでも statusline取得へ切り替え、次に試してよい時刻まで叩かず、
// 値が古ければ statusline取得を開くこと、次に 200 が返れば戻り、INFO を1行出すことを確かめる。
// 恒久的な失敗のあとは usage API を叩かないこと。
//
// 与える情報: 手で進める時計と、誤りを返す usage API の台本（場合ごと）。値は1度も入っていない。
// 成功条件: 巡回1回目で切り替わり（WARN は1行）、次に試してよい時刻が now + max(poll_interval_ms,
// Retry-After)（上限24時間）になり、statusline取得を開く（使える clone が無い WARN が出る）。
// 同じ時刻の巡回2回目では叩かない。次に試してよい時刻を過ぎて 200 を返すと、切り替えを解き
// INFO が1行出る。恒久的な失敗では、時刻を過ぎても叩かず、切り替えたままである。
func TestQuotaAPI_どの誤りでも切り替え次に試してよい時刻まで叩かず読めたら戻る(t *testing.T) {
	for _, tc := range apiFailures(time.Now()) {
		t.Run(tc.name, func(t *testing.T) {
			clock := newTestClock()
			tc = findFailure(t, apiFailures(clock.Now()), tc.name)
			script := &apiScript{respond: tc.respond}
			if script.respond == nil {
				script.respond = apiOK()
			}
			reader, heal := tc.reader(t, script)
			fx := newAPIFixture(t, clock, reader, nil)
			t0 := clock.Now()

			tickAndSettle(t, fx)
			st := fx.Orc.QuotaAPIStateForTest()
			if !st.Switched || st.LastOK {
				t.Fatalf("切り替わっていない: %+v", st)
			}
			if st.GaveUp != tc.permanent {
				t.Fatalf("恒久的な失敗の印が %v（want %v）", st.GaveUp, tc.permanent)
			}
			if !tc.permanent && !st.NextAt.Equal(t0.Add(tc.wait)) {
				t.Errorf("次に試してよい時刻が %s（want %s）", st.NextAt.Sub(t0), tc.wait)
			}
			if got := logCount(fx, "usage API から statusline取得へ切り替えます"); got != 1 {
				t.Errorf("切り替えの WARN が %d 行（want 1）:\n%s", got, fx.Logs.String())
			}
			if got := logCount(fx, "使える clone が無い"); got != 1 {
				t.Errorf("値が古いのに statusline取得を開いていない（clone の WARN が %d 行）", got)
			}
			calls := script.Calls()

			tickAndSettle(t, fx)
			if got := script.Calls(); got != calls {
				t.Errorf("次に試してよい時刻の前に usage API を叩いた: %d → %d", calls, got)
			}

			script.set(apiOK(apiLimit(handoff.LimitKindSession, 12, ptr(t0.Add(48*time.Hour)))))
			if tc.permanent {
				clock.Advance(25 * time.Hour)
				tickAndSettle(t, fx)
				if got := script.Calls(); got != calls {
					t.Errorf("恒久的な失敗のあとに usage API を叩いた: %d → %d", calls, got)
				}
				if st := fx.Orc.QuotaAPIStateForTest(); !st.Switched {
					t.Errorf("恒久的な失敗のあとに切り替えが解けた: %+v", st)
				}
				return
			}
			if heal != nil {
				heal()
			}
			clock.Advance(tc.wait)
			tickAndSettle(t, fx)
			if st := fx.Orc.QuotaAPIStateForTest(); st.Switched || !st.LastOK || !st.EverRead {
				t.Errorf("200 が返ったのに戻っていない: %+v", st)
			}
			if got := logCount(fx, "切り替えを解きます"); got != 1 {
				t.Errorf("戻った INFO が %d 行（want 1）", got)
			}
			if fx.Orc.QuotaForBidForTest() == nil {
				t.Error("200 が返ったのに入札の写しが nil である")
			}
		})
	}
}

// findFailure は名前で場合を探す（時計を作り直した場合の一覧から選び直すため）。
func findFailure(t *testing.T, all []apiFailure, name string) apiFailure {
	t.Helper()
	for _, f := range all {
		if f.name == name {
			return f
		}
	}
	t.Fatalf("場合 %q が無い", name)
	return apiFailure{}
}

// 目的: トークンの一時的な失敗が6回続いても諦めない（切り替えたまま、poll_interval_ms ごとに試し直す）
// ことを確かめる。ba24db63 までは5回で諦めていた。
//
// 与える情報: 返ってこない偽の `security`（期限 200ms）と手で進める時計。
// 成功条件: 6回とも試し（`security` を起こし）、恒久的な失敗の印が立たず、7回目の前にも
// 次に試してよい時刻が now + poll_interval_ms であること。
func TestQuotaAPI_トークンの一時的な失敗が6回続いても諦めない(t *testing.T) {
	clock := newTestClock()
	script := &apiScript{respond: apiOK()}
	// **token_source を keychain に固定する。**Keychain の案内を WARN に足すかは orchestrator の
	// 設定で決まり、既定は OS で変わる（macOS 以外は claude_credentials）。固定しないと Linux で落ちる。
	fx := newAPIFixture(t, clock, newKeychainReader(t, script, "exec sleep 30", 200*time.Millisecond),
		func(cfg *config.Config) { cfg.RateLimit.TokenSource = ratelimit.TokenSourceKeychain })

	for i := 1; i <= 6; i++ {
		fx.Orc.PollAPIForTest(context.Background())
		st := fx.Orc.QuotaAPIStateForTest()
		if st.GaveUp {
			t.Fatalf("%d 回目で諦めた", i)
		}
		if !st.NextAt.Equal(clock.Now().Add(5 * time.Minute)) {
			t.Fatalf("%d 回目: 次に試してよい時刻が now + poll_interval_ms でない: %s", i, st.NextAt.Sub(clock.Now()))
		}
		clock.Advance(5 * time.Minute)
	}
	if got := logCount(fx, "usage API から statusline取得へ切り替えます"); got != 1 {
		t.Errorf("切り替えの WARN が %d 行（want 1。切り替えた1回だけ）", got)
	}
	if got := logCount(fx, "allow-keychain-access"); got < 1 {
		t.Errorf("一時的な失敗の WARN に Keychain の案内が無い:\n%s", fx.Logs.String())
	}
}

// 目的: 新しさの幅を確かめる。usage API が読めた直後は max(refresh_interval_ms, poll_interval_ms +
// polling.interval_ms) のあいだ statusline取得を開かず、誤りへ変わると幅が refresh_interval_ms へ縮み、
// 値が古ければすぐ開くこと。
//
// 与える情報: 手で進める時計。1回目は 200、2回目からは 500 を返す usage API。
// refresh_interval_ms = poll_interval_ms = 5分、polling.interval_ms = 30秒。
// 成功条件: 読めた直後の幅は 5分30秒。5分10秒後（usage API の次の読み取りの前）は新しく、開かない。
// 5分10秒後に usage API が 500 を返すと幅は5分へ縮み、その巡回で statusline取得を開き、入札の写しが nil になる。
func TestQuotaAPI_読めた直後は幅が広く誤りへ変わると縮んですぐ開く(t *testing.T) {
	clock := newTestClock()
	script := &apiScript{respond: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return apiOK(apiLimit(handoff.LimitKindSession, 10, ptr(time.Now().Add(4*time.Hour))))(n, r)
		}
		return apiResponse(http.StatusInternalServerError, `{}`, nil), nil
	}}
	fx := newAPIFixture(t, clock, newScriptedReader(t, script, "test-token"), nil)

	tickAndSettle(t, fx)
	if got := fx.Orc.QuotaRefreshIntervalForTest(); got != 5*time.Minute+30*time.Second {
		t.Fatalf("読めた直後の幅が %s（want 5m30s）", got)
	}
	clock.Advance(4 * time.Minute)
	tickAndSettle(t, fx)
	if got := logCount(fx, "使える clone が無い"); got != 0 {
		t.Fatalf("読めているのに statusline取得を開いた")
	}

	clock.Advance(time.Minute + 10*time.Second)
	if fx.Orc.QuotaForBidForTest() == nil {
		t.Fatal("5分10秒後（幅の中）なのに入札の写しが nil である")
	}
	tickAndSettle(t, fx)
	if got := fx.Orc.QuotaRefreshIntervalForTest(); got != 5*time.Minute {
		t.Errorf("誤りへ変わったのに幅が %s（want 5m）", got)
	}
	if got := logCount(fx, "使える clone が無い"); got != 1 {
		t.Errorf("幅が縮んで古くなったのに statusline取得を開いていない（clone の WARN が %d 行）", got)
	}
	if snap := fx.Orc.QuotaForBidForTest(); snap != nil {
		t.Errorf("古くなったのに入札の写しがある: %+v", snap)
	}
}

// 目的: usage API が読めなくなっても、新しさの幅（refresh_interval_ms）のあいだは入札し、
// 過ぎたら入札を止めることを確かめる（設計 3-77i。ba24db63 の TestHandoff_枠が読めなくなったら入札を止める
// を、切り替えの形に直したもの）。
//
// **古い値で入札させない。**資格情報が切れた機械は、切れる直前の「使用率 5%」を持ち続ける。
// それで入札すると、正直に読めている機械に必ず勝つ。
//
// 与える情報: 手で進める時計。1回目だけ 5% を返し、2回目からは 500 を返す usage API。
// refresh_interval_ms = 20分。巡回1回目で issue 188、5分10秒後の巡回（usage API が 500）で issue 189、
// さらに16分後の巡回で issue 190 を候補にする。
// 成功条件: 188 と 189 には入札があり、190 には入札が1件も無いこと。
func TestQuotaAPI_読めなくなっても新しさの幅のあいだは入札し過ぎたら止める(t *testing.T) {
	clock := newTestClock()
	script := &apiScript{respond: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return apiOK(apiLimit(handoff.LimitKindSession, 5, ptr(time.Now().Add(4*time.Hour))))(n, r)
		}
		return apiResponse(http.StatusInternalServerError, `{}`, nil), nil
	}}
	fx := newAPIFixture(t, clock, newScriptedReader(t, script, "test-token"), func(cfg *config.Config) {
		cfg.RateLimit.RefreshIntervalMs = 1200000
		// **締め切りを待たせる。**待たせないと1回目の巡回で担当者になり、
		// スロットが埋まって次の候補を見なくなる。
		cfg.Tracker.Provider.Handoff.BidWindowMs = 3600000
	})
	holdPrompt(fx)
	bids := func(n int) int {
		return len(fx.Tracker.MarkedHandoffCommentsOf(issueNode(n), config.HandoffBidMarker))
	}

	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	tickAndSettle(t, fx)
	if got := bids(188); got != 1 {
		t.Fatalf("usage API が読めているのに入札していない: %d 件", got)
	}

	clock.Advance(5*time.Minute + 10*time.Second)
	fx.Tracker.AddIssue(sampleIssue(189, "Ready"))
	tickAndSettle(t, fx)
	if st := fx.Orc.QuotaAPIStateForTest(); !st.Switched {
		t.Fatalf("前提: 500 で切り替わっていない: %+v", st)
	}
	if got := bids(189); got != 1 {
		t.Errorf("新しさの幅のあいだなのに入札していない: %d 件", got)
	}

	clock.Advance(16 * time.Minute)
	fx.Tracker.AddIssue(sampleIssue(190, "Ready"))
	tickAndSettle(t, fx)
	if got := bids(190); got != 0 {
		t.Errorf("新しさの幅を過ぎたのに古い値で入札している: %d 件", got)
	}
}

// 目的: 成功した 200 に weekly_scoped が無ければ保管値から消え、session と weekly_all は
// 応答に無くても残ることを確かめる。weekly_scoped が複数あれば使用率が最大の行の resets_at を採ること。
//
// 与える情報: weekly_scoped を2件（30% と 80%）持つ応答、次に weekly_all だけの応答。
// session はステータスラインから入れておく。
// 成功条件: 1回目のあと weekly_scoped は 80% で、80% の行の resets_at を持つ。2回目のあと
// weekly_scoped が消え、session は残っていること。
func TestQuotaAPI_weekly_scopedは応答に無ければ消え複数なら最大を採る(t *testing.T) {
	fx := newStubFixture(t, stubFixtureOptions{Mutate: apiConfig})
	t.Cleanup(fx.Close)
	now := time.Now()
	feedFreshQuota(fx.Orc, "pane-a", now, 10, 20)
	scopedLow := now.Add(100 * time.Hour).Truncate(time.Minute)
	scopedHigh := now.Add(50 * time.Hour).Truncate(time.Minute)

	fx.Orc.OnAPISnapshot(&ratelimit.Snapshot{Limits: []ratelimit.Limit{
		{Kind: handoff.LimitKindWeeklyScoped, Percent: 30, ResetsAt: ptr(scopedLow)},
		{Kind: handoff.LimitKindWeeklyScoped, Percent: 80, ResetsAt: ptr(scopedHigh)},
		{Kind: handoff.LimitKindWeeklyAll, Percent: 20, ResetsAt: ptr(now.Add(72 * time.Hour))},
	}})
	got, ok := limitOf(fx.Orc.QuotaSnapshotForTest(), handoff.LimitKindWeeklyScoped)
	if !ok || got.Percent != 80 || !got.ResetsAt.Equal(scopedHigh) {
		t.Fatalf("weekly_scoped が最大の行になっていない: %+v", got)
	}

	fx.Orc.OnAPISnapshot(&ratelimit.Snapshot{Limits: []ratelimit.Limit{
		{Kind: handoff.LimitKindWeeklyAll, Percent: 21, ResetsAt: ptr(now.Add(72 * time.Hour))},
	}})
	snap := fx.Orc.QuotaSnapshotForTest()
	if _, ok := limitOf(snap, handoff.LimitKindWeeklyScoped); ok {
		t.Errorf("応答に無い weekly_scoped が残っている: %+v", snap)
	}
	if _, ok := limitOf(snap, handoff.LimitKindSession); !ok {
		t.Errorf("応答に無い session が消えた: %+v", snap)
	}
}

// 目的: 止めるときの取り消しでは、トークンでも HTTP でも切り替えず、WARN を出さないことを確かめる。
//
// 与える情報: 呼ぶ前に取り消したコンテキスト。HTTP の段で取り消される台本の Reader と、
// Keychain の段（返ってこない偽の `security`）で取り消される Reader。
// 成功条件: どちらも切り替えず、次に試してよい時刻も動かず、切り替えの WARN が1行も出ないこと。
func TestQuotaAPI_止めるときの取り消しでは切り替えずWARNも出さない(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reader func(t *testing.T, s *apiScript) *ratelimit.Reader
	}{
		{"HTTP", func(t *testing.T, s *apiScript) *ratelimit.Reader { return newScriptedReader(t, s, "test-token") }},
		{"トークン", func(t *testing.T, s *apiScript) *ratelimit.Reader {
			return newKeychainReader(t, s, "exec sleep 30", 10*time.Second)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := newTestClock()
			script := &apiScript{respond: apiStatus(http.StatusInternalServerError, nil)}
			fx := newAPIFixture(t, clock, tc.reader(t, script), nil)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			fx.Orc.PollAPIForTest(ctx)

			if st := fx.Orc.QuotaAPIStateForTest(); st.Switched || !st.NextAt.IsZero() {
				t.Errorf("取り消しで切り替えた: %+v", st)
			}
			if got := logCount(fx, "usage API"); got != 0 {
				t.Errorf("取り消しで usage API の WARN を出した:\n%s", fx.Logs.String())
			}
		})
	}
}

// 目的: weekly_scoped の使用率が「100 の期間」の判定を止めず、**余裕が無ければ**切り替え中でも
// 開かないことを確かめる。
//
// **線は入札の余裕値と同じ1本である**（人間の決定。2026-09-06。issue #173）。
// **`pause_above_percent` はキーごと消えた。**
//
// 与える情報: usage API が 401 を返し続ける（切り替えている）。値は古い。weekly_scoped が
// 100%（1週間のマージン0 → 余裕値0…ではなく `100 − 100 − 0 = 0` で0以下なので開かない）の場合と、
// 85%（マージン10 → 余裕値5で余裕あり）の場合と、96%（マージン10 → 余裕値 −6）の場合。
// 成功条件: 余裕がある場合だけ開くこと（clone の WARN が出る）。
func TestQuotaAPI_weekly_scopedに余裕があるときだけstatusline取得を開く(t *testing.T) {
	for _, tc := range []struct {
		name    string
		percent int
		margin  int
		opens   bool
	}{
		{"85でマージン10なら開く", 85, 10, true},
		{"96でマージン10なら開かない", 96, 10, false},
		{"100でマージン0でも開かない", 100, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := newTestClock()
			script := &apiScript{respond: apiStatus(http.StatusUnauthorized, nil)}
			fx := newAPIFixture(t, clock, newScriptedReader(t, script, "test-token"), func(cfg *config.Config) {
				cfg.Tracker.Provider.Handoff.WeeklyMarginPercent = tc.margin
			})
			fx.Orc.OnAPISnapshot(&ratelimit.Snapshot{Limits: []ratelimit.Limit{
				{Kind: handoff.LimitKindSession, Percent: 10, ResetsAt: ptr(clock.Now().Add(4 * time.Hour))},
				{Kind: handoff.LimitKindWeeklyScoped, Percent: tc.percent, ResetsAt: ptr(clock.Now().Add(50 * time.Hour))},
			}})
			clock.Advance(10 * time.Minute)

			tickAndSettle(t, fx)
			opened := logCount(fx, "使える clone が無い") > 0
			if opened != tc.opens {
				t.Errorf("statusline取得を開いたか: %v（want %v）\n%s", opened, tc.opens, fx.Logs.String())
			}
		})
	}
}

// 目的: quota.json が weekly_scoped を含めて書かれ、source が oauth_usage_api のときだけ読み戻される
// ことを確かめる。
//
// 与える情報: oauth_usage_api の fixture で weekly_scoped を含む usage API の値を入れる。同じ実行時
// ディレクトリで、oauth_usage_api と statusline の fixture を作り、起動時の準備（PrepareStatusline）を呼ぶ。
// 成功条件: quota.json に weekly_scoped があり、oauth_usage_api では読み戻され、statusline では
// 読み戻されない（session は両方で読み戻される）こと。
func TestQuotaAPI_quota_jsonはweekly_scopedを含めて書きoauth_usage_apiのときだけ読み戻す(t *testing.T) {
	root := t.TempDir()
	writer := newStubFixture(t, stubFixtureOptions{Root: root, Mutate: apiConfig})
	now := time.Now()
	writer.Orc.OnAPISnapshot(&ratelimit.Snapshot{Limits: []ratelimit.Limit{
		{Kind: handoff.LimitKindSession, Percent: 10, ResetsAt: ptr(now.Add(4 * time.Hour))},
		{Kind: handoff.LimitKindWeeklyScoped, Percent: 33, ResetsAt: ptr(now.Add(50 * time.Hour))},
	}})
	writer.Close()
	raw, err := os.ReadFile(filepath.Join(root, "quota.json"))
	if err != nil {
		t.Fatalf("quota.json を読めません: %v", err)
	}
	if !strings.Contains(string(raw), handoff.LimitKindWeeklyScoped) {
		t.Fatalf("quota.json に weekly_scoped が無い: %s", raw)
	}

	for _, tc := range []struct {
		source     string
		wantScoped bool
	}{
		{ratelimit.SourceOAuthUsageAPI, true},
		{ratelimit.SourceStatusline, false},
	} {
		t.Run(tc.source, func(t *testing.T) {
			fx := newStubFixture(t, stubFixtureOptions{Root: root, Mutate: func(cfg *config.Config) {
				apiConfig(cfg)
				cfg.RateLimit.Source = tc.source
			}})
			t.Cleanup(fx.Close)
			fx.Orc.PrepareStatusline(context.Background())
			snap := fx.Orc.QuotaSnapshotForTest()
			if _, ok := limitOf(snap, handoff.LimitKindSession); !ok {
				t.Errorf("session が読み戻されない: %+v", snap)
			}
			if _, ok := limitOf(snap, handoff.LimitKindWeeklyScoped); ok != tc.wantScoped {
				t.Errorf("weekly_scoped を読み戻したか: %v（want %v）", ok, tc.wantScoped)
			}
			if st := fx.Orc.QuotaAPIStateForTest(); st.EverRead {
				t.Error("quota.json から読み戻した値で「1度でも読めた」が立った")
			}
		})
	}
}

// 目的: source が statusline なら usage API を叩かず、none なら usage API も statusline も動かないことを確かめる。
//
// 与える情報: 台本の Reader を渡した fixture（source は statusline と none）。値は無い。
// 成功条件: どちらも usage API を1回も叩かない。none では statusline取得も開かないこと。
func TestQuotaAPI_statuslineならweb_APIを叩かずnoneなら何も動かない(t *testing.T) {
	for _, source := range []string{ratelimit.SourceStatusline, ratelimit.SourceNone} {
		t.Run(source, func(t *testing.T) {
			clock := newTestClock()
			script := &apiScript{respond: apiOK(apiLimit(handoff.LimitKindSession, 1, ptr(time.Now().Add(time.Hour))))}
			fx := newAPIFixture(t, clock, newScriptedReader(t, script, "test-token"), func(cfg *config.Config) {
				cfg.RateLimit.Source = source
			})
			tickAndSettle(t, fx)
			if got := script.Calls(); got != 0 {
				t.Errorf("source: %s なのに usage API を %d 回叩いた", source, got)
			}
			if source == ratelimit.SourceNone && logCount(fx, "statusline取得") != 0 {
				t.Errorf("source: none なのに statusline取得を開いた:\n%s", fx.Logs.String())
			}
		})
	}
}

// 目的: oauth_usage_api で statusline を使えない（sl.sock が長すぎて空のパス・開けなくて
// DisableStatusline）ときは、切り替えても statusline取得を開かないことを確かめる。
// statusLine を書かないことは TestSettings_statusLineはnone以外でstatuslineを使えるときだけ入りhookの部分は変わらない が見る。
//
// 与える情報: usage API が 401 を返し続け、値が無い。DisableStatusline を呼んだ fixture。
// 成功条件: 切り替わるが、statusline取得を開かず、WARN は「statusline も使えない」の文であること。
func TestQuotaAPI_statuslineを使えなければ切り替えてもstatusline取得を開かない(t *testing.T) {
	clock := newTestClock()
	script := &apiScript{respond: apiStatus(http.StatusUnauthorized, nil)}
	fx := newAPIFixture(t, clock, newScriptedReader(t, script, "test-token"), nil)
	fx.AllowLog("statusline も使えない")
	fx.Orc.DisableStatusline()

	tickAndSettle(t, fx)
	if st := fx.Orc.QuotaAPIStateForTest(); !st.Switched {
		t.Fatalf("切り替わっていない: %+v", st)
	}
	if got := logCount(fx, "使える clone が無い"); got != 0 {
		t.Errorf("statusline を使えないのに statusline取得を開いた")
	}
	if got := logCount(fx, "statusline も使えない"); got != 1 {
		t.Errorf("statusline を使えないことの WARN が %d 行（want 1）", got)
	}
}

// 目的: oauth_usage_api で refresh_interval_ms が polling.interval_ms 以下なら、起動時（orchestrator.New）に
// 1回だけ WARN を出し、polling.interval_ms の2倍として扱うことを確かめる。
//
// 与える情報: refresh_interval_ms = polling.interval_ms = 30秒の設定。巡回を2回。
// 成功条件: WARN が1行だけ出て、新しさの幅が1分であること（usage API は読めていない）。
func TestQuotaAPI_refreshが巡回の間隔以下なら起動時に1回だけWARNを出し2倍として扱う(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now, Mutate: func(cfg *config.Config) {
		apiConfig(cfg)
		cfg.RateLimit.RefreshIntervalMs = 30000
	}})
	fx.AllowLog("polling.interval_ms の2倍として扱います")
	fx.Orc.Tick(context.Background())
	fx.Orc.Tick(context.Background())
	if got := logCount(fx, "polling.interval_ms の2倍として扱います"); got != 1 {
		t.Errorf("WARN が %d 行（want 1）", got)
	}
	if got := fx.Orc.QuotaRefreshIntervalForTest(); got != time.Minute {
		t.Errorf("新しさの幅が %s（want 1m）", got)
	}
}

// ===== 取得止め（testing/synctest の偽の時計） =====

// newAPIFetchStubFixture は、usage API の台本と信頼済みの clone を持つ stub の fixture を作る
// （statusline取得を走らせられる）。**bubble の中で呼ぶこと。終わる前に Close を呼ぶこと。**
func newAPIFetchStubFixture(t *testing.T, clone trustedClone, ws *memWorkspaceHerdr, reader *ratelimit.Reader, root string) *stubFixture {
	t.Helper()
	return newStubFixture(t, stubFixtureOptions{
		Logs:           true,
		Root:           root,
		WorkspaceHerdr: ws,
		HomeDir:        clone.Home,
		RateLimit:      reader,
		GhqList: func(_ context.Context, owner, repo string) (string, error) {
			if owner+"/"+repo == "octocat/hello-world" {
				return clone.Dir, nil
			}
			return "", nil
		},
		Mutate: func(cfg *config.Config) {
			apiConfig(cfg)
			cfg.Trust.Repositories = []string{"octocat/hello-world"}
		},
	})
}

// stopWarns は取得止めの WARN の行数を返す。
func stopWarns(fx *stubFixture) int {
	return fx.countLog("statusline取得を止めます")
}

// noValueScript は、hello に応答するが使用率の無い行を送る台本である（fetchNoValue になる）。
func noValueScript(fx *stubFixture) stubSLScript {
	return stubSLScript{Prompt: func(context.Context, herdr.AgentPromptParams) (*herdr.AgentPromptResult, error) {
		fx.Orc.OnStatusline(slLine(slSessionOf(fx, len(fx.Herdr.SLStarts())-1), 900, nil, nil))
		return &herdr.AgentPromptResult{Type: "agent_prompted"}, nil
	}}
}

// 目的: 1度も読めていないとき、usage API のどの誤りで切り替えていても、statusline取得が
// fetchNoValue（応答はあったが値が無い）で終われば取得止めになり、以後開かず、取得止めの WARN だけを
// 1回出すことを確かめる（API キーの機械で課金を1回で止める）。
//
// 与える情報: 誤りを返す usage API（session も weekly_all も無い 200・429・401・403・5xx・通信の失敗・
// トークンの恒久的な失敗）。値の無い行を送る Claude Code。
// 成功条件: 取得止めが立ち、取得止めの WARN が1行、「statusline取得ができません」の WARN が0行で、
// 値の古さの上限を3回過ぎても2回目の statusline取得を開かないこと。
//
// **トークンの一時的な失敗は TestQuotaAPI_トークンの一時的な失敗でも値が届かなければ取得止めになる が見る**
// （偽の `security` を起こすので、偽の時計の中では期限が進まない）。
func TestQuotaAPIStop_どの誤りでも値の無い取得で1度も読めていなければ止まる(t *testing.T) {
	clone := newTrustedClone(t)
	for _, tc := range apiFailures(time.Now()) {
		if tc.name == "トークンの一時的な失敗" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			script := &apiScript{respond: tc.respond}
			if script.respond == nil {
				script.respond = apiOK()
			}
			reader, _ := tc.reader(t, script)
			root := t.TempDir()
			synctest.Test(t, func(t *testing.T) {
				ws := newMemWorkspaceHerdr()
				fx := newAPIFetchStubFixture(t, clone, ws, reader, root)
				defer fx.Close()
				fx.Herdr.SetSL(noValueScript(fx))

				startFetch(fx)
				time.Sleep(3*time.Minute + time.Second)
				synctest.Wait()

				if st := fx.Orc.QuotaAPIStateForTest(); !st.FetchStopped {
					t.Fatalf("取得止めになっていない: %+v\n%s", st, fx.Logs.String())
				}
				if got := stopWarns(fx); got != 1 {
					t.Errorf("取得止めの WARN が %d 行（want 1）", got)
				}
				if got := fetchWarns(fx); len(got) != 0 {
					t.Errorf("取得止めなのにふだんの WARN も出た: %q", got)
				}
				for i := 0; i < 3; i++ {
					time.Sleep(6 * time.Minute)
					startFetch(fx)
				}
				if got := len(fx.Herdr.SLStarts()); got != 1 {
					t.Errorf("取得止めのあとも statusline取得を開いた: %d 回", got)
				}
			})
		})
	}
}

// 目的: 値の届かなかった取得のうち、fetchNoLine・送ったあとの fetchMidwayError・送ったあとの期限切れ
// でも取得止めになることを確かめる（1度も読めておらず、401 で切り替えているとき）。
//
// 与える情報: 401 を返し続ける usage API。Claude Code は、行を1行も送らない・AgentPrompt が誤りを返す・
// AgentPrompt が全体の上限を越えて返らない、の3通り。
// 成功条件: どれも取得止めになり、取得止めの WARN が1行だけ出ること。
func TestQuotaAPIStop_値の届かなかった取得のどの結果でも止まる(t *testing.T) {
	clone := newTrustedClone(t)
	for _, tc := range []struct {
		name   string
		script func(fx *stubFixture) stubSLScript
	}{
		{"fetchNoLine", func(*stubFixture) stubSLScript { return stubSLScript{} }},
		{"送ったあとのfetchMidwayError", func(*stubFixture) stubSLScript {
			return stubSLScript{Prompt: func(context.Context, herdr.AgentPromptParams) (*herdr.AgentPromptResult, error) {
				return nil, errors.New("agent.prompt が落ちた")
			}}
		}},
		{"送ったあとの期限切れ", func(fx *stubFixture) stubSLScript {
			startup := time.Duration(fx.Config.Herdr.StartupTimeoutMs) * time.Millisecond
			return stubSLScript{Prompt: func(ctx context.Context, _ herdr.AgentPromptParams) (*herdr.AgentPromptResult, error) {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(2*startup + 4*time.Minute):
					return &herdr.AgentPromptResult{Type: "agent_prompted"}, nil
				}
			}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := &apiScript{respond: apiStatus(http.StatusUnauthorized, nil)}
			reader := newScriptedReader(t, script, "test-token")
			root := t.TempDir()
			synctest.Test(t, func(t *testing.T) {
				ws := newMemWorkspaceHerdr()
				fx := newAPIFetchStubFixture(t, clone, ws, reader, root)
				defer fx.Close()
				fx.Herdr.SetSL(tc.script(fx))

				startFetch(fx)
				time.Sleep(10 * time.Minute)
				synctest.Wait()

				if st := fx.Orc.QuotaAPIStateForTest(); !st.FetchStopped {
					t.Fatalf("取得止めになっていない: %+v\n%s", st, fx.Logs.String())
				}
				if got := stopWarns(fx); got != 1 {
					t.Errorf("取得止めの WARN が %d 行（want 1）", got)
				}
			})
		})
	}
}

// 目的: 取得止めを解く経路を確かめる。usage API が読めたら解く。pane の run から使用率を持つ行が
// 届いたら（保管値と同じ値の行でも）解く。立て直すと解ける。
//
// 与える情報: 401 の usage API で切り替え、値の無い取得で取得止めにしたあと、(1) usage API が 200 を返す、
// (2) quota.json から戻した値と同じ値の pane の行が届く、(3) 同じ実行時ディレクトリで作り直す、の3通り。
// 成功条件: どれも取得止めが解けること。(3) は作り直した直後の巡回で statusline取得を開くこと。
func TestQuotaAPIStop_読めたら解き立て直すと解ける(t *testing.T) {
	clone := newTrustedClone(t)
	for _, name := range []string{"web_APIが読めた", "同じ値のpaneの行", "立て直し"} {
		t.Run(name, func(t *testing.T) {
			script := &apiScript{respond: apiStatus(http.StatusUnauthorized, nil)}
			reader := newScriptedReader(t, script, "test-token")
			root := t.TempDir()
			resets := time.Now().Add(4 * time.Hour).Truncate(time.Second)
			if name == "同じ値のpaneの行" {
				// **quota.json から戻した値は「1度でも読めた」に数えない。**同じ値の行で保管値が
				// 変わらなくても印が立つことを見るため、先に置いておく。
				quota := `{"session":{"percent":40,"resets_at":"` + resets.UTC().Format(time.RFC3339) + `"}}`
				if err := os.WriteFile(filepath.Join(root, "quota.json"), []byte(quota), 0o600); err != nil {
					t.Fatalf("quota.json を書けません: %v", err)
				}
			}
			synctest.Test(t, func(t *testing.T) {
				ws := newMemWorkspaceHerdr()
				fx := newAPIFetchStubFixture(t, clone, ws, reader, root)
				fx.Orc.PrepareStatusline(context.Background())
				fx.Herdr.SetSL(noValueScript(fx))
				startFetch(fx)
				time.Sleep(3*time.Minute + time.Second)
				synctest.Wait()
				if st := fx.Orc.QuotaAPIStateForTest(); !st.FetchStopped {
					fx.Close()
					t.Fatalf("前提: 取得止めになっていない: %+v", st)
				}

				switch name {
				case "web_APIが読めた":
					script.set(apiOK(apiLimit(handoff.LimitKindSession, 5, ptr(time.Now().Add(4*time.Hour)))))
					time.Sleep(6 * time.Minute)
					startFetch(fx)
				case "同じ値のpaneの行":
					fx.Orc.OnStatusline(slLine("pane-run", 100, slWin(40, resets), nil))
				case "立て直し":
					fx.Close()
					fx = newAPIFetchStubFixture(t, clone, ws, reader, root)
					fx.Herdr.SetSL(noValueScript(fx))
					if st := fx.Orc.QuotaAPIStateForTest(); st.FetchStopped {
						t.Errorf("立て直したのに取得止めのまま: %+v", st)
					}
					startFetch(fx)
					if got := len(fx.Herdr.SLStarts()); got != 1 {
						t.Errorf("立て直した直後の巡回で statusline取得を開いていない: %d 回", got)
					}
					time.Sleep(3*time.Minute + time.Second)
					synctest.Wait()
				}
				defer fx.Close()
				if name != "立て直し" {
					if st := fx.Orc.QuotaAPIStateForTest(); st.FetchStopped || !st.EverRead {
						t.Errorf("読めたのに取得止めが解けていない: %+v", st)
					}
				}
			})
		})
	}
}

// 目的: 取得止めにしない場合を確かめる。
//
//   - 取得の途中で pane の行（使用率を持つ）が届いた（終わった時点で1度でも読めている）
//   - usage API が1度読めたあとに誤りへ変わった（上限で断られた Pro / Max の機械が、あとで値を取り直せる）
//   - fetchBlocked・AgentPrompt を呼ぶ前の誤り・止めるときの取り消し・使える clone が無い
//   - source: statusline
//
// 与える情報: 401 の usage API（1度読めた場合は1回目だけ 200）と、場合ごとの Claude Code の台本。
// 成功条件: どれも取得止めにならず、取得止めの WARN が出ないこと。1度読めた場合は、値の古さの上限の
// あとに2回目の statusline取得を開くこと。
func TestQuotaAPIStop_止めない場合(t *testing.T) {
	clone := newTrustedClone(t)
	for _, tc := range []struct {
		name   string
		mutate func(cfg *config.Config)
		first  bool
		script func(fx *stubFixture) stubSLScript
		cancel bool
	}{
		{name: "取得の途中でpaneの行が届いた", script: func(fx *stubFixture) stubSLScript {
			return stubSLScript{Prompt: func(context.Context, herdr.AgentPromptParams) (*herdr.AgentPromptResult, error) {
				fx.Orc.OnStatusline(slLine("pane-run", 100, slWin(30, time.Now().Add(3*time.Hour)), nil))
				return &herdr.AgentPromptResult{Type: "agent_prompted"}, nil
			}}
		}},
		{name: "web_APIが1度読めていた", first: true, script: noValueScript},
		{name: "fetchBlocked", script: func(*stubFixture) stubSLScript {
			return stubSLScript{Get: func(_ context.Context, p herdr.AgentGetParams) (*herdr.AgentGetResult, error) {
				return &herdr.AgentGetResult{Agent: herdr.Agent{Name: p.Target.String(), AgentStatus: herdr.AgentStatusBlocked}}, nil
			}}
		}},
		{name: "AgentPromptを呼ぶ前の誤り", script: func(*stubFixture) stubSLScript {
			return stubSLScript{Start: func(context.Context, herdr.AgentStartParams) (*herdr.AgentStartResult, error) {
				return nil, errors.New("agent.start が落ちた")
			}}
		}},
		{name: "止めるときの取り消し", cancel: true, script: func(*stubFixture) stubSLScript {
			return stubSLScript{Prompt: func(ctx context.Context, _ herdr.AgentPromptParams) (*herdr.AgentPromptResult, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			}}
		}},
		{name: "使える cloneが無い", mutate: func(cfg *config.Config) { cfg.Trust.Repositories = nil }, script: noValueScript},
		{name: "source_statusline", mutate: func(cfg *config.Config) { cfg.RateLimit.Source = ratelimit.SourceStatusline }, script: noValueScript},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := &apiScript{respond: func(n int, r *http.Request) (*http.Response, error) {
				if tc.first && n == 1 {
					return apiOK(apiLimit(handoff.LimitKindSession, 20, ptr(time.Now().Add(4*time.Hour))))(n, r)
				}
				return apiResponse(http.StatusUnauthorized, `{}`, nil), nil
			}}
			reader := newScriptedReader(t, script, "test-token")
			root := t.TempDir()
			synctest.Test(t, func(t *testing.T) {
				ws := newMemWorkspaceHerdr()
				fx := newStubFixture(t, stubFixtureOptions{
					Logs: true, Root: root, WorkspaceHerdr: ws, HomeDir: clone.Home, RateLimit: reader,
					GhqList: func(_ context.Context, owner, repo string) (string, error) {
						if owner+"/"+repo == "octocat/hello-world" {
							return clone.Dir, nil
						}
						return "", nil
					},
					Mutate: func(cfg *config.Config) {
						apiConfig(cfg)
						cfg.Trust.Repositories = []string{"octocat/hello-world"}
						if tc.mutate != nil {
							tc.mutate(cfg)
						}
					},
				})
				fx.Herdr.SetSL(tc.script(fx))

				if tc.first {
					// 1回目の巡回で読め、値の古さの上限のあとの巡回で 401 へ変わって開く。
					startFetch(fx)
					time.Sleep(6 * time.Minute)
				}
				startFetch(fx)
				if tc.cancel {
					fx.Close()
					synctest.Wait()
				} else {
					time.Sleep(3*time.Minute + time.Second)
					synctest.Wait()
				}
				if st := fx.Orc.QuotaAPIStateForTest(); st.FetchStopped {
					t.Errorf("取得止めにした: %+v\n%s", st, fx.Logs.String())
				}
				if got := stopWarns(fx); got != 0 {
					t.Errorf("取得止めの WARN が %d 行出た", got)
				}
				if tc.first {
					time.Sleep(6 * time.Minute)
					startFetch(fx)
					if got := len(fx.Herdr.SLStarts()); got != 2 {
						t.Errorf("1度読めていたのに2回目の statusline取得を開いていない: %d 回", got)
					}
					time.Sleep(3*time.Minute + time.Second)
					synctest.Wait()
				}
				if !tc.cancel {
					fx.Close()
				}
			})
		})
	}
}

// 目的: トークンの一時的な失敗で切り替えていても、1度も読めていなければ、送ったあとの誤りで
// 取得止めになることを確かめる（偽の `security` を起こすので、テスト用herdr mock と実時間で行う）。
//
// 与える情報: 返ってこない偽の `security`（期限 200ms）。statusline取得の agent.prompt が誤りを返す
// テスト用herdr mock（送ったあとの fetchMidwayError）。信頼済みの clone。
// 成功条件: 取得止めになり、取得止めの WARN が1行、途中の誤りの WARN が0行であること。
func TestQuotaAPI_トークンの一時的な失敗でも値が届かなければ取得止めになる(t *testing.T) {
	clock := newTestClock()
	script := &apiScript{respond: apiOK()}
	reader := newKeychainReader(t, script, "exec sleep 30", 200*time.Millisecond)
	fx := newFixture(t, fixtureOptions{
		Now:       clock.Now,
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			apiConfig(cfg)
			cfg.Trust.Repositories = []string{"octocat/hello-world"}
		},
	})
	fx.AllowLog("usage API から statusline取得へ切り替えます", "statusline取得を止めます")
	fx.Herdr.HandleSL(herdr.MethodAgentPrompt, func(map[string]any) (any, *rpcErr) {
		return nil, &rpcErr{Code: "internal", Message: "agent.prompt が落ちた"}
	})

	tickAndSettle(t, fx)
	if st := fx.Orc.QuotaAPIStateForTest(); !st.FetchStopped || st.GaveUp {
		t.Fatalf("取得止めになっていない（または一時的な失敗で諦めた）: %+v\n%s", st, fx.Logs.String())
	}
	if got := logCount(fx, "statusline取得を止めます"); got != 1 {
		t.Errorf("取得止めの WARN が %d 行（want 1）", got)
	}
	if got := logCount(fx, "statusline取得ができません"); got != 0 {
		t.Errorf("取得止めなのにふだんの WARN も出た:\n%s", fx.Logs.String())
	}
}
