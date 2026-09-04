import { useEffect, useId, useRef, type DialogHTMLAttributes, type ReactNode } from "react"

type DialogProps = Omit<DialogHTMLAttributes<HTMLDialogElement>, "open" | "title"> & {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description?: ReactNode
  footer?: ReactNode
  headerBorder?: boolean
  footerBorder?: boolean
}

export default function Dialog({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
  headerBorder = true,
  footerBorder = true,
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
      <div className={headerBorder ? "border-b border-border p-5" : "p-5"}>
          <h2 id={titleId} className="text-lg font-semibold">{title}</h2>
          {description && (
            <p id={descriptionId} className="mt-1 text-sm text-muted-foreground">
              {description}
            </p>
          )}

      </div>

      {children != null && children !== false && (
        <div className="p-5">{children}</div>
      )}

      {footer && (
        <div className={`flex justify-end gap-3 p-3${footerBorder ? " border-t border-border" : ""}`}>
          {footer}
        </div>
      )}
    </dialog>
  )
}
