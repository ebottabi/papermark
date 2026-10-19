import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "./button";

export interface DialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  children?: React.ReactNode;
  footer?: React.ReactNode;
  className?: string;
}

const Overlay = () => (
  <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-ink-deep/40 animate-overlay-in" />
);

/** Centered modal dialog. */
export const Dialog = ({ open, onOpenChange, title, description, children, footer, className }: DialogProps) => (
  <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
    <DialogPrimitive.Portal>
      <Overlay />
      <DialogPrimitive.Content
        className={cn(
          "fixed left-1/2 top-1/2 z-50 w-[calc(100vw-2rem)] max-w-lg -translate-x-1/2 -translate-y-1/2",
          "rounded-xl border border-gray-200 bg-background-surface p-5 shadow-dropdown animate-dialog-in",
          className,
        )}
      >
        <div className="mb-4 flex items-start justify-between gap-4">
          <div className="space-y-1">
            <DialogPrimitive.Title className="text-h-3 font-semibold text-ink">{title}</DialogPrimitive.Title>
            {description ? (
              <DialogPrimitive.Description className="text-body-3 text-ink-muted">
                {description}
              </DialogPrimitive.Description>
            ) : null}
          </div>
          <DialogPrimitive.Close asChild>
            <Button variant="ghost" size="sm" className="aspect-square w-8 px-0" aria-label="Close">
              <X className="h-4 w-4" />
            </Button>
          </DialogPrimitive.Close>
        </div>
        {children}
        {footer ? <div className="mt-5 flex justify-end gap-2">{footer}</div> : null}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  </DialogPrimitive.Root>
);

/** Right-side drawer, for detail panels. */
export const Drawer = ({ open, onOpenChange, title, description, children, footer, className }: DialogProps) => (
  <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
    <DialogPrimitive.Portal>
      <Overlay />
      <DialogPrimitive.Content
        className={cn(
          "fixed inset-y-0 right-0 z-50 flex w-[calc(100vw-2rem)] max-w-md flex-col",
          "border-l border-gray-200 bg-background-surface shadow-dropdown animate-drawer-in",
          className,
        )}
      >
        <div className="flex items-start justify-between gap-4 border-b border-gray-200 p-5">
          <div className="space-y-1">
            <DialogPrimitive.Title className="text-h-3 font-semibold text-ink">{title}</DialogPrimitive.Title>
            {description ? (
              <DialogPrimitive.Description className="text-body-3 text-ink-muted">
                {description}
              </DialogPrimitive.Description>
            ) : null}
          </div>
          <DialogPrimitive.Close asChild>
            <Button variant="ghost" size="sm" className="aspect-square w-8 px-0" aria-label="Close">
              <X className="h-4 w-4" />
            </Button>
          </DialogPrimitive.Close>
        </div>
        <div className="scrollbar-thin flex-1 overflow-y-auto p-5">{children}</div>
        {footer ? <div className="flex justify-end gap-2 border-t border-gray-200 p-4">{footer}</div> : null}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  </DialogPrimitive.Root>
);

export interface ConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  confirmLabel?: string;
  destructive?: boolean;
  loading?: boolean;
  onConfirm: () => void;
}

export const ConfirmDialog = ({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel = "Confirm",
  destructive,
  loading,
  onConfirm,
}: ConfirmDialogProps) => (
  <Dialog
    open={open}
    onOpenChange={onOpenChange}
    title={title}
    description={description}
    footer={
      <>
        <Button variant="secondary" onClick={() => onOpenChange(false)} disabled={loading}>
          Cancel
        </Button>
        <Button variant={destructive ? "danger" : "accent"} onClick={onConfirm} loading={loading}>
          {confirmLabel}
        </Button>
      </>
    }
  />
);
