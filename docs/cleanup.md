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
所有者を宣言しないfixture/recorded Taskは自動削除しません。再帰Taskも所有者単位で扱い、生存する別Taskから参照されるTaskは参照切れを防ぐため保持します。
削除した担当の変換キャッシュも破棄し、次周期でカードが復活しないようにします。

状態・所有関係・不在の待機時刻はメモリ上です。起動時は空で、前回起動のカードを読み込んで一斉削除する動作はありません。
設定変更は再起動時に反映されます。再起動はTask状態とモデル送信予算もリセットします。

## fixtureの所有者

`fixture`変換器はツリーをそのまま取り込みます。任意の`fixture.owners`で、Task IDごとに所有者の配列を宣言できます。`source_id`は空でない担当ID、`scope`省略時は`default`です。TaskにないID、空の配列、重複した所有者はエラーになります。複数Taskで同じ所有者を共有できます。

file/command入力の例（`transform.plugin: "fixture"`）：

```json
{
  "connected": true,
  "observations": [],
  "source_snapshots": [
    {"scope":"session-1","complete":true,"source_ids":["pane-a"]}
  ],
  "fixture": {
    "tasks": {
      "card-a": {"id":"card-a","purpose":"表示の調整","state":"doing","attention":"none","tree":[]}
    },
    "roots": ["card-a"],
    "sources": {},
    "owners": {"card-a":[{"source_id":"pane-a","scope":"session-1"}]}
  }
}
```

`input.plugin: "fixture"`のファイルもこの包み形式を受け付けます。従来の`tasks/roots/sources`だけのファイルは引き続き利用でき、所有者なしなら削除しません。包み形式では`connected:true`を明示してください。

完全一覧の`source_ids`は、scope内のobservationsと今回のfixture所有者宣言の和集合に一致する必要があります。fixtureがある場合はobservationsを省略または空にできます。同じ担当IDを異なるscopeへ重複登録することはできません。`sources`は根拠データであり、所有者や生存一覧を推測するためには使いません。

pane-aを閉じたら、今回のfixtureから`card-a`とそのownersを除き、同じscopeの完全一覧を`source_ids:[]`として送ります。最後のカードを閉じた場合は`tasks:{}, roots:[], sources:{}, owners:{}`です。前回のカードは猶予時間まで保持され、正常な完全取得が続けば期限後に消えます。他のscope、所有者不明、他Taskが参照するカードは保持します。

共有Taskはownersに複数の担当を指定できます。すべての所有者のscopeが完全で、すべて不在の場合だけ猶予が進みます。いずれかが存在・不明・不完全なら待機をリセットします。現在のfixtureに再掲載したTaskは今回のownersで置換し、ownersを省略したTaskは所有者不明に戻します。今回掲載されなかったTaskの既存所有者は保持します。Task IDは入力全体で一意にしてください。
