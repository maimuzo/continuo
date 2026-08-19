package server_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/server"
)

// 目的: `server.port` が null のときに listen しないことを確認する（設計 5-2。ダッシュボードは
// 任意の機能であり、既定では1つも port を開かない）。
// 与える情報: Port を nil にした Options。
// 成功条件: New が (nil, nil) を返し、返った nil をそのまま Close へ渡しても落ちないこと。
func TestNew_serverPortがnullならlistenしない(t *testing.T) {
	src := &fakeSource{views: sampleViews()}
	s, err := server.New(server.Options{Port: nil, Source: src, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatalf("port が null のときはエラーにしない: %v", err)
	}
	if s != nil {
		t.Fatalf("port が null なのにダッシュボードが組み立てられた: %#v", s)
	}
	// 呼び出す側が nil の判定を書かずに済むこと（daemon の終了処理がこの形で呼ぶ）。
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("nil のダッシュボードを閉じてエラーになった: %v", err)
	}
	if src.calls != 0 {
		t.Fatalf("listen していないのに run の写しを取りに行った: %d 回", src.calls)
	}
}

// 目的: 待ち受け先が 127.0.0.1 に固定されていることを確認する。run の中身（issue の URL・
// worktree のパス・トークンの消費）は外へ晒すものではなく、このサーバは認証を持たない。
// 与える情報: ポート 0（OS に選ばせる）で起動したダッシュボードと、この機材の
// ループバックでない IPv4 アドレス。
// 成功条件: 実際の待ち受け先のホストが 127.0.0.1 であること。ループバックでない
// アドレスの同じポートへは接続できないこと。
func TestStart_ループバックにしか待ち受けない(t *testing.T) {
	s, _ := newTestServer(t, sampleViews())
	if err := s.Start(); err != nil {
		t.Fatalf("待ち受けを開始できなかった: %v", err)
	}
	defer func() { _ = s.Close(context.Background()) }()

	host, port, err := net.SplitHostPort(s.Addr())
	if err != nil {
		t.Fatalf("待ち受け先を解釈できなかった（%s）: %v", s.Addr(), err)
	}
	if host != server.LoopbackHost {
		t.Fatalf("ループバック以外で待ち受けている: got %q, want %q", host, server.LoopbackHost)
	}

	for _, addr := range nonLoopbackIPv4(t) {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(addr, port), 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			t.Fatalf("ループバックでないアドレス %s から接続できてしまった", addr)
		}
	}
}

// 目的: 実行中の run の一覧（issue / Status / turn 数 / 最後に hook を受けた時刻）を
// HTML で出せることを確認する。09_dashboard.md の受け入れの基準そのものである。
// 与える情報: 実行中の run 2件の写し。
// 成功条件: 200 が返り、issue の識別子・タイトル・Status・turn 数・最後に hook を
// 受けた時刻が本文に含まれること。並び順が identifier の昇順であること。
func TestIndex_実行中のrunの一覧を出せる(t *testing.T) {
	s, src := newTestServer(t, sampleViews())
	code, body := get(t, s, http.MethodGet, "/")
	if code != http.StatusOK {
		t.Fatalf("状態コードが違う: got %d, want %d", code, http.StatusOK)
	}
	if src.calls != 1 {
		t.Fatalf("run の写しを取った回数が違う: got %d, want 1", src.calls)
	}

	for _, want := range []string{
		"maimuzo/continuo#12",                                // issue の識別子
		"ダッシュボードを作る",                                         // タイトル
		"In Progress",                                        // Status
		"hook の受け口を直す",                                       // もう1件のタイトル
		testTime.Add(-90 * time.Second).Format(time.RFC3339), // 最後に hook を受けた時刻
		"1分30秒前",                                             // その経過
		"枠待ち",                                                // 枠待ちの表示
	} {
		if !strings.Contains(body, want) {
			t.Errorf("本文に %q が無い", want)
		}
	}

	// turn 数（3 と 11）が数値の欄に出ていること。
	for _, want := range []string{">3<", ">11<"} {
		if !strings.Contains(body, want) {
			t.Errorf("turn 数 %q が本文に無い", want)
		}
	}

	// identifier の昇順（#12 が #7 より前）で並ぶこと。`RunViews` の順序は不定なので、
	// 並べ替えないと再読み込みのたびに行が入れ替わる。
	if strings.Index(body, "maimuzo/continuo#12") > strings.Index(body, "maimuzo/continuo#7") {
		t.Error("identifier の昇順で並んでいない")
	}
}

// 目的: 実行中の run が1件も無いときに落ちないことを確認する。continuo は起動直後や
// 全部の run が終わったあとに必ずこの状態になる。
// 与える情報: 空の写し。
// 成功条件: 200 が返り、「実行中の run はありません」と出ること。
func TestIndex_実行中のrunが無くても表示できる(t *testing.T) {
	s, _ := newTestServer(t, nil)
	code, body := get(t, s, http.MethodGet, "/")
	if code != http.StatusOK {
		t.Fatalf("状態コードが違う: got %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "実行中の run はありません") {
		t.Error("実行中の run が無いことの表示が出ていない")
	}
}

// 目的: issue のタイトルが必ずエスケープされることを確認する。タイトルは continuo の
// 外（GitHub）から来る文字列であり、そのまま HTML へ混ぜると script を仕込める。
// 与える情報: script タグと属性を閉じる引用符を含むタイトル。
// 成功条件: 生の `<script>` が本文に現れず、エスケープされた形で現れること。
func TestIndex_issueのタイトルをエスケープする(t *testing.T) {
	views := []orchestrator.RunView{{
		Identifier: "maimuzo/continuo#1",
		Title:      `<script>alert("x")</script>`,
		State:      `" onmouseover="alert(1)`,
		URL:        "https://github.com/maimuzo/continuo/issues/1",
	}}
	s, _ := newTestServer(t, views)
	_, body := get(t, s, http.MethodGet, "/")

	if strings.Contains(body, "<script>alert") {
		t.Error("タイトルの script タグがそのまま出ている")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Error("タイトルがエスケープされた形で出ていない")
	}
	if strings.Contains(body, `onmouseover="alert(1)"`) {
		t.Error("Status の引用符がエスケープされていない")
	}
}

// 目的: issue の URL に危ない scheme が入っていてもリンクとして出さないことを確認する。
// URL もトラッカーから来る文字列であり、`javascript:` を踏ませられてはならない。
// 与える情報: `javascript:` で始まる URL。
// 成功条件: その文字列が href に現れないこと。
func TestIndex_危ないURLはリンクにしない(t *testing.T) {
	views := []orchestrator.RunView{{
		Identifier: "maimuzo/continuo#1",
		Title:      "危ない URL",
		URL:        "javascript:alert(1)",
		State:      "In Progress",
	}}
	s, _ := newTestServer(t, views)
	_, body := get(t, s, http.MethodGet, "/")
	if strings.Contains(body, "javascript:alert(1)") {
		t.Error("javascript: の URL がそのまま href に入った")
	}
}

// 目的: トークンの集計を出せることを確認する（受け入れの基準）。run ごとの内訳と、
// 全体の合計の両方を出す。
// 与える情報: トークンの集計を持つ run 2件の写し。
// 成功条件: HTML に内訳が出ること。JSON の totals が2件の総和になっていること。
func TestトークンAPI_集計を出せる(t *testing.T) {
	s, _ := newTestServer(t, sampleViews())

	_, html := get(t, s, http.MethodGet, "/")
	for _, want := range []string{"701,185", "14,358", "1,216"} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML にトークンの内訳 %q が無い", want)
		}
	}

	code, body := get(t, s, http.MethodGet, server.APIStatePath)
	if code != http.StatusOK {
		t.Fatalf("状態コードが違う: got %d, want %d", code, http.StatusOK)
	}
	var snap server.Snapshot
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		t.Fatalf("JSON を解釈できなかった: %v\n%s", err, body)
	}
	want := server.Tokens{
		APICalls:      20,
		Input:         40,
		CacheCreation: 14361,
		CacheRead:     701189,
		Output:        1221,
		Total:         40 + 14361 + 701189 + 1221,
	}
	if snap.Totals != want {
		t.Errorf("トークンの合計が違う: got %+v, want %+v", snap.Totals, want)
	}
	if len(snap.Runs) != 2 {
		t.Fatalf("run の件数が違う: got %d, want 2", len(snap.Runs))
	}
	if snap.Runs[0].Identifier != "maimuzo/continuo#12" {
		t.Errorf("identifier の昇順で並んでいない: got %q", snap.Runs[0].Identifier)
	}
	if snap.Runs[0].TurnCount != 3 {
		t.Errorf("turn 数が違う: got %d, want 3", snap.Runs[0].TurnCount)
	}
	if snap.Runs[0].LastHookAt == nil || !snap.Runs[0].LastHookAt.Equal(testTime.Add(-90*time.Second)) {
		t.Errorf("最後に hook を受けた時刻が違う: got %v", snap.Runs[0].LastHookAt)
	}
}

// 目的: ダッシュボードが出すトークンが `requestId` で重複排除されたものであることを、
// transcript の実物から確認する（設計 3-15。assistant の行が API 呼び出しと1対1である
// 保証が無いので、重複排除しない値を出してはならない）。
// 与える情報: 同じ `requestId` を2回持つ transcript を `orchestrator.ReadTranscript` に
// 通した結果を、そのまま run の写しへ入れたもの。
// 成功条件: API 応答の件数が2（3ではない）になり、JSON と HTML の両方にその値が出ること。
func TestトークンAPI_requestIdで重複排除した値を出す(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"assistant","requestId":"req_1","message":{"usage":{"input_tokens":10,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000,"output_tokens":5}}}`,
		`{"type":"assistant","requestId":"req_1","message":{"usage":{"input_tokens":10,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000,"output_tokens":5}}}`,
		`{"type":"assistant","requestId":"req_2","message":{"usage":{"input_tokens":7,"cache_creation_input_tokens":0,"cache_read_input_tokens":2000,"output_tokens":3}}}`,
	})
	result, err := orchestrator.ReadTranscript(path, "", "CONTINUO-STATUS:", "maimuzo/continuo#1")
	if err != nil {
		t.Fatalf("transcript を読めなかった: %v", err)
	}
	if result.Usage.APICalls != 2 {
		t.Fatalf("重複排除が効いていない: got %d, want 2", result.Usage.APICalls)
	}

	s, _ := newTestServer(t, []orchestrator.RunView{{
		Identifier: "maimuzo/continuo#1",
		Title:      "重複排除の確認",
		State:      "In Progress",
		TurnCount:  1,
		LastHookAt: testTime,
		Tokens:     result.Usage,
		TokensAt:   testTime,
	}})

	code, body := get(t, s, http.MethodGet, server.APIStatePath)
	if code != http.StatusOK {
		t.Fatalf("状態コードが違う: got %d, want %d", code, http.StatusOK)
	}
	var snap server.Snapshot
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		t.Fatalf("JSON を解釈できなかった: %v", err)
	}
	want := server.Tokens{APICalls: 2, Input: 17, CacheCreation: 100, CacheRead: 3000, Output: 8, Total: 3125}
	if snap.Totals != want {
		t.Errorf("重複排除した合計になっていない: got %+v, want %+v", snap.Totals, want)
	}

	_, html := get(t, s, http.MethodGet, "/")
	if !strings.Contains(html, "3,125") {
		t.Error("HTML に重複排除後の合計が出ていない")
	}
}

// 目的: 書き込みの経路が存在しないことを確認する。このサーバは認証を持たないので、
// run を止める・Status を書くといった操作を受け付けてはならない。
// 与える情報: GET 以外のメソッドと、登録していないパス。
// 成功条件: GET 以外は 405 になり、run の写しを1度も取りに行かないこと。
// 登録していないパスは 404 になること。
func TestルーティングGET以外は受け付けない(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		for _, path := range []string{"/", server.APIStatePath} {
			s, src := newTestServer(t, sampleViews())
			code, _ := get(t, s, method, path)
			if code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s の状態コードが違う: got %d, want %d", method, path, code, http.StatusMethodNotAllowed)
			}
			if src.calls != 0 {
				t.Errorf("%s %s でハンドラまで届いた", method, path)
			}
		}
	}

	s, _ := newTestServer(t, sampleViews())
	if code, _ := get(t, s, http.MethodGet, "/runs/1/stop"); code != http.StatusNotFound {
		t.Errorf("登録していないパスの状態コードが違う: got %d, want %d", code, http.StatusNotFound)
	}
}

// 目的: 待ち受け先が New の時点の値で固定されることを確認する（設計 3-24。自前の
// リソースを掴んでいるので、変えるには continuo の再起動が要る。**設定の読み直しは
// 実装していない**が、入れるとしても `server.port` は反映しない）。
// 与える情報: 起動後に、New へ渡したポート番号の変数そのものを書き換える。
// 成功条件: 待ち受け先が変わらず、書き換えた後の番号では接続できないこと。
func Test待ち受け先はポート番号の変数を書き換えても変わらない(t *testing.T) {
	src := &fakeSource{views: sampleViews()}
	port := 0
	s, err := server.New(server.Options{
		Port:   &port,
		Source: src,
		Logger: slog.New(slog.DiscardHandler),
		Now:    func() time.Time { return testTime },
	})
	if err != nil {
		t.Fatalf("ダッシュボードを組み立てられなかった: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("待ち受けを開始できなかった: %v", err)
	}
	defer func() { _ = s.Close(context.Background()) }()

	before := s.Addr()

	// 設定を書き換えたのと同じ操作。**サーバは New の時点で値を写し取っている**ので、
	// 参照の先を書き換えても待ち受け先は動かない。
	port = 65535

	if after := s.Addr(); after != before {
		t.Fatalf("設定の書き換えで待ち受け先が変わった: before %q, after %q", before, after)
	}
	if code, _ := httpGet(t, "http://"+before+server.APIStatePath); code != http.StatusOK {
		t.Fatalf("元の待ち受け先で応答しなくなった: got %d", code)
	}
}

// 目的: 実際に listen した状態で HTTP を通して取得できることを確認する。
// httptest だけでは、bind と Serve の結線が正しいことを確かめられない。
// 与える情報: ポート 0（OS に選ばせる）で起動したダッシュボード。
// 成功条件: HTML と JSON の両方が 200 で返り、安全側のヘッダが付いていること。
// Close のあとは接続できなくなること。
func TestStart_実際に待ち受けてHTTPで取得できる(t *testing.T) {
	s, _ := newTestServer(t, sampleViews())
	if err := s.Start(); err != nil {
		t.Fatalf("待ち受けを開始できなかった: %v", err)
	}
	base := "http://" + s.Addr()

	code, body := httpGet(t, base+"/")
	if code != http.StatusOK {
		t.Fatalf("HTML の状態コードが違う: got %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, "maimuzo/continuo#12") {
		t.Error("HTML に issue の識別子が出ていない")
	}

	code, body = httpGet(t, base+server.APIStatePath)
	if code != http.StatusOK {
		t.Fatalf("JSON の状態コードが違う: got %d, want %d", code, http.StatusOK)
	}
	if !strings.Contains(body, `"api_calls"`) {
		t.Error("JSON にトークンの集計が出ていない")
	}

	// 安全側のヘッダ。script を1つも読み込ませない。
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Get(base + "/")
	if err != nil {
		t.Fatalf("取得できなかった: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("Content-Security-Policy が緩い: %q", csp)
	}
	if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options が違う: got %q", got)
	}

	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("閉じられなかった: %v", err)
	}
	if _, err := client.Get(base + "/"); err == nil {
		t.Error("閉じたあとも接続できた")
	}
	// 二重に閉じても落ちないこと（終了の作法が2回走っても壊れない）。
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("二重に閉じてエラーになった: %v", err)
	}
}

// 目的: 組み立てに失敗する入力を弾くことを確認する。
// 与える情報: 範囲外のポート番号と、供給元を渡さない Options。
// 成功条件: どちらもエラーになり、ダッシュボードが返らないこと。
func TestNew_不正な入力を弾く(t *testing.T) {
	badPort := 70000
	if s, err := server.New(server.Options{Port: &badPort, Source: &fakeSource{}}); err == nil {
		_ = s.Close(context.Background())
		t.Error("範囲外のポート番号を受け付けた")
	}
	zero := 0
	if s, err := server.New(server.Options{Port: &zero}); err == nil {
		_ = s.Close(context.Background())
		t.Error("run の供給元が無いのに組み立てられた")
	}
}

// 目的: JSON の取得先が読み取り専用の1本だけであることを、URL の組み立てからも確認する。
// 与える情報: 問い合わせ文字列を付けた取得先。
// 成功条件: 200 が返り、問い合わせ文字列に影響されないこと（操作の引数として解釈しない）。
func TestAPI_問い合わせ文字列を解釈しない(t *testing.T) {
	s, _ := newTestServer(t, sampleViews())
	u := url.URL{Path: server.APIStatePath, RawQuery: "stop=maimuzo/continuo%2312"}
	code, body := get(t, s, http.MethodGet, u.String())
	if code != http.StatusOK {
		t.Fatalf("状態コードが違う: got %d, want %d", code, http.StatusOK)
	}
	var snap server.Snapshot
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		t.Fatalf("JSON を解釈できなかった: %v", err)
	}
	if len(snap.Runs) != 2 {
		t.Errorf("問い合わせ文字列で結果が変わった: got %d 件, want 2 件", len(snap.Runs))
	}
}

// 目的: ループバック以外の宛先（`Host`）で来た要求を断ることを確認する。
// **127.0.0.1 に bind するだけでは DNS rebinding を塞げない。**攻撃者が自分のドメインを
// 127.0.0.1 へ解決させると、そのページから見て同一オリジンになるため CORS では止まらず、
// issue の識別子・タイトル・URL・Status・トークンの消費が読み出される。
// 与える情報: `Host` を攻撃者のドメインにした要求（HTML と JSON の両方）。
// 成功条件: 421 が返り、run の写しを1度も取りに行かず、本文に issue の中身が出ないこと。
func TestHost_ループバック以外の宛先は断る(t *testing.T) {
	for _, path := range []string{"/", server.APIStatePath} {
		s, src := newTestServer(t, sampleViews())
		code, body := getWithHost(t, s, http.MethodGet, path, "attacker.example.com")
		if code != http.StatusMisdirectedRequest {
			t.Errorf("%s の状態コードが違う: got %d, want %d", path, code, http.StatusMisdirectedRequest)
		}
		if src.calls != 0 {
			t.Errorf("%s で run の写しを取りに行った（中身が漏れる）: %d 回", path, src.calls)
		}
		if strings.Contains(body, "maimuzo/continuo#12") {
			t.Errorf("%s の応答に issue の中身が入っている: %q", path, body)
		}
	}
}

// 目的: 手元から使う宛先の書き方を全部受け入れることを確認する。断りすぎると、
// ダッシュボードが人間から使えなくなる。
// 与える情報: `127.0.0.1` / `localhost` / `[::1]` の、ポートあり・なしの書き方。
// 成功条件: どれも 200 が返ること。
func TestHost_手元からの宛先は受け入れる(t *testing.T) {
	for _, host := range []string{
		server.LoopbackHost,
		server.LoopbackHost + ":8787",
		"localhost",
		"LocalHost:8787",
		"[::1]:8787",
	} {
		s, _ := newTestServer(t, sampleViews())
		if code, _ := getWithHost(t, s, http.MethodGet, "/", host); code != http.StatusOK {
			t.Errorf("Host %q を断ってしまった: got %d, want %d", host, code, http.StatusOK)
		}
	}
}

// 目的: 待ち受け中は、宛先のポート番号が自分のものと違う要求も断ることを確認する。
// 与える情報: 実際に listen しているダッシュボードへ、別のポートを書いた `Host` で
// 取りに行く要求。
// 成功条件: 421 が返ること。同じ接続で正しい宛先なら 200 が返ること。
func TestHost_待ち受け中は宛先のポートも照合する(t *testing.T) {
	s, _ := newTestServer(t, sampleViews())
	if err := s.Start(); err != nil {
		t.Fatalf("待ち受けを開始できなかった: %v", err)
	}
	defer func() { _ = s.Close(context.Background()) }()

	_, port, err := net.SplitHostPort(s.Addr())
	if err != nil {
		t.Fatalf("待ち受け先を解釈できなかった（%s）: %v", s.Addr(), err)
	}
	if code, _ := getWithHost(t, s, http.MethodGet, "/", server.LoopbackHost+":"+port); code != http.StatusOK {
		t.Fatalf("自分の宛先を断ってしまった: got %d", code)
	}

	other := "1"
	if port == other {
		other = "2"
	}
	if code, _ := getWithHost(t, s, http.MethodGet, "/", server.LoopbackHost+":"+other); code != http.StatusMisdirectedRequest {
		t.Fatalf("別のポートを書いた宛先を受け入れた: got %d, want %d", code, http.StatusMisdirectedRequest)
	}
}

// 目的: JSON の取得口が `SPEC.md` 13.7.2 の求める `/api/v1/*` にあることを確認する。
// 与える情報: 仕様が最低限として挙げる経路と、実装だけが知っている別の経路。
// 成功条件: `/api/v1/state` が 200 を返し、`/api/runs` は 404 になること。
func TestAPI_取得口は仕様どおりの経路にある(t *testing.T) {
	if server.APIStatePath != "/api/v1/state" {
		t.Fatalf("JSON の取得口が仕様（SPEC.md 13.7.2）と違う: got %q", server.APIStatePath)
	}
	s, _ := newTestServer(t, sampleViews())
	if code, _ := get(t, s, http.MethodGet, server.APIStatePath); code != http.StatusOK {
		t.Errorf("仕様の経路で取れない: got %d, want %d", code, http.StatusOK)
	}
	if code, _ := get(t, s, http.MethodGet, "/api/runs"); code != http.StatusNotFound {
		t.Errorf("仕様に無い経路が生きている: got %d, want %d", code, http.StatusNotFound)
	}
}

// 目的: run の件数の内訳（`SPEC.md` 13.7.2 の `counts`）を出すことを確認する。
// 与える情報: バックオフ中の run 1件と、そうでない run 1件。
// 成功条件: `counts.running` が 1、`counts.retrying` が 1 になること。
func TestAPI_runの件数の内訳を出す(t *testing.T) {
	views := []orchestrator.RunView{
		{Identifier: "maimuzo/continuo#1", State: "In Progress"},
		{Identifier: "maimuzo/continuo#2", State: "In Progress", RetryCount: 1,
			BackoffUntil: testTime.Add(5 * time.Minute)},
	}
	s, _ := newTestServer(t, views)
	_, body := get(t, s, http.MethodGet, server.APIStatePath)
	var snap server.Snapshot
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		t.Fatalf("JSON を解釈できなかった: %v\n%s", err, body)
	}
	if snap.Counts.Running != 1 || snap.Counts.Retrying != 1 {
		t.Fatalf("件数の内訳が違う: got %+v, want {Running:1 Retrying:1}", snap.Counts)
	}
}

// 目的: 「最後に hook を受けた時刻」の欄が、hook を1件も受けていない run で
// 新しい時刻を出さないことを確認する。**stall の時計は hook を受けていなくても進む**ので、
// それを流用すると、固まったエージェントでも「0秒前」と表示されて生死を判断できない。
// 与える情報: hook を1件も受けていない（LastHookAt がゼロ値）が、stall の時計だけが
// いまの時刻まで進んでいる run。
// 成功条件: HTML に「まだ1件も受けていない」と出て、いまの時刻が hook の欄に出ないこと。
// JSON では `last_hook_at` が null になり、`stall_clock_at` には時計の値が入ること。
func Test最後にhookを受けた時刻はstallの時計を流用しない(t *testing.T) {
	views := []orchestrator.RunView{{
		Identifier:   "maimuzo/continuo#1",
		Title:        "hook を1件も返さないまま固まった run",
		State:        "In Progress",
		TurnCount:    2,
		StartedAt:    testTime.Add(-30 * time.Minute),
		StallClockAt: testTime,
	}}
	s, _ := newTestServer(t, views)

	_, html := get(t, s, http.MethodGet, "/")
	if !strings.Contains(html, "まだ1件も受けていない") {
		t.Error("hook を1件も受けていないことが HTML に出ていない")
	}
	if strings.Contains(html, "0秒前") {
		t.Error("hook を1件も受けていないのに「0秒前」と出ている（stall の時計を流用している）")
	}

	_, body := get(t, s, http.MethodGet, server.APIStatePath)
	var snap server.Snapshot
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		t.Fatalf("JSON を解釈できなかった: %v\n%s", err, body)
	}
	if snap.Runs[0].LastHookAt != nil {
		t.Errorf("hook を受けていないのに時刻が入っている: %v", snap.Runs[0].LastHookAt)
	}
	if snap.Runs[0].StallClockAt == nil || !snap.Runs[0].StallClockAt.Equal(testTime) {
		t.Errorf("stall の時計が出ていない: %v", snap.Runs[0].StallClockAt)
	}
}

// 目的: 他のページの iframe へ埋め込めないことを確認する。`frame-ancestors` は
// `default-src` に落ちてこないので、`default-src 'none'` だけでは埋め込みを止められない。
// 与える情報: HTML の取得。
// 成功条件: Content-Security-Policy に `frame-ancestors 'none'` が入っていること。
func TestCSP_他のページに埋め込ませない(t *testing.T) {
	s, _ := newTestServer(t, sampleViews())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = server.LoopbackHost
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	csp := rec.Result().Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("frame-ancestors が無いので iframe に埋め込める: %q", csp)
	}
}

// 目的: Start を2回呼んだときに2本目を断ることを確認する。**`server.port` が 0 のとき、
// 2本目を素通しにすると別のポートで待ち受けてしまい、ログに出した待ち受け先と
// 実際に開いているポートが食い違う。**
// 与える情報: ポート 0 で起動したあと、もう一度 Start を呼ぶ。
// 成功条件: 2本目がエラーになり、待ち受け先が1本目のまま変わらないこと。
func TestStart_2回目は待ち受けずに断る(t *testing.T) {
	s, _ := newTestServer(t, sampleViews())
	if err := s.Start(); err != nil {
		t.Fatalf("待ち受けを開始できなかった: %v", err)
	}
	defer func() { _ = s.Close(context.Background()) }()

	first := s.Addr()
	if err := s.Start(); err == nil {
		t.Fatal("2本目の Start を受け付けてしまった（待ち受け先が2つになる）")
	}
	if got := s.Addr(); got != first {
		t.Fatalf("2本目の Start で待ち受け先が変わった: before %q, after %q", first, got)
	}
	if code, _ := httpGet(t, "http://"+first+server.APIStatePath); code != http.StatusOK {
		t.Fatalf("1本目の待ち受け先が応答しなくなった: got %d", code)
	}
}

// 目的: 閉じたあとに Addr が古い待ち受け先を返さないことを確認する。
// もう繋がらない宛先を人間にもログにも見せないためである。
// 与える情報: 起動して待ち受け先を控えたあと、Close を呼ぶ。
// 成功条件: Close の前は `127.0.0.1:<ポート>` を返し、Close のあとは空文字になること。
func TestClose_閉じたあとの待ち受け先は空になる(t *testing.T) {
	s, _ := newTestServer(t, sampleViews())
	if err := s.Start(); err != nil {
		t.Fatalf("待ち受けを開始できなかった: %v", err)
	}
	if before := s.Addr(); before == "" {
		t.Fatal("待ち受け中なのに待ち受け先が空だった")
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("閉じられなかった: %v", err)
	}
	if after := s.Addr(); after != "" {
		t.Fatalf("閉じたあとも待ち受け先を返した: %q", after)
	}
}

// 目的: 応答の期限が4つとも埋まっていることを確認する。**このサーバは認証を持たない**ので、
// 同じマシンのどのプロセスからでも接続でき、期限が抜けていると1本の接続で goroutine を
// 握られ続ける（ヘッダの期限だけでは、本文をだらだら送る接続も、応答を読まない接続も切れない）。
// 与える情報: 既定で組み立てたダッシュボード。
// 成功条件: ヘッダ・本文・書き出し・待機の4つとも 0 でないこと。
func Test応答の期限が4つとも埋まっている(t *testing.T) {
	s, _ := newTestServer(t, sampleViews())

	readHeader, read, write, idle := s.ResponseTimeouts()
	limits := map[string]time.Duration{
		"ヘッダを読み切るまで": readHeader,
		"本文まで読み切るまで": read,
		"応答を書き終えるまで": write,
		"次の要求を待つ":    idle,
	}
	for name, d := range limits {
		if d <= 0 {
			t.Errorf("%s の期限が入っていない: %v", name, d)
		}
	}
	if readHeader > read {
		t.Errorf("ヘッダの期限が本文の期限より長い: header %v, read %v", readHeader, read)
	}
}
