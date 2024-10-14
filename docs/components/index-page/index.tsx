"use client";

import { HeroSection } from "./HeroSection";
import HomepageFeatures from "./HomePageFeatures";
import { ShowCase } from "./ShowCase";
import CarouselScreenShots from "../CarouselScreenShots";

export const IndexPage = () => (
  <section className="">
    <div className=" m-auto">
      <HeroSection />
      <ShowCase />
      <HomepageFeatures />
      <CarouselScreenShots />
    </div>
  </section>
);
