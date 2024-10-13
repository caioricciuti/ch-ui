"use client";
import {
  ChartAreaIcon,
  Download,
  TerminalSquare,
  Newspaper,
} from "lucide-react";
import { BentoCard, BentoGrid } from "@/components/ui/BentoGrid";

const features = [
  {
    Icon: Download,
    name: "Export your data",
    description: "Export your data in JSON and/or CSV.",
    href: "/docs/getting-started",
    className: "col-span-3 lg:col-span-1",
    cta: "Learn more",
    background: { light: "", dark: "" },
  },
  {
    Icon: ChartAreaIcon,
    name: "Metrics",
    description:
      "Monitor your instance performance with all important metrics.",
    href: "/docs/core-concepts",
    cta: "Understand more",
    className: "col-span-3 lg:col-span-2",
    background: {
      light: "/chart-light.png",
      dark: "/chart-dark.png",
    },
  },
  {
    Icon: TerminalSquare,
    name: "Sql Editor",
    description:
      "Use the SQL editor to write and execute queries. Whith syntax highlighting and autocompletion.",
    href: "/docs/",
    cta: "Learn more",
    className: "col-span-3 lg:col-span-2",
    background: {
      light: "/sql-light.png",
      dark: "/sql-dark.png",
    },
  },
  {
    Icon: Newspaper,
    name: "Check the latest news!",
    description: "Check the latest news about CH-UI.",
    className: "col-span-3 lg:col-span-1",
    href: "/blog",
    cta: "Take me to the blog",
    background: { light: "", dark: "" },
  },
];

export function ShowCase() {
  return (
    <section>
      <div className="text-center">
        <h2 className="text-3xl mb-20 font-extrabold tracking-tight sm:text-4xl md:text-5xl lg:text-6xl bg-clip-text text-transparent bg-gradient-to-r from-orange-300 to-orange-600">
          Yeah you can do that!
        </h2>
      </div>
      <BentoGrid>
        {features.map((feature, idx) => (
          <BentoCard key={idx} {...feature} />
        ))}
      </BentoGrid>
    </section>
  );
}
