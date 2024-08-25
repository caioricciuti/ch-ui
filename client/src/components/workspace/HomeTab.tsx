import React from "react";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardFooter,
} from "@/components/ui/card";
import { Github, ExternalLink, Terminal, BookOpen } from "lucide-react";
import useAuthStore from "@/stores/user.store";
import useTabStore from "@/stores/tabs.store";
import { motion } from "framer-motion";
import { bgGradientByInitials } from "@/lib/helpers";

const homeTabCards = [
  {
    title: "Star us on GitHub!",
    description:
      "Support our project by starring it on GitHub. Your star helps us grow and reach more developers!",
    link: "https://github.com/caioricciuti/ch-ui",
    Icon: Github,
    action: "Star on GitHub",
    color: "bg-purple-600",
  },
  {
    title: "ClickHouse Docs",
    description:
      "Explore ClickHouse, the lightning-fast open-source OLAP database designed for big data analytics.",
    Icon: BookOpen,
    link: "https://clickhouse.com/docs/en/intro",
    action: "Read Docs",
    color: "bg-yellow-500",
  },
  {
    title: "Start Querying",
    description:
      "Jump right in! Create and run queries on your ClickHouse instance. Save your work for future use.",
    action: "New Query",
    Icon: Terminal,
    color: "bg-green-500",
  },
  {
    title: "CH-UI Documentation",
    Icon: ExternalLink,
    description:
      "Learn how to make the most of CH-UI. Contribute to our open-source project and help shape its future!",
    link: "https://ch-ui.caioricciuti.com",
    action: "Explore CH-UI",
    color: "bg-blue-500",
  },
];

interface HomeTabCard {
  title: string;
  description: string;
  link?: string;
  Icon?: React.FC<{ className?: string }>;
  action: string;
  color: string;
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
    <div className="p-2 mt-4 space-y-8 flex flex-col h-screen overflow-auto w-full">
      <motion.h1
        className="text-4xl font-bold"
        initial={{ opacity: 0, y: -20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 1.5 }}
      >
        Welcome,{" "}
        <span
          className={`
            ${bgGradientByInitials(user?.name ?? "Guest")}
            text-transparent bg-clip-text 
          `}
        >
          {user?.name ?? "Guest"}!
        </span>
      </motion.h1>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6 px-4">
        {homeTabCards.map((card, index) => (
          <motion.div
            key={index}
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5, delay: index * 0.3 }}
          >
            <Card className="flex flex-col h-full transition-all duration-300 hover:shadow-lg hover:scale-105">
              <CardHeader className="flex-grow">
                <CardTitle className="flex items-center space-x-3">
                  {card.Icon && (
                    <div className={`p-2 rounded-full ${card.color}`}>
                      <card.Icon className="w-6 h-6" />
                    </div>
                  )}
                  <span>{card.title}</span>
                </CardTitle>
                <CardDescription className="mt-2">
                  {card.description}
                </CardDescription>
              </CardHeader>
              <CardFooter>
                <Button
                  className="w-full font-semibold"
                  variant={
                    card.title === "Start Querying" ? "default" : "outline"
                  }
                  onClick={() => handleAction(card)}
                >
                  {card.action}
                </Button>
              </CardFooter>
            </Card>
          </motion.div>
        ))}
      </div>
      <motion.div
        className="relative bottom-2 text-xs text-muted-foreground"
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ duration: 0.5, delay: 0.8 }}
      >
        Made with ❤️ by the CH-UI Team
      </motion.div>
    </div>
  );
};

export default HomeTab;
