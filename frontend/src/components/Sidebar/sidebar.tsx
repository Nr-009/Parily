import { useState } from "react"
import { apiFetch } from "../../context/AuthContext"
import type { OnlineUser } from "../../hooks/useRoomSocket"
import type { File } from "../../pages/RoomPage"
import { MembersList } from "./memberlist"
import { FileTree } from "../FileTree/FileTree"
import "./sidebar.css"

interface Member {
  user_id: string
  name:    string
  role:    string
}

interface Props {
  roomId:          string
  files:           File[]
  activeFile:      File | null
  members:         Member[]
  currentRole:     string
  onlineUsers:     Map<string, OnlineUser>
  onFileClick:     (file: File) => void
  onMembersChange: (members: Member[]) => void
  onFilesChange:   (files: File[]) => void
}

export function Sidebar({
  roomId,
  files,
  activeFile,
  members,
  currentRole,
  onlineUsers,
  onFileClick,
  onMembersChange,
  onFilesChange,
}: Props) {
  const isOwner = currentRole === "owner"

  const [inviteEmail, setInviteEmail] = useState("")
  const [inviteRole, setInviteRole]   = useState<"editor" | "viewer">("editor")
  const [inviting, setInviting]       = useState(false)
  const [inviteError, setInviteError] = useState("")

  const handleInvite = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (!inviteEmail.trim()) return
    setInviteError("")
    setInviting(true)
    try {
      const data = await apiFetch(`/api/rooms/${roomId}/members`, {
        method: "POST",
        body:   JSON.stringify({ email: inviteEmail.trim(), role: inviteRole }),
      })
      // avoid duplicate if member already exists
      const exists = members.some(m => m.user_id === data.user.id)
      if (!exists) {
        onMembersChange([...members, {
          user_id: data.user.id,
          name:    data.user.name,
          role:    inviteRole,
        }])
      } else {
        onMembersChange(members.map(m =>
          m.user_id === data.user.id ? { ...m, role: inviteRole } : m
        ))
      }
      setInviteEmail("")
    } catch (err: any) {
      setInviteError(err.message ?? "Could not invite")
    } finally {
      setInviting(false)
    }
  }

  return (
    <aside className="sidebar">
      <FileTree
        roomId={roomId}
        files={files}
        activeFile={activeFile}
        currentRole={currentRole}
        onFileClick={onFileClick}
        onFilesChange={onFilesChange}
      />

      <div className="sidebar-divider" />

      <div className="sidebar-section">
        <div className="sidebar-label">Members</div>
        <MembersList
          roomId={roomId}
          members={members}
          currentRole={currentRole}
          onlineUsers={onlineUsers}
          onMembersChange={onMembersChange}
        />
        {isOwner && (
          <form className="sidebar-invite" onSubmit={handleInvite}>
            <div className="sidebar-label" style={{ marginTop: "0.75rem" }}>Invite</div>
            <input
              type="email"
              placeholder="email"
              value={inviteEmail}
              onChange={(e) => setInviteEmail(e.target.value)}
              className="sidebar-invite-input"
            />
            <div className="sidebar-invite-row">
              <select
                value={inviteRole}
                onChange={(e) => setInviteRole(e.target.value as "editor" | "viewer")}
                className="sidebar-invite-select"
              >
                <option value="editor">editor</option>
                <option value="viewer">viewer</option>
              </select>
              <button
                type="submit"
                className="sidebar-invite-btn"
                disabled={inviting}
              >
                {inviting ? "..." : "Add"}
              </button>
            </div>
            {inviteError && <p className="sidebar-invite-error">{inviteError}</p>}
          </form>
        )}
      </div>
    </aside>
  )
}