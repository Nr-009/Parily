import { useEffect, useRef } from "react"

interface NotificationEvent {
  type:      string
  room_id?:  string
  room_name?: string
  role?:     string
  name?:     string
}

interface UseNotificationSocketOptions {
  enabled:        boolean  // only true on dashboard
  onRoomInvited:  (roomId: string, roomName: string, role: string) => void
  onRoomRenamed:  (roomId: string, name: string) => void
  onRoomDeleted:  (roomId: string, roomName: string) => void
}

export function useNotificationSocket({
  enabled,
  onRoomInvited,
  onRoomRenamed,
  onRoomDeleted,
}: UseNotificationSocketOptions) {
  const wsRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    if (!enabled) return

    const wsUrl = import.meta.env.VITE_WS_URL ?? "ws://localhost:8080"
    const ws    = new WebSocket(`${wsUrl}/notify-ws`)
    wsRef.current = ws

    ws.onmessage = (e) => {
      const event: NotificationEvent = JSON.parse(e.data)

      if (event.type === "room_invited" && event.room_id && event.room_name && event.role) {
        onRoomInvited(event.room_id, event.room_name, event.role)
        return
      }

      if (event.type === "room_renamed" && event.room_id && event.name) {
        onRoomRenamed(event.room_id, event.name)
        return
      }

      if (event.type === "room_deleted" && event.room_id && event.room_name) {
        onRoomDeleted(event.room_id, event.room_name)
        return
      }
    }

    ws.onerror = (err) => console.error("notify ws error:", err)

    return () => {
      ws.close()
    }
  }, [enabled])
}