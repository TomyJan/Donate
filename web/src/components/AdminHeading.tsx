import type { ReactNode } from 'react';

export function AdminHeading({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <header className="a-heading">
      <h1>{title}</h1>
      <div className="a-heading-actions">{children}</div>
    </header>
  );
}
