// Package config_test のうち、このファイルは language の検査を固定する（設計 3-35）。
//
// **画面に出す文言の言語は、設定が主で環境変数 LANG が従である。**書いたつもりの言語が
// 効いていないことに無人運用では誰も気づけないので、資源の無い言語は起動時に落とす。
package config_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/i18n"
)

// 目的: language を書かなかったときの既定が "auto"（環境変数 LANG から決める）であることを確認する。
// 与える情報: language を1行も書いていない front matter。
// 成功条件: config.Load が通り、Language が "auto" になること。
func TestLoad_languageの既定はautoである(t *testing.T) {
	path := writeWorkflow(t, validFrontMatter, "")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("設定を読み込めません: %v", err)
	}
	if loaded.Config.Language != i18n.LangConfigAuto {
		t.Fatalf("language の既定が %q ではなく %q だった", i18n.LangConfigAuto, loaded.Config.Language)
	}
}

// 目的: 資源のある言語を書けば、そのまま読み込めることを確認する。
// 与える情報: language に "ja" / "en" / "auto" を書いた front matter。
// 成功条件: どれも config.Load が通り、書いた値がそのまま入ること。
func TestLoad_資源のある言語は受け付ける(t *testing.T) {
	for _, value := range []string{"auto", "ja", "en"} {
		t.Run(value, func(t *testing.T) {
			path := writeWorkflow(t, validFrontMatter+"language: "+value+"\n", "")

			loaded, err := config.Load(path)
			if err != nil {
				t.Fatalf("language: %s を読み込めません: %v", value, err)
			}
			if loaded.Config.Language != value {
				t.Fatalf("language が %q ではなく %q だった", value, loaded.Config.Language)
			}
		})
	}
}

// 目的: 資源の無い言語を書くと起動が止まることを確認する。
//
// **黙って日本語へ落とさない。**落とすと、書いたつもりの言語が効いていないことに
// 気づけないまま無人で走り続ける。
//
// 与える情報: language に資源の無い言語（fr）を書いた front matter。
// 成功条件: config.Load がエラーを返し、その文に "language" が含まれること。
func TestLoad_資源の無い言語は起動を止める(t *testing.T) {
	assertLoadFailsWith(t, validFrontMatter+"language: fr\n", "language")
}
