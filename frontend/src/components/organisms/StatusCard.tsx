import type { SkillDefinition } from '@taskguild/proto/taskguild/v1/skill_pb.ts'
import type { ScriptDefinition } from '@taskguild/proto/taskguild/v1/script_pb.ts'
import { HookTrigger, HookActionType } from '@taskguild/proto/taskguild/v1/workflow_pb.ts'
import { Plus, Trash2, ChevronUp, ChevronDown, Zap, Wrench } from 'lucide-react'
import { Button, Input, Select, Checkbox, Badge } from '../atoms/index.ts'
import { FormField, Card } from '../molecules/index.ts'
import { MODEL_OPTIONS, EFFORT_OPTIONS } from '@/lib/constants.ts'
import type { StatusDraft, HookDraft } from './WorkflowFormTypes.ts'

export function StatusCard({ status: s, index, statuses, skills, scripts, onMoveStatus, onRemoveStatus, onUpdateStatus, onToggleTransition, onAddHook, onRemoveHook, onMoveHook, onUpdateHook }: {
  status: StatusDraft
  index: number
  statuses: StatusDraft[]
  skills: SkillDefinition[]
  scripts: ScriptDefinition[]
  onMoveStatus: (index: number, direction: -1 | 1) => void
  onRemoveStatus: (key: string) => void
  onUpdateStatus: (key: string, patch: Partial<StatusDraft>) => void
  onToggleTransition: (fromKey: string, toKey: string) => void
  onAddHook: (statusKey: string) => void
  onRemoveHook: (statusKey: string, hookKey: string) => void
  onMoveHook: (statusKey: string, hookIndex: number, direction: -1 | 1) => void
  onUpdateHook: (statusKey: string, hookKey: string, patch: Partial<HookDraft>) => void
}) {
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

          {/* Effort */}
          <FormField label="Effort" labelSize="xs" className="mb-2">
            <Select
              value={s.effort}
              onChange={(e) => onUpdateStatus(s.key, { effort: e.target.value })}
              selectSize="xs"
              className="rounded"
            >
              {EFFORT_OPTIONS.map(opt => (
                <option key={opt.value} value={opt.value}>{opt.label}</option>
              ))}
            </Select>
          </FormField>

          {/* Execution Skill */}
          <FormField label="Execution Skill" labelSize="xs">
            <Select
              value={s.skillId}
              onChange={(e) => onUpdateStatus(s.key, { skillId: e.target.value })}
              selectSize="xs"
              className="rounded"
            >
              <option value="">None</option>
              {skills.map(sk => (
                <option key={sk.id} value={sk.id}>
                  {sk.name}{sk.description ? ` — ${sk.description}` : ''}
                </option>
              ))}
            </Select>
          </FormField>
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
              <option value="auto">Auto (model-classified)</option>
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
