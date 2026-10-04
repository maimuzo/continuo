package hookclient_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/hookclient"
	"github.com/maimuzo/continuo/internal/statussignal"
)

// writeSignalFile は、逃がし先の親（issue ごとのディレクトリ）へ、取り得る値のファイルを置く。
//
// **本体が着手のときに書くものと同じ形である**（既定の対応表）。
//
// t: 呼び出し元のテスト。
// pendingDir: `--pending-dir` に渡すパス。
func writeSignalFile(t *testing.T, pendingDir string) {
	t.Helper()
	inReview, blocked := "In Review", "Blocked"
	data, err := statussignal.Encode(statussignal.File{
		Identifier: "octocat/hello-world#188",
		Prefix:     "CONTINUO-STATUS:",
		Values:     map[string]*string{"review": &inReview, "blocked": &blocked, "working": nil},
	})
	if err != nil {
		t.Fatalf("取り得る値のファイルを組み立てられない: %v", err)
	}
	writeRawSignalFile(t, pendingDir, data)
}

// writeRawSignalFile は、取り得る値のファイルの場所へ、渡された中身をそのまま置く。
//
// t: 呼び出し元のテスト。
// pendingDir: `--pending-dir` に渡すパス。
// data: 置く中身。
func writeRawSignalFile(t *testing.T, pendingDir string, data []byte) {
	t.Helper()
	path := statussignal.PathFromPendingDir(pendingDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("issue ごとのディレクトリを作れない: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("取り得る値のファイルを書けない: %v", err)
	}
}

// stopLine は `Stop` hook の入力の1行を作る。
//
// fields: 既定の項目へ上書き・追加する項目。値に nil を渡すと、その項目を落とす。
// 戻り値: JSON の1行。
func stopLine(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	in := map[string]any{
		"hook_event_name":        "Stop",
		"session_id":             "session-1",
		"stop_hook_active":       false,
		"background_tasks":       []any{},
		"last_assistant_message": "終わりました。\n\nCONTINUO-STATUS: done",
	}
	for k, v := range fields {
		if v == nil {
			delete(in, k)
			continue
		}
		in[k] = v
	}
	line, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("hook の入力を組み立てられない: %v", err)
	}
	return line
}

// TestCheckStop_決まり以外の値なら差し戻して一覧を返す は、issue #274 の経路1 を確かめる。
//
// 目的: `Stop` の最後のテキストブロックに、取り得る値に無い表明が在れば、差し戻すと決め、
// 書いた値と取り得る値の一覧を本文に載せること。
// 与える情報: `CONTINUO-STATUS: done` で終わる `last_assistant_message` と、既定の対応表のファイル。
// 成功条件: 差し戻す。本文に「done」と3つの値が載っている。
func TestCheckStop_決まり以外の値なら差し戻して一覧を返す(t *testing.T) {
	pending := newPendingDir(t)
	writeSignalFile(t, pending)

	got := hookclient.CheckStop(stopLine(t, nil), pending)

	if !got.Block {
		t.Fatalf("決まり以外の値なのに差し戻していない")
	}
	for _, want := range []string{"「done」", "CONTINUO-STATUS: blocked", "CONTINUO-STATUS: review", "CONTINUO-STATUS: working"} {
		if !strings.Contains(got.Reason, want) {
			t.Errorf("本文に %q が無い:\n%s", want, got.Reason)
		}
	}
}

// TestCheckStop_調べない条件ではどれも差し戻さない は、差し戻しを出さない条件を1つずつ確かめる。
//
// **ここで差し戻さなかった分は、本体が次の turn の指示で返す**（issue #274 の経路2）。
// hook は、材料が1つでも欠けていたら黙って通す。
//
// 目的: 設計 3-25 の「hook が調べないとき」の条件ごとに、差し戻さないこと。
// 与える情報: 条件を1つずつ満たした入力。**どれも表明の値は決まり以外（`done`）である。**
// 成功条件: どれも差し戻さず、本文が空。
func TestCheckStop_調べない条件ではどれも差し戻さない(t *testing.T) {
	cases := []struct {
		name string
		// fields は `Stop` の入力へ上書きする項目である。
		fields map[string]any
		// file は取り得る値のファイルの置き方である。nil なら既定の対応表を置く。
		file func(t *testing.T, pending string)
		// line は入力そのものを差し替える。nil なら fields から作る。
		line []byte
	}{
		{name: "Stop 以外の hook である", fields: map[string]any{"hook_event_name": "SubagentStop"}},
		{name: "stop_hook_active が真である", fields: map[string]any{"stop_hook_active": true}},
		{name: "stop_hook_active の欄が無い", fields: map[string]any{"stop_hook_active": nil}},
		{name: "last_assistant_message の欄が無い", fields: map[string]any{"last_assistant_message": nil}},
		{name: "last_assistant_message が文字列でない", fields: map[string]any{"last_assistant_message": 123}},
		{name: "last_assistant_message が空である", fields: map[string]any{"last_assistant_message": ""}},
		{name: "入力が JSON でない", line: []byte("これは JSON ではない")},
		{name: "取り得る値のファイルが無い", file: func(*testing.T, string) {}},
		{name: "取り得る値のファイルが壊れている", file: func(t *testing.T, pending string) {
			writeRawSignalFile(t, pending, []byte(`{"prefix": "CONTINUO-STATUS:", "values": `))
		}},
		{name: "取り得る値のファイルに識別子が無い", file: func(t *testing.T, pending string) {
			writeRawSignalFile(t, pending, []byte(`{"prefix":"CONTINUO-STATUS:","values":{"review":"In Review"}}`))
		}},
		{name: "取り得る値のファイルに印が無い", file: func(t *testing.T, pending string) {
			writeRawSignalFile(t, pending, []byte(`{"identifier":"octocat/hello-world#188","values":{"review":"In Review"}}`))
		}},
		{name: "取り得る値のファイルに値が無い", file: func(t *testing.T, pending string) {
			writeRawSignalFile(t, pending, []byte(`{"identifier":"octocat/hello-world#188","prefix":"CONTINUO-STATUS:","values":{}}`))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pending := newPendingDir(t)
			if tc.file != nil {
				tc.file(t, pending)
			} else {
				writeSignalFile(t, pending)
			}
			line := tc.line
			if line == nil {
				line = stopLine(t, tc.fields)
			}

			got := hookclient.CheckStop(line, pending)

			if got.Block || got.Reason != "" {
				t.Fatalf("差し戻している: %+v", got)
			}
		})
	}
}

// TestCheckStop_決まりどおりの表明は差し戻さない は、正しい応答を止めないことを確かめる。
//
// 目的: 取り得る値を書いた応答・表明の無い応答・対象付きの正しい表明を、差し戻さないこと。
// **とくに `#45 review` を差し戻さないこと**（hook がいま作業している issue を知らないと、
// `#45` を値と読んで差し戻す）。
// 与える情報: それぞれの `last_assistant_message`。
// 成功条件: どれも差し戻さない。
func TestCheckStop_決まりどおりの表明は差し戻さない(t *testing.T) {
	for name, message := range map[string]string{
		"取り得る値":              "終わりました。\n\nCONTINUO-STATUS: review",
		"大文字で書いた値":           "CONTINUO-STATUS: REVIEW",
		"表明が無い":              "作業を進めています。",
		"対象付きの正しい表明":         "CONTINUO-STATUS: review\nCONTINUO-STATUS: #45 review",
		"途中で引用し最後に正しく書いた":    "CONTINUO-STATUS: done と書くと無視されます。\nCONTINUO-STATUS: done\nCONTINUO-STATUS: blocked",
		"行頭に無い決まり以外の値":       "- CONTINUO-STATUS: done\n結果は CONTINUO-STATUS: done です。",
		"自分の番号を書いた行のあとに対象なし": "CONTINUO-STATUS: #188 done\nCONTINUO-STATUS: review",
	} {
		t.Run(name, func(t *testing.T) {
			pending := newPendingDir(t)
			writeSignalFile(t, pending)

			got := hookclient.CheckStop(stopLine(t, map[string]any{"last_assistant_message": message}), pending)

			if got.Block {
				t.Fatalf("決まりどおりの応答を差し戻している:\n%s", got.Reason)
			}
		})
	}
}

// TestCheckStop_別のissueを指す表明が決まり以外なら対象を添えて差し戻す は、グループの run を確かめる。
//
// 目的: 自分の issue の表明が正しくても、別の issue を指す表明の値が決まり以外なら差し戻し、
// 対象を付けた形で書き直すよう求めること。
// 与える情報: `review` と `#45 done`。
// 成功条件: 差し戻す。本文に対象と、`#45 <値>` の形が載っている。
func TestCheckStop_別のissueを指す表明が決まり以外なら対象を添えて差し戻す(t *testing.T) {
	pending := newPendingDir(t)
	writeSignalFile(t, pending)

	got := hookclient.CheckStop(stopLine(t, map[string]any{
		"last_assistant_message": "CONTINUO-STATUS: review\nCONTINUO-STATUS: #45 done",
	}), pending)

	if !got.Block {
		t.Fatalf("別の issue を指す表明が決まり以外なのに、差し戻していない")
	}
	for _, want := range []string{"octocat/hello-world#45 の値 「done」", "`CONTINUO-STATUS: #45 <値>`"} {
		if !strings.Contains(got.Reason, want) {
			t.Errorf("本文に %q が無い:\n%s", want, got.Reason)
		}
	}
}

// TestForward_転送できたときだけ転送した1行を返す は、hook が表明を調べる条件を確かめる。
//
// **本体が動いていないあいだは調べない。**取り得る値のファイルが、次に起動する本体の設定と
// 食い違っているかもしれないためである。呼び出し側は `Result.Line` が空なら調べない。
//
// 目的: socket へ転送できたときだけ `Line` が入り、逃がし先へ書いたときと、上限を超えた
// 入力では空であること。
// 与える情報: 受け口が在る場合・無い場合・上限を超えた入力。
// 成功条件: 順に、`Line` が入力と同じ JSON・空・空。
func TestForward_転送できたときだけ転送した1行を返す(t *testing.T) {
	input := string(stopLine(t, nil))

	sink := newFakeSink(t, false)
	sent := hookclient.Forward(hookclient.Config{
		SocketPath: sink.socketPath, PendingDir: newPendingDir(t), Stdin: strings.NewReader(input),
	})
	if sent.Outcome != hookclient.OutcomeSent {
		t.Fatalf("転送できていない: %+v", sent)
	}
	var a, b map[string]any
	if err := json.Unmarshal(sent.Line, &a); err != nil {
		t.Fatalf("Line が JSON でない: %v（%q）", err, sent.Line)
	}
	_ = json.Unmarshal([]byte(input), &b)
	if a["last_assistant_message"] != b["last_assistant_message"] {
		t.Errorf("Line が転送した入力と違う: %q", sent.Line)
	}

	spilled := hookclient.Forward(hookclient.Config{
		SocketPath: filepath.Join(os.TempDir(), "continuo-no-such.sock"),
		PendingDir: newPendingDir(t), Stdin: strings.NewReader(input),
	})
	if spilled.Outcome != hookclient.OutcomeSpilled || len(spilled.Line) != 0 {
		t.Errorf("逃がし先へ書いたのに Line が入っている: outcome=%v line=%q", spilled.Outcome, spilled.Line)
	}

	truncated := hookclient.Forward(hookclient.Config{
		SocketPath: sink.socketPath, PendingDir: newPendingDir(t),
		Stdin: strings.NewReader(input), MaxInputBytes: 60,
	})
	if !truncated.Truncated || len(truncated.Line) != 0 {
		t.Errorf("上限を超えた入力なのに Line が入っている: %+v", truncated)
	}
}
