"use client";
import {
  Carousel,
  CarouselContent,
  CarouselItem,
  CarouselNext,
  CarouselPrevious,
} from "../ui/carousel";
import { Card, CardContent } from "../ui/card";

const screenshotPairs = [
  "/screenshots/main-screen-light.png",
  "/screenshots/main-screen-dark.png",
  "/screenshots/metrics-table-options-light.png",
  "/screenshots/metrics-table-options-dark.png",
  "/screenshots/metrics-queries-light.png",
  "/screenshots/metrics-queries-dark.png",
  "/screenshots/metrics-overview-light.png",
  "/screenshots/metrics-overview-dark.png",
  "/screenshots/command-open-light.png",
  "/screenshots/command-open-dark.png",
  "/screenshots/settings-connected-light.png",
  "/screenshots/settings-connected-dark.png",
  "/screenshots/settings-disconnect-light.png",
  "/screenshots/settings-disconnect-dark.png",
];

const CarouselScreenShots = () => {
  return (
    <div className="container mx-auto mb-12 md:mb-24 lg:mb-32">
      <h2
        className="text-4xl text-center mb-20 font-extrabold tracking-tight sm:text-5xl md:text-6xl lg:text-7xl bg-clip-text
       text-transparent bg-gradient-to-r from-orange-700 via-red-500 to-orange-300"
      >
        App Screenshots
      </h2>
      <Carousel className="w-full max-w-[90vw] md:max-w-[80vw] lg:max-w-[70vw] xl:max-w-[1200px] mx-auto">
        <CarouselContent>
          {screenshotPairs.map((item, index) => (
            <CarouselItem key={index}>
              <div className="p-1">
                <Card>
                  <CardContent className="flex items-center justify-center p-2 md:p-4 lg:p-6">
                    <img
                      src={item}
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
    </div>
  );
};

export default CarouselScreenShots;
