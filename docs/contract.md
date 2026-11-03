# 入出力契約

## CLI入力

```json
{
  "contractVersion": 1,
  "prompt": "prompt_fileの全文",
  "input": {"connected": true, "observations": []},
  "previous": {
    "tasks": {}, "roots": [], "sources": {},
    "revision": 0, "connected": true, "error": null
  }
}
```

[完全な入力例](../examples/input.json)は架空のHerdrデータです。
変換器には現在の入力全体と前回のTaskストアを渡します。ログ差分だけを渡す方式ではありません。

Herdr observationの項目：

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
