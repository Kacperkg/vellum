import type { SelectHTMLAttributes } from "react"

type SelectProps = SelectHTMLAttributes<HTMLSelectElement>

export default function Select({
  className = "",
  children,
  ...props
}: SelectProps) {
  const classes = [
    "inline-flex items-center justify-center gap-2 rounded-md p-3",
    "transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring",
    "bg-background text-primary-foreground border border-border",
    className,
  ]
    .filter(Boolean)
    .join(" ")

  return (
    <select className={classes} {...props}>
      {children}
    </select>
  )
}
