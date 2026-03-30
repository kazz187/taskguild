import { useState } from 'react'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listTemplates, createTemplate, updateTemplate, deleteTemplate } from '@taskguild/proto/taskguild/v1/template-TemplateService_connectquery.ts'
import type { Template } from '@taskguild/proto/taskguild/v1/template_pb.ts'
import { Layers, Plus, X, Save } from 'lucide-react'
import { Button, Input, MutationError } from '../atoms/index.ts'
import { Card, FormField, EmptyState } from '../molecules/index.ts'
import { toggleArrayItem } from '@/lib/arrays.ts'
import type { EntityType, TemplateFormData } from './TemplateListTypes.ts'
import { TABS, emptyTemplateForm, templateToForm } from './TemplateListTypes.ts'
import { TemplateCard } from './TemplateCard.tsx'
import { AgentConfigForm, SkillConfigForm, ScriptConfigForm } from './TemplateConfigForms.tsx'

export function TemplateList() {
  const [activeTab, setActiveTab] = useState<EntityType>('agent')
  const { data, refetch, isLoading } = useQuery(listTemplates, { entityType: activeTab })
  const createMut = useMutation(createTemplate)
  const updateMut = useMutation(updateTemplate)
  const deleteMut = useMutation(deleteTemplate)

  const [formMode, setFormMode] = useState<'create' | 'edit' | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<TemplateFormData>(emptyTemplateForm('agent'))
  const [skillInput, setSkillInput] = useState('')

  const templates = data?.templates ?? []

  const openCreate = () => {
    setFormMode('create')
    setEditingId(null)
    setForm(emptyTemplateForm(activeTab))
    setSkillInput('')
  }

  const openEdit = (t: Template) => {
    setFormMode('edit')
    setEditingId(t.id)
    setForm(templateToForm(t))
    setSkillInput('')
  }

  const closeForm = () => {
    setFormMode(null)
    setEditingId(null)
    setForm(emptyTemplateForm(activeTab))
    setSkillInput('')
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const base = {
      name: form.templateName,
      description: form.templateDescription,
    }

    const configPayload = (() => {
      switch (form.entityType) {
        case 'agent': return { agentConfig: form.agentConfig }
        case 'skill': return { skillConfig: form.skillConfig }
        case 'script': return { scriptConfig: form.scriptConfig }
      }
    })()

    if (formMode === 'create') {
      createMut.mutate(
        { ...base, entityType: form.entityType, ...configPayload },
        { onSuccess: () => { closeForm(); refetch() } },
      )
    } else if (formMode === 'edit' && editingId) {
      updateMut.mutate(
        { id: editingId, ...base, ...configPayload },
        { onSuccess: () => { closeForm(); refetch() } },
      )
    }
  }

  const handleDelete = (id: string) => {
    if (!confirm('Delete this template?')) return
    deleteMut.mutate({ id }, { onSuccess: () => refetch() })
  }

  const handleTabChange = (type: EntityType) => {
    closeForm()
    setActiveTab(type)
  }

  const mutation = formMode === 'create' ? createMut : updateMut
  const activeTabInfo = TABS.find(t => t.type === activeTab)!

  const toggleTool = (tool: string) => {
    setForm(prev => ({
      ...prev,
      agentConfig: { ...prev.agentConfig, tools: toggleArrayItem(prev.agentConfig.tools, tool) },
    }))
  }

  const toggleDisallowedTool = (tool: string) => {
    setForm(prev => ({
      ...prev,
      agentConfig: { ...prev.agentConfig, disallowedTools: toggleArrayItem(prev.agentConfig.disallowedTools, tool) },
    }))
  }

  const addSkill = () => {
    const trimmed = skillInput.trim()
    if (trimmed && !form.agentConfig.skills.includes(trimmed)) {
      setForm(prev => ({
        ...prev,
        agentConfig: { ...prev.agentConfig, skills: [...prev.agentConfig.skills, trimmed] },
      }))
      setSkillInput('')
    }
  }

  const removeSkill = (skill: string) => {
    setForm(prev => ({
      ...prev,
      agentConfig: { ...prev.agentConfig, skills: prev.agentConfig.skills.filter(s => s !== skill) },
    }))
  }

  const toggleAllowedTool = (tool: string) => {
    setForm(prev => ({
      ...prev,
      skillConfig: { ...prev.skillConfig, allowedTools: toggleArrayItem(prev.skillConfig.allowedTools, tool) },
    }))
  }

  return (
    <div className="p-4 md:p-6 max-w-4xl mx-auto">
      {/* Header */}
      <div className="flex items-center justify-between mb-4 md:mb-6">
        <div className="flex items-center gap-2">
          <Layers className="w-5 h-5 text-amber-400" />
          <h2 className="text-lg md:text-xl font-bold text-white">Templates</h2>
        </div>
        <Button
          variant="danger"
          size="sm"
          onClick={openCreate}
          icon={<Plus className="w-4 h-4" />}
          className="text-xs md:text-sm md:px-3"
        >
          <span className="hidden sm:inline">New Template</span>
          <span className="sm:hidden">New</span>
        </Button>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 mb-4 md:mb-6 bg-slate-900 rounded-lg p-1 border border-slate-800">
        {TABS.map(tab => {
          const Icon = tab.icon
          return (
            <button
              key={tab.type}
              onClick={() => handleTabChange(tab.type)}
              className={`flex-1 flex items-center justify-center gap-1.5 px-3 py-2 text-xs md:text-sm rounded-md transition-colors ${
                activeTab === tab.type
                  ? `bg-slate-800 text-white`
                  : 'text-gray-500 hover:text-gray-300'
              }`}
            >
              <Icon className="w-3.5 h-3.5" />
              {tab.label}
            </button>
          )
        })}
      </div>

      {/* Form */}
      {formMode && (
        <form onSubmit={handleSubmit}>
          <Card className="p-4 md:p-5 mb-4 md:mb-6">
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-semibold text-white">
                {formMode === 'create' ? 'New' : 'Edit'} {activeTabInfo.label.slice(0, -1)} Template
              </h3>
              <Button variant="ghost" size="sm" iconOnly onClick={closeForm} type="button" icon={<X className="w-5 h-5" />} />
            </div>

            <div className="space-y-4">
              {/* Template Name & Description */}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <FormField label="Template Name *">
                  <Input
                    type="text"
                    required
                    value={form.templateName}
                    onChange={e => setForm(prev => ({ ...prev, templateName: e.target.value }))}
                    className="focus:border-amber-500"
                    placeholder="e.g. My Code Reviewer Template"
                  />
                </FormField>
                <FormField label="Template Description">
                  <Input
                    type="text"
                    value={form.templateDescription}
                    onChange={e => setForm(prev => ({ ...prev, templateDescription: e.target.value }))}
                    className="focus:border-amber-500"
                    placeholder="Description of this template"
                  />
                </FormField>
              </div>

              <div className="border-t border-slate-800 pt-4">
                <p className="text-xs text-gray-500 mb-3">Configuration</p>
              </div>

              {/* Entity-specific config form */}
              {activeTab === 'agent' && <AgentConfigForm form={form} setForm={setForm} skillInput={skillInput} setSkillInput={setSkillInput} toggleTool={toggleTool} toggleDisallowedTool={toggleDisallowedTool} addSkill={addSkill} removeSkill={removeSkill} />}
              {activeTab === 'skill' && <SkillConfigForm form={form} setForm={setForm} toggleAllowedTool={toggleAllowedTool} />}
              {activeTab === 'script' && <ScriptConfigForm form={form} setForm={setForm} />}
            </div>

            <MutationError error={mutation.error} />

            <div className="flex justify-end gap-2 mt-4">
              <Button type="button" variant="secondary" size="sm" onClick={closeForm}>
                Cancel
              </Button>
              <Button
                type="submit"
                variant="danger"
                size="sm"
                disabled={mutation.isPending || !form.templateName}
                icon={<Save className="w-3.5 h-3.5" />}
              >
                {mutation.isPending ? 'Saving...' : formMode === 'create' ? 'Create' : 'Save'}
              </Button>
            </div>
          </Card>
        </form>
      )}

      {/* Template Cards */}
      {isLoading && <p className="text-gray-400 text-sm">Loading templates...</p>}

      <div className="space-y-3">
        {templates.map(tmpl => (
          <TemplateCard
            key={tmpl.id}
            template={tmpl}
            onEdit={() => openEdit(tmpl)}
            onDelete={() => handleDelete(tmpl.id)}
            isDeleting={deleteMut.isPending}
          />
        ))}

        {!isLoading && templates.length === 0 && !formMode && (
          <EmptyState
            icon={Layers}
            message={`No ${activeTabInfo.label.toLowerCase()} templates yet.`}
            hint="Save entities as templates or create one from scratch."
          />
        )}
      </div>
    </div>
  )
}
