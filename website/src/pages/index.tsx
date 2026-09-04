import React from 'react';
import Link from '@docusaurus/Link';
import Layout from '@theme/Layout';

export default function Home(): React.JSX.Element {
  return (
    <Layout title="Documentation" description="IVOAI product and operations documentation">
      <header className="hero hero--primary">
        <div className="container">
          <p className="ivoai-hero__eyebrow">Host-first AI control plane</p>
          <h1 className="hero__title">IVOAI</h1>
          <p className="hero__subtitle">Use an OpenCode frontend with Codex and Claude Code executors, durable memory, private context, and isolated knowledge servers.</p>
          <div className="ivoai-hero__actions">
            <Link className="button button--primary button--lg" to="/docs/quickstart">Start with the quickstart</Link>
            <Link className="button button--outline button--lg" to="/docs/architecture">Understand the architecture</Link>
          </div>
        </div>
      </header>
      <main className="container margin-vert--xl">
        <div className="ivoai-capability-grid">
          <section className="ivoai-capability"><h2>One managed session</h2><p>OpenCode provides the interactive surface while IVOAI owns executor selection, quota, policy, and lifecycle.</p></section>
          <section className="ivoai-capability"><h2>Official authentication</h2><p>Codex and Claude Code keep their own login state. IVOAI does not copy provider credentials into OpenCode.</p></section>
          <section className="ivoai-capability"><h2>Visible knowledge scope</h2><p>See which isolated servers participate, then federate all enabled sources or restrict the current session.</p></section>
        </div>
      </main>
    </Layout>
  );
}
