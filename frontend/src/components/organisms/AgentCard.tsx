import type { AgentDefinition } from '@taskguild/proto/taskguild/v1/agent_pb.ts'
import type { AgentDiff } from '@taskguild/proto/taskguild/v1/agent_manager_pb.ts'
import { Bot, Trash2, Edit2, Copy, Cloud, AlertTriangle } from 'lucide-react'
import { Button, Badge } from '../atoms/index.ts'
import { Card } from '../molecules/index.ts'
import { diffTypeLabel } from './AgentListUtils.ts'

export function AgentCard({ agent, diff, onEdit, onDelete, onSaveAsTemplate, onShowDiff, isDeleting }: {
  agent: AgentDefinition
  diff?: AgentDiff
  onEdit: () => void
  onDelete: () => void
  onSaveAsTemplate: () => void
  onShowDiff: (diff: AgentDiff) => void
  isDeleting: boolean
}) {
  return (
    <Card
      className={`hover:border-slate-700 transition-colors ${diff ? 'border-amber-500/30' : ''}`}
    >
      <div className="flex items-start justify-between">
        <div className="flex items-start gap-3 flex-1 min-w-0">
          <Bot className="w-5 h-5 text-cyan-400 mt-0.5 shrink-0" />
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 mb-1">
              <h3
                className="text-sm font-semibold text-white truncate cursor-pointer hover:text-cyan-400 transition-colors"
                onClick={onEdit}
              >{agent.name}</h3>
              {agent.isSynced && (
                <Badge color="blue" size="xs" pill variant="outline" icon={<Cloud className="w-2.5 h-2.5" />}>
                  synced
                </Badge>
              )}
              {diff && (
                <Badge
                  color="amber"
                  size="xs"
                  pill
                  variant="outline"
                  icon={<AlertTriangle className="w-2.5 h-2.5" />}
                  className="cursor-pointer hover:bg-amber-500/20"
                  onClick={() => onShowDiff(diff)}
                >
                  {diffTypeLabel(diff.diffType)}
                </Badge>
              )}
              {agent.model && (
                <Badge color="gray" size="xs" pill variant="outline">
                  {agent.model}
                </Badge>
              )}
              {agent.memory && (
                <Badge color="purple" size="xs" pill variant="outline">
                  memory: {agent.memory}
                </Badge>
              )}
            </div>
            <p className="text-xs text-gray-400 mb-2">{agent.description}</p>
            {agent.tools?.length > 0 && (
              <div className="flex flex-wrap gap-1 mb-1">
                {agent.tools.map(tool => (
                  <Badge key={tool} color="gray" size="xs">
                    {tool}
                  </Badge>
                ))}
              </div>
            )}
            {agent.disallowedTools?.length > 0 && (
              <div className="flex flex-wrap gap-1 mb-1">
                {agent.disallowedTools.map(tool => (
                  <Badge key={tool} color="red" size="xs">
                    -{tool}
                  </Badge>
                ))}
              </div>
            )}
            {agent.skills?.length > 0 && (
              <div className="flex flex-wrap gap-1 mb-1">
                {agent.skills.map(skill => (
                  <Badge key={skill} color="purple" size="xs">
                    {skill}
                  </Badge>
                ))}
              </div>
            )}
            {agent.prompt && (
              <pre className="text-[11px] text-gray-600 mt-1 truncate max-w-lg font-mono">
                {agent.prompt.slice(0, 120)}{agent.prompt.length > 120 ? '...' : ''}
              </pre>
            )}
          </div>
        </div>
        <div className="flex items-center gap-1 shrink-0 ml-2">
          <Button
            variant="ghost"
            size="sm"
            iconOnly
            icon={<Copy className="w-3.5 h-3.5" />}
            onClick={onSaveAsTemplate}
            title="Save as Template"
            className="hover:text-amber-400"
          />
          <Button
            variant="ghost"
            size="sm"
            iconOnly
            icon={<Edit2 className="w-3.5 h-3.5" />}
            onClick={onEdit}
            title="Edit"
            className="hover:text-cyan-400"
          />
          <Button
            variant="ghost"
            size="sm"
            iconOnly
            icon={<Trash2 className="w-3.5 h-3.5" />}
            onClick={onDelete}
            disabled={isDeleting}
            title="Delete"
            className="hover:text-red-400"
          />
        </div>
      </div>
    </Card>
  )
}

export function AgentOnlyDiffCard({ diff, onClick }: {
  diff: AgentDiff
  onClick: () => void
}) {
  return (
    <Card
      className="border-amber-500/30 hover:border-amber-500/50 transition-colors cursor-pointer"
      onClick={onClick}
    >
      <div className="flex items-start justify-between">
        <div className="flex items-start gap-3 flex-1 min-w-0">
          <Bot className="w-5 h-5 text-amber-400 mt-0.5 shrink-0" />
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 mb-1">
              <h3 className="text-sm font-semibold text-white truncate">{diff.agentName}</h3>
              <Badge color="amber" size="xs" pill variant="outline" icon={<AlertTriangle className="w-2.5 h-2.5" />}>
                Agent Only
              </Badge>
              <Badge color="gray" size="xs" className="font-mono">{diff.filename}</Badge>
            </div>
            <p className="text-xs text-gray-400">
              This agent exists on the local agent but not in the server database. Click to resolve.
            </p>
          </div>
        </div>
      </div>
    </Card>
  )
}
