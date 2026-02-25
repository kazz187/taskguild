import { useState } from 'react'
import { InteractionType, InteractionStatus } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import type { Interaction } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import { Shield, MessageSquare, Bell, CheckCircle } from 'lucide-react'
import { formatTime } from './ChatBubble'
import { MarkdownDescription } from './MarkdownDescription'

export function RequestItem({
  interaction,
  onRespond,
  isRespondPending,
}: {
  interaction: Interaction
  onRespond: (id: string, response: string) => void
  isRespondPending: boolean
}) {
  const [freeText, setFreeText] = useState('')
  const isPending = interaction.status === InteractionStatus.PENDING
  const isResponded = interaction.status === InteractionStatus.RESPONDED

  const icon =
    interaction.type === InteractionType.PERMISSION_REQUEST ? (
      <Shield className="w-4 h-4 text-amber-400" />
    ) : interaction.type === InteractionType.QUESTION ? (
      <MessageSquare className="w-4 h-4 text-blue-400" />
    ) : (
      <Bell className="w-4 h-4 text-gray-400" />
    )

  return (
    <div
      className={`border rounded-lg p-3 ${
        isPending ? 'bg-slate-800 border-amber-500/30' : 'bg-slate-800/40 border-slate-700/50'
      }`}
    >
      {/* Header row: icon + title + timestamp */}
      <div className="flex items-center gap-2">
        <span className="shrink-0">{icon}</span>
        <span className="text-sm font-medium text-white truncate flex-1 min-w-0">
          {interaction.title}
        </span>
        {interaction.createdAt && (
          <span className="text-[10px] text-gray-600 shrink-0">
            {formatTime(interaction.createdAt)}
          </span>
        )}
      </div>

      {/* Description — only shown for pending */}
      {isPending && interaction.description && (
        <div className="mt-1.5 ml-6">
          <MarkdownDescription content={interaction.description} className="text-xs" />
        </div>
      )}

      {/* Action buttons for pending */}
      {isPending && interaction.options.length > 0 && (
        interaction.type === InteractionType.QUESTION ? (
          <div className="flex flex-col gap-1.5 mt-2 ml-6">
            {interaction.options.map((opt) => (
              <button
                key={opt.value}
                onClick={() => onRespond(interaction.id, opt.value)}
                disabled={isRespondPending}
                className="flex flex-col items-start gap-0.5 px-3 py-2 text-left bg-slate-700/60 border border-slate-600 rounded-lg hover:border-blue-500/50 hover:bg-slate-700 transition-colors disabled:opacity-50"
              >
                <span className="text-xs font-medium text-gray-200">{opt.label}</span>
                {opt.description && (
                  <span className="text-[11px] text-gray-400">{opt.description}</span>
                )}
              </button>
            ))}
          </div>
        ) : (
          <div className="flex gap-2 flex-wrap mt-2 ml-6">
            {interaction.options.map((opt) => (
              <button
                key={opt.value}
                onClick={() => onRespond(interaction.id, opt.value)}
                disabled={isRespondPending}
                className="px-3 py-1.5 text-xs bg-slate-700 border border-slate-600 rounded-lg text-gray-200 hover:border-cyan-500/50 hover:text-white transition-colors disabled:opacity-50"
                title={opt.description}
              >
                {opt.label}
              </button>
            ))}
          </div>
        )
      )}

      {/* Free text input for pending with no options */}
      {isPending && interaction.options.length === 0 && (
        <div className="flex gap-2 mt-2 ml-6">
          <input
            value={freeText}
            onChange={(e) => setFreeText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.nativeEvent.isComposing && freeText.trim()) {
                onRespond(interaction.id, freeText.trim())
                setFreeText('')
              }
            }}
            className="flex-1 px-2.5 py-1.5 bg-slate-900 border border-slate-700 rounded-lg text-white text-xs focus:outline-none focus:border-cyan-500 placeholder-gray-600"
            placeholder="Type your response..."
          />
          <button
            onClick={() => {
              if (freeText.trim()) {
                onRespond(interaction.id, freeText.trim())
                setFreeText('')
              }
            }}
            disabled={isRespondPending || !freeText.trim()}
            className="px-3 py-1.5 text-xs bg-cyan-600 text-white rounded-lg disabled:opacity-50 hover:bg-cyan-500 transition-colors"
          >
            Send
          </button>
        </div>
      )}

      {/* Responded inline */}
      {isResponded && interaction.response && (
        <div className="flex items-center gap-1.5 mt-1.5 ml-6">
          <CheckCircle className="w-3 h-3 text-green-400 shrink-0" />
          <span className="text-xs text-green-400 truncate">{interaction.response}</span>
        </div>
      )}
    </div>
  )
}
