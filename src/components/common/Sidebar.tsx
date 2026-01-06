import { Link, useLocation, useNavigate } from "react-router-dom";
import {
  LayoutDashboard,
  Server,
  Settings,
  Activity,
  LogOut,
  Database,
  FileText,
  Shield,
  ChevronLeft,
  ChevronRight,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  TooltipProvider,
} from "@/components/ui/tooltip";
import useAppStore from "@/store";
import { useState } from "react";
import { motion } from "framer-motion";
import { withBasePath } from "@/lib/basePath";

interface SidebarItemProps {
  icon: React.ElementType;
  label: string;
  to: string;
  isActive?: boolean;
  isCollapsed: boolean;
}

const SidebarItem = ({ icon: Icon, label, to, isActive, isCollapsed }: SidebarItemProps) => {
  return (
    <TooltipProvider delayDuration={0}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Link
            to={to}
            className={cn(
              "flex items-center gap-3 rounded-lg px-3 py-2.5 transition-all duration-300 group relative overflow-hidden",
              isActive
                ? "bg-white/10 text-white shadow-lg shadow-purple-500/10 ring-1 ring-white/10"
                : "text-gray-400 hover:text-white hover:bg-white/5",
              isCollapsed && "justify-center px-2"
            )}
          >
            {isActive && (
              <div className="absolute inset-0 bg-gradient-to-r from-purple-500/10 to-blue-500/10 opacity-50" />
            )}
            <Icon className={cn("h-5 w-5 transition-transform group-hover:scale-110", isActive && "text-purple-400")} />
            {!isCollapsed && (
              <span className={cn("font-medium z-10", isActive && "text-white")}>{label}</span>
            )}

            {/* Active Indicator Line */}
            {isActive && !isCollapsed && (
              <motion.div
                layoutId="sidebar-active"
                className="absolute left-0 top-0 bottom-0 w-1 bg-purple-500 rounded-r-full"
              />
            )}
          </Link>
        </TooltipTrigger>
        {isCollapsed && (
          <TooltipContent side="right" className="bg-black/80 text-white border-white/10 backdrop-blur-md">
            {label}
          </TooltipContent>
        )}
      </Tooltip>
    </TooltipProvider>
  );
};

export default function Sidebar() {
  const location = useLocation();
  const navigate = useNavigate();
  const { credential, clearCredentials, isAdmin } = useAppStore();
  const [isCollapsed, setIsCollapsed] = useState(false);

  const Logo = withBasePath("logo.svg");

  const sidebarItems = [
    ...(isAdmin ? [{ icon: LayoutDashboard, label: "Overview", to: "/" }] : []), // Admin Only
    { icon: Database, label: "Explorer", to: "/explorer" }, // Everyone
    ...(isAdmin ? [{ icon: Activity, label: "Metrics", to: "/metrics" }] : []), // Admin Only
    { icon: FileText, label: "Logs", to: "/logs" }, // Everyone
    ...(isAdmin ? [{ icon: Shield, label: "Admin", to: "/admin" }] : []), // Admin Only
    { icon: Settings, label: "Settings", to: "/settings" }, // Everyone (with internal guards)
  ];

  const handleLogout = () => {
    clearCredentials();
    navigate("/login");
  };

  return (
    <motion.div
      animate={{ width: isCollapsed ? 80 : 256 }}
      className="relative z-20 flex flex-col border-r border-white/10 bg-black/20 backdrop-blur-xl h-full transition-all duration-300 ease-in-out"
    >
      {/* Toggle Button */}
      <Button
        variant="ghost"
        size="icon"
        className="absolute -right-3 top-6 z-30 h-6 w-6 rounded-full border border-white/10 bg-[#1a1a1a] text-gray-400 hover:text-white shadow-md hover:bg-purple-500/20"
        onClick={() => setIsCollapsed(!isCollapsed)}
      >
        {isCollapsed ? <ChevronRight className="h-3 w-3" /> : <ChevronLeft className="h-3 w-3" />}
      </Button>

      <div className={cn("flex items-center gap-3 p-6", isCollapsed && "justify-center p-4")}>
        <div className="flex h-10 w-10 shrink-0 items-center justify-center">
          <img src={Logo} alt="Logo" className="w-8 h-8 object-contain drop-shadow-[0_0_10px_rgba(255,200,0,0.2)]" />
        </div>
        {!isCollapsed && (
          <div className="flex flex-col overflow-hidden">
            <span className="font-bold text-lg bg-gradient-to-r from-white to-gray-400 bg-clip-text text-transparent truncate">
              ClickHouse UI
            </span>
            <span className="text-xs text-gray-500 font-medium truncate">
              ClickHouse Client
            </span>
          </div>
        )}
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-4 scrollbar-hide">
        <nav className="flex flex-col gap-2">

          {!isCollapsed && <div className="px-2 mb-2 text-xs font-semibold text-gray-600 uppercase tracking-wider">Menu</div>}

          {sidebarItems.map((item) => (
            <SidebarItem
              key={item.to + item.label}
              icon={item.icon}
              label={item.label}
              to={item.to}
              isActive={location.pathname === item.to}
              isCollapsed={isCollapsed}
            />
          ))}
        </nav>
      </div>

      <div className="p-4 mt-auto border-t border-white/5 bg-black/20">
        {!isCollapsed && <div className="px-2 mb-2 text-xs font-semibold text-gray-600 uppercase tracking-wider">Connection</div>}

        <div className={cn("flex items-center gap-3 rounded-xl bg-white/5 p-3 mb-3 border border-white/5", isCollapsed && "justify-center bg-transparent border-0 p-0 mb-4")}>
          {!isCollapsed ? (
            <>
              <div className="h-8 w-8 rounded-full bg-gradient-to-br from-green-500/20 to-emerald-500/20 flex items-center justify-center ring-1 ring-white/10">
                <Server className="h-4 w-4 text-green-400" />
              </div>
              <div className="flex flex-col overflow-hidden">
                <span className="text-sm font-medium text-white truncate w-32" title={credential?.username}>
                  {credential?.username || "Connected"}
                </span>
                <span className="text-xs text-green-400 flex items-center gap-1">
                  <span className="w-1.5 h-1.5 rounded-full bg-green-400 animate-pulse" />
                  Online
                </span>
              </div>
            </>
          ) : (
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger>
                  <div className="h-8 w-8 rounded-full bg-gradient-to-br from-green-500/20 to-emerald-500/20 flex items-center justify-center ring-1 ring-white/10 relative">
                    <Server className="h-4 w-4 text-green-400" />
                    <span className="absolute top-0 right-0 w-2 h-2 rounded-full bg-green-400 border border-black" />
                  </div>
                </TooltipTrigger>
                <TooltipContent side="right">Connected as {credential?.username}</TooltipContent>
              </Tooltip>
            </TooltipProvider>
          )}
        </div>

        <Button
          variant="ghost"
          className={cn(
            "w-full gap-2 text-red-400 hover:text-red-300 hover:bg-red-500/10 transition-colors",
            isCollapsed && "justify-center px-0"
          )}
          onClick={handleLogout}
        >
          <LogOut className="h-4 w-4" />
          {!isCollapsed && "Disconnect"}
        </Button>
      </div>
    </motion.div>
  );
}
