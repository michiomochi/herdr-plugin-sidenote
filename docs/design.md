# herdr-plugin-sidenote 設計ドキュメント

- ステータス: Draft（人間承認ゲート1 待ち）
- 作成日: 2026-07-14
- 対象リポジトリ: `michiomochi/herdr-plugin-sidenote`（ローカル先行開発）

---

## 1. 概要

herdr の「母艦 space」の右 pane に常駐する **TUI プラグイン** を作る。母艦（全 space を統括する AI）と各 space（workspace）で動くエージェントの「今の状況」と「進捗」を 1 画面に一覧表示し、母艦・人間が全体の動きを一目で把握できるようにする。

状況の受け渡しと永続化は **ファイル経由** で行う。母艦が state ファイルを更新すると、TUI がそれを読み取っていい感じに一覧表示する。

## 2. 背景・目的

### 背景

herdr は terminal-native なエージェント多重化ツールで、複数の workspace / tab / pane に跨ってエージェント（Claude 等）を並行稼働させられる。母艦 → team-lead → engineer のような多層構成で作業が進むと、「今どの space が何をしていて、どこまで進んでいるか」を人間・母艦が把握しづらくなる。

`herdr workspace list` は各 space の稼働状況（label / agent_status / pane 数）を機械的には持っているが、「何のタスクを、どこまで、次に何を」という **意味的な文脈** は herdr の関知外である。

### 目的

- 全 space の **稼働状況（骨格）** と **意味的な進捗（肉付け）** を 1 つの常駐 TUI に集約表示する。
- 母艦の **書き込み負担を最小化** する（骨格は herdr から自動取得し、母艦は意味情報だけを書く）。
- 状態は人間が読める **ファイル** に永続化し、TUI や herdr の再起動をまたいで残す。

### 非目標（YAGNI）

- 母艦や各エージェントの操作・制御（本プラグインは表示と状況記録に徹する。pane 操作は herdr CLI の役割）。
- 時系列の詳細な履歴分析やメトリクス集計（将来拡張余地としてのみ検討。§9 参照）。
- Web UI やリモート配信（ローカルの TUI に限定）。

## 3. 用語

| 用語 | 意味 |
|---|---|
| 母艦 | 全 space を統括する親 AI（本環境では HQ workspace で稼働する Claude）。 |
| space / workspace | herdr のプロジェクト単位。`herdr workspace list` の 1 要素。 |
| 骨格情報 | herdr が自動で持つ構造情報（workspace_id / label / agent_status / pane 数 / cwd）。 |
| 意味情報 | 母艦にしか書けない文脈（今やっていること / 進捗 / 次のアクション / ブロッカー）。 |
| state ファイル | space ごとの意味情報を保持する JSON ファイル。 |
| state ディレクトリ | state ファイルを集約するディレクトリ（既定 `~/.herdr/sidenote/state/`）。 |

## 4. 全体アーキテクチャ（二層構成）

sidenote は情報源を 2 層に分離する。

- **骨格層（構造情報）**: TUI が `herdr workspace list` / `pane list` を **低頻度ポーリング** して取得する。space の存在・label・agent_status・pane 数・cwd。母艦は一切書かなくてよい。
- **肉付け層（意味情報）**: 母艦が `sidenote set/update` で state ファイルに書く。TUI は state ファイルを **ファイル監視（fsnotify）** で追従する。

TUI は両者を `workspace_id`（fallback で `label`）で突合してマージし、1 行 1 space の一覧として描画する。

```
              ┌──────────────────────────────────────────────┐
   母艦(AI) ──┤ sidenote set/update (atomic write: tmp→rename)│
              └───────────────────┬──────────────────────────┘
                                  ▼
                    state/<workspace_id>.json   ← 意味情報（肉付け）
                                  │  fsnotify で即時検知
                                  ▼
   herdr workspace/pane list ─▶ ┌─────────────────────────────┐
     (label/agent_status/cwd)   │   sidenote watch (TUI)       │
      2〜3秒ポーリング  ───────▶ │  骨格 × 肉付け をマージ表示  │
                                 └─────────────────────────────┘
                                    母艦 space の右 pane に常駐
```

この分離により、母艦は「変化した space の意味情報だけ」を書けばよく、workspace の増減・稼働状態は TUI が自律的に追従する。

## 5. AI（母艦）からの状況入力方式

### 採用: space ごと 1 ファイル + ディレクトリ監視

- state ディレクトリ配下に、space 単位で `<workspace_id>.json` を 1 ファイルずつ置く（例 `w2F.json`）。
- 母艦が状況を更新する時は、その space の 1 ファイルだけを書き換える（部分更新が自然）。
- 書き込みは **atomic write（同ディレクトリ内の一時ファイルに書いてから `rename`）** を必須規約とする。`rename` は同一ファイルシステム上でアトミックなため、TUI が書き込み途中の壊れた JSON を読む事故を防げる。
- TUI は state ディレクトリを監視し、ファイルの作成・更新・削除を検知して再描画する。ファイルの増減はそのまま「表示対象 space の増減」に対応する。

### 母艦の書き込み経路: `sidenote set` CLI（既定）

母艦（Claude）が JSON を直接書くのは、スキーマずれ・書き込み途中の破損・エスケープ事故のリスクがある。これを避けるため、**書き込み用 CLI サブコマンドを提供**し、母艦はコマンドを叩くだけにする。

```bash
# 全体を設定（未指定項目は既定値/空）
sidenote set --space herdr-plugin-sidenote --workspace-id w2F \
  --headline "設計ブレスト中" --status working --next "設計Docを書く"

# 一部フィールドだけ部分更新（既存にマージ）
sidenote update --workspace-id w2F --status review --percent 80

# その space の記録を消す（または done マーク）
sidenote clear --workspace-id w2F
```

CLI 側が atomic write・スキーマ検証・`updated_at` の自動付与を内包する。母艦の JSON 直書きも技術的には可能なままにするが、正規経路は CLI とする。

## 6. 記録ファイル形式（スキーマ）

### 形式: JSON（space ごと 1 ファイル）

母艦（Claude）が構造化データを最も誤りなく生成でき、どの実装言語でもパースが容易なため JSON を採用する（TOML/YAML はネスト・配列で誤記しやすく、plain は構造化に弱い）。

### スキーマ

```json
{
  "schema_version": 1,
  "space": "herdr-plugin-sidenote",
  "workspace_id": "w2F",
  "headline": "設計ブレスト中",
  "status": "working",
  "progress": {
    "summary": "4軸を整理中",
    "steps": [
      { "label": "ブレスト", "state": "done" },
      { "label": "設計Doc",  "state": "doing" },
      { "label": "実装",     "state": "todo" }
    ],
    "percent": 40
  },
  "next": "設計Docを書く",
  "blockers": [],
  "notes": [],
  "updated_at": "2026-07-14T10:50:00+09:00"
}
```

### 項目定義

| 項目 | 必須 | 型 | 説明 |
|---|---|---|---|
| `schema_version` | ○ | int | スキーマのバージョン。CLI が自動付与。 |
| `space` | ○ | string | 人間可読ラベル。herdr の `label` と突合する。 |
| `workspace_id` | △ | string | herdr の workspace id。突合の第一キー。省略時は `space` で突合。 |
| `headline` | ○ | string | 「今やっていること」の一言。一覧の主役。 |
| `status` | ○ | enum | 母艦視点の意味的ステータス: `planning` / `working` / `blocked` / `review` / `done`。 |
| `progress.summary` | × | string | 進捗の短い説明。 |
| `progress.steps[]` | × | array | ステップ配列。各要素 `{label, state}`、`state` は `todo` / `doing` / `done`。 |
| `progress.percent` | × | int | 0〜100。任意。 |
| `next` | × | string | 次にやること。 |
| `blockers` | × | string[] | ブロッカー（空配列 = なし）。 |
| `notes` | × | string[] | 補足・直近の出来事。 |
| `updated_at` | ○ | string | RFC3339。CLI が書き込み時に自動付与。TUI の鮮度表示に使う。 |

**必須は `schema_version` / `space` / `headline` / `status` / `updated_at` のみ。** 他は母艦が書ける範囲で任意とし、段階的な記述を許容する。

### `status`（意味）と `agent_status`（骨格）の関係

- `status` は母艦が書く **意味的ステータス**（例: レビュー待ちなら `review`）。
- `agent_status` は herdr が自動検出する **稼働状態**（`idle` / `working` / `blocked` / `done` / `unknown`）。
- 両者は別軸であり、TUI は両方を並記できる（例: herdr 上は `idle` でも母艦視点は `blocked`、といった食い違いを可視化できる）。

### `schema_version` の意図

将来スキーマを変えても TUI が旧バージョンを検出して安全に扱えるようにする。TUI は未知の高いバージョンを読んだら「要更新」を薄く表示し、クラッシュしない方針とする。

### 将来拡張余地（今は作らない）

時系列の履歴が必要になったら、state ディレクトリに `events.jsonl` を追記併設する余地を残す（イベント append）。現時点の目的は「今の状況の一覧」なので状態再生は過剰であり採用しない（§9）。

## 7. TUI 技術選定

### 採用: Go + Bubble Tea（次点: Python + Textual）

| 候補 | 長所 | 短所 |
|---|---|---|
| **Go + Bubble Tea（採用）** | 単一バイナリ配布・起動が軽い・fsnotify 成熟・書込 CLI と表示 TUI を 1 バイナリに同梱可・クロスコンパイル容易 | リッチな装飾は Textual に一歩譲る |
| Python + Textual/Rich | 見た目・レイアウトが最も強力 | venv/uv 依存・起動が重め・常駐配布がやや面倒 |
| Rust + ratatui | 高速・単一バイナリ | ビルド/イテレーション速度が Go より遅い |
| Node + Ink | React ライクで書きやすい | node_modules・起動が重い |

### 採用理由

1. **単一バイナリ**で `herdr pane run <pane> "sidenote watch"` から即起動でき、右 pane 常駐が安定する。
2. **起動が軽い** = 常駐用途に最適。
3. **書込側と表示側を 1 バイナリのサブコマンドに同梱**でき、配布が 1 つで済む。
4. fsnotify によるファイル監視、`herdr workspace list` の呼び出し（`os/exec`）がいずれも素直。

> 注: この言語選定は個人リポの保守者（人間）の選好が効く最重要判断であり、**人間の承認を仰ぐ**（§12 / ゲート1）。Python + Textual への変更も可能。そのため本ドキュメントの §4〜§6・§8（アーキ / ファイル形式 / 組込）は言語非依存に保ち、言語変更時に書き直す範囲を §7・§11（実装計画）に閉じ込めている。

### サブコマンド構成

| コマンド | 役割 | 使う主体 |
|---|---|---|
| `sidenote watch` | TUI 常駐（表示） | TUI（右 pane） |
| `sidenote set` | space の状況を全体設定（atomic write） | 母艦 |
| `sidenote update` | 指定フィールドのみ部分更新（既存にマージ） | 母艦 |
| `sidenote clear` | space の記録を削除／done 化 | 母艦 |
| `sidenote list` | state をダンプ（デバッグ用） | 人間 |

## 8. 母艦右 pane への組み込み方

### 運用フロー（母艦の体験）

1. 母艦が自 space（例 HQ = `w27`）の pane を右に分割:
   ```bash
   herdr pane split w27:p1 --direction right --no-focus
   ```
2. 返ってきた新 pane id で TUI を起動:
   ```bash
   herdr pane run <newpane> "sidenote watch"
   ```
3. 以降、母艦は状況が変わるたびに `sidenote set/update` を叩くだけ。**TUI には一切触れない。**
4. TUI が state ファイル変更と `workspace list` を自律的に追従して再描画する。

### リフレッシュ方式: fsnotify + ポーリングのハイブリッド

- **state ファイル**: fsnotify でイベント駆動。母艦が書いた瞬間に反映される。
- **骨格情報（agent_status 等）**: ファイル変更を伴わない herdr 内部状態のため、`herdr workspace list` を **2〜3 秒間隔で低頻度ポーリング**する（CLI 実行コストは小さい）。
- 純ポーリングのみだと意味情報の反映が遅く、純ファイル監視のみだと agent_status の変化を拾えない。両立のためハイブリッドとする。

### 配布・起動の規約

- `sidenote` バイナリを PATH に配置（または `./bin/sidenote`）。
- state ディレクトリ既定は `~/.herdr/sidenote/state/`。環境変数 `SIDENOTE_STATE_DIR` で上書き可能とし、書込側（母艦）と読込側（TUI）が同じ既定を共有する。
- state ディレクトリが存在しなければ CLI / TUI が起動時に自動作成する。

### 表示イメージ（1 行 1 space）

```
 space                        herdr    母艦        headline / next            更新
 herdr-plugin-sidenote        working  ● working   設計Docを書く              12s前
 氏名突合セルフホスティング    done     ✔ review    レビュー指摘対応中          3m前
 FATCA取引時確認              done     ✔ done      完了                       1h前
 利用規約PP同意取得           idle     ○ planning  要件整理                   （古い）2d前
```

古い情報（`updated_at` が一定時間より前）は薄く（グレーアウト）表示し、鮮度を可視化する。

## 9. 採用しなかった案

| 軸 | 不採用案 | 理由 |
|---|---|---|
| 入力方式 | 単一 JSON を毎回全体上書き | 母艦は複数 subagent / pane を並行稼働させるため、全体 read-modify-write は競合・ロストアップデートの温床になる。 |
| 入力方式 | 追記型 JSONL イベントログを主方式に | 「今の状況の一覧」が目的で、状態再生は過剰（YAGNI）。履歴が要れば将来 `events.jsonl` を併設する余地のみ残す。 |
| 入力方式 | 名前付きパイプ / ソケット | 永続性がなく TUI 再起動で状態が消える。母艦が書きにくい。ファイルの単純さ・永続性に劣る。 |
| 記録形式 | TOML / YAML | ネスト・配列で母艦が誤記しやすい。 |
| 記録形式 | plain text | 構造化に弱く、TUI でのパース・突合が困難。 |
| 骨格取得 | 母艦が workspace 一覧も手で書く | herdr が自動で持つ情報を二重管理させると書込負担と不整合が増える。CLI ポーリングで自動取得する。 |
| TUI 言語 | Python+Textual / Rust+ratatui / Node+Ink | §7 の比較表参照。常駐の軽さ・単一バイナリ配布・書込 CLI 同梱で Go を優先。 |
| リフレッシュ | 純ポーリング のみ | 意味情報の反映が遅い。 |
| リフレッシュ | 純ファイル監視 のみ | agent_status など herdr 内部状態の変化を拾えない。 |

## 10. リスクと緩和策

| リスク | 緩和策 |
|---|---|
| `workspace list` ポーリングのコスト | 2〜3 秒の低頻度に留める。差分がなければ再描画しない。将来 herdr にイベント購読 API が出たら移行。 |
| state ファイル肥大化 | 1 space 1 ファイルで小さく保つ。`notes` / `steps` は件数に上限を設ける。履歴は主方式に入れない。 |
| 母艦の書き込み忘れで情報が古くなる | `updated_at` を基準に古い行をグレーアウト。鮮度を明示して「信用できない情報」を見分けられるようにする。 |
| herdr id の compaction（tab/pane 開閉で id が変わる） | 突合は `workspace_id` を第一キー、`label` を fallback とする。`workspace list` に存在しない state ファイルは stale として薄く表示し、`sidenote clear` または鮮度判定で掃除する。 |
| herdr CLI の JSON スキーマ変更依存 | パースは必要フィールドのみを緩く読む（未知フィールドは無視）。取得失敗時は骨格なしで意味情報だけ表示し、TUI をクラッシュさせない。 |
| atomic write が別 FS だと非アトミック | 一時ファイルは **state ディレクトリと同一ディレクトリ内**に作成してから `rename` する。 |
| スキーマ将来変更 | `schema_version` で判定。未知の高バージョンは「要更新」を薄く表示し安全に無視。 |

## 11. 実装計画（Go 前提）

> 本セクションのみ言語依存。Python 採用時はここを差し替える。

### マイルストーン

1. **M1: state 読み書き + スキーマ**
   - Go struct でスキーマを定義（JSON marshal/unmarshal）。
   - atomic write ヘルパ（同ディレクトリ tmp → `rename`）。
   - バリデーション（必須項目・enum・percent 範囲）。
   - state ディレクトリ解決（`SIDENOTE_STATE_DIR` → 既定 `~/.herdr/sidenote/state/`、自動作成）。
2. **M2: 書込 CLI（`set` / `update` / `clear` / `list`）**
   - `set`（全体設定）/ `update`（部分マージ）/ `clear`（削除）/ `list`（ダンプ）。
   - `updated_at` の自動付与。
   - 母艦が叩く想定の引数設計とヘルプ。
3. **M3: watch TUI 表示**
   - Bubble Tea で state ディレクトリを読み込み、1 行 1 space の一覧を描画。
   - fsnotify でファイル変更を検知して再描画。
   - `updated_at` によるグレーアウト（鮮度表示）。
4. **M4: 骨格マージ + ポーリング**
   - `herdr workspace list` / `pane list` を `os/exec` で実行しパース。
   - `workspace_id`（fallback `label`）で骨格と意味情報を突合・マージ。
   - agent_status 表示、2〜3 秒ポーリング、差分時のみ再描画。
5. **M5（任意）: 磨き込み**
   - レイアウト・配色の調整、狭い pane 幅への対応、`events.jsonl` 拡張余地の検討。

各マイルストーンは、その単位で `go build` が通り、`go test`（スキーマ/atomic write/マージのユニットテスト）が緑になることを完了条件とする。

### verify（動作確認方法）

実装後、TDD で書いたユニットテストに加えて、実環境で end-to-end に確認する:

1. 母艦 space を右 split し、`herdr pane run <newpane> "sidenote watch"` で TUI を常駐させる。
2. 別 pane から `sidenote set/update` で複数 space の状況を書き込み、TUI に**即時反映**されることを確認（fsnotify）。
3. どこかの space でエージェントを稼働/停止させ、agent_status がポーリングで**追従**することを確認。
4. `updated_at` を古くした state を置き、**グレーアウト**表示を確認。
5. 壊れた JSON / 未知 schema_version を置いても TUI が**クラッシュしない**ことを確認。

## 12. 未決事項（人間承認ゲート1）

以下は Design Doc 完成後に人間へ一括承認を仰ぐ。

1. **TUI 言語**: Go + Bubble Tea を既定として起草。ただし個人リポ保守者の選好が効く最重要判断のため、**Python + Textual への変更も可**として人間の確認を仰ぐ。
2. **state ディレクトリ**: `~/.herdr/sidenote/state/`（`SIDENOTE_STATE_DIR` で上書き可）を既定として採用。
3. **母艦の書込方法**: `sidenote set` CLI 提供を既定として採用（JSON 直書きより安全）。

**この承認が出るまで実装には着手しない。**
