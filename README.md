# herdr-plugin-sidenote

herdr の母艦 space の右 pane に常駐する TUI プラグイン。各 space（workspace）の
稼働状況と「今やっていることの進捗」を 1 画面に一覧表示する。

設計の詳細は [docs/design.md](docs/design.md) を参照。

## しくみ（二層構成）

- **骨格情報**: `herdr workspace list` を低頻度ポーリングして取得（space 一覧・
  ラベル・agent_status）。母艦が書く必要はない。
- **意味情報**: 母艦が `sidenote set/update` で state ファイル（space ごと JSON）に
  書く「今やっていること・進捗・次のアクション・ブロッカー」。

TUI はこの 2 つを workspace_id（無ければ space 名）で突合してマージ表示する。
state ファイルの変更は fsnotify で即時反映し、herdr の骨格は数秒間隔で追従する。

## ビルド

```sh
go build -o bin/sidenote ./cmd/sidenote
```

## 使い方

state ディレクトリの既定は `~/.herdr/sidenote/state/`（環境変数
`SIDENOTE_STATE_DIR` または `--dir` で変更可）。

### 母艦 space の右 pane に常駐させる

```sh
# 母艦 space の pane を右に分割して TUI を起動
NEW_PANE=$(herdr pane split <母艦pane> --direction right --no-focus \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["result"]["pane"]["pane_id"])')
herdr pane run "$NEW_PANE" "sidenote watch"
```

### 母艦が状況を書き込む

```sh
# 全体設定（初回など）
sidenote set --space herdr-plugin-sidenote --workspace-id w2F \
  --headline "設計ブレスト中" --status working --next "設計Docを書く" \
  --percent 40 --step "ブレスト:done" --step "設計Doc:doing"

# 一部だけ更新（既存にマージ、updated_at は自動更新）
sidenote update --key w2F --status review --percent 80

# 記録を削除
sidenote clear --key w2F
```

`status` は `planning` / `working` / `blocked` / `review` / `done`。
`--step` は `"ラベル:状態"`（状態は `todo` / `doing` / `done`）で繰り返し指定可。
`--blocker` / `--note` も繰り返し指定できる。

### デバッグ

```sh
sidenote list          # state を JSON でダンプ
```

## サブコマンド

| コマンド | 役割 |
|---|---|
| `sidenote watch`  | TUI 常駐（表示） |
| `sidenote set`    | space の状況を全体設定 |
| `sidenote update` | space の状況を部分更新 |
| `sidenote clear`  | space の記録を削除 |
| `sidenote list`   | state をダンプ |

## 開発

```sh
go test ./...     # 全テスト
go vet ./...
```

純ロジック（スキーマ検証・atomic write・部分マージ・骨格マージ・鮮度判定）は
テストで担保し、TUI 描画・監視ループは手動で動作確認する方針。
