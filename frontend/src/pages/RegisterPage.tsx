import { Link, useNavigate } from "react-router-dom"
import { useAuth } from "../context/AuthContext"
import { useForm } from "../hooks/useForm"
import "./auth.css"

export default function RegisterPage() {
  const { register } = useAuth()
  const navigate = useNavigate()

  const { values, errors, submitting, serverError, onChange, handleSubmit } =
    useForm(
      { email: "", name: "", password: "", confirm: "" },
      {
        email:    (v) => (!v ? "Email is required" : undefined),
        name:     (v) => (!v ? "Name is required" : v.length < 2 ? "Min 2 characters" : undefined),
        password: (v) => (!v ? "Password is required" : v.length < 8 ? "Min 8 characters" : undefined),
        confirm:  (v) => (!v ? "Please confirm your password" : undefined),
      }
    )

  return (
    <div className="auth-page">
      <div className="auth-card">

        <div className="auth-brand">
          <div className="auth-brand-mark"><span>p</span></div>
          <span className="auth-brand-name">parily</span>
        </div>

        <h1 className="auth-heading">Create account</h1>
        <p className="auth-sub">Start collaborating in seconds</p>

        <form
          className="auth-form"
          onSubmit={(e) =>
            handleSubmit(e, async ({ email, name, password, confirm }) => {
              if (password !== confirm) throw new Error("Passwords do not match")
              await register(email, name, password)
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
            <label htmlFor="name">Name</label>
            <input
              id="name"
              name="name"
              type="text"
              autoComplete="name"
              placeholder="Your name"
              value={values.name}
              onChange={onChange}
              className={errors.name ? "error" : ""}
            />
            {errors.name && <span className="field-error">{errors.name}</span>}
          </div>

          <div className="field">
            <label htmlFor="password">Password</label>
            <input
              id="password"
              name="password"
              type="password"
              autoComplete="new-password"
              placeholder="min. 8 characters"
              value={values.password}
              onChange={onChange}
              className={errors.password ? "error" : ""}
            />
            {errors.password && <span className="field-error">{errors.password}</span>}
          </div>

          <div className="field">
            <label htmlFor="confirm">Confirm password</label>
            <input
              id="confirm"
              name="confirm"
              type="password"
              autoComplete="new-password"
              placeholder="••••••••"
              value={values.confirm}
              onChange={onChange}
              className={errors.confirm ? "error" : ""}
            />
            {errors.confirm && <span className="field-error">{errors.confirm}</span>}
          </div>

          {serverError && <p className="auth-error">{serverError}</p>}

          <button type="submit" className="auth-submit" disabled={submitting}>
            {submitting ? <span className="btn-spinner" /> : "Create account"}
          </button>
        </form>

        <p className="auth-footer">
          Already have an account? <Link to="/login">Sign in</Link>
        </p>

      </div>
    </div>
  )
}