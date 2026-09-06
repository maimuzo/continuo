package config

// AIMarker は、人間ではなく機械が書いたコメントに付ける印である（設計 3-82）。
//
// **この印が言うのは「この本文を打鍵したのは機械である」ことだけである。**
// **continuo が付けるのは2つの経路である。**continuo 本体（Go のプログラム）が書くコメントと、
// continuo が起動した Claude Code が書くコメント（組み込みの指示書が書かせる）。
//
// **人間が自分で動かした Claude Code には、continuo は届かない。**
// そちらへ同じ印を付けさせたい project は、`CLAUDE.md` へ規則を書く
// （文面は [docs/FAQ.md](../../docs/FAQ.md) にある）。
// **だから「印が無ければ人間」は成り立たない。**印が無いことは、
// **人間が打鍵したか、continuo の外の Claude Code が書いたか、この仕組みより前のものか、のどれかである。**
//
// **なぜ要るか。**エージェントも continuo も人間も、同じ GitHub アカウントで投稿する
// （[internal/tracker/ghuser.go](../tracker/ghuser.go) の 23-25行）。
// **投稿者でも `author_association` でも見分けられない。**
// **人間が前に出した指示を探して、その下から読み直す**という読み方が、いまはできない。
//
// **印が言わないことが2つある。**
//
//	書いてある内容が誰の決定か … 人間の決定を AI が記録したコメントにも、この印は付く
//	偽れないこと           … 印は誰でも書ける文字列であり、認証ではない（設計 3-65）
//
// **設定キーにしない。**理由は ProgressMarker と同じである。
// `tracker.comments.marker` も `tracker.comments.self_marker` も機械ごとに違う値を書けるので、
// **別の機械や別の project が書いた印を読めなくなる。**
// **綴りが固定であること自体が、この印の値打ちである。**
// project ごとの設定を知らない読み手でも、機械の本文を当てられる。
//
// **組み込みのプロンプト（[internal/prompt/builtin.md](../prompt/builtin.md)）が
// エージェントへ書かせる文字列と、1文字も違ってはならない。**
const AIMarker = "<!-- continuo:ai -->"
