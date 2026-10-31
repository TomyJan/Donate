import { ChevronDown } from 'lucide-react';
import { motion, useReducedMotion } from 'motion/react';
import { useId, useLayoutEffect, useRef, useState } from 'react';
import { useApp } from '../app/context';
import { Button } from '../components/ui';
import { dateTime } from '../lib/api';
import type { TaskOverview } from '../lib/types';

const series = ['created', 'started', 'completed', 'failed', 'retried'] as const;
export function TaskQueueTrend({
  data,
  recordingSince,
}: {
  data: TaskOverview['trend'];
  recordingSince: string;
}) {
  const { t } = useApp();
  const reduced = useReducedMotion();
  const id = useId();
  const panelId = useId();
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(760);
  useLayoutEffect(() => {
    const element = root.current;
    if (!element) return;
    const measure = () => {
      if (element.clientWidth > 0) setWidth(element.clientWidth);
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  const max = Math.max(1, ...data.flatMap((point) => series.map((key) => point[key])));
  const hasEvents = data.some((point) => series.some((key) => point[key] > 0));
  const right = width - 12;
  const x = (index: number) => 32 + (index / Math.max(1, data.length - 1)) * (right - 32);
  const y = (value: number) => 160 - (value / max) * 132;
  const ticks = max > 1 ? [0, Math.round(max / 2), max] : [0, max];
  return (
    <div className="task-trend" ref={root}>
      {!hasEvents ? (
        <p className="task-trend-empty">{t('taskTrendEmpty')}</p>
      ) : (
        <svg viewBox={`0 0 ${width} 190`} role="img" aria-label={t('taskTrend')} aria-describedby={id}>
          <title>{t('taskTrend')}</title>
          <desc id={id}>
            {t('taskTrendAccessible').replace('{count}', String(data.length)).replace('{max}', String(max))}
          </desc>
          {ticks.map((value) => (
            <g key={value}>
              <line x1="32" x2={right} y1={y(value)} y2={y(value)} className="task-grid-line" />
              <text x="22" y={y(value) + 4} textAnchor="end">
                {value}
              </text>
            </g>
          ))}
          {series.map((key) => (
            <g key={key} className={`task-line-${key}`}>
              <polyline
                fill="none"
                strokeWidth="2"
                vectorEffect="non-scaling-stroke"
                pathLength="1"
                className={reduced ? undefined : 'task-curve-draw'}
                points={data.map((point, index) => `${x(index)},${y(point[key])}`).join(' ')}
              />
              {data.length === 1 && <circle cx={x(0)} cy={y(data[0][key])} r="3" />}
            </g>
          ))}
          {data.length > 0 && (
            <>
              <text x="32" y="184">
                {data[0].date}
              </text>
              <text x={right} y="184" textAnchor="end">
                {data.at(-1)?.date}
              </text>
            </>
          )}
        </svg>
      )}
      {hasEvents && (
        <div className="task-legend">
          {series.map((key) => (
            <span key={key} className={`task-line-${key}`}>
              <i />
              {t(`task_event_${key}`)}
            </span>
          ))}
        </div>
      )}
      <p className="task-trend-recording">
        {t('taskTrendRecordingSince')} {dateTime(recordingSince)}
      </p>
      <div className="task-trend-data">
        <Button
          className="d-ghost task-trend-toggle"
          aria-expanded={open}
          aria-controls={panelId}
          onClick={() => setOpen((current) => !current)}
        >
          {t('taskTrendData')}
          <ChevronDown />
        </Button>
        <motion.div
          id={panelId}
          role="region"
          aria-label={t('taskTrendData')}
          className="task-trend-data-wrap"
          initial={false}
          aria-hidden={!open}
          inert={!open}
          animate={{ height: open ? 'auto' : 0, opacity: open ? 1 : 0 }}
          transition={{ duration: reduced ? 0 : 0.28 }}
        >
          <div className="table-scroll">
            <table className="a-table">
              <thead>
                <tr>
                  <th>{t('time')}</th>
                  {series.map((key) => (
                    <th key={key}>{t(`task_event_${key}`)}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {data.map((point) => (
                  <tr key={point.date}>
                    <td>{point.date}</td>
                    {series.map((key) => (
                      <td key={key}>{point[key]}</td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </motion.div>
      </div>
    </div>
  );
}
