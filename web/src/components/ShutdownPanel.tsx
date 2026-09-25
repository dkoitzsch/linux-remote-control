import { useEffect, useState } from 'react'
import { cancelShutdown, getShutdownState, scheduleShutdown } from '../api'
import type { ShutdownState } from '../types'
import { formatDuration } from '../format'

const DEFAULT_MESSAGE = 'Dieser Computer wird gleich heruntergefahren. Bitte speichere deine Arbeit.'

export default function ShutdownPanel() {
  const [state, setState] = useState<ShutdownState | null>(null)
  const [delayMinutes, setDelayMinutes] = useState(5)
  const [message, setMessage] = useState(DEFAULT_MESSAGE)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [now, setNow] = useState(Date.now())

  async function load() {
    try {
      const s = await getShutdownState()
      setState(s)
    } catch {
      // ignore transient errors, next poll will retry
    }
  }

  useEffect(() => {
    load()
    const t = setInterval(load, 2000)
    return () => clearInterval(t)
  }, [])

  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(t)
  }, [])

  async function handleSchedule() {
    setBusy(true)
    setError(null)
    try {
      const s = await scheduleShutdown(delayMinutes * 60, message.trim() || DEFAULT_MESSAGE)
      setState(s)
    } catch {
      setError('Herunterfahren konnte nicht geplant werden.')
    } finally {
      setBusy(false)
    }
  }

  async function handleCancel() {
    setBusy(true)
    setError(null)
    try {
      await cancelShutdown()
      setState({ active: false })
    } catch {
      setError('Abbrechen fehlgeschlagen.')
    } finally {
      setBusy(false)
    }
  }

  const remainingSeconds = state?.active && state.at ? Math.max(0, Math.round((new Date(state.at).getTime() - now) / 1000)) : 0

  return (
    <div className="panel">
      <h2>Herunterfahren</h2>

      {state?.active ? (
        <div className="shutdown-active">
          <p>
            Geplantes Herunterfahren in <strong>{formatDuration(remainingSeconds)}</strong>
            {state.requestedBy && <> (von {state.requestedBy})</>}
          </p>
          <p className="hint">Nachricht: {state.message}</p>
          <button className="danger" onClick={handleCancel} disabled={busy}>
            Herunterfahren abbrechen
          </button>
        </div>
      ) : (
        <>
          <label>
            Verzögerung (Minuten)
            <input
              type="number"
              min={1}
              max={1440}
              value={delayMinutes}
              onChange={(e) => setDelayMinutes(Number(e.target.value))}
            />
          </label>
          <label>
            Nachricht im Countdown-Fenster
            <textarea rows={3} value={message} onChange={(e) => setMessage(e.target.value)} />
          </label>
          <button className="danger" onClick={handleSchedule} disabled={busy}>
            {busy ? 'Planen…' : 'Herunterfahren planen'}
          </button>
        </>
      )}
      {error && <div className="error">{error}</div>}
    </div>
  )
}
