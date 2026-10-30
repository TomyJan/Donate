import { useApp } from '../app/context';
import './AdminLoading.css';

/** The route, identity check and initial queries share the same layout footprint. */
export function AdminLoading({
  shell = false,
  kind = 'overview',
}: {
  shell?: boolean;
  kind?: 'overview' | 'table' | 'detail';
}) {
  const { t } = useApp();
  const content = (
    <div className={`admin-loading admin-loading-${kind}`} role="status" aria-label={t('loading')}>
      <span className="admin-loading-label">
        <span className="admin-loading-spinner" aria-hidden="true" />
        {t('loading')}
      </span>
      <div className="admin-loading-preview" aria-hidden="true">
        {kind === 'detail' ? (
          <div className="admin-loading-detail">
            <span className="admin-loading-block admin-loading-value" />
            <div className="admin-loading-detail-fields">
              {[0, 1, 2, 3, 4, 5].map((field) => (
                <div key={field}>
                  <span className="admin-loading-block admin-loading-caption" />
                  <span className="admin-loading-block" />
                </div>
              ))}
            </div>
          </div>
        ) : kind === 'overview' ? (
          <>
            <div className="admin-loading-stats">
              {[0, 1, 2, 3].map((item) => (
                <div key={item}>
                  <span className="admin-loading-block admin-loading-caption" />
                  <span className="admin-loading-block admin-loading-value" />
                </div>
              ))}
            </div>
            <div className="admin-loading-columns">
              {[0, 1].map((item) => (
                <div className="admin-loading-list" key={item}>
                  <span className="admin-loading-block admin-loading-caption" />
                  {[0, 1, 2, 3].map((row) => (
                    <div className="admin-loading-row" key={row}>
                      <span className="admin-loading-block" />
                      <span className="admin-loading-block" />
                    </div>
                  ))}
                </div>
              ))}
            </div>
          </>
        ) : (
          <div className="admin-loading-list admin-loading-table">
            {[0, 1, 2, 3, 4].map((row) => (
              <div className="admin-loading-row" key={row}>
                <span className="admin-loading-block" />
                <span className="admin-loading-block" />
                <span className="admin-loading-block" />
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
  return shell ? (
    <div className="a-layout">
      <div className="a-side admin-loading-side" aria-hidden="true">
        {[0, 1, 2, 3, 4].map((item) => (
          <span className={`d-btn admin-loading-nav ${item === 4 ? 'admin-loading-home' : ''}`} key={item}>
            <span className="admin-loading-block" />
          </span>
        ))}
      </div>
      <main className="a-main">{content}</main>
    </div>
  ) : (
    content
  );
}
