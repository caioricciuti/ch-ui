import React from "react";
import clsx from "clsx";
import {
  FiDatabase,
  FiEdit3,
  FiTable,
  FiBarChart2,
  FiActivity,
  FiCommand,
} from "react-icons/fi";
import styles from "./styles.module.css";

type FeatureItem = {
  title: string;
  Icon: React.ElementType;
  description: JSX.Element;
};

const FeatureList: FeatureItem[] = [
  {
    title: "Data Explorer",
    Icon: FiDatabase,
    description: (
      <>
        Browse your databases and tables effortlessly with our intuitive data
        explorer. Quickly find and access the information you need.
      </>
    ),
  },
  {
    title: "SQL Editor",
    Icon: FiEdit3,
    description: (
      <>
        Write and run queries with our powerful SQL editor. Enjoy features like
        syntax highlighting and auto-completion for efficient coding.
      </>
    ),
  },
  {
    title: "Data Visualization",
    Icon: FiTable,
    description: (
      <>
        View your data in interactive tables. Easily filter, sort, and export
        your data for further analysis or reporting.
      </>
    ),
  },
  {
    title: "Data Insights",
    Icon: FiBarChart2,
    description: (
      <>
        Gain valuable insights from your data with ease. Our tools help you
        uncover patterns and trends, making data-driven decisions simpler than
        ever.
      </>
    ),
  },
  {
    title: "Instance Metrics",
    Icon: FiActivity,
    description: (
      <>
        Monitor your instance performance with real-time metrics. Keep track of
        system health and optimize your database operations.
      </>
    ),
  },
  {
    title: "Simple and Intuitive",
    Icon: FiCommand,
    description: (
      <>
        CH-UI is designed to be user-friendly and easy to navigate. Whether
        you're a beginner or an expert, you'll feel right at home.
      </>
    ),
  },
];

function Feature({ title, Icon, description }: FeatureItem) {
  return (
    <div className={clsx("col col--4", styles.feature)}>
      <div className="text--center">
        <Icon className={styles.featureIcon} />
      </div>
      <div className="text--center padding-horiz--lg">
        <h3>{title}</h3>
        <p>{description}</p>
      </div>
    </div>
  );
}

export default function HomepageFeatures(): JSX.Element {
  return (
    <section className={styles.features}>
      <div className="container">
        <div className="row">
          {FeatureList.map((props, idx) => (
            <Feature key={idx} {...props} />
          ))}
        </div>
      </div>
    </section>
  );
}
