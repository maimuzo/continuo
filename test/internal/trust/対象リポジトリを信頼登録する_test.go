// {"RUCM-CFG-SHA256": "5c0a174a30e3df14be9fd9c6e9bf7edfe2c1f1488198f00e033bd8428061e181", "SOURCE": "docs/spec/usecases/particular_case/対象リポジトリを信頼登録する.cfg.json"}
//
// **ユースケース記述「対象リポジトリを信頼登録する」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
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

// {"RUCM-PATH": "P001"}
//
// 目的: 未信頼のリポジトリを登録し、そのときバックアップを残すことを確認する。
//
// **バックアップは消さない**（設計 3-33）。書き換えを元へ戻す唯一の手段である。
//
// 与える情報: 他のリポジトリの記述を持つ `.claude.json` と、未信頼のリポジトリ1つ。
// 成功条件: hasTrustDialogAccepted が true になり、
// バックアップに書き換える前の中身がそのまま残っていること。
func Test_対象リポジトリを信頼登録する_P001_未信頼のリポジトリを登録しバックアップを残す(t *testing.T) {
	repo := initRepo(t, "hello-world")
	home, configPath := fakeHome(t, otherSettings)
	report := planFor(t, home, map[string]string{"octocat/hello-world": repo}, "octocat/hello-world")

	result, err := trust.Apply(context.Background(), optionsFor(home, map[string]string{"octocat/hello-world": repo}), report)
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

// {"RUCM-PATH": "P001"}
//
// 目的: 書き込んだあと、巡回のループから信頼済みに見えることを確認する。
//
// **鍵の作り方がずれていると「書いたのに効かない」が静かに起きる。**
// dispatch の直前の検査（internal/workspace）と同じ関数で確かめる。
//
// 与える情報: 未信頼のリポジトリ1つ。
// 成功条件: Apply の確認で問題が出ず、workspace.CheckTrustForClonePath が真を返すこと。
func Test_対象リポジトリを信頼登録する_P001_書き込んだものが巡回のループから信頼済みに見える(t *testing.T) {
	repo := initRepo(t, "hello-world")
	home, _ := fakeHome(t, `{"projects":{}}`)
	report := planFor(t, home, map[string]string{"octocat/hello-world": repo}, "octocat/hello-world")

	result, err := trust.Apply(context.Background(), optionsFor(home, map[string]string{"octocat/hello-world": repo}), report)
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

// {"RUCM-PATH": "P009"}
//
// 目的: 既に true のものは触らず、バックアップも書き込みも行わないことを確認する。
//
// 与える情報: 対象のリポジトリが既に信頼済みである `.claude.json`。
// 成功条件: ファイルが1バイトも変わらず、バックアップが作られないこと。
func Test_対象リポジトリを信頼登録する_P009_既にtrueのものは触らない(t *testing.T) {
	repo := initRepo(t, "hello-world")
	key := trustKeyOf(t, repo)
	before := `{
  "projects": {
    "` + key + `": {"hasTrustDialogAccepted": true, "allowedTools": []}
  }
}
`
	home, configPath := fakeHome(t, before)
	report := planFor(t, home, map[string]string{"octocat/hello-world": repo}, "octocat/hello-world")

	result, err := trust.Apply(context.Background(), optionsFor(home, map[string]string{"octocat/hello-world": repo}), report)
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

// {"RUCM-PATH": "P001"}
//
// 目的: projects の下の他のリポジトリの記述を1つも変えないことを確認する。
//
// **`~/.claude.json` には認証情報を含む全設定が同居している**（設計 4-3）。
// 触るのは対象の hasTrustDialogAccepted だけである。
//
// 与える情報: 別のリポジトリの記述と、continuo が知らないトップレベルのキーを持つ `.claude.json`。
// 成功条件: 追加した鍵以外のすべてが、中身として変わっていないこと。
func Test_対象リポジトリを信頼登録する_P001_他のリポジトリの記述を1つも変えない(t *testing.T) {
	repo := initRepo(t, "hello-world")
	home, configPath := fakeHome(t, otherSettings)
	report := planFor(t, home, map[string]string{"octocat/hello-world": repo}, "octocat/hello-world")

	if _, err := trust.Apply(context.Background(), optionsFor(home, map[string]string{"octocat/hello-world": repo}), report); err != nil {
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

// {"RUCM-PATH": "P001"}
//
// 目的: 書き込みの直前に読み直すことを確認する。
//
// **起動中の Claude Code のセッションが同じファイルを書き戻している**（設計 4-3）。
// 調べたときの内容で上書きすると、その間の変更が消える。
//
// 与える情報: Plan のあと、Apply の前に別のキーが足された `.claude.json`。
// 成功条件: あとから足されたキーが、書き換えたあとも残っていること。
func Test_対象リポジトリを信頼登録する_P001_書き込みの直前に読み直す(t *testing.T) {
	repo := initRepo(t, "hello-world")
	home, configPath := fakeHome(t, `{"projects":{}}`)
	report := planFor(t, home, map[string]string{"octocat/hello-world": repo}, "octocat/hello-world")

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

	if _, err := trust.Apply(context.Background(), optionsFor(home, map[string]string{"octocat/hello-world": repo}), report); err != nil {
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

// {"RUCM-PATH": "P011"}
//
// 目的: 調べたあとに JSON の形が壊れたら、1バイトも書かずに止めることを確認する。
//
// **これは「書き込みの直前に読み直す」ことと対になる守りである**（設計 3-33 / 4-3）。
// 起動中の Claude Code のセッションが書き戻しに失敗して壊れた中身を残したまま、
// continuo がそこへ read-modify-write を撃つと、**認証情報を含む全設定を失いうる。**
// 調べた時点では読めていたので、Apply の中の検査だけがこれを止められる。
//
// 与える情報: 調べたあとに壊した `.claude.json` を5通り。
// 成功条件: ErrUnexpectedShape が返り、ファイルが変わらず、バックアップも作られないこと。
func Test_対象リポジトリを信頼登録する_P011_調べたあとに形が壊れたら1バイトも書かない(t *testing.T) {
	repo := initRepo(t, "hello-world")
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
			clones := map[string]string{"octocat/hello-world": repo}
			report := planFor(t, home, clones, "octocat/hello-world")
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

// {"RUCM-PATH": "P013"}
//
// 目的: 調べた時点で形が読めなかったら、登録の対象から外して何もしないことを確認する。
//
// 与える情報: トップレベルが配列である `.claude.json`。
// 成功条件: 登録の対象が0件になり、ファイルもバックアップも変わらないこと。
func Test_対象リポジトリを信頼登録する_P013_調べた時点で形が読めなければ対象から外す(t *testing.T) {
	repo := initRepo(t, "hello-world")
	const broken = `[1, 2, 3]`
	home, configPath := fakeHome(t, broken)
	clones := map[string]string{"octocat/hello-world": repo}
	report := planFor(t, home, clones, "octocat/hello-world")

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

// {"RUCM-PATH": "P012"}
//
// 目的: `~/.claude.json` が無いとき、作らずに止めることを確認する。
//
// **Claude Code を一度も起動していない状態でこのファイルを先に作ると、
// Claude Code が初回の設定を済ませたものとして扱う可能性がある。**
//
// 与える情報: `.claude.json` を置いていないテスト用ホームディレクトリ。
// 成功条件: ErrNoClaudeConfig が返り、ファイルが作られないこと。
func Test_対象リポジトリを信頼登録する_P012_claudejsonが無ければ作らずに止める(t *testing.T) {
	repo := initRepo(t, "hello-world")
	home, configPath := fakeHome(t, "")
	report := planFor(t, home, map[string]string{"octocat/hello-world": repo}, "octocat/hello-world")

	_, err := trust.Apply(context.Background(), optionsFor(home, map[string]string{"octocat/hello-world": repo}), report)
	if !errors.Is(err, trust.ErrNoClaudeConfig) {
		t.Fatalf("無いことを表すエラーが返っていない: %v", err)
	}
	if _, statErr := os.Stat(configPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("止めたのにファイルを作っている: %v", statErr)
	}
}

// {"RUCM-PATH": "P001"}
//
// 目的: 元のファイルの権限を引き継ぐことを確認する。
//
// **このファイルには認証情報を含む全設定が入っている。**書き換えたあとに
// 0644 になっていると、同じ機械の他の利用者から読めるようになる。
//
// 与える情報: 0600 の `.claude.json`。
// 成功条件: 書き換えたあとも 0600 であり、バックアップも 0600 であること。
func Test_対象リポジトリを信頼登録する_P001_権限を引き継ぐ(t *testing.T) {
	repo := initRepo(t, "hello-world")
	home, configPath := fakeHome(t, `{"projects":{}}`)
	report := planFor(t, home, map[string]string{"octocat/hello-world": repo}, "octocat/hello-world")

	result, err := trust.Apply(context.Background(), optionsFor(home, map[string]string{"octocat/hello-world": repo}), report)
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

// {"RUCM-PATH": "P002"}
//
// 目的: clone が無いなど調べられなかったものを、書き込みの対象にしないことを確認する。
//
// 与える情報: 登録できるリポジトリ1つと、clone の無いリポジトリ1つ。
// 成功条件: 登録できるほうだけが書かれ、もう一方の鍵が作られないこと。
func Test_対象リポジトリを信頼登録する_P002_調べられなかったものは書き込まない(t *testing.T) {
	repo := initRepo(t, "hello-world")
	home, configPath := fakeHome(t, `{"projects":{}}`)
	clones := map[string]string{"octocat/hello-world": repo}
	report := planFor(t, home, clones, "octocat/hello-world", "octocat/nowhere")

	result, err := trust.Apply(context.Background(), optionsFor(home, clones), report)
	if err != nil {
		t.Fatalf("登録できなかった: %v", err)
	}
	if len(result.Changed) != 1 || result.Changed[0].Repository != "octocat/hello-world" {
		t.Fatalf("書き込んだ対象が想定と違う: %+v", result.Changed)
	}
	after := readFile(t, configPath)
	if strings.Contains(after, "nowhere") {
		t.Errorf("clone の無いリポジトリまで書かれている:\n%s", after)
	}
}

// {"RUCM-PATH": "P001"}
//
// 目的: バックアップの名前に時刻が入り、消さずに残ることを確認する（設計 3-33）。
//
// 与える情報: 時刻を固定した Options。
// 成功条件: `~/.claude.json.continuo-backup-<RFC3339>` という名前で残ること。
func Test_対象リポジトリを信頼登録する_P001_バックアップの名前は時刻つきで残る(t *testing.T) {
	repo := initRepo(t, "hello-world")
	home, _ := fakeHome(t, `{"projects":{}}`)
	report := planFor(t, home, map[string]string{"octocat/hello-world": repo}, "octocat/hello-world")

	fixed := time.Date(2026, 8, 20, 13, 45, 12, 0, time.FixedZone("JST", 9*60*60))
	opts := optionsFor(home, map[string]string{"octocat/hello-world": repo})
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

// {"RUCM-PATH": "P015"}
//
// 目的: `continuo trust --dry-run` が要求内容を出すだけで、`~/.claude.json` を
// 1バイトも書き換えないことを、実際にコマンドを起動して確認する。
//
// **`--dry-run` は信頼のダイアログの代わりである**（設計 3-33）。
// ここが書き換えてしまうと、確かめてから決めるという手順そのものが成立しない。
//
// 与える情報: テスト用ホームディレクトリ・テスト用ghq mock の置き場所・
// trust.repositories に2件を書いた WORKFLOW.md。
// 成功条件: 出力に要求内容が出て、終了コードが 1（登録の対象が残っている）で、
// `~/.claude.json` が変わらず、バックアップも作られないこと。
func Test_対象リポジトリを信頼登録する_P015_dryrunは要求内容を出すだけで書き換えない(t *testing.T) {
	requireCommands(t, "ghq", "git")

	env := setUpCLI(t)
	stdout, code := runContinuo(t, env, "trust", "--dry-run")

	if code != 1 {
		t.Errorf("登録の対象が残っているのに終了コードが 1 でない: %d\n%s", code, stdout)
	}
	for _, want := range []string{
		"octocat/demo-a", "Bash(rm -rf:*)", "/etc", "payments", "docs",
		"--dry-run なので何も書き換えていません",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("出力に %q が無い:\n%s", want, stdout)
		}
	}
	if got := readFile(t, env.configPath); got != env.before {
		t.Errorf("--dry-run なのにファイルが変わっている\n期待:\n%s\n実際:\n%s", env.before, got)
	}
	if names := backupNames(t, env.home); len(names) != 0 {
		t.Errorf("--dry-run なのにバックアップを作っている: %v", names)
	}
}

// {"RUCM-PATH": "P001"}
//
// 目的: `continuo trust` が列挙された2件だけを登録し、列挙していないものに触らないことを、
// 実際にコマンドを起動して確認する。あわせて、2回目の実行が何も書かないことを見る。
//
// 与える情報: dry-run と同じ環境。ghq には列挙していないリポジトリも置いてある。
// 成功条件: 終了コードが 0、列挙した2件だけが登録され、
// 列挙していないリポジトリの記述が作られないこと。2回目は何も書かないこと。
func Test_対象リポジトリを信頼登録する_P001_列挙した2件だけを登録し2回目は何も書かない(t *testing.T) {
	requireCommands(t, "ghq", "git")

	env := setUpCLI(t)
	stdout, code := runContinuo(t, env, "trust")

	if code != 0 {
		t.Fatalf("登録できたのに終了コードが 0 でない: %d\n%s", code, stdout)
	}
	after := readFile(t, env.configPath)
	for _, repo := range []string{"demo-a", "demo-b"} {
		key := trustKeyOf(t, filepath.Join(env.ghqRoot, "github.com", "octocat", repo))
		entry, ok := projectEntry(t, after, key)
		if !ok || entry["hasTrustDialogAccepted"] != true {
			t.Errorf("%s が登録されていない:\n%s", repo, after)
		}
	}
	if strings.Contains(after, "demo-unlisted") {
		t.Errorf("列挙していないリポジトリまで登録されている:\n%s", after)
	}
	if names := backupNames(t, env.home); len(names) != 1 {
		t.Errorf("バックアップが1つ残っていない: %v", names)
	}

	// 2回目。既に true なので何も書かない。
	stdout2, code2 := runContinuo(t, env, "trust")
	if code2 != 0 {
		t.Errorf("2回目の終了コードが 0 でない: %d\n%s", code2, stdout2)
	}
	if got := readFile(t, env.configPath); got != after {
		t.Errorf("2回目でファイルが変わっている\n1回目:\n%s\n2回目:\n%s", after, got)
	}
	if names := backupNames(t, env.home); len(names) != 1 {
		t.Errorf("2回目でバックアップが増えている: %v", names)
	}
}

// {"RUCM-PATH": "P001"}
//
// 目的: 調べたあとに信頼の登録が外れていたリポジトリを、黙って書き換えずに、結果へ警告を1行出すことを確認する。
//
// **要求内容の応答は「既に信頼済み。触りません」と出している。**そのあとで登録が外れていると、
// 書き込みの直前の読み直しで未信頼に見えるので、continuo は書き換える。
// 結果に何も出さないと、人間は「触りません」と読んだリポジトリが書き換わったことに気づけない。
//
// 与える情報: Plan の時点では信頼済みで、Apply の前に hasTrustDialogAccepted が false へ書き戻された `.claude.json`。
// 成功条件: Apply の結果の RevokedSincePlan にそのリポジトリが入り、
// WriteApplyResult の出力に、リポジトリ名と「調べた時点では信頼済み」の警告が出ること。
// 調べた時点で未信頼だったリポジトリは、RevokedSincePlan に入らないこと。
func Test_対象リポジトリを信頼登録する_P001_調べたあとに登録が外れていたら結果に警告を出す(t *testing.T) {
	trustedRepo := initRepo(t, "hello-world")
	pendingRepo := initRepo(t, "spoon-knife")
	trustedKey := trustKeyOf(t, trustedRepo)
	home, configPath := fakeHome(t, `{
  "projects": {
    "`+trustedKey+`": {"hasTrustDialogAccepted": true}
  }
}
`)
	clones := map[string]string{"octocat/hello-world": trustedRepo, "octocat/spoon-knife": pendingRepo}
	report := planFor(t, home, clones, "octocat/hello-world", "octocat/spoon-knife")
	if !report.Entries[0].Trusted || report.Entries[1].Trusted {
		t.Fatalf("前提が崩れている（1件目が信頼済み、2件目が未信頼のはず）: %+v", report.Entries)
	}

	// Plan と Apply の間に、別のセッションが登録を外したことにする。
	if err := os.WriteFile(configPath, []byte(`{
  "projects": {
    "`+trustedKey+`": {"hasTrustDialogAccepted": false}
  }
}
`), 0o600); err != nil {
		t.Fatalf("途中の書き換えを再現できなかった: %v", err)
	}

	result, err := trust.Apply(context.Background(), optionsFor(home, clones), report)
	if err != nil {
		t.Fatalf("登録できなかった: %v", err)
	}
	if len(result.Changed) != 2 {
		t.Fatalf("2件とも書き換えるはず: %+v", result.Changed)
	}
	if len(result.RevokedSincePlan) != 1 || result.RevokedSincePlan[0].Repository != "octocat/hello-world" {
		t.Fatalf("調べたあとに登録が外れていた項目が想定と違う: %+v", result.RevokedSincePlan)
	}

	var out strings.Builder
	if err := trust.WriteApplyResult(&out, result); err != nil {
		t.Fatalf("結果を書き出せなかった: %v", err)
	}
	got := out.String()
	var warning string
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "調べた時点では信頼済み") {
			warning = line
		}
	}
	if warning == "" {
		t.Fatalf("警告の行が出ていない:\n%s", got)
	}
	if !strings.Contains(warning, "octocat/hello-world") {
		t.Errorf("警告の行に、登録が外れていたリポジトリの名前が無い: %s", warning)
	}
	if strings.Contains(warning, "octocat/spoon-knife") {
		t.Errorf("調べた時点で未信頼だったリポジトリまで警告に載せている: %s", warning)
	}
}
