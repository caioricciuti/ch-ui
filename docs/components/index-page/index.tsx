import { HeroSection } from "./HeroSection";
import HomepageFeatures from "./HomePageFeatures";
import { ShowCase } from "./ShowCase";

export const IndexPage = () => (
  <section className="">
    <div className=" m-auto">
      <HeroSection />
      <ShowCase />
      <HomepageFeatures />
    </div>
  </section>
);
