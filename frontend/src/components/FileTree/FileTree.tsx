import { useState, useRef, useEffect, useCallback } from "react"
import { apiFetch } from "../../context/AuthContext"
import type { File } from "../../pages/RoomPage"
import { createTree } from "../../utils/buildTree"
import type { FileNode } from "../../utils/buildTree"
import "./filetree.css"

const LANGUAGES = [
  "plaintext", "python", "javascript", "typescript",
  "go", "java", "c", "cpp", "rust", "ruby",
  "php", "html", "css", "json", "markdown", "yaml", "shell",
]

interface ContextMenu {
  x:        number
  y:        number
  node:     FileNode | null  // null = root level
  isTrash:  boolean
}

interface InlineInput {
  parentId: string | null  // for create
  fileId:   string | null  // for rename
  value:    string
  isFolder: boolean
}

interface Props {
  roomId:        string
  files:         File[]
  activeFile:    File | null
  currentRole:   string
  onFileClick:   (file: File) => void
  onFilesChange: (files: File[]) => void
}

export function FileTree({ roomId, files, activeFile, currentRole, onFileClick, onFilesChange }: Props) {
  const canEdit = currentRole === "owner" || currentRole === "editor"

  const activeTree = createTree(files.filter(f => f.is_active))
  const trashTree  = createTree(files.filter(f => !f.is_active))

  const [expanded,    setExpanded]    = useState<Set<string>>(new Set())
  const [context,     setContext]     = useState<ContextMenu | null>(null)
  const [inline,      setInline]      = useState<InlineInput | null>(null)
  const [langFileId,  setLangFileId]  = useState<string | null>(null)
  const [trashOpen,   setTrashOpen]   = useState(false)
  const contextRef = useRef<HTMLDivElement>(null)
  const inputRef   = useRef<HTMLInputElement>(null)

  // close context menu on outside click
  useEffect(() => {
    if (!context) return
    const handler = (e: MouseEvent) => {
      if (contextRef.current && !contextRef.current.contains(e.target as Node)) {
        setContext(null)
      }
    }
    document.addEventListener("mousedown", handler)
    return () => document.removeEventListener("mousedown", handler)
  }, [context])

  // focus inline input when it appears
  useEffect(() => {
    if (inline) inputRef.current?.focus()
  }, [inline])

  const toggleExpand = (id: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }

  const openContext = (e: React.MouseEvent, node: FileNode | null, isTrash: boolean) => {
    e.preventDefault()
    e.stopPropagation()
    if (!canEdit && !isTrash) return
    setContext({ x: e.clientX, y: e.clientY, node, isTrash })
  }

  // ── mutations ────────────────────────────────────────────────────────────

  const mergeFiles = useCallback((updated: File | File[]) => {
    const arr = Array.isArray(updated) ? updated : [updated]
    onFilesChange(files.map(f => {
      const u = arr.find(u => u.id === f.id)
      return u ?? f
    }))
  }, [files, onFilesChange])

  const addFile = useCallback((file: File) => {
    onFilesChange([...files, file])
  }, [files, onFilesChange])

  const handleCreate = async (name: string) => {
    if (!inline || !name.trim()) { setInline(null); return }
    const isFolder = inline.isFolder
    try {
      const data = await apiFetch(`/api/rooms/${roomId}/files`, {
        method:      "POST",
        credentials: "include",
        body: JSON.stringify({
          name:      name.trim(),
          parent_id: inline.parentId,
          is_folder: isFolder,
        }),
      })
      addFile(data.file)
      if (inline.parentId) {
        setExpanded(prev => new Set(prev).add(inline.parentId!))
      }
    } catch (err: any) {
      console.error("create failed:", err.message)
    }
    setInline(null)
  }

  const handleRename = async (name: string) => {
    if (!inline || !inline.fileId || !name.trim()) { setInline(null); return }
    // find the current file so we can preserve its parent_id
    const current = files.find(f => f.id === inline.fileId)
    try {
      const data = await apiFetch(`/api/rooms/${roomId}/files/${inline.fileId}`, {
        method:      "PATCH",
        credentials: "include",
        body: JSON.stringify({
          name:      name.trim(),
          parent_id: current?.parent_id ?? null,
        }),
      })
      mergeFiles(data.file)
    } catch (err: any) {
      console.error("rename failed:", err.message)
    }
    setInline(null)
  }

  const handleToggle = async (file: File) => {
    try {
      const data = await apiFetch(`/api/rooms/${roomId}/files/${file.id}/toggle`, {
        method:      "PATCH",
        credentials: "include",
      })
      if (data.files) mergeFiles(data.files)
      else            mergeFiles(data.file)
    } catch (err: any) {
      console.error("toggle failed:", err.message)
    }
  }

  const handleLangChange = async (file: File, language: string) => {
    try {
      const data = await apiFetch(`/api/rooms/${roomId}/files/${file.id}`, {
        method:      "PATCH",
        credentials: "include",
        body: JSON.stringify({ name: file.name, language }),
      })
      mergeFiles(data.file)
    } catch (err: any) {
      console.error("lang change failed:", err.message)
    }
    setLangFileId(null)
  }

  // ── context menu actions ─────────────────────────────────────────────────

  const ctxNewFile = (parentId: string | null) => {
    setContext(null)
    setInline({ parentId, fileId: null, value: "", isFolder: false })
    if (parentId) setExpanded(prev => new Set(prev).add(parentId))
  }

  const ctxNewFolder = (parentId: string | null) => {
    setContext(null)
    setInline({ parentId, fileId: null, value: "", isFolder: true })
    if (parentId) setExpanded(prev => new Set(prev).add(parentId))
  }

  const ctxRename = (node: FileNode) => {
    setContext(null)
    setInline({ parentId: null, fileId: node.file.id, value: node.file.name, isFolder: node.file.is_folder })
  }

  const ctxDelete = async (node: FileNode) => {
    setContext(null)
    await handleToggle(node.file)
  }

  const ctxRestore = async (node: FileNode) => {
    setContext(null)
    await handleToggle(node.file)
  }

  const ctxPermanentDelete = async (node: FileNode) => {
    setContext(null)
    if (!window.confirm(`Permanently delete "${node.file.name}"? This cannot be undone.`)) return
    try {
      await apiFetch(`/api/rooms/${roomId}/files/${node.file.id}/permanent`, {
        method:      "DELETE",
        credentials: "include",
      })
      onFilesChange(files.filter(f => f.id !== node.file.id))
    } catch (err: any) {
      console.error("permanent delete failed:", err.message)
    }
  }

  // ── render helpers ───────────────────────────────────────────────────────

  const renderInlineInput = (parentId: string | null, isFolder: boolean) => (
    <div className="ft-inline-input" style={{ paddingLeft: `calc(0.75rem + ${getDepth(parentId) * 12}px)` }}>
      {isFolder ? <FolderIcon /> : <FileIcon />}
      <input
        ref={inputRef}
        className="ft-name-input"
        value={inline?.value ?? ""}
        onChange={e => setInline(prev => prev ? { ...prev, value: e.target.value } : null)}
        onKeyDown={e => {
          if (e.key === "Enter") handleCreate((e.target as HTMLInputElement).value)
          if (e.key === "Escape") setInline(null)
        }}
        onBlur={e => handleCreate(e.target.value)}
        placeholder={isFolder ? "folder name" : "file name"}
      />
    </div>
  )

  const renderRenameInput = (node: FileNode, depth: number) => (
    <div className="ft-inline-input" style={{ paddingLeft: `calc(0.75rem + ${depth * 12}px)` }}>
      {node.file.is_folder ? <FolderIcon /> : <FileIcon />}
      <input
        ref={inputRef}
        className="ft-name-input"
        value={inline?.value ?? ""}
        onChange={e => setInline(prev => prev ? { ...prev, value: e.target.value } : null)}
        onKeyDown={e => {
          if (e.key === "Enter") handleRename((e.target as HTMLInputElement).value)
          if (e.key === "Escape") setInline(null)
        }}
        onBlur={e => handleRename(e.target.value)}
      />
    </div>
  )

  const renderNode = (node: FileNode, depth: number, isTrash: boolean): React.ReactNode => {
    const { file, children } = node
    const isActive    = activeFile?.id === file.id
    const isOpen      = expanded.has(file.id)
    const isRenaming  = inline?.fileId === file.id
    const showLang    = langFileId === file.id

    return (
      <div key={file.id} className="ft-node">
        {isRenaming ? (
          renderRenameInput(node, depth)
        ) : (
          <button
            className={`ft-row ${isActive ? "active" : ""} ${isTrash ? "trash" : ""}`}
            style={{ paddingLeft: `calc(0.75rem + ${depth * 12}px)` }}
            onClick={() => {
              if (file.is_folder) toggleExpand(file.id)
              else if (!isTrash) onFileClick(file)
            }}
            onContextMenu={e => openContext(e, node, isTrash)}
          >
            {file.is_folder && (
              <span className={`ft-chevron ${isOpen ? "open" : ""}`}>›</span>
            )}
            {file.is_folder ? <FolderIcon open={isOpen} /> : <FileIcon />}
            <span className="ft-name">{file.name}</span>
            {!file.is_folder && !isTrash && (
              <span className="ft-lang">{file.language}</span>
            )}
          </button>
        )}

        {/* language selector dropdown */}
        {showLang && !file.is_folder && (
          <div className="ft-lang-select" style={{ paddingLeft: `calc(0.75rem + ${depth * 12}px)` }}>
            <select
              autoFocus
              value={file.language}
              onChange={e => handleLangChange(file, e.target.value)}
              onBlur={() => setLangFileId(null)}
            >
              {LANGUAGES.map(l => <option key={l} value={l}>{l}</option>)}
            </select>
          </div>
        )}

        {/* inline create input inside this folder */}
        {inline && !inline.fileId && inline.parentId === file.id && (
          renderInlineInput(file.id, inline.isFolder)
        )}

        {/* children */}
        {file.is_folder && isOpen && children.map(child => renderNode(child, depth + 1, isTrash))}
      </div>
    )
  }

  const getDepth = (parentId: string | null): number => {
    if (!parentId) return 0
    const parent = files.find(f => f.id === parentId)
    if (!parent) return 0
    return 1 + getDepth(parent.parent_id)
  }

  return (
    <div className="ft-root" onContextMenu={e => openContext(e, null, false)}>

      {/* header + root-level create buttons */}
      <div className="ft-header">
        <span className="ft-header-label">Files</span>
        {canEdit && (
          <div className="ft-header-actions">
            <button className="ft-icon-btn" title="New File" onClick={() => ctxNewFile(null)}>+ file</button>
            <button className="ft-icon-btn" title="New Folder" onClick={() => ctxNewFolder(null)}>+ folder</button>
          </div>
        )}
      </div>

      {/* root-level inline create */}
      {inline && !inline.fileId && inline.parentId === null && (
        renderInlineInput(null, inline.isFolder)
      )}

      {/* active tree */}
      {activeTree.map(node => renderNode(node, 0, false))}

      {/* trash section */}
      {trashTree.length > 0 && (
        <div className="ft-trash-section">
          <button className="ft-trash-toggle" onClick={() => setTrashOpen(p => !p)}>
            <span className={`ft-chevron ${trashOpen ? "open" : ""}`}>›</span>
            <span>Trash</span>
            <span className="ft-trash-count">{trashTree.length}</span>
          </button>
          {trashOpen && trashTree.map(node => renderNode(node, 1, true))}
        </div>
      )}

      {/* context menu */}
      {context && (
        <div
          ref={contextRef}
          className="ft-context"
          style={{ top: context.y, left: context.x }}
        >
          {context.isTrash ? (
            <>
              <button className="ft-ctx-item" onClick={() => context.node && ctxRestore(context.node)}>
                Restore
              </button>
              <div className="ft-ctx-divider" />
              <button className="ft-ctx-item danger" onClick={() => context.node && ctxPermanentDelete(context.node)}>
                Delete permanently
              </button>
            </>
          ) : (
            <>
              {/* folder or root → show create options */}
              {(!context.node || context.node.file.is_folder) && (
                <>
                  <button className="ft-ctx-item" onClick={() => ctxNewFile(context.node?.file.id ?? null)}>
                    New File
                  </button>
                  <button className="ft-ctx-item" onClick={() => ctxNewFolder(context.node?.file.id ?? null)}>
                    New Folder
                  </button>
                  {context.node && <div className="ft-ctx-divider" />}
                </>
              )}
              {context.node && (
                <>
                  <button className="ft-ctx-item" onClick={() => ctxRename(context.node!)}>
                    Rename
                  </button>
                  {!context.node.file.is_folder && (
                    <button className="ft-ctx-item" onClick={() => { setContext(null); setLangFileId(context.node!.file.id) }}>
                      Language
                    </button>
                  )}
                  <div className="ft-ctx-divider" />
                  <button className="ft-ctx-item danger" onClick={() => ctxDelete(context.node!)}>
                    Delete
                  </button>
                </>
              )}
            </>
          )}
        </div>
      )}
    </div>
  )
}

function FileIcon() {
  return (
    <svg className="ft-icon" width="13" height="13" viewBox="0 0 16 16" fill="none">
      <path d="M3 2h7l3 3v9H3V2z" stroke="currentColor" strokeWidth="1.2" strokeLinejoin="round"/>
      <path d="M10 2v3h3" stroke="currentColor" strokeWidth="1.2" strokeLinejoin="round"/>
    </svg>
  )
}

function FolderIcon({ open = false }: { open?: boolean }) {
  return (
    <svg className="ft-icon" width="13" height="13" viewBox="0 0 16 16" fill="none">
      {open
        ? <path d="M1 4h5l2 2h7v8H1V4z" stroke="currentColor" strokeWidth="1.2" strokeLinejoin="round" fill="var(--accent-subtle)"/>
        : <path d="M1 4h5l2 2h7v8H1V4z" stroke="currentColor" strokeWidth="1.2" strokeLinejoin="round"/>
      }
    </svg>
  )
}