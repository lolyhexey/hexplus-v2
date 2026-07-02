import * as React from 'react'
import { cn } from '@/lib/cn'
import { X } from 'lucide-react'

// Minimal modal — enough for the create/edit forms without pulling
// in radix-ui/react-dialog. Trap-focus is intentionally not implemented
// (the panel is admin-only, not a public UX surface).
export interface DialogProps {
  open: boolean
  onClose: () => void
  title?: string
  description?: string
  children: React.ReactNode
  footer?: React.ReactNode
  wide?: boolean
}

export function Dialog({ open, onClose, title, description, children, footer, wide }: DialogProps) {
  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
         onClick={(e) => { if (e.target === e.currentTarget) onClose() }}>
      <div className={cn('w-full rounded-xl border bg-card text-card-foreground shadow-lg',
                          wide ? 'max-w-3xl' : 'max-w-md')}>
        <div className="flex items-start justify-between p-6 pb-2">
          <div>
            {title && <h2 className="text-lg font-semibold">{title}</h2>}
            {description && <p className="text-sm text-muted-foreground mt-1">{description}</p>}
          </div>
          <button onClick={onClose} className="rounded-md p-1 hover:bg-accent" aria-label="Close">
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="px-6 py-4 space-y-3">{children}</div>
        {footer && <div className="flex justify-end gap-2 border-t p-4">{footer}</div>}
      </div>
    </div>
  )
}
