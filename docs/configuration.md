# 設定

`--config`を省略すると、バイナリに埋め込まれたサンプルを表示します。
JSONファイルを指定した場合、相対パスは起動ディレクトリ基準です。

```json
{
  "input": {"plugin": "herdr", "options": {
    "socket": "/path/to/core.sock", "terminals": ["selected-address"]
  }},
  "transform": {"plugin": "command", "options": {
    "command": "/path/to/wrapper", "argv": [],
    "prompt_file": "examples/herdr-prompt.txt", "timeout": 120,
    "max_output_bytes": 2000000
  }},
  "interval": 120,
  "mode": "読み取り専用"
}
```

## 入力

| plugin | options | 出力 |
|---|---|---|
| `demo` | なし | 埋め込みのサンプルTask |
| `recorded` | `path` | 保存済みTask JSONをfixtureとして読む |
| `file` | `path` | 任意の入力JSONオブジェクトを読む |
| `command` | `command`, `argv`, `timeout`, `max_output_bytes` | 外部プログラムへ`{}`をstdinで渡し、入力JSONをstdoutから読む |
| `herdr` | `socket`, `terminals` | 選択した端末の画面とメタデータ |

入力の共通部分は`connected`です。それ以外は変換器との契約で決めます。
新しい入力元は外部コマンドとして追加でき、バイナリの再ビルドは不要です。
Go内で拡張する場合は`Input`インターフェースと`makeInput`に実装を追加します。
Python版の既存組み込みplugin名は互換エイリアスとして受け付けますが、Pythonモジュールの動的ロードは行いません。

## 変換

| plugin | 動作 |
|---|---|
| `fixture` | 入力の`fixture`をTask結果として返す |
| `observations` | 入力のobservationsを未解釈のTaskとしてローカル表示 |
| `command` | [CLI契約](transforms.md)に従い外部コマンドを起動 |

`interval`は秒、既定120・最小15です。同じ入力では再変換を抑制します。
取得時刻`observedAt`だけの変更は無視します。失敗は次の周期で再試行します。

## Herdr

許可した端末だけを読みます。`terminal.list(all:true)`、`terminal.read(lines:0)`、
`events.subscribe`を使い、画面操作や送信はしません。
イベントのsnapshot/missed/再接続時は世代を更新し、古い変換結果を破棄します。
端末更新イベントも変換中の結果を無効化します。出力変化は周期的な画面取得でも確認します。

## ローカルWeb

127.0.0.1限定、ランダムURL、GETのみです。Hostを検証し、キャッシュを無効にします。
入力データやプロンプトから設定を書き換えません。外部コマンドは信頼するものだけを設定してください。
`*.local.json`はGit管理対象外です。ローカルのソケットや設定は公開しないでください。
