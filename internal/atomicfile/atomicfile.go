// Package atomicfile は、既にあるファイルの書き換えを「同じディレクトリの一時ファイルへ
// 書き切ってから差し替える」に揃える（CLAUDE.md の「絶対に守る制約」4 / 設計 3-59）。
//
// **書き込む先をその場で空にしてから書いてはならない。**途中で落ちると、元の内容が失われる。
// 一時ファイルへ書き切ってから os.Rename で差し替えれば、書き込む先は
// 「古い内容のまま」か「新しい内容」のどちらかにしかならない。
//
// **中身は internal/scaffold/update.go にあった writeAtomically をそのまま移したものである。**
// `continuo setup` で既に動いていた手順であり、新しく考え直してはいない。呼ぶ側が
// internal/scaffold の2箇所と internal/orchestrator の1箇所に増えたので、写しを3つ置く代わりに
// 1つのパッケージへ寄せた。
//
// **i18n のキーも移す前のものを使い続ける。**文言は WORKFLOW.md を名指ししているが、
// キーを増やさないことを優先している。
package atomicfile

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/maimuzo/continuo/internal/i18n"
)

// Write は data を path へ不可分に書き込む。
//
// **同じディレクトリに一時ファイルを作ってから os.Rename する。**別のファイルシステムを
// またぐと os.Rename が使えないので、置き場所は必ず書き込む先と同じディレクトリにする。
// 途中で失敗したときは一時ファイルを消す（消せなかった場合は、書き込みの失敗の理由を優先して返す）。
//
// **差し替えには、書き込む先のディレクトリへの書き込み権限が要る**（設計 3-59）。
// その場で開いて書く実装では要らなかったものなので、書き込む先だけを書けるように
// 用意した場所では、ここで落ちる。
//
// path: 書き込む先の絶対パス。
// data: 書き込む全文。
// perm: 差し替えたあとに設定する権限。**既にあるファイルを書き換えるなら、
// そのファイルの権限をそのまま渡すこと**（渡さないと、利用者が変えた権限が塗り潰される）。
// 戻り値: 書き込みに失敗した理由。成功したら nil。
func Write(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return i18n.Errorf(i18n.KeyScaffoldUpdateTempCreateFailed, dir, err)
	}
	tmp := f.Name()

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return i18n.Errorf(i18n.KeyScaffoldFileWriteFailed, tmp, err)
	}
	// os.CreateTemp は 0600 で作るので、渡された権限に戻す。
	if err := f.Chmod(perm); err != nil {
		f.Close()
		os.Remove(tmp)
		return i18n.Errorf(i18n.KeyScaffoldUpdateChmodFailed, tmp, err)
	}
	// **rename の前に fsync する。**書き込んだ内容がディスクに届く前に rename が
	// 先に届くと、電源が落ちたときに中身の無いファイルが残りうる。
	//
	// **これは移す前からあったものである。**`continuo setup` の書き換えで既に効いていた。
	// **親ディレクトリの fsync は足さない。**「途中で止まる」に備えるのが目的であり、
	// 電源断まで見るなら書き込む先ごとに要否が変わる。
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return i18n.Errorf(i18n.KeyScaffoldUpdateSyncFailed, tmp, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return i18n.Errorf(i18n.KeyScaffoldFileCloseFailed, tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return i18n.Errorf(i18n.KeyScaffoldUpdateRenameFailed, path, err)
	}
	return nil
}
