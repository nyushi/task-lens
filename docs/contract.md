# 入出力契約

## CLI入力

外部commandはcontractVersion 2です。version 1の全担当一括送信は廃止しました。

```json
{"contractVersion":2,"prompt":"...","input":{"source":{"id":"p1","revision":2},"namespace":"src-example:","change":{"kind":"screen_edit","prefixChars":10,"removedChars":0,"insertedText":"追加","suffixChars":0,"baseHash":"...","currentHash":"...","reset":false,"historyComplete":false}},"previous":{"tasks":{},"roots":[],"sources":{}}}
```

初回のchangeは`kind:snapshot`, `text`, `partial`, `historyComplete:false`です。
変更後は画面置換を表し、全文や過去ログを再構成する保証はありません。previousは対象担当だけで根拠本文を除きます。
`input.instructions`も実行時に添付されます。[予算・復旧仕様](incremental.md)を参照してください。
[入力例](../examples/input.json)と[出力例](../examples/output.json)は同じnamespaceを使用します。
出力は対象担当の完全なResultを返し、Task・Node・Source IDをnamespaceで始めます。Task/Node schema自体は従来どおりです。

herdr-core observationの項目（TUIは[別仕様](herdr-tui.md)）：

| 項目 | 意味 |
|---|---|
| `id` | `herdr:`＋端末アドレス。Task IDとは独立 |
| `label` | 端末の表示名 |
| `revision` / `epoch` | 内容の版／イベント・再接続の世代 |
| `observedAt` | 取得時刻のUnix秒 |
| `content` | 現在画面のテキスト。末尾12,000文字まで |
| `coverage` | `complete`, `how`, `why`。ローカル切り詰め時もcomplete=false |
| `metadata` | address/name/title/state/status/turn/created_ms |

入力トップレベルの`epoch`も更新検出に使用します。
`complete=true`は全会話履歴の保証ではありません。画面から文字が消えたことを削除や完了と解釈しないでください。

## CLI出力

```json
{"tasks": {}, "roots": [], "sources": {}}
```

`tasks`はID→Task、`roots`は表示するTask IDの配列、`sources`は根拠ID→オブジェクトです。
[完全な出力例](../examples/output.json)をテストで検証しています。

| 対象 | 必須 | 任意 |
|---|---|---|
| Task | `id`, `purpose`, `state`, `attention`, `tree` | `updatedAt`, `stale` |
| Node | `id`, `state`, `attention`, `current`, `sourceRefs`, `content` | `children`, `detail` |
| text content | `kind: "text"`, `text` | — |
| Task content | `kind: "task"`, `taskId` | — |

```json
{
  "id": "review-node", "state": "doing", "attention": "review",
  "current": false, "sourceRefs": ["source-1"],
  "content": {"kind": "task", "taskId": "review-task"}, "children": []
}
```

参照先Taskもtasks内に含めます。そのTaskがさらにツリーを持てます。
表示には参照先の目的・状態・対応要否を使い、ノードのcurrentはその位置を強調します。
独自childrenも使用できますが、参照Taskのツリーの後に表示するため、通常は空にします。

- 配列順が表示順です。同じTask・Nodeには更新後も同じIDを使います。
- Taskのキーとidは一致させます。Node IDはTask内で一意にします。
- state: `done`（完了）、`doing`（進行中）、`todo`（未着手）、`unknown`（未確認）。
- attention: `decision`（判断）、`review`（レビュー）、`none`（不要）、`unknown`（未判定）。
- currentは真偽値です。現在位置が不明ならfalse。並行作業は複数指定できます。
- sourceRefsはsourcesのキーを参照します。元データの版・取得範囲を残してください。
- updatedAtはUnix秒。省略時は「未更新」。AIに時刻を推測させる必要はありません。

## 検証と更新

purpose/textは最大2,000文字、最大200 Task・合計2,000 Nodeです。
ノード階層・Task参照の深さを制限し、循環・重複ID・参照欠落・不正な状態を拒否します。
Webは合成深さ24で表示を打ち切ります。追加のJSONフィールドは無視します。
構文検証は解釈の正確さを保証しません。

ストアrevisionと入力の版を確認してcommitします。変換中に入力が変わった場合は破棄します。
失敗時は前回のTaskを維持します。入力から消えたTaskは削除せずstaleにします。
初回変換の失敗は起動エラーになります。永続化や明示削除は未実装です。

担当一覧の完全性は[削除契約](cleanup.md)のsource_snapshotsで別途宣言します。本文coverageとは独立しています。
