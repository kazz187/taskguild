# TaskGuild

TaskGuild は、AI エージェントによるタスク自動実行を実現するワークフローオートメーションシステムです。

Workflow にステータスを定義し、各ステータスに Agent をバインドすることで、タスクのステータス変更をトリガーとして Agent が自動的にタスクを実行します。

## Architecture

```
┌─────────────┐       ┌──────────────────┐       ┌─────────────────┐
│   Frontend   │──────▶│  Backend Server   │◀──────│  Agent Manager  │
│ (React SPA)  │  RPC  │  (Go / Connect)  │ Stream │  (taskguild-    │
│              │       │                  │        │   agent)        │
└─────────────┘       └──────────────────┘       └────────┬────────┘
                              │                           │
                              │                    ┌──────▼──────┐
                              │                    │ Claude Agent │
                              │                    │  (SDK-based) │
                              ▼                    └─────────────┘
                       ┌──────────────┐
                       │   Storage    │
                       │ (Local/S3)   │
                       └──────────────┘
```

| Component | Description |
|-----------|-------------|
| **Backend Server** | Connect RPC ベースの API サーバー。Project / Workflow / Task / Agent の管理と、Agent Manager への Task 配信を行う |
| **Agent Manager** | プロジェクトのリポジトリ内で起動する常駐プロセス。Backend から Task を受信し、Claude Agent を起動してタスクを実行する |
| **Frontend** | React + TanStack ベースの Web UI。タスク管理、Workflow 設計、Agent 定義、Interaction 対応を行う |

## Getting Started

### 1. Backend Server のセットアップ

Backend Server はグローバルにアクセス可能な場所にデプロイします（HTTPS 推奨）。VPN 環境内に配置することも可能です。

#### 環境変数

| 変数名 | 必須 | デフォルト | 説明 |
|--------|------|-----------|------|
| `TASKGUILD_API_KEY` | **Yes** | - | API 認証キー。**十分に複雑なランダム文字列を使用してください** |
| `TASKGUILD_HTTP_HOST` | No | `""` (all interfaces) | バインドするホスト |
| `TASKGUILD_HTTP_PORT` | No | `3100` | リッスンポート |
| `TASKGUILD_ENV` | No | `local` | 環境 (`local` / `production`) |
| `TASKGUILD_LOG_LEVEL` | No | `debug` | ログレベル (`debug` / `info` / `warn` / `error`) |
| `TASKGUILD_STORAGE_TYPE` | No | `local` | ストレージ種別 (`local` / `s3`) |
| `TASKGUILD_STORAGE_BASE_DIR` | No | `.taskguild/data` | ローカルストレージのパス |
| `TASKGUILD_S3_BUCKET` | No | - | S3 バケット名 (`s3` 選択時) |
| `TASKGUILD_S3_PREFIX` | No | `taskguild/` | S3 プレフィックス |
| `TASKGUILD_S3_REGION` | No | `ap-northeast-1` | S3 リージョン |
| `TASKGUILD_PUBLIC_URL` | No | `http://localhost:3100` | 外部からアクセス可能な Backend の URL。プッシュ通知のアクションボタンからの API コールに使用 |
| `TASKGUILD_VAPID_PUBLIC_KEY` | No | - | Web Push 用 VAPID 公開鍵（プッシュ通知を使用する場合は必須） |
| `TASKGUILD_VAPID_PRIVATE_KEY` | No | - | Web Push 用 VAPID 秘密鍵（プッシュ通知を使用する場合は必須） |
| `TASKGUILD_VAPID_CONTACT` | No | `mailto:admin@taskguild.dev` | VAPID の連絡先メールアドレス |

#### ビルドと起動

```bash
cd backend

# ビルド
make build

# 起動
TASKGUILD_API_KEY="your-secure-random-api-key" ./bin/taskguild-server
```

> **API Key について**: `TASKGUILD_API_KEY` はすべてのクライアント（Frontend / Agent Manager）が使用する共通の認証キーです。`openssl rand -hex 32` などで十分に長いランダム文字列を生成してください。

#### Cloudflare Tunnel を使った公開（推奨）

Backend Server をインターネットからアクセス可能にする方法として、[Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/) が便利です。

```bash
# 1. cloudflared をインストール
# macOS
brew install cloudflared

# Linux
curl -L https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64 -o /usr/local/bin/cloudflared
chmod +x /usr/local/bin/cloudflared

# 2. Cloudflare にログイン
cloudflared tunnel login

# 3. Tunnel を作成
cloudflared tunnel create taskguild

# 4. DNS ルーティングを設定 (例: taskguild-api.example.com)
cloudflared tunnel route dns taskguild taskguild-api.example.com

# 5. config.yml を作成
cat > ~/.cloudflared/config.yml << 'EOF'
tunnel: <tunnel-id>
credentials-file: /home/<user>/.cloudflared/<tunnel-id>.json

ingress:
  - hostname: taskguild-api.example.com
    service: http://localhost:3100
  - service: http_status:404
EOF

# 6. Tunnel を起動（Backend Server と同じマシンで実行）
cloudflared tunnel run taskguild
```

### 2. Frontend の利用

Frontend は **https://taskguild.cc** で公開されています。

初回アクセス時に以下を設定してください：

- **API Base URL**: Backend Server の URL（例: `https://taskguild-api.example.com`）
- **API Key**: Backend Server に設定した `TASKGUILD_API_KEY`

設定はブラウザの localStorage に保存されます。

### 3. プッシュ通知のセットアップ（オプション）

スマートフォン（Android / iOS）やデスクトップのブラウザにプッシュ通知を送信できます。Agent が Permission Request や Question を作成すると、登録済みのデバイスに自動で通知が届きます。

> **前提条件**: HTTPS 環境が必要です。Service Worker と Push API は HTTPS（または localhost）でのみ動作します。

#### VAPID キーペアの生成

```bash
npx web-push generate-vapid-keys
```

出力される `Public Key` と `Private Key` を環境変数に設定してください。

#### 環境変数の設定

```bash
TASKGUILD_VAPID_PUBLIC_KEY="<生成された Public Key>"
TASKGUILD_VAPID_PRIVATE_KEY="<生成された Private Key>"
TASKGUILD_PUBLIC_URL="https://taskguild-api.example.com"
```

### 4. Agent Manager のセットアップ

Agent Manager は **プロジェクトのリポジトリ内で起動** します。

```bash
cd backend

# ビルド
make build-agent
```

```bash
# プロジェクトのリポジトリに移動
cd /path/to/your-project

# Agent Manager を起動
TASKGUILD_API_KEY="your-secure-random-api-key" \
TASKGUILD_SERVER_URL="https://taskguild-api.example.com" \
/path/to/bin/taskguild-agent
```

#### Agent Manager の環境変数

| 変数名 | 必須 | デフォルト | 説明 |
|--------|------|-----------|------|
| `TASKGUILD_API_KEY` | **Yes** | - | Backend と同じ API Key |
| `TASKGUILD_SERVER_URL` | No | `http://localhost:3100` | Backend Server の URL |
| `TASKGUILD_AGENT_MANAGER_ID` | No | 自動生成 (ULID) | Agent Manager の一意な ID |
| `TASKGUILD_MAX_CONCURRENT_TASKS` | No | `10` | 同時実行可能なタスク数 |
| `TASKGUILD_WORK_DIR` | No | `.` (カレントディレクトリ) | タスク実行時の作業ディレクトリ |
| `TASKGUILD_PROJECT_NAME` | No | 作業ディレクトリ名 | バインド先のプロジェクト名 |

Agent Manager は起動すると Backend に Subscribe し、タスク配信を待ち受けます。

---

## Core Concepts

### Project

Project は TaskGuild における管理単位です。リポジトリと 1:1 で対応します。

Frontend から Project を作成する際に `name` を設定します。Agent Manager は起動時の作業ディレクトリ名（または `TASKGUILD_PROJECT_NAME`）で Project とマッチングされます。

### Agent

Agent は、タスクを実行する AI エージェントの定義です。プロジェクトのリポジトリ内に `.claude/agents/` ディレクトリを作成し、Markdown ファイルで定義します。Agent Manager 起動時に自動的に Backend に同期されます。

```
your-project/
├── .claude/
│   └── agents/
│       ├── architect.md
│       ├── software-engineer.md
│       └── senior-engineer.md
└── ...
```

#### Agent 定義ファイルのフォーマット

YAML フロントマターとプロンプト本文で構成されます。

```markdown
---
name: software-engineer
description: ソフトウェアエンジニア。タスクの内容を読み実装を行う。
tools: Read, Write, Glob, Grep, Bash, WebSearch, Task
model: opus
permissionMode: acceptEdits
skills: [create-pr, investigate-logs]
memory: project
---

あなたはソフトウェアエンジニアです。タスクの内容をよく読み実装してください。
```

#### フロントマターのフィールド

| フィールド | 型 | 説明 |
|-----------|-----|------|
| `name` | string | Agent の識別名（ファイル名から自動設定も可能） |
| `description` | string | Agent の説明 |
| `tools` | string / list | 使用可能なツール（カンマ区切り or YAML リスト） |
| `disallowedTools` | string / list | 使用禁止のツール |
| `model` | string | 使用モデル: `sonnet` / `opus` / `haiku` / `inherit` |
| `permissionMode` | string | 権限モード: `default` / `acceptEdits` / `dontAsk` / `bypassPermissions` / `plan` |
| `skills` | list | プリロードするスキル |
| `memory` | string | メモリスコープ: `user` / `project` / `local` |

### Workflow

Workflow はタスクが辿るステータスの流れを定義します。各ステータスに Agent をバインドすることで、タスクのステータス遷移に応じた自動実行が可能になります。

```
Workflow: "Development Flow"
├── Draft        (is_initial: true)
│   └── transitions_to: [Plan]
├── Plan         (agent: architect, permission_mode: plan)
│   └── transitions_to: [Develop]
├── Develop      (agent: software-engineer, inherit_session_from: Plan)
│   └── transitions_to: [Review]
├── Review       (agent: senior-engineer, inherit_session_from: Develop)
│   └── transitions_to: [Develop, Closed]
└── Closed       (is_terminal: true)
```

#### Status のフィールド

| フィールド | 説明 |
|-----------|------|
| `name` | ステータス名（Workflow 内で一意） |
| `order` | 表示順序 |
| `is_initial` | `true` の場合、タスク作成時のデフォルトステータス（1つのみ） |
| `is_terminal` | `true` の場合、タスクの終了状態 |
| `transitions_to` | 遷移可能な次のステータス名のリスト |
| `agent_id` | このステータスにバインドする Agent の ID |
| `permission_mode` | このステータスでの権限モード（Agent 設定より優先） |
| `inherit_session_from` | 前ステータスのセッションを直接 resume するための設定 |
| `hooks` | このステータスで実行するフック（スキル/スクリプト） |
| `enable_agent_md_harness` | Agent 定義ファイルの自動更新（デフォルト有効） |

### Task

Task はワークフロー上で実行される作業単位です。

#### Task のライフサイクル

```
Task 作成 (Initial Status, UNASSIGNED)
    │
    ▼
[Status with Agent] ── Orchestrator が検知 → PENDING → Agent が Claim → ASSIGNED
    │
    │  Agent がタスクを実行（複数ターン）
    │    ├── Tool 使用 → 権限チェック → TaskLog に記録
    │    ├── NEXT_STATUS 出力 → ステータス遷移
    │    ├── CREATE_TASK 出力 → サブタスク作成
    │    └── TASK_DESCRIPTION 出力 → タスク説明更新
    │
    ▼
[Next Status] → 次の Agent が自動実行 ... → [Terminal Status]
```

#### Task のフィールド

| フィールド | 説明 |
|-----------|------|
| `title` | タスクのタイトル |
| `description` | タスクの詳細説明（Agent への指示内容） |
| `workflow_id` | 使用する Workflow の ID |
| `status_id` | 現在のステータス |
| `metadata` | カスタムメタデータ（key-value） |
| `use_worktree` | `true` の場合、Agent が git worktree を使用して作業 |

### Interaction

Agent がタスク実行中にユーザーの入力や承認を必要とする場合、Interaction が作成されます。

| タイプ | 説明 |
|--------|------|
| **Permission Request** | ツール使用の許可要求（Bash コマンド等） |
| **Question** | Agent からの質問（プラン承認等） |
| **Notification** | ステータス変更通知 |
| **User Message** | ユーザーからの追加指示 |

Frontend の Chat 画面やタスク詳細画面でリアルタイムに確認・応答が可能です。プッシュ通知を設定すればスマートフォンからも応答できます。

---

## Agent Directives

Agent はタスク実行中に以下のディレクティブを出力に含めることで、タスクの制御を行います。これらは Agent の出力テキストから自動的にパースされます。

### NEXT_STATUS

タスクのステータスを遷移させます。出力の最終行に記述します。

```
NEXT_STATUS: Review
```

遷移可能なステータスが 1 つだけの場合、Agent が `NEXT_STATUS` を指定しなくても自動的に遷移します。

### CREATE_TASK

サブタスクを作成します。

```
CREATE_TASK_START
title: サブタスクのタイトル
status: Develop
use_worktree: true
worktree: existing-worktree-name

サブタスクの説明文。
親タスクのコンテキストを引き継ぐ。
CREATE_TASK_END
```

サブタスクは親タスクのセッション ID を引き継ぎ、同じ会話コンテキストで実行を開始できます。

### TASK_DESCRIPTION

タスクの説明を更新します。

```
TASK_DESCRIPTION_START
更新後のタスク説明。
Agent が作業の進捗に合わせて説明を更新する際に使用。
TASK_DESCRIPTION_END
```

### TASK_METADATA

タスクのメタデータを更新します。

```
TASK_METADATA: pr_url=https://github.com/owner/repo/pull/123
```

---

## Session Management

TaskGuild はステータス別のセッション ID（`session_id_{StatusName}`）を使用して Claude のセッションを管理します。

### セッションの Resume

- **同一ステータス内の複数ターン**: Turn 0 で作成されたセッションを Turn 1 以降で直接 resume します
- **ステータス遷移 (inherit)**: `inherit_session_from` が設定されている場合、前ステータスのセッションを直接 resume します（fork しません）
- **サブタスク**: 親タスクのセッション ID を引き継ぎ、直接 resume します

### セッションの Fork

以下のケースでのみセッションが fork されます：

- **Hook/スキル実行**: `after_task_execution` 等のフックは、メインタスクのセッションに影響を与えないよう fork して実行されます
- **Agent MD ハーネス**: タスク完了後の Agent 定義ファイル自動更新は fork して実行されます

### セッション ID の保持

ステータス別セッション ID（`session_id_Plan`、`session_id_Develop` 等）はすべての遷移を通じて保持され、消去されることはありません。これにより、どのステータスのセッションでも後から参照・引き継ぎが可能です。

---

## Worktree

`use_worktree: true` のタスクは、git worktree を使用して独立したブランチで作業します。

### Worktree の動作

1. タスク開始時に `.claude/worktrees/{name}/` ディレクトリと `worktree-{name}` ブランチが作成されます
2. Agent は worktree ディレクトリ内で作業します
3. タスク完了後、worktree は保持されます（手動で削除可能）

### 同時実行制御

同一 worktree では一度に 1 つのタスクのみ実行可能です。

- `ClaimTask` 時にサーバーサイドで worktree の占有状態をチェックします
- 同じ worktree で既に ASSIGNED のタスクがある場合、新しいタスクの claim は拒否されます
- タスクが完了（UNASSIGNED）すると、同じ worktree で待機中の PENDING タスクが自動的にリブロードキャストされます

これにより、同一ブランチ上での複数 Agent の同時書き込みによる git 競合を防ぎます。

---

## Hooks

Workflow の各ステータスにフック（スキルまたはスクリプト）を設定できます。

### トリガー

| トリガー | タイミング | セッション |
|---------|----------|-----------|
| `before_task_execution` | タスク開始前 | セッションなし |
| `after_task_execution` | タスク完了後 | メインセッションを fork |
| `before_worktree_creation` | Worktree 作成前 | セッションなし |
| `after_worktree_creation` | Worktree 作成後 | メインセッションを fork |

### フックの種類

- **スキル**: `.claude/skills/{name}/SKILL.md` で定義された再利用可能なプロンプト
- **スクリプト**: シェルスクリプト（`.claude/scripts/` に配置）

フックは順序付きで順次実行されます。個々のフックの失敗はメインタスクをブロックしません。

---

## Skills

スキルは再利用可能なプロンプトテンプレートです。`.claude/skills/{name}/SKILL.md` に配置します。

```markdown
---
name: create-pr
description: PR を作成または既存 PR に push する
allowedTools: [Bash, Read, Grep]
model: inherit
context: inline
userInvocable: true
argumentHint: "[branch-name]"
---

## Create PR

PR の作成手順...
```

スキルは以下の場面で使用されます：

- Workflow のフックとして自動実行
- Agent にプリロードして利用可能にする（`skills` フロントマター）

---

## Agent MD Harness

タスク完了時に自動的に実行される Agent 定義ファイルの更新機能です。

1. タスクの実行結果を分析し、Agent 定義ファイル（`.claude/agents/{name}.md`）の「Lessons Learned」セクションに知見を追記します
2. コードベースの構造知識やプロセス改善の知見が蓄積され、次回以降のタスク実行効率が向上します
3. メインセッションを fork して独立に実行されるため、タスクの実行には影響しません

ステータスごとに `enable_agent_md_harness` で有効/無効を切り替えられます（デフォルト有効）。

---

## Task Logs (Append-Only)

タスクの実行ログは追記型の TaskLog として時系列で記録されます。メタデータの上書きは行いません。

### ログカテゴリ

| カテゴリ | 説明 |
|---------|------|
| `TURN_START` / `TURN_END` | Claude ターンの開始/終了 |
| `TOOL_USE` | ツール使用（入力・出力を記録） |
| `AGENT_OUTPUT` | Agent のテキスト出力 |
| `DIRECTIVE` | ディレクティブの検出（NEXT_STATUS, CREATE_TASK 等） |
| `RESULT` | タスク結果（summary / error / plan / description） |
| `STATUS_CHANGE` | ステータス変更 |
| `HOOK` | フック実行 |
| `SYSTEM` | システムイベント |
| `ERROR` | エラー |
| `STDERR` | 標準エラー出力 |

### Result ログ

タスクの結果は `RESULT` カテゴリの TaskLog として記録されます。`result_type` で種別を区別します：

- `summary`: タスク完了時の結果サマリー
- `error`: エラーメッセージ
- `plan`: プラン承認後のプラン内容
- `description`: タスク説明の更新内容

タスク詳細ページでは、これらの Result ログが時系列で表示されます。

---

## Frontend

### ページ構成

| ページ | パス | 説明 |
|--------|------|------|
| プロジェクト一覧 | `/` | プロジェクトの選択・作成 |
| プロジェクトボード | `/projects/:id` | タスクのカンバンボード表示 |
| タスク詳細 | `/projects/:id/tasks/:taskId` | タスクの実行ログ・Interaction・結果表示 |
| プロジェクトチャット | `/projects/:id/chat` | プロジェクト内の Interaction と Result の時系列表示 |
| グローバルチャット | `/global-chat` | 全プロジェクトの統合タイムライン |
| ワークフロー | `/projects/:id/workflows` | ステータス・遷移・フック・Agent バインドの設定 |
| Agent 管理 | `/projects/:id/agents` | Agent 定義の編集・同期 |
| スキル管理 | `/projects/:id/skills` | スキルの編集・同期 |
| スクリプト管理 | `/projects/:id/scripts` | スクリプトの編集・実行 |
| パーミッション | `/projects/:id/permissions` | ツール使用の許可ルール設定 |
| Worktree 管理 | `/projects/:id/worktrees` | Worktree の一覧・削除・git pull |
| テンプレート | `/templates` | タスクテンプレートの管理 |

### リアルタイム更新

タスクの作成・更新・ステータス変更・Interaction 作成などのイベントをリアルタイムに受信し、UI を自動更新します。

---

## Example: 開発ワークフローのセットアップ

### Step 1: Agent を定義

`.claude/agents/architect.md`:
```markdown
---
name: architect
description: システムアーキテクト。タスクの計画を立案する。
model: opus
permissionMode: plan
---

あなたはシステムアーキテクトです。タスクの要件を分析し、実装計画を立ててください。
```

`.claude/agents/software-engineer.md`:
```markdown
---
name: software-engineer
description: ソフトウェアエンジニア。計画に基づいて実装する。
tools: Read, Write, Glob, Grep, Bash, WebSearch, Task
model: opus
permissionMode: acceptEdits
skills: [create-pr]
---

あなたはソフトウェアエンジニアです。計画に従って実装してください。
```

`.claude/agents/senior-engineer.md`:
```markdown
---
name: senior-engineer
description: シニアエンジニア。コードレビューを行う。
tools: Read, Glob, Grep, Bash
model: opus
permissionMode: default
---

あなたはシニアエンジニアです。変更内容をレビューしてください。
```

### Step 2: Workflow を作成

Frontend から以下の Workflow を作成します：

| Status | Initial | Terminal | Transitions | Agent | Inherit Session |
|--------|---------|----------|-------------|-------|-----------------|
| Draft | Yes | No | Plan | - | - |
| Plan | No | No | Develop | architect | - |
| Develop | No | No | Review | software-engineer | Plan |
| Review | No | No | Develop, Closed | senior-engineer | Develop |
| Closed | No | Yes | - | - | - |

`inherit_session_from` により、Develop は Plan のセッションを直接 resume し、Review は Develop のセッションを直接 resume します。これにより、各 Agent は前ステータスの会話コンテキストを完全に引き継いで作業できます。

### Step 3: Agent Manager を起動

```bash
cd /path/to/your-project

TASKGUILD_API_KEY="your-api-key" \
TASKGUILD_SERVER_URL="https://taskguild-api.example.com" \
taskguild-agent
```

### Step 4: Task を作成して実行

1. Frontend で Task を作成（Draft ステータスで作成される）
2. Task のステータスを **Plan** に変更
3. `architect` Agent が自動的に計画を立案
4. 完了すると **Develop** に自動遷移（Plan のセッションを resume）
5. `software-engineer` Agent が計画に基づいて実装
6. 完了すると **Review** に自動遷移（Develop のセッションを resume）
7. `senior-engineer` Agent がコードレビューを実行
8. レビュー結果に応じて **Develop**（修正が必要）or **Closed**（完了）に遷移

---

## API Authentication

すべての API リクエストには認証が必要です。以下のいずれかのヘッダーで API Key を送信します：

```
X-API-Key: <your-api-key>
Authorization: Bearer <your-api-key>
```

## Storage

Backend Server はデータを YAML ファイルとして保存します。

- **Local Storage** (デフォルト): `.taskguild/data/` ディレクトリに保存
- **S3 Storage**: Amazon S3 バケットに保存（`TASKGUILD_STORAGE_TYPE=s3` で有効化）

## Technology Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go, Connect RPC |
| Agent | Go, Claude Agent SDK |
| Frontend | React 19, TypeScript, TanStack Router/Query, Tailwind CSS 4 |
| Storage | YAML (local) / S3 |
| Build | Make (backend), Vite (frontend) |

## License

See [LICENSE](./LICENSE) for details.
