import { Navigate, Outlet } from "react-router-dom"
import { useAuth } from "../context/AuthContext"

function Splash() {
  return (
    <div style={{
      minHeight: "100vh",
      display: "flex",
      alignItems: "center",
      justifyContent: "center",
      background: "var(--bg-base)",
    }}>
      <div style={{
        width: 20,
        height: 20,
        border: "2px solid var(--border-default)",
        borderTopColor: "var(--accent)",
        borderRadius: "50%",
        animation: "spin 0.7s linear infinite",
      }} />
      <style>{`@keyframes spin { to { transform: rotate(360deg) } }`}</style>
    </div>
  )
}

export function ProtectedRoute() {
  const { user, loading } = useAuth()
  if (loading) return <Splash />
  return user ? <Outlet /> : <Navigate to="/login" replace />
}

export function PublicOnlyRoute() {
  const { user, loading } = useAuth()
  if (loading) return <Splash />
  return user ? <Navigate to="/dashboard" replace /> : <Outlet />
}