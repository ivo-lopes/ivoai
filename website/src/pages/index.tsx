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
          <p className="ivoai-hero__eyebrow"><Translate id="home.eyebrow">Agent development, one backend</Translate></p>
          <h1 className="hero__title" aria-label="IVOAI"><Wordmark /></h1>
          <p className="hero__subtitle"><Translate id="home.subtitle">Run managed Codex sessions with one orchestration core, durable memory, scoped evidence, and isolated workers.</Translate></p>
          <div className="ivoai-hero__actions">
            <Link className="button button--primary button--lg" to="/docs/quickstart"><Translate id="home.quickstart">Start with the quickstart</Translate></Link>
            <Link className="button button--outline button--lg" to="/docs/architecture"><Translate id="home.architecture">Understand the architecture</Translate></Link>
          </div>
        </div>
      </header>
      <main className="container margin-vert--xl">
        <div className="ivoai-capability-grid">
          <section className="ivoai-capability"><h2><Translate id="home.session.title">One managed session</Translate></h2><p><Translate id="home.session.body">IVOAI admits natural requests, answers simple questions directly, and coordinates plans and workers when needed.</Translate></p></section>
          <section className="ivoai-capability"><h2><Translate id="home.auth.title">Official authentication</Translate></h2><p><Translate id="home.auth.body">Official clients retain their own login state. Managed integrations use their supported interfaces and existing authentication.</Translate></p></section>
          <section className="ivoai-capability"><h2><Translate id="home.knowledge.title">Administration in your browser</Translate></h2><p><Translate id="home.knowledge.body">Manage capabilities through the same backend, with scoped access, explicit confirmations, an audit trail, and reverse proxy support.</Translate></p></section>
        </div>
      </main>
    </Layout>
  );
}
