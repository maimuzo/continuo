package tracker_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/tracker"
)

// TestGraphQLEndpointForHost_接続先ホストから宛先を導く は、宛先を決める規則を確かめる。
//
// 目的: 設計 3-86。宛先の規則は `gh` 本体と同じにする（gh 2.100.0 の `GH_DEBUG=api` で実測）。
// 規則を取り違えると、GitHub Enterprise Server と `<名前>.ghe.com` のどちらかが必ず 404 になる。
// 与える情報: github.com・GitHub Enterprise Server のホスト名・`<名前>.ghe.com`・空文字・大文字混じり。
// 成功条件: それぞれ決まった URL が返ること。空文字は github.com として扱うこと。
func TestGraphQLEndpointForHost_接続先ホストから宛先を導く(t *testing.T) {
	cases := []struct {
		host string
		want string
	}{
		{"github.com", "https://api.github.com/graphql"},
		{"", "https://api.github.com/graphql"},
		{"GitHub.com", "https://api.github.com/graphql"},
		{"ghe.example.com", "https://ghe.example.com/api/graphql"},
		{"ghe", "https://ghe/api/graphql"},
		{"octocorp.ghe.com", "https://api.octocorp.ghe.com/graphql"},
		{"Octocorp.GHE.com", "https://api.octocorp.ghe.com/graphql"},
	}
	for _, tc := range cases {
		if got := tracker.GraphQLEndpointForHost(tc.host); got != tc.want {
			t.Errorf("host %q の宛先が違う: got %q, want %q", tc.host, got, tc.want)
		}
	}
}

// recordingTransport は、リクエストの宛先を記録して、決まった応答を返す RoundTripper である。
// **外へは1バイトも送らない。**
type recordingTransport struct {
	// urls は受け取ったリクエストの URL である（受け取った順）。
	urls []string
	// auth は受け取ったリクエストの Authorization ヘッダである（受け取った順）。
	auth []string
	// body は返す応答の本文である。
	body string
}

// RoundTrip は宛先とヘッダを記録し、決まった応答を返す。
func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.urls = append(rt.urls, req.URL.String())
	rt.auth = append(rt.auth, req.Header.Get("Authorization"))
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(rt.body)),
		Request:    req,
	}, nil
}

// schemaAllPresent は、4要素とも在ると答えるスキーマの照会の応答である。
const schemaAllPresent = `{"data":{
  "projectV2":{"fields":[{"name":"items","args":[{"name":"first"},{"name":"query"}]}]},
  "event":{"name":"ProjectV2ItemStatusChangedEvent"},
  "itemTypes":{"enumValues":[{"name":"PROJECT_V2_ITEM_STATUS_CHANGED_EVENT"}]},
  "issue":{"fields":[{"name":"blockedBy"}]}
}}`

// TestNewAdapter_宛先を渡さなければ接続先ホストから導いた宛先へ送る は、
// NewAdapter が宛先を自分で導くことを確かめる。
//
// 目的: 設計 3-86。常駐プロセス・`continuo doctor`・`continuo abandon`・`continuo prompt` の
// 4つは、どれも環境変数の値（無ければ空文字）を NewAdapter へ渡す。**宛先を導くのは
// NewAdapter の1箇所だけ**なので、4つが別の宛先へ向くことが無い。
// 与える情報: `Host` が `ghe.example.com` の設定と、空文字の宛先と、宛先を記録する RoundTripper。
// 成功条件: リクエストが `https://ghe.example.com/api/graphql` へ送られ、渡したトークンが付き、
// 警告（差し替えられています）が出ないこと。
func TestNewAdapter_宛先を渡さなければ接続先ホストから導いた宛先へ送る(t *testing.T) {
	cfg := testTrackerConfig()
	cfg.Provider.Host = "ghe.example.com"
	rt := &recordingTransport{body: schemaAllPresent}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	adapter, err := tracker.NewAdapter(cfg, "", "token-for-ghe", &http.Client{Transport: rt}, logger, nil)
	if err != nil {
		t.Fatalf("NewAdapter に失敗した: %v", err)
	}
	if _, err := adapter.MissingSchemaElements(context.Background()); err != nil {
		t.Fatalf("照会に失敗した: %v", err)
	}

	if len(rt.urls) != 1 || rt.urls[0] != "https://ghe.example.com/api/graphql" {
		t.Fatalf("宛先が接続先ホストから導いたものではない: %v", rt.urls)
	}
	if rt.auth[0] != "Bearer token-for-ghe" {
		t.Fatalf("渡したトークンが付いていない: %q", rt.auth[0])
	}
	if strings.Contains(logs.String(), "差し替えられています") {
		t.Fatalf("正規の接続先なのに、差し替えの警告が出た: %s", logs.String())
	}
}

// TestNewAdapter_宛先を差し替えたときだけ警告を出す は、警告の条件を確かめる。
//
// 目的: 設計 3-86。警告は「設定の接続先ホストから導いた宛先と違うとき」だけ出す。
// github.com の宛先と比べると、GitHub Enterprise を正規の接続先にした利用者が、
// 起動のたびに「本番の GitHub ではありません」を読む。
// 与える情報: `Host` が `ghe.example.com` の設定と、loopback の宛先（環境変数で差し替えた状態）。
// 成功条件: 警告が1行出て、差し替えた宛先と接続先ホストの両方が載ること。
func TestNewAdapter_宛先を差し替えたときだけ警告を出す(t *testing.T) {
	cfg := testTrackerConfig()
	cfg.Provider.Host = "ghe.example.com"
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	if _, err := tracker.NewAdapter(cfg, "http://127.0.0.1:1/graphql", "t", nil, logger, nil); err != nil {
		t.Fatalf("NewAdapter に失敗した: %v", err)
	}

	out := logs.String()
	for _, want := range []string{"差し替えられています", "http://127.0.0.1:1/graphql", "ghe.example.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("警告に %q が無い: %s", want, out)
		}
	}
}

// TestMissingSchemaElements_足りない要素を決まった順で返す は、照会の応答の読み方を確かめる。
//
// 目的: 設計 3-86。**存在しない型は null で返る**（誤りにはならない。github.com で実測）。
// null と「フィールドの一覧に無い」の両方を「足りない」と読めなければ、
// GitHub Enterprise Server 3.19 以下を見分けられない。
// 与える情報: 3.17 と同じ形の応答（`items` に `query` 引数が無い・型が null・enum に値が無い・
// `Issue` に `blockedBy` が無い）。
// 成功条件: 4要素が、決まった順で全部返ること。
func TestMissingSchemaElements_足りない要素を決まった順で返す(t *testing.T) {
	cfg := testTrackerConfig()
	cfg.Provider.Host = "ghe.example.com"
	rt := &recordingTransport{body: `{"data":{
	  "projectV2":{"fields":[{"name":"items","args":[{"name":"first"}]}]},
	  "event":null,
	  "itemTypes":{"enumValues":[{"name":"ISSUE_COMMENT"}]},
	  "issue":{"fields":[{"name":"number"}]}
	}}`}
	adapter, err := tracker.NewAdapter(cfg, "", "t", &http.Client{Transport: rt}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatalf("NewAdapter に失敗した: %v", err)
	}

	missing, err := adapter.MissingSchemaElements(context.Background())
	if err != nil {
		t.Fatalf("照会に失敗した: %v", err)
	}

	want := []string{
		tracker.SchemaElementItemsQuery,
		tracker.SchemaElementStatusChangedEvent,
		tracker.SchemaElementStatusChangedEnum,
		tracker.SchemaElementBlockedBy,
	}
	if strings.Join(missing, ",") != strings.Join(want, ",") {
		t.Fatalf("足りない要素が違う: got %v, want %v", missing, want)
	}
}

// writeRecordingGH は、受け取った引数と環境変数 GH_HOST を1行ずつ記録する偽の `gh` を PATH の先頭へ置く。
//
// **本物の `gh` を呼ばない。**本物の認証情報を読ませないためである。
//
// t: 呼び出し元のテスト。
// stdout: 偽の `gh` が標準出力へ返す文字列。
// 戻り値: 記録先のファイルのパス。
func writeRecordingGH(t *testing.T, stdout string) string {
	t.Helper()
	dir := t.TempDir()
	record := filepath.Join(dir, "gh-args.txt")
	script := "#!/bin/sh\n" +
		"echo \"args=$* GH_HOST=${GH_HOST:-}\" >> \"" + record + "\"\n" +
		"printf '%s\\n' '" + stdout + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatalf("テスト用gh mock を書けません: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return record
}

// TestGHの関数は接続先ホストを必ず渡す は、continuo が起こす `gh` の呼び方を確かめる。
//
// 目的: 設計 3-86。ホストを渡さないと `gh` は自分の既定ホストで答えるので、github.com と
// GitHub Enterprise の両方にログインしている機械では、**接続先の宛先へ別のホストのトークンを送る。**
// 与える情報: 引数を記録する偽の `gh` と、接続先ホスト `ghe.example.com`。
// 成功条件: `auth token`・`auth status`・`api user` のどれにも `--hostname ghe.example.com` が付くこと。
func TestGHの関数は接続先ホストを必ず渡す(t *testing.T) {
	record := writeRecordingGH(t, "octocat")
	ctx := context.Background()

	if _, err := tracker.GHAuthTokenForHost("ghe.example.com")(ctx); err != nil {
		t.Fatalf("auth token に失敗した: %v", err)
	}
	if _, err := tracker.GHAuthStatusForHost("ghe.example.com")(ctx); err != nil {
		t.Fatalf("auth status に失敗した: %v", err)
	}
	if _, err := tracker.GHAPIUserLoginForHost("ghe.example.com")(ctx); err != nil {
		t.Fatalf("api user に失敗した: %v", err)
	}

	raw, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("テスト用gh mock の記録を読めません: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	want := []string{
		"args=auth token --hostname ghe.example.com ",
		"args=auth status --hostname ghe.example.com ",
		"args=api --hostname ghe.example.com user --jq .login ",
	}
	if len(lines) != len(want) {
		t.Fatalf("gh の呼び出しの数が違う: %v", lines)
	}
	for i, w := range want {
		if !strings.HasPrefix(lines[i], w) {
			t.Errorf("%d 本目の呼び方が違う: got %q, want（前方一致） %q", i+1, lines[i], w)
		}
	}
}

// TestResolveToken_既定の取得関数は設定の接続先ホストを使う は、トークンの取り違えを防ぐ線を確かめる。
//
// 目的: 設計 3-86。`ResolveToken` へ関数を渡さない呼び出し元（`continuo abandon` と
// `continuo prompt`）でも、設定の接続先ホストのトークンを取ること。
// 与える情報: `Host` が `ghe.example.com` の設定と、nil の取得関数と、引数を記録する偽の `gh`。
// 成功条件: 偽の `gh` が `auth token --hostname ghe.example.com` で呼ばれること。
func TestResolveToken_既定の取得関数は設定の接続先ホストを使う(t *testing.T) {
	record := writeRecordingGH(t, "token-from-fake-gh")
	provider := config.TrackerProviderConfig{TokenSource: "gh_auth", Host: "ghe.example.com"}

	token, err := tracker.ResolveToken(context.Background(), provider, nil)
	if err != nil {
		t.Fatalf("ResolveToken に失敗した: %v", err)
	}
	if token != "token-from-fake-gh" {
		t.Fatalf("トークンが想定と違う: %q", token)
	}
	raw, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("テスト用gh mock の記録を読めません: %v", err)
	}
	if !strings.HasPrefix(string(raw), "args=auth token --hostname ghe.example.com ") {
		t.Fatalf("接続先ホストを渡していない: %q", string(raw))
	}
}

// TestCheckGHProjectScope_直し方に接続先ホストを入れる は、案内の文面を確かめる。
//
// 目的: 設計 3-86。`gh auth login -s project` とだけ案内すると、GitHub Enterprise の利用者が
// 案内どおりに叩いて github.com へログインする。
// 与える情報: 未ログインの出力と、接続先ホスト `ghe.example.com`。
// 成功条件: エラー文に `gh auth login --hostname ghe.example.com -s project` が入ること。
func TestCheckGHProjectScope_直し方に接続先ホストを入れる(t *testing.T) {
	err := tracker.CheckGHProjectScope(context.Background(), "ghe.example.com", func(context.Context) (string, error) {
		return "You are not logged into any accounts on ghe.example.com", nil
	})
	if err == nil {
		t.Fatalf("未ログインなのに合格した")
	}
	if !strings.Contains(err.Error(), "gh auth login --hostname ghe.example.com -s project") {
		t.Fatalf("直し方に接続先ホストが入っていない: %v", err)
	}
}
