import { useState } from "react";
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
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import {
  Menu,
  LogOut,
  CogIcon,
  Users,
  Layers,
  BarChart3,
  Shield,
  Key,
  SearchCode,
} from "lucide-react";
import Logo from "/logo.png";
import useAuthStore from "@/stores/user.store";
import OrganizationCredentialSelector from "@/components/OrganizationCredentialSelector";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { ScrollArea } from "@/components/ui/scroll-area";
import { getInitials, bgColorsByInitials } from "@/lib/helpers";

const Navbar = () => {
  const { user, logout, admin } = useAuthStore();
  const navigate = useNavigate();
  const location = useLocation();
  const [isOpen, setIsOpen] = useState(false);

  const handleLogout = async () => {
    try {
      await logout();
      navigate("/login");
      toast.success("Logged out successfully");
    } catch (error) {
      console.error("Logout failed:", error);
      toast.error("Logout failed. Please try again.");
    }
  };

  const navItems = [
    { to: "/credentials", label: "Credentials", icon: Key },
    { to: "/organizations", label: "Organizations", icon: Users },
    { to: "/workspace", label: "Workspace", icon: Layers },
    { to: "/metrics", label: "Metrics", icon: BarChart3 },
  ];

  const isActive = (path: string) => location.pathname === path;

  const NavContent = ({ mobile = false, onItemClick = () => {} }) => (
    <>
      {admin() && (
        <Link
          to="/admin"
          className={`flex items-center px-3 py-2 rounded-md text-sm font-medium transition-colors ${
            isActive("/admin")
              ? "bg-secondary text-secondary-foreground"
              : "hover:bg-secondary/80"
          } ${mobile ? "w-full" : ""}`}
          onClick={onItemClick}
        >
          <Shield className="xl:mr-2 h-4 w-4" />
          <span className="hidden xl:block">Admin</span>
        </Link>
      )}
      {navItems.map((item) => (
        <Link
          key={item.to}
          to={item.to}
          className={`flex items-center px-3 py-2 rounded-md text-sm font-medium transition-colors ${
            isActive(item.to)
              ? "bg-secondary text-secondary-foreground"
              : "hover:bg-secondary/80"
          } ${mobile ? "w-full" : ""}`}
          onClick={onItemClick}
        >
          <item.icon className="mr-2 h-4 w-4" />
          {item.label}
        </Link>
      ))}
    </>
  );

  return (
    <nav className="sticky top-0 z-40 w-full border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <div className="container flex h-16 items-center justify-between lg:justify-between">
        <Sheet open={isOpen} onOpenChange={setIsOpen}>
          <SheetTrigger asChild>
            <Button
              variant="outline"
              size="icon"
              className="lg:hidden"
              aria-label="Open menu"
            >
              <Menu className="h-5 w-5" />
            </Button>
          </SheetTrigger>
          <SheetContent side="left" className="w-[300px] sm:w-[400px]">
            <SheetHeader>
              <SheetTitle>Menu</SheetTitle>
            </SheetHeader>
            <ScrollArea className="my-4 h-[calc(100vh-8rem)] pb-10 pl-6">
              <div className="flex flex-col space-y-3">
                <NavContent mobile onItemClick={() => setIsOpen(false)} />
              </div>
              <div className="mt-6 space-y-3">
                <OrganizationCredentialSelector />
              </div>
            </ScrollArea>
          </SheetContent>
        </Sheet>

        <div className="flex items-center lg:hidden">
          <Link to="/" className="flex items-center space-x-2">
            <img src={Logo} alt="Logo" className="h-8 w-8 min-h-8 min-w-8" />
            <span className="font-bold text-lg">CH-UI</span>
          </Link>
        </div>

        <div className="hidden lg:flex items-center xl:space-x-4">
          <Link to="/">
            <img src={Logo} alt="Logo" className="h-8 w-8" />
          </Link>
          <nav className="flex items-center space-x-4 text-sm font-medium">
            <NavContent />
          </nav>
        </div>

        <div className="flex items-center space-x-4">
          <div className="hidden lg:flex space-x-2">
            <OrganizationCredentialSelector />
          </div>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                className="relative h-8 w-8 rounded-full"
                aria-label="User menu"
              >
                <Avatar>
                  <AvatarFallback
                    className={`h-10 w-10 font-bold ${bgColorsByInitials(
                      getInitials(user?.name || "")
                    )}`}
                  >
                    {getInitials(user?.name || "")}
                  </AvatarFallback>
                </Avatar>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent className="w-56" align="end" forceMount>
              <DropdownMenuItem className="flex flex-col items-start">
                <div className="text-sm font-medium">{user?.name}</div>
                <div className="text-xs text-muted-foreground">
                  {user?.email}
                </div>
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
      </div>
    </nav>
  );
};

export default Navbar;
