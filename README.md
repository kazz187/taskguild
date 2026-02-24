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

これにより、`https://taskguild-api.example.com` 経由で Backend Server に HTTPS でアクセスできるようになります。

> **Tips**: `cloudflared` をsystemd サービスとして登録すれば、バックグラウンドで常時稼働させることができます。
> ```bash
> cloudflared service install
> ```

### 2. Frontend の利用

Frontend は **https://taskguild.cc** で公開されています。

初回アクセス時に以下を設定してください：

- **API Base URL**: Backend Server の URL（例: `https://taskguild-api.example.com`）
- **API Key**: Backend Server に設定した `TASKGUILD_API_KEY`

設定はブラウザの localStorage に保存されます。

### 3. Agent Manager のセットアップ

Agent Manager は **プロジェクトのリポジトリ内で起動** します。ローカルマシンで実行可能です。

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

Agent Manager は起動すると Backend に Subscribe し、タスク配信を待ち受けます。ログは `agent-manager.log` に出力されます。

---

## Core Concepts

### Project

Project は TaskGuild における管理単位です。リポジトリと 1:1 で対応します。

Frontend から Project を作成する際に `name` を設定します。Agent Manager は起動時の作業ディレクトリ名（または `TASKGUILD_PROJECT_NAME`）で Project とマッチングされます。

### Agent

Agent は、タスクを実行する AI エージェントの定義です。システムプロンプト、使用可能なツール、モデルなどを設定します。

#### Agent の作成方法

**方法 1: Frontend (Web UI) から作成**

Frontend の Agent 管理画面から直接作成・編集できます。

**方法 2: `.claude/agents/*.md` ファイルから同期**

プロジェクトのリポジトリ内に `.claude/agents/` ディレクトリを作成し、Markdown ファイルで Agent を定義できます。Agent Manager 起動時に自動的に Backend に同期されます。

```
your-project/
├── .claude/
│   └── agents/
│       ├── software-engineer.md
│       ├── code-reviewer.md
│       └── test-runner.md
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
---

あなたはソフトウェアエンジニアです。タスクの内容をよく読み実装してください。
```

#### フロントマターのフィールド

| フィールド | 型 | 説明 |
|-----------|-----|------|
| `name` | string | Agent の識別名（ファイル名から自動設定も可能） |
| `description` | string | Agent の説明。いつ・なぜこの Agent を使うか |
| `tools` | string / list | 使用可能なツール（カンマ区切り or YAML リスト） |
| `disallowedTools` | string / list | 使用禁止のツール |
| `model` | string | 使用モデル: `sonnet` / `opus` / `haiku` / `inherit` |
| `permissionMode` | string | 権限モード: `default` / `acceptEdits` / `dontAsk` / `bypassPermissions` / `plan` |
| `skills` | list | プリロードするスキル |
| `memory` | string | メモリスコープ: `user` / `project` / `local` |

#### tools のリスト形式

```yaml
# インライン形式
tools: Read, Write, Glob, Grep, Bash

# YAML リスト形式
tools:
  - Read
  - Write
  - Glob
  - Grep
  - Bash
```

### Workflow

Workflow はタスクが辿るステータスの流れを定義します。各ステータスに Agent をバインドすることで、タスクのステータス遷移に応じた自動実行が可能になります。

#### Workflow の構成要素

```
Workflow
├── name: "Development Flow"
├── description: "開発タスク用のワークフロー"
└── statuses:
    ├── Draft        (is_initial: true)
    │   └── transitions_to: [Develop]
    ├── Develop      (agent_id: "software-engineer")
    │   └── transitions_to: [Review]
    ├── Review       (agent_id: "code-reviewer")
    │   └── transitions_to: [Develop, Done]
    └── Done         (is_terminal: true)
        └── transitions_to: []
```

#### Status のフィールド

| フィールド | 説明 |
|-----------|------|
| `id` | ステータスの一意な ID |
| `name` | ステータス名 |
| `order` | 表示順序 |
| `is_initial` | `true` の場合、タスク作成時のデフォルトステータス（1つのみ） |
| `is_terminal` | `true` の場合、タスクの終了状態。以降の遷移は不可 |
| `transitions_to` | 遷移可能な次のステータス ID のリスト |
| `agent_id` | このステータスにバインドする Agent の ID |

#### `transitions_to` について

`transitions_to` は、そのステータスから遷移可能な次のステータスを制限します。

```
Draft ──transitions_to──▶ Develop
                              │
                    transitions_to
                              │
                              ▼
                           Review
                           │    │
              transitions_to    transitions_to
                   │                  │
                   ▼                  ▼
                Develop             Done
```

- タスクのステータス変更時に、`transitions_to` に含まれるステータスのみ遷移が許可されます
- 許可されていない遷移を行おうとするとエラーになります
- Agent がタスク完了時に次のステータスを選択する場合も、この制約に従います

#### Workflow の作成例

Frontend から Workflow を作成する際、以下のように設計します：

1. **ステータスを定義**: タスクが辿る状態を洗い出す
2. **遷移ルールを設定**: 各ステータスから遷移可能な次のステータスを `transitions_to` で指定
3. **Agent をバインド**: 自動実行したいステータスに Agent を紐付ける
4. **初期・終了ステータスを設定**: `is_initial` と `is_terminal` を適切に設定

### Task

Task はワークフロー上で実行される作業単位です。

#### Task の作成と実行

1. **Task を作成**: Frontend から Task を作成すると、Workflow の `is_initial` ステータスに配置されます
2. **ステータスを変更**: Agent がバインドされたステータスに遷移すると、自動的に Agent がタスクを実行します
3. **Agent が実行**: Agent Manager がタスクを受信し、Claude Agent がタスク内容に基づいて作業を行います
4. **次のステータスへ遷移**: Agent はタスク完了時に `NEXT_STATUS` を出力し、次のステータスに遷移します

#### Task のライフサイクル

```
                          ┌─── Agent がバインドされていない場合
                          │    → 手動でステータスを変更するまで待機
                          │
Task 作成                 │
    │                     │
    ▼                     │
[Initial Status] ────▶ [Status with Agent] ────▶ [Next Status] ────▶ [Terminal]
                          │                          ▲
                          │  1. Orchestrator が検知   │
                          │  2. Agent Manager に配信  │
                          │  3. Agent がタスクを実行  │
                          │  4. NEXT_STATUS を出力 ───┘
                          │
                          └─── Agent がバインドされている場合
                               → 自動的に Agent が起動
```

#### Task のフィールド

| フィールド | 説明 |
|-----------|------|
| `title` | タスクのタイトル |
| `description` | タスクの詳細説明（Agent への指示内容） |
| `workflow_id` | 使用する Workflow の ID |
| `status_id` | 現在のステータス ID |
| `metadata` | カスタムメタデータ（key-value） |
| `use_worktree` | `true` の場合、Agent が git worktree を使用して作業 |

#### Agent によるステータス遷移

Agent はタスク実行後、出力の最終行に以下の形式でステータス遷移を指示します：

```
NEXT_STATUS: <status_id>
```

Agent には実行時に以下のメタデータが渡されます：

- `_task_title`: タスクのタイトル
- `_task_description`: タスクの詳細
- `_current_status_name`: 現在のステータス名
- `_available_transitions`: 遷移可能なステータスの JSON 配列
- `_sub_agents`: プロジェクト内の他の Agent 情報

遷移可能なステータスが 1 つだけの場合、Agent が `NEXT_STATUS` を指定しなくても自動的にそのステータスに遷移します。

### Interaction

Agent がタスク実行中にユーザーの入力や承認を必要とする場合、Interaction を作成してユーザーに問い合わせることができます。

Frontend の Interaction 画面でリアルタイムに確認・応答が可能です。

---

## Example: 開発ワークフローのセットアップ

### Step 1: Agent を定義

`.claude/agents/developer.md`:
```markdown
---
name: developer
description: 開発タスクを実装するエージェント
tools: Read, Write, Glob, Grep, Bash, WebSearch, Task
model: opus
permissionMode: acceptEdits
---

あなたはソフトウェアエンジニアです。タスクの内容をよく読み実装してください。
```

`.claude/agents/reviewer.md`:
```markdown
---
name: reviewer
description: コードレビューを行うエージェント
tools: Read, Glob, Grep, Bash
model: sonnet
permissionMode: plan
---

あなたはコードレビュアーです。変更内容を確認し、問題点や改善点を指摘してください。
```

### Step 2: Workflow を作成

Frontend から以下の Workflow を作成します：

| Status | is_initial | is_terminal | transitions_to | agent |
|--------|-----------|-------------|----------------|-------|
| Draft | Yes | No | Develop | - |
| Develop | No | No | Review | developer |
| Review | No | No | Develop, Done | reviewer |
| Done | No | Yes | - | - |

### Step 3: Agent Manager を起動

```bash
cd /path/to/your-project

TASKGUILD_API_KEY="your-api-key" \
TASKGUILD_SERVER_URL="https://taskguild-api.example.com" \
taskguild-agent
```

### Step 4: Task を作成して実行

1. Frontend で Task を作成（Draft ステータスで作成される）
2. Task のステータスを **Develop** に変更
3. → `developer` Agent が自動的にタスクを実行
4. → Agent が完了すると **Review** に自動遷移
5. → `reviewer` Agent が自動的にレビューを実行
6. → レビュー結果に応じて **Develop**（修正が必要）or **Done**（完了）に遷移

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

## License

See [LICENSE](./LICENSE) for details.
