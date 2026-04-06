import { useState, useRef, useCallback, useEffect, type DragEvent, type ChangeEvent } from 'react'
import { useMutation, useQuery } from '@connectrpc/connect-query'
import { uploadTaskImage, listTaskImages, getTaskImage } from '@taskguild/proto/taskguild/v1/task-TaskService_connectquery.ts'
import { ImagePlus, X, Loader } from 'lucide-react'
import { Textarea } from '../atoms/index.ts'

const ACCEPTED_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp']
const MAX_SIZE_BYTES = 10 * 1024 * 1024 // 10MB

export interface PendingImage {
  file: File
  /** Sequential number used in [Image#N] reference */
  num: number
  /** Object URL for preview */
  previewUrl: string
}

export interface ImageUploadTextareaProps {
  value: string
  onChange: (value: string) => void
  /** When set, images are uploaded immediately via RPC */
  taskId?: string
  /** For create flow: pending images held in parent state */
  pendingImages?: PendingImage[]
  onPendingImagesChange?: (images: PendingImage[]) => void
  placeholder?: string
  textareaSize?: 'sm' | 'md'
  disabled?: boolean
}

export function ImageUploadTextarea({
  value,
  onChange,
  taskId,
  pendingImages,
  onPendingImagesChange,
  placeholder,
  textareaSize = 'md',
  disabled,
}: ImageUploadTextareaProps) {
  const [isDragging, setIsDragging] = useState(false)
  const [uploading, setUploading] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const dragCounterRef = useRef(0)
  const uploadMut = useMutation(uploadTaskImage)

  // Fetch existing images when taskId is available
  const { data: imagesData } = useQuery(listTaskImages, { taskId: taskId ?? '' }, {
    enabled: !!taskId,
  })

  // Track uploaded image previews (for edit mode)
  const [uploadedPreviews, setUploadedPreviews] = useState<Map<string, string>>(new Map())

  // Load previews for existing images
  useEffect(() => {
    if (!taskId || !imagesData?.images?.length) return

    const loadPreviews = async () => {
      // We'll load previews lazily when needed
    }
    loadPreviews()
  }, [taskId, imagesData])

  const getNextImageNum = useCallback((): number => {
    // Find the highest [Image#N] reference in the text
    const matches = value.matchAll(/\[Image#(\d+)\]/g)
    let maxNum = 0
    for (const match of matches) {
      const num = parseInt(match[1], 10)
      if (num > maxNum) maxNum = num
    }
    return maxNum + 1
  }, [value])

  const insertImageRef = useCallback((num: number) => {
    const ref = `[Image#${num}]`
    const textarea = textareaRef.current
    if (textarea) {
      const start = textarea.selectionStart
      const end = textarea.selectionEnd
      const newValue = value.substring(0, start) + ref + value.substring(end)
      onChange(newValue)
      // Move cursor after the inserted reference
      requestAnimationFrame(() => {
        textarea.selectionStart = textarea.selectionEnd = start + ref.length
        textarea.focus()
      })
    } else {
      onChange(value + ref)
    }
  }, [value, onChange])

  const handleFiles = useCallback(async (files: FileList | File[]) => {
    const validFiles = Array.from(files).filter(f => {
      if (!ACCEPTED_TYPES.includes(f.type)) return false
      if (f.size > MAX_SIZE_BYTES) return false
      return true
    })

    if (validFiles.length === 0) return

    if (taskId) {
      // Edit mode: upload immediately
      setUploading(true)
      for (const file of validFiles) {
        try {
          const arrayBuffer = await file.arrayBuffer()
          const data = new Uint8Array(arrayBuffer)
          const resp = await uploadMut.mutateAsync({
            taskId,
            filename: file.name,
            mediaType: file.type,
            data,
          })
          if (resp.image) {
            const num = parseInt(resp.image.id, 10)
            insertImageRef(num)
            // Store preview
            const url = URL.createObjectURL(file)
            setUploadedPreviews(prev => new Map(prev).set(resp.image!.id, url))
          }
        } catch (err) {
          console.error('Failed to upload image:', err)
        }
      }
      setUploading(false)
    } else {
      // Create mode: hold in pending state
      if (!onPendingImagesChange) return
      const current = pendingImages ?? []
      let nextNum = getNextImageNum()
      const newPending: PendingImage[] = []

      for (const file of validFiles) {
        const num = nextNum++
        newPending.push({
          file,
          num,
          previewUrl: URL.createObjectURL(file),
        })
        insertImageRef(num)
      }

      onPendingImagesChange([...current, ...newPending])
    }
  }, [taskId, uploadMut, insertImageRef, getNextImageNum, pendingImages, onPendingImagesChange])

  const handleDragEnter = useCallback((e: DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    dragCounterRef.current++
    if (dragCounterRef.current === 1) {
      setIsDragging(true)
    }
  }, [])

  const handleDragLeave = useCallback((e: DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    dragCounterRef.current--
    if (dragCounterRef.current === 0) {
      setIsDragging(false)
    }
  }, [])

  const handleDragOver = useCallback((e: DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
  }, [])

  const handleDrop = useCallback((e: DragEvent) => {
    e.preventDefault()
    e.stopPropagation()
    dragCounterRef.current = 0
    setIsDragging(false)
    if (e.dataTransfer.files.length > 0) {
      handleFiles(e.dataTransfer.files)
    }
  }, [handleFiles])

  const handleFileSelect = useCallback((e: ChangeEvent<HTMLInputElement>) => {
    if (e.target.files && e.target.files.length > 0) {
      handleFiles(e.target.files)
    }
    // Reset input so the same file can be selected again
    e.target.value = ''
  }, [handleFiles])

  const handleRemovePending = useCallback((num: number) => {
    if (!onPendingImagesChange || !pendingImages) return
    const img = pendingImages.find(p => p.num === num)
    if (img) {
      URL.revokeObjectURL(img.previewUrl)
    }
    onPendingImagesChange(pendingImages.filter(p => p.num !== num))
    // Remove the [Image#N] reference from text
    onChange(value.replace(`[Image#${num}]`, ''))
  }, [pendingImages, onPendingImagesChange, value, onChange])

  // Collect all image references in the text for preview display
  const imageRefs = Array.from(value.matchAll(/\[Image#(\d+)\]/g)).map(m => parseInt(m[1], 10))

  // Build preview data: merge pending images and uploaded images
  const previewItems = imageRefs.map(num => {
    const id = String(num)
    // Check pending images first
    const pending = pendingImages?.find(p => p.num === num)
    if (pending) {
      return { num, url: pending.previewUrl, isPending: true }
    }
    // Check uploaded previews
    const uploadedUrl = uploadedPreviews.get(id)
    if (uploadedUrl) {
      return { num, url: uploadedUrl, isPending: false }
    }
    return { num, url: null, isPending: false }
  })

  return (
    <div className="relative">
      {/* Drag overlay */}
      <div
        onDragEnter={handleDragEnter}
        onDragLeave={handleDragLeave}
        onDragOver={handleDragOver}
        onDrop={handleDrop}
        className="relative"
      >
        {isDragging && (
          <div className="absolute inset-0 z-10 bg-cyan-500/10 border-2 border-dashed border-cyan-500 rounded-lg flex items-center justify-center pointer-events-none">
            <span className="text-cyan-400 text-sm font-medium">Drop image here</span>
          </div>
        )}

        <Textarea
          ref={textareaRef}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          textareaSize={textareaSize}
          placeholder={placeholder}
          disabled={disabled}
        />
      </div>

      {/* Toolbar */}
      <div className="flex items-center gap-2 mt-1">
        <button
          type="button"
          onClick={() => fileInputRef.current?.click()}
          disabled={disabled || uploading}
          className="flex items-center gap-1 text-xs text-gray-500 hover:text-gray-300 transition-colors disabled:opacity-50"
          title="Add image"
        >
          {uploading ? (
            <Loader className="w-3.5 h-3.5 animate-spin" />
          ) : (
            <ImagePlus className="w-3.5 h-3.5" />
          )}
          <span>Add image</span>
        </button>
        <input
          ref={fileInputRef}
          type="file"
          accept={ACCEPTED_TYPES.join(',')}
          multiple
          onChange={handleFileSelect}
          className="hidden"
        />
      </div>

      {/* Image previews */}
      {previewItems.length > 0 && (
        <div className="flex flex-wrap gap-2 mt-2">
          {previewItems.map(({ num, url, isPending }) => (
            <div key={num} className="relative group">
              <div className="w-16 h-16 rounded border border-slate-700 overflow-hidden bg-slate-800 flex items-center justify-center">
                {url ? (
                  <img src={url} alt={`Image #${num}`} className="w-full h-full object-cover" />
                ) : (
                  <span className="text-[10px] text-gray-600">#{num}</span>
                )}
              </div>
              <span className="absolute bottom-0 left-0 right-0 text-center text-[9px] text-gray-400 bg-slate-900/80 leading-4">
                #{num}
              </span>
              {isPending && (
                <button
                  type="button"
                  onClick={() => handleRemovePending(num)}
                  className="absolute -top-1 -right-1 w-4 h-4 bg-red-600 rounded-full flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity"
                >
                  <X className="w-2.5 h-2.5 text-white" />
                </button>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
