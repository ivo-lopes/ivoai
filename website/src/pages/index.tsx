import React from 'react';
import Link from '@docusaurus/Link';
import Layout from '@theme/Layout';

export default function Home(): React.JSX.Element {
  return (
    <Layout title="Documentation" description="IVOAI product and operations documentation">
      <header className="hero hero--primary">
        <div className="container">
          <h1 className="hero__title">IVOAI</h1>
          <p className="hero__subtitle">One host-first runtime for AI executors, durable memory, private context and multiple independent servers.</p>
          <Link className="button button--secondary button--lg" to="/docs/quickstart">Start with the quickstart</Link>
        </div>
      </header>
      <main className="container margin-vert--xl">
        <div className="row">
          <div className="col col--4"><h2>Run</h2><p>Codex, Claude Code and OpenCode through one secure local control plane.</p></div>
          <div className="col col--4"><h2>Remember</h2><p>Keep exact evidence, durable memory and private context under explicit policy.</p></div>
          <div className="col col--4"><h2>Federate</h2><p>Use all enabled IVOAI servers by default, or restrict a session to named sources.</p></div>
        </div>
      </main>
    </Layout>
  );
}
