package orchestrator_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// 使用率の保管値（issue #284。internal/orchestrator/quota.go）の規則を確かめる。
//
// **行は OnStatusline で直に入れ、時計は testing/synctest の偽の時計で進める。**
// 保管値の中身は、入札が読む写し（QuotaForBidForTest。新しくなければ nil）と、
// 回復待ちと閾値が読む写し（QuotaSnapshotForTest。新しさを問わない）の2つで見る。
//
// 語の意味（計画の「値の保管と読み方」）。
//
//	新しい応答の行   そのセッションで api_ms が基準より増えた行
//	それ以外の行     新しい応答の行でない行（初めて見るセッションの最初の行を含む）
//	新しさの時刻     rate_limits を持つ新しい応答の行を最後に受けた時刻

// newQuotaStubFixture は、使用率を読む設定（`rate_limit.source: statusline`）の stub の fixture を作る。
//
// t: 呼び出し元のテスト。
// mutate: 設定を書き換える関数。nil なら既定のまま。
// 戻り値: 組み立てた fixture。**bubble の中で呼んだら、終わる前に Close を呼ぶこと。**
func newQuotaStubFixture(t *testing.T, mutate func(cfg *config.Config)) *stubFixture {
	t.Helper()
	return newStubFixture(t, stubFixtureOptions{
		Logs: true,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceStatusline
			if mutate != nil {
				mutate(cfg)
			}
		},
	})
}

// percentOf は写しから種別の使用率を引く。無ければ -1。
func percentOf(snap *ratelimit.Snapshot, kind string) int {
	if snap == nil {
		return -1
	}
	for _, l := range snap.Limits {
		if l.Kind == kind {
			return l.Percent
		}
	}
	return -1
}

// TestQuotaStore_新しい応答の行はresets_atが違えば置き換え同じなら大きいほうを残す は、
// 新しい応答の行の規則を確かめる。
//
// 目的: 新しい応答は今の値である。**同じ期間（resets_at が同じ）の中では値は下がらない**ので
// 大きいほうを残し、**resets_at が違えば（早くても遅くても）期間が替わったので置き換える**
// （アカウントを替えると resets_at が早まることがある）。
// 与える情報: 同じセッションから api_ms を増やしながら送る行。5時間の期間の値と resets_at を
// 40%@R1 → 30%@R1 → 10%@R0（R1 より早い）→ 20%@R2（R1 より遅い）の順に変える。
// 成功条件: 保管値が 40 → 40 → 10 → 20 と移ること。
func TestQuotaStore_新しい応答の行はresets_atが違えば置き換え同じなら大きいほうを残す(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, nil)
		defer fx.Close()
		now := time.Now()
		r0, r1, r2 := now.Add(time.Hour), now.Add(2*time.Hour), now.Add(3*time.Hour)

		fx.Orc.OnStatusline(slLine("s", 100, slWin(40, r1), nil))
		steps := []struct {
			apiMs int64
			pct   float64
			at    time.Time
			want  int
		}{
			{200, 30, r1, 40},
			{300, 10, r0, 10},
			{400, 20, r2, 20},
		}
		for _, st := range steps {
			fx.Orc.OnStatusline(slLine("s", st.apiMs, slWin(st.pct, st.at), nil))
			if got := percentOf(fx.Orc.QuotaSnapshotForTest(), handoff.LimitKindSession); got != st.want {
				t.Fatalf("api_ms %d の新しい応答の行のあと、保管値が %d（want %d）", st.apiMs, got, st.want)
			}
		}
	})
}

// TestQuotaStore_それ以外の行は遅いresets_atなら置き換え同じなら大きいほう早ければ捨てる は、
// 新しい応答でない行の規則を確かめる。
//
// 目的: **止まっているセッションは古い値を送り続ける。**それで保管値を下げない。
// 一方で、**上限で断られた呼び出しでは api_ms が増えないことがある**ので、増えていない行の
// 100 も受ける（計画の「それ以外の行」）。
// 与える情報: セッション A の新しい応答の行で 50%@R1 を入れたあと、セッション B から
// api_ms を増やさずに 30%@R1・70%@R1・100%@R1・20%@R0（早い）・5%@R2（遅い）を送る。
// 成功条件: 保管値が 50 → 70 → 100 → 100 → 5 と移ること（下がる値と早い期間は捨て、
// 同じ期間の大きい値と、遅い期間は受ける）。
func TestQuotaStore_それ以外の行は遅いresets_atなら置き換え同じなら大きいほう早ければ捨てる(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, nil)
		defer fx.Close()
		now := time.Now()
		r0, r1, r2 := now.Add(time.Hour), now.Add(2*time.Hour), now.Add(3*time.Hour)

		fx.Orc.OnStatusline(slLine("a", 100, slWin(50, r1), nil))
		fx.Orc.OnStatusline(slLine("a", 200, slWin(50, r1), nil))
		steps := []struct {
			pct  float64
			at   time.Time
			want int
			why  string
		}{
			{30, r1, 50, "止まっているセッションの同じ期間の低い値で下げない（初めて見るセッションの最初の行）"},
			{70, r1, 70, "同じ期間の大きい値は受ける"},
			{100, r1, 100, "上限で断られた行（api_ms が増えず、値が上がった行）の 100 を受ける"},
			{20, r0, 100, "早い resets_at の期間は捨てる"},
			{5, r2, 5, "遅い resets_at の期間は置き換える"},
		}
		for _, st := range steps {
			fx.Orc.OnStatusline(slLine("b", 7, slWin(st.pct, st.at), nil))
			if got := percentOf(fx.Orc.QuotaSnapshotForTest(), handoff.LimitKindSession); got != st.want {
				t.Fatalf("%s: 保管値が %d（want %d）", st.why, got, st.want)
			}
		}
	})
}

// TestQuotaStore_初めて見るセッションの最初の行は値を当てるが新しさの時刻を進めない は、
// 最初の行の扱いを確かめる。
//
// 目的: **立て直した直後に上限に当たっている run の 100 を捨てない**（値は当てる）。
// 一方で、**止まっているセッションの古い値で新しさを作らない**（入札には使わない）。
// 与える情報: 初めて見るセッションの最初の行（100%）。
// 成功条件: 回復待ちの写しには 100 が入り、入札の写しは nil のままであること。
func TestQuotaStore_初めて見るセッションの最初の行は値を当てるが新しさの時刻を進めない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, nil)
		defer fx.Close()
		fx.Orc.OnStatusline(slLine("a", 5000, slWin(100, time.Now().Add(time.Hour)), nil))

		if got := percentOf(fx.Orc.QuotaSnapshotForTest(), handoff.LimitKindSession); got != 100 {
			t.Errorf("最初の行の 100 を当てていない: %d", got)
		}
		if snap := fx.Orc.QuotaForBidForTest(); snap != nil {
			t.Errorf("最初の行で新しさの時刻を進めた（止まっているセッションの古い値で入札する）: %+v", snap)
		}
	})
}

// TestQuotaStore_基準のapi_msはrate_limitsを持つ行でだけ上がる は、基準の上げ方と null の行を確かめる。
//
// 目的: **`api_ms` だけが先に増えた null の行が、増えた分を使い切らないこと。**null の行で
// 基準を上げると、そのあとに値を持って届いた行が「新しい応答」と見なされず、入札が止まる。
// また、**null の行では resets_at の過ぎた保管値を消さない**（値の無い行で期限の過ぎた期間を
// 消すと、次の行が届くまでの数秒、入札がその期間を0と読みうる）。
// 与える情報: 5時間の期間を1分後に明ける値にした新しい応答の行。2分待ってから
// `rate_limits` が null で api_ms が大きく増えた行。そのあと、api_ms がその null の行より小さく
// 最初の基準より大きい、7日の期間だけを持つ行。`refresh_interval_ms` は10分。
// 成功条件: null の行のあとも入札は読めない（期限の過ぎた期間が残っている）。最後の行は
// 新しい応答の行として扱われ、入札が7日の期間だけを読めるようになること。
func TestQuotaStore_基準のapi_msはrate_limitsを持つ行でだけ上がる(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, func(cfg *config.Config) {
			cfg.RateLimit.RefreshIntervalMs = int((10 * time.Minute).Milliseconds())
		})
		defer fx.Close()
		now := time.Now()
		fx.Orc.OnStatusline(slLine("a", 100, slWin(20, now.Add(time.Minute)), slWin(30, now.Add(72*time.Hour))))
		fx.Orc.OnStatusline(slLine("a", 200, slWin(20, now.Add(time.Minute)), slWin(30, now.Add(72*time.Hour))))
		if fx.Orc.QuotaForBidForTest() == nil {
			t.Fatal("前提: 新しい応答の行のあとで入札が読めない")
		}

		time.Sleep(2 * time.Minute)
		if snap := fx.Orc.QuotaForBidForTest(); snap != nil {
			t.Fatalf("5時間の期間が明けたのに入札が読める: %+v", snap)
		}
		fx.Orc.OnStatusline(slLine("a", 900, nil, nil))
		if snap := fx.Orc.QuotaForBidForTest(); snap != nil {
			t.Fatalf("null の行で resets_at の過ぎた保管値を消した（入札がその期間を0と読む）: %+v", snap)
		}

		fx.Orc.OnStatusline(slLine("a", 500, nil, slWin(31, now.Add(72*time.Hour))))
		snap := fx.Orc.QuotaForBidForTest()
		if snap == nil {
			t.Fatal("null の行のあとに値を持って届いた行を、新しい応答の行として扱っていない（null の行が基準を上げた）")
		}
		if got := percentOf(snap, handoff.LimitKindSession); got != -1 {
			t.Errorf("明けた5時間の期間が入札の写しに残っている: %d", got)
		}
		if got := percentOf(snap, handoff.LimitKindWeeklyAll); got != 31 {
			t.Errorf("7日の期間が %d（want 31）", got)
		}
	})
}

// TestQuotaStore_api_msが基準より減ったら次に増えた行から新しい応答として扱う は、基準の下げ方を確かめる。
//
// 目的: セッションを作り直すなどで api_ms が戻ったとき、基準をその値へ下げないと、
// そのセッションの行が基準を超えるまで永久に「新しい応答」にならない。
// 与える情報: 新しさの期限を過ぎたあとに、基準（200）より小さい api_ms 50 の行と、
// 続けて api_ms 60 の行。
// 成功条件: 50 の行では入札は読めないまま、60 の行で読めるようになること。
func TestQuotaStore_api_msが基準より減ったら次に増えた行から新しい応答として扱う(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, nil)
		defer fx.Close()
		feedFreshQuota(fx.Orc, "a", time.Now(), 10, 20)
		time.Sleep(time.Duration(fx.Config.RateLimit.RefreshIntervalMs)*time.Millisecond + time.Second)
		if fx.Orc.QuotaForBidForTest() != nil {
			t.Fatal("前提: 新しさの期限を過ぎても入札が読める")
		}

		now := time.Now()
		fx.Orc.OnStatusline(slLine("a", 50, slWin(11, now.Add(2*time.Hour)), nil))
		if snap := fx.Orc.QuotaForBidForTest(); snap != nil {
			t.Fatalf("基準より減った行を新しい応答の行として扱った: %+v", snap)
		}
		fx.Orc.OnStatusline(slLine("a", 60, slWin(12, now.Add(2*time.Hour)), nil))
		if fx.Orc.QuotaForBidForTest() == nil {
			t.Fatal("基準を下げたあとに増えた行を、新しい応答の行として扱っていない")
		}
	})
}

// TestQuotaStore_期間が片方だけの新しい応答の行でも新しさを進め入札の写しは期限内の保管値を持つ は、
// 期間が欠けた行の扱いを確かめる。
//
// 目的: **期間は独立に欠けうる**（公式文書）。片方だけの行で新しさを進めないと入札が止まり、
// 入札の写しに行に無い期間を渡さないと、入札はその期間を0と読む。
// 与える情報: 両方の期間を持つ新しい応答の行。新しさの期限を過ぎたあとに、5時間の期間
// だけを持つ新しい応答の行。
// 成功条件: 入札が読めるようになり、写しに7日の期間（期限内の保管値）も入っていること。
func TestQuotaStore_期間が片方だけの新しい応答の行でも新しさを進め入札の写しは期限内の保管値を持つ(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, nil)
		defer fx.Close()
		feedFreshQuota(fx.Orc, "a", time.Now(), 10, 40)
		time.Sleep(time.Duration(fx.Config.RateLimit.RefreshIntervalMs)*time.Millisecond + time.Second)

		fx.Orc.OnStatusline(slLine("a", 300, slWin(12, time.Now().Add(time.Hour)), nil))
		snap := fx.Orc.QuotaForBidForTest()
		if snap == nil {
			t.Fatal("期間が片方だけの新しい応答の行で、新しさの時刻を進めていない")
		}
		if got := percentOf(snap, handoff.LimitKindSession); got != 12 {
			t.Errorf("5時間の期間が %d（want 12）", got)
		}
		if got := percentOf(snap, handoff.LimitKindWeeklyAll); got != 40 {
			t.Errorf("行に無い7日の期間を入札の写しに渡していない（入札は0と読む）: %d", got)
		}
	})
}

// TestQuotaStore_resets_atを過ぎたら入札は読めず閾値と回復待ちはその期間を除いて読む は、
// 行が1つも届かない暇な機械で期間が明けたときを確かめる。
//
// 目的: **期限は読む時点の時計で見る**（行が届かない機械でも、期限が過ぎた時点で入札を止める）。
// 回復待ちと閾値は新しさを問わないが、明けた期間は使わない。次の新しい応答の行で入札が
// 読めるようになり、明けた期間は保管値から消える。
// 与える情報: 1時間後に明ける 100% の5時間の期間と、3日後に明ける 30% の7日の期間。
// `refresh_interval_ms` は3時間（新しさの期限で読めなくなるのと分けるため）。
// 行を送らずに61分進め、そのあと7日の期間だけを持つ新しい応答の行。
// 成功条件: 61分後は入札が読めず、回復待ちの写しは7日の期間だけ（上限ではない）。
// 最後の行のあとは入札が読め、写しに5時間の期間が無いこと。
func TestQuotaStore_resets_atを過ぎたら入札は読めず閾値と回復待ちはその期間を除いて読む(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, func(cfg *config.Config) {
			cfg.RateLimit.RefreshIntervalMs = int((3 * time.Hour).Milliseconds())
		})
		defer fx.Close()
		now := time.Now()
		five := slWin(100, now.Add(time.Hour))
		seven := slWin(30, now.Add(72*time.Hour))
		fx.Orc.OnStatusline(slLine("a", 100, five, seven))
		fx.Orc.OnStatusline(slLine("a", 200, five, seven))
		if !fx.Orc.QuotaSnapshotForTest().AtFullPercent() {
			t.Fatal("前提: 100% の期間が上限として読めていない")
		}

		time.Sleep(61 * time.Minute)
		if snap := fx.Orc.QuotaForBidForTest(); snap != nil {
			t.Errorf("resets_at を過ぎた期間があるのに入札が読める: %+v", snap)
		}
		snap := fx.Orc.QuotaSnapshotForTest()
		if snap.AtFullPercent() || percentOf(snap, handoff.LimitKindSession) != -1 {
			t.Errorf("回復待ちと閾値が、明けた期間を読んでいる: %+v", snap)
		}
		if percentOf(snap, handoff.LimitKindWeeklyAll) != 30 {
			t.Errorf("回復待ちと閾値が、期限内の7日の期間を読めていない: %+v", snap)
		}

		fx.Orc.OnStatusline(slLine("a", 300, nil, slWin(31, now.Add(72*time.Hour))))
		bid := fx.Orc.QuotaForBidForTest()
		if bid == nil {
			t.Fatal("次の新しい応答の行で入札が読めるようになっていない")
		}
		if got := percentOf(bid, handoff.LimitKindSession); got != -1 {
			t.Errorf("明けた期間が保管値から消えていない: %d", got)
		}
	})
}

// TestQuotaStore_新しさの時刻からrefresh_interval_msを過ぎたら期限内の値があっても入札は読めない は、
// 新しさの期限を確かめる。
//
// 目的: 設計 3-77i の「古い値で入札しない」。issue の「決めること」の4の
// 「取り直しの間隔より古い値は使わない」を新しさの時刻で満たす。
// 与える情報: 新しい応答の行を受けたあと、`refresh_interval_ms`（既定5分）ちょうどまで進める。
// 成功条件: 直前までは入札が読め、ちょうどで読めなくなること。回復待ちの写しは残ること。
func TestQuotaStore_新しさの時刻からrefresh_interval_msを過ぎたら期限内の値があっても入札は読めない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, nil)
		defer fx.Close()
		feedFreshQuota(fx.Orc, "a", time.Now(), 10, 20)
		interval := time.Duration(fx.Config.RateLimit.RefreshIntervalMs) * time.Millisecond

		time.Sleep(interval - time.Second)
		if fx.Orc.QuotaForBidForTest() == nil {
			t.Fatal("新しさの期限の前なのに入札が読めない")
		}
		time.Sleep(time.Second)
		if snap := fx.Orc.QuotaForBidForTest(); snap != nil {
			t.Errorf("新しさの期限を過ぎたのに入札が読める: %+v", snap)
		}
		if fx.Orc.QuotaSnapshotForTest() == nil {
			t.Error("新しさの期限を過ぎただけで、回復待ちと閾値の写しまで消した")
		}
	})
}

// TestQuotaStore_100を超えた値は100にし小数は切り捨て今より前のresets_atは取り込まない は、
// 値の形を確かめる。
//
// 目的: 上限の判定を小数の切り上げで早めない。期間が消えたあとに、過ぎた期間の行が遅れて
// 届いても戻さない（計画の「値の形」）。
// 与える情報: 150.2% の5時間の期間と 42.9% の7日の期間。続けて、今より前の resets_at を
// 持つ別のセッションの行。
// 成功条件: 保管値が 100 と 42 で、過ぎた期間の行で変わらないこと。
func TestQuotaStore_100を超えた値は100にし小数は切り捨て今より前のresets_atは取り込まない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, nil)
		defer fx.Close()
		now := time.Now()
		fx.Orc.OnStatusline(slLine("a", 100, slWin(150.2, now.Add(time.Hour)), slWin(42.9, now.Add(72*time.Hour))))
		fx.Orc.OnStatusline(slLine("b", 100, slWin(1, now.Add(-time.Minute)), slWin(1, now.Add(-time.Minute))))

		snap := fx.Orc.QuotaSnapshotForTest()
		if got := percentOf(snap, handoff.LimitKindSession); got != 100 {
			t.Errorf("100 を超えた値が %d（want 100）", got)
		}
		if got := percentOf(snap, handoff.LimitKindWeeklyAll); got != 42 {
			t.Errorf("小数が %d（want 42。切り捨て）", got)
		}
	})
}

// TestQuotaStore_quota_jsonを読み直すと保管値は戻り新しさの時刻は戻らない は、保管値の置き場所を確かめる。
//
// 目的: **上限の最中に立て直しても、回復待ちの判定が効く**（issue の「決めること」7）。
// 一方で、**新しさの時刻は置かない**（立て直したあとは値が古いものとして statusline取得で取り直す）。
// 与える情報: 1つ目の orchestrator に新しい応答の行を入れる。同じ実行時ディレクトリで
// 2つ目の orchestrator を組み立て、起動時の準備（PrepareStatusline）を呼ぶ。
// 成功条件: quota.json が 0600 で書かれ、2つ目の回復待ちの写しが同じ値になり、
// 入札の写しは nil であること。
func TestQuotaStore_quota_jsonを読み直すと保管値は戻り新しさの時刻は戻らない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		opts := stubFixtureOptions{Root: root, Logs: true, Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceStatusline
		}}
		first := newStubFixture(t, opts)
		defer first.Close()
		feedFreshQuota(first.Orc, "a", time.Now(), 100, 34)

		info, err := os.Stat(filepath.Join(root, "quota.json"))
		if err != nil {
			t.Fatalf("quota.json が書かれていない: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("quota.json の権限が %o（want 600）", perm)
		}

		second := newStubFixture(t, opts)
		defer second.Close()
		second.Orc.PrepareStatusline(context.Background())
		snap := second.Orc.QuotaSnapshotForTest()
		if percentOf(snap, handoff.LimitKindSession) != 100 || percentOf(snap, handoff.LimitKindWeeklyAll) != 34 {
			t.Errorf("読み直した保管値が違う: %+v", snap)
		}
		if bid := second.Orc.QuotaForBidForTest(); bid != nil {
			t.Errorf("読み直しで新しさの時刻まで戻した（立て直した直後に古い値で入札する）: %+v", bid)
		}
	})
}

// TestQuotaStore_壊れたquota_jsonはWARNを出して捨て起動を止めない は、読めない quota.json を確かめる。
//
// 目的: quota.json は外から観測した使用率の写しで、失っても statusline取得へ戻るだけである。
// 読めないことで起動を止めない。
// 与える情報: JSON として読めない quota.json。
// 成功条件: PrepareStatusline が戻り、WARN が1行出て、保管値が空であること。
func TestQuotaStore_壊れたquota_jsonはWARNを出して捨て起動を止めない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "quota.json"), []byte("{壊れている"), 0o600); err != nil {
			t.Fatalf("quota.json を書けません: %v", err)
		}
		fx := newStubFixture(t, stubFixtureOptions{Root: root, Logs: true, Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceStatusline
		}})
		defer fx.Close()

		fx.Orc.PrepareStatusline(context.Background())
		if got := fx.countLog("quota.json の形が違うので捨てます"); got != 1 {
			t.Errorf("壊れた quota.json の WARN が %d 行（want 1）:\n%s", got, fx.Logs.String())
		}
		if snap := fx.Orc.QuotaSnapshotForTest(); snap != nil {
			t.Errorf("壊れた quota.json から値を読んだ: %+v", snap)
		}
	})
}

// TestQuotaStore_ほぼ同時に届いた行でも最後に書かれるのが最新の写し は、quota.json の書き込みの順を確かめる。
//
// 目的: **書き込み用の錠を取ってから写しを取り、外で書く**（計画の「置き場所」）。錠の順を
// 誤ると、古い写しが新しい写しを上書きし、立て直したときに古い値へ戻る。
// 与える情報: 32個のセッションから、同じ期間の 1〜32% の行を同時に送る。
// 成功条件: 全部届いたあと、quota.json の値が保管値（32%）と同じであること。
func TestQuotaStore_ほぼ同時に届いた行でも最後に書かれるのが最新の写し(t *testing.T) {
	fx := newQuotaStubFixture(t, nil)
	t.Cleanup(fx.Close)
	resetsAt := time.Now().Add(2 * time.Hour)

	var wg sync.WaitGroup
	for i := 1; i <= 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			fx.Orc.OnStatusline(slLine(fmt.Sprintf("s%d", i), 100, slWin(float64(i), resetsAt), nil))
		}(i)
	}
	wg.Wait()

	if got := percentOf(fx.Orc.QuotaSnapshotForTest(), handoff.LimitKindSession); got != 32 {
		t.Fatalf("前提: 保管値が %d（want 32）", got)
	}
	raw, err := os.ReadFile(filepath.Join(fx.Root, "quota.json"))
	if err != nil {
		t.Fatalf("quota.json を読めません: %v", err)
	}
	var file map[string]struct {
		Percent int `json:"percent"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("quota.json を読めません: %v\n%s", err, raw)
	}
	if got := file[handoff.LimitKindSession].Percent; got != 32 {
		t.Errorf("quota.json に最新でない写しが残った: %d（want 32）\n%s", got, raw)
	}
}

// TestQuotaStore_quota_jsonを書けないときはそのたびにWARNを出して動き続ける は、書き込みの失敗を確かめる。
//
// 目的: 書けないことで本体を止めない。書けないことを隠さない（そのたびに WARN）。
// 与える情報: quota.json の場所にディレクトリを置き、値の変わる行を2本送る。
// 成功条件: WARN が2行出て、保管値は2本目の値になっていること。
func TestQuotaStore_quota_jsonを書けないときはそのたびにWARNを出して動き続ける(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, nil)
		defer fx.Close()
		if err := os.Mkdir(filepath.Join(fx.Root, "quota.json"), 0o700); err != nil {
			t.Fatalf("quota.json の場所にディレクトリを置けません: %v", err)
		}
		resetsAt := time.Now().Add(time.Hour)
		fx.Orc.OnStatusline(slLine("a", 100, slWin(10, resetsAt), nil))
		fx.Orc.OnStatusline(slLine("a", 200, slWin(20, resetsAt), nil))

		if got := fx.countLog("quota.json へ書けませんでした"); got != 2 {
			t.Errorf("書けなかった WARN が %d 行（want 2）:\n%s", got, fx.Logs.String())
		}
		if got := percentOf(fx.Orc.QuotaSnapshotForTest(), handoff.LimitKindSession); got != 20 {
			t.Errorf("書けなかったあとの保管値が %d（want 20）", got)
		}
	})
}

// TestQuotaStore_24時間届かないセッションの記録を消す は、セッションごとの記録の寿命を確かめる。
//
// 目的: 閉じたセッションの記録（基準の api_ms）を持ち続けない。
// 与える情報: セッション a の行。24時間と1秒進めてから、セッション b の行。
// 成功条件: a の記録が消え、b の記録があること。
func TestQuotaStore_24時間届かないセッションの記録を消す(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, nil)
		defer fx.Close()
		fx.Orc.OnStatusline(slLine("a", 100, nil, nil))
		time.Sleep(24*time.Hour + time.Second)
		fx.Orc.OnStatusline(slLine("b", 100, nil, nil))

		if fx.Orc.QuotaSessionKnownForTest("a") {
			t.Error("24時間届かないセッションの記録が残っている")
		}
		if !fx.Orc.QuotaSessionKnownForTest("b") {
			t.Error("届いたセッションの記録が無い")
		}
	})
}

// TestQuotaStore_受け口はrunの状態へ何も書かない は、hook と別の socket にした理由を確かめる。
//
// 目的: **ステータスラインは描き直すたびに行を送る**（run が止まっていても送る）。受け口が
// hook の時刻や stall の時計を動かすと、止まった run を見逃す（計画の「run の状態」）。
// 与える情報: 引き継いだ run。1分待ってから、その run のセッション ID で行を2本送る。
// 成功条件: run の最後に hook を受けた時刻と stall の時計が変わらないこと。
func TestQuotaStore_受け口はrunの状態へ何も書かない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newQuotaStubFixture(t, nil)
		defer fx.Close()
		adoptRun(fx, 188)
		before, ok := viewOf(fx, "octocat/hello-world#188")
		if !ok {
			t.Fatal("前提: run が印に入っていない")
		}
		time.Sleep(time.Minute)
		feedFreshQuota(fx.Orc, "session-188", time.Now(), 10, 20)

		after, _ := viewOf(fx, "octocat/hello-world#188")
		if !after.LastHookAt.Equal(before.LastHookAt) || !after.StallClockAt.Equal(before.StallClockAt) {
			t.Errorf("受け口が run の状態を書き換えた: before=%+v after=%+v", before, after)
		}
	})
}
