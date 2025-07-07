# TaskGuild

TaskGuildは、Claude Codeを中心としたAIエージェントチームを管理・調整し、高品質なソフトウェア開発を実現するためのCLIツールです。

## 主な機能

- **タスク管理**: GitHub Issuesライクなタスク管理システム
- **エージェント管理**: 複数のAIエージェントのライフサイクル管理
- **イベント駆動**: タスク状態変更時の自動エージェント起動
- **Git統合**: 各タスクごとの独立したworktree管理（予定）

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
taskguild update TASK-001 IN_PROGRESS

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
taskguild agent status architect-123456

# エージェントのスケーリング
taskguild agent scale developer 3
```

## 設定ファイル

TaskGuildは`.taskguild/`ディレクトリに設定ファイルを保存します。

### ディレクトリ構造

```
.taskguild/
├── task.yaml              # タスクデータ
├── task-definition.yaml   # カスタムタスクステータス定義
└── agents/               # エージェント設定ディレクトリ
    ├── architect/        # アーキテクトエージェント
    │   ├── agent.yaml   # エージェント設定
    │   └── CLAUDE.md    # エージェント仕様書
    ├── developer/       # 開発者エージェント
    │   ├── agent.yaml
    │   └── CLAUDE.md
    ├── reviewer/        # レビュアーエージェント
    │   ├── agent.yaml
    │   └── CLAUDE.md
    └── qa/              # QAエージェント
        ├── agent.yaml
        └── CLAUDE.md
```

### エージェント設定ファイル（agent.yaml）

各エージェントは独立した設定ファイル（`agent.yaml`）を持ちます：

```yaml
# .taskguild/agents/developer/agent.yaml
role: developer
type: claude-code
memory: CLAUDE.md
description: Developer for implementing features based on architectural designs
version: "1.0"
triggers:
  - event: TaskStatusChanged
    condition: task.status == "DESIGNED"
approval_required:
  - action: file_write
    pattern: '*.go'
  - action: git_commit
  - action: git_push
scaling:
  min: 1
  max: 3
  auto: true
```

#### 設定項目

- **role**: エージェントの役割（必須）
- **type**: エージェントタイプ（現在は`claude-code`のみ）
- **memory**: エージェント仕様書のパス（相対パスの場合は同じディレクトリ内）
- **description**: エージェントの説明
- **version**: 設定のバージョン
- **triggers**: エージェントが反応するイベントと条件
- **approval_required**: 承認が必要なアクション
- **scaling**: スケーリング設定（オプション）

### エージェント仕様書（CLAUDE.md）

各エージェントは、その役割と責務を定義した仕様書（`CLAUDE.md`）を持ちます。この仕様書は、Claude Codeインスタンスに読み込まれ、エージェントの振る舞いを決定します。

例: `.taskguild/agents/architect/CLAUDE.md`

```markdown
# アーキテクトエージェント仕様

## 役割
あなたはシステムアーキテクトとして、タスクを分析し最適な設計を提案します。

## 責務
- タスクの要件分析
- 技術選定の提案
- システム設計ドキュメントの作成
- 実装タスクへの分解

## 設計原則
- SOLID原則の遵守
- 過度な設計を避ける（YAGNI）
- 保守性と拡張性のバランス

## 成果物
- 設計ドキュメント（docs/design/）
- タスク分解案（.taskguild/subtasks.yaml）
- 技術選定理由書（必要に応じて）
```

## 開発状況

現在、以下の機能が実装済みです：

- ✅ タスク管理（作成、更新、一覧、詳細表示）
- ✅ イベントシステム（watermill使用）
- ✅ エージェント設定管理
- ✅ 個別エージェント設定ファイル
- 🚧 Claude Code統合（開発中）
- 🚧 Git worktree統合（開発中）
- 📋 ワークスペースTUI（計画中）

## ライセンス

MIT License

## 貢献

プルリクエストを歓迎します。大きな変更を行う場合は、まずissueを作成して変更内容について議論してください。