import { useState } from 'react'
import { sendNotify } from '../api'

export default function NotifyPanel() {
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [status, setStatus] = useState<string | null>(null)

  async function handleSend() {
    if (!message.trim()) return
    setBusy(true)
    setStatus(null)
    try {
      await sendNotify(message.trim())
      setStatus('Nachricht gesendet – wird auf dem Bildschirm angezeigt, bis "Verstanden" geklickt wird.')
      setMessage('')
    } catch {
      setStatus('Fehler: Nachricht konnte nicht gesendet werden (ist rc-helper verbunden?).')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="panel">
      <h2>Nachricht senden</h2>
      <p className="hint">
        Die Nachricht erscheint als Vollbild-Fenster auf oberster Ebene und bleibt bestehen, bis sie mit
        "Verstanden" bestätigt wird.
      </p>
      <textarea
        rows={4}
        value={message}
        onChange={(e) => setMessage(e.target.value)}
        placeholder="z.B. Bitte melde dich kurz bei mir…"
      />
      <button onClick={handleSend} disabled={busy || !message.trim()}>
        {busy ? 'Senden…' : 'Nachricht senden'}
      </button>
      {status && <div className="status">{status}</div>}
    </div>
  )
}
