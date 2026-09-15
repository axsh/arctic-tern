# 000-Explicit-System-Artifact-CRUD

> **Source Specification**: `prompts/phases/001-phase02/branches/feat-register-files/ideas/000-Explicit-System-Artifact-CRUD.md`

## Goal Description

System Artifact に対する明示的な外部登録 API を追加し、自動収集 (Tier1/2/3) と共存可能な append-only イベントモデルを維持したまま、`create/update/delete` を Web API と `client/v1` から実行可能にする。既存 read API (`list/get/content/archive`) の後方互換性を維持し、検証を unit/integration/e2e でコード化する。

## User Review Required

- `DELETE /api/v1/artifacts/system/{key}` の未存在 key 挙動を **404（厳格）** で固定する方針で進める（仕様 R12）。
- `POST /api/v1/artifacts/system/events` は operation 明示の低レベル API とし、通常利用は `PUT` / `DELETE` を推奨する方針で進める。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: System Artifact 明示 CRUD API 追加 | Proposed Changes > Artifact API Handler (`shared/libs/go/artifact/api/system.go`) / Client (`client/v1/artifacts.go`) |
| R2: append-only で既存 event モデル再利用 | Proposed Changes > Artifact API Handler (`shared/libs/go/artifact/api/system.go`) / Store 既存 `SaveSystemArtifactEvent` 利用 |
| R3: リクエストスキーマ定義 | Proposed Changes > Artifact API Handler (`shared/libs/go/artifact/api/system.go`) / API tests |
| R4: PUT create/update 判定統一 | Proposed Changes > Artifact API Handler (`shared/libs/go/artifact/api/system.go`) |
| R5: key/path 検証と create/update 存在確認 | Proposed Changes > Artifact API Handler (`shared/libs/go/artifact/api/system.go`) / API tests |
| R6: collectors ON/OFF 非依存の明示登録 | Proposed Changes > E2E (`tests/artifact_e2e_test.go`) |
| R7: list/filter 互換維持 | Proposed Changes > E2E / API tests (`shared/libs/go/artifact/api/system_test.go`) |
| R8: client/v1 追加 API | Proposed Changes > Client (`client/v1/artifacts.go`) / Client tests |
| R9: API/Client/Integration テスト追加 | Proposed Changes > `_test.go` files / Step-by-Step 実装順 |
| R10: docs 更新 | Proposed Changes > `docs/ReferenceManual-WebAPIs.md`, `README.md` |
| R11: 後方互換性維持 | Proposed Changes 全体 + Verification Plan 回帰実行 |
| R12: エラーコード方針固定 | Proposed Changes > API Handler バリデーション / API tests |

## Proposed Changes

### Artifact API Tests (TDD First)

#### [MODIFY] `shared/libs/go/artifact/api/system_test.go` (file://shared/libs/go/artifact/api/system_test.go)
* **Description**: System Artifact 明示 CRUD のユニットテストを先に追加し、既存 read API テストを維持したまま write API の expected behavior を固定する。
* **Technical Design**:
  * 追加テスト関数:
    * `TestSystemAPI_Put_Create`
    * `TestSystemAPI_Put_Update`
    * `TestSystemAPI_Delete_Tombstone`
    * `TestSystemAPI_PostEvents_AppendWithOperation`
    * `TestSystemAPI_Write_BadRequestCases`
  * `session` seed、実ファイル作成 (`t.TempDir`) を使って `actual_path` 存在検証を再現。
* **Logic**:
  * PUT create/update 判定:
    1. `GetSystemArtifactByKey(key)` が空、または最新 operation が `delete` なら `create` (`201`)。
    2. それ以外は `update` (`200`)。
  * DELETE:
    * 該当 key が存在しない場合 `404`。
    * 存在する場合 `operation=delete` の新規 event を追加し `200` または `204`（本計画では `200` + JSON を採用）。
  * POST /events:
    * `operation in {create,update,delete}` を必須化し append-only で保存。
  * バリデーション:
    * key 空 / `..` / 絶対 key / `session_id` 空 / create-update で path 不存在 / RFC3339 不正を `400`。

#### [MODIFY] `client/v1/artifacts_test.go` (file://client/v1/artifacts_test.go)
* **Description**: `SystemArtifactClient` の新規 write メソッドの HTTP マッピングと status handling を固定する。
* **Technical Design**:
  * stub server に `PUT /api/v1/artifacts/system/{key}`, `DELETE /api/v1/artifacts/system/{key}`, `POST /api/v1/artifacts/system/events` を追加。
  * 追加テスト:
    * `TestSystemClient_Put`
    * `TestSystemClient_Delete`
    * `TestSystemClient_AppendEvent`
* **Logic**:
  * Put は `201|200` を成功扱い。
  * Delete は `200|204` を成功扱い（サーバ実装は 200 を返す）。
  * AppendEvent は `201` を成功扱い。

#### [MODIFY] `tests/artifact_e2e_test.go` (file://tests/artifact_e2e_test.go)
* **Description**: integration build tag 付き E2E に明示 CRUD シナリオを追加する。
* **Technical Design**:
  * 追加テスト:
    * `TestE2E_SystemArtifact_ExplicitCRUD`
    * `TestE2E_SystemArtifact_ExplicitCRUD_WithCollectorsOff`
    * `TestE2E_SystemArtifact_ContentAfterExplicitPut`
  * `v1.Client` 経由で session を作成し、テスト用実ファイルを workDir に生成して明示登録。
* **Logic**:
  * collectors 全 OFF セッション (`structured_tool=false`, `shell_parser=false`, `workdir_reconcile=false`) でも明示 PUT/DELETE が成功することを検証。
  * 登録後に `List` / `GetByKey` / `Download` / `include_deleted` 条件を通す。

### Artifact API Handler

#### [MODIFY] `shared/libs/go/artifact/api/system.go` (file://shared/libs/go/artifact/api/system.go)
* **Description**: System handler に write routes と validation/normalization を追加し、既存 read routes との互換を保つ。
* **Technical Design**:
  * ルーティング拡張:
    * `routeRoot`: `GET` に加えて `POST` (subPath=`events`) を許可。
    * `routeByKey`: `GET` / `PUT` / `DELETE` / `POST archive` / `GET content` を分岐。
  * 新規 request struct:
    * `systemArtifactWriteRequest`:
      * `SessionID string json:"session_id"`
      * `TurnID string json:"turn_id,omitempty"`
      * `CorrelationID string json:"correlation_id,omitempty"`
      * `ActualPath string json:"actual_path,omitempty"`
      * `ToolName string json:"tool_name,omitempty"`
      * `OccurredAt string json:"occurred_at,omitempty"`
    * `systemArtifactAppendRequest`:
      * 上記 + `Key string json:"key"` + `Operation string json:"operation"`
  * 新規ヘルパ:
    * `validateSystemKey(key string) (string, error)`
    * `resolveActualPath(sessionID, key, actualPath string) (string, error)`
    * `parseOccurredAt(raw string) (time.Time, error)`
    * `latestOperation(events []store.SystemArtifactEvent) string`
* **Logic**:
  * `tool_name` 省略時は `api:system_manual` を採用。
  * `actual_path` 省略時は `session.work_dir + key` で解決するために session 情報参照を追加する。`store.ArtifactStore` には session 参照 API がないため、handler 側に session resolver を注入する（下記 `agentservice/service.go` と連動）。
  * create/update は `os.Stat` で存在確認、delete は確認不要。
  * append-only: `SaveSystemArtifactEvent` のみを呼び既存 row 更新はしない。
  * 既存 `handleList` / `handleGetByKey` / `handleContent` / `handleArchive` の I/O 互換は維持。

### Agent Service Wiring

#### [MODIFY] `shared/libs/go/agentservice/service.go` (file://shared/libs/go/agentservice/service.go)
* **Description**: SystemArtifactHandler が `session_id` から `work_dir` を引けるように resolver を注入する。
* **Technical Design**:
  * `artifactapi.NewSystemArtifactHandler` のシグネチャ拡張に追従:
    * 例: `NewSystemArtifactHandler(store, func(sessionID string) (workDir string, ok bool) { ... })`
  * 既存 route 登録部のみ差分。
* **Logic**:
  * `sessions.Get(sessionID)` で取得できる場合に `record.WorkDir` を返す。
  * 取得失敗時は `actual_path` 省略をエラーにする経路へ流す。

### Client API

#### [MODIFY] `client/v1/artifacts.go` (file://client/v1/artifacts.go)
* **Description**: `SystemArtifactClient` に明示 write API を追加し、既存 read API と同一利用体験で呼べるようにする。
* **Technical Design**:
  * 新規型:
    * `type SystemArtifactWriteRequest struct { SessionID, TurnID, CorrelationID, ActualPath, ToolName, OccurredAt string }`
    * `type SystemArtifactAppendRequest struct { Key, Operation string; SystemArtifactWriteRequest }`
    * `type SystemArtifactWriteResponse struct { Source, Key, Operation, Status string; SessionID, TurnID, CorrelationID, ToolName, OccurredAt string }`
  * 新規メソッド:
    * `func (sc *SystemArtifactClient) Put(ctx context.Context, key string, req SystemArtifactWriteRequest) (*SystemArtifactWriteResponse, error)`
    * `func (sc *SystemArtifactClient) Delete(ctx context.Context, key string, req SystemArtifactWriteRequest) (*SystemArtifactWriteResponse, error)`
    * `func (sc *SystemArtifactClient) AppendEvent(ctx context.Context, req SystemArtifactAppendRequest) (*SystemArtifactWriteResponse, error)`
* **Logic**:
  * Put: `201|200` 成功、他はエラー。
  * Delete: `200|204` 成功、他はエラー。
  * AppendEvent: `201` 成功、他はエラー。
  * request body は `json.Marshal` で送信し `Content-Type: application/json` を統一。

### Documentation

#### [MODIFY] `docs/ReferenceManual-WebAPIs.md` (file://docs/ReferenceManual-WebAPIs.md)
* **Description**: System Artifact write API を API リファレンスに追加する。
* **Technical Design**:
  * 新節:
    * `PUT /api/v1/artifacts/system/:key`
    * `DELETE /api/v1/artifacts/system/:key`
    * `POST /api/v1/artifacts/system/events`
  * request/response JSON, `400/404/405/500` を明記。
* **Logic**:
  * append-only 意味論（update/delete はイベント追加）を明示。
  * collectors 設定とは独立して利用可能であることを記載。

#### [MODIFY] `README.md` (file://README.md)
* **Description**: Artifact API Examples に System Artifact 明示登録例を追加する。
* **Technical Design**:
  * Go code snippet に `SystemArtifacts().Put/Delete/AppendEvent` 例を追加。
  * User Artifact と用途分離（入力データ保管 vs 変更イベント登録）を追記。
* **Logic**:
  * path + operation メタデータであること、content はディスク参照であることを維持記述。

## Implementation Tasks (Checklist)

- [x] 1. `system_test.go` に明示 CRUD の失敗先行テストを追加する。
- [x] 2. `artifacts_test.go` に client write API テストを追加する。
- [x] 3. `tests/artifact_e2e_test.go` に明示 CRUD E2E を追加する。
- [x] 4. `system.go` に write route と validation/normalization を実装する。
- [x] 5. `service.go` を更新して session workDir resolver を handler へ注入する。
- [x] 6. `artifacts.go` に `Put/Delete/AppendEvent` と新規型を実装する。
- [x] 7. `docs/ReferenceManual-WebAPIs.md` と `README.md` を更新する。
- [x] 8. `./scripts/process/build.sh` を実行して成功させる。
- [x] 9. `./scripts/process/integration_test.sh --specify "TestSystemAPI_Put_|TestSystemClient_Put|TestE2E_SystemArtifact_ExplicitCRUD"` を実行して成功させる。
- [/] 10. 各ステップ単位でコミットし、最終的に `git push` する。

## Step-by-Step Implementation Guide

1. **Red (API tests)**: Edit `shared/libs/go/artifact/api/system_test.go` to add PUT/DELETE/POST(events) tests and validation error cases.
2. **Red (Client tests)**: Edit `client/v1/artifacts_test.go` to add write API stub routes and tests.
3. **Red (E2E tests)**: Edit `tests/artifact_e2e_test.go` to add explicit CRUD scenarios including collectors-off session.
4. **Green (API handler)**: Edit `shared/libs/go/artifact/api/system.go` to implement routes, request structs, validation, append-only save, and responses.
5. **Green (Wiring)**: Edit `shared/libs/go/agentservice/service.go` to provide session workDir resolution to system artifact handler.
6. **Green (Client)**: Edit `client/v1/artifacts.go` to expose typed write operations.
7. **Green (Docs)**: Edit `docs/ReferenceManual-WebAPIs.md` and `README.md`.
8. **Run build/unit**: Run `./scripts/process/build.sh` and fix until green.
9. **Run integration**: Run `./scripts/process/integration_test.sh --specify "TestSystemAPI_Put_|TestSystemClient_Put|TestE2E_SystemArtifact_ExplicitCRUD"` and fix until green.
10. **Commit/Push**: Commit each completed logical step; after all tests pass, push branch.

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration Tests (実行可能コマンド)**: `./scripts/process/integration_test.sh --specify "TestSystemAPI_Put_|TestSystemClient_Put|TestE2E_SystemArtifact_ExplicitCRUD"`
3. **Integration Tests (カテゴリ併記ルール表記)**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories common --specify "TestSystemAPI_Put_|TestSystemClient_Put|TestE2E_SystemArtifact_ExplicitCRUD"`
4. **E2E Tests**: `tests/artifact_e2e_test.go` に新規ケースを追加し、上記 integration 実行で自動検証する。

## Documentation

- `docs/ReferenceManual-WebAPIs.md` に新規 System Artifact write APIs とエラーコード表を追加。
- `README.md` の Artifact API Examples に System Artifact 明示登録サンプルを追加。
- 既存の `file_change_collectors` 説明とは矛盾しないよう「自動収集」と「明示登録」の関係を併記する。

