# TaskGuild - AIエージェントオーケストレーションツール

TaskGuildは、Claude Codeを中心としたAIエージェントチームを管理・調整し、高品質なソフトウェア開発を実現するためのCLIツールです。タスク管理、イベント駆動、エージェントのライフサイクル管理を通じて、複数のAIエージェントが協調して働く環境を提供します。

## 主な機能

- **カスタマイズ可能なタスク管理**: GitHub Issuesライクなタスク管理システム
- **イベント駆動アーキテクチャ**: タスク状態変更時の自動エージェント起動
- **動的エージェントスケーリング**: 必要に応じてエージェント数を自動調整
- **リアルタイムモニタリング**: TUIによるタスク実行状況の可視化
- **Git Worktree統合**: 各タスクごとの独立したworktree管理
- **Claude Code Sub Agent対応**: 名前ベースのエージェント呼び出し

## インストール

```bash
go install github.com/kazz187/taskguild/cmd/taskguild@latest
```

または、ソースからビルド：

```bash
git clone https://github.com/kazz187/taskguild.git
cd taskguild
go build -o bin/taskguild cmd/taskguild/main.go
```

## 使い方

### タスク管理

```bash
# タスクの作成
taskguild create "ユーザー認証機能の実装"

# タスク一覧
taskguild list

# タスクの状態更新
taskguild update TASK-001 --status IN_PROGRESS

# タスクの完了
taskguild close TASK-001

# タスク詳細の表示
taskguild show TASK-001
```

### エージェント管理

```bash
# エージェント一覧
taskguild agent list

# エージェントの状態確認
taskguild agent status developer

# エージェントの開始
taskguild agent start developer

# エージェントの停止
taskguild agent stop developer

# エージェントのスケーリング
taskguild agent scale developer 3
```

### ワークスペース（インタラクティブTUI）

```bash
# ワークスペースの起動
taskguild workspace

# キーボードショートカット
# Tab - ペイン間の移動
# a - 承認（Approve）
# r - 却下（Reject）
# d - 詳細表示
# f - ログフィルター
# t - タスク詳細
# q - 終了
```

## 設定ファイル

TaskGuildは`.taskguild/`ディレクトリに設定ファイルを保存します。

### ディレクトリ構造

```
.taskguild/
├── task.yaml              # タスクデータ
├── task-definition.yaml   # カスタムタスクステータス定義
├── agents/                # エージェント設定ディレクトリ（フラット構造）
│   ├── developer.yaml     # 開発者エージェント設定
│   ├── reviewer.yaml      # レビュアーエージェント設定
│   └── qa.yaml            # QAエージェント設定
└── worktrees/             # タスクごとのGit worktree
    ├── TASK-001/          # feature/auth-TASK-001
    ├── TASK-002/          # feature/api-TASK-002
    └── TASK-003/          # bugfix/login-TASK-003
```

### エージェント設定ファイル

Claude Code Sub Agent対応により、設定形式が簡素化されました。各エージェントはフラット構造で配置されます：

```yaml
# .taskguild/agents/developer.yaml
name: developer  # Sub Agent として呼び出す際の名前
type: claude-code
description: Developer for implementing features
version: "1.0"

# Agent-specific configuration
instructions: |
  ## Core Responsibilities
  - Implement features based on design documents
  - Write comprehensive unit tests
  - Follow project coding standards
  
  ## Implementation Principles
  - Write readable, maintainable code
  - Implement proper error handling
  - Follow Go best practices

triggers:
  - event: TaskStatusChanged
    condition: task.status == "DESIGNED"
scaling:
  min: 1
  max: 3
  auto: true
```

#### 設定項目

- **name**: Sub Agentの名前（必須、`@{name}`で呼び出し）
- **type**: エージェントタイプ（現在は`claude-code`のみ）
- **description**: エージェントの説明
- **version**: 設定のバージョン
- **instructions**: エージェントへの指示（Sub Agent用）
- **triggers**: エージェントが反応するイベントと条件
- **scaling**: スケーリング設定（オプション）

**注**: プロジェクトルートの`CLAUDE.md`がすべてのエージェントで共通使用されます。

### カスタムステータス定義

タスクのワークフローをカスタマイズできます：

```yaml
# .taskguild/task-definition.yaml
statuses:
  - name: CREATED
    description: タスク作成直後
    transitions: [ANALYZING, CANCELLED]
  
  - name: ANALYZING
    description: アーキテクトが分析中
    transitions: [DESIGNED, NEEDS_INFO]
  
  - name: DESIGNED
    description: 設計完了
    transitions: [IN_PROGRESS]
  
  - name: IN_PROGRESS
    description: 実装中
    transitions: [REVIEW_READY, BLOCKED]
  
  - name: REVIEW_READY
    description: レビュー待ち
    transitions: [IN_PROGRESS, QA_READY]
  
  - name: QA_READY
    description: 動作確認待ち
    transitions: [IN_PROGRESS, CLOSED]
  
  - name: CLOSED
    description: 完了
    transitions: []
```

## アーキテクチャ

### システム構成

```
┌─────────────────────────────────────────────────────────┐
│                    TaskGuild CLI                         │
├─────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐    │
│  │    Task     │  │    Event    │  │   Agent     │    │
│  │  Manager    │  │    Queue    │  │  Manager    │    │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘    │
│         │                 │                 │           │
│  ┌──────┴──────────────────┴─────────────────┴──────┐  │
│  │              State Management (In-Memory)         │  │
│  └───────────────────────┬──────────────────────────┘  │
│                          │                              │
│  ┌───────────────────────┴──────────────────────────┐  │
│  │           File Persistence (.taskguild/)          │  │
│  └──────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
                            │
                   ┌────────┴────────┐
                   │  Claude Code    │
                   │   Instances     │
                   └─────────────────┘
```

### Git Worktreeによる並行開発

各タスクは独立したgit worktreeを持ち、関連するエージェントがそのworktreeで協力して作業します：

```
/project
├── main branch
└── .taskguild/worktrees/
    ├── TASK-001/    → feature/auth-TASK-001
    │   # developerが作業
    ├── TASK-002/    → feature/api-TASK-002
    │   # reviewerが作業
    └── TASK-003/    → bugfix/login-TASK-003
        # developer、qaが共同作業
```

## 開発状況

### 実装済み機能

- ✅ タスク管理（CRUD操作、カスタムステータス定義）
- ✅ イベントシステム（watermill使用）
- ✅ エージェント設定管理（個別設定ファイル対応）
- ✅ エージェントライフサイクル管理
- ✅ 動的スケーリング機能
- ✅ 承認フロー基盤
- ✅ Claude Code Sub Agent対応

### 開発中

- 🚧 Claude Code SDK統合
- 🚧 Git worktree自動管理（go-git使用）
- 🚧 ワークスペースTUI（bubbletea使用）
- 🚧 エージェント間通信メカニズム
- 🚧 ファイルロックメカニズム

### 計画中

- 📋 Web UI
- 📋 複数プロジェクト同時管理
- 📋 パフォーマンスメトリクス
- 📋 カスタムエージェントプラグイン
- 📋 リモートエージェントサポート

## 必要要件

- Go 1.20以上
- Git
- Claude Code CLI（エージェント実行時）

## 貢献

プルリクエストを歓迎します。大きな変更を行う場合は、まずissueを作成して変更内容について議論してください。

### 開発ガイドライン

- **コーディング規約**: `gofmt`で整形、GoDocフォーマット準拠
- **テスト**: ユニットテストカバレッジ80%以上を目標
- **エラーハンドリング**: 呼び出し元に制御を返すエラー処理
- **依存管理**: 標準ライブラリ優先、必要最小限の外部ライブラリ使用

## ライセンス

MIT License

## 関連プロジェクト

- [Claude Code](https://github.com/anthropics/claude-code) - Anthropic公式CLI
- [Watermill](https://github.com/ThreeDotsLabs/watermill) - イベント駆動ライブラリ
- [Bubbletea](https://github.com/charmbracelet/bubbletea) - TUIフレームワーク
- [go-git](https://github.com/go-git/go-git) - Git操作ライブラリ