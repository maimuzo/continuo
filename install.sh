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
#   3. 前提の道具が揃っているかを調べ、**足りないものは1つずつ聞いてから**入れる
#   4. `~/.local/bin/continuo` へ置き、PATH の通し方を案内する
#   5. **上げるときに破壊的変更があれば、入れたうえで名指しで知らせる**（止めない）
#
# **`herdr` と `claude` は入れない。**どちらも独自の配布経路と認証があり、
# 勝手に入れると利用者の既存の設定を壊しうる。足りないことを告げて案内するだけである。
#
# **オプション。**
#   --yes       すべての確認に「はい」と答える（依存も入れる）
#   --no-deps   依存を1つも入れない（不足を並べるだけ）
#   --dir DIR   置き先を変える（既定 ~/.local/bin）
#   --version V 版を指定する（既定は最新の release）
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
# VERSION が空なら最新の release を使う。
VERSION=""

# BASE_URL は実行ファイルの取得先である。
# API_URL は最新の版を引く先である。
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

# RELEASES_URL は release の一覧を引く先である（破壊的変更の印を読むために使う）。
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
			shift 2
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
  3. 前提の道具が揃っているかを調べ、足りないものは1つずつ聞いてから入れる
  4. ~/.local/bin/continuo へ置き、PATH の通し方を案内する
  5. 上げるときに破壊的変更があれば、入れたうえで知らせる（インストールは止めません）

herdr と claude は入れません。どちらも独自の配布経路と認証があり、
勝手に入れると既存の設定を壊しうるためです。案内するだけです。

オプション:
  --yes, -y     すべての確認に「はい」と答える（依存も入れる）
  --no-deps     依存を1つも入れない（不足を並べるだけ）
  --dir DIR     置き先を変える（既定 ~/.local/bin）
  --version V   版を指定する（既定は最新の release）
  --insecure-no-checksum
                チェックサムを照合できなくても続ける（既定では止まります）
  -h, --help    この案内を出す

  --repo O/R    fork から入れる（既定 maimuzo/continuo）。使うと警告が出ます

テスト用（ふだんは使いません）:
  --base-url U  実行ファイルの取得先を差し替える
  --api-url U   版の問い合わせ先を差し替える
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

# resolve_version は入れる版を決める。
#
# **`--version` があればそれを使う。**無ければ GitHub API で最新の release を引く。
# **release が1つも無ければ、その旨を告げて止まる**（まだ配布していない状態である）。
resolve_version() {
	if [ -n "$VERSION" ]; then
		return 0
	fi

	# **「引けなかった」と「1つも無い」を分ける。**
	# GitHub の API は未認証だと 60回/時・IP 単位で、NAT の内側では普通に枯れる。
	# 区別しないと、**release が実在するのに「まだ配布していません」と断言してしまう**。
	#
	# **404 だけが「1つも無い」である。**403 と 429 はレートリミット、
	# それ以外は繋がらなかったということである。
	http_code=""
	body=""
	if have curl; then
		# `-w` で状態コードを最終行に足し、あとで切り離す。
		if [ "$INSECURE_SOURCE" = "1" ]; then
			body="$(curl --max-time 60 -sSL -w '\n%{http_code}' "$API_URL" 2> /dev/null || true)"
		else
			body="$(curl --proto '=https' --proto-redir '=https' --tlsv1.2 \
				--max-time 60 -sSL -w '\n%{http_code}' "$API_URL" 2> /dev/null || true)"
		fi
		http_code="$(printf '%s' "$body" | tail -1)"
		body="$(printf '%s' "$body" | sed '$d')"
	else
		# wget では状態コードを素直に取れないので、区別しない。
		body="$(fetch "$API_URL" 2> /dev/null || true)"
		[ -n "$body" ] && http_code="200" || http_code="404"
	fi

	if [ "$http_code" != "200" ] && [ "$http_code" != "404" ]; then
		say ""
		say "配布された版を問い合わせられませんでした: $API_URL"
		say ""
		say "ネットワークに繋がらないか、GitHub の API の呼び出し回数の上限に達しています"
		say "（未認証では1時間あたり60回、接続元のアドレスごとに数えられます）。"
		say ""
		say "時間をおいて試し直すか、版を指定してください。"
		say ""
		say "    curl -fsSL https://raw.githubusercontent.com/$REPO/main/install.sh | sh -s -- --version v0.1.0"
		say ""
		die "配布された版を問い合わせられません（応答: ${http_code}）"
	fi

	VERSION="$(printf '%s' "$body" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
	if [ -z "$VERSION" ]; then
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
	awk -v from="$1" -v to="$2" '
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
# **何も言わない場合が4つある。**
#   1. 置き先に実行ファイルが無い（新規の導入なので、警告する相手がいない）
#   2. いま入っているものが版を名乗らない（`dev`。比べられない）
#   3. これから入れる版が vN.N.N の形でない（同上）
#   4. release の一覧を引けなかった（**入れるのは止めない。**警告は付随的なものである）
#
# **下げるときも何も出ない。**範囲が「いま入っている版より後」なので空になる。
collect_breaking() {
	[ -n "$INSTALLED_VERSION" ] || return 0
	is_release_version "$INSTALLED_VERSION" || return 0
	is_release_version "$VERSION" || return 0

	cb_body="$(fetch "$RELEASES_URL" 2> /dev/null || true)"
	[ -n "$cb_body" ] || return 0

	BREAKING_NOTES="$(printf '%s\n' "$cb_body" | breaking_lines "$INSTALLED_VERSION" "$VERSION")"
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
			gh) say "  continuo はボードの読み書きに gh を使います。" ;;
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
		say "   版の問い合わせ: $API_URL"
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

	resolve_version
	# **置き換える前に、いま入っているものへ版を訊く。**あとでは分からない。
	detect_installed_version
	collect_breaking
	install_binary

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
	say "    continuo setup     # ボードの Status を5つの役割に対応づける"
	say "    continuo doctor    # 前提が揃っているかを調べる"
	say ""
	say "詳しくは https://github.com/${REPO}#使う を見てください。"
	say ""

	# **破壊的変更の警告は、いちばん最後に出す。**
	# 上のほうへ出すと、そのあとの案内で流れて読まれない。
	report_breaking
}

main
