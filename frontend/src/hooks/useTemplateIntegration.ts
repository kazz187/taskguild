import { useState } from 'react'

interface SaveDialogState {
  entityId: string
  name: string
  description: string
}

export function useTemplateIntegration() {
  const [saveDialog, setSaveDialog] = useState<SaveDialogState | null>(null)
  const [pickerOpen, setPickerOpen] = useState(false)

  const openSaveDialog = (entity: { id: string; name: string; description: string }) => {
    setSaveDialog({ entityId: entity.id, name: entity.name, description: entity.description })
  }

  const closeSaveDialog = () => setSaveDialog(null)

  const openPicker = () => setPickerOpen(true)
  const closePicker = () => setPickerOpen(false)

  return {
    saveDialog,
    setSaveDialog,
    openSaveDialog,
    closeSaveDialog,
    pickerOpen,
    openPicker,
    closePicker,
  }
}
