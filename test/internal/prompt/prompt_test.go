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
	"github.com/maimuzo/continuo/internal/scaffold"
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
	frag := prompt.Build("## 固有\n中身\n", "/tmp/WORKFLOW.md")
	if strings.Contains(frag.Text(), prompt.Marker) {
		t.Error("組み立てた全文に目印が残っています")
	}
}

// 目的: WORKFLOW.md の本文が、組み込みの前半と後半のあいだに入ることを固定する（設計 5-3c）。
//
// **末尾に足す形にすると、表明の1行の説明より後ろに利用者の文が来る。**
// **最後に読む文が仕組みの側であるようにする**のが、この並びの唯一の目的である。
//
// **本文を「全文の差し替え」として扱っていたら、この検査は落ちる。**
// 組み込みの見出しが1つも出てこないためである。
//
// 与える情報: 本文に `## 固有の目印` だけを置いたもの。
// 成功条件: 組み込みの前半の最後の見出し・本文・組み込みの後半の最初の見出しが、この順に並ぶこと。
func TestBuild_本文は組み込みの真ん中に挟まる(t *testing.T) {
	const needle = "## 固有の目印"
	// **見出しだけの本文は落とされる**（prompt.Build の中の dropEmptySections）。中身を持たせる。
	frag := prompt.Build(needle+"\n固有の中身\n", "/tmp/WORKFLOW.md")
	got := frag.Text()

	head := strings.Index(got, "## 4-3. 関連する記録を読む")
	mid := strings.Index(got, needle)
	tail := strings.Index(got, "# 5. 共通ルール")

	if head < 0 || mid < 0 || tail < 0 {
		t.Fatalf("組み立てた文面に見出しが揃っていません: head=%d mid=%d tail=%d", head, mid, tail)
	}
	if !(head < mid && mid < tail) {
		t.Errorf("本文が真ん中に入っていません（head=%d mid=%d tail=%d）。"+
			"末尾に足すと、表明の1行の説明より後ろに利用者の文が来ます", head, mid, tail)
	}
}

// 目的: 断片のあいだが必ず空行1つになることを固定する（設計 5-3c）。
//
// **固有のファイルが改行で終わっていないと、次の見出しが前の行にくっつく。**
// markdown として壊れ、エージェントは見出しを見失う。
//
// 与える情報: 改行で終わっていない本文。
// 成功条件: 本文の最後の行と、組み込みの後半の最初の見出しのあいだに、空行がちょうど1つあること。
func TestBuild_断片のあいだは空行1つになる(t *testing.T) {
	// **末尾に改行を付けない。**これが起きうる書き方である。
	frag := prompt.Build("## 固有の目印\n最後の行", "/tmp/WORKFLOW.md")
	got := frag.Text()

	// **見出しを書き写さない。**書き写すと、組み込みの後半の先頭に節が増えるたびに
	// この検査が落ちる。**見張りたいのは見出しの名前ではなく、あいだの空行の数である。**
	want := "最後の行\n\n" + firstLineOf(t, prompt.BuiltinTail())
	if !strings.Contains(got, want) {
		i := strings.Index(got, "最後の行")
		end := i + 60
		if end > len(got) {
			end = len(got)
		}
		t.Errorf("断片のあいだが空行1つになっていません。\n  そのあたり: %q", got[i:end])
	}
}

// 目的: 本文が `{{if}}` を開いたまま終えても、組み込みの後半を飲み込まないことを固定する
// （設計 5-3c）。
//
// **3つを連結してから解釈すると、本文の `{{if}}` の中に仕組みの締めくくりが入る。**
// **別々に解釈していれば、本文の断片だけが誤りになる。**
//
// 与える情報: `{{if .attempt}}` を閉じていない本文。
// 成功条件: 検査が誤りを返し、その文言が本文の断片を名指ししていること。
func TestValidate_本文の閉じ忘れは本文の誤りとして出る(t *testing.T) {
	frag := prompt.Build("{{if .attempt}}閉じ忘れ\n", "/tmp/WORKFLOW.md")
	err := frag.Validate()
	if err == nil {
		t.Fatal("閉じ忘れを見逃しました")
	}
	if !strings.Contains(err.Error(), prompt.NameWorkflowBody) {
		t.Errorf("誤りの文言が %s を名指ししていません: %v", prompt.NameWorkflowBody, err)
	}
}

// 目的: 一覧に無い変数を書いたら、起動の時点で誤りになることを固定する（設計 5-3c）。
//
// 与える情報: `{{.issue.nope}}` を書いた本文。
// 成功条件: 検査が誤りを返すこと。
func TestValidate_知らない変数を止める(t *testing.T) {
	frag := prompt.Build("{{.issue.nope}}\n", "/tmp/WORKFLOW.md")
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
// 与える情報: `{{index .issue "nope"}}` を書いた本文。
// 成功条件: 検査が誤りを返すこと。
func TestValidate_indexは封じてある(t *testing.T) {
	frag := prompt.Build(`{{index .issue "nope"}}`+"\n", "/tmp/WORKFLOW.md")
	if err := frag.Validate(); err == nil {
		t.Fatal("index が素通りしました（Funcs で封じられていません）")
	}
}

// 目的: `{{if .attempt}}` の中の誤りも、起動の時点で見つかることを固定する（設計 5-3c）。
//
// **1回しか変数展開しないと、この誤りは見つからない。**`.attempt` は1回目が空なので、
// 中は一度も解釈されず、**やり直しが起きるまで表に出ない。**
//
// 与える情報: `{{if .attempt}}` の中にだけ知らない変数を書いた本文。
// 成功条件: 検査が誤りを返すこと。
func TestValidate_attemptの中の誤りも見つかる(t *testing.T) {
	frag := prompt.Build("{{if .attempt}}{{.issue.nope}}{{end}}\n", "/tmp/WORKFLOW.md")
	if err := frag.Validate(); err == nil {
		t.Fatal("{{if .attempt}} の中の誤りを見逃しました（変数展開が1回しか走っていません）")
	}
}

// 目的: 組み込みそのものが、決められた変数だけで変数展開できることを固定する（設計 5-3c）。
//
// **壊れた組み込みを配ると、利用者の側では直しようが無い。**
//
// 与える情報: 本文が空の組み立て。
// 成功条件: 検査が誤りを返さないこと。
func TestValidate_組み込みだけなら通る(t *testing.T) {
	frag := prompt.Build("", "/tmp/WORKFLOW.md")
	if err := frag.Validate(); err != nil {
		t.Fatalf("組み込みのプロンプトが検査を通りません: %v", err)
	}
}

// 目的: `continuo init` が置く WORKFLOW.md の本文が、そのまま送れる形であることを固定する
// （設計 5-3d）。
//
// **雛形が起動を止める形で配られると、`continuo init` の直後に continuo が起動しない。**
//
// 与える情報: scaffold.Template() の front matter より後ろを本文として置いた組み立て。
// 成功条件: 本文が空でなく、検査が誤りを返さないこと。
func TestValidate_雛形の本文はそのまま送れる(t *testing.T) {
	body := templateBody(t)
	if strings.TrimSpace(body) == "" {
		t.Fatal("WORKFLOW.md の雛形に本文がありません（固有の指示の見本が消えています）")
	}
	frag := prompt.Build(body, "/tmp/WORKFLOW.md")
	if err := frag.Validate(); err != nil {
		t.Fatalf("WORKFLOW.md の雛形の本文が検査を通りません: %v", err)
	}
}

// 目的: 雛形の本文が、組み込みの真ん中に挟まって送られることを固定する（設計 5-3c / 5-3d）。
//
// **`continuo init` の直後に送られる文面が、組み込み + 本文 + 組み込みであることを見る。**
// 本文を全文の差し替えとして扱っていたら、組み込みの見出しが1つも出てこない。
//
// 与える情報: scaffold.Template() の本文。
// 成功条件: 組み込みの前半・本文の見出し・組み込みの後半が、この順に並ぶこと。
func TestBuild_雛形の本文は組み込みの真ん中に挟まる(t *testing.T) {
	got := prompt.Build(templateBody(t), "/tmp/WORKFLOW.md").Text()

	head := strings.Index(got, "## 4-3. 関連する記録を読む")
	mid := strings.Index(got, "### 始める前に読む文書")
	tail := strings.Index(got, "# 5. 共通ルール")

	if head < 0 || mid < 0 || tail < 0 {
		t.Fatalf("組み立てた文面に見出しが揃っていません: head=%d mid=%d tail=%d", head, mid, tail)
	}
	if !(head < mid && mid < tail) {
		t.Errorf("雛形の本文が真ん中に入っていません（head=%d mid=%d tail=%d）", head, mid, tail)
	}
}

// templateBody は WORKFLOW.md の雛形から、front matter より後ろを取り出す。
//
// **閉じの `---` だけを行として持つ2行目以降を探す。**front matter の中にも
// `---` で始まる行はあるので、行そのものが `---` であることを見る。
//
// t: テストの文脈。
// 戻り値: 雛形の本文。
func templateBody(t *testing.T) string {
	t.Helper()
	lines := strings.Split(scaffold.Template(), "\n")
	seen := 0
	for i, line := range lines {
		if strings.TrimRight(line, " \t") != "---" {
			continue
		}
		seen++
		if seen == 2 {
			return strings.Join(lines[i+1:], "\n")
		}
	}
	t.Fatalf("WORKFLOW.md の雛形に front matter の閉じの --- がありません（見つかった --- は %d 行）", seen)
	return ""
}

// 目的: 本文が空白だけなら、何も足さずに組み立てることを固定する（設計 5-3c）。
//
// **「固有の指示は要らないが、front matter は要る」を成り立たせる。**
//
// 与える情報: 空白だけの本文。
// 成功条件: 本文を空にした組み立てと1バイトも違わないこと。
func TestBuild_本文が空白だけなら何も足さない(t *testing.T) {
	got := prompt.Build("   \n\n", "/tmp/WORKFLOW.md")
	want := prompt.Build("", "/tmp/WORKFLOW.md")
	if got.Text() != want.Text() {
		t.Errorf("空白だけの本文が文面を変えています\n  got:  %q\n  want: %q",
			tailOf(got.Text()), tailOf(want.Text()))
	}
	if got.HasBody() {
		t.Error("空白だけの本文が「本文がある」と数えられています")
	}
}

// 目的: 本文の有無を HasBody が正しく返すことを固定する（設計 5-3f）。
//
// **`continuo prompt --show` の内訳と doctor の文言が、これで分かれる。**
//
// **判定は、コメントと空の見出しを落としたあとで行う。**落とす前で決めると、
// 本文が案内のコメントだけだったときに「本文はあります」と言いながら断片は足されず、
// **内訳から本文の行が丸ごと消える。**
//
// 与える情報: 中身のある本文 / 空の本文 / 見出しだけの本文 / 案内のコメントだけの本文。
// 成功条件: 中身があるときだけ真になり、パスはどれでも埋まっていること。
func TestBuild_本文の有無とパスを返す(t *testing.T) {
	const path = "/tmp/WORKFLOW.md"
	if got := prompt.Build("## 何か\n\n中身です。\n", path); !got.HasBody() || got.BodyPath() != path {
		t.Errorf("本文があるのに HasBody=%v BodyPath=%q です", got.HasBody(), got.BodyPath())
	}
	for _, body := range []struct {
		name string
		text string
	}{
		{"空", ""},
		{"見出しだけ", "## 何か\n"},
		{"案内のコメントだけ", "## 何か\n<!-- ここに書いてください -->\n"},
	} {
		if got := prompt.Build(body.text, path); got.HasBody() || got.BodyPath() != path {
			t.Errorf("%s の本文なのに HasBody=%v BodyPath=%q です",
				body.name, got.HasBody(), got.BodyPath())
		}
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

// firstLineOf は、断片の前後の改行を落としたうえで最初の1行を返す。
//
// **`prompt.Build` は断片の前後の改行を落としてから連結する**（`join`）ので、
// 連結後に本文の次へ来るのはこの1行である。
//
// t: テストコンテキスト。
// text: 取り出す元の断片。
// 戻り値: 最初の1行。**空の断片を渡したら、その場で落とす。**
func firstLineOf(t *testing.T, text string) string {
	t.Helper()

	trimmed := strings.Trim(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if trimmed == "" {
		t.Fatal("組み込みの後半が空です（目印の行が最後にあります）")
	}
	line, _, _ := strings.Cut(trimmed, "\n")
	return line
}

// 目的: 中身が案内のコメントだけになった見出しが、送る文面から落ちることを固定する
// （#188（エージェントへ送る指示書が長く、順序も強調も揃っていないため、初見で読み取れない）。設計 5-3m）。
//
// **なぜ落とすか。**`continuo init` が作る WORKFLOW.md は、節の中身を
// `<!-- ここに書いてください -->` という案内のコメントで置いている。
// **人間が書き込む前は、コメントを取り除いた時点でその節が空になる。**
// 空の見出しだけを送っても、エージェントには何も伝わらない。
// **それを残すと、この issue が直そうとしている「長くて読み取れない」がそのまま残る。**
//
// **落とさないものが2つある。**コードブロックで囲んだコメントと、
// **深い見出しを従えている見出し**である。後者を落とすと、
// `# 5. 共通ルール` のような親の見出しが、子を残したまま消える。
//
// 与える情報: 4通りの本文。
// 成功条件: 空になった見出しだけが落ち、残り3つが残ること。
func TestBuild_中身が無くなった見出しは落ちる(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want bool // 見出しが残るか
	}{
		{"中身が案内のコメントだけ", "## 消える節\n<!-- ここに書いてください -->\n", false},
		{"中身がある", "## 残る節\n\n中身です。\n", true},
		{"コメントをコードブロックで囲んだ", "## 残る節\n\n```\n<!-- これは送ります -->\n```\n", true},
		{"深い見出しを従えている", "## 残る節\n\n### 子の見出し\n\n中身です。\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := prompt.Build(tc.body, "/tmp/WORKFLOW.md").Text()
			heading := "## 消える節"
			if tc.want {
				heading = "## 残る節"
			}
			if has := strings.Contains(got, heading); has != tc.want {
				t.Errorf("%q が %v であるべきなのに %v です。全文:\n%s", heading, tc.want, has, got)
			}
		})
	}
}
