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
import useAppStore from "@/store/appStore";
import { motion } from "framer-motion";

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
  const { addTab } = useAppStore();

  const handleAction = (card: HomeTabCard) => {
    if (card.link) {
      window.open(card.link, "_blank");
    } else if (card.title === "Start Querying") {
      addTab({
        id: Math.random().toString(36).substr(2, 9),
        type: "sql",
        title: "Start Query",
        content: "",
      });
    } else {
      console.log(`Action for ${card.title}`);
    }
  };

  return (
    <div className="p-2 mt-4 space-y-8 flex flex-col h-full overflow-auto w-full">
      <motion.h1
        //text gradient color from orange to orange-red
        className="text-6xl font-extrabold bg-gradient-to-r from-orange-600 via-pink-300 to-orange-300 inline-block text-transparent bg-clip-text"
        initial={{ opacity: 0, y: -20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 1.5 }}
      >
        Welcome! 🚀
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
    </div>
  );
};

export default HomeTab;
