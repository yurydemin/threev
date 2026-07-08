import type { ReactNode } from 'react';
import { Dialog, DialogBackdrop, DialogPanel, DialogTitle } from '@headlessui/react';
import { X } from 'lucide-react';
import { cn } from '../../lib/utils';
import { Button } from './Button';

export interface ModalProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  /** Rendered as a flex row, justify-end, gap 8px (buttons). */
  footer?: ReactNode;
  /** `default` = 480px max-width, `large` = 640px. */
  size?: 'default' | 'large';
}

/**
 * Modal / Dialog per docs/03-ux-ui-spec.md section 4.7, built on Headless
 * UI's `Dialog` + `DialogPanel`/`DialogBackdrop` (2.x API — transitions are
 * driven by the `transition` prop + `data-[closed]`/`data-[enter]` variants
 * rather than a separate `<Transition>` wrapper).
 */
export function Modal({ isOpen, onClose, title, children, footer, size = 'default' }: ModalProps) {
  return (
    <Dialog open={isOpen} onClose={onClose} className="relative z-50">
      <DialogBackdrop
        transition
        className={cn(
          'fixed inset-0 bg-black/50 backdrop-blur-[2px]',
          'transition-opacity duration-fast ease-out data-[closed]:opacity-0',
        )}
      />
      <div className="fixed inset-0 flex w-screen items-center justify-center p-4">
        <DialogPanel
          transition
          className={cn(
            'w-full rounded border border-border bg-bg-elevated p-4 shadow-[0_4px_12px_rgba(0,0,0,0.15)]',
            'transition duration-normal ease-out data-[closed]:translate-y-2 data-[closed]:opacity-0',
            size === 'large' ? 'max-w-[640px]' : 'max-w-[480px]',
          )}
        >
          <div className="flex items-start justify-between gap-4">
            <DialogTitle as="h2" className="text-[13px] font-semibold text-fg-primary">
              {title}
            </DialogTitle>
            <Button
              iconOnly
              variant="ghost"
              onClick={onClose}
              aria-label="Закрыть"
              className="-mr-1 -mt-1"
            >
              <X className="h-4 w-4" aria-hidden="true" />
            </Button>
          </div>
          <div className="pt-2">{children}</div>
          {footer && <div className="flex items-center justify-end gap-2 pt-4">{footer}</div>}
        </DialogPanel>
      </div>
    </Dialog>
  );
}
