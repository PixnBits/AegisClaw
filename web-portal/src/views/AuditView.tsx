export function AuditView() {
  return (
    <section className="panel content-panel" data-testid="audit-panel" data-page="audit">
      <header>
        <p className="eyebrow">Compliance</p>
        <h1>Audit Log</h1>
      </header>
      <pre className="log-box" data-testid="audit-log">
        Audit events stream here. Use Court exports for structured compliance reports.
      </pre>
    </section>
  );
}