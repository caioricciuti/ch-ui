"use client";

import { HeroSection } from "./HeroSection";
import HomepageFeatures from "./HomePageFeatures";
import CarouselScreenShots from "./CarouselScreenShots";
import Sponsors from "./Sponsors";

export const IndexPage = () => (
  <div className=" m-auto">
    <HeroSection />
    <CarouselScreenShots />
    <HomepageFeatures />
    <Sponsors />
  </div>
);
