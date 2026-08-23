package tracker_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/maimuzo/continuo/internal/tracker"
)

// 目的: https でない接続先を拒否することを確認する
// （レビュー指摘「endpoint の scheme を検査せずトークンを送る」の回帰テスト）。
//
// **接続先は環境変数 CONTINUO_GITHUB_GRAPHQL_ENDPOINT で1行書き換えられる。**
// 検査しないと、`gh auth token` で取った GitHub のトークンが Authorization: Bearer 付きで
// 平文のまま任意の第三者へ渡る。herdr の socket は「環境変数を勝手に優先しない」と
// 決めてあるのに（設計 2-1）、資格情報が付いてまわるこちらだけが無検査だった。
//
// 与える情報: http:// の外部ホスト・scheme 無し・ホスト名無し・URL として壊れた値。
// 成功条件: いずれも NewAdapter が CategoryInvalidConfig のエラーを返すこと。
func TestNewAdapter_httpsでない接続先を拒否する(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
	}{
		{name: "外部ホストへの平文http", endpoint: "http://example.com/graphql"},
		{name: "scheme無し", endpoint: "api.github.com/graphql"},
		{name: "ホスト名無し", endpoint: "https:///graphql"},
		{name: "URLとして壊れている", endpoint: "https://%zz/graphql"},
		{name: "httpsでないscheme", endpoint: "ftp://api.github.com/graphql"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tracker.NewAdapter(testTrackerConfig(), tc.endpoint, "test-token", nil, nil, nil)
			if err == nil {
				t.Fatalf("接続先 %q を受け付けてしまった（トークンが平文で流れる）", tc.endpoint)
			}
			if !tracker.IsCategory(err, tracker.CategoryInvalidConfig) {
				t.Fatalf("エラーの分類が想定と違う: %v", err)
			}
		})
	}
}

// 目的: loopback の http（テストの httptest.Server）だけは受け付けることを確認する。
//
// **これを弾くと、本番のボードへ接続しないためのテスト用の偽サーバが使えなくなる。**
//
// 与える情報: 127.0.0.1 / localhost / [::1] の http URL。
// 成功条件: いずれも NewAdapter が成功すること。
func TestNewAdapter_loopbackのhttpは受け付ける(t *testing.T) {
	for _, endpoint := range []string{
		"http://127.0.0.1:8080/graphql",
		"http://localhost:8080/graphql",
		"http://[::1]:8080/graphql",
	} {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := tracker.NewAdapter(testTrackerConfig(), endpoint, "test-token", nil, nil, nil); err != nil {
				t.Fatalf("loopback の http を拒否した（テストの偽サーバが使えなくなる）: %v", err)
			}
		})
	}
}

// 目的: endpoint を空文字にしたときは本番の GitHub（https）が既定になることを確認する。
// 与える情報: 空文字の endpoint。
// 成功条件: NewAdapter が成功すること（既定値そのものが検査を通ること）。
func TestNewAdapter_endpointが空なら既定のhttpsを使う(t *testing.T) {
	if _, err := tracker.NewAdapter(testTrackerConfig(), "", "test-token", nil, nil, nil); err != nil {
		t.Fatalf("既定の接続先が検査を通らない: %v", err)
	}
}

// 目的: httpClient を渡さなかったときに http.DefaultClient を使っていないことを確認する
// （レビュー指摘「http.DefaultClient は Timeout を持たない」の回帰テスト）。
//
// **本番の組み立ては httpClient に nil を渡し、Tick に渡る ctx にも期限が無い。**
// http.DefaultClient は Timeout を持たないので、TCP は張れたが応答が返らない相手
// （不安定な回線・captive portal）に当たると、候補の取得・実行中の照合・stall の判定が
// 全部止まったまま復帰しない。
//
// 与える情報: http.DefaultClient.Timeout を 50 ミリ秒に縮めたうえで（テストの間だけ）、
// 応答を 300 ミリ秒遅らせる偽サーバへ nil の httpClient で接続する。
// 成功条件: 呼び出しが**成功する**こと。失敗するなら http.DefaultClient を使っている。
func TestNewAdapter_httpClientがnilでもDefaultClientを使わない(t *testing.T) {
	saved := http.DefaultClient.Timeout
	http.DefaultClient.Timeout = 50 * time.Millisecond
	t.Cleanup(func() { http.DefaultClient.Timeout = saved })

	fs := newFakeGraphQLServer(t, func(n int, req capturedRequest) fakeGraphQLResponse {
		time.Sleep(300 * time.Millisecond)
		return dataResponse(candidateItemsPayload(nil, false, ""))
	})

	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	if _, err := a.FetchIssuesByStates(context.Background(), []string{"Ready"}); err != nil {
		t.Fatalf("http.DefaultClient の上限で打ち切られた（既定のクライアントを組み立てていない）: %v", err)
	}
}

// 目的: エラーメッセージへ載せる応答本文が、多バイト文字の途中で割れないことを確認する
// （レビュー指摘「truncate がバイト単位で切るので日本語が壊れる」の回帰テスト）。
// 与える情報: 日本語だけの長い本文を返す 500 応答。
// 成功条件: エラーメッセージが妥当な UTF-8 で、置換文字（U+FFFD）を含まないこと。
func TestFetchIssuesByStates_エラー本文の切り詰めで日本語が壊れない(t *testing.T) {
	body := strings.Repeat("ボードを読み取れませんでした。", 80)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	a, err := tracker.NewAdapter(testTrackerConfig(), srv.URL, "test-token", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}

	_, err = a.FetchIssuesByStates(context.Background(), []string{"Ready"})
	if err == nil {
		t.Fatalf("500 なのにエラーが返らなかった")
	}
	msg := err.Error()
	if !utf8.ValidString(msg) {
		t.Fatalf("エラーメッセージが妥当な UTF-8 でない（多バイト文字が割れている）: %q", msg)
	}
	if strings.ContainsRune(msg, '�') {
		t.Fatalf("エラーメッセージに壊れた文字が入っている: %q", msg)
	}
}
