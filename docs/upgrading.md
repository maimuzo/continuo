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

---

## 何が足りないかは `continuo doctor` が出す

**手で見比べる必要はありません。**`continuo doctor` の `未記入の項目` の行に、
**足りない設定項目と、それを足す差分**が出ます。

**差分だけが要るなら、次のコマンドで出せます。**

```bash
cd ~/continuo-work && continuo doctor --missing-keys-patch WORKFLOW.md
```

**そのまま当てるなら、`patch` へ渡します。**当てる前に、上のコマンドで差分を読んでください。

```bash
cd ~/continuo-work && continuo doctor --missing-keys-patch WORKFLOW.md | patch -p0
```

**足すだけの差分です。**あなたが書いた行は1つも消えません。
**並び順を変えていても当たります**（差分は雛形ではなく、あなたの `WORKFLOW.md` から組み立てます）。

**当てたあとに何が増えたのかは、下の版ごとの節を読んでください。**
差分には雛形の説明のコメントもそのまま入っていますが、**書かなかったときに何が起きるかは
そちらにしか書いてありません。**

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
| **自動化に Status を動かされたら** | **猶予（`tracker.unknown_state_grace_ms`、既定10分）を置いて worker を止める** | **本来の Status へ戻して、止めずに続ける**（戻すのは3回まで） |
| **人間が Status を動かしたら** | 猶予を置いて止める | **猶予を置いて止める**（変わりません） |
| **起動** | 通ります | 通ります |

**壊れません。**書かないと、いままでどおり止まるだけです。**新しい機能が使えないだけです。**

**書き戻しは無制限ではありません。****同じ issue の同じ Status を戻せるのは3回まで**です。
continuo が戻すたびに自動化が書き直す押し合いになったら、**3回でその issue の書き戻しをやめ、
いままでどおり猶予を置いて worker を止めます**（そこから先は人間が決めます）。
そのとき issue のコメントに、押し合いが起きたことと、`Workflows` を切る手が案内されます。

### 足す場所と中身

**`tracker:` の下に足します。**`continuo init` が置いた雛形の Status 名のままなら、
次の2行をそのまま貼れば起動します。

```yaml
tracker:
  automated_state_rewrite:
    "Todo": "In Progress"   # 自動化が書く Status: 戻す先の Status
```

**Status の名前は、あなたのボードと `WORKFLOW.md` に合わせて置き換えてください。**上の例は
「自動化が `Todo` を書いたら、`In Progress` へ戻す」という意味です。
**`Todo` は GitHub が既定で作る選択肢です。**ボードから消してあるなら
`! 対応表のキー` が出ますが、**起動は止まりません**（下の「足したあとの確かめ方」を見てください）。

**左に、`tracker` の他のキーで使っている Status 名を書くと起動しません。**
雛形のままなら `Ready` / `In Progress` / `Done` / `Blocked` / `In Review` の5つが該当します。
たとえば `"In Progress": "AI In Progress"` を足すと、`continuo doctor` がこう言います。

```text
✗ 設定ファイル    … 設定キー tracker.automated_state_rewrite のキー の値 In Progress が不正です:
  tracker の他のキー（active_states / terminal_states / running_state / dispatch_state /
  failure_state / status_signal_map の遷移先）に無い Status 名にすること
```

**自動化が書く Status が `tracker` の他のキーに既に書いてあるなら、対応表は要りません。**
continuo が知っている Status なので、そもそも止まらないからです。

### 左と右の決め方

| どちら | 何を書くか | 守らないとどうなるか |
| --- | --- | --- |
| **左（キー）** | **`tracker` の他のキーに名前が出てこない Status。**自動化が書く Status 名です | **continuo が起動しません** |
| **右（値）** | **`tracker.active_states` に入っている Status** | **continuo が起動しません** |

**「`tracker` の他のキー」は6つです。**`active_states` / `terminal_states` / `running_state` /
`dispatch_state` / `failure_state` / `status_signal_map` の遷移先。
**`tracker` の外（`cleanup` など）は見ません。**

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

**`continuo doctor` の `対応表のキー` の行を読みます。****記号だけで判断しないでください。**
**1行も足していなくても `✓` が出ます**（照合するものが無いためです）。
**足せたかどうかは、記号のうしろの文で見分けます。**

```bash
cd ~/continuo-work && continuo doctor; echo "exit=$?"
```

| 記号のうしろに出る文 | どう読むか |
| --- | --- |
| `tracker.automated_state_rewrite は空です（書き戻しを行わない設定です）` | **足せていません。**別のファイルを編集したか、`tracker:` の下に置けていません |
| `tracker.automated_state_rewrite のキーはすべてボードの Status の選択肢にあります（1件）` | **足せています。**括弧の中の件数が、書いた行数と合っているかも見てください |
| `tracker.automated_state_rewrite のキーに、ボードの Status の選択肢に無いものがあります（1件）` | 足せてはいますが、**綴りがボードと違うか、その Status をボードで使わなくなっています** |

**上の2つは `✓`、いちばん下は `!` です。**
**`!` のままでも continuo は起動します**（終了コードも 0 です）。
直し方は [FAQ.md](FAQ.md) の「doctor が通らないとき」にあります。

**`✗ 設定ファイル` が出たときは、対応表そのものが弾かれています。**
左右の決め方をもう一度読んでください。**この場合 continuo は起動しません。**

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
