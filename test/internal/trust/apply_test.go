package trust_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/trust"
	"github.com/maimuzo/continuo/internal/workspace"
)

// otherSettings は、書き換えの対象ではない他のリポジトリの記述である。
//
// **1つも変わってはならない**（設計 3-33）。continuo が知らないキーも含めてある。
const otherSettings = `{
  "numStartups": 41,
  "oauthAccount": {"accountUuid": "0000-1111", "emailAddress": "someone@example.invalid"},
  "projects": {
    "/somewhere/else": {
      "allowedTools": ["Bash(ls:*)"],
      "hasTrustDialogAccepted": true,
      "history": [{"display": "hello", "pastedContents": {}}],
      "mcpServers": {}
    }
  },
  "tipsHistory": {"new-user-warmup": 3}
}
`

// 目的: 未信頼のリポジトリを登録し、そのときバックアップを残すことを確認する。
//
// **バックアップは消さない**（設計 3-33）。書き換えを元へ戻す唯一の手段である。
//
// 与える情報: 他のリポジトリの記述を持つ `.claude.json` と、未信頼のリポジトリ1つ。
// 成功条件: hasTrustDialogAccepted が true になり、
// バックアップに書き換える前の中身がそのまま残っていること。
func TestApply_未信頼のリポジトリを登録しバックアップを残す(t *testing.T) {
	repo := initRepo(t, "continuo")
	home, configPath := fakeHome(t, otherSettings)
	report := planFor(t, home, map[string]string{"maimuzo/continuo": repo}, "maimuzo/continuo")

	result, err := trust.Apply(context.Background(), optionsFor(home, map[string]string{"maimuzo/continuo": repo}), report)
	if err != nil {
		t.Fatalf("登録できなかった: %v", err)
	}

	key := trustKeyOf(t, repo)
	if len(result.Changed) != 1 || result.Changed[0].TrustKey != key {
		t.Fatalf("登録した項目が想定と違う: %+v", result.Changed)
	}
	entry, ok := projectEntry(t, readFile(t, configPath), key)
	if !ok {
		t.Fatalf("%s の記述が作られていない:\n%s", key, readFile(t, configPath))
	}
	if entry["hasTrustDialogAccepted"] != true {
		t.Errorf("信頼が登録されていない: %+v", entry)
	}

	if result.BackupPath == "" {
		t.Fatal("バックアップのパスが返っていない")
	}
	if !strings.HasPrefix(filepath.Base(result.BackupPath), trust.BackupPrefix) {
		t.Errorf("バックアップの名前が想定と違う: %s", result.BackupPath)
	}
	if got := readFile(t, result.BackupPath); got != otherSettings {
		t.Errorf("バックアップが書き換える前の中身と違う\n期待:\n%s\n実際:\n%s", otherSettings, got)
	}
}

// 目的: 書き込んだあと、巡回のループから信頼済みに見えることを確認する。
//
// **鍵の作り方がずれていると「書いたのに効かない」が静かに起きる。**
// dispatch の直前の検査（internal/workspace）と同じ関数で確かめる。
//
// 与える情報: 未信頼のリポジトリ1つ。
// 成功条件: Apply の確認で問題が出ず、workspace.CheckTrustForClonePath が真を返すこと。
func TestApply_書き込んだものが巡回のループから信頼済みに見える(t *testing.T) {
	repo := initRepo(t, "continuo")
	home, _ := fakeHome(t, `{"projects":{}}`)
	report := planFor(t, home, map[string]string{"maimuzo/continuo": repo}, "maimuzo/continuo")

	result, err := trust.Apply(context.Background(), optionsFor(home, map[string]string{"maimuzo/continuo": repo}), report)
	if err != nil {
		t.Fatalf("登録できなかった: %v", err)
	}
	if len(result.VerifyProblems) != 0 {
		t.Fatalf("書き込んだあとの確認で問題が出ている: %v", result.VerifyProblems)
	}
	if len(result.Verified) != 1 {
		t.Fatalf("確認できた項目が返っていない: %+v", result.Verified)
	}

	trusted, reason, err := workspace.CheckTrustForClonePath(repo, home)
	if err != nil {
		t.Fatalf("巡回のループと同じ判定を実行できなかった: %v", err)
	}
	if !trusted {
		t.Errorf("登録したのに巡回のループからは未信頼に見える: %s", reason)
	}
}

// 目的: 既に true のものは触らず、バックアップも書き込みも行わないことを確認する。
//
// 与える情報: 対象のリポジトリが既に信頼済みである `.claude.json`。
// 成功条件: ファイルが1バイトも変わらず、バックアップが作られないこと。
func TestApply_既にtrueのものは触らない(t *testing.T) {
	repo := initRepo(t, "continuo")
	key := trustKeyOf(t, repo)
	before := `{
  "projects": {
    "` + key + `": {"hasTrustDialogAccepted": true, "allowedTools": []}
  }
}
`
	home, configPath := fakeHome(t, before)
	report := planFor(t, home, map[string]string{"maimuzo/continuo": repo}, "maimuzo/continuo")

	result, err := trust.Apply(context.Background(), optionsFor(home, map[string]string{"maimuzo/continuo": repo}), report)
	if err != nil {
		t.Fatalf("実行できなかった: %v", err)
	}
	if len(result.Changed) != 0 {
		t.Errorf("既に信頼済みなのに書き換えている: %+v", result.Changed)
	}
	if len(result.Skipped) != 1 {
		t.Errorf("触らなかった件として数えていない: %+v", result.Skipped)
	}
	if result.BackupPath != "" {
		t.Errorf("書き換えていないのにバックアップを作っている: %s", result.BackupPath)
	}
	if got := readFile(t, configPath); got != before {
		t.Errorf("ファイルが変わっている\n期待:\n%s\n実際:\n%s", before, got)
	}
	if names := backupNames(t, home); len(names) != 0 {
		t.Errorf("バックアップのファイルが作られている: %v", names)
	}
}

// 目的: projects の下の他のリポジトリの記述を1つも変えないことを確認する。
//
// **`~/.claude.json` には認証情報を含む全設定が同居している**（設計 4-3）。
// 触るのは対象の hasTrustDialogAccepted だけである。
//
// 与える情報: 別のリポジトリの記述と、continuo が知らないトップレベルのキーを持つ `.claude.json`。
// 成功条件: 追加した鍵以外のすべてが、中身として変わっていないこと。
func TestApply_他のリポジトリの記述を1つも変えない(t *testing.T) {
	repo := initRepo(t, "continuo")
	home, configPath := fakeHome(t, otherSettings)
	report := planFor(t, home, map[string]string{"maimuzo/continuo": repo}, "maimuzo/continuo")

	if _, err := trust.Apply(context.Background(), optionsFor(home, map[string]string{"maimuzo/continuo": repo}), report); err != nil {
		t.Fatalf("登録できなかった: %v", err)
	}

	after := readFile(t, configPath)
	key := trustKeyOf(t, repo)

	// 追加した鍵を取り除いたものが、元の中身と同じであること。
	root, ok := decodeJSON(t, "書き換えたあとの ~/.claude.json", after).(map[string]any)
	if !ok {
		t.Fatalf("トップレベルがオブジェクトではない")
	}
	projects, ok := root["projects"].(map[string]any)
	if !ok {
		t.Fatalf("projects がオブジェクトではない")
	}
	if _, ok := projects[key]; !ok {
		t.Fatalf("登録した鍵が無い")
	}
	delete(projects, key)

	trimmed, err := marshalJSON(root)
	if err != nil {
		t.Fatalf("比較用に組み立て直せなかった: %v", err)
	}
	assertSameJSON(t, "対象以外の記述", otherSettings, trimmed)
}

// 目的: 書き込みの直前に読み直すことを確認する。
//
// **起動中の Claude Code のセッションが同じファイルを書き戻している**（設計 4-3）。
// 調べたときの内容で上書きすると、その間の変更が消える。
//
// 与える情報: Plan のあと、Apply の前に別のキーが足された `.claude.json`。
// 成功条件: あとから足されたキーが、書き換えたあとも残っていること。
func TestApply_書き込みの直前に読み直す(t *testing.T) {
	repo := initRepo(t, "continuo")
	home, configPath := fakeHome(t, `{"projects":{}}`)
	report := planFor(t, home, map[string]string{"maimuzo/continuo": repo}, "maimuzo/continuo")

	// Plan と Apply の間に、別のセッションが書き戻したことにする。
	const meanwhile = `{
  "projects": {
    "/another/repo": {"hasTrustDialogAccepted": true}
  },
  "numStartups": 99
}
`
	if err := os.WriteFile(configPath, []byte(meanwhile), 0o600); err != nil {
		t.Fatalf("途中の書き換えを再現できなかった: %v", err)
	}

	if _, err := trust.Apply(context.Background(), optionsFor(home, map[string]string{"maimuzo/continuo": repo}), report); err != nil {
		t.Fatalf("登録できなかった: %v", err)
	}

	after := readFile(t, configPath)
	if !strings.Contains(after, "/another/repo") || !strings.Contains(after, "numStartups") {
		t.Errorf("調べたときの内容で上書きしている（あとから足された記述が消えた）:\n%s", after)
	}
	if _, ok := projectEntry(t, after, trustKeyOf(t, repo)); !ok {
		t.Errorf("登録そのものができていない:\n%s", after)
	}
}

// 目的: 調べたあとに JSON の形が壊れたら、1バイトも書かずに止めることを確認する。
//
// **これは「書き込みの直前に読み直す」ことと対になる守りである**（設計 3-33 / 4-3）。
// 起動中の Claude Code のセッションが書き戻しに失敗して壊れた中身を残したまま、
// continuo がそこへ read-modify-write を撃つと、**認証情報を含む全設定を失いうる。**
// 調べた時点では読めていたので、Apply の中の検査だけがこれを止められる。
//
// 与える情報: 調べたあとに壊した `.claude.json` を5通り。
// 成功条件: ErrUnexpectedShape が返り、ファイルが変わらず、バックアップも作られないこと。
func TestApply_調べたあとに形が壊れたら1バイトも書かない(t *testing.T) {
	repo := initRepo(t, "continuo")
	key := trustKeyOf(t, repo)

	cases := map[string]string{
		"トップレベルが配列":                   `[1, 2, 3]`,
		"projects が配列":                `{"projects": ["/a", "/b"]}`,
		"projects の要素が文字列":            `{"projects": {"` + key + `": "trusted"}}`,
		"hasTrustDialogAccepted が文字列": `{"projects": {"` + key + `": {"hasTrustDialogAccepted": "yes"}}}`,
		"JSON として壊れている":               `{"projects": {`,
	}
	for name, broken := range cases {
		t.Run(name, func(t *testing.T) {
			home, configPath := fakeHome(t, `{"projects":{}}`)
			clones := map[string]string{"maimuzo/continuo": repo}
			report := planFor(t, home, clones, "maimuzo/continuo")
			if len(report.Pending()) != 1 {
				t.Fatalf("調べた時点では登録の対象であるはずだが、そうなっていない: %+v", report.Entries)
			}

			// 調べたあとに壊れたことにする。
			if err := os.WriteFile(configPath, []byte(broken), 0o600); err != nil {
				t.Fatalf("壊れた状態を再現できなかった: %v", err)
			}

			result, err := trust.Apply(context.Background(), optionsFor(home, clones), report)
			if !errors.Is(err, trust.ErrUnexpectedShape) {
				t.Fatalf("形が違うのに止まっていない: err=%v, result=%+v", err, result)
			}
			if got := readFile(t, configPath); got != broken {
				t.Errorf("止まったのにファイルが変わっている\n期待:\n%s\n実際:\n%s", broken, got)
			}
			if names := backupNames(t, home); len(names) != 0 {
				t.Errorf("書いていないのにバックアップを作っている: %v", names)
			}
		})
	}
}

// 目的: 調べた時点で形が読めなかったら、登録の対象から外して何もしないことを確認する。
//
// 与える情報: トップレベルが配列である `.claude.json`。
// 成功条件: 登録の対象が0件になり、ファイルもバックアップも変わらないこと。
func TestApply_調べた時点で形が読めなければ対象から外す(t *testing.T) {
	repo := initRepo(t, "continuo")
	const broken = `[1, 2, 3]`
	home, configPath := fakeHome(t, broken)
	clones := map[string]string{"maimuzo/continuo": repo}
	report := planFor(t, home, clones, "maimuzo/continuo")

	if len(report.Problems()) != 1 {
		t.Fatalf("読めない形なのに調べられた扱いになっている: %+v", report.Entries)
	}
	result, err := trust.Apply(context.Background(), optionsFor(home, clones), report)
	if err != nil {
		t.Fatalf("何もしないはずなのにエラーになった: %v", err)
	}
	if len(result.Changed) != 0 {
		t.Errorf("書き込んでいる: %+v", result.Changed)
	}
	if got := readFile(t, configPath); got != broken {
		t.Errorf("ファイルが変わっている:\n%s", got)
	}
	if names := backupNames(t, home); len(names) != 0 {
		t.Errorf("バックアップを作っている: %v", names)
	}
}

// 目的: `~/.claude.json` が無いとき、作らずに止めることを確認する。
//
// **Claude Code を一度も起動していない状態でこのファイルを先に作ると、
// Claude Code が初回の設定を済ませたものとして扱う可能性がある。**
//
// 与える情報: `.claude.json` を置いていない偽のホームディレクトリ。
// 成功条件: ErrNoClaudeConfig が返り、ファイルが作られないこと。
func TestApply_claudejsonが無ければ作らずに止める(t *testing.T) {
	repo := initRepo(t, "continuo")
	home, configPath := fakeHome(t, "")
	report := planFor(t, home, map[string]string{"maimuzo/continuo": repo}, "maimuzo/continuo")

	_, err := trust.Apply(context.Background(), optionsFor(home, map[string]string{"maimuzo/continuo": repo}), report)
	if !errors.Is(err, trust.ErrNoClaudeConfig) {
		t.Fatalf("無いことを表すエラーが返っていない: %v", err)
	}
	if _, statErr := os.Stat(configPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("止めたのにファイルを作っている: %v", statErr)
	}
}

// 目的: 元のファイルの権限を引き継ぐことを確認する。
//
// **このファイルには認証情報を含む全設定が入っている。**書き換えたあとに
// 0644 になっていると、同じ機械の他の利用者から読めるようになる。
//
// 与える情報: 0600 の `.claude.json`。
// 成功条件: 書き換えたあとも 0600 であり、バックアップも 0600 であること。
func TestApply_権限を引き継ぐ(t *testing.T) {
	repo := initRepo(t, "continuo")
	home, configPath := fakeHome(t, `{"projects":{}}`)
	report := planFor(t, home, map[string]string{"maimuzo/continuo": repo}, "maimuzo/continuo")

	result, err := trust.Apply(context.Background(), optionsFor(home, map[string]string{"maimuzo/continuo": repo}), report)
	if err != nil {
		t.Fatalf("登録できなかった: %v", err)
	}
	for _, path := range []string{configPath, result.BackupPath} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("%s を確かめられなかった: %v", path, statErr)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s の権限が 0600 でない: %o", path, perm)
		}
	}
}

// 目的: clone が無いなど調べられなかったものを、書き込みの対象にしないことを確認する。
//
// 与える情報: 登録できるリポジトリ1つと、clone の無いリポジトリ1つ。
// 成功条件: 登録できるほうだけが書かれ、もう一方の鍵が作られないこと。
func TestApply_調べられなかったものは書き込まない(t *testing.T) {
	repo := initRepo(t, "continuo")
	home, configPath := fakeHome(t, `{"projects":{}}`)
	clones := map[string]string{"maimuzo/continuo": repo}
	report := planFor(t, home, clones, "maimuzo/continuo", "maimuzo/nowhere")

	result, err := trust.Apply(context.Background(), optionsFor(home, clones), report)
	if err != nil {
		t.Fatalf("登録できなかった: %v", err)
	}
	if len(result.Changed) != 1 || result.Changed[0].Repository != "maimuzo/continuo" {
		t.Fatalf("書き込んだ対象が想定と違う: %+v", result.Changed)
	}
	after := readFile(t, configPath)
	if strings.Contains(after, "nowhere") {
		t.Errorf("clone の無いリポジトリまで書かれている:\n%s", after)
	}
}

// 目的: バックアップの名前に時刻が入り、消さずに残ることを確認する（設計 3-33）。
//
// 与える情報: 時刻を固定した Options。
// 成功条件: `~/.claude.json.continuo-backup-<RFC3339>` という名前で残ること。
func TestApply_バックアップの名前は時刻つきで残る(t *testing.T) {
	repo := initRepo(t, "continuo")
	home, _ := fakeHome(t, `{"projects":{}}`)
	report := planFor(t, home, map[string]string{"maimuzo/continuo": repo}, "maimuzo/continuo")

	fixed := time.Date(2026, 8, 20, 13, 45, 12, 0, time.FixedZone("JST", 9*60*60))
	opts := optionsFor(home, map[string]string{"maimuzo/continuo": repo})
	opts.Now = func() time.Time { return fixed }

	result, err := trust.Apply(context.Background(), opts, report)
	if err != nil {
		t.Fatalf("登録できなかった: %v", err)
	}
	want := filepath.Join(home, ".claude.json.continuo-backup-2026-08-20T13:45:12+09:00")
	if result.BackupPath != want {
		t.Errorf("バックアップの名前が想定と違う: got %q, want %q", result.BackupPath, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("バックアップが残っていない: %v", err)
	}
}

// planFor はテスト用に Plan を1回呼ぶ。
//
// t: テストコンテキスト。
// home: 偽のホームディレクトリ。
// clones: "owner/repo" から clone の絶対パスへの対応。
// repositories: trust.repositories に書かれているとみなす値。
// 戻り値: 調べた結果。
func planFor(t *testing.T, home string, clones map[string]string, repositories ...string) *trust.Report {
	t.Helper()
	opts := optionsFor(home, clones)
	opts.Repositories = repositories
	report, err := trust.Plan(context.Background(), opts)
	if err != nil {
		t.Fatalf("調べられなかった: %v", err)
	}
	return report
}

// optionsFor はテスト用の Options を組み立てる。
//
// home: 偽のホームディレクトリ。
// clones: "owner/repo" から clone の絶対パスへの対応。
// 戻り値: Options。
func optionsFor(home string, clones map[string]string) trust.Options {
	return trust.Options{HomeDir: home, ResolveClone: staticClones(clones)}
}

// backupNames は偽のホームディレクトリにあるバックアップのファイル名を返す。
//
// t: テストコンテキスト。
// home: 偽のホームディレクトリ。
// 戻り値: バックアップのファイル名の並び。
func backupNames(t *testing.T, home string) []string {
	t.Helper()
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("%s を読めなかった: %v", home, err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), trust.BackupPrefix) {
			names = append(names, e.Name())
		}
	}
	return names
}
