import React from 'react';
import Link from '@docusaurus/Link';
import Layout from '@theme/Layout';
import Translate, {translate} from '@docusaurus/Translate';
import Wordmark from '@site/src/components/Wordmark';

export default function Home(): React.JSX.Element {
  return (
    <Layout
      title={translate({id: 'home.title', message: 'Documentation'})}
      description={translate({id: 'home.description', message: 'IVOAI product and operations documentation'})}>
      <header className="hero hero--primary">
        <div className="container">
          <p className="ivoai-hero__eyebrow"><Translate id="home.eyebrow">Host-first AI control plane</Translate></p>
          <h1 className="hero__title" aria-label="IVOAI"><Wordmark /></h1>
          <p className="hero__subtitle"><Translate id="home.subtitle">Use an OpenCode frontend with Codex and Claude Code executors, durable memory, private context, and isolated knowledge servers.</Translate></p>
          <div className="ivoai-hero__actions">
            <Link className="button button--primary button--lg" to="/docs/quickstart"><Translate id="home.quickstart">Start with the quickstart</Translate></Link>
            <Link className="button button--outline button--lg" to="/docs/architecture"><Translate id="home.architecture">Understand the architecture</Translate></Link>
          </div>
        </div>
      </header>
      <main className="container margin-vert--xl">
        <div className="ivoai-capability-grid">
          <section className="ivoai-capability"><h2><Translate id="home.session.title">One managed session</Translate></h2><p><Translate id="home.session.body">OpenCode provides the interactive surface while IVOAI owns executor selection, quota, policy, and lifecycle.</Translate></p></section>
          <section className="ivoai-capability"><h2><Translate id="home.auth.title">Official authentication</Translate></h2><p><Translate id="home.auth.body">Codex and Claude Code keep their own login state. IVOAI does not copy provider credentials into OpenCode.</Translate></p></section>
          <section className="ivoai-capability"><h2><Translate id="home.knowledge.title">Visible knowledge scope</Translate></h2><p><Translate id="home.knowledge.body">See which isolated servers participate, then federate all enabled sources or restrict the current session.</Translate></p></section>
        </div>
      </main>
    </Layout>
  );
}
