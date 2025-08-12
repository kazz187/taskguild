package agent

import (
	"context"
	"fmt"
	"os"

	"github.com/kazz187/taskguild/internal/event"
	"github.com/kazz187/taskguild/pkg/worktree"
)

type Service struct {
	manager    *Manager
	config     *Config
	configPath string
}

func NewService(configPath string, eventBus *event.EventBus) (*Service, error) {
	config, err := LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load agent config: %w", err)
	}

	// Create worktree manager
	worktreeManager, err := worktree.NewManager(".")
	if err != nil {
		return nil, fmt.Errorf("failed to create worktree manager: %w", err)
	}

	manager := NewManager(config, eventBus, worktreeManager)

	return &Service{
		manager:    manager,
		config:     config,
		configPath: configPath,
	}, nil
}

func (s *Service) Start(ctx context.Context) error {
	// Ensure agent memory directories exist
	if err := s.ensureAgentMemoryDirs(); err != nil {
		return fmt.Errorf("failed to create agent memory directories: %w", err)
	}

	return s.manager.Start(ctx)
}

func (s *Service) ListAgents() []*Agent {
	return s.manager.ListAgents()
}

func (s *Service) GetAgent(agentID string) (*Agent, bool) {
	return s.manager.GetAgent(agentID)
}

func (s *Service) GetAgentsByRole(role string) []*Agent {
	return s.manager.GetAgentsByRole(role)
}

func (s *Service) GetAvailableAgents() []*Agent {
	return s.manager.GetAvailableAgents()
}

func (s *Service) AssignAgentToTask(agentID, taskID, worktreePath string) error {
	return s.manager.AssignAgentToTask(agentID, taskID, worktreePath)
}

func (s *Service) UnassignAgent(agentID string) error {
	return s.manager.UnassignAgent(agentID)
}

func (s *Service) RequestApproval(agentID string, action Action, target string, details map[string]interface{}) (bool, error) {
	return s.manager.RequestApproval(agentID, action, target, details)
}

func (s *Service) ScaleAgents(role string, targetCount int) error {
	return s.manager.ScaleAgents(role, targetCount)
}

func (s *Service) GetApprovalRequests() <-chan *ApprovalRequest {
	return s.manager.GetApprovalRequests()
}

func (s *Service) GetConfig() *Config {
	return s.config
}

func (s *Service) ReloadConfig() error {
	config, err := LoadConfig(s.configPath)
	if err != nil {
		return fmt.Errorf("failed to reload config: %w", err)
	}

	s.config = config
	// TODO: Update manager with new config
	return nil
}

func (s *Service) ensureAgentMemoryDirs() error {
	// Memory management is now handled by Claude Code itself
	// No need to create agent-specific memory directories
	return nil
}

func (s *Service) createDefaultMemoryFile(memoryPath, role string) error {
	var content string
	switch role {
	case "architect":
		content = `# アーキテクトエージェント仕様

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

## コミュニケーション
- 不明点は積極的にユーザーに確認
- 設計の意図を明確に文書化
- 他のエージェントが理解しやすい形式で出力
`
	case "developer":
		content = `# 開発者エージェント仕様

## 役割
あなたは開発者として、設計に基づいて高品質なコードを実装します。

## 責務
- 設計ドキュメントに基づく実装
- 単体テストの作成
- コードレビューへの対応
- バグ修正

## 実装原則
- 可読性の高いコード
- 適切なエラーハンドリング
- 十分なテストカバレッジ
- パフォーマンスの考慮

## 成果物
- 実装コード
- 単体テスト
- 実装ドキュメント

## コミュニケーション
- 実装の進捗を定期的に報告
- 疑問点は積極的に質問
- レビューフィードバックに真摯に対応
`
	case "reviewer":
		content = `# レビュアーエージェント仕様

## 役割
あなたはシニアエンジニアとして、実装されたコードを厳格にレビューします。

## レビュー観点
- コードの可読性と保守性
- パフォーマンスとセキュリティ
- テストカバレッジと品質
- 設計パターンの適切な使用
- エラーハンドリングの網羅性

## レビュープロセス
1. 設計ドキュメントとの整合性確認
2. コード品質チェック
3. 潜在的なバグの検出
4. 改善提案の作成

## 成果物
- レビューコメント（Pull Request形式）
- 改善提案リスト
- 承認/差し戻し判定

## コミュニケーション
- 建設的なフィードバック
- 具体的な改善例の提示
- 良い点も積極的に評価
`
	case "qa":
		content = `# QAエージェント仕様

## 役割
あなたはQAエンジニアとして、実装された機能を総合的にテストします。

## テスト観点
- 機能要件の充足
- 非機能要件の確認
- エラーケースの検証
- ユーザビリティの評価
- パフォーマンステスト

## テストプロセス
1. テストケースの作成
2. 機能テストの実行
3. 結合テストの実行
4. パフォーマンステスト
5. テスト結果の報告

## 成果物
- テストケース
- テスト結果レポート
- バグレポート
- 品質評価レポート

## コミュニケーション
- テスト結果の明確な報告
- 問題の再現手順を詳細に記載
- 改善提案の積極的な提示
`
	default:
		content = fmt.Sprintf(`# %s エージェント仕様

## 役割
あなたは %s として、プロジェクトの成功に貢献します。

## 責務
- 割り当てられたタスクの実行
- 高品質な成果物の作成
- チームとの円滑なコミュニケーション

## 原則
- 品質第一
- 効率的な作業
- 継続的な改善

## 成果物
- 担当領域の成果物
- 作業報告書

## コミュニケーション
- 進捗の定期的な報告
- 問題の早期エスカレーション
- 建設的なフィードバック
`, role, role)
	}

	return os.WriteFile(memoryPath, []byte(content), 0644)
}
