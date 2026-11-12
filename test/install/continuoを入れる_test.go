// {"RUCM-CFG-SHA256": "23ec740c65d64de71d5203a87b74ba874fb2893e7c0b2b9d9fdb16beb4691b2c", "SOURCE": "docs/spec/usecases/particular_case/continuoを入れる.cfg.json"}
//
// **ユースケース記述「continuoを入れる」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。

//go:build unix

package install_test

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// {"RUCM-PATH": "P007"}
//
// Test_continuoを入れる_P007_版に使えない文字があれば取得の前に弾く は、URL への混入を防ぐことを確かめる。
//
// **`--version` の値は URL のパスに入る。**`../` を含む値を通すと、curl が送信前に
// パスを正規化し、**別のリポジトリの release に到達できる**（安全性のレビューで、
// 本物の github.com が 200 を返すことが確かめられた）。
//
// 目的: 使えない文字を含む版を、配布サーバへ繋ぐ前に弾くこと。
// 与える情報: `../` や空白を含む版。
// 成功条件: どれも終了コードが 0 でなく、偽サーバへ1度も繋がないこと。
func Test_continuoを入れる_P007_版に使えない文字があれば取得の前に弾く(t *testing.T) {
	fr := newFakeRelease(t, "v1.2.3", "#!/bin/sh\necho ok\n", true)
	// **1度でも繋がれたら記録する。**
	var reached atomic.Bool
	fr.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
		http.NotFound(w, r)
	})
	dir := t.TempDir()

	for _, bad := range []string{"../../other/v1", "v1.0.0/../../x", "v1 0", "$(id)"} {
		code, out := runInstaller(t, fr, dir, "--no-deps", "--version", bad)
		if code == 0 {
			t.Errorf("%q を受け付けています:\n%s", bad, out)
		}
		if !strings.Contains(out, "使える文字") {
			t.Errorf("%q で、使える文字を示していません:\n%s", bad, out)
		}
	}
	if reached.Load() {
		t.Error("弾いたはずなのに配布サーバへ繋いでいます")
	}
	if _, err := os.Stat(filepath.Join(dir, "continuo")); err == nil {
		t.Error("使えない版なのに実行ファイルを置いています")
	}
}

// {"RUCM-PATH": "P004"}
//
// Test_continuoを入れる_P004_helpはパイプ経由でも出る は、`curl … | sh -s -- --help` を確かめる。
//
// **`$0` は "sh" になるので、自分自身を読み直す作りだと壊れる。**
// 実際、`sed -n '2,30p' "$0"` で書いたときは `sed: sh: No such file or directory` になった。
//
// 目的: 標準入力からスクリプトを流し込んでも、案内が出ること。
// 与える情報: install.sh の中身を標準入力から流す。
// 成功条件: 終了コードが 0 で、オプションの一覧が出ること。
func Test_continuoを入れる_P004_helpはパイプ経由でも出る(t *testing.T) {
	body, err := os.ReadFile(scriptPath(t))
	if err != nil {
		t.Fatalf("install.sh を読めません: %v", err)
	}
	cmd := exec.Command(installShell, "-s", "--", "--help")
	cmd.Stdin = strings.NewReader(string(body))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("終了コードが 0 ではありません: %v\n%s", err, out)
	}
	text := string(out)
	for _, want := range []string{"--no-deps", "--dir", "--version", "herdr と claude は入れません"} {
		if !strings.Contains(text, want) {
			t.Errorf("案内に %q がありません:\n%s", want, text)
		}
	}
	if strings.Contains(text, "No such file") {
		t.Errorf("自分自身を読み直そうとして壊れています:\n%s", text)
	}
}

// {"RUCM-PATH": "P005"}
//
// Test_continuoを入れる_P005_知らないオプションは弾く は、引数の検査を確かめる。
//
// 目的: 知らないオプションで、何もせずに落ちること。
// 与える情報: `--bogus`。
// 成功条件: 終了コードが 0 でなく、置き先に何も無いこと。
func Test_continuoを入れる_P005_知らないオプションは弾く(t *testing.T) {
	dir := t.TempDir()
	code, out := runInstaller(t, nil, dir, "--bogus")
	if code == 0 {
		t.Fatalf("知らないオプションを受け付けています:\n%s", out)
	}
	if !strings.Contains(out, "知らないオプションです") {
		t.Errorf("理由を示していません:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "continuo")); err == nil {
		t.Error("引数が誤っているのに実行ファイルを置いています")
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_破壊的変更のある版へ上げると名指しで警告する は、上げる前の警告を確かめる。
//
// **設定ファイルは、未知のキーがあると起動を止める。**キーが増減した版へ上げると、
// **古い設定のまま起動しようとした時点で落ちる。**インストーラーが黙って上書きすると、
// **上げたあとに初めて気づくことになる。**
//
// **破壊的変更は、更新フィードの本文の見出し「破壊的変更」の下の箇条書きから読む**（設計 3-36）。
//
// 目的: 飛び越えて上げたとき、あいだの版の破壊的変更まで名指しで並べること。
// 与える情報: v0.2.0 を名乗る実行ファイルと、v0.3.0 と v0.10.0 に見出し「破壊的変更」がある更新フィード。
// 説明の1件は `<` と `'` と `&` を含む（本物と同じく、XML と HTML の2段に逃がされて届く）。
// 成功条件: 終了コードが 0 で、実行ファイルが入れ替わり、
//
//	v0.3.0 と v0.10.0 の説明が元の文字のまま出て、範囲の外（v0.1.0）の説明が出ず、
//	確かめられなかった版の知らせが出ないこと。
func Test_continuoを入れる_P001_破壊的変更のある版へ上げると名指しで警告する(t *testing.T) {
	fr := newFakeRelease(t, "v0.10.0", "#!/bin/sh\necho v0.10.0\n", true)
	fr.SetFeed(releasesFeed(t, []releaseNote{
		{Tag: "v0.10.0", Breaking: []string{"WORKFLOW.md に tracker.dispatch_state が要ります"}},
		{Tag: "v0.9.0"},
		{Tag: "v0.3.0", Breaking: []string{"claude.model の既定が変わりました", "ghq list -p -e <ホスト名>/<owner> の 'x' & y で引きます"}},
		{Tag: "v0.2.0"},
		{Tag: "v0.1.0", Breaking: []string{"これは範囲の外なので出てはいけません"}},
	}))
	dir := t.TempDir()
	// **いま入っているものを置く。**install.sh は置き換える前にここへ版を訊く。
	placeInstalled(t, dir, "v0.2.0")

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}

	// **止めない。**入れ替えたうえで警告する。
	got, err := exec.Command(filepath.Join(dir, "continuo")).Output()
	if err != nil {
		t.Fatalf("置いた実行ファイルを動かせません: %v", err)
	}
	if !strings.Contains(string(got), "v0.10.0") {
		t.Errorf("入れ替わっていません: %q", string(got))
	}

	if !strings.Contains(out, "破壊的変更があります") {
		t.Fatalf("破壊的変更を伝えていません:\n%s", out)
	}
	for _, want := range []string{
		"  v0.10.0  WORKFLOW.md に tracker.dispatch_state が要ります",
		"  v0.3.0  claude.model の既定が変わりました",
		"  v0.3.0  ghq list -p -e <ホスト名>/<owner> の 'x' & y で引きます",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q を名指ししていません:\n%s", want, out)
		}
	}
	// **v0.10.0 と v0.2.0 の大小を、文字列として比べてはならない。**
	// 取り違えると、範囲の外の v0.1.0 まで並ぶ。
	if strings.Contains(out, "これは範囲の外なので出てはいけません") {
		t.Errorf("いま入っている版より前の破壊的変更まで並べています:\n%s", out)
	}
	// **箇条書きのあとの段落は読まない。**
	if strings.Contains(out, "動いている continuo を止めてください") {
		t.Errorf("見出しの下の箇条書きより後ろまで読んでいます:\n%s", out)
	}
	if strings.Contains(out, "確かめられなかった版があります") {
		t.Errorf("全部読めているのに、確かめられなかったと言っています:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_日本語の見出しを英語の環境でも取り違えない は、awk の比べ方を確かめる。
//
// **macOS の /usr/bin/awk は `LANG=en_US.UTF-8` の下で、日本語どうしの `==` を等しいと返す**
// （`"できるようになったこと"=="破壊的変更"` が 1。2026-11-12 に実測）。install.sh は awk を全部
// `LC_ALL=C` で呼ぶ。付け忘れると、英語の環境の macOS では、どの見出しの下の箇条書きも破壊的変更として出る。
//
// 目的: `LANG=en_US.UTF-8` で走らせても、見出し「できるようになったこと」の下を破壊的変更と取り違えないこと。
// 与える情報: v0.2.0 を名乗る実行ファイルと、見出し「できるようになったこと」だけを持つ v0.3.0 の更新フィード。
// 成功条件: 終了コードが 0 で、「破壊的変更があります」も「確かめられなかった」も出ないこと。
func Test_continuoを入れる_P001_日本語の見出しを英語の環境でも取り違えない(t *testing.T) {
	fr := newFakeRelease(t, "v0.3.0", "#!/bin/sh\necho v0.3.0\n", true)
	fr.SetFeed(releasesFeed(t, []releaseNote{
		{Tag: "v0.3.0", HTML: "<h2>できるようになったこと</h2>\n\n<ul>\n<li>新しい機能の説明なので出てはいけません</li>\n</ul>"},
		{Tag: "v0.2.0"},
	}))
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.2.0")

	code, out := runInstallerEnv(t, fr, dir, []string{"LANG=en_US.UTF-8", "LC_ALL="}, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	if strings.Contains(out, "破壊的変更があります") || strings.Contains(out, "新しい機能の説明なので出てはいけません") {
		t.Errorf("見出し「できるようになったこと」を破壊的変更と取り違えています:\n%s", out)
	}
	if strings.Contains(out, "確かめられなかった版があります") {
		t.Errorf("読めているのに、確かめられなかったと言っています:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_英語の見出しBreaking_changesも読む は、fork の英語のノートを確かめる。
//
// **以前の印は言語に依らなかった。**見出しの文字で読むと、`--repo` で fork から入れる人が
// 英語で `## Breaking changes` と書いたとき、何も言われずに警告を失う。だから英語の見出しも受ける。
//
// 目的: 見出し「Breaking Changes」（属性付き・大文字小文字が違う）の下の箇条書きを読むこと。
// 与える情報: v0.2.0 を名乗る実行ファイルと、`<h3 dir="auto">Breaking Changes</h3>` を持つ v0.3.0。
// 成功条件: 終了コードが 0 で、その箇条書きが「破壊的変更があります」の枠に出ること。
func Test_continuoを入れる_P001_英語の見出しBreaking_changesも読む(t *testing.T) {
	fr := newFakeRelease(t, "v0.3.0", "#!/bin/sh\necho v0.3.0\n", true)
	fr.SetFeed(releasesFeed(t, []releaseNote{
		{Tag: "v0.3.0", HTML: "<h3 dir=\"auto\">Breaking Changes</h3>\n<ul dir=\"auto\">\n<li><p>The <code>runtime</code> section was removed</p></li>\n</ul>"},
		{Tag: "v0.2.0"},
	}))
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.2.0")

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	if !strings.Contains(out, "破壊的変更があります") || !strings.Contains(out, "  v0.3.0  The runtime section was removed") {
		t.Errorf("英語の見出しの下の箇条書きを出していません:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_端末を操作する文字を通さない は、制御文字を消すことを確かめる。
//
// **リリースノートの文は、そのまま利用者の端末に出る。**ESC（0x1b）や C1 制御文字（U+009B）を通すと、
// 本文に置いた並びで利用者の端末を操作できてしまう。タブと改行は空白にまとめる。
//
// 目的: 箇条書きの中の制御文字を消し、タブと改行を空白にすること。
// 与える情報: ESC・U+009B・タブ・改行を含む箇条書きを持つ v0.3.0。
// 成功条件: 出力に ESC も U+009B も無く、語が空白1つでつながって出ること。
func Test_continuoを入れる_P001_端末を操作する文字を通さない(t *testing.T) {
	fr := newFakeRelease(t, "v0.3.0", "#!/bin/sh\necho v0.3.0\n", true)
	fr.SetFeed(releasesFeed(t, []releaseNote{
		{Tag: "v0.3.0", HTML: "<h2>破壊的変更</h2>\n<ul>\n<li>前\x1b[31m赤\u009b2J\tタブ\n改行</li>\n</ul>"},
		{Tag: "v0.2.0"},
	}))
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.2.0")

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\u009b") {
		t.Errorf("制御文字を通しています: %q", out)
	}
	if !strings.Contains(out, "  v0.3.0  前[31m赤2J タブ 改行") {
		t.Errorf("制御文字を消した1行になっていません: %q", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_古すぎる版から上げると見えない範囲を知らせる は、更新フィードの窓の外を確かめる。
//
// **更新フィードには新しい10件しか載らない。**それより古い版から上げると、あいだの版の本文を読めない。
// 人間の決定（issue のコメント）: 載っている範囲の破壊的変更はそのまま出し、そのうえで
// 「古すぎるため、ほかにも破壊的変更がある可能性があります」と、載っていない範囲と releases の画面を出す。
//
// 目的: 窓の外の古い版から上げたとき、載っている範囲の警告と「古すぎるため」の知らせが両方出ること。
// 与える情報: v0.1.0 を名乗る実行ファイルと、v0.5.0（破壊的変更あり）と v0.4.0 だけの更新フィード。
// 成功条件: 終了コードが 0 で、v0.5.0 の破壊的変更と、「v0.1.0 より後、v0.4.0 より前の版は」と
// 「古すぎるため」と releases の画面の URL が出ること。
func Test_continuoを入れる_P001_古すぎる版から上げると見えない範囲を知らせる(t *testing.T) {
	fr := newFakeRelease(t, "v0.5.0", "#!/bin/sh\necho v0.5.0\n", true)
	fr.SetFeed(releasesFeed(t, []releaseNote{
		{Tag: "v0.5.0", Breaking: []string{"窓の中の破壊的変更です"}},
		{Tag: "v0.4.0"},
	}))
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.1.0")

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	for _, want := range []string{
		" 破壊的変更があります: v0.1.0 → v0.5.0",
		"  v0.5.0  窓の中の破壊的変更です",
		" 破壊的変更があるかを確かめられなかった版があります: v0.1.0 → v0.5.0",
		"   v0.1.0 より後、v0.4.0 より前の版は、更新フィードに載っていないため確かめられません。",
		"   古すぎるため、ほかにも破壊的変更がある可能性があります。",
		" 次に起動する前に、https://github.com/maimuzo/continuo/releases で",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が出ていません:\n%s", want, out)
		}
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_版を指定して上げても警告する は、`--version` で上げるときを確かめる。
//
// **`--version` で上げるときも、同じ更新フィードを1回取って破壊的変更を読む。**
// 取れない案内を受けて `--version` で叩き直した人こそ、警告を失ってはならない。
//
// 目的: `--version` で上げたとき、範囲の破壊的変更が出ること。
// 与える情報: v0.2.0 を名乗る実行ファイルと、v0.3.0 に破壊的変更がある更新フィード、`--version v0.3.0`。
// 成功条件: 終了コードが 0 で、v0.3.0 の破壊的変更が出ること。
func Test_continuoを入れる_P001_版を指定して上げても警告する(t *testing.T) {
	fr := newFakeRelease(t, "v0.3.0", "#!/bin/sh\necho v0.3.0\n", true)
	fr.SetFeed(releasesFeed(t, []releaseNote{
		{Tag: "v0.3.0", Breaking: []string{"版を指定しても出ます"}, Age: 10 * time.Hour},
		{Tag: "v0.2.0"},
	}))
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.2.0")

	code, out := runInstaller(t, fr, dir, "--no-deps", "--version", "v0.3.0")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	if !strings.Contains(out, "  v0.3.0  版を指定しても出ます") {
		t.Errorf("破壊的変更を出していません:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_版を指定して下げるときは何も読まない は、下げるときを確かめる。
//
// **下げるときは、範囲が空になる。**更新フィードを読まず、知らせも出さない
// （docs/FAQ.md の戻す手順は `--version` で古い版を入れる。そこで偽の知らせを出さない）。
//
// 目的: `--version` で下げたとき、更新フィードを1度も取らず、何も言わないこと。
// 与える情報: v0.3.0 を名乗る実行ファイルと `--version v0.2.0`。
// 成功条件: 終了コードが 0 で、v0.2.0 に入れ替わり、更新フィードへの要求が0回で、知らせが出ないこと。
func Test_continuoを入れる_P001_版を指定して下げるときは何も読まない(t *testing.T) {
	fr := newFakeRelease(t, "v0.2.0", "#!/bin/sh\necho v0.2.0\n", true)
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.3.0")

	code, out := runInstaller(t, fr, dir, "--no-deps", "--version", "v0.2.0")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	if n := fr.feedHits.Load(); n != 0 {
		t.Errorf("下げるのに更新フィードを %d 回取っています:\n%s", n, out)
	}
	if strings.Contains(out, "破壊的変更があります") || strings.Contains(out, "確かめられなかった版があります") {
		t.Errorf("下げるのに破壊的変更について言っています:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_入れる版が更新フィードに無ければ知らせる は、押し出された版を名指ししたときを確かめる。
//
// 目的: `--version` で名指しした版が更新フィードに無いとき、その版の本文を読めていないことを知らせること。
// 与える情報: v0.2.0 を名乗る実行ファイルと、v0.3.0 と v0.2.0 だけの更新フィード、`--version v0.2.5`。
// 成功条件: 終了コードが 0 で、「v0.2.5 は更新フィードに載っていない」が知らせの枠に出ること。
func Test_continuoを入れる_P001_入れる版が更新フィードに無ければ知らせる(t *testing.T) {
	fr := newFakeRelease(t, "v0.2.5", "#!/bin/sh\necho v0.2.5\n", true)
	fr.SetFeed(releasesFeed(t, []releaseNote{
		{Tag: "v0.3.0"},
		{Tag: "v0.2.0"},
	}))
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.2.0")

	code, out := runInstaller(t, fr, dir, "--no-deps", "--version", "v0.2.5")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	for _, want := range []string{
		" 破壊的変更があるかを確かめられなかった版があります: v0.2.0 → v0.2.5",
		"   v0.2.5 は更新フィードに載っていないため、そのリリースノートを読めませんでした。",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が出ていません:\n%s", want, out)
		}
	}
	if strings.Contains(out, "破壊的変更があります:") {
		t.Errorf("確かめていないのに「破壊的変更があります」と言っています:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_形の崩れた本文は確かめられないと知らせる は、読み方の「確かめられない」を確かめる。
//
// **GitHub が更新フィードの HTML の形を変えると、警告が黙って消えうる。**黙らずに、
// その版を確かめられなかったと知らせる（設計 3-36）。「破壊的変更があります」とは言わない。
//
// 目的: 次の5つで、確かめられなかった知らせが出ること。
//   - 見出しの直後が `<ul` でない
//   - 見出しの下の `<ul>` に `<li` が0件
//   - 箇条書きが入れ子になっている
//   - `</ul>` が無い
//   - 更新フィードの全件に見出しの要素が無い
//
// 与える情報: v0.2.0 を名乗る実行ファイルと、v0.3.0 の本文を崩した更新フィード。
// 成功条件: 終了コードが 0 で、「確かめられなかった版があります」が出て、「破壊的変更があります」が出ないこと。
func Test_continuoを入れる_P001_形の崩れた本文は確かめられないと知らせる(t *testing.T) {
	cases := []struct {
		name string
		html string
		want string
	}{
		{"見出しの直後が箇条書きでない", "<h2>破壊的変更</h2>\n<p>段落です</p>\n<ul>\n<li>出てはいけません</li>\n</ul>", "読み取れませんでした: v0.3.0"},
		{"箇条書きが0件", "<h2>破壊的変更</h2>\n<ul>\n</ul>", "読み取れませんでした: v0.3.0"},
		{"入れ子の箇条書き", "<h2>破壊的変更</h2>\n<ul>\n<li>親<ul><li>子</li></ul></li>\n</ul>", "読み取れませんでした: v0.3.0"},
		{"閉じの無い箇条書き", "<h2>破壊的変更</h2>\n<ul>\n<li>出てはいけません</li>\n", "読み取れませんでした: v0.3.0"},
		{"全件に見出しが無い", "<p>出てはいけません</p>", "どのリリースノートにも見出しが見つからず"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fr := newFakeRelease(t, "v0.3.0", "#!/bin/sh\necho v0.3.0\n", true)
			older := releaseNote{Tag: "v0.2.0"}
			if tc.name == "全件に見出しが無い" {
				older.HTML = "<p>見出しの無いノート</p>"
			}
			fr.SetFeed(releasesFeed(t, []releaseNote{{Tag: "v0.3.0", HTML: tc.html}, older}))
			dir := t.TempDir()
			placeInstalled(t, dir, "v0.2.0")

			code, out := runInstaller(t, fr, dir, "--no-deps")
			if code != 0 {
				t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
			}
			if !strings.Contains(out, " 破壊的変更があるかを確かめられなかった版があります: v0.2.0 → v0.3.0") ||
				!strings.Contains(out, tc.want) {
				t.Errorf("確かめられなかった知らせ（%q）が出ていません:\n%s", tc.want, out)
			}
			if strings.Contains(out, "破壊的変更があります:") || strings.Contains(out, "出てはいけません") {
				t.Errorf("確かめていないのに破壊的変更として出しています:\n%s", out)
			}
		})
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_破壊的変更が無ければ何も言わない は、余計なことを言わないことを確かめる。
//
// **毎回の更新で警告が出ると、本当に出たときに読まれなくなる。**
//
// **範囲が全部更新フィードに載る形に作る。**いま入っている版が更新フィードのいちばん小さい正式版より
// 小さいと、「古すぎるため」の知らせが出るのが正しい動きになるためである。
//
// 目的: 上げる範囲に見出し「破壊的変更」が1つも無ければ、破壊的変更について何も言わないこと。
// 与える情報: いま入っている v0.2.0 から入れる v0.10.0 までが全部載り、見出し「破壊的変更」を1つも持たない更新フィード。
// 成功条件: 終了コードが 0 で、「破壊的変更があります」の枠も「確かめられなかった」の枠も出ないこと。
func Test_continuoを入れる_P001_破壊的変更が無ければ何も言わない(t *testing.T) {
	fr := newFakeRelease(t, "v0.10.0", "#!/bin/sh\necho v0.10.0\n", true)
	fr.SetFeed(releasesFeed(t, []releaseNote{
		{Tag: "v0.10.0"},
		{Tag: "v0.3.0"},
		{Tag: "v0.2.0"},
	}))
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.2.0")

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	// **警告の見出しで見る。**一時ディレクトリのパスにテストの名前が入るので、
	// 「破壊的変更」だけで探すと、警告が出ていなくても当たってしまう。
	if strings.Contains(out, "破壊的変更があります") {
		t.Errorf("破壊的変更が無いのに何か言っています:\n%s", out)
	}
	if strings.Contains(out, "確かめられなかった版があります") {
		t.Errorf("全部読めているのに、確かめられなかったと言っています:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_新規の導入では破壊的変更を言わない は、警告する相手がいない場合を確かめる。
//
// **置き先に何も無ければ、上げるのではなく初めて入れるのである。**
// 直す設定はまだ無いので、言うことは何も無い。
//
// 目的: 置き先に実行ファイルが無いとき、破壊的変更があっても何も言わないこと。
// 与える情報: 見出し「破壊的変更」を持つ更新フィードと、空の置き先。
// 成功条件: 終了コードが 0 で、出力に「破壊的変更」が出ないこと。
func Test_continuoを入れる_P001_新規の導入では破壊的変更を言わない(t *testing.T) {
	fr := newFakeRelease(t, "v0.10.0", "#!/bin/sh\necho v0.10.0\n", true)
	fr.SetFeed(releasesFeed(t, []releaseNote{
		{Tag: "v0.10.0", Breaking: []string{"新規の導入では出てはいけません"}},
	}))
	dir := t.TempDir()

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	if strings.Contains(out, "破壊的変更があります") ||
		strings.Contains(out, "確かめられなかった版があります") ||
		strings.Contains(out, "新規の導入では出てはいけません") {
		t.Errorf("新規の導入なのに破壊的変更を言っています:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_版を名乗らないものからは警告しない は、ソースから作ったものの扱いを確かめる。
//
// **`go build` しただけの実行ファイルは `dev` と名乗る**（internal/cli の version）。
// **どの release より新しいのか古いのかを決められないので、範囲を作れない。**
//
// 目的: いま入っているものが `dev` と名乗るとき、誤った警告を出さないこと。
// 与える情報: `dev` と名乗る実行ファイルと、見出し「破壊的変更」を持つ更新フィード。
// 成功条件: 終了コードが 0 で、出力に「破壊的変更」が出ないこと。
func Test_continuoを入れる_P001_版を名乗らないものからは警告しない(t *testing.T) {
	fr := newFakeRelease(t, "v0.10.0", "#!/bin/sh\necho v0.10.0\n", true)
	fr.SetFeed(releasesFeed(t, []releaseNote{
		{Tag: "v0.10.0", Breaking: []string{"版を比べられないので出てはいけません"}},
	}))
	dir := t.TempDir()
	placeInstalled(t, dir, "dev")

	code, out := runInstaller(t, fr, dir, "--no-deps")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}
	if strings.Contains(out, "破壊的変更があります") ||
		strings.Contains(out, "確かめられなかった版があります") ||
		strings.Contains(out, "版を比べられないので出てはいけません") {
		t.Errorf("版を比べられないのに破壊的変更を言っています:\n%s", out)
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_更新フィードを取れなくても入れるのは止めない は、警告を作れない経路を確かめる。
//
// **破壊的変更を確かめられないことで、導入を止めてはならない。黙ってもならない**
// （設計 3-36「破壊的変更を集めるときの決めごと」）。入れたうえで、確かめられなかったことを知らせる。
//
// **これが起きるのは `--version` の実行だけである。**版を指定しない実行では、
// 更新フィードが入れる版の決め手なので、取れなければ止まる（クールダウン。設計 3-36）。
//
// 目的: `--version` の実行で更新フィードが 404 でも、終了コードが 0 で実行ファイルが入れ替わり、
// 確かめられなかったことを知らせること。
// 与える情報: v0.2.0 を名乗る実行ファイルと、更新フィードに 404 を返す偽サーバ（FailFeed）。
// 成功条件: 終了コードが 0 で、置いたものが v0.10.0 を名乗り、「破壊的変更があります」は出ず、
// 「確かめられなかった版があります」と更新フィードを取れなかった理由が出ること。
func Test_continuoを入れる_P001_更新フィードを取れなくても入れるのは止めない(t *testing.T) {
	fr := newFakeRelease(t, "v0.10.0", "#!/bin/sh\necho v0.10.0\n", true)
	fr.FailFeed()
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.2.0")

	code, out := runInstaller(t, fr, dir, "--no-deps", "--version", "v0.10.0")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, out)
	}

	got, err := exec.Command(filepath.Join(dir, "continuo")).Output()
	if err != nil {
		t.Fatalf("置いた実行ファイルを動かせません: %v\n%s", err, out)
	}
	if !strings.Contains(string(got), "v0.10.0") {
		t.Errorf("入れ替わっていません: %q\n%s", string(got), out)
	}
	if strings.Contains(out, "破壊的変更があります") {
		t.Errorf("更新フィードを取れていないのに「破壊的変更があります」と言っています:\n%s", out)
	}
	for _, want := range []string{
		" 破壊的変更があるかを確かめられなかった版があります: v0.2.0 → v0.10.0",
		"更新フィードを取れませんでした（応答: 404）",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が出ていません:\n%s", want, out)
		}
	}
}

// {"RUCM-PATH": "P001"}
//
// Test_continuoを入れる_P001_破壊的変更を読み出せなくても入れるのは止めない は、awk が落ちる経路を確かめる。
//
// **install.sh は `set -eu` で走る。**読み出しの代入を `if !` で受けていないと、
// **awk が 0 以外で終わった時点でスクリプトごと終わる。**
// collect_breaking は install_binary より先に走るので、**実行ファイルを置く前に、
// 何のメッセージも出さずに落ちる。**
//
// 目的: 破壊的変更を読み出す awk が落ちても、終了コードが 0 で実行ファイルが入れ替わり、
// 確かめられなかったことを知らせること。
// 与える情報: v0.2.0 を名乗る実行ファイル、見出し「破壊的変更」のある更新フィード、
// 破壊的変更を読む awk だけが落ちる PATH。
// 成功条件: 終了コードが 0 で、置いたものが v0.10.0 を名乗り、「破壊的変更があります」は出ず、
// 「確かめられなかった版があります」と読み出しが失敗した理由が出ること。
func Test_continuoを入れる_P001_破壊的変更を読み出せなくても入れるのは止めない(t *testing.T) {
	fr := newFakeRelease(t, "v0.10.0", "#!/bin/sh\necho v0.10.0\n", true)
	fr.SetFeed(releasesFeed(t, []releaseNote{
		{Tag: "v0.10.0", Breaking: []string{"読み出せないので出てはいけません"}},
		{Tag: "v0.2.0"},
	}))
	dir := t.TempDir()
	placeInstalled(t, dir, "v0.2.0")

	code, out := runInstallerEnv(t, fr, dir,
		[]string{"PATH=" + stubBrokenBreakingAwk(t) + string(os.PathListSeparator) + os.Getenv("PATH")},
		"--no-deps")
	if code != 0 {
		t.Fatalf("破壊的変更を読めないだけで導入が止まっています: %d\n%s", code, out)
	}

	got, gerr := exec.Command(filepath.Join(dir, "continuo")).Output()
	if gerr != nil {
		t.Fatalf("置いた実行ファイルを動かせません: %v\n%s", gerr, out)
	}
	if !strings.Contains(string(got), "v0.10.0") {
		t.Errorf("入れ替わっていません: %q\n%s", string(got), out)
	}
	if strings.Contains(out, "破壊的変更があります") ||
		strings.Contains(out, "読み出せないので出てはいけません") {
		t.Errorf("読み出せていないのに警告を出しています:\n%s", out)
	}
	for _, want := range []string{
		" 破壊的変更があるかを確かめられなかった版があります: v0.2.0 → v0.10.0",
		"破壊的変更を読み出す処理（awk）が失敗しました。",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が出ていません:\n%s", want, out)
		}
	}
}

// {"RUCM-PATH": "P009"}
//
// Test_continuoを入れる_P009_クールダウン_値の誤りは取得の前に弾く は、`--cooldown-hours` の値の検査を確かめる。
//
// 目的: 整数でない値・空の値・7桁以上の値を、配布サーバへ繋ぐ前に弾くこと。
// 与える情報: 誤った値を1つずつ。
// 成功条件: 終了コードが 0 でなく、値の誤りを名指しし、何も置かないこと。
func Test_continuoを入れる_P009_クールダウン_値の誤りは取得の前に弾く(t *testing.T) {
	for _, bad := range []string{"1.5", "", "-1", "24h", "1000000", "00001000000"} {
		t.Run(bad, func(t *testing.T) {
			dir := t.TempDir()
			code, out := runInstaller(t, nil, dir, "--no-deps", "--cooldown-hours", bad)
			if code == 0 {
				t.Fatalf("誤った値 %q を通しています:\n%s", bad, out)
			}
			if !strings.Contains(out, "--cooldown-hours には 0 以上の整数（時間）を続けてください") {
				t.Errorf("値の誤りを名指ししていません:\n%s", out)
			}
			if _, err := os.Stat(filepath.Join(dir, "continuo")); err == nil {
				t.Error("誤った値なのに実行ファイルを置いています")
			}
		})
	}
}
