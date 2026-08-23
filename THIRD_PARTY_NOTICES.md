<!-- 目的: 配布する実行ファイルに含まれる第三者のソフトウェアと、そのライセンスを示す -->

# continuo の実行ファイルに含まれる第三者のソフトウェア

**配布している実行ファイルは静的リンクである。**以下のソフトウェアが、その中に含まれている。

**この一覧は `go list -deps ./cmd/continuo` から作った。**依存を足したときは作り直すこと。

---

## github.com/goccy/go-yaml v1.19.2

| | |
| --- | --- |
| 何に使っているか | `WORKFLOW.md` の front matter を読む |
| ライセンス | MIT |
| 著作権表示 | Copyright (c) 2019 Masaaki Goshima |
| 取得元 | https://github.com/goccy/go-yaml |

**go-yaml 自身は依存を持たない**（`go.sum` に載る module はこれ1本だけ）。
**実行ファイルに入る第三者は、これで全部である。**

```
MIT License

Copyright (c) 2019 Masaaki Goshima

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

---

## Go の標準ライブラリ

実行ファイルには Go の標準ライブラリ（`vendor/golang.org/x/{crypto,net,text}` を含む）が入る。
これらは Go の配布物の一部であり、**BSD 3-Clause で配られている。**

| | |
| --- | --- |
| 著作権表示 | Copyright (c) 2009 The Go Authors. All rights reserved. |
| ライセンス | https://go.dev/LICENSE |

---

## 準拠する仕様（コードは含まない）

**continuo は [openai/symphony](https://github.com/openai/symphony) の SPEC.md（Apache-2.0）に準拠している。**
**ただし、その文書もコードも、このリポジトリと配布物には1バイトも入っていない。**

- `docs/spec/symphony/` は `.gitignore` 済みで、git には1件も入っていない
- symphony から取り込んだコードは無い

**したがって Apache-2.0 の NOTICE を引き継ぐ義務は生じない。**
仕様の本文は https://github.com/openai/symphony/blob/main/SPEC.md にある。
