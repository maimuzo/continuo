package ratelimit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/maimuzo/continuo/internal/ratelimit"
)

// mustTime は RFC3339 の文字列を time.Time にする（テスト用）。
func mustTime(t *testing.T, s string) *time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("時刻を解析できません（%s）: %v", s, err)
	}
	return &v
}

// 目的: 複数の枠を突き合わせる判定（MaxPercent / AtFullPercent /
// LatestResetOfFullLimits）が設計 3-27 のとおりであることを確認する。
//
// **`resets_at` が null の枠は判定から外す。**外さないと、リセット時刻が分からない枠に
// 引きずられて「いつまで待つか」を決められない。
//
// 与える情報: 使い切っている枠が2件（片方は resets_at が null）、まだ余裕のある枠が1件。
// 成功条件: MaxPercent が最大値を返し、AtFullPercent が真になり、
// LatestResetOfFullLimits が **resets_at が入っている枠の中で** いちばん遅い時刻を返すこと。
func TestSnapshot_使い切った枠のうちresets_atがある中でいちばん遅い時刻を返す(t *testing.T) {
	snap := &ratelimit.Snapshot{
		Limits: []ratelimit.Limit{
			{Kind: "session", Percent: 100, ResetsAt: mustTime(t, "2026-08-18T14:09:59Z")},
			{Kind: "weekly_all", Percent: 42, ResetsAt: mustTime(t, "2026-08-24T18:59:59Z")},
			{Kind: "weekly_scoped", Percent: 100, ResetsAt: nil},
		},
	}

	if got := snap.MaxPercent(); got != 100 {
		t.Fatalf("MaxPercent が想定と違う: got %d, want 100", got)
	}
	if !snap.AtFullPercent() {
		t.Fatalf("100%% の枠があるのに AtFullPercent が偽である")
	}
	got, ok := snap.LatestResetOfFullLimits()
	if !ok {
		t.Fatalf("使い切った枠があるのに LatestResetOfFullLimits が見つからないと返した")
	}
	want := *mustTime(t, "2026-08-18T14:09:59Z")
	if !got.Equal(want) {
		t.Fatalf("リセット時刻が想定と違う（resets_at が null の枠を混ぜている可能性）: got %s, want %s", got, want)
	}
}

// 目的: 使い切った枠が resets_at を1つも持たない場合に「見つからない」と返すことを確認する。
//
// **ゼロ値の時刻を「いますぐリセットされる」と読ませてはならない。**
//
// 与える情報: percent が 100 だが resets_at が null の枠だけ。
// 成功条件: AtFullPercent は真、LatestResetOfFullLimits の2つ目の戻り値が false であること。
func TestSnapshot_使い切った枠にresets_atが無ければ見つからないと返す(t *testing.T) {
	snap := &ratelimit.Snapshot{
		Limits: []ratelimit.Limit{{Kind: "weekly_scoped", Percent: 100, ResetsAt: nil}},
	}
	if !snap.AtFullPercent() {
		t.Fatalf("100%% の枠があるのに AtFullPercent が偽である")
	}
	if _, ok := snap.LatestResetOfFullLimits(); ok {
		t.Fatalf("resets_at が1つも無いのに時刻が見つかったと返した")
	}
}

// 目的: nil の Snapshot に対しても panic せず、安全な既定値を返すことを確認する
// （Fetch は資格情報が無いとき nil を返すので、呼び出し側が素直に渡してくる）。
// 与える情報: nil の *Snapshot。
// 成功条件: MaxPercent が 0、AtFullPercent が false、LatestResetOfFullLimits が
// 見つからないと返すこと。
func TestSnapshot_nilに対して安全な既定値を返す(t *testing.T) {
	var snap *ratelimit.Snapshot
	if got := snap.MaxPercent(); got != 0 {
		t.Fatalf("nil の MaxPercent が 0 でない: got %d", got)
	}
	if snap.AtFullPercent() {
		t.Fatalf("nil の AtFullPercent が真である")
	}
	if _, ok := snap.LatestResetOfFullLimits(); ok {
		t.Fatalf("nil の LatestResetOfFullLimits が見つかったと返した")
	}
	if got := snap.FullLimitKinds(); len(got) != 0 {
		t.Fatalf("nil の FullLimitKinds が空でない: got %v", got)
	}
}

// 目的: FullLimitKinds が「使い切っている枠の kind だけ」を返すことを確認する（issue #197）。
//
// **これを使って「1週間の枠のせいで待っているか」を切り分ける。**
// 5時間の枠だけで待っているときに担当を手放すと、2026-08-26 の決定
// 「5時間枠 → 待つ。担当は変えない」を破る。
//
// 与える情報: 使い切っている枠が2件（`session` と `weekly_scoped`）、
// まだ余裕のある枠が1件（`weekly_all`）。
// 成功条件: 使い切っている2件の kind だけが、応答の並び順のまま返ること。
func TestSnapshot_使い切った枠のkindだけを並び順のまま返す(t *testing.T) {
	snap := &ratelimit.Snapshot{
		Limits: []ratelimit.Limit{
			{Kind: "session", Percent: 100, ResetsAt: mustTime(t, "2026-08-18T14:09:59Z")},
			{Kind: "weekly_all", Percent: 42, ResetsAt: mustTime(t, "2026-08-24T18:59:59Z")},
			{Kind: "weekly_scoped", Percent: 100, ResetsAt: nil},
		},
	}

	got := snap.FullLimitKinds()
	want := []string{"session", "weekly_scoped"}
	if len(got) != len(want) {
		t.Fatalf("使い切った枠の件数が想定と違う: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("使い切った枠の kind が想定と違う: got %v, want %v", got, want)
		}
	}
}

// 目的: 使い切っている枠が1件も無ければ、FullLimitKinds が空を返すことを確認する（issue #197）。
//
// **空でないと、待つ上限の判定が「1週間の枠で待っている」と誤って読む。**
//
// 与える情報: どれも 100 に達していない枠が2件。
// 成功条件: 長さ0 が返ること。
func TestSnapshot_使い切った枠が無ければkindを1件も返さない(t *testing.T) {
	snap := &ratelimit.Snapshot{
		Limits: []ratelimit.Limit{
			{Kind: "session", Percent: 99, ResetsAt: nil},
			{Kind: "weekly_all", Percent: 42, ResetsAt: nil},
		},
	}
	if got := snap.FullLimitKinds(); len(got) != 0 {
		t.Fatalf("使い切った枠が無いのに kind を返した: got %v", got)
	}
}

// 目的: エラーメッセージへ載せる応答本文が、多バイト文字の途中で割れないことを確認する
// （レビュー指摘「truncate がバイト単位で切るので日本語が壊れる」の回帰テスト）。
// 与える情報: 日本語だけの長い本文を返す 500 応答。
// 成功条件: 返るエラーメッセージが妥当な UTF-8 であり、置換文字（U+FFFD）を含まないこと。
func TestFetch_エラー本文の切り詰めで日本語が壊れない(t *testing.T) {
	body := strings.Repeat("枠の読み取りに失敗しました。", 60)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	home := t.TempDir()
	writeCredentials(t, home, `{"claudeAiOauth":{"accessToken":"ok"}}`)

	reader, err := ratelimit.NewReader(ratelimit.Options{
		Config:   usageConfig(),
		Endpoint: srv.URL,
		HomeDir:  home,
	})
	if err != nil {
		t.Fatalf("NewReader が失敗した: %v", err)
	}

	_, err = reader.Fetch(context.Background())
	if err == nil {
		t.Fatalf("500 なのにエラーが返らなかった")
	}
	msg := err.Error()
	if !utf8.ValidString(msg) {
		t.Fatalf("エラーメッセージが妥当な UTF-8 でない（多バイト文字が割れている）: %q", msg)
	}
	if strings.ContainsRune(msg, '�') {
		t.Fatalf("エラーメッセージに壊れた文字が入っている: %q", msg)
	}
}
