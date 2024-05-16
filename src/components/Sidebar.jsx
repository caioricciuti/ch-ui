import { useClickHouseState } from "@/providers/ClickHouseContext";
import { Link, useLocation } from "react-router-dom";
import {
  SquareTerminal,
  Github,
  Loader2,
  CircleCheckIcon,
  AlertCircleIcon,
  Sun,
  Moon,
  LifeBuoy,
  CornerDownLeft,
  ServerCogIcon,
  Settings2,
} from "lucide-react";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";

import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { CommandShortcut } from "@/components/ui/command";
import { useTheme } from "@/providers/theme";
import { Button } from "@/components/ui/button";

const commandsSheet = [
  {
    action: "Save Query",
    command: ["S/Ctrl+S"],
    context: "Query Editor",
  },
  {
    action: "Run Query",
    command: [
      "⌘",
      "+",
      <CornerDownLeft size={16} />,
      "/Ctrl",
      "+",
      <CornerDownLeft size={16} />,
    ],
    context: "Query Editor",
  },
];
export default function Sidebar() {
  const { theme, setTheme } = useTheme(); // Use the theme context
  const { isServerAvailable, isLoading, version } = useClickHouseState();
  const location = useLocation();

  const toggleTheme = () => {
    const newTheme = theme === "dark" ? "light" : "dark";
    setTheme(newTheme);
  };

  return (
    <aside className="inset-y fixed left-0 z-20 flex h-full flex-col border-r items-center">
      <div className="border-b p-2">
        <Button variant="ghost" size="icon" aria-label="Home">
          <Github
            className="size-6"
            onClick={() => {
              // new blank window with the url
              window.open(
                "https://github.com/caioricciuti/ch-ui",
                "_blank"
              );
            }}
          />
        </Button>
      </div>
      <div className="flex flex-col gap-3 items-center p-2">
        <Link
          to="/"
          className={`flex items-center p-2 rounded-md ${
            location.pathname === "/" ? "bg-secondary" : ""
          }`}
        >
          <SquareTerminal size={24} />
        </Link>
        <Link
          to="/instance-metrics"
          className={`flex items-center p-2 rounded-md ${
            location.pathname === "/instance-metrics" ? "bg-secondary" : ""
          }`}
        >
          <ServerCogIcon size={24} />
        </Link>
        <Link
          to="/settings"
          className={`flex items-center p-2 rounded-md ${
            location.pathname === "/settings" ? "bg-secondary" : ""
          }`}
        >
          <Settings2 size={24} />
        </Link>
      </div>

      <nav className="mt-auto grid gap-1 p-2">
        {/* commands sheet */}
        <Sheet>
          <SheetTrigger>
            <LifeBuoy className="size-5 m-auto" />
          </SheetTrigger>
          <SheetContent>
            <SheetHeader>
              <SheetTitle>Command Cheat Sheet</SheetTitle>
              <SheetDescription>
                Here are some useful commands to give you ever a better
                experience.
              </SheetDescription>
            </SheetHeader>

            <Table className="rounded-lg mt-6">
              <TableHeader>
                <TableRow>
                  <TableHead>Action</TableHead>
                  <TableHead>Command</TableHead>
                  <TableHead>Context</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody className="text-center">
                {commandsSheet.map((command) => (
                  <TableRow key={command.action} className="">
                    <TableCell>{command.action}</TableCell>
                    <TableCell className="flex items-center justify-center">
                      <CommandShortcut className="bg-muted p-1 rounded-full flex">
                        {/* Map each part of the command array to correctly render text and icons */}
                        {command.command.map((part, i) => (
                          <span className="flex" key={i}>
                            {part}
                          </span>
                        ))}
                      </CommandShortcut>
                    </TableCell>
                    <TableCell>{command.context}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </SheetContent>
        </Sheet>

        {/* theme changer */}
        <Button
          variant="ghost"
          size="icon"
          className="rounded-lg mt-2"
          aria-label="Theme"
          onClick={toggleTheme}
        >
          {theme === "dark" ? (
            <Sun className="size-5" />
          ) : (
            <Moon className="size-5" />
          )}
        </Button>

        {/* server status */}
        <Popover>
          <PopoverTrigger>
            {isLoading ? (
              <Button variant="link" size="icon" aria-label="Help">
                <Loader2 className="size-6 animate-spin" />
              </Button>
            ) : isServerAvailable && !isLoading ? (
              <Button variant="ghost" size="icon" aria-label="Help">
                <CircleCheckIcon className="size-6 text-green-500" />
              </Button>
            ) : (
              <Button variant="ghost" size="icon" aria-label="Help">
                <AlertCircleIcon className="size-6 text-red-500" />
              </Button>
            )}
          </PopoverTrigger>
          <PopoverContent className="ml-2 w-full">
            <div className="flex items-center">
              <p className="text-xs">
                Server Status:{" "}
                {isLoading
                  ? "Loading..."
                  : isServerAvailable
                  ? "Online"
                  : "Offline"}
              </p>
            </div>
            <div className="flex items-center">
              <p className="text-xs">Server Version: {version}</p>
            </div>
          </PopoverContent>
        </Popover>
      </nav>
    </aside>
  );
}
