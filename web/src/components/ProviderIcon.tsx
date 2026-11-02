import { Fingerprint } from 'lucide-react';
import { useState } from 'react';
import type { LoginProvider } from '../lib/providers';
import './ProviderIcon.css';

export function ProviderIcon({ provider }: { provider: LoginProvider }) {
  const [failed, setFailed] = useState('');
  const iconURL =
    provider.kind === 'qq' ? 'https://static-res.qq.com/static-res/imqq/qq-logo.png' : provider.icon_url;
  return (
    <span className="provider-icon" aria-hidden="true">
      {iconURL && failed !== iconURL ? (
        <img src={iconURL} alt="" referrerPolicy="no-referrer" onError={() => setFailed(iconURL)} />
      ) : (
        <Fingerprint />
      )}
    </span>
  );
}
