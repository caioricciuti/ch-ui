import React from 'react';
import clsx from 'clsx';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import Link from '@docusaurus/Link';
import { Github, Slack, Twitter } from 'lucide-react';

import styles from './index.module.css';

const features = [
  {
    title: 'Easy to Use',
    description: 'Manage your ClickHouse databases with a sleek, intuitive interface.',
    icon: '🚀',
  },
  {
    title: 'Powerful Queries',
    description: 'Execute complex queries seamlessly with our advanced query editor.',
    icon: '💻',
  },
  {
    title: 'Data Visualization',
    description: 'Visualize your data with beautiful, interactive charts and graphs.',
    icon: '📊',
  },
];

function Hero() {
  const {siteConfig} = useDocusaurusContext();
  return (
    <header className={clsx('hero', styles.heroBanner)}>
      <div className="container">
        <h1 className="hero__title">{siteConfig.title}</h1>
        <p className="hero__subtitle">{siteConfig.tagline}</p>
        <div className={styles.buttons}>
          <Link
            className="button button--secondary button--lg"
            to="/docs/intro">
            Get Started
          </Link>
        </div>
      </div>
    </header>
  );
}

function Feature({title, description, icon}) {
  return (
    <div className={clsx('col col--4')}>
      <div className="text--center padding-horiz--md">
        <h3>{icon} {title}</h3>
        <p>{description}</p>
      </div>
    </div>
  );
}

function HomepageFeatures() {
  return (
    <section className={styles.features}>
      <div className="container">
        <div className="row">
          {features.map((props, idx) => (
            <Feature key={idx} {...props} />
          ))}
        </div>
      </div>
    </section>
  );
}

function SocialLinks() {
  return (
    <div className={styles.socialLinks}>
      <a href="https://Github.com/your-repo" target="_blank" rel="noopener noreferrer">
        <Github />
      </a>
      <a href="https://your-slack-invite-link.com" target="_blank" rel="noopener noreferrer">
        <Slack />
      </a>
      <a href="https://twitter.com/your-twitter" target="_blank" rel="noopener noreferrer">
        <Twitter />
      </a>
    </div>
  );
}

export default function Home() {
  const {siteConfig} = useDocusaurusContext();
  return (
    <Layout
      title={`Welcome to ${siteConfig.title}`}
      description="Discover a sleek and updated UI for ClickHouse databases with our project. Manage your ClickHouse databases efficiently, execute queries seamlessly, and visualize data with ease."
    >
      <Hero />
      <main>
        <HomepageFeatures />
        <SocialLinks />
      </main>
    </Layout>
  );
}