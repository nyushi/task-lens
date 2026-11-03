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
    "max_input_bytes": 16000, "max_output_bytes": 8000
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
| `herdr-tui` | `socket`, `panes` | Herdr TUIの指定pane。詳細は[専用文書](herdr-tui.md) |
| `herdr-core` / `herdr` | `socket`, `terminals` | 選択した端末の画面とメタデータ |

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

## ローカルHTTP

固定URLは`http://127.0.0.1:8765/`です。`--port N`を指定するとポートのみ変わります。
同じポートで再起動すれば同じURLになります。ポート使用中は起動エラーで、別ポートへ自動変更しません。
`--port 0`を明示した場合だけOSが空きポートを選ぶため、URLは固定されません。

既定は127.0.0.1限定、GETのみです。以前のランダムパスはURLを知るクライアントだけにアクセスを限定するためでした。
現在はURLトークン・Cookie・秘密ヘッダーを使用しません。Hostは127.0.0.1/localhostと実ポートに限定し、
Origin・Refererがあれば同一オリジンを要求します。Sec-Fetch-Siteのcross-site/same-siteも拒否します。
外部サイトのリンクから開けない場合は、アドレスバーかブックマークから直接開いてください。

`/state`には非秘密の`X-TaskLens-Request: 1`ヘッダーが必要です。Web画面は自動で付けます。
これにより他サイトの通常のフォーム・埋め込みからは取得できず、カスタムヘッダー付きfetchのpreflightも拒否します。
CORS許可は返しません。キャッシュ無効、CORP same-origin、CSP frame-ancestors noneで応答します。

ローカル確認用の例：

```sh
curl -H 'X-TaskLens-Request: 1' http://127.0.0.1:8765/state
```

これはユーザー認証ではありません。同じPCのプロセスや他ユーザーはヘッダーを付けて読めます。
共有PCで機密データを扱う場合は別のアクセス制御が必要です。
入力データやプロンプトから設定を書き換えません。外部コマンドは信頼するものだけを設定してください。
`*.local.json`はGit管理対象外です。ローカルのソケットや設定は公開しないでください。

従来の`herdr`はcore入力の互換名です。TUIには`herdr-tui`を選択してください。

外部commandは[担当別増分契約v2・予算](incremental.md)を使います。observationsのない全量入力は拒否します。

## 外部待ち受け

トップレベルの設定例：

```json
{
  "http": {
    "listen": "0.0.0.0:8765",
    "allow_remote": true,
    "allowed_hosts": ["192.168.1.10:8765", "tasklens.internal:8765"]
  },
  "old_after_seconds": 3600
}
```

`listen`は数値IP:port（IPv6は`[::]:8765`形式）。非ループバックはallow_remote=trueとallowed_hostsの両方が必須です。
allowed_hostsは接続先の正確なhost:portで、ワイルドカード・別ポート・port 0は拒否します。
ループバックでallowed_hostsを省略した場合だけlocalhost/127.0.0.1/[::1]と実ポートを許可します。
CLIの`--listen`、`--allowed-hosts`、`--allow-remote`は対応する設定を上書きします。
従来の`--port`も使用できます。同時指定時はlistenのポートを--portで上書きします。

Host検証で未許可ホスト名へのDNS rebindingを拒否し、Origin/Referer/Fetch Metadata、非秘密APIヘッダー、CORS拒否も維持します。
これらは他サイトのブラウザ要求を制限する仕組みで、ネットワーククライアントの認証ではありません。
許可Hostを送るHTTPクライアントはデータを読めます。HTTPは暗号化されず、認証トークンも発行しません。
外部公開時には起動時警告を表示します。ファイアウォールやOS設定は変更しません。

## 更新の古さ

`old_after_seconds`は古いカードの表示閾値で、0または省略時は3600秒です。削除条件とは独立しています。
更新時刻はTask.updatedAtを使います。担当別変換は採用した観測のobservedAt（なければ採用時刻）を記録し、
単なる再取得で更新時刻を進めません。fixture/recordedは入力の時刻を保持します。
時刻不明・不正値・未来時刻は古いと断定せず、別の表示にします。表示中は時計で相対時間を再計算します。
