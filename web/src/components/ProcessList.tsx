import { useEffect, useMemo, useState } from 'react'
import { getProcesses, killProcess } from '../api'
import type { Process } from '../types'
import { formatBytes, formatDuration, formatRelativeTime } from '../format'

type Category = 'all' | 'service' | 'gui' | 'other'
type SortKey = 'name' | 'uptimeSeconds' | 'rssBytes' | 'cpuPercent' | 'netTotal'

function netTotal(p: Process): number {
  return p.netBytesSent + p.netBytesRecv
}

function categoryOf(p: Process): Exclude<Category, 'all'> {
  if (p.hasWindow) return 'gui'
  if (p.isService) return 'service'
  return 'other'
}

export default function ProcessList({ canControl }: { canControl: boolean }) {
  const [processes, setProcesses] = useState<Process[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const [nameFilter, setNameFilter] = useState('')
  const [minUptimeMin, setMinUptimeMin] = useState(0)
  const [minRamMB, setMinRamMB] = useState(0)
  const [minNetKB, setMinNetKB] = useState(0)
  const [category, setCategory] = useState<Category>('all')
  const [sortKey, setSortKey] = useState<SortKey>('rssBytes')
  const [sortDesc, setSortDesc] = useState(true)

  async function load() {
    try {
      const p = await getProcesses()
      setProcesses(p)
      setError(null)
    } catch {
      setError('Prozessliste konnte nicht geladen werden. Ist rc-helper verbunden?')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    const t = setInterval(load, 3000)
    return () => clearInterval(t)
  }, [])

  const filtered = useMemo(() => {
    const nf = nameFilter.trim().toLowerCase()
    let out = processes.filter((p) => {
      if (nf && !p.name.toLowerCase().includes(nf) && !(p.cmdline ?? '').toLowerCase().includes(nf)) return false
      if (minUptimeMin > 0 && p.uptimeSeconds < minUptimeMin * 60) return false
      if (minRamMB > 0 && p.rssBytes < minRamMB * 1024 * 1024) return false
      if (minNetKB > 0 && netTotal(p) < minNetKB * 1024) return false
      if (category !== 'all' && categoryOf(p) !== category) return false
      return true
    })
    out = out.sort((a, b) => {
      let cmp = 0
      switch (sortKey) {
        case 'name':
          cmp = a.name.localeCompare(b.name)
          break
        case 'uptimeSeconds':
          cmp = a.uptimeSeconds - b.uptimeSeconds
          break
        case 'rssBytes':
          cmp = a.rssBytes - b.rssBytes
          break
        case 'cpuPercent':
          cmp = a.cpuPercent - b.cpuPercent
          break
        case 'netTotal':
          cmp = netTotal(a) - netTotal(b)
          break
      }
      return sortDesc ? -cmp : cmp
    })
    return out
  }, [processes, nameFilter, minUptimeMin, minRamMB, minNetKB, category, sortKey, sortDesc])

  function toggleSort(key: SortKey) {
    if (key === sortKey) setSortDesc((d) => !d)
    else {
      setSortKey(key)
      setSortDesc(true)
    }
  }

  async function handleKill(p: Process) {
    if (!confirm(`"${p.name}" (PID ${p.pid}) wirklich beenden?`)) return
    try {
      await killProcess(p.pid)
      setTimeout(load, 500)
    } catch {
      alert('Beenden fehlgeschlagen.')
    }
  }

  return (
    <div className="process-list">
      <div className="filters">
        <input
          placeholder="Name / Kommandozeile filtern…"
          value={nameFilter}
          onChange={(e) => setNameFilter(e.target.value)}
        />
        <label className="inline">
          Min. Laufzeit (Min.)
          <input
            type="number"
            min={0}
            value={minUptimeMin}
            onChange={(e) => setMinUptimeMin(Number(e.target.value))}
          />
        </label>
        <label className="inline">
          Min. RAM (MB)
          <input type="number" min={0} value={minRamMB} onChange={(e) => setMinRamMB(Number(e.target.value))} />
        </label>
        <label className="inline">
          Min. Netzwerk (KB)
          <input type="number" min={0} value={minNetKB} onChange={(e) => setMinNetKB(Number(e.target.value))} />
        </label>
        <label className="inline">
          Kategorie
          <select value={category} onChange={(e) => setCategory(e.target.value as Category)}>
            <option value="all">Alle</option>
            <option value="gui">Nur Programme mit UI</option>
            <option value="service">Nur System-Dienste</option>
            <option value="other">Sonstige</option>
          </select>
        </label>
      </div>

      {error && <div className="error">{error}</div>}
      {loading ? (
        <p>Lade…</p>
      ) : (
        <table className="proc-table">
          <thead>
            <tr>
              <th onClick={() => toggleSort('name')}>Name</th>
              <th>PID</th>
              <th onClick={() => toggleSort('uptimeSeconds')}>Laufzeit</th>
              <th onClick={() => toggleSort('rssBytes')}>RAM</th>
              <th onClick={() => toggleSort('cpuPercent')}>CPU</th>
              <th onClick={() => toggleSort('netTotal')}>Netzwerk (Σ)</th>
              <th>Netzwerk zuletzt</th>
              <th>Typ</th>
              {canControl && <th>Aktion</th>}
            </tr>
          </thead>
          <tbody>
            {filtered.map((p) => (
              <tr key={p.pid}>
                <td title={p.cmdline}>{p.name}</td>
                <td>{p.pid}</td>
                <td>{formatDuration(p.uptimeSeconds)}</td>
                <td>{formatBytes(p.rssBytes)}</td>
                <td>{p.cpuPercent.toFixed(1)}%</td>
                <td>{formatBytes(netTotal(p))}</td>
                <td>{formatRelativeTime(p.netLastActive)}</td>
                <td>
                  {p.hasWindow && <span className="badge badge-gui">UI</span>}
                  {p.isService && <span className="badge badge-service">Dienst</span>}
                  {!p.hasWindow && !p.isService && <span className="badge">–</span>}
                </td>
                {canControl && (
                  <td>
                    <button className="danger" onClick={() => handleKill(p)}>
                      Beenden
                    </button>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <p className="hint">{filtered.length} von {processes.length} Prozessen angezeigt.</p>
    </div>
  )
}
