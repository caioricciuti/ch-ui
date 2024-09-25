// src/components/MetricsNavigationMenu.tsx
import React from "react";
import {
  NavigationMenu,
  NavigationMenuList,
  NavigationMenuItem,
  NavigationMenuTrigger,
  NavigationMenuContent,
  NavigationMenuLink,
} from "@/components/ui/navigation-menu";
import { Link } from "react-router-dom";
import { menuItemsConfig } from "@/lib/metricsConfig";
import MetricRefresh from "@/components/MetricRefresh";
import { cn } from "@/lib/utils";

const MetricsNavigationMenu: React.FC = () => {
  return (
    <NavigationMenu className="max-w-screen-xl mx-auto px-4">
      <NavigationMenuList className="space-x-4">
        {menuItemsConfig.map((item) => (
          <NavigationMenuItem key={item.title}>
            {item.items ? (
              <>
                <NavigationMenuTrigger className="flex items-center space-x-2">
                  <item.icon className="w-4 h-4" />
                  <span>{item.title}</span>
                </NavigationMenuTrigger>
                <NavigationMenuContent>
                  <ul className="grid w-[400px] gap-3 p-4 md:w-[500px] md:grid-cols-2 lg:w-[600px]">
                    {item.items.map((subItem) => (
                      <ListItem
                        key={subItem.title}
                        title={subItem.title}
                        href={subItem.href}
                        icon={
                          subItem.icon as React.ComponentType<{
                            className?: string;
                          }>
                        }
                      >
                        {subItem.description}
                      </ListItem>
                    ))}
                  </ul>
                </NavigationMenuContent>
              </>
            ) : (
              <NavigationMenuLink asChild>
                <Link
                  to={item.href}
                  className="flex items-center space-x-2 p-2 rounded-md hover:bg-gray-100 dark:hover:bg-gray-800"
                >
                  <item.icon className="w-4 h-4" />
                  <span>{item.title}</span>
                </Link>
              </NavigationMenuLink>
            )}
          </NavigationMenuItem>
        ))}
      </NavigationMenuList>
      <div className="ml-auto">
        <MetricRefresh />
      </div>
    </NavigationMenu>
  );
};

interface ListItemProps {
  title: string;
  href: string;
  icon: React.ComponentType<{ className?: string }>;
  children: React.ReactNode;
}

const ListItem = React.forwardRef<
  React.ElementRef<"a">,
  React.ComponentPropsWithoutRef<"a"> & ListItemProps
>(({ className, title, children, href, icon: Icon, ...props }, ref) => {
  return (
    <li>
      <NavigationMenuLink asChild>
        <Link
          ref={ref}
          to={href}
          className={cn(
            "block select-none space-y-1 rounded-md p-3 leading-none no-underline outline-none transition-colors hover:bg-gray-100 focus:bg-gray-100 dark:hover:bg-gray-800 dark:focus:bg-gray-800",
            className
          )}
          {...props}
        >
          <div className="flex items-center space-x-2">
            <Icon className="w-4 h-4" />
            <div className="text-sm font-medium leading-none">{title}</div>
          </div>
          <p className="line-clamp-2 text-sm leading-snug text-gray-500 dark:text-gray-400">
            {children}
          </p>
        </Link>
      </NavigationMenuLink>
    </li>
  );
});
ListItem.displayName = "ListItem";

export default MetricsNavigationMenu;
