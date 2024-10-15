"use client";
import React from "react";
import {
  DatabaseZap,
  SquareTerminal,
  Table2,
  ChartArea,
  Activity,
  CommandIcon,
  Network,
  DownloadCloud,
} from "lucide-react";
import { Marquee } from "../ui/Marquee";

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
      "Browse your databases and tables effortlessly with our intuitive data explorer.",
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
    title: "Connect to ClickHouse",
    Icon: Network,
    description:
      "Use your ClicHhouse instance with CH-UI. Connect to your instance with a few clicks.",
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
    title: "Instance Metrics",
    Icon: Activity,
    description:
      "Monitor your instance performance with real-time metrics. Keep track of system health.",
  },
  {
    title: "Simple and Intuitive",
    Icon: CommandIcon,
    description:
      "CH-UI is designed to be user-friendly and easy to navigate for beginners and experts alike.",
  },
];

function FeatureCard({ title, Icon, description }: FeatureItem) {
  return (
    <div className="w-72 flex-shrink-0 cursor-pointer overflow-hidden rounded-xl border-2 border-orange-500 p-4 mx-4">
      <div className="flex flex-col items-center text-center">
        <Icon size={48} className="text-orange-500 mb-4" />
        <h3 className="text-lg font-semibold mb-2">{title}</h3>
        <p className="text-sm ">{description}</p>
      </div>
    </div>
  );
}

export default function HomepageFeatures(): JSX.Element {
  const firstRow = FeatureList.slice(0, FeatureList.length / 2);
  const secondRow = FeatureList.slice(FeatureList.length / 2);

  return (
    <section className="w-full">
      <div className="w-full">
        <div className="text-center">
          <h2 className="text-4xl mb-12 md:mb-24 font-extrabold tracking-tight sm:text-5xl md:text-6xl lg:text-7xl bg-clip-text text-transparent bg-gradient-to-r from-orange-700 to-orange-300">
            Features
          </h2>
        </div>
        <div className="relative h-[450px] md:h-[600px] lg:h-[600px] w-full overflow-hidden rounded-lg ">
          <Marquee pauseOnHover className="[--duration:60s]">
            {firstRow.map((feature, index) => (
              <FeatureCard key={index} {...feature} />
            ))}
          </Marquee>
          <Marquee reverse pauseOnHover className="[--duration:40s] mt-8">
            {secondRow.map((feature, index) => (
              <FeatureCard key={index} {...feature} />
            ))}
          </Marquee>
        </div>
      </div>
    </section>
  );
}
