import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useConfirmStore, type ConfirmOptions } from '../../stores/useConfirmStore';
import { Modal } from './Modal';
import { Button } from './Button';

/**
 * Generic confirmation dialog, replacing `window.confirm` (see
 * `useConfirmStore`'s doc comment for why). Mounted once at the app root
 * (`App.tsx`), alongside `ToastContainer` — not per call site — and driven
 * imperatively via `lib/confirm.ts`'s `confirmDialog(...)`.
 *
 * `useConfirmStore.handleCancel`/`handleConfirm` reset `options` to `null`
 * the instant the dialog closes, but `Modal`'s `DialogPanel` still needs
 * `title`/`children` to render something sensible during its exit
 * transition. Same guard `ConnectionForm` uses for its own form `values`
 * (only synced from props while `isOpen`, so they don't blank out mid-close):
 * `displayOptions` here only follows the store's `options` while it's
 * non-null, so the last real options stick around through the close
 * animation instead of flashing empty.
 */
export function ConfirmDialog() {
  const { t } = useTranslation();
  const isOpen = useConfirmStore((state) => state.isOpen);
  const options = useConfirmStore((state) => state.options);
  const handleConfirm = useConfirmStore((state) => state.handleConfirm);
  const handleCancel = useConfirmStore((state) => state.handleCancel);

  const [displayOptions, setDisplayOptions] = useState<ConfirmOptions | null>(null);

  useEffect(() => {
    if (options) setDisplayOptions(options);
  }, [options]);

  if (displayOptions === null) return null;

  return (
    <Modal
      isOpen={isOpen}
      onClose={handleCancel}
      title={displayOptions.title ?? t('common.confirmTitle')}
      footer={
        <>
          <Button variant="secondary" onClick={handleCancel}>
            {displayOptions.cancelLabel ?? t('common.cancel')}
          </Button>
          <Button variant={displayOptions.danger ? 'danger' : 'primary'} onClick={handleConfirm}>
            {displayOptions.confirmLabel ?? t('common.confirm')}
          </Button>
        </>
      }
    >
      <p className="text-[13px] text-fg-primary">{displayOptions.message}</p>
    </Modal>
  );
}
