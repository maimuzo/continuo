// Package config_test のうち、このファイルは front matter の値の検査（internal/config の
// validate）が「起動を止めるべき設定」を確実に止めることを検証する。
//
// ここで扱う設定はどれも YAML としては正しく、型も合っている。検査が無ければ起動を通り、
// 走り出してから初めておかしくなる種類のものである（無停止のループ・状態の往復・
// 枠の判定の無効化）。無人運用では走り出したあとの異常に誰も気づけないので、
// 設定を読んだ時点で名指しして落ちることをテストで固定する。
package config_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
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
// ループになる。
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
		{"herdr.read_timeout_msが負", validFrontMatter + "herdr:\n  read_timeout_ms: -5\n", "herdr.read_timeout_ms"},
		{"herdr.startup_timeout_msが0", validFrontMatter + "herdr:\n  startup_timeout_ms: 0\n", "herdr.startup_timeout_ms"},
		{"agent.max_retry_backoff_msが負", validFrontMatter + "agent:\n  max_retry_backoff_ms: -1\n", "agent.max_retry_backoff_ms"},
		{"rate_limit.poll_interval_msが負", validFrontMatter + "rate_limit:\n  poll_interval_ms: -1\n", "rate_limit.poll_interval_ms"},
		{"workspace_hooks.timeout_msが負", validFrontMatter + "workspace_hooks:\n  timeout_ms: -1\n", "workspace_hooks.timeout_ms"},
		{"tracker.verify_states_everyが負", trackerFrontMatter("  verify_states_every: -1\n"), "tracker.verify_states_every"},
		{"tracker.unknown_state_grace_msが負", trackerFrontMatter("  unknown_state_grace_ms: -1\n"), "tracker.unknown_state_grace_ms"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertLoadFailsWith(t, c.front, c.key)
		})
	}
}

// 目的: 「0 に意味がある」設定値は 0 でも起動が通ることを確認する。
// claude.turn_timeout_ms は「0 以下なら打ち切りを行わない」と設計 3-21 が定めており
// （`SPEC.md` 8.4 の "If stall_timeout_ms <= 0, skip stall detection entirely"）、
// tracker.verify_states_every の 0 は「起動時だけ照合する」である。
// 上の一律の検査に巻き込んで落としてはならない。
// **poll_wait_ms との大小関係の検査にも巻き込んではならない**（0 は「上限」ではない）。
// 与える情報: この2つのキーに 0 を書いた front matter。
// 成功条件: config.Load が成功し、値が 0 のまま保たれていること。
func TestLoad_0に意味がある設定値は0でも通る(t *testing.T) {
	front := trackerFrontMatter("  verify_states_every: 0\n  unknown_state_grace_ms: 0\n") +
		"claude:\n  turn_timeout_ms: 0\n"
	path := writeWorkflow(t, front, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("0 に意味がある設定値なのに起動が止まった: %v", err)
	}
	if loaded.Config.Claude.TurnTimeoutMs != 0 {
		t.Errorf("claude.turn_timeout_ms が 0 のまま保たれていない: got %d", loaded.Config.Claude.TurnTimeoutMs)
	}
	if loaded.Config.Tracker.VerifyStatesEvery != 0 {
		t.Errorf("tracker.verify_states_every が 0 のまま保たれていない: got %d", loaded.Config.Tracker.VerifyStatesEvery)
	}
	// tracker.unknown_state_grace_ms の 0 は「猶予を置かずにその巡回で止める」である（3-50）。
	if loaded.Config.Tracker.UnknownStateGraceMs != 0 {
		t.Errorf("tracker.unknown_state_grace_ms が 0 のまま保たれていない: got %d", loaded.Config.Tracker.UnknownStateGraceMs)
	}
}

// 目的: agent.max_concurrent_agents_by_state に 0 以下を書くと起動が止まることを確認する。
//
// 0 を書くと internal/orchestrator の hasFreeSlot が `inRunningState < limit` で常に偽を返し、
// ボード全体の dispatch が永久に止まる。**ログにも何も出ないので、無人運用では
// 止まっていることに誰も気づけない。**`SPEC.md` 5.3.5 は非正の値を黙って無視すると
// 定めるが、continuo は起動時に名指しで落とす。
//
// 与える情報: `In Progress: 0` と `In Progress: -1` を書いた front matter。
// 成功条件: どちらも config.Load がエラーを返し、その文にキー名と Status 名が含まれること。
func TestLoad_状態ごとの上限が0以下だとdispatchが永久に止まるので落ちる(t *testing.T) {
	for _, limit := range []string{"0", "-1"} {
		t.Run("上限が"+limit, func(t *testing.T) {
			front := validFrontMatter +
				"agent:\n  max_concurrent_agents_by_state:\n    \"In Progress\": " + limit + "\n"
			assertLoadFailsWith(t, front, "agent.max_concurrent_agents_by_state.In Progress")
		})
	}
}

// 目的: agent.max_concurrent_agents_by_state に 1 以上を書けば起動が通ることを確認する。
// 上の検査が正しい値まで巻き込んで落としていないことを固定する。
// 与える情報: `In Progress: 1` を書いた front matter。
// 成功条件: config.Load が成功し、値が 1 のまま読めること。
func TestLoad_状態ごとの上限が1以上なら通る(t *testing.T) {
	front := validFrontMatter + "agent:\n  max_concurrent_agents_by_state:\n    \"In Progress\": 1\n"
	path := writeWorkflow(t, front, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("正しい上限なのに起動が止まった: %v", err)
	}
	if got := loaded.Config.Agent.MaxConcurrentAgentsByState["In Progress"]; got != 1 {
		t.Errorf("agent.max_concurrent_agents_by_state.In Progress が読めていない: got %d, want 1", got)
	}
}

// 目的: claude.wait_until に herdr が知らない状態名を書くと起動が止まることを確認する。
//
// internal/orchestrator の waitUntilStatuses は文字列をそのまま herdr へ渡す。
// 綴りを間違えても起動は通ってしまい、turn の終わりを拾えないまま時間切れまで待つ。
//
// 与える情報: "idle" を "idel" と綴り間違えた front matter と、同じ状態を2回書いた front matter。
// 成功条件: どちらも config.Load がエラーを返し、その文に claude.wait_until が含まれること。
func TestLoad_wait_untilの綴りが違うと落ちる(t *testing.T) {
	t.Run("herdrが知らない状態名", func(t *testing.T) {
		front := validFrontMatter + "claude:\n  wait_until: [\"idel\", \"done\"]\n"
		assertLoadFailsWith(t, front, "claude.wait_until[0]")
	})
	t.Run("同じ状態を2回書いた", func(t *testing.T) {
		front := validFrontMatter + "claude:\n  wait_until: [\"idle\", \"idle\"]\n"
		assertLoadFailsWith(t, front, "claude.wait_until[1]")
	})
}

// 目的: claude.wait_until に herdr の AgentStatus の値だけを書けば起動が通ることを確認する。
// 上の検査が正しい綴りまで巻き込んで落としていないことを、herdr の定数そのものを使って固定する。
// 与える情報: herdr.AgentStatuses() の全件を書いた front matter。
// 成功条件: config.Load が成功し、書いた並びがそのまま読めること。
func TestLoad_wait_untilにherdrの状態名を全部書いても通る(t *testing.T) {
	all := herdr.AgentStatuses()
	quoted := make([]string, 0, len(all))
	want := make([]string, 0, len(all))
	for _, s := range all {
		quoted = append(quoted, "\""+string(s)+"\"")
		want = append(want, string(s))
	}
	front := validFrontMatter + "claude:\n  wait_until: [" + strings.Join(quoted, ", ") + "]\n"
	path := writeWorkflow(t, front, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("herdr が受け付ける状態名だけを書いたのに起動が止まった: %v", err)
	}
	if !slices.Equal(loaded.Config.Claude.WaitUntil, want) {
		t.Errorf("claude.wait_until が読めていない: got %v, want %v", loaded.Config.Claude.WaitUntil, want)
	}
}

// 目的: tracker.comments のマーカーが tracker の直下から読めることを確認する。
// provider（GitHub 固有の箱）の下ではなく tracker の直下に置いたことを固定する。
// 与える情報: tracker.comments.marker / self_marker を書いた front matter。
// 成功条件: config.Load が成功し、書いた値がそのまま読めること。
func TestLoad_マーカーはtrackerの直下から読む(t *testing.T) {
	front := trackerFrontMatter("  comments:\n" +
		"    marker: \"<!-- x:agent -->\"\n" +
		"    self_marker: \"<!-- x:self -->\"\n")
	path := writeWorkflow(t, front, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("tracker.comments を読み込めませんでした: %v", err)
	}
	if got := loaded.Config.Tracker.Comments.Marker; got != "<!-- x:agent -->" {
		t.Errorf("tracker.comments.marker が読めていない: got %q", got)
	}
	if got := loaded.Config.Tracker.Comments.SelfMarker; got != "<!-- x:self -->" {
		t.Errorf("tracker.comments.self_marker が読めていない: got %q", got)
	}
}

// 目的: 消した設定キーを書いたままの WORKFLOW.md が、起動時に未知のキーとして落ちることを確認する。
//
// front matter は yaml.Strict() で読んでいる（設計 8-1）。**消したキーを黙って読み飛ばすと、
// 「書いたつもりの設定が効いていない」ことに無人運用では誰も気づけない。**
//
// 与える情報: 消した3つのキーと、名前を変える前の agent.max_turns を1つずつ書いた front matter
// （tracker.provider.comments.fetch は下の専用のテストで見る）。
// 成功条件: すべての場合で config.Load がエラーを返し、その文にキー名が含まれること。
func TestLoad_消したキーを書いたままだと落ちる(t *testing.T) {
	cases := []struct {
		name  string
		front string
		key   string
	}{
		{"tracker.write_interval_ms", trackerFrontMatter("  write_interval_ms: 1000\n"), "write_interval_ms"},
		{"workspace.layout", validFrontMatter + "workspace:\n  layout: gwq\n", "layout"},
		{"claude.hook_bridge.mode", validFrontMatter + "claude:\n  hook_bridge:\n    mode: settings_flag\n", "mode"},
		{"agent.max_turns", validFrontMatter + "agent:\n  max_turns: 20\n", "max_turns"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertLoadFailsWith(t, c.front, c.key)
		})
	}
}

// 目的: tracker.provider.comments.fetch を書いたままだと起動が止まることを確認する。
//
// **`fetch: false` にすると、成功した run も含めて全件が failure_state へ落ちる。**
// コメントを取らない → エージェントのコメントが見つからない、という経路になる。
// キーごと消したので、書いてあれば未知のキーとして落ちる。
//
// 与える情報: tracker.provider.comments.fetch を書いた front matter。
// 成功条件: config.Load がエラーを返し、その文に fetch が含まれること。
func TestLoad_comments_fetchを書いたままだと落ちる(t *testing.T) {
	front := "tracker:\n" +
		"  provider:\n" +
		"    owner: acme\n" +
		"    project_number: 1\n" +
		"    status_field: Status\n" +
		"    comments:\n" +
		"      fetch: false\n"
	assertLoadFailsWith(t, front, "fetch")
}

// 目的: 待ちの大小関係が逆転した設定で起動が止まることを確認する。
// 1回の待ち（poll_wait_ms）が「画面が止まったとみなす時間」（turn_timeout_ms）より長いと
// 打ち切りの判定より待ちのほうが粗くなり、turn の終わりを確かめる猶予（settle_ms）が
// 1回の待ちより長いと猶予が待ちに収まらない。
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

// 目的: `cleanup.on_states` に `tracker.terminal_states` の外の値を書いても、
// **起動が止まらない**ことを固定する（設計 3-9。issue #35）。
//
// **`tracker.active_states` との重なりとは扱いが違う。**あちらは走っている worktree を
// 消すので `config.Load` が止める。こちらは**片付けの筋が通らないだけ**で壊れるものが無く、
// **止めると、いま動いている人の continuo が版を上げた瞬間に起動しなくなる。**
// 報告された `WORKFLOW.md`（`terminal_states: ["AI Done"]` と `on_states: ["Done"]`）が、
// まさにこの形である。
//
// 与える情報: `terminal_states` に無い値を `cleanup.on_states` に書いた front matter。
// 成功条件: `config.Load` が成功し、`CleanupStatesOutsideTerminal` が食い違った値を返すこと。
func TestLoad_片付ける状態が終わったとみなす状態の外にあっても起動は止まらない(t *testing.T) {
	front := trackerFrontMatter("  terminal_states: [\"AI Done\"]\n") +
		"cleanup:\n  on_states: [\"Done\"]\n"
	path := writeWorkflow(t, front, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("起動を止めない設定なのにエラーが返った: %v", err)
	}

	got := config.CleanupStatesOutsideTerminal(loaded.Config)
	if !slices.Equal(got, []string{"Done"}) {
		t.Fatalf("食い違った値が取れない: %v（期待: [Done]）", got)
	}
}

// 目的: `CleanupStatesOutsideTerminal` が、どの設定で何を返すかを固定する
// （設計 3-9。issue #35）。**起動時の警告と `continuo doctor` は、どちらもこの関数を呼ぶ。**
//
// 与える情報: 噛み合っている場合・片付けを行わない場合・綴りだけが違う場合・
// 一部だけ外れている場合の4通り。
// 成功条件: 食い違った値だけが、書いてある綴り・書いてある順で返ること。
func TestCleanupStatesOutsideTerminal_食い違った値だけを書いてある綴りで返す(t *testing.T) {
	cases := []struct {
		name     string
		enabled  bool
		terminal []string
		onStates []string
		want     []string
	}{
		{
			name:     "全部入っていれば何も返さない",
			enabled:  true,
			terminal: []string{"AI Done", "Done"},
			onStates: []string{"AI Done", "Done"},
			want:     nil,
		},
		{
			// **片付けそのものが走らないので、噛み合っていなくても何も起きない。**
			name:     "片付けを行わない設定なら何も返さない",
			enabled:  false,
			terminal: []string{"AI Done"},
			onStates: []string{"Done"},
			want:     nil,
		},
		{
			// **比べ方は containsStateFold に合わせる**（大文字小文字と前後の空白を無視する）。
			// ここだけ完全一致で比べると、実行時には同じ Status として扱われる値を
			// 「食い違っている」と報告することになる。
			name:     "大文字小文字と前後の空白だけが違えば同じ値とみなす",
			enabled:  true,
			terminal: []string{"Done"},
			onStates: []string{"  dONE  "},
			want:     nil,
		},
		{
			name:     "外れている値だけを書いてある綴りで返す",
			enabled:  true,
			terminal: []string{"AI Done"},
			onStates: []string{"AI Done", "Done", "Archived"},
			want:     []string{"Done", "Archived"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := *config.DefaultConfig()
			cfg.Cleanup.Enabled = c.enabled
			cfg.Tracker.TerminalStates = c.terminal
			cfg.Cleanup.OnStates = c.onStates

			if got := config.CleanupStatesOutsideTerminal(cfg); !slices.Equal(got, c.want) {
				t.Fatalf("戻り値が違う: %v（期待: %v）", got, c.want)
			}
		})
	}
}

// 目的: 状態の集合の重なりを、**大文字小文字を無視して**見ることを確認する
// （設計 3-13 / SPEC.md 11.3）。
// **照合する側（トラッカー・巡回・abandon）は全部そうしている。**ここだけ完全一致で
// 比べると、検証を通った設定が実行時には別の意味になる。とくに `failure_state` が
// 綴り違いで `active_states` に入ると、**打ち切った issue が永久に再 dispatch される。**
// 与える情報: `active_states` に `In Progress`、`failure_state` に `in progress` と
// 書いた front matter（完全一致では重ならない）。
// 成功条件: config.Load がエラーを返し、その文に "tracker.failure_state" が含まれること。
func TestLoad_状態の重なりは大文字小文字を無視して見る(t *testing.T) {
	front := trackerFrontMatter(
		"  active_states: [\"Ready\", \"In Progress\"]\n" +
			"  failure_state: in progress\n")
	assertLoadFailsWith(t, front, "tracker.failure_state")
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

// TestLoad_止めたときの案内どおりに直した設定は起動する は、設計 3-57b を固定する（issue #76）。
//
// 目的: **知らない Status で止めたときの案内は、貼ったらそのまま起動する形でなければならない。**
// issue #76 は「`tracker.active_states` へ書き足せ」という案内どおりに直したら
// `config.Load` に弾かれた、というものである。**同じ名前が
// `tracker.automated_state_rewrite` のキーと `cleanup.on_states` の両方にある設定でも同じことが起きる。**
// **どちらの検査も、その組み合わせ自体は弾かない**ので、その設定は起動できてしまう。
//
// 与える情報: `Archived` を対応表のキーと `cleanup.on_states` の両方に書いた front matter
// （出発点）と、案内が示す2つの直し方、そして案内が禁じている2つの直し方。
//
// 成功条件: 出発点と2つの直し方が `config.Load` を通り、禁じている2つが落ちて、
// エラー文が直す先のキーを名指しすること。
func TestLoad_止めたときの案内どおりに直した設定は起動する(t *testing.T) {
	// 出発点。**この組み合わせを弾く検査は無いので、そのまま起動できる。**
	t.Run("対応表と片付けの両方に名前がある設定はそのまま起動する", func(t *testing.T) {
		front := trackerFrontMatter(
			"  automated_state_rewrite:\n"+
				"    Archived: In Progress\n") +
			"cleanup:\n  on_states: [\"Archived\"]\n"
		if _, err := config.Load(writeWorkflow(t, front, "")); err != nil {
			t.Fatalf("起動できる設定のはずが落ちた（この道は到達不能ということになる）: %v", err)
		}
	})

	// 案内が出す直し方。**どちらもそのまま起動する。**
	ok := []struct {
		name  string
		front string
	}{
		{
			name: "対応表の行を消して terminal_states へ足す",
			front: trackerFrontMatter("  terminal_states: [\"Done\", \"Archived\"]\n") +
				"cleanup:\n  on_states: [\"Archived\"]\n",
		},
		{
			name: "対応表の行と cleanup.on_states の行を消して active_states へ足す",
			front: trackerFrontMatter(
				"  active_states: [\"Ready\", \"In Progress\", \"Archived\"]\n") +
				"cleanup:\n  on_states: [\"Done\"]\n",
		},
	}
	for _, c := range ok {
		t.Run(c.name, func(t *testing.T) {
			if _, err := config.Load(writeWorkflow(t, c.front, "")); err != nil {
				t.Fatalf("案内どおりに直した設定が起動しない（案内が誤っている）: %v", err)
			}
		})
	}

	// 案内が禁じている直し方。**落ちること自体は正しい。**案内がこれを勧めてはならない。
	ng := []struct {
		name  string
		front string
		key   string
	}{
		{
			name: "対応表の行だけ消して active_states へ足す",
			front: trackerFrontMatter(
				"  active_states: [\"Ready\", \"In Progress\", \"Archived\"]\n") +
				"cleanup:\n  on_states: [\"Archived\"]\n",
			key: "cleanup.on_states",
		},
		{
			name: "対応表の行を残したまま terminal_states へ足す",
			front: trackerFrontMatter(
				"  terminal_states: [\"Done\", \"Archived\"]\n"+
					"  automated_state_rewrite:\n"+
					"    Archived: In Progress\n") +
				"cleanup:\n  on_states: [\"Archived\"]\n",
			key: "tracker.automated_state_rewrite のキー",
		},
	}
	for _, c := range ng {
		t.Run(c.name, func(t *testing.T) {
			assertLoadFailsWith(t, c.front, c.key)
		})
	}
}
