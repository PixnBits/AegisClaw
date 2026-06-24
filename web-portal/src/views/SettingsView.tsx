import { useEffect, useState } from 'react';
import { api } from '@/api/client';
import { PolicyPresetToggle } from '@/components/PolicyPreset/PolicyPresetToggle';
import { usePortalStore } from '@/store/portalStore';

export function SettingsView() {
  const dashboard = usePortalStore((s) => s.dashboard);
  const [cisoDelegation, setCisoDelegation] = useState(false);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.cisoDelegation().then((d) => { setCisoDelegation(!!d.enabled); setLoading(false); }).catch(() => setLoading(false));
  }, []);

  const toggleCiso = async () => {
    const next = !cisoDelegation;
    await api.setCisoDelegation(next);
    setCisoDelegation(next);
  };

  return (
    <section className="panel content-panel" data-testid="settings-panel" data-page="settings">
      <header>
        <p className="eyebrow">Configuration</p>
        <h1>Settings</h1>
      </header>
      <article className="subpanel">
        <p className="eyebrow">Reasoning Visibility (Global)</p>
        <PolicyPresetToggle />
      </article>
      <article className="subpanel" data-testid="settings-safe-mode">
        <p className="eyebrow">Safe Mode</p>
        <p>
          Current: <strong id="settingsSafeModeLabel">{dashboard?.safe_mode ? 'ON' : 'OFF'}</strong>
        </p>
      </article>
      <article className="subpanel" data-testid="settings-ciso-delegation">
        <p className="eyebrow">CISO Delegation (opt-in)</p>
        <label>
          <input
            type="checkbox"
            checked={cisoDelegation}
            onChange={toggleCiso}
            disabled={loading}
            data-testid="ciso-delegation-toggle"
          />
          {' '}Allow CISO persona to receive and propose routine permission grants (high-impact still Court)
        </label>
        <p className="subtle">Default off. Toggle persists immediately.</p>
      </article>
      <article className="subpanel">
        <p className="eyebrow">Security</p>
        <ul className="list-stack subtle">
          <li>Browser isolated — no secrets in client</li>
          <li>All actions mediated via vsock bridge</li>
          <li>STOMP subscriptions are view-scoped</li>
        </ul>
      </article>
    </section>
  );
}