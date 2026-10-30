import type { ReactNode } from 'react';
import './DialogActions.css';

export function DialogActions({ children, className = '' }: { children: ReactNode; className?: string }) {
  return (
    <div className={`dialog-actions dialog-action-row ${className}`} data-component="dialog-actions">
      {children}
    </div>
  );
}
