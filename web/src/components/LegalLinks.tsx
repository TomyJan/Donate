import { Link } from 'react-router';
import { useApp } from '../app/context';

export function LegalLinks({ newTab = false }: { newTab?: boolean }) {
  const { t } = useApp();
  const external = newTab ? { target: '_blank', rel: 'noopener noreferrer' } : {};
  return (
    <span className="legal-links">
      <Link to="/terms" {...external}>
        {t('terms')}
      </Link>
      <span className="legal-link-separator" aria-hidden="true">
        ·
      </span>
      <Link to="/privacy" {...external}>
        {t('privacy')}
      </Link>
    </span>
  );
}
