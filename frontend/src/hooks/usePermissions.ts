import { useEffect } from "react"
import { useNavigate } from "react-router-dom"

interface PermissionEvent {
  type:    "removed" | "role_changed" | "room_deleted"
  user_id?: string
  role?:    string
}

interface UsePermissionsOptions {
  roomId:        string
  currentUserId: string
  onRoleChanged: (newRole: string) => void
}

export function usePermissions({ roomId, currentUserId, onRoleChanged }: UsePermissionsOptions) {
  const navigate = useNavigate()

  useEffect(() => {
    if (!roomId || !currentUserId) return

    const wsUrl = import.meta.env.VITE_WS_URL ?? "ws://localhost:8080"
    const ws    = new WebSocket(`${wsUrl}/ws/${roomId}/permissions`)

    ws.onmessage = (e) => {
      const event: PermissionEvent = JSON.parse(e.data)

      // Room deleted — everyone gets kicked regardless of who they are
      if (event.type === "room_deleted") {
        navigate("/dashboard")
        return
      }

      // These events are user-specific — only react if it's about the current user
      if (event.user_id !== currentUserId) return

      if (event.type === "removed") {
        navigate("/dashboard")
      }

      if (event.type === "role_changed" && event.role) {
        onRoleChanged(event.role)
      }
    }

    ws.onerror = (err) => console.error("permissions ws error:", err)

    return () => ws.close()
  }, [roomId, currentUserId])
}