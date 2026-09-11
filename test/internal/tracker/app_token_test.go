package tracker_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/tracker"
)

// このファイルは、PostComment がどのトークンで書くかを決める経路を検証する
// （docs/plans/impl/issue245_github_app_attribution.md の 3-82c「取れないときに止める」と
// 3-82d「continuo 本体の投稿」）。
//
// **本物の GitHub も本物の資格情報も叩かない。**トークンを取る関数は固定の文字列を返す偽物で、
// 投稿先は偽サーバ（fakeGraphQLServer）である。偽サーバは Authorization ヘッダのトークンごとに
// 応答（200 / 401 / 403）を変え、**どのトークンで叩かれたか**を記録する。

const (
	// attHumanToken は NewAdapter へ渡す人間の認証（tracker.provider.token_source）のトークンである。
	attHumanToken = "human-token"
	// attTokenA / attTokenB は、トークンを取る関数が順に返す GitHub App のトークンである。
	attTokenA = "ghu_A"
	attTokenB = "ghu_B"
	// attSelfMarker は本体の投稿に付ける印である（tracker.comments.self_marker の既定）。
	attSelfMarker = "<!-- continuo:self -->"
	// attBody は素の本文である。**改行で終わらない**（末尾の行を壊さないことも同時に見る）。
	attBody = "止まった理由の本文"
	// attFallbackLogText は、書き直しに入ったときに Adapter が Warn で出す文言の一部である。
	attFallbackLogText = "人間の認証で書き直します"
)

// attStatusByToken は Authorization ヘッダのトークンごとに応答を変える responder を作る。
//
// statusByToken: トークン → 返す HTTP ステータス。**表に無いトークンは 401 で落とす**
// （GitHub が知らないトークンに返すのと同じ）。200 なら addComment の成功応答を返し、
// 本文には受け取った body をそのまま写す。
func attStatusByToken(statusByToken map[string]int) func(n int, req capturedRequest) fakeGraphQLResponse {
	return func(n int, req capturedRequest) fakeGraphQLResponse {
		token := strings.TrimPrefix(req.Authorization, "Bearer ")
		status, ok := statusByToken[token]
		if !ok {
			status = http.StatusUnauthorized
		}
		switch status {
		case http.StatusOK:
			body, _ := req.Variables["body"].(string)
			return dataResponse(map[string]any{
				"addComment": map[string]any{
					"commentEdge": map[string]any{
						"node": map[string]any{
							"id": "c-" + token, "url": "https://example.com/c-" + token, "body": body,
							"createdAt": "2026-09-11T00:00:00Z",
							"author":    map[string]any{"login": "octocat"},
						},
					},
				},
			})
		case http.StatusUnauthorized:
			return fakeGraphQLResponse{
				Status: http.StatusUnauthorized,
				Body:   map[string]any{"message": "Bad credentials"},
			}
		case http.StatusForbidden:
			// **本文に "rate limit" を含めない。**含めると classifyHTTPStatus が
			// レートリミットとして分類し、403 の経路を見なくなる。
			return fakeGraphQLResponse{
				Status: http.StatusForbidden,
				Body:   map[string]any{"message": "Resource not accessible by integration"},
			}
		default:
			return fakeGraphQLResponse{
				Status: status,
				Body:   map[string]any{"message": "unexpected"},
			}
		}
	}
}

// attTokenSequence は、呼ばれるたびに tokens を順に返す、トークンを取る関数と、
// 呼ばれた回数を数える口を返す。tokens が尽きたら最後の1本を返し続ける。
//
// **PostComment はこの関数を同じ goroutine から同期で呼ぶ**ので、回数は plain な int でよい。
func attTokenSequence(tokens ...string) (tracker.AppTokenFunc, *int) {
	calls := 0
	fn := func(ctx context.Context) (string, error) {
		calls++
		i := calls - 1
		if i >= len(tokens) {
			i = len(tokens) - 1
		}
		return tokens[i], nil
	}
	return fn, &calls
}

// attFailingToken は、常に err を返す、トークンを取る関数と、呼ばれた回数を数える口を返す。
func attFailingToken(err error) (tracker.AppTokenFunc, *int) {
	calls := 0
	fn := func(ctx context.Context) (string, error) {
		calls++
		return "", err
	}
	return fn, &calls
}

// attLogger は Adapter のログを捕まえる logger と、その書き込み先を返す。
//
// **NewAdapter は接続先が既定と違うことを Warn で1行出す**（偽サーバは既定の GitHub ではない）。
// だからテストは行数ではなく、書き直しの文言（attFallbackLogText）の出現回数を数える。
func attLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

// attNewAdapter は、人間の認証のトークン attHumanToken と、トークンを取る関数 appToken を
// 渡した Adapter を（Bootstrap を呼ばずに）作る。
func attNewAdapter(t *testing.T, fs *fakeGraphQLServer, logger *slog.Logger, appToken tracker.AppTokenFunc) *tracker.Adapter {
	t.Helper()
	a, err := tracker.NewAdapter(testTrackerConfig(), fs.URL(), attHumanToken, nil, logger, nil, appToken)
	if err != nil {
		t.Fatalf("NewAdapter が失敗した: %v", err)
	}
	return a
}

// attPostedBodies は偽サーバが受け取った addComment の本文を、受け取った順に返す。
func attPostedBodies(fs *fakeGraphQLServer) []string {
	reqs := fs.Requests()
	out := make([]string, len(reqs))
	for i, r := range reqs {
		out[i], _ = r.Variables["body"].(string)
	}
	return out
}

// attAuthorizations は偽サーバが受け取った Authorization ヘッダを、受け取った順に返す。
func attAuthorizations(fs *fakeGraphQLServer) []string {
	reqs := fs.Requests()
	out := make([]string, len(reqs))
	for i, r := range reqs {
		out[i] = r.Authorization
	}
	return out
}

// attFallbackBody は、書き直しの本文の期待値（印 → 断り → 素の本文）を組み立てる。
func attFallbackBody(body string) string {
	return attSelfMarker + "\n" + tracker.AppTokenFallbackNote + "\n" + body
}

// 目的: トークンを取る関数が nil なら、いままでどおり人間の認証で1回だけ投稿し、断りを入れない
// ことを確認する（3-82d「`github_app_attribution` が `false` なら、GitHub App のクライアントを
// 作らない」。挙動を1バイトも変えない）。
// 与える情報: appToken が nil の Adapter（人間のトークンは "test-token"）と、self_marker 付きの本文。
// 成功条件: リクエストが1件で、Authorization が `Bearer test-token`。本文が
// `<self_marker>\n<素の本文>` そのもので、断りの1行を含まない。
func TestPostComment_appTokenがnilなら人間の認証で1回だけ投稿する(t *testing.T) {
	fs := newFakeGraphQLServer(t, attStatusByToken(map[string]int{"test-token": http.StatusOK}))
	a := newAdapterForFetch(t, fs)

	comment, err := a.PostComment(t.Context(), "ISSUENODE_1", attBody, attSelfMarker)
	if err != nil {
		t.Fatalf("PostComment が失敗した: %v", err)
	}
	if !comment.IsSelf {
		t.Fatalf("投稿したコメントの IsSelf が true になっていない")
	}
	if got := attAuthorizations(fs); len(got) != 1 || got[0] != "Bearer test-token" {
		t.Fatalf("人間の認証で1回だけ投稿されていない: Authorization=%q", got)
	}
	if got := attPostedBodies(fs)[0]; got != attSelfMarker+"\n"+attBody {
		t.Fatalf("本文が想定と違う: got %q", got)
	}
	if strings.Contains(attPostedBodies(fs)[0], tracker.AppTokenFallbackNote) {
		t.Fatalf("appToken が nil なのに断りが入っている: %q", attPostedBodies(fs)[0])
	}
}

// 目的: トークンを取る関数が GitHub App のトークンを返せば、そのトークンで投稿し、人間の認証では
// 投稿しないことを確認する（3-82d「投稿のたびに作る1本」）。
// 与える情報: 常に "ghu_A" を返す関数。偽サーバは "ghu_A" に 200 を返す。
// 成功条件: リクエストが1件で、Authorization が `Bearer ghu_A`。本文が `<self_marker>\n<素の本文>`
// のまま（断り無し）。トークンを取る関数は1回だけ呼ばれる。戻り値の IsSelf が true。
func TestPostComment_appTokenが取れればそのトークンで投稿する(t *testing.T) {
	fs := newFakeGraphQLServer(t, attStatusByToken(map[string]int{
		attTokenA:     http.StatusOK,
		attHumanToken: http.StatusOK,
	}))
	tokenFn, calls := attTokenSequence(attTokenA)
	logger, _ := attLogger()
	a := attNewAdapter(t, fs, logger, tokenFn)

	comment, err := a.PostComment(t.Context(), "ISSUENODE_1", attBody, attSelfMarker)
	if err != nil {
		t.Fatalf("PostComment が失敗した: %v", err)
	}
	if !comment.IsSelf {
		t.Fatalf("投稿したコメントの IsSelf が true になっていない")
	}
	if got := attAuthorizations(fs); len(got) != 1 || got[0] != "Bearer "+attTokenA {
		t.Fatalf("GitHub App のトークンで1回だけ投稿されていない: Authorization=%q", got)
	}
	if got := attPostedBodies(fs)[0]; got != attSelfMarker+"\n"+attBody {
		t.Fatalf("本文が想定と違う（断りが入っているか、印が崩れている）: got %q", got)
	}
	if *calls != 1 {
		t.Fatalf("トークンを取る関数の呼び出し回数が想定と違う: got %d, want 1", *calls)
	}
}

// 目的: トークンが取れなければ、人間の認証で書き直し、印の直後に断りの1行を挟み、Warn を1行出す
// ことを確認する（3-82c「走行中に取れなければ、人間の認証で書き直す。黙らない」）。
// 与える情報: 常にエラーを返す関数。偽サーバは人間のトークンに 200 を返す。
// 成功条件: リクエストが1件で、Authorization が `Bearer human-token`。本文が
// `<self_marker>\n<断り>\n<素の本文>`（素の本文は改行で終わらないまま）。書き直しの Warn が
// ちょうど1行あり、その行に issue のノード ID と取れなかった理由が載っている。
func TestPostComment_appTokenが取れなければ人間の認証で書き直し断りを挟む(t *testing.T) {
	fs := newFakeGraphQLServer(t, attStatusByToken(map[string]int{attHumanToken: http.StatusOK}))
	tokenFn, calls := attFailingToken(errors.New("資格情報のファイルが無い"))
	logger, logs := attLogger()
	a := attNewAdapter(t, fs, logger, tokenFn)

	comment, err := a.PostComment(t.Context(), "ISSUENODE_1", attBody, attSelfMarker)
	if err != nil {
		t.Fatalf("書き直しが通るはずなのに PostComment が失敗した: %v", err)
	}
	if !comment.IsSelf {
		t.Fatalf("書き直したコメントの IsSelf が true になっていない")
	}
	if got := attAuthorizations(fs); len(got) != 1 || got[0] != "Bearer "+attHumanToken {
		t.Fatalf("人間の認証で1回だけ書き直されていない: Authorization=%q", got)
	}
	if got, want := attPostedBodies(fs)[0], attFallbackBody(attBody); got != want {
		t.Fatalf("書き直した本文が想定と違う:\n got %q\nwant %q", got, want)
	}
	if *calls != 1 {
		t.Fatalf("トークンを取る関数の呼び出し回数が想定と違う: got %d, want 1", *calls)
	}
	if n := strings.Count(logs.String(), attFallbackLogText); n != 1 {
		t.Fatalf("書き直しの Warn が1行ではない: got %d 行\n%s", n, logs.String())
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("書き直しのログが WARN で出ていない:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "ISSUENODE_1") || !strings.Contains(logs.String(), "資格情報のファイルが無い") {
		t.Fatalf("書き直しの Warn に issue のノード ID か理由が載っていない:\n%s", logs.String())
	}
}

// 目的: 1回目が 401 なら、トークンを取り直して1回だけ叩き直し、2回目が通れば断り無しで返ることを
// 確認する（3-82d「401 を受けたら、資格情報を読み直してトークンを取り直し、1回だけ再送する」）。
// 与える情報: "ghu_A" → "ghu_B" の順に返す関数。偽サーバは "ghu_A" に 401、"ghu_B" に 200 を返す。
// 成功条件: リクエストが2件で、Authorization が順に `Bearer ghu_A`・`Bearer ghu_B`。
// 2件目の本文は `<self_marker>\n<素の本文>` のまま（断り無し）。トークンを取る関数は2回呼ばれる。
// 人間の認証では投稿されない。
func TestPostComment_401なら取り直して1回だけ叩き直す(t *testing.T) {
	fs := newFakeGraphQLServer(t, attStatusByToken(map[string]int{
		attTokenA:     http.StatusUnauthorized,
		attTokenB:     http.StatusOK,
		attHumanToken: http.StatusOK,
	}))
	tokenFn, calls := attTokenSequence(attTokenA, attTokenB)
	logger, logs := attLogger()
	a := attNewAdapter(t, fs, logger, tokenFn)

	if _, err := a.PostComment(t.Context(), "ISSUENODE_1", attBody, attSelfMarker); err != nil {
		t.Fatalf("取り直しが通るはずなのに PostComment が失敗した: %v", err)
	}
	want := []string{"Bearer " + attTokenA, "Bearer " + attTokenB}
	if got := attAuthorizations(fs); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("叩いた順序が想定と違う: got %q, want %q", got, want)
	}
	if got := attPostedBodies(fs)[1]; got != attSelfMarker+"\n"+attBody {
		t.Fatalf("取り直した投稿の本文に断りが入っている: got %q", got)
	}
	if *calls != 2 {
		t.Fatalf("トークンを取る関数の呼び出し回数が想定と違う: got %d, want 2", *calls)
	}
	if strings.Contains(logs.String(), attFallbackLogText) {
		t.Fatalf("取り直しで通ったのに書き直しの Warn が出ている:\n%s", logs.String())
	}
}

// 目的: 401 が2回続いたら、それ以上取り直さずに人間の認証で書き直し、断りを挟むことを確認する
// （3-82d「2回目も落ちたら、人間の認証で書き直す」。3-82c「401 が2回続いた」）。
// 与える情報: "ghu_A" → "ghu_B" の順に返す関数。偽サーバはどちらにも 401、人間のトークンに 200 を返す。
// 成功条件: リクエストが3件で、Authorization が順に `Bearer ghu_A`・`Bearer ghu_B`・`Bearer human-token`。
// 3件目の本文に断りが入っている。トークンを取る関数はちょうど2回呼ばれる（3回以上は呼ばれない）。
// 書き直しの Warn が1行あり、ログにトークンの文字（`ghu_`）が無い。
func TestPostComment_401が2回続いたら人間の認証で書き直す(t *testing.T) {
	fs := newFakeGraphQLServer(t, attStatusByToken(map[string]int{
		attTokenA:     http.StatusUnauthorized,
		attTokenB:     http.StatusUnauthorized,
		attHumanToken: http.StatusOK,
	}))
	tokenFn, calls := attTokenSequence(attTokenA, attTokenB)
	logger, logs := attLogger()
	a := attNewAdapter(t, fs, logger, tokenFn)

	if _, err := a.PostComment(t.Context(), "ISSUENODE_1", attBody, attSelfMarker); err != nil {
		t.Fatalf("書き直しが通るはずなのに PostComment が失敗した: %v", err)
	}
	want := []string{"Bearer " + attTokenA, "Bearer " + attTokenB, "Bearer " + attHumanToken}
	if got := attAuthorizations(fs); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("叩いた順序が想定と違う: got %q, want %q", got, want)
	}
	if got, want := attPostedBodies(fs)[2], attFallbackBody(attBody); got != want {
		t.Fatalf("書き直した本文が想定と違う:\n got %q\nwant %q", got, want)
	}
	if *calls != 2 {
		t.Fatalf("トークンを取る関数の呼び出し回数が想定と違う（401 は1回しか取り直さない）: got %d, want 2", *calls)
	}
	if n := strings.Count(logs.String(), attFallbackLogText); n != 1 {
		t.Fatalf("書き直しの Warn が1行ではない: got %d 行\n%s", n, logs.String())
	}
	if strings.Contains(logs.String(), "ghu_") {
		t.Fatalf("ログにトークンの文字が出ている:\n%s", logs.String())
	}
}

// 目的: 403 なら取り直さずに（401 ではないので直らない）人間の認証で書き直し、断りを挟むことを
// 確認する（3-82c「401 だけを見ると、そこで書き直しが発火しない」。理由を問わず書き直す）。
// 与える情報: 常に "ghu_A" を返す関数。偽サーバは "ghu_A" に 403、人間のトークンに 200 を返す。
// 成功条件: リクエストが2件で、Authorization が順に `Bearer ghu_A`・`Bearer human-token`。
// 2件目の本文に断りが入っている。トークンを取る関数は1回だけ呼ばれる。
func TestPostComment_403なら取り直さずに人間の認証で書き直す(t *testing.T) {
	fs := newFakeGraphQLServer(t, attStatusByToken(map[string]int{
		attTokenA:     http.StatusForbidden,
		attHumanToken: http.StatusOK,
	}))
	tokenFn, calls := attTokenSequence(attTokenA)
	logger, logs := attLogger()
	a := attNewAdapter(t, fs, logger, tokenFn)

	if _, err := a.PostComment(t.Context(), "ISSUENODE_1", attBody, attSelfMarker); err != nil {
		t.Fatalf("書き直しが通るはずなのに PostComment が失敗した: %v", err)
	}
	want := []string{"Bearer " + attTokenA, "Bearer " + attHumanToken}
	if got := attAuthorizations(fs); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("叩いた順序が想定と違う: got %q, want %q", got, want)
	}
	if got, want := attPostedBodies(fs)[1], attFallbackBody(attBody); got != want {
		t.Fatalf("書き直した本文が想定と違う:\n got %q\nwant %q", got, want)
	}
	if *calls != 1 {
		t.Fatalf("403 なのにトークンを取り直している: got %d 回, want 1", *calls)
	}
	if n := strings.Count(logs.String(), attFallbackLogText); n != 1 {
		t.Fatalf("書き直しの Warn が1行ではない: got %d 行\n%s", n, logs.String())
	}
}

// 目的: selfMarker が空（持ち回りの4件。入札・hold・released）なら、書き直しても断りを入れない
// ことを確認する（3-82c「`Adapter` は `selfMarker` が空なら断りを入れない」。印の直後に JSON の
// 取り決めが続くので、行を挟むと他の機械が読めなくなる）。
// 与える情報: 常にエラーを返す関数。`<!-- continuo:bid -->` で始まり JSON が続く本文。selfMarker は空。
// 成功条件: 人間の認証で1回投稿され、本文が渡したものと1文字も違わない（断りが無い）。
func TestPostComment_selfMarkerが空なら書き直しても断りを入れない(t *testing.T) {
	const bidBody = "<!-- continuo:bid -->\n{\"holder\":\"octocat\",\"until\":\"2026-09-11T00:00:00Z\"}"
	fs := newFakeGraphQLServer(t, attStatusByToken(map[string]int{attHumanToken: http.StatusOK}))
	tokenFn, _ := attFailingToken(errors.New("資格情報のファイルが無い"))
	logger, logs := attLogger()
	a := attNewAdapter(t, fs, logger, tokenFn)

	if _, err := a.PostComment(t.Context(), "ISSUENODE_1", bidBody, ""); err != nil {
		t.Fatalf("書き直しが通るはずなのに PostComment が失敗した: %v", err)
	}
	if got := attAuthorizations(fs); len(got) != 1 || got[0] != "Bearer "+attHumanToken {
		t.Fatalf("人間の認証で1回だけ書き直されていない: Authorization=%q", got)
	}
	if got := attPostedBodies(fs)[0]; got != bidBody {
		t.Fatalf("selfMarker が空なのに本文が変わっている:\n got %q\nwant %q", got, bidBody)
	}
	if n := strings.Count(logs.String(), attFallbackLogText); n != 1 {
		t.Fatalf("断りは入れなくても書き直しの Warn は1行出るはず: got %d 行\n%s", n, logs.String())
	}
}

// 目的: 印が2行並ぶ本文（`<!-- continuo:self -->` の次に `<!-- continuo:gated:assignee -->`）では、
// 断りが2行目の印の次の行に入ることを確認する（3-82c「先頭に並ぶ印を全部通したあとの行」。
// 「先頭の印の次の行」ではない。印の並びの途中に挟むと、印を HasPrefix で切る判定が外れる）。
// 与える情報: 常にエラーを返す関数。`<!-- continuo:gated:assignee -->\n案内の本文\n` という本文
// （改行で終わる）と self_marker。
// 成功条件: 書き直した本文が `<self_marker>\n<!-- continuo:gated:assignee -->\n<断り>\n案内の本文\n`
// そのもの。
func TestPostComment_印が2行並ぶときは断りを2行目の印の次に入れる(t *testing.T) {
	const gatedBody = "<!-- continuo:gated:assignee -->\n案内の本文\n"
	fs := newFakeGraphQLServer(t, attStatusByToken(map[string]int{attHumanToken: http.StatusOK}))
	tokenFn, _ := attFailingToken(errors.New("資格情報のファイルが無い"))
	logger, _ := attLogger()
	a := attNewAdapter(t, fs, logger, tokenFn)

	if _, err := a.PostComment(t.Context(), "ISSUENODE_1", gatedBody, attSelfMarker); err != nil {
		t.Fatalf("書き直しが通るはずなのに PostComment が失敗した: %v", err)
	}
	want := attSelfMarker + "\n" +
		"<!-- continuo:gated:assignee -->\n" +
		tracker.AppTokenFallbackNote + "\n" +
		"案内の本文\n"
	if got := attPostedBodies(fs)[0]; got != want {
		t.Fatalf("断りの位置が想定と違う:\n got %q\nwant %q", got, want)
	}
}

// 目的: 書き直しも落ちたら、そのエラーが返ることを確認する（3-82c「書き直しも落ちたときだけ、
// 呼び出し側の12箇所がいまと同じ `Warn` を出す」）。
// 与える情報: 常に "ghu_A" を返す関数。偽サーバは "ghu_A" にも人間のトークンにも 403 を返す。
// 成功条件: PostComment がエラーを返し、それが書き直し（人間の認証）の 403 に対応する
// CategoryStatus である。リクエストは2件（GitHub App のトークン → 人間の認証）で、
// トークンを取る関数は1回だけ呼ばれる。
func TestPostComment_書き直しも落ちたらそのエラーを返す(t *testing.T) {
	fs := newFakeGraphQLServer(t, attStatusByToken(map[string]int{
		attTokenA:     http.StatusForbidden,
		attHumanToken: http.StatusForbidden,
	}))
	tokenFn, calls := attTokenSequence(attTokenA)
	logger, _ := attLogger()
	a := attNewAdapter(t, fs, logger, tokenFn)

	comment, err := a.PostComment(t.Context(), "ISSUENODE_1", attBody, attSelfMarker)
	if err == nil {
		t.Fatalf("書き直しも落ちたのに PostComment が成功した")
	}
	if comment != nil {
		t.Fatalf("失敗したのにコメントが返っている: %+v", comment)
	}
	if !tracker.IsCategory(err, tracker.CategoryStatus) {
		t.Fatalf("返ったエラーの分類が書き直しの 403（CategoryStatus）ではない: %v", err)
	}
	want := []string{"Bearer " + attTokenA, "Bearer " + attHumanToken}
	if got := attAuthorizations(fs); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("叩いた順序が想定と違う: got %q, want %q", got, want)
	}
	if *calls != 1 {
		t.Fatalf("トークンを取る関数の呼び出し回数が想定と違う: got %d, want 1", *calls)
	}
}

// 目的: ProbeAppToken は、トークンを取る関数が nil なら何もせずに nil を返すことを確認する
// （3-82c「`github_app_attribution` が false なら何も検査しない」）。
// 与える情報: appToken が nil の Adapter。
// 成功条件: 戻り値が nil で、偽サーバへのリクエストが0件。
func TestProbeAppToken_nilなら何もしない(t *testing.T) {
	fs := newFakeGraphQLServer(t, attStatusByToken(map[string]int{}))
	a := newAdapterForFetch(t, fs)

	if err := a.ProbeAppToken(t.Context()); err != nil {
		t.Fatalf("appToken が nil なのに ProbeAppToken がエラーを返した: %v", err)
	}
	if fs.RequestCount() != 0 {
		t.Fatalf("appToken が nil なのに偽サーバへ %d 件のリクエストが飛んだ", fs.RequestCount())
	}
}

// 目的: ProbeAppToken は、トークンが取れれば nil を返し、取る関数を1回だけ呼び、投稿はしない
// ことを確認する（3-82c「起動時に1回だけ実際にトークンを取り、通らなければ起動しない」。
// 取れたトークンは捨てる）。
// 与える情報: 常に "ghu_A" を返す関数。
// 成功条件: 戻り値が nil。トークンを取る関数が1回だけ呼ばれ、偽サーバへのリクエストが0件。
func TestProbeAppToken_取れればnilを返しトークンは捨てる(t *testing.T) {
	fs := newFakeGraphQLServer(t, attStatusByToken(map[string]int{attTokenA: http.StatusOK}))
	tokenFn, calls := attTokenSequence(attTokenA)
	logger, _ := attLogger()
	a := attNewAdapter(t, fs, logger, tokenFn)

	if err := a.ProbeAppToken(t.Context()); err != nil {
		t.Fatalf("取れるはずなのに ProbeAppToken がエラーを返した: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("トークンを取る関数の呼び出し回数が想定と違う: got %d, want 1", *calls)
	}
	if fs.RequestCount() != 0 {
		t.Fatalf("検査だけなのに偽サーバへ %d 件のリクエストが飛んだ", fs.RequestCount())
	}
}

// 目的: ProbeAppToken は、トークンが取れなければそのエラーをそのまま返すことを確認する
// （起動時の検査が、取れなかった理由をそのまま人間へ見せられるようにするため）。
// 与える情報: 固定のエラーを返す関数。
// 成功条件: 戻り値が errors.Is でその固定のエラーに当たる。
func TestProbeAppToken_取れなければそのエラーを返す(t *testing.T) {
	sentinel := errors.New("資格情報のファイルが無い")
	fs := newFakeGraphQLServer(t, attStatusByToken(map[string]int{}))
	tokenFn, calls := attFailingToken(sentinel)
	logger, _ := attLogger()
	a := attNewAdapter(t, fs, logger, tokenFn)

	err := a.ProbeAppToken(t.Context())
	if !errors.Is(err, sentinel) {
		t.Fatalf("取る関数のエラーがそのまま返っていない: got %v, want %v", err, sentinel)
	}
	if *calls != 1 {
		t.Fatalf("トークンを取る関数の呼び出し回数が想定と違う: got %d, want 1", *calls)
	}
}

// 目的: 断りの1行が、設計で決めた文言そのものであり、bash の `--body "…"` を壊す文字
// （backtick・`$`・二重引用符）を含まないことを確認する（3-82c「断りは、この1文だけである」。
// エージェントの投稿は同じ1文を二重引用符で bash へ渡すので、backtick は command substitution
// として実行され、`$` は展開される）。
// 与える情報: tracker.AppTokenFallbackNote。
// 成功条件: 文言が設計の1文と1文字も違わない。backtick・`$`・二重引用符・改行を含まない。
func TestAppTokenFallbackNote_設計の1文そのもので危険な文字を含まない(t *testing.T) {
	const want = "GitHub App のトークンで投稿できなかったので、attribution 無しで投稿しています。continuo のログと continuo doctor を確かめてください"
	if tracker.AppTokenFallbackNote != want {
		t.Fatalf("断りの文言が設計（3-82c）と違う:\n got %q\nwant %q", tracker.AppTokenFallbackNote, want)
	}
	for _, forbidden := range []string{"`", "$", "\"", "\n"} {
		if strings.Contains(tracker.AppTokenFallbackNote, forbidden) {
			t.Fatalf("断りの文言に %q が入っている（bash の --body \"…\" を壊す）: %q", forbidden, tracker.AppTokenFallbackNote)
		}
	}
}
