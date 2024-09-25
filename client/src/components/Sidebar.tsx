import { useState, useRef } from "react";
import { Link, useNavigate, useLocation } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import { CommandShortcut } from "@/components/ui/command";
import {
  LogOut,
  CogIcon,
  Users,
  Layers,
  BarChart3,
  Shield,
  Key,
  SearchCode,
  ChevronRight,
  ChevronLeft,
  Bell,
} from "lucide-react";

import Logo from "/logo.png";
import useAuthStore from "@/stores/user.store";
import OrganizationCredentialSelector from "@/components/OrganizationCredentialSelector";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { ScrollArea } from "@/components/ui/scroll-area";
import { getInitials, bgColorsByInitials } from "@/lib/helpers";

const Sidebar = () => {
  const { user, logout, admin } = useAuthStore();
  const navigate = useNavigate();
  const location = useLocation();
  const [isExpanded, setIsExpanded] = useState(false);

  const sidebarRef = useRef<HTMLDivElement>(null);

  const handleLogout = async () => {
    try {
      await logout();
      navigate("/account/login");
      toast.success("Logged out successfully");
    } catch (error) {
      console.error("Logout failed:", error);
      toast.error("Logout failed. Please try again.");
    }
  };

  const toggleSidebar = () => {
    setIsExpanded(!isExpanded);
  };

  const navItems = [
    { to: "/organizations", label: "Organizations", icon: Users },
    { to: "/credentials", label: "Credentials", icon: Key },
    { to: "/workspace", label: "Workspace", icon: Layers },
    { to: "/metrics", label: "Metrics", icon: BarChart3 },
    {
      to: "/notifications",
      label: "Notifications",
      icon: Bell,
    },
  ];

  const isActive = (path: string) => location.pathname === path;

  return (
    <div
      ref={sidebarRef}
      className={`flex flex-col h-screen bg-background border-r transition-all duration-300 ${
        isExpanded ? "w-64" : "w-16"
      }`}
    >
      <div className="p-4 flex items-center justify-between">
        <Link
          to="/"
          className={`flex items-center space-x-2 ${
            !isExpanded ? "justify-center" : ""
          }`}
        >
          <img src={Logo} alt="Logo" className="h-8 w-8 min-h-8 min-w-8" />
          {isExpanded && (
            <span className="font-bold text-lg truncate">CH-UI</span>
          )}
        </Link>
      </div>

      <ScrollArea className="flex-grow">
        <nav className="space-y-2 p-2">
          {admin() && (
            <Link
              to="/admin"
              className={`flex items-center px-3 py-2 rounded-md text-sm font-medium transition-colors ${
                isActive("/admin")
                  ? "bg-secondary text-secondary-foreground"
                  : "hover:bg-secondary/80"
              }`}
            >
              <Shield className={`h-5 w-5 ${!isExpanded ? "" : "mr-2"}`} />
              {isExpanded && <span>Admin</span>}
            </Link>
          )}
          {navItems.map((item) => (
            <Link
              key={item.to}
              to={item.to}
              className={`flex items-center justify-between px-3 py-2 rounded-md text-sm font-medium transition-colors ${
                isActive(item.to)
                  ? "bg-secondary text-secondary-foreground"
                  : "hover:bg-secondary/80"
              }`}
            >
              <div className="flex items-center">
                <item.icon className={`h-5 w-5 ${!isExpanded ? "" : "mr-2"}`} />
                {isExpanded && <span>{item.label}</span>}
              </div>
            </Link>
          ))}
        </nav>
      </ScrollArea>

      <div className="px-4">
        <OrganizationCredentialSelector isExpanded={isExpanded} />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              className={`w-full justify-start ${
                !isExpanded ? "px-0" : "px-2"
              } mt-2`}
            >
              <Avatar className="h-8 w-8">
                <AvatarFallback
                  className={`h-8 w-8 font-bold ${bgColorsByInitials(
                    getInitials(user?.name || "")
                  )}`}
                >
                  {getInitials(user?.name || "")}
                </AvatarFallback>
              </Avatar>
              {isExpanded && (
                <span className="ml-2 text-sm font-medium">{user?.name}</span>
              )}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent className="w-56" align="end" forceMount>
            <DropdownMenuItem className="flex flex-col items-start">
              <div className="text-sm font-medium">{user?.name}</div>
              <div className="text-xs text-muted-foreground">{user?.email}</div>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => navigate("/settings")}>
              <CogIcon className="mr-2 h-4 w-4" />
              Settings
            </DropdownMenuItem>
            <DropdownMenuItem
              onSelect={() =>
                document.dispatchEvent(
                  new KeyboardEvent("keydown", {
                    key: "k",
                    metaKey: true,
                    bubbles: true,
                  })
                )
              }
            >
              <SearchCode className="mr-2 h-4 w-4" />
              Search <CommandShortcut>⌘K</CommandShortcut>
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={handleLogout}>
              <LogOut className="mr-2 h-4 w-4" />
              Log out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <Button
        variant="link"
        className="w-full flex justify-center items-center"
        onClick={toggleSidebar}
      >
        {isExpanded ? (
          <>
            <ChevronLeft className="h-5 w-5" />
            <span className="ml-2">Collapse</span>
          </>
        ) : (
          <ChevronRight className="h-5 w-5" />
        )}
      </Button>
    </div>
  );
};

export default Sidebar;
