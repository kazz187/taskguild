# MCP TaskGuild Server

TaskGuildのタスク管理機能を外部から利用するためのMCP（Model Context Protocol）サーバーです。

## 概要

このMCPサーバーは、TaskGuildのBuf Connect APIを通じてタスクの作成、読取り、更新、クローズ操作を提供します。エージェントがタスク終了時にTaskGuildと通信するために使用されます。

## 機能

### 利用可能なツール

1. **taskguild_list_tasks** - タスク一覧の取得
   - ステータスやタイプによるフィルタリング
   - ページネーション対応

2. **taskguild_get_task** - 特定のタスク詳細取得
   - タスクIDによる単一タスクの詳細情報取得

3. **taskguild_create_task** - 新しいタスクの作成
   - タイトル、説明、タイプ、メタデータの設定

4. **taskguild_update_task** - 既存タスクの更新
   - ステータス変更、説明更新、メタデータ編集

5. **taskguild_close_task** - タスクのクローズ
   - 完了理由の記録

## 設定

環境変数で設定を行います：

- `MCPTG_TASKGUILD_ADDR`: TaskGuild Connect APIサーバーのURL（デフォルト: `http://localhost:8080`）

## 使用方法

### 直接実行

```bash
# 環境変数設定
export MCPTG_TASKGUILD_ADDR=http://localhost:8080

# 実行
./mcp-taskguild
```

### Claude Code設定

Claude Codeの設定ファイル（通常 `~/.claude/config.json`）にMCPサーバーを追加：

```json
{
  "mcpServers": {
    "taskguild": {
      "command": "/path/to/mcp-taskguild",
      "env": {
        "MCPTG_TASKGUILD_ADDR": "http://localhost:8080"
      }
    }
  }
}
```

## 開発

### ビルド

```bash
go build -o mcp-taskguild .
```

### 依存関係更新

```bash
go mod tidy
```

## アーキテクチャ

```
┌─────────────────┐    MCP      ┌─────────────────┐
│                 │◄───────────►│                 │
│  Claude Code    │             │  mcp-taskguild  │
│   (Agent)       │             │                 │
└─────────────────┘             └─────────────────┘
                                          │
                                          │ Connect API
                                          │ (HTTP/2)
                                          ▼
                                ┌─────────────────┐
                                │                 │
                                │   TaskGuild     │
                                │    Server       │
                                │                 │
                                └─────────────────┘
```

