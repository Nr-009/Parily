import { useState, useEffect, useRef, useCallback } from "react"
import { useParams, useNavigate } from "react-router-dom"
import { useAuth, apiFetch } from "../context/AuthContext"
import { useRoomSocket, getUserColor } from "../hooks/useRoomSocket"
import { Sidebar } from "../components/Sidebar/sidebar"
import { Editor } from "../components/Editor/Editor"
import "./room.css"

export interface File {
  id:        string
  name:      string
  language:  string
  parent_id: string | null
  is_folder: boolean
  is_active: boolean
}

interface Member {
  user_id: string
  name:    string
  role:    string
}

export default function RoomPage() {
  const { roomId }                     = useParams<{ roomId: string }>()
  const { user, loading: authLoading } = useAuth()
  const navigate                       = useNavigate()

  const [role, setRole]             = useState<string | null>(null)
  const [files, setFiles]           = useState<File[]>([])
  const [members, setMembers]       = useState<Member[]>([])
  const [activeFile, setActiveFile] = useState<File | null>(null)
  const [loading, setLoading]       = useState(true)
  const [error, setError]           = useState("")

  // saveRef is set by Editor via onSaveReady — lets RoomPage trigger a save
  // before switching files without having to pass useYjs state up the tree
  const saveRef      = useRef<(() => void) | null>(null)
  const activeFileRef = useRef<File | null>(null)

  // keep ref in sync so socket callbacks always see current activeFile
  useEffect(() => {
    activeFileRef.current = activeFile
  }, [activeFile])

  useEffect(() => {
    const load = async () => {
      try {
        const [roleData, filesData, membersData] = await Promise.all([
          apiFetch(`/api/rooms/${roomId}/role`),
          apiFetch(`/api/rooms/${roomId}/files`),
          apiFetch(`/api/rooms/${roomId}/members`),
        ])
        setRole(roleData.role)

        const allFiles: File[] = filesData.files ?? []
        setFiles(allFiles)
        setMembers(membersData.members ?? [])

        // only auto-select the first active, non-folder file
        const firstFile = allFiles.find(f => f.is_active && !f.is_folder)
        if (firstFile) setActiveFile(firstFile)
      } catch {
        setError("You do not have access to this room.")
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [roomId])

  const currentUserId = authLoading ? "" : (user?.id ?? "")
  const currentName   = user?.name ?? user?.email ?? ""
  const currentColor  = currentUserId ? getUserColor(currentUserId) : "#fff"

  const { onlineUsers } = useRoomSocket({
    roomId:         roomId!,
    currentUserId,
    currentName,
    onRoleChanged:  (newRole) => setRole(newRole),
    onFilesUpdated: (updated) => {
      setFiles(updated)
      const current = activeFileRef.current
      if (current && !updated.find(f => f.id === current.id)?.is_active) {
        saveRef.current?.()
        setActiveFile(null)
      }
    },
  })

  // handleFileClick is called by FileTree when a file node is clicked.
  // Folders are ignored. For files: force-save current file then switch.
  const handleFileClick = useCallback((file: File) => {
    if (file.is_folder) return
    if (!file.is_active) return
    if (file.id === activeFile?.id) return
    saveRef.current?.()
    setActiveFile(file)
  }, [activeFile])

  // handleFilesChange is called by FileTree after create/toggle/rename
  // to keep the files list in sync without a full refetch.
  const handleFilesChange = useCallback((updated: File[]) => {
    setFiles(updated)
    // if activeFile was toggled inactive, clear it
    if (activeFile && updated.find(f => f.id === activeFile.id)?.is_active === false) {
      saveRef.current?.()
      setActiveFile(null)
    }
  }, [activeFile])

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
          onlineUsers={onlineUsers}
          onFileClick={handleFileClick}
          onMembersChange={setMembers}
          onFilesChange={handleFilesChange}
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
              currentUserId={currentUserId}
              currentName={currentName}
              currentColor={currentColor}
              onSaveReady={(saveFn) => { saveRef.current = saveFn }}
            />
          )}
        </div>
      </div>
    </div>
  )
}