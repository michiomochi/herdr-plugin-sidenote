# done 履歴の append 運用 — 設計案

- ステータス: 承認済み・実装済み（2026-07-14）
- 作成日: 2026-07-14
- 対象: `michiomochi/herdr-plugin-sidenote`
- 前提: 複数行ブロック（`8d3c380`）＋ dim 2階調（`1da930e`）＋ グルーピング（`f707146`）と整合

## 1. 背景・目的

母艦は `sidenote set` で `progress.steps` を**上書き**するため、完了(done)項目が
蓄積されず消える。要望は以下:

- **済み(done)は過去の完了を消さず積み上げる**（append・履歴保持）。
- **予定は現在〜将来の予定を全件書ける**。
- 表示は各 space を **『済み／いま／予定』の3区分**でまとめる（済み=積み上げ履歴、予定=全件）。

以前の「`log[]` は後回し（done steps で十分）」判断を、本要望で見直し、
**done 履歴の append 運用**（蓄積領域＋append する CLI）を導入する。

## 2. スキーマ変更

### 2-1. `done_log`（State トップレベル・append 専用）

```json
{
  "schema_version": 1,
  "space": "...", "workspace_id": "...", "headline": "...", "status": "...",
  "progress": { "steps": [ ... doing/todo 中心 ... ], "percent": 40 },
  "next": "...", "blockers": [],
  "done_log": [
    { "at": "2026-07-14T10:00:00+09:00", "text": "設計Doc作成" },
    { "at": "2026-07-14T11:00:00+09:00", "text": "M1実装" }
  ],
  "updated_at": "..."
}
```

- `done_log: [{at: RFC3339, text: string}]` を **State 直下**に追加（`omitempty`）。
- **置き場所を progress 内でなく State 直下にする理由**: `done_log` は「現タスクの進捗
  スナップショット」である `progress`（summary/steps/percent）とは性質が異なる累積履歴で
  あり、かつ `set`（progress ごと上書き）から**独立に保持**したいため。トップレベルなら
  set の引き継ぎ実装が progress の有無に依存せず明快になる。

### 2-2. `steps` との責務切り分け

| 区分 | データソース（新） |
|---|---|
| 済み | `done_log`（積み上げ）＋ 後方互換で `steps.state==done` も合成 |
| いま | `steps.state==doing` ＋ `headline` |
| 予定 | `steps.state==todo` ＋ `next`（全件） |

- 済みの正は **`done_log`** に一本化する。`steps` は doing/todo 中心に運用。
- 既存 `steps.done`（旧データ）は**表示時に done_log と合成**して拾う（§6 後方互換）。
  自動移送はしない。

### 2-3. `schema_version` は据置（1 のまま）

`done_log` は任意追加フィールド。旧バイナリは未知フィールドを無視して読め、
新バイナリは `done_log` 無しの旧 state も読める（空として扱う）。破壊的変更では
ないため **`schema_version` は 1 のまま**。将来、意味を変える破壊的変更が生じたときだけ上げる。

### 2-4. 無制限増加への上限

- **保存上限**: append 時に `done_log` を**最新 N 件（推奨 100）**に丸める（古い順に切り捨て）。
  ファイル肥大化を防ぐ。
- **表示上限**: ブロックでは**直近 M 件（推奨 5）**＋「…他 K 件」（§5）。

## 3. CLI 変更

### 3-1. append 用コマンド `sidenote done`

```bash
sidenote done --key <workspace-id か space> --text "M2実装が完了"
```

- 既存ファイルを読み、`done_log` に `{at: now, text}` を append → 保存（atomic write）。
- 上限（N 件）を超えたら古いものを切り捨てる。
- 母艦が「完了のたびに 1 回」叩く運用。text は必須。

### 3-2. `set` を done_log 非破壊にする

現状 `set`（`BuildForSet`）は既存を読まず新規 State を作るため done_log が消える。
**`set` 実行時に既存ファイルの `done_log` を読み込み、新しい State に引き継ぐ**よう変更する
（他フィールドは従来どおり上書き、`done_log` のみ保持）。

- `update`（部分マージ）は既存 State を clone するため `done_log` は自然に保持される（変更不要）。
- `clear` は従来どおりファイルごと削除（履歴も消える。仕様どおり）。

### 3-3. 「予定を全件」書ける点

予定は既存の `--step "ラベル:todo"` を**複数指定**すれば全件表現できるため、
**追加引数は不要**。`next` は「次の一手」の補助として併記（従来どおり）。
（母艦向けに運用を明記するのみ。）

## 4. 表示（view / tui）

- `view.Row` に済みのデータソースを `done_log` ベースへ変更:
  - 済み = `done_log` の text を**新しい順で直近 M 件**＋（後方互換）`steps.done` を合成。
  - いま = `steps.doing` ＋ headline（従来）。
  - 予定 = `steps.todo` ＋ next（全件、従来）。
- `classifySteps` を拡張 or 別関数で done_log を取り込む（純ロジック → TDD）。
- ブロックの `✓ 済` 行は直近 M 件を「・」区切り、超過分は `…他 K 件` を付す。
- グルーピング（f707146）・複数行ブロック・dim 2階調（1da930e）はそのまま整合
  （済み行が長くなっても既存の width truncate と block 単位 height クリップが効く）。

## 5. モックアップ

### 5-1. 済みが積み上がった space（3区分）

```
▍ herdr-plugin-sidenote   herdr:working  母艦:working   2s前
    done履歴の実装
    ✓ 済   グルーピング ・ dim改善 ・ M3実装 ・ M2実装 ・ M1実装  …他8件
    ▸ いま done_log の CLI 実装
    ○ 予定 表示のデータソース差し替え → verify → リリース
```

- 済み: `done_log` 直近 5 件（新しい順）＋「…他 8 件」。
- 予定: `todo` ステップ全件 ＋ `next`。

### 5-2. done_log が空 / 旧 state（後方互換）

```
▍ 旧データな space   herdr:idle  母艦:working   5m前
    作業中
    ✓ 済   設計          ← done_log 無し。steps.state==done から表示（従来どおり）
    ▸ いま 実装
    ○ 予定 テスト
```

## 6. 後方互換・移行

- **done_log 無しの既存 state**: `Load` で `DoneLog` は nil。済みは `steps.done` から表示
  （従来動作を維持）。壊れない。
- **移行**: 自動移送は**しない**（放置）。表示時に `done_log ＋ steps.done` を合成することで
  滑らかに移行する。以後の完了は `sidenote done` で `done_log` に積む。母艦が `set` し直すと
  `steps.done` は消えるが `done_log` は残る（set 非破壊）。
- 破壊的変更ではないため一括マイグレーションは不要。

## 7. 実装範囲の見立て

| パッケージ | 変更 | テスト |
|---|---|---|
| `internal/state` | `State.DoneLog []DoneEntry`、`DoneEntry{At,Text}` 追加。append ヘルパ（上限 N 件で丸め）。Validate は done_log を任意扱い | **TDD**（append・上限丸め・後方互換ロード） |
| `internal/cli` | `done` サブコマンド（append）。`Set` で既存 `done_log` を引き継ぐ（set 非破壊）。`update` は現状維持 | **TDD**（append／set が done_log を消さない／update 保持） |
| `internal/view` | 済みのデータソースを `done_log`（直近 M 件・新しい順）＋`steps.done` 合成に変更 | **TDD**（データソース・上限・合成・後方互換） |
| `internal/tui` | `✓ 済` 行に直近 M 件＋「…他 K 件」表示。既存 renderBlock を微修正 | 手動 verify |

## 8. 確定事項（母艦承認済み・全推奨どおり）

1. `done_log` は **State 直下**（set 引き継ぎが明快）。
2. 保存上限 **N=100**・表示件数 **M=5**。
3. `steps.done` は **done_log に一本化＋表示合成**（後方互換）。
4. append CLI は **`sidenote done --key X --text "..."`**。
5. 済み表示順は **新しい順（直近を先頭）**。
6. `clear` は履歴ごと削除（現状どおり）。

### 実装時に確定した表示仕様（1列TODOリスト・dim調整）

- 「済み／いま／予定」を別サブセクションに分けず、**各ブロック内を 1 列の TODO
  リスト**にした（可読性優先）。マーカーは 完了 `✓` / 進行中 `→` / 予定 `□`、
  並びは **済み→いま→予定**。headline はヘッダ直下に維持、`⚠ 障害`(blockers) は従来どおり別表示。
- 済み = `done_log` 直近 M=5 件（新しい順）＋ `steps.done` 合成、超過は `✓ …他 N 件`。
- 予定 = `todo` 全件 ＋ `next`。
- 鮮度 dim をさらに明るく調整（ヘッダ 244→247 / 本文 250→253）。実端末で可読性確認済み。
