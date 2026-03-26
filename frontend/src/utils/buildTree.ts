import type { File } from "../pages/RoomPage"

export interface FileNode {
  file:     File
  children: FileNode[]
}

// createTree builds a recursive tree from a flat list of files.
// Caller filters before passing in (active or trash).
// Within each level: folders first, then files, both alphabetical.
export function createTree(files: File[]): FileNode[] {
  const map = new Map<string | null, File[]>()

  // pass 1 — register every file as a possible parent
  for (const file of files) {
    map.set(file.id, [])
  }

  // pass 2 — place each file under its parent
  for (const file of files) {
    const key = file.parent_id ?? null
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(file)
  }

  return collect(map, null)
}

function collect(map: Map<string | null, File[]>, parentId: string | null): FileNode[] {
  const children = map.get(parentId) ?? []

  const sorted = [...children].sort((a, b) => {
    if (a.is_folder !== b.is_folder) return a.is_folder ? -1 : 1
    return a.name.localeCompare(b.name)
  })

  const nodes: FileNode[] = []
  for (const file of sorted) {
    nodes.push({
      file,
      children: collect(map, file.id),
    })
  }
  return nodes
}