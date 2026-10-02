// {"RUCM-CFG-SHA256": "aa215785e5c5834cfb0007c83a5c294e4263dcfae94a3476815e9b116fc1213e", "SOURCE": "docs/spec/usecases/particular_case/巡回が回っているあいだに常駐を止める.cfg.json"}
//
// **ユースケース記述「巡回が回っているあいだに常駐を止める」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package daemon_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// {"RUCM-PATH": "P001"}
//
// Test_巡回が回っているあいだに常駐を止める_P001_portを付けてSIGINTを受けたら段ごとに名乗って終わる は、
// 「Ctrl+C を押しても何も反応しない」を潰したことを、バイナリを起動して確かめる。
//
// 目的: 終了は3段の直列（ダッシュボード → hook の受け口 → turn ループ）で、
// 合計で 30 秒を超えうる。**入口の1行だけ出して黙り込むと、止まったのか固まったのかが
// 人間に区別できない。**段ごとに名乗り、待たせる理由と抜け道を必ず出す。
//
// **`--port` を付けた `SIGINT` の経路を通す。**ダッシュボードを開く経路は
// `SIGTERM` でしか通っていなかった。利用者が実際に押すのは Ctrl+C である。
//
// 与える情報: `--port=0` で開いたダッシュボードと、**応答を返し終えられない要求が1本**
// （後始末の1段目を必ず期限まで引き延ばす）。そこへ `SIGINT` を1回。
//
// 成功条件:
//   - 3段が順に名乗ること（1/3 → 2/3 → 3/3）
//   - 2回目の Ctrl+C で即座に終わることと、`kill -QUIT` の案内が出ること
//   - **ダッシュボードを叩き切ること**（応答の読み切りの期限 10 秒まで待たない）
//   - 終了コード 0 で終わること
func Test_巡回が回っているあいだに常駐を止める_P001_portを付けてSIGINTを受けたら段ごとに名乗って終わる(t *testing.T) {
	env := newDaemonEnv(t)
	env.GitHub = newFakeGitHub(t, "octocat", env.Timeline)
	env.Herdr.Handle("pane.list", func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "pane_list", "panes": []any{}}, nil
	})
	env.Herdr.Handle("agent.list", func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "agent_list", "agents": []any{}}, nil
	})

	cmd, logs := env.startWithArgs(t, "--port=0")
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("continuo の出力:\n%s", logs.String())
		}
	})

	var addr string
	waitFor(t, 30*time.Second, "ダッシュボードが開く", func() bool {
		var ok bool
		addr, ok = dashboardAddr(logs)
		return ok
	})
	waitFor(t, 30*time.Second, "巡回が始まる", func() bool {
		return strings.Contains(logs.String(), "巡回を始めます")
	})
	holdDashboardConnection(t, addr)

	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("SIGINT を送れません: %v", err)
	}

	// **押した直後に反応が出ること。**後始末の1段目より前に出る。
	waitFor(t, 5*time.Second, "割り込みを受けたことがすぐ出る", func() bool {
		return strings.Contains(logs.String(), "もう一度 Ctrl+C")
	})
	early := logs.String()
	if !strings.Contains(early, "kill -QUIT") {
		t.Fatalf("止まらないときの次の一手が出ていない:\n%s", early)
	}

	started := time.Now()
	code, finished := waitProcess(context.Background(), cmd, 30*time.Second)
	if !finished {
		t.Fatalf("SIGINT を受けても 30 秒以内に終了しなかった\n%s", logs.String())
	}
	if code != 0 {
		t.Fatalf("終了コードが 0 ではない: got %d\n%s", code, logs.String())
	}

	// **応答の読み切りの期限（10 秒）まで待っていない。**待っていたら叩き切れていない。
	if elapsed := time.Since(started); elapsed > 9*time.Second {
		t.Fatalf("ダッシュボードを叩き切らずに待った: %v\n%s", elapsed, logs.String())
	}

	out := logs.String()
	for _, want := range []string{
		"後始末 1/3: ダッシュボードを閉じています",
		"後始末 2/3: hook の受け口を閉じています",
		"後始末 3/3: 走行中の turn ループの終了を待っています",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("段ごとの名乗りが出ていない: %q\n%s", want, out)
		}
	}
	if !strings.Contains(out, "ダッシュボードの応答が期限内に終わらないので、接続を切って閉じます") {
		t.Fatalf("ダッシュボードを叩き切ったことが出ていない:\n%s", out)
	}
	// socket のファイルを残さない（次の起動が「残骸がある」と言って止まる）。
	if _, err := os.Stat(env.SocketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hook の socket のファイルが残っている: %v", err)
	}
}

// {"RUCM-PATH": "P002"}
//
// Test_巡回が回っているあいだに常駐を止める_P002_SIGINTを無視に設定した親から起動しても2回目のCtrlCで止まる は、
// **利用者の「連打しても、いつまで経っても止まらなかった」を潰した筋**を確かめる。
//
// 目的: 2回目の割り込みを `signal.Stop`（元の動作へ戻す）に頼ると、**起動元が `SIGINT` を
// 「無視」に設定していたときに戻る先が「無視」になり、2回目以降が何も起こさない。**
// `nohup` / `setsid` / job control の無いシェルのバックグラウンド起動がこの状態を作る。
// **自分で数えて終わらせれば、起動元が何であっても結果は変わらない。**
//
// 与える情報: `trap "" INT` を掛けた `/bin/sh` から `exec` した continuo と、
// **応答を返し終えられない要求が1本**（後始末の1段目を必ず期限まで引き延ばす）。
// そこへ `SIGINT` を2回。
//
// 成功条件: 2回目のあと 10 秒以内に、**割り込みの終了コード（130）**で終わること。
// 0 で終わったなら、それは後始末が普通に終わっただけで、2回目は効いていない。
func Test_巡回が回っているあいだに常駐を止める_P002_SIGINTを無視に設定した親から起動しても2回目のCtrlCで止まる(t *testing.T) {
	env := newDaemonEnv(t)
	env.GitHub = newFakeGitHub(t, "octocat", env.Timeline)
	env.Herdr.Handle("pane.list", func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "pane_list", "panes": []any{}}, nil
	})
	env.Herdr.Handle("agent.list", func(map[string]any) (any, *rpcErr) {
		return map[string]any{"type": "agent_list", "agents": []any{}}, nil
	})

	cmd, logs := env.startIgnoringSIGINT(t, "--port=0")
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("continuo の出力:\n%s", logs.String())
		}
	})

	var addr string
	waitFor(t, 30*time.Second, "ダッシュボードが開く", func() bool {
		var ok bool
		addr, ok = dashboardAddr(logs)
		return ok
	})
	holdDashboardConnection(t, addr)

	// 1回目。**まだ終わらない。**待たせる理由が出るだけである。
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("1回目の SIGINT を送れません: %v", err)
	}
	waitFor(t, 10*time.Second, "1回目の SIGINT が届く（無視のままなら1行も出ない）", func() bool {
		return strings.Contains(logs.String(), "もう一度 Ctrl+C")
	})

	// 2回目。**後始末を待たずに終わる。**
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("2回目の SIGINT を送れません: %v", err)
	}
	code, finished := waitProcess(context.Background(), cmd, 10*time.Second)
	if !finished {
		t.Fatalf("2回目の SIGINT を受けても 10 秒以内に終了しなかった\n%s", logs.String())
	}
	if code != 130 {
		t.Fatalf("2回目の SIGINT が効いていない（割り込みの終了コードで終わっていない）: got %d\n%s",
			code, logs.String())
	}
}
