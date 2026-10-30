import { Check, Copy } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { Button } from './ui';
import './CopyButton.css';

export function CopyButton({
  text,
  label,
  className = '',
}: {
  text: string;
  label: string;
  className?: string;
}) {
  const { t, report } = useApp();
  const [copied, setCopied] = useState(false);
  const [busy, setBusy] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const version = useRef(0);
  const copying = useRef(false);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Changed copy content revokes pending feedback and its timer.
  useEffect(() => {
    setCopied(false);
    setBusy(false);
    copying.current = false;
    return () => {
      version.current += 1;
      clearTimeout(timer.current);
    };
  }, [text]);
  const copy = async () => {
    if (copying.current) return;
    const current = version.current;
    copying.current = true;
    setBusy(true);
    try {
      await navigator.clipboard.writeText(text);
      if (current !== version.current) return;
      clearTimeout(timer.current);
      setCopied(true);
      timer.current = setTimeout(() => setCopied(false), 1000);
    } catch (error) {
      if (current === version.current) report(error);
    } finally {
      if (current === version.current) {
        copying.current = false;
        setBusy(false);
      }
    }
  };
  return (
    <Button
      className={`copy-button ${className}`}
      onClick={() => void copy()}
      disabled={busy}
      aria-label={copied ? t('copySuccess') : label}
      aria-busy={busy}
      data-copied={copied}
    >
      <span className="copy-button-content" aria-hidden="true">
        <span className="copy-button-default">
          <Copy />
          <span>{label}</span>
        </span>
        <span className="copy-button-success">
          <Check />
          <span>{t('copySuccess')}</span>
        </span>
      </span>
      <span className="copy-button-announcement" role="status">
        {copied ? t('copySuccess') : ''}
      </span>
    </Button>
  );
}
