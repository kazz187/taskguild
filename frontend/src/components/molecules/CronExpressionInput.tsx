import { Input } from '../atoms/index.ts'

export interface CronExpressionInputProps {
  value: string
  onChange: (value: string) => void
  disabled?: boolean
  /** @default 'sm' */
  inputSize?: 'xs' | 'sm' | 'md'
}

interface CronPreset {
  label: string
  expr: string
}

const PRESETS: CronPreset[] = [
  { label: 'Every minute', expr: '* * * * *' },
  { label: 'Every 5 minutes', expr: '*/5 * * * *' },
  { label: 'Every hour', expr: '0 * * * *' },
  { label: 'Daily at 09:00', expr: '0 9 * * *' },
  { label: 'Weekdays at 09:00', expr: '0 9 * * 1-5' },
  { label: 'Weekly (Mon 09:00)', expr: '0 9 * * 1' },
  { label: 'Monthly (1st 09:00)', expr: '0 9 1 * *' },
]

/**
 * Returns null when expr is structurally valid (5 whitespace-separated tokens),
 * otherwise a short error string for inline display. Final cron semantics are
 * validated server-side; this is a smoke test only.
 */
export function validateCronShape(expr: string): string | null {
  const trimmed = expr.trim()
  if (!trimmed) return 'cron expression is required'

  const tokens = trimmed.split(/\s+/)
  if (tokens.length !== 5) {
    return `expected 5 fields (minute hour day-of-month month day-of-week), got ${tokens.length}`
  }

  return null
}

export function CronExpressionInput({
  value,
  onChange,
  disabled,
  inputSize = 'sm',
}: CronExpressionInputProps) {
  const validationError = value.trim() === '' ? null : validateCronShape(value)

  return (
    <div className="space-y-1.5">
      <Input
        inputSize={inputSize}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        placeholder="*/5 * * * *"
        className="font-mono"
        spellCheck={false}
      />
      {validationError && (
        <p className="text-[11px] text-amber-400">{validationError}</p>
      )}
      <div className="flex flex-wrap gap-1 pt-0.5">
        <span className="text-[10px] text-gray-500 mr-1 pt-1">Examples:</span>
        {PRESETS.map((p) => (
          <button
            key={p.expr}
            type="button"
            onClick={() => onChange(p.expr)}
            disabled={disabled}
            title={p.expr}
            className="text-[10px] px-1.5 py-0.5 rounded bg-slate-800 hover:bg-slate-700 text-gray-400 hover:text-white border border-slate-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {p.label}
          </button>
        ))}
      </div>
    </div>
  )
}
