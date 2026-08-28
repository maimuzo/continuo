# リリースの手順

## 絶対条件：文書を直してから出す

**リリースノートに書いただけでは足りない。**
**ノートは1回きりで、あとから困った人が引けない。**

**新しい設定・新しい `doctor` の項目・変わった振る舞いは、
[docs/FAQ.md](../../docs/FAQ.md) に「症状 → 原因 → 直し方」の形で入れてから出す。**

**確かめ方。**次が0件なら、まだ出してはいけない。

```bash
# continuo リポジトリの root で実行する
for k in "<新しい設定キー>" "<新しい doctor の見出し語>"; do
  printf '%s: ' "$k"
  grep -rc "$k" README.md README.ja.md docs/FAQ.md 2>/dev/null | paste -sd' ' -
done
```

**雛形（`internal/scaffold/template.go`）にしか書いていない設定は、
`continuo init` を新しく走らせた人にしか届かない。**既に設定を持っている人には届かない。

---

## 手順

**上から順に行う。1つでも飛ばさない。**

### 1. main が緑であることを確かめる

```bash
# continuo リポジトリの root で実行する
git fetch origin
gh run list --branch main --limit 1 --json headSha,status,conclusion \
  --jq '.[] | "\(.headSha[0:7]) \(.status)/\(.conclusion)"'
```

**`completed/success` でなければ止まる。**

### 2. 前の版からの差分を読む

```bash
# continuo リポジトリの root で実行する。<前の版> は v0.1.8 のような形
git log --oneline <前の版>..origin/main
git diff --stat <前の版>..origin/main -- internal/scaffold/template.go
```

**雛形が変わっていたら、利用者が `WORKFLOW.md` を更新する必要があるかを判断する。**

| 何 | 更新が要るか |
| --- | --- |
| **キーを足しただけで、省略できる**（既定値がある） | **要らない** |
| キーの名前を変えた・消した | **要る** |
| 検査が厳しくなり、既存の設定が弾かれる | **要る** |

**「要らない」と判断したら、その根拠を確かめる。**

```bash
# continuo リポジトリの root で実行する
grep -n "<新しいキーの Go の名前>" internal/config/default.go   # 既定値があるか
git diff <前の版>..origin/main -- internal/config/validate.go | grep '^+' | grep -i "required"
```

### 3. 文書を直す

**上の「絶対条件」のとおり。**直し終えてから次へ進む。

### 4. 版を決める

**このリポジトリは `v0.1.x` である。**

| 何が入ったか | どう上げるか |
| --- | --- |
| 直しと、省略できる設定の追加だけ | **末尾を1つ上げる** |
| **利用者の設定を書き換えないと動かない変更** | **人間に確認する。**勝手に上げない |

### 5. リリースノートを書く

**[.claude/rules/reporting.md](reporting.md) と同じ考え方で書く。**
**利用者のための文書であって、作業の報告ではない。**

| 書く | 書かない |
| --- | --- |
| **何ができるようになったか** | 内部の作業量・手法 |
| **何をすれば使えるか**（設定の例） | PR 番号・issue 番号の羅列 |
| **振る舞いが変わったところ**（見出しを分ける） | 未修正の件数 |
| **入れ方** | 「直しました」という報告調 |

**振る舞いが変わったところは、必ず独立した見出しで書く。**
**設定を変えなくても動きが変わる箇所は、利用者が踏むまで気づけない。**

**個人の絶対パス・トークン・実在のリポジトリ名を書かない。**例は `<owner>/<repo>` を使う。

### 6. タグを打つ

```bash
# continuo リポジトリの root で実行する
git tag v0.1.9 origin/main
git push origin v0.1.9
```

**タグを打つと [.github/workflows/release.yml](../../.github/workflows/release.yml) が走る。**
**それまで release は1つも作られない。**

### 7. 実行ファイルができるのを待つ

```bash
# continuo リポジトリの root で実行する
gh run list --limit 1 --json databaseId,status,conclusion \
  --jq '.[] | "\(.databaseId) \(.status)/\(.conclusion)"'
```

**できるもの。**

```
continuo_darwin_arm64.tar.gz   continuo_darwin_amd64.tar.gz
continuo_linux_amd64.tar.gz    continuo_linux_arm64.tar.gz
checksums.txt                  provenance
```

### 8. リリースノートを差し替える

**workflow は `--generate-notes` で作るので、commit の一覧が載る。**
**書いたノートで上書きする。**

```bash
# continuo リポジトリの root で実行する
gh release edit v0.1.9 --notes-file <書いたノートのパス>
```

### 9. 手元へ入れて、起動するところまで確かめる

```bash
curl -fsSL https://raw.githubusercontent.com/<owner>/<repo>/main/install.sh | sh
continuo --version
continuo doctor
```

**`continuo doctor` まで通すこと。**
**テストが全部通っても、実機で起動するまで見つからない不具合がある。**

### 10. 動いている continuo を入れ替える

**入れ替えは自動では起きない。**動いているものは古いバイナリのままである。

```bash
# 動いているか確かめる
pgrep -fl continuo
```

**止めて起動し直すのは人間の判断である。**勝手に止めない。

---

## やってはいけないこと

| 何 | なぜ |
| --- | --- |
| **文書を直さずに出す** | ノートは1回きり。あとから困った人が引けない |
| **タグを打ち直す** | 既に取った人と中身が食い違う |
| **CI が赤のまま出す** | 壊れたものを配ることになる |
| **`--generate-notes` のまま放置する** | commit の一覧は利用者に読めない |
