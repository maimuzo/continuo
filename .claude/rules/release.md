# リリースの手順

**[docs/releasing.md](../../docs/releasing.md) のとおりに行うこと。**

**手順の正はあちらであり、この規則は手順を持たない。**
**同じ工程を2つの文書が持つと、緩いほうへ流れるためである。**
[docs/releasing.md](../../docs/releasing.md) は公開の文書で、[SECURITY.md](../../SECURITY.md) からも辿れる。
**直したものを次の release として出す、という約束の裏付けがそこにある。**だから手順はそちらへ置く。

**とくに次の3点。**

- **実機で issue を1件通してから出す。**テストが全部通っていても、実機で初めて出る欠陥がある
- **文書を直してから出す。**[docs/FAQ.md](../../docs/FAQ.md) と [docs/upgrading.md](../../docs/upgrading.md) の両方に入れる。ノートは1回きりで、あとから困った人が引けない
- **`--generate-notes` のまま放置しない。**commit の一覧は利用者に読めない
