# 000-README-Docs-Index

> **Source Specification**: [prompts/phases/001-phase02/branches/refine-documentation/ideas/000-README-Docs-Index.md](file://prompts/phases/001-phase02/branches/refine-documentation/ideas/000-README-Docs-Index.md)

## Goal Description

トップ `README.md`（約 886 行）を短い紹介 + ドキュメント索引に縮退し、詳細を `docs/` 配下の英語ドキュメントへ切り出す。あわせて `docs/ReferenceManual-WebAPIs.md` を kebab-case にリネームし、README 冒頭の紹介を「組み込み・一瞬の用途・lightweight・セッション継続／エージェント横断」軸（R8）へ書き換える。コード変更は行わない。

## User Review Required

None.（仕様レビュー済み。実行はユーザー指示 `/create-implementation-plan` → `/execute-implementation-plan` による）

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1 README をインデックス中心に | Proposed Changes > `README.md` |
| R2 docs 切り出し 8 ファイル | Proposed Changes > 各 `docs/*.md` NEW |
| R3 `docs/README.md` ハブ | Proposed Changes > `docs/README.md` |
| R4 リンク整合（ユーザー向け） | Proposed Changes > examples READMEs + check script |
| R5 手順意味不変・紹介は例外 | 切り出し Logic / R8 README |
| R6 英語 docs / README | 全 NEW/MODIFY ドキュメント |
| R7 kebab-case（ReferenceManual リネーム必須） | `git mv` + 参照更新 |
| R8 紹介軸の更新 | `README.md` Why/What + `docs/use-cases.md` |
| S1 Start here CTA | `README.md` |
| S2 Roadmap は一言 | `README.md` Status |
| S3 各 docs 先頭に概要 | 各 NEW docs |

## Proposed Changes

### Verification (docs structure)

#### [NEW] [scripts/utils/check_docs_index.sh](file://scripts/utils/check_docs_index.sh)

*   **Description**: ドキュメント再編の受け入れ条件を自動検証する。失敗時は非ゼロ終了。
*   **Technical Design**: bash。リポジトリルート相対で実行。`--help` 対応。
*   **Logic**:
    1. 必須ファイルが存在すること:
       - `docs/README.md`
       - `docs/getting-started.md`
       - `docs/examples.md`
       - `docs/artifacts.md`
       - `docs/use-cases.md`
       - `docs/architecture.md`
       - `docs/roadmap.md`
       - `docs/testing.md`
       - `docs/project-structure.md`
       - `docs/reference-manual-web-apis.md`
       - `docs/client-sse-tool-input.md`
       - `docs/sse-chunk-protocol.md`
    2. 旧パス `docs/ReferenceManual-WebAPIs.md` が **存在しない** こと。
    3. トップ `README.md` に次の見出しが **無い** こと（切り出し済み確認）:
       - `## Installation`
       - `## Quick Start`
       - `## Artifact API Examples`
       - `## Example`（単独の Example セクション）
       - `## Testing`
       - `## Roadmap`
    4. トップ `README.md` に次が **含まれる** こと:
       - `docs/README.md` へのリンク
       - `docs/getting-started.md` へのリンク
       - `docs/reference-manual-web-apis.md` へのリンク
       - R8 を示す語のいずれか（例: `embed` / `lightweight` / `on-demand` を case-insensitive で少なくとも 2 語）
    5. ユーザー向け参照の旧パス残存チェック:
       - `rg -n 'ReferenceManual-WebAPIs' README.md docs examples` がマッチ 0 件
    6. 相対リンク存在確認（簡易）:
       - `README.md` と `docs/*.md` 内の `](...md)` / `](.../...)` のうち `http` で始まらないリンク先ファイルが存在すること（アンカー `#` 以降は無視）

#### [NEW] [tests/docs_index_layout_test.go](file://tests/docs_index_layout_test.go)（または既存 docs カテゴリが無ければ `tests/docs_readme_index_test.go`）

*   **Description**: `check_docs_index.sh` を統合テストから呼び、CI/統合スイートで再発防止する。
*   **Technical Design**: 標準 `testing`。`exec.Command` で `scripts/utils/check_docs_index.sh` を実行。作業ディレクトリはリポジトリルート（既存テストと同様にルート検出）。
*   **Logic**:
    - `TestDocsIndexLayout` がスクリプト exit 0 を期待。
    - カテゴリ: 既存の軽量 docs/smoke 系があればそれに合わせる。無ければファイル名 `docs_readme_index_test.go` とし、`integration_test.sh --specify TestDocsIndexLayout` で単体指定可能にする。

> 注: 本変更は製品コード無し。TDD は「チェックスクリプトが失敗する状態 → ドキュメント配置でグリーン」の順で進める。

### Docs rename (R7)

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md) → [docs/reference-manual-web-apis.md](file://docs/reference-manual-web-apis.md)

*   **Description**: `git mv docs/ReferenceManual-WebAPIs.md docs/reference-manual-web-apis.md`
*   **Technical Design**: 内容変更なし（リネームのみ）。
*   **Logic**: 履歴維持のため `git mv` 必須。

### User-facing link updates (R4)

#### [MODIFY] [examples/config-dir-switch/README.md](file://examples/config-dir-switch/README.md)

*   **Logic**: `../../docs/ReferenceManual-WebAPIs.md` → `../../docs/reference-manual-web-apis.md`

#### [MODIFY] [examples/session-follow/README.md](file://examples/session-follow/README.md)

*   **Logic**: 同上パス更新。

#### [MODIFY] [examples/token-usage/README.md](file://examples/token-usage/README.md)

*   **Logic**: `docs/ReferenceManual-WebAPIs.md` → `docs/reference-manual-web-apis.md`

#### [MODIFY] [docs/client-sse-tool-input.md](file://docs/client-sse-tool-input.md)

*   **Logic**: 内部リンクに旧 ReferenceManual が無いか確認。変更不要なら触らない。`sse-chunk-protocol.md` リンクは維持。

### New documentation files (R2, R3, S3, R5, R8 for use-cases)

各 NEW ファイルは英語。先頭に 1 段落の概要（S3）。README からの切り出しは意味を変えず移動（R5）。パスはプロジェクトルート相対のまま（`examples/`、`settings/`、`docs/`）。

#### [NEW] [docs/getting-started.md](file://docs/getting-started.md)

*   **Source**: README `## Installation` + `## Quick Start` 全文。
*   **Logic**: 見出しを `# Getting Started` とし、Installation / Quick Start を H2 以下で維持。コードブロック・YAML・表は原文のまま。

#### [NEW] [docs/examples.md](file://docs/examples.md)

*   **Source**: README `## Example` 配下（Server / Client / Vault / Multimodal / Interoperability）全文。
*   **Logic**: `# Examples`。旧 `docs/ReferenceManual-WebAPIs.md` リンクを新パスへ置換してから配置。

#### [NEW] [docs/artifacts.md](file://docs/artifacts.md)

*   **Source**: README `## Artifact API Examples` 全文。
*   **Logic**: `# Artifacts`。

#### [NEW] [docs/use-cases.md](file://docs/use-cases.md)

*   **Source**: README `## Example Use Cases` + `## Vision` を素材に **R8 で再構成**。
*   **Logic**（継承すべきメッセージ）:
    1. Embeddable for momentary use（コンテナ強制なし）
    2. Lightweight by default（サービス起動可だが常駐基盤より軽い）
    3. Short-lived optional
    4. Session continuity + cross-agent
    5. Casual agent tool トーン
    6. 旧 Use Cases（Maintain workflows / Evaluate models / Reduce migration / Future migration）は「Additional scenarios」として残してよいが主軸にしない
    7. HarnessRouter 名は必須ではない。書くなら 1〜2 文の境界説明のみ

#### [NEW] [docs/architecture.md](file://docs/architecture.md)

*   **Source**: README `## Architecture Overview` + `## Design Documents`。
*   **Logic**: CAWA / LLMGP / Integration。`docs/reference-manual-web-apis.md`、`docs/sse-chunk-protocol.md`、`docs/client-sse-tool-input.md` へのリンク。

#### [NEW] [docs/roadmap.md](file://docs/roadmap.md)

*   **Source**: README `## Roadmap` チェックリスト全文（Phase 1/2/3）。意味変更なし。

#### [NEW] [docs/testing.md](file://docs/testing.md)

*   **Source**: README `## Testing` のコマンド説明全文。

#### [NEW] [docs/project-structure.md](file://docs/project-structure.md)

*   **Source**: README `### Project Structure` ツリー。

#### [NEW] [docs/README.md](file://docs/README.md)

*   **Description**: ドキュメントハブ（R3）。
*   **Logic** カテゴリ:
    1. Getting Started → `getting-started.md`
    2. Guides / Examples → `examples.md`, `artifacts.md`, `use-cases.md`
    3. Architecture & Protocols → `architecture.md`, `sse-chunk-protocol.md`, `client-sse-tool-input.md`
    4. API Reference → `reference-manual-web-apis.md`
    5. Roadmap & Testing → `roadmap.md`, `testing.md`
    6. Project Structure → `project-structure.md`
    - `issue-26-verification.md` は載せない。

### Top-level README (R1, R8, S1, S2)

#### [MODIFY] [README.md](file://README.md)

*   **Description**: 120〜200 行目安の索引 README。詳細本文を削除しリンクへ。
*   **Logic**（構成）:
    1. ロゴ + タグライン維持
    2. 短い紹介（R8）: embed / momentary / lightweight / no forced containers / session continuity / cross-agent。interoperability は第一の売りにしない
    3. `## Why Tern?` — 北極アカモズ比喩は短く残してよいが、動機は「常駐共通基盤ではなく組み込みで気軽に使う」
    4. `## What Tern Provides`:
       - Embeddable, on-demand agent runtime
       - Lightweight operation (service mode possible)
       - Session continuity and cross-agent workflows
       - Context / Agent / Model interoperability（capability）
    5. `## Architecture Overview` — 3 コンポーネントの短い説明 + `docs/architecture.md` リンク
    6. `## Status` — early phase + `Current phase: Phase 2 — see [Roadmap](docs/roadmap.md).`
    7. Start here CTA → `docs/getting-started.md`（S1）
    8. `## Documentation` — Hub + 主要直リンク（新パス）
    9. `## Contributing` / `## License` 維持
    10. 禁止: Installation / Quick Start / Example / Artifact / Testing / Roadmap 本文

### Out of scope (明示)

*   `prompts/` 配下の旧パスは更新しない（仕様 R4）。
*   `tmp/` 内ドラフトは対象外。
*   API 契約・コードの変更なし。

## Step-by-Step Implementation Guide

1. **[x] Red: check script stub**: `scripts/utils/check_docs_index.sh` と `tests/docs_readme_index_test.go` を先に追加。現状 README では失敗することを確認（任意でスクリプト単体実行）。
2. **[x] Rename reference manual**: `git mv docs/ReferenceManual-WebAPIs.md docs/reference-manual-web-apis.md`。examples 3 ファイルのリンク更新。コミット。
3. **[x] Extract getting-started / examples / artifacts**: README 該当節を切り出して NEW docs 作成。旧 ReferenceManual リンクを置換。コミット。
4. **[x] Extract architecture / roadmap / testing / project-structure**: 同様。コミット。
5. **[x] Write use-cases (R8) + docs/README hub**: コミット。
6. **[x] Rewrite top README**: R1/R8/S1/S2。コミット。
7. **[x] Green: check script**: `./scripts/utils/check_docs_index.sh` 成功。必要ならスクリプト調整。
8. **[x] Build**: `./scripts/process/build.sh`（Windows/Git Bash）。統合: `./scripts/process/integration_test.sh --specify TestDocsIndexLayout`（該当テストがカテゴリ必須なら合わせて指定）。
9. **[/] Push**: 全成功後 `git push`。

## Verification Plan

### Automated Verification

1. **Docs layout**: `./scripts/utils/check_docs_index.sh`
2. **Build & Unit Tests**: `./scripts/process/build.sh`
3. **Integration Tests**: `./scripts/process/integration_test.sh --specify "TestDocsIndexLayout"`
4. **E2E Tests**: `tests/docs_readme_index_test.go` の `TestDocsIndexLayout`（上記 specify）。製品 GUI E2E は対象外（文書のみ変更）。

### Regression note

製品コード未変更のため、フル統合スイートは必須としない。ただし `build.sh` は必須。`TestDocsIndexLayout` がビルド成果物に依存しない場合でも、プロジェクト規約に従い build → specify の順で実行する。

## Documentation

本計画自体がドキュメント再編。追加のユーザー向けドキュメント更新は Proposed Changes に含む。`prompts/` 履歴文書は更新しない。
