package project

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/internal/agent"
	"github.com/kazz187/taskguild/internal/skill"
	"github.com/kazz187/taskguild/internal/workflow"
)

// Seeder creates default workflow, agents, and skills for a newly created project.
type Seeder struct {
	workflowRepo workflow.Repository
	agentRepo    agent.Repository
	skillRepo    skill.Repository
}

// NewSeeder creates a new Seeder.
func NewSeeder(workflowRepo workflow.Repository, agentRepo agent.Repository, skillRepo skill.Repository) *Seeder {
	return &Seeder{
		workflowRepo: workflowRepo,
		agentRepo:    agentRepo,
		skillRepo:    skillRepo,
	}
}

// Seed creates the default development workflow with architect and software-engineer
// agents, and the create-pr skill for the given project.
func (s *Seeder) Seed(ctx context.Context, projectID string) error {
	now := time.Now()

	// 1. Create agents.
	architectAgent := &agent.Agent{
		ID:             ulid.Make().String(),
		ProjectID:      projectID,
		Name:           "architect",
		Description:    "system architect",
		Prompt:         defaultArchitectPrompt,
		Tools:          []string{"Read", "Glob", "Grep", "WebSearch", "WebFetch", "Task"},
		Model:          "opus",
		PermissionMode: "plan",
		Memory:         "user",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.agentRepo.Create(ctx, architectAgent); err != nil {
		return err
	}

	swEngineerAgent := &agent.Agent{
		ID:             ulid.Make().String(),
		ProjectID:      projectID,
		Name:           "software-engineer",
		Description:    "software engineer",
		Prompt:         defaultSoftwareEngineerPrompt,
		Tools:          []string{"Read", "Write", "Edit", "Glob", "Grep", "WebSearch", "WebFetch", "Task", "NotebookEdit"},
		Model:          "opus",
		PermissionMode: "acceptEdits",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.agentRepo.Create(ctx, swEngineerAgent); err != nil {
		return err
	}

	// 2. Create create-pr skill.
	createPRSkill := &skill.Skill{
		ID:            ulid.Make().String(),
		ProjectID:     projectID,
		Name:          "create-pr",
		Description:   `Use when the user wants to create a pull request or push changes to an existing PR. Examples: "create a PR", "make a pull request", "push changes", "update the PR".`,
		Content:       defaultCreatePRSkillContent,
		UserInvocable: true,
		AllowedTools:  []string{"Bash", "Read", "Grep", "Glob"},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.skillRepo.Create(ctx, createPRSkill); err != nil {
		return err
	}

	// 3. Create sync-default-branch skill.
	syncBranchSkill := &skill.Skill{
		ID:            ulid.Make().String(),
		ProjectID:     projectID,
		Name:          "sync-default-branch",
		Description:   "Fetches the latest default branch from origin before worktree creation.",
		Content:       defaultSyncDefaultBranchSkillContent,
		UserInvocable: false,
		AllowedTools:  []string{"Bash"},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.skillRepo.Create(ctx, syncBranchSkill); err != nil {
		return err
	}

	// 4. Create development workflow referencing the created agent and skill IDs.
	hookID := ulid.Make().String()
	wf := &workflow.Workflow{
		ID:        ulid.Make().String(),
		ProjectID: projectID,
		Name:      "development",
		Statuses: []workflow.Status{
			{
				Name:                 "Draft",
				Order:                0,
				IsInitial:            true,
				TransitionsTo:        []string{"Plan", "Develop"},
				EnableAgentMDHarness: true,
			},
			{
				Name:                 "Plan",
				Order:                1,
				TransitionsTo:        []string{"Develop"},
				AgentID:              architectAgent.ID,
				EnableAgentMDHarness: true,
				PermissionMode:       "plan",
			},
			{
				Name:          "Develop",
				Order:         2,
				TransitionsTo: []string{"Review"},
				AgentID:       swEngineerAgent.ID,
				Hooks: []workflow.StatusHook{
					{
						ID:         ulid.Make().String(),
						SkillID:    syncBranchSkill.ID,
						Trigger:    workflow.HookTriggerBeforeWorktreeCreation,
						Name:       "sync-default-branch",
						ActionType: workflow.HookActionTypeSkill,
						ActionID:   syncBranchSkill.ID,
						Order:      0,
					},
					{
						ID:         hookID,
						SkillID:    createPRSkill.ID,
						Trigger:    workflow.HookTriggerAfterTaskExecution,
						Name:       "create-pr",
						ActionType: workflow.HookActionTypeSkill,
						ActionID:   createPRSkill.ID,
					},
				},
				EnableAgentMDHarness: true,
				PermissionMode:       "acceptEdits",
			},
			{
				Name:                 "Review",
				Order:                3,
				TransitionsTo:        []string{"Closed"},
				EnableAgentMDHarness: true,
			},
			{
				Name:                 "Closed",
				Order:                4,
				IsTerminal:           true,
				TransitionsTo:        []string{},
				EnableAgentMDHarness: true,
			},
		},
		DefaultUseWorktree: true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.workflowRepo.Create(ctx, wf); err != nil {
		return err
	}

	return nil
}

const defaultArchitectPrompt = `あなたはシステム設計者です。以下の手順でタスクの仕様を策定してください。

## 役割

1. **現状調査**: 既存のコードベースを Read, Glob, Grep で徹底的に調査し、タスクに関連するアーキテクチャ・既存実装を把握する
2. **仕様の明確化**: ユーザーがタスク概要欄に書いた内容の不明点・曖昧な点を洗い出し、ユーザーに質問して要件を確定させる
3. **設計の策定**: 変更対象ファイル、実装方針、影響範囲、テスト方針を含む設計をまとめる
4. **仕様の記録**: 確定した仕様を ` + "`TASK_DESCRIPTION_START`" + ` / ` + "`TASK_DESCRIPTION_END`" + ` ブロックでタスク概要に書き戻す

## 制約

- このエージェントは Plan mode で動作するため、ファイルの編集・作成・コマンド実行はできない。調査と対話に専念すること
- 仕様が確定しユーザーが承認するまで ` + "`NEXT_STATUS`" + ` を出力しないこと。早期遷移はユーザーとの対話機会を奪う
- 設計は次のステータス（Develop）の software-engineer エージェントが迷わず実装できる粒度で記述すること

## Lessons Learned

- ユーザーが明示的に承認するまで NEXT_STATUS を出力しないこと。自動遷移により対話機会が失われる。`

const defaultSoftwareEngineerPrompt = `あなたはソフトウェアエンジニアです。タスクの仕様に従いコードを実装してください。

## 役割

1. **仕様の確認**: タスク概要に記載された設計・仕様を熟読し、実装スコープを把握する
2. **実装**: 変更対象ファイルを特定し、仕様に沿ってコードを追加・修正する。既存コードのスタイル・パターンに合わせること
3. **テスト**: 既存テストが壊れていないことを確認し、必要に応じてテストを追加する

## 制約

- 仕様に記載されていない範囲の変更（リファクタリング、機能追加等）は行わないこと
- 実装完了後、` + "`NEXT_STATUS: Review`" + ` を出力して次のステータスに遷移すること`

const defaultCreatePRSkillContent = `# Create PR / Push to Existing PR

PRの作成、または既存PRへの差分pushを行うスキルです。

## 手順

### 1. 現在の状態を確認する

以下のコマンドを**並列で**実行して現在の状態を把握してください:

- ` + "`git status`" + ` — 未コミットの変更がないか確認
- ` + "`git branch --show-current`" + ` — 現在のブランチ名を取得
- ` + "`gh repo view --json defaultBranchRef --jq '.defaultBranchRef.name'`" + ` — リポジトリのデフォルトブランチ名を取得
- ` + "`git log --oneline -5`" + ` — 直近のコミット履歴を確認

以降、取得したデフォルトブランチ名を ` + "`<default-branch>`" + ` と表記します。

### 2. ブランチの確認

現在のブランチが ` + "`<default-branch>`" + ` の場合は、PRを作成できません。エラーとして報告し、処理を終了してください。

### 3. 未コミットの変更がある場合 → コミットする

` + "`git status`" + ` で未コミットの変更（unstaged / staged / untracked）がある場合は、コミットしてください。

1. 変更内容を確認する:
   ` + "```bash" + `
   git diff
   git diff --staged
   ` + "```" + `
2. 変更内容を分析し、適切なコミットメッセージを作成する:
   - 変更の性質を要約する（新機能、バグ修正、リファクタリングなど）
   - "why" を重視した簡潔な1-2文のメッセージにする
3. コミット対象のファイルを選別する:
   - ` + "`.env`" + `、クレデンシャル、シークレットを含むファイルは除外すること
   - ビルド生成物やキャッシュファイル（` + "`node_modules/`" + `, ` + "`dist/`" + `, ` + "`*.pyc`" + ` 等）は除外すること
   - 判断に迷うファイルがある場合は ` + "`.gitignore`" + ` を確認すること
4. ステージングしてコミットする:
   ` + "```bash" + `
   git add <対象ファイル...>
   git commit -m "コミットメッセージ"
   ` + "```" + `

### 4. リモートにpushする

ローカルのコミットをリモートにpushします。upstream が未設定の場合があるため、常に以下のコマンドを使用してください:

` + "```bash" + `
git push -u origin HEAD
` + "```" + `

### 5. 既存PRの状態を確認する

現在のブランチに紐づくPRがあるか確認してください:

` + "```bash" + `
gh pr view --json number,title,url,state 2>/dev/null
` + "```" + `

結果に応じて以下に分岐します:

- **PRが存在し ` + "`state`" + ` が ` + "`OPEN`" + `** → 5A へ
- **PRが存在し ` + "`state`" + ` が ` + "`MERGED`" + ` または ` + "`CLOSED`" + `** → 5B へ（新規PRを作成）
- **PRが存在しない**（コマンドがエラー） → 5B へ（新規PRを作成）

### 5A. 既存のオープンPRがある場合

ステップ4で既にpush済みなので、PRのURLを出力してください:

` + "```" + `
TASK_METADATA: pr_url=<PRのURL>
` + "```" + `

### 5B. 新規PRを作成する

以下の手順で新しいPRを作成します:

1. デフォルトブランチとの差分を確認:
   ` + "```bash" + `
   git log <default-branch>..HEAD --oneline
   ` + "```" + `

2. 差分の内容を分析し、PRのタイトルとサマリーを作成する:
   - タイトルは70文字以内で簡潔に。英語で記述すること
   - サマリーには変更の要点をbullet pointsで記述

3. PRを作成する:
   ` + "```bash" + `
   gh pr create --title "PRタイトル" --body "$(cat <<'EOF'
   ## Summary
   - 変更点1
   - 変更点2
   EOF
   )"
   ` + "```" + `

4. 作成されたPRのURLを出力する:
   ` + "```" + `
   TASK_METADATA: pr_url=<PRのURL>
   ` + "```" + `

### 禁止事項

- ` + "`git push --force`" + ` および ` + "`git push --force-with-lease`" + ` は絶対に行わないこと
- コミットメッセージやPRの body に機密情報を含めないこと`

const defaultSyncDefaultBranchSkillContent = `# Sync Default Branch

worktree 作成前にリモートの最新デフォルトブランチを取得するスキルです。

## 手順

### 1. デフォルトブランチ名を取得する

` + "```bash" + `
gh repo view --json defaultBranchRef --jq '.defaultBranchRef.name'
` + "```" + `

取得したブランチ名を ` + "`<default-branch>`" + ` と表記します。

### 2. リモートから最新を取得する

` + "```bash" + `
git fetch origin <default-branch>
` + "```" + `

### 3. ローカルブランチのポインタを更新する

ローカルの ` + "`<default-branch>`" + ` ブランチを ` + "`origin/<default-branch>`" + ` に合わせます。
working tree を変更せずにブランチポインタのみを更新します:

` + "```bash" + `
git update-ref refs/heads/<default-branch> origin/<default-branch>
` + "```" + `

## 重要事項

- ` + "`git checkout`" + ` や ` + "`git pull`" + ` は使用しないこと（メインリポジトリの working tree を変更してしまうため）
- ` + "`git update-ref`" + ` はブランチポインタのみを更新するため安全
- fetch が失敗した場合もエラーにはせず、ローカルの状態でworktreeを作成する`
