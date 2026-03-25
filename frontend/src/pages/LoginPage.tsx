import { Link, useNavigate } from "react-router-dom"
import { useAuth } from "../context/AuthContext"
import { useForm } from "../hooks/useForm"
import "./auth.css"

export default function LoginPage() {
  const { login } = useAuth()
  const navigate = useNavigate()

  const { values, errors, submitting, serverError, onChange, handleSubmit } =
    useForm(
      { email: "", password: "" },
      {
        email:    (v) => !v ? "Email is required" : undefined,
        password: (v) => !v ? "Password is required" : undefined,
      }
    )

  return (
    <div className="auth-page">
      <div className="auth-card">

        <div className="auth-brand">
          <div className="auth-brand-mark"><span>p</span></div>
          <span className="auth-brand-name">parily</span>
        </div>

        <h1 className="auth-heading">Welcome back</h1>
        <p className="auth-sub">Sign in to your account</p>

        <form
          className="auth-form"
          onSubmit={(e) =>
            handleSubmit(e, async ({ email, password }) => {
              await login(email, password)
              navigate("/dashboard")
            })
          }
        >
          <div className="field">
            <label htmlFor="email">Email</label>
            <input
              id="email"
              name="email"
              type="email"
              autoComplete="email"
              placeholder="you@example.com"
              value={values.email}
              onChange={onChange}
              className={errors.email ? "error" : ""}
            />
            {errors.email && <span className="field-error">{errors.email}</span>}
          </div>

          <div className="field">
            <label htmlFor="password">Password</label>
            <input
              id="password"
              name="password"
              type="password"
              autoComplete="current-password"
              placeholder="••••••••"
              value={values.password}
              onChange={onChange}
              className={errors.password ? "error" : ""}
            />
            {errors.password && <span className="field-error">{errors.password}</span>}
          </div>

          {serverError && <p className="auth-error">{serverError}</p>}

          <button type="submit" className="auth-submit" disabled={submitting}>
            {submitting ? <span className="btn-spinner" /> : "Sign in"}
          </button>
        </form>

        <p className="auth-footer">
          No account? <Link to="/register">Create one</Link>
        </p>

      </div>
    </div>
  )
}