// `margin-status`（continuo 本体が書く、レートリミットのマージンを切ったかの1行）の形の検査である（issue #262）。
//
// **外部へ1回も接続しない。**
package marginstatus_test

import (
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/marginstatus"
)

// 目的: 置き場所を `quota.json` と同じ条件で決めることを固定する（issue #262）。
//
// **書くのは、`rate_limit.source` が `none` 以外で、hook を受ける socket のパスが空でないときだけ。**
// **パスは socket と同じディレクトリの `margin-status`。**
//
// 与える情報: 3つの設定と socket のパス。
// 成功条件: パスが期待どおりであること。
func TestPath_quota_jsonと同じ条件で決める(t *testing.T) {
	cases := []struct {
		name   string
		source string
		socket string
		want   string
	}{
		{"usage API を読む機械", "oauth_usage_api", "/tmp/continuo/hooks.sock", "/tmp/continuo/margin-status"},
		{"ステータスラインを読む機械", "statusline", "/tmp/continuo/hooks.sock", "/tmp/continuo/margin-status"},
		{"使用率を読まない機械", "none", "/tmp/continuo/hooks.sock", ""},
		{"socket のパスが空", "oauth_usage_api", "", ""},
	}
	for _, c := range cases {
		if got := marginstatus.Path(config.RateLimitConfig{Source: c.source}, c.socket); got != c.want {
			t.Errorf("%s: Path = %q, want %q", c.name, got, c.want)
		}
	}
}

// 目的: 1行の3つの形と `expires_at` を固定する（issue #262。組み込みの指示書の 5-8 がこの形を読む）。
//
// **`expires_at` は UNIX 時刻の秒で書く。**5-8 は `date +%s` の出力と数として比べさせる。
// **`date -u` と比べさせてはならない**（issue #258）。
//
// 与える情報: 切った期間が2つ（片方はリセットの時刻を読めない）と、書いた時刻。
// 成功条件: 3つの状態の書き出しと、期間ごとの欄が期待どおりであること。
func TestFormat_3つの状態と欄(t *testing.T) {
	expires := time.Unix(1791336000, 0)
	resets := time.Date(2026, 10, 7, 5, 0, 0, 0, time.UTC)
	periods := []marginstatus.Period{
		{Kind: "session", Used: 93, Margin: 10, ResetsAt: &resets},
		{Kind: "weekly_scoped", Used: 95, Margin: 10},
	}
	cut := marginstatus.Format(marginstatus.StateCut, periods, expires)
	if !strings.HasPrefix(cut, "MARGIN: cut kind=session used=93 margin=10 resets_at=") {
		t.Errorf("切ったときの1行の頭が違います: %q", cut)
	}
	if !strings.Contains(cut, " kind=weekly_scoped used=95 margin=10 resets_at=unknown") {
		t.Errorf("リセットの時刻を読めない期間が unknown になっていません: %q", cut)
	}
	if !strings.HasSuffix(cut, " expires_at=1791336000\n") {
		t.Errorf("expires_at が UNIX 時刻の秒で最後に1つだけ置かれていません: %q", cut)
	}
	if got := marginstatus.Format(marginstatus.StateOK, nil, expires); got != "MARGIN: ok expires_at=1791336000\n" {
		t.Errorf("切っていないときの1行が違います: %q", got)
	}
	if got := marginstatus.Format(marginstatus.StateUnknown, nil, expires); got != "MARGIN: unknown expires_at=1791336000\n" {
		t.Errorf("読めていないときの1行が違います: %q", got)
	}
}

// 目的: `expires_at` の決め方を固定する（issue #262）。
//
// **書いた時刻に「巡回の間隔の3倍」と「10分」の長いほうを足す。ただし、切っている期間のリセットの時刻のうち
// いちばん早いものより後にはしない**（リセットの時刻を読めない期間は数えない）。
//
// 与える情報: 巡回の間隔30秒と10分、リセットが5分後の期間と読めない期間。
// 成功条件: 期限が期待どおりであること。
func TestExpiresAt_巡回の間隔とリセットの時刻(t *testing.T) {
	now := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	if got := marginstatus.ExpiresAt(now, 30*time.Second, nil); !got.Equal(now.Add(10 * time.Minute)) {
		t.Errorf("巡回の間隔が短いときは10分のはず: %v", got)
	}
	if got := marginstatus.ExpiresAt(now, 10*time.Minute, nil); !got.Equal(now.Add(30 * time.Minute)) {
		t.Errorf("巡回の間隔の3倍のはず: %v", got)
	}
	soon := now.Add(5 * time.Minute)
	periods := []marginstatus.Period{{Kind: "session", ResetsAt: &soon}, {Kind: "weekly_scoped"}}
	if got := marginstatus.ExpiresAt(now, 30*time.Second, periods); !got.Equal(soon) {
		t.Errorf("リセットの時刻より後にしてはならない: %v", got)
	}
}

// 目的: パスの単引用符を、単引用符で囲んだシェルの1語の中へ置ける形にすることを固定する（issue #262）。
func TestShellQuote_単引用符を置き換える(t *testing.T) {
	if got := marginstatus.ShellQuote(`/tmp/it's/margin-status`); got != `/tmp/it'\''s/margin-status` {
		t.Errorf("ShellQuote = %q", got)
	}
	if got := marginstatus.ShellQuote("/tmp/continuo/margin-status"); got != "/tmp/continuo/margin-status" {
		t.Errorf("単引用符の無いパスを変えています: %q", got)
	}
}
