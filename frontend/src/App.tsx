import { BrowserRouter, Routes, Route, Navigate, useNavigate } from "react-router-dom"
import { AuthProvider ,useAuth} from "./context/AuthContext"
import { ProtectedRoute, PublicOnlyRoute } from "./router/ProtectedRoute"
import LoginPage from "./pages/LoginPage"
import RegisterPage from "./pages/RegisterPage"

// Placeholder — replace with real dashboard in 2.3
// replace the Dashboard placeholder in App.tsx with this
function Dashboard() {
  const { user, logout } = useAuth()
  const navigate = useNavigate()

  return (
    <div style={{
      padding: "2rem",
      color: "var(--text-primary)",
      display: "flex",
      flexDirection: "column",
      gap: "1rem"
    }}>
      <p>Logged in as {user?.email}</p>
      <button
        onClick={async () => {
          await logout()
          navigate("/login")
        }}
        style={{
          width: "fit-content",
          padding: "0.5rem 1rem",
          background: "var(--bg-elevated)",
          border: "1px solid var(--border-default)",
          borderRadius: "var(--radius-md)",
          color: "var(--text-primary)",
          cursor: "pointer",
        }}
      >
        Logout
      </button>
    </div>
  )
}
export default function App() {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Routes>
          <Route element={<PublicOnlyRoute />}>
            <Route path="/login"    element={<LoginPage />} />
            <Route path="/register" element={<RegisterPage />} />
          </Route>

          <Route element={<ProtectedRoute />}>
            <Route path="/dashboard" element={<Dashboard />} />
          </Route>
          <Route path="*" element={<Navigate to="/dashboard" replace />} />
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  )
}