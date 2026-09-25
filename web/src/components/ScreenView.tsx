import { useState } from 'react'
import { screenshotUrl, streamUrl } from '../api'

export default function ScreenView() {
  const [live, setLive] = useState(true)
  const [fps, setFps] = useState(2)
  const [quality, setQuality] = useState(60)
  const [snapshotUrl, setSnapshotUrl] = useState<string | null>(null)
  const [imgKey, setImgKey] = useState(0)

  function refreshSnapshot() {
    setSnapshotUrl(screenshotUrl())
  }

  return (
    <div className="screen-view">
      <div className="screen-controls">
        <label className="inline">
          <input type="checkbox" checked={live} onChange={(e) => setLive(e.target.checked)} />
          Live-Stream
        </label>
        {live ? (
          <>
            <label className="inline">
              FPS
              <select value={fps} onChange={(e) => setFps(Number(e.target.value))}>
                {[1, 2, 3, 5, 8, 10].map((v) => (
                  <option key={v} value={v}>
                    {v}
                  </option>
                ))}
              </select>
            </label>
            <label className="inline">
              Qualität
              <select value={quality} onChange={(e) => setQuality(Number(e.target.value))}>
                {[40, 60, 75, 90].map((v) => (
                  <option key={v} value={v}>
                    {v}%
                  </option>
                ))}
              </select>
            </label>
            <button onClick={() => setImgKey((k) => k + 1)}>Neu verbinden</button>
          </>
        ) : (
          <button onClick={refreshSnapshot}>Screenshot aktualisieren</button>
        )}
      </div>
      <div className="screen-frame">
        {live ? (
          <img key={imgKey} src={streamUrl(fps, quality)} alt="Live-Bildschirm" />
        ) : snapshotUrl ? (
          <img src={snapshotUrl} alt="Screenshot" />
        ) : (
          <div className="screen-placeholder" onClick={refreshSnapshot}>
            Klicken für Screenshot
          </div>
        )}
      </div>
    </div>
  )
}
