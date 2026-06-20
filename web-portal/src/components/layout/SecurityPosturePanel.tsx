import { SecurityPosture } from '@/contracts';
import './SecurityPosture.css';

type Props = {
  posture: SecurityPosture | null;
};

export function SecurityPosturePanel({ posture }: Props) {
  const indicators = posture?.indicators || [];

  return (
    <section className="security-posture" data-testid="security-posture">
      <p className="eyebrow">Security Posture</p>
      {indicators.length === 0 ? (
        <p className="subtle">Loading posture from daemon…</p>
      ) : (
        <ul className="security-posture__list">
          {indicators.map((item) => (
            <li key={item.id} className={`security-posture__item security-posture__item--${item.status}`}>
              <span className="security-posture__dot" aria-hidden="true" />
              <div>
                <strong>{item.label}</strong>
                {item.detail ? <p className="subtle">{item.detail}</p> : null}
              </div>
            </li>
          ))}
        </ul>
      )}
      {posture?.court_personas_online != null ? (
        <p className="subtle security-posture__meta">
          Court personas online: {posture.court_personas_online}
          {posture.store_collab_ready === false ? ' · Store still starting' : ''}
        </p>
      ) : null}
    </section>
  );
}
