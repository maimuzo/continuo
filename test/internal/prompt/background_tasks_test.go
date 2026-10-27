package prompt_test

import (
	"strings"
	"testing"
)

// backgroundDemandMarker は「バックグラウンドの処理を終わらせてから表明を書く」文を見分ける目印である。
// 値の名前（`blocked` / `review`）を含まない部分だけを使う（片方が消えたときに、
// 「文が無い」と「文はあるが値が抜けた」を区別するため）。
// **句点は含めない。**文面は「…全部終わらせてください（待てないものは止めます）。」で、括弧の注記が句点の前に入る。
const backgroundDemandMarker = "を書く前に、バックグラウンドで動かしたコマンドと subagent を全部終わらせてください"

// 目的: continuo が送る組み込みのプロンプトが、`blocked` か `review` を書く前に、
// バックグラウンドの処理を全部終わらせるよう求めることを固定する（issue #275）。
//
// **なぜ要るか。**continuo は、`Stop` hook の `background_tasks` が空でないあいだ、
// turn が終わったとみなさない（設計 3-2）。表明を読むのは turn の終わりを確定したあとなので、
// バックグラウンドのコマンドや subagent を残したまま `blocked` を書くと、
// **Status がいつまでも `Blocked` へ動かない。**
//
// **`working` には当てない。**`working` は「まだ続ける」の表明で、長いビルドを
// バックグラウンドで回したまま応答を返す使い方を禁じることになる。
//
// 与える情報: instructionsText(t) の全文。
// 成功条件: その文が1つだけあり、`blocked` と `review` を名指しし、`working` を名指ししていない。
// コマンドと subagent の両方を挙げ、表明のあとに新しく起動しないよう求めている。
// **その文が「runの終了を宣言する」の中に在り、表明の3行の見本より後ろに在る。**
func TestTemplate_組み込みのプロンプトは表明の前にバックグラウンドの処理を終わらせる(t *testing.T) {
	body := instructionsText(t)

	var found []string
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, backgroundDemandMarker) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("バックグラウンドの処理を終わらせる文が %d 行あります（1行のはず）。"+
			"残したまま `blocked` を書くと、Status がいつまでも動きません:\n  %s",
			len(found), strings.Join(found, "\n  "))
	}

	line := found[0]
	for _, value := range []string{"blocked", "review"} {
		if !strings.Contains(line, "`"+value+"`") {
			t.Errorf("その文が `%s` を名指ししていません（issue #275）: %q", value, line)
		}
	}
	if strings.Contains(line, "`working`") {
		t.Errorf("その文が `working` を名指ししています。"+
			"`working` はバックグラウンドで回したまま応答を返してよい表明です: %q", line)
	}

	for _, want := range []string{
		"continuo は処理が1つでも残っているあいだ turn が終わったとみなさず",
		"（待てないものは止めます）",
		"この1行を書いたあとに、新しくコマンドも subagent も起動しないでください。",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("組み込みのプロンプトに %q がありません", want)
		}
	}

	head := strings.Index(body, "CONTINUO-STATUS: working    まだ続きがある")
	if section := strings.Index(body, "## runの終了を宣言する"); section < 0 || head < section {
		t.Fatalf("「runの終了を宣言する」の表明の3行の見本を見つけられません（節=%d 見本=%d）", section, head)
	}
	demand := strings.Index(body, backgroundDemandMarker)
	// **次の節の頭までに在ること。**手順がスキルへ移り（issue #288）、「runの終了を宣言する」の次は
	// 同じスキル「finishing-run」の「まとめて直したとき」である。
	next := -1
	if head >= 0 {
		if i := strings.Index(body[head:], "\n## "); i >= 0 {
			next = head + i
		}
	}
	if head < 0 || next < 0 || demand < head || demand > next {
		t.Errorf("その文が、「runの終了を宣言する」の表明の3行の見本より後ろにありません（見本=%d 文=%d 次の章=%d）。"+
			"表明の書き方を決めている節に置かないと、表明を書くときに読まれません", head, demand, next)
	}
}
