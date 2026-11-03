# 閉じた担当のカード削除

既定では削除せず、従来どおり過去のTaskを保持します。設定JSONのトップレベルに指定します。

```json
{"cleanup":{"remove_missing":true,"missing_after_seconds":300}}
```

- `remove_missing:true`のみ：完全な一覧で担当の不在を確認した周期に削除。
- `missing_after_seconds:N`（正の秒数）：不在を初めて確認してからN秒後以降の、正常な完全取得周期に削除。
- 両方指定：秒数が優先。`false`と`0`は削除なし。秒数の上限は1年。

これは更新時刻による寿命ではありません。存在する担当は、何時間更新がなくても削除しません。
失敗・不完全・取得対象外のscopeでは削除せず、不在の待機をリセットします。復旧後に改めて不在を確認した時点から待ちます。
再出現も待機をリセットします。削除は次の取得周期で行われ、独立したタイマーでは動きません。

## 入力の完全性と所有

Herdr TUI/coreアダプターは、指定担当の一覧・読み取りが成功したときだけ`source_snapshots`を出します。
ここでのcompleteは「担当の一覧が完全」という意味で、画面本文のcoverage.completeとは別です。
担当が閉じる途中でreadが失敗した周期は削除せず、次の正常な一覧で判断します。

外部file/command入力は、次のようにscopeごとの完全性と現在の担当IDを宣言します。

```json
{
  "connected": true,
  "observations": [{"id":"agent-a","scope":"machine-a","content":"synthetic"}],
  "source_snapshots": [{"scope":"machine-a","complete":true,"source_ids":["agent-a"]}]
}
```

空の完全一覧は`observations:[]`と`source_ids:[]`で表します。宣言なしは不完全扱いです。
observationsとsource_idsが一致しない、IDが重複、同一scopeの宣言が重複する場合も削除しません。
各observationのscope省略時は`default`です。複数入力元をまとめる場合はscopeとグローバルに一意な担当IDを分けてください。
あるscopeの一覧から、別scopeの担当を削除することはありません。

Taskの所有者は`observations`変換器と担当別`command`スケジューラーが記録します。モデル出力のTask省略だけでは削除しません。
所有者不明のfixture/recorded Taskは自動削除しません。再帰Taskも所有者単位で扱い、生存する別Taskから参照されるTaskは参照切れを防ぐため保持します。
削除した担当の変換キャッシュも破棄し、次周期でカードが復活しないようにします。

状態・所有関係・不在の待機時刻はメモリ上です。起動時は空で、前回起動のカードを読み込んで一斉削除する動作はありません。
設定変更は再起動時に反映されます。再起動はTask状態とモデル送信予算もリセットします。
