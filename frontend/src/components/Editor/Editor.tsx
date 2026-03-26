import { useState, useEffect, useRef } from "react"
import MonacoEditor from "@monaco-editor/react"
import type { editor as MonacoEditorType } from "monaco-editor"
import * as monaco from "monaco-editor"
import { useYjs } from "../../hooks/useYjs"
import { StatusBar } from "../StatusBar/StatusBar"
import { KeyMod, KeyCode } from "monaco-editor"

interface Props {
  roomId:        string
  fileId:        string
  role:          "owner" | "editor" | "viewer"
  language:      string
  currentUserId: string
  currentName:   string
  currentColor:  string
  onSaveReady:   (saveFn: () => void) => void
}

export function Editor({ roomId, fileId, role, language, currentUserId, currentName, currentColor, onSaveReady }: Props) {
  const [editorInstance, setEditorInstance] =
    useState<MonacoEditorType.IStandaloneCodeEditor | null>(null)

  const decorationsRef = useRef<Map<number, string[]>>(new Map())
  const isRenderingRef = useRef(false)

  const { status, save, providerRef } = useYjs({
    roomId,
    fileId,
    monacoEditor:  editorInstance,
    currentUserId,
    currentName,
    currentColor,
  })

  useEffect(() => {
    onSaveReady(save)
  }, [save, onSaveReady])

  useEffect(() => {
    if (!editorInstance) return
    const disposable = editorInstance.addAction({
      id:          "save-document",
      label:       "Save Document",
      keybindings: [KeyMod.CtrlCmd | KeyCode.KeyS],
      run:         () => save(),
    })
    return () => disposable.dispose()
  }, [editorInstance, save])

  useEffect(() => {
    if (!editorInstance || !providerRef.current) return

    const disposable = editorInstance.onDidChangeCursorSelection((e) => {
      const provider = providerRef.current
      if (!provider) return

      const { positionLineNumber, positionColumn } = e.selection
      const hasSelection = !e.selection.isEmpty()

      provider.awareness.setLocalStateField("cursor", {
        lineNumber: positionLineNumber,
        column:     positionColumn,
      })

      provider.awareness.setLocalStateField("selection", hasSelection ? {
        startLine: e.selection.startLineNumber,
        startCol:  e.selection.startColumn,
        endLine:   e.selection.endLineNumber,
        endCol:    e.selection.endColumn,
      } : null)
    })

    return () => disposable.dispose()
  }, [editorInstance, providerRef.current])

  useEffect(() => {
    if (!editorInstance || !providerRef.current) return

    const provider = providerRef.current

    const renderCursors = () => {
      if (isRenderingRef.current) return
      isRenderingRef.current = true
      try {
        const states  = provider.awareness.getStates()
        const localId = provider.awareness.clientID

        states.forEach((state, clientId) => {
          if (clientId === localId) return
          if (!state.user || !state.cursor) return

          const { color, name } = state.user
          const { lineNumber, column } = state.cursor

          const newDecorations: MonacoEditorType.IModelDeltaDecoration[] = []

          newDecorations.push({
            range: new monaco.Range(lineNumber, column, lineNumber, column),
            options: {
              className:              `remote-cursor-${clientId}`,
              beforeContentClassName: `remote-cursor-label-${clientId}`,
              stickiness: monaco.editor.TrackedRangeStickiness.NeverGrowsWhenTypingAtEdges,
            },
          })

          if (state.selection) {
            const { startLine, startCol, endLine, endCol } = state.selection
            newDecorations.push({
              range: new monaco.Range(startLine, startCol, endLine, endCol),
              options: {
                className: `remote-selection-${clientId}`,
                stickiness: monaco.editor.TrackedRangeStickiness.NeverGrowsWhenTypingAtEdges,
              },
            })
          }

          const styleId = `remote-cursor-style-${clientId}`
          if (!document.getElementById(styleId)) {
            const style       = document.createElement("style")
            style.id          = styleId
            style.textContent = `
              .remote-cursor-${clientId} {
                border-left: 2px solid ${color};
              }
              .remote-cursor-label-${clientId}::before {
                content: "${name}";
                background: ${color};
                color: #fff;
                font-size: 10px;
                font-family: var(--font-sans);
                padding: 1px 4px;
                border-radius: 2px;
                position: absolute;
                top: -18px;
                white-space: nowrap;
                pointer-events: none;
              }
              .remote-selection-${clientId} {
                background: ${color}33;
              }
            `
            document.head.appendChild(style)
          }

          const prev = decorationsRef.current.get(clientId) ?? []
          const next = editorInstance.deltaDecorations(prev, newDecorations)
          decorationsRef.current.set(clientId, next)
        })

        decorationsRef.current.forEach((ids, clientId) => {
          if (!states.has(clientId) || clientId === localId) {
            editorInstance.deltaDecorations(ids, [])
            decorationsRef.current.delete(clientId)
            document.getElementById(`remote-cursor-style-${clientId}`)?.remove()
          }
        })
      } finally {
        isRenderingRef.current = false
      }
    }

    provider.awareness.on("change", renderCursors)
    return () => provider.awareness.off("change", renderCursors)
  }, [editorInstance, providerRef.current])

  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100%" }}>
      <MonacoEditor
        height="calc(100% - 32px)"
        language={language}
        theme="vs-dark"
        onMount={(editor) => setEditorInstance(editor)}
        options={{
          fontSize:             14,
          minimap:              { enabled: false },
          scrollBeyondLastLine: false,
          readOnly:             role === "viewer",
        }}
      />
      <StatusBar status={status} />
    </div>
  )
}