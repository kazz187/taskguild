import { useState } from 'react'
import { InteractionType } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import type { Interaction } from '@taskguild/proto/taskguild/v1/interaction_pb.ts'
import {
  MessageSquare,
  Send,
  Shield,
} from 'lucide-react'
import { ConnectionIndicator } from './ConnectionIndicator.tsx'
import { Button } from '../atoms/index.ts'
import { Input } from '../atoms/index.ts'
import type { ConnectionStatus } from '@/hooks/useEventSubscription'

/* ─── Helpers ─── */

export function formatTime(ts: { seconds: bigint; nanos: number }): string {
  const d = new Date(Number(ts.seconds) * 1000)
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
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
        <Input
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); handleSend() } }}
          inputSize="md"
          className="flex-1 bg-slate-900"
          placeholder={hasPendingText ? 'Type your response...' : 'Send a message...'}
        />
        <Button
          variant="primary"
          size="md"
          iconOnly
          icon={<Send />}
          onClick={handleSend}
          disabled={!canSend}
        />
      </div>
    </div>
  )
}
