# Herdr TUI入力

`herdr-tui`は[Herdr TUI](https://github.com/herdrdev/herdr)の既存サーバーに接続する入力プラグインです。
`herdr-core`（従来名`herdr`も有効）は別の入力です。両者はサーバーも管理対象も共有しません。
以前のcore接続テストはTUI接続の検証ではありません。

```sh
./task-lens --config herdr-tui-config.example.json
```

設定の`socket`に対象セッションのAPIソケットの絶対パス、`panes`に読み取るpane IDを指定します。
自動探索・サーバー起動・既存セッション変更は行いません。
公式の既定パスは`~/.config/herdr/herdr.sock`、名前付きセッションは
`~/.config/herdr/sessions/<name>/herdr.sock`です。設定では`~`を展開した絶対パスを使用してください。
選択するセッションを確認し、利用中のHerdr CLIの`pane list`でIDを確認できます。
インストール済みバージョンの仕様は`herdr api schema --json`で確認できます。

## プロトコル

Unixソケット上の改行区切りJSONです。coreのJSON-RPCとは異なり、文字列IDと型付きresultを使います。

```json
{"id":"tasklens-read","method":"pane.list","params":{}}
{"id":"tasklens-read","result":{"type":"pane_list","panes":[]}}
{"id":"tasklens-read","method":"pane.read","params":{"pane_id":"p1","source":"visible","strip_ansi":true}}
{"id":"tasklens-read","result":{"type":"pane_read","read":{"pane_id":"p1","workspace_id":"w1","tab_id":"t1","source":"visible","format":"text","text":"synthetic","revision":3,"truncated":false}}}
```

`pane.list`でセッション内のメタデータを取得し、指定paneのみ`pane.read`します。
変換器には選択したpaneの観測だけを渡します。cwd、ネイティブエージェントのセッションパス等は除外します。
観測IDは`herdr-tui:<pane_id>`、revisionはread.revision、metadataにはpane/workspace/tab ID・label・agent_status等を含めます。
`coverage.how`は`visible`。completeは取得画面の切り詰め有無で、全会話履歴の保証ではありません。
12,000文字を超える場合は末尾を保持しcomplete=falseにします。

初版は設定intervalでのポーリングです。TUIの`events.subscribe`はsubscriptions配列を要する別仕様で、
coreのイベント実装を流用していません。TUIイベント購読は未実装です。
変換前後の読み取りと入力ハッシュで更新を検出しますが、ポーリング間に消えた一時的な表示は取得できません。
WebとTaskモデルは共通です。`observations`変換器ではモデルを呼びません。

## 検証範囲

公式ソース`ee5bfb5041bb3e4a49d591c72308aae3c81ecdde`を確認しました。

- [Socket API](https://github.com/herdrdev/herdr/blob/ee5bfb5041bb3e4a49d591c72308aae3c81ecdde/docs/preview/website/src/content/docs/socket-api.mdx)
- [paneパラメーターと応答データ](https://github.com/herdrdev/herdr/blob/ee5bfb5041bb3e4a49d591c72308aae3c81ecdde/src/api/schema/panes.rs)
- [応答の型](https://github.com/herdrdev/herdr/blob/ee5bfb5041bb3e4a49d591c72308aae3c81ecdde/src/api/schema/response.rs)

模擬Unixソケットで文字列ID、応答型、選択pane限定読み取り、書き込み拒否をテストしています。
実TUIプロセスとの接続は未検証です。既存TUIや作業データには接続していません。

外部変換は[担当別増分と送信予算](incremental.md)を適用します。ローカル画面取得とモデル送信を区別してください。
