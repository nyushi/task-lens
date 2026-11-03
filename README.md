# TaskLens

入力データをTaskとツリーに変換し、ローカルWebで表示するツールです。
Herdr TUI、herdr-core、JSONファイル、外部コマンドを入力にできます。入力・変換・表示は一方向で、返信や操作は行いません。

## 起動

Go 1.24以降。macOS / Linux対応。外部ライブラリは使用していません。

```sh
go build -o task-lens .
./task-lens
```

表示されたlocalhostのURLを開きます。既定では埋め込みのサンプルを表示します。
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
