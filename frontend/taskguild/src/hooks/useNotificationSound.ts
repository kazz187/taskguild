import { useEffect, useRef } from 'react'

// ── Singleton AudioContext & cached AudioBuffer ──────────────────────
// Using Web Audio API instead of HTMLAudioElement so that playing a short
// notification sound does NOT acquire Android's system audio focus, which
// would otherwise pause music apps like Spotify / YouTube Music.

let audioCtx: AudioContext | null = null
let bufferPromise: Promise<AudioBuffer> | null = null

function getContext(): AudioContext {
  if (!audioCtx) {
    audioCtx = new AudioContext()
  }
  return audioCtx
}

function loadBuffer(): Promise<AudioBuffer> {
  if (!bufferPromise) {
    bufferPromise = fetch('/bell.mp3')
      .then((res) => res.arrayBuffer())
      .then((arr) => getContext().decodeAudioData(arr))
      .catch((err) => {
        // Allow retry on next play attempt
        bufferPromise = null
        throw err
      })
  }
  return bufferPromise
}

async function playBell(): Promise<void> {
  try {
    const ctx = getContext()
    if (ctx.state === 'suspended') {
      await ctx.resume()
    }
    const buffer = await loadBuffer()
    const source = ctx.createBufferSource()
    source.buffer = buffer
    source.connect(ctx.destination)
    source.start(0)
  } catch {
    // Silently ignore – notification sound is non-critical
  }
}

/**
 * Plays a notification bell sound whenever `count` increases.
 *
 * Uses Web Audio API (AudioContext) instead of HTMLAudioElement to avoid
 * stealing audio focus on Android, which would pause other media apps.
 */
export function useNotificationSound(count: number): void {
  const prevCountRef = useRef(count)

  useEffect(() => {
    if (count > prevCountRef.current) {
      playBell()
    }
    prevCountRef.current = count
  }, [count])
}
