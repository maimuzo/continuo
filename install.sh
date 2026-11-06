#!/bin/sh
# continuo のネットワークインストーラーである（設計 3-36）。
#
# **利用者が叩くのは1行だけである。**
#
#   curl -fsSL https://raw.githubusercontent.com/maimuzo/continuo/main/install.sh | sh
#
# **やること。**
#   1. OS と命令セットを見分ける（Windows なら WSL2 を案内して止まる）
#   2. GitHub の release から、その組み合わせの実行ファイルを取る
#      （**既定では公開から 168 時間経った版だけを入れる。**クールダウン。設計 3-36）
#   3. 前提の道具が揃っているかを調べ、**足りないものは1つずつ聞いてから**入れる
#   4. `~/.local/bin/continuo` へ置き、PATH の通し方を案内する
#      （**クールダウンで選んだ版がいま入っている版より古ければ、既定では置き換えない**）
#   5. **上げるときに破壊的変更があれば、入れたうえで名指しで知らせる**（止めない）
#
# **`herdr` と `claude` は入れない。**どちらも独自の配布経路と認証があり、
# 勝手に入れると利用者の既存の設定を壊しうる。足りないことを告げて案内するだけである。
#
# **オプション。**
#   --yes       すべての確認に「はい」と答える（依存も入れる）
#   --no-deps   依存を1つも入れない（不足を並べるだけ）
#   --dir DIR   置き先を変える（既定 ~/.local/bin）
#   --version V 版を指定する（既定は、公開から 168 時間経った最新の正式版）
#   --cooldown-hours H
#               公開から H 時間経った版だけを入れる（既定 168 = 7日。0 で最新版）
#   --force     選んだ版が、いま入っている版より古くても入れ替える
#
# POSIX sh で書く。**bash を前提にしない**（Linux の /bin/sh は dash のことが多い）。
#
# **日本語の文言に変数を埋めるときは、必ず `${NAME}` と波括弧で囲むこと。**
# `${actual}）` のように全角文字が続くと、シェルが `actual）` を変数名として読み、
# `set -u` の下で `unbound variable` になる（2026-08-21 に実測）。

set -eu

# ---------------------------------------------------------------------------
# 設定
# ---------------------------------------------------------------------------

# REPO は取得先である。
#
# **環境変数からは読まない。**`curl … | CONTINUO_REPO=… sh` の1行を貼らせるだけで、
# **警告も出ないまま別のリポジトリの実行ファイルを置けてしまう**（実測で確かめた）。
# fork した人は `--repo` フラグを使う。**使うと警告が出る。**
REPO="maimuzo/continuo"

# DEFAULT_REPO は既定の取得先である。**これと違えば「差し替えられている」と判断する。**
DEFAULT_REPO="$REPO"

# INSTALL_DIR は実行ファイルの置き先である。
INSTALL_DIR="${CONTINUO_INSTALL_DIR:-$HOME/.local/bin}"

# ASSUME_YES が 1 なら、すべての確認に「はい」と答える。
ASSUME_YES=0
# SKIP_DEPS が 1 なら、依存を1つも入れない。
SKIP_DEPS=0
# VERSION が空なら、release の一覧からクールダウンを当てて選ぶ。
VERSION=""
# VERSION_GIVEN が 1 なら、`--version` で版を名指しされている（クールダウンを使わない）。
VERSION_GIVEN=0

# COOLDOWN_HOURS は、公開から何時間経った版だけを入れるかである（設計 3-36）。
#
# **環境変数からは読まない。**取得先と同じく、`curl … | VAR=… sh` を貼らせるだけで
# 挙動が変わる入口を増やさない。外すのは利用者に見える形（引数）だけにする。
DEFAULT_COOLDOWN_HOURS=168
COOLDOWN_HOURS="$DEFAULT_COOLDOWN_HOURS"

# FORCE が 1 なら、選んだ版がいま入っている版より古くても入れ替える。
FORCE=0

# BASE_URL は実行ファイルの取得先である。
# API_URL は、`/releases/latest` で終わる問い合わせ先である。
# **これ自体はもう引かない。**末尾の `/latest` を落として、release の一覧（RELEASES_URL）を組み立てるのに使う。
#
# **どちらも固定である。**環境変数では差し替えられない。
# **`--base-url` を渡したときだけ差し替わり、そのときは大きな警告を出す。**
#
# 環境変数で差し替えられるようにしていた版では、`curl … | CONTINUO_BASE_URL=http://… sh`
# の1行を貼らせるだけで、偽の実行ファイルを置けることが実測で確かめられた。
# **貼られた1行の先頭は公式のままなので、利用者からは正規に見える。**
# **引数を解釈したあとに組み立てる**（`--repo` を反映するため）。set_urls が行う。
BASE_URL=""
API_URL=""

# RELEASES_URL は release の一覧を引く先である。
#
# **版を指定しない実行では、入れる版をこの一覧から決める**（クールダウン。resolve_version）。
# 破壊的変更の印も、同じ一覧から読む（collect_breaking）。
#
# **API_URL から組み立てる。**`--api-url` でテストの偽サーバへ向けたときも、
# 同じ規則（末尾の `/latest` を落とす）で一覧に辿り着ける。set_urls が行う。
RELEASES_URL=""

# INSECURE_SOURCE が 1 なら、取得先が差し替えられている（テスト専用）。
INSECURE_SOURCE=0
# ALLOW_NO_CHECKSUM が 1 なら、チェックサムを照合できなくても続ける。
ALLOW_NO_CHECKSUM=0

# ---------------------------------------------------------------------------
# 表示
# ---------------------------------------------------------------------------

# say は普通の案内を出す。
say() {
	printf '%s\n' "$*"
}

# warn は注意を標準エラーへ出す。
warn() {
	printf '警告: %s\n' "$*" >&2
}

# die は理由を出して終わる。
die() {
	printf 'エラー: %s\n' "$*" >&2
	exit 1
}

# have はコマンドがあるかを返す。
have() {
	command -v "$1" > /dev/null 2>&1
}

# ask は「はい / いいえ」を尋ねる。
#
# **`curl … | sh` では標準入力がパイプなので `read` が使えない。**端末から直接読む。
# 端末が無ければ「いいえ」を返す（無人の実行で勝手に入れないため）。
#
# $1: 尋ねる文言。
# 戻り値: 「はい」なら 0、そうでなければ 1。
ask() {
	if [ "$ASSUME_YES" = "1" ]; then
		return 0
	fi
	# **存在ではなく「開けるか」で判定する。**Linux では /dev/tty のデバイスノードが
	# 常にあるので、`[ -e /dev/tty ]` は制御端末が無くても真になる（実測）。
	# 判定しないと、生のエラー（cannot open /dev/tty）が利用者の画面に出る。
	# **`{ } 2> /dev/null` で囲む。**コマンドに `2> /dev/null` を付けるだけでは、
	# **リダイレクト自体の失敗を消せない**（シェルがリダイレクトを評価した時点で
	# `/dev/tty: Device not configured` を出す。macOS で実測）。
	#
	# **`:` を使ってはならない。**POSIX では、特殊ビルトイン（`:` はその1つ）への
	# リダイレクトが失敗すると、**非対話シェルはそこで終了する。**
	# dash では実際に exit 2 で落ちた（bash は落ちないので手元では見つからない）。
	# `true` は特殊ビルトインではないので、失敗しても続く。
	if ! { true < /dev/tty; } 2> /dev/null; then
		return 1
	fi
	# 読めても書けないことがあるので、書き込みも同じ形で試す。
	if ! { printf '%s [y/N]: ' "$1" > /dev/tty; } 2> /dev/null; then
		return 1
	fi
	# read が失敗する（端末が閉じている）場合も「いいえ」にする。
	answer=""
	{ read -r answer < /dev/tty; } 2> /dev/null || return 1
	case "$answer" in
		y | Y | yes | YES) return 0 ;;
		*) return 1 ;;
	esac
}

# ---------------------------------------------------------------------------
# 引数
# ---------------------------------------------------------------------------

while [ $# -gt 0 ]; do
	case "$1" in
		--yes | -y)
			ASSUME_YES=1
			shift
			;;
		--no-deps)
			SKIP_DEPS=1
			shift
			;;
		--dir)
			[ $# -ge 2 ] || die "--dir には置き先を続けてください"
			INSTALL_DIR="$2"
			shift 2
			;;
		--version)
			[ $# -ge 2 ] || die "--version には版を続けてください（例: v0.1.0）"
			# **URL のパスに入る値なので、必ず検証する。**
			# `../` を含む値を通すと、curl が送信前にパスを正規化し、
			# **別のリポジトリの release に到達できる**（本物の github.com で 200 を確認済み）。
			case "$2" in
				"" | *[!A-Za-z0-9._+-]*)
					die "--version に使える文字は英数字と . _ + - だけです: $2"
					;;
			esac
			VERSION="$2"
			VERSION_GIVEN=1
			shift 2
			;;
		--cooldown-hours)
			# **時間の整数だけを受ける。**小数や単位付きの値は受けない。
			# 先頭の 0 を落としてから桁数を数える（`08` を sh の算術が8進数と読んで落ちるため）。
			# **6桁（999999 時間）までに限る。**それより大きいと、シェルの数の範囲を越えうる。
			[ $# -ge 2 ] || die "--cooldown-hours には時間を続けてください（例: --cooldown-hours 24）"
			case "$2" in
				"" | *[!0-9]*)
					die "--cooldown-hours には 0 以上の整数（時間）を続けてください（例: --cooldown-hours 24）: $2"
					;;
			esac
			ch_value="$(printf '%s' "$2" | sed 's/^0*//')"
			[ -n "$ch_value" ] || ch_value=0
			if [ "${#ch_value}" -gt 6 ]; then
				die "--cooldown-hours には 0 以上の整数（時間）を続けてください（例: --cooldown-hours 24）: $2"
			fi
			COOLDOWN_HOURS="$ch_value"
			shift 2
			;;
		--force)
			# **選んだ版がいま入っている版より古くても入れ替える。**
			# `--version` と一緒に付けたときは何もしない（名指しの版はもともと上書きする）。
			FORCE=1
			shift
			;;
		--repo)
			# **fork した人のための入口である。**既定と違えば、下で警告して尋ねる。
			[ $# -ge 2 ] || die "--repo には <owner>/<repo> を続けてください"
			case "$2" in
				*/*) ;;
				*) die "--repo は <owner>/<repo> の形で渡してください: $2" ;;
			esac
			case "$2" in
				*[!A-Za-z0-9._/-]*) die "--repo に使える文字は英数字と . _ - / だけです: $2" ;;
			esac
			REPO="$2"
			shift 2
			;;
		--base-url)
			# **テスト専用である。**偽の release サーバを立てて、配布を作る前に
			# インストーラーが本当に動くかを確かめるために使う。
			[ $# -ge 2 ] || die "--base-url には取得先を続けてください"
			BASE_URL="$2"
			INSECURE_SOURCE=1
			shift 2
			;;
		--api-url)
			# **テスト専用である。**--base-url と対で使う。
			[ $# -ge 2 ] || die "--api-url には問い合わせ先を続けてください"
			API_URL="$2"
			INSECURE_SOURCE=1
			shift 2
			;;
		--insecure-no-checksum)
			# **チェックサムを照合できなくても続ける。**既定では止まる。
			ALLOW_NO_CHECKSUM=1
			shift
			;;
		-h | --help)
			# **`$0` に依存しない。**`curl … | sh -s -- --help` では $0 が "sh" になり、
			# 自分自身を読み直せない（実際に `sed: sh: No such file or directory` で壊れた）。
			cat << 'USAGE'
continuo のネットワークインストーラー

  curl -fsSL https://raw.githubusercontent.com/maimuzo/continuo/main/install.sh | sh

やること:
  1. OS と命令セットを見分ける（Windows なら WSL2 を案内して止まる）
  2. GitHub の release から、その組み合わせの実行ファイルを取る
     （既定では、公開から 168 時間経った版だけを入れます）
  3. 前提の道具が揃っているかを調べ、足りないものは1つずつ聞いてから入れる
  4. ~/.local/bin/continuo へ置き、PATH の通し方を案内する
     （選んだ版がいま入っている版より古ければ、既定では入れ替えません）
  5. 上げるときに破壊的変更があれば、入れたうえで知らせる（インストールは止めません）

herdr と claude は入れません。どちらも独自の配布経路と認証があり、
勝手に入れると既存の設定を壊しうるためです。案内するだけです。

オプション:
  --yes, -y     すべての確認に「はい」と答える（依存も入れる）
  --no-deps     依存を1つも入れない（不足を並べるだけ）
  --dir DIR     置き先を変える（既定 ~/.local/bin）
  --version V   版を指定する（既定は、公開から 168 時間経った最新の正式版）
  --cooldown-hours H
                公開から H 時間経った版だけを入れる（既定 168 = 7日。0 で最新版）
  --force       選んだ版が、いま入っている版より古くても入れ替える
  --insecure-no-checksum
                チェックサムを照合できなくても続ける（既定では止まります）
  -h, --help    この案内を出す

  --repo O/R    fork から入れる（既定 maimuzo/continuo）。使うと警告が出ます

テスト用（ふだんは使いません）:
  --base-url U  実行ファイルの取得先を差し替える
  --api-url U   版の一覧の問い合わせ先を差し替える（末尾の /latest を落として使う）
                どちらも HTTPS の強制が外れます。使うと警告が出ます。

環境変数:
  CONTINUO_INSTALL_DIR  置き先（既定 ~/.local/bin）

取得先は環境変数からは変えられません。`--repo` を使ってください。
USAGE
			exit 0
			;;
		*)
			die "知らないオプションです: $1"
			;;
	esac
done

# set_urls は取得先を組み立てる。
#
# **`--base-url` / `--api-url` で明示されていれば、それを残す。**
# そうでなければ REPO から組み立てる。
set_urls() {
	if [ -z "$BASE_URL" ]; then
		BASE_URL="https://github.com/$REPO/releases/download"
	fi
	if [ -z "$API_URL" ]; then
		API_URL="https://api.github.com/repos/$REPO/releases/latest"
	fi
	if [ -z "$RELEASES_URL" ]; then
		# **一覧は `/releases/latest` の1つ上である。**
		# **1回の取得で見渡せるように多めに取る**（既定は30件）。
		# 飛び越えて上げたとき、あいだの版の印も同じ応答の中に入る。
		RELEASES_URL="${API_URL%/latest}?per_page=100"
	fi
	# **既定と違うリポジトリなら、差し替えられているものとして扱う。**
	if [ "$REPO" != "$DEFAULT_REPO" ]; then
		INSECURE_SOURCE=1
	fi
}

# ---------------------------------------------------------------------------
# OS と命令セットを見分ける
# ---------------------------------------------------------------------------

detect_platform() {
	os="$(uname -s)"
	arch="$(uname -m)"

	case "$os" in
		Darwin) GOOS="darwin" ;;
		Linux) GOOS="linux" ;;
		MINGW* | MSYS* | CYGWIN* | Windows_NT)
			die "Windows ネイティブには対応していません。WSL2 の中で実行してください（continuo が使う herdr の Windows 版が安定していないためです）"
			;;
		*)
			die "対応していない OS です: ${os}（macOS と Linux に対応しています）"
			;;
	esac

	case "$arch" in
		x86_64 | amd64) GOARCH="amd64" ;;
		arm64 | aarch64) GOARCH="arm64" ;;
		*)
			die "対応していない命令セットです: ${arch}（x86-64 と arm64 に対応しています）"
			;;
	esac
}

# ---------------------------------------------------------------------------
# release から取る
# ---------------------------------------------------------------------------

# fetch は URL の中身を標準出力へ出す。
#
# $1: URL。
# fetch は URL の中身を標準出力へ出す。
#
# **平文 HTTP へ降格させない。**`-L` はリダイレクトを追うので、
# https から http への転送も追ってしまう。取得先を差し替えているとき（テスト）だけ制限を外す。
#
# **オプションを変数に入れて展開しない。**POSIX sh に配列が無いので、
# 変数を無引用で展開して単語分割に頼ることになる。**それは事故の元である**
# （shellcheck も SC2046 / SC2086 で警告する）。**分岐して書き下すほうが安全である。**
fetch() {
	if have curl; then
		if [ "$INSECURE_SOURCE" = "1" ]; then
			curl --max-time 60 -fsSL "$1"
		else
			curl --proto '=https' --proto-redir '=https' --tlsv1.2 --max-time 60 -fsSL "$1"
		fi
	elif have wget; then
		if [ "$INSECURE_SOURCE" = "1" ]; then
			wget --timeout=60 -qO- "$1"
		else
			wget --https-only --timeout=60 -qO- "$1"
		fi
	else
		die "curl も wget もありません。どちらかを入れてください"
	fi
}

# download はファイルを落とす。
#
# $1: URL。$2: 置き先。
download() {
	if have curl; then
		if [ "$INSECURE_SOURCE" = "1" ]; then
			curl --max-time 300 -fsSL -o "$2" "$1"
		else
			curl --proto '=https' --proto-redir '=https' --tlsv1.2 --max-time 300 -fsSL -o "$2" "$1"
		fi
	elif have wget; then
		if [ "$INSECURE_SOURCE" = "1" ]; then
			wget --timeout=300 -qO "$2" "$1"
		else
			wget --https-only --timeout=300 -qO "$2" "$1"
		fi
	else
		die "curl も wget もありません。どちらかを入れてください"
	fi
}

# shquote は、貼らせる1行に置く値を、そのまま貼れる形にする。
#
# **英数字と `. _ / - +` だけなら、そのまま書く。**それ以外を含めば一重引用符で囲み、
# 値の中の `'` は `'\''` に替える。**`~` は裸で書かない。**貼ったシェルが `$HOME` へ展開し、
# 別の場所を指してしまう（sh と dash で実測）。
#
# $1: 値。
shquote() {
	case "$1" in
		"" | *[!A-Za-z0-9._/+-]*)
			printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
			;;
		*)
			printf '%s' "$1"
			;;
	esac
}

# install_line は、利用者に貼らせる1行を組み立てる。
#
# **表示どおりに貼った結果が、表示と同じになるようにする。**だから、いまの実行で効いている
# 取得元・置き先・時間を足す。置き先は `--dir` で渡したか、環境変数 `CONTINUO_INSTALL_DIR` で
# 渡したかを問わない（どちらも INSTALL_DIR に入る）。
#
# **URL は既定のリポジトリの install.sh のままにする。**fork の `main` の install.sh が、
# 新しいオプションを知らないことがあるためである。fork は `--repo` で渡す。
#
# $1: 時間を足すか（`keep` なら、既定と違う `--cooldown-hours` を足す。`drop` なら足さない）。
# $2 以降: 末尾に足すオプション（そのまま貼れる形で渡す）。
install_line() {
	il_keep="$1"
	shift
	il_line="curl -fsSL https://raw.githubusercontent.com/${DEFAULT_REPO}/main/install.sh | sh -s --"
	if [ "$REPO" != "$DEFAULT_REPO" ]; then
		il_line="$il_line --repo $(shquote "$REPO")"
	fi
	if [ "$INSTALL_DIR" != "$HOME/.local/bin" ]; then
		il_line="$il_line --dir $(shquote "$INSTALL_DIR")"
	fi
	if [ "$il_keep" = "keep" ] && [ "$COOLDOWN_HOURS" != "$DEFAULT_COOLDOWN_HOURS" ]; then
		il_line="$il_line --cooldown-hours $COOLDOWN_HOURS"
	fi
	for il_opt in "$@"; do
		il_line="$il_line $il_opt"
	done
	printf '%s' "$il_line"
}

# cooldown_label は、「公開から 168 時間（7日）」の形の文言を返す。
#
# **日数の括弧は、時間が 24 の倍数のときだけ付ける。**30 時間を「1日」と書くと嘘になる。
cooldown_label() {
	if [ "$COOLDOWN_HOURS" -gt 0 ] && [ $((COOLDOWN_HOURS % 24)) -eq 0 ]; then
		printf '公開から %s 時間（%s日）' "$COOLDOWN_HOURS" "$((COOLDOWN_HOURS / 24))"
	else
		printf '公開から %s 時間' "$COOLDOWN_HOURS"
	fi
}

# AWK_VCMP は、版を比べる awk の関数である。**select_release と breaking_lines の両方の先頭に付ける。**
#
# **1つだけ置く。**下がるかどうかの判定と、破壊的変更を並べる範囲が、同じ規則で版を比べるためである。
# 片方だけ直すと、2つが「どちらが新しいか」で食い違う。
#
# **`-` や `+` から後ろは落として比べる。**そのため `v2.98.0-rc1` と `v2.98.0` は同じ大きさになる
# （設計 3-36。rc を出すことにするなら、先にここを直すこと）。
AWK_VCMP='
		# vpart は版の i 番目の数を返す。先頭の v と、- や + から後ろは落とす。
		function vpart(v, i,   a, n) {
			sub(/^[vV]/, "", v)
			sub(/[-+].*$/, "", v)
			n = split(v, a, "[.]")
			if (i > n) { return 0 }
			return a[i] + 0
		}
		# vcmp は2つの版を比べる。**桁ごとに数として比べる。**
		# 文字列として比べると v0.10.0 が v0.2.0 より小さくなる。
		function vcmp(x, y,   i, dx, dy) {
			for (i = 1; i <= 3; i++) {
				dx = vpart(x, i)
				dy = vpart(y, i)
				if (dx != dy) { return (dx < dy) ? -1 : 1 }
			}
			return 0
		}
'

# select_release は、release の一覧から入れる版を選ぶ。
#
# 標準入力: GitHub API の release の一覧（JSON）。**引数では渡さない。**
# いまの一覧は18件で 290,624 バイトあり、引数の長さの上限に当たりうる。
# $1: いまの時刻（`date -u '+%Y-%m-%dT%H:%M:%SZ'` の形）。
# $2: 公開から何時間経った版を入れるか（0 なら時刻で絞らない）。
# $3: いま入っている版（vN.N.N の形でなければ空を渡す）。
# 標準出力: 1行に1件、先頭の1文字で種類を分け、欄はタブで区切る。
#   N <正式版の数>
#   C <版> <公開の時刻> <公開から何時間>                  選んだ版
#   S <版> <公開の時刻> <公開から何時間> <あと何時間>     見送った版（選んだ版より大きいもの。大きい順）
#   E <版> <公開の時刻> <公開から何時間> <あと何時間>     入れてよい版が無いとき、いちばん早く入れられる版
#   D cooldown|newer                                     選んだ版がいま入っている版より小さい（その理由）
#   U                                                    見送った版に、いま入っている版より大きいものがある
#
# **JSON を厳密に解釈しない**（設計 3-36。POSIX sh に道具が無く、`jq` は前提にできない）。
# 鍵は、引用符で囲んだ名前の直後に（空白を挟んでもよい）`:` が来るものだけを数える。
# `"name": "draft"` の値を鍵と取り違えないためである。`"tag_name"` を拾ったら、そのあとに
# 現れる `"draft"`・`"prerelease"`・`"published_at"` を、その release のものとする。
# この4つは release の階層にだけ現れる（資産の欄は `created_at` と `updated_at` だけ）。
#
# **正式版**は、`draft` も `prerelease` も `false` で、公開の時刻が読めるものである。
# 版の文字が `--version` と同じ規則（英数字と `. _ + -`）に外れるものは数えない（URL に入るため）。
#
# **選ぶのは、入れてよい正式版のうち版の数がいちばん大きいものである。**公開の新しさでは選ばない。
# 古い系列へ修正版を出しても新しい系列を選び、下がるかどうかの比べと同じ規則に揃えるためである。
# 版の比べ方は breaking_lines の `vcmp` と同じ（桁ごとに数として比べ、`-` と `+` から後ろは落とす）。
#
# **経過時間が負なら 0 とみなす。**手元の時計が遅れていても、0 で最新を入れられるようにする。
select_release() {
	awk -v now="$1" -v hours="$2" -v installed="$3" "$AWK_VCMP"'
		# epoch は 2026-11-05T06:00:00Z の形を、1970-01-01 からの秒へ直す。
		# 読めなければ -1 を返す。**mktime は POSIX に無いので、日付から通し日数を出す式で数える。**
		function epoch(s,   y, m, d, hh, mm, ss, mp, doy, days) {
			if (s !~ /^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]Z$/) { return -1 }
			y = substr(s, 1, 4) + 0
			m = substr(s, 6, 2) + 0
			d = substr(s, 9, 2) + 0
			hh = substr(s, 12, 2) + 0
			mm = substr(s, 15, 2) + 0
			ss = substr(s, 18, 2) + 0
			if (m <= 2) { y = y - 1 }
			mp = (m + 9) % 12
			doy = int((153 * mp + 2) / 5) + d - 1
			days = y * 365 + int(y / 4) - int(y / 100) + int(y / 400) + doy - 719468
			return days * 86400 + hh * 3600 + mm * 60 + ss
		}
		# show は 2026-11-05T06:00:00Z を 2026-11-05 06:00 UTC の形にする。
		function show(s) {
			return substr(s, 1, 10) " " substr(s, 12, 5) " UTC"
		}
		function flush() {
			if (tag != "" && draft == "false" && pre == "false" && tag !~ /[^A-Za-z0-9._+-]/) {
				t = epoch(pub)
				if (t >= 0) {
					n++
					rt[n] = tag
					rp[n] = pub
					rs[n] = t
				}
			}
			tag = ""
			draft = ""
			pre = ""
			pub = ""
		}
		BEGIN {
			re = "\"(tag_name|draft|prerelease|published_at)\"[ \t\r\n]*:[ \t\r\n]*"
			n = 0
		}
		# **1行ずつ読む。**応答を1つの文字列に溜めて切り出すと、件数に対して2乗で遅くなる
		# （整形済みの100件で 2.5 秒かかった）。JSON の文字列は生の改行を含まないので、
		# 鍵と値は同じ行に在る。release の欄は行をまたいで tag などに持ち越す。
		{
			rest = $0
			while (match(rest, re)) {
				key = substr(rest, RSTART + 1, RLENGTH)
				sub(/".*$/, "", key)
				rest = substr(rest, RSTART + RLENGTH)
				if (substr(rest, 1, 1) == "\"") {
					rest = substr(rest, 2)
					q = index(rest, "\"")
					if (q == 0) { break }
					val = substr(rest, 1, q - 1)
					rest = substr(rest, q + 1)
				} else {
					val = rest
					sub(/[] \t\r\n,}].*$/, "", val)
					rest = substr(rest, length(val) + 1)
				}
				if (key == "tag_name") {
					flush()
					tag = val
				} else if (key == "draft") {
					draft = val
				} else if (key == "prerelease") {
					pre = val
				} else if (key == "published_at") {
					pub = val
				}
			}
		}
		END {
			flush()
			printf "N\t%d\n", n
			if (n == 0) { exit 0 }

			nowt = epoch(now)
			if (nowt < 0) { exit 2 }
			need = hours * 3600

			# 入れてよい版のうち、版の数がいちばん大きいものを選ぶ。
			ci = 0
			for (i = 1; i <= n; i++) {
				el[i] = nowt - rs[i]
				if (el[i] < 0) { el[i] = 0 }
				ok[i] = (hours == 0 || el[i] >= need) ? 1 : 0
				if (ok[i] && (ci == 0 || vcmp(rt[i], rt[ci]) > 0)) { ci = i }
			}

			if (ci == 0) {
				# いちばん早く入れられる版（残り時間がいちばん短いもの）。
				ei = 0
				for (i = 1; i <= n; i++) {
					if (ei == 0 || need - el[i] < need - el[ei]) { ei = i }
				}
				left = need - el[ei]
				printf "E\t%s\t%s\t%d\t%d\n", rt[ei], show(rp[ei]), int(el[ei] / 3600), int((left + 3599) / 3600)
				exit 0
			}

			printf "C\t%s\t%s\t%d\n", rt[ci], show(rp[ci]), int(el[ci] / 3600)

			# 見送った版: 選んだ版より大きく、まだ時間が経っていないもの。大きい順に並べる。
			m = 0
			for (i = 1; i <= n; i++) {
				if (!ok[i] && vcmp(rt[i], rt[ci]) > 0) {
					m++
					sk[m] = i
				}
			}
			for (a = 2; a <= m; a++) {
				x = sk[a]
				for (b = a - 1; b >= 1 && vcmp(rt[sk[b]], rt[x]) < 0; b--) { sk[b + 1] = sk[b] }
				sk[b + 1] = x
			}
			up = 0
			cause = "newer"
			for (a = 1; a <= m; a++) {
				i = sk[a]
				left = need - el[i]
				printf "S\t%s\t%s\t%d\t%d\n", rt[i], show(rp[i]), int(el[i] / 3600), int((left + 3599) / 3600)
				if (installed != "" && vcmp(rt[i], installed) >= 0) { cause = "cooldown" }
				if (installed != "" && vcmp(rt[i], installed) > 0) { up = 1 }
			}
			if (installed != "" && vcmp(rt[ci], installed) < 0) {
				printf "D\t%s\n", cause
				if (up) { printf "U\n" }
			}
		}
	'
}

# resolve_version は入れる版を決める。
#
# **`--version` があればそれを使い、クールダウンを当てない。**版を名指しした人は、その版が欲しいと決めている。
#
# **無ければ release の一覧を1回引き、クールダウンを当てて選ぶ**（設計 3-36）。
# 引いた一覧は LIST_BODY に残し、破壊的変更の印を読むときにも使う（未認証の 60回/時の上限を食わないように）。
#
# **版を指定しない実行では、一覧を引けなければ止まる。**一覧が版の決め手だからである
# （以前の `/releases/latest` を引けないときも止まっていた。引く API が替わっただけである）。
#
# 選んだ版がいま入っている版より古いときは、`--force` が無ければ KEEP_INSTALLED を 1 にする。
# 呼び出し元は、そのとき実行ファイルを置き換えない。
LIST_BODY=""
KEEP_INSTALLED=0
resolve_version() {
	if [ "$VERSION_GIVEN" = "1" ]; then
		say "クールダウン: --version で版を指定されたので、使いません。"
		return 0
	fi

	# **「引けなかった」「リポジトリが無い」「1つも無い」を分ける。**
	# GitHub の API は未認証だと 60回/時・IP 単位で、NAT の内側では普通に枯れる。
	#
	# **一覧の API は、release が1つも無くても 200 と `[]` を返す。**だから 404 は
	# 「リポジトリが見つからない」（`--repo` の打ち間違いなど）である。403 と 429 はレートリミット、
	# それ以外は繋がらなかったということである。
	http_code=""
	body=""
	if have curl; then
		# `-w` で状態コードを最終行に足し、あとで切り離す。
		if [ "$INSECURE_SOURCE" = "1" ]; then
			body="$(curl --max-time 60 -sSL -w '\n%{http_code}' "$RELEASES_URL" 2> /dev/null || true)"
		else
			body="$(curl --proto '=https' --proto-redir '=https' --tlsv1.2 \
				--max-time 60 -sSL -w '\n%{http_code}' "$RELEASES_URL" 2> /dev/null || true)"
		fi
		http_code="$(printf '%s' "$body" | tail -1)"
		body="$(printf '%s' "$body" | sed '$d')"
	else
		# wget では状態コードを素直に取れない。**空の本文は「問い合わせられなかった」とする。**
		# 1つも無いリポジトリは `[]` を返すので、空にはならない。
		body="$(fetch "$RELEASES_URL" 2> /dev/null || true)"
		[ -n "$body" ] && http_code="200" || http_code=""
	fi

	if [ "$http_code" = "404" ]; then
		say ""
		say "リポジトリが見つかりません: $RELEASES_URL"
		say ""
		if [ "$REPO" != "$DEFAULT_REPO" ]; then
			say "--repo の値を確かめてください。"
			say ""
		fi
		die "リポジトリが見つかりません（${REPO}）"
	fi

	if [ "$http_code" != "200" ]; then
		say ""
		say "配布された版を問い合わせられませんでした: $RELEASES_URL"
		say ""
		say "ネットワークに繋がらないか、GitHub の API の呼び出し回数の上限に達しています"
		say "（未認証では1時間あたり60回、接続元のアドレスごとに数えられます）。"
		say ""
		say "時間をおいて試し直すか、版を指定してください。"
		say ""
		# **例の版を書かない。**実在の版を書くと、貼った人がそのまま古い版へ下がる
		# （`--version` の実行は下げない判定を当てない）。版は一覧の画面で選んでもらう。
		say "    $(install_line drop --version "$(shquote '<版>')")"
		say ""
		say "配布している版は https://github.com/${REPO}/releases で確かめられます。"
		say ""
		die "配布された版を問い合わせられません（応答: ${http_code:-なし}）"
	fi
	LIST_BODY="$body"

	rv_now="$(date -u '+%Y-%m-%dT%H:%M:%SZ')" || die "いまの時刻を読めません（date -u が失敗しました）"
	rv_installed=""
	if is_release_version "$INSTALLED_VERSION"; then
		rv_installed="$INSTALLED_VERSION"
	fi
	# **選ぶ処理が落ちたら、理由を出して止める。**`set -eu` の下で代入が落ちると、
	# 何も出さずに終わってしまう（collect_breaking の注記と同じ落とし穴）。
	if ! rv_out="$(printf '%s\n' "$LIST_BODY" | select_release "$rv_now" "$COOLDOWN_HOURS" "$rv_installed")"; then
		say ""
		say "release の一覧から版を選べませんでした（awk が失敗しました）。"
		say "版を指定すれば、一覧を読まずに入れられます。"
		say ""
		say "    $(install_line drop --version "$(shquote '<版>')")"
		say ""
		die "release の一覧を読めません"
	fi

	rv_count="$(printf '%s\n' "$rv_out" | awk -F '\t' '$1 == "N" {print $2}')"
	if [ "${rv_count:-0}" = "0" ]; then
		say ""
		say "$REPO には release がまだ1つもありません。"
		say ""
		say "continuo はまだ配布していません。作者が実機で確かめ、出してよいと判断した時点で"
		say "タグが打たれ、release に実行ファイルが載ります。"
		say ""
		# **ここだけ英語も添える。**英語の README から来た人が最初に当たるのがこの画面である。
		say "(No release yet. Build from source with the commands below.)"
		say ""
		say "いま試したい場合は、ソースから作ってください。"
		say ""
		say "    git clone https://github.com/${REPO}.git"
		say "    cd $(basename "$REPO")"
		say "    go build -o \"$INSTALL_DIR/continuo\" ./cmd/continuo"
		say ""
		exit 1
	fi

	rv_label="$(cooldown_label)"

	# 入れてよい版が1つも無い（公開から時間が経っていない版しか無い）。
	rv_e="$(printf '%s\n' "$rv_out" | awk -F '\t' '$1 == "E" {print $2 "\t" $3 "\t" $4 "\t" $5}')"
	if [ -n "$rv_e" ]; then
		IFS='	' read -r e_tag e_pub e_el e_left << EOF
$rv_e
EOF
		say "クールダウン: ${rv_label}経った版がありません。"
		say "  いちばん早く入れられる版: ${e_tag}（${e_pub} 公開。公開から ${e_el} 時間。あと ${e_left} 時間で入れられます）"
		say "  待たずに入れるときは、クールダウンを 0 にします。"
		say "    $(install_line drop --cooldown-hours 0)"
		die "公開から ${COOLDOWN_HOURS} 時間経った版が無いので、入れませんでした"
	fi

	rv_c="$(printf '%s\n' "$rv_out" | awk -F '\t' '$1 == "C" {print $2 "\t" $3 "\t" $4}')"
	IFS='	' read -r c_tag c_pub c_el << EOF
$rv_c
EOF
	VERSION="$c_tag"
	rv_skipped="$(printf '%s\n' "$rv_out" | awk -F '\t' '$1 == "S"')"
	rv_down="$(printf '%s\n' "$rv_out" | awk -F '\t' '$1 == "D" {print $2}')"
	rv_up="$(printf '%s\n' "$rv_out" | awk -F '\t' '$1 == "U" {print "1"}')"

	if [ "$COOLDOWN_HOURS" = "0" ]; then
		say "クールダウン: 0 時間を指定されたので、公開からの時間で絞りません。"
	else
		say "クールダウン: ${rv_label}経った版だけを入れます。"
	fi

	# 見送った版を、大きい順に5行まで並べる。残りは1行にまとめる。
	rv_n=0
	if [ -n "$rv_skipped" ]; then
		while IFS='	' read -r s_kind s_tag s_pub s_el s_left; do
			[ "$s_kind" = "S" ] || continue
			rv_n=$((rv_n + 1))
			if [ "$rv_n" -le 5 ]; then
				say "  見送った版: ${s_tag}（${s_pub} 公開。公開から ${s_el} 時間。あと ${s_left} 時間で入れられます）"
			fi
		done << EOF
$rv_skipped
EOF
		if [ "$rv_n" -gt 5 ]; then
			say "  ほか $((rv_n - 5)) 件"
		fi
	fi

	if [ -n "$rv_down" ] && [ "$FORCE" != "1" ]; then
		KEEP_INSTALLED=1
		say "  選んだ版:   ${c_tag}（${c_pub} 公開。公開から ${c_el} 時間）"
		if [ "$rv_down" = "cooldown" ]; then
			say "  いま入っているのは ${INSTALLED_VERSION} です。クールダウンを当てると ${INSTALLED_VERSION} → ${c_tag} へ版が下がるので、入れ替えません。"
		else
			say "  いま入っているのは ${INSTALLED_VERSION} です。配布している正式版のどれより新しい版です（取り下げられた版か prerelease の可能性があります）。入れ替えません。"
		fi
		if [ -n "$rv_up" ]; then
			say "  見送った版をすぐに入れるときは、クールダウンを 0 にします。"
			say "    $(install_line drop --cooldown-hours 0)"
		fi
		say "  ${c_tag} へ下げるときは、--force を付けます。"
		say "    $(install_line keep --force)"
		if [ "$COOLDOWN_HOURS" != "0" ]; then
			say "  時間を変えるときは --cooldown-hours <時間> を付けます（例: --cooldown-hours 24）。"
		fi
		return 0
	fi

	say "  入れる版:   ${c_tag}（${c_pub} 公開。公開から ${c_el} 時間）"
	if [ -n "$rv_down" ]; then
		# **ここで終えない。**下げて入れるときも、見送った版へ上がる道と時間の変え方を続けて出す。
		say "  いま入っているのは ${INSTALLED_VERSION} です。--force が付いているので、${INSTALLED_VERSION} → ${c_tag} へ版を下げます。"
	fi
	if [ "$rv_n" -gt 0 ]; then
		say "  見送った版をすぐに入れるときは、クールダウンを 0 にします。"
		say "    $(install_line drop --cooldown-hours 0)"
		say "  時間を変えるときは --cooldown-hours <時間> を付けます（例: --cooldown-hours 24）。"
	elif [ "$COOLDOWN_HOURS" != "0" ]; then
		say "  時間を変えるときは --cooldown-hours <時間> を付けます（例: --cooldown-hours 24）。0 にすると、公開直後の版も入れます。"
	fi
}

# checksum_failed は、照合できなかったときの扱いを決める。
#
# **既定は「止まる」である。**`--insecure-no-checksum` が渡っているときだけ、
# 警告して続ける。
#
# $1: 照合できなかった理由。
checksum_failed() {
	if [ "$ALLOW_NO_CHECKSUM" = "1" ]; then
		warn "$1（--insecure-no-checksum が指定されているので続けます）"
		return 0
	fi
	say ""
	say "チェックサムを照合できませんでした: $1"
	say ""
	say "取ってきたものが途中で入れ替わっていないかを確かめられないので、置きません。"
	say "承知のうえで続けるなら --insecure-no-checksum を付けてください。"
	say ""
	die "チェックサムを照合できません"
}

# verify_checksum は、取ってきた書庫のチェックサムを照合する。
#
# $1: 書庫を置いた一時ディレクトリ。$2: 書庫のファイル名。
verify_checksum() {
	vdir="$1"
	vasset="$2"

	if have shasum; then
		sum_cmd="shasum -a 256"
	elif have sha256sum; then
		sum_cmd="sha256sum"
	else
		checksum_failed "shasum も sha256sum もありません"
		return 0
	fi

	if ! download "$BASE_URL/$VERSION/checksums.txt" "$vdir/checksums.txt" 2> /dev/null; then
		checksum_failed "checksums.txt を取得できません"
		return 0
	fi

	# **awk で2列目の完全一致を見る。**
	# `grep -F "  $vasset"` は部分一致なので、`…tar.gz.sig` の行にも当たる
	# （署名を後から足した日に、正しい release が入らなくなる）。
	# `grep " $vasset\$"` は行末を見るが、ファイル名の `.` が任意の1文字になる。
	expected="$(awk -v f="$vasset" '$2 == f {print $1; exit}' "$vdir/checksums.txt")"
	if [ -z "$expected" ]; then
		checksum_failed "checksums.txt に $vasset の行がありません"
		return 0
	fi

	actual="$(cd "$vdir" && $sum_cmd "$vasset" | awk '{print $1}')"
	if [ "$expected" != "${actual}" ]; then
		die "チェックサムが合いません（期待 $expected / 実際 ${actual}）。取得をやり直してください"
	fi
	say "チェックサムを照合しました。"
}

# verify_provenance は、配布物が「どの workflow のどの commit から作られたか」を確かめる。
#
# **checksums.txt では改竄を検知できない。**書庫と同じ release から配るので、
# release ごと差し替えられれば、checksums.txt も一緒に差し替わる。
# **GitHub が署名した provenance のほうが強い。**
#
# **`gh` が無ければ飛ばす。**インストーラーは `gh` を要求しない（あとで `continuo doctor` が言う）。
# **取得先を差し替えているときも飛ばす**（テストの偽サーバには provenance が無い）。
#
# $1: 書庫を置いた一時ディレクトリ。$2: 書庫のファイル名。
verify_provenance() {
	if [ "$INSECURE_SOURCE" = "1" ]; then
		return 0
	fi
	if ! have gh; then
		say "gh が無いので、配布物の出所（provenance）は確かめませんでした。"
		say "  あとで確かめるなら: gh attestation verify <書庫> --repo ${REPO}"
		return 0
	fi
	if gh attestation verify "$1/$2" --repo "$REPO" > /dev/null 2>&1; then
		say "配布物の出所を確かめました（GitHub の provenance）。"
		return 0
	fi
	# **失敗しても止めない。**gh が未認証、古い版、または provenance を付ける前の
	# release ということがある。**確かめられなかったことを、そのまま伝える。**
	say "配布物の出所を確かめられませんでした。"
	say "  gh が未認証か、その release に provenance が付いていない可能性があります。"
	say "  自分で確かめるなら: gh attestation verify <書庫> --repo ${REPO}"
}

# install_binary は実行ファイルを取って置く。
install_binary() {
	asset="continuo_${GOOS}_${GOARCH}.tar.gz"
	url="$BASE_URL/$VERSION/$asset"

	tmp="$(mktemp -d)"
	# 途中で失敗しても後片付けする。
	#
	# **INT と TERM は EXIT と分ける。**POSIX sh は trap を実行したあと処理を続けるので、
	# まとめて書くと Ctrl-C で一時ディレクトリだけ消えて先へ進み、
	# 直後の展開が「展開に失敗しました」という無関係な理由で落ちる（実測）。
	trap 'rm -rf "$tmp"' EXIT
	trap 'rm -rf "$tmp"; exit 130' INT
	trap 'rm -rf "$tmp"; exit 143' TERM HUP

	say "取ってきます: $asset ($VERSION)"
	# **curl / wget の生のメッセージは見せない。**利用者には「何が無いか」だけを伝える。
	if ! download "$url" "$tmp/$asset" 2> /dev/null; then
		say ""
		say "$VERSION に $asset がありません。"
		say ""
		say "版の指定が誤っているか、その組み合わせを配布していない可能性があります。"
		say "配布しているものは https://github.com/$REPO/releases で確かめられます。"
		say ""
		die "取得に失敗しました: $url"
	fi

	# **チェックサムを照合できなければ、既定では止まる。**
	# 照合しないまま続けるには `--insecure-no-checksum` を明示的に渡す必要がある。
	#
	# **「照合しました」と言ってよいのは、本当に比較が成立したときだけである。**
	# 対象の行が無いのに成功と表示していた版では、**利用者に検証が通ったように見えていた。**
	verify_checksum "$tmp" "$asset"
	verify_provenance "$tmp" "$asset"

	# **`--no-same-owner` を付ける。**書庫が持ち主を指定していても従わない。
	tar -xzf "$tmp/$asset" -C "$tmp" --no-same-owner || die "展開に失敗しました"

	# **symlink を弾く。**`[ -f ]` は symlink を追跡するので、これだけでは通ってしまう。
	# 通すと `chmod +x` がリンク先の権限を変え、置き先が任意のパスへの symlink になる
	# （実測で 600 のファイルが 711 になった）。
	if [ -L "$tmp/continuo" ]; then
		die "取ってきた書庫の continuo が symlink です。配布物として妥当ではないので置きません"
	fi
	[ -f "$tmp/continuo" ] || die "取ってきた書庫に continuo が入っていません"

	# **権限は移す前に付ける。**置き先に実行できないものが一瞬でも見えないようにする。
	chmod +x "$tmp/continuo" || die "実行の権限を付けられません"

	mkdir -p "$INSTALL_DIR" || die "$INSTALL_DIR を作れません"
	# **同じ名前が既にあれば上書きする。**版を上げるのが普通の使い方である。
	mv "$tmp/continuo" "$INSTALL_DIR/continuo" || die "$INSTALL_DIR へ置けません"

	rm -rf "$tmp"
	trap - EXIT INT TERM HUP

	say "置きました: $INSTALL_DIR/continuo"
}

# ---------------------------------------------------------------------------
# 破壊的変更の警告
# ---------------------------------------------------------------------------

# INSTALLED_VERSION は、置き換える前に置き先にあったものが名乗った版である。
#
# **空なら、新規の導入か、版を訊けなかったということである。**どちらも何も言わない。
INSTALLED_VERSION=""

# BREAKING_NOTES は、上げる範囲で見つかった破壊的変更である（1行に1件）。
BREAKING_NOTES=""

# detect_installed_version は、置き換える前の実行ファイルに版を訊く。
#
# **置き換えたあとでは、何版から上げたのかを誰も知らない。**だから install_binary より前に呼ぶ。
#
# **symlink なら何もしない。**置き先が別のパスへのリンクだと、版を訊くつもりで
# 無関係な実行ファイルを走らせることになる。
# **答えを待ちすぎない。**`timeout` があれば5秒で打ち切る（無い環境ではそのまま呼ぶ）。
detect_installed_version() {
	div_bin="$INSTALL_DIR/continuo"
	if [ -L "$div_bin" ]; then
		return 0
	fi
	if [ ! -f "$div_bin" ] || [ ! -x "$div_bin" ]; then
		return 0
	fi
	div_out=""
	if have timeout; then
		div_out="$(timeout 5 "$div_bin" version < /dev/null 2> /dev/null || true)"
	else
		div_out="$("$div_bin" version < /dev/null 2> /dev/null || true)"
	fi
	INSTALLED_VERSION="$(printf '%s' "$div_out" | head -1 | tr -d '\r')"
}

# is_release_version は、版が vN.N.N の形かを返す。
#
# **`dev` や `ci-<commit>` は比べられない。**手元で `go build` しただけのものが名乗る値で、
# **どの release より新しいのか古いのかを決められない。**そういうものには何も言わない。
#
# $1: 調べる版。
# 戻り値: vN.N.N の形なら 0。
is_release_version() {
	case "$1" in
		v[0-9]*.[0-9]*.[0-9]*) return 0 ;;
		*) return 1 ;;
	esac
}

# breaking_lines は、release の一覧から、上げる範囲の破壊的変更を並べる。
#
# 標準入力: GitHub API の release の一覧（JSON）。
# $1: いま入っている版。$2: これから入れる版。
# 標準出力: 「  <版>  <説明>」の行。**$1 より後・$2 まで**（$2 を含む）の版だけを出す。
#
# **release の本文に置いた印を読む。**
#
#     <!-- breaking:start -->
#     - WORKFLOW.md の … が必須になりました
#     <!-- breaking:end -->
#
# **JSON を厳密に解釈しない。**POSIX sh に JSON の道具は無く、`jq` は前提にできない。
# `"tag_name"` と印を、応答に現れた順に拾う。**GitHub の応答では版が本文より先に来る**ので、
# 直前に拾った版がその印の持ち主である。
#
# **本文は JSON の文字列1つに収まっている**ので、印の始まりと終わりは同じ行に来る。
# 応答が整形されていても1行にまとめられていても、同じ読み方で拾える。
breaking_lines() {
	awk -v from="$1" -v to="$2" "$AWK_VCMP"'
		# unesc は JSON の文字列の中の書き換えを戻す。
		#
		# **戻すのは改行・タブ・引用符だけである。**JSON では制御文字が
		# バックスラッシュと u で始まる6文字の形で書かれる。**ここで戻さないので、
		# そのままの文字列として出る。**戻すと、release の本文に置かれた並びで
		# 利用者の端末を操作できてしまう。
		function unesc(s) {
			gsub(/\\r/, "", s)
			gsub(/\\t/, " ", s)
			gsub(/\\"/, "\"", s)
			return s
		}
		# nlsplit は、JSON の中の改行（バックスラッシュと n の2文字）で分ける。
		function nlsplit(s, out,   n, i, nl) {
			nl = "\\" "n"
			n = 0
			while ((i = index(s, nl)) > 0) {
				n++
				out[n] = substr(s, 1, i - 1)
				s = substr(s, i + 2)
			}
			n++
			out[n] = s
			return n
		}
		# emit は1つの印の中身を、範囲に入っていれば1行ずつ出す。
		function emit(tag, block,   n, i, parts, line) {
			if (tag == "") { return }
			if (vcmp(tag, from) <= 0) { return }
			if (vcmp(tag, to) > 0) { return }
			n = nlsplit(block, parts)
			for (i = 1; i <= n; i++) {
				line = unesc(parts[i])
				sub(/^[ \t]+/, "", line)
				sub(/[ \t]+$/, "", line)
				sub(/^[-*][ \t]+/, "", line)
				if (line == "") { continue }
				printf "  %s  %s\n", tag, line
			}
		}
		BEGIN {
			TAGKEY = "\"tag_name\""
			STMARK = "<!-- breaking:start -->"
			ENDMARK = "<!-- breaking:end -->"
		}
		{
			rest = $0
			while (1) {
				ti = index(rest, TAGKEY)
				si = index(rest, STMARK)
				if (ti == 0 && si == 0) { break }
				if (si == 0 || (ti != 0 && ti < si)) {
					# 鍵の後ろの、最初の引用符から次の引用符までが版である。
					rest = substr(rest, ti + length(TAGKEY))
					q = index(rest, "\"")
					if (q == 0) { break }
					rest = substr(rest, q + 1)
					q = index(rest, "\"")
					if (q == 0) { break }
					tag = substr(rest, 1, q - 1)
					rest = substr(rest, q + 1)
				} else {
					rest = substr(rest, si + length(STMARK))
					ei = index(rest, ENDMARK)
					if (ei == 0) { break }
					emit(tag, substr(rest, 1, ei - 1))
					rest = substr(rest, ei + length(ENDMARK))
				}
			}
		}
	'
}

# collect_breaking は、上げる範囲に破壊的変更があるかを調べる。
#
# **何も言わない場合が6つある。**
#   1. 置き先に実行ファイルが無い（新規の導入なので、警告する相手がいない）
#   2. いま入っているものが版を名乗らない（`dev`。比べられない）
#   3. これから入れる版が vN.N.N の形でない（同上）
#   4. release の一覧を引けなかった（**入れるのは止めない。**警告は付随的なものである）
#   5. 印を読み出す awk が落ちた（同上。**`|| true` で受ける**）
#   6. `-rc1` などの付いた版から、同じ数字の正式版へ上げる
#      （`vcmp` は `-` から後ろを落とすので `v2.98.0-rc1` と `v2.98.0` が同じ大きさになる。
#      continuo はまだ rc を出していない。設計 3-36 の「何も言わない場合」の表に載せてある）
#
# **下げるときも何も出ない。**範囲が「いま入っている版より後」なので空になる。
collect_breaking() {
	[ -n "$INSTALLED_VERSION" ] || return 0
	is_release_version "$INSTALLED_VERSION" || return 0
	is_release_version "$VERSION" || return 0

	# **版を指定しない実行では、版を選ぶときに引いた一覧を使う。**もう1回は引かない。
	# **`--version` の実行では、ここで初めて一覧を引く。引けなくても導入は止めない**（設計 3-36）。
	if [ -n "$LIST_BODY" ]; then
		cb_body="$LIST_BODY"
	else
		cb_body="$(fetch "$RELEASES_URL" 2> /dev/null || true)"
	fi
	[ -n "$cb_body" ] || return 0

	# **awk が落ちても導入は止めない。**このスクリプトは `set -eu` なので、`|| true` が無いと
	# **代入の時点で終了し、実行ファイルを置く前に落ちる**（collect_breaking は install_binary より
	# 先に走る）。しかも何のメッセージも出ない。**警告は付随的なものであり、作れなければ黙って続ける。**
	BREAKING_NOTES="$(printf '%s\n' "$cb_body" | breaking_lines "$INSTALLED_VERSION" "$VERSION" || true)"
}

# report_breaking は、見つかった破壊的変更を目立つ形で出す。
#
# **止めない。**`curl … | sh` の途中で止めると、**利用者は何が起きたか分からないまま、
# 実行ファイルが古いまま残る。**入れたうえで、設定を直す必要があることを伝える。
#
# **チェックサムの照合とは扱いを変える。**あちらは「取ってきたものが壊れている・
# すり替えられている」なので止める。こちらは「入れてよいが、設定を直す必要がある」である。
#
# **いちばん最後に出す。**先に出すと、そのあとの案内で流れて読まれない。
report_breaking() {
	[ -n "$BREAKING_NOTES" ] || return 0
	say "============================================================"
	say " 破壊的変更があります: ${INSTALLED_VERSION} → ${VERSION}"
	say ""
	say " 実行ファイルは入れ替えました。次に起動する前に設定を直してください。"
	say " 直さないまま起動すると、設定を読めずに落ちることがあります。"
	say ""
	printf '%s\n' "$BREAKING_NOTES"
	say ""
	say " 詳しくは https://github.com/${REPO}/releases を見てください。"
	say "============================================================"
	say ""
}

# report_kept は、入れ替えなかったことを最後にもう1度知らせる。
#
# **いちばん最後に出す。**「クールダウン:」の数行で言っても、そのあとの道具の確かめと
# 案内で流れて読まれない（破壊的変更の警告と同じ理由である）。
report_kept() {
	[ "$KEEP_INSTALLED" = "1" ] || return 0
	say "============================================================"
	say " 実行ファイルは入れ替えていません。いまは ${INSTALLED_VERSION} のままです。"
	say " ${VERSION} へ下げるときは --force を付けてください（上の案内の1行）。"
	say "============================================================"
	say ""
}

# ---------------------------------------------------------------------------
# 前提の道具
# ---------------------------------------------------------------------------

# pkg_manager は使えるパッケージ管理を返す。
pkg_manager() {
	if have brew; then
		printf 'brew'
	elif have apt-get; then
		printf 'apt'
	elif have dnf; then
		printf 'dnf'
	elif have pacman; then
		printf 'pacman'
	else
		printf ''
	fi
}

# install_cmd_text は、その道具を入れるために実際に走るコマンドを返す。
#
# **利用者に見せるためのものである。**`sudo` を隠さない。
#
# $1: 道具の名前。
install_cmd_text() {
	# **ghq は Debian / Fedora / Arch の公式リポジトリに無い**（Debian の sources API で確認）。
	# `sudo apt-get install -y ghq` は必ず失敗するので、そもそも尋ねない。
	if [ "$1" = "ghq" ] && [ "$(pkg_manager)" != "brew" ]; then
		printf ''
		return 0
	fi
	case "$(pkg_manager)" in
		brew) printf 'brew install %s' "$1" ;;
		apt) printf 'sudo apt-get install -y %s' "$1" ;;
		dnf) printf 'sudo dnf install -y %s' "$1" ;;
		pacman) printf 'sudo pacman -S --noconfirm %s' "$1" ;;
		*) printf '' ;;
	esac
}

# install_pkg は1つのパッケージを入れる。
#
# $1: パッケージ名。
install_pkg() {
	case "$(pkg_manager)" in
		brew) brew install "$1" ;;
		apt) sudo apt-get install -y "$1" ;;
		dnf) sudo dnf install -y "$1" ;;
		pacman) sudo pacman -S --noconfirm "$1" ;;
		*) return 1 ;;
	esac
}

# check_deps は前提の道具を調べ、足りないものを1つずつ尋ねて入れる。
#
# **`herdr` と `claude` は入れない。**案内するだけである。
check_deps() {
	missing_manual=""

	# 自動で入れられるもの。
	for tool in git gh ghq; do
		if have "$tool"; then
			continue
		fi
		say ""
		say "$tool がありません。"
		case "$tool" in
			git) say "  continuo は worktree の作成に git を使います。" ;;
			gh) say "  continuo はカンバンの読み書きに gh を使います。" ;;
			ghq) say "  continuo は clone の置き場所の解決に ghq を使います。" ;;
		esac
		if [ "$SKIP_DEPS" = "1" ]; then
			missing_manual="$missing_manual $tool"
			continue
		fi
		cmd_text="$(install_cmd_text "$tool")"
		if [ -z "$cmd_text" ]; then
			if [ "$tool" = "ghq" ]; then
				say "  この環境のパッケージ管理には ghq がありません。次のいずれかで入れてください。"
				say "    go install github.com/x-motemen/ghq@latest"
				say "    https://github.com/x-motemen/ghq/releases から取る"
			else
				warn "パッケージ管理が見つからないので、$tool は自分で入れてください。"
			fi
			missing_manual="$missing_manual $tool"
			continue
		fi
		# **実際に走るコマンドをそのまま見せる。**
		# 「apt で入れますか」だけでは、`sudo` で root に昇格することが伝わらない。
		if ask "次を実行してよいですか: $cmd_text"; then
			if install_pkg "$tool"; then
				say "$tool を入れました。"
			else
				warn "$tool の導入に失敗しました。自分で入れてください。"
				missing_manual="$missing_manual $tool"
			fi
		else
			missing_manual="$missing_manual $tool"
		fi
	done

	# 案内だけするもの。
	if ! have herdr; then
		say ""
		say "herdr がありません。continuo は herdr が無いと1件も処理できません。"
		say "  導入: https://github.com/herdrdev/herdr"
		missing_manual="$missing_manual herdr"
	fi
	if ! have claude; then
		say ""
		say "claude がありません。continuo は Claude Code を起動できません。"
		say "  導入: https://claude.com/claude-code"
		missing_manual="$missing_manual claude"
	fi

	MISSING="$missing_manual"
}

# ---------------------------------------------------------------------------
# 本体
# ---------------------------------------------------------------------------

main() {
	say "continuo のインストーラー"
	say ""

	set_urls

	# **取得先を差し替えているなら、必ず知らせる。**
	# 黙って差し替わると、公式から取ってきたつもりの利用者に偽の実行ファイルが渡る。
	if [ "$INSECURE_SOURCE" = "1" ]; then
		say "============================================================"
		say " 取得先が既定ではありません。公式の配布物を取りに行きません。"
		say ""
		say "   実行ファイル: $BASE_URL"
		say "   版の一覧の問い合わせ: $RELEASES_URL"
		say ""
		say " HTTPS の強制も外れています。テスト以外で使わないでください。"
		say "============================================================"
		say ""
		# **端末があるなら、続けてよいかを尋ねる。**無ければそのまま進む（テストの実行）。
		if ! ask "この取得先で続けますか"; then
			if { true < /dev/tty; } 2> /dev/null; then
				die "取得先が既定ではないので中止しました"
			fi
		fi
	fi

	detect_platform
	say "見分けました: $GOOS / $GOARCH"

	# **取ってくる道具が無いなら、ここで理由を出して止まる。**
	# fetch と download も同じ理由で die するが、resolve_version は標準エラーを捨てて
	# 呼ぶので、その文は画面に届かない（版を指定しない実行では、代わりに
	# 「まだ配布していません」と、事実と違う案内が出ていた）。
	have curl || have wget || die "curl も wget もありません。どちらかを入れてください"

	# **置き換える前に、いま入っているものへ版を訊く。**あとでは分からない。
	# **版を選ぶより前に訊く。**クールダウンで選んだ版がいま入っている版より古くなるかを、
	# 「クールダウン:」の数行の中で言うためである。
	detect_installed_version
	resolve_version
	# **選んだ版がいま入っている版より古く、`--force` が無ければ、置き換えない。**
	# いま動いている新しい実行ファイルを残すだけなので、何も壊れない（設計 3-36）。
	if [ "$KEEP_INSTALLED" != "1" ]; then
		collect_breaking
		install_binary
	fi

	check_deps

	say ""
	if [ -n "${MISSING:-}" ]; then
		say "まだ足りないもの:${MISSING}"
		say ""
	fi

	# PATH に入っていなければ、通し方を案内する。
	case ":$PATH:" in
		*":$INSTALL_DIR:"*) ;;
		*)
			say "$INSTALL_DIR は PATH に入っていません。次の1行を、お使いのシェルの設定へ足してください。"
			say ""
			say "    export PATH=\"$INSTALL_DIR:\$PATH\""
			say ""
			;;
	esac

	say "次にやること:"
	say ""
	say "    mkdir -p ~/continuo-work && cd ~/continuo-work"
	say "    continuo init      # WORKFLOW.md の雛形を置く"
	say "    continuo setup     # カンバンの Status を6つの役割に対応づける（6つ目は飛ばせる）"
	say "    continuo doctor    # 前提が揃っているかを調べる"
	say ""
	say "詳しくは https://github.com/${REPO}#使う を見てください。"
	say ""

	# **破壊的変更の警告は、いちばん最後に出す。**
	# 上のほうへ出すと、そのあとの案内で流れて読まれない。
	report_breaking
	report_kept
}

main
