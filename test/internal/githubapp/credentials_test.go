// Package githubapp_test は internal/githubapp（GitHub App の資格情報の読み書きと、
// 更新用のトークンの回転）を公開 API を通して検証する。
//
// **本物の `~/.continuo/` にも本物の GitHub にも触らない。**置き場所は t.TempDir()、
// GitHub は httptest.Server である。
package githubapp_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/githubapp"
	"github.com/maimuzo/continuo/internal/lock"
)

// testExpiry はテストで使う更新用のトークンの期限である。
var testExpiry = time.Date(2027, 3, 9, 0, 0, 0, 0, time.UTC)

// fullCredentials は認可まで通した資格情報の見本である（設計 3-82b の中身の見本と同じ欄）。
func fullCredentials() githubapp.Credentials {
	return githubapp.Credentials{
		ClientID:              "Iv23liexample",
		ClientSecret:          "secret-example",
		RefreshToken:          "ghr_example",
		RefreshTokenExpiresAt: testExpiry,
		AuthorizedLogin:       "octocat",
		Slug:                  "continuo-octocat",
	}
}

// 目的: 資格情報が設計 3-82b の見本と同じキー名で JSON に書かれ、読み戻せることを確認する。
//
// **キー名は人間が読む。**`refresh_token_expires_at` は RFC 3339 でなければ、
// 「いつ切れるか」を人間が見て判断できない。
//
// 与える情報: 認可まで通した資格情報。
// 成功条件: 6つのキーが設計の綴りで書かれ、期限が RFC 3339 の UTC で書かれ、読み戻したものが等しいこと。
func TestCredentials_JSONの形は設計の見本と同じ(t *testing.T) {
	data, err := json.Marshal(fullCredentials())
	if err != nil {
		t.Fatalf("Marshal に失敗した: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("書いた JSON を読めない: %v", err)
	}
	want := map[string]string{
		"client_id":                "Iv23liexample",
		"client_secret":            "secret-example",
		"refresh_token":            "ghr_example",
		"refresh_token_expires_at": "2027-03-09T00:00:00Z",
		"authorized_login":         "octocat",
		"slug":                     "continuo-octocat",
	}
	for k, v := range want {
		if raw[k] != v {
			t.Errorf("キー %q の値が %v（want %q）", k, raw[k], v)
		}
	}
	if len(raw) != len(want) {
		t.Errorf("キーの数が %d（want %d）: %s", len(raw), len(want), data)
	}

	var back githubapp.Credentials
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal に失敗した: %v", err)
	}
	if back != fullCredentials() {
		t.Errorf("読み戻した資格情報が違う: %+v", back)
	}
}

// 目的: GitHub App を作った直後（認可の前）の資格情報に、期限の欄が書かれないことを確認する。
//
// **`time.Time` のゼロ値をそのまま書くと `0001-01-01T00:00:00Z` が入る。**人間が読んで
// 「切れている」と誤解する。
//
// 与える情報: `client_id` / `client_secret` / `slug` だけの資格情報。
// 成功条件: JSON に `refresh_token` と `refresh_token_expires_at` と `authorized_login` が無いこと。
func TestCredentials_認可の前は期限を書かない(t *testing.T) {
	data, err := json.Marshal(githubapp.Credentials{ClientID: "id", ClientSecret: "secret", Slug: "continuo-octocat"})
	if err != nil {
		t.Fatalf("Marshal に失敗した: %v", err)
	}
	for _, absent := range []string{"refresh_token", "refresh_token_expires_at", "authorized_login", "0001-01-01"} {
		if strings.Contains(string(data), absent) {
			t.Errorf("認可の前の資格情報に %q が書かれている: %s", absent, data)
		}
	}
}

// 目的: 期限の文字列が RFC 3339 として読めないとき、黙ってゼロ値にせずにエラーになることを確認する。
// 与える情報: `refresh_token_expires_at` に `tomorrow` と書いた JSON。
// 成功条件: Unmarshal がエラーを返すこと。
func TestCredentials_期限が読めなければエラー(t *testing.T) {
	var c githubapp.Credentials
	err := json.Unmarshal([]byte(`{"refresh_token_expires_at":"tomorrow"}`), &c)
	if err == nil {
		t.Fatal("読めない期限を黙って通した")
	}
}

// 目的: 期限の判定（切れている・30日を切っている・未認可）を確認する（設計 3-82c の doctor の表）。
// 与える情報: 期限の前後と、期限を持たない資格情報。
// 成功条件: 表のとおりの真偽が返ること。
func TestCredentials_期限の判定(t *testing.T) {
	c := fullCredentials()
	if c.RefreshTokenExpired(testExpiry.Add(-time.Hour)) {
		t.Error("期限の1時間前なのに切れていると判定した")
	}
	if !c.RefreshTokenExpired(testExpiry) {
		t.Error("期限ちょうどなのに切れていないと判定した")
	}
	if c.RefreshTokenExpiresSoon(testExpiry.Add(-31 * 24 * time.Hour)) {
		t.Error("残り31日なのに近いと判定した")
	}
	if !c.RefreshTokenExpiresSoon(testExpiry.Add(-29 * 24 * time.Hour)) {
		t.Error("残り29日なのに近いと判定しなかった")
	}
	none := githubapp.Credentials{ClientID: "id", ClientSecret: "secret"}
	if none.RefreshTokenExpired(testExpiry) || none.RefreshTokenExpiresSoon(testExpiry) {
		t.Error("期限を持たない（未認可の）資格情報を、切れている・近いと判定した")
	}
	if none.HasRefreshToken() || !none.HasApp() {
		t.Error("HasApp / HasRefreshToken の判定が違う")
	}
}

// 目的: Store が `<ホーム>/.continuo/github-app-credentials.json` を 0600 で書き、
// ディレクトリを 0700 で作ることを確認する（設計 3-82b「置き場所の決まり」）。
// 与える情報: まだ `.continuo` の無い一時ディレクトリ。
// 成功条件: 書いたあとにディレクトリが 0700、ファイルが 0600 で、Read が同じ内容と 0600 を返すこと。
func TestStore_0700のディレクトリに0600で書く(t *testing.T) {
	home := t.TempDir()
	store := githubapp.NewStore(home)
	if got, want := store.Path(), filepath.Join(home, ".continuo", "github-app-credentials.json"); got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
	if got, want := store.LockPath(), filepath.Join(home, ".continuo", "github-app-credentials.lock"); got != want {
		t.Fatalf("LockPath() = %q, want %q", got, want)
	}

	if err := store.Write(fullCredentials()); err != nil {
		t.Fatalf("Write に失敗した: %v", err)
	}
	dirInfo, err := os.Stat(store.Dir())
	if err != nil {
		t.Fatalf("ディレクトリが無い: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("ディレクトリの権限が %o（want 0700）", perm)
	}
	got, perm, err := store.Read()
	if err != nil {
		t.Fatalf("Read に失敗した: %v", err)
	}
	if perm != githubapp.CredentialsPerm {
		t.Errorf("ファイルの権限が %o（want 0600）", perm)
	}
	if got != fullCredentials() {
		t.Errorf("読み戻した資格情報が違う: %+v", got)
	}
	// 一時ファイルが残っていないこと（一時ファイルへ書いてから差し替える）。
	entries, _ := os.ReadDir(store.Dir())
	for _, e := range entries {
		if e.Name() != githubapp.CredentialsFileName {
			t.Errorf("余計なファイルが残っている: %s", e.Name())
		}
	}
}

// 目的: 既にある `~/.continuo/` の権限を書き換えないことを確認する（設計 3-82b。
// 「continuo は、自分が作っていないディレクトリの権限を書き換えない」）。
// 与える情報: 0755 で作ってある `.continuo`。
// 成功条件: Write のあとも 0755 のままであること。
func TestStore_既にあるディレクトリの権限は変えない(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".continuo")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := githubapp.NewStore(home)
	if err := store.Write(fullCredentials()); err != nil {
		t.Fatalf("Write に失敗した: %v", err)
	}
	info, _ := os.Stat(dir)
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Errorf("既にあるディレクトリの権限が %o に変わった（want 0755）", perm)
	}
}

// 目的: 資格情報が無いときは ErrNotFound を包んだエラーになり、壊れているときは包まないことを確認する。
//
// **起動時の検査はこの2つで文面を変える**（設計 3-82c の2通り）。
//
// 与える情報: ファイルの無い一時ディレクトリと、JSON として壊れたファイル。
// 成功条件: 前者は errors.Is(err, ErrNotFound) が真で Exists が偽、後者は偽で Exists が真。
func TestStore_無いのと壊れているのを言い分ける(t *testing.T) {
	store := githubapp.NewStore(t.TempDir())
	_, _, err := store.Read()
	if !errors.Is(err, githubapp.ErrNotFound) {
		t.Errorf("無いのに ErrNotFound を包んでいない: %v", err)
	}
	if ok, err := store.Exists(); ok || err != nil {
		t.Errorf("Exists = %v, %v（want false, nil）", ok, err)
	}

	if err := os.MkdirAll(store.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = store.Read()
	if err == nil {
		t.Fatal("壊れた JSON を読めてしまった")
	}
	if errors.Is(err, githubapp.ErrNotFound) {
		t.Errorf("壊れているだけなのに ErrNotFound を包んでいる: %v", err)
	}
	if ok, err := store.Exists(); !ok || err != nil {
		t.Errorf("Exists = %v, %v（want true, nil）", ok, err)
	}
}

// 目的: 権限が 0600 でなくても Read は読み、権限を返すことを確認する（設計 3-82b の表。
// 止めるかどうかは呼ぶ側が決める）。
// 与える情報: 0644 で置いた資格情報。
// 成功条件: 読めて、権限として 0644 が返ること。
func TestStore_権限が違っても読んで権限を返す(t *testing.T) {
	store := githubapp.NewStore(t.TempDir())
	if err := store.Write(fullCredentials()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.Path(), 0o644); err != nil {
		t.Fatal(err)
	}
	got, perm, err := store.Read()
	if err != nil {
		t.Fatalf("0644 のファイルを読めない: %v", err)
	}
	if perm != fs.FileMode(0o644) {
		t.Errorf("権限が %o（want 0644）", perm)
	}
	if got.ClientID != "Iv23liexample" {
		t.Errorf("中身が読めていない: %+v", got)
	}
}

// 目的: Store.Lock が資格情報のロックを取り、掴んだままなら上限まで待って番兵を包んだエラーで返ることを確認する
// （設計 3-82d「同時に叩かれたとき」）。
// 与える情報: 1本目のロックを掴んだまま、短い上限で2本目を取る。
// 成功条件: 2本目が lock.ErrAlreadyRunning を包んだエラーを返し、1本目を放したあとは取れること。
func TestStore_ロックは放されるまで待つ(t *testing.T) {
	store := githubapp.NewStore(t.TempDir())
	first, err := store.Lock(time.Second)
	if err != nil {
		t.Fatalf("1本目のロックに失敗した: %v", err)
	}
	if _, err := os.Stat(store.LockPath()); err != nil {
		t.Errorf("ロックファイルが無い: %v", err)
	}
	_, err = store.Lock(200 * time.Millisecond)
	if !errors.Is(err, lock.ErrAlreadyRunning) {
		t.Errorf("掴んだままなのに番兵を包んだエラーにならない: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := store.Lock(time.Second)
	if err != nil {
		t.Fatalf("放したあとに取れない: %v", err)
	}
	_ = second.Release()
}
