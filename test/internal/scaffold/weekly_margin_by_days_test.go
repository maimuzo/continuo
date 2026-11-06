package scaffold_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// 目的: 雛形に、残り日数ごとの1週間のマージン（`weekly_margin_percent_by_days_until_reset`）の
// 全キー（1〜7）がコメントアウトで用意されていて、「# 」を外せばそのまま読めることを確かめる（issue #324）。
//
// **人間の求めが2つある。**「先に全キー名がコメントアウトとして用意されているようにして」と、
// 「今日が金曜日3:00AMで、週間レートリミットのリセットが金曜の9:00AMの時、残り日数の何日目が使用されるのかを
// コメントで表現しておいて」である。**コメント行は設計 5-2 との突き合わせ（キーの集合）に数えられないので、
// ここで別に確かめる。**
//
// 与える情報: owner とカンバンの番号を埋めた雛形。
// 成功条件: コメントアウトした表の行が 1〜7 の全部と金曜の例を持ち、表の行の「# 」を外した WORKFLOW.md が
// 起動前の検査を通って、7日ぶんの値を読めること。
func TestTemplate_残り日数ごとの1週間のマージンの全キーをコメントで用意する(t *testing.T) {
	base := scaffold.TemplateWithValues(scaffold.Values{Owner: "octocat", ProjectNumber: 3})
	if !strings.Contains(base, "金曜 3:00 で、リセットが金曜 9:00 なら、残り6時間なので 1 を使う") {
		t.Error("金曜 3:00 の例が雛形に無い")
	}

	// **日ごとの行に、その日が指す期間の例を書く**（人間の求め「WORKFLOW.mdの1:0部分のコメントに書いてあるべきじゃない?」。
	// issue #324 のコメント 6678703481）。例はリセットが火曜 9:00 のとき。
	for day, period := range map[string]string{
		"1": "月 9:00 から火 9:00 まで", "2": "日 9:00 から月 9:00 まで", "3": "土 9:00 から日 9:00 まで",
		"4": "金 9:00 から土 9:00 まで", "5": "木 9:00 から金 9:00 まで", "6": "水 9:00 から木 9:00 まで",
		"7": "火 9:00 から水 9:00 まで",
	} {
		found := false
		for _, l := range strings.Split(base, "\n") {
			if strings.HasPrefix(l, "      #   "+day+": ") && strings.Contains(l, "例: "+period) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s の行のコメントに期間の例「%s」が無い", day, period)
		}
	}

	// **値の単位と意味も、コメントだけで読めるように書く**（人間の指摘「1:0の0の単位が％であることはどこに書いてあるの?」。
	// issue #324 のコメント 6680279690）。
	// **雛形だけを初めて読む利用者が迷う点も書く**（実装レビュー4周目の3つ目の役の指摘）。
	for _, want := range []string{
		"値はその日のマージン（単位は %。0以上100未満の整数）",
		"値は % を付けずに数字だけ書く",
		"「2: 5」なら、残り2日の日は1週間の使用率が 95% に達したら作業しない",
		"「1: 0」なら、残り1日の日は使用率100%まで使い切る",
		"値が無い・値に % などの文字を付けた、のどれかだと continuo は起動しない",
		"今効いている値は、server.port を設定したダッシュボードのレートリミットの区画の「マージン」（英語の画面では margin）の欄で見る",
		"「リセット」（resets）の欄の時刻は UTC（末尾が Z）。「あと N日M時間」（Nd Mh left）は切り捨てなので、",
		"「あと1日5時間」なら効くキーは 2",
		"行頭の「# 」だけを外す（行の途中の「# 」から後ろは説明なので残す）",
		"使用率を読めていないあいだは、表にかかわらず新しい着手をせず、走っている run も切り上げない",
		"Blocked へ戻った issue は、残り日数が変わってマージンが下がっても自動では戻らない",
		"書き換えたら continuo を立て直す",
		"WORKFLOW.local.md で weekly_margin_percent だけを変えても、この表に書いた日は表の値が効く",
		"WORKFLOW.local.md にこの表を書くと丸ごと置き換わる",
	} {
		if !strings.Contains(base, want) {
			t.Errorf("雛形の表の説明に「%s」が無い", want)
		}
	}

	// **既にある WORKFLOW.md へ足す見本（docs/upgrading.md）は、雛形の塊と同じであること**（実装レビュー4周目の LOW）。
	// 雛形だけを直すと、既存の利用者が写す見本が古いまま残る。
	const header = "      # weekly_margin_percent_by_days_until_reset:"
	block := base[strings.Index(base, header):]
	block = block[:strings.Index(block, "      weekly_margin_percent: 10")]
	upgrading, err := os.ReadFile("../../../docs/upgrading.md")
	if err != nil {
		t.Fatalf("docs/upgrading.md を読めません: %v", err)
	}
	if !strings.Contains(string(upgrading), block) {
		t.Error("docs/upgrading.md の見本が、雛形の残り日数の表の塊と同じではない")
	}
	// **設計 5-2 の写しも同じであること**（実装レビュー5周目の LOW）。キーの集合の突き合わせはコメント行を数えない。
	design, err := os.ReadFile(designDocPath)
	if err != nil {
		t.Fatalf("設計文書を読めません: %v", err)
	}
	if !strings.Contains(string(design), block) {
		t.Error("設計 5-2 の写しが、雛形の残り日数の表の塊と同じではない")
	}

	var b strings.Builder
	found := map[string]bool{}
	for _, l := range strings.Split(base, "\n") {
		switch {
		case strings.HasPrefix(l, header):
			// **見出しの行と、日の行だけ「# 」を外す**（雛形のコメントが利用者に頼んでいるとおり）。
			l = strings.Replace(l, "# ", "", 1)
			found["header"] = true
		case strings.HasPrefix(l, "      #   ") && len(l) > 11 && l[10] >= '1' && l[10] <= '7' && l[11] == ':':
			found[l[10:11]] = true
			l = strings.Replace(l, "# ", "", 1)
		}
		b.WriteString(l + "\n")
	}
	for _, k := range []string{"header", "1", "2", "3", "4", "5", "6", "7"} {
		if !found[k] {
			t.Errorf("コメントアウトした表に %s の行が無い", k)
		}
	}

	path := filepath.Join(t.TempDir(), "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("「# 」を外した雛形を読めない: %v", err)
	}
	got := loaded.Config.Tracker.Provider.Handoff.WeeklyMarginPercentByDaysUntilReset
	if len(got) != 7 {
		t.Errorf("読めた日が %d 個（want 7）: %v", len(got), got)
	}
	if got[1] != 0 || got[2] != 5 {
		t.Errorf("1日目と2日目の値が雛形と違う: %v", got)
	}
}
