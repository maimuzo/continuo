// Package config_test のうち、このファイルは front matter の値の検査（internal/config の
// validate）が「起動を止めるべき設定」を確実に止めることを検証する。
//
// ここで扱う設定はどれも YAML としては正しく、型も合っている。検査が無ければ起動を通り、
// 走り出してから初めておかしくなる種類のものである（無停止のループ・状態の往復・
// 枠の判定の無効化）。無人運用では走り出したあとの異常に誰も気づけないので、
// 設定を読んだ時点で名指しして落ちることをテストで固定する。
package config_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// trackerFrontMatter は tracker ブロックへ追加の行を差し込んだ front matter を組み立てる。
// validFrontMatter に "tracker:" を足すと YAML のキー重複になるため、状態の集合を
// 上書きしたいテストはこちらを使う。
//
// extra: tracker ブロックの直下に足す行（インデント2文字から始め、末尾は "\n" で終えること）。
// 戻り値: front matter の YAML 本体。
func trackerFrontMatter(extra string) string {
	return "tracker:\n" +
		"  provider:\n" +
		"    owner: acme\n" +
		"    project_number: 1\n" +
		"    status_field: Status\n" +
		extra
}

// assertLoadFailsWith は Load が失敗し、そのエラー文に wantKey が含まれることを確かめる。
//
// t: テストコンテキスト。
// frontMatter: 読み込ませる front matter の YAML 本体。
// wantKey: エラー文に必ず現れるべき設定キーの名前。
func assertLoadFailsWith(t *testing.T, frontMatter, wantKey string) {
	t.Helper()
	path := writeWorkflow(t, frontMatter, "")

	_, err := config.Load(path)
	if err == nil {
		t.Fatalf("起動を止めるべき設定なのにエラーが返らなかった（期待したキー: %s）", wantKey)
	}
	if !strings.Contains(err.Error(), wantKey) {
		t.Fatalf("エラー文が設定キーを名指ししていない: got %v, 期待したキー %s", err, wantKey)
	}
}

// 目的: claude.poll_wait_ms に 0 を書くと起動が止まることを確認する。
// 0 だと turn の待ち受けが待ち時間ゼロになり、herdr の socket を無停止で叩き続ける
// ループになる（それが turn_timeout_ms＝既定1時間続く）。
// 与える情報: claude.poll_wait_ms に 0 を書いた front matter。
// 成功条件: config.Load がエラーを返し、その文に "claude.poll_wait_ms" が含まれること。
func TestLoad_poll_wait_msが0だとビジーループになるので落ちる(t *testing.T) {
	front := validFrontMatter + "claude:\n  poll_wait_ms: 0\n"
	assertLoadFailsWith(t, front, "claude.poll_wait_ms")
}

// 目的: 時間を表す設定値が 0 以下のとき、どのキーであっても起動が止まることを確認する。
// 与える情報: 時間を表すキーひとつずつに 0 または負の値を書いた front matter。
// 成功条件: すべての場合で config.Load がエラーを返し、その文に対象のキー名が含まれること。
func TestLoad_時間の設定値が0以下ならキーを名指しして落ちる(t *testing.T) {
	cases := []struct {
		name  string
		front string
		key   string
	}{
		{"claude.settle_msが0", validFrontMatter + "claude:\n  settle_ms: 0\n", "claude.settle_ms"},
		{"claude.turn_timeout_msが0", validFrontMatter + "claude:\n  turn_timeout_ms: 0\n", "claude.turn_timeout_ms"},
		{"claude.read_timeout_msが負", validFrontMatter + "claude:\n  read_timeout_ms: -5\n", "claude.read_timeout_ms"},
		{"claude.startup_timeout_msが0", validFrontMatter + "claude:\n  startup_timeout_ms: 0\n", "claude.startup_timeout_ms"},
		{"agent.max_retry_backoff_msが負", validFrontMatter + "agent:\n  max_retry_backoff_ms: -1\n", "agent.max_retry_backoff_ms"},
		{"rate_limit.poll_interval_msが負", validFrontMatter + "rate_limit:\n  poll_interval_ms: -1\n", "rate_limit.poll_interval_ms"},
		{"workspace_hooks.timeout_msが負", validFrontMatter + "workspace_hooks:\n  timeout_ms: -1\n", "workspace_hooks.timeout_ms"},
		{"tracker.write_interval_msが負", trackerFrontMatter("  write_interval_ms: -1\n"), "tracker.write_interval_ms"},
		{"tracker.verify_states_everyが負", trackerFrontMatter("  verify_states_every: -1\n"), "tracker.verify_states_every"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertLoadFailsWith(t, c.front, c.key)
		})
	}
}

// 目的: 「0 に意味がある」設定値は 0 でも起動が通ることを確認する。
// claude.stall_timeout_ms は「0 以下で無効」と設計 3-21 が定めており、
// tracker.write_interval_ms の 0 は「間隔をあけない」、tracker.verify_states_every の 0 は
// 「起動時だけ照合する」である。上の一律の検査に巻き込んで落としてはならない。
// 与える情報: この3つのキーに 0 を書いた front matter。
// 成功条件: config.Load が成功し、値が 0 のまま保たれていること。
func TestLoad_0に意味がある設定値は0でも通る(t *testing.T) {
	front := trackerFrontMatter("  write_interval_ms: 0\n  verify_states_every: 0\n") +
		"claude:\n  stall_timeout_ms: 0\n"
	path := writeWorkflow(t, front, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("0 に意味がある設定値なのに起動が止まった: %v", err)
	}
	if loaded.Config.Claude.StallTimeoutMs != 0 {
		t.Errorf("claude.stall_timeout_ms が 0 のまま保たれていない: got %d", loaded.Config.Claude.StallTimeoutMs)
	}
	if loaded.Config.Tracker.WriteIntervalMs != 0 {
		t.Errorf("tracker.write_interval_ms が 0 のまま保たれていない: got %d", loaded.Config.Tracker.WriteIntervalMs)
	}
	if loaded.Config.Tracker.VerifyStatesEvery != 0 {
		t.Errorf("tracker.verify_states_every が 0 のまま保たれていない: got %d", loaded.Config.Tracker.VerifyStatesEvery)
	}
}

// 目的: 待ちの大小関係が逆転した設定で起動が止まることを確認する。
// 1回の待ち（poll_wait_ms）が turn 全体の上限（turn_timeout_ms）より長いと上限が効かず、
// turn の終わりを確かめる猶予（settle_ms）が1回の待ちより長いと猶予が待ちに収まらない。
// 与える情報: poll_wait_ms > turn_timeout_ms、settle_ms > poll_wait_ms の2つの front matter。
// 成功条件: どちらも config.Load がエラーを返し、その文に対象のキー名が含まれること。
func TestLoad_待ちの大小関係が逆転していると落ちる(t *testing.T) {
	t.Run("poll_wait_msがturn_timeout_msより長い", func(t *testing.T) {
		front := validFrontMatter + "claude:\n  poll_wait_ms: 60000\n  turn_timeout_ms: 30000\n"
		assertLoadFailsWith(t, front, "claude.poll_wait_ms")
	})
	t.Run("settle_msがpoll_wait_msより長い", func(t *testing.T) {
		front := validFrontMatter + "claude:\n  settle_ms: 40000\n  poll_wait_ms: 30000\n"
		assertLoadFailsWith(t, front, "claude.settle_ms")
	})
}

// 目的: claude.kind を空文字にすると起動が止まることを確認する。
// 空だと herdr へ渡す agent の種別が決まらない。
// 与える情報: claude.kind に空文字を書いた front matter。
// 成功条件: config.Load がエラーを返し、その文に "claude.kind" が含まれること。
func TestLoad_claude_kindが空だと落ちる(t *testing.T) {
	front := validFrontMatter + "claude:\n  kind: \"\"\n"
	assertLoadFailsWith(t, front, "claude.kind")
}

// 目的: 状態の集合が重なっている設定で起動が止まることを確認する（設計 3-9 / 3-10 / 4-1）。
// active と terminal が重なると、完了として片付けた issue を次の巡回で作業中として拾い直す。
// failure_state が active に入ると、打ち切った issue が永久に再 dispatch される。
// running_state が terminal に入ると、dispatch した直後に完了扱いになる。
// cleanup.on_states に作業中の状態を書くと、走っている worktree を片付けてしまう。
// 与える情報: それぞれの重なりを1つずつ作った front matter。
// 成功条件: すべての場合で config.Load がエラーを返し、その文に対象のキー名が含まれること。
func TestLoad_状態の集合が重なっていると落ちる(t *testing.T) {
	cases := []struct {
		name  string
		front string
		key   string
	}{
		{
			name: "active_statesとterminal_statesが重なる",
			front: trackerFrontMatter(
				"  active_states: [\"Ready\", \"In Progress\", \"Done\"]\n" +
					"  terminal_states: [\"Done\"]\n"),
			key: "tracker.active_states",
		},
		{
			name: "failure_stateがactive_statesに入る",
			front: trackerFrontMatter(
				"  active_states: [\"Ready\", \"In Progress\", \"Blocked\"]\n" +
					"  failure_state: Blocked\n"),
			key: "tracker.failure_state",
		},
		{
			// running_state（既定は In Progress）は active_states に含まれることを既に
			// 要求しているので、これを terminal_states に書くと必ず「active と terminal が
			// 重なる」として落ちる。名指しされるキーは tracker.active_states になる。
			name: "running_stateをterminal_statesに書く",
			front: trackerFrontMatter(
				"  terminal_states: [\"Done\", \"In Progress\"]\n"),
			key: "tracker.active_states",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertLoadFailsWith(t, c.front, c.key)
		})
	}

	t.Run("cleanup.on_statesに作業中の状態を書く", func(t *testing.T) {
		front := validFrontMatter + "cleanup:\n  on_states: [\"In Progress\"]\n"
		assertLoadFailsWith(t, front, "cleanup.on_states")
	})
}

// 目的: rate_limit.token_source が env なのに token_env が空だと起動が止まることを確認する。
// 空のまま通すと、枠の取得が毎回 ErrNoCredentials になり、枠の判定が黙って無効化される。
// tracker.provider.token_env は同じ条件を検査しているので、片側だけ抜けている状態を防ぐ。
// 与える情報: rate_limit.token_source に env、token_env に空文字を書いた front matter。
// 成功条件: config.Load がエラーを返し、その文に "rate_limit.token_env" が含まれること。
func TestLoad_rate_limitのtoken_sourceがenvでtoken_envが空だと落ちる(t *testing.T) {
	front := validFrontMatter + "rate_limit:\n  token_source: env\n  token_env: \"\"\n"
	assertLoadFailsWith(t, front, "rate_limit.token_env")
}

// 目的: rate_limit.source に "none" を書いても起動が通ることを確認する（設計 3-27）。
// usage API がトークンを消費するかどうかを判別できていないため、"none" は必須の逃げ道である。
// 与える情報: rate_limit.source に none を書いた front matter。
// 成功条件: config.Load が成功し、値が "none" のまま読めること。
func TestLoad_rate_limitのsourceにnoneを書ける(t *testing.T) {
	front := validFrontMatter + "rate_limit:\n  source: none\n"
	path := writeWorkflow(t, front, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("rate_limit.source: none は受理されるべきなのに起動が止まった: %v", err)
	}
	if loaded.Config.RateLimit.Source != "none" {
		t.Fatalf("rate_limit.source が読めていない: got %q", loaded.Config.RateLimit.Source)
	}
}
