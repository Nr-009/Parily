import { useState, useEffect, useRef, useCallback } from "react"
import { useParams, useNavigate } from "react-router-dom"
import { useAuth, apiFetch } from "../context/AuthContext"
import { useRoomSocket, getUserColor } from "../hooks/useRoomSocket"
import type { ExecutionResult } from "../hooks/useRoomSocket"
import { Sidebar } from "../components/Sidebar/sidebar"
import { Editor } from "../components/Editor/Editor"
import { OutputPanel } from "../components/OutputPanel/OutputPanel"
import { Group, Panel, Separator } from "react-resizable-panels"
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

type RunState = "idle" | "executing" | "done-ok" | "done-err"

export default function RoomPage() {
  const { roomId }                     = useParams<{ roomId: string }>()
  const { user, loading: authLoading } = useAuth()
  const navigate                       = useNavigate()

  const [role, setRole]                 = useState<string | null>(null)
  const [roomName, setRoomName]         = useState<string>("")
  const [renamingRoom, setRenamingRoom] = useState(false)
  const [files, setFiles]               = useState<File[]>([])
  const [members, setMembers]           = useState<Member[]>([])
  const [activeFile, setActiveFile]     = useState<File | null>(null)
  const [loading, setLoading]           = useState(true)
  const [error, setError]               = useState("")

  const [runStateByFileId, setRunStateByFileId] = useState<Map<string, RunState>>(new Map())
  const [outputByFileId, setOutputByFileId]     = useState<Map<string, ExecutionResult>>(new Map())

  const saveRef       = useRef<(() => void) | null>(null)
  const activeFileRef = useRef<File | null>(null)

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
        setRoomName(roleData.name ?? "")
        const allFiles: File[] = filesData.files ?? []
        setFiles(allFiles)
        setMembers(membersData.members ?? [])
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

  useEffect(() => {
    if (!activeFile || !roomId) return
    if (outputByFileId.has(activeFile.id)) return

    apiFetch(`/api/rooms/${roomId}/files/${activeFile.id}/execution`)
      .then(res => {
        if (res && res.output !== undefined) {
          setOutputByFileId(prev => new Map(prev).set(activeFile.id, res))
        }
      })
      .catch(() => {})
  }, [activeFile?.id, roomId])

  const currentUserId = authLoading ? "" : (user?.id ?? "")
  const currentName   = user?.name ?? user?.email ?? ""
  const currentColor  = currentUserId ? getUserColor(currentUserId) : "#fff"

  const { onlineUsers, sendMessage } = useRoomSocket({
    roomId:         roomId!,
    currentUserId,
    currentName,
    onRoleChanged:  (newRole) => setRole(newRole),
    onRoomRenamed:  (name) => setRoomName(name),
    onMemberRemoved: (userId) => {
      setMembers(prev => prev.filter(m => m.user_id !== userId))
    },
    onFilesUpdated: (updated) => {
      setFiles(updated)
      const current = activeFileRef.current
      if (current && !updated.find(f => f.id === current.id)?.is_active) {
        saveRef.current?.()
        setActiveFile(null)
      }
    },
    onExecuting: (fileId) => {
      setRunStateByFileId(prev => new Map(prev).set(fileId, "executing"))
    },
    onExecutionDone: (fileId, result) => {
      setRunStateByFileId(prev =>
        new Map(prev).set(fileId, result.exit_code === 0 ? "done-ok" : "done-err")
      )
      if (result.truncated) {
        apiFetch(`/api/rooms/${roomId}/files/${fileId}/execution`)
          .then(res => {
            if (res) setOutputByFileId(prev => new Map(prev).set(fileId, res))
          })
          .catch(() => {})
      } else {
        setOutputByFileId(prev => new Map(prev).set(fileId, result))
      }
    },
    onExecutionError: (fileId, _reason) => {
      setRunStateByFileId(prev => new Map(prev).set(fileId, "idle"))
    },
  })

  const handleFileClick = useCallback((file: File) => {
    if (file.is_folder) return
    if (!file.is_active) return
    if (file.id === activeFile?.id) return
    saveRef.current?.()
    setActiveFile(file)
  }, [activeFile])

  const handleFilesChange = useCallback((updated: File[]) => {
    setFiles(updated)
    if (activeFile && updated.find(f => f.id === activeFile.id)?.is_active === false) {
      saveRef.current?.()
      setActiveFile(null)
    }
  }, [activeFile])

  const handleRun = () => {
    saveRef.current?.()
    if (!activeFile) return
    sendMessage({
      type:         "run_file",
      file_id:      activeFile.id,
      execution_id: crypto.randomUUID(),
    })
  }

  const runState     = activeFile ? (runStateByFileId.get(activeFile.id) ?? "idle") : "idle"
  const activeOutput = activeFile ? (outputByFileId.get(activeFile.id) ?? null) : null

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
          {renamingRoom && role === "owner" ? (
            <input
              className="room-name-input"
              defaultValue={roomName}
              autoFocus
              onBlur={async (e) => {
                const name = e.target.value.trim()
                if (name && name !== roomName) {
                  await apiFetch(`/api/rooms/${roomId}/name`, {
                    method: "PATCH",
                    body: JSON.stringify({ name }),
                  })
                  setRoomName(name)
                }
                setRenamingRoom(false)
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") e.currentTarget.blur()
                if (e.key === "Escape") setRenamingRoom(false)
              }}
            />
          ) : (
            <span
              className="room-name"
              title={role === "owner" ? "Click to rename" : undefined}
              onClick={() => role === "owner" && setRenamingRoom(true)}
            >
              {roomName}
            </span>
          )}
          <span className="room-active-file">{activeFile?.name ?? ""}</span>
          <span className="room-role" data-role={role}>{role}</span>

          {activeFile && !activeFile.is_folder && (role === "owner" || role === "editor") && (
            <button
              className={`run-btn run-btn--${runState}`}
              disabled={runState === "executing"}
              onClick={handleRun}
            >
              {runState === "idle"      && "▶ Run"}
              {runState === "executing" && "⟳"}
              {runState === "done-ok"   && `✓ ${activeOutput?.exit_code ?? 0}`}
              {runState === "done-err"  && `✗ ${activeOutput?.exit_code ?? 1}`}
            </button>
          )}

          {role !== "owner" && (
            <button
              className="room-leave"
              onClick={async () => {
                if (!window.confirm("Leave this room?")) return
                await apiFetch(`/api/rooms/${roomId}/leave`, { method: "DELETE" })
                navigate("/dashboard")
              }}
            >
              Leave
            </button>
          )}
        </div>
        <span className="room-user">{user?.email}</span>
      </header>

      <div style={{ flex: 1, overflow: "hidden", display: "flex" }}>
        <Group orientation="horizontal" style={{ flex: 1 }}>

          <Panel defaultSize="15%" minSize="10%" maxSize="30%">
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
          </Panel>

          <Separator className="resize-handle resize-handle--horizontal" />

          <Panel minSize="40%">
            <Group orientation="vertical">

              <Panel minSize="20%">
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
              </Panel>

              {activeFile && !activeFile.is_folder && (
                <>
                  <Separator className="resize-handle resize-handle--vertical" />
                  <Panel defaultSize="25%" minSize="10%" maxSize="60%" collapsible>
                    <OutputPanel result={activeOutput} />
                  </Panel>
                </>
              )}

            </Group>
          </Panel>

        </Group>
      </div>
    </div>
  )
}