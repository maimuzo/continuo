package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/daemon"
	"github.com/maimuzo/continuo/internal/instance"
	"github.com/maimuzo/continuo/internal/lock"
)

// writeWiringWorkflow は、結線だけを確かめるための最小の WORKFLOW.md を書く。
//
// **バイナリを起動しない。**このファイルのテストは daemon.Run をそのまま呼び、
// 起動の段（設定 → 接続先の検査 → flock → 組み立て）で止まるところまでを見る。
//
// t: 呼び出し元のテスト。
// root: WORKFLOW.md を置くディレクトリ。
// providerExtra: `tracker.provider` の中へ足す行（4桁の字下げ。空でよい）。
// extra: front matter へ足す節（末尾に改行を含めること。空でよい）。
// 戻り値: 書いた WORKFLOW.md の絶対パス。
func writeWiringWorkflow(t *testing.T, root, providerExtra, extra string) string {
	t.Helper()

	content := fmt.Sprintf(`---
tracker:
  provider:
    owner: octocat
    project_number: 3
    status_field: Status
%sworkspace:
  root: %s
herdr:
  socket: %s
  protocol: 20
rate_limit:
  source: none
%s---

{{.issue.identifier}} を実装してください。
`, providerExtra, filepath.Join(root, "wt"), filepath.Join(root, "h.sock"), extra)

	path := filepath.Join(root, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	return path
}

// wiringRoot は短い一時ディレクトリを1つ作る（socket のパスを103バイト以内に保つため）。
//
// **ホームディレクトリもここへ向ける。**二重起動を防ぐロックは
// `~/.continuo/continuo.lock` に置かれる（設計 3-17）。**向けないと、テストが利用者の
// 本物の `~/.continuo` を掴み、動いている continuo と取り合いになる。**
//
// t: 呼び出し元のテスト。
// 戻り値: 実体のパス（symlink を解決済み）。
func wiringRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "cw")
	if err != nil {
		t.Fatalf("一時ディレクトリを作れません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("一時ディレクトリを解決できません: %v", err)
	}
	t.Setenv("HOME", root)
	return root
}

// TestValidateGraphQLEndpoint_httpsとループバック宛のhttpだけを受け付ける は、
// トークンの送り先の検査を固定する。
//
// 目的: 環境変数に書いた URL へ `gh auth token` のトークンが `Authorization: Bearer` で
// 送られる。**scheme もホストも見ずに採用すると、1行書き足すだけで送り先を変えられる。**
// 与える情報: 空文字・https・ループバック宛の http・外部宛の http・scheme 違い・壊れた URL。
// 成功条件: 空文字と https とループバック宛の http だけが通り、ほかはエラーになること。
func TestValidateGraphQLEndpoint_httpsとループバック宛のhttpだけを受け付ける(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "空なら本番の GitHub を使うので検査しない", raw: "", wantErr: false},
		{name: "本番の GitHub は通る", raw: "https://api.github.com/graphql", wantErr: false},
		{name: "GitHub Enterprise Server も https なら通る", raw: "https://ghe.example.com/api/graphql", wantErr: false},
		{name: "テストの偽サーバ（127.0.0.1 の http）は通る", raw: "http://127.0.0.1:18999/graphql", wantErr: false},
		{name: "localhost の http も通る", raw: "http://localhost:18999/graphql", wantErr: false},
		{name: "ループバック以外の http は断る", raw: "http://example.com/graphql", wantErr: true},
		{name: "https でも http でもない scheme は断る", raw: "ftp://example.com/graphql", wantErr: true},
		{name: "ホスト名が無ければ断る", raw: "https:///graphql", wantErr: true},
		{name: "URL として解釈できなければ断る", raw: "https://%zz/graphql", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := daemon.ValidateGraphQLEndpoint(tc.raw)
			if tc.wantErr && err == nil {
				t.Fatalf("%q を受け付けてしまった（トークンの送り先になる）", tc.raw)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("%q を断ってしまった: %v", tc.raw, err)
			}
		})
	}
}

// TestWaitWithTimeout_期限内に終わらなければ待つのをやめる は、終了処理の期限を固定する。
//
// 目的: 終了時の turn ループの待ちが無期限だと、turn ループが1本でも返らなくなった時点で
// `SIGKILL` でしか止められなくなる（設計 3-4 の終了の作法）。
// 与える情報: すぐ返る処理と、テストが終わるまで返らない処理。
// 成功条件: すぐ返る処理では true、返らない処理では期限のあとに false が返ること。
func TestWaitWithTimeout_期限内に終わらなければ待つのをやめる(t *testing.T) {
	if !daemon.WaitWithTimeout(func() {}, 5*time.Second) {
		t.Fatal("すぐ返る処理を待ち切れなかった")
	}

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	start := time.Now()
	if daemon.WaitWithTimeout(func() { <-release }, 50*time.Millisecond) {
		t.Fatal("返らない処理を待ち切ったことになっている（無期限に待つと終了できない）")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("期限を過ぎても待ち続けた: %v", elapsed)
	}
}

// TestRun_ロックファイルを開けないときは二重起動と報告しない は、
// 起動を止めた理由の言い分けを固定する。
//
// 目的: ロックの置き場所を作れなかった運用者に「二重起動」と報告すると、
// 動いてもいない2つ目の continuo を探しに行かせることになる。
// 与える情報: `~/.continuo` を作れないホームディレクトリ（実体がファイルである）。
// 成功条件: 起動の段のエラー（daemon.ErrStartup）で、文言が「ロックファイルを用意できません」
// であり、**「二重起動」を含まないこと。**
func TestRun_ロックファイルを開けないときは二重起動と報告しない(t *testing.T) {
	root := wiringRoot(t)
	runtimeDir := filepath.Join(root, "rt")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("実行時ディレクトリを作れません: %v", err)
	}
	path := writeWiringWorkflow(t, root, "", "")

	// **ホームディレクトリの実体をファイルにする。**`~/.continuo` を作れないので、
	// 二重起動ではなく「ロックファイルを用意できません」で止まらなければならない。
	notADir := filepath.Join(root, "home-is-a-file")
	if err := os.WriteFile(notADir, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("ホームディレクトリの代わりのファイルを作れません: %v", err)
	}
	t.Setenv("HOME", notADir)

	t.Setenv(daemon.EnvRuntimeDir, runtimeDir)
	t.Setenv(daemon.EnvGraphQLEndpoint, "")

	err := daemon.Run(context.Background(), daemon.Options{
		ConfigPath: path,
		Logger:     slog.New(slog.DiscardHandler),
	})
	if err == nil {
		t.Fatal("ロックファイルを開けないのに起動できてしまった")
	}
	if !errors.Is(err, daemon.ErrStartup) {
		t.Fatalf("起動の段の失敗として印が付いていない: %v", err)
	}
	if !strings.Contains(err.Error(), "ロックファイルを用意できません") {
		t.Fatalf("ロックファイルを用意できないことが文言に出ていない: %v", err)
	}
	if strings.Contains(err.Error(), "二重起動") {
		t.Fatalf("設定の誤りを二重起動として報告している: %v", err)
	}
}

// TestRun_接続先の差し替えがループバック以外のhttpなら起動を止める は、
// トークンの送り先の検査が結線に入っていることを確かめる。
//
// 目的: 環境変数の URL へ GitHub のトークンが送られる。**平文で外部へ流さない。**
// 与える情報: `CONTINUO_GITHUB_GRAPHQL_ENDPOINT` に `http://example.com/graphql`。
// 成功条件: 起動の段のエラーで止まり、文言が https を求めていること。
// **ロックファイルを作る前に止まること**（実行時ディレクトリに何も残らない）。
func TestRun_接続先の差し替えがループバック以外のhttpなら起動を止める(t *testing.T) {
	root := wiringRoot(t)
	runtimeDir := filepath.Join(root, "rt")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("実行時ディレクトリを作れません: %v", err)
	}
	path := writeWiringWorkflow(t, root, "", "")

	t.Setenv(daemon.EnvRuntimeDir, runtimeDir)
	t.Setenv(daemon.EnvGraphQLEndpoint, "http://example.com/graphql")

	err := daemon.Run(context.Background(), daemon.Options{
		ConfigPath: path,
		Logger:     slog.New(slog.DiscardHandler),
	})
	if err == nil {
		t.Fatal("ループバック以外の http へ繋ぐ設定なのに起動できてしまった")
	}
	if !errors.Is(err, daemon.ErrStartup) {
		t.Fatalf("起動の段の失敗として印が付いていない: %v", err)
	}
	if !strings.Contains(err.Error(), "https") {
		t.Fatalf("https を求める文言になっていない: %v", err)
	}
	if entries, readErr := os.ReadDir(runtimeDir); readErr == nil && len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("接続先を確かめる前に実行時ディレクトリへ書き込んでいる: %v", names)
	}
}

// TestRun_ghがPATHに無ければ起動時検査の文言で落ちる は、gh を起動する順序を固定する。
//
// 目的: `token_source` の既定は `gh_auth` なので、依存の組み立ての中で `gh auth token` が
// 走る。**先に `gh` の有無を見ないと、直し方の書いてある起動時検査（設計 3-6）の文言に
// 辿り着かず、「トークンを取得できません」で落ちる。**
// 与える情報: `gh` が1つも無い PATH。
// 成功条件: 起動の段のエラーで、文言が「PATH にありません」であること。
func TestRun_ghがPATHに無ければ起動時検査の文言で落ちる(t *testing.T) {
	root := wiringRoot(t)
	runtimeDir := filepath.Join(root, "rt")
	emptyBin := filepath.Join(root, "emptybin")
	for _, dir := range []string{runtimeDir, emptyBin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("ディレクトリを作れません（%s）: %v", dir, err)
		}
	}
	path := writeWiringWorkflow(t, root, "", "")

	t.Setenv(daemon.EnvRuntimeDir, runtimeDir)
	t.Setenv(daemon.EnvGraphQLEndpoint, "")
	// **`gh` を1つも見つけられない PATH にする。**本物の gh を起動しないためでもある。
	t.Setenv("PATH", emptyBin)

	err := daemon.Run(context.Background(), daemon.Options{
		ConfigPath: path,
		Logger:     slog.New(slog.DiscardHandler),
	})
	if err == nil {
		t.Fatal("gh が無いのに起動できてしまった")
	}
	if !errors.Is(err, daemon.ErrStartup) {
		t.Fatalf("起動の段の失敗として印が付いていない: %v", err)
	}
	if !strings.Contains(err.Error(), "PATH にありません") {
		t.Fatalf("gh が PATH に無いことを指す文言になっていない: %v", err)
	}
}

// cleanupStatesWarning は、片付ける Status が終わったとみなす Status の外にあるときの
// 警告を見分ける文字列である。
//
// **文言そのもので数える。**slog の出力から「この警告が何件出たか」を数えるには、
// ほかの行と重ならない一片を持つしかない。
const cleanupStatesWarning = "tracker.terminal_states にありません"

// runForStartupLog は、起動の段で必ず落ちる状態で daemon.Run を1回呼び、そのログを返す。
//
// **`gh` を1つも見つけられない PATH で呼ぶ。**本物の `gh` を起動しないためであり、
// **段2b（依存の組み立て）で必ず落ちるので、巡回にも復元にも進まない。**
// 段1（設定を読む）のログだけを見たいときに使う。
//
// t: 呼び出し元のテスト。
// extra: front matter へ足す節（末尾に改行を含めること）。
// 戻り値: daemon.Run が書き出したログ全文。
func runForStartupLog(t *testing.T, extra string) string {
	t.Helper()

	root := wiringRoot(t)
	runtimeDir := filepath.Join(root, "rt")
	emptyBin := filepath.Join(root, "emptybin")
	for _, dir := range []string{runtimeDir, emptyBin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("ディレクトリを作れません（%s）: %v", dir, err)
		}
	}
	path := writeWiringWorkflow(t, root, "", extra)

	t.Setenv(daemon.EnvRuntimeDir, runtimeDir)
	t.Setenv(daemon.EnvGraphQLEndpoint, "")
	t.Setenv("PATH", emptyBin)

	var logged strings.Builder
	err := daemon.Run(context.Background(), daemon.Options{
		ConfigPath: path,
		Logger:     slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	// **落ちること自体は前提である。**段2b まで進めば、段1 のログは出揃っている。
	if err == nil {
		t.Fatal("gh が無い PATH なのに起動できてしまった")
	}
	return logged.String()
}

// TestRun_片付ける状態が終わったとみなす状態の外にあれば警告を出して起動を続ける は、
// **起動を止めずに知らせる**ことを固定する（設計 3-9。issue #35）。
//
// 目的: `cleanup.on_states` に `tracker.terminal_states` の外の値があるとき、
// 起動時に警告を1件だけ出し、**そこで起動を止めないこと。**
// **止めると、いま動いている人の continuo が版を上げた瞬間に起動しなくなる。**
// **どのキーのどの値かを本文に出すこと**（「食い違っています」だけでは、どの行を直せばよいか分からない）。
//
// 与える情報: 既定の `tracker.terminal_states`（`["Done"]`）と `cleanup.on_states: ["Archived"]`。
// 成功条件: 警告が1件だけ出て、本文に両方のキー名と `Archived` が入り、
// **設定の検証では落ちていない**こと（落ちる先は `gh` が無いことである）。
func TestRun_片付ける状態が終わったとみなす状態の外にあれば警告を出して起動を続ける(t *testing.T) {
	logged := runForStartupLog(t, "cleanup:\n  on_states: [\"Archived\"]\n")

	if got := strings.Count(logged, cleanupStatesWarning); got != 1 {
		t.Fatalf("警告が1件ではなく %d件だった\n%s", got, logged)
	}
	for _, want := range []string{"cleanup.on_states", "tracker.terminal_states", "Archived"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("%q が警告に出ていない\n%s", want, logged)
		}
	}
	// **設定の検証で落としていない**ことを確かめる（落ちる先は `gh` が無いことである）。
	if !strings.Contains(logged, "設定ファイルを読み込みました") {
		t.Fatalf("設定を読む段より前で止まっている\n%s", logged)
	}
}

// TestRun_片付ける状態が噛み合っていれば警告を出さない は、
// **読み飛ばされる警告を作らない**ことを確かめる。
//
// 目的: 噛み合っている設定と、片付けそのものを行わない設定では、警告を1件も出さないこと。
// 与える情報: `cleanup.on_states: ["Done"]`（既定の `terminal_states` と同じ）と、
// `cleanup.enabled: false` で外れた値を書いた設定の2通り。
// 成功条件: どちらもこの警告が1件も出ないこと。
func TestRun_片付ける状態が噛み合っていれば警告を出さない(t *testing.T) {
	cases := []struct {
		name  string
		extra string
	}{
		{
			name:  "終わったとみなす状態に全部入っている",
			extra: "cleanup:\n  on_states: [\"Done\"]\n",
		},
		{
			// **片付けそのものが走らないので、噛み合っていなくても何も起きない。**
			name:  "片付けを行わない設定",
			extra: "cleanup:\n  enabled: false\n  on_states: [\"Archived\"]\n",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logged := runForStartupLog(t, c.extra)
			if strings.Contains(logged, cleanupStatesWarning) {
				t.Fatalf("警告を出す設定ではないのに出ている\n%s", logged)
			}
		})
	}
}

// writeHangingGHAuthToken は、`gh auth token` が返ってこない偽の `gh` を PATH の先頭へ置く。
//
// **`gh auth status` には答える。**止まるのはトークンの取得だけにして、
// どの段で止まったのかを言い分けられるようにする。
//
// t: 呼び出し元のテスト。
// dir: 実行ファイルを置くディレクトリ。
func writeHangingGHAuthToken(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("テスト用gh mock を置くディレクトリを作れません: %v", err)
	}
	// **`exec` で置き換える。**シェルを残すと、殺したあともシェルが標準出力の書き手として
	// 残り、後始末を待つぶんテストが遅くなる。
	gh := `#!/bin/sh
if [ "$1" = "auth" ] && [ "$2" = "token" ]; then
  exec sleep 300
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(gh), 0o755); err != nil {
		t.Fatalf("テスト用gh mock を書けません: %v", err)
	}
}

// TestRun_ghauthtokenが返らなければ起動を止める は、
// 依存を組み立てる段の期限を固定する。
//
// 目的: `tracker.provider.token_source` の既定は `gh_auth` なので、依存の組み立ての中で
// `gh auth token` が走る。**そこに期限が無いと、gh が返らないとき continuo は何のログも
// 出さずに永久に止まる**（起動時検査にも復元にも巡回にも進まない）。Keychain がロックされて
// 確認のダイアログが出ると、無人で起動した continuo には答える人がいない。
// flock は握ったままなので、別の端末から起動すると「二重起動」と言われる。
//
// 与える情報: `gh auth token` が返ってこない偽の `gh` と、500 ミリ秒の起動時検査の期限。
//
// 成功条件: 30 秒以内に起動の段のエラーで返り、文言がトークンの取得の失敗を指すこと。
func TestRun_ghauthtokenが返らなければ起動を止める(t *testing.T) {
	root := wiringRoot(t)
	runtimeDir := filepath.Join(root, "rt")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("実行時ディレクトリを作れません: %v", err)
	}
	writeHangingGHAuthToken(t, binDir)
	path := writeWiringWorkflow(t, root, "", "")

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(daemon.EnvRuntimeDir, runtimeDir)
	t.Setenv(daemon.EnvGraphQLEndpoint, "")

	start := time.Now()
	err := daemon.Run(context.Background(), daemon.Options{
		ConfigPath:          path,
		Logger:              slog.New(slog.DiscardHandler),
		StartupCheckTimeout: 500 * time.Millisecond,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("gh auth token が返らないのに起動できてしまった")
	}
	if elapsed > 30*time.Second {
		t.Fatalf("期限を掛けずに待ち続けた: %v", elapsed)
	}
	if !errors.Is(err, daemon.ErrStartup) {
		t.Fatalf("起動の段の失敗として印が付いていない: %v", err)
	}
	if !strings.Contains(err.Error(), "トークンを取得できません") {
		t.Fatalf("トークンの取得で止まったことが文言に出ていない: %v", err)
	}
}

// TestWatchInterrupt_1回目で理由を出し2回目で待たずに終わる は、
// 「Ctrl+C を押しても何も反応しない」を潰した仕掛けを確かめる。
//
// 目的: 終了は3段の直列で最大 daemon.ShutdownBudget() だけ掛かる。**1回目の割り込みで
// 待たせる理由と抜け道を出さないと、その間ずっと画面が無反応になる。**また、2回目の
// 割り込みは**自分で数えて終わらせる。**`signal.Stop` で「元の動作」へ戻すやり方は、
// 起動元が `SIGINT` を無視に設定していると戻る先が「無視」になり、何も起きない。
// 与える情報: 自分自身へ送る `SIGINT` を2回と、os.Exit の代わりに呼ばれたことを記録する関数。
// 成功条件: 1回目で待つ理由・2回目の効き目・`kill -QUIT` の案内が出ること。
// 2回目で ExitInterrupted が渡ること。
func TestWatchInterrupt_1回目で理由を出し2回目で待たずに終わる(t *testing.T) {
	logs := &syncBuffer{}
	exited := make(chan int, 1)

	stop := daemon.WatchInterrupt(slog.New(slog.NewTextHandler(logs, nil)), func(code int) {
		exited <- code
	})
	// **登録を外すのは最後である。**外したあとに SIGINT が届くと、テストのプロセスごと死ぬ。
	defer stop()

	// 1回目。**まだ終わらない。**待たせる理由が出るだけである。
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("1回目の SIGINT を送れません: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "kill -QUIT") {
		if time.Now().After(deadline) {
			t.Fatalf("1回目の割り込みで何も出なかった:\n%s", logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case code := <-exited:
		t.Fatalf("1回目の割り込みで終わってしまった: exit code %d", code)
	case <-time.After(100 * time.Millisecond):
	}

	first := logs.String()
	if !strings.Contains(first, "もう一度 Ctrl+C") {
		t.Fatalf("2回目で即座に終わることが画面に出ていない:\n%s", first)
	}
	if !strings.Contains(first, "max_wait") {
		t.Fatalf("待たせる時間が画面に出ていない:\n%s", first)
	}

	// 2回目。**後始末を待たずに終わる。**
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("2回目の SIGINT を送れません: %v", err)
	}
	select {
	case code := <-exited:
		if code != daemon.ExitInterrupted {
			t.Fatalf("終了コードが %d ではない: %d", daemon.ExitInterrupted, code)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("2回目の割り込みが効かなかった:\n%s", logs.String())
	}
}

// TestWatchInterrupt_stopを2回呼んでも落ちない は、`defer stop()` を重ねても
// 安全であることを確かめる（signal の登録を外す関数は何回呼んでも安全と約束している）。
func TestWatchInterrupt_stopを2回呼んでも落ちない(t *testing.T) {
	stop := daemon.WatchInterrupt(slog.New(slog.NewTextHandler(&syncBuffer{}, nil)), func(int) {})
	stop()
	stop()
}

// TestRun_起動時検査が期限内に終わらなければ起動を止める は、外向きの呼び出しの期限を確かめる。
//
// 目的: 起動時検査（設計 3-6）は `gh` を起動し、herdr の socket を叩き、GitHub の GraphQL を
// 読む。**期限が1つも無いと、応答を返さない相手に当たった時点で起動が無言で止まる**
// （復元にも巡回にも進まない）。
//
// 与える情報: 応答を返さないテスト用GraphQL mock（127.0.0.1）と、200ms の起動時検査の期限。
// gh と herdr は答える（止まるのはボードの読み取りだけ）。
//
// 成功条件: 30 秒以内に起動の段のエラーで返り、文言が起動時検査の失敗を指すこと。
func TestRun_起動時検査が期限内に終わらなければ起動を止める(t *testing.T) {
	root := wiringRoot(t)
	runtimeDir := filepath.Join(root, "rt")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("実行時ディレクトリを作れません: %v", err)
	}
	// **テスト用gh / ghq mock を PATH の先頭に置く。**本物の認証情報を読ませない。
	writeFakeGH(t, binDir, root)
	// **テスト用herdr mock は ping に答える。**止まるのはボードの読み取りだけにする。
	newFakeHerdr(t, root, &timeline{})

	// **応答を1バイトも返さないサーバ。**
	blocked := make(chan struct{})
	github := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-blocked:
		case <-r.Context().Done():
		}
	}))
	// **後始末は「待たせるのをやめる → サーバを閉じる」の順に走らせる**（t.Cleanup は
	// 後に登録したものから走る）。逆順だと、応答を待たせたまま Close が処理中の
	// リクエストの終わりを待ち、テストが終われなくなる。
	t.Cleanup(github.Close)
	t.Cleanup(func() { close(blocked) })

	path := writeWiringWorkflow(t, root,
		"    token_source: env\n    token_env: CONTINUO_TEST_TOKEN\n", "")

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(daemon.EnvRuntimeDir, runtimeDir)
	t.Setenv(daemon.EnvGraphQLEndpoint, github.URL)
	t.Setenv("CONTINUO_TEST_TOKEN", "dummy-token-for-the-fake-server")

	start := time.Now()
	err := daemon.Run(context.Background(), daemon.Options{
		ConfigPath:          path,
		Logger:              slog.New(slog.DiscardHandler),
		StartupCheckTimeout: 200 * time.Millisecond,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("ボードが応答しないのに起動できてしまった")
	}
	if elapsed > 30*time.Second {
		t.Fatalf("期限を過ぎても待ち続けた: %v", elapsed)
	}
	if !errors.Is(err, daemon.ErrStartup) {
		t.Fatalf("起動の段の失敗として印が付いていない: %v", err)
	}
	if !strings.Contains(err.Error(), "起動時の検査に落ちました") {
		t.Fatalf("起動時の検査で止まったことが文言に出ていない: %v", err)
	}
}

// TestHookCLI_socketとpending_dirが相対パスなら受け付けない は、
// hook の逃がし先が worktree の中に掘られるのを防ぐ。
//
// 目的: hook の cwd は worktree である（設計 1-5）。相対パスを受けると、逃がし先が
// worktree の中に作られ、continuo は実行時ディレクトリの下しか走査しないので
// **その hook は永久に読まれない**（worktree も汚れる）。受け口の側
// （`hookserver.New`）は同じ理由で既に絶対パスを要求している。
//
// 与える情報: `--socket` か `--pending-dir` の片方だけを相対パスにした `continuo hook`。
//
// 成功条件: 終了コードが 1 で、文言が絶対パスを求めていること。**2 を返さないこと**
// （Claude Code は hook の 2 を「その操作を止めろ」と解釈するため）。
// 相対パスのディレクトリを1つも作らないこと。
func TestHookCLI_socketとpending_dirが相対パスなら受け付けない(t *testing.T) {
	root := wiringRoot(t)
	bin := buildBinary(t, root)

	cases := []struct {
		name       string
		socket     string
		pendingDir string
	}{
		{
			name:       "socket が相対パス",
			socket:     "hooks.sock",
			pendingDir: filepath.Join(root, "pending"),
		},
		{
			name:       "pending_dir が相対パス",
			socket:     filepath.Join(root, "hooks.sock"),
			pendingDir: "pending",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bin, "hook", "--socket="+tc.socket, "--pending-dir="+tc.pendingDir)
			cmd.Dir = root
			cmd.Stdin = strings.NewReader(`{"hook_event_name":"Stop","session_id":"s","cwd":"` + root + `"}` + "\n")
			out, err := cmd.CombinedOutput()

			code := 0
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatalf("continuo hook を実行できません: %v\n%s", err, out)
				}
				code = exitErr.ExitCode()
			}
			if code != 1 {
				t.Fatalf("終了コードが 1 ではない: got %d\n%s", code, out)
			}
			if !strings.Contains(string(out), "絶対パス") {
				t.Fatalf("絶対パスを求める文言が出ていない:\n%s", out)
			}
			if _, statErr := os.Stat(filepath.Join(root, "pending")); statErr == nil && !filepath.IsAbs(tc.pendingDir) {
				t.Fatal("相対パスの逃がし先を掘ってしまった（worktree の中に作られる）")
			}
		})
	}
}

// wiringShortHome は、ホームディレクトリの代わりに使う短い一時ディレクトリを作り、
// `HOME` をそこへ向ける。
//
// **短くしておく。**hook を受ける socket のパスは103バイトに収まらないと起動できない
// （設計 3-23）。**macOS の `TMPDIR` はそれだけで66文字前後ある。**
//
// t: 呼び出し元のテスト。
// 戻り値: 実体のパス（symlink を解決済み）。
func wiringShortHome(t *testing.T) string {
	t.Helper()

	for _, base := range []string{"/tmp", ""} {
		dir, err := os.MkdirTemp(base, "ch")
		if err != nil {
			continue
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			resolved = dir
		}
		t.Setenv("HOME", resolved)
		return resolved
	}
	t.Fatal("一時ディレクトリを作れません")
	return ""
}

// TestRun_idを付けるとロックが名前ごとに分かれる は、`--id` の結線を固定する。
//
// 目的: 設計 3-17b。**`--id` が分けるのはロックだけである。**
// **CLI が解決した Layout を、常駐の側がそのまま使っていることを結線で確かめる。**
// worktree の置き場所・hook の socket・branch 名は、検証用の `WORKFLOW.md` で
// 書き換えるものなので、ここでは分かれない。
//
// 与える情報: `--id e2e` と、`~/.continuo/id/e2e/continuo.lock` を先に握った別のプロセス。
// 成功条件: 起動の段のエラーで、**その名前ごとのロックのパスを指して**二重起動と言うこと。
// **worktree の置き場所は `workspace.root` のままであること。**
func TestRun_idを付けるとロックが名前ごとに分かれる(t *testing.T) {
	root := wiringRoot(t)
	home := wiringShortHome(t)
	runtimeDir := filepath.Join(root, "rt")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("実行時ディレクトリを作れません: %v", err)
	}
	path := writeWiringWorkflow(t, root, "", "")

	t.Setenv(daemon.EnvRuntimeDir, runtimeDir)
	t.Setenv(daemon.EnvGraphQLEndpoint, "")

	// **1本目の代わりに、名前ごとのロックを先に握る。**
	idLock := filepath.Join(home, ".continuo", "id", "e2e", "continuo.lock")
	if err := os.MkdirAll(filepath.Dir(idLock), 0o700); err != nil {
		t.Fatalf("名前ごとのロックの置き場所を作れません: %v", err)
	}
	held, err := lock.Acquire(idLock)
	if err != nil {
		t.Fatalf("名前ごとのロックを先に握れません: %v", err)
	}
	t.Cleanup(func() { _ = held.Release() })

	inst, err := instance.Resolve("e2e")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}

	logs := &syncBuffer{}
	err = daemon.Run(context.Background(), daemon.Options{
		ConfigPath: path,
		Logger:     slog.New(slog.NewTextHandler(logs, nil)),
		Instance:   &inst,
	})

	if err == nil {
		t.Fatal("同じ名前のロックを握られているのに起動できてしまった")
	}
	if !errors.Is(err, daemon.ErrStartup) {
		t.Fatalf("起動の段の失敗として印が付いていない: %v", err)
	}
	if !strings.Contains(err.Error(), idLock) {
		t.Fatalf("名前ごとのロックを見ていない: %v", err)
	}
	if !strings.Contains(err.Error(), "二重起動") {
		t.Fatalf("二重起動として報告していない: %v", err)
	}

	// **worktree の置き場所は動かさない。**`--id` から導くと、設定に書いた値と
	// 実際に使う値が食い違う（設計 3-17b）。
	notWanted := filepath.Join(root, "wt", "e2e")
	if strings.Contains(logs.String(), notWanted) {
		t.Fatalf("--id から worktree の置き場所を導いている（%q が記録に出ている）:\n%s", notWanted, logs.String())
	}
}

// TestResolveLockFilePath_idとlock_fileと既定の優先順位を固定する は、
// ロックの置き場所の決め方を固定する（設計 3-17）。
//
// 目的: **hook の socket の置き場所から導かないこと。**socket の場所は設定と環境変数で
// 動くので、そこから導くと **socket を分けただけで2本目が黙って起動できてしまう。**
// **`runtime.lock_file` は読み続けること。**このキーは `continuo init` の雛形に入っており、
// 書いている人が既に居る。**黙って無視すると、その人のロックの場所が予告なく変わる。**
// **`--id` はその上に立つこと**（設定より、その場で打ったフラグを優先する）。
//
// 与える情報: `--id` の有無と、`runtime.lock_file` の有無の組み合わせ。
// 成功条件: `--id` ＞ `runtime.lock_file` ＞ `~/.continuo/continuo.lock` の順で決まること。
func TestResolveLockFilePath_idとlock_fileと既定の優先順位を固定する(t *testing.T) {
	home := wiringShortHome(t)
	written := filepath.Join(home, "somewhere-else.lock")

	defaultLayout, err := instance.Resolve("")
	if err != nil {
		t.Fatalf("既定の置き場所を決められない: %v", err)
	}
	namedLayout, err := instance.Resolve("e2e")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}

	cases := []struct {
		name     string
		layout   instance.Layout
		lockFile *string
		want     string
	}{
		{
			name:   "どちらも無ければ既定の1本",
			layout: defaultLayout,
			want:   filepath.Join(home, instance.DirName, instance.LockFileName),
		},
		{
			name:     "lock_fileを書いてあればそれを使う",
			layout:   defaultLayout,
			lockFile: &written,
			want:     written,
		},
		{
			name:   "idを付ければ名前ごとに分かれる",
			layout: namedLayout,
			want:   filepath.Join(home, instance.DirName, instance.IDDirName, "e2e", instance.LockFileName),
		},
		{
			name:     "idはlock_fileより強い",
			layout:   namedLayout,
			lockFile: &written,
			want:     filepath.Join(home, instance.DirName, instance.IDDirName, "e2e", instance.LockFileName),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := *config.DefaultConfig()
			cfg.Runtime.LockFile = c.lockFile

			if got := daemon.ResolveLockFilePath(cfg, c.layout); got != c.want {
				t.Fatalf("ロックの置き場所が違う: got %q, want %q", got, c.want)
			}
		})
	}
}

// TestEnsureLockFileDir_人が書いたパスの親は作らない は、
// 打ち間違いを黙って通さないことを固定する（設計 3-17）。
//
// 目的: **`runtime.lock_file` の親ディレクトリを作ってはならない。**作ると、
// パスを打ち間違えたときに黙って別の場所へロックを置き、**二重起動を検出できなくなる。**
// **continuo が場所を決めたときは作る**（`~/.continuo/id/<名前>` は誰も作らない）。
//
// 与える情報: 親ディレクトリが無い `runtime.lock_file` と、`--id` の置き場所。
// 成功条件: 前者では親が作られず、後者では作られること。
func TestEnsureLockFileDir_人が書いたパスの親は作らない(t *testing.T) {
	home := wiringShortHome(t)

	defaultLayout, err := instance.Resolve("")
	if err != nil {
		t.Fatalf("既定の置き場所を決められない: %v", err)
	}
	written := filepath.Join(home, "no-such-dir", "continuo.lock")
	if err := daemon.EnsureLockFileDir(defaultLayout, written); err != nil {
		t.Fatalf("人が書いたパスでエラーになった: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(written)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("人が書いたパスの親を作っている: %v", err)
	}

	namedLayout, err := instance.Resolve("e2e")
	if err != nil {
		t.Fatalf("--id から置き場所を決められない: %v", err)
	}
	if err := daemon.EnsureLockFileDir(namedLayout, namedLayout.LockPath()); err != nil {
		t.Fatalf("--id の置き場所を用意できない: %v", err)
	}
	if info, err := os.Stat(filepath.Dir(namedLayout.LockPath())); err != nil || !info.IsDir() {
		t.Fatalf("--id の置き場所を用意していない: %v", err)
	}
}

// lineContaining は、ログ全文から want を含む最初の1行を返す。
//
// **行で切り出す。**全文に対して `Contains` を当てると、別の行に出ている文字列を
// 拾ってしまい、「どの行の値か」を確かめられない。
//
// t: 呼び出し元のテスト。
// logged: ログ全文。
// want: 目印にする文字列。
// 戻り値: want を含む最初の行。見つからなければテストを落とす。
func lineContaining(t *testing.T, logged, want string) string {
	t.Helper()
	for _, line := range strings.Split(logged, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}
	t.Fatalf("%q を含む行がログに無い\n%s", want, logged)
	return ""
}
