package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

// DefaultFileName は WORKFLOW.md の既定のファイル名である（設計 5-1 / SPEC.md 5.1）。
const DefaultFileName = "WORKFLOW.md"

// Loaded は WORKFLOW.md を読み込んだ結果である。
type Loaded struct {
	// Config は front matter をパースし、検証・値の展開まで終えた設定である。
	Config Config
	// PromptTemplate は front matter の下に続く本文（プロンプトのテンプレート文字列）である。
	// この段階ではテンプレートとして解釈しない。文字列のまま保持する。
	PromptTemplate string
	// Path は実際に読み込んだ WORKFLOW.md の絶対パスである。
	Path string
}

// ResolvePath は CLI の位置引数と作業ディレクトリから、読み込む WORKFLOW.md の
// パスを決める（設計 5-1 の探索順）。
//
//  1. CLI の位置引数で明示されたパス
//  2. いまいるディレクトリの WORKFLOW.md
//
// argPath: CLI の位置引数で明示されたパス。空文字なら未指定として扱う。
// workDir: いまいるディレクトリの絶対パス。argPath が相対パスのとき、および argPath が
// 空のときの基準になる。
// 戻り値: 解決した絶対パス。argPath が絶対パスならそのまま、相対パスなら workDir を
// 基準にして絶対パスへ変換する。
//
// 戻り値のパスは、身元ファイルに書いたパスとの一致検査（3-18 / 3-23）や、
// 相対パスの解決の基準（5-1）に使われるため、必ず絶対パスでなければならない。
// したがって workDir を基準にする必要があるのに workDir が絶対パスでない場合はエラーを返す。
func ResolvePath(argPath, workDir string) (string, error) {
	if argPath != "" && filepath.IsAbs(argPath) {
		return argPath, nil
	}
	if !filepath.IsAbs(workDir) {
		return "", fmt.Errorf(
			"WORKFLOW.md の場所を絶対パスで決められません: 基準にする作業ディレクトリ %q が絶対パスではありません",
			workDir,
		)
	}
	if argPath != "" {
		return filepath.Join(workDir, argPath), nil
	}
	return filepath.Join(workDir, DefaultFileName), nil
}

// Load は path にある WORKFLOW.md を読み込み、front matter と本文に分けたうえで
// front matter を検証・展開して返す。
//
// path: 読み込む WORKFLOW.md の絶対パス。
// 戻り値: 検証済みの設定と本文。次のいずれかに該当する場合は起動を止めるエラーを返す
// （CLAUDE.md の指示および設計「その2」「その3」）。
//   - ファイルが読めない
//   - front matter の区切り行が無い、または閉じていない
//   - front matter に未知のキーが含まれる（設計 8-1。仕様は無視すべきとしているが、
//     continuo は意図的に逆にしている。書いたつもりの設定が効いていないことに、
//     無人運用では気づけないため）
//   - front matter の値が型として不正、または値として不正である
//   - 5-5 の展開規則に反する（未定義の環境変数・$ の誤用・~user 形式など）
func Load(path string) (*Loaded, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("WORKFLOW.md を読み込めません: %s: %w", path, err)
	}

	frontMatter, body, err := splitFrontMatter(string(raw))
	if err != nil {
		return nil, fmt.Errorf("%s の front matter を切り出せません: %w", path, err)
	}

	cfg, err := parseFrontMatter(frontMatter)
	if err != nil {
		return nil, fmt.Errorf("%s の front matter が不正です: %w", path, err)
	}

	if err := expandConfig(cfg); err != nil {
		return nil, fmt.Errorf("%s の設定値の展開に失敗しました: %w", path, err)
	}

	// 絶対パスの検査は展開のあとでなければ成立しない（"~/run/continuo.lock" は
	// 展開前には絶対パスに見えないため）。
	if err := validateExpanded(cfg); err != nil {
		return nil, fmt.Errorf("%s の front matter が不正です: %w", path, err)
	}

	return &Loaded{
		Config:         *cfg,
		PromptTemplate: body,
		Path:           path,
	}, nil
}

// parseFrontMatter は YAML 文字列を Config へパースし、意味的な妥当性まで検証する。
//
// frontMatter: front matter の YAML 本体（区切り行を含まない）。
// 戻り値: パース・検証済みの Config。未知のキーがあれば起動を止める
// （yaml.Strict()。goccy/go-yaml が行・桁・ソース抜粋つきのエラーを返す）。
// 型は正しいが値が不正な場合は validate が起動を止める。
//
// 検査の順序は「プレースホルダ → 値の妥当性」に固定してある（設計 3-32）。
// `continuo init` の雛形が置く project_number: 0 は validate の値域の検査でも落ちるため、
// 逆順にすると「まだ埋めていない」ではなく「0より大きい整数にすること」が出てしまう。
func parseFrontMatter(frontMatter string) (*Config, error) {
	cfg := DefaultConfig()
	if err := yaml.UnmarshalWithOptions([]byte(frontMatter), cfg, yaml.Strict()); err != nil {
		return nil, err
	}
	if err := validatePlaceholders(cfg); err != nil {
		return nil, err
	}
	if err := validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
