package agentplugin_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/agentplugin"
)

// このファイルは、plugin `continuo-agent-procedures` の書き出しと古い版の片付けを確かめる（issue #288）。
//
// **書き出しは Claude Code を起動する前に毎回走る。**エージェントが書き換えたファイル・足したファイル・
// symlink への差し替え・権限の書き換えを、次の起動の前に元へ戻すことが、共有の plugin を守る唯一の手である。

// install は、一時ディレクトリを実行時ディレクトリにして書き出す。
//
// t: テストコンテキスト。
// 戻り値の1つ目: 実行時ディレクトリ。
// 戻り値の2つ目: 書き出した plugin のディレクトリ。
func install(t *testing.T) (string, string) {
	t.Helper()
	runtimeDir := t.TempDir()
	dir, err := agentplugin.Install(runtimeDir, time.Now())
	if err != nil {
		t.Fatalf("書き出せません: %v", err)
	}
	return runtimeDir, dir
}

// assertPristine は、plugin のディレクトリが期待値どおりであることを確かめる。
//
// **見るのは、ファイルの一覧・中身・権限・種類の全部である。**
//
// t: テストコンテキスト。
// dir: plugin のディレクトリ。
func assertPristine(t *testing.T, dir string) {
	t.Helper()
	want := map[string]bool{}
	for _, rel := range agentplugin.RawFiles() {
		want[rel] = true
		p := filepath.Join(dir, filepath.FromSlash(rel))
		info, err := os.Lstat(p)
		if err != nil {
			t.Errorf("%s がありません: %v", rel, err)
			continue
		}
		if !info.Mode().IsRegular() {
			t.Errorf("%s が普通のファイルではありません（%s）", rel, info.Mode())
			continue
		}
		perm := fs.FileMode(0o444)
		if strings.Contains(rel, "/scripts/") {
			perm = 0o555
		}
		if info.Mode().Perm() != perm {
			t.Errorf("%s の権限が %o です（%o のはず）", rel, info.Mode().Perm(), perm)
		}
		raw, _ := agentplugin.RawFile(rel)
		got, _ := os.ReadFile(p)
		if string(got) != string(agentplugin.Expand(raw, dir)) {
			t.Errorf("%s の中身が期待値と違います", rel)
		}
	}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			info, _ := os.Lstat(p)
			if info.Mode().Perm() != 0o755 {
				t.Errorf("ディレクトリ %s の権限が %o です（755 のはず）", rel, info.Mode().Perm())
			}
			return nil
		}
		if !want[rel] {
			t.Errorf("期待値に無いファイル %s が残っています", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("plugin のディレクトリを辿れません: %v", err)
	}
}

// 目的: 書き出した plugin が、中身・権限・置き場所とも期待どおりであることを固定する。
//
// 与える情報: 空の実行時ディレクトリ。
// 成功条件: `<実行時ディレクトリ>/plugins/<12桁>/continuo-agent-procedures` に書き出され、
// SKILL.md と plugin.json が 0444、scripts が 0555、ディレクトリが 0755 で、置き換える語が1つも残っていないこと。
func TestInstall_書き出したpluginは期待値どおりである(t *testing.T) {
	runtimeDir, dir := install(t)
	if want := filepath.Join(runtimeDir, "plugins", agentplugin.Hash(), "continuo-agent-procedures"); dir != want {
		t.Errorf("書き出し先が %s です（%s のはず）", dir, want)
	}
	if len(agentplugin.Hash()) != 12 {
		t.Errorf("ハッシュが12桁ではありません: %q", agentplugin.Hash())
	}
	assertPristine(t, dir)
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, _ := os.ReadFile(p)
		for _, word := range []string{agentplugin.HashPlaceholder, agentplugin.DirPlaceholder} {
			if strings.Contains(string(data), word) {
				t.Errorf("%s に置き換える語 %s が残っています", p, word)
			}
		}
		return nil
	})
	// **終わりの印には、書き出し先の名前と同じ12桁が入る。**
	data, err := os.ReadFile(filepath.Join(dir, "skills", "starting-run", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<!-- continuo-skill-end: starting-run "+agentplugin.Hash()+" -->") {
		t.Error("starting-run の終わりの印に12桁が入っていません")
	}
}

// 目的: エージェントが中身を書き換えたりファイルを足したりしても、次の書き出しで元へ戻ることを固定する。
//
// **共有の plugin である。**直さないと、エージェントが書き換えた scripts を、ほかの全 issue の run が実行する。
// **ディレクトリごとの差し替えはしない。**走っている run から見てディレクトリが消える瞬間を作らない。
//
// 与える情報: 書き出したあと、SKILL.md を書き換え、hooks/hooks.json を足し、scripts の権限を書き換え、
// SKILL.md を symlink に差し替えたもの。
// 成功条件: もう一度書き出すと期待値どおりに戻り、plugin のディレクトリの inode は変わらないこと。
func TestInstall_書き換えと足したファイルとsymlinkと権限は次の書き出しで元へ戻る(t *testing.T) {
	runtimeDir, dir := install(t)
	before, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}

	skill := filepath.Join(dir, "skills", "writing-plan", "SKILL.md")
	if err := os.Chmod(skill, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, []byte("書き換えた\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks", "hooks.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "skills", "starting-run", "scripts", "check-assignee.sh")
	if err := os.Chmod(script, 0o777); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "skills", "trusting-instructions", "SKILL.md")
	if err := os.Remove(other); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "evil.md")
	if err := os.WriteFile(target, []byte("外のファイル\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, other); err != nil {
		t.Fatal(err)
	}

	again, err := agentplugin.Install(runtimeDir, time.Now())
	if err != nil {
		t.Fatalf("書き出し直せません: %v", err)
	}
	if again != dir {
		t.Fatalf("書き出し先が変わりました: %s → %s", dir, again)
	}
	assertPristine(t, dir)
	after, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Error("plugin のディレクトリが作り直されています（走っている run から見て消える瞬間ができます）")
	}
	if got, _ := os.ReadFile(target); string(got) != "外のファイル\n" {
		t.Errorf("symlink の先のファイルを書き換えています: %q", got)
	}
}

// 目的: 別の continuo が書きかけの一時ファイル（1時間より新しいもの）は消さず、古いものは消すことを固定する。
//
// 与える情報: plugin の中に、`os.CreateTemp` の名前の形のファイルを2つ（いまの時刻と2時間前）置いたもの。
// 成功条件: 新しい方は残り、古い方は消えること。
func TestInstall_書きかけの一時ファイルは1時間までは消さない(t *testing.T) {
	runtimeDir, dir := install(t)
	fresh := filepath.Join(dir, "skills", "writing-plan", ".SKILL.md.123456")
	stale := filepath.Join(dir, "skills", "writing-plan", ".SKILL.md.654321")
	for _, p := range []string{fresh, stale} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := agentplugin.Install(runtimeDir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(fresh); err != nil {
		t.Errorf("1時間より新しい一時ファイルを消しました: %v", err)
	}
	if _, err := os.Lstat(stale); err == nil {
		t.Error("1時間より古い一時ファイルが残っています")
	}
}

// 目的: 古い版は、`.orphaned_at` から14日を過ぎ、どの system-prompt.md にも載っていないときだけ消えることを
// 固定する。現役の版と `.tmp-*` には `.orphaned_at` が残らないことも確かめる。
//
// **立て直したあとも、pane の Claude Code は古い版のディレクトリを読み続ける。**消すと、手順もスクリプトも失われる。
//
// 与える情報: いまの版と、古い版を3つ（system-prompt.md に載っているもの・載っていないもの・14日を過ぎていないもの）、
// 1時間より古い `.tmp-*` と新しい `.tmp-*`。
// 成功条件: 1回目の片付けで古い版3つに `.orphaned_at` が置かれ、いまの版と `.tmp-*` には置かれない。
// 15日後の片付けで、載っていない古い版だけが消え、古い `.tmp-*` が消え、新しい `.tmp-*` は残る。
func TestCleanup_古い版は14日を過ぎてどのシステムプロンプトにも無いときだけ消す(t *testing.T) {
	runtimeDir, _ := install(t)
	root := filepath.Join(runtimeDir, "plugins")
	now := time.Now()

	const referenced, unreferenced, young = "aaaaaaaaaaaa", "bbbbbbbbbbbb", "cccccccccccc"
	for _, h := range []string{referenced, unreferenced, young} {
		if err := os.MkdirAll(filepath.Join(root, h, "continuo-agent-procedures"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// **いまの版に、別の continuo が置いた `.orphaned_at` が残っている。**
	if err := os.WriteFile(filepath.Join(root, agentplugin.Hash(), agentplugin.OrphanedAtName), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleTmp := filepath.Join(root, ".tmp-111")
	freshTmp := filepath.Join(root, ".tmp-222")
	for _, p := range []string{staleTmp, freshTmp} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	twoHoursAgo := now.Add(-2 * time.Hour)
	if err := os.Chtimes(staleTmp, twoHoursAgo, twoHoursAgo); err != nil {
		t.Fatal(err)
	}
	issueDir := filepath.Join(runtimeDir, "issues", "octocat-hello-world-42")
	if err := os.MkdirAll(issueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(issueDir, agentplugin.SystemPromptFileName),
		// **使っているとみなすのは、版のディレクトリ（`plugins/<12桁>/`）が載っているときだけである。**
		// 12桁だけが載っている版（unreferenced）は、使っているとみなさない。
		[]byte("| plugin のディレクトリ | "+filepath.Join(root, referenced, "continuo-agent-procedures")+" |\n"+
			"別の16進の並び: "+unreferenced+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := agentplugin.Cleanup(runtimeDir, now); err != nil {
		t.Fatalf("片付けに失敗しました: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, agentplugin.Hash(), agentplugin.OrphanedAtName)); err == nil {
		t.Error("いまの版に .orphaned_at が残っています")
	}
	for _, h := range []string{referenced, unreferenced, young} {
		if _, err := os.Lstat(filepath.Join(root, h, agentplugin.OrphanedAtName)); err != nil {
			t.Errorf("古い版 %s に .orphaned_at が置かれていません: %v", h, err)
		}
	}
	for _, p := range []string{staleTmp, freshTmp} {
		if _, err := os.Lstat(filepath.Join(p, agentplugin.OrphanedAtName)); err == nil {
			t.Errorf("%s に .orphaned_at を置いています", p)
		}
	}
	if _, err := os.Lstat(staleTmp); err == nil {
		t.Error("1時間より古い .tmp-* が残っています")
	}
	if _, err := os.Lstat(freshTmp); err != nil {
		t.Error("1時間より新しい .tmp-* を消しました")
	}

	// **14日を過ぎていない版を作る。**その版の `.orphaned_at` だけを、いまの時刻に置き直す。
	later := now.Add(15 * 24 * time.Hour)
	if err := os.WriteFile(filepath.Join(root, young, agentplugin.OrphanedAtName),
		[]byte(later.Add(-time.Hour).UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agentplugin.Cleanup(runtimeDir, later); err != nil {
		t.Fatalf("片付けに失敗しました: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, unreferenced)); err == nil {
		t.Error("14日を過ぎ、どのシステムプロンプトにも載っていない版が残っています")
	}
	if _, err := os.Lstat(filepath.Join(root, referenced)); err != nil {
		t.Error("システムプロンプトに載っている版を消しました（その pane の Claude Code が手順を失います）")
	}
	if _, err := os.Lstat(filepath.Join(root, young)); err != nil {
		t.Error("14日を過ぎていない版を消しました")
	}
	if _, err := os.Lstat(filepath.Join(root, agentplugin.Hash(), "continuo-agent-procedures")); err != nil {
		t.Error("いまの版を消しました")
	}
}

// 目的: plugin が hook を1つも持たないことを固定する。
//
// **plugin の hook は、continuo の hook とは別に Claude Code が実行する。**持たせると、
// CLAUDE.md の「hook の挙動を変える変更」に当たり、本体と hook の版が混ざる。
//
// 与える情報: 埋め込んだ plugin のファイルの一覧と、plugin.json と各 SKILL.md の front matter。
// 成功条件: `hooks/` の下にファイルが無く、plugin.json に `hooks` のキーが無く、どの SKILL.md の
// front matter にも `hooks:` が無いこと。
func TestPlugin_hookを持たない(t *testing.T) {
	for _, rel := range agentplugin.RawFiles() {
		if strings.HasPrefix(rel, "hooks/") || strings.Contains(rel, "/hooks/") {
			t.Errorf("plugin に hook のファイル %s があります", rel)
		}
	}
	raw, ok := agentplugin.RawFile(".claude-plugin/plugin.json")
	if !ok {
		t.Fatal(".claude-plugin/plugin.json がありません")
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("plugin.json を読めません: %v", err)
	}
	if _, ok := manifest["hooks"]; ok {
		t.Error("plugin.json に hooks があります")
	}
	if manifest["name"] != "continuo-agent-procedures" {
		t.Errorf("plugin.json の name が %v です", manifest["name"])
	}
	for _, name := range agentplugin.SkillNames() {
		data, _ := agentplugin.RawFile("skills/" + name + "/SKILL.md")
		front, _, _ := strings.Cut(strings.TrimPrefix(string(data), "---\n"), "\n---\n")
		for _, line := range strings.Split(front, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "hooks:") {
				t.Errorf("%s の front matter に hooks があります", name)
			}
		}
	}
}

// 目的: 書き出し先を絶対パスで渡さないと断ることを固定する。
//
// 与える情報: 相対パス。
// 成功条件: 誤りが返り、何も作らないこと。
func TestInstall_相対パスには書き出さない(t *testing.T) {
	if _, err := agentplugin.Install("relative/dir", time.Now()); err == nil {
		t.Error("相対パスへ書き出しました")
	}
}
