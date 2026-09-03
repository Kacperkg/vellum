import { useEffect, useId, useRef, type DialogHTMLAttributes, type ReactNode } from "react"

type DialogProps = Omit<DialogHTMLAttributes<HTMLDialogElement>, "open" | "title"> & {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description?: ReactNode
  footer?: ReactNode
}

export default function Dialog({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
  className = "",
  ...props
}: DialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const titleId = useId()
  const descriptionId = useId()

  useEffect(() => {
    const dialog = dialogRef.current
    if (!dialog) return

    if (open && !dialog.open) dialog.showModal()
    if (!open && dialog.open) dialog.close()
  }, [open])

  const classes = [
    "m-auto w-[min(32rem,calc(100%-2rem))] rounded-md border border-border bg-surface p-0 text-foreground shadow-2xl",
    "backdrop:bg-black/70",
    className,
  ]
    .filter(Boolean)
    .join(" ")

  return (
    <dialog
      ref={dialogRef}
      className={classes}
      aria-labelledby={titleId}
      aria-describedby={description ? descriptionId : undefined}
      onCancel={() => onOpenChange(false)}
      onClose={() => onOpenChange(false)}
      onClick={(event) => {
        if (event.target === event.currentTarget) onOpenChange(false)
      }}
      {...props}
    >
      <div className="flex items-start justify-between gap-4 border-b border-border p-5">
        <div>
          <h2 id={titleId} className="text-lg font-semibold">{title}</h2>
          {description && (
            <p id={descriptionId} className="mt-1 text-sm text-muted-foreground">
              {description}
            </p>
          )}
        </div>
        <button
          type="button"
          className="rounded-md px-2 py-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          aria-label="Close dialog"
          onClick={() => onOpenChange(false)}
        >
          x
        </button>
      </div>

      <div className="p-5">{children}</div>

      {footer && (
        <div className="flex justify-end gap-3 border-t border-border p-5">
          {footer}
        </div>
      )}
    </dialog>
  )
}
