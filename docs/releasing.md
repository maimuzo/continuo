# リリースの作り方

**言いたいこと。**タグを打つのは人間である。**タグを push した瞬間に CI が動き、release ができる。**
**この文書は、そのとき CI が何を作り、受け取った人がどう検証できるかを書く。**

---

## 1. 打つ前に確かめる

**通す検査と、その順番は [.claude/rules/release.md](../.claude/rules/release.md) にある。**
**6つあり、1つでも落ちていたらタグを打ってはならない。**
**手順を2箇所に置かないため、ここには置いていない。**

**いちばん重いのは「実機で issue を1件通す」である**（[docs/trying_it_out.md](trying_it_out.md)）。
mock だけで通しても、実機で初めて出る欠陥がある。
実際、`interactive_ready` を見ていなかった欠陥は、テストが全部通っている状態で残っていた。

## 2. タグを打つ

```bash
git checkout main
git pull --ff-only
git tag v0.1.0
git push origin v0.1.0
```

**タグは main の先頭に打つ。**CI はタグが指す commit をビルドするので、
別の commit に打つと、その中身が配られる。

## 3. CI が作るもの

`.github/workflows/release.yml` が動き、次を release へ載せる。

```text
continuo_darwin_arm64.tar.gz   （macOS / Apple Silicon）
continuo_darwin_amd64.tar.gz   （macOS / Intel）
continuo_linux_amd64.tar.gz    （Linux / x86-64）
continuo_linux_arm64.tar.gz    （Linux / arm64）
checksums.txt                  （install.sh が照合する）
```

**それぞれの書庫には、実行ファイルと `LICENSE` と README 2つが入る。**
配布物だけを受け取った人が読めるようにするためである。

**provenance も作る。**「どの workflow の、どの commit から作られたか」を GitHub が証明する。
利用者は次で確かめられる。

```bash
gh attestation verify continuo_darwin_arm64.tar.gz --repo <owner>/continuo
```

**`checksums.txt` は改竄の検知には効かない。**書庫と同じ場所から配るので、
どちらも差し替えられる。**壊れていないことしか分からない。**provenance のほうが強い。

## 4. 入るかを確かめる

**release ができたら、実際に入れてみる。**

```bash
curl -fsSL https://raw.githubusercontent.com/<owner>/continuo/main/install.sh \
  | sh -s -- --no-deps --dir /tmp/continuo-check
/tmp/continuo-check/continuo version
```

**打ったタグと同じ版が返れば成功である。**

## 5. 失敗したとき

**CI が落ちたら、release は作られない。**タグだけが残る。

```bash
git tag -d v0.1.0                    # 手元のタグを消す
git push origin :refs/tags/v0.1.0    # リモートのタグを消す
```

**直してから同じタグを打ち直してよい。**release がまだ無いので、誰も掴んでいない。

**release ができてしまった後に問題が見つかったら、タグは消さない。**
既に入れた人がいるかもしれない。**次の版を出す。**

## 打つ前に落ちるようにしてあること

**タグを打ってから気づくのを避けるため、CI の test の段で先に落とす。**

| 何 | なぜ先に見るか |
| --- | --- |
| `LICENSE` / `README.md` / `README.ja.md` が git に入っているか | **build の段で `cp` する。**追跡されていないと4つ全部が落ちる |
| `install.sh` が sh と dash で走るか | 利用者が最初に叩くものである |
| 変数の直後に全角文字が続いていないか | `set -u` の下で落ちるが、**dash では起きないので構文検査では出ない** |
| 4つの組み合わせがビルドできるか | 手元が macOS でも、Linux 向けが通るとは限らない |
| `continuo version` が版を答えるか | **`-X` の左辺が誤っていても Go は警告を出さない** |
