import { useEffect, useRef, useState } from "react"
import Editor from "@monaco-editor/react"
import "./VersionHistory.css"

interface HistoryEntry {
  version: number
  saved_at: string
  user_id: string
}

interface Props {
  roomId:  string
  fileId:  string
  role:    string
  apiBase: string
  onClose: () => void
}

export default function VersionHistory({ roomId, fileId, role, apiBase, onClose }: Props) {
  const [entries, setEntries]                 = useState<HistoryEntry[]>([])
  const [loading, setLoading]                 = useState(true)
  const [selectedVersion, setSelectedVersion] = useState<number | null>(null)
  const [previewText, setPreviewText]         = useState<string | null>(null)
  const [previewLoading, setPreviewLoading]   = useState(false)
  const [copied, setCopied]                   = useState(false)
  const panelRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    fetch(`${apiBase}/api/rooms/${roomId}/files/${fileId}/history`, {
      credentials: "include",
    })
      .then(r => r.json())
      .then(data => {
        const sorted = [...(data.history || [])].sort((a, b) => b.version - a.version)
        setEntries(sorted)
      })
      .finally(() => setLoading(false))
  }, [roomId, fileId, apiBase])

  function handleSelect(version: number) {
    if (selectedVersion === version) return
    setSelectedVersion(version)
    setPreviewText(null)
    setPreviewLoading(true)
    setCopied(false)
    fetch(`${apiBase}/api/rooms/${roomId}/files/${fileId}/history/${version}`, {
      credentials: "include",
    })
      .then(r => r.json())
      .then(data => setPreviewText(data.text ?? ""))
      .finally(() => setPreviewLoading(false))
  }

  function handleCopy() {
    if (!previewText) return
    navigator.clipboard.writeText(previewText).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (panelRef.current && !panelRef.current.contains(e.target as Node)) {
        onClose()
      }
    }
    document.addEventListener("mousedown", handleClick)
    return () => document.removeEventListener("mousedown", handleClick)
  }, [onClose])

  function formatDate(iso: string) {
    const d = new Date(iso)
    return d.toLocaleString(undefined, {
      month:  "short",
      day:    "numeric",
      hour:   "2-digit",
      minute: "2-digit",
    })
  }

  return (
    <div className="vh-overlay">
      <div className="vh-panel" ref={panelRef}>
        <div className="vh-header">
          <span className="vh-title">Version History</span>
          <button className="vh-close" onClick={onClose}>✕</button>
        </div>

        <div className="vh-body">
          <div className="vh-list">
            {loading && <p className="vh-hint">Loading...</p>}
            {!loading && entries.length === 0 && (
              <p className="vh-hint">No saved versions yet.</p>
            )}
            {entries.map(e => (
              <div
                key={e.version}
                className={`vh-card ${selectedVersion === e.version ? "vh-card--active" : ""}`}
                onClick={() => handleSelect(e.version)}
              >
                <div className="vh-card-top">
                  <span className="vh-card-version">v{e.version}</span>
                  <span className="vh-card-date">{formatDate(e.saved_at)}</span>
                </div>
                <div className="vh-card-bottom">
                  <span className="vh-card-user">{e.user_id.slice(0, 8)}...</span>
                </div>
              </div>
            ))}
          </div>

          <div className="vh-preview">
            {!selectedVersion && (
              <p className="vh-hint vh-hint--center">Click a version to preview</p>
            )}
            {selectedVersion && previewLoading && (
              <p className="vh-hint vh-hint--center">Loading preview...</p>
            )}
            {selectedVersion && !previewLoading && previewText !== null && (
              <>
                <div className="vh-preview-toolbar">
                  <span className="vh-preview-label">v{selectedVersion} preview</span>
                  <button className="vh-copy-btn" onClick={handleCopy}>
                    {copied ? "✓ Copied" : "Copy"}
                  </button>
                </div>
                <div className="vh-preview-editor">
                  <Editor
                    height="100%"
                    defaultLanguage="plaintext"
                    value={previewText}
                    options={{
                      readOnly:             true,
                      minimap:              { enabled: false },
                      fontSize:             13,
                      scrollBeyondLastLine: false,
                      lineNumbers:          "on",
                    }}
                    theme="vs-dark"
                  />
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}