# 版を上げるとき

**この文書が答えること。**

- **continuo を新しい版に入れ替えたら、`WORKFLOW.md` に何を足せばよいのか**
- **足さないと何が起きるのか**
- **足したかどうかを、どうやって確かめるのか**

**読む前に知っておくこと。**`WORKFLOW.md` は `continuo init` が置いたきり、
continuo が勝手に書き足すことはありません。**増えた設定は、書かなければ既定の値で動きます。**
壊れませんが、**書かない限りその機能は使えません。**

**症状から引きたいときは [FAQ.md](FAQ.md) を見てください。**この文書は版から引くものです。

---

## `continuo init --force` で作り直さないこと

**`continuo setup` で決めた Status の割り当てが、雛形で潰れます。**
**下半分に書いたプロンプトも消えます。**増えた設定は、**その行だけを手で足してください。**

**雛形の説明を読みたいときは、別の場所へ書き出して見比べます**
（`continuo init` はディレクトリを作らないので、先に作ります）。

```bash
mkdir -p /tmp/continuo-template
continuo init /tmp/continuo-template
diff /tmp/continuo-template/WORKFLOW.md ~/continuo-work/WORKFLOW.md
```

---

## v0.1.8 から v0.1.9 へ

**増えた設定は1つだけです。**

| 何 | 中身 |
| --- | --- |
| **増えたキー** | `tracker.automated_state_rewrite` の1つだけ |
| **消えたキー** | **ありません** |
| **名前が変わったキー** | **ありません** |

**したがって、v0.1.8 の `WORKFLOW.md` はそのまま起動します。**作り直しは要りません。

### `tracker.automated_state_rewrite` — 自動化に動かされた Status を戻す

**何のためのものか。**GitHub Projects の**組み込みの自動化**（`Pull request linked to issue` /
`Pull request merged` など）が Status を書き換えたときに、**continuo が意図した Status へ戻す**ための対応表です。
新しく作ったボードでは、これらの自動化が有効な状態で作られます。

**書かないとどうなるか。**

| | 書かない場合 | 書いた場合 |
| --- | --- | --- |
| **自動化に Status を動かされたら** | **猶予（`tracker.unknown_state_grace_ms`、既定10分）を置いて worker を止める** | **本来の Status へ戻して、止めずに続ける** |
| **人間が Status を動かしたら** | 猶予を置いて止める | **猶予を置いて止める**（変わりません） |
| **起動** | 通ります | 通ります |

**壊れません。**書かないと、いままでどおり止まるだけです。**新しい機能が使えないだけです。**

### 足す場所と中身

**`tracker:` の下に足します。**そのまま貼れる形は次のとおりです。

```yaml
tracker:
  active_states: ["AI Ready", "AI In Progress"]
  automated_state_rewrite:
    "In Progress": "AI In Progress"
```

**Status の名前は、あなたのボードのものに置き換えてください。**上の例は
「自動化が `In Progress` を書いたら、`AI In Progress` へ戻す」という意味です。

### 左と右の決め方

| どちら | 何を書くか | 守らないとどうなるか |
| --- | --- | --- |
| **左（キー）** | **設定の他のどこにも名前が出てこない Status。**自動化が書く Status 名です | **continuo が起動しません** |
| **右（値）** | **`tracker.active_states` に入っている Status** | **continuo が起動しません** |

**どちらも起動時に弾かれます。**貼ってから気づけるので、当てずっぽうで書いても手元は壊れません。

**左が `active_states` などに書いてあると、その行は1度も効きません。**
書き戻しを引くのは「continuo が知らない Status になったとき」だけだからです。
**右が `terminal_states` だと、戻した瞬間に片付けが走ります。**だから両方とも弾きます。

### 左に何を書けばよいか分からないとき

**書かなくて構いません。**空のままにしておいてください。

**次に自動化が Status を動かしたとき、continuo がその issue のコメントに
「この2行を足してください」と、そのまま貼れる形で書きます。**
誰が Status を書いたか（人間か、ボードの自動化か）は continuo が見分けるので、
**自動化だったときだけ、その案内が出ます。**

```bash
gh issue view https://github.com/<owner>/<repo>/issues/42 --comments
```

### 足したあとの確かめ方

**`continuo doctor` に `対応表のキー` の行があります。**
`✓` なら、キーがボードの Status の選択肢にすべて実在しています。

```bash
cd ~/continuo-work && continuo doctor; echo "exit=$?"
```

**`! 対応表のキー` が出たら、綴りがボードと違うか、その Status をボードで使わなくなっています。**
直し方は [FAQ.md](FAQ.md) の「doctor が通らないとき」にあります。
**`!` のままでも continuo は起動します**（終了コードも 0 です）。

**書き換えたら continuo を再起動してください。**動いている最中は設定を読み直しません。

### 足したくない場合

**ボードの側で自動化そのものを止める手もあります。**
ボードの `Workflows` の設定から、動いてほしくない自動化を無効にしてください。
**そうすれば Status は横取りされないので、対応表も要りません。**

**どちらか一方でかまいません。**両方やっても構いませんが、片方で足ります。

**ただし、そのボードを人間も使っているなら、対応表のほうを勧めます。**
`Workflows` を切ると、continuo と関係なくボードを見ている人の使い勝手も変わります。
**continuo を入れたことが、人間の設定を変える理由になってはなりません。**

---

## 関連する文書

- 症状から引ける説明は [FAQ.md](FAQ.md) にある
- 書き戻しの仕組みは [agent_life_cycle.md](agent_life_cycle.md) の「自動化に Status を横取りされたとき」にある
- 設定の1行ずつの説明は、あなたの `WORKFLOW.md` の front matter のコメントにある
