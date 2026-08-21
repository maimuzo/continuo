package ratelimit

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
)

// TokenSourceKeychain は資格情報を macOS の Keychain から読むことを表す。
//
// **macOS でだけ選べる**（`security` は macOS の標準コマンドで、ほかの OS には無い）。
// 選べるかどうかの検査は internal/config の validate が持つ。
const TokenSourceKeychain = "keychain"

// KeychainService は Claude Code が資格情報を入れている Keychain の項目名である。
//
// 実測（2026-08-21、macOS）: `security find-generic-password -s "Claude Code-credentials" -w`
// が JSON を返し、その `claudeAiOauth` に accessToken / refreshToken / expiresAt /
// refreshTokenExpiresAt / scopes / subscriptionType / rateLimitTier が入っていた。
// **中身の形は `~/.claude/.credentials.json` と同じである**ので、解釈は parseAccessToken に
// 寄せてある。
const KeychainService = "Claude Code-credentials"

// securityBinary は Keychain を読む macOS の標準コマンドである。
const securityBinary = "security"

// DefaultKeychainTimeout は**無人で走る経路**が `security` を待つ上限である。
//
// **上限が無いと、確認のダイアログが出たまま誰も答えないときに巡回のループごと止まる。**
// 値は internal/workspace の trustCheckTimeout（巡回のたびに ghq / git を待つ上限）と
// そろえてある。**無人のプロセスが外部コマンドを待ってよい長さは、この10秒を超えない。**
const DefaultKeychainTimeout = 10 * time.Second

// AllowAccessTimeout は `continuo allow-keychain-access` が `security` を待つ上限である。
//
// **人間がダイアログに答えるのを待つので、無人の経路より長く取る。**
// 値は internal/herdr の DefaultStartupTimeout（人間の手が要る準備を待つ上限）と同じ60秒である。
// **10秒では、ダイアログに気づく前に打ち切られる。**
const AllowAccessTimeout = 60 * time.Second

// keychainWaitDelay は、期限が来て `security` を殺したあと、その入出力の後始末を待つ上限である。
//
// **これが無いと、`security` が孫プロセスへ標準出力を渡していた場合に Wait が返らない。**
// 殺したあとの後始末なので短くてよい。
const keychainWaitDelay = 2 * time.Second

// keychainStderrMax はエラー文へ載せる `security` の標準エラー出力の長さ（文字数）である。
const keychainStderrMax = 200

// ErrKeychainTimeout は `security` が期限内に返らなかったことを表す。
//
// **「読めなかった」と「返ってこなかった」を言い分けるためにある。**返ってこなかった場合は
// 確認のダイアログが出たままである可能性が高く、人間に見せる案内が変わる（設計 3-34b）。
// **この場合も枠の判定は諦める**ので、ErrNoCredentials と一緒に包んで返す。
var ErrKeychainTimeout = errors.New("Keychain の読み取りが期限内に終わりませんでした")

// KeychainProbe は Keychain から読めた資格情報の「項目の名前」だけを持つ。
//
// **値は1つも持たない。**`continuo allow-keychain-access` が画面へ出すのはこの名前だけである。
type KeychainProbe struct {
	// Fields は claudeAiOauth の下にあった項目の名前である（昇順）。**値は含まない。**
	Fields []string
	// HasAccessToken は accessToken があり、空でなかったかどうかである。
	HasAccessToken bool
}

// credentialsPayload は Claude の資格情報の JSON のうち、continuo が読む部分だけを写した型である。
//
// **`~/.claude/.credentials.json` と Keychain の中身は同じ形である**ので、写しは1つしか持たない。
type credentialsPayload struct {
	ClaudeAIOauth struct {
		AccessToken string `json:"accessToken"`
	} `json:"claudeAiOauth"`
}

// parseAccessToken は資格情報の JSON から claudeAiOauth.accessToken を取り出す。
//
// **どこから読んだ JSON かをこの関数は知らない。**出所（ファイルのパス / Keychain の項目名）を
// エラー文へ載せるのは呼び出し側の仕事である。
//
// data: 資格情報の JSON。
// 戻り値の1つ目: accessToken の値。**キーが無ければ空文字**（エラーにはしない）。
// 戻り値の2つ目: JSON として解釈できなかった場合のエラー。**data の中身は載せない。**
func parseAccessToken(data []byte) (string, error) {
	var parsed credentialsPayload
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", err
	}
	return parsed.ClaudeAIOauth.AccessToken, nil
}

// ProbeKeychain は Keychain の資格情報を1回読み、**項目の名前だけ**を返す。
//
// **`continuo allow-keychain-access` のためにある。**Keychain へのアクセスを人間に1回
// 許可させ、そのとき何が読めたのかを見せるのが目的である。**値は1つも返さない。**
//
// ctx: 呼び出しに適用するコンテキスト。
// timeout: `security` を待つ上限。**0 以下なら DefaultKeychainTimeout を使う。**
// 戻り値の1つ目: 読めた項目の名前。
// 戻り値の2つ目: 読めなかった場合のエラー。**どれも ErrNoCredentials を包んでいる。**
// 期限内に返らなかった場合は ErrKeychainTimeout も包む。
func ProbeKeychain(ctx context.Context, timeout time.Duration) (KeychainProbe, error) {
	data, err := runSecurity(ctx, timeout)
	if err != nil {
		return KeychainProbe{}, err
	}

	// **値は json.RawMessage のまま触らない。**ここに入っているのはトークンそのものなので、
	// 取り出すのはキーの名前だけにする。
	var outer struct {
		ClaudeAIOauth map[string]json.RawMessage `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &outer); err != nil {
		return KeychainProbe{}, i18n.Errorf(i18n.KeyRatelimitKeychainParseFailed, ErrNoCredentials, KeychainService, err)
	}
	if outer.ClaudeAIOauth == nil {
		return KeychainProbe{}, i18n.Errorf(i18n.KeyRatelimitKeychainOauthMissing, ErrNoCredentials, KeychainService)
	}

	fields := make([]string, 0, len(outer.ClaudeAIOauth))
	for name := range outer.ClaudeAIOauth {
		fields = append(fields, name)
	}
	// **並び順を固定する。**map の走査順は実行ごとに変わるので、そのまま出すと
	// 同じ環境で2回叩いたときに出力が違って見える。
	sort.Strings(fields)

	token, err := parseAccessToken(data)
	if err != nil {
		return KeychainProbe{}, i18n.Errorf(i18n.KeyRatelimitKeychainParseFailed, ErrNoCredentials, KeychainService, err)
	}
	return KeychainProbe{Fields: fields, HasAccessToken: token != ""}, nil
}

// runSecurity は `security find-generic-password -s "Claude Code-credentials" -w` を1回実行する。
//
// **`security` が PATH に無ければ起動しない**（macOS 以外ではこのコマンドが無い）。
// **期限を過ぎたら殺す。**確認のダイアログが出たまま人間が答えないと `security` は返らないので、
// 上限が無いと無人のプロセスがそこで止まる。
//
// ctx: 呼び出しに適用するコンテキスト。
// timeout: 待つ上限。**0 以下なら DefaultKeychainTimeout を使う。**
// 戻り値の1つ目: `security` の標準出力（資格情報の JSON）。
// 戻り値の2つ目: 起動できなかった・異常終了した・期限内に返らなかった場合のエラー。
// **どれも ErrNoCredentials を包む。標準出力（トークンそのもの）はエラー文に載せない。**
func runSecurity(ctx context.Context, timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		timeout = DefaultKeychainTimeout
	}
	// **無い実行ファイルを起動しに行かない。**macOS 以外では `security` が無いのが普通で、
	// exec の失敗をそのまま見せると「PATH の設定を直せ」という誤った案内になる。
	if _, err := exec.LookPath(securityBinary); err != nil {
		return nil, i18n.Errorf(i18n.KeyRatelimitKeychainBinaryNotFound, ErrNoCredentials, securityBinary)
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, securityBinary, "find-generic-password", "-s", KeychainService, "-w")
	// **殺したあとの後始末にも上限を置く**（keychainWaitDelay のコメント参照）。
	cmd.WaitDelay = keychainWaitDelay

	out, err := cmd.Output()
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return nil, i18n.Errorf(i18n.KeyRatelimitKeychainTimeout,
				ErrNoCredentials, ErrKeychainTimeout, timeout, securityBinary)
		}
		return nil, i18n.Errorf(i18n.KeyRatelimitKeychainRunFailed,
			ErrNoCredentials, securityBinary, KeychainService, err, securityStderr(err))
	}
	return out, nil
}

// securityStderr は `security` の標準エラー出力を、エラー文へ載せられる形にする。
//
// **標準出力（トークンそのもの）は絶対に載せない。**載せてよいのは標準エラー出力だけで、
// そこに出るのは `security: ... The specified item could not be found in the keychain.`
// のような理由の1行である。
//
// err: exec が返したエラー。
// 戻り値: 前に区切りを付けた標準エラー出力（空なら空文字）。
func securityStderr(err error) string {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return ""
	}
	s := strings.TrimSpace(string(exitErr.Stderr))
	if s == "" {
		return ""
	}
	return ": " + truncate([]byte(s), keychainStderrMax)
}

// tokenFromKeychain は macOS の Keychain から accessToken を読む。
//
// **中身の形は `~/.claude/.credentials.json` と同じ**なので、解釈は parseAccessToken に寄せる。
//
// ctx: 呼び出しに適用するコンテキスト。
// 戻り値の1つ目: `claudeAiOauth.accessToken` の値。
// 戻り値の2つ目: 読めない・解釈できない・トークンが空の場合は ErrNoCredentials を包んだエラー。
// **読み取った値そのものはエラー文に載せない。**
func (r *Reader) tokenFromKeychain(ctx context.Context) (string, error) {
	data, err := runSecurity(ctx, r.keychainTimeout)
	if err != nil {
		return "", err
	}
	token, err := parseAccessToken(data)
	if err != nil {
		return "", i18n.Errorf(i18n.KeyRatelimitKeychainParseFailed, ErrNoCredentials, KeychainService, err)
	}
	if token == "" {
		return "", i18n.Errorf(i18n.KeyRatelimitKeychainAccessTokenMissing, ErrNoCredentials, KeychainService)
	}
	return token, nil
}
