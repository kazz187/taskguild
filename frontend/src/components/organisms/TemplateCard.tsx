import type { Template } from '@taskguild/proto/taskguild/v1/template_pb.ts'
import { Edit2, Trash2 } from 'lucide-react'
import { Button, Badge } from '../atoms/index.ts'
import { Card } from '../molecules/index.ts'
import { TABS } from './TemplateListTypes.ts'

export function TemplateCard({ template: tmpl, onEdit, onDelete, isDeleting }: {
  template: Template
  onEdit: () => void
  onDelete: () => void
  isDeleting: boolean
}) {
  const tabInfo = TABS.find(t => t.type === tmpl.entityType) ?? TABS[0]
  const Icon = tabInfo.icon

  const configName = (() => {
    if (tmpl.entityType === 'agent' && tmpl.agentConfig) return tmpl.agentConfig.name
    if (tmpl.entityType === 'skill' && tmpl.skillConfig) return tmpl.skillConfig.name
    if (tmpl.entityType === 'script' && tmpl.scriptConfig) return tmpl.scriptConfig.name
    return ''
  })()

  const configPreview = (() => {
    if (tmpl.entityType === 'agent' && tmpl.agentConfig) return tmpl.agentConfig.prompt
    if (tmpl.entityType === 'skill' && tmpl.skillConfig) return tmpl.skillConfig.content
    if (tmpl.entityType === 'script' && tmpl.scriptConfig) return tmpl.scriptConfig.content
    return ''
  })()

  return (
    <Card className="hover:border-slate-700 transition-colors">
      <div className="flex items-start justify-between">
        <div className="flex items-start gap-3 flex-1 min-w-0">
          <Icon className={`w-5 h-5 mt-0.5 shrink-0 ${
            tmpl.entityType === 'agent' ? 'text-cyan-400' :
            tmpl.entityType === 'skill' ? 'text-purple-400' : 'text-green-400'
          }`} />
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 mb-1">
              <h3 className="text-sm font-semibold text-white truncate">{tmpl.name}</h3>
              {configName && configName !== tmpl.name && (
                <Badge color="gray" size="xs" pill>
                  {configName}
                </Badge>
              )}
              <Badge
                color={
                  tmpl.entityType === 'agent' ? 'cyan' :
                  tmpl.entityType === 'skill' ? 'purple' : 'green'
                }
                size="xs"
                variant="outline"
                pill
              >
                {tmpl.entityType}
              </Badge>
            </div>
            {tmpl.description && (
              <p className="text-xs text-gray-400 mb-2">{tmpl.description}</p>
            )}

            {/* Agent-specific details */}
            {tmpl.entityType === 'agent' && tmpl.agentConfig && (
              <>
                {tmpl.agentConfig.tools && tmpl.agentConfig.tools.length > 0 && (
                  <div className="flex flex-wrap gap-1 mb-1">
                    {tmpl.agentConfig.tools.map(tool => (
                      <Badge key={tool} color="gray" size="xs" className="bg-slate-800 text-gray-500">
                        {tool}
                      </Badge>
                    ))}
                  </div>
                )}
                {tmpl.agentConfig.skills && tmpl.agentConfig.skills.length > 0 && (
                  <div className="flex flex-wrap gap-1 mb-1">
                    {tmpl.agentConfig.skills.map(skill => (
                      <Badge key={skill} color="purple" size="xs">
                        {skill}
                      </Badge>
                    ))}
                  </div>
                )}
              </>
            )}

            {/* Skill-specific details */}
            {tmpl.entityType === 'skill' && tmpl.skillConfig && (
              <>
                {tmpl.skillConfig.allowedTools && tmpl.skillConfig.allowedTools.length > 0 && (
                  <div className="flex flex-wrap gap-1 mb-1">
                    {tmpl.skillConfig.allowedTools.map(tool => (
                      <Badge key={tool} color="purple" size="xs">
                        {tool}
                      </Badge>
                    ))}
                  </div>
                )}
              </>
            )}

            {configPreview && (
              <pre className="text-[11px] text-gray-600 mt-1 truncate max-w-lg font-mono">
                {configPreview.slice(0, 120)}{configPreview.length > 120 ? '...' : ''}
              </pre>
            )}
          </div>
        </div>
        <div className="flex items-center gap-1 shrink-0 ml-2">
          <Button
            variant="ghost"
            size="sm"
            iconOnly
            onClick={onEdit}
            title="Edit"
            className="hover:text-amber-400"
            icon={<Edit2 className="w-3.5 h-3.5" />}
          />
          <Button
            variant="ghost"
            size="sm"
            iconOnly
            onClick={onDelete}
            disabled={isDeleting}
            title="Delete"
            className="hover:text-red-400"
            icon={<Trash2 className="w-3.5 h-3.5" />}
          />
        </div>
      </div>
    </Card>
  )
}
