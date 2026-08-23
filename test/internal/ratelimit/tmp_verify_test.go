package ratelimit_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// 一時検証: i18n へ移した文言が %w の連鎖を保つか。
func Test一時_移した文言がErrNoCredentialsの連鎖を保つ(t *testing.T) {
	keys := []i18n.Key{
		i18n.KeyRatelimitTokenEnvNameEmpty,
		i18n.KeyRatelimitCredentialsFileHomeDirUnknown,
		i18n.KeyRatelimitCredentialsFileAccessTokenMissing,
		i18n.KeyRatelimitCredentialsFileNotRegularFile,
		i18n.KeyRatelimitCredentialsFileReadFailed,
		i18n.KeyRatelimitCredentialsFileParseFailed,
	}
	args := map[i18n.Key][]any{
		i18n.KeyRatelimitTokenEnvNameEmpty:                 {ratelimit.ErrNoCredentials},
		i18n.KeyRatelimitCredentialsFileHomeDirUnknown:     {ratelimit.ErrNoCredentials},
		i18n.KeyRatelimitCredentialsFileAccessTokenMissing: {ratelimit.ErrNoCredentials, "/p"},
		i18n.KeyRatelimitCredentialsFileNotRegularFile:     {ratelimit.ErrNoCredentials, "/p", "Lrwx"},
		i18n.KeyRatelimitCredentialsFileReadFailed:         {ratelimit.ErrNoCredentials, "/p", io.EOF},
		i18n.KeyRatelimitCredentialsFileParseFailed:        {ratelimit.ErrNoCredentials, "/p", io.EOF},
	}
	for _, k := range keys {
		err := i18n.Errorf(k, args[k]...)
		if !errors.Is(err, ratelimit.ErrNoCredentials) {
			t.Fatalf("%s: ErrNoCredentials へ辿れない: %v", k, err)
		}
		msg := err.Error()
		if strings.Contains(msg, "%!") || strings.Contains(msg, "MISSING") || strings.Contains(msg, "EXTRA") {
			t.Fatalf("%s: 書式の当てはめが壊れている: %q", k, msg)
		}
		t.Logf("%s => %s", k, msg)
	}
	// %w が2つある文言で、2つ目の連鎖も保つか。
	err := i18n.Errorf(i18n.KeyRatelimitCredentialsFileParseFailed, ratelimit.ErrNoCredentials, "/p", io.EOF)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("2つ目の %%w が連鎖していない: %v", err)
	}
}

// 一時検証: 移した17件の文言が資源から引けるか（空でないこと）。
func Test一時_移した17件の文言が引ける(t *testing.T) {
	keys := []i18n.Key{
		i18n.KeyRatelimitNewReaderHomeDirFailed, i18n.KeyRatelimitFetchRequestBuildFailed,
		i18n.KeyRatelimitFetchRequestFailed, i18n.KeyRatelimitFetchBodyReadFailed,
		i18n.KeyRatelimitFetchUnexpectedStatus, i18n.KeyRatelimitFetchParseFailed,
		i18n.KeyRatelimitTokenEnvNameEmpty, i18n.KeyRatelimitTokenEnvValueEmpty,
		i18n.KeyRatelimitCredentialsFileHomeDirUnknown, i18n.KeyRatelimitCredentialsFileReadFailed,
		i18n.KeyRatelimitCredentialsFileNotRegularFile, i18n.KeyRatelimitCredentialsFileParseFailed,
		i18n.KeyRatelimitCredentialsFileAccessTokenMissing,
		i18n.KeyServerNewPortOutOfRange, i18n.KeyServerStartListenFailed,
		i18n.KeyServerCloseShutdownFailed, i18n.KeyServerWriteJSONEncodeFailed,
	}
	for _, k := range keys {
		got := i18n.T(k)
		if got == "" || got == string(k) {
			t.Fatalf("%s: 文言を引けていない (got=%q)", k, got)
		}
	}
	t.Logf("17件すべて引けた")
}
