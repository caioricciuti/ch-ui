"use client";

import { HeroSection } from "./HeroSection";
import HomepageFeatures from "./HomePageFeatures";
import CarouselScreenShots from "./CarouselScreenShots";
import Sponsors from "./Sponsors";
import Updates from "./Updates";
export const IndexPage = () => (
  <div className=" m-auto">
    <HeroSection />
    <Updates />
    <CarouselScreenShots />
    <HomepageFeatures />
    <Sponsors />
  </div>
);
