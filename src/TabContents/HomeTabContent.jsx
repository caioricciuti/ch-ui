import { Github, Database } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import clickHouseSvg from "/ch_logo.svg";
import { useTabState } from "@/providers/TabsStateContext";

export default function HomeTabContent() {
  const { addQueryTab } = useTabState();

  const projects = [
    {
      title: "Star us on GitHub!",
      description:
        "Star us on GitHub if you like this project. It helps us a lot, and it also helps other people find this project.",
      link: "https://github.com/caioricciuti/ch-ui",
      icon: <Github />,
    },
    {
      description:
        "ClickHouse is a fast open-source OLAP database management system, designed for big data analytics. Ah, also, it's open-source.",
      link: "https://clickhouse.com/?utm_source=clickhouse-ui-app&utm_medium=home-tab-card",
      icon: <img src={clickHouseSvg} alt="ClickHouse" />,
    },
    {
      title: "Start Querying",
      description:
        "Create and run queries on your ClickHouse instance. You can also save your queries for later use.",
      icon: <Database />,
      cta: "Let's go!",
      action: () => addQueryTab(),
    },
  ];
  return (
    <>
      <div className="h-[88vh] relative w-full flex flex-col p-10 overflow-hidden rounded-md">
        <div className="flex flex-col h-full">
          <div className="flex flex-wrap gap-4">
            {projects.map((project, index) => (
              <Card key={index} className="flex-1">
                <CardHeader>
                  <div className="flex items-center justify-between">
                    <CardTitle>
                      <a
                        href={project.link}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="font-semibold hover:underline"
                      >
                        {project.title || "ClickHouse"}
                      </a>
                    </CardTitle>
                    <div className="flex-shrink-0 w-12 h-12 bg-muted rounded-lg p-3">
                      {project.icon}
                    </div>
                  </div>
                </CardHeader>
                <CardContent>
                  <div className="flex items-center gap-4">
                    <div>
                      <p className="text-sm text-gray-500">
                        {project.description}
                      </p>
                      {project.cta && (
                        <button
                          onClick={project.action}
                          className="text-blue-500 mt-2 hover:underline"
                        >
                          {project.cta}
                        </button>
                      )}
                    </div>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        </div>
      </div>
    </>
  );
}
