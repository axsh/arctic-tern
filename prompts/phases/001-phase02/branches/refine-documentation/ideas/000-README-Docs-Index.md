# 000: README 簡素化と docs/ インデックス化

## 背景 (Background)

### 現状

ワークスペーストップの `README.md` は約 886 行に肥大化している。

含まれる主なセクション:

| セクション | 行数目安 | 性質 |
| :--- | ---: | :--- |
| Why / What / Vision / Architecture | 短い〜中 | プロダクト紹介 |
| Example / Vault / Multimodal / Artifact のコード例 | 長い | チュートリアル・リファレンス混在 |
| Example Use Cases / Roadmap / Status | 中 | ビジョン・進捗 |
| Installation / Quick Start | 長い | セットアップ手順 |
| Documentation / Design / Testing / Contributing / License | 短い | メタ情報 |

一方 `docs/` には次のファイルのみがあり、README からの詳細移行先としては未整備である。

- `docs/ReferenceManual-WebAPIs.md`
- `docs/client-sse-tool-input.md`
- `docs/sse-chunk-protocol.md`
- `docs/issue-26-verification.md`（検証メモ系）
- `docs/resources/`（画像など）

### 課題

1. **初見読者の導線が長い** — インストールや Artifact API まで一気に並び、プロジェクト概要が埋もれる。
2. **README が単一の情報源になりすぎている** — 手順・API 例・ロードマップが同居し、更新コストが高い。
3. **`docs/` がインデックスになっていない** — 既存の API マニュアルと README 内の手順が分断されている。
4. **差別化の軸が古い** — 現行 README は interoperability（Agent / Model / Context）を主軸にしている。それ自体は価値だが、HarnessRouter 等でも選択可能な軸であり、Tern 固有の強みが伝わらない。

### Tern の位置づけ（本仕様で採用する紹介方針）

レビューで確定したプロダクト理解:

- Tern は Coding Agent の「共通処理基盤としてサービス起動を中心に据える」設計ではない。
- 設計の起点は、**組み込んで一瞬の用途に使える**こと。コンテナ利用も強制しない。
- **短命も強制しない**。サービスとして起動することも可能だが、HarnessRouter より遥かに lightweight。
- セッションには継続性があり、Coding Agent を横断できる。
- トーンは「気軽なエージェントツール」。常駐前提ではないため、メモリも多くは消費しない。
- Interoperability（Context / Agent / Model）は能力として残すが、**第一の売り文句にはしない**。

競合・隣接との対比（README 本文に製品名で攻撃的に書く必要はない。境界は短く示してよい）:

| | HarnessRouter 側のイメージ | Tern |
| :--- | :--- | :--- |
| 設計中心 | Coding Agent 共通処理基盤としての **サービス起動** | **組み込み・一瞬の用途**から想定 |
| デプロイ | （サービス基盤寄り） | コンテナ強制なし。インプロセス／オンデマンド可 |
| 稼働形態 | 常駐サービス向き | 短命から使えるが短命強制なし。サービス起動も可でより lightweight |
| セッション | （基盤側の関心） | 継続性あり。エージェント横断も可 |
| 資源 | 常駐前提になりやすい | 常駐しない使い方が主なのでメモリ消費を抑えやすい |

### 本仕様で決めること

1. トップ `README.md` を「短い紹介 + ドキュメント索引」に縮退させること。
2. README 紹介文を、上記の位置づけ（組み込み・一瞬の用途・lightweight・セッション継続／エージェント横断）に合わせて書き換えること。
3. README から切り出す詳細ドキュメントの配置・ファイル名・内容境界。
4. `docs/` ファイル名を kebab-case に統一すること（既存含む）。
5. 既存 `docs/` 文書とのリンク整合（リネーム・移動後の相対リンク切れを防ぐ）。
6. 文書の言語方針（現状どおり英語を維持する）。

### スコープ外

- API / プロトコル仕様そのものの書き換え（手順・API 契約の意味変更はしない）
- `prompts/` 配下の仕様・計画書の大規模再編（旧パス参照の更新は R4 の範囲で行う）
- `examples/*/README.md` の大規模書き換え（必要ならリンク更新と、紹介方針に沿った短い一文の調整のみ）
- 新規機能の実装やコード変更
- `docs/issue-26-verification.md` など検証メモの統合・削除判断（今回は現状維持）
- HarnessRouter 等の詳細比較文書の作成（任意。README で長文比較はしない）

---

## 要件 (Requirements)

### 必須要件 (Must)

#### R1: README をインデックス中心の短い文書にする

トップ `README.md` に残す内容は次に限定する。

1. ロゴ・タグライン・1〜2 段落の紹介（**R8 の位置づけに従う**）
2. Why Tern / What Tern Provides（簡潔。長文化しない。軸は R8）
3. Architecture Overview（CAWA / LLMGP / Integration Layer の短い説明）
4. Status（1〜2 文）と現在 Phase への一言（S2）
5. **Documentation** セクション — `docs/` 配下への索引（カテゴリ付きリンク一覧）
6. Contributing / License

README から **本文ごと移す** もの（索引リンクのみ残す）:

- Installation
- Quick Start（手順・設定 YAML・vault 説明の全文）
- Example（Server / Client / Vault / Multimodal / Interoperability の長いコードブロック）
- Artifact API Examples
- Example Use Cases（詳細）→ `docs/use-cases.md`（R8 に合わせて再構成可）
- Vision（短く Why に吸収するか `docs/use-cases.md` へ）
- Roadmap（詳細チェックリスト）
- Project Structure（ツリー）
- Design Documents の長い説明（アーキテクチャ文書へ統合）
- Testing のコマンド一覧

目標行数の目安: **約 120〜200 行**（厳密な上限ではないが、現状の半分以下を目指す）。

#### R2: 詳細は `docs/` 以下に英語で整理する

切り出し先の提案構成（ファイル名は実装計画で微調整可。いずれも kebab-case）:

| 新規ファイル | 移行元 (README) | 内容 |
| :--- | :--- | :--- |
| `docs/getting-started.md` | Installation + Quick Start | 前提条件、ビルド、vault、設定、起動、ternctl、短い client 例 |
| `docs/examples.md` | Example（Server/Client/Vault/Multimodal/Interoperability） | 動作例とコード片。`examples/` へのリンクを維持 |
| `docs/artifacts.md` | Artifact API Examples | System/User Artifact、collectors、upload/download、artifact-pipeline |
| `docs/use-cases.md` | Example Use Cases + Vision | **R8 に沿って**ユースケースを再構成。interoperability 例は残してよいが主軸にしない |
| `docs/architecture.md` | Architecture Overview（拡充）+ Design Documents | CAWA / LLMGP / Integration、既存 protocol へのリンク |
| `docs/roadmap.md` | Roadmap | Phase 1/2/3 チェックリスト |
| `docs/testing.md` | Testing | build / integration / unit の実行方法 |
| `docs/project-structure.md` | Documentation > Project Structure | ディレクトリツリー |

既存ファイルは **R7** に従ってリネームし、新パスでリンクする。

| 現状 | リネーム後 | 扱い |
| :--- | :--- | :--- |
| `docs/ReferenceManual-WebAPIs.md` | `docs/reference-manual-web-apis.md` | 索引に「API Reference」として掲載 |
| `docs/client-sse-tool-input.md` | （変更なし・既に kebab-case） | 索引または examples からリンク |
| `docs/sse-chunk-protocol.md` | （変更なし・既に kebab-case） | 索引または architecture からリンク |
| `docs/issue-26-verification.md` | （変更なし・既に kebab-case） | 公開索引には載せない（内部検証メモとして現状維持） |
| `docs/resources/` | （変更なし） | ロゴ等。README の画像パスは維持 |

#### R3: `docs/README.md` をドキュメントハブにする

`docs/README.md` を追加し、カテゴリ別の目次を置く。トップ `README.md` の Documentation セクションは、このハブへのリンクを先頭に置き、主要ドキュメントへの直リンクも併記してよい。

提案カテゴリ:

1. Getting Started
2. Guides / Examples
3. Architecture & Protocols
4. API Reference
5. Roadmap & Testing
6. Project Structure

#### R4: リンク整合を保つ

- README 内の相対リンクを、移動・リネーム後パスに更新する。
- 切り出し文書内の `examples/`・`settings/`・`docs/*.md` へのリンクが切れないこと。
- リポジトリ内（`README.md`、`docs/`、`examples/**/README.md`、必要ならコードコメント）の旧パス参照を新パスへ更新する。
- `prompts/` 配下の履歴文書に残る旧パスは必須更新としない（実装計画で「ユーザー向け・現行ドキュメント」を優先範囲とする）。
- リネームは `git mv` を使い、履歴を維持する。

#### R5: 手順・API 契約の意味を変えない（紹介文は例外）

- 手順・フラグ・API パス・設定キー・プロトコル説明の切り出しは原則「移動 + 見出し調整 + 前後の導入文」。
- **例外**: README 先頭のプロダクト紹介、Why / What、および `docs/use-cases.md` の主軸は **R8 に従って意図的に書き換える**。
- 明らかな重複（Quick Start の client 例と Example の client 例）は、どちらかに寄せてよいが、削除する情報はインデックスまたは他文書から到達可能であること。

#### R6: 言語方針

- `README.md` および `docs/` 配下のユーザー向け文書は **英語**（現行 README に合わせる）。
- 本仕様・実装計画（`prompts/`）は日本語。

#### R7: `docs/` 内の命名を kebab-case で統一する（旧 M1・必須）

- 新規ファイルはすべて kebab-case（例: `getting-started.md`, `reference-manual-web-apis.md`）。
- 既存の `ReferenceManual-WebAPIs.md` は必ず `reference-manual-web-apis.md` へリネームする。
- リネーム後、ユーザー向け文書と example README からの参照をすべて新パスに更新する（R4）。

#### R8: README 紹介を「組み込み・一瞬の用途・lightweight」軸に更新する

README（および必要なら `docs/use-cases.md`）で伝えるべきメッセージ:

1. **Embeddable for momentary use** — 設計の起点は、サービス基盤ではなく、アプリに組み込んで短時間の用途に使うこと。コンテナを強制しない。
2. **Lightweight by default** — サービスとして起動することもできるが、常駐共通基盤より遥かに軽い。常駐しない使い方が主なのでメモリも多くは消費しない。
3. **Short-lived is optional, not mandatory** — 短命利用から始められるが、短命を強制しない。必要なら延命・サービス起動も可。
4. **Continuity when you need it** — セッションに継続性がある。Coding Agent を横断して使い続けられる。
5. **Casual agent tool** — 「気軽に使えるエージェントツール」というトーンを冒頭で示す。
6. **Interoperability remains a capability** — Context / Agent / Model の相互運用は What の一項目として残してよいが、冒頭の主軸は上記 1〜5 とする。

英語での表現方針（実装時に自然な英文へ整える。以下は意図のメモ）:

- 旧: 「単一エージェントにコミットできないから interoperability」
- 新: 「組み込んで一瞬使い、必要ならセッションを続け、エージェントもまたげる。軽い。コンテナも常駐も強制しない」

競合名（HarnessRouter 等）は README に必須ではない。書く場合は「サービス起動中心の共通基盤」との境界を 1〜2 文で示し、比較表や攻撃的な文言は作らない。

### 推奨要件 (Should)

#### S1: README 先頭で 30 秒で価値が分かる

Why / What のあとに「Start here」として `docs/getting-started.md` への明確な CTA を置く。

#### S2: Roadmap は docs に寄せ、README には現状 Phase のみ一言

例: `Current phase: Phase 2 — see [Roadmap](docs/roadmap.md).`

#### S3: 切り出し後の各 docs ファイル先頭に 1 段落の概要を置く

単体で読めるようにする。

### 任意要件 (May)

#### M1: 将来の i18n（日本語版 docs）は対象外のまま設計スペースだけ残す

（旧 M2。kebab-case 統一は R7 へ昇格済み）

---

## 提案: 簡素化後 README の骨格

```markdown
# Tern
> Fly with the best agent. Anywhere. Anytime.

(短い紹介: embed for a moment, lightweight, no forced containers/residency)

## Why Tern?
(共通処理基盤として常駐させる前提ではなく、組み込んで気軽に使う。必要なら継続・横断もできる)

## What Tern Provides
- Embeddable, on-demand agent runtime (containers optional, not required)
- Lightweight operation (service mode possible; not a heavy always-on platform)
- Session continuity and cross-agent workflows
- Context / Agent / Model interoperability (capability, not the sole pitch)

## Architecture Overview
...

## Status
Current phase: Phase 2 — see docs/roadmap.md

## Documentation
- [Documentation Hub](docs/README.md)
- Getting Started → docs/getting-started.md
- Examples → docs/examples.md
- Artifacts → docs/artifacts.md
- Use Cases → docs/use-cases.md
- ...
- API Reference → docs/reference-manual-web-apis.md

## Contributing
...

## License
...
```

---

## 受け入れ条件 (Acceptance Criteria)

1. トップ `README.md` が索引中心になり、インストール手順・長いコード例・Artifact API の本文が README に残っていない。
2. 上記 R2 の新規 docs（または実装計画で確定した同等構成）が存在し、移行元の実質的な情報が欠落なく到達できる。
3. `docs/README.md` がカテゴリ付きハブとして機能する。
4. README および新 docs・example README 内の主要相対リンクが有効である。
5. `docs/ReferenceManual-WebAPIs.md` が存在せず、`docs/reference-manual-web-apis.md` へリネームされ、ユーザー向け参照が新パスになっている。
6. README 冒頭の紹介が R8（組み込み・一瞬の用途・lightweight・セッション継続／エージェント横断）を主軸にしており、interoperability のみを第一の売りにしていない。
7. コード・ビルド・テストの動作要件は変更しない（文書のみの変更）。

---

## 実装時の注意

- ブランチ: `refine-documentation`
- 中間生成物は `tmp/` 配下に置く。
- フェーズ移行は人間の明示指示があるまで行わない（本ファイルは仕様フェーズの成果物）。
- 大きなコミットを避ける場合の分割案（実装計画で詳細化）:
  1. 既存 docs の kebab-case リネームと参照更新
  2. docs 新規ファイル作成（切り出し）と use-cases の再構成
  3. README 簡素化（R8 紹介文）とリンク更新
)