//Home Page Features

import React from "react";
import {
  DatabaseZap,
  SquareTerminal,
  Table2,
  ChartArea,
  Activity,
  CommandIcon,
  DownloadCloud,
  LaptopMinimalCheck,
} from "lucide-react";

type FeatureItem = {
  title: string;
  Icon: React.ElementType;
  description: string;
};

const FeatureList: FeatureItem[] = [
  {
    title: "Data Explorer",
    Icon: DatabaseZap,
    description:
      "Browse your databases, tables and files effortlessly with our intuitive data explorer.",
  },
  {
    title: "SQL Editor",
    Icon: SquareTerminal,
    description:
      "Write and run queries with our powerful SQL editor. Enjoy syntax highlighting and auto-completion.",
  },
  {
    title: "Data Visualization",
    Icon: Table2,
    description:
      "View your data in interactive tables. Easily filter, sort, and export your data.",
  },
  {
    title: "Data Export",
    Icon: DownloadCloud,
    description:
      "Export your data in JSON/CSV. Download your query results with a single click.",
  },
  {
    title: "Data Insights",
    Icon: ChartArea,
    description:
      "Gain valuable insights from your data with ease. Uncover patterns and trends quickly.",
  },
  {
    title: "Fast and Efficient",
    Icon: Activity,
    description:
      "Simply twick your queries to get the metrics you need, fast and efficient.",
  },
  {
    title: "Simple and Intuitive",
    Icon: CommandIcon,
    description:
      "CH-UI is designed to be user-friendly and easy to navigate for beginners and experts alike.",
  },
  {
    title: "Easy to deploy",
    Icon: LaptopMinimalCheck,
    description:
      "With only 1 command spin the image and start querying your data.",
  },
];

function FeatureCard({ title, Icon, description }: FeatureItem) {
  return (
    <div className="feature-card">
      <div className="feature-card-content">
        <Icon className="feature-card-icon" />
        <h3 className="feature-card-title">{title}</h3>
        <p className="feature-card-description">{description}</p>
      </div>
    </div>
  );
}

export default function HomepageFeatures(): JSX.Element {
  return (
    <>
      <section className="features-section">
        <div className="features-header">
          <h2 className="gradient-text">CH-UI Features</h2>
        </div>
        <div className="features-grid">
          {FeatureList.map((feature, index) => (
            <FeatureCard key={index} {...feature} />
          ))}
        </div>
      </section>
    </>
  );
}
