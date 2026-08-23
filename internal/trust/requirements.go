package trust

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// settingsRelPath は、リポジトリが権限を要求してくる設定ファイルの、clone からの相対パスである。
//
// **信頼するとこのファイルの `permissions` が効く**（公式ドキュメント `permissions.md`）。
// `continuo trust --dry-run` が見せるのは、まずこれである（3-33）。
const settingsRelPath = ".claude/settings.json"

// mcpRelPath は、リポジトリが MCP サーバーを要求してくるファイルの、clone からの相対パスである。
const mcpRelPath = ".mcp.json"

// maxRequirementFileSize は要求内容を読むファイルの大きさの上限（バイト）である。
//
// **上限が無いと、対象リポジトリに置かれた巨大なファイルをそのままメモリに載せる。**
// 設定ファイルとしては十分に大きい値にしてある。超えたら「読まなかった」と伝える
// （切り詰めて解釈すると、要求されている権限を取りこぼしたまま人間に見せることになる）。
const maxRequirementFileSize = 1 << 20

// MCPServer は `.mcp.json` に書かれた MCP サーバー1つぶんの要約である。
type MCPServer struct {
	// Name は `mcpServers` のキー（サーバー名）である。
	Name string
	// Summary は何を起動するかの1行である（command と args、または url）。
	//
	// **名前だけでは危うさを判断できない。**何が起動されるのかを添える。
	Summary string
}

// Requirements は、そのリポジトリを信頼すると何が効くようになるかである（3-33）。
//
// **信頼のダイアログが人間に見せていたはずの一覧である。**これを見せずに登録すると、
// 人間が中身を確かめる機会が消える。
type Requirements struct {
	// SettingsPath は読んだ `.claude/settings.json` の絶対パスである。
	SettingsPath string
	// SettingsFound はそのファイルがあったかどうかである。
	SettingsFound bool
	// Allow は `permissions.allow` に書かれていた項目である。
	Allow []string
	// AdditionalDirectories は `permissions.additionalDirectories` に書かれていた項目である。
	//
	// **リポジトリの外のディレクトリを読み書きの対象に足すものである。**
	AdditionalDirectories []string
	// MCPPath は読んだ `.mcp.json` の絶対パスである。
	MCPPath string
	// MCPFound はそのファイルがあったかどうかである。
	MCPFound bool
	// MCPServers は `mcpServers` に書かれていたサーバーである（名前の辞書順）。
	MCPServers []MCPServer
	// Notes は、読めなかった・見落としうる点についての1行である。
	//
	// **空にしない運用にはしない。**問題が無ければ空である。
	Notes []string
}

// Empty は、そのリポジトリが何も要求していないかどうかを返す。
//
// 戻り値: allow も additionalDirectories も MCP サーバーも無ければ true。
func (r Requirements) Empty() bool {
	return len(r.Allow) == 0 && len(r.AdditionalDirectories) == 0 && len(r.MCPServers) == 0
}

// readRequirements は clone の中の設定ファイルを読んで、要求内容を組み立てる。
//
// **読むだけである。**ここで何かを書き換えることは無い。
//
// clonePath: clone の絶対パス。
// 戻り値: 要求内容。ファイルが無い場合は、無いという事実を持った Requirements を返す
// （エラーにしない。設定ファイルを持たないリポジトリは普通にある）。
func readRequirements(clonePath string) Requirements {
	req := Requirements{
		SettingsPath: filepath.Join(clonePath, settingsRelPath),
		MCPPath:      filepath.Join(clonePath, mcpRelPath),
	}

	settingsRaw, found, note := readSmallFile(req.SettingsPath)
	req.SettingsFound = found
	if note != "" {
		req.Notes = append(req.Notes, note)
	}
	if found {
		var parsed struct {
			Permissions struct {
				Allow                 []string `json:"allow"`
				AdditionalDirectories []string `json:"additionalDirectories"`
			} `json:"permissions"`
		}
		if err := json.Unmarshal(settingsRaw, &parsed); err != nil {
			req.Notes = append(req.Notes,
				fmt.Sprintf("%s を JSON として読めませんでした（%v）。要求されている権限を確かめられていません", req.SettingsPath, err))
		} else {
			req.Allow = parsed.Permissions.Allow
			req.AdditionalDirectories = parsed.Permissions.AdditionalDirectories
		}
	}

	mcpRaw, found, note := readSmallFile(req.MCPPath)
	req.MCPFound = found
	if note != "" {
		req.Notes = append(req.Notes, note)
	}
	if found {
		var parsed struct {
			MCPServers map[string]struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
				URL     string   `json:"url"`
				Type    string   `json:"type"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal(mcpRaw, &parsed); err != nil {
			req.Notes = append(req.Notes,
				fmt.Sprintf("%s を JSON として読めませんでした（%v）。要求されている MCP サーバーを確かめられていません", req.MCPPath, err))
		} else {
			names := make([]string, 0, len(parsed.MCPServers))
			for name := range parsed.MCPServers {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				s := parsed.MCPServers[name]
				summary := s.URL
				if s.Command != "" {
					summary = strings.TrimSpace(s.Command + " " + strings.Join(s.Args, " "))
				}
				if summary == "" {
					summary = s.Type
				}
				if summary == "" {
					summary = "（起動する内容が書かれていません）"
				}
				req.MCPServers = append(req.MCPServers, MCPServer{Name: name, Summary: summary})
			}
		}
	}

	return req
}

// readSmallFile は上限つきでファイルを読む。
//
// **そのファイル自身が symlink なら、中身を読まずに知らせる。**リポジトリの中の設定
// ファイルが symlink だと、リポジトリの外にあるものを見せられていることになり、
// 「このリポジトリが要求している内容」という前提が崩れる。
//
// **確かめるのは最後の要素だけである。**途中のディレクトリ（`.claude/` そのもの）が
// symlink である場合までは見ていない。**この検査は「見せている中身の出所を人間に伝える」
// ためのものであり、悪意のある配置を全部塞ぐものではない。**
// 最終的に信頼するかどうかを決めるのは、この一覧を読んだ人間である。
//
// path: 読むファイルの絶対パス。
// 戻り値の1つ目: 読んだ中身。
// 戻り値の2つ目: 中身を読めたかどうか。
// 戻り値の3つ目: 人間に伝える1行（何も伝えることが無ければ空文字）。
func readSmallFile(path string) ([]byte, bool, string) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, ""
		}
		return nil, false, fmt.Sprintf("%s を確かめられませんでした（%v）", path, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, false, fmt.Sprintf(
			"%s は symlink です。リポジトリの外のファイルを見せられている可能性があるので、中身を読みませんでした", path)
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Sprintf("%s は通常のファイルではありません。中身を読みませんでした", path)
	}
	if info.Size() > maxRequirementFileSize {
		return nil, false, fmt.Sprintf(
			"%s が %d バイトを超えています（%d バイト）。切り詰めて読むと要求内容を取りこぼすので、読みませんでした",
			path, maxRequirementFileSize, info.Size())
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, false, fmt.Sprintf("%s を開けませんでした（%v）", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxRequirementFileSize+1))
	if err != nil {
		return nil, false, fmt.Sprintf("%s を読めませんでした（%v）", path, err)
	}
	if len(data) > maxRequirementFileSize {
		return nil, false, fmt.Sprintf(
			"%s が %d バイトを超えています。切り詰めて読むと要求内容を取りこぼすので、読みませんでした",
			path, maxRequirementFileSize)
	}
	return data, true, ""
}
