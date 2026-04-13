import { useState } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listSkills, createSkill, updateSkill, deleteSkill, syncSkillsFromDir } from '@taskguild/proto/taskguild/v1/skill-SkillService_connectquery.ts'
import type { SkillDefinition } from '@taskguild/proto/taskguild/v1/skill_pb.ts'
import type { Template } from '@taskguild/proto/taskguild/v1/template_pb.ts'
import { Sparkles, Plus, Trash2, Edit2, Cloud, Layers, Copy } from 'lucide-react'
import { Button, Badge } from '../atoms/index.ts'
import { Card, PageHeading, EmptyState, SyncButton } from '../molecules/index.ts'
import { useTemplateIntegration } from '@/hooks/useTemplateIntegration.ts'
import { SaveAsTemplateDialog } from './SaveAsTemplateDialog.tsx'
import { TemplatePickerDialog } from './TemplatePickerDialog.tsx'
import { SkillFormModal } from './SkillFormModal.tsx'
import { emptyForm, skillToForm } from './SkillListUtils.ts'
import type { SkillFormData } from './SkillListUtils.ts'

export function SkillList({ projectId }: { projectId: string }) {
  const { data, refetch, isLoading } = useQuery(listSkills, { projectId })
  const createMut = useMutation(createSkill)
  const updateMut = useMutation(updateSkill)
  const deleteMut = useMutation(deleteSkill)
  const syncMut = useMutation(syncSkillsFromDir)

  const [formMode, setFormMode] = useState<'create' | 'edit' | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<SkillFormData>(emptyForm)

  const { saveDialog, setSaveDialog, openSaveDialog, closeSaveDialog, pickerOpen, openPicker, closePicker } = useTemplateIntegration()

  const skills = data?.skills ?? []

  const openCreate = () => {
    setFormMode('create')
    setEditingId(null)
    setForm(emptyForm)
  }

  const openEdit = (s: SkillDefinition) => {
    setFormMode('edit')
    setEditingId(s.id)
    setForm(skillToForm(s))
  }

  const closeForm = () => {
    setFormMode(null)
    setEditingId(null)
    setForm(emptyForm)
  }

  const handleCreateFromTemplate = (tmpl: Template) => {
    if (!tmpl.skillConfig) return
    closePicker()
    setFormMode('create')
    setEditingId(null)
    setForm({
      name: tmpl.skillConfig.name,
      description: tmpl.skillConfig.description,
      content: tmpl.skillConfig.content,
      disableModelInvocation: tmpl.skillConfig.disableModelInvocation,
      userInvocable: tmpl.skillConfig.userInvocable,
      allowedTools: [...(tmpl.skillConfig.allowedTools ?? [])],
      model: tmpl.skillConfig.model,
      context: tmpl.skillConfig.context,
      agent: tmpl.skillConfig.agent,
      argumentHint: tmpl.skillConfig.argumentHint,
    })
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (formMode === 'create') {
      createMut.mutate(
        { projectId, ...form },
        { onSuccess: () => { closeForm(); refetch() } },
      )
    } else if (formMode === 'edit' && editingId) {
      updateMut.mutate(
        { id: editingId, ...form },
        { onSuccess: () => { closeForm(); refetch() } },
      )
    }
  }

  const handleDelete = (id: string) => {
    if (!confirm('Delete this skill?')) return
    deleteMut.mutate({ id }, { onSuccess: () => refetch() })
  }

  const handleSync = () => {
    syncMut.mutate(
      { projectId },
      { onSuccess: () => refetch() },
    )
  }

  const mutation = formMode === 'create' ? createMut : updateMut

  return (
    <div className="p-6 max-w-4xl mx-auto">
      <div className="flex items-center justify-between mb-6">
        <PageHeading icon={Sparkles} title="Skills" iconColor="text-purple-400">
          <Badge color="gray" size="xs" pill variant="outline">
            {skills.length}
          </Badge>
        </PageHeading>
        <div className="flex items-center gap-2">
          <SyncButton
            onClick={handleSync}
            isPending={syncMut.isPending}
            title="Sync skills from .claude/skills/ directory"
          />
          <Button
            variant="ghost"
            size="sm"
            onClick={openPicker}
            icon={<Layers className="w-4 h-4" />}
            title="Create skill from template"
            className="border border-slate-700 hover:border-slate-600"
          >
            From Template
          </Button>
          <Button
            variant="primary"
            size="sm"
            onClick={openCreate}
            icon={<Plus className="w-4 h-4" />}
            className="bg-purple-600 hover:bg-purple-500"
          >
            New Skill
          </Button>
        </div>
      </div>

      {syncMut.isSuccess && (
        <div className="mb-4 px-3 py-2 bg-green-500/10 border border-green-500/20 rounded-lg text-green-400 text-sm">
          Synced: {syncMut.data?.created ?? 0} created, {syncMut.data?.updated ?? 0} updated
        </div>
      )}

      {/* Skill Form Modal */}
      {formMode && (
        <SkillFormModal
          formMode={formMode}
          form={form}
          setForm={setForm}
          onSubmit={handleSubmit}
          onClose={closeForm}
          isPending={mutation.isPending}
          error={mutation.error}
        />
      )}

      {/* Skill Cards */}
      {isLoading && <p className="text-gray-400 text-sm">Loading skills...</p>}

      <div className="space-y-3">
        {skills.map(skill => (
          <Card
            key={skill.id}
            className="hover:border-slate-700 transition-colors"
          >
            <div className="flex items-start justify-between">
              <div className="flex items-start gap-3 flex-1 min-w-0">
                <Sparkles className="w-5 h-5 text-purple-400 mt-0.5 shrink-0" />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <h3 className="text-sm font-semibold text-white truncate">{skill.name}</h3>
                    {skill.isSynced && (
                      <Badge color="blue" size="xs" variant="outline" pill icon={<Cloud className="w-2.5 h-2.5" />}>
                        synced
                      </Badge>
                    )}
                    {skill.model && (
                      <Badge color="gray" size="xs" pill>
                        {skill.model}
                      </Badge>
                    )}
                    {skill.context === 'fork' && (
                      <Badge color="orange" size="xs" variant="outline" pill>
                        fork{skill.agent ? `: ${skill.agent}` : ''}
                      </Badge>
                    )}
                    {skill.disableModelInvocation && (
                      <Badge color="yellow" size="xs" variant="outline" pill>
                        manual only
                      </Badge>
                    )}
                    {!skill.userInvocable && (
                      <Badge color="gray" size="xs" pill className="bg-slate-700 text-gray-400">
                        model only
                      </Badge>
                    )}
                  </div>
                  {skill.description && (
                    <p className="text-xs text-gray-400 mb-2">{skill.description}</p>
                  )}
                  {skill.argumentHint && (
                    <p className="text-[10px] text-gray-500 mb-1">
                      <span className="text-gray-600">Usage:</span> /{skill.name} {skill.argumentHint}
                    </p>
                  )}
                  {skill.allowedTools?.length > 0 && (
                    <div className="flex flex-wrap gap-1 mb-1">
                      {skill.allowedTools.map(tool => (
                        <Badge key={tool} color="purple" size="xs">
                          {tool}
                        </Badge>
                      ))}
                    </div>
                  )}
                  {skill.content && (
                    <pre className="text-[11px] text-gray-600 mt-1 truncate max-w-lg font-mono">
                      {skill.content.slice(0, 120)}{skill.content.length > 120 ? '...' : ''}
                    </pre>
                  )}
                </div>
              </div>
              <div className="flex items-center gap-1 shrink-0 ml-2">
                <Button
                  variant="ghost"
                  size="sm"
                  iconOnly
                  onClick={() => openSaveDialog(skill)}
                  title="Save as Template"
                  className="hover:text-amber-400"
                  icon={<Copy className="w-3.5 h-3.5" />}
                />
                <Button
                  variant="ghost"
                  size="sm"
                  iconOnly
                  onClick={() => openEdit(skill)}
                  title="Edit"
                  className="hover:text-purple-400"
                  icon={<Edit2 className="w-3.5 h-3.5" />}
                />
                <Button
                  variant="ghost"
                  size="sm"
                  iconOnly
                  onClick={() => handleDelete(skill.id)}
                  disabled={deleteMut.isPending}
                  title="Delete"
                  className="hover:text-red-400"
                  icon={<Trash2 className="w-3.5 h-3.5" />}
                />
              </div>
            </div>
          </Card>
        ))}

        {!isLoading && skills.length === 0 && !formMode && (
          <EmptyState
            icon={Sparkles}
            message="No skills defined yet."
            hint="Create skills or sync from your repository's .claude/skills/ directory."
          />
        )}
      </div>

      {/* Template Picker Dialog */}
      <TemplatePickerDialog
        open={pickerOpen}
        entityType="skill"
        entityLabel="Skill"
        icon={Sparkles}
        iconColor="text-purple-400"
        onSelect={handleCreateFromTemplate}
        onClose={closePicker}
      />

      {/* Save as Template Dialog */}
      <SaveAsTemplateDialog
        dialog={saveDialog}
        setDialog={setSaveDialog}
        entityType="skill"
        onSaved={closeSaveDialog}
      />
    </div>
  )
}
