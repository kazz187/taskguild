import { useQuery } from '@connectrpc/connect-query'
import { listTemplates } from '@taskguild/proto/taskguild/v1/template-TemplateService_connectquery.ts'
import type { Template } from '@taskguild/proto/taskguild/v1/template_pb.ts'
import type { LucideIcon } from 'lucide-react'
import { Modal } from '../molecules/index.ts'

export interface TemplatePickerDialogProps {
  open: boolean
  entityType: 'agent' | 'skill' | 'script'
  entityLabel: string
  icon: LucideIcon
  iconColor: string
  onSelect: (template: Template) => void
  onClose: () => void
}

export function TemplatePickerDialog({
  open,
  entityType,
  entityLabel,
  icon: Icon,
  iconColor,
  onSelect,
  onClose,
}: TemplatePickerDialogProps) {
  const { data } = useQuery(listTemplates, { entityType })
  const templates = data?.templates ?? []

  return (
    <Modal open={open} onClose={onClose} size="sm">
      <Modal.Header onClose={onClose}>
        <h3 className="text-lg font-semibold text-white">Select {entityLabel} Template</h3>
      </Modal.Header>
      <Modal.Body>
        {templates.length === 0 ? (
          <p className="text-gray-500 text-sm text-center py-6">No {entityLabel.toLowerCase()} templates available.</p>
        ) : (
          <div className="space-y-2">
            {templates.map(tmpl => (
              <button
                key={tmpl.id}
                onClick={() => onSelect(tmpl)}
                className="w-full text-left p-3 bg-slate-800 hover:bg-slate-700 rounded-lg transition-colors"
              >
                <div className="flex items-center gap-2 mb-1">
                  <Icon className={`w-4 h-4 ${iconColor}`} />
                  <span className="text-sm font-medium text-white">{tmpl.name}</span>
                </div>
                {tmpl.description && (
                  <p className="text-xs text-gray-400 ml-6">{tmpl.description}</p>
                )}
              </button>
            ))}
          </div>
        )}
      </Modal.Body>
    </Modal>
  )
}
