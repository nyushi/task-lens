# TaskLens

入力データをTaskとツリーに変換し、ローカルWebで表示するツールです。
Herdr TUI、herdr-core、JSONファイル、外部コマンドを入力にできます。入力・変換・表示は一方向で、返信や操作は行いません。

## 起動

Go 1.24以降。macOS / Linux対応。外部ライブラリは使用していません。

```sh
go build -o task-lens .
./task-lens
```

既定のURLは **http://127.0.0.1:8765/** です。同じポートなら再起動後も変わりません。
URLトークンや認証Cookieは使いません。既定では埋め込みのサンプルを表示します。
完了タスクも通常の一覧に表示し、上部の「完了」で絞り込めます。件数は検索条件に一致するタスク数です。
ループバックだけで待ち受け、Host・Origin等を検証します。同じPCの他プロセス／ユーザーからの読み取りを防ぐ認証ではありません。
[ローカルHTTPの制限](docs/configuration.md#ローカルhttp)も参照してください。

Webもバイナリに含まれるため、実行時にGoやPythonは不要です。
外部CLIを選ぶ場合は、そのCLIを別途用意してください。

```sh
./task-lens --config herdr-tui-config.example.json --port 8765
```

Herdr TUI設定のソケットとpane一覧を変更して使います。例の変換器は取得内容の表示のみで、外部送信しません。

## 変換器

外部CLIの実行ファイル、引数配列、プロンプトファイルを設定できます。
担当別の画面差分と前回Taskだけをstdinに渡し、stdoutからTask JSONを受け取ります。
初回の大量入力や送信予算超過は保留します。

- [増分変換・予算](docs/incremental.md)
- [合成15万字の検証結果](docs/incremental-test-results.md)
- [設定](docs/configuration.md)
- [Herdr TUI入力](docs/herdr-tui.md)
- [入出力契約](docs/contract.md)
- [プロンプトとCLIラッパー](docs/transforms.md)
- [Herdr用プロンプト例](examples/herdr-prompt.txt)
- [入力例](examples/input.json) / [出力例](examples/output.json)

## Claude Codeフック連携（設計案・未実装）

画面の巡回に代えて、Claude Codeのフックからローカル受信器へ状態を通知する案です。
受信器・hook入力アダプター・以下のコマンドは未実装です。

| フック | 取得する情報・用途 | 制約 |
|---|---|---|
| `PermissionRequest` | `tool_name`, `tool_input`から権限確認待ちを記録 | 許可結果ではない。非対話時の自動拒否直前にも発生し、sandboxのネットワーク確認は対象外 |
| `PreToolUse`（`AskUserQuestion`のみ） | `tool_use_id`, `tool_input.questions`から質問を記録 | 実行前の通知。通常の文章による質問は捕捉せず、回答・表示完了も保証しない |
| `Stop` | `last_assistant_message`, `stop_hook_active`から応答終了を記録 | タスク完了とは限らない。ユーザー中断では発火せず、APIエラーは別イベント |
| `UserPromptSubmit` | `prompt`から入力再開の候補を記録 | 権限ダイアログやツール内の回答をすべて捕捉するものではない |

commandフックはstdinでJSONを受け取ります。共通の`session_id`・`hook_event_name`で関連付け、受信時刻を付ける想定です。
`cwd`・`transcript_path`等も届きますが、全文ログを読む必要はありません。必要なフィールドだけを長さ制限付きで記録し、重複・欠落・待ち状態の解除を扱う設計が必要です。
回答済みの確定には、対応する`PostToolUse`等との照合を別途検討します。

設定の形だけを示します。**`/UNIMPLEMENTED/tasklens-hook`は存在しないプレースホルダーです。そのまま設定に入れないでください。**
実装後に既存のClaude Code設定へマージする想定で、既存hooksは上書きしません。

```json
{
  "hooks": {
    "PermissionRequest": [{"hooks": [{"type": "command", "command": "/UNIMPLEMENTED/tasklens-hook"}]}],
    "PreToolUse": [{"matcher": "AskUserQuestion", "hooks": [{"type": "command", "command": "/UNIMPLEMENTED/tasklens-hook"}]}],
    "Stop": [{"hooks": [{"type": "command", "command": "/UNIMPLEMENTED/tasklens-hook"}]}],
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "/UNIMPLEMENTED/tasklens-hook"}]}]
  }
}
```

受信コマンドは短時間のローカル記録だけを行い、stdoutを空にして終了コード0を返す想定です。
許可・拒否・入力変更・Stopの継続指示は返さず、追加モデルも呼びません。
フック仕様は利用中のバージョンの[公式リファレンス](https://code.claude.com/docs/en/hooks)で確認してください。

**フックだけでは作業ツリーは作れません。** 目的・実績・現在位置・予定を安定したTask/Node ID付きで報告する仕組みが別途必要です。
その構造化進捗とフック情報だけで必要な表示を作れれば、Herdr入力を使わない構成も可能です。
既存のHerdr TUI/coreアダプターは維持し、Claude以外の担当や補助的な画面情報が必要な場合に選択できます。

## 開発

```sh
go test -race ./...
go vet ./...
go build -o task-lens .
./task-lens --config command-demo-config.json
```

最後のコマンドは同じバイナリのダミー変換器を実行します。モデルは呼びません。
GitHub ActionsでmacOS / Linuxのテストと、両OSのamd64 / arm64バイナリをビルドします。
成果物はActionsのArtifactsから取得できます。展開後は`chmod +x task-lens`を実行してください。

## 制限

状態はメモリ上にあり、再起動で消えます。Herdr入力は現在画面で、完全な会話履歴ではありません。
永続的な費用上限、完全な会話差分取得、待ち時間順ソート、編集・返信は未実装です。
外部モデルを使う場合は、取得内容と前回状態の送信範囲を確認してください。
公開サンプルには実データを含めていません。
