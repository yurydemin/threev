import {
  cloneElement,
  isValidElement,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type FocusEvent as ReactFocusEvent,
  type MouseEvent as ReactMouseEvent,
  type ReactElement,
} from 'react';
import { createPortal } from 'react-dom';
import { cn } from '../../lib/utils';

export interface TooltipProps {
  /** Text shown in the tooltip bubble. */
  content: string;
  /**
   * Single trigger element (an icon-only button, etc.) — cloned to attach
   * hover/focus handlers and a DOM ref, not wrapped in an extra `<span>`, so
   * it drops into existing `flex`/`gap` layouts (Toolbar, TransferCard, ...)
   * without affecting sizing. Typed loosely (`ReactElement<any>`) rather
   * than a specific prop shape — this wraps arbitrary triggers (`Button`,
   * plain `<button>`, Headless UI's `MenuButton`, ...), so `cloneElement`
   * below needs to be able to attach `ref`/`onMouseEnter`/etc regardless of
   * the trigger's own declared prop type.
   */
  children: ReactElement<any>;
  /** Renders `children` unwrapped, with no tooltip behavior at all. */
  disabled?: boolean;
}

const SHOW_DELAY_MS = 400;
const TRIGGER_GAP_PX = 8;
const VIEWPORT_MARGIN_PX = 4;

/**
 * Dark bubble per docs/03-ux-ui-spec.md section 4.9 — deliberately a fixed
 * hex pair, not a `--bg-*`/`--fg-*` theme token: the tooltip stays dark in
 * both the light and dark app themes.
 */
const TOOLTIP_BG = '#0f172a';
const TOOLTIP_FG = '#f8fafc';

type Placement = 'top' | 'bottom';

interface BubblePosition {
  x: number;
  y: number;
  placement: Placement;
}

/**
 * Tooltip per docs/03-ux-ui-spec.md section 4.9 (UX-006): 400ms show delay,
 * instant hide, positioned above its trigger by default and flipped below
 * when there isn't enough room above — the same `getBoundingClientRect` +
 * `position: fixed` + portal-into-`document.body` approach as
 * `ContextMenu.tsx`, including the same "render once at a provisional
 * position, correct synchronously via `useLayoutEffect` before paint" trick
 * so the flip/clamp is never visible as a jump.
 *
 * Used exclusively on icon-only controls that have no visible text label
 * next to them (Toolbar nav/view buttons, TransferCard action buttons,
 * ...) — per UX-006, a button with a visible label doesn't need one.
 */
export function Tooltip({ content, children, disabled = false }: TooltipProps) {
  const id = useId();
  const triggerRef = useRef<HTMLElement | null>(null);
  const bubbleRef = useRef<HTMLDivElement>(null);
  const showTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const [isVisible, setIsVisible] = useState(false);
  const [position, setPosition] = useState<BubblePosition | null>(null);

  function clearShowTimer() {
    if (showTimerRef.current !== null) {
      clearTimeout(showTimerRef.current);
      showTimerRef.current = null;
    }
  }

  function show() {
    clearShowTimer();
    showTimerRef.current = setTimeout(() => {
      const trigger = triggerRef.current;
      if (!trigger) return;
      const rect = trigger.getBoundingClientRect();
      // Provisional guess (the bubble isn't mounted/measured yet) —
      // corrected synchronously below via `useLayoutEffect` before the
      // browser paints, so this rough value is never actually visible.
      setPosition({ x: rect.left + rect.width / 2, y: rect.top, placement: 'top' });
      setIsVisible(true);
    }, SHOW_DELAY_MS);
  }

  // Disappearance is instant per the spec: cancel any pending show timer and
  // hide right away, no fade/delay.
  function hide() {
    clearShowTimer();
    setIsVisible(false);
  }

  useLayoutEffect(() => () => clearShowTimer(), []);

  // Clamp/flip so the bubble never renders past the viewport edge and stays
  // above the trigger unless there isn't room, in which case it flips below
  // — same logic as `ContextMenu.tsx`'s clamp effect.
  useLayoutEffect(() => {
    if (!isVisible) return;
    const trigger = triggerRef.current;
    const bubble = bubbleRef.current;
    if (!trigger || !bubble) return;

    const triggerRect = trigger.getBoundingClientRect();
    const bubbleRect = bubble.getBoundingClientRect();

    const fitsAbove = triggerRect.top - bubbleRect.height - TRIGGER_GAP_PX >= 0;
    const placement: Placement = fitsAbove ? 'top' : 'bottom';
    const y =
      placement === 'top'
        ? triggerRect.top - bubbleRect.height - TRIGGER_GAP_PX
        : triggerRect.bottom + TRIGGER_GAP_PX;

    const idealX = triggerRect.left + triggerRect.width / 2 - bubbleRect.width / 2;
    const maxX = Math.max(VIEWPORT_MARGIN_PX, window.innerWidth - bubbleRect.width - VIEWPORT_MARGIN_PX);
    const x = Math.min(Math.max(VIEWPORT_MARGIN_PX, idealX), maxX);

    setPosition({ x, y, placement });
    // Only needs to (re-)run when a new show cycle starts, not every time it
    // sets `position` itself.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isVisible]);

  if (!isValidElement(children) || disabled || !content) return children;

  // Re-widened to `ReactElement<any>` after the `isValidElement` guard,
  // which narrows to a generic `ReactElement<unknown, ...>` regardless of
  // `TooltipProps.children`'s own declared type — `cloneElement` below needs
  // the wider type back to accept arbitrary extra props.
  const element = children as ReactElement<any>;
  const childProps = element.props as Record<string, unknown>;

  function mergedRef(node: HTMLElement | null) {
    triggerRef.current = node;
    const childRef = (element as unknown as { ref?: unknown }).ref;
    if (typeof childRef === 'function') (childRef as (n: HTMLElement | null) => void)(node);
    else if (childRef && typeof childRef === 'object') (childRef as { current: HTMLElement | null }).current = node;
  }

  const trigger = cloneElement(element, {
    ref: mergedRef,
    'aria-describedby': isVisible ? id : undefined,
    onMouseEnter: (event: ReactMouseEvent) => {
      (childProps.onMouseEnter as ((e: ReactMouseEvent) => void) | undefined)?.(event);
      show();
    },
    onMouseLeave: (event: ReactMouseEvent) => {
      (childProps.onMouseLeave as ((e: ReactMouseEvent) => void) | undefined)?.(event);
      hide();
    },
    onFocus: (event: ReactFocusEvent) => {
      (childProps.onFocus as ((e: ReactFocusEvent) => void) | undefined)?.(event);
      show();
    },
    onBlur: (event: ReactFocusEvent) => {
      (childProps.onBlur as ((e: ReactFocusEvent) => void) | undefined)?.(event);
      hide();
    },
  });

  return (
    <>
      {trigger}
      {isVisible &&
        position &&
        createPortal(
          <div
            id={id}
            ref={bubbleRef}
            role="tooltip"
            style={{ left: position.x, top: position.y, backgroundColor: TOOLTIP_BG, color: TOOLTIP_FG }}
            className={cn(
              'fixed z-[60] max-w-xs whitespace-nowrap rounded-sm px-2 py-1 text-xs leading-none',
              'shadow-[0_2px_6px_rgba(0,0,0,0.3)]',
            )}
          >
            {content}
            <div
              aria-hidden="true"
              className="absolute left-1/2 h-0 w-0 -translate-x-1/2 border-x-[6px] border-x-transparent"
              style={
                position.placement === 'top'
                  ? { top: '100%', borderTop: `6px solid ${TOOLTIP_BG}` }
                  : { bottom: '100%', borderBottom: `6px solid ${TOOLTIP_BG}` }
              }
            />
          </div>,
          document.body,
        )}
    </>
  );
}
