import { useState, useEffect, useRef } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { useTheme } from "@/components/theme-provider";
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
  Settings2,
  Search,
  ChevronRight,
  ChevronLeft,
  LineChart,
  BookText,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { toast } from "sonner";
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
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "@/components/ui/command";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import Logo from "/logo.png";
import useAppStore from "@/store/appStore";

const commandsSheet = [
  {
    action: "Expand/Shrink Sidebar",
    command: ["⌘/Ctrl", "+", "B"],
    context: "Global",
  },
  {
    action: "Search Bar",
    command: ["⌘/Ctrl", "+", "K"],
    context: "Global",
  },
  {
    action: "Switch Tab",
    command: ["⌘/Ctrl", "+", "Tab Number"],
    context: "Home/Workspace",
  },
  {
    action: "Run Query",
    command: ["⌘/Ctrl", "+", "Enter"],
    context: "Home/Workspace",
  },
];

const Sidebar = () => {
  const { theme, setTheme } = useTheme();
  const { isServerAvailable, version, isLoadingCredentials, clearCredentials } =
    useAppStore();
  const location = useLocation();
  const navigate = useNavigate();
  const [isExpanded, setIsExpanded] = useState(false);
  const [open, setOpen] = useState(false);
  const sidebarRef = useRef<HTMLDivElement>(null);

  const toggleTheme = () => {
    setTheme(theme === "dark" ? "light" : "dark");
  };

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setOpen((open) => !open);
      }
      // Toogle sidebar when pressing Cmd/Ctrl + B
      if (e.key === "b" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setIsExpanded((isExpanded) => !isExpanded);
      }
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, []);

  const navItems = [
    { to: "/", label: "Home", icon: SquareTerminal },
    { to: "/metrics", label: "Metrics", icon: LineChart },
    { to: "/settings", label: "Settings", icon: Settings2 },
    {
      to: "https://github.com/caioricciuti/ch-ui?utm_source=ch-ui&utm_medium=sidebar",
      label: "GitHub",
      icon: Github,
      isNewWindow: true,
    },
    {
      to: "https://ch-ui.caioricciuti.com/docs?utm_source=ch-ui&utm_medium=sidebar",
      label: "Documentation",
      icon: BookText,
      isNewWindow: true,
    },
  ];

  return (
    <div
      ref={sidebarRef}
      className={`flex flex-col h-screen bg-background border-r transition-all duration-300 ${
        isExpanded ? "w-64" : "w-16"
      }`}
    >
      <div className="p-2 ml-2 mt-2 flex items-center justify-between w-full">
        <Link to="/" className="flex items-center space-x-2">
          <img src={Logo} alt="Logo" className="h-8 w-8" />
          {isExpanded && (
            <span className="font-bold text-lg truncate">CH-UI</span>
          )}
        </Link>
        {isExpanded && (
          <Button
            variant="ghost"
            size="icon"
            onClick={() => setIsExpanded(false)}
          >
            <ChevronLeft className="h-4 w-4" />
          </Button>
        )}
      </div>

      <ScrollArea className="flex-grow">
        <nav className="space-y-1 p-2">
          {navItems.map((item) => (
            <Link
              key={item.to}
              to={item.to}
              target={item.isNewWindow ? "_blank" : "_self"}
              className={`flex items-center px-3 py-2 rounded-md text-sm font-medium transition-colors ${
                location.pathname === item.to
                  ? "bg-secondary text-secondary-foreground"
                  : "hover:bg-secondary/80"
              }`}
            >
              <item.icon className={`h-5 w-5 ${isExpanded ? "mr-2" : ""}`} />
              {isExpanded && <span>{item.label}</span>}
            </Link>
          ))}
        </nav>
      </ScrollArea>

      <div className="w-full">
        <div
          className={`${isExpanded ? "flex justify-around" : "block p-2.5"}`}
        >
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => setOpen(true)}
                >
                  <Search className="h-5 w-5" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                <span className="text-xs">Search (Cmd/Ctrl + K)</span>
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>

          <Button variant="ghost" size="icon" onClick={toggleTheme}>
            {theme === "dark" ? (
              <Sun className="h-5 w-5" />
            ) : (
              <Moon className="h-5 w-5" />
            )}
          </Button>

          <Sheet>
            <SheetTrigger asChild>
              <Button variant="ghost" size="icon">
                <LifeBuoy className="h-5 w-5" />
              </Button>
            </SheetTrigger>
            <SheetContent className="xl:w-[1000px] xl:max-w-none sm:w-[400px] sm:max-w-[540px]">
              <SheetHeader>
                <SheetTitle>Command Cheat Sheet</SheetTitle>
                <SheetDescription>
                  Useful commands to enhance your experience.
                </SheetDescription>
              </SheetHeader>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Action</TableHead>
                    <TableHead>Command</TableHead>
                    <TableHead>Context</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {commandsSheet.map((command) => (
                    <TableRow key={command.action}>
                      <TableCell>{command.action}</TableCell>
                      <TableCell>
                        <CommandShortcut>
                          {command.command.map((part, i) => (
                            <span key={i}>{part}</span>
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
        </div>

        <Popover>
          <PopoverTrigger asChild>
            <Button
              className={`${
                isExpanded
                  ? "w-full justify-items-stretch mb-2"
                  : "m-auto w-full"
              } hover:bg-transparent bg-transparent text-primary`}
            >
              {isLoadingCredentials ? (
                <Loader2 className="h-5 w-5 animate-spin" />
              ) : isServerAvailable ? (
                <CircleCheckIcon className="h-5 w-5 text-green-500" />
              ) : (
                <AlertCircleIcon className="h-5 w-5 text-red-500" />
              )}
              {isExpanded && (
                <span className="ml-2">
                  {isLoadingCredentials
                    ? "Connecting..."
                    : isServerAvailable
                    ? "Connected"
                    : "Disconnected"}
                </span>
              )}
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-auto p-2">
            <div className="space-y-1">
              <p className="text-sm font-medium">Server Status</p>
              <p className="text-xs">
                {isLoadingCredentials
                  ? "Connecting..."
                  : isServerAvailable
                  ? "Connected"
                  : "Disconnected"}
              </p>
              <p className="text-xs">Version: {version}</p>
            </div>
          </PopoverContent>
        </Popover>
      </div>

      {!isExpanded && (
        <Button
          variant="ghost"
          size="icon"
          className="m-2"
          onClick={() => setIsExpanded(true)}
        >
          <ChevronRight className="h-4 w-4" />
        </Button>
      )}

      <CommandDialog open={open} onOpenChange={setOpen}>
        <CommandInput placeholder="Type a command or search..." />
        <CommandList>
          <CommandEmpty>No results found.</CommandEmpty>
          <CommandGroup heading="Navigation">
            {navItems.map((item) => (
              <CommandItem
                key={item.to}
                onSelect={() => {
                  if (item.isNewWindow) {
                    window.open(item.to, "_blank");
                    setOpen(false);
                    return;
                  }
                  navigate(item.to);
                  setOpen(false);
                }}
              >
                <item.icon className="mr-2 h-4 w-4" />
                {item.label}
              </CommandItem>
            ))}
          </CommandGroup>
          <CommandGroup heading="Actions">
            <CommandItem
              onSelect={() => {
                toast.success("Theme changed!");
                toggleTheme();
                setOpen(false);
              }}
            >
              Toggle Theme
            </CommandItem>
            <CommandItem
              onSelect={() => {
                clearCredentials();
                toast.success("Credentials cleared!");
                setOpen(false);
              }}
            >
              Reset Credentials
            </CommandItem>
          </CommandGroup>
        </CommandList>
      </CommandDialog>
    </div>
  );
};

export default Sidebar;
