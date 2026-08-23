#!/bin/sh
# CI と同じ状況で、手元のテストを走らせる。
#
# **「手元では通るのに CI で落ちる」を繰り返さないためのものである。**
# 実際に2度踏んだ（2026-08-23）:
#
#   - `continuo doctor` の検査が、開発者の PATH にある本物の `claude` を見ていた。
#     CI には無いので落ちた。**mock だけで通すはずのテストだった**
#   - install.sh が dash で exit 2 になった。macOS の /bin/sh は bash なので手元では出ない
#
# **何が違うのか。**
#
# | 何 | 手元（macOS） | CI（ubuntu-latest） |
# | --- | --- | --- |
# | `/bin/sh` | bash 3.2 | dash |
# | `claude` / `herdr` | 入っている | **無い** |
# | `gh` | ログイン済み | 未認証 |
#
# **PATH から claude と herdr を隠して走らせる。**
# シェルの違いは `test/install` が自分で両方を試すので、ここでは扱わない。

set -eu

# mise の shim を残す（go を引くため）。それ以外は最小限にする。
clean_path="/usr/bin:/bin:/usr/sbin:/sbin"
for extra in "${HOME}/.local/share/mise/shims" "${HOME}/go/bin"; do
	if [ -d "$extra" ]; then
		clean_path="${clean_path}:${extra}"
	fi
done

# go が引けなければ、いまの PATH から場所だけ借りる。
if ! PATH="$clean_path" command -v go > /dev/null 2>&1; then
	go_dir="$(dirname "$(command -v go)")"
	clean_path="${clean_path}:${go_dir}"
fi

echo "PATH: $clean_path"
echo ""
for tool in claude herdr gh; do
	if PATH="$clean_path" command -v "$tool" > /dev/null 2>&1; then
		echo "  注意: $tool がまだ見えています（CI には無いので、隠せていません）"
	else
		echo "  $tool は見えません（CI と同じ）"
	fi
done
echo ""

exec env PATH="$clean_path" go test -p 1 -count=1 "$@" ./...
