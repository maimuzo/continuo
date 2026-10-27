package agentplugin

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maimuzo/continuo/internal/atomicfile"
	"github.com/maimuzo/continuo/internal/hookserver"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/plugins"
)

// 片付けの時間である。
const (
	// TempKeep は、書きかけの一時のファイルとディレクトリを消さずに残す時間である。
	//
	// **別の continuo（`--id` の違うもの）が同じ実行時ディレクトリへ書いている最中のものを消さない。**
	// 書き出しは数ミリ秒で終わるので、1時間より古いものは途中で落ちた残りである。
	TempKeep = time.Hour
	// OrphanKeep は、古い版を `.orphaned_at` から消さずに残す時間である。
	//
	// **Claude Code が自分の plugin の古い版に使う決まりと同じ14日である**（公式文書 plugins/loading）。
	// 立て直したあとも、pane の Claude Code は起動したときの `--plugin-dir` を読み続ける。
	OrphanKeep = 14 * 24 * time.Hour
)

// installMu は、1つの continuo の中で書き出しと片付けを1本に絞る鍵である。
//
// **別の continuo との取り合いは、この鍵では守らない。**ファイルごとの差し替えは rename で不可分であり、
// 初めて作るときは一時の場所から rename するので、どちらが先に書いても中身は同じになる。
var installMu sync.Mutex

// hashName は、版のディレクトリの名前（16進の小文字12桁）に当たる。
var hashName = regexp.MustCompile(`^[0-9a-f]{12}$`)

// Install は、plugin を `<runtimeDir>/plugins/<12桁>/continuo-agent-procedures/` へ書き出す。
//
// **Claude Code を起動する前に毎回呼ぶ。**次の順に行う。
//
//  1. 版のディレクトリが無ければ、`plugins/.tmp-<乱数>/` へ書き切ってから rename する。
//     rename に失敗し、版のディレクトリが在れば（別の書き手が先に作った）、一時の方を消して 2 へ進む
//  2. ファイルを1つずつ期待値（埋め込みの中身の置き換える語を埋めたもの）と比べる。
//     種類と権限も `os.Lstat` で比べる（symlink への差し替えと権限の書き換えも「違う」とする）。
//     無いか違えば、同じディレクトリの一時ファイルから rename で差し替える
//  3. 期待値に無いファイル（エージェントが足した `hooks/hooks.json` など）は消す。
//     ただし `os.CreateTemp` の名前の形で1時間より新しいもの（別の continuo が書きかけのもの）は消さない
//
// **ディレクトリごとの差し替えはしない。**中身のあるディレクトリへの rename は macOS で
// `Directory not empty` で失敗し、消してから置くと、走っている run から見てディレクトリが消える瞬間ができる。
//
// runtimeDir: 実行時ディレクトリの絶対パス。
// now: いまの時刻（一時ファイルの新しさを判定する）。
// 戻り値の1つ目: `--plugin-dir` に渡す plugin のディレクトリ。
// 戻り値の2つ目: 書き出せなかった理由。
func Install(runtimeDir string, now time.Time) (string, error) {
	installMu.Lock()
	defer installMu.Unlock()
	if !filepath.IsAbs(runtimeDir) {
		return "", i18n.Errorf(i18n.KeyAgentPluginNotAbs, runtimeDir)
	}
	root := filepath.Join(runtimeDir, PluginsDirName)
	if err := os.MkdirAll(root, dirPerm); err != nil {
		return "", i18n.Errorf(i18n.KeyAgentPluginMkdirFailed, root, err)
	}
	vdir := VersionDir(runtimeDir, hash)
	pdir := PluginDir(runtimeDir)

	info, err := os.Lstat(vdir)
	switch {
	case err == nil && !info.IsDir():
		// **symlink かファイルに差し替えられている。**消して作り直す。
		if err := os.RemoveAll(vdir); err != nil {
			return "", i18n.Errorf(i18n.KeyAgentPluginRemoveFailed, vdir, err)
		}
		if err := installFresh(root, vdir, pdir); err != nil {
			return "", err
		}
	case errors.Is(err, fs.ErrNotExist):
		if err := installFresh(root, vdir, pdir); err != nil {
			return "", err
		}
	case err != nil:
		return "", i18n.Errorf(i18n.KeyAgentPluginStatFailed, vdir, err)
	default:
		if info.Mode().Perm() != dirPerm {
			if err := os.Chmod(vdir, dirPerm); err != nil {
				return "", i18n.Errorf(i18n.KeyAgentPluginChmodFailed, vdir, err)
			}
		}
	}
	if err := syncTree(pdir, now); err != nil {
		return "", err
	}
	return pdir, nil
}

// installFresh は、初めての版を一時の場所へ書き切ってから rename で置く。
//
// root: `<runtimeDir>/plugins`。
// vdir: 版のディレクトリ。
// pdir: plugin のディレクトリ（中身の置き換える語に埋めるパス）。
// 戻り値: 置けなかった理由。**別の書き手が先に置いていたときは nil である。**
func installFresh(root, vdir, pdir string) error {
	tmp, err := os.MkdirTemp(root, TmpPrefix+"*")
	if err != nil {
		return i18n.Errorf(i18n.KeyAgentPluginMkdirFailed, root, err)
	}
	if err := os.Chmod(tmp, dirPerm); err != nil {
		_ = os.RemoveAll(tmp)
		return i18n.Errorf(i18n.KeyAgentPluginChmodFailed, tmp, err)
	}
	if err := writeTree(filepath.Join(tmp, plugins.AgentProceduresName), pdir); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, vdir); err != nil {
		_ = os.RemoveAll(tmp)
		if _, statErr := os.Lstat(vdir); statErr == nil {
			// **別の書き手が先に置いた。**中身は次の確かめで期待値と比べる。
			return nil
		}
		return i18n.Errorf(i18n.KeyAgentPluginRenameFailed, tmp, vdir, err)
	}
	return nil
}

// writeTree は、空の場所へ plugin の中身を全部書く。
//
// dst: 書く先（まだ無いディレクトリ）。
// pdir: 中身の置き換える語に埋める、最終の plugin のディレクトリ。
// 戻り値: 書けなかった理由。
func writeTree(dst, pdir string) error {
	if err := mkdirPerm(dst); err != nil {
		return err
	}
	for _, d := range dirs {
		if err := mkdirPerm(filepath.Join(dst, filepath.FromSlash(d))); err != nil {
			return err
		}
	}
	for _, f := range files {
		p := filepath.Join(dst, filepath.FromSlash(f.rel))
		if err := atomicfile.Write(p, Expand(f.data, pdir), f.perm); err != nil {
			return i18n.Errorf(i18n.KeyAgentPluginWriteFailed, p, err)
		}
	}
	return nil
}

// mkdirPerm は、ディレクトリを 0755 で作る（umask に左右されないよう、作ったあとに権限を決め直す）。
//
// p: 作るディレクトリ。**親は在ること。**
// 戻り値: 作れなかった理由。
func mkdirPerm(p string) error {
	if err := os.Mkdir(p, dirPerm); err != nil && !errors.Is(err, fs.ErrExist) {
		return i18n.Errorf(i18n.KeyAgentPluginMkdirFailed, p, err)
	}
	if err := os.Chmod(p, dirPerm); err != nil {
		return i18n.Errorf(i18n.KeyAgentPluginChmodFailed, p, err)
	}
	return nil
}

// syncTree は、plugin のディレクトリの中身を期待値へ揃える（Install の段2と段3）。
//
// pdir: plugin のディレクトリ。
// now: いまの時刻。
// 戻り値: 揃えられなかった理由。
func syncTree(pdir string, now time.Time) error {
	if err := ensureRealDir(pdir); err != nil {
		return err
	}
	wantFiles := make(map[string]embedded, len(files))
	for _, f := range files {
		wantFiles[f.rel] = f
	}
	wantDirs := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		wantDirs[d] = true
	}
	seen := make(map[string]bool, len(files)+len(dirs))
	if err := syncDir(pdir, "", pdir, wantFiles, wantDirs, seen, now); err != nil {
		return err
	}
	// **無いものを作る。**ディレクトリは親から順に作る（dirs は名前の順に並んでいる）。
	for _, d := range dirs {
		if seen[d] {
			continue
		}
		if err := mkdirPerm(filepath.Join(pdir, filepath.FromSlash(d))); err != nil {
			return err
		}
	}
	for _, f := range files {
		if seen[f.rel] {
			continue
		}
		p := filepath.Join(pdir, filepath.FromSlash(f.rel))
		if err := atomicfile.Write(p, Expand(f.data, pdir), f.perm); err != nil {
			return i18n.Errorf(i18n.KeyAgentPluginWriteFailed, p, err)
		}
	}
	return nil
}

// ensureRealDir は、p が symlink ではない本物のディレクトリで、権限が 0755 であることを保証する。
//
// p: 確かめるディレクトリ。
// 戻り値: 保証できなかった理由。
func ensureRealDir(p string) error {
	info, err := os.Lstat(p)
	if err == nil && !info.IsDir() {
		if err := os.RemoveAll(p); err != nil {
			return i18n.Errorf(i18n.KeyAgentPluginRemoveFailed, p, err)
		}
		return mkdirPerm(p)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return mkdirPerm(p)
	}
	if err != nil {
		return i18n.Errorf(i18n.KeyAgentPluginStatFailed, p, err)
	}
	if info.Mode().Perm() != dirPerm {
		if err := os.Chmod(p, dirPerm); err != nil {
			return i18n.Errorf(i18n.KeyAgentPluginChmodFailed, p, err)
		}
	}
	return nil
}

// syncDir は、ディレクトリ1つの中身を期待値と比べ、違うものを差し替え、余分なものを消す。
//
// abs: 見るディレクトリの絶対パス。
// rel: plugin のディレクトリからの相対パス（`/` 区切り。plugin のディレクトリ自身は空文字）。
// pdir: plugin のディレクトリ（中身の置き換える語に埋めるパス）。
// wantFiles / wantDirs: 期待値。
// seen: 期待値のうち、揃っていることを確かめたもの（書き足す）。
// now: いまの時刻。
// 戻り値: 揃えられなかった理由。
func syncDir(abs, rel, pdir string, wantFiles map[string]embedded, wantDirs map[string]bool,
	seen map[string]bool, now time.Time,
) error {
	entries, err := os.ReadDir(abs)
	if err != nil {
		return i18n.Errorf(i18n.KeyAgentPluginReadDirFailed, abs, err)
	}
	for _, e := range entries {
		childAbs := filepath.Join(abs, e.Name())
		childRel := e.Name()
		if rel != "" {
			childRel = rel + "/" + e.Name()
		}
		info, err := os.Lstat(childAbs)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return i18n.Errorf(i18n.KeyAgentPluginStatFailed, childAbs, err)
		}
		switch {
		case wantDirs[childRel]:
			if !info.IsDir() {
				// **symlink かファイルに差し替えられている。**消して、あとで作り直す。
				if err := os.RemoveAll(childAbs); err != nil {
					return i18n.Errorf(i18n.KeyAgentPluginRemoveFailed, childAbs, err)
				}
				continue
			}
			if info.Mode().Perm() != dirPerm {
				if err := os.Chmod(childAbs, dirPerm); err != nil {
					return i18n.Errorf(i18n.KeyAgentPluginChmodFailed, childAbs, err)
				}
			}
			seen[childRel] = true
			if err := syncDir(childAbs, childRel, pdir, wantFiles, wantDirs, seen, now); err != nil {
				return err
			}
		case hasFile(wantFiles, childRel):
			f := wantFiles[childRel]
			want := Expand(f.data, pdir)
			if info.Mode().IsRegular() && info.Mode().Perm() == f.perm && sameContent(childAbs, want) {
				seen[childRel] = true
				continue
			}
			if !info.Mode().IsRegular() {
				// **ディレクトリや symlink に差し替えられている。**rename では上書きできないので消す。
				if err := os.RemoveAll(childAbs); err != nil {
					return i18n.Errorf(i18n.KeyAgentPluginRemoveFailed, childAbs, err)
				}
			}
			if err := atomicfile.Write(childAbs, want, f.perm); err != nil {
				return i18n.Errorf(i18n.KeyAgentPluginWriteFailed, childAbs, err)
			}
			seen[childRel] = true
		default:
			if info.Mode().IsRegular() && atomicfile.IsTempName(e.Name()) && now.Sub(info.ModTime()) < TempKeep {
				// **別の continuo が書きかけの一時ファイルである。**消さない。
				continue
			}
			if err := os.RemoveAll(childAbs); err != nil {
				return i18n.Errorf(i18n.KeyAgentPluginRemoveFailed, childAbs, err)
			}
		}
	}
	return nil
}

// hasFile は、期待値にそのファイルがあるかを返す。
//
// want: 期待値。
// rel: 相対パス。
// 戻り値: あれば真。
func hasFile(want map[string]embedded, rel string) bool {
	_, ok := want[rel]
	return ok
}

// sameContent は、ファイルの中身が want と同じかを返す。
//
// p: 読むファイル。
// want: 期待する中身。
// 戻り値: 読めて同じなら真。**読めなければ偽**（書き直させる）。
func sameContent(p string, want []byte) bool {
	got, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	return bytes.Equal(got, want)
}

// Cleanup は、古い版と書きかけの残りを片付ける。
//
// **書き出すたびに（版が変わらなくても）呼ぶ。**次の4つを行う。
//
//  1. いまの版のディレクトリから `.orphaned_at` を消す（別の continuo が古い版と見なして置いたもの）
//  2. いまの版以外の版のディレクトリ（`.tmp-*` を除く）に `.orphaned_at` が無ければ、いまの時刻で置く
//  3. `.orphaned_at` から14日を過ぎ、どの `issues/*/system-prompt.md` もその12桁を載せていない版だけを、
//     消す直前に `.orphaned_at` が残っているかを読み直してから消す
//     （別の continuo がその版で起動して印を消していれば消さない）
//  4. 1時間より古い `.tmp-*` を消す（書き出しの途中で落ちた残り）
//
// **12桁の16進でない名前のものには触らない。**continuo が作ったと言い切れないためである。
//
// runtimeDir: 実行時ディレクトリの絶対パス。
// now: いまの時刻。
// 戻り値: 片付けられなかったものの理由（まとめたもの）。**片付けは run を止めない**ので、呼び出し側は警告に出す。
func Cleanup(runtimeDir string, now time.Time) error {
	installMu.Lock()
	defer installMu.Unlock()
	root := filepath.Join(runtimeDir, PluginsDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return i18n.Errorf(i18n.KeyAgentPluginReadDirFailed, root, err)
	}
	var errs []error
	if err := os.Remove(filepath.Join(root, hash, OrphanedAtName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, i18n.Errorf(i18n.KeyAgentPluginOrphanFailed, filepath.Join(root, hash, OrphanedAtName), err))
	}
	var prompts []string
	promptsRead := false
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(root, name)
		info, err := os.Lstat(full)
		if err != nil {
			continue
		}
		if strings.HasPrefix(name, TmpPrefix) {
			if now.Sub(info.ModTime()) > TempKeep {
				if err := os.RemoveAll(full); err != nil {
					errs = append(errs, i18n.Errorf(i18n.KeyAgentPluginOrphanFailed, full, err))
				}
			}
			continue
		}
		if name == hash || !hashName.MatchString(name) || !info.IsDir() {
			continue
		}
		marker := filepath.Join(full, OrphanedAtName)
		at, ok, err := readOrphanedAt(marker)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !ok {
			if err := atomicfile.Write(marker, []byte(now.UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
				errs = append(errs, i18n.Errorf(i18n.KeyAgentPluginOrphanFailed, marker, err))
			}
			continue
		}
		if now.Sub(at) <= OrphanKeep {
			continue
		}
		if !promptsRead {
			prompts, err = systemPrompts(runtimeDir)
			if err != nil {
				// **どの版が使われているかを確かめられない。**消さない。
				errs = append(errs, err)
				return errors.Join(errs...)
			}
			promptsRead = true
		}
		if referenced(prompts, name) {
			continue
		}
		// **消す直前に読み直す。**別の continuo がこの版で起動していれば、印は消えている。
		if _, err := os.Lstat(marker); err != nil {
			continue
		}
		if err := os.RemoveAll(full); err != nil {
			errs = append(errs, i18n.Errorf(i18n.KeyAgentPluginOrphanFailed, full, err))
		}
	}
	return errors.Join(errs...)
}

// readOrphanedAt は、`.orphaned_at` の時刻を読む。
//
// **中身を RFC 3339 で読めなければ、ファイルの更新時刻を使う。**
//
// p: `.orphaned_at` のパス。
// 戻り値の1つ目: 古い版になった時刻。
// 戻り値の2つ目: ファイルが在れば真。
// 戻り値の3つ目: 在るかどうかを確かめられなかった理由。
func readOrphanedAt(p string) (time.Time, bool, error) {
	info, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, i18n.Errorf(i18n.KeyAgentPluginOrphanFailed, p, err)
	}
	data, err := os.ReadFile(p)
	if err == nil {
		if t, perr := time.Parse(time.RFC3339, strings.TrimSpace(string(data))); perr == nil {
			return t, true, nil
		}
	}
	return info.ModTime(), true, nil
}

// systemPrompts は、`issues/*/system-prompt.md` の中身を全部読む。
//
// **system-prompt.md は worktree の片付けで消える**ので、direct chat のように14日より長く生きる
// pane の版も、その issue の system-prompt.md が残っているあいだは消さない。
//
// runtimeDir: 実行時ディレクトリ。
// 戻り値の1つ目: 中身の並び。
// 戻り値の2つ目: 読めないファイルがあった理由（そのときは何も消さない）。
func systemPrompts(runtimeDir string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(runtimeDir, hookserver.IssuesDirName, "*", SystemPromptFileName))
	if err != nil {
		return nil, i18n.Errorf(i18n.KeyAgentPluginSystemPromptReadFailed, filepath.Join(runtimeDir, hookserver.IssuesDirName), err)
	}
	sort.Strings(paths)
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, i18n.Errorf(i18n.KeyAgentPluginSystemPromptReadFailed, p, err)
		}
		out = append(out, string(data))
	}
	return out, nil
}

// referenced は、どれかのシステムプロンプトがその版のディレクトリ（`plugins/<12桁>/`）を載せているかを返す。
//
// **12桁だけでは探さない。**システムプロンプトに別の16進の並びが入っていたときに、
// 使っていない版を使っているとみなして消し損ねる。
//
// prompts: システムプロンプトの中身の並び。
// h: 版の12桁。
// 戻り値: 載っていれば真。
func referenced(prompts []string, h string) bool {
	sep := string(filepath.Separator)
	needle := "plugins" + sep + h + sep
	for _, p := range prompts {
		if strings.Contains(p, needle) {
			return true
		}
	}
	return false
}
