package project

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/internal/skill"
	"github.com/kazz187/taskguild/internal/workflow"
	"github.com/kazz187/taskguild/pkg/cerr"
)

// Default role-skill content, embedded from seed_skills/*.md. These files
// are the harness-agnostic body of the role definitions under
// .claude/agents/ — the YAML frontmatter (tools, model, permissionMode,
// etc.) is intentionally excluded so that the harness configuration stays
// at the workflow Status level, not on the skill itself.
var (
	//go:embed seed_skills/architect.md
	defaultArchitectSkillContent string

	//go:embed seed_skills/senior-engineer.md
	defaultSeniorEngineerSkillContent string

	//go:embed seed_skills/software-engineer.md
	defaultSoftwareEngineerSkillContent string
)

// Seeder creates default workflow and skills for a newly created project.
type Seeder struct {
	workflowRepo workflow.Repository
	skillRepo    skill.Repository
}

// NewSeeder creates a new Seeder.
func NewSeeder(workflowRepo workflow.Repository, skillRepo skill.Repository) *Seeder {
	return &Seeder{
		workflowRepo: workflowRepo,
		skillRepo:    skillRepo,
	}
}

// buildDefaultSkillDefinitions returns fresh copies of the default skill
// definitions used by both Seed (create-only) and UpsertSkills (upsert).
// Returned skills have Name/Description/Content/UserInvocable/AllowedTools
// populated, but ID/ProjectID/CreatedAt/UpdatedAt are left zero — callers
// are responsible for filling them in.
func buildDefaultSkillDefinitions() []*skill.Skill {
	return []*skill.Skill{
		{
			Name:          "project-rules",
			Description:   "プロジェクトのビルド・コマンド・ディレクトリ規約",
			UserInvocable: false,
		},
		{
			Name:          "codebase-map",
			Description:   "プロジェクトのアーキテクチャ知識・変更伝播パス",
			UserInvocable: false,
		},
		{
			Name:          "go-guards",
			Description:   "Go コードを書く際の技術規約・罠",
			UserInvocable: false,
		},
		{
			Name:          "frontend-guards",
			Description:   "TypeScript/React コードの規約・罠",
			UserInvocable: false,
		},
		{
			Name:          "architect",
			Description:   "システム設計者。タスクの仕様を策定する。",
			Content:       defaultArchitectSkillContent,
			UserInvocable: false,
		},
		{
			Name:          "software-engineer",
			Description:   "ソフトウェアエンジニア。設計に従いコードを実装する。",
			Content:       defaultSoftwareEngineerSkillContent,
			UserInvocable: false,
		},
		{
			Name:          "senior-engineer",
			Description:   "シニアエンジニア。コードレビュー・品質保証を行う。",
			Content:       defaultSeniorEngineerSkillContent,
			UserInvocable: false,
		},
		{
			Name:          "create-pr",
			Description:   `Slash command that commits any pending changes, pushes the branch, and opens or updates a PR. Only invoke when the user explicitly asks via "/create-pr"; this skill is also wired as an after_task_execution hook, so tasks should not call it on their own.`,
			Content:       defaultCreatePRSkillContent,
			UserInvocable: true,
			AllowedTools:  []string{"Bash", "Read", "Grep", "Glob"},
		},
		{
			Name:          "sync-default-branch",
			Description:   "Fetches the latest default branch from origin before worktree creation.",
			Content:       defaultSyncDefaultBranchSkillContent,
			UserInvocable: false,
			AllowedTools:  []string{"Bash"},
		},
	}
}

// Seed creates the default development workflow with role skills, guard
// skills, and hook skills for a newly created project.
func (s *Seeder) Seed(ctx context.Context, projectID string) error {
	now := time.Now()

	defs := buildDefaultSkillDefinitions()

	skillsByName := make(map[string]*skill.Skill, len(defs))
	for _, def := range defs {
		def.ID = ulid.Make().String()
		def.ProjectID = projectID
		def.CreatedAt = now

		def.UpdatedAt = now
		err := s.skillRepo.Create(ctx, def)
		if err != nil {
			return err
		}

		skillsByName[def.Name] = def
	}

	architectSkill := skillsByName["architect"]
	softwareEngineerSkill := skillsByName["software-engineer"]
	seniorEngineerSkill := skillsByName["senior-engineer"]
	createPRSkill := skillsByName["create-pr"]
	syncBranchSkill := skillsByName["sync-default-branch"]

	// Create development workflow referencing the created skill IDs.
	hookID := ulid.Make().String()

	wf := &workflow.Workflow{
		ID:        ulid.Make().String(),
		ProjectID: projectID,
		Name:      "development",
		Statuses: []workflow.Status{
			{
				Name:               "Draft",
				Order:              0,
				IsInitial:          true,
				TransitionsTo:      []string{"Plan", "Develop"},
				EnableSkillHarness: true,
			},
			{
				Name:               "Plan",
				Order:              1,
				TransitionsTo:      []string{"Develop"},
				PermissionMode:     "plan",
				Model:              "opus",
				Effort:             "high",
				SkillIDs:           []string{architectSkill.ID},
				EnableSkillHarness: true,
			},
			{
				Name:               "Develop",
				Order:              2,
				TransitionsTo:      []string{"Review"},
				InheritSessionFrom: "Plan",
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
				PermissionMode:     "acceptEdits",
				Model:              "opus",
				Effort:             "max",
				SkillIDs:           []string{softwareEngineerSkill.ID},
				EnableSkillHarness: true,
			},
			{
				Name:               "Review",
				Order:              3,
				TransitionsTo:      []string{"Closed"},
				PermissionMode:     "acceptEdits",
				Model:              "opus",
				Effort:             "high",
				SkillIDs:           []string{seniorEngineerSkill.ID},
				EnableSkillHarness: true,
			},
			{
				Name:          "Closed",
				Order:         4,
				IsTerminal:    true,
				TransitionsTo: []string{},
			},
		},
		DefaultUseWorktree: true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	err := s.workflowRepo.Create(ctx, wf)
	if err != nil {
		return err
	}

	return nil
}

// UpsertSkills upserts the default skill definitions into the given project.
// For each definition, if a skill with the same name already exists in the
// project, its description/content/tools/etc. are updated while preserving
// the existing ID and CreatedAt. Otherwise a new skill is created.
//
// UpsertSkills never deletes skills — any skills in the project that are
// not in the default definitions are left untouched. The workflow is also
// left alone, so calling this against an existing project is safe.
func (s *Seeder) UpsertSkills(ctx context.Context, projectID string) error {
	now := time.Now()
	defs := buildDefaultSkillDefinitions()

	for _, def := range defs {
		existing, err := s.skillRepo.FindByName(ctx, projectID, def.Name)
		if err != nil {
			var cerrErr *cerr.Error
			if !errors.As(err, &cerrErr) || cerrErr.Code != cerr.NotFound {
				return err
			}

			def.ID = ulid.Make().String()
			def.ProjectID = projectID
			def.CreatedAt = now

			def.UpdatedAt = now
			err := s.skillRepo.Create(ctx, def)
			if err != nil {
				return err
			}

			continue
		}

		existing.Description = def.Description
		existing.Content = def.Content
		existing.DisableModelInvocation = def.DisableModelInvocation
		existing.UserInvocable = def.UserInvocable
		existing.AllowedTools = def.AllowedTools
		existing.Model = def.Model
		existing.Context = def.Context
		existing.Agent = def.Agent
		existing.ArgumentHint = def.ArgumentHint

		existing.UpdatedAt = now
		if err := s.skillRepo.Update(ctx, existing); err != nil {
			return err
		}
	}

	return nil
}

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
