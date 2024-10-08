import {
  ChartAreaIcon,
  Download,
  Share2Icon,
  Calendar,
  TerminalSquare,
  NetworkIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { BentoCard, BentoGrid } from "@/components/ui/BentoGrid";
import { Marquee } from "@/components/ui/Marquee";

const files = [
  {
    name: "ch-ui.json",
    body: "{ 'name': 'CH-UI', 'description': 'Just work with your data' }",
  },
  {
    name: "ch-ui.csv",
    body: "export,import\n'CH-UI','Just work with your data'",
  },
  {
    name: "getStart.txt",
    body: "Welcome to CH-UI! Start working with your data.",
  },
];

const features = [
  {
    Icon: Download,
    name: "Export your data",
    description: "Export your data in JSON and/or CSV.",
    href: "/docs/getting-started",
    className: "col-span-3 lg:col-span-1",
    cta: "Learn more",
    background: {},
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
    Icon: NetworkIcon,
    name: "Connect to your instance",
    description: "Connect to your ClickHouse instance with ease.",
    className: "col-span-3 lg:col-span-1",
    href: "/docs/acknowledgements",
    cta: "See CH-UI acknowledgements",
    background: {},
  },
];

export function ShowCase() {
  return (
    <section>
      <div className="text-center">
        <h2 className="text-2xl mb-20 font-extrabold tracking-tight sm:text-5xl md:text-6xl lg:text-7xl bg-clip-text text-transparent bg-gradient-to-r from-orange-300 to-orange-600">
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
