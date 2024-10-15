"use client";

import { HeroSection } from "./HeroSection";
import HomepageFeatures from "./HomePageFeatures";
import CarouselScreenShots from "../CarouselScreenShots";

export const IndexPage = () => (
  <section className="">
    <div className=" m-auto">
      <HeroSection />
      <CarouselScreenShots />
      <HomepageFeatures />
    </div>
  </section>
);
