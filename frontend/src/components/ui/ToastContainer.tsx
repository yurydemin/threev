import { createPortal } from 'react-dom';
import { useToastStore } from '../../stores/useToastStore';
import { Toast } from './Toast';

/**
 * Toast stack, per docs/03-ux-ui-spec.md section 4.8. Mounted once at the
 * app root (`App.tsx`), portaled into `document.body` — same pattern as
 * `ContextMenu` — so it's visible above whatever screen is currently active
 * and unaffected by any ancestor's `overflow`/`transform`.
 *
 * `toasts` is rendered in insertion order (oldest first) with `flex-col`, so
 * with `bottom-4 right-4` anchoring the newest toast lands closest to the
 * bottom-right corner — the natural "just appeared" spot — while older ones
 * get pushed up.
 *
 * Unlike `Modal`/`ContextMenu`, there's no Headless UI `Transition` here:
 * a toast is either in the `toasts` array or it isn't, no open/closed
 * toggle. Appearance uses a plain CSS `animate-toast-in` keyframe (slide-in
 * from the right, 250ms, `duration-normal`'s value — see
 * `tailwind.config.js`). Disappearance is instant (no fade-out): animating
 * an *unmount* without a library like `framer-motion` needs either exit
 * animations (not available here) or manual timeout-based DOM removal
 * bookkeeping duplicating what `useToastStore.dismiss` already does — not
 * worth the complexity for a fire-and-forget notification, so this settles
 * for slide-in only, per the task's documented acceptable simplification.
 */
export function ToastContainer() {
  const toasts = useToastStore((state) => state.toasts);
  const dismiss = useToastStore((state) => state.dismiss);

  if (toasts.length === 0) return null;

  return createPortal(
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2">
      {toasts.map((item) => (
        <Toast key={item.id} id={item.id} type={item.type} message={item.message} onDismiss={dismiss} />
      ))}
    </div>,
    document.body,
  );
}
