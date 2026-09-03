import type { InputHTMLAttributes } from "react"

type InputVariant = "email" | "password" | "string" | "number" | "date"

type InputProps = InputHTMLAttributes<HTMLInputElement> & {
  variant?: InputVariant
}

const variantClasses: Record<InputVariant, string> = {
  email: "bg-surface text-primary-foreground border border-border",
  password: "bg-surface text-primary-foreground border border-border",
  string: "bg-background text-primary-foreground border border-border",
  number: "bg-background text-primary-foreground border border-border",
  date: "bg-background text-primary-foreground border border-border dark:[color-scheme:dark]",
}

const variantTypes: Record<InputVariant, string> = {
    email: "email",
    password: "password",
    string: "text",
    number: "number",
    date: "date"
}


export default function Input({
  variant = "string",
  type,
  placeholder,
  className = "",
  ...props
}: InputProps) {
  const classes = [
    "inline-flex items-center justify-center gap-2 rounded-md p-2",
    "transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring",
    variantClasses[variant],
    className,
  ]
    .filter(Boolean)
    .join(" ")

  return <input type={type ?? variantTypes[variant]} placeholder={placeholder} className={classes} {...props} />
}
