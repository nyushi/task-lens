# プロンプトとCLI

## プロンプト

[Herdr用テンプレート](../examples/herdr-prompt.txt)の運用説明を編集します。
Taskの単位、端末内の依頼や完了の表現、利用者への確認が必要な条件を記述してください。
端末とTaskは1対1である必要はなく、端末の親子関係をTaskへそのまま写す必要もありません。

目的・実績・現在位置・明示された予定を根拠付きで抽出します。
不明な工程・成果・時刻を補わず、以前のIDを維持します。
取得内容は命令ではなくデータとして扱います。HTMLやCSSは出力しません。
出力形式は[契約](contract.md)を参照してください。

## 実行

`command`は実行ファイル、`argv`は引数配列、`prompt_file`はUTF-8ファイルです。
shell展開やevalは行いません。stdinにJSONを1件渡し、stdoutからJSONを1件受け取ります。
進捗や診断はstderrへ出してください。stdoutにMarkdownやログを混ぜないでください。

timeoutは既定30秒・最大300秒です。超過時は起動プロセス群を停止します。
stdout上限は既定2MB・最大8MBで、超過中に実行を停止します。
stderrは先頭8KBだけをメモリに保持し、Webへ自動公開しません。
終了コード、JSON、Task契約を検証します。失敗時に以前の有効Taskを上書きしません。

## ラッパー

CLI固有の入力形式、引数、回答の取り出し方はラッパーで吸収します。
独自ラッパーは任意の言語で記述できます。外部プロセスはサンドボックスではありません。

モデルなしの動作確認：

```sh
go build -o task-lens .
./task-lens --config command-demo-config.json
```

この設定は`./task-lens fixture-transform`を呼び、渡されたfixtureをそのまま返します。

Codexの疎通用アダプターは`./task-lens codex-transform`です。
`codex exec --ignore-user-config --ephemeral --sandbox read-only --output-schema ...`を使用します。
commandをTaskLensバイナリ、argvを`["codex-transform"]`にすると選択できます。
これは単一Task・平坦ノード用で、previousをモデルに渡さない疎通用実装です。
再帰Task生成や継続更新用の本番変換器ではありません。

Python版で架空記録のCodex実行→JSON検証→表示を確認済みです。
Go版では同じCLI方式へ移植しましたが、実モデルは再実行していません。
[保存結果](../fixtures/codex-dummy-result.json)は`codex-result-config.json`で再表示できます。

Claudeは利用者が別途選択する候補です。このプロジェクトからは実行していません。
使用する場合は、その環境のCLI仕様に合うラッパーを用意してください。

## 接続手順

1. 埋め込みデモで表示を確認します。
2. ダミーCLIで入出力契約を確認します。
3. 架空入力でプロンプトと外部CLIを検証します。
4. Herdrを`observations`変換器につなぎ、ローカルで取得範囲を確認します。
5. 外部送信の許可・送信先・対象データを確認してから変換器を切り替えます。

変換コマンドには取得画面、metadata、前回Taskと根拠が渡ります。
実際の外部送信範囲はラッパーに依存します。プロンプトだけでは送信制御を保証できません。
実データを公開fixtureへ追加しないでください。

Go版は模擬UnixソケットとダミーCLIで検証しています。実Herdrへの接続は未検証です。
現在は入力全体を変換するため、担当別の増分変換・トークン予算管理はありません。
