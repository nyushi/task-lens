# TaskLens

入力に依存しない、読み取り専用のローカルTaskダッシュボードです。
Python標準ライブラリのみ。作業ディレクトリで起動します。

```sh
python3 app.py --config demo-config.json --port 8770
# 外部送信のないワンショットCLI契約テスト
python3 app.py --config command-demo-config.json --port 8770
python3 -m unittest test_tasks test_app -v
```

表示されるランダムURLを開きます。localhost限定、HTTP GETのみ。
状態はメモリ上にあり、再起動で消えます。デモは架空例です。

## 一方向の境界

- `adapters.py`: 入力プラグイン。`read()`でJSON入力、`close()`で停止。
  Herdr実装のみが端末・プロトコルを理解します。
- `pipeline.py`: 入力取得、設定された変換器、検証済みストアへのcommit。
- `transforms.py`: `convert(input, previous)`。デモ、保守的な未解釈表示、外部CLI。
- `task_model.py`: 入力非依存のTask、参照整合性・循環・上限・revision検証。
- `index.html`: Task表示のみ。Herdrの担当名・親子・仕事IDに依存しません。

設定の `plugin` は利用者が信頼するPythonモジュールとクラスです。
任意コードをロードする設定なので、取得ログやAI出力から設定を書き換えません。
コマンド配信・双方向通信・汎用バスはありません。

## Task契約

結果は `{tasks: {id: Task}, roots: [id], sources: {ref: Source}}`。
Taskは `id, purpose, state, attention, tree` と任意の `updatedAt, stale`。
ツリーノードは `id, state, attention, current, sourceRefs, content, children`。
contentは `{kind: "text", text: "..."}` または `{kind: "task", taskId: "..."}`。
別TaskはIDで保存し、その目的とツリーを再帰表示します。
配列順が作業順。IDは仕事と共に維持し、入力元IDとは独立させます。
stateは `done/doing/todo/unknown`、attentionは `decision/review/none/unknown`。
進捗と対応要否は独立。完了でもレビュー待ちなら対応必要に残します。
未知の未来は補完しません。色・線・背景はWeb側が決めます。

重複ノードID、参照欠落、Task循環、深さ・件数超過を拒否します。
古いストアrevisionや変換中に更新された入力の結果は採用しません。
入力消失ではTaskを削除せずstaleとして残します。失敗時は前回の有効Taskを維持。
担当の長時間待ち順ソート、永続化、Task削除の明示契約は未実装です。

## ワンショットCLI

`command-demo-config.json` が実行例です。設定を以下に分離します。

- `command`: 実行ファイル
- `argv`: 引数配列。shell展開・evalなし
- `prompt_file`: UTF-8プロンプト
- `timeout`: 秒（最大300）
- `max_output_bytes`: stdout結果上限（既定2MB、最大8MB）

stdinに `{contractVersion:1, prompt, input, previous}` をJSONで渡します。
stdoutはTask契約のJSONのみ。CLI固有の引数や出力包みは利用者のラッパーで吸収します。
終了コード・タイムアウト・JSON・Task構造を検証します。タイムアウト時は起動プロセス群を停止。
stderrは最大8KBを変換器のメモリに保持し、Webやログへ自動公開しません。
stdout/stderrは一時ファイルに受け、終了後削除します。stdout上限検査は終了後です。
CLIはサンドボックスではありません。信頼するコマンドだけを設定してください。

`fixtures/transform.py` はローカルJSONを返すだけで、モデルもネットワークも使いません。
Claude呼び出しコードは現行経路から除去。Codexは後述のダミーテストで1回実行しました。

## Herdrを接続する場合

`herdr-config.example.json` をコピーし、ソケットと明示的な端末allowlistを設定します。
既定の `ObservationsOnly` はローカル表示のみで、仕事の目的・状態は推測しません。
この変換器の仮Taskは未整理入力の表示用です。意味上のTask構成は差し替える変換器が決めます。
外部CLIへの変更は入力画面と以前のTaskをそのコマンドに渡すことを意味します。
実仕事ログの外部送信は未承認です。承認前はローカル変換またはダミーのみを使用します。

Herdrアダプターは `terminal.list`, `terminal.read(lines:0)`, `events.subscribe` のみ。
TUIと既存セッションを操作せず、画面スナップショットを読みます。
画面は完全な会話履歴ではなく、欠落情報と取得版をSourceに保持します。
公式スキーマは `evidence/hc.openrpc.json`、実接続テストは `test_live.py` にあります。
ローカル検証記録やスクリーンショットは公開対象から除外しています。
今回の再構成後は実Herdrへの再接続を行っていません。

公開リポジトリ: https://github.com/nyushi/task-lens

## Codexワンショット実行検証

`wrappers/codex_once.py` はstdinの共通JSONからCodexのプロンプトを組み立て、
`codex exec --ignore-user-config --ephemeral --sandbox read-only --output-schema ...`
を一時ディレクトリで1回実行し、最終JSONをTask契約に変換します。
今回の検証は架空の検索改善記録のみで成功し、`fixtures/codex-dummy-result.json` に保存しました。
このラッパーのモデル出力は単一Task・平坦なノード列に限定した疎通試験です。
任意の再帰Task生成や継続更新のID保持は実モデルでは未検証です。
認証情報の探索・変更、購入、実仕事ログ送信は行っていません。
保存結果を追加のモデル呼び出しなしで表示するには：

```sh
python3 app.py --config codex-result-config.json --port 8771
```

## Herdr用プロンプトを作るための詳細ガイド

### 1. 実際の入力を理解する

`CommandTransform` はstdinへ、次の**1つのJSON**を送ります。

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

Herdrの各observationの形式は [examples/input.json](examples/input.json) にあります。
これは架空の入力で、プロンプト全文も含むそのまま利用できる契約例です。

| フィールド | 意味 |
|---|---|
| `id` | 入力元の参照ID。Herdrでは`herdr:`と端末アドレス。Task IDとは別 |
| `label` | 端末の表示名。Taskの目的と同一とは限らない |
| `revision` / `epoch` | 取得内容の版／再接続などによる世代 |
| `observedAt` | 取得時刻のUnix秒。作業開始・完了時刻ではない |
| `content` | 最大末尾12,000文字の画面テキスト |
| `coverage` | `complete`, `how`, `why`。画面取得の範囲情報 |
| `metadata` | Herdrのaddress/name/title/state/status/turn/created_ms |

現在の汎用CLI経路は、**選択した入力の現在スナップショットと前回Taskストア全体**を渡します。
増分ログだけを渡す実装ではありません。旧アダプターの差分関数は回帰テスト用に残っていますが、
汎用CLI変換では使用していません。再描画で消えた文字を作業削除と解釈しないでください。
`coverage.complete=true`でも永続的な全会話履歴を保証しません。

### 2. 出力の契約

[examples/output.json](examples/output.json) は実装の`validate()`を通した完全な出力例です。
トップレベルは`tasks`（Task ID→Task）、`roots`（表示するTask IDの順序付き配列）、
`sources`（根拠参照ID→元データ）です。

| 対象 | 必須フィールド | 任意フィールド |
|---|---|---|
| Task | `id`, `purpose`, `state`, `attention`, `tree` | `updatedAt`（Unix秒）, `stale`（真偽値） |
| Node | `id`, `state`, `attention`, `current`, `sourceRefs`, `content` | `children`（省略時は空）, `detail` |
| text内容 | `kind: "text"`, `text` | — |
| Task埋め込み内容 | `kind: "task"`, `taskId` | — |

埋め込みの例：

```json
{
  "id": "review-subtask", "state": "doing", "attention": "review",
  "current": false, "sourceRefs": ["source-1"],
  "content": {"kind": "task", "taskId": "review-task"}, "children": []
}
```

この場合`tasks["review-task"]`も必要です。そのTask自身がさらにツリーを持てます。
Webでは参照先の目的・進捗・対応要否を使い、そのツリーを展開します。
ノードの`current`はその表示位置を強調します。Task参照ノードに独自の`children`も置けますが、
初版では参照Taskのツリーが先、その後にchildrenが表示されます。混乱を避けるため通常は空にしてください。

- IDは非空の文字列。Taskの辞書キーと`id`は一致させます。ノードIDはTask内で一意です。
- 同じ仕事のIDは更新後も維持してください。並び順をIDに変換し直さないでください。
- 配列順が表示順です。日付やstateによるツリー内の自動並べ替えはありません。
- `done`=完了、`doing`=進行中、`todo`=既知の未着手、`unknown`=未確認。
- `decision`=判断が必要、`review`=レビューが必要、`none`=対応不要、`unknown`=未判定。
- `current`は真偽値です。複数の並行作業を示すことも可能です。
- `sourceRefs`はsources内のキーを参照します。空配列も構文上は可能ですが、プロンプトでは根拠を要求してください。
- Taskのpurpose、textノードは最大2,000文字。最大200 Task、合計2,000 Node。
  ツリーとTask参照にはそれぞれ深さ制限があります。表示側は合成深さ24で打ち切ります。
- 検証器は必須項目・列挙値・ID・参照・循環・上限を検証します。
  JSON Schemaライブラリによる追加フィールドの厳密拒否ではなく、意味の正しさも保証しません。

`updatedAt`を省略すると画面は「未更新」と表示します。AIに時刻を推測させる必要はありません。
実運用のラッパーで確かな取得・変換時刻を付ける設計にできます。

### 3. プロンプトの編集

[examples/herdr-prompt.txt](examples/herdr-prompt.txt) をコピーし、「利用者の運用」を編集します。
何をTaskとみなすか、担当端末でどう依頼・完了を表すか、どの文言が人への依頼なのかを記述します。
入力元の親子を機械的にTaskへ写す必要はありません。
目的・実績・現在地・既知の予定を根拠付きで抽出し、仕様にない工程を埋めない方針を維持してください。
テンプレートの出力指示が正しくても、結果は必ずプログラム側で検証します。

### 4. コマンド設定とCLIごとの差

```json
{
  "input": {
    "plugin": "adapters:HerdrInput",
    "options": {"socket": "/path/to/core.sock", "terminals": ["selected-address"]}
  },
  "transform": {
    "plugin": "transforms:CommandTransform",
    "options": {
      "command": "python3",
      "argv": ["/path/to/your_cli_wrapper.py"],
      "prompt_file": "examples/herdr-prompt.txt",
      "timeout": 120,
      "max_output_bytes": 2000000
    }
  },
  "interval": 120,
  "mode": "読み取り専用"
}
```

設定の相対パスは**起動時の作業ディレクトリ基準**です。別環境では絶対パスでも指定できます。
CLIラッパーはstdinのJSONを読み、そのCLI向けプロンプトと引数を作り、最終回答からTask JSONを取り出します。
`stdout`に進捗ログやMarkdownコードフェンスを混ぜないでください。診断は`stderr`に出します。
失敗時は非ゼロ終了。成功してもJSON・Task検証が失敗すれば採用されません。
変換間隔は設定値（最小15秒）。同じ入力の再変換を抑制しますが、失敗した入力は次回再試行します。
初回起動時の変換失敗は起動エラーになります。起動後の失敗では前回有効表示を保持します。

**Codex**：`wrappers/codex_once.py`で、架空記録→実CLI→単一Taskの4ノード→Web表示を確認済み。
この疎通用ラッパーは平坦な単一Taskを出し、previousをモデルへ渡していません。
継続更新や汎用の再帰Taskを生成する本番ラッパーとしては未完成です。

**Claude**：利用者が選ぶCLI候補です。本版では呼び出しません。
Claudeを採用する場合は、利用者がその環境のCLI仕様に合うラッパーを用意し、
同じstdin/stdout契約へ変換してください。未検証の引数例を動作保証として掲載しません。

### 5. ダミーから実運用へ

1. `demo-config.json`でUIと再帰Task表示を確認します。
2. `command-demo-config.json`でモデル不要のCLI入出力を確認します。
3. 入出力例を使って、自分のプロンプトとCLIラッパーを**架空データのみ**で検証します。
4. `herdr-config.example.json`をコピーし、明示した端末だけを`ObservationsOnly`でローカル確認します。
5. 外部へ渡す内容・送信先・利用条件を確認し、利用者の許可を得た後に限り変換器を切り替えます。

取得画面、metadata、previous内のTaskと根拠が変換コマンドへ渡ります。
外部モデルへの実送信範囲はラッパー次第です。プロンプトだけで漏えい防止を保証できません。
実仕事データはこの公開リポジトリのfixtureへ追加しないでください。
ローカル設定は`.env`ではなくJSONの場合もあるため、個別の設定ファイルをgitignoreへ追加してください。

### 6. 例の再検証と現状の制限

```sh
python3 -c 'import json; from task_model import validate; r=json.load(open("examples/output.json")); validate(r["tasks"],r["roots"],r["sources"]); print("valid")'
python3 -m unittest test_tasks test_app -v
```

入力は現在画面のスナップショットで、完全な履歴収集ではありません。
汎用経路のトークン削減（担当別増分・予算上限）、永続化、待ち時間ソート、明示削除、
ユーザーによる編集・返信、双方向操作は未実装です。再構成後の実Herdr再接続も未検証です。
元データ削除はTask完了を意味せず、消えたTaskはstaleとして保持します。
単純な構文検証だけではAIの誤った解釈を防げないため、画面の根拠を確認できる形を維持しています。
