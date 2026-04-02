import { useState, useEffect, useRef } from "react"
import { useNavigate } from "react-router-dom"
import type { File } from "../pages/RoomPage"

const COLORS = [
  "#f87171", "#fb923c", "#fbbf24", "#4ade80",
  "#60a5fa", "#a78bfa", "#f472b6",
]

function hashUserId(userId: string): number {
  let hash = 0
  for (let i = 0; i < userId.length; i++) {
    hash = (hash * 31 + userId.charCodeAt(i)) >>> 0
  }
  return hash
}

export function getUserColor(userId: string): string {
  return COLORS[hashUserId(userId) % COLORS.length]
}

export interface ExecutionResult {
  output:      string
  exit_code:   number
  duration_ms: number
  truncated:   boolean
}

interface IncomingEvent {
  type:         string
  user_id?:     string
  role?:        string
  name?:        string
  files?:       File[]
  file_id?:     string
  output?:      string
  exit_code?:   number
  duration_ms?: number
  truncated?:   boolean
  reason?:      string
}

export interface OnlineUser {
  userId:   string
  name:     string
  color:    string
  lastSeen: number
}

interface UseRoomSocketOptions {
  roomId:            string
  currentUserId:     string
  currentName:       string
  onRoleChanged:     (newRole: string) => void
  onFilesUpdated:    (files: File[]) => void
  onRoomRenamed?:    (name: string) => void
  onMemberRemoved?:  (userId: string) => void
  onExecuting?:      (fileId: string) => void
  onExecutionDone?:  (fileId: string, result: ExecutionResult) => void
  onExecutionError?: (fileId: string, reason: string) => void
}

export function useRoomSocket({
  roomId,
  currentUserId,
  currentName,
  onRoleChanged,
  onFilesUpdated,
  onRoomRenamed,
  onMemberRemoved,
  onExecuting,
  onExecutionDone,
  onExecutionError,
}: UseRoomSocketOptions) {
  const navigate                      = useNavigate()
  const [onlineUsers, setOnlineUsers] = useState<Map<string, OnlineUser>>(new Map())
  const wsRef                         = useRef<WebSocket | null>(null)
  const heartbeatRef                  = useRef<ReturnType<typeof setInterval> | null>(null)

  useEffect(() => {
    if (!roomId || !currentUserId) return

    const wsUrl = import.meta.env.VITE_WS_URL ?? "ws://localhost:8080"
    const ws    = new WebSocket(`${wsUrl}/room-ws/${roomId}`)
    wsRef.current = ws

    ws.onopen = () => {
      sendHeartbeat(ws)
      heartbeatRef.current = setInterval(() => sendHeartbeat(ws), 10_000)
    }

    ws.onmessage = (e) => {
      const event: IncomingEvent = JSON.parse(e.data)

      if (event.type === "executing" && event.file_id) {
        onExecuting?.(event.file_id)
        return
      }

      if (event.type === "execution_done" && event.file_id) {
        onExecutionDone?.(event.file_id, {
          output:      event.output      ?? "",
          exit_code:   event.exit_code   ?? 0,
          duration_ms: event.duration_ms ?? 0,
          truncated:   event.truncated   ?? false,
        })
        return
      }

      if (event.type === "execution_error" && event.file_id) {
        onExecutionError?.(event.file_id, event.reason ?? "error")
        return
      }

      if (event.type === "room_deleted") {
        navigate("/dashboard")
        return
      }

      if (event.type === "removed" && event.user_id === currentUserId) {
        navigate("/dashboard")
        return
      }

      if (event.type === "member_left" && event.user_id) {
        if (event.user_id === currentUserId) {
          navigate("/dashboard")
        } else {
          onMemberRemoved?.(event.user_id)
        }
        return
      }

      if (event.type === "role_changed" && event.user_id === currentUserId && event.role) {
        onRoleChanged(event.role)
        return
      }

      if (event.type === "room_renamed" && event.name) {
        onRoomRenamed?.(event.name)
        return
      }

      if (event.type === "files_updated" && event.files) {
        onFilesUpdated(event.files)
        return
      }

      if (event.type === "heartbeat" && event.user_id && event.name) {
        setOnlineUsers((prev) => {
          const next = new Map(prev)
          next.set(event.user_id!, {
            userId:   event.user_id!,
            name:     event.name!,
            color:    getUserColor(event.user_id!),
            lastSeen: Date.now(),
          })
          return next
        })
        return
      }

      if (event.type === "disconnect" && event.user_id) {
        setOnlineUsers((prev) => {
          const next = new Map(prev)
          next.delete(event.user_id!)
          return next
        })
        return
      }
    }

    ws.onerror = (err) => console.error("room ws error:", err)

    return () => {
      if (heartbeatRef.current) clearInterval(heartbeatRef.current)
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({
          type:    "disconnect",
          user_id: currentUserId,
        }))
      }
      ws.close()
    }
  }, [roomId, currentUserId])

  function sendHeartbeat(ws: WebSocket) {
    if (ws.readyState !== WebSocket.OPEN) return
    ws.send(JSON.stringify({
      type:    "heartbeat",
      user_id: currentUserId,
      name:    currentName,
      color:   getUserColor(currentUserId),
    }))
  }

  const sendMessage = (msg: object) => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(msg))
    }
  }

  return { onlineUsers, sendMessage }
}