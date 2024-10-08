import { ReactNode } from "react";
import { ArrowRightIcon } from "lucide-react";
import Image from "next/image";
import { cn } from "@/lib/utils";
import { Link } from "nextra-theme-docs";
import { useTheme } from "next-themes";

const BentoGrid = ({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) => {
  return (
    <div
      className={cn(
        "grid w-full auto-rows-[22rem] grid-cols-3 gap-4",
        className
      )}
    >
      {children}
    </div>
  );
};

const BentoCard = ({
  name,
  className,
  background,
  Icon,
  description,
  href,
  cta,
}: {
  name: string;
  className: string;
  background: ReactNode | { light: string; dark: string };
  Icon: any;
  description: string;
  href: string;
  cta: string;
}) => {
  const { theme } = useTheme();

  const getBackgroundImage = () => {
    if (
      typeof background === "object" &&
      background &&
      "light" in background &&
      "dark" in background
    ) {
      return theme === "dark" || theme === "system"
        ? background.dark
        : background.light;
    }
    return null;
  };

  const backgroundImage = getBackgroundImage();

  return (
    <div
      key={name}
      className={cn(
        "group relative col-span-3 flex flex-col justify-between overflow-hidden rounded-xl",
        "bg-white [box-shadow:0_0_0_1px_rgba(0,0,0,.03),0_2px_4px_rgba(0,0,0,.05),0_12px_24px_rgba(0,0,0,.05)]",
        "transform-gpu dark:bg-black dark:[border:1px_solid_rgba(255,255,255,.1)] dark:[box-shadow:0_-20px_80px_-20px_#ffffff1f_inset]",
        className
      )}
    >
      {backgroundImage && (
        <div className="absolute inset-0 z-0">
          <Image
            src={backgroundImage}
            alt={name}
            layout="fill"
            objectFit="cover"
            className="blur-[1px] opacity-30"
          />
        </div>
      )}
      <div className="relative z-10">
        {typeof background !== "object" && background}
      </div>
      <div className="pointer-events-none z-10 flex transform-gpu flex-col gap-1 p-6 transition-all duration-300 group-hover:-translate-y-10">
        <Icon className="h-14 w-14 origin-left transform-gpu text-neutral-700 transition-all duration-300 ease-in-out group-hover:scale-75 dark:text-neutral-200" />
        <h3 className="text-2xl font-extrabold text-neutral-700 dark:text-neutral-200">
          {name}
        </h3>
        <p className="max-w-lg font-bold text-neutral-500 dark:text-neutral-300">
          {description}
        </p>
      </div>

      <div
        className={cn(
          "z-10 cursor-pointer absolute bottom-0 flex w-full translate-y-10 transform-gpu flex-row items-center p-4 opacity-0 transition-all duration-300 group-hover:translate-y-0 group-hover:opacity-100"
        )}
      >
        <Link
          href={href}
          className="flex items-center no-underline font-bold"
        >
          {cta}
          <ArrowRightIcon className="ml-2 h-4 w-4" />
        </Link>
      </div>
      <div className="pointer-events-none absolute inset-0 transform-gpu transition-all duration-300 group-hover:bg-black/[.03] group-hover:dark:bg-neutral-800/10" />
    </div>
  );
};

export { BentoCard, BentoGrid };
