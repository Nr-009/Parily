import { useState } from "react"
import type { ChangeEvent } from "react"

type Rules<T> = Partial<Record<keyof T, (v: string) => string | undefined>>

export function useForm<T extends Record<string, string>>(
  initial: T,
  rules?: Rules<T>
) {
  const [values, setValues] = useState<T>(initial)
  const [errors, setErrors] = useState<Partial<Record<keyof T, string>>>({})
  const [submitting, setSubmitting] = useState(false)
  const [serverError, setServerError] = useState("")

  function onChange(e: ChangeEvent<HTMLInputElement>) {
    const { name, value } = e.target
    setValues((v) => ({ ...v, [name]: value }))
    if (errors[name as keyof T]) {
      setErrors((e) => { const n = { ...e }; delete n[name as keyof T]; return n })
    }
  }

  function validate() {
    if (!rules) return true
    const next: Partial<Record<keyof T, string>> = {}
    for (const key in rules) {
      const msg = rules[key]?.(values[key] ?? "")
      if (msg) next[key] = msg
    }
    setErrors(next)
    return Object.keys(next).length === 0
  }

  async function handleSubmit(
    e: React.BaseSyntheticEvent,
    onSubmit: (values: T) => Promise<void>
  ) {
    e.preventDefault()
    setServerError("")
    if (!validate()) return
    setSubmitting(true)
    try {
      await onSubmit(values)
    } catch (err: any) {
      setServerError(err.message ?? "Something went wrong")
    } finally {
      setSubmitting(false)
    }
  }

  return { values, errors, submitting, serverError, onChange, handleSubmit }
}