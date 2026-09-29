// 人間のコメントを最初のメッセージに付けて渡す機能（relay）の、選び方と節の組み立ての検査である
// （設計 3-84。issue #246）。
//
// **境目は、信頼できる立場が書いた閉じた記録（`<!-- continuo:closed -->`）のうち、作成時刻がいちばん新しいもの**
// である。渡すのは、境目より後に作られ、信頼できる立場（OWNER / MEMBER / COLLABORATOR）で、
// AI の印が無く、隠されていないコメントだけである。
package orchestrator_test

import (
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/tracker"
)

// relayBase は、選び方の検査で使う時刻の起点である。
var relayBase = time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)

// relayComment は、選び方の検査で使うコメントを1件作る。
//
// n: コメントの番号（URL の `#issuecomment-<番号>` と ID に入れる）。
// body: 本文。
// association: 投稿者の立場。
// at: 起点からの経過（作成時刻）。
// 戻り値: コメント。
func relayComment(n int, body, association string, at time.Duration) tracker.Comment {
	return tracker.Comment{
		ID:                "C" + strconv.Itoa(n),
		URL:               "https://github.com/octocat/hello-world/issues/12#issuecomment-" + strconv.Itoa(n),
		Body:              body,
		CreatedAt:         relayBase.Add(at),
		AuthorAssociation: association,
	}
}

// closedRecordBody は閉じた記録の本文である（1行目が印）。
const closedRecordBody = config.ClosedMarker + "\nClaude Code を閉じました。"

// relayIDs は、選ばれたコメントの ID を並べる。
func relayIDs(list []tracker.Comment) []string {
	out := make([]string, 0, len(list))
	for _, c := range list {
		out = append(out, c.ID)
	}
	return out
}

// 目的: 境目より後の、信頼できる人間のコメントだけを古い順に選ぶことを固定する。
//
// **外部の人の閉じた記録は境目にしない。**境目を後ろへずらされると、許可が渡らなくなる。
// **AI の印・self_marker・隠されたコメント・外部の人のコメントは渡さない。**
//
// 与える情報: 境目より前の人間のコメント、OWNER の閉じた記録、そのあとの OWNER / MEMBER / COLLABORATOR の
// コメント、NONE のコメント、NONE の閉じた記録、`<!-- code-review-result -->` と self_marker の付いたコメント、
// 隠されたコメント。
// 成功条件: 結果が ok で、選ばれたのが境目より後の OWNER / MEMBER / COLLABORATOR の3件だけ（古い順）であること。
func TestRelaySelect_境目より後の信頼できる人間のコメントだけを渡す(t *testing.T) {
	cfg := *config.DefaultConfig()
	hidden := relayComment(9, "隠した許可", "OWNER", 9*time.Minute)
	hidden.IsMinimized = true
	comments := []tracker.Comment{
		relayComment(1, "前の許可（境目より前）", "OWNER", 1*time.Minute),
		relayComment(2, closedRecordBody, "OWNER", 2*time.Minute),
		relayComment(3, "issue を作ってよい", "OWNER", 3*time.Minute),
		relayComment(4, "外部の人の書き込み", "NONE", 4*time.Minute),
		relayComment(5, closedRecordBody, "NONE", 5*time.Minute),
		relayComment(6, "題名は README にして", "MEMBER", 6*time.Minute),
		relayComment(7, "<!-- code-review-result -->\nレビューの結果", "OWNER", 7*time.Minute),
		relayComment(8, cfg.Tracker.Comments.SelfMarker+"\nStatus を動かしました", "OWNER", 8*time.Minute),
		hidden,
		relayComment(10, "  \nラベルも付けて", "COLLABORATOR", 10*time.Minute),
	}

	verdict, picked := orchestrator.SelectRelayCommentsForTest(cfg, comments, false)

	if verdict != "ok" {
		t.Fatalf("結果が ok ではない: %s", verdict)
	}
	if got, want := strings.Join(relayIDs(picked), ","), "C3,C6,C10"; got != want {
		t.Errorf("選ばれたコメントが違う: got %s, want %s", got, want)
	}
}

// 目的: 閉じた記録が1件も無ければ、何も渡さないことを固定する（初めての着手と同じ）。
//
// 与える情報: 人間のコメントだけがあり、閉じた記録が無い issue。外部の人の閉じた記録が1件ある issue。
// 成功条件: どちらも結果が no_boundary であること。
func TestRelaySelect_閉じた記録が無ければ渡さない(t *testing.T) {
	cfg := *config.DefaultConfig()
	for name, comments := range map[string][]tracker.Comment{
		"記録が0件":     {relayComment(1, "issue を作ってよい", "OWNER", time.Minute)},
		"外部の人の記録だけ": {relayComment(1, closedRecordBody, "NONE", time.Minute), relayComment(2, "許可", "OWNER", 2*time.Minute)},
	} {
		t.Run(name, func(t *testing.T) {
			if verdict, _ := orchestrator.SelectRelayCommentsForTest(cfg, comments, false); verdict != "no_boundary" {
				t.Errorf("結果が no_boundary ではない: %s", verdict)
			}
		})
	}
}

// 目的: 境目より後に、信頼できる立場の AI の報告があれば、何も渡さないことを固定する（記録が確かめられないとき）。
//
// **前の回が閉じられたことを確かめられないためである。**機械が落ちて記録を書けなかったときなどに、
// 前の回の Claude Code が書いたものを人間の許可として渡さない。
// **`tracker.comments.marker` を別の値にしていても当たる。**
//
// 与える情報: 境目・人間のコメント・`<!-- continuo:agent -->` の報告。marker を変えた設定と、その印の報告。
// 外部の人が書いた `<!-- continuo:agent -->`。
// 成功条件: 前の2つは unverified、外部の人のものは ok（その1件は渡さない）であること。
func TestRelaySelect_境目より後にエージェントの報告があれば渡さない(t *testing.T) {
	cfg := *config.DefaultConfig()
	base := []tracker.Comment{
		relayComment(1, closedRecordBody, "OWNER", time.Minute),
		relayComment(2, "issue を作ってよい", "OWNER", 2*time.Minute),
	}

	withAgent := append(append([]tracker.Comment{}, base...), relayComment(3, "<!-- continuo:agent -->\n報告", "OWNER", 3*time.Minute))
	if verdict, _ := orchestrator.SelectRelayCommentsForTest(cfg, withAgent, false); verdict != "unverified" {
		t.Errorf("<!-- continuo:agent --> の報告があるのに unverified ではない: %s", verdict)
	}

	custom := cfg
	custom.Tracker.Comments.Marker = "<!-- my-agent -->"
	withCustom := append(append([]tracker.Comment{}, base...), relayComment(3, "<!-- my-agent -->\n報告", "OWNER", 3*time.Minute))
	if verdict, _ := orchestrator.SelectRelayCommentsForTest(custom, withCustom, false); verdict != "unverified" {
		t.Errorf("設定の marker の報告があるのに unverified ではない: %s", verdict)
	}

	byStranger := append(append([]tracker.Comment{}, base...), relayComment(3, "<!-- continuo:agent -->\n偽の報告", "NONE", 3*time.Minute))
	verdict, picked := orchestrator.SelectRelayCommentsForTest(cfg, byStranger, false)
	if verdict != "ok" || strings.Join(relayIDs(picked), ",") != "C2" {
		t.Errorf("外部の人の印で渡すのをやめている（外部の人が止められる）: %s %v", verdict, relayIDs(picked))
	}
}

// 目的: 境目と同じ秒のコメントは、URL の番号で前後を決めることを固定する。
//
// **GitHub の作成時刻は秒までしか無い。**番号の大きいほうを後とみなし、番号を読めないものは渡さない。
//
// 与える情報: 境目（番号 20）と同じ秒の、番号 21・19・番号の無いコメント。
// 成功条件: 番号 21 だけが選ばれること。
func TestRelaySelect_同じ秒は番号で前後を決める(t *testing.T) {
	cfg := *config.DefaultConfig()
	noNumber := relayComment(0, "番号の無い許可", "OWNER", time.Minute)
	noNumber.ID, noNumber.URL = "Cx", "https://github.com/octocat/hello-world/issues/12"
	comments := []tracker.Comment{
		relayComment(20, closedRecordBody, "OWNER", time.Minute),
		relayComment(21, "後の許可", "OWNER", time.Minute),
		relayComment(19, "前の許可", "OWNER", time.Minute),
		noNumber,
	}

	verdict, picked := orchestrator.SelectRelayCommentsForTest(cfg, comments, false)

	if verdict != "ok" || strings.Join(relayIDs(picked), ",") != "C21" {
		t.Errorf("同じ秒の前後の決め方が違う: %s %v", verdict, relayIDs(picked))
	}
}

// 目的: ページ数の上限で古い側を読み切れなかったときの扱いを固定する。
//
// **読めた中でいちばん古い更新時刻が、境目の作成時刻以下なら全部読めている**とみなす
// （コメントは更新日時の新しい順に読み、落ちるのは古い側だけである）。
//
// 与える情報: (1) 境目より古いコメントまで読めている (2) 境目が見つからない (3) 境目が編集されていて、
// 読めた中でいちばん古い更新時刻が境目の作成時刻より新しい。
// 成功条件: (1) は ok、(2) と (3) は incomplete であること。
func TestRelaySelect_読み切れなかったときは境目より後を読めたかで決める(t *testing.T) {
	cfg := *config.DefaultConfig()

	full := []tracker.Comment{
		relayComment(1, "古いコメント", "OWNER", time.Minute),
		relayComment(2, closedRecordBody, "OWNER", 2*time.Minute),
		relayComment(3, "許可", "OWNER", 3*time.Minute),
	}
	if verdict, _ := orchestrator.SelectRelayCommentsForTest(cfg, full, true); verdict != "ok" {
		t.Errorf("境目より古いものまで読めているのに ok ではない: %s", verdict)
	}

	noBoundary := []tracker.Comment{relayComment(3, "許可", "OWNER", 3*time.Minute)}
	if verdict, _ := orchestrator.SelectRelayCommentsForTest(cfg, noBoundary, true); verdict != "incomplete" {
		t.Errorf("境目を読めていないのに incomplete ではない: %s", verdict)
	}

	edited := relayComment(2, closedRecordBody, "OWNER", 2*time.Minute)
	edited.UpdatedAt = relayBase.Add(4 * time.Minute)
	editedOnly := []tracker.Comment{edited, relayComment(5, "許可", "OWNER", 5*time.Minute)}
	if verdict, _ := orchestrator.SelectRelayCommentsForTest(cfg, editedOnly, true); verdict != "incomplete" {
		t.Errorf("境目の作成時刻より前を読めていないのに incomplete ではない: %s", verdict)
	}
}

// 目的: 節の長さの上限の詰め方を固定する。
//
// **新しいものから本文を丸ごと入れ、最初に入らなかった1件で止める**（それより古いものは短くても入れない）。
// 途中を飛ばすと、長い取り消しだけが落ちて古い許可が入りうる。**本文を途中で切らない。**
//
// 与える情報: 古い順に「短い許可」「上限を超える長い取り消し」「短い新しい許可」の3件。
// 成功条件: 新しい1件だけが本文ごと入り、「入りきらなかった、より古いコメントが 2 件」の1行があり、
// 古い短い許可は入らず、節の全体が上限以下であること。
func TestRelaySection_上限を超えたら最初に入らなかった1件で止める(t *testing.T) {
	long := strings.Repeat("あ", orchestrator.RelayMaxRunesForTest)
	picked := []tracker.Comment{
		relayComment(1, "古い許可", "OWNER", time.Minute),
		relayComment(2, "取り消し"+long, "OWNER", 2*time.Minute),
		relayComment(3, "新しい許可", "OWNER", 3*time.Minute),
	}

	got := orchestrator.BuildRelaySectionForTest(picked)

	if !strings.Contains(got, "# 権限確認済みの人間からのメッセージ") {
		t.Errorf("見出しが無い:\n%.300s", got)
	}
	if !strings.Contains(got, "新しい許可") {
		t.Errorf("いちばん新しい1件が入っていない:\n%.300s", got)
	}
	if strings.Contains(got, "古い許可") || strings.Contains(got, "取り消し") {
		t.Errorf("入らなかった1件より古いものが入っている:\n%.300s", got)
	}
	if !strings.Contains(got, "入りきらなかった、より古いコメントが 2 件あります") {
		t.Errorf("入りきらなかった件数の1行が無い:\n%.300s", got)
	}
	if n := utf8.RuneCountInString(got); n > orchestrator.RelayMaxRunesForTest {
		t.Errorf("節が上限を超えている: %d > %d", n, orchestrator.RelayMaxRunesForTest)
	}
}

// 目的: いちばん新しい1件が上限を超えるときは、本文が0件になることを固定する（途中で切らない）。
//
// 与える情報: 上限を超える1件だけ。
// 成功条件: その本文が1文字も入らず、「入りきらなかった、より古いコメントが 1 件」の1行があること。
func TestRelaySection_最新の1件が上限を超えたら本文を入れない(t *testing.T) {
	picked := []tracker.Comment{relayComment(1, "許可"+strings.Repeat("い", orchestrator.RelayMaxRunesForTest), "OWNER", time.Minute)}

	got := orchestrator.BuildRelaySectionForTest(picked)

	if strings.Contains(got, "許可い") {
		t.Errorf("上限を超える本文が途中で切られて入っている:\n%.300s", got)
	}
	if !strings.Contains(got, "入りきらなかった、より古いコメントが 1 件あります") {
		t.Errorf("入りきらなかった件数の1行が無い:\n%.300s", got)
	}
}

// 目的: 本文の制御文字を、改行とタブを除いて落とすことを固定する。
//
// 与える情報: ベル文字・エスケープ・CR を含み、改行とタブも含む本文。
// 成功条件: ベル文字・エスケープ・CR が無く、改行とタブが残り、URL と見出しが入ること。
func TestRelaySection_制御文字を落とす(t *testing.T) {
	picked := []tracker.Comment{relayComment(7, "許可\x07します\r\n\t次の行\x1b[31m", "OWNER", time.Minute)}

	got := orchestrator.BuildRelaySectionForTest(picked)

	for _, ng := range []string{"\x07", "\x1b", "\r"} {
		if strings.Contains(got, ng) {
			t.Errorf("制御文字 %q が残っている: %q", ng, got)
		}
	}
	if !strings.Contains(got, "許可します\n\t次の行[31m") {
		t.Errorf("改行とタブが残っていない: %q", got)
	}
	if !strings.Contains(got, "## 1件目（https://github.com/octocat/hello-world/issues/12#issuecomment-7）") {
		t.Errorf("1件目の見出しに URL が無い: %q", got)
	}
	if orchestrator.BuildRelaySectionForTest(nil) != "" {
		t.Error("0件なのに節を組み立てている")
	}
}

// 目的: relay が有効になる条件を固定する（auto・relay_trusted_comments・self_marker が空でない）。
//
// **self_marker が空だと、continuo 自身の「Status を動かしました」に目印が付かず、人間のコメントとして渡る。**
//
// 与える情報: 既定の設定と、1つずつ条件を外した設定。
// 成功条件: 既定だけが true であること。
func TestRelayEnabled_3つの条件がそろったときだけ有効(t *testing.T) {
	base := *config.DefaultConfig()
	if !orchestrator.RelayEnabledForTest(base) {
		t.Fatal("既定の設定で relay が無効になっている（既定は有効）")
	}
	dontAsk := base
	dontAsk.Claude.PermissionMode = config.ClaudePermissionModeDontAsk
	off := base
	off.Agent.RelayTrustedComments = false
	noSelf := base
	noSelf.Tracker.Comments.SelfMarker = ""
	for name, cfg := range map[string]config.Config{"dontAsk": dontAsk, "設定が false": off, "self_marker が空": noSelf} {
		if orchestrator.RelayEnabledForTest(cfg) {
			t.Errorf("%s なのに relay が有効になっている", name)
		}
	}
}
