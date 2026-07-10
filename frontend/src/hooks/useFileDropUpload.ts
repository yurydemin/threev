import { useEffect, useRef, useState, type DragEvent } from 'react';
import { OnFileDrop, OnFileDropOff } from '../../wailsjs/runtime/runtime';
import { useTransferStore } from '../stores/useTransferStore';

export interface FileDropHandlers {
  onDragEnter: (event: DragEvent) => void;
  onDragOver: (event: DragEvent) => void;
  onDragLeave: (event: DragEvent) => void;
  onDrop: (event: DragEvent) => void;
}

export interface UseFileDropUploadResult {
  /** Whether an overlay ("Отпустите файлы для загрузки") should be rendered. */
  isDraggingOver: boolean;
  /** Spread onto the DOM container that should visually react to a drag. */
  dragHandlers: FileDropHandlers;
}

/**
 * Drives drag-and-drop upload for the Object List area, per
 * docs/03-ux-ui-spec.md section 5.4.3.
 *
 * This intentionally wires up TWO independent mechanisms that are easy to
 * conflate:
 *
 * 1. Visual feedback — plain React/DOM `dragenter`/`dragover`/`dragleave`/
 *    `drop` handlers on the container, tracked with a nesting-depth counter
 *    (`useRef<number>`, `dragenter` increments, `dragleave` decrements) so a
 *    `dragleave` bubbling up from a child element (e.g. a `FileRow`) doesn't
 *    hide the overlay while the pointer is still within the container.
 *    `dragover`/`drop` call `preventDefault()` — required both to make the
 *    browser show a "can drop here" cursor and to let `drop` fire at all.
 *    This path never reads `event.dataTransfer.files`: in the Wails WebView
 *    it does not reliably carry absolute filesystem paths across platforms,
 *    so it is not a usable source of upload paths.
 *
 * 2. The actual upload — Wails' own `OnFileDrop` runtime event, subscribed
 *    once for the lifetime of the hook. It fires with real, OS-provided
 *    absolute paths once a drop completes, independently of the DOM `drop`
 *    event above (and, per Wails' docs, requires the CSS custom property
 *    `--wails-drop-target` to be set on the drop area or an ancestor for
 *    `useDropTarget: true` to recognize it as a valid zone — see
 *    `FileManagerScreen`, which sets it).
 *
 * `profileId`/`bucket` are read fresh inside the `OnFileDrop` callback via
 * refs rather than captured at mount time, since the callback is registered
 * once (`useEffect` with `[]` deps mirrors `OnFileDropOff` cleanup 1:1) but
 * the active bucket/prefix change as the user navigates.
 */
export function useFileDropUpload(
  profileId: number | null,
  bucket: string | null,
  prefix: string,
): UseFileDropUploadResult {
  const [isDraggingOver, setIsDraggingOver] = useState(false);
  const depthRef = useRef(0);

  // Kept in refs so the single `OnFileDrop` subscription below always sees
  // the latest navigation state without needing to re-subscribe on every
  // bucket/prefix change (re-subscribing would mean repeated
  // `OnFileDropOff`/`OnFileDrop` churn for no benefit).
  const profileIdRef = useRef(profileId);
  const bucketRef = useRef(bucket);
  const prefixRef = useRef(prefix);
  profileIdRef.current = profileId;
  bucketRef.current = bucket;
  prefixRef.current = prefix;

  useEffect(() => {
    OnFileDrop((_x, _y, paths) => {
      const currentProfileId = profileIdRef.current;
      const currentBucket = bucketRef.current;
      if (currentProfileId === null || currentBucket === null) return;
      if (paths.length === 0) return;
      void useTransferStore
        .getState()
        .queueUploadPaths(currentProfileId, currentBucket, prefixRef.current, paths);
    }, true);
    return () => OnFileDropOff();
  }, []);

  const dragHandlers: FileDropHandlers = {
    onDragEnter: (event) => {
      event.preventDefault();
      depthRef.current += 1;
      setIsDraggingOver(true);
    },
    onDragOver: (event) => {
      event.preventDefault();
    },
    onDragLeave: (event) => {
      event.preventDefault();
      depthRef.current = Math.max(0, depthRef.current - 1);
      if (depthRef.current === 0) setIsDraggingOver(false);
    },
    onDrop: (event) => {
      event.preventDefault();
      depthRef.current = 0;
      setIsDraggingOver(false);
    },
  };

  return { isDraggingOver, dragHandlers };
}
