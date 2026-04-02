import { useEffect, useRef, useCallback, useState } from "react"
import * as Y from "yjs"
import { WebsocketProvider } from "y-websocket"
import { MonacoBinding } from "y-monaco"
import type { editor } from "monaco-editor"

const SAVE_INTERVAL_MS = 60_000
const SAVE_OP_COUNT    = 100

interface UseYjsOptions {
  roomId:        string
  fileId:        string
  monacoEditor:  editor.IStandaloneCodeEditor | null
  currentUserId: string
  currentName:   string
  currentColor:  string
}

export function useYjs({
  roomId,
  fileId,
  monacoEditor,
  currentUserId,
  currentName,
  currentColor,
}: UseYjsOptions) {
  const ydocRef         = useRef<Y.Doc | null>(null)
  const providerRef     = useRef<WebsocketProvider | null>(null)
  const opCountRef      = useRef(0)
  const sendSnapshotRef = useRef<(() => void) | null>(null)
  const [status, setStatus] = useState<"connected" | "disconnected">("disconnected")

  useEffect(() => {
    if (!monacoEditor || !roomId || !fileId) return

    const wsUrl  = import.meta.env.VITE_WS_URL  ?? "ws://localhost:8080"
    const apiUrl = import.meta.env.VITE_API_URL ?? "http://localhost:8080"

    const ydoc  = new Y.Doc()
    const ytext = ydoc.getText("content")

    const init = async () => {
      try {
        const res = await fetch(
          `${apiUrl}/api/rooms/${roomId}/files/${fileId}/state`,
          { credentials: "include" }
        )
        if (res.ok && res.status !== 204) {
          const buffer = await res.arrayBuffer()
          const state  = new Uint8Array(buffer)
          if (state.length > 0) {
            Y.applyUpdate(ydoc, state)
          }
        }
      } catch (err) {
        console.warn("could not load persisted state:", err)
      }

      const provider = new WebsocketProvider(
        wsUrl,
        `ws/${roomId}/${fileId}`,
        ydoc,
        { connect: true }
      )
      providerRef.current = provider

      provider.awareness.setLocalStateField("user", {
        userId: currentUserId,
        name:   currentName,
        color:  currentColor,
      })

      provider.on("status", ({ status: s }: { status: string }) => {
        setStatus(s === "connected" ? "connected" : "disconnected")
      })

      new MonacoBinding(
        ytext,
        monacoEditor.getModel()!,
        new Set([monacoEditor]),
        provider.awareness
      )

      ydocRef.current = ydoc

      const sendSnapshot = () => {
        const state = Y.encodeStateAsUpdate(ydoc)
        if (state.length === 0) return
        fetch(`${apiUrl}/api/rooms/${roomId}/files/${fileId}/state`, {
          method:      "POST",
          credentials: "include",
          headers:     { "Content-Type": "application/octet-stream" },
          body:        state.buffer as ArrayBuffer,
        }).catch((err) => console.error("snapshot save failed:", err))
        opCountRef.current = 0
      }

      sendSnapshotRef.current = sendSnapshot

      const handleObserve = () => {
        opCountRef.current++
        if (opCountRef.current >= SAVE_OP_COUNT) sendSnapshot()
      }
      ytext.observe(handleObserve)

      const interval = setInterval(() => {
        if (opCountRef.current > 0) sendSnapshot()
      }, SAVE_INTERVAL_MS)

      window.addEventListener("beforeunload", sendSnapshot)

      return () => {
        ytext.unobserve(handleObserve)
        clearInterval(interval)
        window.removeEventListener("beforeunload", sendSnapshot)
        sendSnapshot()
        provider.destroy()
        ydoc.destroy()
        providerRef.current     = null
        sendSnapshotRef.current = null
        setStatus("disconnected")
      }
    }

    let cleanup: (() => void) | undefined
    init().then((fn) => { cleanup = fn })
    return () => { cleanup?.() }

  }, [roomId, fileId, monacoEditor])

  const save = useCallback(() => {
    sendSnapshotRef.current?.()
  }, [])

  return { status, save, providerRef }
}