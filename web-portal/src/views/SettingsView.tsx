import { PolicyPresetToggle } from '@/components/PolicyPreset/PolicyPresetToggle';
import { usePortalStore } from '@/store/portalStore';

export function SettingsView() {
  const dashboard = usePortalStore((s) => s.dashboard);

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