# ステータス別グルーピング表示 — 設計案

- ステータス: 承認済み（design gate 通過・全推奨どおり）・実装済み（2026-07-14）
- 作成日: 2026-07-14
- 対象: `michiomochi/herdr-plugin-sidenote`
- 前提: 複数行ブロック表示（`8d3c380`）＋ 鮮度 dim 2階調（`1da930e`）と整合

## 1. 目的

ダッシュボードで「**母艦（人間）の対応が必要なもの**」と「**各 space で実施中のもの**」を
一目で分けられるよう、`status` ベースで 3 グループにセクション分けする。
永続スキーマ・CLI は変更しない（表示層のみ）。

## 2. グルーピング定義とマッピングの妥当性

### 2-1. 3 グループ

| グループ | 見出し（案） | 含む条件（母艦 status ベース） |
|---|---|---|
| 要母艦対応 | `● 要母艦対応` | `blocked` / `review`、**および `blockers[]` が非空**（後述の昇格） |
| 実施中 | `▸ 実施中` | `working` / `planning` |
| 完了・待機 | `✓ 完了・待機` | `done`、skeleton（state 未記入）、broken（末尾） |

### 2-2. 母艦 status は 5 値のみ（`idle` は含まれない）— 重要

現行の母艦 `status`（`internal/state`）の enum は
**`planning` / `working` / `blocked` / `review` / `done` の 5 値のみ**。
`idle` は herdr の `agent_status`（骨格）側の値であり、母艦 status には存在しない。

→ 要件記載の「完了・待機 = done / **idle** / skeleton」の `idle` は、
**skeleton 行（state 未記入・母艦 status なし）**を指すものと解釈する。
skeleton 行は母艦が何も書いていない＝母艦の関与対象外なので「完了・待機」に入れる。

### 2-3. マッピングは妥当（明示フラグ `awaiting-captain` は不要）

母艦の運用（承認待ち=`review`、ブロッカー/停止=`blocked`、実行中=`working`、
着手前=`planning`、完了=`done`）で 3 分類は回る。専用フラグの追加は不要で
**スキーマ変更なし**で成立する。ただし取りこぼし防止に次の 2 点を補う。

### 2-4. `blockers[]` 非空は「要母艦対応」へ昇格

母艦が `status` を `working` のままにしつつ `blockers[]` だけ書くケースがある。
この取りこぼしを防ぐため、**`blockers[]` が非空なら status に関わらず
『要母艦対応』に昇格**させる（分類の第一条件）。

### 2-5. 母艦 status と herdr agent_status が食い違う場合

**グルーピングは母艦 status（意味）を基準**とする。「対応が要るか」は母艦の
認識が主であり、herdr の `agent_status`（骨格）は稼働の外形にすぎないため。
`agent_status` は従来どおり各ブロックのヘッダに併記して手掛かりを残す。

- 例外的補強（任意・母艦判断）: skeleton 行で `agent_status == blocked` の場合のみ
  「要母艦対応」に寄せる案もある。ただし skeleton は母艦の意味情報が無く誤検知の
  恐れもあるため、**初期は skeleton は一律「完了・待機」**を推奨（YAGNI）。

### 2-6. broken 行の扱い（要判断）

壊れた state ファイルは稀だが「人が直すべき」情報でもある。

- **推奨: 「完了・待機」グループの末尾**に置く（現行 sortRows も壊れを末尾に寄せている）。
  要対応グループを blocked/review の純粋な集合に保てる。
- 代替: 「要母艦対応」に含めて見落としを防ぐ。
- どちらでもスキーマ非依存。母艦の好みで確定したい（§6）。

### 2-7. 分類ロジック（純ロジック → TDD）

```
groupOf(row):
  1. row.Broken                       → 完了・待機（末尾）   ※推奨。§2-6
  2. len(row.Blockers) > 0            → 要母艦対応           ※§2-4 昇格
  3. status == blocked | review       → 要母艦対応
  4. status == working | planning     → 実施中
  5. status == done                   → 完了・待機
  6. その他（skeleton / status=="")   → 完了・待機
```

## 3. レイアウト

- **グループ順（固定）**: `要母艦対応`（最上部）→ `実施中` → `完了・待機`。
- **セクション見出し**: 件数付き（例 `● 要母艦対応 (2)`）。要対応は目を引く色、
  実施中は通常、完了・待機は弱め。dim 2階調（`1da930e`）と整合させる。
- **空グループは見出しごと省略**。
- **グループ内の並び**: 既存ソート踏襲（更新時刻の新しい順、壊れは末尾）。
  Merge 後の `sortRows` 済み配列をグループへ安定分配するだけでよい。
- **グループ内ブロック**: 既存の複数行ブロック（ヘッダ / headline / ✓済 / ▸いま /
  ○予定 / ⚠障害）をそのまま流用。
- **height クリップ**: 上から順（＝要母艦対応から）ブロック単位で詰め、入り切らなく
  なった時点で打ち切って末尾に `…他 N 件`。要母艦対応が最上部なので自然に優先表示
  される。見出しも 1 行として高さに数える。

## 4. モックアップ

### 4-1. 通常時（3 グループが埋まっている）

```
sidenote — 8 spaces   q:quit  r:reload

● 要母艦対応 (2)
▍ 氏名突合セルフホスティング   herdr:done  母艦:review   1m前
    レビュー指摘対応
    ✓ 済   実装 ・ 単体テスト
    ▸ いま 結合テスト
    ○ 予定 リリース準備
    ⚠ 障害 API仕様の確定待ち
▍ FATCA取引時確認   herdr:working  母艦:blocked   3m前
    移行バッチが停止
    ✓ 済   要件整理
    ▸ いま 原因調査
    ⚠ 障害 権限不足で本番参照不可

▸ 実施中 (1)
▍ herdr-plugin-sidenote   herdr:working  母艦:working   2s前
    グルーピング表示の実装
    ✓ 済   複数行表示 ・ dim改善
    ▸ いま グルーピング実装
    ○ 予定 実機verify

✓ 完了・待機 (4)
▍ HQ   herdr:idle  母艦:done   12m前
    全体統括（本日分完了）
▍ nakayoku   herdr:idle  母艦:-   2d前
    -
▍ Vim   herdr:unknown  母艦:-   —
    -
…他 1 件
```

（`完了・待機` は弱いグレー基調。`要母艦対応` の見出し・障害行は目を引く色を残す。
stale ブロックは `1da930e` の 2 階調で本文が読める明るさ。）

### 4-2. 一部グループが空（見出しごと省略）

要母艦対応が 0 件なら、その見出しごと出さない。

```
sidenote — 3 spaces   q:quit  r:reload

▸ 実施中 (2)
▍ herdr-plugin-sidenote   herdr:working  母艦:working   5s前
    グルーピング表示の実装
    ✓ 済   複数行表示
    ▸ いま グルーピング実装
▍ 別プロジェクト   herdr:working  母艦:planning   40s前
    要件整理中
    ○ 予定 設計

✓ 完了・待機 (1)
▍ HQ   herdr:idle  母艦:done   15m前
    全体統括（完了）
```

## 5. 実装範囲の見立て

| パッケージ | 変更 | テスト |
|---|---|---|
| `internal/state` / `internal/cli` | **変更なし**（スキーマ・CLI 据え置き） | 既存維持 |
| `internal/view` | `groupOf(row)` 分類関数と、ソート済み rows をグループへ安定分配する `GroupRows(rows) []Group`（`Group{Kind, Title, Rows}`）を追加。純ロジック | **TDD**（各 status→グループ、blockers 昇格、skeleton/broken の扱い、空グループ、順序保持） |
| `internal/tui` | View をグループ順に描画、セクション見出し（件数・色）、空グループ省略、height クリップをグループ跨ぎのブロック単位に拡張 | 手動 verify |

- 後方互換: グルーピングは表示のみ。state ファイルは不変。
- 既存の複数行 `renderBlock` と dim 2階調はそのまま再利用。

## 6. 確定事項（母艦承認済み・全推奨どおり）

1. マッピングは §2 の母艦 status ベース＋`blockers[]` 昇格で確定（明示フラグ不要）。
2. broken 行は「完了・待機」末尾。
3. skeleton 行は一律「完了・待機」（`agent_status==blocked` の特別扱いはしない・YAGNI）。
4. セクション見出しは `● 要母艦対応` / `▸ 実施中` / `✓ 完了・待機` ＋件数。

実装は §5 のとおり `internal/view`（`groupOf`/`GroupRows`・TDD）と
`internal/tui`（セクション描画・グループ跨ぎ height クリップ・手動 verify）で完了した。
