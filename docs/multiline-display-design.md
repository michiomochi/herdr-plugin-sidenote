# 複数行ブロック表示への拡張 — 設計案

- ステータス: 承認済み（design gate 通過）・実装済み（2026-07-14）
- 作成日: 2026-07-14
- 対象: `michiomochi/herdr-plugin-sidenote`

## 1. 目的

現状の「1 space = 1 行」要約表示を、各 space について
**「やったこと（過去・完了）」「いま（現在）」「やる予定（今後）」** が一目で
分かる **複数行ブロック表示** に拡張する。

## 2. 現行データで何が表現できるか（結論: 永続スキーマ拡張は不要）

現行 `state.State`（`docs/design.md` §6）は既に過去・現在・未来の素材を持つ。

| 見せたい軸 | 現行データ | 備考 |
|---|---|---|
| やったこと（過去・完了） | `progress.steps[]` の `state=done` | 現タスク内の完了ステップ |
| いま（現在） | `headline` ＋ `progress.steps[]` の `state=doing` | 主役の一言＋進行中ステップ |
| やる予定（今後） | `progress.steps[]` の `state=todo` ＋ `next` | 未着手ステップ＋次の一手 |
| 障害 | `blockers[]` | |
| 進捗率 | `progress.percent` | 任意 |

→ **`steps`(done/doing/todo) ＋ `next` ＋ `blockers` で過去・現在・未来は表現できる。**
永続スキーマ（`state.State`）の変更も CLI（`set`/`update`）の引数追加も**不要**。
必要なのは **表示層（`internal/view` / `internal/tui`）の拡張のみ**。

### 内部表示モデル（`view.Row`）の拡張は必要

現行 `view.Row` は `steps` を保持していない（`BuildRows` は headline/next/blockers
のみ取り込む）。複数行化に伴い、`Row` に steps 由来の分類を持たせる:

```
Row に追加（内部モデルのみ・永続化しない）:
  Done  []string   // steps.state==done の label
  Doing []string   // steps.state==doing の label
  Todo  []string   // steps.state==todo の label
```

`BuildRows` で `progress.steps` をこの 3 群に振り分ける（純ロジック → テスト対象）。

### 将来の最小拡張案（今は不要・YAGNI）

以下は「現タスクの steps」では足りなくなった場合の後方互換な最小案。**現時点では採用しない**。

- **タスクをまたいだ完了履歴**を長く残したい場合のみ:
  任意フィールド `log: [{"at": <RFC3339>, "text": "..."}]` を追加。
  任意フィールドの追加は既存リーダーが無視できるため `schema_version` は 1 のままでよい
  （破壊的変更が生じたときだけ版を上げる）。
- **予定を steps と別管理**したい場合: `planned: ["..."]`。
  ただし `todo` ステップで代替できるため不要。

## 3. モックアップ（テキスト）

### 3-1. 通常時（各 space を複数行ブロック）

```
▍ herdr-plugin-sidenote      herdr:working  母艦:working    2s前
    複数行表示の設計中
    ✓ 済    設計Doc ・ M1–M5実装 ・ verify
    ▸ いま   レビュー修正
    ○ 予定   複数行表示の実装 → docs更新

▍ HQ                         herdr:idle     母艦:planning   9m前
    全体統括・母艦
    ✓ 済    (なし)
    ▸ いま   各spaceの状況確認
    ○ 予定   進捗レビュー
```

### 3-2. ブロッカーがある space

```
▍ 氏名突合セルフホスティング   herdr:done     母艦:blocked    1m前
    API仕様待ちで停止
    ✓ 済    実装 ・ 単体テスト
    ▸ いま   結合テスト
    ○ 予定   リリース準備
    ⚠ 障害   API仕様の確定待ち
```

### 3-3. 多数 space 時（推奨案 = 状態ベースの自動折りたたみ）

作業中/ブロック中/レビュー中はブロック展開、静かな space（idle/done/planning・
state 未記入）は 1 行に畳む。末尾に省略件数。

```
── 作業中 ─────────────────────────────────────────────
▍ herdr-plugin-sidenote      herdr:working  母艦:working    2s前
    複数行表示の設計中
    ✓ 済 設計Doc・M1–M5   ▸ いま レビュー修正   ○ 予定 実装
▍ 氏名突合セルフホスティング   herdr:done     母艦:blocked    1m前
    ⚠ API仕様待ち
    ✓ 済 実装・テスト   ▸ いま 結合テスト   ○ 予定 リリース

── 静観 ───────────────────────────────────────────────
  HQ              idle   planning  全体統括            9m前
  nakayoku        idle   -         -                   2d前
  利用規約PP同意   idle   -         -                   2d前
  …他 3 件（q:終了 e:全展開 c:全折りたたみ）
```

（展開ブロックが端末高さを超える場合は、既存の height クリップと同じ要領で
末尾を切り「…他 N 件」を表示する。）

## 4. UX 案

| 案 | 内容 | コスト | 効果 |
|---|---|---|---|
| **案1（推奨）** | 状態ベース自動折りたたみ＋手動トグル | 中 | 「動いている所」を詳しく、静かな所は畳んで一覧性を両立 |
| 案2（最小） | 全 space をブロック全展開＋height クリップのみ | 小 | 実装最小。多数時は下が見えないが新しい順ソートで実用 |

- **推奨は案1**。母艦 status が `working`/`blocked`/`review` の space を自動展開、
  `planning`/`done`/`idle`・skeleton は 1 行。加えてキー操作:
  - `e` 全展開 / `c` 全折りたたみ（グローバルトグル）
  - （任意）カーソル選択して `space`/`enter` で個別トグル ← 実装コスト増のため第2段階
- 過剰実装は避ける: viewport スクロールや `bubbles/list` 導入は今は不要（YAGNI）。
  折りたたみ＋height クリップで多数 space に対応できる。

段階導入も可: まず案2（全展開＋クリップ）で最小リリース → 不便なら案1 の
自動折りたたみ＋`e`/`c` トグルを追加。

## 5. 実装計画の概要（承認後）

| パッケージ | 変更 | テスト方針 |
|---|---|---|
| `internal/state` | 変更なし（永続スキーマ据え置き） | 既存維持 |
| `internal/cli` | 変更なし（`--step`/`--next`/`--blocker` で入力済み） | 既存維持 |
| `internal/view` | `Row` に Done/Doing/Todo 追加、`BuildRows` で steps 分類、折りたたみ判定関数を追加 | **TDD**（分類・折りたたみ判定は純ロジック） |
| `internal/tui` | `renderRow` を複数行ブロック描画に、展開状態を model に、`e`/`c` トグル、height クリップをブロック単位に調整 | 手動 verify（描画） |

- 後方互換: steps が空の space は従来どおり headline 中心の簡素なブロックになる（過去/予定行は「(なし)」または省略）。
- verify: 実機で複数 space・steps あり/なし・blocker あり・多数 space（折りたたみ）・
  狭い pane 幅を確認。

## 6. 確定事項（母艦承認済み）

1. UX = **案2（全 space を常に複数行展開）**。自動折りたたみ・`e`/`c` トグルは非採用。
   端末高さ超過時は末尾を切り「…他 N 件」を表示（ブロック単位クリップ）。
2. **スキーマ拡張なし・CLI 追加なし**。永続 `state.State` と `set`/`update` は据え置き。
   表示層のみ変更し、`view.Row` に `Done`/`Doing`/`Todo`（steps 由来・内部モデル）を追加。
3. 個別カーソル選択トグルは後回し（YAGNI・実装しない）。

実装は §5 のとおり `internal/view`（steps 分類・複数行 BuildRows を TDD）と
`internal/tui`（複数行ブロック描画・ブロック単位 height クリップ・手動 verify）で完了した。
