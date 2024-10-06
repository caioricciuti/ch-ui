import React from "react";
import {
  FiDatabase,
  FiEdit3,
  FiTable,
  FiBarChart2,
  FiActivity,
  FiCommand,
} from "react-icons/fi";
import { motion } from "framer-motion";

type FeatureItem = {
  title: string;
  Icon: React.ElementType;
  description: string;
};

const FeatureList: FeatureItem[] = [
  {
    title: "Data Explorer",
    Icon: FiDatabase,
    description:
      "Browse your databases and tables effortlessly with our intuitive data explorer. Quickly find and access the information you need.",
  },
  {
    title: "SQL Editor",
    Icon: FiEdit3,
    description:
      "Write and run queries with our powerful SQL editor. Enjoy features like syntax highlighting and auto-completion for efficient coding.",
  },
  {
    title: "Data Visualization",
    Icon: FiTable,
    description:
      "View your data in interactive tables. Easily filter, sort, and export your data for further analysis or reporting.",
  },
  {
    title: "Data Insights",
    Icon: FiBarChart2,
    description:
      "Gain valuable insights from your data with ease. Our tools help you uncover patterns and trends, making data-driven decisions simpler than ever.",
  },
  {
    title: "Instance Metrics",
    Icon: FiActivity,
    description:
      "Monitor your instance performance with real-time metrics. Keep track of system health and optimize your database operations.",
  },
  {
    title: "Simple and Intuitive",
    Icon: FiCommand,
    description:
      "CH-UI is designed to be user-friendly and easy to navigate. Whether you're a beginner or an expert, you'll feel right at home.",
  },
];

function Feature({ title, Icon, description }: FeatureItem) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 20 }}
      whileInView={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5 }}
      viewport={{ once: true }}
      className="flex flex-col items-center p-6 rounded-lg transition-all duration-300 transform hover:scale-105 hover:shadow-xl"
    >
      <div className="text-orange-500 dark:text-orange-400 mb-4">
        <Icon className="w-12 h-12" />
      </div>
      <h3 className="text-xl font-semibold mb-2 ">{title}</h3>
      <p className="text-center">{description}</p>
    </motion.div>
  );
}

export default function HomepageFeatures(): JSX.Element {
  return (
    <section className="py-24 0">
      <div className="container mx-auto px-4">
        <h2 className="text-4xl font-bold text-center mb-16 text-gray-800 dark:text-gray-200">
          Key Features
        </h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-8">
          {FeatureList.map((props, idx) => (
            <Feature key={idx} {...props} />
          ))}
        </div>
      </div>
    </section>
  );
}
