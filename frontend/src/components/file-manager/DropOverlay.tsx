import { useTranslation } from 'react-i18next';

/**
 * Purely visual overlay shown over the Object List while a file drag from
 * outside the window is in progress, per docs/03-ux-ui-spec.md section
 * 5.4.3 ("Drag-and-drop"): "2px dashed --accent" border + "Отпустите файлы
 * для загрузки" text.
 *
 * Carries no logic of its own — the caller decides when to render it (see
 * `useFileDropUpload`'s `isDraggingOver`) and must position its own
 * container `relative` for this `absolute inset-0` to fill it.
 * `pointer-events-none` so it never intercepts the drag events its parent
 * needs to keep tracking.
 */
export function DropOverlay() {
  const { t } = useTranslation();
  return (
    <div
      className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center border-2 border-dashed border-accent bg-accent-subtle"
      aria-hidden="true"
    >
      <p className="text-sm font-medium text-accent">{t('fileManager.dropOverlay.text')}</p>
    </div>
  );
}
