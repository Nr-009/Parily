import { useState, useEffect } from "react"
import { useParams, useNavigate } from "react-router-dom"
import { useAuth, apiFetch } from "../context/AuthContext"
import { usePermissions } from "../hooks/usePermissions"
import { Sidebar } from "../components/Sidebar/sidebar"
import { Editor } from "../components/Editor/Editor"
import "./room.css"

interface File {
  id:       string
  name:     string
  language: string
}

interface Member {
  user_id: string
  name:    string
  role:    string
}

export default function RoomPage() {
  const { roomId }   = useParams<{ roomId: string }>()
  const { user }     = useAuth()
  const navigate     = useNavigate()

  const [role, setRole]           = useState<string | null>(null)
  const [files, setFiles]         = useState<File[]>([])
  const [members, setMembers]     = useState<Member[]>([])
  const [activeFile, setActiveFile] = useState<File | null>(null)
  const [loading, setLoading]     = useState(true)
  const [error, setError]         = useState("")

  useEffect(() => {
    const load = async () => {
      try {
        const [roleData, filesData, membersData] = await Promise.all([
          apiFetch(`/api/rooms/${roomId}/role`),
          apiFetch(`/api/rooms/${roomId}/files`),
          apiFetch(`/api/rooms/${roomId}/members`),
        ])
        setRole(roleData.role)
        setFiles(filesData.files ?? [])
        setMembers(membersData.members ?? [])
        if (filesData.files?.length > 0) {
          setActiveFile(filesData.files[0])
        }
      } catch {
        setError("You do not have access to this room.")
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [roomId])

  // Listen for permission changes via dedicated WebSocket
  usePermissions({
    roomId:        roomId!,
    currentUserId: user?.id ?? "",
    onRoleChanged: (newRole) => setRole(newRole),
  })

  if (loading) {
    return (
      <div className="room-loading">
        <div className="room-spinner" />
      </div>
    )
  }

  if (error || !role) {
    return (
      <div className="room-error">
        <p>{error || "Access denied"}</p>
        <button onClick={() => navigate("/dashboard")}>Back to dashboard</button>
      </div>
    )
  }

  return (
    <div className="room">
      <header className="room-header">
        <button className="room-back" onClick={() => navigate("/dashboard")}>
          ← Dashboard
        </button>
        <div className="room-info">
          <span className="room-active-file">{activeFile?.name ?? ""}</span>
          <span className="room-role" data-role={role}>{role}</span>
        </div>
        <span className="room-user">{user?.email}</span>
      </header>

      <div className="room-body">
        <Sidebar
          roomId={roomId!}
          files={files}
          activeFile={activeFile}
          members={members}
          currentRole={role}
          onFileClick={setActiveFile}
          onMembersChange={setMembers}
        />

        <div className="room-editor">
          {role === "viewer" && (
            <div className="room-viewer-banner">
              Read only — you can view but not edit
            </div>
          )}
          {activeFile && roomId && (
            <Editor
              roomId={roomId}
              fileId={activeFile.id}
              role={role as "owner" | "editor" | "viewer"}
              language={activeFile.language}
            />
          )}
        </div>
      </div>
    </div>
  )
}