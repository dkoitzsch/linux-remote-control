import { useState, type FormEvent } from 'react'
import { login, ApiError } from '../api'
import type { User } from '../types'

export default function LoginPage({ onLogin }: { onLogin: (u: User) => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setBusy(true)
    try {
      const u = await login(username, password)
      onLogin(u)
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        setError('Benutzername oder Passwort falsch.')
      } else if (e instanceof ApiError && e.status === 429) {
        setError('Zu viele Versuche. Bitte kurz warten.')
      } else {
        setError('Anmeldung fehlgeschlagen.')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login-page">
      <form className="login-card" onSubmit={handleSubmit}>
        <h1>Fernzugriff-Portal</h1>
        <p className="subtitle">Anmeldung erforderlich</p>
        <label>
          Benutzername
          <input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus required />
        </label>
        <label>
          Passwort
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        </label>
        {error && <div className="error">{error}</div>}
        <button type="submit" disabled={busy}>
          {busy ? 'Anmelden…' : 'Anmelden'}
        </button>
      </form>
    </div>
  )
}
