package i18n_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/i18n"
)

// envOf は環境変数を引く偽の関数を作る。
//
// **本物の環境変数を読ませない。**テストを走らせた端末の LANG によって結果が変わると、
// 「設定が主、環境変数が従」であることを確かめられない。
//
// values: 変数名と値の対応。
// 戻り値: os.Getenv と同じ形の関数。
func envOf(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

// 目的: 環境変数 LANG のロケール文字列から言語を取り出せることを確認する（設計 3-35）。
//
// 与える情報: `ja_JP.UTF-8` のような実際に見かける書き方。
// 成功条件: 言語の部分だけが取り出され、資源の無い言語と `C` / `POSIX` は
// **既定の言語（英語）**になること（設計 3-35）。
func TestFromEnv_LANGから言語を決める(t *testing.T) {
	cases := []struct {
		name string
		lang string
		want i18n.Lang
	}{
		{"文字集合つきの日本語", "ja_JP.UTF-8", i18n.LangJA},
		{"文字集合つきの英語", "en_US.UTF-8", i18n.LangEN},
		{"ハイフン区切りの英語", "en-GB", i18n.LangEN},
		{"言語だけ", "en", i18n.LangEN},
		{"修飾子つき", "en_US@euro", i18n.LangEN},
		{"ロケールを使わない指定", "C", i18n.LangEN},
		{"POSIX", "POSIX", i18n.LangEN},
		{"空", "", i18n.LangEN},
		{"資源の無い言語", "fr_FR.UTF-8", i18n.LangEN},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := i18n.FromEnv(envOf(map[string]string{i18n.EnvLangName: c.lang}))
			if got != c.want {
				t.Fatalf("LANG=%q のとき %s ではなく %s になった", c.lang, c.want, got)
			}
		})
	}
}

// 目的: **設定が主、環境変数 LANG が従**であることを確認する（設計 3-35）。
//
// 与える情報: 設定に書いた値と、それと食い違う LANG。
// 成功条件: 設定に言語が書かれていればその言語になり、`auto` と空のときだけ
// LANG から決まること。
func TestResolve_設定が主で環境変数が従である(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		envLang    string
		want       i18n.Lang
		wantErr    bool
	}{
		{"設定が環境変数に勝つ", "en", "ja_JP.UTF-8", i18n.LangEN, false},
		{"設定が環境変数に勝つ（逆向き）", "ja", "en_US.UTF-8", i18n.LangJA, false},
		{"auto なら環境変数で決まる", "auto", "en_US.UTF-8", i18n.LangEN, false},
		{"空でも環境変数で決まる", "", "en_US.UTF-8", i18n.LangEN, false},
		{"auto で LANG が無ければ英語", "auto", "", i18n.LangEN, false},
		{"資源の無い言語は落とす", "fr", "en_US.UTF-8", i18n.LangEN, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := i18n.Resolve(c.configured, envOf(map[string]string{i18n.EnvLangName: c.envLang}))
			if c.wantErr && err == nil {
				t.Fatalf("language=%q は受け付けてはならないのにエラーにならなかった", c.configured)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("language=%q でエラーになった: %v", c.configured, err)
			}
			if got != c.want {
				t.Fatalf("language=%q / LANG=%q のとき %s ではなく %s になった",
					c.configured, c.envLang, c.want, got)
			}
		})
	}
}

// 目的: 資源の無い言語を Use で渡しても、画面に何も出せなくならないことを確認する。
//
// **Resolve が先に弾く経路だが、Use 単体でも既定へ落とす。**
//
// 与える情報: 資源の無い言語。
// 成功条件: いま使う言語が既定（英語）になること。
func TestUse_資源の無い言語は既定へ落ちる(t *testing.T) {
	i18n.Use(i18n.Lang("fr"))
	t.Cleanup(func() { i18n.Use(i18n.DefaultLang) })

	if got := i18n.Current(); got != i18n.DefaultLang {
		t.Fatalf("資源の無い言語を渡したのに %s になった", got)
	}
}

// 目的: 資源を持っている言語が、日本語と英語の2つであることを確認する（設計 3-35）。
//
// 与える情報: なし（埋め込んだ資源そのもの）。
// 成功条件: `en` と `ja` が並ぶこと。**英語を消した／増やしたらここで気づける。**
func TestAvailable_資源のある言語(t *testing.T) {
	want := []i18n.Lang{i18n.LangEN, i18n.LangJA}
	got := i18n.Available()
	if len(got) != len(want) {
		t.Fatalf("資源のある言語が %v ではなく %v だった", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("資源のある言語が %v ではなく %v だった", want, got)
		}
	}
}
