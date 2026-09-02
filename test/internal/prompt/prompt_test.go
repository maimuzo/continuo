// Package prompt_test は、continuo が Claude Code へ最初に送る指示書の検査である
// （設計 5-3 / 5-3c / 5-3d）。
//
// **ここが守るのは「送る文面が壊れていないこと」だけである。**
// front matter の検査は test/internal/scaffold にある。
package prompt_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/prompt"
)

// 目的: builtin.md の目印の行がちょうど1つあることを固定する（設計 5-3c）。
//
// **0個だと後半が空になり、表明の1行の説明ごと消える。**
// **2個以上だと、どこで切ったのかが読む人に決まらない。**
// どちらもビルドは通り、テストが無ければ配ってから気づくことになる。
//
// 与える情報: prompt.BuiltinRaw()。
// 成功条件: 目印だけの行がちょうど1行あること。
func TestBuiltin_目印がちょうど1つある(t *testing.T) {
	count := 0
	for _, line := range strings.Split(prompt.BuiltinRaw(), "\n") {
		if strings.TrimSpace(line) == prompt.Marker {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("builtin.md の目印（%s）が %d 行あります。ちょうど1行にしてください",
			prompt.Marker, count)
	}
}

// 目的: 目印の行そのものが、送る文面に残らないことを固定する（設計 5-3c）。
//
// **残ると、エージェントへ送る指示書に continuo の内部の目印が混ざる。**
//
// 与える情報: prompt.Builtin() と prompt.Build() が組み立てた全文。
// 成功条件: どちらにも目印の文字列が1つも無いこと。
func TestBuiltin_目印は送る文面に残らない(t *testing.T) {
	if strings.Contains(prompt.Builtin(), prompt.Marker) {
		t.Error("組み込みだけの全文に目印が残っています")
	}
	frag := prompt.Build("", "## 固有\n", "/tmp/PROJECT_SPECIFIC_PROMPT.md", true)
	if strings.Contains(frag.Text(), prompt.Marker) {
		t.Error("組み立てた全文に目印が残っています")
	}
}

// 目的: 固有のプロンプトが、組み込みの前半と後半のあいだに入ることを固定する（設計 5-3c）。
//
// **末尾に足す形にすると、表明の1行の説明より後ろに利用者の文が来る。**
// **最後に読む文が仕組みの側であるようにする**のが、この並びの唯一の目的である。
//
// 与える情報: 固有のプロンプトに `## 固有の目印` だけを置いたもの。
// 成功条件: 組み込みの前半の最後の見出し・固有・組み込みの後半の最初の見出しが、この順に並ぶこと。
func TestBuild_固有は組み込みの真ん中に挟まる(t *testing.T) {
	const needle = "## 固有の目印"
	frag := prompt.Build("", needle+"\n", "/tmp/PROJECT_SPECIFIC_PROMPT.md", true)
	got := frag.Text()

	head := strings.Index(got, "## この issue に紐づく PR も読むこと")
	mid := strings.Index(got, needle)
	tail := strings.Index(got, "## 終わったらやること")

	if head < 0 || mid < 0 || tail < 0 {
		t.Fatalf("組み立てた文面に見出しが揃っていません: head=%d mid=%d tail=%d", head, mid, tail)
	}
	if !(head < mid && mid < tail) {
		t.Errorf("固有が真ん中に入っていません（head=%d mid=%d tail=%d）。"+
			"末尾に足すと、表明の1行の説明より後ろに利用者の文が来ます", head, mid, tail)
	}
}

// 目的: 断片のあいだが必ず空行1つになることを固定する（設計 5-3c）。
//
// **固有のファイルが改行で終わっていないと、次の見出しが前の行にくっつく。**
// markdown として壊れ、エージェントは見出しを見失う。
//
// 与える情報: 改行で終わっていない固有のプロンプト。
// 成功条件: 固有の最後の行と、組み込みの後半の最初の見出しのあいだに、空行がちょうど1つあること。
func TestBuild_断片のあいだは空行1つになる(t *testing.T) {
	// **末尾に改行を付けない。**これが起きうる書き方である。
	frag := prompt.Build("", "## 固有の目印\n最後の行", "/tmp/PROJECT_SPECIFIC_PROMPT.md", true)
	got := frag.Text()

	const want = "最後の行\n\n## 終わったらやること"
	if !strings.Contains(got, want) {
		i := strings.Index(got, "最後の行")
		end := i + 60
		if end > len(got) {
			end = len(got)
		}
		t.Errorf("断片のあいだが空行1つになっていません。\n  そのあたり: %q", got[i:end])
	}
}

// 目的: 固有の側が `{{if}}` を開いたまま終えても、組み込みの後半を飲み込まないことを固定する
// （設計 5-3c）。
//
// **3つを連結してから解釈すると、固有の `{{if}}` の中に仕組みの締めくくりが入る。**
// **別々に解釈していれば、固有の断片だけが誤りになる。**
//
// 与える情報: `{{if .attempt}}` を閉じていない固有のプロンプト。
// 成功条件: 検査が誤りを返し、その文言が固有のファイルの名前を名指ししていること。
func TestValidate_固有の閉じ忘れは固有の誤りとして出る(t *testing.T) {
	frag := prompt.Build("", "{{if .attempt}}閉じ忘れ\n", "/tmp/PROJECT_SPECIFIC_PROMPT.md", true)
	err := frag.Validate()
	if err == nil {
		t.Fatal("閉じ忘れを見逃しました")
	}
	if !strings.Contains(err.Error(), prompt.ProjectFileName) {
		t.Errorf("誤りの文言が %s を名指ししていません: %v", prompt.ProjectFileName, err)
	}
}

// 目的: 一覧に無い変数を書いたら、起動の時点で誤りになることを固定する（設計 5-3c）。
//
// 与える情報: `{{.issue.nope}}` を書いた固有のプロンプト。
// 成功条件: 検査が誤りを返すこと。
func TestValidate_知らない変数を止める(t *testing.T) {
	frag := prompt.Build("", "{{.issue.nope}}\n", "/tmp/PROJECT_SPECIFIC_PROMPT.md", true)
	if err := frag.Validate(); err == nil {
		t.Fatal("知らない変数を見逃しました（missingkey=error が効いていません）")
	}
}

// 目的: `index` の逃げ道が塞がれていることを固定する（設計 5-3c）。
//
// **`missingkey=error` が見るのは `.foo` の形だけである。**
// `{{index .issue "nope"}}` は誤りにならず、何も出さずに素通りする。
// **逃げ道が1つでも残っていると、doctor の「知らない変数はありません」が嘘になる。**
//
// 与える情報: `{{index .issue "nope"}}` を書いた固有のプロンプト。
// 成功条件: 検査が誤りを返すこと。
func TestValidate_indexは封じてある(t *testing.T) {
	frag := prompt.Build("", `{{index .issue "nope"}}`+"\n", "/tmp/PROJECT_SPECIFIC_PROMPT.md", true)
	if err := frag.Validate(); err == nil {
		t.Fatal("index が素通りしました（Funcs で封じられていません）")
	}
}

// 目的: `{{if .attempt}}` の中の誤りも、起動の時点で見つかることを固定する（設計 5-3c）。
//
// **1回しか変数展開しないと、この誤りは見つからない。**`.attempt` は1回目が空なので、
// 中は一度も解釈されず、**やり直しが起きるまで表に出ない。**
//
// 与える情報: `{{if .attempt}}` の中にだけ知らない変数を書いた固有のプロンプト。
// 成功条件: 検査が誤りを返すこと。
func TestValidate_attemptの中の誤りも見つかる(t *testing.T) {
	frag := prompt.Build("", "{{if .attempt}}{{.issue.nope}}{{end}}\n", "/tmp/PROJECT_SPECIFIC_PROMPT.md", true)
	if err := frag.Validate(); err == nil {
		t.Fatal("{{if .attempt}} の中の誤りを見逃しました（変数展開が1回しか走っていません）")
	}
}

// 目的: 組み込みそのものが、決められた変数だけで変数展開できることを固定する（設計 5-3c）。
//
// **壊れた組み込みを配ると、利用者の側では直しようが無い。**
//
// 与える情報: 固有のプロンプトを置かない組み立て。
// 成功条件: 検査が誤りを返さないこと。
func TestValidate_組み込みだけなら通る(t *testing.T) {
	frag := prompt.Build("", "", "/tmp/PROJECT_SPECIFIC_PROMPT.md", false)
	if err := frag.Validate(); err != nil {
		t.Fatalf("組み込みのプロンプトが検査を通りません: %v", err)
	}
}

// 目的: `continuo init` が置く固有のプロンプトの雛形が、そのまま送れる形であることを固定する。
//
// **雛形が起動を止める形で配られると、`continuo init` の直後に continuo が起動しない。**
//
// 与える情報: prompt.ProjectTemplate() を固有のプロンプトとして置いた組み立て。
// 成功条件: 検査が誤りを返さないこと。
func TestValidate_固有の雛形はそのまま送れる(t *testing.T) {
	frag := prompt.Build("", prompt.ProjectTemplate(), "/tmp/PROJECT_SPECIFIC_PROMPT.md", true)
	if err := frag.Validate(); err != nil {
		t.Fatalf("固有のプロンプトの雛形が検査を通りません: %v", err)
	}
}

// 目的: 本文が残っていたら組み込みを送らないことを固定する（設計 5-3d）。
//
// **本文と組み込みを両方送ると、表明の1行の説明が版違いで2回届く。**
//
// 与える情報: 本文と固有のプロンプトの両方。
// 成功条件: 組み立てた全文に本文と固有が入り、組み込みの見出しが1つも入らないこと。
func TestBuild_本文が残っていれば組み込みを送らない(t *testing.T) {
	frag := prompt.Build("残っている本文\n", "## 固有の目印\n", "/tmp/PROJECT_SPECIFIC_PROMPT.md", true)
	if !frag.Compat() {
		t.Fatal("本文が残っているのに互換の経路として扱われていません")
	}
	got := frag.Text()
	for _, want := range []string{"残っている本文", "## 固有の目印"} {
		if !strings.Contains(got, want) {
			t.Errorf("組み立てた全文に %q がありません", want)
		}
	}
	if strings.Contains(got, "## 終わったらやること") {
		t.Error("本文が残っているのに組み込みのプロンプトが送られています（版違いの説明が2回届きます）")
	}
}

// 目的: 固有のプロンプトが空白だけなら、何も足さずに組み立てることを固定する（設計 5-3c）。
//
// **「消したいが、ファイルは残しておきたい」を成り立たせる。**
//
// 与える情報: 空白だけの固有のプロンプト。
// 成功条件: 組み込みだけを組み立てたものと1バイトも違わないこと。
func TestBuild_固有が空白だけなら何も足さない(t *testing.T) {
	got := prompt.Build("", "   \n\n", "/tmp/PROJECT_SPECIFIC_PROMPT.md", true).Text()
	want := prompt.Build("", "", "/tmp/PROJECT_SPECIFIC_PROMPT.md", false).Text()
	if got != want {
		t.Errorf("空白だけの固有のプロンプトが文面を変えています\n  got:  %q\n  want: %q",
			tailOf(got), tailOf(want))
	}
}

// tailOf は、食い違いを出すときに末尾だけを見せる。
//
// s: 対象の文字列。
// 戻り値: 末尾の120文字（短ければ全部）。
func tailOf(s string) string {
	if len(s) <= 120 {
		return s
	}
	return "…" + s[len(s)-120:]
}

// 目的: 送る文面の変数が、設計 5-3 の一覧の9つだけであることを固定する。
//
// **一覧に無い名前を組み込みへ書くと、利用者の側では直しようが無い。**
// **一覧に在る名前を実装が渡さなくなると、起動が止まる。**
// どちらも「一覧と実装のずれ」なので、作り物の issue の変数を1つずつ落として確かめる。
//
// 与える情報: prompt.SampleData() から名前を1つ落とした変数。
// 成功条件: `.issue` の8つはどれを落としても誤りになり、9つ揃えば通ること。
func TestSampleData_9つの変数がそろっている(t *testing.T) {
	want := []string{"identifier", "owner", "repo", "number", "url", "title", "state", "labels"}
	data := prompt.SampleData()
	issue, ok := data["issue"].(map[string]any)
	if !ok {
		t.Fatal("SampleData の issue が map ではありません")
	}
	if len(issue) != len(want) {
		t.Errorf("issue の変数が %d 個あります（設計 5-3 の一覧は %d 個です）", len(issue), len(want))
	}
	for _, name := range want {
		v, ok := issue[name]
		if !ok {
			t.Errorf("SampleData に .issue.%s がありません", name)
			continue
		}
		// **空でない値を入れる。**空文字だと `{{if .issue.title}}` の中が検査されない。
		if s, isStr := v.(string); isStr && s == "" {
			t.Errorf("SampleData の .issue.%s が空文字です（{{if}} の中が検査されません）", name)
		}
	}
	if _, ok := data["attempt"]; !ok {
		t.Error("SampleData に .attempt がありません（キーごと省くと missingkey=error で落ちます）")
	}
}
