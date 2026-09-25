import { useEffect, useState } from 'react'
import type { User, Status } from '../types'
import { getStatus, logout } from '../api'
import ScreenView from './ScreenView'
import ProcessList from './ProcessList'
import NotifyPanel from './NotifyPanel'
import ShutdownPanel from './ShutdownPanel'
import AdminUsers from './AdminUsers'

type Tab = 'screen' | 'processes' | 'control' | 'admin'

export default function Dashboard({ user, onLogout }: { user: User; onLogout: () => void }) {
  const canControl = user.role === 'full'
  const [tab, setTab] = useState<Tab>('screen')
  const [status, setStatus] = useState<Status | null>(null)

  useEffect(() => {
    const load = () => getStatus().then(setStatus).catch(() => setStatus(null))
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [])

  async function handleLogout() {
    await logout()
    onLogout()
  }

  return (
    <div className="dashboard">
      <header>
        <h1>Fernzugriff-Portal</h1>
        <div className="header-right">
          <span className={`status-dot ${status?.helperConnected ? 'ok' : 'bad'}`} />
          <span className="hint">{status?.helperConnected ? 'Kind-PC verbunden' : 'Kind-PC nicht verbunden'}</span>
          <span className="user-badge">
            {user.username} ({user.role === 'full' ? 'Vollzugriff' : 'Betrachter'})
          </span>
          <button onClick={handleLogout}>Abmelden</button>
        </div>
      </header>

      <nav className="tabs">
        <button className={tab === 'screen' ? 'active' : ''} onClick={() => setTab('screen')}>
          Bildschirm
        </button>
        <button className={tab === 'processes' ? 'active' : ''} onClick={() => setTab('processes')}>
          Prozesse
        </button>
        {canControl && (
          <button className={tab === 'control' ? 'active' : ''} onClick={() => setTab('control')}>
            Nachricht &amp; Herunterfahren
          </button>
        )}
        {canControl && (
          <button className={tab === 'admin' ? 'active' : ''} onClick={() => setTab('admin')}>
            Accounts
          </button>
        )}
      </nav>

      <main>
        {tab === 'screen' && <ScreenView />}
        {tab === 'processes' && <ProcessList canControl={canControl} />}
        {tab === 'control' && canControl && (
          <div className="control-grid">
            <NotifyPanel />
            <ShutdownPanel />
          </div>
        )}
        {tab === 'admin' && canControl && <AdminUsers currentUserId={user.id} />}
      </main>
    </div>
  )
}
