# 表示改善3点（状態別色／headline削除／per-item 母艦対応待ち） — 設計案

- ステータス: 承認済み・実装済み（2026-07-15）
- 作成日: 2026-07-15
- 対象: `michiomochi/herdr-plugin-sidenote`
- 前提: 複数行ブロック（`8d3c380`）＋dim 2階調（`1da930e`）＋グルーピング（`f707146`）＋done履歴・1列TODO（`2541d72`）

## 1. (1) タスク項目の状態別 文字色

各 TODO 項目にマーカー色を付ける（fresh 時）:

| 区分 | マーカー | 色（案） |
|---|---|---|
| 完了 | `✓` | 緑（lipgloss `42`・既存 working 色と同系） |
| 進行中 | `→` | 太字の白（Bold＋前景 `15`/端末既定） |
| 予定 | `□` | グレー（`245`） |

### stale との兼ね合い規則（両立）

- **fresh 時**: 上表の状態別色を適用。
- **stale 時（updated_at が閾値超）**: 状態別色を**無効化し、ブロック全体を弱グレーに統一**
  （現行どおりヘッダ `247` / 本文 `253`）。鮮度の手掛かりを最優先し、色数を増やして
  読みにくくしない。
- 規則を一言で: **「stale はグレー一律が勝つ。fresh のときだけ状態別色」**。

## 2. (2) headline 行の削除

- やることが全て TODO リスト化されたため、タイトル下の `headline` 行を**表示から削除**。
- **スキーマは変更しない**（`headline` は必須のまま）。理由: 既存 state・CLI・検証を
  壊さず最小変更にとどめる。`headline` は `list`（デバッグ）では引き続き見え、内部記録として残る。
  - 代替（任意化）は将来検討可だが、今回はスコープ外を推奨。
- **skeleton 行**（state 無し）: これまで headline `-` を出していたが、削除後は
  **ヘッダ行のみ**の 1 行ブロックになる（TODO も無い）。見え方は下記モックアップ参照。

## 3. (3) per-item 母艦対応待ち ＋ グルーピング廃止

### 3-1. per-item 待ち機構（候補比較）

| 案 | 内容 | 評価 |
|---|---|---|
| **A（推奨）** | `Step` に `await bool` を追加。CLI は `--step "ラベル:state:await"`（末尾 `:await` を任意サフィックス化） | 項目と待ちが直接紐づく。表示で該当項目の右に赤字を出せる。後方互換◎ |
| B | 専用フィールド `awaiting: ["テキスト"]` を追加し、表示時に項目テキストと照合 | テキスト照合が脆い（重複・表記揺れ）。項目と疎結合 |
| C | 既存 `blockers[]` を per-item に流用 | blockers は space 全体の障害で意味がずれる |

**推奨は A**: `state.Step` に `Await bool json:"await,omitempty"` を追加。

- **CLI**: `ParseStep` を拡張し `"ラベル:state"` に加えて `"ラベル:state:await"` を受ける。
  現行は `LastIndex(":")` で label 内の `:` を許容しているため、**末尾が正確に `:await` の
  ときだけ先に剥がしてから** 従来の `label:state` 分割を行えば後方互換を保てる。
- **schema_version は据置 1**（任意フィールド追加・前後方互換）。旧 state は `await=false`。
- **上限**: step 数に従属（実用上少数）。専用の上限は不要。

### 3-2. グルーピング廃止の是非

per-item で「項目の右に赤 `〈母艦対応待ち〉`」を出せるなら、space-level の
セクション見出し（`●要母艦対応/▸実施中/✓完了待機`）は冗長になる。

| | 廃止案（推奨） | 維持案 |
|---|---|---|
| 俯瞰性 | 順序（待ち space を上寄せ）で担保 | 見出しで明確 |
| 縦の消費 | 見出しぶん節約・スッキリ | 見出し＋空行を消費 |
| per-item との重複 | 無し | 待ちが space/項目で二重表現 |

- **推奨: グルーピング廃止**（`groupOf`/`GroupRows` と見出し描画を撤去）。per-item 待ち表示
  ＋下記ソートで俯瞰を担保する。廃止で `f707146` のセクション機構は不要になるが、
  **分類の考え方はソート順として引き継ぐ**（見出しを消すだけ）。

### 3-3. スペース順（グルーピング廃止時のソート）

見出しは消すが、並び順で俯瞰性を残す（優先度の高い順）:

1. **母艦対応待ちを持つ space**（`await` 項目あり／`blockers[]` 非空／status `blocked`・`review`）
2. **実施中**（`working`・`planning`、待ちなし）
3. **完了・待機**（`done`・skeleton）／broken は末尾
- 各群内は既存ソート踏襲（`updated_at` 新しい順）。

実質は現行グルーピングの3分類を「見出し無しのソートキー」に転用するだけ。

### 3-4. CLI（待ちの設定/解除）

- **設定**: `--step "ラベル:doing:await"` のように await 付きで step を書く。
- **解除**: `:await` を外して再 `set`（set は上書き）。または `update --step ...` で差し替え。
- 専用コマンド（`await`/`unawait`）は過剰（YAGNI）。step ベースで `set`/`update` に統合。
- 母艦運用: 「この項目を母艦に見てほしい」→ 該当 step に `:await` を付けて set。判断が済んだら外す。

## 4. モックアップ

### 4-1. 推奨案（グルーピング廃止・状態別色・headline なし・per-item 待ち）

```
sidenote — 6 spaces   q:quit  r:reload

▍ 実施中タスク            herdr:working  母艦:working   2s前
    ✓ 完了項目A                         (緑)
    → 検証                              (太字白)
    □ リリース  〈母艦対応待ち〉          (□=グレー / 〈〉=赤)
    □ ドキュメント                       (グレー)

▍ レビュー待ち            herdr:-  母艦:review   1m前
    → PR作成                            (太字白)
    □ マージ  〈母艦対応待ち〉
    ⚠ 障害 承認待ち

▍ 実装中B                 herdr:working  母艦:working   40s前
    ✓ 設計                              (緑)
    → 実装                              (太字白)
    □ テスト                            (グレー)

▍ 古いタスク(stale)       herdr:idle  母艦:done   3h前
    ✓ 実装完了                          (全体 stale グレー・状態別色は無効)
    ✓ 設計完了

▍ HQ                      herdr:idle  母艦:-   —        (skeleton=ヘッダのみ)
```

- 上 3 つは「母艦対応待ち／実施中」で上寄せ、stale・skeleton は下。
- `〈母艦対応待ち〉` は該当項目の右に赤字。

### 4-2. 維持案（グルーピング残す・比較用）

```
● 要母艦対応 (2)
▍ レビュー待ち   herdr:-  母艦:review   1m前
    → PR作成
    □ マージ  〈母艦対応待ち〉
    ⚠ 障害 承認待ち
▍ 実施中タスク   herdr:working  母艦:working   2s前
    □ リリース  〈母艦対応待ち〉
    ...

▸ 実施中 (1)
▍ 実装中B   ...

✓ 完了・待機 (N)
▍ 古いタスク(stale) ...
```

（見出しと per-item 待ちが二重になり冗長。→ 4-1 を推奨。）

## 5. 実装範囲の見立て

| パッケージ | 変更 | テスト |
|---|---|---|
| `internal/state` | `Step.Await bool`（omitempty）追加。Validate 据置。schema 据置 | **TDD**（await ロード・後方互換） |
| `internal/cli` | `ParseStep` に末尾 `:await` サフィックス対応 | **TDD**（`label:state:await` 解釈・label 内 `:` 互換） |
| `internal/view` | TODO 項目を `Item{Text, State, Await}` として保持（Doing/Todo を string→Item 化）。`groupOf`/`GroupRows` を撤去し、待ち→実施中→完了の**ソート**関数に置換 | **TDD**（項目の await 反映・ソート順） |
| `internal/tui` | 状態別色（✓緑/→太字白/□グレー）、`〈母艦対応待ち〉` 赤、headline 行削除、グルーピング見出し撤去。stale 時は状態別色を無効化し弱グレー統一 | 手動 verify |

- 後方互換: `await` 無しの既存 step は待ち表示なし。headline 必須据置で既存 state 壊さない。

## 6. 確定事項（母艦承認済み）

1. per-item 機構は **案A**（`Step.Await bool`＋`--step "ラベル:state:await"`）。schema_version 据置1。
2. **グルーピング廃止＋ソート**（母艦対応待ち→実施中→完了・待機、群内 updated_at 新しい順）。
   `groupOf`/`GroupRows` と見出し描画は撤去。
3. headline は **表示削除のみ・スキーマ必須据置**（`list` では残る）。skeleton はヘッダ1行。
4. 予定 `□`=グレー `245`、進行中 `→`=Bold＋前景 `15`（太字白）、完了 `✓`=緑 `42`。
5. **stale 時は状態別色を無効化し弱グレー統一**（ヘッダ247/本文253）。
   **ただし赤 `〈母艦対応待ち〉` は行動シグナルなので stale でも残す**（人間の明示判断）。
6. 待ち解除は **`:await` を外して再 set**（専用コマンドは作らない）。

実装は §5 のとおり state（`Step.Await`）/cli（`ParseStep` 拡張）/view（Item 化・
`spaceRank` ソート・グルーピング撤去）/tui（状態別色・赤ラベル・headline削除・見出し撤去）で完了。
