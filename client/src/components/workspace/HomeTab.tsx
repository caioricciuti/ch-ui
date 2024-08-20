import { Button } from "@/components/ui/button";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardFooter,
} from "@/components/ui/card";
import { Github, Link, ExternalLink } from "lucide-react";
import useAuthStore from "@/stores/user.store";
import useTabStore from "@/stores/tabs.store";

import { bgGradientByInitials } from "@/lib/helpers";

const homeTabCards = [
  {
    title: "Star us on GitHub!",
    description:
      "Star us on GitHub if you like this project. It helps us a lot, and it also helps other people find this project.",
    link: "https://github.com/caioricciuti/ch-ui",
    Icon: Github,
    action: "Star on GitHub",
  },
  {
    title: "ClickHouse",
    description:
      "ClickHouse is a fast open-source OLAP database management system, designed for big data analytics. Ah, also, it's open-source.",
    Icon: ExternalLink,
    link: "https://clickhouse.tech/docs/en/",
    action: "Click House Docs",
  },
  {
    title: "Start Querying",
    description:
      "Create and run queries on your ClickHouse instance. You can also save your queries for later use.",
    action: "Create a new query",
    Icon: Link,
  },
  {
    title: "CH-UI Docs",
    Icon: ExternalLink,
    description:
      "Learn more about CH-UI and how to use it. You can also contribute to the project.",
    link: "https://ch-ui.caioricciuti.com",
    action: "CH-UI Docs",
  },
];

interface HomeTabCard {
  title: string;
  description: string;
  link?: string;
  Icon?: React.FC;
  action: string;
}

const HomeTab = () => {
  const { addTab } = useTabStore();
  const { user } = useAuthStore();

  const handleAction = (card: HomeTabCard) => {
    if (card.link) {
      window.open(card.link, "_blank");
    } else if (card.title === "Start Querying") {
      addTab({
        type: "sql",
        title: "New Query",
        content: "",
      });
    } else {
      console.log(`Action for ${card.title}`);
    }
  };

  return (
    <div className="h-screen w-full p-6 space-y-6 flex flex-col">
      <h1 className="text-3xl font-bold mb-6">
        Welcome,{" "}
        <span
          className={`
        ${bgGradientByInitials(user?.name ?? "")}
        text-transparent bg-clip-text 
        `}
        >
          {user?.name ?? ""}!
        </span>
      </h1>
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {homeTabCards.map((card, index) => (
          <Card key={index} className="flex flex-col">
            <CardHeader>
              <CardTitle className="flex items-center space-x-2">
                {card.Icon && <card.Icon className="w-6 h-6" />}
                <span>{card.title}</span>
              </CardTitle>
              <CardDescription>{card.description}</CardDescription>
            </CardHeader>
            <CardFooter>
              <Button
                className="w-full"
                variant={index === 2 ? "default" : "outline"}
                onClick={() => handleAction(card)}
              >
                {card.action}
              </Button>
            </CardFooter>
          </Card>
        ))}
      </div>
    </div>
  );
};

export default HomeTab;
