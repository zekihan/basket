import { useEffect, useId, useRef } from "react";
import type React from "react";
import type { ReactNode } from "react";
import { linkify } from "../data/logic.ts";

export function LinkifiedNotes({ text }: { text: string }): React.JSX.Element {
  const parts = linkify(text);
  return (
    <span className="item-notes">
      {parts.map((part, i) => part.href ? (
        <a key={i} href={part.href} target="_blank" rel="noopener noreferrer">{part.text}</a>
      ) : (
        <span key={i}>{part.text}</span>
      ))}
    </span>
  );
}

export function Sheet({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }): React.JSX.Element {
  const ref = useRef<HTMLDialogElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const titleId = useId();
  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;
    const previous = document.activeElement;
    dialog.showModal();
    heading.current?.focus();
    return () => {
      dialog.close();
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      className="sheet"
      aria-labelledby={titleId}
      onClose={onClose}
      onCancel={event => { event.preventDefault(); onClose(); }}
    >
      <div className="sheet-header">
        <h2 id={titleId} tabIndex={-1} ref={heading}>{title}</h2>
        <button type="button" className="icon-button" onClick={onClose} aria-label="Close dialog">×</button>
      </div>
      {children}
    </dialog>
  );
}

export function PrimaryButton(props: React.ButtonHTMLAttributes<HTMLButtonElement>): React.JSX.Element {
  const { children, className, ...rest } = props;
  return (
    <button type="button" className={`button primary ${className ?? ""}`} {...rest}>
      {children}
    </button>
  );
}

export function SecondaryButton(props: React.ButtonHTMLAttributes<HTMLButtonElement>): React.JSX.Element {
  const { children, className, ...rest } = props;
  return (
    <button type="button" className={`button secondary ${className ?? ""}`} {...rest}>
      {children}
    </button>
  );
}
