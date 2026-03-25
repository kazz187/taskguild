import type { AgentDiff } from '@taskguild/proto/taskguild/v1/agent_manager_pb.ts'
import { AgentDiffType, AgentResolutionChoice } from '@taskguild/proto/taskguild/v1/agent_manager_pb.ts'
import { AlertTriangle, Server, Monitor } from 'lucide-react'
import { Button, Badge } from '../atoms/index.ts'
import { Modal } from '../molecules/index.ts'
import { diffTypeLabel } from './AgentListUtils.ts'

export function DiffResolutionModal({ diff, onClose, onResolve, isPending, error }: {
  diff: AgentDiff | null
  onClose: () => void
  onResolve: (diff: AgentDiff, choice: AgentResolutionChoice) => void
  isPending: boolean
  error?: Error | null
}) {
  return (
    <Modal open={!!diff} onClose={onClose} size="lg">
      <Modal.Header onClose={onClose}>
        <div className="flex items-center gap-2">
          <AlertTriangle className="w-5 h-5 text-amber-400" />
          <h3 className="text-lg font-semibold text-white">Agent Conflict</h3>
        </div>
      </Modal.Header>
      <Modal.Body>
        {diff && (
          <div className="space-y-4">
            <div className="flex items-center gap-2 text-sm">
              <span className="text-gray-400">Agent:</span>
              <span className="text-white font-medium">{diff.agentName}</span>
              <Badge color="gray" size="xs" className="font-mono">{diff.filename}</Badge>
              <Badge color="amber" size="xs" variant="outline">{diffTypeLabel(diff.diffType)}</Badge>
            </div>

            <div className="grid grid-cols-2 gap-3">
              {/* Server version */}
              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <Server className="w-4 h-4 text-blue-400" />
                  <span className="text-sm font-medium text-blue-400">Server Version</span>
                </div>
                <pre className="text-xs text-gray-300 font-mono bg-slate-900 rounded p-3 whitespace-pre-wrap max-h-[400px] overflow-y-auto border border-slate-700">
                  {diff.serverContent || <span className="text-gray-600 italic">No server version</span>}
                </pre>
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => onResolve(diff, AgentResolutionChoice.SERVER)}
                  disabled={isPending || diff.diffType === AgentDiffType.AGENT_ONLY}
                  icon={<Server className="w-3.5 h-3.5" />}
                  className="w-full bg-blue-600 hover:bg-blue-500"
                >
                  {isPending ? 'Resolving...' : 'Use Server Version'}
                </Button>
              </div>

              {/* Agent version */}
              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <Monitor className="w-4 h-4 text-green-400" />
                  <span className="text-sm font-medium text-green-400">Agent Version</span>
                </div>
                <pre className="text-xs text-gray-300 font-mono bg-slate-900 rounded p-3 whitespace-pre-wrap max-h-[400px] overflow-y-auto border border-slate-700">
                  {diff.agentContent || <span className="text-gray-600 italic">No agent version</span>}
                </pre>
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => onResolve(diff, AgentResolutionChoice.AGENT)}
                  disabled={isPending || diff.diffType === AgentDiffType.SERVER_ONLY}
                  icon={<Monitor className="w-3.5 h-3.5" />}
                  className="w-full bg-green-600 hover:bg-green-500"
                >
                  {isPending ? 'Resolving...' : 'Use Agent Version'}
                </Button>
              </div>
            </div>

            {error && (
              <p className="text-red-400 text-sm">{error.message}</p>
            )}
          </div>
        )}
      </Modal.Body>
    </Modal>
  )
}
