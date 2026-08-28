# リリースの作り方

**言いたいこと。**タグを打つのは人間である。**タグを push した瞬間に CI が動き、release ができる。**
打つ前に確かめることが5つある。

---

## 1. 打つ前に確かめる

**この5つを通してから打つ。**どれか1つでも落ちていたら、タグを打ってはならない。

| 何を | どう確かめるか |
| --- | --- |
| **CI が緑である** | `gh run list --branch main --limit 1` |
| **CI と同じ状況で手元も通る** | `sh scripts/test-like-ci.sh` |
| **仕様とテストの連鎖が揃っている** | `sh scripts/check-rucm.sh --strict` |
| **実機で issue を1件通した** | [docs/trying_it_out.md](trying_it_out.md) の手順 |
| **`docs/upgrading.md` にこの版の節がある** | 下の「2. 版ごとの節を書く」 |

**実機で issue を1件通すのがいちばん重い。**mock だけで通しても、実機で初めて出る欠陥がある。
実際、`interactive_ready` を見ていなかった欠陥は、テストが全部通っている状態で残っていた。

**タグを打たずに CI を試せる。**

```bash
gh workflow run release.yml --ref main
```

**test と build までが走り、release は作られない**（publish はタグのときだけ動く）。

## 2. 版ごとの節を書く

**[docs/upgrading.md](upgrading.md) は版ごとに積む文書である。**打ってから書くと、
**その版を入れた人が、何を足せばよいか分からないまま最初の起動を迎える。**

**利用者は `WORKFLOW.md` を作り直せない。**`continuo init --force` は `continuo setup` で
決めた Status の割り当てと、下半分のプロンプトを雛形で潰す。**だから、足す行はこちらが書いて渡すしかない。**

**設定のキーの増減は機械で調べる。**

```bash
git fetch --tags
diff <(git show v0.1.8:internal/config/types.go | grep -o 'yaml:"[^"]*"' | sort -u) \
     <(git show main:internal/config/types.go   | grep -o 'yaml:"[^"]*"' | sort -u)
```

**節に置くのは4つである。**

| 何を書くか | 例 |
| --- | --- |
| **増えたキー・消えたキー・改名したキー** | 「増えたのは `tracker.automated_state_rewrite` の1つだけ」 |
| **書かないと何が起きるか** | 「壊れない。いままでどおり猶予を置いて止まるだけ」 |
| **そのまま貼れる yaml** | **雛形の値のままで起動すること**を、手元で1度確かめてから載せる |
| **足したかどうかの確かめ方** | `continuo doctor` のどの行が何と出れば足せているか |

**1つも増えていなければ、「増えたキーはありません」とだけ書く。**節そのものは作る。
**「何も無い」と書いてあることが、読む人には要る。**

## 3. タグを打つ

```bash
git checkout main
git pull --ff-only
git tag v0.1.0
git push origin v0.1.0
```

**タグは main の先頭に打つ。**CI はタグが指す commit をビルドするので、
別の commit に打つと、その中身が配られる。

## 4. CI が作るもの

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

## 5. 入るかを確かめる

**release ができたら、実際に入れてみる。**

```bash
curl -fsSL https://raw.githubusercontent.com/<owner>/continuo/main/install.sh \
  | sh -s -- --no-deps --dir /tmp/continuo-check
/tmp/continuo-check/continuo version
```

**打ったタグと同じ版が返れば成功である。**

## 6. 失敗したとき

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
