import type { AgentDefinition } from '@taskguild/proto/taskguild/v1/agent_pb.ts'
import type { SkillDefinition } from '@taskguild/proto/taskguild/v1/skill_pb.ts'
import type { ScriptDefinition } from '@taskguild/proto/taskguild/v1/script_pb.ts'
import { HookTrigger, HookActionType } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import { Plus, Trash2, Bot, ChevronUp, ChevronDown, Zap, Wrench, BookOpen } from 'lucide-react'
import { Button, Input, Select, Checkbox, Badge } from '../atoms/index.ts'
import { FormField, Card } from '../molecules/index.ts'
import { AVAILABLE_TOOLS, MODEL_OPTIONS } from '@/lib/constants.ts'
import type { StatusDraft, HookDraft, AgentConfigDraft } from './WorkflowFormTypes.ts'

export function StatusCard({ status: s, index, statuses, agents, skills, scripts, agentConfigs, onMoveStatus, onRemoveStatus, onUpdateStatus, onToggleTransition, onAddHook, onRemoveHook, onMoveHook, onUpdateHook }: {
  status: StatusDraft
  index: number
  statuses: StatusDraft[]
  agents: AgentDefinition[]
  skills: SkillDefinition[]
  scripts: ScriptDefinition[]
  agentConfigs: AgentConfigDraft[]
  onMoveStatus: (index: number, direction: -1 | 1) => void
  onRemoveStatus: (key: string) => void
  onUpdateStatus: (key: string, patch: Partial<StatusDraft>) => void
  onToggleTransition: (fromKey: string, toKey: string) => void
  onAddHook: (statusKey: string) => void
  onRemoveHook: (statusKey: string, hookKey: string) => void
  onMoveHook: (statusKey: string, hookIndex: number, direction: -1 | 1) => void
  onUpdateHook: (statusKey: string, hookKey: string, patch: Partial<HookDraft>) => void
}) {
  const selectedAgent = agents.find(a => a.id === s.agentId)
  const legacyAgent = agentConfigs.find(a => a.statusKey === s.key)

  return (
    <Card
      variant="default"
      className="rounded-lg p-3 md:p-4"
    >
      <div className="flex items-center gap-2 md:gap-3 mb-3">
        <div className="flex flex-col -my-1">
          <Button
            type="button"
            variant="ghost"
            size="xs"
            iconOnly
            icon={<ChevronUp className="w-4 h-4" />}
            onClick={() => onMoveStatus(index, -1)}
            disabled={index === 0}
            className="!p-0 !rounded-none"
          />
          <Button
            type="button"
            variant="ghost"
            size="xs"
            iconOnly
            icon={<ChevronDown className="w-4 h-4" />}
            onClick={() => onMoveStatus(index, 1)}
            disabled={index === statuses.length - 1}
            className="!p-0 !rounded-none"
          />
        </div>
        <Input
          type="text"
          required
          pattern="[a-zA-Z0-9]+"
          value={s.name}
          onChange={(e) => {
            const v = e.target.value.replace(/[^a-zA-Z0-9]/g, '')
            onUpdateStatus(s.key, { name: v })
          }}
          inputSize="sm"
          className="flex-1 min-w-0 rounded"
          placeholder="Status name (alphanumeric)"
        />
        <div className="flex items-center gap-2 shrink-0">
          <label className="flex items-center gap-1 text-xs text-gray-400 cursor-pointer">
            <Checkbox
              checked={s.isInitial}
              onChange={(e) => onUpdateStatus(s.key, { isInitial: e.target.checked })}
            />
            <span className="hidden sm:inline">Initial</span>
            <span className="sm:hidden">I</span>
          </label>
          <label className="flex items-center gap-1 text-xs text-gray-400 cursor-pointer">
            <Checkbox
              checked={s.isTerminal}
              onChange={(e) => onUpdateStatus(s.key, { isTerminal: e.target.checked })}
            />
            <span className="hidden sm:inline">Terminal</span>
            <span className="sm:hidden">T</span>
          </label>
          {statuses.length > 1 && (
            <Button
              type="button"
              variant="ghost"
              size="xs"
              iconOnly
              icon={<Trash2 className="w-4 h-4" />}
              onClick={() => onRemoveStatus(s.key)}
              className="text-gray-600 hover:text-red-400"
            />
          )}
        </div>
      </div>

      {/* Transitions */}
      <div className="mb-3">
        <span className="text-xs text-gray-500 mr-2 block sm:inline mb-1 sm:mb-0">Transitions to:</span>
        <div className="inline-flex gap-1 flex-wrap">
          {statuses
            .filter((other) => other.key !== s.key)
            .map((other) => {
              const active = s.transitionsTo.includes(other.key)
              return (
                <button
                  key={other.key}
                  type="button"
                  onClick={() => onToggleTransition(s.key, other.key)}
                  className="transition-colors"
                >
                  <Badge
                    color={active ? 'cyan' : 'gray'}
                    variant="outline"
                    size="xs"
                    className={active ? '' : 'hover:text-gray-300'}
                  >
                    {other.name || '(unnamed)'}
                  </Badge>
                </button>
              )
            })}
        </div>
      </div>

      {/* Agent Assignment (dropdown) */}
      {!s.isTerminal && (
        <Card variant="nested" className="p-2.5 md:p-3 mt-2">
          <div className="flex items-center gap-2 mb-2">
            <Bot className="w-3.5 h-3.5 text-cyan-400" />
            <span className="text-xs text-cyan-400">Assigned Agent</span>
          </div>
          <Select
            value={s.agentId}
            onChange={(e) => onUpdateStatus(s.key, { agentId: e.target.value })}
            selectSize="xs"
            className="rounded"
          >
            <option value="">No agent (manual status)</option>
            {agents.map(agent => (
              <option key={agent.id} value={agent.id}>
                {agent.name} — {agent.description}
              </option>
            ))}
          </Select>
          {selectedAgent && (
            <div className="mt-2 text-[11px] text-gray-500">
              <span className="text-gray-400">Model:</span> {selectedAgent.model || 'inherit'}
              {selectedAgent.tools.length > 0 && (
                <>
                  {' · '}
                  <span className="text-gray-400">Tools:</span> {selectedAgent.tools.join(', ')}
                </>
              )}
            </div>
          )}
          {!s.agentId && legacyAgent && (
            <div className="mt-2 text-[11px] text-amber-500/70">
              Legacy agent config: {legacyAgent.name} (will be preserved)
            </div>
          )}
          {agents.length === 0 && (
            <p className="mt-2 text-[11px] text-gray-600">
              No agents defined yet. Create agents in the Agents page first.
            </p>
          )}
        </Card>
      )}

      {/* Execution Configuration */}
      {!s.isTerminal && (
        <Card variant="nested" className="p-2.5 md:p-3 mt-2">
          <div className="flex items-center gap-2 mb-2">
            <Wrench className="w-3.5 h-3.5 text-emerald-400" />
            <span className="text-xs text-emerald-400">Execution Config</span>
          </div>

          {/* Model */}
          <FormField label="Model" labelSize="xs" className="mb-2">
            <Select
              value={s.model}
              onChange={(e) => onUpdateStatus(s.key, { model: e.target.value })}
              selectSize="xs"
              className="rounded"
            >
              {MODEL_OPTIONS.map(opt => (
                <option key={opt.value} value={opt.value}>{opt.label}</option>
              ))}
            </Select>
          </FormField>

          {/* Allowed Tools */}
          <FormField label="Allowed Tools" labelSize="xs" className="mb-2">
            <div className="flex gap-1 flex-wrap">
              {AVAILABLE_TOOLS.map(tool => {
                const active = s.tools.includes(tool)
                return (
                  <button
                    key={tool}
                    type="button"
                    onClick={() => {
                      const next = active
                        ? s.tools.filter(t => t !== tool)
                        : [...s.tools, tool]
                      onUpdateStatus(s.key, { tools: next })
                    }}
                    className="transition-colors"
                  >
                    <Badge
                      color={active ? 'green' : 'gray'}
                      variant="outline"
                      size="xs"
                      className={active ? '' : 'hover:text-gray-300'}
                    >
                      {tool}
                    </Badge>
                  </button>
                )
              })}
            </div>
          </FormField>

          {/* Disallowed Tools */}
          <FormField label="Disallowed Tools" labelSize="xs">
            <div className="flex gap-1 flex-wrap">
              {AVAILABLE_TOOLS.map(tool => {
                const active = s.disallowedTools.includes(tool)
                return (
                  <button
                    key={tool}
                    type="button"
                    onClick={() => {
                      const next = active
                        ? s.disallowedTools.filter(t => t !== tool)
                        : [...s.disallowedTools, tool]
                      onUpdateStatus(s.key, { disallowedTools: next })
                    }}
                    className="transition-colors"
                  >
                    <Badge
                      color={active ? 'red' : 'gray'}
                      variant="outline"
                      size="xs"
                      className={active ? '' : 'hover:text-gray-300'}
                    >
                      {tool}
                    </Badge>
                  </button>
                )
              })}
            </div>
          </FormField>
        </Card>
      )}

      {/* Skills */}
      {!s.isTerminal && (
        <Card variant="nested" className="p-2.5 md:p-3 mt-2">
          <div className="flex items-center gap-2 mb-2">
            <BookOpen className="w-3.5 h-3.5 text-violet-400" />
            <span className="text-xs text-violet-400">Skills</span>
          </div>
          <div className="flex gap-1 flex-wrap">
            {skills.map(sk => {
              const active = s.skillIds.includes(sk.id)
              return (
                <button
                  key={sk.id}
                  type="button"
                  onClick={() => {
                    const next = active
                      ? s.skillIds.filter(id => id !== sk.id)
                      : [...s.skillIds, sk.id]
                    onUpdateStatus(s.key, { skillIds: next })
                  }}
                  className="transition-colors"
                >
                  <Badge
                    color={active ? 'purple' : 'gray'}
                    variant="outline"
                    size="xs"
                    className={active ? '' : 'hover:text-gray-300'}
                  >
                    {sk.name}
                  </Badge>
                </button>
              )
            })}
          </div>
          {skills.length === 0 && (
            <p className="text-[11px] text-gray-600">No skills defined yet.</p>
          )}
        </Card>
      )}

      {/* Permission Mode */}
      {!s.isTerminal && (
        <div className="mt-2 px-1">
          <FormField label="Permission Mode" labelSize="xs">
            <Select
              value={s.permissionMode}
              onChange={(e) => onUpdateStatus(s.key, { permissionMode: e.target.value })}
              selectSize="xs"
              className="rounded"
            >
              <option value="">Default (ask for permission)</option>
              <option value="acceptEdits">Accept Edits (auto-approve file changes)</option>
              <option value="plan">Plan (no tool execution, plan only)</option>
              <option value="bypassPermissions">Bypass Permissions (auto-approve all)</option>
            </Select>
          </FormField>
        </div>
      )}

      {/* Inherit Session From */}
      {!s.isTerminal && (
        <div className="mt-2 px-1">
          <FormField label="Inherit Session From" labelSize="xs">
            <Select
              value={s.inheritSessionFrom}
              onChange={(e) => onUpdateStatus(s.key, { inheritSessionFrom: e.target.value })}
              selectSize="xs"
              className="rounded"
            >
              <option value="">None (fresh session)</option>
              {statuses
                .filter((other) => other.key !== s.key && statuses.indexOf(other) < index)
                .map((other) => (
                  <option key={other.key} value={other.name}>
                    {other.name || '(unnamed)'}
                  </option>
                ))}
            </Select>
          </FormField>
        </div>
      )}

      {/* Skill Harness Toggle */}
      {!s.isTerminal && (
        <div className="mt-2 px-1">
          <label className="flex items-center gap-2 text-xs text-gray-400 cursor-pointer">
            <Checkbox
              checked={s.enableSkillHarness}
              onChange={(e) => onUpdateStatus(s.key, {
                enableSkillHarness: e.target.checked,
                skillHarnessExplicitlyDisabled: !e.target.checked,
              })}
            />
            <span>Skill Harness</span>
            <span className="text-[10px] text-gray-600">(append failure patterns to skill files on status exit)</span>
          </label>
        </div>
      )}

      {/* Agent Markdown Harness Toggle (deprecated) */}
      {!s.isTerminal && s.agentId && (
        <div className="mt-2 px-1">
          <label className="flex items-center gap-2 text-xs text-gray-500 cursor-pointer">
            <Checkbox
              checked={s.enableAgentMdHarness}
              onChange={(e) => onUpdateStatus(s.key, {
                enableAgentMdHarness: e.target.checked,
                agentMdHarnessExplicitlyDisabled: !e.target.checked,
              })}
            />
            <span>Agent MD Harness</span>
            <span className="text-[10px] text-gray-600">(deprecated)</span>
          </label>
        </div>
      )}

      {/* Hooks */}
      {!s.isTerminal && (
        <Card variant="nested" className="p-2.5 md:p-3 mt-2">
          <div className="flex items-center justify-between mb-2">
            <div className="flex items-center gap-2">
              <Zap className="w-3.5 h-3.5 text-amber-400" />
              <span className="text-xs text-amber-400">Hooks</span>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="xs"
              icon={<Plus className="w-3 h-3" />}
              onClick={() => onAddHook(s.key)}
              className="text-[11px] text-amber-400 hover:text-amber-300"
            >
              Add Hook
            </Button>
          </div>
          {s.hooks.length === 0 && (
            <p className="text-[11px] text-gray-600">No hooks configured.</p>
          )}
          <div className="space-y-2">
            {s.hooks.map((h, hi) => (
              <HookRow
                key={h.key}
                hook={h}
                hookIndex={hi}
                statusKey={s.key}
                totalHooks={s.hooks.length}
                skills={skills}
                scripts={scripts}
                onMoveHook={onMoveHook}
                onUpdateHook={onUpdateHook}
                onRemoveHook={onRemoveHook}
              />
            ))}
          </div>
          {skills.length === 0 && scripts.length === 0 && s.hooks.length > 0 && (
            <p className="mt-2 text-[11px] text-gray-600">
              No skills or scripts defined yet. Create them in the Skills or Scripts page first.
            </p>
          )}
        </Card>
      )}
    </Card>
  )
}

function HookRow({ hook: h, hookIndex: hi, statusKey, totalHooks, skills, scripts, onMoveHook, onUpdateHook, onRemoveHook }: {
  hook: HookDraft
  hookIndex: number
  statusKey: string
  totalHooks: number
  skills: SkillDefinition[]
  scripts: ScriptDefinition[]
  onMoveHook: (statusKey: string, hookIndex: number, direction: -1 | 1) => void
  onUpdateHook: (statusKey: string, hookKey: string, patch: Partial<HookDraft>) => void
  onRemoveHook: (statusKey: string, hookKey: string) => void
}) {
  return (
    <div className="flex items-center gap-2 bg-slate-900/50 rounded p-2">
      <div className="flex flex-col -my-1">
        <Button
          type="button"
          variant="ghost"
          size="xs"
          iconOnly
          icon={<ChevronUp className="w-3 h-3" />}
          onClick={() => onMoveHook(statusKey, hi, -1)}
          disabled={hi === 0}
          className="!p-0 !rounded-none"
        />
        <Button
          type="button"
          variant="ghost"
          size="xs"
          iconOnly
          icon={<ChevronDown className="w-3 h-3" />}
          onClick={() => onMoveHook(statusKey, hi, 1)}
          disabled={hi === totalHooks - 1}
          className="!p-0 !rounded-none"
        />
      </div>
      <Select
        value={h.trigger}
        onChange={(e) =>
          onUpdateHook(statusKey, h.key, { trigger: Number(e.target.value) as HookTrigger })
        }
        selectSize="xs"
        className="flex-[2] min-w-0 rounded text-[11px]"
      >
        <option value={HookTrigger.BEFORE_TASK_EXECUTION}>Before Task</option>
        <option value={HookTrigger.AFTER_TASK_EXECUTION}>After Task</option>
        <option value={HookTrigger.AFTER_WORKTREE_CREATION}>After Worktree</option>
        <option value={HookTrigger.BEFORE_WORKTREE_CREATION}>Before Worktree</option>
      </Select>
      <Select
        value={h.actionType}
        onChange={(e) => {
          const newType = Number(e.target.value) as HookActionType
          onUpdateHook(statusKey, h.key, {
            actionType: newType,
            actionId: '',
            skillId: '',
            name: '',
          })
        }}
        selectSize="xs"
        className="flex-[1] min-w-0 rounded text-[11px]"
      >
        <option value={HookActionType.SKILL}>Skill</option>
        <option value={HookActionType.SCRIPT}>Script</option>
      </Select>
      {h.actionType === HookActionType.SCRIPT ? (
        <Select
          value={h.actionId}
          onChange={(e) => {
            const sc = scripts.find((sc) => sc.id === e.target.value)
            onUpdateHook(statusKey, h.key, {
              actionId: e.target.value,
              skillId: '',
              name: sc?.name ?? h.name,
            })
          }}
          selectSize="xs"
          className="flex-[3] min-w-0 rounded text-[11px]"
        >
          <option value="">Select script...</option>
          {scripts.map((sc) => (
            <option key={sc.id} value={sc.id}>
              {sc.name}
            </option>
          ))}
        </Select>
      ) : (
        <Select
          value={h.actionId}
          onChange={(e) => {
            const sk = skills.find((sk) => sk.id === e.target.value)
            onUpdateHook(statusKey, h.key, {
              actionId: e.target.value,
              skillId: e.target.value,
              name: sk?.name ?? h.name,
            })
          }}
          selectSize="xs"
          className="flex-[3] min-w-0 rounded text-[11px]"
        >
          <option value="">Select skill...</option>
          {skills.map((sk) => (
            <option key={sk.id} value={sk.id}>
              {sk.name}
            </option>
          ))}
        </Select>
      )}
      <Button
        type="button"
        variant="ghost"
        size="xs"
        iconOnly
        icon={<Trash2 className="w-3.5 h-3.5" />}
        onClick={() => onRemoveHook(statusKey, h.key)}
        className="text-gray-600 hover:text-red-400 shrink-0"
      />
    </div>
  )
}
