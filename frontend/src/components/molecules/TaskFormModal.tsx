import type { ReactNode } from 'react'
import { Button, Input } from '../atoms/index.ts'
import { Modal } from './Modal.tsx'

export interface TaskFormModalProps {
  /** ヘッダーラベル ("New Task", "Edit Task", "New Subtask" など) */
  headerLabel: string
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
  headerLabel,
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
      <Modal.Header onClose={onClose}>
        <h3 className="text-lg font-semibold text-white">{headerLabel}</h3>
      </Modal.Header>

      {/* Task 固有: タイトル入力欄 */}
      <div className="px-4 pb-1">
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

      <Modal.Body>
        {children}
      </Modal.Body>

      <Modal.Footer align={footerLeadingActions ? 'between' : 'end'}>
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
      </Modal.Footer>
    </Modal>
  )
}
