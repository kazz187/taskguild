import type { ReactNode } from 'react'
import { X } from 'lucide-react'
import { Button, Input } from '../atoms/index.ts'
import { Modal } from './Modal.tsx'

export interface TaskFormModalProps {
  /** タイトル入力の値 */
  title: string
  /** タイトル変更コールバック */
  onTitleChange: (value: string) => void
  /** @default "Task title..." */
  titlePlaceholder?: string
  /** Enter キー押下 / Submit ボタン押下時のコールバック */
  onSubmit: () => void
  /** モーダルを閉じるコールバック (Cancel ボタン・X ボタン・ESC 共通) */
  onClose: () => void

  /** 送信ボタンのラベル */
  submitLabel: string
  /** 送信中のラベル */
  submitPendingLabel?: string
  /** 送信中かどうか */
  isSubmitting?: boolean
  /** 送信ボタンの disabled (isSubmitting とは別に追加条件を渡す) */
  submitDisabled?: boolean

  /** フッター左側のアクション (Delete, Subtask 等) */
  footerLeadingActions?: ReactNode

  /** Modal.Body の中身 */
  children: ReactNode
}

export function TaskFormModal({
  title,
  onTitleChange,
  titlePlaceholder = 'Task title...',
  onSubmit,
  onClose,
  submitLabel,
  submitPendingLabel,
  isSubmitting = false,
  submitDisabled = false,
  footerLeadingActions,
  children,
}: TaskFormModalProps) {
  return (
    <Modal open={true} onClose={onClose} size="full">
      {/* Header */}
      <div className="flex items-start justify-between px-4 pt-4 pb-1">
        <div className="flex-1 min-w-0 mr-3">
          <Input
            autoFocus
            value={title}
            onChange={(e) => onTitleChange(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) onSubmit()
            }}
            className="!border-slate-600 text-base md:text-lg font-semibold !rounded"
            placeholder={titlePlaceholder}
          />
        </div>
        <button
          tabIndex={-1}
          onClick={onClose}
          className="text-gray-500 hover:text-gray-300 transition-colors shrink-0 mt-1 p-1"
        >
          <X className="w-5 h-5" />
        </button>
      </div>

      {/* Body */}
      <Modal.Body>
        {children}

        {/* Footer */}
        <div
          className={`border-t border-slate-800 mt-4 pt-3 flex items-center gap-2 ${
            footerLeadingActions ? 'justify-between' : 'justify-end'
          }`}
        >
          {footerLeadingActions && (
            <div className="flex items-center gap-2">{footerLeadingActions}</div>
          )}
          <div className="flex items-center gap-2">
            <Button variant="secondary" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button
              variant="primary"
              size="sm"
              onClick={onSubmit}
              disabled={isSubmitting || submitDisabled}
            >
              {isSubmitting && submitPendingLabel ? submitPendingLabel : submitLabel}
            </Button>
          </div>
        </div>
      </Modal.Body>
    </Modal>
  )
}
