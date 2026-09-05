package cli

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/maimuzo/continuo/internal/abandon"
	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/daemon"
	"github.com/maimuzo/continuo/internal/tracker"
)

// promptFetchTimeout は `continuo prompt --show --url` がボードを読むのに使う上限である。
//
// **人間が手で叩いて待つコマンドなので、返らないまま待たせない。**
// **`continuo doctor` と同じ30秒にしてある。**あちらも同じ性質のコマンドである。
const promptFetchTimeout = 30 * time.Second

// promptIssueIdentifier は issue の URL を `<owner>/<repo>#<番号>` の形へ直す。
//
// **分解は書き直さない。**`internal/abandon` の `ParseIssueURL` をそのまま呼ぶ。
// あちらは末尾のスラッシュ・クエリ・フラグメントを落とし、`+42` と `042` を弾き、
// **`pull` の URL を受け付けない**（pull request と issue は番号を共有するので、
// 受け付けると「pull request の URL を貼ったのに issue の文面が出る」ことになる）。
// **書き直すと、その境界条件をどれか1つ落とす。**文言の資源も既にあちらが持っている。
//
// **別の package へ移していない。**移すと文言のキー6本の名前替えと
// `internal/abandon` の検査に及ぶ。**`internal/cli` は既に `internal/abandon` を import している。**
//
// raw: 利用者が渡した issue の URL。
// 戻り値: `<owner>/<repo>#<番号>` の形の識別子と、URL として読めなかった理由。
func promptIssueIdentifier(raw string) (string, error) {
	ref, err := abandon.ParseIssueURL(raw)
	if err != nil {
		return "", err
	}
	return ref.Identifier(), nil
}

// fetchIssueForPrompt は、識別子でボードから issue を1件引く（設計 5-3f）。
//
// **`Bootstrap` を呼ばない。**`FetchIssueByIdentifier` が使うのは
// `owner` / `project_number` / `status_field` の3つを名前のまま渡すことだけで、
// `Bootstrap` が解決する ID の一群を1つも読まない。**1リクエスト節約できる。**
// **前例がある。**`internal/abandon` の `readBoard` も同じく通していない。
//
// **`Bootstrap` は Status の選択肢名の照合もしている。**呼ばないぶん、
// `tracker.provider.status_field` の綴りがボードとずれていると**全件が Status 未設定に見える。**
// **そのため、引けなかったときの文言で `continuo doctor` を案内する**（`runPrompt`）。
//
// **信頼の判定関数は渡さない**（`NewAdapter` の最後の引数が nil）。
// 送る文面は `Issue.Dispatchable` を1つも使わないのに、渡すと
// **issue 1件を引くたびに ghq と git が起動する。**
//
// ctx: 呼び出しに適用するコンテキスト。
// cfg: `WORKFLOW.md` の front matter の tracker セクション。
// endpoint: GraphQL API の接続先。空なら本番の GitHub。**呼び出し側が検査してある。**
// identifier: `<owner>/<repo>#<番号>` の形の識別子。
// 戻り値の1つ目: 見つかった issue。
// 戻り値の2つ目: ボードに載っていて、issue として組み立てられたなら true。
// 戻り値の3つ目: `gh` が無い場合・トークンを引けない場合・ボードを読めない場合のエラー。
func fetchIssueForPrompt(
	ctx context.Context,
	cfg config.TrackerConfig,
	endpoint string,
	identifier string,
) (tracker.Issue, bool, error) {
	if err := tracker.CheckGHAvailable(); err != nil {
		return tracker.Issue{}, false, err
	}
	token, err := tracker.ResolveToken(ctx, cfg.Provider, nil)
	if err != nil {
		return tracker.Issue{}, false, err
	}
	// **logger を捨てる。**nil を渡すと `NewAdapter` が `slog.Default()` を入れ、
	// **`runPrompt` が受け取った `stderr` を通らない生の行が `os.Stderr` へ直接出る。**
	// ボードに載っていない issue では、下の i18n の断りと**同じことを2つの書式で二重に出す。**
	// **姉妹コマンドは2つとも捨てている**（`internal/abandon` と `internal/doctor`）。
	adapter, err := tracker.NewAdapter(
		cfg, endpoint, token, &http.Client{Timeout: daemon.DefaultTrackerTimeout},
		slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		return tracker.Issue{}, false, err
	}
	return adapter.FetchIssueByIdentifier(ctx, identifier)
}
