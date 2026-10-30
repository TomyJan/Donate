import { useEffect } from 'react';
import { NavLink } from 'react-router';
import { useApp } from '../app/context';
import { LEGAL_VERSION } from '../lib/legal';
import { legalDocuments } from './legal-content';
import '../styles/legal.css';

export function LegalPage({ kind }: { kind: 'terms' | 'privacy' }) {
  const { lang, t } = useApp();
  const documentContent = legalDocuments[kind][lang];
  const title = t(kind);
  useEffect(() => {
    const previous = document.title;
    document.title = `${title} · ${t('donate')} TomyJan`;
    window.scrollTo({ top: 0, behavior: 'instant' });
    return () => {
      document.title = previous;
    };
  }, [title, t]);
  return (
    <main className="legal-main">
      <article className="legal-paper">
        <header className="legal-heading">
          <div>
            <h1>{title}</h1>
            <p>
              {t('legalUpdated')} <time dateTime={LEGAL_VERSION}>{LEGAL_VERSION}</time>
            </p>
          </div>
          <nav aria-label={t('legalNavigation')}>
            <NavLink to="/terms">{t('terms')}</NavLink>
            <NavLink to="/privacy">{t('privacy')}</NavLink>
          </nav>
        </header>
        <p className="legal-intro">{documentContent.intro}</p>
        {documentContent.sections.map((section) => (
          <section key={section.title} className={section.important ? 'legal-important' : undefined}>
            <h2>{section.title}</h2>
            {section.paragraphs.map((paragraph) => (
              <p key={paragraph}>{paragraph}</p>
            ))}
          </section>
        ))}
        <section className="legal-contact">
          <h2>{t('legalContact')}</h2>
          <p>
            TomyJan ·{' '}
            <a href="https://t.me/tomyjan/" target="_blank" rel="noreferrer">
              Telegram
            </a>
            {' · '}
            <a href="https://qun.tomys.top/" target="_blank" rel="noreferrer">
              {t('legalContactQQ')}
            </a>
          </p>
          <p>{t('legalContactHint')}</p>
        </section>
      </article>
    </main>
  );
}
