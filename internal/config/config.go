package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/maimuzo/continuo/internal/i18n"
)

// DefaultFileName は WORKFLOW.md の既定のファイル名である（設計 5-1 / SPEC.md 5.1）。
const DefaultFileName = "WORKFLOW.md"

// Loaded は WORKFLOW.md を読み込んだ結果である。
type Loaded struct {
	// Config は front matter をパースし、検証・値の展開まで終えた設定である。
	Config Config
	// PromptTemplate は front matter の下に続く本文（プロンプトのテンプレート文字列）である。
	// この段階ではテンプレートとして解釈しない。文字列のまま保持する。
	//
	// **これがエージェントへ送る指示書の、利用者が書く部分である**（設計 5-3c / 5-3d）。
	// 組み込みの前半と後半のあいだへ挟まって送られる。**空でもよい。**
	//
	// **WORKFLOW.md の本文だけを持つ。**WORKFLOW.local.md の本文は LocalPromptTemplate にある。
	// **どちらを送るかは prompt.BodyOf が決める**（取り除く規則が prompt にあり、
	// prompt が config を import しているため、ここでは決められない。設計 5-1a）。
	PromptTemplate string
	// Path は実際に読み込んだ WORKFLOW.md の絶対パスである。
	//
	// **WORKFLOW.local.md を重ねても、ここは WORKFLOW.md のままである。**
	// 身元ファイルとの一致検査（設計 3-18 / 3-23）と読み直しの宛先に使う。
	Path string
	// LocalPath は重ねた WORKFLOW.local.md の絶対パスである（設計 5-1a）。
	// **無かったときは空文字である。**
	LocalPath string
	// LocalPromptTemplate は WORKFLOW.local.md の本文である。LocalPath が空なら空文字である。
	LocalPromptTemplate string
}

// SourceLabel は、設定の出どころを1つの文字列で返す（誤りの文言とログに使う）。
//
// 戻り値: WORKFLOW.local.md を重ねていなければ WORKFLOW.md のパス、
// 重ねていれば両方を名指す文字列。
func (l *Loaded) SourceLabel() string {
	return sourceLabel(l.Path, l.LocalPath)
}

// sourceLabel は、WORKFLOW.md と WORKFLOW.local.md のパスから出どころの文字列を作る。
//
// path: WORKFLOW.md のパス。
// localPath: 重ねた WORKFLOW.local.md のパス。重ねていなければ空文字。
// 戻り値: localPath が空なら path、そうでなければ両方を名指す文字列。
func sourceLabel(path, localPath string) string {
	if localPath == "" {
		return path
	}
	return i18n.T(i18n.KeyConfigLoadMergedLabel, path, localPath)
}

// LocalPathFor は、WORKFLOW.md のパスから、上に重ねる WORKFLOW.local.md のパスを決める（設計 5-1a）。
//
// **同じディレクトリに置き、拡張子の前に `.local` を挟む。**
// `WORKFLOW.md` → `WORKFLOW.local.md`、`team.md` → `team.local.md`、
// 拡張子の無い `WORKFLOW` → `WORKFLOW.local`。拡張子は filepath.Ext が返す最後の1つで、
// 大文字小文字はそのまま使う。
//
// **WORKFLOW.local.md そのものを渡されたら、重ねるものは無い。**判定は、拡張子を除いた名前が
// `.local` で終わるか、拡張子そのものが `.local` か（`team.local.md` / `WORKFLOW.local`）。
// そうしないと `WORKFLOW.local.local.md` を探しに行き、名前の規則が利用者に読めなくなる。
//
// path: WORKFLOW.md の絶対パス。
// 戻り値: 重ねる WORKFLOW.local.md の絶対パス。重ねるものが無ければ空文字。
func LocalPathFor(path string) string {
	dir, base := filepath.Split(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" || ext == ".local" || strings.HasSuffix(stem, ".local") {
		return ""
	}
	return filepath.Join(dir, stem+".local"+ext)
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
		return withFileName(argPath), nil
	}
	if !filepath.IsAbs(workDir) {
		return "", i18n.Errorf(i18n.KeyConfigResolvePathWorkDirNotAbsolute, workDir)
	}
	if argPath != "" {
		return withFileName(filepath.Join(workDir, argPath)), nil
	}
	return filepath.Join(workDir, DefaultFileName), nil
}

// withFileName は、渡されたパスがディレクトリなら、その中の設定ファイルを指す。
//
// **`continuo init <ディレクトリ>` がディレクトリを取るので、他のサブコマンドも揃える。**
// 揃えないと、`continuo doctor <ディレクトリ>` が
// 「`<ディレクトリ>` を読めません: is a directory」で落ち、**設定を読まないまま検査が進む。**
// そのとき言語の設定も効かないので、環境変数の言語で結果が出る（2026-08-23 に実測）。
//
// p: 絶対パス。
// 戻り値: p がディレクトリなら `p/WORKFLOW.md`、そうでなければ p のまま。
// **存在しないパスはそのまま返す**（「無い」ことは呼び出し側が報告する）。
func withFileName(p string) string {
	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		return filepath.Join(p, DefaultFileName)
	}
	return p
}

// Load は path にある WORKFLOW.md を読み込み、front matter と本文に分けたうえで
// front matter を検証・展開して返す。
//
// **同じディレクトリに WORKFLOW.local.md があれば、その front matter を上に重ねる**（設計 5-1a）。
// チームで共有する WORKFLOW.md を書き換えずに、個人の値（週のマージンなど）を変えるためである。
// **無ければ、いままでと同じに動く。**本文は重ねずに両方を返し、どちらを送るかは prompt.BodyOf が決める。
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
//   - path が絶対パスでない（相対パスの解決の基準を決められないため。5-1）
//   - WORKFLOW.local.md が在るのに読めない・区切り行が無い・未知のキーや型の誤りがある
func Load(path string) (*Loaded, error) {
	if !filepath.IsAbs(path) {
		return nil, i18n.Errorf(i18n.KeyConfigLoadPathNotAbsolute, path)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyConfigLoadReadFailed, path, err)
	}

	frontMatter, body, err := splitFrontMatter(string(raw))
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyConfigLoadFrontMatterSplitFailed, path, err)
	}

	cfg := DefaultConfig()
	if err := decodeFrontMatter(frontMatter, cfg); err != nil {
		return nil, i18n.Errorf(i18n.KeyConfigLoadFrontMatterInvalid, path, err)
	}

	// **WORKFLOW.local.md を、同じ cfg の上へ続けて読む**（設計 5-1a）。
	// 誤りは WORKFLOW.local.md のパスで出す。**利用者がチームの WORKFLOW.md を直しに行かないため。**
	local, err := readLocal(LocalPathFor(path))
	if err != nil {
		return nil, err
	}
	if local.path != "" && hasYAMLContent(local.frontMatter) {
		if err := decodeFrontMatter(local.frontMatter, cfg); err != nil {
			return nil, i18n.Errorf(i18n.KeyConfigLoadFrontMatterInvalid, local.path, err)
		}
	}

	// **ここから下の誤りは、重ねた結果の誤りである。**どちらのファイルの値かは分からないので、
	// 重ねていれば両方を名指す。
	label := sourceLabel(path, local.path)
	if err := validateDecoded(cfg); err != nil {
		return nil, i18n.Errorf(i18n.KeyConfigLoadFrontMatterInvalid, label, err)
	}

	if err := expandConfig(cfg); err != nil {
		return nil, i18n.Errorf(i18n.KeyConfigLoadExpandFailed, label, err)
	}

	// 相対パスの解決は展開のあとに行う（"~/worktrees" は展開前には相対パスに見えるため）。
	// **WORKFLOW.local.md は同じディレクトリに置くので、基準は1つで足りる。**
	resolveRelativePaths(filepath.Dir(path), cfg)

	// 絶対パスの検査は展開・相対パスの解決のあとでなければ成立しない
	// （"~/run/hooks.sock" は展開前には絶対パスに見えないため）。
	if err := validateExpanded(cfg); err != nil {
		return nil, i18n.Errorf(i18n.KeyConfigLoadFrontMatterInvalid, label, err)
	}

	return &Loaded{
		Config:              *cfg,
		PromptTemplate:      body,
		Path:                path,
		LocalPath:           local.path,
		LocalPromptTemplate: local.body,
	}, nil
}

// localFile は、読んだ WORKFLOW.local.md を front matter と本文に分けたものである。
type localFile struct {
	// path は読んだ WORKFLOW.local.md の絶対パスである。**無かったときは空文字である。**
	path string
	// frontMatter は front matter の YAML 本体（区切り行を含まない）である。
	frontMatter string
	// body は本文である。
	body string
}

// readLocal は WORKFLOW.local.md を読んで、front matter と本文に分ける（設計 5-1a）。
//
// **無いのは正常である**（issue の求め「無くても動く」）。そのときは空の localFile を返す。
// **在るのに読めない・区切りが無いときは止める。**黙って飛ばすと、個人の上書きが
// 効いていないことに気づけず、チームの値で動き続ける。
//
// localPath: WORKFLOW.local.md の絶対パス。空文字なら重ねるものが無い。
// 戻り値: 分けた結果と、読めなかった・分けられなかった場合の起動を止めるエラー。
func readLocal(localPath string) (localFile, error) {
	if localPath == "" {
		return localFile{}, nil
	}
	raw, err := os.ReadFile(localPath)
	if errors.Is(err, fs.ErrNotExist) && !nameExists(localPath) {
		return localFile{}, nil
	}
	if err != nil {
		return localFile{}, i18n.Errorf(i18n.KeyConfigLoadLocalReadFailed, localPath, err)
	}
	frontMatter, body, err := splitFrontMatter(string(raw))
	if err != nil {
		return localFile{}, i18n.Errorf(i18n.KeyConfigLoadFrontMatterSplitFailed, localPath, err)
	}
	return localFile{path: localPath, frontMatter: frontMatter, body: body}, nil
}

// nameExists は、その名前のディレクトリ項目が在るかを返す（symlink は辿らない）。
//
// **リンク先の無い symlink を「無い」と取り違えないためにある。**`os.ReadFile` はリンク先が無いと
// ENOENT を返すので、それだけで「無い」とすると、dotfiles へリンクして置いた個人の上書きが、
// リンク先を動かしただけで黙って外れる（設計 5-1a の「在るのに読めないときは止める」に反する）。
//
// path: 確かめるパス。
// 戻り値: 名前が在れば真（リンク先の有無は問わない）。
func nameExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// hasYAMLContent は、front matter に値が1つでも書いてあるかを返す（設計 5-1a）。
//
// **空・空行・注釈だけの front matter を構造体へ読ませないためにある。**goccy/go-yaml は
// 空の文書を読むと、読み込み先の構造体を丸ごとゼロ値にする（2026-11-05 に v1.19.2 で実測）。
// WORKFLOW.local.md の front matter を空にしただけで、チームの設定が全部消えてしまう。
//
// **判定には同じライブラリを使う。**別の基準で「空」を決めると、goccy が空とみなす文書を
// 「中身あり」と読んで同じ事故を起こす。`any` へ読んで nil なら空とみなす
// （最上位に null だけを書いた文書も nil になるが、重ねるものが無いのは同じである）。
//
// frontMatter: front matter の YAML 本体。
// 戻り値: 値が1つでも書いてあれば真。**YAML として読めなければ真を返す**
// （続けて読む decodeFrontMatter に、行番号つきの誤りを出させるため）。
func hasYAMLContent(frontMatter string) bool {
	var probe any
	if err := yaml.Unmarshal([]byte(frontMatter), &probe); err != nil {
		return true
	}
	return probe != nil
}

// resolveRelativePaths は、相対パスで書かれた設定値を WORKFLOW.md が置かれている
// ディレクトリを基準に絶対パスへ直す（設計 5-1。SPEC.md 5.3.3 / 5.4）。
//
// **対象は workspace.root だけである。**claude.hook_bridge.listen は
// 絶対パスのまま要求する（validateExpanded が弾く）。身元ファイルに書いたパスとの
// 一致検査（3-23 / 3-18）に使うため、書き手が絶対パスで
// 1つに決めたことを明示していないと困るからである。
// herdr.socket は continuo が作るファイルではなく herdr が待ち受けている場所なので、
// WORKFLOW.md の置き場所を基準にするのは筋が通らない。ここでは触らない。
//
// baseDir: WORKFLOW.md が置かれているディレクトリの絶対パス。
// cfg: 5-5 の展開まで終えた Config。対象キーの値が絶対パスへ書き換わる。
func resolveRelativePaths(baseDir string, cfg *Config) {
	if cfg.Workspace.Root != "" && !filepath.IsAbs(cfg.Workspace.Root) {
		cfg.Workspace.Root = filepath.Join(baseDir, cfg.Workspace.Root)
	}
}

// decodeFrontMatter は YAML 文字列を、既に値の入った Config の上へ読む。
//
// **書いていない欄は、いまの値のまま残る。**だから WORKFLOW.md を読んだ cfg の上へ
// WORKFLOW.local.md をもう一度読むと、書いた分だけが上書きされる（設計 5-1a）。
// 入れ子の欄はキーごとに重なり、map とリストは丸ごと置き換わり、値を書かない行は
// いまの値を残す（ポインタの欄そのものに値を書かない行だけは nil になる）。**WORKFLOW.md に同じ文字を書いたときと同じ意味である。**
//
// frontMatter: front matter の YAML 本体（区切り行を含まない）。
// cfg: 読み込み先。
// 戻り値: 未知のキー・型の誤り（yaml.Strict()。goccy/go-yaml が行・桁・ソース抜粋つきのエラーを返す）。
func decodeFrontMatter(frontMatter string, cfg *Config) error {
	return yaml.UnmarshalWithOptions([]byte(frontMatter), cfg, yaml.Strict())
}

// validateDecoded は、読み終えた Config の意味的な妥当性を検証する。
//
// **WORKFLOW.local.md を重ねたあとで1回だけ呼ぶ。**WORKFLOW.local.md 単独では検査しない
// （`owner` のような必須の値を、個人のファイルに書かなくてよいようにするため。設計 5-1a）。
//
// 検査の順序は「プレースホルダ → 値の妥当性」に固定してある（設計 3-32）。
// `continuo init` の雛形が置く project_number: 0 は validate の値域の検査でも落ちるため、
// 逆順にすると「まだ埋めていない」ではなく「0より大きい整数にすること」が出てしまう。
//
// cfg: 検証する Config。
// 戻り値: 型は正しいが値が不正な場合の起動を止める理由。
func validateDecoded(cfg *Config) error {
	if err := validatePlaceholders(cfg); err != nil {
		return err
	}
	return validate(cfg)
}
