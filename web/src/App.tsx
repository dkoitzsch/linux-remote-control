import { useEffect, useState } from 'react'
import { me } from './api'
import type { User } from './types'
import LoginPage from './components/LoginPage'
import Dashboard from './components/Dashboard'
import './App.css'

function App() {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    me()
      .then(setUser)
      .finally(() => setLoading(false))
  }, [])

  if (loading) return <div className="app-loading">Lädt…</div>

  if (!user) return <LoginPage onLogin={setUser} />

  return <Dashboard user={user} onLogout={() => setUser(null)} />
}

export default App
