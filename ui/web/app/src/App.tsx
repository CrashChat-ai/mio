const tenants = [
  { name: "Tenant registry", state: "Phase 2", detail: "Create and list tenants through AdminService." },
  { name: "Channel installs", state: "Phase 2", detail: "Start and complete provider installs." },
  { name: "Message tail", state: "Phase 2", detail: "Stream account messages for operator triage." },
];

function App() {
  return (
    <main className="shell">
      <aside className="rail" aria-label="MIO sections">
        <div className="brand">MIO</div>
        <nav>
          <a href="#overview" aria-current="page">Overview</a>
          <a href="#tenants">Tenants</a>
          <a href="#accounts">Accounts</a>
          <a href="#messages">Messages</a>
        </nav>
      </aside>

      <section className="workspace">
        <header className="topbar">
          <div>
            <p className="eyebrow">Operator Console</p>
            <h1>Admin workspace</h1>
          </div>
          <div className="status" role="status">
            <span aria-hidden="true" />
            AdminService not connected
          </div>
        </header>

        <section className="summary" aria-label="Web admin scope">
          <article>
            <span className="metric">0</span>
            <p>Active tenants loaded</p>
          </article>
          <article>
            <span className="metric">0</span>
            <p>Installed accounts loaded</p>
          </article>
          <article>
            <span className="metric">Shell</span>
            <p>Current milestone</p>
          </article>
        </section>

        <section className="panel" id="overview">
          <div className="panelHeader">
            <h2>Tracked admin flows</h2>
            <button type="button">Refresh</button>
          </div>
          <div className="flowList">
            {tenants.map((item) => (
              <article key={item.name} className="flowItem">
                <div>
                  <h3>{item.name}</h3>
                  <p>{item.detail}</p>
                </div>
                <span>{item.state}</span>
              </article>
            ))}
          </div>
        </section>
      </section>
    </main>
  );
}

export default App;
