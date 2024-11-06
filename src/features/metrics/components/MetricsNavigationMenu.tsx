import * as React from "react";
import { Link, useLocation, LinkProps, useNavigate } from "react-router-dom";
import { metrics } from "@/features/metrics/config/metricsConfig";
import { cn } from "@/lib/utils";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import {
  NavigationMenu,
  NavigationMenuContent,
  NavigationMenuItem,
  NavigationMenuLink,
  NavigationMenuList,
  NavigationMenuTrigger,
} from "@/components/ui/navigation-menu";

export function MetricsNavigationMenu() {
  const navigate = useNavigate();

  const handleMetricClick = (metric: any) => {
    const scope = metric.scope;
    navigate(scope ? `/metrics?scope=${scope}` : "/metrics");
  };

  return (
    <div className="flex items-center justify-between w-full container p-2">
      <NavigationMenu>
        <NavigationMenuList>
          <NavigationMenuItem>
            <NavigationMenuTrigger>Metrics</NavigationMenuTrigger>
            <NavigationMenuContent>
              <ul className="grid w-[400px] gap-3 p-4 md:w-[500px] md:grid-cols-2 lg:w-[600px] z-50">
                {metrics.map((metric) => (
                  <ListItem
                    key={metric.title}
                    title={metric.title}
                    icon={metric.icon}
                    onClick={() => handleMetricClick(metric)}
                  >
                    {metric.description}
                  </ListItem>
                ))}
              </ul>
            </NavigationMenuContent>
          </NavigationMenuItem>
        </NavigationMenuList>
      </NavigationMenu>
      <DynamicBreadcrumb />
    </div>
  );
}

const DynamicBreadcrumb = () => {
  const location = useLocation();
  const scope = new URLSearchParams(location.search).get("scope");

  const breadcrumbItems = [
    { label: "Metrics", to: "/metrics" },
    ...(scope
      ? [
          {
            label: scope.charAt(0).toUpperCase() + scope.slice(1),
            to: `/metrics?scope=${scope}`,
          },
        ]
      : []),
  ];

  return (
    <Breadcrumb>
      <BreadcrumbList>
        {breadcrumbItems.map((item, index) => (
          <React.Fragment key={item.to}>
            {index > 0 && <BreadcrumbSeparator />}
            <BreadcrumbItem>
              {index === breadcrumbItems.length - 1 ? (
                <BreadcrumbPage>{item.label}</BreadcrumbPage>
              ) : (
                <BreadcrumbLink asChild>
                  <Link to={item.to}>{item.label}</Link>
                </BreadcrumbLink>
              )}
            </BreadcrumbItem>
          </React.Fragment>
        ))}
      </BreadcrumbList>
    </Breadcrumb>
  );
};

interface ListItemProps extends Omit<LinkProps, "to"> {
  title: string;
  icon: React.ElementType;
}

const ListItem = React.forwardRef<HTMLAnchorElement, ListItemProps>(
  (
    { className, title, children, icon: IconComponent, onClick, ...props },
    ref
  ) => {
    return (
      <li>
        <NavigationMenuLink asChild>
          <a
            ref={ref}
            className={cn(
              "block select-none rounded-md p-3 leading-none no-underline outline-none transition-colors hover:bg-accent hover:text-accent-foreground focus:bg-accent focus:text-accent-foreground",
              className
            )}
            onClick={onClick}
            {...props}
          >
            <div className="grid grid-cols-[auto,1fr] items-center gap-4">
              <div className="text-2xl leading-none">
                {IconComponent && React.createElement(IconComponent)}
              </div>
              <div>
                <div className="text-sm font-medium leading-none">{title}</div>
                <p className="line-clamp-2 text-sm leading-snug text-muted-foreground">
                  {children}
                </p>
              </div>
            </div>
          </a>
        </NavigationMenuLink>
      </li>
    );
  }
);

ListItem.displayName = "ListItem";

export default MetricsNavigationMenu;
