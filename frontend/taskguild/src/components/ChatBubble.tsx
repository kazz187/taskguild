import { useState } from 'react'
import { InteractionType, InteractionStatus } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import type { Interaction } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import {
  Bot,
  MessageSquare,
  Send,
  Shield,
  Bell,
  CheckCircle,
  Timer,
} from 'lucide-react'
import { MarkdownDescription } from './MarkdownDescription'
import { ConnectionIndicator } from './ConnectionIndicator'
import type { ConnectionStatus } from '@/hooks/useEventSubscription'

/* ─── Helpers ─── */

export function formatTime(ts: { seconds: bigint; nanos: number }): string {
  const d = new Date(Number(ts.seconds) * 1000)
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

/* ─── Chat bubble ─── */

export function ChatBubble({
  interaction,
  onRespond,
  isRespondPending,
}: {
  interaction: Interaction
  onRespond: (id: string, response: string) => void
  isRespondPending: boolean
}) {
  // User message — right-aligned bubble
  if (interaction.type === InteractionType.USER_MESSAGE) {
    return (
      <div className="flex gap-3 justify-end">
        <div className="max-w-[80%] rounded-lg p-3 bg-cyan-600/10 border border-cyan-500/20">
          {interaction.createdAt && (
            <span className="text-[10px] text-gray-600 block mb-1">
              {formatTime(interaction.createdAt)}
            </span>
          )}
          <p className="text-sm text-cyan-300 whitespace-pre-wrap">{interaction.title}</p>
        </div>
      </div>
    )
  }

  // Agent interaction — left-aligned bubble
  const isPending = interaction.status === InteractionStatus.PENDING
  const isResponded = interaction.status === InteractionStatus.RESPONDED
  const isExpired = interaction.status === InteractionStatus.EXPIRED

  const typeIcon = interaction.type === InteractionType.PERMISSION_REQUEST
    ? <Shield className="w-4 h-4 text-amber-400" />
    : interaction.type === InteractionType.QUESTION
    ? <MessageSquare className="w-4 h-4 text-blue-400" />
    : <Bell className="w-4 h-4 text-gray-400" />

  const typeLabel = interaction.type === InteractionType.PERMISSION_REQUEST
    ? 'Permission Request'
    : interaction.type === InteractionType.QUESTION
    ? 'Question'
    : 'Notification'

  return (
    <div className="space-y-2">
      {/* Agent message */}
      <div className="flex gap-3">
        <div className="shrink-0 w-8 h-8 rounded-full bg-cyan-500/10 border border-cyan-500/20 flex items-center justify-center">
          <Bot className="w-4 h-4 text-cyan-400" />
        </div>
        <div className={`flex-1 min-w-0 rounded-lg p-3 ${
          isPending
            ? 'bg-slate-800 border border-amber-500/30'
            : 'bg-slate-800/60 border border-slate-800'
        }`}>
          <div className="flex items-center gap-2 mb-1">
            {typeIcon}
            <span className="text-[11px] text-gray-500">{typeLabel}</span>
            {interaction.createdAt && (
              <span className="text-[10px] text-gray-600 ml-auto">
                {formatTime(interaction.createdAt)}
              </span>
            )}
          </div>
          <p className="text-sm font-medium text-white">{interaction.title}</p>
          {interaction.description && (
            <MarkdownDescription content={interaction.description} className="mt-1" />
          )}

          {/* Inline action buttons for pending interactions */}
          {isPending && interaction.options.length > 0 && (
            interaction.type === InteractionType.QUESTION ? (
              /* Question: vertical card layout with label + description */
              <div className="flex flex-col gap-2 mt-3">
                {interaction.options.map((opt) => (
                  <button
                    key={opt.value}
                    onClick={() => onRespond(interaction.id, opt.value)}
                    disabled={isRespondPending}
                    className="flex flex-col items-start gap-0.5 px-4 py-2.5 text-left bg-slate-700/60 border border-slate-600 rounded-lg hover:border-blue-500/50 hover:bg-slate-700 transition-colors disabled:opacity-50"
                  >
                    <span className="text-sm font-medium text-gray-200">{opt.label}</span>
                    {opt.description && (
                      <span className="text-xs text-gray-400">{opt.description}</span>
                    )}
                  </button>
                ))}
              </div>
            ) : (
              /* Permission request: horizontal button layout */
              <div className="flex gap-2 flex-wrap mt-3">
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
        </div>
      </div>

      {/* User response (if responded to agent interaction) */}
      {isResponded && interaction.response && (
        <div className="flex gap-3 justify-end">
          <div className="max-w-[80%] rounded-lg p-3 bg-cyan-600/10 border border-cyan-500/20">
            <div className="flex items-center gap-1.5 mb-1">
              <CheckCircle className="w-3 h-3 text-green-400" />
              <span className="text-[11px] text-gray-500">Responded</span>
              {interaction.respondedAt && (
                <span className="text-[10px] text-gray-600 ml-auto">
                  {formatTime(interaction.respondedAt)}
                </span>
              )}
            </div>
            <p className="text-sm text-cyan-300">{interaction.response}</p>
          </div>
        </div>
      )}

      {/* Expired indicator */}
      {isExpired && (
        <div className="flex gap-3 justify-end">
          <div className="flex items-center gap-1.5 text-xs text-gray-600">
            <Timer className="w-3 h-3" />
            Expired
          </div>
        </div>
      )}
    </div>
  )
}

/* ─── Input bar (inline) ─── */

export function InputBar({
  pendingInteraction,
  onRespond,
  onSendMessage,
  isRespondPending,
  isSendPending,
  connectionStatus,
  onReconnect,
}: {
  pendingInteraction?: Interaction
  onRespond: (id: string, response: string) => void
  onSendMessage: (message: string) => void
  isRespondPending: boolean
  isSendPending: boolean
  connectionStatus?: ConnectionStatus
  onReconnect?: () => void
}) {
  const [text, setText] = useState('')

  const hasPendingText = pendingInteraction && pendingInteraction.options.length === 0
  const isBusy = isRespondPending || isSendPending
  const canSend = text.trim() && !isBusy

  const handleSend = () => {
    if (!canSend) return
    if (hasPendingText) {
      onRespond(pendingInteraction.id, text.trim())
    } else {
      onSendMessage(text.trim())
    }
    setText('')
  }

  return (
    <div className="pt-2">
      {hasPendingText && (
        <p className="text-[11px] text-amber-400 mb-1.5 flex items-center gap-1">
          {pendingInteraction.type === InteractionType.PERMISSION_REQUEST
            ? <Shield className="w-3 h-3" />
            : <MessageSquare className="w-3 h-3" />
          }
          {pendingInteraction.title}
        </p>
      )}
      <div className="flex gap-2 items-end">
        {connectionStatus && onReconnect && (
          <ConnectionIndicator status={connectionStatus} onReconnect={onReconnect} />
        )}
        <input
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); handleSend() } }}
          className="flex-1 px-4 py-2.5 bg-slate-900 border border-slate-700 rounded-lg text-sm text-white focus:outline-none focus:border-cyan-500 placeholder-gray-600"
          placeholder={hasPendingText ? 'Type your response...' : 'Send a message...'}
        />
        <button
          onClick={handleSend}
          disabled={!canSend}
          className="px-4 py-2.5 bg-cyan-600 hover:bg-cyan-500 text-white rounded-lg transition-colors disabled:opacity-30 disabled:hover:bg-cyan-600"
        >
          <Send className="w-4 h-4" />
        </button>
      </div>
    </div>
  )
}
