import { useLayoutEffect, useRef, useState } from 'react';
import { Markdown } from './Markdown';

/** Markdown collapsed to `lines` lines with a show more / show less toggle when it overflows. */
export function ClampedMarkdown({ text, lines }: { text: string; lines: number }) {
  const [open, setOpen] = useState(false);
  const [overflows, setOverflows] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    const el = ref.current;
    if (el && !open) setOverflows(el.scrollHeight > el.clientHeight + 1);
  }, [text, open]);

  return (
    <>
      <div ref={ref} className={open ? undefined : 'clamped'} style={open ? undefined : { WebkitLineClamp: lines }}>
        <Markdown text={text} />
      </div>
      {overflows || open ? (
        <button type="button" className="link-button small" aria-expanded={open} onClick={() => setOpen(!open)}>
          {open ? 'show less' : 'show more'}
        </button>
      ) : null}
    </>
  );
}
