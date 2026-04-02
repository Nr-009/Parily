interface ExecutionResult {
  output:      string
  exit_code:   number
  duration_ms: number
  truncated:   boolean
}

interface OutputPanelProps {
  result: ExecutionResult | null
}

export function OutputPanel({ result }: OutputPanelProps) {
  if (!result) {
    return (
      <div className="output-panel output-panel--empty">
        <span>No output yet — click Run to execute</span>
      </div>
    )
  }

  const success = result.exit_code === 0

  return (
    <div className="output-panel">
      <div className="output-panel-header">
        <span className={`output-status ${success ? "output-status--ok" : "output-status--err"}`}>
          {success ? "✓" : "✗"} exit {result.exit_code}
        </span>
        <span className="output-duration">{result.duration_ms}ms</span>
        {result.truncated && (
          <span className="output-truncated">output truncated at 50kb</span>
        )}
      </div>
      <pre className="output-content">{result.output}</pre>
    </div>
  )
}