import { useState, useEffect, useCallback } from "react"
import { useNavigate } from "react-router-dom"
import { useAuth, apiFetch } from "../context/AuthContext"
import { useNotificationSocket } from "../hooks/useNotificationSocket"
import "./dashboard.css"

interface Room {
  id:         string
  name:       string
  owner_id:   string
  role:       "owner" | "editor" | "viewer"
  created_at: string
}

interface Toast {
  id:      number
  message: string
}

function ShareModal({ room, onClose }: { room: Room; onClose: () => void }) {
  const [email, setEmail]     = useState("")
  const [role, setRole]       = useState<"editor" | "viewer">("editor")
  const [error, setError]     = useState("")
  const [success, setSuccess] = useState("")
  const [loading, setLoading] = useState(false)

  const handleInvite = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    setError(""); setSuccess(""); setLoading(true)
    try {
      await apiFetch(`/api/rooms/${room.id}/members`, {
        method: "POST",
        body:   JSON.stringify({ email, role }),
      })
      setSuccess(`${email} added as ${role}`)
      setEmail("")
    } catch (err: any) {
      setError(err.message ?? "Failed to invite")
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h3>Share "{room.name}"</h3>
          <button className="modal-close" onClick={onClose}>✕</button>
        </div>
        <form onSubmit={handleInvite} className="share-form">
          <input
            type="email"
            placeholder="colleague@example.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            autoFocus
          />
          <select value={role} onChange={(e) => setRole(e.target.value as "editor" | "viewer")}>
            <option value="editor">Editor</option>
            <option value="viewer">Viewer</option>
          </select>
          <button type="submit" disabled={loading}>
            {loading ? "..." : "Invite"}
          </button>
        </form>
        {error   && <p className="share-error">{error}</p>}
        {success && <p className="share-success">{success}</p>}
        <div className="share-legend">
          <span><strong>Editor</strong> — can read and write</span>
          <span><strong>Viewer</strong> — read only</span>
        </div>
      </div>
    </div>
  )
}

let toastId = 0

export default function DashboardPage() {
  const { user, logout } = useAuth()
  const navigate         = useNavigate()

  const [rooms, setRooms]           = useState<Room[]>([])
  const [loading, setLoading]       = useState(true)
  const [creating, setCreating]     = useState(false)
  const [newName, setNewName]       = useState("")
  const [showCreate, setShowCreate] = useState(false)
  const [shareRoom, setShareRoom]   = useState<Room | null>(null)
  const [toasts, setToasts]         = useState<Toast[]>([])

  const addToast = useCallback((message: string) => {
    const id = ++toastId
    setToasts(prev => [...prev, { id, message }])
    setTimeout(() => {
      setToasts(prev => prev.filter(t => t.id !== id))
    }, 4000)
  }, [])

  useEffect(() => {
    const load = async () => {
      try {
        const data = await apiFetch("/api/rooms")
        setRooms(data.rooms ?? [])
      } catch (err) {
        console.error(err)
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [])

  // ── notification socket ────────────────────────────────────────────────────
  useNotificationSocket({
    enabled: true,
    onRoomInvited: (roomId, roomName, role) => {
      setRooms(prev => {
        const exists = prev.some(r => r.id === roomId)
        if (exists) return prev
        return [{
          id:         roomId,
          name:       roomName,
          owner_id:   "",
          role:       role as "editor" | "viewer",
          created_at: new Date().toISOString(),
        }, ...prev]
      })
      addToast(`You were invited to "${roomName}"`)
    },
    onRoomRenamed: (roomId, name) => {
      setRooms(prev => prev.map(r => r.id === roomId ? { ...r, name } : r))
      addToast(`Room renamed to "${name}"`)
    },
    onRoomDeleted: (roomId, roomName) => {
      setRooms(prev => prev.filter(r => r.id !== roomId))
      addToast(`"${roomName}" was deleted`)
    },
  })

  const handleCreate = async () => {
    if (!newName.trim()) return
    setCreating(true)
    try {
      const data = await apiFetch("/api/rooms", {
        method: "POST",
        body:   JSON.stringify({ name: newName.trim() }),
      })
      setRooms((prev) => [data.room, ...prev])
      setNewName("")
      setShowCreate(false)
      navigate(`/room/${data.room.id}`)
    } catch (err) {
      console.error(err)
    } finally {
      setCreating(false)
    }
  }

  const handleDelete = async (room: Room, e: React.MouseEvent) => {
    e.stopPropagation()
    if (!confirm(`Delete "${room.name}"? This cannot be undone.`)) return
    try {
      await apiFetch(`/api/rooms/${room.id}`, { method: "DELETE" })
      setRooms((prev) => prev.filter((r) => r.id !== room.id))
    } catch (err: any) {
      console.error("could not delete room:", err)
    }
  }

  const handleLogout = async () => {
    await logout()
    navigate("/login")
  }

  const roleColor: Record<string, string> = {
    owner:  "var(--accent-text)",
    editor: "var(--success)",
    viewer: "var(--text-tertiary)",
  }

  return (
    <div className="dash">
      <header className="dash-header">
        <div className="dash-brand">
          <div className="dash-brand-mark">p</div>
          <span className="dash-brand-name">parily</span>
        </div>
        <div className="dash-user">
          <span className="dash-email">{user?.email}</span>
          <button className="dash-logout" onClick={handleLogout}>Sign out</button>
        </div>
      </header>

      <main className="dash-main">
        <div className="dash-toolbar">
          <h1 className="dash-title">Documents</h1>
          <button className="dash-new" onClick={() => setShowCreate(true)}>
            New document
          </button>
        </div>

        {showCreate && (
          <div className="dash-create">
            <input
              autoFocus
              type="text"
              placeholder="Document name"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && handleCreate()}
              className="dash-create-input"
            />
            <button className="dash-create-confirm" onClick={handleCreate} disabled={creating}>
              {creating ? "Creating..." : "Create"}
            </button>
            <button className="dash-create-cancel" onClick={() => setShowCreate(false)}>
              Cancel
            </button>
          </div>
        )}

        {loading ? (
          <div className="dash-loading"><div className="dash-spinner" /></div>
        ) : rooms.length === 0 ? (
          <div className="dash-empty">
            <p>No documents yet.</p>
            <button className="dash-new" onClick={() => setShowCreate(true)}>
              Create your first document
            </button>
          </div>
        ) : (
          <div className="dash-grid">
            {rooms.map((room) => (
              <div key={room.id} className="dash-card">
                <div
                  className="dash-card-body"
                  onClick={() => navigate(`/room/${room.id}`)}
                >
                  <div className="dash-card-icon">
                    <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                      <path d="M3 2h7l3 3v9H3V2z" stroke="currentColor" strokeWidth="1" strokeLinejoin="round"/>
                      <path d="M10 2v3h3" stroke="currentColor" strokeWidth="1" strokeLinejoin="round"/>
                    </svg>
                  </div>
                  <div className="dash-card-info">
                    <span className="dash-card-name">{room.name}</span>
                    <span className="dash-card-date">
                      {new Date(room.created_at).toLocaleDateString(undefined, {
                        month: "short", day: "numeric", year: "numeric",
                      })}
                    </span>
                  </div>
                  <span className="dash-card-role" style={{ color: roleColor[room.role] }}>
                    {room.role}
                  </span>
                </div>

                {room.role === "owner" && (
                  <div className="dash-card-actions">
                    <button
                      className="dash-card-share"
                      onClick={(e) => { e.stopPropagation(); setShareRoom(room) }}
                    >
                      Share
                    </button>
                    <button
                      className="dash-card-delete"
                      onClick={(e) => handleDelete(room, e)}
                      title="Delete room"
                    >
                      Delete
                    </button>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </main>

      {shareRoom && (
        <ShareModal room={shareRoom} onClose={() => setShareRoom(null)} />
      )}

      {/* ── toast notifications ── */}
      {toasts.length > 0 && (
        <div className="dash-toasts">
          {toasts.map(toast => (
            <div key={toast.id} className="dash-toast">
              {toast.message}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}