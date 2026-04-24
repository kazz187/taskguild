import { GitBranch, RefreshCw } from 'lucide-react'
import { Select, Checkbox } from '../atoms/index.ts'
import { FormField } from './FormField.tsx'
import { ImageUploadTextarea, type PendingImage } from './ImageUploadTextarea.tsx'
import { EFFORT_OPTIONS } from '@/lib/constants.ts'

export interface TaskFormFieldsWorktreeItem {
  name: string
  branch: string
}

export interface TaskFormFieldsProps {
  // --- Description ---
  description: string
  onDescriptionChange: (value: string) => void
  descriptionDisabled?: boolean
  /** Used by TaskDetailModal (edit mode) for immediate image upload */
  taskId?: string
  /** Used by TaskCreateModal / ChildTaskCreateModal for deferred upload */
  pendingImages?: PendingImage[]
  onPendingImagesChange?: (images: PendingImage[]) => void
  /** @default "Add description..." */
  descriptionPlaceholder?: string

  // --- Effort ---
  effort: string
  onEffortChange: (value: string) => void
  effortDisabled?: boolean

  // --- Worktree ---
  useWorktree: boolean
  onUseWorktreeChange: (checked: boolean) => void
  useWorktreeDisabled?: boolean
  selectedWorktree: string
  onSelectedWorktreeChange: (value: string) => void
  worktrees: TaskFormFieldsWorktreeItem[]
  onRequestWorktrees: () => void
  worktreeRequestPending: boolean
  /**
   * When set (non-empty), render a read-only GitBranch badge showing this value
   * instead of the worktree dropdown. Used when the task is assigned/pending
   * and the worktree cannot be edited.
   */
  lockedWorktreeValue?: string | null
}

export function TaskFormFields({
  description,
  onDescriptionChange,
  descriptionDisabled,
  taskId,
  pendingImages,
  onPendingImagesChange,
  descriptionPlaceholder = 'Add description...',
  effort,
  onEffortChange,
  effortDisabled,
  useWorktree,
  onUseWorktreeChange,
  useWorktreeDisabled,
  selectedWorktree,
  onSelectedWorktreeChange,
  worktrees,
  onRequestWorktrees,
  worktreeRequestPending,
  lockedWorktreeValue,
}: TaskFormFieldsProps) {
  return (
    <>
      <ImageUploadTextarea
        value={description}
        onChange={onDescriptionChange}
        taskId={taskId}
        pendingImages={pendingImages}
        onPendingImagesChange={onPendingImagesChange}
        textareaSize="md"
        placeholder={descriptionPlaceholder}
        disabled={descriptionDisabled}
      />

      {/* Effort */}
      <FormField label="Effort" labelSize="xs">
        <Select
          value={effort}
          onChange={(e) => onEffortChange(e.target.value)}
          selectSize="xs"
          className="rounded"
          disabled={effortDisabled}
        >
          {EFFORT_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>{opt.label}</option>
          ))}
        </Select>
      </FormField>

      <Checkbox
        label="Use Worktree (isolate changes in a git worktree)"
        checked={useWorktree}
        onChange={(e) => onUseWorktreeChange(e.target.checked)}
        disabled={useWorktreeDisabled}
      />

      {/* Worktree selection / display */}
      {lockedWorktreeValue ? (
        <div className="flex items-center gap-1.5 text-xs text-gray-400 bg-slate-800 border border-slate-700 rounded px-2.5 py-1.5">
          <GitBranch className="w-3 h-3 text-gray-500 shrink-0" />
          <span className="font-mono truncate">{lockedWorktreeValue}</span>
        </div>
      ) : useWorktree && !useWorktreeDisabled ? (
        <div className="pl-6">
          <div className="flex items-center gap-2 mb-1">
            <GitBranch className="w-3.5 h-3.5 text-gray-500" />
            <label className="text-xs text-gray-400">Worktree</label>
            <button
              type="button"
              onClick={onRequestWorktrees}
              className="text-gray-500 hover:text-gray-300 transition-colors"
              title="Refresh worktree list"
            >
              <RefreshCw className={`w-3 h-3 ${worktreeRequestPending ? 'animate-spin' : ''}`} />
            </button>
          </div>
          <Select
            value={selectedWorktree}
            onChange={(e) => onSelectedWorktreeChange(e.target.value)}
          >
            <option value="">New worktree (auto-generated)</option>
            {worktrees.map((wt) => (
              <option key={wt.name} value={wt.name}>
                {wt.name} ({wt.branch})
              </option>
            ))}
          </Select>
        </div>
      ) : null}
    </>
  )
}
