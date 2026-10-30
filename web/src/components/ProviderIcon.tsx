import { Fingerprint } from 'lucide-react';
import { useState } from 'react';
import type { LoginProvider } from '../lib/providers';
import './ProviderIcon.css';

export function ProviderIcon({ provider }: { provider: LoginProvider }) {
  const [failed, setFailed] = useState('');
  return (
    <span className="provider-icon" aria-hidden="true">
      {provider.icon_url && failed !== provider.icon_url ? (
        <img
          src={provider.icon_url}
          alt=""
          referrerPolicy="no-referrer"
          onError={() => setFailed(provider.icon_url)}
        />
      ) : provider.kind === 'qq' ? (
        <span>Q</span>
      ) : (
        <Fingerprint />
      )}
    </span>
  );
}
