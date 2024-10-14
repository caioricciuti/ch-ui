import React, { useEffect, useState } from "react";
import {
  Carousel,
  CarouselContent,
  CarouselItem,
  CarouselNext,
  CarouselPrevious,
} from "@/components/ui/carousel";
import { Card, CardContent } from "@/components/ui/card";
import { useTheme } from "next-themes";

const screenshotPairs = [
  {
    light: "/screenshots/main-screen-light.png",
    dark: "/screenshots/main-screen-dark.png",
  },
  {
    light: "/screenshots/metrics-table-options-light.png",
    dark: "/screenshots/metrics-table-options-dark.png",
  },
  {
    light: "/screenshots/metrics-queries-light.png",
    dark: "/screenshots/metrics-queries-dark.png",
  },
  {
    light: "/screenshots/metrics-overview-light.png",
    dark: "/screenshots/metrics-overview-dark.png",
  },
  {
    light: "/screenshots/command-open-light.png",
    dark: "/screenshots/command-open-dark.png",
  },
  {
    light: "/screenshots/settings-connected-light.png",
    dark: "/screenshots/settings-connected-dark.png",
  },
  {
    light: "/screenshots/settings-disconnect-light.png",
    dark: "/screenshots/settings-disconnect-dark.png",
  },
];

const CarouselScreenShots = () => {
  const { resolvedTheme } = useTheme();
  const [currentTheme, setCurrentTheme] = useState(resolvedTheme);

  useEffect(() => {
    setCurrentTheme(resolvedTheme);
  }, [resolvedTheme]);

  return (
    <Carousel className="w-full max-w-[90vw] md:max-w-[80vw] lg:max-w-[70vw] xl:max-w-[1200px] mx-auto">
      <CarouselContent>
        {screenshotPairs.map((pair, index) => (
          <CarouselItem key={index}>
            <div className="p-1">
              <Card>
                <CardContent className="flex items-center justify-center p-2 md:p-4 lg:p-6">
                  <img
                    src={currentTheme === "dark" ? pair.dark : pair.light}
                    alt={`Screenshot ${index + 1}`}
                    className="w-full h-auto object-contain max-h-[70vh]"
                  />
                </CardContent>
              </Card>
            </div>
          </CarouselItem>
        ))}
      </CarouselContent>
      <CarouselPrevious />
      <CarouselNext />
    </Carousel>
  );
};

export default CarouselScreenShots;