import { useEffect, useState, type FormEvent } from 'react'
import { createUser, deleteUser, listUsers } from '../api'
import type { Role, User } from '../types'

export default function AdminUsers({ currentUserId }: { currentUserId: number }) {
  const [users, setUsers] = useState<User[]>([])
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<Role>('viewer')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function load() {
    try {
      setUsers(await listUsers())
    } catch {
      setError('Accounts konnten nicht geladen werden.')
    }
  }

  useEffect(() => {
    load()
  }, [])

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setBusy(true)
    try {
      await createUser(username, password, role)
      setUsername('')
      setPassword('')
      setRole('viewer')
      await load()
    } catch {
      setError('Account konnte nicht angelegt werden (Benutzername vergeben oder Passwort zu kurz).')
    } finally {
      setBusy(false)
    }
  }

  async function handleDelete(u: User) {
    if (!confirm(`Account "${u.username}" wirklich löschen?`)) return
    try {
      await deleteUser(u.id)
      await load()
    } catch {
      alert('Löschen fehlgeschlagen (evtl. letzter Vollzugriff-Account).')
    }
  }

  return (
    <div className="panel">
      <h2>Accounts verwalten</h2>
      <table className="proc-table">
        <thead>
          <tr>
            <th>Benutzername</th>
            <th>Rolle</th>
            <th>Angelegt</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {users.map((u) => (
            <tr key={u.id}>
              <td>{u.username}</td>
              <td>{u.role === 'full' ? 'Vollzugriff' : 'Betrachter'}</td>
              <td>{new Date(u.createdAt).toLocaleString('de-DE')}</td>
              <td>
                {u.id !== currentUserId && (
                  <button className="danger" onClick={() => handleDelete(u)}>
                    Löschen
                  </button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <h3>Neuen Account anlegen</h3>
      <form onSubmit={handleCreate} className="user-form">
        <input placeholder="Benutzername" value={username} onChange={(e) => setUsername(e.target.value)} required />
        <input
          type="password"
          placeholder="Passwort (min. 8 Zeichen)"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
        />
        <select value={role} onChange={(e) => setRole(e.target.value as Role)}>
          <option value="viewer">Betrachter (nur ansehen)</option>
          <option value="full">Vollzugriff (ansehen + steuern)</option>
        </select>
        <button type="submit" disabled={busy}>
          Anlegen
        </button>
      </form>
      {error && <div className="error">{error}</div>}
    </div>
  )
}
